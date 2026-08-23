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
	"github.com/xuanlv2002/ezloop/types"
)

/* 记忆体系目录布局（工作目录相对）。 */
const (
	MemoryDir      = "memory"
	LongtermDir    = "memory/longterm"
	SkillsDir      = "memory/skills"
	HarnessMd      = LongtermDir + "/harness.md"
)

/* MemoryFile 是初始加载进上下文的索引文件（兼容旧名引用）。 */
const MemoryFile = HarnessMd

/*
DefaultHarnessMd 是索引文件的初始模板：首次访问（文件缺失）时落盘，
此后完全归 agent 维护。
*/
const DefaultHarnessMd = `# ezharness 长期记忆索引

本文件是长期记忆的入口，随会话加载进上下文。重要的用户偏好与事实
按主题写入独立文件（如 user.md、projects.md），并在此登记一行摘要；
本文件也可直接更新。其余记忆文件不进上下文，用 grep/findstr 检索。
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
	data, err := m.fsys.Read(ctx, MemoryFile)
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
