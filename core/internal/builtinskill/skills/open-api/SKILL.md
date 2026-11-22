---
name: open-api
description: 调用 ezharness core 的 OpenAPI——直调大模型、调 MCP 工具；写批量脚本、快应用页面/后端、本地服务对接核心能力时用
---

# core OpenAPI

core 在本机暴露 HTTP API（基址见 system `<local_api>` 段，形如 `http://127.0.0.1:5260`），供脚本、快应用、外部程序调用。OpenAPI 是独立面：只认 `EzHarnessOpenToken`，不会弹登录。

## 判定

| 调用方 | 取令牌 | 依赖 SDK |
| --- | --- | --- |
| 终端/子进程脚本 | 调 `create_openapi_token` 工具生成，令牌作为脚本常量/参数 | 否 |
| 外部程序/手写脚本 | 同上；令牌由调用方自己保存 | 否 |
| 快应用后端（推荐交付形态） | 同上；写进应用自己的配置 | 否（裸 HTTP） |
| 纯前端页面（含 `file://` 测试页） | 同上；**明文写进** `ez.config` | 是（`/api/open/sdk.js`） |

策略：令牌就是调用方自己的配置，前后端都可以明文使用；core 不做进程环境注入，也不依赖浏览器会话。

## 做法

**鉴权**：请求头 `EzHarnessOpenToken: <令牌>`。需要调 OpenAPI 时先调 `create_openapi_token` 工具生成一枚（name 可选），工具会直接把令牌返回给你；把令牌显式写进调用代码或配置。不需要调用就不要生成。

**MCP OAuth 是唯一例外**：`/api/open/mcp/call` 返回 `401 + needs_auth` 表示该 MCP 服务需要授权，不是 OpenAPI 令牌失效——按 `auth_url` 完成授权后重发原调用；SDK 会自动处理。

**端点**：

| 端点 | 载荷 | 返回 |
| --- | --- | --- |
| `POST /api/open/model/call` | `{prompt, model?(省略=主模型), images?(本机绝对路径≤4张，须视觉模型)}` | `200 {result}`；单次上限 5 分钟不重试 |
| `POST /api/open/image/call` | `{prompt, model?, negative_prompt?, image_size?("1024x1024"), batch_size?(1-4), seed?, wait_seconds?(0-60，默认10)}` | `200 {ok, job}`（异步任务，见下） |
| `POST /api/open/audio/call` | `{input(≤5000字), model?, voice?(短名 alex/anna…), response_format?(mp3/wav/opus/pcm), speed?, wait_seconds?}` | 同上 |
| `POST /api/open/video/call` | `{prompt, model?, duration?(5-15秒), resolution?(480p/768p), first_frame?/last_frame?(URL或本机路径), wait_seconds?}` | 同上（生成需数分钟，响应基本是执行中的 job） |
| `GET /api/open/media/jobs/:id` | — | `{job}`（done 带 files，failed 带 err） |
| `GET /api/open/media/jobs?limit=` | — | `{jobs}`（最近任务，新→旧） |
| `POST /api/open/mcp/call` | `{server, tool, args}`（工具名以清单/schema 为准，勿臆造） | `200 {result}`；未授权 `401+needs_auth` |
| `GET /api/open/mcp` | — | 已启用服务清单 |

生成类（image/audio/video）是**异步任务**：提交后接口短等待（`wait_seconds`，默认 10 秒上限 60），完成直接带产物；未完成返回执行中的 `job`，持 `job.id` 轮询 `media/jobs/:id`（视频隔 1-2 分钟查一次，不要密轮询）。job 形态：`{id, kind, status(queued|running|done|failed), model, brief, createdAt, files[], err, quota}`——`files[].path` 是产物本机绝对路径（已落盘 `<工作目录>/tmp/`），图片/音频带 `b64`、视频带原始 `url`。贵价模型：**每日配额**统一计数（agent 工具与 OpenAPI 同一份，超限 `429`），失败不扣额；配额在 多媒体·模型与限额 可调。

**错误码**：

| 状态 | 含义 | 处理 |
| --- | --- | --- |
| 400 | 参数错误 | 不重试，查载荷 |
| 401（无 needs_auth） | 令牌无效/被吊销 | 重新调 `create_openapi_token` 生成，替换代码/配置里的旧值 |
| 401 + needs_auth | MCP 未授权（仅 mcp/call） | 见模板·授权等待 |
| 502 | 上游失败（模型配置/apiKey/额度；MCP 服务不可达） | 修因后重试 |

**令牌重置的影响面**：令牌被吊销后旧代码里的值失效，重新生成并替换即可；OpenAPI 不随登录/登出变化。

## 模板

curl（其余语言同构：node 用内置 fetch、powershell 用 Invoke-RestMethod，头一样）：

```bash
TOKEN='<create_openapi_token 返回的令牌>'
curl -s -H "EzHarnessOpenToken: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"prompt":"把这段话翻译成英文：你好世界"}' http://127.0.0.1:5260/api/open/model/call
```

python（标准库，批量脚本的基础件）：

```python
import json, urllib.request

TOKEN = "<create_openapi_token 返回的令牌>"

def ez_call(path, body):
    req = urllib.request.Request(f"http://127.0.0.1:5260/api/open/{path}",
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json",
                 "EzHarnessOpenToken": TOKEN})
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
import json, sys, urllib.request, urllib.error

base = "http://127.0.0.1:" + (sys.argv[1] if len(sys.argv) > 1 and sys.argv[1].isdigit() else "5260")
tok = "<create_openapi_token 返回的令牌>"

def call(path, body=None):
    req = urllib.request.Request(base + "/api/open/" + path,
        data=json.dumps(body).encode() if body else None,
        headers={"Content-Type": "application/json", "EzHarnessOpenToken": tok})
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
| 401 | 先分清：带 `needs_auth` 是 OAuth 问题（等授权重发），不带是令牌问题（重新生成一枚） |
| `file://` 页面请求被拦 | 引 SDK 用完整地址 `http://127.0.0.1:PORT/api/open/sdk.js`，`ez.config` 传令牌 |
| 授权后调用仍 401 | 授权全局共享但**调用要重发**；仍不行查 mcp-config 的状态排查 |
| model/call 502 | 模型配置/apiKey/额度问题，与令牌无关 |
