/*
tui 终端界面：只显示用户消息、每轮最终回复和需要人回答的请求（审批 /
提问 / 计划 / MCP 授权）；工具调用与思考过程折叠成底栏计数，不进时间线。
斜杠命令管分支、归档、审批档与模型；启动目录随消息带给 core，在
agent_status 里作软提示。
*/
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

/* line 是会话区一条逻辑消息（只放正式内容与需要人看的决策）。 */
type line struct {
	kind string // user | assistant | ask | plan | approve | mcp | info | err
	text string
}

/* decision 是一条等待用户回答的请求。 */
type decision struct {
	id       string
	kind     string // approve | ask | plan | mcp
	name     string // 工具名 / MCP 服务名
	question string
	options  []string
	goal     string
	steps    int
	metaURL  string
	busy     bool
}

/* picker 是覆盖在会话区的选择列表（分支 / 模型 / 审批档）。 */
type picker struct {
	kind  string
	title string
	items []pickItem
	sel   int
}

type pickItem struct {
	id    string
	label string
}

type tuiModel struct {
	cl        *client
	addr      string
	version   string
	cwd       string
	rootID    string
	branches  []branch
	lines     []line
	histCount int // 历史消息行数：replay.sync 重建时截回这里
	input     textinput.Model
	streaming bool
	tools     int
	echoUser  string
	frames    chan frame
	stopSse   func()
	width     int
	height    int
	err       string
	pending   []decision
	picker    *picker
	status    *statusView
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	userStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	aiStyle    = lipgloss.NewStyle()
	askStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func cmdTUI(server string) {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		fatalf("tui 需要在终端里运行")
	}
	base := resolveBase(server)
	cl := ensureAuth(base)
	b, err := cl.bootstrap()
	if err != nil {
		fatalf("获取会话失败: %v", err)
	}
	if len(b.Branches) == 0 {
		// 空话题（全新安装）：自助开一条分支，不必先去 web/桌面端
		if _, err := cl.newBranch(); err != nil {
			fatalf("新建分支失败: %v", err)
		}
		if b, err = cl.bootstrap(); err != nil || len(b.Branches) == 0 {
			fatalf("分支清单为空（bootstrap: %v）", err)
		}
	}

	cwd, _ := os.Getwd()
	in := textinput.New()
	in.Placeholder = "输入消息，/ 开头是命令"
	in.Focus()
	in.CharLimit = 8000

	in.Width = 76
	m := &tuiModel{
		cl: cl, addr: displayAddr(base), version: version, cwd: cwd,
		rootID: b.SessionID, branches: b.Branches, input: in,
		frames: make(chan frame, 256), status: b.Status,
		width: 80, height: 24, // 无窗口尺寸事件时的兜底（管道/pty 尺寸为 0）
	}
	m.loadHistory()
	m.lines = append(m.lines,
		line{kind: "info", text: "工作中 " + cwd},
		line{kind: "info", text: "/help 查看命令"},
	)

	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if mm, ok := final.(*tuiModel); ok && mm.stopSse != nil {
		mm.stopSse()
	}
	if err != nil {
		fatalf("tui 退出: %v", err)
	}
}

func (m *tuiModel) Init() tea.Cmd {
	m.subscribe()
	return tea.Batch(textinput.Blink, m.waitForFrame())
}

/* subscribe（重）订阅当前分支的事件流。 */
func (m *tuiModel) subscribe() {
	if m.stopSse != nil {
		m.stopSse()
	}
	m.stopSse = m.cl.sseStream(m.rootID, func(f frame) {
		select {
		case m.frames <- f:
		default: // 消费端卡住（渲染）时丢帧保活：turn_end 会兜底复位
		}
	}, func(err error) {
		data, _ := json.Marshal("SSE 断开: " + err.Error())
		select {
		case m.frames <- frame{Type: "sse.error", Data: data}:
		default:
		}
	})
}

/* isSystemNote 系统注入的 user 消息（快照/引用/收尾/护栏等）：不进行情时间线。 */
func isSystemNote(s string) bool {
	for _, tag := range []string{
		"<agent_status>", "<resource_change>", "<reference_file>", "<image_loaded>",
		"<end_reason>", "<context_trim", "<loop_guard>", "<scheduled_callback>",
	} {
		if strings.Contains(s, tag) {
			return true
		}
	}
	return false
}

/*
	loadHistory 拉当前分支的持久消息：只留用户输入与最终回复（带工具调用的

中间发言、思考、工具结果都折叠掉）。
*/
func (m *tuiModel) loadHistory() {
	m.lines = m.lines[:0]
	h, err := m.cl.history(m.rootID)
	if err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "历史加载失败: " + err.Error()})
		m.histCount = len(m.lines)
		return
	}
	for _, msg := range h.Messages {
		switch msg.Role {
		case "user":
			if msg.Content != "" && !isSystemNote(msg.Content) {
				m.lines = append(m.lines, line{kind: "user", text: msg.Content})
			}
		case "assistant":
			if msg.Content != "" && len(msg.ToolCalls) == 0 {
				m.lines = append(m.lines, line{kind: "assistant", text: msg.Content})
			}
		}
	}
	m.histCount = len(m.lines)
}

/* reload 分支/话题变化后重建：清单、历史、事件流。 */
func (m *tuiModel) reload() {
	b, err := m.cl.bootstrap()
	if err != nil {
		m.err = "刷新会话失败: " + err.Error()
		return
	}
	m.branches = b.Branches
	m.rootID = b.SessionID
	m.status = b.Status
	m.pending = nil
	m.loadHistory()
	m.subscribe()
	m.err = ""
}

/* applyFrame 把 SSE 帧并入界面：只有正式输出和决策进时间线，其余折叠。 */
func (m *tuiModel) applyFrame(f frame) {
	if f.ForkID != "" {
		return // 后台分支的帧不进主时间线
	}
	switch f.Type {
	case "sse.error":
		var s string
		if json.Unmarshal(f.Data, &s) == nil {
			m.err = s
		}
		m.streaming = false
	case "replay.sync":
		var d struct {
			TurnActive bool `json:"turnActive"`
		}
		_ = json.Unmarshal(f.Data, &d)
		if d.TurnActive {
			m.lines = m.lines[:m.histCount]
			m.streaming = true
			m.tools = 0
			m.echoUser = ""
		}
	case "loop_start":
		var text string
		if json.Unmarshal(f.Data, &text) != nil {
			var d struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(f.Data, &d) == nil {
				text = d.Text
			}
		}
		if text != "" {
			if text == m.echoUser {
				m.echoUser = "" // 本地已回显
			} else {
				m.lines = append(m.lines, line{kind: "user", text: text})
			}
		}
		m.streaming = true
		m.tools = 0
	case "tool_start":
		m.tools++
	case "model_end":
		var d modelEndData
		if json.Unmarshal(f.Data, &d) == nil && len(d.ToolCalls) == 0 && strings.TrimSpace(d.Content) != "" {
			m.lines = append(m.lines, line{kind: "assistant", text: d.Content})
		}
	case "turn_end":
		var d turnEndData
		_ = json.Unmarshal(f.Data, &d)
		m.streaming = false
		m.tools = 0
		m.echoUser = ""
		if d.Err != "" {
			m.lines = append(m.lines, line{kind: "err", text: d.Err})
		}
	case "approve.request", "askuser.request", "plan.request":
		m.addDecision(f.Type, f.Data)
	case "mcp.auth.request":
		m.addMcpAuth(f.Data)
	}
}

/* decisionData 是审批/提问/计划请求的载荷。 */
type decisionData struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

/* mcpAuthData 是 MCP 授权请求的载荷。 */
type mcpAuthData struct {
	ID          string `json:"id"`
	Server      string `json:"server"`
	MetadataURL string `json:"metadata_url,omitempty"`
}

/* addDecision 登记一条审批/提问/计划请求并写进时间线。 */
func (m *tuiModel) addDecision(typ string, data json.RawMessage) {
	var d decisionData
	if json.Unmarshal(data, &d) != nil || d.ID == "" || m.hasPending(d.ID) {
		return
	}
	dec := decision{id: d.ID, name: d.Name}
	switch typ {
	case "approve.request":
		dec.kind = "approve"
		m.lines = append(m.lines, line{kind: "approve", text: "需要批准：" + d.Name})
	case "askuser.request":
		var a struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		decodeArgs(d.Args, &a)
		dec.kind, dec.question, dec.options = "ask", a.Question, a.Options
		m.lines = append(m.lines, line{kind: "ask", text: "提问：" + a.Question})
		if len(a.Options) > 0 {
			m.lines = append(m.lines, line{kind: "info", text: "选项：" + strings.Join(a.Options, " / ")})
		}
	case "plan.request":
		var p struct {
			Goal     string `json:"goal"`
			Progress []struct {
				Title string `json:"title"`
			} `json:"progress"`
		}
		decodeArgs(d.Args, &p)
		dec.kind, dec.goal, dec.steps = "plan", p.Goal, len(p.Progress)
		m.lines = append(m.lines, line{kind: "plan", text: "计划：" + p.Goal})
	}
	m.pending = append(m.pending, dec)
}

/* addMcpAuth 登记一条 MCP OAuth 授权请求。 */
func (m *tuiModel) addMcpAuth(data json.RawMessage) {
	var d mcpAuthData
	if json.Unmarshal(data, &d) != nil || d.ID == "" || d.Server == "" || m.hasPending(d.ID) {
		return
	}
	m.pending = append(m.pending, decision{id: d.ID, kind: "mcp", name: d.Server, metaURL: d.MetadataURL})
	m.lines = append(m.lines, line{kind: "mcp", text: "MCP 服务 " + d.Server + " 需要 OAuth 授权"})
}

/* decodeArgs 解工具参数（兼容 JSON 对象与「字符串里再放 JSON」两种形态）。 */
func decodeArgs(raw json.RawMessage, out any) bool {
	if len(raw) == 0 {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	return json.Unmarshal(raw, out) == nil
}

func (m *tuiModel) hasPending(id string) bool {
	for _, d := range m.pending {
		if d.id == id {
			return true
		}
	}
	return false
}

func (m *tuiModel) waitForFrame() tea.Cmd {
	return func() tea.Msg {
		f, ok := <-m.frames
		if !ok {
			return nil
		}
		return f
	}
}

/* ---- 决策回传 ---- */

func (m *tuiModel) dropPending() decision {
	d := m.pending[0]
	m.pending = m.pending[1:]
	return d
}

func (m *tuiModel) decideApprove(ok bool) {
	d := m.dropPending()
	if err := m.cl.decideApprove(m.rootID, d.id, ok, ""); err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "审批回传失败: " + err.Error()})
		return
	}
	if ok {
		m.lines = append(m.lines, line{kind: "info", text: "已允许"})
	} else {
		m.lines = append(m.lines, line{kind: "info", text: "已拒绝"})
	}
}

func (m *tuiModel) resolvePlan(action, feedback string) {
	d := m.dropPending()
	if err := m.cl.decidePlan(m.rootID, d.id, action, feedback); err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "计划回传失败: " + err.Error()})
		return
	}
	if action == "confirm" {
		m.lines = append(m.lines, line{kind: "info", text: "计划已确认"})
	} else {
		m.lines = append(m.lines, line{kind: "info", text: "计划反馈已提交"})
	}
}

/* compactMsg 是归档（总结开新会话）的结果。 */
type compactMsg struct{ err error }

func compactCmd(cl *client, rootID string) tea.Cmd {
	return func() tea.Msg { return compactMsg{err: cl.compact(rootID)} }
}

/* mcpAuthMsg 是授权流程（打开浏览器 → 轮询）的结果。 */
type mcpAuthMsg struct {
	id  string
	ok  bool
	err string
}

/* startMcpAuth 先打开授权页，再轮询到授权完成（5 分钟上限）。 */
func startMcpAuth(cl *client, d decision) tea.Cmd {
	return func() tea.Msg {
		url, err := cl.oauthStart(d.name, d.metaURL)
		if err != nil {
			return mcpAuthMsg{id: d.id, err: err.Error()}
		}
		openURL(url)
		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)
			ok, err := cl.oauthStatus(d.name)
			if err == nil && ok {
				return mcpAuthMsg{id: d.id, ok: true}
			}
		}
		return mcpAuthMsg{id: d.id, err: "授权超时"}
	}
}

/* finishMcpAuth 结算一条授权请求：回传决策并写结果。 */
func (m *tuiModel) finishMcpAuth(msg mcpAuthMsg) {
	idx := -1
	for i, d := range m.pending {
		if d.id == msg.id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	d := m.pending[idx]
	m.pending = append(m.pending[:idx], m.pending[idx+1:]...)
	if msg.err == "" && msg.ok {
		_ = m.cl.decideMcpAuth(m.rootID, d.id, true, "")
		m.lines = append(m.lines, line{kind: "info", text: "MCP 已授权：" + d.name})
		return
	}
	reason := msg.err
	if reason == "" {
		reason = "授权失败"
	}
	_ = m.cl.decideMcpAuth(m.rootID, d.id, false, reason)
	m.lines = append(m.lines, line{kind: "err", text: "MCP 授权失败：" + reason})
}

/* ---- 斜杠命令 ---- */

func (m *tuiModel) runCommand(cmd string) tea.Cmd {
	if len(m.pending) > 0 && (cmd == "session" || cmd == "new" || cmd == "compact") {
		m.lines = append(m.lines, line{kind: "info", text: "先处理当前请求（审批/回答）再切分支"})
		return nil
	}
	switch cmd {
	case "help":
		m.lines = append(m.lines,
			line{kind: "info", text: "命令：/session 分支 · /new 新分支 · /compact 归档 · /permissions 审批档 · /models 模型 · /status 状态"},
			line{kind: "info", text: "审批/提问/计划/授权出现时按提示直接回答（y 允许 · n 拒绝）"},
		)
	case "session":
		m.openSessionPicker()
	case "new":
		if _, err := m.cl.newBranch(); err != nil {
			m.lines = append(m.lines, line{kind: "err", text: "新建分支失败: " + err.Error()})
			return nil
		}
		m.reload()
		m.lines = append(m.lines, line{kind: "info", text: "已开新分支"})
	case "compact":
		m.lines = append(m.lines, line{kind: "info", text: "归档中…"})
		return compactCmd(m.cl, m.rootID)
	case "permissions":
		m.openPermissionsPicker()
	case "models":
		m.openModelsPicker()
	case "status":
		m.showStatus()
	default:
		m.lines = append(m.lines, line{kind: "err", text: "未知命令 /" + cmd + "，/help 查看"})
	}
	return nil
}

func (m *tuiModel) openSessionPicker() {
	p := &picker{kind: "session", title: "/session · 切换分支"}
	for _, br := range m.branches {
		title := br.Title
		if title == "" {
			title = "(未命名)"
		}
		label := fmt.Sprintf("%s · %d 条", title, br.Msgs)
		if br.ID == m.rootID {
			label += "（当前）"
		}
		p.items = append(p.items, pickItem{id: br.ID, label: label})
		if br.ID == m.rootID {
			p.sel = len(p.items) - 1
		}
	}
	m.picker = p
}

func (m *tuiModel) openPermissionsPicker() {
	cur := ""
	if st, err := m.cl.status(); err == nil {
		m.status = st
		cur = st.ApproveMode
	}
	modes := []pickItem{
		{id: "", label: "跟随安全配置"},
		{id: "off", label: "无审批（全部放行）"},
		{id: "strict", label: "全审批（逐个确认）"},
	}
	p := &picker{kind: "permissions", title: "/permissions · 审批档"}
	for i, it := range modes {
		if it.id == cur {
			p.sel = i
		}
		p.items = append(p.items, it)
	}
	m.picker = p
}

func (m *tuiModel) openModelsPicker() {
	list, err := m.cl.models()
	if err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "模型清单加载失败: " + err.Error()})
		return
	}
	cur := ""
	if st, err := m.cl.status(); err == nil {
		m.status = st
		cur = st.Model
	}
	p := &picker{kind: "models", title: "/models · 切换模型"}
	for _, e := range list {
		if e.Disabled {
			continue
		}
		label := e.Name
		if e.Vision {
			label += "（视觉）"
		}
		p.items = append(p.items, pickItem{id: e.Name, label: label})
		if e.Name == cur {
			p.sel = len(p.items) - 1
		}
	}
	if len(p.items) == 0 {
		m.lines = append(m.lines, line{kind: "err", text: "没有可用模型（先在设置页启用）"})
		return
	}
	m.picker = p
}

/* applyPick 执行选择列表的当前项。 */
func (m *tuiModel) applyPick() {
	p := m.picker
	if p == nil || len(p.items) == 0 {
		return
	}
	it := p.items[p.sel]
	m.picker = nil
	switch p.kind {
	case "session":
		m.pending = nil
		if err := m.cl.activate(it.id); err != nil {
			m.lines = append(m.lines, line{kind: "err", text: "切换分支失败: " + err.Error()})
			return
		}
		m.rootID = it.id
		m.loadHistory()
		m.subscribe()
		m.lines = append(m.lines, line{kind: "info", text: "已切换分支：" + it.label})
	case "permissions":
		if _, err := m.cl.setPrefs(m.rootID, map[string]any{"approveMode": it.id}); err != nil {
			m.lines = append(m.lines, line{kind: "err", text: "审批档保存失败: " + err.Error()})
			return
		}
		if m.status != nil {
			m.status.ApproveMode = it.id
		}
		m.lines = append(m.lines, line{kind: "info", text: "审批档：" + it.label})
	case "models":
		if _, err := m.cl.setPrefs(m.rootID, map[string]any{"model": it.id}); err != nil {
			m.lines = append(m.lines, line{kind: "err", text: "模型切换失败: " + err.Error()})
			return
		}
		if m.status != nil {
			m.status.Model = it.id
		}
		m.lines = append(m.lines, line{kind: "info", text: "模型已切到：" + it.id})
	}
}

/* showStatus 把会话状态写进时间线。 */
func (m *tuiModel) showStatus() {
	st, err := m.cl.status()
	if err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "状态加载失败: " + err.Error()})
		return
	}
	m.status = st
	m.lines = append(m.lines,
		line{kind: "info", text: "地址 " + m.addr + " · 目录 " + m.cwd},
		line{kind: "info", text: "分支 " + m.branchTitle() + " · " + st.RootID},
		line{kind: "info", text: "模型 " + st.Model + " · 审批 " + approveLabel(st.ApproveMode)},
		line{kind: "info", text: fmt.Sprintf("消息 %d 条 · 水位 %d/%d tokens", st.SessionMsgs, st.ContextTokens, st.ContextWindow)},
	)
}

func approveLabel(mode string) string {
	switch mode {
	case "off":
		return "无审批"
	case "strict":
		return "全审批"
	}
	return "跟随安全配置"
}

func (m *tuiModel) branchTitle() string {
	for _, br := range m.branches {
		if br.ID == m.rootID {
			if br.Title != "" {
				return br.Title
			}
			return "(未命名)"
		}
	}
	return "?"
}

/* ---- 输入与按键 ---- */

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 && msg.Height > 0 { // 尺寸为 0 的伪终端不覆盖兜底尺寸
			m.width, m.height = msg.Width, msg.Height
			m.input.Width = msg.Width - 4
		}
		return m, nil
	case compactMsg:
		if msg.err != nil {
			m.lines = append(m.lines, line{kind: "err", text: "归档失败: " + msg.err.Error()})
		} else {
			m.reload()
			m.lines = append(m.lines, line{kind: "info", text: "已归档，新话题已就绪"})
		}
		return m, nil
	case mcpAuthMsg:
		m.finishMcpAuth(msg)
		return m, nil
	case tea.KeyMsg:
		if m.picker != nil {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.picker = nil
			case tea.KeyUp:
				m.picker.sel = (m.picker.sel - 1 + len(m.picker.items)) % len(m.picker.items)
			case tea.KeyDown:
				m.picker.sel = (m.picker.sel + 1) % len(m.picker.items)
			case tea.KeyEnter:
				m.applyPick()
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			return m, m.submit()
		}
		if len(m.pending) > 0 && msg.Type == tea.KeyRunes {
			d := m.pending[0]
			switch string(msg.Runes) {
			case "y":
				switch d.kind {
				case "approve":
					m.decideApprove(true)
				case "mcp":
					if !d.busy {
						m.pending[0].busy = true
						return m, startMcpAuth(m.cl, d)
					}
				}
			case "n":
				switch d.kind {
				case "approve":
					m.decideApprove(false)
				case "mcp":
					if !d.busy {
						m.dropPending()
						_ = m.cl.oauthCancel(d.name)
						_ = m.cl.decideMcpAuth(m.rootID, d.id, false, "已取消")
						m.lines = append(m.lines, line{kind: "info", text: "已取消 MCP 授权"})
					}
				}
			}
		}
	case frame:
		m.applyFrame(msg)
		return m, m.waitForFrame()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

/* submit 处理回车：斜杠命令 / 决策回答 / 发消息。 */
func (m *tuiModel) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		if len(m.pending) > 0 && m.pending[0].kind == "plan" {
			m.resolvePlan("confirm", "")
			m.input.SetValue("")
		}
		return nil
	}
	if strings.HasPrefix(text, "/") {
		m.input.SetValue("")
		return m.runCommand(strings.TrimSpace(strings.TrimPrefix(text, "/")))
	}
	if len(m.pending) > 0 {
		d := m.pending[0]
		switch d.kind {
		case "ask":
			answer := text
			if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(d.options) {
				answer = d.options[n-1]
			}
			m.dropPending()
			if err := m.cl.decideAnswer(m.rootID, d.id, answer); err != nil {
				m.lines = append(m.lines, line{kind: "err", text: "回答回传失败: " + err.Error()})
				return nil
			}
			m.lines = append(m.lines, line{kind: "info", text: "已回答"})
		case "plan":
			m.resolvePlan("feedback", text)
		default:
			m.lines = append(m.lines, line{kind: "info", text: "这一项请按 y / n 选择"})
			return nil
		}
		m.input.SetValue("")
		return nil
	}
	if err := m.cl.send(m.rootID, text, m.cwd); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusConflict {
			m.lines = append(m.lines, line{kind: "info", text: "已有轮次运行中"})
		} else {
			m.lines = append(m.lines, line{kind: "err", text: "发送失败: " + err.Error()})
		}
		return nil
	}
	m.lines = append(m.lines, line{kind: "user", text: text})
	m.echoUser = text
	m.streaming = true
	m.tools = 0
	m.input.SetValue("")
	return nil
}

/* ---- 渲染 ---- */

func (m *tuiModel) View() string {
	if m.width == 0 {
		return "启动中…"
	}
	head := titleStyle.Render(" ezharness tui") + dimStyle.Render(" · "+m.addr+" · "+m.version)
	bodyH := m.height - 3 // 标题 / 提示 / 输入
	if bodyH < 1 {
		bodyH = 1
	}

	var body []string
	if m.picker != nil {
		body = m.pickerRows(bodyH)
	} else {
		rows := m.chatRows(m.width)
		if len(rows) > bodyH {
			rows = rows[len(rows)-bodyH:]
		} else if len(rows) < bodyH {
			rows = append(rows, make([]string, bodyH-len(rows))...)
		}
		body = rows
	}

	hint := ""
	switch {
	case m.picker != nil:
		hint = "↑↓ 选择 · Enter 确认 · Esc 取消"
	case len(m.pending) > 0:
		hint = pendingHint(m.pending[0])
	case m.streaming:
		hint = fmt.Sprintf("● 运行中 · 工具 %d", m.tools)
	case m.err != "":
		hint = m.err
	}
	hint = runewidth.Truncate(hint, m.width-2, "…")

	return head + "\n" + strings.Join(body, "\n") + "\n" + dimStyle.Render(" "+hint) + "\n" + " ❯ " + m.input.View()
}

/* pendingHint 底栏提示：当前等待什么回答。 */
func pendingHint(d decision) string {
	switch d.kind {
	case "approve":
		return "审批中：y 允许 · n 拒绝"
	case "ask":
		return "等待回答：输入答案后 Enter 发送"
	case "plan":
		return "计划：Enter 确认 · 输入反馈后 Enter 提交"
	case "mcp":
		if d.busy {
			return "等待浏览器授权…"
		}
		return "授权：y 打开浏览器 · n 取消"
	}
	return ""
}

/* chatRows 把消息展开成终端行（按显示宽度折行）。 */
func (m *tuiModel) chatRows(width int) []string {
	var out []string
	for _, l := range m.lines {
		prefix, style := "  ", infoStyle
		switch l.kind {
		case "user":
			prefix, style = "你  ", userStyle
		case "assistant":
			prefix, style = "AI  ", aiStyle
		case "ask":
			prefix, style = "?   ", askStyle
		case "plan":
			prefix, style = "计划 ", askStyle
		case "approve":
			prefix, style = "审批 ", askStyle
		case "mcp":
			prefix, style = "授权 ", askStyle
		case "err":
			prefix, style = "!   ", errStyle
		}
		w := width - runewidth.StringWidth(prefix) - 1
		if w < 10 {
			w = 10
		}
		for i, row := range strings.Split(runewidth.Wrap(l.text, w), "\n") {
			if i == 0 {
				out = append(out, style.Render(prefix+row))
			} else {
				out = append(out, style.Render(strings.Repeat(" ", runewidth.StringWidth(prefix))+row))
			}
		}
	}
	return out
}

/* pickerRows 渲染选择列表（超出高度时窗口跟随选中项）。 */
func (m *tuiModel) pickerRows(height int) []string {
	p := m.picker
	rows := []string{titleStyle.Render(" " + p.title)}
	start := 0
	if p.sel >= height-2 && height > 2 {
		start = p.sel - height + 3
	}
	for i := start; i < len(p.items) && len(rows) < height; i++ {
		mark := "  "
		if i == p.sel {
			mark = selStyle.Render("▸ ")
		}
		rows = append(rows, mark+p.items[i].label)
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return rows
}
