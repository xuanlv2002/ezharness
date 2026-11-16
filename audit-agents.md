# AGENTS.md 规范检查清单

对照 AGENTS.md 全库审计（frontend/lib、8 个大组件、中小组件、desktop、core 五路）。
每条 `[x]` = 已改；`[ ]` = 保留未改（附原因）。

**状态（2026-11-16）：确认 67 条已改 64 条（3 条兼容迁移类保留）；存疑 32 条已改 30 条（2 条有真实竞态/迁移依赖保留）；并发问题已修。**
验证：core `go build + go vet + go test ./... + -race domain` 通过；frontend `tsc --noEmit + vite build` 通过；desktop/sdk `node --check` 通过。

## 总览

| 分类 | 确认 | 已改 | 存疑 | 已改 |
|------|------|------|------|------|
| 注释问题 | 20 | 20 | 10 | 10 |
| 死代码 | 20 | 20 | 1 | 1 |
| 文件内碎拆 | 12 | 12 | 11 | 11 |
| 过度设计 | 8 | 8 | 6 | 5 |
| 兼容适配残留 | 4 | 1 | 2 | 1 |
| 命名大词化 | 3 | 3 | 2 | 2 |

---

## 一、注释问题

### 1.1 文件头超过 2 行（确认 16 条，约 65 个文件，全部已改）

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
- [x] 批量 47 文件全部处理（ExcelViewer/PdfViewer/PptxViewer/StatusTagCard 原本 ≤2 行，跳过）
- [x] 补充：PinRack、PinTimeline、PinSortableList、ChatEmpty 一并压了

### 1.2 其他注释问题（确认 4 条，全部已改）

- [x] `MemoryView.svelte:1303` 漂移错位注释已删
- [x] `McpView.svelte:753,1354` 跨文件指路已删
- [x] `desktop/src/main/browser/index.js` 重复注释已删一行
- [x] `agent_service.go`/`settings_service.go` 「三槽/四槽」已改单槽实际

### 1.3 注释问题（存疑 10 条，全部已改）

- [x] `term.ts` / `auth.ts` / `findtext.ts` 文件头压到 1 行
- [x] `McpView.svelte` 头 3 行 → 1 行
- [x] `devtools.js` / `inspect.js` 头 9-11 行 → 1 行
- [x] `remind.go` 头 19 行 → 2 行（保留"为什么在此处"）
- [x] `MessageItem.svelte` supper_url 协议说明压成 1 行
- [x] `Timeline.svelte` 两处 why 块压成 1 行
- [x] `WorkspaceDrawer.svelte` 拖拽设计块压成 1 行
- [x] `PinboardView.svelte` 相邻重复注释删一条
- [x] `ToolsView.svelte` CSS `//` 改 `/* */`
- [x] `tools/apps.go` 参数文案改为 `<apps> 清单可查`（原写不存在的 list_apps）

---

## 二、死代码（确认 20 条全部已删）

- [x] `api.ts` — `archiveSession`/`deleteTopic`/`mediaJob`/`callModel` 已删
- [x] `AssistantPet.svelte` 恒 0 三元已改字面量
- [x] `devtools.js` — `dt` 导出、`LABEL` 恒等映射、`ensureDevtools` 转发已删
- [x] `trace.go` — `CloseRoot`/`OpenRoot` 已删
- [x] `session.go` — `type E`、`PendingFrames`、`ReplayFrames`、`SetIdentity` 已删
- [x] `stats.go` — `StatsView`、`Total`、`DaysServed`、`CacheHitRate` 已删
- [x] `callback/store.go` — `Store.Save` 已删
- [x] `tools/media.go` — `jobResult` 的 `done` 参数与冗余条件已删
- [x] `hooks/topics.go` — `Topics.Match` 已删（测试一并删）
- [x] 存疑：`MemoryView` 永久禁用的「归档」占位按钮已删

---

## 三、文件内碎拆（确认 12 + 存疑 11，全部已改）

- [x] `store.svelte.ts` — `jumpToNotice`、`trimText`、`turnFiles` 处理
- [x] `CallbacksView.svelte` — `asRunRow` 收敛为唯一构造点
- [x] `McpView.svelte` — `removeServer` 内联
- [x] `PinCard.svelte` — `removeNow` 内联
- [x] `ExplorerTree.svelte` — `subtreeStatus` 内联
- [x] `ModelsView.svelte` — `defaultBase` 内联
- [x] `PinRack/PinTimeline/PinSortableList` — `onDragUp` 转发已删
- [x] `devtools.js` — `ensureDevtools` 转发已删
- [x] `loopguard.go` — `guardMsg` 内联
- [x] `hooks` 三个截断助手合并为 `oneLine(s, max)`
- [x] 存疑：`openImageDraftData` 并入 `openImageDraft`
- [x] 存疑：`boardSnap` 被 `snapshot` 复用
- [x] 存疑：`ExplorerTree.removeRecent` 内联
- [x] 存疑：`ModelsView.escapeHtml` 内联
- [x] 存疑：`SettingsView` 两个 copy 函数合并为 `copyToken(input, kind)`
- [x] 存疑：`InputBar` 六个菜单处理器合并为 `toggleMenu`，pick 内联
- [x] 存疑：`PinRack` `latestSession`/`extraSessions` 内联进 derived
- [x] 存疑：`PinTimeline.clearPeek` 内联
- [x] 存疑：`desktop` 三个保存/隐藏函数合并为 `saveSetting`
- [x] 存疑：`hooks/memory.go` `SkillDirOf` 删除，调用方直调 `skill.DirOf`
- [x] 存疑：`tools/memory.go` 闭包求下标改 `slices.ContainsFunc`

---

## 四、过度设计

### 确认 8 条（全部已改）

- [x] `api.ts` — 重复的 `CallbackSessionTarget`/`CallbackSessionInput` 合并
- [x] `store.svelte.ts` — 恒真 `typeof args === 'string'` 已删
- [x] `ExplorerTree.svelte` — 恒真 `?.err` 条件已删
- [x] `CallbacksView.svelte` — `RunRow` 加结构化字段，不再解析展示串
- [x] `browser/index.js` — 「不会到这」的 try/catch 已删
- [x] `agent_service.go` — 单元素切片循环已展开
- [x] `settings_service.go` — 单键 map 循环已展开
- [x] `reschange.go` — 不可达 `len(p) < 1` 已删

### 存疑 6 条（已改 5）

- [x] `api.ts` 9 处 `'…' | string` 联合失效写法已收窄
- [x] `toolface.ts`/`linediff.ts` — LCS 收敛为 `linediff.lcsLines` 唯一实现
- [ ] `McpView.svelte` `persistChain` 保留：**真实竞态**（全量保存 API 下"删后立刻加"会被旧载荷覆盖），前端串行是当前架构下的最小修复；根治需后端按 server 增量保存，单独立项
- [x] `MemoryView.svelte` `r.items || items` 判空已删
- [x] `settings_controller.go` 错误文本匹配已改为 `errors.Is(ErrBusy)`（原字符串在代码库与依赖中无生产者，分支是死的）
- [x] `media_controller.go` 手写数字解析改用 `strconv.Atoi`

---

## 五、兼容适配残留

### 确认 4 条（已改 1，保留 3）

- [x] `ModelsView.svelte` — 多槽空转抽象整体删除
- [ ] `pinboard.svelte.ts` `normCard`/`normRecord` 保留：自持落盘数据缺字段在读取处补齐、下次落盘自愈，无迁移脚本通道；要改成版本号迁移需单独做
- [ ] `sessionstore.go` `RepairForkSnap` 保留：删除需先写启动迁移把存量空壳 fork 修完（含标记文件与首启扫描），涉及用户数据，单独立项
- [ ] `sessionstore.go` 旧分身快照补读分支保留：随 `RepairForkSnap` 一起处理

### 存疑 2 条（已改 1）

- [x] `api.ts` 文件头「v0.2」版本注记已删
- [ ] `sessionstore.go` prefs.json / session.json 双源偏好保留：兼容读取旧会话偏好，需迁移完成后才能删

---

## 六、命名大词化

### 确认 3 条（全部已改）

- [x] 前端 `ensure*` — `getFork`/`toChatView`/`loadModels`/`loadPinboard`/`addStyle`
- [x] `SecurityView.svelte` `persist→save`
- [x] `MediaConfigPanel.svelte` `persist→save`（ModelsView 同步改）

### 存疑 2 条（全部已改）

- [x] `TerminalTab.init→connect`（原 `inited` 标志改 `started`，避开已有 `connected` 状态）；`MagicBoard.init→load`
- [x] core `Ensure*` → `CreateLayout`/`GetSession`/`CreateStatsFile`/`getBucket`；`devtools.ensureAttached→attachOnce`；`sdk.js` `ensureHost→addHost`、`ensureAuthorized→authorize`

---

## 附：并发正确性问题（已修）

- [x] `core/internal/domain/session.go` `Publish` 无锁读 `s.turnRefs` — 改为持锁取引用（`go test -race` 通过）
