---
name: quick-app
description: 构建/更新快应用——app.quick 协议、save_app 覆盖语义、后端约定与交付清单（需要本机命令能力、MCP 覆盖不了时加后端）
---

# 快应用构建

快应用 = `<数据目录>/apps/<名>/` 目录（绝对路径见 system `<workspace>` 段）。目录里有 `app.quick` 才算一个应用；用户点启动 = **打开应用窗口**，声明了 `backend` 就在内置终端把后端跑起来。

快应用是一套**与宿主无关的打包标准**：只约定文件组织，不约束语言技术栈；页面要用大模型/MCP 能力时的调用方法见 open-api 技能。

## 判定

| 情形 | 形态 |
| --- | --- |
| 展示、计算、一次 HTTP 能拿到的能力（模型直调/MCP，见 open-api 技能） | 纯前端（`file://` 直开即可测） |
| 要长期可靠调核心能力、要本机命令/文件/长任务、MCP 覆盖不了 | 加后端 |
| 多步应用（形态/数据通道/结构/头像/验证） | **先 plan 再动手**（`action=new` 写清方案与 3-6 步 progress，批准后执行；一次 save_app 能写完的小页面不用 plan） |

## 做法

**app.quick 字段**：

| 字段 | 规则 |
| --- | --- |
| `title` | 卡片标题；缺省取 entry 的 `<title>`，再缺省目录名 |
| `entry` | 前端入口，缺省 `index.html` |
| `backend` | 启动命令（cwd=应用目录）；**省略=纯前端** |
| `icon` | 卡片头像，应用目录内文件名 |
| `description`/`version`/`document` | 一句话描述 / 语义化版本 / 文档路径（缺省 APP.md） |
| `callbacks` | 定时调用端点声明（数组；见下），供回调中心按 `name` 引用 |
| 未识别字段 | 一律忽略（向前兼容） |

**save_app 是整页重写，不是增量更新**（最易翻车）：

- 每次调用都用 `html` 参数**覆盖** `index.html`——先 write_file 写了完整页面再 save_app 传占位 html，页面就没了
- 每次调用都**全量重建** `app.quick`——`document`/`description`/`version`/`backend`/`icon`/`callbacks` 省略哪个哪个就消失；**document 首次就传**

正确顺序：**先** `save_app`（html 传完整内容，大页面用 `<@toolArg>` 引草稿）→ **再** `write_file` 补后端脚本、icon 等其余文件 → 后续增量更新用 `write_file`/`edit_file`，**整页重建才再调 save_app**（清单字段全带上）。

**头像**：建应用顺手给 `icon.svg`——128×128 视图框、透明底、主题色大色块 + 1-2 个字符/几何图形，16px 仍可辨识；不要插画照片密集文字。没给回落默认图标。

**后端约定**：

| 约定 | 内容 |
| --- | --- |
| 运行 | cwd=应用目录，**前台**（不 `start /b`、不 `&`）；停=终端 Ctrl+C |
| 端口 | 自定高位端口，页面同值；**启动先检测占用**——被占打印「可能已在运行（去卡片打开）或先停旧进程」后干净退出，不抛栈 |
| 依赖 | 只用用户机器已有运行时，不装依赖 |
| 跨源 | 后端独立端口=跨源，须回 `Access-Control-Allow-Origin: *` |
| 互为依赖 | 应用本身是别的应用的 MCP/数据源时：由它自己的 backend 启动（用户点它的卡片）；依赖方**查 `GET /api/open/mcp` 清单**判断在不在，不探端口，不在就提示用户启动 |

**定时回调端点**（`callbacks`，需要被「回调」模块定时调用时声明）：

| 字段 | 规则 |
| --- | --- |
| `name` | 稳定标识，应用内唯一（字母/数字/`-_.`），回调任务按它引用 |
| `method` | HTTP 方法，缺省 `POST`；支持 GET/POST/PUT/PATCH/DELETE |
| `url` | 绝对 `http(s)` 地址（通常是应用后端的本机端口） |
| `headers`/`body` | 附加请求头 / JSON 请求体（可选；回调只按声明调用） |
| `title`/`description` | 展示用名称与说明（可选） |

声明只描述接口，不带计划；定时由 `callback` 工具创建任务（`kind=app`、`callback=<name>`）。触发时执行器会自动启动应用后端（若声明了 `backend`），再调用端点，并在连接失败时重试约 15 秒。端点后可以是 HTTP 接口，也可以由脚本/进程提供；方法的 URL/headers/body 都以声明为准，任务不覆盖。

```json
"callbacks": [
  {
    "name": "daily-digest",
    "title": "每日摘要",
    "method": "POST",
    "url": "http://127.0.0.1:18731/api/digest",
    "body": {"days": 1}
  }
]
```

**版本**：每次交付 `version` 递增 + `CHANGELOG.md` 追加（版本、日期、摘要）。

## 模板

APP.md 骨架（六要素一个不能少；不需要 agent 调用的应用**不写「Agent 调用」节**）：

```markdown
---
name: certprobe
description: 探测域名 HTTPS 证书链与到期时间
version: 1.0.0
---
# 能力
一句话：输入什么，产出什么。

# Agent 调用
入口：（window.certprobe.probe({...}) / POST http://127.0.0.1:18731/api/probe / CLI）
输入：参数名、类型、必填
输出：返回结构与示例
副作用：只读 / 写哪里 / 起进程
失败语义：reject / 非 2xx {"error":...} / 退出码
就绪条件：后端需先启动；首次调用按 3 次×300ms 重试
```

就绪检查（点启动立刻开窗、后端可能没监听——统一轮询 `/health` 而不是拿业务请求试错；后端 `/health` 顺便探 core 连通）：

```js
async function waitReady() {
  for (let i = 0; i < 30; i++) {          // 30×500ms，覆盖 15s 冷启动
    try { if ((await fetch('/health')).ok) return } catch {}
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error('后端 15 秒内未就绪')
}
```

## 验证

- 已有应用：清单在 system `<apps>` 段，`load_app` 读调用文档；不凭 `app.quick` 猜调用方式
- OpenAPI/MCP 状态用 open-api 技能的自检脚本；页面按就绪检查范式
- 定时端点：用 `callback` 工具 `create`（kind=app、callback=声明名）后 `list` 确认下次时间，`run` 立即验证一次（结果看任务 lastResult 与页面「回调记录」；成功/失败都会进通知栏）
- **交付必附运行清单**（这些信息只在对话里，不写下来下次谁也找不到）：

```
运行中：
- certprobe 后端  127.0.0.1:18731  终端 t1  停止：终端 Ctrl+C
入口：app://certprobe（卡片启动）
```

## 排错

| 症状 | 处置 |
| --- | --- |
| 卡片启动后端崩溃（Address in use） | 手动终端里调试跑过后端占着端口——Ctrl+C 释放再点卡片，或留在终端跑不再点卡片 |
| 同名再点启动没反应 | 后端已在跑时只开窗聚焦；重启=先停旧进程再点 |
| 换代后后端没了 | 换代连带回收终端与后端，重新点启动 |
| 定时端点调用失败 | 看任务 lastResult：连接失败先确认后端能启动、URL 端口与 `backend` 一致；404 检查 `app.quick` 的 `callbacks[].url` 与服务端路由 |
| save_app 后又丢 callbacks | save_app 整页重建，更新时把 `callbacks` 数组原样带上 |
| 页面改了没生效 | 增量更新走 write_file/edit_file；整页重建才 save_app（字段全带） |
