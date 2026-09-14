<script lang="ts">
  import { onMount } from 'svelte'
  import pkg from '../package.json'
  import { store } from './lib/store.svelte'
  import { filePane } from './lib/filePaneState'
  import Sidebar from './components/Sidebar.svelte'
  import TitleBar from './components/TitleBar.svelte'
  import ChatView from './components/ChatView.svelte'
  import ModelsView from './components/ModelsView.svelte'
  import MemoryView from './components/MemoryView.svelte'
  import KnowledgeView from './components/KnowledgeView.svelte'
  import ToolsView from './components/ToolsView.svelte'
  import McpView from './components/McpView.svelte'
  import SecurityView from './components/SecurityView.svelte'
  import SettingsView from './components/SettingsView.svelte'
  import WorkspaceDrawer from './components/board/WorkspaceDrawer.svelte'
  import TerminalTab from './components/board/TerminalTab.svelte'
  import ResourcePane from './components/viewer/ResourcePane.svelte'
  import BrowserPane from './components/board/BrowserPane.svelte'

  let view = $state<'chat' | 'models' | 'memory' | 'knowledge' | 'tools' | 'mcp' | 'security' | 'settings'>('chat')
  let expanded = $state(false)

  /* 弹出窗口（ez:popout 创建）：只渲染单个工具 pane 的轻布局，无会话/
     SSE（三个 pane 均不依赖 bootstrap），系统标题栏负责宽高/全屏 */
  const popoutView = new URLSearchParams(location.search).get('popout')
  const ez = (window as any).ez

  /* 资源页弹出侧：状态变化防抖上报主进程（关窗回流取最新快照） */
  let pushTimer: ReturnType<typeof setTimeout> | undefined
  function pushFileState(json: string) {
    clearTimeout(pushTimer)
    pushTimer = setTimeout(() => ez?.popout?.push('file', json), 300)
  }

  onMount(() => {
    if (popoutView) {
      /* 弹出窗口：装载初始状态 + 接收主窗口转发的外部定位请求 */
      if (popoutView === 'file') {
        void ez?.popout?.take('file').then((json: string | null) => {
          if (json) filePane.restore(json)
        })
      }
      ez?.popout?.onSignal((name: string, value: string) => {
        if (name === 'file') store.fileFocus = value
        else if (name === 'term') store.termFocus = value
      })
      return
    }
    /* 主窗口：弹窗关窗回流（重开抽屉带回状态）+ 弹出清单同步 */
    ez?.popout?.onClosed((view: string, stateJson: string | null) => store.popoutClosed(view, stateJson))
    void store.syncPopoutTools()
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
    if (!popoutView && view === 'chat') void store.refreshStatus()
  })
</script>

{#if popoutView}
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
  <TitleBar />
  <div class="app" class:expanded>
    <Sidebar view={view} expanded={expanded} onNavigate={(v) => (view = v)} onToggle={() => (expanded = !expanded)} />
    <main>
      {#if view === 'chat'}
        <ChatView />
      {:else if view === 'models'}
        <ModelsView />
      {:else if view === 'memory'}
        <MemoryView onNavigate={(v) => (view = v as typeof view)} />
      {:else if view === 'knowledge'}
        <KnowledgeView />
      {:else if view === 'tools'}
        <ToolsView />
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
    <WorkspaceDrawer />
  </div>
</div>

<footer class="brand-foot" class:away={store.termDrawerOpen}>
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
  }
  .brand-foot {
    position: fixed;
    right: 12px;
    bottom: 8px;
    z-index: 50;
    display: flex;
    gap: 6px;
    font-family: var(--font-mono);
    font-size: 10px;
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
