/*
noempty 是 provider 装饰器：纯附件轮（用户没打字）的 user 输入消息
content 为空串，裸发给模型易被拒/被忽略——填充占位语义。只填充
请求副本，不改引擎 state。
*/
package noempty

import (
	"context"
	"strings"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"
)

/* fillText 是空输入的占位语义。 */
const fillText = "（本轮无文字输入，用户仅发送了附件）"

/* Warp 返回空消息填充装饰器。 */
func Warp() warp.ModelHandler {
	return func(_ event.Emitter, p provider.ModelProvider) provider.ModelProvider {
		return &noEmptyProvider{inner: p}
	}
}

type noEmptyProvider struct {
	inner provider.ModelProvider
}

func (p *noEmptyProvider) Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	return p.inner.Invoke(ctx, filled(req))
}

func (p *noEmptyProvider) Stream(ctx context.Context, req *types.ModelRequest, onChunk provider.ModelChunkHandler) (*types.ModelResponse, error) {
	if sp, ok := p.inner.(provider.StreamProvider); ok {
		return sp.Stream(ctx, filled(req), onChunk)
	}
	return p.Invoke(ctx, req)
}

/* filled 返回填充后的请求副本（无空消息时原样返回）。 */
func filled(req *types.ModelRequest) *types.ModelRequest {
	need := false
	for _, m := range req.Messages {
		if isEmptyUser(m) {
			need = true
			break
		}
	}
	if !need {
		return req
	}
	msgs := make([]types.Message, len(req.Messages))
	copy(msgs, req.Messages)
	for i := range msgs {
		if isEmptyUser(msgs[i]) {
			msgs[i].Content = fillText
		}
	}
	nr := *req
	nr.Messages = msgs
	return &nr
}

func isEmptyUser(m types.Message) bool {
	return m.Role == types.RoleUser && strings.TrimSpace(m.Content) == "" && len(m.Images) == 0
}
