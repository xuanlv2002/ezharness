/*
modeldump 是 provider 装饰器：每次模型调用前把完整输入
（Messages + Tools）全文打印到控制台，供调试上下文机制。
挂在 warp 链最外层（先于 modelretry 注册），重试不重复打印；
fork 分身复刻 warp 链，分身的输入同样可见。
*/
package modeldump

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"
)

var (
	seq atomic.Int64
	mu  sync.Mutex // fork 与主循环并行时避免多次打印交错
)

/* Warp 返回模型输入打印装饰器。 */
func Warp() warp.ModelHandler {
	return func(_ event.Emitter, p provider.ModelProvider) provider.ModelProvider {
		return &dumpProvider{inner: p}
	}
}

type dumpProvider struct {
	inner provider.ModelProvider
}

func (d *dumpProvider) Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	dump(req)
	return d.inner.Invoke(ctx, req)
}

func (d *dumpProvider) Stream(ctx context.Context, req *types.ModelRequest, onChunk provider.ModelChunkHandler) (*types.ModelResponse, error) {
	if sp, ok := d.inner.(provider.StreamProvider); ok {
		dump(req)
		return sp.Stream(ctx, req, onChunk)
	}
	return d.Invoke(ctx, req)
}

type dumpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ArgsSchema  json.RawMessage `json:"args_schema"`
}

/* dump 打印一行头部摘要 + 请求全量 JSON。 */
func dump(req *types.ModelRequest) {
	tools := make([]dumpTool, 0, len(req.Tools))
	chars := 0
	for _, m := range req.Messages {
		chars += len(m.Content)
	}
	for _, t := range req.Tools {
		tools = append(tools, dumpTool{Name: t.Name(), Description: t.Description(), ArgsSchema: t.ArgsSchema()})
	}
	body, err := json.MarshalIndent(struct {
		Messages []types.Message `json:"messages"`
		Tools    []dumpTool      `json:"tools"`
	}{req.Messages, tools}, "", "  ")
	if err != nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	fmt.Printf("========== [model-dump #%d] %s · %d msgs · %d chars · %d tools ==========\n%s\n",
		seq.Add(1), time.Now().Format("15:04:05"), len(req.Messages), chars, len(tools), body)
}
