package service

import (
	"strings"
	"testing"
)

func TestStripAnsi(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"\x1b[31mred\x1b[0m", "red"},           // SGR
		{"\x1b[2J\x1b[Hcleared", "cleared"},     // 清屏/光标
		{"\x1b]0;title\x07body", "body"},        // OSC
		{"\x1b]0;title\x1b\\body", "body"},      // OSC(ST)
		{"a\x07b", "ab"},                        // BEL
		{"line1\r\nline2", "line1\nline2"},      // CRLF
		{"v\x1b[Kersion", "version"},            // 行内清除
		{"a\n\n\n\nb", "a\n\nb"},                // 空行压缩
		{"\x1b[?25h\x1b[6n", ""},                // 纯控制序列
		{"中文输出正常", "中文输出正常"},                    // UTF-8
		{"\x1b[1;32mOK\x1b[0m done", "OK done"}, // 多参 SGR
		{strings.Repeat("x", 10) + "\x1b[999;999H" + strings.Repeat("y", 5), strings.Repeat("x", 10) + strings.Repeat("y", 5)}, // 光标定位
	}
	for _, c := range cases {
		if got := stripAnsi(c.in); got != c.want {
			t.Errorf("stripAnsi(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRingBuffer(t *testing.T) {
	r := newRing(8)
	r.append([]byte("12345"))
	if got := string(r.snapshot()); got != "12345" {
		t.Fatalf("snapshot = %q", got)
	}
	m := r.mark()
	r.append([]byte("678"))
	if got := string(r.since(m)); got != "678" {
		t.Fatalf("since = %q", got)
	}
	r.append([]byte("90abcdef")) // 覆盖最旧,保留流式最新 8 字节
	if got := string(r.snapshot()); got != "90abcdef" {
		t.Fatalf("overflow snapshot = %q", got)
	}
	m2 := r.mark()
	if m2 <= m {
		t.Fatalf("mark not advancing")
	}
	if got := string(r.since(m2)); got != "" {
		t.Fatalf("since(当前) = %q", got)
	}
}

func TestTailRunes(t *testing.T) {
	if got := tailRunes("abcdef", 3); got != "def" {
		t.Fatalf("tailRunes = %q", got)
	}
	if got := tailRunes("ab", 10); got != "ab" {
		t.Fatalf("tailRunes short = %q", got)
	}
	if got := tailRunes("中文字符测试", 2); got != "测试" {
		t.Fatalf("tailRunes runes = %q", got)
	}
}

func TestIsRawControl(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"\u0003", true},       // 纯 ^C
		{"\u0003\u0003", true}, // 多个控制符
		{"y\r", false},         // 含可打印内容
		{"echo hi", false},     // 普通命令
		{"", false},            // 空
		{"\ty", false},         // \t 不算控制输入
	}
	for _, c := range cases {
		if got := isRawControl(c.in); got != c.want {
			t.Errorf("isRawControl(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

/* 终端全局共享:Get 按 id 取,lastAi 空参兜底最近终端。 */
func TestGetLastAiFallback(t *testing.T) {
	s := NewTerminalService("")
	s.sessions["t1"] = &TermSession{ID: "t1", Name: "build"}
	s.lastAi = "t1"

	if _, err := s.Get("t1"); err != nil {
		t.Fatalf("按 id 访问应成功: %v", err)
	}
	if _, err := s.Get(""); err != nil {
		t.Fatalf("lastAi 兜底应命中: %v", err)
	}
	if _, err := s.Get("t9"); err == nil {
		t.Fatal("不存在的终端应报错")
	}
}

/* remove 清理全局 lastAi 引用。 */
func TestRemoveClearsLastAi(t *testing.T) {
	s := NewTerminalService("")
	sess := &TermSession{ID: "t1"}
	s.sessions["t1"] = sess
	s.lastAi = "t1"

	s.remove(sess)
	if _, ok := s.sessions["t1"]; ok {
		t.Fatal("t1 应被移除")
	}
	if s.lastAi != "" {
		t.Fatalf("lastAi 引用应清理, got %q", s.lastAi)
	}
}

/* 游标式读即消费:send/read 共用 readMark,已交付内容不重复。 */
func TestReadMarkConsume(t *testing.T) {
	sess := &TermSession{ring: newRing(64)}
	sess.ring.append([]byte("hello"))
	sess.readMark = sess.ring.mark() // term_start:读位点取创建时刻
	sess.ring.append([]byte(" world"))

	if got := string(sess.ring.since(sess.readMark)); got != " world" {
		t.Fatalf("首次读 = %q, want %q", got, " world")
	}
	sess.readMark = sess.ring.mark() // 读后推进

	sess.ring.append([]byte("!")) // 新输出
	if got := string(sess.ring.since(sess.readMark)); got != "!" {
		t.Fatalf("续读 = %q, want %q", got, "!")
	}
	sess.readMark = sess.ring.mark()
	if got := string(sess.ring.since(sess.readMark)); got != "" {
		t.Fatalf("无新输出时续读 = %q, want 空", got)
	}
}

func TestAggregate(t *testing.T) {
	s := &TerminalService{}
	cases := []struct {
		in   string
		want []string
	}{
		{"echo hi\r\n", []string{"echo hi"}},
		{"\x1b[I\x1b[Oecho hi\r", []string{"echo hi"}}, // 聚焦/失焦上报不污染
		{"\x1b[A\x1b[Aping\r", []string{"ping"}},       // 方向上历史
		{"\x1bOAls\r", []string{"ls"}},                 // SS3 方向键
		{"ab\x7f\x7fcd\r", []string{"cd"}},             // 退格
		{"stop\x03\r", []string{"^C"}},                 // ^C
		{"中文 ok\r", []string{"中文 ok"}},                 // UTF-8
		{"\x1b[Iec", []string{"ESC_CHUNK"}},            // 序列跨 chunk:先半截
		{"ho hi\r", []string{"echo hi"}},               // 续上块收尾
	}
	var sess TermSession
	for i, c := range cases {
		if c.want != nil && c.want[0] == "ESC_CHUNK" {
			s.aggregate(&sess, []byte(c.in))
			continue
		}
		got := s.aggregate(&sess, []byte(c.in))
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("case %d aggregate(%q) = %v, want %v", i, c.in, got, c.want)
		}
		s = &TerminalService{} // 每例独立会话
		sess = TermSession{}
	}
}
