---
name: open-api
description: 调用 ezharness core 的 OpenAPI（直调大模型、调 MCP 工具）——写批量脚本、快应用页面/后端、本地服务对接核心能力时用
---

# core OpenAPI

core 在本机暴露 HTTP API，供脚本、快应用、外部程序调用，所有调用方同一套认证。

## 基址与令牌

- 基址 `http://127.0.0.1:<port>`（端口见 system 提示 `<workspace>` 段；服务只绑本机）
- 令牌取法（二选一）：
  - 终端/快应用后端等子进程：环境变量 `$EZHARNESS_OPENAPI_TOKEN`（Windows `%EZHARNESS_OPENAPI_TOKEN%`），core 自动继承
  - 外部程序：读 `<数据目录>/openapi.json`（0600）的 `token` 字段；数据目录绝对路径见 system 提示 `<workspace>` 段
- 请求头：`X-EZ-Open-Token: <令牌>`
- 令牌持久有效，直到用户在设置页重置。收到 401 先怀疑令牌被重置：重新取令牌再试；不要让用户把令牌贴进对话，指引用户去设置页查看
- 令牌可以硬编码进页面/脚本（纯前端产物唯一的办法）；代价是重置后失效，产物要重新生成。长命产物改用后端模式（见下）

## 端点

### POST /api/open/model/call — 直调大模型

```json
{ "prompt": "提示词", "model": "可省略=主模型", "images": ["可选：本机图片绝对路径，最多 4 张，模型须支持视觉"] }
```

- `200 {"result":"输出文本"}`；非 2xx `{"error":"原因"}`
- 单次上限 5 分钟，失败不重试
- 适合翻译/总结/分类/识别/生成这类一次问答；多步推理走会话，别硬塞进一次 prompt

### POST /api/open/mcp/call — 调 MCP 工具

```json
{ "server": "服务名", "tool": "工具名", "args": {} }
```

- `200 {"result":"文本"}`；失败 `{"error":"原因"}`
- 服务未授权时返回 `401 + {"needs_auth":true,"server":...,"auth_url":...}`
- 工具名与参数不要臆造：先 `GET /api/open/mcp` 拉服务清单，以工具 schema 为准

### GET /api/open/mcp — 已启用 MCP 服务清单（名/描述/启用）

## 调用示例（零依赖，令牌一律从环境变量取）

curl：

```bash
curl -s -H "X-EZ-Open-Token: $EZHARNESS_OPENAPI_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"把这段话翻译成英文：你好世界"}' \
  http://127.0.0.1:5260/api/open/model/call
```

python（标准库）：

```python
import json, os, urllib.request

def ez_call(path, body):
    req = urllib.request.Request(
        f"http://127.0.0.1:5260/api/open/{path}",
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json",
                 "X-EZ-Open-Token": os.environ["EZHARNESS_OPENAPI_TOKEN"]},
    )
    with urllib.request.urlopen(req, timeout=300) as r:
        return json.load(r)

print(ez_call("model/call", {"prompt": "总结：..."})["result"])
```

node（内置 fetch）：

```js
const r = await fetch('http://127.0.0.1:5260/api/open/model/call', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'X-EZ-Open-Token': process.env.EZHARNESS_OPENAPI_TOKEN,
  },
  body: JSON.stringify({ prompt: '总结：…' }),
})
const { result } = await r.json()
```

powershell：

```powershell
Invoke-RestMethod -Uri http://127.0.0.1:5260/api/open/model/call -Method Post `
  -Headers @{ 'X-EZ-Open-Token' = $env:EZHARNESS_OPENAPI_TOKEN } `
  -ContentType 'application/json' -Body '{"prompt":"hi"}'
```

（端口号以 system 提示为准，示例里的 5260 只是默认值。）

## 批量脚本模式

读一批输入 → 循环调 model/call 或 mcp/call → 汇总落盘。要点：串行或小并发（≤4）；失败记录后继续，末尾汇总失败清单；超时给足（model/call 最长 5 分钟）；令牌从 env 取，不写死。

## 浏览器页面：SDK

```html
<script src="http://127.0.0.1:PORT/api/open/sdk.js"></script>
<script>
  ez.config({ token: 'openapi 令牌' })            // 无脑传、永远传
  const text = await ez.model.call('把这段翻译成英文：…')
  const data = await ez.mcp.call('notion', 'search', { q: 'hi' })
</script>
```

- SDK 从自己的加载地址推导基址：`file://` 直接打开的页面、远程页面都能用
- `ez.model.call(prompt, {model?, images?})` → 输出文本
- `ez.mcp.call(server, tool, args?)` → 工具结果；需要 OAuth 的服务自动弹授权框，用户在浏览器完成授权后自动重试本次调用；`ez.mcp.authorize(server)` 只发起授权不调用
- 跨源已放开（`file://`、远程页面直调无障碍）；在 ezharness 里同源打开的页面还自动带 cookie——两通道任一命中即过，所以硬编码令牌被重置的页面在 ezharness 里仍可正常调用，只有拿到 ezharness 之外才会失效
- 写完页面用 `file://` 直接打开即可测试（SDK + 硬编码令牌就能调通），不必先装成快应用

## 后端模式（长命应用首选）

需要长期可靠调核心能力的应用加一个后端：后端进程从 env（core 自动继承）或 `openapi.json` 实时取令牌，页面只请求自己的后端。链路：页面 → 后端 → core。令牌不出后端、不进页面源码，重置令牌后端无感（下次取就是新的）。

## MCP 授权（OAuth）

- `mcp/call` 返回 `401 + needs_auth` 不是程序错误：该服务需要**用户本人**在浏览器完成 OAuth 授权
- 脚本/后端不要替用户跑授权流程：输出一行提示让用户在 MCP 设置页或会话弹框完成授权（授权全局共享），稍后重试即可
- 浏览器页面走 SDK 自动弹框（见上）
