# 计划（plan）工具与上下文策略

> 现状快照。上下文注入这一块后续要重新设计；本文只记录眼下的真实行为与已知局限，作为重设计的底稿。

## 一、数据与归属

- 文件：`sessions/<rootID>/plan.json`（`hooks.PlanPath`），一个分支根一份计划。
- 结构：三段式，卡片直接按字段分区渲染，不再解析正文里的行：

  ```json
  {
    "goal": "目标：要达成什么（一句话）",
    "approach": "方案：采用的做法、选择理由与关键取舍",
    "progress": [{ "title": "步骤标题", "status": "pending|running|completed" }],
    "updatedAt": "..."
  }
  ```

- 归属按**分支根**（RootID）：compact 换代不丢，分叉各算各的。
- `clear` 写一个空结构；`LoadPlan` 把空/无文件都当「没有计划」（返回 nil）。

## 二、工具动作

`plan` 工具（`internal/tools/plan.go`）三个动作：

| 动作 | 行为 | 默认审批 |
| --- | --- | --- |
| `new` | 校验后提交，**阻塞等用户在计划卡里确认**，确认后才落盘 | 免审（走计划确认卡） |
| `update` | 整表替换 `progress`；`goal` / `approach` 可缺省（保留原值） | 免审 |
| `clear` | 写空计划 | 免审 |

校验（超出直接报错回模型自纠）：

- `goal`：必填（new），≤200 字；
- `approach`：必填（new），10–2000 字；
- `progress`：2–12 步，标题单行 ≤60 字、不重复，状态只认 `pending/running/completed`，最多一个 `running`。

## 三、确认流程（当前实现）

- `plan:new` 默认免审：`domain/toolrules.go` 里是 `LevelAuto`。若用户在安全页把它改成审批（或审批模式选「严格」），通用审批先弹；拒绝即短路，计划确认卡不会出现——与 ask_user 同序。
- `hooks/planconfirm.go` 在审批之后拦截 `plan` 的 `new`：
  1. 发 `plan.request` 事件（Data 是 `*types.ToolCall`，前端解析 `Args` 的 goal/approach/progress）；
  2. 阻塞等待用户处置（`PlanDecision{CallID, Action, Feedback}`）；
  3. **confirm**：原样放行，工具校验、落盘并发 `plan.update`；
  4. **feedback**：短路本次调用，意见作为工具结果回给模型（「按反馈修改后重新 new，未确认前不要执行」）。
- 计划卡**只读**（目标/方案/步骤三段渲染），不支持编辑——要改就提交反馈让模型重做。
- 用户停止本轮时 ctx 取消，等待立刻返回。
- 未决请求进 `Session.pending`，断线/刷新由 `PendingFrames` 回放重建计划卡；同时进通知栏（kind=`plan`，可「确认」或「查看」跳转）。
- 决策回执写 `sessions/<leafID>/decisions.jsonl`，`kind=plan`（`已确认计划` / `已反馈：…`），轮末刷新后显示为工具卡徽标。
- 决策端点：`POST /api/sessions/:id/decisions/plan`。

## 四、上下文注入现状（待重设计）

每轮由 `hooks/remind.go` 注入两段固定记录：

- **轮首** `<agent_status>`（`OnStart`）：在状态行后追加计划——
  - `当前任务计划：目标：<压平成一行，最多 300 字>`
  - `方案：<压平成一行，最多 1200 字>`
  - 每个步骤一行：`- [执行中|已完成|待执行] <标题，最多 160 字>`
  - 有计划就每轮全量注入，与用户这轮说什么、计划是否变化无关。
- **轮尾** `<end_reason>`（`OnEnd`）：运行轮次、时长、结束时间、结束原因。

没有实现的部分（本次明确留待重设计）：

- 进度保鲜：计划多轮不更新时没有任何提醒，模型可能长期停留在旧进度；
- 用户取消/偏离：没有手动取消入口，也没有「用户已停止该计划」的告知；
- 注入按变化/摘要裁剪：目前每轮全量注入，长计划会在多轮里反复占 token。

## 五、前端展示

- **输入框上方 PlanPanel**（`components/PlanPanel.svelte`，只读）：折叠显示目标与完成计数；展开是目标（强调块）/方案/步骤三段。数据源 `store.plan`，由 `plan.update` 事件、history 的 `plan` 字段更新。
- **时间线计划确认卡**（`components/PlanReviewCard.svelte`，经 `DecisionCard` 按 `dtype=plan` 渲染）：目标/方案/步骤只读展示；「确认计划」放行落盘，「提交反馈」把意见交回模型。已解决后与审批卡一致折叠成工具徽标。
- **通知栏**（`components/NoticePanel.svelte`）：`kind=plan` 与人机请求同列，显示目标摘要，可「确认」或「查看 →」跳到时间线卡片。
- **工具卡**（`components/ToolFace.svelte`）：plan 工具调用参数/结果的行内只读展示。

## 六、待重设计清单

1. 注入频率与篇幅：是否只在计划变化时注入、是否只注入摘要/当前步；
2. 进度保鲜：按轮次提醒模型更新 progress 的具体策略；
3. 取消与偏离：用户手动取消计划、取消/偏离后如何告知模型；
4. 完成态：全部 completed 后是否自动归档/清理；
5. 与审批体系的关系：确认卡已迁出通用审批，后续是否还需要审批规则里的 plan 条目。
