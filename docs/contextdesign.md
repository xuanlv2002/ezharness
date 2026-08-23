# ezharness 上下文策略（当前实现）

> 版本：2026-08-23 · 描述当前代码的真实行为，取代早期设计方案。
> 调试工具：`internal/warp/modeldump` 每轮模型调用前把完整输入打印到后端控制台。

## 0. 总原则

1. **最简上下文**：对话 + session 存档。不装配压缩轮换（rotate）/卸载（offload）/
   contextfix / 话题轮换等扩展（internal/hooks 中相关代码保留但不装配）。
2. **session 对用户屏蔽**：无 newSession 操作；话题翻页（compact）由模型或水位自动完成。
3. **注入一律标签语法**：所有 harness 注入的上下文块用 `<tag>…</tag>` 包裹——
   `<memory>` / `<skills>` / `<mcp>` / `<compact-summary>` / `<agent_status>`。
   全量清单进 system，状态栏只报变更。
4. **工具调用中途对上下文零改动**：任何 hook 不得在工具调用往返之间改写消息历史——
   模型对历史的记忆是它推理的依据，同轮内世界观突变会导致汇总错乱。

## 1. system 的组装（每个 session 一次，同 session 不变）

session 创建时组装一次（compact 创建新 session 时全量重载），之后固定；
恢复的会话从快照还原、不重组。结构：

```
人格（ezharness 是什么、能做什么）
SystemExtra（用户在设置页填写的附加提示，原文拼接）

<memory>
  # 长期记忆
  - 根目录 memory/（工作目录相对），分三个区：
    - memory/longterm/ —— 长期记忆：harness.md 是索引（下方已加载，可直接用文件工具更新），
      主题文件按需创建，不进上下文，用 findstr/grep 检索
    - memory/skills/ —— 能力记忆：沉淀的技能
    - sessions/ —— 话题存档：历史会话全文（compact 后的旧库）
  # 索引（harness.md）
  …harness.md 文件内容…
</memory>

<skills>（有技能时）仅名称与描述；使用前先 load_skill 取全文 </skills>

<mcp>（有启用的 server 时）server 清单；经 mcp_router 调用 </mcp>
```

- **harness.md 自动初始化**：缺失或为空时，`hooks.EnsureHarnessMd` 落盘默认模板
  （索引使用说明）并注入；此后内容完全归 agent 维护。
- 记忆/skill/mcp 的变更**不进当前 session 的 system**——等下个 session 重载，
  过渡期靠状态栏的 `changes` 告知模型。

## 2. 状态栏 `<agent_status>`（每轮一条）

每次用户输入前插入一条 user 消息（插在真实输入之前），字段：

| 字段 | 含义 |
|---|---|
| now | 当前时间 |
| sinceLastOutputMin | 距上次输出分钟数 |
| ctxTokens / ctxWindow | 当前上下文水位 / 窗口 |
| suggestCompact | 水位超 70% 窗口时推荐压缩 |
| changes | 距上轮的 skill/mcp 增删（`+ mcp: time` 格式） |

- **不含** skill/mcp 全量清单（在 system 里）——状态栏管"变了什么"，system 管"有什么"。
- 前端消费：常规状态实时同步右上角状态卡（水位条 + 压缩提示 + 变更）；
  时间线里**仅异常时**（推荐压缩 / 有变更）显示一条细警示行。

## 3. 每轮模型输入的完整构成

```
[system（标签块组装，session 内固定）]
[历史消息（上一轮结束时的完整 state.Messages，含 agent_status 与全部工具往返）]
[<agent_status> 本轮状态（user 消息）]
[本轮用户输入（user 消息）]
```

历史不做任何裁剪/摘要（最简定调）；上下文增长由压缩翻页解决（§4）。

## 4. 压缩翻页（compact）

### 4.1 触发

- **模型自主**：`compact_context` 工具（用户要求换话题、状态栏 suggestCompact 时模型调用）。
- **水位自动**：轮末 `promptTokens > CompactThreshold`（settings.json 配置，`0 = 禁用`）。
  阈值建议留足提前量（约窗口 75%，如 128K 窗口配 96000）——summarize 的输入是全量
  历史拼接，压得太晚会让摘要调用自身溢出，且无解（只能防，不能治）。

### 4.2 流程（工具路径为例）

```
模型调 compact_context（带着完整上下文发出调用）
  ├─ 摘要生成（独立 2 分钟预算的 provider 调用）
  ├─ 挂 pending（新 system 两段备好）——上下文零改动
  ├─ Skip 结果返回 → 模型在【原上下文】里自然汇总收尾（世界观连续）
  └─ 轮末 OnEnd 收尾（finishPending）：
      1. 老库补写完整历史（含本轮触发输入与汇总）并置 archived
      2. topics.json 入档（标题/摘要/路径）
      3. sys.Set（rebuildBase 重读记忆/skill/mcp + <compact-summary> 摘要段）
      4. SetTrace / SetID 切新库；新库落初始快照（messages 空 + 新 system + prev 链）
      5. 内存翻页：state.Messages = [新 system]
      6. 发 session.compact 事件（前端渲染分界线并切换 activeId）
```

- **新 session 空置起步**：首条内容 = 用户的下一轮输入。
- 老库存档的 system pin 压缩前的版本（摘要后的新 system 属于新库）。
- 水位自动路径同一收尾（轮末触发，无剩余迭代）。
- **fork 分身例外**：就地压缩增量（seed 保留 + 增量摘要拼进 fork 内 system）——
  分身只有一轮，必须立即回收，否则爆上下文。

### 4.3 恢复（重启 / 刷新）

- 启动恢复 = mtime 最新且未封存的存档，`WithHistory` + 新 user 消息（恢复 = 新对话，
  不做断点续传）；同时 `SetPrev` 重建 compact 链（上翻懒加载用）。
- 压缩后没说话就重启：新库初始快照保证可恢复到带摘要 system 的空会话。
- **轮进行中重启后端**：本轮整体丢弃（未落盘），回到上一轮完整末态——历史协议
  无孤儿无损坏，重说一遍即可。
- **轮进行中刷新前端**：SSE 回放整轮聚合帧（loop_start/model_end/tool_start/
  tool_end/决策/decision.resolved，跳过增量 chunk），时间线完整重建。

## 5. 存储布局（文件夹即存储）

```
sessions/<id>/
  session.json     可恢复快照：消息历史（剥离 system）、systemPrompt 三段、
                   compact 链引用（prevSession/compactSummary）、archived 标记
  trace.jsonl      otel 风格调用链（turn/model/tool/fork/compact span）
  decisions.jsonl  人机决策记录（审批/回答/规划处置 → 刷新后工具卡徽标）
  forks/<id>/      fork 分身独立存档（剥离 seed 只存增量）
topics.json        话题索引（压缩归档的标题/摘要/路径）
memory/
  longterm/harness.md  长期记忆索引（随 system 加载，agent 维护）
  longterm/*.md        主题记忆文件（不进上下文，grep 检索）
  skills/              能力记忆
```

## 6. 前端时间线与压缩链

- 打开页面只展示最新 session；压缩链用 **pull-to-load** 上翻：置顶下拉（滚轮停顿
  即"松手"），一次拉出一个 session，其底部带 `⇪ 上下文已压缩归档：标题` 分隔线。
- 压缩进行时：session.compact 事件渲染分界线并切换 activeId。
