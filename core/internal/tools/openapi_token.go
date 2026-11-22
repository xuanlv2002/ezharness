/*
create_openapi_token 工具：agent 需要调本机 OpenAPI 时自己生成一枚令牌。
令牌明文返回给 agent，由它显式写进脚本/快应用/外部程序；不调用就不生成。
create 由 service 注入，避免 tools → service 的循环依赖。
*/
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/types"
)

/* OpenapiToken 返回生成 OpenAPI 令牌的工具。 */
func OpenapiToken(create func(name string) string) types.Tool {
	return openapiTokenTool{create: create}
}

type openapiTokenTool struct {
	create func(name string) string
}

func (openapiTokenTool) Name() string { return "create_openapi_token" }

func (openapiTokenTool) Description() string {
	return "生成一枚用于调用本机 OpenAPI 的令牌（请求头 EzHarnessOpenToken）。" +
		"脚本、快应用或外部程序要调 /api/open/* 时生成一枚，把令牌明文写进调用代码或配置；" +
		"不需要调用就不要生成。令牌可在安全页吊销。MCP OAuth 授权与这枚令牌无关：" +
		"mcp/call 返回 401+needs_auth 时按授权流程处理，不要重新生成令牌"
}

func (openapiTokenTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "令牌名称（可选，默认 agent）"}
		}
	}`)
}

func (t openapiTokenTool) Invoke(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", err
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = "agent"
	}
	token := t.create(name)
	if token == "" {
		return "", fmt.Errorf("生成 OpenAPI 令牌失败")
	}
	return fmt.Sprintf("OpenAPI 令牌「%s」：%s\n用法：请求头 EzHarnessOpenToken: %s；脚本/快应用可明文写入代码或配置。", name, token, token), nil
}
