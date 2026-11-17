<script lang="ts">
  import { onMount } from 'svelte'
  import NoticePanel from './NoticePanel.svelte'
  import { store, type NoticeData } from '../lib/store.svelte'

  /* 独立通知窗（?notice=1 窗口）：直接复用客户端的通知栏与 store，只跑
     轮询（不 bootstrap/SSE）；点条目跳转发 IPC 让主客户端亮起并转载荷。 */
  const ez = (window as any).ez

  onMount(() => {
    void store.refreshBranches().then(() => store.refreshNotices())
    const t = setInterval(() => void store.refreshNotices(), 3000)
    return () => clearInterval(t)
  })

  function resolve(id: string, action: string, input?: string) {
    const n = store.notices.find((x) => x.id === id)
    if (n) void store.resolveNoticeGlobal(n, action, input)
  }

  function jump(n: NoticeData) {
    ez?.notice?.jump?.({ rootId: n.rootId, forkId: n.forkId, id: n.id, target: n.target })
  }
</script>

<div class="wrap">
  <NoticePanel
    notices={store.notices}
    onResolve={resolve}
    onJump={jump}
    onDismiss={(id) => store.dismissNotice(id)}
  />
</div>

<style>
  .wrap {
    height: 100vh;
    padding: 12px;
    box-sizing: border-box;
    overflow: hidden;
  }
</style>
