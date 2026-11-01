/*
CallbackService 是全局定时回调中心：callbacks.json 的 CRUD、每秒到点
触发与执行。三类任务——向当前会话投递消息（可每次新建会话）、操作
快应用（自动启动后端并调用 app.quick 声明的回调端点），或在工作目录
执行 shell 脚本。

执行器不切换活动会话：后台会话照常跑完，事件进各自时间线。会话任务
绑定创建时的当前叶 session ID，投递复用 ChatService 的打断式发送：
正在跑轮先取消再发送，归档/compact 换代后该会话不可用，任务失败，不
继承到新会话。脚本任务在工作目录执行，输出尾部与退出码进运行记录。
一次性任务执行后自动停用；间隔/cron 从完成时刻重新计下一轮。
每次触发（含失败/跳过）写一条运行记录并进通知栏（待用户确认）：会话
投递成功时点通知直达目标会话，其余点通知打开回调页并展开该任务。
*/
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"ezharness/core/internal/callback"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
	"ezharness/core/internal/quickapp"
)

/* ErrCallbackInvalid 表示任务配置不合法（controller 映射 400）。 */
var ErrCallbackInvalid = errors.New("定时回调配置无效")

/* ErrCallbackRunning 表示任务正在执行，不能重复手动触发。 */
var ErrCallbackRunning = errors.New("定时回调正在执行")

/* callbackTick 是调度器扫描周期。 */
const callbackTick = time.Second

/* callbackHTTPTimeout 是调用快应用端点的单次请求上限。 */
const callbackHTTPTimeout = 30 * time.Second

/* callbackLaunchWait 是端点连接失败的等待窗口（等后端冷启动）。 */
const callbackLaunchWait = 15 * time.Second

/* callbackNoticeLimit 是通知栏保留的触发通知条数。 */
const callbackNoticeLimit = 20

/* callbackScriptTimeout 是单次脚本执行的时长上限。 */
const callbackScriptTimeout = 10 * time.Minute

/* callbackScriptOutput 是脚本输出的保留上限（超出丢弃最旧部分）。 */
const callbackScriptOutput = 64 * 1024

/* CallbackBrief 是状态卡展示的当前会话任务摘要。 */
type CallbackBrief struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Schedule   string `json:"schedule"`
	Enabled    bool   `json:"enabled"`
	NextRunAt  int64  `json:"nextRunAt,omitempty"`
	LastStatus string `json:"lastStatus,omitempty"`
}

/* CallbackService 定时回调用例与调度器。 */
type CallbackService struct {
	Hub    *domain.Hub
	Agents *AgentService
	Topics *TopicService
	Apps   *AppsService
	Chat   *ChatService
	Store  *callback.Store

	mu      sync.Mutex
	running map[string]bool
	notices []domain.PendingNotice // 触发通知（内存，关闭即读）
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	now     func() time.Time
	client  *http.Client
}

/* NewCallbackService 读取 callbacks.json 并构造服务（损坏文件降级为空，仍可启动）。 */
func NewCallbackService(hub *domain.Hub, agents *AgentService, topics *TopicService,
	apps *AppsService, chat *ChatService, fsys osfs.OS) *CallbackService {
	store, err := callback.Load(context.Background(), fsys)
	if err != nil {
		log.Printf("定时回调加载失败（%s）: %v", callback.FileName, err)
	}
	return &CallbackService{
		Hub: hub, Agents: agents, Topics: topics, Apps: apps, Chat: chat, Store: store,
		running: map[string]bool{},
		now:     time.Now,
		client: &http.Client{
			Timeout: callbackHTTPTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse // 端点不跟随重定向：结果可预期，不把请求带去别处
			},
		},
	}
}

/* Start 校正启动态并开始每秒扫描（每代一次，幂等）。 */
func (s *CallbackService) Start() {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	s.mu.Unlock()

	if err := s.Store.Normalize(s.now()); err != nil {
		log.Printf("定时回调启动校正失败: %v", err)
	}
	s.wg.Add(1)
	go s.loop(ctx)
}

/* Stop 停止调度并等待执行中的任务收尾（换代切目录前必须先停）。 */
func (s *CallbackService) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.ctx, s.cancel = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

/* loop 每秒扫描到期任务。 */
func (s *CallbackService) loop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(callbackTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fireDue(ctx)
		}
	}
}

/* fireDue 触发全部到期且未在运行的任务。 */
func (s *CallbackService) fireDue(ctx context.Context) {
	now := s.now().UnixMilli()
	for _, task := range s.Store.List() {
		if !task.Enabled || task.NextRunAt <= 0 || task.NextRunAt > now {
			continue
		}
		s.startTask(ctx, task, false)
	}
}

/* startTask 占任务执行位并异步执行；已在运行/服务已停返回 false。 */
func (s *CallbackService) startTask(ctx context.Context, task callback.Task, manual bool) bool {
	s.mu.Lock()
	if s.ctx == nil || s.running[task.ID] {
		s.mu.Unlock()
		return false
	}
	s.running[task.ID] = true
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer s.endTask(task.ID)
		s.run(ctx, task, manual)
	}()
	return true
}

/* endTask 释放任务执行位。 */
func (s *CallbackService) endTask(id string) {
	s.mu.Lock()
	delete(s.running, id)
	s.mu.Unlock()
}

/* callbackRun 是一次执行向运行记录贡献的字段；RootID 供通知点击后打开会话。 */
type callbackRun struct {
	Target   string
	TargetID string
	RootID   string
	Content  string
	Result   string
}

/*
run 执行一条任务并写回运行态。manual=true 是页面/工具的“立即运行”，
不推进计划（不动 Enabled/NextRunAt）。每次执行（成功/失败/跳过）都追加
一条记录，并写一条通知栏 info。
*/
func (s *CallbackService) run(ctx context.Context, task callback.Task, manual bool) {
	started := s.now()
	if err := s.Store.SetRuntime(context.Background(), task.ID, func(t *callback.Task) {
		t.LastStatus = callback.StatusRunning
		t.LastRunAt = started.UnixMilli()
		t.RunCount++
		if !manual {
			t.NextRunAt = 0 // 执行期间不再被扫描到
		}
	}); err != nil {
		return // 任务已被删除/落盘失败：不再执行
	}

	outcome, err := s.execute(ctx, task, started)
	if outcome.Target == "" {
		outcome.Target = task.TargetLabel()
	}
	if outcome.Content == "" {
		outcome.Content = task.ContentSummary()
	}
	status := callback.StatusOK
	if err != nil {
		status = callback.StatusError
		outcome.Result = err.Error()
	}
	result := oneLine(outcome.Result, 500)
	duration := s.now().Sub(started).Milliseconds()

	_ = s.Store.SetRuntime(context.Background(), task.ID, func(t *callback.Task) {
		t.LastStatus = status
		t.LastResult = result
		t.Runs = callback.PushRun(t.Runs, callback.Run{
			At: started.UnixMilli(), Status: status, Manual: manual,
			Target: outcome.Target, TargetID: outcome.TargetID,
			Content: oneLine(outcome.Content, 300), Result: result, DurationMs: duration,
		})
		if manual || !t.Enabled {
			return
		}
		if t.Schedule.Kind == callback.ScheduleOnce {
			t.Enabled = false
			t.NextRunAt = 0
			return
		}
		next, nerr := t.Schedule.Next(s.now())
		if nerr != nil || next.IsZero() {
			t.Enabled = false
			t.NextRunAt = 0
			t.LastStatus = callback.StatusError
			if nerr != nil {
				t.LastResult = nerr.Error()
			} else {
				t.LastResult = "计划表达式没有可运行的下一次"
			}
			return
		}
		t.NextRunAt = next.UnixMilli()
	})
	s.addRunNotice(task, status, outcome, result)
}

/* execute 按任务类型执行。 */
func (s *CallbackService) execute(ctx context.Context, task callback.Task, started time.Time) (callbackRun, error) {
	switch task.Kind {
	case callback.KindSession:
		return s.runSession(ctx, task, started)
	case callback.KindApp:
		return s.runApp(ctx, task)
	case callback.KindScript:
		return s.runScript(ctx, task)
	}
	return callbackRun{}, fmt.Errorf("未知任务类型 %q", task.Kind)
}

/* runSession 向绑定的叶会话或新建会话投递一条带回调元数据的消息。 */
func (s *CallbackService) runSession(ctx context.Context, task callback.Task, started time.Time) (callbackRun, error) {
	target := task.Session
	out := callbackRun{}
	if target == nil {
		return out, fmt.Errorf("会话目标缺失")
	}
	out.Content = target.Message
	text := callback.FormatMessage(task, target.Message, started)
	if target.Mode == callback.ModeNew {
		out.Target = "新建会话"
		sess := s.Hub.CreateBranch()
		s.Agents.Assemble(sess, s.Hub.SettingsSnapshot())
		if err := s.Chat.Send(sess.RootID, text, nil); err != nil {
			s.Hub.Unregister(sess.RootID)
			return out, fmt.Errorf("新建会话发送失败: %w", err)
		}
		out.TargetID = sess.RootID
		out.RootID = sess.RootID
		out.Result = "已新建会话 " + sess.RootID + " 并发送消息"
		return out, nil
	}

	out.Target = "会话 " + target.SessionID
	out.TargetID = target.SessionID
	sess, err := s.Topics.EnsureSession(ctx, target.SessionID)
	if err != nil {
		return out, sessionTargetError(target.SessionID, err)
	}
	out.RootID = sess.RootID
	if sess.CurrentID() != target.SessionID {
		return out, fmt.Errorf("目标会话已归档或已换代（不是创建任务时的会话），本次未发送")
	}
	interrupted := sess.Busy()
	if err := s.Chat.SendSession(sess.RootID, target.SessionID, text, nil); err != nil {
		switch {
		case errors.Is(err, domain.ErrSessionChanged):
			return out, fmt.Errorf("目标会话已归档或已换代（不是创建任务时的会话），本次未发送")
		case errors.Is(err, domain.ErrBusy):
			return out, fmt.Errorf("目标会话正在归档或无法结束当前轮，本次未发送")
		default:
			return out, fmt.Errorf("发送消息失败: %w", err)
		}
	}
	out.Result = "已发送到会话 " + target.SessionID
	if interrupted {
		out.Result = "已打断上一轮；" + out.Result
	}
	return out, nil
}

/* sessionTargetError 把按会话定位的失败翻译成任务记录里的原因。 */
func sessionTargetError(sessionID string, err error) error {
	switch {
	case errors.Is(err, ErrSessionArchived):
		return fmt.Errorf("目标会话已归档，不能再投递")
	case errors.Is(err, ErrSessionArchiving):
		return fmt.Errorf("目标会话正在归档，不能再投递")
	case errors.Is(err, ErrSessionSuperseded):
		return fmt.Errorf("目标会话已换代（不是当前会话），不能再投递；如仍需要请重新创建任务")
	case errors.Is(err, ErrSessionNotFound):
		return fmt.Errorf("会话 %s 不存在或已删除", sessionID)
	default:
		return fmt.Errorf("目标会话 %s 不可用: %v", sessionID, err)
	}
}

/*
runApp 找到 app.quick 的声明端点，确保后端启动后调用它。端点自身的
实现不限——可以是一个 HTTP 接口，也可以由脚本/进程提供。
*/
func (s *CallbackService) runApp(ctx context.Context, task callback.Task) (callbackRun, error) {
	target := task.App
	out := callbackRun{}
	if target == nil {
		return out, fmt.Errorf("快应用目标缺失")
	}
	info, err := quickapp.Load(ctx, s.Hub.Fsys, target.Name)
	if err != nil {
		return out, fmt.Errorf("快应用 %s 不可用: %w", target.Name, err)
	}
	out.Target = "快应用 " + info.Name + " · " + target.Callback
	out.TargetID = info.Name
	cb, ok := info.Spec.FindCallback(target.Callback)
	if !ok {
		names := make([]string, 0, len(info.Spec.Callbacks))
		for _, c := range info.Spec.Callbacks {
			names = append(names, c.Name)
		}
		if len(names) > 0 {
			return out, fmt.Errorf("快应用 %s 未声明回调 %q（可用：%s）",
				info.Name, target.Callback, strings.Join(names, "、"))
		}
		return out, fmt.Errorf("快应用 %s 未声明回调 %q（app.quick 没有 callbacks）", info.Name, target.Callback)
	}
	if err := quickapp.ValidateCallback(cb); err != nil {
		return out, fmt.Errorf("回调声明无效: %w", err)
	}
	out.Content = cb.Method + " " + cb.URL
	if len(cb.Body) > 0 {
		out.Content += " · " + oneLine(string(cb.Body), 200)
	}
	var launchErr error
	if info.Spec.HasBackend() {
		if s.Apps == nil {
			launchErr = errors.New("应用服务不可用，无法启动后端")
		} else if _, lerr := s.Apps.Launch(ctx, info.Name); lerr != nil {
			launchErr = fmt.Errorf("后端启动失败: %w", lerr)
		}
	}
	result, ierr := s.invoke(ctx, cb)
	if ierr != nil && launchErr != nil {
		return out, fmt.Errorf("%w（%v）", ierr, launchErr)
	}
	out.Result = result
	return out, ierr
}

/*
invoke 调用声明端点：连接失败时在 15 秒窗口内每 500ms 重试（等后端
冷启动）；非 2xx 立即失败。响应只留短摘要进任务运行态。
*/
func (s *CallbackService) invoke(ctx context.Context, cb quickapp.Callback) (string, error) {
	body := cb.Body
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: callbackHTTPTimeout}
	}
	deadline := time.Now().Add(callbackLaunchWait)
	for {
		reqCtx, cancel := context.WithTimeout(ctx, callbackHTTPTimeout)
		req, err := http.NewRequestWithContext(reqCtx, cb.Method, cb.URL, bytes.NewReader(body))
		if err != nil {
			cancel()
			return "", err
		}
		for k, v := range cb.Headers {
			req.Header.Set(k, v)
		}
		if len(body) > 0 && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(500 * time.Millisecond):
					continue
				}
			}
			return "", fmt.Errorf("请求失败: %w", err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4097))
		_ = resp.Body.Close()
		cancel()
		text := oneLine(string(data), 300)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if text == "" {
				return "", fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, text)
		}
		if text == "" {
			return fmt.Sprintf("HTTP %d", resp.StatusCode), nil
		}
		return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, text), nil
	}
}

/*
runScript 在 agent 工作目录执行一条 shell 命令。输出取尾部进运行记录；
退出码非零记为失败，超时或服务停止时结束进程树。
*/
func (s *CallbackService) runScript(ctx context.Context, task callback.Task) (callbackRun, error) {
	target := task.Script
	out := callbackRun{}
	if target == nil {
		return out, fmt.Errorf("脚本目标缺失")
	}
	out.Target = "脚本"
	out.Content = target.Command
	runCtx, cancel := context.WithTimeout(ctx, callbackScriptTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, shellPath(), scriptArgs(target.Command)...)
	cmd.Dir = ResolveWorkDir(s.Hub.SettingsSnapshot().WorkDir)
	cmd.WaitDelay = 5 * time.Second
	prepareScriptCommand(cmd)
	var output tailBuffer
	output.max = callbackScriptOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	text := tailLine(output.String(), 300)
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return out, fmt.Errorf("脚本超时（%s）", callbackScriptTimeout)
	case ctx.Err() != nil:
		return out, ctx.Err()
	case err != nil:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if text == "" {
				return out, fmt.Errorf("退出码 %d", exitErr.ExitCode())
			}
			return out, fmt.Errorf("退出码 %d: %s", exitErr.ExitCode(), text)
		}
		return out, fmt.Errorf("启动失败: %w", err)
	}
	if text == "" {
		out.Result = "退出码 0"
	} else {
		out.Result = "退出码 0: " + text
	}
	return out, nil
}

/* scriptArgs 按平台给出 shell 执行单条命令的参数。 */
func scriptArgs(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/C", command}
	}
	return []string{"-c", command}
}

/* tailBuffer 保留最后 max 字节的输出（长日志末段才带错误原因）。 */
type tailBuffer struct {
	data []byte
	max  int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if b.max > 0 && len(b.data) > b.max {
		b.data = append(b.data[:0], b.data[len(b.data)-b.max:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string { return string(b.data) }

/* List 返回全部任务。 */
func (s *CallbackService) List() []callback.Task { return s.Store.List() }

/*
Create 校验并创建任务：enabled 缺省 true；session 模式优先绑定显式
sessionId，未给时用调用方会话（工具传入），HTTP 面再退化到当前活动会话。
*/
func (s *CallbackService) Create(ctx context.Context, in callback.CreateInput) (callback.Task, error) {
	now := s.now()
	t := callback.Task{
		ID: callback.NewID(now), Title: in.Title, Description: in.Description,
		Kind: in.Kind, Schedule: in.Schedule,
		Session: sessionTarget(in.Session, in.BindSession), App: in.App, Script: in.Script,
		Enabled: true, CreatedAt: now.UnixMilli(),
	}
	if in.Enabled != nil {
		t.Enabled = *in.Enabled
	}
	if err := s.prepare(ctx, &t, now); err != nil {
		return callback.Task{}, err
	}
	return s.Store.Create(ctx, t)
}

/* Update 合并配置字段并校验（运行态保留）；未提供 enabled 时保持原值。 */
func (s *CallbackService) Update(ctx context.Context, id string, in callback.UpdateInput) (callback.Task, error) {
	now := s.now()
	var out callback.Task
	_, err := s.Store.Update(ctx, id, func(t *callback.Task) error {
		oldKind := t.Kind
		if in.Title != nil {
			t.Title = *in.Title
		}
		if in.Description != nil {
			t.Description = *in.Description
		}
		if in.Kind != nil {
			t.Kind = strings.ToLower(strings.TrimSpace(*in.Kind))
		}
		if in.Schedule != nil {
			t.Schedule = *in.Schedule
		}
		if in.Session != nil {
			// 显式 sessionId 优先；只改消息时保留既有绑定，从别的类型切过来才取调用方会话
			var bound string
			if t.Session != nil && t.Session.Mode == callback.ModeSession {
				bound = t.Session.SessionID
			}
			if bound == "" {
				bound = in.BindSession
			}
			t.Session = sessionTarget(in.Session, bound)
		}
		if in.App != nil {
			t.App = in.App
		}
		if in.Script != nil {
			t.Script = in.Script
		}
		// 改类型时清掉其余目标：调用方只提交新目标，旧目标不能残留
		if in.Kind != nil && t.Kind != oldKind {
			switch t.Kind {
			case callback.KindSession:
				t.App, t.Script = nil, nil
			case callback.KindApp:
				t.Session, t.Script = nil, nil
			case callback.KindScript:
				t.Session, t.App = nil, nil
			}
		}
		if in.Enabled != nil {
			t.Enabled = *in.Enabled
		}
		if err := s.prepare(ctx, t, now); err != nil {
			return err
		}
		out = *t
		return nil
	})
	if err != nil {
		return callback.Task{}, err
	}
	return out, nil
}

/* SetEnabled 启停任务；启用时重新计算下次（一次性过期拒绝）。 */
func (s *CallbackService) SetEnabled(ctx context.Context, id string, enabled bool) (callback.Task, error) {
	now := s.now()
	var out callback.Task
	_, err := s.Store.Update(ctx, id, func(t *callback.Task) error {
		if enabled {
			next, err := t.Schedule.Next(now)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrCallbackInvalid, err)
			}
			if next.IsZero() {
				if t.Schedule.Kind == callback.ScheduleOnce {
					return fmt.Errorf("%w: 一次性时间已过，请先修改时间", ErrCallbackInvalid)
				}
				return fmt.Errorf("%w: 计划表达式没有可运行的下一次", ErrCallbackInvalid)
			}
			t.NextRunAt = next.UnixMilli()
		} else {
			t.NextRunAt = 0
		}
		t.Enabled = enabled
		out = *t
		return nil
	})
	if err != nil {
		return callback.Task{}, err
	}
	return out, nil
}

/* Delete 删除任务。 */
func (s *CallbackService) Delete(ctx context.Context, id string) error {
	return s.Store.Delete(ctx, id)
}

/* RunNow 立即执行一次（不推进计划）。 */
func (s *CallbackService) RunNow(id string) error {
	t, ok := s.Store.Get(id)
	if !ok {
		return callback.ErrNotFound
	}
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("定时回调服务未启动")
	}
	if !s.startTask(ctx, t, true) {
		return ErrCallbackRunning
	}
	return nil
}

/*
Notices 返回触发通知（通知栏全局轮询数据源，新的在前）。
*/
func (s *CallbackService) Notices() []domain.PendingNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.PendingNotice, len(s.notices))
	copy(out, s.notices)
	return out
}

/* DismissNotice 关闭一条触发通知。 */
func (s *CallbackService) DismissNotice(callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, n := range s.notices {
		if n.CallID == callID {
			s.notices = append(s.notices[:i], s.notices[i+1:]...)
			return
		}
	}
}

/*
addRunNotice 为每次触发写一条待确认通知（kind=callback）：标题是任务名，
detail 带目标与结果；成功投递到会话时 target 指向会话根（点击直接跳到
会话），其余指向任务（点击打开回调页并展开该任务的记录）。
*/
func (s *CallbackService) addRunNotice(task callback.Task, status string, outcome callbackRun, result string) {
	target := "callback-task:" + task.ID
	if status == callback.StatusOK && outcome.RootID != "" {
		target = "callback-session:" + outcome.RootID
	}
	detail := outcome.Target
	if detail == "" {
		detail = task.TargetLabel()
	}
	detail += " · " + callback.StatusLabel(status)
	if result != "" {
		detail += "：" + result
	}
	args, _ := json.Marshal(map[string]string{
		"detail": oneLine(detail, 500),
		"target": target,
	})
	n := domain.PendingNotice{
		CallID: fmt.Sprintf("cb-%d", time.Now().UnixNano()),
		Kind:   "callback",
		Tool:   task.Title,
		Args:   string(args),
		Ts:     time.Now().UnixMilli(),
	}
	s.mu.Lock()
	s.notices = append([]domain.PendingNotice{n}, s.notices...)
	if len(s.notices) > callbackNoticeLimit {
		s.notices = s.notices[:callbackNoticeLimit]
	}
	s.mu.Unlock()
}

/* Brief 返回绑定到指定会话的任务（状态卡数据源）。 */
func (s *CallbackService) Brief(sessionID string) []CallbackBrief {
	if sessionID == "" {
		return nil
	}
	var out []CallbackBrief
	for _, t := range s.Store.List() {
		if t.Kind != callback.KindSession || t.Session == nil ||
			t.Session.Mode != callback.ModeSession || t.Session.SessionID != sessionID {
			continue
		}
		out = append(out, CallbackBrief{
			ID: t.ID, Title: t.Title, Kind: t.Kind, Schedule: t.Schedule.Summary(),
			Enabled: t.Enabled, NextRunAt: t.NextRunAt, LastStatus: t.LastStatus,
		})
	}
	return out
}

/* sessionTarget 把创建/更新输入转成落盘目标：mode=session 时显式 sessionId 优先，缺省用调用方绑定。 */
func sessionTarget(in *callback.SessionInput, bind string) *callback.SessionTarget {
	if in == nil {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = callback.ModeSession
	}
	t := &callback.SessionTarget{Mode: mode, Message: strings.TrimSpace(in.Message)}
	if mode == callback.ModeSession {
		t.SessionID = strings.TrimSpace(in.SessionID)
		if t.SessionID == "" {
			t.SessionID = strings.TrimSpace(bind)
		}
	}
	return t
}

/*
prepare 归一化并校验任务配置：session 模式绑定显式 sessionId（未给时
取活动会话），目标必须是仍可写的当前叶；快应用/端点必须存在；
脚本命令非空；启用时计算下一次运行时间（一次性过期在创建/启用时拒绝）。
*/
func (s *CallbackService) prepare(ctx context.Context, t *callback.Task, now time.Time) error {
	t.Normalize()
	// 绑定要在 Validate 前落值：mode=session 的输入不带会话 ID。
	if t.Kind == callback.KindSession && t.Session != nil &&
		t.Session.Mode == callback.ModeSession && t.Session.SessionID == "" {
		if active := s.Hub.ActiveSession(); active != nil {
			t.Session.SessionID = active.CurrentID()
		}
	}
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrCallbackInvalid, err)
	}
	if t.Kind == callback.KindSession && t.Session.Mode == callback.ModeSession {
		if _, err := s.Topics.EnsureSession(ctx, t.Session.SessionID); err != nil {
			return fmt.Errorf("%w: %v", ErrCallbackInvalid, sessionTargetError(t.Session.SessionID, err))
		}
	}
	if t.Kind == callback.KindApp {
		info, err := quickapp.Load(ctx, s.Hub.Fsys, t.App.Name)
		if err != nil {
			return fmt.Errorf("%w: 快应用 %s 不可用", ErrCallbackInvalid, t.App.Name)
		}
		cb, ok := info.Spec.FindCallback(t.App.Callback)
		if !ok {
			names := make([]string, 0, len(info.Spec.Callbacks))
			for _, c := range info.Spec.Callbacks {
				names = append(names, c.Name)
			}
			if len(names) > 0 {
				return fmt.Errorf("%w: 快应用 %s 未声明回调 %s（可用：%s）",
					ErrCallbackInvalid, t.App.Name, t.App.Callback, strings.Join(names, "、"))
			}
			return fmt.Errorf("%w: 快应用 %s 没有 callbacks 声明", ErrCallbackInvalid, t.App.Name)
		}
		if err := quickapp.ValidateCallback(cb); err != nil {
			return fmt.Errorf("%w: %v", ErrCallbackInvalid, err)
		}
	}
	if !t.Enabled {
		t.NextRunAt = 0
		return nil
	}
	next, err := t.Schedule.Next(now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCallbackInvalid, err)
	}
	if next.IsZero() {
		if t.Schedule.Kind == callback.ScheduleOnce {
			return fmt.Errorf("%w: 一次性时间已过，请改时间", ErrCallbackInvalid)
		}
		return fmt.Errorf("%w: 计划表达式没有可运行的下一次", ErrCallbackInvalid)
	}
	t.NextRunAt = next.UnixMilli()
	return nil
}

/* oneLine 压平换行并按 rune 截断（运行态结果与错误进 JSON 单行）。 */
func oneLine(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "\r", ""), "\n", " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

/* tailLine 压平换行并保留最后 max 个 rune（脚本输出错误通常在末尾）。 */
func tailLine(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "\r", ""), "\n", " ")
	r := []rune(s)
	if len(r) > max {
		return "…" + string(r[len(r)-max:])
	}
	return s
}
