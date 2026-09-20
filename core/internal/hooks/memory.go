/*
memory 是长期记忆 hook：每轮 OnStart 把 memory/longterm/harness.md
（索引文件）拼进 system（复用 skill 的单条 system 拼接模式），agent
用文件工具直接编辑该文件或 grep 其余记忆文件。长期记忆是一套文件
系统：harness.md 初始加载，其余文件按需检索。
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
	HarnessMd   = LongtermDir + "/harness.md"
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
DefaultHarnessMd 是索引文件的初始模板：首次访问（文件缺失）时落盘，
此后完全归 agent 维护。
*/
const DefaultHarnessMd = `# ezharness 长期记忆索引

本文件是长期记忆的入口，随会话加载进上下文。长期记忆固定三个主题文件：
- user.md — 用户偏好与习惯、稳定的用户事实
- projects.md — 项目与任务背景（在做什么、关键路径与约定、交付物位置）
- lessons.md — 踩过的坑与解法、验证过的环境特性

会话归档时系统会把值得长期保留的内容自动合并进这三个文件；对话中
也可用文件工具直接编辑它们或本索引。三个文件不进上下文，需要时用
grep/findstr 检索。
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
