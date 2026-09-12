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
