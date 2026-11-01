---
name: open-api
description: 调用 ezharness core 的 OpenAPI——直调大模型、调 MCP 工具；写批量脚本、快应用页面/后端、本地服务对接核心能力时用
---

# core OpenAPI

core 在本机暴露 HTTP API（基址见 system `<local_api>` 段，形如 `http://127.0.0.1:5260`），供脚本、快应用、外部程序调用，所有调用方同一套认证。

## 判定

| 调用方 | 取令牌 | 依赖 SDK |
| --- | --- | --- |
| 终端/子进程脚本 | env `$EZHARNESS_OPENAPI_TOKEN`（Windows `%…%`，自动继承） | 否 |
| 外部程序/手写脚本 | 读 `<数据目录>/openapi.json`（0600）的 `token` | 否 |
| 快应用后端（推荐交付形态） | env 或文件，实时新鲜 | 否（裸 HTTP） |
| 纯前端页面（含 `file://` 测试页） | **硬编码**进 `ez.config` | 是（`/api/open/sdk.js`） |

策略：**调试期允许硬编码，交付期切后端**——长住应用一律后端取令牌（页面→后端→core），令牌不出后端。

## 做法

**鉴权**：请求头 `X-EZ-Open-Token: <令牌>`。令牌持久有效，直到用户在设置页重置；**不要让用户把令牌贴进对话**，指引用户去设置页。ezharness 同源页面还会自动带 cookie，与令牌头两通道任一命中即过——硬编码令牌被重置的页面在 ezharness 里仍可用，拿到外面才失效。

**端点**：

| 端点 | 载荷 | 返回 |
| --- | --- | --- |
| `POST /api/open/model/call` | `{prompt, model?(省略=主模型), images?(本机绝对路径≤4张，须视觉模型)}` | `200 {result}`；单次上限 5 分钟不重试 |
| `POST /api/open/mcp/call` | `{server, tool, args}`（工具名以清单/schema 为准，勿臆造） | `200 {result}`；未授权 `401+needs_auth` |
| `GET /api/open/mcp` | — | 已启用服务清单 |

**错误码**：

| 状态 | 含义 | 处理 |
| --- | --- | --- |
| 400 | 参数错误 | 不重试，查载荷 |
| 401（无 needs_auth） | 令牌无效/被重置 | 重取令牌再试；仍 401 让用户去设置页 |
| 401 + needs_auth | MCP 未授权（仅 mcp/call） | 见模板·授权等待 |
| 502 | 上游失败（模型配置/apiKey/额度；MCP 服务不可达） | 修因后重试 |

**令牌重置的影响面**：纯前端页面在 ezharness 内仍可用（cookie），外面失效 → 重新生成；后端与终端脚本 → **已运行进程的 env 是旧值，重启后端/重开终端**即恢复，文件永远新鲜。

## 模板

curl（其余语言同构：node 用内置 fetch、powershell 用 Invoke-RestMethod，头一样）：

```bash
curl -s -H "X-EZ-Open-Token: $EZHARNESS_OPENAPI_TOKEN" -H 'Content-Type: application/json' \
  -d '{"prompt":"把这段话翻译成英文：你好世界"}' http://127.0.0.1:5260/api/open/model/call
```

python（标准库，批量脚本的基础件）：

```python
import json, os, urllib.request

def ez_call(path, body):
    req = urllib.request.Request(f"http://127.0.0.1:5260/api/open/{path}",
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json",
                 "X-EZ-Open-Token": os.environ["EZHARNESS_OPENAPI_TOKEN"]})
    with urllib.request.urlopen(req, timeout=300) as r:
        return json.load(r)

print(ez_call("model/call", {"prompt": "总结：…"})["result"])
```

授权等待（脚本拿到 `needs_auth` **不会自动重试**；`auth_url` 交给用户本人在浏览器打开）：

```python
r = ez_call_post("mcp/call", {"server": S, "tool": T, "args": A})   # ez_get 同款但 GET
if r.get("needs_auth"):
    print(f"请在浏览器完成授权：{r['auth_url']}")
    for _ in range(200):                       # 1.5s×200，上限 5 分钟
        time.sleep(1.5)
        if ez_get(f"mcp/oauth/status?name={S}").get("authorized"):
            r = ez_call_post("mcp/call", {...})  # 重发同一个调用
            break
```

批量要点：串行或小并发（≤4）、失败记录后继续末尾汇总、超时给足。

浏览器 SDK（基址从脚本加载地址推导，`file://` 直开可用；OAuth 自动弹框重试）：

```html
<script src="http://127.0.0.1:PORT/api/open/sdk.js"></script>
<script>
  ez.config({ token: 'openapi 令牌' })            // 无脑传、永远传
  const text = await ez.model.call('…')
  const data = await ez.mcp.call('server', 'tool', args)
</script>
```

## 验证

连通性自检（新任务直接复用，改 port 即用）：

```python
#!/usr/bin/env python3
"""ezharness 自检：core 可达 / 令牌 / MCP 清单 /（--model）模型可用"""
import json, os, sys, urllib.request, urllib.error

base = "http://127.0.0.1:" + (sys.argv[1] if len(sys.argv) > 1 and sys.argv[1].isdigit() else "5260")
tok = os.environ.get("EZHARNESS_OPENAPI_TOKEN")
if not tok:
    try: tok = json.load(open("openapi.json"))["token"]   # cwd=数据目录时
    except Exception: sys.exit("取不到令牌：EZHARNESS_OPENAPI_TOKEN 或 openapi.json")

def call(path, body=None):
    req = urllib.request.Request(base + "/api/open/" + path,
        data=json.dumps(body).encode() if body else None,
        headers={"Content-Type": "application/json", "X-EZ-Open-Token": tok})
    with urllib.request.urlopen(req, timeout=30) as r: return json.load(r)

try:
    print("✓ core 可达，令牌有效；MCP：", ", ".join(s["name"] for s in call("mcp")["servers"]) or "（无）")
except urllib.error.HTTPError as e: sys.exit(f"✗ HTTP {e.code}（401=重置过，重取令牌）")
except Exception as e: sys.exit(f"✗ core 不可达：{e}")
if "--model" in sys.argv:
    try: print("✓ 模型可用：", call("model/call", {"prompt": "回复 ok"})["result"][:60])
    except Exception as e: print(f"✗ 模型不可用（查配置/apiKey）：{e}")
```

## 排错

| 症状 | 处置 |
| --- | --- |
| 401 | 先分清：带 `needs_auth` 是 OAuth 问题（等授权重发），不带是令牌问题（重取） |
| `file://` 页面请求被拦 | 引 SDK 用完整地址 `http://127.0.0.1:PORT/api/open/sdk.js`，`ez.config` 传令牌 |
| 授权后调用仍 401 | 授权全局共享但**调用要重发**；仍不行查 mcp-config 的状态排查 |
| model/call 502 | 模型配置/apiKey/额度问题，与令牌无关 |
