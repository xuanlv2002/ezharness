/*
task 是定时回调任务的领域模型：配置（标题/计划/目标）与运行态
（下次时间/最后状态/触发记录）同存 callbacks.json。任务与 app/skill/mcp
一样是可发现资源：system 清单、resource_change、agent 工具与页面都读同一份。
*/
package callback

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"slices"
	"strings"
	"time"

	"ezharness/core/internal/quickapp"
)

/* 任务类型。 */
const (
	KindSession = "session" // 向会话发消息：当前会话或每次新建
	KindApp     = "app"     // 启动快应用后端并调用其声明端点
	KindScript  = "script"  // 在工作目录执行 shell 命令
)

/* 会话任务的目标模式。 */
const (
	ModeSession = "session" // 绑定指定叶会话（缺省为调用方当前会话）
	ModeNew     = "new"     // 每次触发新建会话再发消息
)

/* 运行态状态。 */
const (
	StatusOK      = "ok"
	StatusError   = "error"
	StatusSkipped = "skipped" // 目标归档中，本次跳过
	StatusMissed  = "missed"  // 停机错过一次性时间
	StatusRunning = "running"
)

/* RunHistoryLimit 是每个任务保留的触发记录条数（新的在前）。 */
const RunHistoryLimit = 30

/*
Task 是一条定时回调。ID 创建后不变；Session.SessionID 存创建任务时的
叶 session ID——compact 换代或归档后该会话不再可用，任务失败，不继承
到新会话。
*/
type Task struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Kind        string         `json:"kind"`
	Enabled     bool           `json:"enabled"`
	Schedule    Schedule       `json:"schedule"`
	Session     *SessionTarget `json:"session,omitempty"`
	App         *AppTarget     `json:"app,omitempty"`
	Script      *ScriptTarget  `json:"script,omitempty"`

	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"` // 一次性任务终态时间：使命结束，不再进清单、不可编辑
	NextRunAt  int64  `json:"nextRunAt,omitempty"` // 0 = 无下次（停用/一次性已完成）
	LastRunAt  int64  `json:"lastRunAt,omitempty"`
	LastStatus string `json:"lastStatus,omitempty"`
	LastResult string `json:"lastResult,omitempty"` // 短结果或错误（截断）
	RunCount   int    `json:"runCount,omitempty"`
	Runs       []Run  `json:"runs,omitempty"` // 最近触发记录，新的在前
}

/*
Run 是一次触发的页面记录：触发时间、目标、投递内容与结果。
TargetID 供页面把会话/应用名还原成标题，不参与调度。
*/
type Run struct {
	At         int64  `json:"at"`
	Status     string `json:"status"`
	Manual     bool   `json:"manual,omitempty"`
	Target     string `json:"target"`
	TargetID   string `json:"targetId,omitempty"`
	Content    string `json:"content,omitempty"`
	Result     string `json:"result,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

/* SessionTarget 是会话任务的投递目标（叶会话 ID 由服务在创建时落值）。 */
type SessionTarget struct {
	SessionID string `json:"sessionId,omitempty" desc:"绑定的叶 session ID；归档/compact 换代后任务失败"`
	Mode      string `json:"mode" desc:"session（指定会话）| new（每次新建会话）"`
	Message   string `json:"message" desc:"到点后作为用户消息发送的正文"`
}

/* SessionInput 是创建/更新时的会话目标；mode=session 时 sessionId 指定叶会话，缺省绑定调用方当前会话。 */
type SessionInput struct {
	SessionID string `json:"sessionId,omitempty" desc:"mode=session 时要绑定的叶 session ID；缺省绑定调用方当前会话"`
	Mode      string `json:"mode" desc:"session（绑定指定会话）| new（每次新建会话）"`
	Message   string `json:"message" desc:"到点后作为用户消息发送的正文"`
}

/* AppTarget 是快应用任务的目标：触发时自动启动后端并调用声明端点。 */
type AppTarget struct {
	Name     string `json:"name" desc:"快应用目录名"`
	Callback string `json:"callback" desc:"app.quick callbacks[].name"`
}

/* ScriptTarget 是脚本任务的目标：在本机工作目录执行的 shell 命令。 */
type ScriptTarget struct {
	Command string `json:"command" desc:"工作目录里执行的 shell 命令，如 ./sync.sh、python tools/sync.py"`
}

/* CreateInput 是创建任务的配置面（运行态由服务生成）。 */
type CreateInput struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Kind        string        `json:"kind"`
	Enabled     *bool         `json:"enabled,omitempty"`
	Schedule    Schedule      `json:"schedule"`
	Session     *SessionInput `json:"session,omitempty"`
	App         *AppTarget    `json:"app,omitempty"`
	Script      *ScriptTarget `json:"script,omitempty"`
	BindSession string        `json:"-"` // 调用方所在会话（工具内部填写），mode=session 时优先绑定
}

/* UpdateInput 是更新任务的配置面：nil 字段保持原值。 */
type UpdateInput struct {
	Title       *string       `json:"title,omitempty"`
	Description *string       `json:"description,omitempty"`
	Kind        *string       `json:"kind,omitempty"`
	Enabled     *bool         `json:"enabled,omitempty"`
	Schedule    *Schedule     `json:"schedule,omitempty"`
	Session     *SessionInput `json:"session,omitempty"`
	App         *AppTarget    `json:"app,omitempty"`
	Script      *ScriptTarget `json:"script,omitempty"`
	BindSession string        `json:"-"` // 调用方所在会话；仅在目标还没有既有绑定时使用
}

/* Normalize 去除文本字段首尾空白并归一化枚举值。 */
func (t *Task) Normalize() {
	t.Title = strings.TrimSpace(t.Title)
	t.Description = strings.TrimSpace(t.Description)
	t.Kind = strings.ToLower(strings.TrimSpace(t.Kind))
	t.Schedule.Normalize()
	if t.Session != nil {
		t.Session.SessionID = strings.TrimSpace(t.Session.SessionID)
		t.Session.Mode = strings.ToLower(strings.TrimSpace(t.Session.Mode))
		if t.Session.Mode == "" {
			t.Session.Mode = ModeSession
		}
		if t.Session.Mode == ModeNew {
			t.Session.SessionID = ""
		}
		t.Session.Message = strings.TrimSpace(t.Session.Message)
	}
	if t.App != nil {
		t.App.Name = strings.TrimSpace(t.App.Name)
		t.App.Callback = strings.TrimSpace(t.App.Callback)
	}
	if t.Script != nil {
		t.Script.Command = strings.TrimSpace(t.Script.Command)
	}
}

/* Validate 校验任务的配置结构（目标是否存在由服务层再校验）。 */
func (t Task) Validate() error {
	if t.Title == "" {
		return fmt.Errorf("标题不能为空")
	}
	if len([]rune(t.Title)) > 120 {
		return fmt.Errorf("标题不能超过 120 字")
	}
	if len([]rune(t.Description)) > 2000 {
		return fmt.Errorf("描述不能超过 2000 字")
	}
	if err := t.Schedule.Validate(); err != nil {
		return err
	}
	switch t.Kind {
	case KindSession:
		if t.App != nil {
			return fmt.Errorf("会话任务不能带 app 目标")
		}
		if t.Script != nil {
			return fmt.Errorf("会话任务不能带 script 目标")
		}
		if t.Session == nil {
			return fmt.Errorf("会话任务需要 session 目标")
		}
		switch t.Session.Mode {
		case ModeSession:
			if t.Session.SessionID == "" {
				return fmt.Errorf("当前没有可绑定的会话，请先打开一个会话，或改用新建会话")
			}
		case ModeNew:
		default:
			return fmt.Errorf("session.mode 需为 session（当前会话）或 new（新建会话）")
		}
		if t.Session.Message == "" {
			return fmt.Errorf("session.message 不能为空")
		}
		if len([]rune(t.Session.Message)) > 8000 {
			return fmt.Errorf("消息不能超过 8000 字")
		}
	case KindApp:
		if t.Session != nil {
			return fmt.Errorf("快应用任务不能带 session 目标")
		}
		if t.Script != nil {
			return fmt.Errorf("快应用任务不能带 script 目标")
		}
		if t.App == nil {
			return fmt.Errorf("快应用任务需要 app 目标")
		}
		if !quickapp.ValidName(t.App.Name) {
			return fmt.Errorf("非法快应用名 %q", t.App.Name)
		}
		if t.App.Callback == "" {
			return fmt.Errorf("快应用任务需要 app.callback（app.quick 的 callbacks[].name）")
		}
	case KindScript:
		if t.Session != nil {
			return fmt.Errorf("脚本任务不能带 session 目标")
		}
		if t.App != nil {
			return fmt.Errorf("脚本任务不能带 app 目标")
		}
		if t.Script == nil {
			return fmt.Errorf("脚本任务需要 script 目标")
		}
		if t.Script.Command == "" {
			return fmt.Errorf("script.command 不能为空")
		}
		if len([]rune(t.Script.Command)) > 8000 {
			return fmt.Errorf("脚本命令不能超过 8000 字")
		}
	default:
		return fmt.Errorf("kind 需为 session、app 或 script")
	}
	return nil
}

/* Signature 是 resource_change 的变更基线（内容字段用哈希，避免分隔符歧义）。 */
func (t Task) Signature() string {
	enabled := "0"
	if t.Enabled {
		enabled = "1"
	}
	target := "none"
	switch t.Kind {
	case KindSession:
		if t.Session != nil {
			target = "session:" + t.Session.Mode + ":" + t.Session.SessionID
		}
	case KindApp:
		if t.App != nil {
			target = "app:" + t.App.Name + ":" + t.App.Callback
		}
	case KindScript:
		target = "script"
	}
	content := ""
	if t.Session != nil {
		content += t.Session.Message
	}
	if t.Script != nil {
		content += "\x00" + t.Script.Command
	}
	content += "\x00" + t.Description
	sum := crc32.ChecksumIEEE([]byte(content))
	title := strings.ReplaceAll(strings.ReplaceAll(t.Title, "|", " "), "\n", " ")
	return strings.Join([]string{
		t.ID, title, t.Kind, enabled, t.Schedule.Summary(), target, fmt.Sprintf("%08x", sum),
	}, "|")
}

/* TargetLabel 渲染目标的一句话描述（system 清单与工具结果共用）。 */
func (t Task) TargetLabel() string {
	switch t.Kind {
	case KindSession:
		if t.Session == nil {
			return "（无目标）"
		}
		if t.Session.Mode == ModeNew {
			return "新建会话"
		}
		if t.Session.SessionID == "" {
			return "（会话未绑定）"
		}
		return "会话 " + t.Session.SessionID
	case KindApp:
		if t.App == nil {
			return "（无目标）"
		}
		return "快应用 " + t.App.Name + " · " + t.App.Callback
	case KindScript:
		if t.Script == nil {
			return "（无目标）"
		}
		return "脚本 · " + oneLine(t.Script.Command, 120)
	}
	return t.Kind
}

/* ContentSummary 渲染投递内容的一句话描述（触发记录缺失时的兜底）。 */
func (t Task) ContentSummary() string {
	switch t.Kind {
	case KindSession:
		if t.Session != nil {
			return t.Session.Message
		}
	case KindApp:
		if t.App != nil {
			return "快应用 " + t.App.Name + " · " + t.App.Callback
		}
	case KindScript:
		if t.Script != nil {
			return oneLine(t.Script.Command, 300)
		}
	}
	return ""
}

/* oneLine 压平换行并按 rune 截断（摘要与清单单行展示）。 */
func oneLine(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "\r", ""), "\n", " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

/* Summary 渲染任务的单行摘要（system 清单、工具结果与页面共用）。 */
func (t Task) Summary() string {
	state := "启用"
	if !t.Enabled {
		state = "停用"
	}
	s := t.Schedule.Summary() + " · " + t.TargetLabel() + " · " + state
	if t.LastStatus != "" {
		s += " · 上次" + StatusLabel(t.LastStatus)
	}
	if t.NextRunAt > 0 {
		s += " · 下次 " + time.UnixMilli(t.NextRunAt).Format("2006-01-02 15:04")
	}
	return s
}

/* StatusLabel 把运行态状态渲染成中文。 */
func StatusLabel(status string) string {
	switch status {
	case StatusOK:
		return "成功"
	case StatusError:
		return "失败"
	case StatusSkipped:
		return "跳过"
	case StatusMissed:
		return "错过"
	case StatusRunning:
		return "执行中"
	}
	return status
}

/* PushRun 把一条记录放到最前并裁到 RunHistoryLimit。 */
func PushRun(runs []Run, r Run) []Run {
	out := make([]Run, 0, len(runs)+1)
	out = append(out, r)
	out = append(out, runs...)
	if len(out) > RunHistoryLimit {
		out = out[:RunHistoryLimit]
	}
	return out
}

/* Finished 报告一次性任务是否已终态（触发过、错过或过期）：不再进清单、不可编辑，仍可手动触发与删除。 */
func (t Task) Finished() bool { return t.FinishedAt > 0 }

/*
SortTasks 就地排序任务清单：启用中的置顶，其余（停用/已完成）殿后，
组内按创建时间新→旧。system 清单、工具 list 与页面共用同一顺序。
*/
func SortTasks(tasks []Task) {
	slices.SortStableFunc(tasks, func(a, b Task) int {
		if a.Enabled != b.Enabled {
			if b.Enabled {
				return 1
			}
			return -1
		}
		if c := cmp.Compare(b.CreatedAt, a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
}

/* NewID 生成任务 ID（时间 + 短随机，便于人读与排障）。 */
func NewID(now time.Time) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "cb-" + now.Format("20060102-150405") + "-" + hex.EncodeToString(b)
}
