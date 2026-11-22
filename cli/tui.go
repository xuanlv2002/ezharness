/*
tui 终端界面：左分支右会话 + 底部输入。SSE 实时渲染（用户消息来自主轮
loop_start 帧、回复流式累积、工具一行动作名）；Tab 在分支/输入间切焦点，
分支切换走 activate + 重载历史 + 重订阅。
*/
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

/* line 是会话区一行渲染单元。 */
type line struct {
	kind string // user | assistant | tool | info | err
	text string
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	userStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	aiStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	infoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	borderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type tuiModel struct {
	cl        *client
	addr      string
	version   string
	branches  []branch
	sel       int
	lines     []line
	histCount int // 历史消息行数：replay.sync 重建时截回这里
	input     textinput.Model
	listFocus bool
	streaming bool
	sawChunk  bool
	echoUser  string // 刚本地回显的用户消息：loop_start 回放同一条时跳过，防双显
	frames    chan frame
	stopSse   func()
	width     int
	height    int
	err       string
}

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

	in := textinput.New()
	in.Placeholder = "输入消息，Enter 发送…"
	in.Focus()
	in.Width = 60
	in.CharLimit = 8000

	m := &tuiModel{
		cl: cl, addr: displayAddr(base), version: version,
		branches: b.Branches, input: in, frames: make(chan frame, 256),
	}
	sel := 0
	for i, br := range b.Branches {
		if br.ID == b.SessionID {
			sel = i
		}
	}
	m.sel = sel
	m.loadHistory()

	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if m, ok := final.(*tuiModel); ok && m.stopSse != nil {
		m.stopSse()
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
	m.stopSse = m.cl.sseStream(m.branches[m.sel].ID, func(f frame) {
		select {
		case m.frames <- f:
		default: // 消费端卡住（弹窗/渲染）时丢帧保活：turn_end 会兜底补全
		}
	}, func(err error) {
		/* SSE 回调在独立 goroutine：错误也走帧通道，不直接改模型字段 */
		data, _ := json.Marshal("SSE 断开: " + err.Error())
		select {
		case m.frames <- frame{Type: "sse.error", Data: data}:
		default:
		}
	})
}

/* loadHistory 拉当前分支的持久消息（运行中本轮由 SSE 回放重建）。 */
func (m *tuiModel) loadHistory() {
	m.lines = m.lines[:0]
	h, err := m.cl.history(m.branches[m.sel].ID)
	if err != nil {
		m.lines = append(m.lines, line{kind: "err", text: "历史加载失败: " + err.Error()})
		m.histCount = len(m.lines)
		return
	}
	for _, msg := range h.Messages {
		switch msg.Role {
		case "user":
			m.lines = append(m.lines, line{kind: "user", text: msg.Content})
		case "assistant":
			if msg.Err != "" {
				m.lines = append(m.lines, line{kind: "err", text: msg.Err})
			}
			if msg.Content != "" {
				m.lines = append(m.lines, line{kind: "assistant", text: msg.Content})
			}
		}
	}
	m.histCount = len(m.lines)
}

/* applyFrame 把 SSE 帧并入时间线。 */
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
			m.echoUser = "" // 回放会重发本轮 loop_start
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
	case "model_chunk":
		var s string
		if json.Unmarshal(f.Data, &s) == nil && s != "" {
			m.appendAssistant(s)
			m.sawChunk = true
		}
	case "model_end":
		var d modelEndData
		if json.Unmarshal(f.Data, &d) == nil && d.Content != "" && !m.sawChunk {
			m.appendAssistant(d.Content)
		}
		m.sawChunk = false
	case "tool_start":
		var d toolStartData
		if json.Unmarshal(f.Data, &d) == nil {
			m.lines = append(m.lines, line{kind: "tool", text: d.Name})
		}
	case "turn_end":
		var d turnEndData
		_ = json.Unmarshal(f.Data, &d)
		m.streaming = false
		m.echoUser = ""
		if d.Err != "" {
			m.lines = append(m.lines, line{kind: "err", text: d.Err})
		}
	}
}

/* appendAssistant 流式追加到末尾的助手行（没有则新开）。 */
func (m *tuiModel) appendAssistant(delta string) {
	if n := len(m.lines); n > 0 && m.lines[n-1].kind == "assistant" {
		m.lines[n-1].text += delta
		return
	}
	m.lines = append(m.lines, line{kind: "assistant", text: delta})
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = msg.Width - 8
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyTab:
			m.listFocus = !m.listFocus
			if m.listFocus {
				m.input.Blur()
			} else {
				m.input.Focus()
			}
			return m, textinput.Blink
		}
		if m.listFocus {
			switch msg.Type {
			case tea.KeyUp, tea.KeyDown:
				if len(m.branches) == 0 {
					return m, nil
				}
				if msg.Type == tea.KeyUp {
					m.sel = (m.sel - 1 + len(m.branches)) % len(m.branches)
				} else {
					m.sel = (m.sel + 1) % len(m.branches)
				}
				if err := m.cl.activate(m.branches[m.sel].ID); err != nil {
					m.err = err.Error()
				}
				m.loadHistory()
				m.subscribe()
				return m, nil
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEnter:
			text := strings.TrimSpace(m.input.Value())
			if text == "" || m.streaming {
				return m, nil
			}
			if err := m.cl.send(m.branches[m.sel].ID, text); err != nil {
				m.lines = append(m.lines, line{kind: "err", text: "发送失败: " + err.Error()})
				return m, nil
			}
			m.lines = append(m.lines, line{kind: "user", text: text})
			m.echoUser = text
			m.streaming = true
			m.input.SetValue("")
			return m, nil
		}
	case frame:
		m.applyFrame(msg)
		return m, m.waitForFrame()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

/* waitForFrame 等下一帧 SSE（bubbletea 命令）。 */
func (m *tuiModel) waitForFrame() tea.Cmd {
	return func() tea.Msg {
		f, ok := <-m.frames
		if !ok {
			return nil
		}
		return f
	}
}

func (m *tuiModel) View() string {
	if m.width == 0 {
		return "启动中…"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf(" ezharness tui · v%s · %s", m.version, m.addr)))
	b.WriteString("\n")

	bodyH := m.height - 4 // 标题 1 + 输入区 3
	listW := 30

	// 分支列
	var list []string
	for i, br := range m.branches {
		mark := "  "
		if i == m.sel {
			mark = selStyle.Render("▸ ")
		}
		title := br.Title
		if title == "" {
			title = dimStyle.Render("(未命名)")
		}
		row := fmt.Sprintf("%s%-18s %3d", mark, truncate(title, 16), br.Msgs)
		if i == m.sel && m.listFocus {
			row = selStyle.Render(row)
		}
		list = append(list, row)
	}
	chatW := m.width - listW - 5
	visible := m.visibleLines(bodyH - 2)
	var chat []string
	for _, l := range visible {
		chat = append(chat, wrapLine(l, chatW))
	}

	listPane := panel(" 分支 ", list, listW, bodyH)
	chatPane := panel(" 会话 · "+truncate(m.branches[m.sel].Title, 20)+" ", chat, chatW+3, bodyH)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, listPane, chatPane))
	b.WriteString("\n")

	status := ""
	if m.streaming {
		status = selStyle.Render("● 运行中")
	} else if m.err != "" {
		status = errStyle.Render(truncate(m.err, m.width-30))
	}
	b.WriteString(fmt.Sprintf(" %s\n", status))
	if m.listFocus {
		b.WriteString(dimStyle.Render(" [分支焦点] ↑↓ 切换 · Tab 回输入 · Ctrl+C 退出"))
	} else {
		b.WriteString(" ❯ " + m.input.View())
	}
	b.WriteString("\n")
	return b.String()
}

/* visibleLines 取会话区最后 N 行（滚动跟随最新）。 */
func (m *tuiModel) visibleLines(n int) []line {
	if n <= 0 {
		return nil
	}
	if len(m.lines) <= n {
		return m.lines
	}
	return m.lines[len(m.lines)-n:]
}

/* panel 画一个带标题的边框面板。 */
func panel(title string, rows []string, w, h int) string {
	for len(rows) < h-2 {
		rows = append(rows, "")
	}
	if len(rows) > h-2 {
		rows = rows[len(rows)-h+2:]
	}
	inner := lipgloss.NewStyle().Width(w).Height(h - 2).Render(strings.Join(rows, "\n"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(w).
		Render(borderStyle.Render(title) + "\n" + inner)
}

/* wrapLine 按样式渲染一行并折行（超宽截断，保底可用）。 */
func wrapLine(l line, w int) string {
	var s string
	switch l.kind {
	case "user":
		s = userStyle.Render("你 ") + l.text
	case "assistant":
		s = aiStyle.Render("AI ") + l.text
	case "tool":
		s = toolStyle.Render("▸ " + l.text)
	case "err":
		s = errStyle.Render("✗ " + l.text)
	default:
		s = infoStyle.Render(l.text)
	}
	// 简单截断：折行交给终端换行，行数预算按 1 行计（内容过长时末尾省略）
	r := []rune(s)
	limit := w * 2
	if len(r) > limit {
		r = append(r[:limit], []rune("…")...)
	}
	return string(r)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
