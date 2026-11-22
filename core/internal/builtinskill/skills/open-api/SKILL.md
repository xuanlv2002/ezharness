---
name: open-api
description: 调用 ezharness core 的 OpenAPI——模型直调、MCP、多媒体；脚本、快应用与外部程序统一按这里的认证和数据结构写
---

# core OpenAPI

core 在本机暴露 `http://127.0.0.1:<port>/api/open/*`（基址见 system `<local_api>`）。OpenAPI 与登录完全独立：只认 `EzHarnessOpenToken`，不弹登录，不读 cookie/env。

## 认证

需要调用时先调 `create_openapi_token` 工具生成一枚令牌（name 可选）；不需要调用就不生成。所有请求统一带：

```
EzHarnessOpenToken: <令牌>
```

令牌是应用自己的配置，明文保存即可；被吊销后重新生成并替换。应用统一用下面两种写法，不要另造认证封装。

后端/脚本（python 标准库）：

```python
import json, urllib.error, urllib.request

BASE = "http://127.0.0.1:5260"          # 以 system <local_api> 的基址为准
TOKEN = "<create_openapi_token 返回的令牌>"
HEADERS = {"Content-Type": "application/json", "EzHarnessOpenToken": TOKEN}

def _ez_request(method, path, body=None):
    req = urllib.request.Request(BASE + "/api/open/" + path,
        data=json.dumps(body).encode() if body is not None else None,
        headers=HEADERS, method=method)
    try:
        with urllib.request.urlopen(req, timeout=300) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        return json.load(e)   # 非 2xx 也是 JSON：看 error / needs_auth

def ez_call(path, body):
    return _ez_request("POST", path, body)

def ez_get(path):
    return _ez_request("GET", path)
```

浏览器/快应用（SDK）：

```html
<script src="http://127.0.0.1:PORT/api/open/sdk.js"></script>
<script>
  ez.config({ token: '<create_openapi_token 返回的令牌>' })
  const text = await ez.model.call('…')
  const data = await ez.mcp.call('server', 'tool', args)
</script>
```

SDK 自动带 `ez.config` 设置的令牌、不带 cookie，`file://` 直开可用；MCP OAuth 会自动弹授权并重试。方法：

| 方法 | 返回 |
| --- | --- |
| `ez.model.call(prompt, {model?, images?})` | 文本 |
| `ez.gen.image(prompt, opts)` / `ez.gen.speech(input, opts)` / `ez.gen.video(prompt, opts)` | `{ok, job}` |
| `ez.gen.job(id)` / `ez.gen.jobs()` | job / `{jobs}` |
| `ez.mcp.call(server, tool, args?)` | MCP 结果（已解析） |
| `ez.mcp.authorize(server)` | 仅发起 OAuth 授权 |

## 端点

| 端点 | 请求 | 响应 |
| --- | --- | --- |
| `POST /api/open/model/call` | `{prompt, model?(省略=主模型), images?(本机绝对路径，≤4 张，须视觉模型)}` | `{result}`；单次上限 5 分钟 |
| `POST /api/open/image/call` | `{prompt, model?, negative_prompt?, image_size?, batch_size?(1-4), seed?, wait_seconds?(0-60，默认10)}` | `{ok, job}` |
| `POST /api/open/audio/call` | `{input(≤5000 字), model?, voice?, response_format?(mp3/wav/opus/pcm), speed?, wait_seconds?}` | 同上 |
| `POST /api/open/video/call` | `{prompt, model?, duration?(5-15 秒), resolution?(480p/768p), ratio?, first_frame?, last_frame?, wait_seconds?}` | 同上 |
| `GET /api/open/media/jobs?limit=` | — | `{jobs}`，新→旧 |
| `GET /api/open/media/jobs/:id` | — | `{job}` |
| `GET /api/open/mcp` | — | `{servers}`：名/描述/启用，不含连接配置 |
| `POST /api/open/mcp/call` | `{server, tool, args}`（工具名以清单/schema 为准） | `{result}`；未授权 `401+needs_auth` |
| `POST /api/open/mcp/oauth/start` | `{name, metadata_url?}` | `{url}` |
| `GET /api/open/mcp/oauth/status?name=` | — | `{authorized, pending}` |
| `POST /api/open/mcp/oauth/cancel` | `{name}` | `{ok}` |

生成类是异步任务：提交后短等待 `wait_seconds`，完成直接带产物；未完成返回执行中的 `job`。job 形态：

```json
{
  "id": "…", "kind": "image|audio|video",
  "status": "queued|running|done|failed",
  "model": "…", "brief": "…", "createdAt": 0, "doneAt": 0,
  "files": [{"name": "…", "path": "/绝对路径", "size": 0, "mime": "…", "b64": "…", "url": "…"}],
  "err": "…", "quota": {"kind": "image", "used": 0, "limit": 0}
}
```

`files[].path` 是本机绝对路径（已落盘工作目录的 `tmp/`）；图片/音频带 `b64`，视频带原始 `url`。视频隔 1-2 分钟查一次，不要密轮询。每日配额超限回 `429`，失败不扣额。

## MCP OAuth

OpenAPI 令牌鉴权通过后，MCP 服务本身还可能要求 OAuth。此时 `mcp/call` 回：

```json
{
  "error": "需要 OAuth 认证：…",
  "needs_auth": true,
  "server": "notion",
  "auth_url": "https://…",
  "metadata_url": "https://…"
}
```

处理顺序：

1. 把 `auth_url` 交给用户，在浏览器完成授权（`auth_url` 为空时先 `POST /mcp/oauth/start` 取）。
2. 轮询 `GET /mcp/oauth/status?name=<server>`，直到 `authorized: true`。
3. **重发原 `mcp/call`**。授权是全局的，但调用不会自动重试。
4. 不要因为 `needs_auth` 重新生成 OpenAPI 令牌；它与 OpenAPI 令牌无关。

脚本示例（SDK 已自动处理，无需手写）：

```python
import time

r = ez_call("mcp/call", {"server": S, "tool": T, "args": A})
if r.get("needs_auth"):
    print("请在浏览器完成授权：", r["auth_url"])
    for _ in range(200):                       # 1.5s × 200，上限 5 分钟
        time.sleep(1.5)
        if ez_get(f"mcp/oauth/status?name={S}").get("authorized"):
            r = ez_call("mcp/call", {"server": S, "tool": T, "args": A})
            break
elif "error" in r:
    raise RuntimeError(r["error"])
```

## 错误与排错

| 状态/症状 | 处理 |
| --- | --- |
| 400 | 参数错误，查载荷，不重试 |
| 401（无 `needs_auth`） | OpenAPI 令牌无效/被吊销：重新生成一枚并替换 |
| 401 + `needs_auth` | MCP OAuth：按上面的授权流程处理后重发 |
| 429 | 当日配额超限；不要重试，告知用户 |
| 502 | 上游失败（模型/apiKey/MCP 不可达）：修因后重试 |
| `file://` 页面请求被拦 | SDK 用完整地址 `http://127.0.0.1:PORT/api/open/sdk.js` |
