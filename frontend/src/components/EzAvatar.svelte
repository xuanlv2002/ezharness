<script module lang="ts">
  /* 表情池：IDLE 是对话头像的固定轮询序（打招呼→眨眼→困了→东张西望）；
     POOL 是空页面的随机播放池（含 happy/love，不含 spin），载入时洗牌一次。 */
  const IDLE = ['hello', 'wink', 'sleep', 'look']
  const POOL = ['hello', 'happy', 'love', 'wink', 'sleep', 'look'].sort(() => Math.random() - 0.5)

  /* 资产随前端打包：public/ezavatar/expr/<表情>/（透明底 webp 动画 +
     still.png 静帧），构建产物自带，不依赖应用数据目录 */
  const BASE = '/ezavatar/expr/'

  /* 预热缓存（模块级）：同一表情整页只拉一次，轮换切换不闪白 */
  const warmed = new Set<string>()
</script>

<script lang="ts">
  /*
  ezharness 表情头像：动画用 anim.webp（透明底），静帧用 still.png；
  加载失败回退 children（ez 字样 / Logo）。
  run：运行中＝spin 转圈；animate=false 用静帧（历史回复组，不产生
  播放开销）；random：空页面从 POOL 随机轮换；period：轮换间隔毫秒。
  */
  let {
    size = 26,
    run = false,
    animate = true,
    random = false,
    period = 7000,
    children,
  }: {
    size?: number
    run?: boolean
    animate?: boolean
    random?: boolean
    period?: number
    children?: import('svelte').Snippet
  } = $props()

  let failed = $state(false)
  let pick = $state(Math.floor(Math.random() * 4))

  const expr = $derived.by(() => {
    if (run) return 'spin'
    const pool = random ? POOL : IDLE
    return pool[pick % pool.length]
  })
  const src = $derived(`${BASE}${expr}/${run || animate ? 'anim.webp' : 'still.png'}`)

  /* 动画实例才预热 + 计时：静帧头像不产生网络与定时器开销 */
  $effect(() => {
    if (!animate || failed) return
    for (const e of run ? ['spin'] : random ? POOL : IDLE) {
      if (!warmed.has(e)) {
        warmed.add(e)
        new Image().src = `${BASE}${e}/anim.webp`
      }
    }
    if (run) return
    const t = setInterval(() => pick++, period)
    return () => clearInterval(t)
  })
</script>

{#if failed}
  {@render children?.()}
{:else}
  <img {src} alt="" draggable="false" style="width:{size}px;height:{size}px"
    onerror={() => (failed = true)} />
{/if}

<style>
  img {
    display: block;
    object-fit: contain;
    user-select: none;
    pointer-events: none;
  }
</style>
