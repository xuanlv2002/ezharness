---
name: mcp-build
description: 从零造一个 MCP 服务端——stdio（本机自用，即配即连）或 http（远程/共享/OAuth），含最小实现骨架与协议契约
---

# MCP 服务端自建

为用户造一个新的 MCP 工具服务。配置已有服务 → mcp-config 技能。

## 判定

| 需求 | 形态 | 理由 |
| --- | --- | --- |
| 本机自用工具（默认） | **stdio** | core 直接拉起子进程：零端口、零启动顺序、崩溃自动重连、即配即用，**不需要 OAuth** |
| 要被远程/多客户端共享，或包装第三方 OAuth 能力（notion 等） | http | 需要网络端点与鉴权 |

**默认选 stdio**：http 形态服务必须先跑起来才能连，引入启动顺序与端口管理问题。

## 做法 · stdio

1. 写服务端脚本，放 `<数据目录>/apps/<名>/server.py`（cwd=数据目录，相对路径可用）
2. mcp.json 加条目（配置细节见 mcp-config 技能）：

```json
{ "name": "my-tools", "description": "…", "type": "stdio",
  "command": "python3", "args": ["apps/my-tools/server.py"] }
```

3. 下一轮迭代热加载即连，按「验证」走通

铁律：

- **stdout 只输出协议 JSON**（一行一条）；日志、调试信息一律走 **stderr**——混进 stdout 就是协议污染，连接直接坏
- 握手超时 10 秒：启动期不要做慢初始化（预下载、大盘扫描），挪到首次调用
- 进程随连接生命周期由 core 管理，正常退出即可，不要 daemonize

环境继承：stdio 服务继承 core 进程环境（会话令牌被过滤），**能拿到 `EZHARNESS_OPENAPI_TOKEN`**——工具实现内部可直接调 `POST /api/open/model/call` 让"工具+大模型"组合干活（用法见 open-api 技能）。

## 模板 · stdio 最小服务（零依赖 python）

```python
#!/usr/bin/env python3
import json, sys

TOOLS = [{
    "name": "echo", "description": "原样返回输入",
    "inputSchema": {"type": "object",
                    "properties": {"text": {"type": "string"}}, "required": ["text"]},
}]

def reply(i, result):
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": i, "result": result}) + "\n")
    sys.stdout.flush()

for line in sys.stdin:                      # JSON-RPC 2.0，一行一条
    req = json.loads(line)
    m, i = req.get("method"), req.get("id")
    if m == "initialize":
        reply(i, {"protocolVersion": "2024-11-05",
                  "capabilities": {"tools": {}},
                  "serverInfo": {"name": "my-tools", "version": "1.0.0"}})
    elif m == "notifications/initialized":
        pass                                 # 通知无 id，不回包
    elif m == "tools/list":
        reply(i, {"tools": TOOLS})
    elif m == "tools/call":
        name, args = req["params"]["name"], req["params"].get("arguments", {})
        if name == "echo":
            reply(i, {"content": [{"type": "text", "text": args.get("text", "")}]})
        else:
            reply(i, {"isError": True,
                      "content": [{"type": "text", "text": f"unknown tool {name}"}]})
```

工具就照 `TOOLS` 数组扩：每个工具 `name/description/inputSchema`，`tools/call` 里分发实现，结果放 `content` 数组（文本用 `{"type":"text","text":…}`）。复杂服务可用官方 SDK（python `mcp`/FastMCP、node `@modelcontextprotocol/sdk`），但先确认用户机器装得上。

## 做法 · http（含 OAuth）

- 端点：StreamableHTTP（`POST` 收 JSON-RPC，方法同 stdio：initialize/tools/list/tools/call），带 `Authorization: Bearer <token>`
- **生命周期**：先起服务才能连。建议做成快应用（`backend` 声明启动命令），用户点卡片即拉起——见 quick-app 技能
- 排错顺序：**先无鉴权跑通**（下一节验证全绿），再叠 OAuth，一步一验

要 OAuth 时，core 作为客户端认以下契约（服务端必须全部满足）：

1. **发现**（二选一）：受保护端点回 `401` 带 `WWW-Authenticate: Bearer realm="…", resource_metadata="<发现文档URL>"`；或在端点 origin 提供发现文档（core 按序探：`/.well-known/oauth-protected-resource[/<MCP路径>]` → `oauth-authorization-server` → `openid-configuration`），文档含 `authorization_servers` 或 `authorization_endpoint`
2. **授权服务器元数据**（`authorization_servers` 指向的 RFC8414 文档）：必备 `issuer`/`authorization_endpoint`/`token_endpoint`；走 DCR 还要 `registration_endpoint`（core 无 client_id 时用它动态注册，scope 申请 `ezharness`）
3. **授权码流程**：PKCE S256 必须；`redirect_uri` 只会是 loopback `http://127.0.0.1:<port>/oauth/callback`，**端口由 core 决定并持久化，按 RFC8252 接受任意 loopback 端口、勿假设**；带 `resource` 与 `state`；建议支持 `refresh_token`（core 自动刷新）
4. **令牌校验**：无效回 401，不要回 200 空结果

## 验证

| 步骤 | stdio | http |
| --- | --- | --- |
| 1 配置 | mcp.json 写入 → 下一轮热加载 | 同左（服务须已在跑） |
| 2 发现 | `GET /api/open/mcp` 有它 | 同左 |
| 3 调用 | `POST /api/open/mcp/call` 调只读工具 → 200 | 同左（未授权得 401+needs_auth → 用户授权 → **重发**） |

## 排错

| 症状 | 原因 → 处置 |
| --- | --- |
| 连不上/握手超时 | 用 terminal 手跑 `command args` 看报错：命令不存在（PATH）、依赖没装、启动慢于 10 秒 |
| 时通时不通/输出怪异 | stdout 混了日志 → 全部改 stderr |
| http 反复 401 不弹授权 | 服务端令牌只存内存重启即丢 → 服务端持久化令牌，或删 `mcp-auth/<server>.json` 重授权 |
| OAuth 授权被拒 redirect_uri 不匹配 | 服务端写死了端口 → 放开 loopback 任意端口 |
