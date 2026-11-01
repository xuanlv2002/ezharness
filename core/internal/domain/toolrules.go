/* 工具审批策略的结构与默认值（加载与落盘在 service 层）。 */
package domain

/* Level 是审批策略档位。 */
type Level string

const (
	LevelAsk   Level = "ask"   // 每次审批
	LevelBlack Level = "black" // 黑名单审批：名单外放行
	LevelWhite Level = "white" // 白名单免审：名单内放行
	LevelAuto  Level = "auto"  // 全部免审
)

/*
ToolRule 是一条审批策略。Key 是工具名，或"工具名:action"（合并式工具
term_action / browser_action 逐 action 配档，如 term_action:send、
browser_action:eval）。List 匹配语义随档位与名单类型：
白名单=前缀命中即免审（命令词/路径/server 前缀），黑名单=包含命中才拦审。
*/
type ToolRule struct {
	Key   string   `json:"key"`
	Level Level    `json:"level"`
	List  []string `json:"list"`
}

/* 内置默认白名单：终端命令（terminal 与 term_action:send 共用）。 */
var defaultCommandWhite = []string{
	"ls", "cat", "head", "tail", "pwd", // POSIX 只读
	"dir", "type", "cd", "ver", // cmd 只读（Windows 原生 shell）
	"git status", "git diff", "git log", "go test",
}

/*
	DefaultToolRules 内置默认。

只有终端的可执行动作（terminal、term_action:send）与 mcp 有黑白名单；
write_file/edit_file 有路径白名单；其余工具两档（审批/免审）。
*/
func DefaultToolRules() []ToolRule {
	return []ToolRule{
		{Key: "read_file", Level: LevelAuto},
		{Key: "write_file", Level: LevelAsk},
		{Key: "edit_file", Level: LevelAsk},
		{Key: "terminal", Level: LevelWhite, List: defaultCommandWhite},
		/* 共享终端（魔法看板）：生命周期单一档；send 与 terminal 白名单同集，
		read 只读免审 */
		{Key: "term_tab", Level: LevelAsk},
		{Key: "term_action:send", Level: LevelWhite, List: defaultCommandWhite},
		{Key: "term_action:read", Level: LevelAuto},
		/* 共享浏览器：标签开关一档；观察类动作免审，改变页面/外发数据的动作
		（导航、脚本、上传）默认审批。每一项都必须在这里列全——漏掉的 action 在
		needsApprove 里查不到规则会一律要求审批，安全页也不会出现该行。 */
		{Key: "browser_tab", Level: LevelAsk},
		{Key: "browser_action:navigate", Level: LevelAsk},
		{Key: "browser_action:read", Level: LevelAuto},
		{Key: "browser_action:eval", Level: LevelAsk},
		{Key: "browser_action:console", Level: LevelAuto},
		{Key: "browser_action:network", Level: LevelAuto},
		{Key: "browser_action:screenshot", Level: LevelAuto},
		{Key: "browser_action:upload", Level: LevelAsk},
		/* 系统自带：两档，默认免审（save_app 会覆盖应用文件，默认审批） */
		{Key: "task", Level: LevelAuto},
		{Key: "save_app", Level: LevelAsk},
		{Key: "load_app", Level: LevelAuto}, // 只读应用文档
		/* 定时回调逐 action：list 只读免审，其余动作默认审批。新增 action
		必须同时列在这里，否则安全页不会出现该行。 */
		{Key: "callback:list", Level: LevelAuto},
		{Key: "callback:create", Level: LevelAsk},
		{Key: "callback:update", Level: LevelAsk},
		{Key: "callback:delete", Level: LevelAsk},
		{Key: "callback:enable", Level: LevelAsk},
		{Key: "callback:disable", Level: LevelAsk},
		{Key: "callback:run", Level: LevelAsk},
		{Key: "sleep", Level: LevelAuto},
		{Key: "memory", Level: LevelAuto},      // 提案本身即待审（用户在记忆页审阅），双重审批无意义
		{Key: "plan:new", Level: LevelAuto},    // 新方案走计划确认卡（可编辑/反馈），不再叠一层审批
		{Key: "plan:update", Level: LevelAuto}, // 进度同步免审
		{Key: "plan:clear", Level: LevelAuto},  // 清理免审
		{Key: "ask_user", Level: LevelAuto},
		{Key: "trim_context", Level: LevelAuto},
		{Key: "load_skill", Level: LevelAuto},
		{Key: "model_call", Level: LevelAuto}, // 调模型只读无副作用：默认免审
		{Key: "mcp.*", Level: LevelAsk},
	}
}

/*
ListKindOf 返回该 key 的名单类型：""=无名单能力（只有 审批/免审 两档）；
"path"=文件路径前缀白名单；"command"=终端命令词；"mcpTool"=server 或
server.tool。前端据此决定展示哪些档位与名单编辑框。
*/
func ListKindOf(key string) string {
	switch key {
	case "write_file", "edit_file":
		return "path"
	case "terminal", "term_action:send":
		return "command"
	case "mcp.*":
		return "mcpTool"
	}
	return ""
}

/*
ValidLevel 校验某 key 的档位是否合法：无名单能力只有 审批/免审；
路径白名单只有 白名单（无黑名单）；命令/mcp 四档齐全。
*/
func ValidLevel(key string, lv Level) bool {
	switch ListKindOf(key) {
	case "":
		return lv == LevelAsk || lv == LevelAuto
	case "path":
		return lv == LevelAsk || lv == LevelWhite || lv == LevelAuto
	default:
		return lv == LevelAsk || lv == LevelBlack || lv == LevelWhite || lv == LevelAuto
	}
}
