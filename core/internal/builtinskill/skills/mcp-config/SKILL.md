---
name: mcp-config
description: 把已有 MCP 服务配置到 ezharness——编辑 mcp.json 热加载、验证连通、OAuth 授权与状态排查
---

# MCP 配置（客户端侧）

把一个**已存在**的 MCP 服务（远程 http/sse，或现成的 stdio 命令）接入 ezharness。要从零造一个 MCP 服务 → 用 mcp-build 技能。

## 判定

| 情形 | 走向 |
| --- | --- |
| 服务已存在（别人部署的 http/sse，或现成命令如 `npx -y @modelcontextprotocol/server-filesystem`） | 本技能 |
| 要写代码造一个新服务 | mcp-build（本机自用选 stdio） |
| 地址/鉴权方式不确定 | 先问用户，不要猜着配 |

## 做法

配置文件 `<数据目录>/mcp.json`（绝对路径见 system `<workspace>` 段），**保存后下一轮迭代自动热加载**，无需重启。

```json
{ "servers": [
  { "name": "唯一标识", "description": "用途说明（给模型看）",
    "type": "http", "url": "https://example.com/mcp",
    "headers": { "Authorization": "Bearer <key>" },
    "allow": ["tool_a"], "enabled": true },
  { "name": "fs", "description": "本机文件检索",
    "type": "stdio", "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "D:/work"] }
] }
```

字段规则：

| 字段 | 规则 |
| --- | --- |
| `type` | `http` \| `sse` \| `stdio` |
| `http`/`sse` | 给 `url`，可选 `headers` 鉴权 |
| `stdio` | 给 `command` + `args`（cwd=数据目录），可选 `env` 增量附加 |
| `allow` | 工具白名单，省略=全部；**已有条目的 allow 原样保留** |
| `enabled` | 省略=启用；`false` 保留配置不装配 |

操作：`read_file` 读 → 改（全量保留其余条目与字段）→ `write_file` 整体写回（合法 JSON、缩进 2）。API key 不要在对用户的回复里明文复述。

## 验证（改完必走）

1. `GET /api/open/mcp` — 清单里有它且 `enabled: true`
2. `POST /api/open/mcp/call` 调一个只读工具：`200` 通了；`401+needs_auth` 见下节；`502` 服务不可达，查 url/进程
3. 新工具以 `<resource_change>` 反馈为准，不要臆造

## 排错

**OAuth（http/sse 自动检测，无需配置）**：调用遇 401（RFC9728 元数据）或命中发现文档即自动进入授权；首次会话里出「MCP 需要授权」决策卡，用户浏览器完成即可。`needs_auth` 后**原生调用不会自动重试**——授权完成后重发同一个调用。仅当服务不支持 DCR 或要指定 scope/发现地址时才加 `oauth` 块（`client_id`/`client_secret`/`scopes`/`metadata_url`）。用 OAuth 的服务不要再写 `headers.Authorization`。

授权状态文件（`<数据目录>/mcp-auth/`）：

| 文件 | 内容 | 出问题时 |
| --- | --- | --- |
| `<server>.json` | OAuth 令牌；空文件=已注销 | 反复 401 删它重新授权 |
| `<server>.client.json` | DCR 登记的 client_id | 服务端清了注册表时删它 |
| `<server>.needs` | 「需 OAuth」标记 | 一般不动 |
| `port` | 回调端口（勿手改） | — |

| 症状 | 原因 → 处置 |
| --- | --- |
| 持续 401 且不弹授权 | 服务端重启丢了令牌（core 侧还是旧的）→ 删 `<server>.json` 重新授权；根治让服务端持久化令牌 |
| 工具没出现 | 看下一轮 `<resource_change>`；仍未出现按验证步骤 2 查连接 |
| 502 | url 错 / 服务没起 / stdio 命令跑不起来（用 terminal 手跑 command 看报错） |
