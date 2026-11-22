# 认证与 OpenAPI

> 两枚令牌、两个信任域：会话令牌驾驶 core，OpenAPI 令牌使用 core 的能力。当前阶段三端用默认密码静默登录，不展示登录界面。

## 一、模型

core 暴露两件东西，各有各的凭证，全部显式请求头：

| | 会话令牌 | OpenAPI 令牌 |
| --- | --- | --- |
| 请求头 | `EzHarnessToken` | `EzHarnessOpenToken` |
| 谁用 | desktop / cli / 浏览器页面（人驾） | agent 产物：脚本、快应用页面/后端、外部程序 |
| 权限 | 全量 `/api/*`（agent 控制、设置、终端、工作区） | 只有 `/api/open/*`（模型直调、MCP 调用、多媒体生成、服务清单、MCP 授权） |
| 来源 | 默认密码 login 签发 | 安全页生成（多枚、可命名、可吊销） |
| 生命周期 | 持久（core 重启不换），多端并存，可吊销 | 持久，随时吊销 |
| 存放 | 浏览器 localStorage；desktop 的 session.json（令牌）；cli 的 session.json（令牌） | `<数据目录>/openapi.json`（0600，多枚清单） |

**默认密码**：core 启动时确保 `<数据目录>/auth.json`（0600）里是默认密码 `ezharness` 的 bcrypt 哈希；缺失、损坏或仍是旧密码就重建哈希并清空旧会话。web / desktop / cli 启动时用默认密码静默换会话令牌，令牌各自持久保存，失效自动重登。安全页可看会话清单（客户端类型、来源 IP、登录时间、会话 id）并吊销任意一端。显式改密码留给远程形态。

**连接地址**：web 与 desktop 固定跟随本机 core（server.json）；cli 默认本机，`--server <主机:端口>` 可显式指定（无端口默认 5260）。core 目前只绑 127.0.0.1，跨机访问留给之后的远程形态（login 的本机 Origin 校验也只放行回环来源）。

**传输适配**（同一凭证，不是第二套认证）：

- 页面 fetch 层所有接口一律带 `EzHarnessToken` 头；令牌存 localStorage（新标签页、重启后仍可带头与建 WS）。
- `<img>`/`<video>`/PDF/页面导航这类浏览器自动发起的请求加不了自定义头——登录响应同时下发 HttpOnly cookie（`ez_token`，SameSite=Strict，与令牌同寿命一年）作资产传输。**cookie 只在只读 HTTP 请求（GET/HEAD：导航、图片、SSE）上生效**；状态变更接口必须带显式请求头，挡住同 host 其他端口页面借浏览器发起的 CSRF（cookie 按 host 生效、不看端口，SameSite 也挡不住跨端口）。
- WebSocket 握手虽然是 GET，但连上能发命令，**不接受 cookie**：浏览器 WS 不能设自定义头，令牌走子协议 `new WebSocket(url, ["agent-auth", token])`（core 要求 `agent-auth` 排首位、令牌为第二个条目，校验后回显 `agent-auth`）；desktop 桥这类非浏览器客户端走 `EzHarnessToken` 头。
- `/api/auth/login` 只接受本机页面发起：请求带 `Origin` 时其 host 必须是回环地址且与 `Host` 一致（cli/desktop 主进程不带 `Origin`，直接放行）。否则任意网站都能用一个无需预检的跨站 POST 换会话令牌。

## 二、路由两面

```
免鉴权面（全量清单）：
  GET  /api/app/health        探活 {ok, boot, version}（入口程序附着判断）
  POST /api/auth/login        默认密码换会话令牌（三端启动时静默调用）
  GET  /api/open/sdk.js       SDK（本身无秘密）
  GET  /apps/*                快应用静态页（页面无秘密，令牌在页面自己的调用里）

/api/*        会话面：EzHarnessToken 头 / WS 子协议，只读请求另认 cookie；跨源不放行
/api/open/*   OpenAPI 面：EzHarnessOpenToken 头或会话凭证任一命中，跨源放行（CORS *）
```

OpenAPI 面放行跨源是为了 `file://` 直开的页面与外部脚本——浏览器对 `file://` 的 origin 判为 `null`，先发 CORS 预检；gin 按 method 匹配路由，预检必须显式挂 OPTIONS 通配路由。跨源请求只认令牌头（`ACAO *` 与 cookie 凭证互斥）；同源页面不受此限，cookie 仍可按上面的只读规则参与鉴权。

会话凭证在 OpenAPI 面同样有效：ezharness 前端与同源页面无需 OpenAPI 令牌。

## 三、端点

| 端点 | 作用 |
| --- | --- |
| `GET /api/auth/sessions` · `DELETE …/:id` · `POST /api/auth/logout` | 会话清单 / 吊销指定端 / 登出当前端（安全页） |
| `GET/POST /api/security/openapi-tokens` · `DELETE …/:id` | 令牌清单 / 生成命名令牌 / 吊销（安全页） |
| `GET /api/security/openapi-audit?limit=` | 最近 OpenAPI 调用审计（倒序） |
| `POST /api/open/model/call` | 直调大模型（`{prompt, model?, images?}` → `{result}`） |
| `POST /api/open/image|audio|video/call` | 文生图 / 语音合成 / 视频生成（异步任务，`wait_seconds` 默认 10、上限 60） |
| `GET /api/open/media/jobs` · `GET …/jobs/:id` | 最近任务清单 / 任务详情（done 带产物路径） |
| `POST /api/open/mcp/call` | 调 MCP 工具（`{server, tool, args}` → `{result}`；未授权回 `401 + needs_auth`） |
| `GET /api/open/mcp` | 已启用服务清单（名/描述/启用，不含连接配置） |
| `POST /api/open/mcp/oauth/start` · `GET …/status` · `POST …/cancel` | MCP OAuth：SDK/脚本侧发起，授权本身由用户在浏览器完成 |

MCP 管理（配置读写、连接、探测、注销）留在会话面——openapi 令牌持有者不可见连接细节（env/header 可能带密钥）。

## 四、审计

仅 `/api/open/*`：每笔调用（含被拒的 401）一行 jsonl 落 `<数据目录>/audit/openapi.jsonl`——时间、端点、令牌 id/名、状态码；超过 5MB 轮转为 `.1`（只留一代）。安全页倒序查看最近 100 条。会话面不审计。

## 五、SDK

`GET /api/open/sdk.js` 由 core 伺服（随 core 版本走，不依赖前端构建）：

```html
<script src="http://127.0.0.1:PORT/api/open/sdk.js"></script>
<script>
  ez.config({ token: 'openapi 令牌' })   // 无脑传、永远传
  const text = await ez.model.call('…')
  const data = await ez.mcp.call('notion', 'search', { q: 'hi' })
  const { job } = await ez.gen.image('一张产品海报…', { image_size: '1024x1024' })
</script>
```

- 基址从脚本自身的加载地址推导：`file://` 直开、远程页面都能用
- 令牌只经 `ez.config` 进入（请求头 `EzHarnessOpenToken`），SDK 文件本身无秘密，所以敢免鉴权开放伺服
- 终端与子进程环境里恒有 `EZHARNESS_OPENAPI_TOKEN`（最新一枚，吊销后失效）；正式做法是读 `openapi.json` 拿实时清单
- 快应用协议本身不出现令牌、鉴权、同源——协议只约定文件组织；对接 core 能力的方法全部在 open-api 技能里

## 六、边界与演进

- core 一律绑 127.0.0.1；OpenAPI 令牌泄露的代价上限是"烧 LLM 额度、消耗多媒体配额 + 免审批调 MCP"，吊销即止损；生成另有每日配额封顶
- 格式化 = 恢复首次运行状态：auth.json 与 openapi.json 一并清空；core 重建默认密码，页面刷新后自动登录
- 远程门户是预留方向：开了也只放行登录与 `/api/open/*`，其余一概不露——本模型不需要为此改动

---

上一篇：[工具快查](external.md) · 下一篇：[计划工具](plan设计.md)
