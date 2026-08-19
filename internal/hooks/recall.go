/*
recall 是话题回顾 hook：提供 recall_topic 工具，模型可查看已归档话题
索引、按需加载旧 session 全文——"回忆"能力。分身也可用（只读）。
*/
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/localsession"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* RecallTool 是话题回顾工具的注册名。 */
const RecallTool = "recall_topic"

/* Recall 提供归档话题的检索与加载。 */
type Recall struct {
	fsys   fs.FileSystem
	topics *Topics
}

/* NewRecall 创建回顾 hook。 */
func NewRecall(fsys fs.FileSystem, topics *Topics) *Recall {
	return &Recall{fsys: fsys, topics: topics}
}

func (r *Recall) Name() string { return "recall" }

/* OnStart 注册 recall_topic 工具。 */
func (r *Recall) OnStart(_ context.Context, state *types.LoopState) error {
	state.Tools.Register(recallTool{})
	return nil
}

/* OnToolStart 拦截回顾调用。 */
func (r *Recall) OnToolStart(ctx context.Context, _ *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	if call.Name != RecallTool {
		return ezhook.Proceed, nil
	}
	var args struct {
		Topic string `json:"topic"`
	}
	_ = json.Unmarshal(call.Args, &args)
	return ezhook.Skip(r.recall(ctx, args.Topic)), nil
}

func (r *Recall) recall(ctx context.Context, q string) string {
	list := r.topics.Match(q)
	if len(list) == 0 {
		if q == "" {
			return "还没有已归档的话题。"
		}
		return "未找到匹配「" + q + "」的话题。现有话题：\n" + r.indexText(r.topics.Load())
	}
	if q == "" || len(list) > 1 {
		// 列表视图：让模型看清有什么，再决定是否精读
		return "已归档话题列表（用 recall_topic 指定 id 或标题关键词精读全文）：\n" + r.indexText(list)
	}
	// 单一命中：加载全文
	e := list[0]
	s, err := localsession.Load(ctx, r.fsys, "", e.ID)
	if err != nil {
		return "话题 " + e.Title + " 的存档读取失败: " + err.Error()
	}
	return fmt.Sprintf("话题《%s》完整存档（%d 条消息）：\n%s", e.Title, len(s.Messages), transcript(s.Messages))
}

/* indexText 渲染索引列表。 */
func (r *Recall) indexText(list []TopicEntry) string {
	var b strings.Builder
	for i, e := range list {
		fmt.Fprintf(&b, "%d. [%s] %s\n   %s\n", i+1, e.ID, e.Title, clip(e.Summary, 120))
	}
	return b.String()
}

/* transcript 把消息历史渲染为模型可读的对话文本。 */
func transcript(msgs []types.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case types.RoleUser:
			fmt.Fprintf(&b, "[用户] %s\n", m.Content)
		case types.RoleAssistant:
			if m.Content != "" {
				fmt.Fprintf(&b, "[助手] %s\n", m.Content)
			}
		case types.RoleTool:
			fmt.Fprintf(&b, "[工具%s结果] %s\n", m.ToolCallID, clip(m.Content, 400))
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	t := strings.TrimSpace(s)
	r := []rune(t)
	if len(r) <= n {
		return t
	}
	return string(r[:n]) + "…"
}

/* recallTool 是 recall_topic 壳工具（拦截式）。 */
type recallTool struct{}

func (recallTool) Name() string { return RecallTool }
func (recallTool) Description() string {
	return "回顾已归档的话题：不指定参数返回话题索引列表；指定 topic（id 或标题关键词）" +
		"返回该话题的完整对话存档。当用户提起之前聊过的内容时使用。"
}
func (recallTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string","description":"话题 id 或标题关键词，留空则列出全部话题"}}}`)
}
func (recallTool) Invoke(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("recall: hook not registered")
}
