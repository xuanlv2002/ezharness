/*
memory 是长期记忆 hook：每轮 OnStart 把 memory.md 内容拼进 system
（复用 skill 的单条 system 拼接模式），agent 用 filetools 直接编辑
该文件即可持久记忆。对用户暴露为左栏可编辑页。
*/
package hooks

import (
	"context"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"
)

/* MemoryFile 是记忆文件路径（工作目录相对）。 */
const MemoryFile = "memory.md"

/* Memory 实现记忆注入。 */
type Memory struct {
	fsys fs.FileSystem
}

/* NewMemory 创建记忆 hook。 */
func NewMemory(fsys fs.FileSystem) *Memory { return &Memory{fsys: fsys} }

func (m *Memory) Name() string { return "memory" }

/* OnStart 读 memory.md 拼接进 system（文件缺失/为空则跳过）。 */
func (m *Memory) OnStart(ctx context.Context, state *types.LoopState) error {
	data, err := m.fsys.Read(ctx, MemoryFile)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	block := "# 长期记忆（用户与你的沉淀，可用文件工具更新 memory.md）\n" + string(data)
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
