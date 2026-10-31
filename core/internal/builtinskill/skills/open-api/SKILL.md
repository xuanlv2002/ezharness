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

## 错误码语义

| 状态 | 含义 | 处理 |
| --- | --- | --- |
| 200 | 成功 | — |
| 400 | 参数错误（载荷不合法/缺字段） | 不重试，查载荷 |
| 401（无 `needs_auth`） | **令牌**问题：无效或已被重置 | 重取令牌（env/文件）再试一次；仍 401 让用户去设置页查 |
| 401 + `needs_auth: true` | **OAuth** 问题：该 MCP 服务未授权（仅 mcp/call） | 不是令牌问题；见下节「脚本拿到 needs_auth」 |
| 502 | 上游失败：model/call 是模型配置/apiKey/额度，mcp/call 是服务不可达或工具执行失败 | 修因后可重试 |

## 脚本拿到 needs_auth 怎么办

脚本/后端**不会自动弹框、不会自动重试**。正确姿势：

```python
import time

# ez_call 见上文 python 示例；ez_get 同款但 GET：
def ez_get(path):
    req = urllib.request.Request(base + "/api/open/" + path,
        headers={"X-EZ-Open-Token": os.environ["EZHARNESS_OPENAPI_TOKEN"]})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)

r = ez_call("mcp/call", {"server": S, "tool": T, "args": A})
if r.get("needs_auth"):
    print(f"请在浏览器完成授权：{r.get('auth_url')}")
    # 轮询等用户授权（SDK 同款节奏：1.5s 一次，上限 5 分钟），授权全局共享
    for _ in range(200):
        time.sleep(1.5)
        st = ez_get(f"mcp/oauth/status?name={S}")
        if st.get("authorized"):
            r = ez_call("mcp/call", {"server": S, "tool": T, "args": A})  # 重发同一个调用
            break
    else:
        raise SystemExit("授权超时")
```

要点：`auth_url` 交给**用户本人**在浏览器打开（脚本不要自己去跑授权流程）；授权完成后**重新发起同一个调用**。

## 连通性自检（ez_check.py，新任务直接复用）

```python
#!/usr/bin/env python3
"""ezharness 连通性自检：core 可达 / 令牌有效 / MCP 清单 / （可选）模型可用
用法：python3 ez_check.py [port] [--model]   # port 默认 5260；--model 附带一次最小模型调用"""
import json, os, sys, urllib.request

port = sys.argv[2] if len(sys.argv) > 2 and sys.argv[1].isdigit() else (sys.argv[1] if len(sys.argv) > 1 and sys.argv[1].isdigit() else "5260")
base = f"http://127.0.0.1:{port}"
tok = os.environ.get("EZHARNESS_OPENAPI_TOKEN")
if not tok:
    try:
        tok = json.load(open("openapi.json"))["token"]  # cwd=数据目录时
    except Exception:
        sys.exit("取不到令牌：环境变量 EZHARNESS_OPENAPI_TOKEN 或 <数据目录>/openapi.json")

def call(path, body=None):
    req = urllib.request.Request(base + "/api/open/" + path,
        data=json.dumps(body).encode() if body else None,
        headers={"Content-Type": "application/json", "X-EZ-Open-Token": tok})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)

try:
    servers = call("mcp")["servers"]
    print(f"✓ core 可达，令牌有效；MCP {len(servers)} 个：", ", ".join(s["name"] for s in servers) or "（无）")
except urllib.error.HTTPError as e:
    sys.exit(f"✗ 令牌无效或 core 异常：HTTP {e.code}（401=重置过，重取令牌）")
except Exception as e:
    sys.exit(f"✗ core 不可达：{e}")

if "--model" in sys.argv:
    try:
        r = call("model/call", {"prompt": "回复 ok 两个字母即可"})
        print("✓ 模型可用：", r["result"][:60])
    except Exception as e:
        print(f"✗ 模型不可用（查模型配置/apiKey）：{e}")
```

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

## 后端模式（默认交付形态）

**默认策略：调试期允许硬编码，交付期切后端。** `file://` 原型、一次性脚本硬编码令牌没问题；要长住、要复用、会给别人的应用一律加后端——后端进程从 env（core 自动继承）或 `openapi.json` 实时取令牌，页面只请求自己的后端。链路：页面 → 后端 → core。令牌不出后端、不进页面源码。

**令牌重置的影响面**（设置页重置后旧令牌立即作废）：

| 产物 | 影响 | 谁负责恢复 |
| --- | --- | --- |
| 纯前端页面（硬编码） | ezharness 内打开仍可用（cookie 通道）；拿到外面失效 | 重新生成页面换新令牌 |
| 后端应用 | **重启后端进程**才恢复——已运行进程的 env 是启动时继承的旧值；重启后自动取到新值，无需改代码 | 提示用户重启后端 |
| 终端里跑的脚本 | 已开终端的 env 也是旧值；**重开终端**即新 | 重开终端或改读 openapi.json（文件永远新鲜） |

## MCP 授权（OAuth）

- `mcp/call` 返回 `401 + needs_auth` 不是程序错误：该服务需要**用户本人**在浏览器完成 OAuth 授权
- 脚本/后端不要替用户跑授权流程：输出一行提示让用户在 MCP 设置页或会话弹框完成授权（授权全局共享），稍后重试即可
- 浏览器页面走 SDK 自动弹框（见上）
