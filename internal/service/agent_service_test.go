package service

import (
	"encoding/json"
	"testing"

	"ezharness/internal/domain"
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
		s := newApproveService([]domain.ToolRule{{Tool: "mcp.*", Level: lv}})
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
		{"ask 恒审批", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelAsk}, mcpCall("tool_call", "time", "getCurrentTime"), true},
		{"auto 恒免审", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelAuto}, mcpCall("tool_call", "time", "getCurrentTime"), false},
		{"white 名单精确命中", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelWhite, List: []string{"time.getCurrentTime"}}, mcpCall("tool_call", "time", "getCurrentTime"), false},
		{"white 名单未命中", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelWhite, List: []string{"time.getCurrentTime"}}, mcpCall("tool_call", "time", "convertTime"), true},
		{"white 整站放行", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelWhite, List: []string{"time"}}, mcpCall("tool_call", "time", "convertTime"), false},
		{"black 名单命中拦", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelBlack, List: []string{"time.convertTime"}}, mcpCall("tool_call", "time", "convertTime"), true},
		{"black 名单外放行", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelBlack, List: []string{"time.convertTime"}}, mcpCall("tool_call", "time", "getCurrentTime"), false},
		{"未知 action 按规则兜底", domain.ToolRule{Tool: "mcp.*", Level: domain.LevelAsk}, mcpCall("hack", "time", "x"), true},
		{"无规则默认审批", domain.ToolRule{Tool: "read_file"}, mcpCall("tool_call", "time", "getCurrentTime"), true},
	}
	for _, c := range cases {
		if got := newApproveService([]domain.ToolRule{c.rule}).needsApprove(c.call); got != c.want {
			t.Errorf("%s: got %v want %v", c.desc, got, c.want)
		}
	}
}

// 点边界：server 前缀放行整站，部分前缀不误命中。
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
		if got := matchRuleList(c.list, "mcp.*", args); got != c.want {
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
	got := buildServers(f)
	if len(got) != 2 || got[0].Name != "time" || got[1].Name != "default-on" {
		t.Fatalf("disabled server must be excluded, got %v", got)
	}
}
