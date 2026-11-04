<script lang="ts">
  import { onMount } from 'svelte'
  import pkg from '../package.json'
  import { store } from './lib/store.svelte'
  import { pinboardBusy, reloadPinboard } from './lib/pinboard.svelte'
  import { filePane } from './lib/filePaneState'
  import Sidebar from './components/Sidebar.svelte'
  import TitleBar from './components/TitleBar.svelte'
  import ChatView from './components/ChatView.svelte'
  import ModelsView from './components/ModelsView.svelte'
  import PinboardView from './components/pinboard/PinboardView.svelte'
  import PinQuickWindow from './components/pinboard/PinQuickWindow.svelte'
  import MemoryView from './components/MemoryView.svelte'
  import ToolsView from './components/ToolsView.svelte'
  import CallbacksView from './components/CallbacksView.svelte'
  import McpView from './components/McpView.svelte'
  import SecurityView from './components/SecurityView.svelte'
  import SettingsView from './components/SettingsView.svelte'
  import WorkspaceDrawer from './components/board/WorkspaceDrawer.svelte'
  import TerminalTab from './components/board/TerminalTab.svelte'
  import ResourcePane from './components/viewer/ResourcePane.svelte'
  import BrowserPane from './components/board/BrowserPane.svelte'
  import AssistantPet from './components/AssistantPet.svelte'
  import NoticeWindow from './components/NoticeWindow.svelte'

  let view = $state<'chat' | 'pinboard' | 'models' | 'memory' | 'tools' | 'callbacks' | 'mcp' | 'security' | 'settings'>('chat')
  let expanded = $state(false)

  /* 弹出窗口（ez:popout 创建）：只渲染单个工具 pane 的轻布局，无会话/
     SSE（三个 pane 均不依赖 bootstrap），系统标题栏负责宽高/全屏 */
  const popoutView = new URLSearchParams(location.search).get('popout')
  /* 桌面宠物（ez:pet 创建）：只有头像 + 状态角标（AssistantPet 内自轮询
  notifications/topics，不走 bootstrap/SSE/store） */
  const petView = new URLSearchParams(location.search).get('pet') === '1'
  /* 独立通知窗（铃铛球打开）：复用 NoticePanel + store 轮询，不 bootstrap */
  const noticeView = new URLSearchParams(location.search).get('notice') === '1'
  /* 便签小窗（便签球打开）：只轮询便签板，不 bootstrap/SSE */
  const pinsView = new URLSearchParams(location.search).get('pins') === '1'
  const ez = (window as any).ez

  /* 资源页弹出侧：状态变化防抖上报主进程（关窗回流取最新快照） */
  let pushTimer: ReturnType<typeof setTimeout> | undefined
  function pushFileState(json: string) {
    clearTimeout(pushTimer)
    pushTimer = setTimeout(() => ez?.popout?.push('file', json), 300)
  }

  onMount(() => {
    /* 桌面宠物：无 bootstrap / SSE / store（状态感知在 AssistantPet 内轮询） */
    if (petView) return
    /* 独立通知窗：自管 refreshBranches/refreshNotices 轮询，不 bootstrap */
    if (noticeView) return
    /* 便签小窗：只轮询便签板 */
    if (pinsView) return
    if (popoutView) {
      /* 弹出窗口：装载初始状态 + 接收主窗口转发的外部定位请求 */
      if (popoutView === 'file') {
        void ez?.popout?.take('file').then((json: string | null) => {
          if (json) filePane.restore(json)
        })
      }
      ez?.popout?.onSignal((name: string, value: string) => {
        if (name === 'file') store.fileFocus = value
        else if (name === 'draft') store.fileDraftSrc = value
        else if (name === 'term') store.termFocus = value
      })
      return
    }
    /* 主窗口：弹窗关窗回流（重开抽屉带回状态）+ 弹出清单同步 */
    ez?.popout?.onClosed((view: string, stateJson: string | null) => store.popoutClosed(view, stateJson))
    void store.syncPopoutTools()
    /* 独立通知窗点条目 → 主进程亮起主窗并转发跳转载荷 */
    ez?.notice?.onJump?.((ref: { rootId: string; forkId: string; id: string; target: string }) =>
      void store.jumpToNoticeRef(ref),
    )
    /* 便签小窗点条目 → 主进程亮起主窗并转发卡片 id（空 = 只开便签页）。
       小窗里可能刚完成/排过序：不定位卡片时先拉最新板；定位卡片由
       PinboardView 拉完再提层，免得提层被随后返回的旧板盖掉 */
    ez?.pin?.onJump?.((cardId: string) => {
      store.pinFocus = cardId
      store.jumpView = 'pinboard'
      if (!cardId && !pinboardBusy()) void reloadPinboard()
    })
    void store.bootstrap().catch((e) => {
      console.error('bootstrap 失败', e)
      store.lastStatus = `启动失败：${(e as Error).message}`
    })
    // 窗口重新聚焦时刷新（skill/MCP 可能在别的窗口或本机文件系统被改；
    // 后台分支的运行/审批状态不经当前 SSE，聚焦时拉取）
    const onFocus = () => {
      if (view === 'chat') {
        void store.refreshStatus()
        void store.refreshBranches()
      }
    }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  })

  // 进入对话页即刷新：MCP/记忆等页面改动开关或 skill 后，右上角状态卡实时反映
  $effect(() => {
    if (!popoutView && !petView && view === 'chat') void store.refreshStatus()
  })

  // 通知跳页请求（本地记忆通知 → 打开记忆页）
  $effect(() => {
    if (store.jumpView) {
      view = store.jumpView
      store.jumpView = ''
    }
  })
</script>

{#if petView}
  <AssistantPet />
{:else if noticeView}
  <NoticeWindow />
{:else if pinsView}
  <PinQuickWindow />
{:else if popoutView}
  <div class="popout-pane">
    {#if popoutView === 'term'}
      <TerminalTab active={true} />
    {:else if popoutView === 'file'}
      <ResourcePane active={true} push={pushFileState} />
    {:else if popoutView === 'browser'}
      <BrowserPane active={true} />
    {/if}
  </div>
{:else}
<div class="shell">
  <TitleBar onNavigate={(v) => (view = v as typeof view)} sidebarWidth={expanded ? 176 : 64} />
  <div class="app" class:expanded>
    <Sidebar view={view} expanded={expanded} onNavigate={(v) => (view = v)} onToggle={() => (expanded = !expanded)} />
    <main>
      {#if view === 'chat'}
        <ChatView />
      {:else if view === 'pinboard'}
        <PinboardView />
      {:else if view === 'models'}
        <ModelsView />
      {:else if view === 'memory'}
        <MemoryView onNavigate={(v) => (view = v as typeof view)} />
      {:else if view === 'tools'}
        <ToolsView />
      {:else if view === 'callbacks'}
        <CallbacksView />
      {:else if view === 'mcp'}
        <McpView />
      {:else if view === 'security'}
        <SecurityView />
      {:else}
        <SettingsView />
      {/if}
    </main>
    <!-- 工作区抽屉（终端/文件/画板三工具页）：推挤式右布局列（.app flex
    行内，打开挤窄 main；pane 常驻挂载保活） -->
    <WorkspaceDrawer visible={view === 'chat'} />
  </div>
</div>

<footer class="brand-foot" class:away={view === 'chat' && store.termDrawerOpen}>
  <a href="https://github.com/xuanlv2002/ezharness" target="_blank" rel="noreferrer">ezharness-{pkg.version}</a>
  <span>·</span>
  <a href="https://github.com/xuanlv2002/ezloop" target="_blank" rel="noreferrer">powered by ezloop</a>
</footer>
{/if}

<style>
  /* 弹出窗口的单工具布局：撑满（padding 对齐抽屉 body） */
  .popout-pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    padding: 10px 12px 12px 16px;
  }
  .shell {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .app {
    --sidebar-w: 64px; /* 工作区抽屉宽度联动基数（expanded 时覆写） */
    display: flex;
    flex: 1;
    min-height: 0;
    /* 内容区左上角的圆角要露出这层灰，而不是窗口白底 */
    background: var(--bg-soft);
  }
  .app.expanded {
    --sidebar-w: 176px;
  }
  main {
    flex: 1;
    /* 主列手机宽下限：抽屉展开时主列压到 480（与窗口 minWidth 840 =
    480 + 64 侧栏 + 280 抽屉保底 + 余量 对齐）防内容挤碎 */
    min-width: 480px;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    /* 与窗口左上圆角呼应，避免白色直角紧贴灰色顶栏/侧栏 */
    border-top-left-radius: 10px;
  }
  .brand-foot {
    position: fixed;
    right: 12px;
    bottom: 8px;
    z-index: 50;
    display: flex;
    gap: 6px;
    font-size: var(--font-size-small);
    color: var(--faint);
    user-select: none;
    opacity: 0.45;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .brand-foot:hover {
    opacity: 1;
  }
  /* 工作区抽屉打开时避让：右下角会与抽屉底部操作区重叠 */
  .brand-foot.away {
    opacity: 0;
    pointer-events: none;
  }
  .brand-foot a {
    color: var(--faint);
    text-decoration: none;
    border-bottom: 1px dotted transparent;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .brand-foot a:hover {
    color: var(--accent);
  }
</style>
