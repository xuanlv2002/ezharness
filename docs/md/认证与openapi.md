# 认证与 OpenAPI

> 两枚令牌、两个信任域：会话令牌驾驶 core，OpenAPI 令牌使用 core 的能力。

## 一、模型

core 暴露两件东西，各有各的凭证：

| | 会话令牌 | OpenAPI 令牌 |
| --- | --- | --- |
| 谁用 | 桌面壳 + 网页前端（人驾） | agent 产物：脚本、快应用页面/后端、外部程序 |
| 权限 | 全量 `/api/*`（agent 控制、设置、终端、工作区） | 只有 `/api/open/*`（模型直调、MCP 调用、服务清单、MCP 授权） |
| 生命周期 | 每次启动新生成，只活在进程内；不落盘、不下发子进程 | 持久 `<数据目录>/openapi.json`（0600）；设置页手动重置 |
| 怎么到手 | desktop 经 `EZHARNESS_TOKEN` 环境变量注入；web 直跑打印一次性入口 URL 换 cookie | 子进程环境变量 `EZHARNESS_OPENAPI_TOKEN`（core 自动继承）；外部程序读文件 |
| 怎么带 | `X-EZ-Token` 头 / `ez_token` HttpOnly cookie / WS 子协议 | `X-EZ-Open-Token` 头 |
| 泄露后果 | 全机被驾驶——所以短命 | 烧 LLM 额度 + 免审批调 MCP——窄权限，一键重置 |

会话令牌是"登录态"：浏览器自动发起的请求（导航、`<img>`、PDF）加不了自定义头，所以前端用入口令牌换一次 HttpOnly cookie（`SameSite=Strict`），这是同一凭证的浏览器传输适配，不是第二套认证。

OpenAPI 令牌是"能力态"：它永远过不了会话面（路由层硬隔离），拿到它也只能调模型与 MCP。

## 二、路由两面

```
/api/*        会话面：会话凭证（X-EZ-Token / cookie / WS 子协议），跨源不放行
/api/open/*   OpenAPI 面：X-EZ-Open-Token 或会话凭证任一命中，跨源放行（CORS *）
/api/open/sdk.js   SDK：无秘密，免鉴权、跨源放行
```

OpenAPI 面放行跨源是为了 `file://` 直开的页面与外部脚本——浏览器对 `file://` 的 origin 判为 `null`，先发 CORS 预检；gin 按 method 匹配路由，预检必须显式挂 OPTIONS 通配路由。鉴权只认令牌头，不放开 cookie 凭证（`ACAO *` 与凭证互斥）。

会话凭证在 OpenAPI 面同样有效：ezharness 前端与同源页面无需 OpenAPI 令牌。于是"cookie 覆盖页面硬编码令牌"的语义自然成立——请求层两通道并存、任一命中即过，页面硬编码的令牌即使已被重置，同源下凭 cookie 仍可调用，拿到 ezharness 之外才会失效。

## 三、端点

| 端点 | 作用 |
| --- | --- |
| `POST /api/open/model/call` | 直调大模型（`{prompt, model?, images?}` → `{result}`） |
| `POST /api/open/mcp/call` | 调 MCP 工具（`{server, tool, args}` → `{result}`；未授权回 `401 + needs_auth`） |
| `GET /api/open/mcp` | 已启用服务清单（名/描述/启用，不含连接配置） |
| `POST /api/open/mcp/oauth/start` · `GET …/status` · `POST …/cancel` | MCP OAuth：SDK/脚本侧发起，授权本身由用户在浏览器完成 |
| `GET /api/app/openapi-token` · `POST …/reset` | 令牌查看/重置（会话面，设置页用） |

MCP 管理（配置读写、连接、探测、注销）留在会话面——openapi 令牌持有者不可见连接细节（env/header 可能带密钥）。

## 四、SDK

`GET /api/open/sdk.js` 由 core 伺服（随 core 版本走，不依赖前端构建）：

```html
<script src="http://127.0.0.1:PORT/api/open/sdk.js"></script>
<script>
  ez.config({ token: 'openapi 令牌' })   // 无脑传、永远传
  const text = await ez.model.call('…')
  const data = await ez.mcp.call('notion', 'search', { q: 'hi' })
</script>
```

- 基址从脚本自身的加载地址推导：`file://` 直开、远程页面都能用
- 令牌只经 `ez.config` 进入，SDK 文件本身无秘密，所以敢免鉴权开放伺服
- MCP 需 OAuth 时自动弹授权框（Shadow DOM 隔离样式），用户在浏览器完成授权后自动重试本次调用

## 五、快应用的两条接入路径

| 形态 | 链路 | 令牌来源 | 依赖 SDK |
| --- | --- | --- | --- |
| 纯前端 | 页面 → core | 硬编码进 `ez.config`（重置后失效，重新生成） | 是 |
| 有后端 | 页面 → 后端 → core | 后端读 env / `openapi.json`，实时新鲜，重置无感 | 否（后端裸 HTTP） |

快应用协议本身不出现令牌、鉴权、同源——协议只约定文件组织；对接 core 能力的方法全部在 open-api 技能里（那是给 agent 的操作手册）。

## 六、边界与演进

- core 只绑本机；OpenAPI 令牌泄露的代价上限是"烧 LLM 额度 + 免审批调 MCP"，重置即止损
- 会话令牌不出进程：终端与快应用后端等子进程环境里只有 `EZHARNESS_OPENAPI_TOKEN`
- 换代（换数据目录）时 openapi 令牌随新目录的 `openapi.json`（缺则新建）
- 将来做 web 远程访问：会话层换成登录页发 JWT，远程只暴露 `/api/open/*` + 登录，其余一概不露——本模型不需要为此改动

---

上一篇：[MCP](mcp设计.md) · 回到[设计理念](设计理念.md)
