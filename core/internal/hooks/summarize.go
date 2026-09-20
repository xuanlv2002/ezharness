/*
summarize 是共享的摘要基础：trim（上下文折叠）与 archive（话题归档）
两条路径复用，仅 prompt 不同。不走 summary.Summarize（内置 30s 超时是
给 EndHook 防挂死设计的），这里用独立 2 分钟预算——大上下文摘要本就
慢，被 30s 卡死会让每次整理都失败。
*/
package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

func summarizeMsgs(ctx context.Context, p provider.ModelProvider, prompt string, msgs []types.Message) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s", m.Role, m.Content)
		if m.Err != "" {
			fmt.Fprintf(&b, " (error: %s)", m.Err)
		}
		b.WriteByte('\n')
	}
	resp, err := p.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{
		{Role: types.RoleUser, Content: prompt + "\n\n" + b.String()},
	}})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

/*
normalizeStructuredSummary 规范四节结构化摘要（trim 提示词要求的
【已完成】【正在做】【待办】【关键事实】）：剥模型手滑加的代码围栏，
部分缺节补占位，全部缺失时整体包成【关键事实】兜底可用——不重试，
弱模型的格式保证靠解析端兜底比再调一次模型可靠。
*/
func normalizeStructuredSummary(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") { // 剥 ``` 围栏（含 ```markdown 前缀）
		if i := strings.IndexByte(s[len(s[:3]):], '\n'); i >= 0 {
			s = strings.TrimSpace(s[3+i+1:])
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	for _, sec := range trimSections {
		if strings.Contains(s, sec) {
			for _, missing := range trimSections {
				if !strings.Contains(s, missing) {
					s += "\n" + missing + "\n（无）"
				}
			}
			return s
		}
	}
	return "【关键事实】\n" + s
}

/* trimSections trim 结构化摘要的四个固定小节标题。 */
var trimSections = [4]string{"【已完成】", "【正在做】", "【待办】", "【关键事实】"}
