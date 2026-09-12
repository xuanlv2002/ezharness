/*
魔法看板·共享终端:多终端全局公共池(跨会话共享,个人助手语义:任何
会话都能操作全部终端)。每个终端是一个 ConPTY + shell 进程,用户
(WS 输入)与 AI(term_* 工具)共写同一终端。输出进环形缓冲(WS 重连
hello 恢复),AI 读走游标式增量(term_send/term_read 共用读位点,读即
消费)。用户手敲的命令行聚合记录供 agent_status(每会话的 ResSnapshot
基线独立对比——A 会话首轮见到 B 会话开的终端同样报"新增",模型各
自知悉全局终端水位)。AI 写入不记(避免自反馈)。
*/
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

/* TermInfo 是终端清单条目(WS/REST/AI term_list 共用)。 */
type TermInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Origin  string `json:"origin"`  // 创建来源:"用户" / "AI·<会话名>"
	Exited  bool   `json:"exited"`  // shell 已退出(连接保留可看残留输出)
	LastCmd string `json:"lastCmd"` // 最近一次写入的命令(AI 或用户)
}

/* UserLine 是一条用户手动输入记录(agent_status 用)。 */
type UserLine struct {
	ID   string `json:"id"`
	Line string `json:"line"`
}

/* TermFrame 是 service → WS 订阅者的广播帧(controller 负责编码)。 */
type TermFrame struct {
	Type     string     `json:"type"` // data | terminals
	ID       string     `json:"id,omitempty"`
	Data     []byte     `json:"-"` // data 帧原始 PTY 字节
	Sessions []TermInfo `json:"sessions,omitempty"`
}

const (
	termRingSize   = 256 * 1024 // 每终端输出环形缓冲
	termReadBuf    = 4096
	termSubsBuf    = 64  // 每订阅者帧队列,满丢帧(重连 hello 兜底)
	termUserQueue  = 50  // 每分支用户命令行 pending 上限
	termLineMax    = 256 // 行聚合缓冲上限
	termCollectMax = 20  // 每轮收割条数上限
)

/* ── 环形缓冲 ── */

type ringBuffer struct {
	buf     []byte
	head    int // 起始下标
	size    int
	written int64 // 累计写入(代标记用)
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

/* mark 返回当前写入位置(配合 since 取增量)。 */
func (r *ringBuffer) mark() int64 { return r.written }

/* since 返回自 mark 之后新增的数据(被覆盖则返回整个缓冲)。 */
func (r *ringBuffer) since(mark int64) []byte {
	n := r.written - mark
	if n <= 0 {
		return nil
	}
	if n > int64(r.size) {
		n = int64(r.size)
	}
	out := make([]byte, 0, n)
	start := (r.head + r.size - int(n)) % len(r.buf)
	for n > 0 {
		tail := (start + int(n)) % len(r.buf)
		end := tail
		if end <= start {
			end = len(r.buf)
		}
		out = append(out, r.buf[start:end]...)
		n -= int64(end - start)
		start = 0
	}
	return out
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
	ID      string
	Name    string
	Origin  string
	LastCmd string

	mu       sync.Mutex
	pty      pty.Pty
	cmd      *pty.Cmd
	ring     *ringBuffer
	lastOut  time.Time
	exited   bool
	readMark int64 // agent 读位点(term_send/term_read 共用,读即消费)

	/* 用户输入聚合(agent_status):按回车切行,滤控制字符 */
	lineAgg  []byte
	escState int // 输入转义序列过滤状态(0 正常 1 ESC后 2 CSI中 3 OSC中)

	condCh chan struct{} // 静默等待(写泵 close 广播)
}

/* ── 服务 ── */

/* TerminalService 管理全部终端(全局单例,随换代重建)。 */
type TerminalService struct {
	mu        sync.Mutex
	seq       int
	sessions  map[string]*TermSession
	subs      map[chan TermFrame]struct{}
	lastAi    string // AI 最近使用/创建的终端 id(无 id 参数时兜底)
	userQueue []UserLine
	workDir   string
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
	return "/bin/sh"
}

/* Create 启动一个新终端(id 形如 t1/t2)。 */
func (s *TerminalService) Create(name, origin string) (*TermInfo, error) {
	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("pty: %w", err)
	}
	if err := p.Resize(120, 30); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("pty resize: %w", err)
	}
	cmd := p.Command(shellPath())
	cmd.Dir = s.workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("t%d", s.seq)
	if name == "" {
		name = id
	}
	sess := &TermSession{
		ID: id, Name: name, Origin: origin,
		pty: p, cmd: cmd, ring: newRing(termRingSize),
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
	return &TermInfo{ID: sess.ID, Name: sess.Name, Origin: sess.Origin,
		Exited: sess.exited, LastCmd: sess.LastCmd}
}

/* readPump 持续读 PTY 输出:入 ring、刷新静默时钟、广播给 WS 订阅者。 */
func (sess *TermSession) readPump(s *TerminalService) {
	buf := make([]byte, termReadBuf)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			sess.mu.Lock()
			sess.ring.append(data)
			sess.lastOut = time.Now()
			close(sess.condCh)
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
	ch := sess.condCh
	close(ch)
	sess.condCh = make(chan struct{})
	sess.mu.Unlock()
	s.broadcast(TermFrame{Type: "terminals", Sessions: s.List()})
}

func (s *TerminalService) get(id string) (*TermSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

/* Get 返回终端;id 为空时取 AI 最近使用的终端(全局兜底)。 */
func (s *TerminalService) Get(id string) (*TermSession, error) {
	s.mu.Lock()
	if id == "" {
		id = s.lastAi
	}
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("终端 %q 不存在(可用 term_list 查看)", id)
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
	ListTermsJSON 终端清单的 JSON 文本(term_list 工具直接返回;

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
	StartTerm 新建终端(desc 为描述/名称);带 command 时立即运行

并等输出静默返回(等价"新建+send"一步到位)。读位点取注入前的当前
位置(欢迎横幅不计入 agent 可读增量)。
*/
func (s *TerminalService) StartTerm(desc, command string, quietMs, timeoutMs int) (string, error) {
	quiet := clampInt(quietMs, 100, 5000, 800)
	timeout := clampInt(timeoutMs, 1000, 60000, 30000)

	info, err := s.Create(desc, "AI")
	if err != nil {
		return "", err
	}
	sess, ok := s.get(info.ID)
	if !ok {
		return "", fmt.Errorf("终端 %q 创建后即失效", info.ID)
	}
	sess.mu.Lock()
	sess.readMark = sess.ring.mark()
	sess.mu.Unlock()
	if command == "" {
		return fmt.Sprintf("[终端 #%s %q 已创建(分支绑定,用户可在看板查看接管)]\n后续用 term_send(termId=%s) 发送命令", info.ID, info.Name, info.ID), nil
	}

	payload := command
	if !isRawControl(command) {
		payload += "\r"
	}
	sess.mu.Lock()
	sess.lastOut = time.Now()
	gen := sess.ring.mark()
	sess.mu.Unlock()
	s.writeAI(sess, command, []byte(payload))

	out, exited, timedOut := waitQuiet(context.Background(), sess, gen, quiet, timeout)
	sess.mu.Lock()
	sess.readMark = sess.ring.mark()
	name := sess.Name
	sess.mu.Unlock()
	head := fmt.Sprintf("[终端 #%s %q 已创建并执行]", info.ID, name)
	if exited {
		head += " shell 已退出"
	}
	note := "[输出已静默]"
	if timedOut {
		note = fmt.Sprintf("[等待超时(>%ds),命令可能仍在运行,可用 term_read(termId=%s) 续读]", timeout/1000, info.ID)
	}
	return renderTermOutput(head, out, note), nil
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
	writeAI 是 AI 侧写入:记录 lastCmd 与最近终端,不做用户输入聚合

(避免 agent_status 自反馈)。
*/
func (s *TerminalService) writeAI(sess *TermSession, lastCmd string, b []byte) {
	s.mu.Lock()
	s.lastAi = sess.ID
	s.mu.Unlock()
	sess.mu.Lock()
	if lastCmd != "" {
		sess.LastCmd = lastCmd
	}
	sess.pty.Write(b) //nolint:errcheck
	sess.mu.Unlock()
}

/*
	Send 在终端执行命令并等待输出静默,返回本次新增输出。收集起点取

读位点与写前位置的较早者(此前未读的增量一并交付,不丢输出),返回后
推进读位点(send 与 read 共用,读即消费)。
*/
func (s *TerminalService) Send(ctx context.Context, id, cmd string, quietMs, timeoutMs int) (string, error) {
	quiet := clampInt(quietMs, 100, 5000, 800)
	timeout := clampInt(timeoutMs, 1000, 60000, 30000)

	sess, err := s.Get(id)
	if err != nil {
		return "", err
	}
	payload := cmd
	if !isRawControl(cmd) {
		payload += "\r"
	}

	sess.mu.Lock()
	sess.lastOut = time.Now() // 静默计时从写入后起算(命令回显/结果未出时不误判)
	gen := sess.ring.mark()
	if sess.readMark < gen { // 旧未读增量一并带回
		gen = sess.readMark
	}
	sess.mu.Unlock()
	s.writeAI(sess, cmd, []byte(payload))

	out, exited, timedOut := waitQuiet(ctx, sess, gen, quiet, timeout)
	sess.mu.Lock()
	sess.readMark = sess.ring.mark() // 游标推进:已交付内容不再重复
	name := sess.Name
	sess.mu.Unlock()

	head := fmt.Sprintf("[终端 #%s %q]", sess.ID, name)
	if exited {
		head += " shell 已退出"
	}
	note := "[输出已静默]"
	if timedOut {
		note = fmt.Sprintf("[等待超时(>%ds),命令可能仍在运行,可用 term_read 续读]", timeout/1000)
	}
	return renderTermOutput(head, out, note), nil
}

/* waitQuiet 等待输出静默/退出/超时,返回期间新增输出。 */
func waitQuiet(ctx context.Context, sess *TermSession, gen int64, quiet, timeout int) (out []byte, exited, timedOut bool) {
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	for {
		sess.mu.Lock()
		ch := sess.condCh
		out = sess.ring.since(gen)
		quieted := time.Since(sess.lastOut) >= time.Duration(quiet)*time.Millisecond
		exited = sess.exited
		sess.mu.Unlock()
		if exited || quieted {
			return out, exited, false
		}
		if time.Now().After(deadline) {
			return out, exited, true
		}
		select {
		case <-ctx.Done():
			return out, exited, false
		case <-ch:
		case <-time.After(50 * time.Millisecond):
		}
	}
}

/* renderTermOutput 组装工具返回:头行 + 净化输出 + 状态行(截尾 8000 rune)。 */
func renderTermOutput(head string, out []byte, note string) string {
	text := string(bytes.ToValidUTF8(out, nil))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = stripAnsi(text)
	if r := []rune(text); len(r) > 8000 {
		text = "…(输出过长,已截断)\n" + string(r[len(r)-8000:])
	}
	var b strings.Builder
	b.WriteString(head + "\n")
	if body := strings.TrimSpace(text); body != "" {
		b.WriteString(body + "\n")
	}
	b.WriteString(note)
	return b.String()
}

/* ReadTerm 游标式读取终端新输出(读即消费,下次只返回新增)。 */
func (s *TerminalService) ReadTerm(id string, chars int) (string, error) {
	sess, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if chars <= 0 {
		chars = 4000
	}
	if chars > 20000 {
		chars = 20000
	}
	sess.mu.Lock()
	out := sess.ring.since(sess.readMark)
	sess.readMark = sess.ring.mark()
	exited := sess.exited
	name := sess.Name
	sess.mu.Unlock()

	head := fmt.Sprintf("[终端 #%s %q]", sess.ID, name)
	if exited {
		head += " shell 已退出"
	}
	text := string(bytes.ToValidUTF8(out, nil))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	body := tailRunes(stripAnsi(text), chars)
	if body == "" {
		body = "(无新输出)"
	}
	return head + "\n" + body, nil
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

/* CloseTerm 关闭终端(杀进程树防子进程残留);幂等。 */
func (s *TerminalService) CloseTerm(id string) error {
	sess, err := s.Get(id)
	if err != nil {
		return err
	}
	s.remove(sess)
	return nil
}

/* remove 杀进程树并摘除会话,清理 lastAi 引用,广播清单。 */
func (s *TerminalService) remove(sess *TermSession) {
	killTree(sess.cmd)
	s.mu.Lock()
	delete(s.sessions, sess.ID)
	if s.lastAi == sess.ID {
		s.lastAi = ""
	}
	s.mu.Unlock()
	s.broadcast(TermFrame{Type: "terminals", Sessions: s.List()})
}

/*
	UserInput 是用户手敲输入(WS 路径):写入并聚合命令行供 agent_status。

按 id 精确查找(WS 帧必须带 id,不走 AI 最近终端兜底)。
*/
func (s *TerminalService) UserInput(id string, b []byte) error {
	sess, ok := s.get(id)
	if !ok {
		return fmt.Errorf("终端 %q 不存在", id)
	}
	sess.mu.Lock()
	sess.pty.Write(b) //nolint:errcheck
	lines := s.aggregate(sess, b)
	sess.mu.Unlock()
	for _, line := range lines {
		s.enqueueUserLine(sess.ID, line)
	}
	return nil
}

/*
	aggregate 把输入字节聚合成命令行:可打印字符累积,回车切行,

控制字符忽略(\x03 记 ^C,退格弹末字符),单行截 80 字。
ESC 起始的转义序列整体跳过(方向键/聚焦上报等,否则 ESC 后的
"[A""[I" 等可见字符会污染记录)。调用方需持 sess.mu。
*/
func (s *TerminalService) aggregate(sess *TermSession, b []byte) []string {
	var lines []string
	for _, c := range string(b) {
		switch sess.escState {
		case 1: // ESC 后:[ CSI / ] OSC / O SS3 / 其他单字符转义
			switch c {
			case '[':
				sess.escState = 2
			case ']':
				sess.escState = 3
			case 'O', 'P', 'N': // SS3 等:再吃一个 final 字符
				sess.escState = 4
			default:
				sess.escState = 0
			}
			continue
		case 4: // SS3 final:跳过本字符结束
			sess.escState = 0
			continue
		case 2: // CSI 中:吃到 final byte(0x40-0x7E)结束
			if c >= 0x40 && c <= 0x7e {
				sess.escState = 0
			}
			continue
		case 3: // OSC 中:BEL 或 ST 结束
			if c == 0x07 || c == 0x1b {
				sess.escState = 0
			}
			continue
		}
		switch {
		case c == 0x1b:
			sess.escState = 1
		case c == '\r' || c == '\n':
			line := strings.TrimSpace(string(sess.lineAgg))
			sess.lineAgg = sess.lineAgg[:0]
			if line != "" {
				if r := []rune(line); len(r) > 80 {
					line = string(r[:80]) + "…"
				}
				lines = append(lines, line)
			}
		case c == 0x03:
			sess.lineAgg = append(sess.lineAgg[:0], "^C"...)
		case c == 0x7f || c == 0x08:
			if n := len(sess.lineAgg); n > 0 {
				sess.lineAgg = sess.lineAgg[:n-1]
			}
		case c >= 0x20:
			if len(sess.lineAgg) < termLineMax {
				sess.lineAgg = append(sess.lineAgg, string(c)...)
			}
		}
	}
	return lines
}

/* CollectUserActivity 收割用户命令行(每轮 agent_status 调用,取走即清)。 */
func (s *TerminalService) CollectUserActivity() []UserLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.userQueue
	s.userQueue = nil
	if len(out) > termCollectMax {
		out = out[:termCollectMax]
	}
	return out
}

/*
	enqueueUserLine 用户命令行入全局队列(带终端 id,

status 渲染 "用户在终端 tN 执行:xxx")。
*/
func (s *TerminalService) enqueueUserLine(id string, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.userQueue) >= termUserQueue {
		s.userQueue = s.userQueue[1:]
	}
	s.userQueue = append(s.userQueue, UserLine{ID: id, Line: line})
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
	sess.mu.Lock()
	sess.pty.Resize(cols, rows) //nolint:errcheck
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

func clampInt(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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

func killTree(cmd *pty.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
	} else {
		_ = cmd.Process.Kill()
	}
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
	}
}

/* ── ANSI 剥离(term_send/term_read 输出净化) ── */

var (
	csiRe  = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)       // CSI 序列
	oscRe  = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)`) // OSC 序列
	escRe  = regexp.MustCompile(`\x1b[@-Z\\-_]`)                   // 单字符转义
	ctrlRe = regexp.MustCompile(`[\x00-\x08\x0b-\x1f\x7f]`)        // 其余 C0(保留 \n\t)
)

/* stripAnsi 剥终端转义序列与控制字符(保留 \n \t),压缩连续空行。 */
func stripAnsi(s string) string {
	s = oscRe.ReplaceAllString(s, "")
	s = csiRe.ReplaceAllString(s, "")
	s = escRe.ReplaceAllString(s, "")
	s = ctrlRe.ReplaceAllString(s, "")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}
