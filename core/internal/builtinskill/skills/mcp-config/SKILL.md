---
name: mcp-config
description: 给应用增删改 MCP 服务器（编辑 mcp.json 热加载）与自建 OAuth MCP 服务端的协议要求、验证流程、授权状态排查
---

# MCP 服务器自配置

`<数据目录>/mcp.json`（数据目录绝对路径见 system 提示 `<workspace>` 段第一行）是 MCP 服务器的唯一配置来源。你可以直接编辑它：**保存后的下一轮迭代自动热加载**（连接重建），新工具面经 `<resource_change>` 反馈，无需重启应用。

## 配置文件结构

```json
{
  "servers": [
    {
      "name": "唯一标识",
      "description": "用途说明（给模型看，必写清楚）",
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer <key>" },
      "allow": ["tool_a", "tool_b"],
      "enabled": true
    }
  ]
}
```

字段规则：
- `type`：`http` | `sse` | `stdio`
- `http`/`sse` 型给 `url`（+可选 `headers` 鉴权）；`stdio` 型给 `command` + `args`（+可选 `env`）
- `allow` 是工具白名单，省略 = 全部允许；已有条目的 `allow` 必须原样保留
- `enabled` 省略 = 启用；禁用条目保留配置但不装配
- API key 放 `headers`/`env`，不要在对用户的回复里明文复述

## 操作流程

1. `read_file` 读 `<数据目录>/mcp.json`（绝对路径，从 `<workspace>` 段的数据目录行拼）
2. 解析后按需追加/修改/删除条目，**全量保留其余条目与所有字段**
3. `write_file` 整体写回（合法 JSON、缩进 2 空格）
4. 按下节「验证流程」走一遍再交付

无法确定服务地址或鉴权方式时，先问用户，不要猜着配。

## 验证流程（改完必走）

用 OpenAPI 直调验证（令牌用法见 open-api 技能）：

1. `GET /api/open/mcp` — 清单里有它且 `enabled: true`
2. `POST /api/open/mcp/call` 调一个只读工具（如 list 类）：
   - `200 {"result":...}` — 通了
   - `401 + needs_auth` — 拿响应里的 `auth_url` 交给用户（系统浏览器）授权；**原生调用不会自动重试**，授权完成后**重新发同一个调用**（可先轮询 `GET /api/open/mcp/oauth/status?name=<server>` 等到 `authorized: true`）
   - `502` — 服务端不可达/握手失败，查 url 与服务进程
3. 新工具出现在 `<resource_change>` 前，不要在对话里臆造工具清单

## OAuth 客户端侧（自动，通常无需配置）

**OAuth 是自动检测的**——服务是 http/sse 且用 OAuth 时，接入后无需任何配置：调用遇 401（通告了 RFC9728 资源元数据）或命中发现文档即自动进入认证流程，不需要在 `mcp.json` 里加 `oauth` 块。

只有当服务**不支持动态注册（DCR）**、或要**指定 scope / 手工发现地址**时，才加 `oauth` 块覆盖：

```json
{ "name": "notion", "type": "http", "url": "https://mcp.example.com/v1/mcp",
  "oauth": { "enabled": true, "client_id": "…", "client_secret": "…", "scopes": ["read"], "metadata_url": "…" } }
```

- `oauth.client_id` / `client_secret`：服务不支持 DCR 时才填，否则留空
- `oauth.scopes`：可选，申请的权限范围数组
- `oauth.metadata_url`：授权服务器元数据地址（自动发现失败时手工指定）

**首次需要认证时**会话里出现「MCP 需要授权」决策卡：点「去授权」→ 系统浏览器完成 → 该次调用自动重试。用户也可在 MCP 设置页点「连接」。用 OAuth 的服务不要再在 `headers` 里写 `Authorization`。

## 自建 OAuth MCP 服务端：最小协议要求

core 作为 OAuth 客户端认以下契约（自建服务端必须全部满足；实现前先读这节，不要去猜）：

**1. 让 core 发现你需要 OAuth（二选一）**
- 受保护端点回 `401`，带响应头 `WWW-Authenticate: Bearer realm="<任意>", resource_metadata="<发现文档URL>"`
- 或在 MCP 端点 origin 提供发现文档（core 会按序探测，命中任一即可）：
  `/.well-known/oauth-protected-resource` → `/.well-known/oauth-protected-resource/<MCP路径>` → `/.well-known/oauth-authorization-server` → `/.well-known/openid-configuration`
- 发现文档是 JSON，必须含 `authorization_servers: ["<授权服务器元数据URL>"]` 或直接 `authorization_endpoint`（RFC8414 格式）

**2. 授权服务器元数据**（`authorization_servers` 指向的文档，RFC8414）：必备字段 `issuer`、`authorization_endpoint`、`token_endpoint`；要走 DCR 还必须有 `registration_endpoint`（RFC7591，core 无 client_id 时用它动态注册，scope 申请 `ezharness`）。

**3. 授权码流程硬性要求**
- **PKCE S256 必须**：授权请求会带 `code_challenge` + `code_challenge_method=S256`，换令牌时校验 `code_verifier`
- **redirect_uri 只会是 loopback**：`http://127.0.0.1:<port>/oauth/callback`。**端口由 core 决定并持久化**（存在 `mcp-auth/port`），服务端**不要假设具体端口**——按 RFC8252 接受任意 loopback 端口；只回环、不外网
- 授权请求带 `resource=<MCP端点>`（RFC8707 资源指示）与 `state`；回调用标准 `code` / `error` 参数
- 换令牌：标准 `authorization_code` + `code_verifier`；支持 `refresh_token` 的话 core 会在 access 过期时自动刷新（强烈建议支持）

**4. MCP 端点本身**：StreamableHTTP（`POST` JSON-RPC，`tools/list` / `tools/call`），带 `Authorization: Bearer <access_token>`；令牌无效回 401（不要回 200 空结果）。

## 授权状态排查（mcp-auth/ 目录）

| 文件 | 内容 | 出问题时怎么办 |
| --- | --- | --- |
| `mcp-auth/<server>.json` | OAuth 令牌（access/refresh/过期时间）；空文件 = 已注销 | 反复 401 时删它重新授权 |
| `mcp-auth/<server>.client.json` | DCR 登记的 client_id | 服务端清了注册表时删它（会重新 DCR） |
| `mcp-auth/<server>.needs` | 「该服务需要 OAuth」标记（免重复探测） | 一般不动；想强制重探测可删 |
| `mcp-auth/port` | 回调监听端口（redirect_uri 绑定它） | 不要手改 |

**典型故障：服务端重启后令牌丢失**（服务端把令牌只存内存）——core 侧仍是旧令牌，表现为持续 401 且不弹授权（core 认为已授权）。处理：删 `mcp-auth/<server>.json`（或 MCP 设置页「注销」）→ 重新授权；根治：服务端自己持久化签发的令牌。

**排错顺序**：先把服务做成**无鉴权**跑通（步骤 2 的验证流程全绿），再叠加 OAuth——一步一验，别整套上。
