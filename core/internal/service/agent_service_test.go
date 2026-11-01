package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ezharness/core/internal/domain"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/offload"
	"github.com/xuanlv2002/ezloop/types"
)

func newApproveService(rules []domain.ToolRule) *AgentService {
	return &AgentService{Hub: &domain.Hub{ToolRules: rules}}
}

func mcpCall(action, server, tool string) *types.ToolCall {
	args, _ := json.Marshal(map[string]string{"action": action, "server": server, "tool": tool})
	return &types.ToolCall{ID: "c1", Name: "mcp_router", Args: args}
}

// 发现类（mcp_list/tool_list）只读无副作用：四档下一律免审。
func TestNeedsApproveMcpDiscovery(t *testing.T) {
	for _, lv := range []domain.Level{domain.LevelAsk, domain.LevelWhite, domain.LevelBlack, domain.LevelAuto} {
		s := newApproveService([]domain.ToolRule{{Key: "mcp.*", Level: lv}})
		for _, action := range []string{"mcp_list", "tool_list"} {
			if s.needsApprove(mcpCall(action, "", "")) {
				t.Errorf("%s under level %v must not need approval", action, lv)
			}
		}
	}
}

// tool_call 按四档判定：白名单命中放行、整站前缀放行、黑名单命中拦、其余按档默认。
func TestNeedsApproveMcpToolCall(t *testing.T) {
	cases := []struct {
		desc string
		rule domain.ToolRule
		call *types.ToolCall
		want bool
	}{
		{"ask 恒审批", domain.ToolRule{Key: "mcp.*", Level: domain.LevelAsk}, mcpCall("tool_call", "time", "getCurrentTime"), true},
		{"auto 恒免审", domain.ToolRule{Key: "mcp.*", Level: domain.LevelAuto}, mcpCall("tool_call", "time", "getCurrentTime"), false},
		{"white 名单精确命中", domain.ToolRule{Key: "mcp.*", Level: domain.LevelWhite, List: []string{"time.getCurrentTime"}}, mcpCall("tool_call", "time", "getCurrentTime"), false},
		{"white 名单未命中", domain.ToolRule{Key: "mcp.*", Level: domain.LevelWhite, List: []string{"time.getCurrentTime"}}, mcpCall("tool_call", "time", "convertTime"), true},
		{"white 整站放行", domain.ToolRule{Key: "mcp.*", Level: domain.LevelWhite, List: []string{"time"}}, mcpCall("tool_call", "time", "convertTime"), false},
		{"black 名单命中拦", domain.ToolRule{Key: "mcp.*", Level: domain.LevelBlack, List: []string{"time.convertTime"}}, mcpCall("tool_call", "time", "convertTime"), true},
		{"black 名单外放行", domain.ToolRule{Key: "mcp.*", Level: domain.LevelBlack, List: []string{"time.convertTime"}}, mcpCall("tool_call", "time", "getCurrentTime"), false},
		{"未知 action 按规则兜底", domain.ToolRule{Key: "mcp.*", Level: domain.LevelAsk}, mcpCall("hack", "time", "x"), true},
		{"无规则默认审批", domain.ToolRule{Key: "read_file"}, mcpCall("tool_call", "time", "getCurrentTime"), true},
	}
	for _, c := range cases {
		if got := newApproveService([]domain.ToolRule{c.rule}).needsApprove(c.call); got != c.want {
			t.Errorf("%s: got %v want %v", c.desc, got, c.want)
		}
	}
}

/* callback 按 action 逐项查表：list 免审，其余动作各自档位生效。 */
func TestNeedsApproveCallbackActions(t *testing.T) {
	rules := []domain.ToolRule{
		{Key: "callback:list", Level: domain.LevelAuto},
		{Key: "callback:create", Level: domain.LevelAsk},
		{Key: "callback:run", Level: domain.LevelAuto},
	}
	call := func(action string) *types.ToolCall {
		args, _ := json.Marshal(map[string]string{"action": action})
		return &types.ToolCall{ID: "c1", Name: "callback", Args: args}
	}
	s := newApproveService(rules)
	for action, want := range map[string]bool{
		"list": false, "create": true, "run": false,
		"update": true, // 档内没配 → 回落内置默认（callback:update = ask）
	} {
		if got := s.needsApprove(call(action)); got != want {
			t.Errorf("callback:%s = %v want %v", action, got, want)
		}
	}
}

// 白名单 mcp：精确 server.tool 或整站前缀（点边界），部分前缀不误命中。
func TestMatchRuleListDotBoundary(t *testing.T) {
	args := json.RawMessage(`{"action":"tool_call","server":"time","tool":"getCurrentTime"}`)
	cases := []struct {
		list []string
		want bool
		desc string
	}{
		{[]string{"time"}, true, "server 前缀命中"},
		{[]string{"time.getCurrentTime"}, true, "精确命中"},
		{[]string{"time.getC"}, false, "部分前缀不命中"},
		{[]string{"tim"}, false, "server 部分前缀不命中"},
		{[]string{"web-search"}, false, "其他 server 不命中"},
	}
	for _, c := range cases {
		if got := matchRuleList(c.list, domain.LevelWhite, "mcp.*", args); got != c.want {
			t.Errorf("%s: got %v want %v", c.desc, got, c.want)
		}
	}
}

// 黑名单 mcp：包含即命中（宁误拦不漏拦）。
func TestMatchRuleListMcpBlackContains(t *testing.T) {
	args := json.RawMessage(`{"action":"tool_call","server":"time","tool":"getCurrentTime"}`)
	cases := []struct {
		list []string
		want bool
		desc string
	}{
		{[]string{"time.getCurrentTime"}, true, "精确包含命中"},
		{[]string{"getCurrentTime"}, true, "工具名子串命中"},
		{[]string{"web-search"}, false, "无关词不命中"},
	}
	for _, c := range cases {
		if got := matchRuleList(c.list, domain.LevelBlack, "mcp.*", args); got != c.want {
			t.Errorf("%s: got %v want %v", c.desc, got, c.want)
		}
	}
}

// 白名单终端命令：前缀匹配（词边界=结尾或空白，拼接/中段出现放不过）。
func TestMatchRuleListCommandWhitePrefix(t *testing.T) {
	mk := func(cmd string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return b
	}
	cases := []struct {
		list []string
		cmd  string
		want bool
		desc string
	}{
		{[]string{"rm"}, "rm -rf /", true, "命令以名单词开头命中"},
		{[]string{"rm"}, "rm", true, "整词等于名单词命中"},
		{[]string{"git status"}, "git status --short", true, "多词名单前缀命中"},
		{[]string{"rm"}, "rm;curl x", false, "分号拼接不越界"},
		{[]string{"rm"}, "echo rm", false, "命令中段出现不命中"},
		{[]string{"rm"}, `del "rm"`, false, "引号包裹不命中"},
		{[]string{"rm"}, "rmdir x", false, "词干延长不命中"},
		{[]string{"format"}, "format.com", false, "拼接后缀不命中"},
		{[]string{"git status"}, "cd /d && git status", false, "组合命令后段不命中"},
		{[]string{"cd"}, "cd /tmp && curl x", false, "空格后 && 拼接不越界"},
		{[]string{"cat"}, "cat a | curl x", false, "管道不越界"},
		{[]string{"ls"}, "ls $(curl x)", false, "子命令不越界"},
		{[]string{"dir"}, "dir\r\ntype secret", false, "换行分隔不越界"},
		{[]string{"cat"}, "cat a > /dev/tcp/x", false, "重定向不越界"},
		{[]string{"cat"}, "cat a.txt", true, "无元字符正常命中"},
	}
	for _, c := range cases {
		if got := matchRuleList(c.list, domain.LevelWhite, "term_action:send", mk(c.cmd)); got != c.want {
			t.Errorf("%s: got %v want %v", c.desc, got, c.want)
		}
	}
}

// 黑名单终端命令：包含即命中（宁误拦不漏拦）。
func TestMatchRuleListCommandBlackContains(t *testing.T) {
	mk := func(cmd string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return b
	}
	cases := []struct {
		list []string
		cmd  string
		want bool
		desc string
	}{
		{[]string{"rm"}, "rm -rf /", true, "前缀命令命中"},
		{[]string{"rm"}, "echo rm", true, "词中包含命中"},
		{[]string{"rm"}, `del "rm"`, true, "引号包裹命中"},
		{[]string{"format"}, "format.com", true, "拼接后缀命中"},
		{[]string{"rm"}, "rmdir x", true, "词干包含也命中（contain 语义）"},
		{[]string{"git status"}, "cd /d && git status", true, "组合命令命中"},
		{[]string{"git status"}, "git log", false, "无名单词不命中"},
	}
	for _, c := range cases {
		if got := matchRuleList(c.list, domain.LevelBlack, "term_action:send", mk(c.cmd)); got != c.want {
			t.Errorf("%s: got %v want %v", c.desc, got, c.want)
		}
	}
}

// 启停链路：enabled=false 的 server 不进 Router 名单（mcp_list 查不到、无法调用）。
func TestBuildServersEnabledFilter(t *testing.T) {
	off := false
	on := true
	f := &McpFile{Servers: []McpServerFile{
		{Name: "time", Type: "http", URL: "https://x", Enabled: &on},
		{Name: "web-search", Type: "http", URL: "https://y", Enabled: &off},
		{Name: "default-on", Type: "http", URL: "https://z"}, // Enabled 缺省视为启用
	}}
	got := buildServers(context.Background(), f, nil)
	if len(got) != 2 || got[0].Name != "time" || got[1].Name != "default-on" {
		t.Fatalf("disabled server must be excluded, got %v", got)
	}
}

/*
卸载摘要头随阈值分档但封顶：默认 4096 → 512，大阈值最多 1KB，极小
阈值不超过一半。不能与阈值等长，否则多个大结果很快又把上下文灌满。
*/
func TestOffloadHeadFollowsThreshold(t *testing.T) {
	cases := []struct{ threshold, want int }{
		{0, 512},
		{-1, 512},
		{100, 50},
		{1024, 256},
		{4096, 512},
		{8192, 1024},
		{1_000_000, 1024},
	}
	for _, c := range cases {
		if got := offloadHead(c.threshold); got != c.want {
			t.Errorf("offloadHead(%d) = %d, want %d", c.threshold, got, c.want)
		}
	}
}

// 集成校验：卸载后消息里保留的摘要头长度按分档规则生效。
func TestOffloadHeadApplied(t *testing.T) {
	const threshold = 1000
	hook := offload.New(fs.NewLocal(t.TempDir()), offload.WithThreshold(threshold),
		func(o *offload.Options) { o.Head = offloadHead(threshold) })
	res := &types.ToolResult{Name: "dump", Content: strings.Repeat("x", 5000)}
	if err := hook.OnToolEnd(context.Background(), &types.LoopState{}, res); err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("x", 256) + "\n\n[输出共 5000 字节"
	if !strings.HasPrefix(res.Content, want) {
		t.Fatalf("offload head = %q..., want prefix %q", res.Content[:min(len(res.Content), 140)], want)
	}
}
