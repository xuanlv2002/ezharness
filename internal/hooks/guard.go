/*
Guard 是上下文兜底 ToolEndHook：估算"当前历史 tokens + 本次工具结果"，
超出模型上下文窗口余量的结果卸载到 .ezloop/offload/，上下文只留头部
摘要与文件路径——这是结果进入消息历史前的最后防线。

与 offload 的分工：offload 按固定阈值（4096 字节）卸载常规大结果，但
skip 名单与 ReplayTool 豁免的结果（read_file 等）不设防；本 hook 按
窗口余量动态判定，专堵这些豁免漏网（如 read_file 读单行超长文件）。
装配顺序在 offload 之后：常规大结果先被 offload 改小，到达这里的多
是豁免者。写入失败时硬截断丢弃尾部——此处不能像 offload 那样降级
透传原文，透传即撑爆上下文，保会话优先于保内容。

token 估算口径：LastResponse.Usage 是 provider 报告的精确值，但不含
最后一次模型响应之后追加的消息（本轮 assistant tool_calls 与同批先
完成的工具结果），按 1.2 字符/token 补估——中文实际 ≈1.1-1.5 c/t，
取保守端；英文代码会高估（实际 ≈4-5 c/t）导致提前触发，对兜底阀
而言方向安全。须装配在 offload 之后才能看到改写后的最终 Content。
*/
package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode/utf8"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"
)

const (
	guardHead          = 512               // 摘要保留的头部长度（字符）
	guardCharsPerToken = 1.2               // 保守估算系数：字符数 → token 数
	guardDir           = ".ezloop/offload" // 与 offload 同目录，回放体验统一
)

/* Guard 按窗口余量兜底卸载放不下的工具结果。 */
type Guard struct {
	fsys   fs.FileSystem
	window int
}

/* NewGuard window 为模型上下文窗口（tokens），非正值时 hook 不动作。 */
func NewGuard(fsys fs.FileSystem, window int) *Guard {
	return &Guard{fsys: fsys, window: window}
}

func (g *Guard) Name() string { return "guard" }

func (g *Guard) OnToolEnd(ctx context.Context, state *types.LoopState, result *types.ToolResult) error {
	if result.Err != nil || result.Content == "" || g.window <= 0 {
		return nil
	}
	runes := utf8.RuneCountInString(result.Content)
	used := g.usedTokens(state)
	if used+int(float64(runes)/guardCharsPerToken) <= g.window {
		return nil
	}

	head := string([]rune(result.Content)[:min(guardHead, runes)])
	sum := sha256.Sum256(append([]byte(result.Name+"\x00"), result.Content...))
	path := guardDir + "/" + result.Name + "-" + hex.EncodeToString(sum[:6]) + ".txt"
	note := fmt.Sprintf("[输出共 %d 字符，当前上下文约 %d/%d tokens，余量放不下完整结果，已卸载到 %s；如需查看请用 read_file 按行分段读取（offset/limit），勿整文件回放]",
		runes, used, g.window, path)
	if werr := g.fsys.Write(ctx, path, []byte(result.Content)); werr != nil {
		result.Content = head + "\n" + note + "（卸载写入失败，超出部分已丢弃）"
		return nil
	}
	result.Content = head + "\n" + note
	return nil
}

/*
	usedTokens 估算当前消息历史的 token 量：优先用 provider 报告的

Usage（最后一次模型调用的 prompt+completion），再补估其后追加的
消息（本轮 tool_calls 参数与同批先完成的工具结果）。无 Usage 数据
时全量按字符粗估。
*/
func (g *Guard) usedTokens(state *types.LoopState) int {
	if state.LastResponse != nil {
		used := state.LastResponse.Usage.PromptTokens + state.LastResponse.Usage.CompletionTokens
		for i := len(state.Messages) - 1; i >= 0; i-- {
			m := state.Messages[i]
			if m.Role == types.RoleAssistant {
				break // assistant 自身已含在 CompletionTokens 里
			}
			chars := utf8.RuneCountInString(m.Content)
			for _, c := range m.ToolCalls {
				chars += utf8.RuneCountInString(string(c.Args))
			}
			used += int(float64(chars) / guardCharsPerToken)
		}
		return used
	}
	total := 0
	for _, m := range state.Messages {
		total += utf8.RuneCountInString(m.Content)
		for _, c := range m.ToolCalls {
			total += utf8.RuneCountInString(string(c.Args))
		}
	}
	return int(float64(total) / guardCharsPerToken)
}
