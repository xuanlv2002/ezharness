/*
魔法看板·共享终端:多终端全局公共池(跨会话共享,个人助手语义:任何
会话都能操作全部终端)。每个终端是一个 ConPTY + shell 进程,用户
(WS 输入)与 AI(term_* 工具)共写同一终端。同一串字节有两个去向:原始字节
进环形缓冲(WS 重连 hello 回放,前端 xterm.js 自己渲染),同时重放进
termScreen(见 termscreen.go——ConPTY 发的是屏幕 diff,不按终端语义重放
就会把提示符、命令、输出糊成一行)。AI 侧两条读法都不靠持久游标:send 等
输出静默后把这次命令改动的那几行带回;read 取屏幕末尾 N 行,读多少次都是
同一份、天然幂等。终端清单供 agent_status 基线对比(每会话的 ResSnapshot
独立——A 会话首轮见到 B 会话开的终端同样报"新增",模型各自知悉全局终端
水位)。
*/
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

/* TermInfo 是终端清单条目(WS/REST/AI 清单共用)。 */
type TermInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Desc   string `json:"desc,omitempty"` // 用途描述(创建时给出,看板提示展示)
	Origin string `json:"origin"`         // 创建来源:"AI" / "用户" / "快应用·<应用名>"
	Exited bool   `json:"exited"`         // shell 已退出(连接保留可看残留输出)
}

/* termResult 是 term_* 工具的操作返回:身份/状态 + 输出 + 注记。 */
type termResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Desc   string `json:"desc,omitempty"`
	Exited bool   `json:"exited"`
	Output string `json:"output,omitempty"`
	Note   string `json:"note,omitempty"`
}

/* TermFrame 是 service → WS 订阅者的广播帧(controller 负责编码)。 */
type TermFrame struct {
	Type     string     `json:"type"` // data | terminals
	ID       string     `json:"id,omitempty"`
	Data     []byte     `json:"-"` // data 帧原始 PTY 字节
	Sessions []TermInfo `json:"sessions,omitempty"`
}

const (
	termRingSize = 256 * 1024 // 每终端输出环形缓冲
	termReadBuf  = 4096
	termSubsBuf  = 64  // 每订阅者帧队列,满丢帧(重连 hello 兜底)
	termReadLine = 100 // read 默认返回的末尾行数

	termSendQuiet = 800 * time.Millisecond // send 的静默判定窗:这么久没新输出就算命令做完了
	termSendWait  = 10 * time.Second       // send 单次等待上限(长驻命令到点先返回,后续靠 read)
	termPollStep  = 50 * time.Millisecond  // 等待轮询间隔
)

/* ── 环形缓冲 ── */

type ringBuffer struct {
	buf     []byte
	head    int // 起始下标
	size    int
	written int64 // 累计写入(取"从某点起的新增"用)
}

func newRing(n int) *ringBuffer { return &ringBuffer{buf: make([]byte, n)} }

func (r *ringBuffer) append(p []byte) {
	for len(p) > 0 {
		if r.size == len(r.buf) { // 满:按本次写入量丢最旧(腾出的空间才可写)
			advance := min(len(p), len(r.buf))
			r.head = (r.head + advance) % len(r.buf)
			r.size -= advance
		}
		tail := (r.head + r.size) % len(r.buf)
		n := min(len(r.buf)-tail, len(r.buf)-r.size, len(p))
		copy(r.buf[tail:tail+n], p[:n])
		r.size += n
		r.written += int64(n)
		p = p[n:]
	}
}

/* snapshot 有序全量副本。 */
func (r *ringBuffer) snapshot() []byte {
	out := make([]byte, 0, r.size)
	start := r.head
	for i := 0; i < r.size; {
		end := min(start+r.size-i, len(r.buf))
		out = append(out, r.buf[start:end]...)
		i += end - start
		start = 0
	}
	return out
}

/* ── 终端会话 ── */

type TermSession struct {
	ID     string
	Name   string
	Desc   string
	Origin string

	mu      sync.Mutex
	pty     pty.Pty
	cmd     *pty.Cmd
	ring    *ringBuffer   // 原始字节(WS 重连 hello 回放用)
	screen  *termScreen   // 按终端语义重放的屏幕(AI 的 send/read 用)
	lastOut time.Time     // 最近一次输出的时刻(send 的静默判定)
	condCh  chan struct{} // 有输出即 close 广播(读泵唤醒等待者)
	exited  bool
}

/* ── 服务 ── */

/* TerminalService 管理全部终端(全局单例,随换代重建)。 */
type TerminalService struct {
	mu       sync.Mutex
	seq      int
	sessions map[string]*TermSession
	subs     map[chan TermFrame]struct{}
	workDir  string
}

/* NewTerminalService 构造(workDir 与 agent shell 同目录)。 */
func NewTerminalService(workDir string) *TerminalService {
	return &TerminalService{
		sessions: map[string]*TermSession{},
		subs:     map[chan TermFrame]struct{}{},
		workDir:  workDir,
	}
}

func shellPath() string {
	if runtime.GOOS == "windows" {
		// 绝对路径:相对名会被按 cmd.Dir 解析(working dir 里没有 cmd.exe)
		if root := os.Getenv("SystemRoot"); root != "" {
			return filepath.Join(root, "System32", "cmd.exe")
		}
		return `C:\Windows\System32\cmd.exe`
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	// 桌面会话未带 SHELL 时优先 bash（交互体验接近用户日常），再退到 sh
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

/*
Create 启动一个新终端(id 形如 t1/t2);name 是简短标识,desc 是用途描述。

	cols/rows 是观看端实际列行数(0 用默认 120x30):PTY 一开始就按它排版,
	zsh 首屏提示符的部分行标记(%)按同宽擦除,窄窗下不会残留。
*/
func (s *TerminalService) Create(name, desc, origin string, cols, rows int) (*TermInfo, error) {
	return s.create(s.workDir, name, desc, origin, cols, rows)
}

/* create 是 Create 与 Launch 的公共实现;dir 是 shell 的初始目录,空则用服务 workDir。 */
func (s *TerminalService) create(dir, name, desc, origin string, cols, rows int) (*TermInfo, error) {
	if dir == "" {
		dir = s.workDir
	}
	/* 与 Resize 同界:越界(含 0)退回默认,不接受离谱尺寸 */
	if cols < 2 || rows < 2 || cols > 500 || rows > 300 {
		cols, rows = 120, 30
	}
	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("pty: %w", err)
	}
	if err := p.Resize(cols, rows); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("pty resize: %w", err)
	}
	cmd := p.Command(shellPath())
	cmd.Dir = dir
	// 覆盖而非追加 TERM：桌面启动的环境里可能是 dumb/空，追加会保留旧值
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TERM=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env, "TERM=xterm-256color")

	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("t%d", s.seq)
	if name == "" {
		name = id
	}
	sess := &TermSession{
		ID: id, Name: name, Desc: desc, Origin: origin,
		pty: p, cmd: cmd, ring: newRing(termRingSize), screen: newTermScreen(cols, rows),
		lastOut: time.Now(), condCh: make(chan struct{}),
	}
	s.sessions[id] = sess
	s.mu.Unlock()

	if err := cmd.Start(); err != nil {
		_ = p.Close()
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
		return nil, fmt.Errorf("start shell: %w", err)
	}
	go sess.readPump(s)
	go sess.waitPump(s)
	s.broadcast(TermFrame{Type: "terminals", Sessions: s.List()})
	info := *sess.info()
	return &info, nil
}

func (sess *TermSession) info() *TermInfo {
	return &TermInfo{ID: sess.ID, Name: sess.Name, Desc: sess.Desc, Origin: sess.Origin,
		Exited: sess.exited}
}

/*
readPump 持续读 PTY 输出:入 ring(WS 回放)+ 重放进屏幕(AI 读写)、刷新静默

	时钟、广播给 WS 订阅者。
*/
func (sess *TermSession) readPump(s *TerminalService) {
	buf := make([]byte, termReadBuf)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			sess.mu.Lock()
			sess.ring.append(data)
			sess.screen.write(data)
			sess.lastOut = time.Now()
			close(sess.condCh) // 唤醒 send 的静默等待
			sess.condCh = make(chan struct{})
			sess.mu.Unlock()
			s.broadcast(TermFrame{Type: "data", ID: sess.ID, Data: data})
		}
		if err != nil {
			return
		}
	}
}

/* waitPump 等 shell 退出:标记 exited 并广播清单(前端显示状态点)。 */
func (sess *TermSession) waitPump(s *TerminalService) {
	_ = sess.cmd.Wait()
	sess.mu.Lock()
	sess.exited = true
	sess.mu.Unlock()
	s.broadcast(TermFrame{Type: "terminals", Sessions: s.List()})
}

func (s *TerminalService) get(id string) (*TermSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

/*
Get 按 id 返回终端,id 必填。

**没有"最近使用"兜底**:隐式状态只会让模型读错终端(新建之后省略 id 会打到
上一个用过的终端),要哪个终端就写哪个,`term_tab action=list` 查得到。
*/
func (s *TerminalService) Get(id string) (*TermSession, error) {
	if id == "" {
		return nil, fmt.Errorf("需要指定终端 id(term_tab action=list 可查)")
	}
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("终端 %q 不存在(可用 term_tab 查看)", id)
	}
	return sess, nil
}

/* List 返回全部终端清单(创建顺序;WS/REST 全局视角)。 */
func (s *TerminalService) List() []TermInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TermInfo, 0, len(s.sessions))
	for i := 1; i <= s.seq; i++ {
		id := fmt.Sprintf("t%d", i)
		if sess, ok := s.sessions[id]; ok {
			sess.mu.Lock()
			out = append(out, *sess.info())
			sess.mu.Unlock()
		}
	}
	return out
}

/*
	ListTermsJSON 终端清单的 JSON 文本(term_tab 工具直接返回;

tools 侧不能引用 service 类型,走字符串解耦)。
*/
func (s *TerminalService) ListTermsJSON() string {
	b, err := json.Marshal(s.List())
	if err != nil {
		return "[]"
	}
	return string(b)
}

/*
	NewTerm 新建终端(name 简短标识,desc 用途描述)并立即返回终端状态。

返回 termResult JSON。
*/
func (s *TerminalService) NewTerm(name, desc string) (string, error) {
	info, err := s.Create(name, desc, "AI", 0, 0)
	if err != nil {
		return "", err
	}
	return termResultJSON(*info, "", "已创建(用户可在看板查看接管),后续用 term_action action=send 发送命令"), nil
}

/*
Launch 在 dir 目录新建终端并写入 command，不等输出返回——给"起了就一直
跑"的服务进程用（快应用后端）。起的动作只负责写进去，日志留在终端里供
用户与模型按需读（read 取末尾 N 行，启动日志会被后来的输出挤出窗口，
要看就趁早）。
*/
func (s *TerminalService) Launch(dir, name, desc, origin, command string) (*TermInfo, error) {
	info, err := s.create(dir, name, desc, origin, 0, 0)
	if err != nil {
		return nil, err
	}
	sess, ok := s.get(info.ID)
	if !ok {
		return nil, fmt.Errorf("终端 %q 创建后即失效", info.ID)
	}
	s.writeAI(sess, termPayload(command))
	return info, nil
}

/*
	termPayload 把待写入 PTY 的文本转成字节：先还原 \uXXXX 转义，纯控制

输入不补回车。模型按工具说明写 \u0003 表示 Ctrl+C——那是六个字符的文本
转义，不还原就会被 shell 当成命令执行（报"不是内部或外部命令"），中断
长任务失效。
*/
func termPayload(cmd string) []byte {
	payload := decodeEscapes(cmd)
	if !isRawControl(payload) {
		payload += "\r"
	}
	return []byte(payload)
}

/*
decodeEscapes 只还原 \u + 4 位十六进制这一种转义（大小写不限）。

其余反斜杠一律原样保留：Windows 路径与 UNC（\\server\share）不能被改写。
*/
func decodeEscapes(s string) string {
	if !strings.Contains(s, `\u`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+6 <= len(s) && s[i+1] == 'u' {
			if v, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
				b.WriteRune(rune(v))
				i += 6
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

/*
	isRawControl 判断是否为原始控制输入(如 ^C):含 C0 控制字符且无

可打印内容时原样写入、不补回车。
*/
func isRawControl(cmd string) bool {
	hasCtrl, hasPrint := false, false
	for _, c := range cmd {
		switch {
		case c == '\t':
		case c < 0x20 || c == 0x7f:
			hasCtrl = true
		default:
			hasPrint = true
		}
	}
	return hasCtrl && !hasPrint
}

/*
writeAI 是 AI 侧写入（锁外写 PTY，避免与 readPump 互等）。
*/
func (s *TerminalService) writeAI(sess *TermSession, b []byte) {
	sess.pty.Write(b) //nolint:errcheck
}

/*
SendTerm 写入命令,**等输出静默后把这次命令改动的那几行返回**。

静默判定从写入时刻起算(命令还没开始输出时不会被误判成"已结束"),静默
termSendQuiet 或 shell 退出即返回;长驻命令最多等 termSendWait,到点先返回
已产出的部分,其余留给 ReadTerm。取哪几行:写入前后比对可视屏,从第一处
不同的行截到屏底——输出是追加、进度条是原地改写,两种都能落进这一段。
*/
func (s *TerminalService) SendTerm(ctx context.Context, id, cmd string) (string, error) {
	sess, err := s.Get(id)
	if err != nil {
		return "", err
	}
	sess.mu.Lock()
	sess.lastOut = time.Now() // 静默计时从写入起算
	before := sess.screen.visible()
	sess.mu.Unlock()

	s.writeAI(sess, termPayload(cmd))
	exited, timedOut := waitQuiet(ctx, sess, termSendQuiet, termSendWait)

	sess.mu.Lock()
	text := changedSince(before, sess.screen.visible())
	idle := sess.screen.looksIdle()
	info := *sess.info()
	sess.mu.Unlock()
	return termResultJSON(info, text, sendNote(exited, timedOut, idle)), nil
}

/* changedSince 取"从第一处不同到屏底"的那几行(首尾空白行去掉)。 */
func changedSince(before, after []string) string {
	k := 0
	for k < len(before) && k < len(after) && before[k] == after[k] {
		k++
	}
	lines := append([]string(nil), after[k:]...)
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

/* waitQuiet 等输出静默 / shell 退出 / 到上限。 */
func waitQuiet(ctx context.Context, sess *TermSession, quiet, wait time.Duration) (exited, timedOut bool) {
	deadline := time.Now().Add(wait)
	for {
		sess.mu.Lock()
		ch := sess.condCh
		quieted := time.Since(sess.lastOut) >= quiet
		exited = sess.exited
		sess.mu.Unlock()
		if exited || quieted {
			return exited, false
		}
		if time.Now().After(deadline) {
			return exited, true
		}
		select {
		case <-ctx.Done():
			return exited, false
		case <-ch:
		case <-time.After(termPollStep):
		}
	}
}

/* sendNote 拼 send 的等待结果注记:正常收尾(静默且回到提示符)不啰嗦,其余提示。 */
func sendNote(exited, timedOut, idle bool) string {
	var parts []string
	if timedOut {
		parts = append(parts, "等待超时(命令可能仍在运行);后续输出用 action=read 取")
	} else if !idle {
		parts = append(parts, "输出已静默但没见到提示符,命令可能仍在运行;后续输出用 action=read 取")
	}
	if exited {
		parts = append(parts, "shell 已退出(连接保留可看残留输出)")
	}
	return strings.Join(parts, "; ")
}

/*
termResultJSON 组装 term_* 操作返回。text 已是屏幕还原好的多行文本,这里只做
长度兜底(超 8000 字符留末尾并标注)与空输出说明。
*/
func termResultJSON(info TermInfo, text, note string) string {
	text = strings.TrimRight(text, "\n")
	if r := []rune(text); len(r) > 8000 {
		text = "…(输出过长,已截断)\n" + string(r[len(r)-8000:])
	}
	if strings.TrimSpace(text) == "" {
		text = "(无输出)"
	}
	res := termResult{ID: info.ID, Name: info.Name, Desc: info.Desc,
		Exited: info.Exited, Output: text, Note: note}
	b, err := json.Marshal(res)
	if err != nil {
		return "{}"
	}
	return string(b)
}

/*
ReadTerm 读终端**末尾 N 行**(默认 100)——命令回显与输出都在里面,就是
肉眼看终端看到的那一块。没有增量游标:读多少次都是同一份快照,不消费、
不会读空,所以 send 与 read 并发或谁先谁后都不影响结果。
*/
func (s *TerminalService) ReadTerm(id string, lines int) (string, error) {
	sess, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if lines <= 0 {
		lines = termReadLine
	}
	sess.mu.Lock()
	text := sess.screen.text(lines)
	info := *sess.info()
	sess.mu.Unlock()
	return termResultJSON(info, text, ""), nil
}

/* CloseTerm 关闭终端(杀进程树防子进程残留);幂等,返回 closed JSON。 */
func (s *TerminalService) CloseTerm(id string) (string, error) {
	sess, err := s.Get(id)
	if err != nil {
		return "", err
	}
	s.remove(sess)
	b, merr := json.Marshal(struct {
		ID     string `json:"id"`
		Closed bool   `json:"closed"`
	}{ID: sess.ID, Closed: true})
	if merr != nil {
		return "{}", nil
	}
	return string(b), nil
}

/*
	remove 杀进程树并摘除会话,广播清单。

Unix 上 shell 开启作业控制后,前台/后台作业各自成组,单杀 shell 所在组收
不到它们——关闭 PTY 主端,内核会向前台作业组发 SIGHUP,读泵随之退出。
*/
func (s *TerminalService) remove(sess *TermSession) {
	killTree(sess.cmd)
	if sess.pty != nil {
		_ = sess.pty.Close()
	}
	s.mu.Lock()
	delete(s.sessions, sess.ID)
	s.mu.Unlock()
	s.broadcast(TermFrame{Type: "terminals", Sessions: s.List()})
}

/* UserInput 是用户手敲输入(WS 路径,按 id 精确查找)。 */
func (s *TerminalService) UserInput(id string, b []byte) error {
	sess, ok := s.get(id)
	if !ok {
		return fmt.Errorf("终端 %q 不存在", id)
	}
	sess.pty.Write(b) //nolint:errcheck
	return nil
}

/* Resize 调整终端尺寸;参数越界/会话不存在时静默丢弃(前端 fit 后会再上报)。 */
func (s *TerminalService) Resize(id string, cols, rows int) error {
	sess, ok := s.get(id)
	if !ok {
		return nil
	}
	if cols < 2 || rows < 2 || cols > 500 || rows > 300 {
		return nil
	}
	sess.pty.Resize(cols, rows) //nolint:errcheck
	sess.mu.Lock()
	sess.screen.resize(cols, rows) // 屏模型跟着改尺寸,否则 CUP 行号对不上
	sess.mu.Unlock()
	return nil
}

/* Snapshot 返回终端输出全量(WS hello 恢复屏幕用)。 */
func (s *TerminalService) Snapshot(id string) []byte {
	sess, ok := s.get(id)
	if !ok {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.ring.snapshot()
}

/* Subscribe 订阅广播帧(WS 连接用);返回退订函数。 */
func (s *TerminalService) Subscribe() (<-chan TermFrame, func()) {
	ch := make(chan TermFrame, termSubsBuf)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *TerminalService) broadcast(f TermFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- f:
		default: // 慢消费者丢帧,重连 hello 快照兜底
		}
	}
}

/* Close 关闭一个终端(REST 路径,用户可关任意终端);幂等。 */
func (s *TerminalService) Close(id string) {
	sess, ok := s.get(id)
	if !ok {
		return
	}
	s.remove(sess)
}

/* Shutdown 换代/停机收尾:关闭全部终端。 */
func (s *TerminalService) Shutdown(_ time.Duration) {
	s.mu.Lock()
	sessions := make([]*TermSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.sessions = map[string]*TermSession{}
	s.mu.Unlock()
	for _, sess := range sessions {
		killTree(sess.cmd)
		if sess.pty != nil {
			_ = sess.pty.Close() // 换代不停进程:必须显式收掉 PTY,前台作业组靠 SIGHUP 退出
		}
	}
}
