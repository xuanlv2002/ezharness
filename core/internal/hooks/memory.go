/*
memory 是长期记忆 hook：每轮 OnStart 把 memory/longterm/harness.md
（记忆入口：树说明 + 读写纪律）拼进 system（复用 skill 的单条 system
拼接模式）。长期记忆是一棵文件树：入口 + user/soul/项目索引常驻，
project-<名>.md 详情按需读取（agent 用文件工具直接维护）。
*/
package hooks

import (
	"context"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/types"
)

/* 记忆体系目录布局（工作目录相对）。 */
const (
	MemoryDir   = "memory"
	LongtermDir = "memory/longterm"
	SkillsDir   = "memory/skills"
	HarnessMd   = LongtermDir + "/harness.md"        // 记忆入口：树说明 + 读写纪律（常驻）
	UserMd      = LongtermDir + "/user.md"           // 用户个人信息（常驻）
	SoulMd      = LongtermDir + "/soul.md"           // agent 工作习惯（常驻）
	ProjectMd   = LongtermDir + "/project.md"        // 项目记忆索引（常驻）
)

/*
SkillDirOf 从 SKILL.md 的 FS 路径提取技能目录名。目录名是技能的
稳定身份（frontmatter name 可与目录名不同），启停名单与删除都按它定位。
实现在 ezloop skill 包（目录布局的通用知识），此处保留本地入口供
既有调用点使用。
*/
func SkillDirOf(path string) string {
	return skill.DirOf(path)
}

/*
DefaultHarnessMd 是记忆入口文件的初始模板：首次访问（文件缺失）时落盘，
此后完全归 agent 维护。记忆树：入口说明（本文件）+ user/soul/项目索引
常驻注入，项目详情文件按需读取。
*/
const DefaultHarnessMd = `# ezharness 长期记忆入口

记忆树（都在 memory/longterm/ 下，路径相对数据目录）：
- user.md — 用户个人信息：偏好与习惯、背景、工作环境、约定（归档时系统自动沉淀）
- soul.md — agent 自己的工作习惯：本环境下顺手的做法、工具与验证方式偏好（归档时系统自动沉淀）
- project.md — 项目记忆索引：一行一个项目（- 项目名 — 项目描述 | 记忆: 记忆文件地址）
- project-<名>.md — 单个项目的记忆（背景、约定、进度），按需读取

读写纪律：
- user/soul/项目索引随上下文常驻；project-<名>.md 相关时才 read_file，不主动全读
- 对话中出现值得长期保留的用户信息或工作习惯，直接编辑 user.md/soul.md，无需声明
- 项目记忆：判断值得跨会话保留时写 project-<名>.md，并在 project.md 加/改索引行；项目结束或信息过时及时更新
- 更新一律覆盖式：读旧全文 → 融合新信息 → 写新全文；宁精勿杂，过时即删
`

/*
EnsureHarnessMd 确保索引文件存在（缺失写默认模板），返回当前内容。
system 组装的唯一取材入口——初始化与加载合一。
*/
func EnsureHarnessMd(ctx context.Context, fsys fs.FileSystem) string {
	if data, err := fsys.Read(ctx, HarnessMd); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return string(data)
	}
	_ = fsys.Write(ctx, HarnessMd, []byte(DefaultHarnessMd))
	return DefaultHarnessMd
}

/* Memory 实现记忆注入。 */
type Memory struct {
	fsys fs.FileSystem
}

/* NewMemory 创建记忆 hook。 */
func NewMemory(fsys fs.FileSystem) *Memory { return &Memory{fsys: fsys} }

func (m *Memory) Name() string { return "memory" }

/* OnStart 读 harness.md 拼接进 system（文件缺失/为空则跳过）。 */
func (m *Memory) OnStart(ctx context.Context, state *types.LoopState) error {
	data, err := m.fsys.Read(ctx, HarnessMd)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	block := "# 长期记忆（索引随上下文加载，可用文件工具更新 memory/longterm/harness.md；其余记忆文件可用 grep 检索）\n" + string(data)
	// 单条 system 政策：有则拼接，无则新建（与 skill 同模式）
	if len(state.Messages) > 0 && state.Messages[0].Role == types.RoleSystem {
		state.Messages[0].Content += "\n\n" + block
		return nil
	}
	state.Messages = append([]types.Message{{
		Role:    types.RoleSystem,
		Content: block,
	}}, state.Messages...)
	return nil
}
