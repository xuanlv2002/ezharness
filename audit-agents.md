# AGENTS.md 规范检查清单

对照 AGENTS.md 全库审计（frontend/lib、8 个大组件、中小组件、desktop、core 五路）。
每条 `[x]` = 已改；`[ ]` = 未改。分**确认**（明确违反）与**存疑**（可商榷，全部未动待你定）。

**状态（2026-11-16）：确认 67 条中已改 64 条，3 条按下述说明处理；存疑 32 条全部保留待定；附带的并发问题已修。**
验证：core `go build + go test ./... + -race domain` 通过；frontend `tsc --noEmit + vite build` 通过；desktop `node --check` 通过。

## 总览

| 分类 | 确认 | 已改 | 存疑（未动） |
|------|------|------|------|
| 注释问题 | 20 | 20 | 10 |
| 死代码 | 20 | 20 | 1 |
| 文件内碎拆 | 12 | 12 | 11 |
| 过度设计 | 8 | 8 | 6 |
| 兼容适配残留 | 4 | 1 | 2 |
| 命名大词化 | 3 | 3 | 2 |

---

## 一、注释问题（确认 20 条全部已改）

### 1.1 文件头超过 2 行（全部已压到 ≤2 行，约 65 个文件）

- [x] `frontend/src/components/pinboard/PinCard.svelte`（23 行 → 2 行）
- [x] `frontend/src/components/viewer/ExplorerTree.svelte`（13 → 1）
- [x] `frontend/src/components/MemoryView.svelte`（7 → 1）
- [x] `frontend/src/components/CallbacksView.svelte`（7 → 2）
- [x] `frontend/src/components/ModelsView.svelte`（5 → 1）
- [x] `frontend/src/lib/pinboard.svelte.ts`（8 → 2）
- [x] `frontend/src/lib/rows.ts`（9 → 2）
- [x] `frontend/src/components/AssistantPet.svelte`（18 → 1）
- [x] `frontend/src/components/pinboard/PinboardView.svelte`（15 → 2）
- [x] `frontend/src/components/board/WorkspaceDrawer.svelte`（13 → 2）
- [x] `frontend/src/components/pinboard/PinTimeline.svelte`（11 → 2）
- [x] `desktop/src/main/index.js`（6 → 2）
- [x] `desktop/src/main/browser/index.js`（19 → 2）
- [x] `core/main.go`（16 → 2）
- [x] `core/internal/config/cfg.go`（11 → 2）
- [x] 批量 47 文件全部处理（其中 ExcelViewer/PdfViewer/PptxViewer/StatusTagCard 本来就 ≤2 行，跳过）
- [x] 补充：PinRack、PinTimeline、PinSortableList、ChatEmpty（不在原清单里，一并压了）

### 1.2 其他注释问题（已改）

- [x] `MemoryView.svelte:1303` — 漂移错位的弹窗注释已删
- [x] `McpView.svelte:753,1354` — 「同 MemoryView 删除技能」跨文件指路已删
- [x] `desktop/src/main/browser/index.js` — 重复注释已删一行
- [x] `agent_service.go`/`settings_service.go` — 「三槽/四槽」注释已改为单槽实际

### 1.3 注释问题（存疑 10 条，未动）

- [ ] `frontend/src/lib/term.ts:1-6`、`auth.ts:1-6`、`findtext.ts:1-6`
- [ ] `frontend/src/components/McpView.svelte:7-9`（3 行）
- [ ] `desktop/src/main/browser/devtools.js:1-11`、`inspect.js:1-9`
- [ ] `core/internal/hooks/remind.go:1-19`
- [ ] `frontend/src/components/MessageItem.svelte:49-55`（supper_url 协议说明）
- [ ] `frontend/src/components/Timeline.svelte:19-23,159-164`（why 块超 3 行）
- [ ] `frontend/src/components/board/WorkspaceDrawer.svelte:47-53`
- [ ] `frontend/src/components/pinboard/PinboardView.svelte:860-861`
- [ ] `frontend/src/components/ToolsView.svelte:330,381`（CSS 里 `//` 注释）
- [ ] `core/internal/tools/apps.go:25,38` vs `:4`（注释互相矛盾）

---

## 二、死代码（确认 20 条全部已删）

- [x] `api.ts` — `archiveSession`/`deleteTopic`/`mediaJob`/`callModel` 四个零调用端点已删
- [x] `AssistantPet.svelte` — 恒 0 三元已改字面量
- [x] `devtools.js` — `dt` 导出、`LABEL` 恒等映射已删；`ensureDevtools` 纯转发已删（调用方直调 `attachTab`）
- [x] `trace.go` — `CloseRoot`/`OpenRoot` 已删
- [x] `session.go` — `type E`、`PendingFrames`、`ReplayFrames`、`SetIdentity` 已删（测试改调内部实现/直接赋值）
- [x] `stats.go` — `StatsView`、`Total`、`DaysServed`、`CacheHitRate` 已删
- [x] `callback/store.go` — `Store.Save` 已删
- [x] `tools/media.go` — `jobResult` 的 `done` 参数与冗余条件已删
- [x] `hooks/topics.go` — `Topics.Match` 已删（`TestTopicsMatch` 一并删）

### 存疑

- [ ] `MemoryView.svelte` 永久禁用的「归档」按钮（功能预告 or 残留）

---

## 三、文件内碎拆（确认 12 条全部已改）

- [x] `store.svelte.ts` — `jumpToNotice` 纯转发已删（TopRail 直调 `jumpToNoticeRef`）；`trimText` 已内联；`turnFiles` 同形复制已删（直接用 `final`）
- [x] `CallbacksView.svelte` — `asRunRow` 收敛为唯一构造点（列表与详情共用，消除 148 行的重复内联）
- [x] `McpView.svelte` — `removeServer` 已内联
- [x] `PinCard.svelte` — `removeNow` 已内联
- [x] `ExplorerTree.svelte` — `subtreeStatus` 已内联
- [x] `ModelsView.svelte` — `defaultBase` 已内联
- [x] `PinRack/PinTimeline/PinSortableList` — `onDragUp` 纯转发已删（直接注册 `finishDrag`）
- [x] `devtools.js` — `ensureDevtools` 纯转发已删
- [x] `loopguard.go` — `guardMsg` 已内联
- [x] `hooks` 三个截断助手 — 合并为 `oneLine(s, max)`

### 存疑（11 条未动）

- [ ] `store.svelte.ts:813` `openImageDraftData`（有草稿入口语义）
- [ ] `pinboard.svelte.ts` `snapshot`/`boardSnap` 两份浅拷贝
- [ ] `ExplorerTree.svelte:273` `removeRecent`
- [ ] `ModelsView.svelte` `escapeHtml`
- [ ] `SettingsView.svelte` `copyToken`/`copyOaToken` 重复
- [ ] `InputBar.svelte` 六个一次性菜单处理器
- [ ] `PinRack.svelte` `latestSession`/`extraSessions`
- [ ] `PinTimeline.svelte` `clearPeek`
- [ ] `desktop/src/main/index.js` `saveCloseToTray`/`savePetEnabled`/`hidePetBall`
- [ ] `hooks/memory.go` `SkillDirOf` 转发
- [ ] `tools/memory.go` 无用下标闭包

---

## 四、过度设计（确认 8 条全部已改）

- [x] `api.ts` — `CallbackSessionTarget`/`CallbackSessionInput` 合并为一个
- [x] `store.svelte.ts` — 恒真 `typeof args === 'string'` 已删
- [x] `ExplorerTree.svelte:681` — 恒真 `?.err` 条件已删
- [x] `CallbacksView.svelte` — `runTarget` 不再解析展示串：`RunRow` 增加 `appName`/`appCallback`/`sessionNew` 结构化字段（从任务构造时补）
- [x] `browser/index.js:357` — 「不会到这」的 try/catch 已删
- [x] `agent_service.go` — 单元素切片循环已展开
- [x] `settings_service.go` — 单键 map 循环已展开
- [x] `reschange.go` — 不可达 `len(p) < 1` 已删

### 存疑（6 条未动）

- [ ] `api.ts` 多处 `'ok' | … | string` 联合失效写法
- [ ] `toolface.ts` vs `linediff.ts` 两套 LCS
- [ ] `McpView.svelte` `persistChain` 保存排队（竞态真实，根治在后端）
- [ ] `MemoryView.svelte:196` `r.items || items` 判空
- [ ] `settings_controller.go:35` 错误文本匹配状态码
- [ ] `media_controller.go` 手写数字解析

---

## 五、兼容适配残留（确认 4 条：已改 1，保留 1，缓改 2）

- [x] `ModelsView.svelte` — 多槽空转抽象（`slotOf`/`setSlot`/`mainKinds` 循环）已整体删除，直接读写 `cfg.main`
- [ ] `pinboard.svelte.ts:163` `normCard`/`normRecord` 归一化层 — **评估后保留**：自持落盘数据的字段补齐在读取处一次完成，旧字段下次落盘自然消失（注释已说明），没有可部署的迁移脚本通道；`normImages` 白名单是安全门禁。要改成版本号迁移可以再做，需要你拍板
- [ ] `sessionstore.go:644` `RepairForkSnap` 常驻 4 条加载链 — **缓改**：删除需先把旧版空壳 fork 数据一次性修完（启动迁移 + 标记文件），涉及存量用户数据与首启扫描成本，建议单独做
- [ ] `sessionstore.go:747` 旧分身快照补读分支 — 同上，随 RepairForkSnap 一起处理

### 存疑

- [ ] `api.ts:1` 「v0.2」版本注记
- [ ] `sessionstore.go:702` prefs.json / session.json 双源偏好

---

## 六、命名大词化（确认 3 条全部已改）

- [x] 前端 `ensure*` 家族 — `ensureFork→getFork`、`ensureWorkspaceView→toChatView`、`ensureModels→loadModels`、`ensurePinboard→loadPinboard`、`ensureStyle→addStyle`（含 3 个组件的导入方）
- [x] `SecurityView.svelte` `persist→save`
- [x] `MediaConfigPanel.svelte` `persist→save`（顺带 `ModelsView.svelte` 的 `persist` 也改了 `save`）

### 存疑

- [ ] `TerminalTab`/`MagicBoard` 的 `init()`
- [ ] core 的 `Ensure*` 家族（Go 生态惯用）

---

## 附：并发正确性问题（已修）

- [x] `core/internal/domain/session.go` `Publish` 无锁读 `s.turnRefs` — 已改为持锁取引用后再扩载荷（`go test -race` 通过）
