package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// 本地回显的用户消息与 loop_start 回放同一条时只显示一次；回放重建
// （replay.sync）后清掉标记，让回放自己补上用户行。
func TestApplyFrameUserEcho(t *testing.T) {
	m := &tuiModel{}
	m.lines = []line{{kind: "user", text: "旧"}}
	m.histCount = len(m.lines)

	m.echoUser = "你好"
	m.applyFrame(frame{Type: "loop_start", Data: json.RawMessage(`"你好"`)})
	if len(m.lines) != 1 {
		t.Fatalf("重复回显：%+v", m.lines)
	}
	if m.echoUser != "" {
		t.Fatal("echo 标记未清")
	}

	// 带引用/附件的 loop_start 载荷是对象
	m.applyFrame(frame{Type: "loop_start", Data: json.RawMessage(`{"text":"带附件"}`)})
	if len(m.lines) != 2 || m.lines[1].text != "带附件" {
		t.Fatalf("对象载荷未解析：%+v", m.lines)
	}

	// 运行中切回分支：截回历史行，回放会重发 loop_start
	m.lines = append(m.lines, line{kind: "user", text: "回显"})
	m.echoUser = "回显"
	m.applyFrame(frame{Type: "replay.sync", Data: json.RawMessage(`{"turnActive":true}`)})
	if len(m.lines) != m.histCount || m.echoUser != "" {
		t.Fatalf("replay.sync 未截断/清标记：lines=%d hist=%d echo=%q", len(m.lines), m.histCount, m.echoUser)
	}
}

/* SSE 错误经帧通道回主循环（不直接改模型字段），并解除"运行中"阻塞。 */
func TestApplyFrameSseError(t *testing.T) {
	m := &tuiModel{streaming: true}
	m.applyFrame(frame{Type: "sse.error", Data: json.RawMessage(`"SSE 断开: boom"`)})
	if m.err != "SSE 断开: boom" || m.streaming {
		t.Fatalf("err=%q streaming=%v", m.err, m.streaming)
	}
}

/* 工具折叠：连续调用合成一行；正文/思考照常渲染（带工具调用的中间发言也显示）。 */
func TestApplyFrameFoldsProcess(t *testing.T) {
	m := &tuiModel{}
	m.applyFrame(frame{Type: "tool_start", Data: json.RawMessage(`{"id":"c1","name":"terminal"}`)})
	if len(m.lines) != 1 || m.lines[0].kind != "tools" || m.lines[0].text != "1 个调用 · terminal" {
		t.Fatalf("工具组未建：%+v", m.lines)
	}
	m.applyFrame(frame{Type: "tool_start", Data: json.RawMessage(`{"id":"c2","name":"terminal"}`)})
	if len(m.lines) != 1 || m.lines[0].text != "2 个调用 · terminal ×2" {
		t.Fatalf("连续工具未折叠：%+v", m.lines)
	}
	m.applyFrame(frame{Type: "tool_end", Data: json.RawMessage(`{"callId":"c1","name":"terminal"}`)})
	if len(m.running) != 1 || m.running[0].id != "c2" {
		t.Fatalf("执行中工具未出清：%+v", m.running)
	}

	m.applyFrame(frame{Type: "model_end", Data: json.RawMessage(`{"content":"中间话","reasoning":"想一想","toolCalls":[{"id":"c3","name":"read_file"}]}`)})
	if len(m.lines) != 3 || m.lines[1].kind != "reasoning" || m.lines[2].kind != "assistant" || m.lines[2].text != "中间话" {
		t.Fatalf("思考/正文未渲染：%+v", m.lines)
	}
	m.applyFrame(frame{Type: "tool_start", Data: json.RawMessage(`{"id":"c3","name":"read_file"}`)})
	if len(m.lines) != 4 || m.lines[3].kind != "tools" || m.lines[3].text != "1 个调用 · read_file" {
		t.Fatalf("正文后的工具应另起一组：%+v", m.lines)
	}
}

/* 流式帧累积成当前正文/思考，model_end 落定后再清空。 */
func TestApplyFrameStreaming(t *testing.T) {
	m := &tuiModel{}
	m.streaming = true
	m.applyFrame(frame{Type: "reasoning_chunk", Data: json.RawMessage(`"想一"`)})
	m.applyFrame(frame{Type: "reasoning_chunk", Data: json.RawMessage(`"想"`)})
	if got := m.statusHint(false); got != "思考中…" {
		t.Fatalf("思考中提示不对：%q", got)
	}
	m.applyFrame(frame{Type: "model_chunk", Data: json.RawMessage(`"你"`)})
	m.applyFrame(frame{Type: "model_chunk", Data: json.RawMessage(`"好"`)})
	if m.liveThink != "想一想" || m.liveText != "你好" {
		t.Fatalf("流式未累积：think=%q text=%q", m.liveThink, m.liveText)
	}
	if rows := strings.Join(m.chatRows(80), "\n"); !strings.Contains(rows, "你好▍") {
		t.Fatalf("流式正文未渲染：\n%s", rows)
	}
	m.applyFrame(frame{Type: "model_end", Data: json.RawMessage(`{"content":"你好","reasoning":"想一想"}`)})
	if m.liveThink != "" || m.liveText != "" {
		t.Fatalf("model_end 未清流式态：think=%q text=%q", m.liveThink, m.liveText)
	}
	if len(m.lines) != 2 || m.lines[0].kind != "reasoning" || m.lines[1].kind != "assistant" {
		t.Fatalf("落定顺序不对：%+v", m.lines)
	}
	m.applyFrame(frame{Type: "tool_start", Data: json.RawMessage(`{"id":"c1","name":"terminal"}`)})
	if got := m.statusHint(false); got != "执行中 terminal" {
		t.Fatalf("执行中提示不对：%q", got)
	}
}

/* 输入 / 出命令提示；状态栏显示模型/思考/审批/上下文/目录。 */
func TestSlashHintsAndStatusRow(t *testing.T) {
	m := &tuiModel{cwd: "/tmp/ez-work", height: 24, input: textarea.New()}
	m.input.Prompt = ""
	m.input.Focus()
	m.applyWidth(120)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if len(m.suggestionRows()) != len(slashCommands) {
		t.Fatalf("斜杠未出命令提示：%+v", m.suggestionRows())
	}
	view := m.View()
	if !strings.Contains(view, "/session") || !strings.Contains(view, "切换分支") {
		t.Fatalf("命令提示未渲染：\n%s", view)
	}

	m.input.SetValue("/n")
	if rows := m.suggestionRows(); len(rows) != 1 || !strings.Contains(rows[0], "/new") {
		t.Fatalf("前缀过滤不对：%+v", rows)
	}

	m.status = &statusView{Model: "glm-5.4", ReasoningEffort: "high", ApproveMode: "off",
		ContextTokens: 12349, ContextWindow: 102400}
	row := m.statusRow()
	for _, want := range []string{"glm-5.4", "高", "无审批", "12.3k/102.4k", "/tmp/ez-work"} {
		if !strings.Contains(row, want) {
			t.Fatalf("状态栏缺 %q：%q", want, row)
		}
	}
	if strings.Contains(strings.Join(m.inputRows(), "\n"), "/tmp/ez-work") {
		t.Fatalf("目录不该再贴在输入行：%q", m.inputRows())
	}
}

/* 审批不进时间线；选择栏点名工具/参数，底栏给操作键。 */
func TestPendingApprovalHint(t *testing.T) {
	m := &tuiModel{width: 100}
	m.applyFrame(frame{Type: "approve.request", Data: json.RawMessage(`{"id":"a1","name":"write_file","args":{"path":"/tmp/a.ts"}}`)})
	m.applyFrame(frame{Type: "approve.request", Data: json.RawMessage(`{"id":"a2","name":"terminal","args":{"cmd":"npm run build"}}`)})
	if len(m.lines) != 0 {
		t.Fatalf("审批不该进时间线：%+v", m.lines)
	}
	panel := strings.Join(m.decisionPanel(10), "\n")
	for _, want := range []string{"1/2", "write_file", "/tmp/a.ts", "允许", "拒绝"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("选择栏缺 %q：%q", want, panel)
		}
	}
	if hint := m.pendingHint(); !strings.Contains(hint, "↑↓ 选择") {
		t.Fatalf("底栏键位不对：%q", hint)
	}
}

/* 斜杠命令按精确/唯一前缀解析；含糊时给候选。 */
func TestResolveCommand(t *testing.T) {
	if name, _ := resolveCommand("pe"); name != "permissions" {
		t.Fatalf("唯一前缀未解析：%q", name)
	}
	if name, hits := resolveCommand("s"); name != "" || len(hits) != 2 {
		t.Fatalf("含糊前缀：name=%q hits=%v", name, hits)
	}
	if name, _ := resolveCommand(""); name != "help" {
		t.Fatalf("空命令应进帮助：%q", name)
	}
}

/* 决策帧：提问/审批/计划都登记为待办，提问/计划进时间线；重放重复帧按 id 去重。 */
func TestApplyFrameDecisions(t *testing.T) {
	m := &tuiModel{}
	m.applyFrame(frame{Type: "askuser.request", Data: json.RawMessage(`{"id":"c1","name":"ask_user","args":"{\"question\":\"选哪个\",\"options\":[\"A\",\"B\"]}"}`)})
	if len(m.pending) != 1 || m.pending[0].kind != "ask" || m.pending[0].question != "选哪个" || len(m.pending[0].options) != 2 {
		t.Fatalf("ask 解析失败：%+v", m.pending)
	}
	m.applyFrame(frame{Type: "askuser.request", Data: json.RawMessage(`{"id":"c1","name":"ask_user","args":"{}"}`)})
	if len(m.pending) != 1 {
		t.Fatalf("重复决策帧未去重：%+v", m.pending)
	}
	m.applyFrame(frame{Type: "approve.request", Data: json.RawMessage(`{"id":"c2","name":"terminal","args":{"cmd":"ls"}}`)})
	if len(m.pending) != 2 || m.pending[1].kind != "approve" || m.pending[1].name != "terminal" {
		t.Fatalf("approve 解析失败：%+v", m.pending)
	}
	m.applyFrame(frame{Type: "plan.request", Data: json.RawMessage(`{"id":"c3","name":"plan","args":{"goal":"做X","progress":[{"title":"a"},{"title":"b"}]}}`)})
	if len(m.pending) != 3 || m.pending[2].kind != "plan" || m.pending[2].goal != "做X" || m.pending[2].steps != 2 {
		t.Fatalf("plan 解析失败：%+v", m.pending)
	}
}
