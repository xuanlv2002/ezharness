/*
Package visionguard 是无视觉模型的图片兜底装饰器：主模型未开视觉
（ModelEntry.Vision=false）时，模型调用前把请求视图里全部 user 消息
的 Images 剥除，"[图片已加载: …]"文案改为"已省略"说明——防 VLM 400
卡死会话。

只改请求视图：req.Messages 换新切片、消息用副本，落盘历史（含 base64）
不受影响——换回多模态模型图片自动恢复。多模态模型或无图请求零开销。
*/
package visionguard

import (
	"context"
	"strings"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"
)

/*
	imageLoadedTag 与 ezloop filetools 的图片消息标签同源：剥离时给

开标签注入 omitted 属性（模型侧可读的省略说明），落盘历史不动。
*/
const imageLoadedTag = "<image_loaded>"

const elidedOpen = `<image_loaded omitted="当前模型可能已切换，不支持图片输入">`

/* Warp 包装模型节点：visionOn 实时判断主模型视觉能力（闭包读设置）。 */
func Warp(visionOn func() bool) warp.ModelHandler {
	return func(_ event.Emitter, inner provider.ModelProvider) provider.ModelProvider {
		return &guardProvider{inner: inner, visionOn: visionOn}
	}
}

type guardProvider struct {
	inner    provider.ModelProvider
	visionOn func() bool
}

var _ provider.ModelProvider = (*guardProvider)(nil)
var _ provider.StreamProvider = (*guardProvider)(nil)

func (p *guardProvider) Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	p.strip(req)
	return p.inner.Invoke(ctx, req)
}

func (p *guardProvider) Stream(ctx context.Context, req *types.ModelRequest, onChunk provider.ModelChunkHandler) (*types.ModelResponse, error) {
	p.strip(req)
	if sp, ok := p.inner.(provider.StreamProvider); ok {
		return sp.Stream(ctx, req, onChunk)
	}
	return p.inner.Invoke(ctx, req)
}

/* strip 剥除请求视图里的图片（无视觉能力或无图时不动）。 */
func (p *guardProvider) strip(req *types.ModelRequest) {
	if p.visionOn() || len(req.Messages) == 0 {
		return
	}
	has := false
	for i := range req.Messages {
		if len(req.Messages[i].Images) > 0 {
			has = true
			break
		}
	}
	if !has {
		return
	}
	msgs := make([]types.Message, len(req.Messages))
	copy(msgs, req.Messages)
	for i := range msgs {
		if len(msgs[i].Images) == 0 {
			continue
		}
		msgs[i].Images = nil
		msgs[i].Content = strings.Replace(msgs[i].Content, imageLoadedTag, elidedOpen, 1)
	}
	req.Messages = msgs
}
