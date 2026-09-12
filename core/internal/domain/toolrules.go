/*
toolRules.json（数据目录）：工具审批策略（安全模块，四档 + 名单）。
与 settings.json 分离：安全域自成一档，改策略不触碰行为设置；结构对齐
models.json/mcp.json（独立文件、缺失回落内置默认）。
*/
package domain

import (
	"context"
	"encoding/json"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* Level 是审批策略档位。 */
type Level string

const (
	LevelAsk   Level = "ask"   // 每次审批
	LevelBlack Level = "black" // 黑名单审批：名单外放行
	LevelWhite Level = "white" // 白名单免审：名单内放行
	LevelAuto  Level = "auto"  // 全部免审
)

/* ToolRule 是单个工具的审批策略；list 语义随档位（黑=命中才审，白=命中即免）。 */
type ToolRule struct {
	Tool  string   `json:"tool"`
	Level Level    `json:"level"`
	List  []string `json:"list"`
}

/* DefaultToolRules 内置默认（等价旧 needsApprove 硬编码语义）。 */
func DefaultToolRules() []ToolRule {
	return []ToolRule{
		{Tool: "read_file", Level: LevelAuto},
		{Tool: "write_file", Level: LevelAsk},
		{Tool: "edit_file", Level: LevelAsk},
		{Tool: "terminal", Level: LevelWhite, List: []string{
			"ls", "cat", "head", "tail", "pwd", // POSIX 只读
			"dir", "type", "cd", "ver", // cmd 只读（Windows 原生 shell）
			"git status", "git diff", "git log", "go test",
		}},
		/* 共享终端（魔法看板，按分支绑定）：send 与 terminal 白名单同集；
		list/read 只读免审；close 是资源清理（杀本分支终端进程）免审 */
		{Tool: "term_send", Level: LevelWhite, List: []string{
			"ls", "cat", "head", "tail", "pwd",
			"dir", "type", "cd", "ver",
			"git status", "git diff", "git log", "go test",
		}},
		{Tool: "term_start", Level: LevelAsk},
		{Tool: "term_list", Level: LevelAuto},
		{Tool: "term_read", Level: LevelAuto},
		{Tool: "term_close", Level: LevelAuto},
		/* 共享浏览器（魔法看板）：开标签与导航默认审批（navigate 白名单可配
		URL/域名前缀放行常去站点）；页面内操作/读取/截图/管理免审 */
		{Tool: "browser_start", Level: LevelAsk},
		{Tool: "browser_navigate", Level: LevelAsk},
		{Tool: "browser_click", Level: LevelAuto},
		{Tool: "browser_type", Level: LevelAuto},
		{Tool: "browser_key", Level: LevelAuto},
		{Tool: "browser_scroll", Level: LevelAuto},
		{Tool: "browser_read", Level: LevelAuto},
		{Tool: "browser_screenshot", Level: LevelAuto},
		{Tool: "browser_list", Level: LevelAuto},
		{Tool: "browser_close", Level: LevelAuto},
		{Tool: "task", Level: LevelAsk},
		{Tool: "save_app", Level: LevelAsk},
		{Tool: "image_recognize", Level: LevelAuto}, // 图片识别（识别槽模型驱动，只读）
		{Tool: "mcp.*", Level: LevelAsk},
	}
}

/* LoadToolRules 读 toolRules.json；缺失或空档回落内置默认。 */
func LoadToolRules(fsys fs.FileSystem) []ToolRule {
	data, err := fsys.Read(context.Background(), "toolRules.json")
	if err != nil {
		return DefaultToolRules()
	}
	var rules []ToolRule
	if json.Unmarshal(data, &rules) != nil || len(rules) == 0 {
		return DefaultToolRules()
	}
	return rules
}

/* SaveToolRules 落盘审批策略。 */
func SaveToolRules(fsys fs.FileSystem, rules []ToolRule) error {
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "toolRules.json", data)
}
