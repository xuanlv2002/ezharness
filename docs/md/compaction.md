# 压缩与归档：摘要与记忆方案

两条"把上下文变小"的路径，设计目标刻意不同：

| | **trim（上下文整理）** | **compact（归档换代）** |
|---|---|---|
| 一句话 | **接力棒**：整理完接着干 | **总结沉淀**：收尾翻页 |
| 会话 | 同 session 就地折叠，身份不变 | 换新 session（世代 +1），旧库封存 |
| 触发 | 水位自动（窗口 × TrimPercent%）或模型调 `trim_context` | 仅用户手动（`POST /api/topics/compact`） |
| 摘要导向 | **任务连续性**：四节结构化（已完成/正在做/待办/关键事实） | **长期记忆**：先把值得留的沉淀进记忆文件，再出精炼交接摘要 |
| 产物 | `<context_trim>` marker + `sessions/<id>/progress.md` | `memory/longterm/{user,projects,lessons}.md` 更新 + `<compact-summary>` 段 |
| 产物消费者 | 模型自己（下一轮接续现场） | 未来所有会话（长期记忆）+ 新世代（交接摘要） |

分界判据：**内容后续用不用得上、隔多久用**。本世代还要接着干的 → trim 留在上下文与进度档案；跨会话才可能用、本会话已收尾的 → 归档时沉淀进长期记忆；几乎不再用的 → 只留在会话存档（session.json），靠 `<session>` 块的路径兜底回读。

实现位置：trim 在 `core/internal/hooks/trim.go`；归档在 `core/internal/hooks/archive.go` + `distill.go`；共用的摘要基座在 `hooks/summarize.go`。

---

## 一、trim：结构化接力

### 1.1 流程（`doTrim`）

```
水位超阈值(OnLoop) 或 模型调 trim_context(OnToolStart 登记 pending)
  → OnLoop 串行区统一执行 doTrim
  → 摘要折叠段（summarizeMsgs，主模型，2 分钟预算）
  → normalizeStructuredSummary（四节格式兜底，见 1.3）
  → 整体重写 sessions/<id>/progress.md（失败降级，不阻断）
  → 截成 [head(system/seed), tail(末 4 条), marker]，marker 尾插
```

- **就地截断**：`state.Messages` 立即变短，下一次模型调用即新上下文；折叠段挂 `state.Metadata["trim_folded"]`，`sessionstore` 落盘与 `domain.MergeFull` 把它拼回**磁盘全量**——磁盘档案永远完整，模型的视图只是 `ViewStart`（最后 marker）之后的窗口。
- **kept 属性**记录保留段条数，重启恢复时据此回溯视图起点。
- **fork**：只折叠 SeedLen 之后的增量，seed 归主库。

### 1.2 摘要提示词（四节，连续性导向）

```
你在整理一段 agent 工作对话的早期上下文，为"接下来的你"留存衔接材料。
按固定结构输出，保留四个小节标题，无内容的小节写"无"：
【已完成】已完成事项与关键结果（改了哪些文件、命令产出了什么、得到的结论）
【正在做】当前进行中的事项与进行到哪一步
【待办】已明确但尚未开始或未完成的事项
【关键事实】后续必须知道的信息：用户要求与偏好、已定的技术决策、路径/ID/终端/标签等关键标识
要求：每条一行、信息密度优先；忽略工具原始输出的过程细节与状态栏记录；
输入中若出现此前整理产生的旧摘要，把其内容并入对应小节继续浓缩；总长 400 字以内；
这是自我交接材料，不是给人看的总结。
```

设计要点：
- **四节是硬结构**——"已完成+结果"与"正在做到哪"分开，模型接续时第一眼就知道从哪下手；纯散文摘要（旧方案）经常把进行中事项混进已完成里。
- **读者是模型自己**（"自我交接材料"），所以保留 ID/路径/标识这类机器需要的事实，舍弃给人看的叙事。
- **链式浓缩**：折叠段里可能含旧的 `<context_trim>` marker，提示词明确"旧摘要并入对应小节继续浓缩"——多次整理不滚雪球，信息逐次提纯。

### 1.3 格式兜底：`normalizeStructuredSummary`（summarize.go）

弱模型的格式保证靠解析端兜底，**不靠重试**（再调一次模型换不来格式，只换来延迟）：

- 剥代码围栏（```markdown … ```）
- 四节部分缺失 → 末尾补 `【缺节】\n（无）` 占位
- 一节都没有 → 整体包成 `【关键事实】\n{原文}`（退化为旧式单段摘要，仍可用）

### 1.4 progress.md：任务进度档案

每次整理，四节摘要**整体重写**写进 `sessions/<id>/progress.md`：

```md
# 任务进度（会话 {id}，上下文整理时自动重写）

- 最近整理：YYYY-MM-DD HH:MM
- 原始对话全文：sessions/{id}/session.json

【已完成】…
【正在做】…
【待办】…
【关键事实】…
```

- **为什么重写不追加**：新摘要已含旧摘要的链式浓缩，重写天然承袭全部历史信息且不膨胀；追加会产生过期条目堆积。
- **谁读它**：`<session>` 身份块告知绝对路径并写明用法——"整理后要恢复任务现场、或长任务到达阶段性节点时，先读写这份档案"。模型也可在阶段性节点**主动补写**进度（`<action>` 段准则 6 授权了这件事）。
- **失败降级**：写盘失败只记 trace span（`progress_err`），marker 退回"摘要见下"文案，整理本身不失败。
- **fsys/sessionID 未装配**（裸装配、单测）时跳过写入。

### 1.5 marker 与前端契约

```
<context_trim kept="N">
（系统自动整理，非用户发言，无需回应）
此前的早期对话已折叠出模型上下文，整理结果已写入进度档案 sessions/{id}/progress.md（路径见 <session> 块），原始记录仍完整保留在会话存档。当前任务衔接摘要：
{四节摘要}
</context_trim>
```

- `<context_trim` 前缀与 `kept` 属性是硬契约（`ViewStart` / `MergeFull` / `IsTrimMarker` / 前端 `buildBlocks` 共同依赖），**只改内部文案，不动标签**。
- 前端 `trimText`（store.svelte.ts）按 `【已完成】` 锚点截取、160 字截断渲染为一行分割线；**不兼容旧"摘要："格式**（不做历史数据兼容）。
- 失败降级文案（无 progress.md 行）也以"当前任务衔接摘要："引出，锚点不变。

---

## 二、compact：沉淀式归档

### 2.1 流程（`ArchiveSession`，两步模型调用）

```
用户手动 Compact（BeginArchive 锁串行）
  ① 沉淀步 distillToMemory：会话 view + 三个记忆文件现内容 → 模型合并 → 覆盖写回
     （失败静默——沉淀是增值动作，不阻断归档）
  ② 摘要步 summarizeMsgs：模型 view（含历史 marker 链）→ 精炼交接摘要
  → 旧库封存（Archived=true）→ 新库初始快照落盘 → sys 热更 <compact-summary> → 内存换代
```

### 2.2 记忆方案：固定文件集 + 合并重写

长期记忆**预定义三个固定主题文件**（不动态开 topic、不追加行）：

| 文件 | 收录 | 不收录 |
|---|---|---|
| `user.md` | 用户偏好与习惯、稳定的用户事实（职业背景、工作环境、表达与流程约定） | 一次性任务过程 |
| `projects.md` | 项目与任务背景：在做什么、关键路径与约定、交付物位置 | 临时性决定 |
| `lessons.md` | 踩过的坑与解法、验证过的环境特性、可复用经验 | 可从文件恢复的技术细节 |

**为什么合并重写而不是追加**（这是与"每条记忆 append 一行"方案的根本分歧）：

- 追加式在多次归档后必然**重复堆积**（同一事实每次归档各写一遍）与**冲突**（新事实与旧条目矛盾并存），且没有任何机制去清理。
- 合并重写 = 每次归档时模型**看全量旧内容 + 新内容 → 整合出新的文件全文 → 整体覆盖**。去重（新表述覆盖旧表述）、纠偏（过时信息修正）、清理（失效条目删除）都由模型在重写时一次完成，文件自然收敛在"当前为真的记忆"上。
- 每文件 30 行上限，超出按重要性截断——记忆文件是**浓缩索引**不是流水账，细节永远可以回会话存档找。

**为什么是固定三个文件而不是动态 topic**：动态开文件会让索引（harness.md，全文进 system）持续膨胀，且模型归档时自造主题名会产生碎片文件；三个槽位（人/事/经验）覆盖长期记忆的主要诉求，`<memory>` 段与索引模板都按固定结构写死。

### 2.3 沉淀提示词要点（distill.go）

输出协议（纯文本分段，弱模型友好）：

```
===user===
（user.md 新全文，或 UNCHANGED）
===projects===
…
===lessons===
…
```

- 段内容为 `UNCHANGED` 或与现文件相同 → 跳过写盘（零改动时不碰文件）。
- `parseDistillSections` 只认三个固定段名，模型自造的段名直接丢弃（防写歪文件）。
- 写回时统一文件头 `# {name}`。

### 2.4 交接摘要（archive.go）

```
为切换到全新会话生成精炼交接摘要。只保留新会话开工必需的信息：
用户本次的总体目标与尚未完成的部分（含下一步从哪接）、已达成的关键决定及
一句话理由、交付物位置（文件/终端/应用入口）、明确的待办。忽略过程细节、
状态栏记录、已完结且无后续的子任务。300 字以内，自包含——新会话除了这份
摘要与本会话存档路径外没有任何上下文。
```

与 trim 摘要的分工：交接摘要面向"**从零开始的新会话**"（它没有 progress.md，只有这段摘要 + 存档路径），所以要"下一步从哪接"；trim 摘要面向"**上下文被裁剪但记忆还在的自己**"，所以是四节现状盘点。

### 2.5 新世代的记忆入口

归档后新 session 的 system 多出 `<compact-summary>` 段：存档路径、沉淀说明（"偏好/事实/经验已沉淀进 memory/longterm/（user/projects/lessons，索引见 harness.md）"）、交接摘要正文。`<memory>` 段（每 session 固定的 base）则始终说明三文件的位置与检索方式。

---

## 三、记忆体系全景

信息按"还有多久会用"分四层，每层有明确的入口与恢复路径：

| 层 | 载体 | 谁写入 | 谁消费 |
|---|---|---|---|
| 活跃上下文 | `state.Messages`（视图窗口内） | 引擎 | 模型直接看见 |
| 任务进度 | `sessions/<id>/progress.md` + `<context_trim>` marker | trim 每次整理重写；模型可阶段补写 | 同 session 的模型（恢复现场） |
| 会话存档 | `sessions/<id>/session.json`（全量含折叠段） | sessionstore 落盘 | `<session>` 块路径指引，模型按需读 |
| 长期记忆 | `memory/longterm/{user,projects,lessons}.md` + `harness.md` 索引 | 归档沉淀步自动合并；对话中模型可直接编辑 | 一切会话（索引进 system，正文 grep 检索） |

信息流向：对话 →（trim）→ 进度档案/存档 →（compact 沉淀）→ 长期记忆。**越往右越浓缩、越持久**；每一步都留有"回上一层找细节"的路径（progress.md 指向 session.json，`<compact-summary>` 指向存档与记忆文件）。

---

## 四、失败与兜底一览

| 环节 | 失败时 | 理由 |
|---|---|---|
| trim 摘要调用 | 整理放弃，模型继续原上下文作答 | 截断是破坏性操作，摘要失败宁可不折 |
| 摘要格式坏 | `normalizeStructuredSummary` 兜底，不重试 | 解析端兜底比再调一次模型可靠 |
| progress.md 写盘 | 记 trace `progress_err`，marker 降级文案 | marker 内摘要本身已够接续 |
| 归档沉淀步 | 静默跳过，归档继续 | 沉淀是增值动作，不挡用户显式操作 |
| 归档摘要调用 | 整个归档报错（有锁保护，状态不变） | 交接摘要是归档的核心产物，失败必须让用户知道 |

---

## 五、改动检查清单（改这两块前必看）

- `<context_trim` 标签/`kept` 属性：`domain.ViewStart` / `MergeFull` / `IsTrimMarker` / 前端 `buildBlocks`——**别动**
- 前端 `trimText` 锚点与 marker 内引出文案联动
- `NewTrim` / `NewRemind` 的闭包注入参数（hooks 不得反向 import service）
- 记忆文件名（user/projects/lessons）在三处同源：`distill.go` `distillFiles`、`buildSystemBase` `<memory>` 段、`memory.go` `DefaultHarnessMd`——改名三处一起
- 归档唯一生产路径是 `topic_service.go` 的 `Compact`；`POST /sessions/:id/archive` 只翻标记不走摘要
- 文档连带：`AGENTS.md` 2.3/2.4、`docs/md/context.md`（成品样例）、`docs/md/runtime.md`（hook 表）、本文
