---
name: callback
description: 管理定时回调——按计划向当前会话发消息、启动快应用并调用 app.quick 声明的端点，或执行工作目录里的 shell 命令（callback 工具与 callbacks.json 的唯一入口）
---

# 定时回调

定时回调是 ezharness 的全局回调中心（页面左侧「回调」，与对话、快应用平级）。任务存在数据目录的 `callbacks.json`，由 `callback` 工具增删改查；**不要用文件工具手改**。

三类任务：

| kind | 用途 | 目标 |
| --- | --- | --- |
| `session` | 到点向会话发消息 | `mode=session` 发到创建时的当前会话；`mode=new` 每次触发新建会话 |
| `app` | 到点触发快应用 | `app.quick` 声明的 `callbacks[].name`；自动启动后端并调用端点 |
| `script` | 到点执行 shell 命令 | `script.command` 在工作目录执行；输出尾部与退出码进记录 |

## 判定

| 情形 | 做法 |
| --- | --- |
| 用户要「每天/每隔 N 分钟提醒我…」「到点让这个会话继续做…」 | 建 `session` + `mode=session`（绑定当前会话） |
| 每次触发都要从全新上下文开始 | 建 `session` + `mode=new` |
| 用户要定时跑某个快应用的后端能力 | 应用已声明 `callbacks` → 建 `app` 任务；未声明先补声明 |
| 用户要定时跑工作目录里的脚本或命令 | 建 `script`，`command` 写完整命令（`./sync.sh`、`python tools/sync.py`） |
| 只是一轮内等待（等构建、等后端就绪） | 用 `sleep`，不要建定时任务 |
| 要分多步执行方案 | 先 `plan` 再建任务；任务只负责到点发消息，不替用户确认计划 |
| 一次性提醒 | `schedule.kind=once`（触发后自动停用） |

`session` 只有两种模式：`session` 绑定**调用工具时的当前会话**（页面创建时绑定当前活动会话），不提供任选其他会话的入口；`new` 每次触发新建会话。当前会话模式只认创建时的叶 ID。它正在跑轮时，发送走 chat 的打断逻辑（先取消再发送）；一旦该会话归档或 compact 换代，任务失败，**不会继承到新会话**，需要重新创建。

## 做法

**工具**：`callback`，action：

| action | 必填 | 说明 |
| --- | --- | --- |
| `list` | — | 看全部任务、下次时间、上次结果；绑定当前会话的标「本会话」 |
| `create` | `title`、`kind`、`schedule`、对应目标 | 创建；`enabled` 缺省 true |
| `update` | `id` + 至少一个待改字段 | 只改传入字段；改 kind 时同时传新目标 |
| `delete` | `id` | 删除任务与触发记录 |
| `enable`/`disable` | `id` | 启停；启用一次性任务时时间必须仍在未来 |
| `run` | `id` | 立即执行一次，**不推进计划**（用于验证） |

**schedule**（本机时区）：

| kind | 字段 | 示例 |
| --- | --- | --- |
| `once` | `at` | `2026-11-01 09:00` |
| `interval` | `every`（Go duration，≥10s） | `30m`、`2h`、`24h` |
| `cron` | `cron`（5 段：分 时 日 月 周，周日=0） | `0 9 * * *` 每天 09:00；`*/15 * * * *` 每 15 分钟；`0 9 * * 1` 每周一 |

**session 目标**：`{mode:"session"|"new", message}`。

- `session`：绑定调用工具时的当前会话。到点时目标正在跑轮，由 chat 发送逻辑先打断当前轮再发送；归档或 compact 换代后任务失败，不会写到新会话。
- `new`：每次触发新建一条会话再发送，适合「每次从干净上下文开始」的例行任务。

**app 目标**：`{name, callback}`。`callback` 必须是 `app.quick` 的 `callbacks[].name`；先用 `load_app(name)` 看声明与端点说明，不要臆造名字。执行器在应用声明了 `backend` 时先确保后端启动，再调用端点；端点后可以是 HTTP 接口，也可以由脚本/进程提供。方法的 URL/headers/body 都以应用声明为准。

**script 目标**：`{command}`。命令交给本机 shell，在 agent 工作目录（terminal 默认目录）执行；Unix 用 `$SHELL -c`，Windows 用 `cmd /C`。脚本文件本身放工作目录，命令里用相对路径。退出码 0 成功、非 0 失败；合并输出保留末尾 64 KB，运行记录存输出尾部；单次上限 10 分钟，超时会结束进程树。

**执行语义**：
- 每次触发（成功/失败/跳过）都追加一条运行记录：触发时间、目标、投递内容、结果；页面「回调记录」与任务卡最近一次可见。
- 快应用触发额外进通知栏（成功/失败都通知），点通知打开「回调」页；脚本任务不进通知栏，结果看 `lastResult` 与页面记录。
- 按计划触发的一次性任务执行后自动 `enabled=false`（手动 `run` 不推进计划、不停用）；间隔/cron 从完成时刻重新计下一轮。
- 应用停机期间错过的间隔/cron 不补跑；错过的一次性任务标记 `missed` 并停用。
- 任务结果同时写在 `lastStatus`/`lastResult`，`list` 可见。

## 模板

会话每日任务（发给当前会话）：

```json
{"action":"create","title":"每日总结","kind":"session",
 "schedule":{"kind":"cron","cron":"0 21 * * *"},
 "session":{"mode":"session","message":"总结今天的工作并列出明天要做的三件事"}}
```

每次新建会话：

```json
{"action":"create","title":"竞品早报","kind":"session",
 "schedule":{"kind":"interval","every":"24h"},
 "session":{"mode":"new","message":"搜集竞品动态，给出一页摘要"}}
```

调用快应用端点：

```json
{"action":"create","title":"每小时同步","kind":"app",
 "schedule":{"kind":"interval","every":"1h"},
 "app":{"name":"syncer","callback":"sync"}}
```

一次性提醒：

```json
{"action":"create","title":"明早提醒","kind":"session",
 "schedule":{"kind":"once","at":"2026-11-02 09:00"},
 "session":{"mode":"session","message":"提醒我提交周报"}}
```

每天执行工作目录里的脚本：

```json
{"action":"create","title":"每日同步","kind":"script",
 "schedule":{"kind":"cron","cron":"0 9 * * *"},
 "script":{"command":"./sync.sh"}}
```

## 验证

- `callback list`：确认任务 ID、下次时间、启用状态与目标正确。
- `callback run <id>`：立即验证一次；稍后 `list` 看 `lastStatus`/`lastResult`。页面「回调记录」可看触发时间、目标、内容与结果。
- 会话任务：去目标会话确认新消息；目标正在跑轮时应看到上一轮被取消。
- 快应用任务：看 HTTP 状态、应用日志；通知栏应出现一条「快应用触发」。
- 脚本任务：`lastResult` 应有「退出码 0」与实际输出；失败时先看退出码与输出尾部。
- 归档场景：归档目标会话后 `list` 仍显示任务，但下一次触发失败并记录「已归档/已换代」；需要继续就重新创建任务。
- 端点未声明：先在 `app.quick` 的 `callbacks` 里声明并用 `load_app` 确认，再建任务。

## 排错

| 症状 | 处置 |
| --- | --- |
| `create` 报「当前没有可绑定的会话」 | 先打开一条会话再建 `mode=session`，或改用 `mode=new` |
| `create` 报「会话 … 不存在」 | 当前会话没有可用存档；新建一条会话，或改用 `mode=new` |
| 任务失败提示「目标会话已归档 / 已换代」 | 该叶会话已归档或被 compact 替换；重新创建任务绑定当前会话 |
| `create` 报「一次性时间已过」 | 改 `at` 到未来，或改用 interval/cron |
| `create` 报「快应用 … 未声明回调 …」 | `load_app(name)` 看 callbacks 声明；错误里的「可用」列表就是当前端点名 |
| 任务失败提示「正在归档」 | 等归档结束后重新触发；或等会话可用后 `run` 手动验证 |
| 任务 `missed` | 一次性时间在应用停机期间已过；改时间后 `enable` |
| 端点连接失败 | 确认应用 `backend` 能启动、端口与声明 URL 一致；看任务 lastResult 的具体错误 |
| 脚本失败或输出为空 | 看 `lastResult` 的退出码与输出尾部；命令在 agent 工作目录执行，文件路径写相对工作目录；单次超过 10 分钟会被结束 |
| 一次性任务跑完不再触发 | 这是预期：执行后自动停用（任务仍在列表）；需要重复执行请改用 interval/cron |
| 修改后没生效 | 确认使用 `callback update`；不要手改 `callbacks.json`，也不要在 system 清单里猜配置 |
