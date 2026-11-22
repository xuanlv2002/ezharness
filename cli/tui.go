/*
tui 终端界面：与桌面端同一套时间线——用户消息、流式回复、折叠成一行
的思考和工具组；提问 / 计划 / 授权进时间线，审批与授权走底部选择栏
（↑↓ 选择 · Enter 确认，审批输入文字后 Enter 为拒绝理由）。斜杠命令管
分支、归档、审批档与模型；启动目录随消息带给 core，作软提示。
*/
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

/* line 是会话区一条逻辑消息。 */
type line struct {
	kind string // user | assistant | reasoning | tools | ask | plan | mcp | info | err
	text string
	md   string // assistant 的 markdown 渲染缓存（按 mdW 宽）
	mdW  int
}

/* decision 是一条等待用户回答的请求。 */
type decision struct {
	id       string
	kind     string // approve | ask | plan | mcp
	name     string // 工具名 / MCP 服务名
	detail   string // 审批参数摘要（cmd / path …）
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

/* toolRun 是执行中的工具（底栏「执行中 xxx」）。 */
type toolRun struct {
	id   string
	name string
}

/* attach 是待发送的本地文件（粘贴文件路径得到）。 */
type attach struct {
	name  string
	path  string
	image bool
}

/* slashCmd 是斜杠命令的一条提示。 */
type slashCmd struct{ name, desc string }

/* slashCommands 是输入 / 时的命令提示（顺序即展示顺序）。 */
var slashCommands = []slashCmd{
	{"session", "切换分支"},
	{"new", "新建分支"},
	{"compact", "归档并开新话题"},
	{"permissions", "审批档（无审批 / 跟随安全 / 全审批）"},
	{"models", "切换模型"},
	{"status", "会话状态"},
	{"help", "命令帮助"},
}

/*
cursorWriter 把真实终端光标挪到输入框光标处：bubbletea v1 每帧只把

	光标停在末行行首，输入法合成文字会贴到屏幕最左边。全屏渲染每帧以
	\x1b[<行>;H 收尾，这里在帧后补一个同行的列定位。
*/
type cursorWriter struct {
	out io.Writer
	col atomic.Int64 // 1-based 列；0 = 不移动
}

/*
Fd/Read/Close 让 cursorWriter 满足 term.File：bubbletea 用 WithOutput

	换上包装 writer 后原本会丢掉 ttyOutput，初始尺寸和窗口缩放全探不到。
	Read 不会被调用（输出端只写），Close 不能真的关掉 stdout。
*/
func (w *cursorWriter) Fd() uintptr {
	if f, ok := w.out.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return os.Stdout.Fd()
}

func (w *cursorWriter) Read([]byte) (int, error) { return 0, io.EOF }
func (w *cursorWriter) Close() error             { return nil }

func (w *cursorWriter) Write(p []byte) (int, error) {
	n, err := w.out.Write(p)
	if err != nil || n != len(p) {
		return n, err
	}
	col := int(w.col.Load())
	if col < 1 {
		return n, nil
	}
	row, ok := frameRow(p)
	if !ok {
		return n, nil
	}
	fmt.Fprintf(w.out, "\x1b[%d;%dH", row, col)
	return n, nil
}

/* frameRow 取帧末光标归位序列 \x1b[<行>;H 的行号（\x1b[H 是清屏回家，不算帧）。 */
func frameRow(p []byte) (int, bool) {
	s := string(p)
	if !strings.HasSuffix(s, "H") {
		return 0, false
	}
	i := strings.LastIndex(s, "\x1b[")
	if i < 0 {
		return 0, false
	}
	seq := s[i+2 : len(s)-1]
	if !strings.HasSuffix(seq, ";") {
		return 0, false
	}
	row, err := strconv.Atoi(strings.TrimSuffix(seq, ";"))
	if err != nil || row < 1 {
		return 0, false
	}
	return row, true
}

type tuiModel struct {
	cl           *client
	addr         string
	version      string
	cwd          string
	rootID       string
	branches     []branch
	lines        []line
	histCount    int // 历史消息行数：replay.sync 重建时截回这里
	input        textinput.Model
	streaming    bool
	liveText     string    // 当前这次模型调用的流式正文
	liveThink    string    // 当前这次模型调用的流式思考
	running      []toolRun // 执行中的工具
	group        []string  // 最近一组工具调用的名字（多个折叠成一行）
	groupOpen    bool
	echoUser     string
	frames       chan frame
	stopSse      func()
	width        int
	height       int
	scroll       int // 会话向上翻的行数（0 = 贴底最新）
	chatTotal    int // 上一帧会话总行数：上翻时新行不把视图顶走
	err          string
	pending      []decision
	attaches     []attach
	picker       *picker
	status       *statusView
	sugSel       int                // 斜杠提示当前选中项
	sel          int                // 当前待办的选中项（提问选项 / 允许拒绝 / 去授权取消）
	cursor       *cursorWriter      // 真实光标定位（输入法合成位置）
	mcpStop      context.CancelFunc // 取消进行中的 MCP 授权轮询
	interrupting bool               // 刚发新消息打断上一轮：忽略其取消错误
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
	imgStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	fileStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
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
	in.Prompt = "" // 输入行的 ❯ 由 View 统一画，避免 "❯ > " 双前缀
	in.Focus()
	in.CharLimit = 8000

	out := &cursorWriter{out: os.Stdout}
	m := &tuiModel{
		cl: cl, addr: displayAddr(base), version: version, cwd: cwd,
		rootID: b.SessionID, branches: b.Branches, input: in,
		frames: make(chan frame, 256), status: b.Status, cursor: out,
		width: 80, height: 24, // 无窗口尺寸事件时的兜底（管道/pty 尺寸为 0）
	}
	m.applyWidth(m.width)
	m.loadHistory()

	/* 进入前净屏（含滚动区）：部分终端切备用屏不擦已滚出的旧内容 */
	fmt.Print("\x1b[2J\x1b[3J\x1b[H")
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(out), tea.WithMouseCellMotion())
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
	// 不开光标闪烁：View 里靠反色光标定位列，闪烁时找不到它
	return m.waitForFrame()
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
	loadHistory 拉当前分支的持久消息：用户输入、思考、回复与工具组都还原

（带工具调用的中间发言也显示，与桌面端一致）。
*/
func (m *tuiModel) loadHistory() {
	m.lines = m.lines[:0]
	m.scroll, m.chatTotal = 0, 0
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
			if t := reasoningText(msg.Reasoning); t != "" {
				m.lines = append(m.lines, line{kind: "reasoning", text: t})
			}
			if strings.TrimSpace(msg.Content) != "" {
				m.lines = append(m.lines, line{kind: "assistant", text: msg.Content})
			}
			if len(msg.ToolCalls) > 0 {
				names := make([]string, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					names = append(names, tc.Name)
				}
				m.lines = append(m.lines, line{kind: "tools", text: toolsText(names)})
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
	m.stopMcp()
	m.pending = nil
	m.resetSel()
	m.loadHistory()
	m.subscribe()
	m.err = ""
}

/* resetTurn 清掉本轮临时流式状态（切轮 / 回放重建时）。 */
func (m *tuiModel) resetTurn() {
	m.liveText = ""
	m.liveThink = ""
	m.running = nil
	m.group = nil
	m.groupOpen = false
}

/* applyFrame 把 SSE 帧并入界面：正文流式追加，思考与工具折叠成行。 */
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
		m.resetTurn()
		m.echoUser = ""
		m.err = ""
		if d.TurnActive && len(m.lines) > m.histCount {
			m.lines = m.lines[:m.histCount]
		}
		m.streaming = d.TurnActive
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
		m.resetTurn()
		m.streaming = true
	case "stream.snapshot":
		// 回放重建：当前这次调用的正文/思考全量（增量帧不进回放缓存）
		var d struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"`
		}
		if json.Unmarshal(f.Data, &d) == nil {
			m.liveText, m.liveThink = d.Content, d.Reasoning
		}
	case "reasoning_chunk":
		var s string
		if json.Unmarshal(f.Data, &s) == nil {
			m.liveThink += s
		}
	case "model_chunk":
		var s string
		if json.Unmarshal(f.Data, &s) == nil {
			m.liveText += s
		}
	case "model_end":
		var d modelEndData
		if json.Unmarshal(f.Data, &d) == nil {
			if t := reasoningText(d.Reasoning); t != "" {
				m.lines = append(m.lines, line{kind: "reasoning", text: t})
				m.groupOpen = false
			}
			if strings.TrimSpace(d.Content) != "" {
				m.lines = append(m.lines, line{kind: "assistant", text: d.Content})
				m.groupOpen = false
			}
		}
		m.liveText, m.liveThink = "", ""
	case "tool_start":
		var d toolStartData
		if json.Unmarshal(f.Data, &d) == nil && d.Name != "" {
			m.addTool(d.ID, d.Name)
		}
	case "tool_end":
		var d toolEndData
		if json.Unmarshal(f.Data, &d) == nil {
			m.running = slices.DeleteFunc(m.running, func(r toolRun) bool { return r.id == d.CallID })
			if len(m.running) == 0 {
				m.groupOpen = false // 这一批工具结束：下一批另起一行
			}
		}
	case "turn_end":
		var d turnEndData
		_ = json.Unmarshal(f.Data, &d)
		if m.interrupting {
			// 用户主动发新消息打断了上一轮：旧轮的 context canceled 不是错误
			m.interrupting = false
			m.resetTurn()
			m.echoUser = ""
			m.streaming = true // 新消息已在路上，等它的 loop_start
			return
		}
		m.streaming = false
		m.resetTurn()
		m.stopMcp()
		if len(m.pending) > 0 {
			m.input.SetValue("") // 待办已随轮作废，草稿也别留成消息
		}
		m.pending = nil // 轮结束：没答完的请求已随轮作废，别留着挡住输入
		m.resetSel()
		m.echoUser = ""
		if d.Err != "" {
			m.lines = append(m.lines, line{kind: "err", text: d.Err})
		}
	case "decision.resolved":
		// 别的客户端先答了：撤掉本端的待办，避免过期提示挡输入
		var d struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(f.Data, &d) == nil && d.ID != "" {
			if len(m.pending) > 0 && m.pending[0].id == d.ID {
				if m.pending[0].kind == "mcp" {
					m.stopMcp()
				}
				m.input.SetValue("") // 该待办已由别处作答，清掉理由/答案草稿
			}
			m.pending = slices.DeleteFunc(m.pending, func(x decision) bool { return x.id == d.ID })
			m.resetSel()
		}
	case "approve.request", "askuser.request", "plan.request":
		m.addDecision(f.Type, f.Data)
	case "mcp.auth.request":
		m.addMcpAuth(f.Data)
	}
}

/* addTool 登记一次工具调用：连续的调用折叠进同一行，中间有正文则另开一组。 */
func (m *tuiModel) addTool(id, name string) {
	m.running = append(m.running, toolRun{id: id, name: name})
	if m.groupOpen && len(m.lines) > 0 && m.lines[len(m.lines)-1].kind == "tools" {
		m.group = append(m.group, name)
		m.lines[len(m.lines)-1].text = toolsText(m.group)
		return
	}
	m.group = []string{name}
	m.groupOpen = true
	m.lines = append(m.lines, line{kind: "tools", text: toolsText(m.group)})
}

/* reasoningText 折叠思考：只留字数（与桌面端的折叠标题一致）。 */
func reasoningText(s string) string {
	n := len([]rune(strings.TrimSpace(s)))
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d 字", n)
}

/* toolsText 折叠工具组：调用数 + 按名计数（terminal ×3、read_file）。 */
func toolsText(names []string) string {
	return fmt.Sprintf("%d 个调用 · %s", len(names), countNames(names))
}

/* countNames 按名计数并按首次出现顺序拼接。 */
func countNames(names []string) string {
	var order []string
	counts := map[string]int{}
	for _, n := range names {
		n = oneLine(n)
		if counts[n] == 0 {
			order = append(order, n)
		}
		counts[n]++
	}
	parts := make([]string, 0, len(order))
	for _, n := range order {
		if counts[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s ×%d", n, counts[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "、")
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
		// 审批不进时间线：底栏带工具名逐项询问，避免一串审批行刷屏
		dec.kind, dec.detail = "approve", argsSummary(d.Args)
		m.pending = append(m.pending, dec)
		return
	case "askuser.request":
		var a struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		decodeArgs(d.Args, &a)
		dec.kind, dec.question, dec.options = "ask", a.Question, a.Options
		m.lines = append(m.lines, line{kind: "ask", text: "提问：" + a.Question})
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
		for i, s := range p.Progress {
			m.lines = append(m.lines, line{kind: "info", text: fmt.Sprintf("%d. %s", i+1, s.Title)})
		}
	}
	m.groupOpen = false
	m.scroll = 0 // 新请求出现：拉回底部，先看它
	m.pending = append(m.pending, dec)
	if len(m.pending) == 1 {
		m.resetSel()
	}
}

/* addMcpAuth 登记一条 MCP OAuth 授权请求。 */
func (m *tuiModel) addMcpAuth(data json.RawMessage) {
	var d mcpAuthData
	if json.Unmarshal(data, &d) != nil || d.ID == "" || d.Server == "" || m.hasPending(d.ID) {
		return
	}
	m.scroll = 0
	m.pending = append(m.pending, decision{id: d.ID, kind: "mcp", name: d.Server, metaURL: d.MetadataURL})
	m.lines = append(m.lines, line{kind: "mcp", text: "MCP 服务 " + d.Server + " 需要 OAuth 授权"})
	m.groupOpen = false
}

/* argsSummary 取审批工具参数的一行摘要（命令/路径/网址优先）。 */
func argsSummary(raw json.RawMessage) string {
	var m map[string]any
	if !decodeArgs(raw, &m) {
		return ""
	}
	for _, k := range []string{"cmd", "command", "path", "url", "query", "task", "name", "text"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return runewidth.Truncate(strings.Join(strings.Fields(v), " "), 40, "…")
		}
	}
	return ""
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

/* maxAttach 单条消息附件上限（与 core 的 files+refs 上限一致）。 */
const maxAttach = 8

/* pasteAttach 把粘贴进来的本地文件路径转成附件；不是文件返回 false。 */
func (m *tuiModel) pasteAttach(raw string) bool {
	var paths []string
	for _, ln := range strings.Split(strings.TrimSpace(raw), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		p, ok := filePathOf(ln)
		if !ok {
			return false
		}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		m.addAttach(p)
	}
	return true
}

/* filePathOf 解析终端粘贴/拖入的路径（可能带引号或反斜杠转义）。 */
func filePathOf(raw string) (string, bool) {
	unq := strings.Trim(raw, "\"'")
	cands := []string{raw, unq, strings.ReplaceAll(unq, `\ `, " ")}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

/* addAttach 登记一个文件；图片用 [IMAGE]，其余 [FILE]。 */
func (m *tuiModel) addAttach(path string) {
	if len(m.attaches) >= maxAttach {
		m.lines = append(m.lines, line{kind: "err", text: "附件最多 8 个"})
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	m.attaches = append(m.attaches, attach{name: filepath.Base(abs), path: filepath.ToSlash(abs), image: isImageFile(abs)})
}

/* isImageFile 按魔数判图片（与 core 图片通道同一集合）。 */
func isImageFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 12)
	n, _ := f.Read(buf)
	b := buf[:n]
	switch {
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return true
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return true
	}
	return false
}

/* attachRow 附件行：一眼看到 [IMAGE] / [FILE]。 */
func (m *tuiModel) attachRow() string {
	parts := make([]string, 0, len(m.attaches))
	for _, a := range m.attaches {
		tag := fileStyle.Render("[FILE]")
		if a.image {
			tag = imgStyle.Render("[IMAGE]")
		}
		parts = append(parts, tag+dimStyle.Render(" "+a.name))
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), m.width-2, "…")
}

/* sendFiles 把待发附件转成 API 载荷。 */
func (m *tuiModel) sendFiles() []sendFile {
	files := make([]sendFile, 0, len(m.attaches))
	for _, a := range m.attaches {
		files = append(files, sendFile{Name: a.name, Path: a.path})
	}
	return files
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
	m.resetSel()
	return d
}

func (m *tuiModel) decideApprove(ok bool, reason string) {
	d := m.pending[0]
	if err := m.cl.decideApprove(m.rootID, d.id, ok, reason); err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "审批回传失败: " + err.Error()})
		return
	}
	m.pending = m.pending[1:]
	m.resetSel()
	m.input.SetValue("")
	if !ok && strings.TrimSpace(reason) != "" {
		m.lines = append(m.lines, line{kind: "info", text: "已拒绝 " + oneLine(d.name) + "：" + oneLine(reason)})
	}
}

func (m *tuiModel) resolvePlan(action, feedback string) {
	d := m.pending[0]
	if err := m.cl.decidePlan(m.rootID, d.id, action, feedback); err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "计划回传失败: " + err.Error()})
		return
	}
	m.pending = m.pending[1:]
	m.resetSel()
	if action == "confirm" {
		m.lines = append(m.lines, line{kind: "info", text: "计划已确认"})
	} else {
		m.lines = append(m.lines, line{kind: "user", text: feedback})
	}
}

/* stopMcp 取消进行中的授权轮询（切分支/重载时）。 */
func (m *tuiModel) stopMcp() {
	if m.mcpStop != nil {
		m.mcpStop()
		m.mcpStop = nil
	}
}

/* resetSel 待办切换后重置选中项：审批默认落在拒绝，其余落在第一项。 */
func (m *tuiModel) resetSel() {
	m.sel = 0
	if len(m.pending) > 0 && m.pending[0].kind == "approve" {
		m.sel = 1
	}
}

/* answerAsk 回传提问的答案（选中的选项或自由输入）。 */
func (m *tuiModel) answerAsk(d decision, answer string) {
	if err := m.cl.decideAnswer(m.rootID, d.id, answer); err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "回答回传失败: " + err.Error()})
		return
	}
	m.pending = m.pending[1:]
	m.resetSel()
	m.lines = append(m.lines, line{kind: "user", text: answer})
}

/* headAsk 当前待回答且带选项的提问。 */
func (m *tuiModel) headAsk() (decision, bool) {
	if len(m.pending) == 0 || m.pending[0].kind != "ask" || len(m.pending[0].options) == 0 {
		return decision{}, false
	}
	return m.pending[0], true
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

/* startMcpAuth 先打开授权页，再轮询到授权完成（5 分钟上限；n 可取消）。 */
func startMcpAuth(cl *client, d decision, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		url, err := cl.oauthStart(d.name, d.metaURL)
		if err != nil {
			return mcpAuthMsg{id: d.id, err: err.Error()}
		}
		openURL(url)
		deadline := time.Now().Add(5 * time.Minute)
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				_ = cl.oauthCancel(d.name)
				return mcpAuthMsg{id: d.id, err: "已取消"}
			case <-tick.C:
			}
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
	m.resetSel()
	m.mcpStop = nil
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

/* resolveCommand 精确或唯一前缀匹配斜杠命令；hits 是含糊时的候选。 */
func resolveCommand(s string) (string, []string) {
	if s == "" {
		return "help", nil
	}
	exact := ""
	var hits []string
	for _, c := range slashCommands {
		if c.name == s {
			exact = c.name
		}
		if strings.HasPrefix(c.name, s) {
			hits = append(hits, c.name)
		}
	}
	if exact != "" {
		return exact, nil
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	return "", hits
}

func (m *tuiModel) runCommand(cmd string) tea.Cmd {
	if len(m.pending) > 0 && (cmd == "session" || cmd == "new" || cmd == "compact") {
		m.lines = append(m.lines, line{kind: "info", text: "先处理当前请求（审批/回答）再切分支"})
		return nil
	}
	switch cmd {
	case "help":
		m.lines = append(m.lines,
			line{kind: "info", text: "命令：/session 分支 · /new 新分支 · /compact 归档 · /permissions 审批档 · /models 模型 · /status 状态"},
			line{kind: "info", text: "审批/提问/计划/授权：↑↓ 选择 · Enter 确认；审批输入文字后 Enter 为拒绝理由"},
			line{kind: "info", text: "粘贴/拖入文件路径变成 [IMAGE]/[FILE] 附件；空输入退格删除最后一个"},
			line{kind: "info", text: "PgUp/PgDn 或滚轮翻阅历史；选中文本请按住 Shift 拖拽"},
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
		m.stopMcp()
		m.pending = nil
		m.resetSel()
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
			m.height = msg.Height
			m.applyWidth(msg.Width)
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
		switch msg.Type {
		case tea.KeyUp:
			if m.moveSel(-1) {
				return m, nil
			}
		case tea.KeyDown:
			if m.moveSel(1) {
				return m, nil
			}
		}
		if m.picker != nil {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.picker = nil
			case tea.KeyEnter:
				m.applyPick()
			}
			return m, nil
		}
		if (msg.Paste || len(msg.Runes) > 1) && len(m.pending) == 0 && m.pasteAttach(string(msg.Runes)) {
			return m, nil // 粘贴的是本地文件路径：变成附件，不写进输入框
		}
		if len(m.suggestions()) > 0 && msg.Type == tea.KeyTab {
			m.fillSuggestion()
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			return m, m.submit()
		case tea.KeyPgUp:
			m.scrollChat(m.pageStep())
			return m, nil
		case tea.KeyPgDown:
			m.scrollChat(-m.pageStep())
			return m, nil
		}
		if len(m.pending) > 0 && m.pending[0].kind == "mcp" {
			return m, nil // 授权期间不出文本输入
		}
		if msg.Type == tea.KeyBackspace && len(m.pending) == 0 && m.input.Value() == "" && len(m.attaches) > 0 {
			m.attaches = m.attaches[:len(m.attaches)-1] // 空输入退格删最后一个附件
			return m, nil
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if !m.moveSel(-1) {
				m.scrollChat(3)
			}
		case tea.MouseButtonWheelDown:
			if !m.moveSel(1) {
				m.scrollChat(-3)
			}
		}
		return m, nil
	case frame:
		m.applyFrame(msg)
		return m, m.waitForFrame()
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyRunes, tea.KeyBackspace, tea.KeyDelete:
			m.sugSel = 0 // 候选变了，回到第一项
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if len(m.pending) > 0 && m.pending[0].kind == "approve" && strings.TrimSpace(m.input.Value()) != "" {
		m.sel = 1 // 写了理由：选中拒绝（↑ 可再选回允许）
	}
	return m, cmd
}

/* submit 处理回车：待办回答优先（模态），空闲时才是斜杠命令/发消息。 */
func (m *tuiModel) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if len(m.pending) > 0 {
		d := m.pending[0]
		switch d.kind {
		case "approve":
			if m.sel == 1 {
				m.decideApprove(false, text) // 选中拒绝：输入的文字作为理由
			} else {
				m.decideApprove(true, "") // 选中允许：理由不参与
			}
			return nil
		case "mcp":
			if m.sel == 1 { // 取消
				if d.busy {
					m.stopMcp() // 轮询会回来按「已取消」结算
				} else {
					m.dropPending()
					_ = m.cl.oauthCancel(d.name)
					_ = m.cl.decideMcpAuth(m.rootID, d.id, false, "已取消")
					m.lines = append(m.lines, line{kind: "info", text: "已取消 MCP 授权"})
				}
			} else if !d.busy { // 去授权
				ctx, cancel := context.WithCancel(context.Background())
				m.mcpStop = cancel
				m.pending[0].busy = true
				m.sel = 1 // 等待中默认停在取消
				return startMcpAuth(m.cl, d, ctx)
			}
			return nil
		case "ask":
			if text == "" {
				if len(d.options) > 0 {
					if m.sel < 0 || m.sel >= len(d.options) {
						m.sel = 0
					}
					m.answerAsk(d, d.options[m.sel])
				}
				return nil
			}
			answer := text
			if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(d.options) {
				answer = d.options[n-1]
			}
			m.answerAsk(d, answer)
			m.input.SetValue("")
			return nil
		case "plan":
			if text == "" {
				m.resolvePlan("confirm", "")
			} else {
				m.resolvePlan("feedback", text)
			}
			m.input.SetValue("")
			return nil
		}
	}
	if text == "" && len(m.attaches) == 0 {
		return nil
	}
	if len(m.attaches) == 0 && strings.HasPrefix(text, "/") {
		raw := strings.TrimSpace(strings.TrimPrefix(text, "/"))
		if sugs := m.suggestions(); len(sugs) > 0 {
			if m.sugSel < 0 || m.sugSel >= len(sugs) {
				m.sugSel = 0
			}
			if sel := sugs[m.sugSel].name; raw != sel {
				m.input.SetValue("/" + sel) // 先填入，再回车执行
				m.input.CursorEnd()
				return nil
			}
		}
		name, hits := resolveCommand(raw)
		if name == "" {
			if len(hits) == 0 {
				m.lines = append(m.lines, line{kind: "err", text: "未知命令 /" + raw + "，/help 查看"})
			} else {
				m.lines = append(m.lines, line{kind: "info", text: "匹配：/" + strings.Join(hits, " /")})
			}
			return nil // 输入留着，方便接着改
		}
		m.input.SetValue("")
		return m.runCommand(name)
	}
	wasRunning := m.streaming
	if err := m.cl.send(m.rootID, text, m.cwd, m.sendFiles()); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusConflict {
			m.lines = append(m.lines, line{kind: "info", text: "已有轮次运行中"})
		} else {
			m.lines = append(m.lines, line{kind: "err", text: "发送失败: " + err.Error()})
		}
		return nil
	}
	display := text
	for _, a := range m.attaches {
		tag := "[FILE]"
		if a.image {
			tag = "[IMAGE]"
		}
		if display != "" {
			display += "  "
		}
		display += tag + " " + a.name
	}
	m.lines = append(m.lines, line{kind: "user", text: display})
	m.echoUser = text
	m.attaches = nil
	m.scroll = 0
	m.streaming = true
	m.interrupting = wasRunning // 旧轮被 core 主动终止，别把它的取消错误打出来
	m.input.SetValue("")
	return nil
}

/* ---- 渲染 ---- */

/*
applyWidth 按终端宽度排输入区：右侧留出「工作中 目录」；空输入时
textinput 的光标块比 Width 多一格，末尾再留一列不写——写满最后一格
会触发终端自动换行，部分终端（conhost）会因此滚屏。
*/
func (m *tuiModel) applyWidth(w int) {
	m.width = w
	m.input.Width = w - 6 - lipgloss.Width(m.dirLabel())
	if m.input.Width < 10 {
		m.input.Width = 10
	}
}

/* dirLabel 输入行右侧的工作目录（窄屏截短，太窄就不显示）。 */
func (m *tuiModel) dirLabel() string {
	max := m.width / 2
	if max < 16 {
		return ""
	}
	return runewidth.Truncate(oneLine("工作中 "+m.cwd), max, "…")
}

func (m *tuiModel) View() string {
	if m.width == 0 {
		return "启动中…"
	}

	// 命令提示按高度开窗，保证选中项始终可见
	sugs := m.suggestionRows()
	if max := m.height - 5; len(sugs) > max {
		if max < 1 {
			sugs = nil
		} else {
			start := m.sugSel - max + 1
			if start < 0 {
				start = 0
			}
			if start > len(sugs)-max {
				start = len(sugs) - max
			}
			sugs = sugs[start : start+max]
		}
	}
	attachRows := 0
	if len(m.attaches) > 0 {
		attachRows = 1
	}
	bodyH := m.height - 3 - attachRows // 标题 / 提示 / 附件 / 输入
	if bodyH < 1 {
		bodyH = 1
	}

	// 会话从顶部往下排，空白留在下方；选择栏/命令提示压在底部
	panel := m.pickerPanel(bodyH)
	if panel == nil {
		panel = m.askPanel(bodyH)
	}
	if panel == nil {
		panel = m.decisionPanel(bodyH)
	}
	keep := bodyH - len(panel)
	body := append(m.chatWindow(m.chatRows(m.width), keep), panel...)
	head := titleStyle.Render(" ezharness tui") + dimStyle.Render(" · "+m.addr+" · "+m.version)
	if m.scroll > 0 {
		head += dimStyle.Render(fmt.Sprintf(" · 已上翻 %d 行", m.scroll))
	}
	if len(sugs) > 0 {
		body = append(body[:bodyH-len(sugs)], sugs...)
	}

	hintStyle := dimStyle
	switch {
	case len(m.pending) > 0:
		hintStyle = askStyle // 待回答的请求要显眼
	case m.err != "" && !m.streaming:
		hintStyle = errStyle
	}
	hintRow := hintStyle.Render(" " + fitHint(oneLine(m.statusHint(len(sugs) > 0)), m.width-2))

	out := []string{head}
	out = append(out, body...)
	out = append(out, hintRow)
	if len(m.attaches) > 0 {
		out = append(out, m.attachRow())
	}
	out = append(out, m.inputRow())
	/* 最后一道保险：任何一行都不许写满终端宽度，避免自动换行把整屏
	   行序冲乱（markdown 的内边距、宽字符宽度差异都可能超一格）。 */
	for i := range out {
		line := strings.ReplaceAll(out[i], "\t", "    ")
		line = strings.ReplaceAll(line, "\r", "")
		line = strings.ReplaceAll(line, "\n", " ")
		out[i] = ansi.Truncate(line, m.width-2, "")
	}
	return strings.Join(out, "\n")
}

/* inputRow 输入行：左边输入框，右边工作目录；MCP 授权时整行换成操作提示。 */
func (m *tuiModel) inputRow() string {
	if len(m.pending) > 0 && m.pending[0].kind == "mcp" {
		prompt := "↑↓ 选择 · Enter 确认"
		if m.pending[0].busy {
			prompt = "等待浏览器授权…（选取消后 Enter）"
		}
		if m.cursor != nil {
			m.cursor.col.Store(0) // 不画输入框，真实光标留在行首
		}
		return m.withDir(askStyle.Render(" ❯ " + prompt))
	}
	prefix := " ❯ "
	if len(m.pending) > 0 {
		prefix = askStyle.Render(" ❯ ") // 这行现在是给待办的回答/理由
	}
	row := prefix + m.input.View()
	if m.cursor != nil {
		if i := strings.LastIndex(row, "\x1b[7m"); i >= 0 { // textinput 的反色光标
			m.cursor.col.Store(int64(lipgloss.Width(row[:i]) + 1))
		}
	}
	return m.withDir(row)
}

/* withDir 把工作目录贴到这一行右侧（放不下就不显示）。 */
func (m *tuiModel) withDir(left string) string {
	dir := m.dirLabel()
	if dir == "" {
		return left
	}
	right := dimStyle.Render(dir)
	pad := m.width - 1 - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		return left
	}
	return left + strings.Repeat(" ", pad) + right
}

/* suggestions 输入 / 时的候选命令（按前缀过滤）；待办回答期间不出命令。 */
func (m *tuiModel) suggestions() []slashCmd {
	if m.picker != nil || len(m.pending) > 0 {
		return nil
	}
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \t") {
		return nil
	}
	prefix := strings.TrimPrefix(v, "/")
	var out []slashCmd
	for _, c := range slashCommands {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

/* suggestionRows 候选行：选中项高亮，↑↓ 选择、Enter/Tab 填入。 */
func (m *tuiModel) suggestionRows() []string {
	sugs := m.suggestions()
	if len(sugs) == 0 {
		return nil
	}
	if m.sugSel < 0 || m.sugSel >= len(sugs) {
		m.sugSel = 0
	}
	rows := make([]string, 0, len(sugs))
	for i, c := range sugs {
		mark := "  "
		if i == m.sugSel {
			mark = "▸ "
		}
		row := runewidth.Truncate(fmt.Sprintf("%s/%-10s %s", mark, c.name, c.desc), m.width-1, "…")
		if i == m.sugSel {
			rows = append(rows, selStyle.Render(row))
		} else {
			rows = append(rows, dimStyle.Render(row))
		}
	}
	return rows
}

/* fillSuggestion 把选中命令填进输入框（再回车执行）。 */
func (m *tuiModel) fillSuggestion() {
	sugs := m.suggestions()
	if len(sugs) == 0 {
		return
	}
	if m.sugSel < 0 || m.sugSel >= len(sugs) {
		m.sugSel = 0
	}
	m.input.SetValue("/" + sugs[m.sugSel].name)
	m.input.CursorEnd()
}

/* statusHint 底栏左侧：当前状态或待回答的请求。 */
func (m *tuiModel) statusHint(hasSug bool) string {
	switch {
	case m.picker != nil:
		return "↑↓ 选择 · Enter 确认 · Esc 取消"
	case len(m.pending) > 0:
		return m.pendingHint()
	case m.streaming:
		if len(m.running) > 0 {
			names := make([]string, 0, len(m.running))
			for _, r := range m.running {
				names = append(names, r.name)
			}
			return "执行中 " + countNames(names)
		}
		if m.liveThink != "" && m.liveText == "" {
			return "思考中…"
		}
		if m.liveText != "" {
			return "输出中…"
		}
		return "运行中…"
	case m.err != "":
		return m.err
	case hasSug:
		return "↑↓ 选择 · Enter/Tab 填入"
	}
	return "输入消息，/ 开头是命令"
}

/* pendingHint 底栏提示：当前要回答的是哪一项、还有几项。 */
func (m *tuiModel) pendingHint() string {
	d := m.pending[0]
	pos := ""
	if len(m.pending) > 1 || d.kind == "approve" || d.kind == "mcp" {
		pos = fmt.Sprintf(" %d/%d", 1, len(m.pending))
	}
	switch d.kind {
	case "approve":
		return "↑↓ 选择 · Enter 确认 · 输入文字后 Enter 为拒绝理由"
	case "ask":
		if len(d.options) > 0 {
			if strings.TrimSpace(m.input.Value()) != "" {
				return "等待回答" + pos + "：Enter 发送输入 · ↑↓ 选选项"
			}
			return "等待回答" + pos + "：↑↓ 选择 · Enter 发送 · 也可直接输入"
		}
		return "等待回答" + pos + "：输入答案后 Enter 发送"
	case "plan":
		return "计划" + pos + "：Enter 确认 · 输入反馈后 Enter 提交"
	case "mcp":
		if d.busy {
			return "等待浏览器授权：↑↓ 选取消 · Enter 确认"
		}
		return "↑↓ 选择 · Enter 确认"
	}
	return ""
}

/* fitHint 按宽度截断底栏提示；有「：动作」后缀时优先保留动作。 */
func fitHint(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if i := strings.LastIndex(s, "："); i > 0 {
		tail := s[i:]
		if room := w - runewidth.StringWidth(tail) - 1; room > 0 {
			return runewidth.Truncate(s[:i], room, "…") + tail
		}
	}
	return runewidth.Truncate(s, w, "…")
}

/* oneLine 把文本压成单行：选择栏、底栏提示里出现换行会把整帧行数冲乱。 */
func oneLine(s string) string {
	return strings.ReplaceAll(cleanText(s), "\n", " ")
}

/*
cleanText 清掉会破坏终端行布局的控制字符：回车按换行、制表按四空格，

	其余 C0 控制符（含 ESC）丢弃——模型/工具内容不能直接当终端转义用。
*/
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

/* chatWindow 按滚动偏移取可视窗口；上翻时新行不把视图顶走。 */
func (m *tuiModel) chatWindow(all []string, keep int) []string {
	total := len(all)
	if m.scroll > 0 && total > m.chatTotal {
		m.scroll += total - m.chatTotal
	}
	m.chatTotal = total
	maxScroll := total - keep
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if total > keep {
		end := total - m.scroll
		return all[end-keep : end]
	}
	return append(all, make([]string, keep-total)...)
}

/* scrollChat 上翻/下翻会话；正数向上翻。 */
func (m *tuiModel) scrollChat(delta int) {
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > m.chatTotal {
		m.scroll = m.chatTotal
	}
}

/* pageStep 一页滚动的行数。 */
func (m *tuiModel) pageStep() int {
	n := m.height - 6
	if n < 5 {
		n = 5
	}
	return n
}

/* moveSel 上下移动当前可选项（选择栏/命令提示/待办）；没有列表返回 false。 */
func (m *tuiModel) moveSel(delta int) bool {
	if m.picker != nil {
		if n := len(m.picker.items); n > 0 {
			m.picker.sel = (m.picker.sel + delta + n) % n
		}
		return true
	}
	if sugs := m.suggestions(); len(sugs) > 0 {
		m.sugSel = (m.sugSel + delta + len(sugs)) % len(sugs)
		return true
	}
	if len(m.pending) > 0 {
		n := 0
		switch m.pending[0].kind {
		case "ask":
			n = len(m.pending[0].options)
		case "approve", "mcp":
			n = 2
		}
		if n > 0 {
			m.sel = (m.sel + delta + n) % n
			return true
		}
	}
	return false
}

/* chatRows 把消息展开成终端行（按显示宽度折行），末尾接流式正文/思考。 */
func (m *tuiModel) chatRows(width int) []string {
	var out []string
	for i := range m.lines {
		l := &m.lines[i]
		if l.kind == "user" && len(out) > 0 {
			out = append(out, "") // 新一轮前空一行，把对话和上一轮过程分开
		}
		if l.kind == "assistant" && width > 24 {
			if l.mdW != width {
				l.md, l.mdW = renderMarkdown(l.text, width-5), width
			}
			if l.md != "" {
				out = append(out, markdownLines(l.md, width)...)
				continue
			}
		}
		out = appendMessageRows(out, *l, width)
	}
	if m.liveThink != "" {
		out = appendMessageRows(out, line{kind: "reasoning", text: reasoningText(m.liveThink)}, width)
	}
	if m.liveText != "" {
		out = appendMessageRows(out, line{kind: "assistant", text: m.liveText + "▍"}, width)
	}
	return out
}

func appendMessageRows(out []string, l line, width int) []string {
	prefix, style := "  ", infoStyle
	switch l.kind {
	case "user":
		prefix, style = "你  ", userStyle
	case "assistant":
		prefix, style = "AI  ", aiStyle
	case "reasoning":
		prefix, style = "思考 ", dimStyle
	case "tools":
		prefix, style = "工具 ", dimStyle
	case "ask":
		prefix, style = "?   ", askStyle
	case "plan":
		prefix, style = "计划 ", askStyle
	case "mcp":
		prefix, style = "授权 ", askStyle
	case "err":
		prefix, style = "!   ", errStyle
	}
	w := width - runewidth.StringWidth(prefix) - 1
	if w < 10 {
		w = 10
	}
	text := cleanText(l.text)
	if l.kind == "tools" {
		text = runewidth.Truncate(text, w, "…") // 工具组固定一行，不折行刷屏
	}
	for i, row := range strings.Split(runewidth.Wrap(text, w), "\n") {
		if i == 0 {
			out = append(out, style.Render(prefix+row))
		} else {
			out = append(out, style.Render(strings.Repeat(" ", runewidth.StringWidth(prefix))+row))
		}
	}
	return out
}

/* panelRows 底部选择栏：分隔线 + 标题 + 窗口内选项，选中项高亮。 */
func (m *tuiModel) panelRows(titleText string, labels []string, sel, maxH int) []string {
	if maxH < 1 || len(labels) == 0 {
		return nil
	}
	if maxH > 10 { // 选择栏最多占 10 行，上面留出会话
		maxH = 10
	}
	if sel < 0 || sel >= len(labels) {
		sel = 0
	}
	head := " " + titleText + " "
	w := m.width - 1
	if w < 1 {
		w = 1
	}
	var title string
	if hw := runewidth.StringWidth(head); hw < w {
		title = dimStyle.Render("─") + titleStyle.Render(head) + dimStyle.Render(strings.Repeat("─", w-hw-1))
	} else {
		title = titleStyle.Render(runewidth.Truncate(head, w, "…"))
	}
	rows := []string{title}
	itemH := maxH - 1
	if itemH < 1 {
		return rows
	}
	start := 0
	if sel >= itemH {
		start = sel - itemH + 1 // 选中项始终留在窗口内
	}
	labelW := m.width - 3
	if labelW < 4 {
		labelW = 4
	}
	for i := start; i < len(labels) && len(rows) < maxH; i++ {
		label := runewidth.Truncate(oneLine(labels[i]), labelW, "…")
		if i == sel {
			rows = append(rows, selStyle.Render("▸ "+label))
		} else {
			rows = append(rows, "  "+label)
		}
	}
	return rows
}

/* decisionPanel 审批/MCP 的操作选择栏：允许/拒绝、去授权/取消。 */
func (m *tuiModel) decisionPanel(maxH int) []string {
	if len(m.pending) == 0 {
		return nil
	}
	d := m.pending[0]
	pos := ""
	if len(m.pending) > 1 {
		pos = fmt.Sprintf(" 1/%d", len(m.pending))
	}
	switch d.kind {
	case "approve":
		title := "审批" + pos + " · " + oneLine(d.name)
		if d.detail != "" {
			title += " · " + oneLine(d.detail)
		}
		return m.panelRows(title, []string{"允许", "拒绝（输入的文字作为理由）"}, m.sel, maxH)
	case "mcp":
		title := "MCP 授权" + pos + " · " + oneLine(d.name)
		if d.busy {
			title += " · 等待浏览器"
		}
		return m.panelRows(title, []string{"去授权", "取消"}, m.sel, maxH)
	}
	return nil
}

func (m *tuiModel) pickerPanel(maxH int) []string {
	p := m.picker
	if p == nil {
		return nil
	}
	labels := make([]string, len(p.items))
	for i, it := range p.items {
		labels[i] = it.label
	}
	return m.panelRows(p.title, labels, p.sel, maxH)
}

/* askPanel 提问选项选择栏；空提问（无选项）不占面板。 */
func (m *tuiModel) askPanel(maxH int) []string {
	d, ok := m.headAsk()
	if !ok {
		return nil
	}
	labels := make([]string, len(d.options))
	for i, o := range d.options {
		labels[i] = fmt.Sprintf("%d. %s", i+1, o)
	}
	return m.panelRows("提问 · 选择或直接输入回答", labels, m.sel, maxH)
}
