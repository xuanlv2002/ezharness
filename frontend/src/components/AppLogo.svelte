<script lang="ts">
  /* 快应用头像：声明了 icon（或目录里探测到）就直出图片，没有则回落首字
     字母块——不给闪电占位符，闪电是「快应用」页面入口的图标。 */
  let {
    name,
    title = '',
    icon = '',
    size = 18,
  }: { name: string; title?: string; icon?: string; size?: number } = $props()

  let failed = $state(false)
  /* 换应用/换图标时重置失败态（同一组件实例被复用） */
  $effect(() => {
    void icon
    failed = false
  })

  const label = $derived((title || name || '?').trim().charAt(0).toUpperCase() || '?')
  const radius = $derived(Math.max(4, Math.round(size * 0.28)))
</script>

{#if icon && !failed}
  <img
    class="qlogo"
    src={icon}
    alt=""
    loading="lazy"
    style:width="{size}px"
    style:height="{size}px"
    style:border-radius="{radius}px"
    onerror={() => (failed = true)}
  />
{:else}
  <span
    class="qlogo mono"
    style:width="{size}px"
    style:height="{size}px"
    style:border-radius="{radius}px"
    style:font-size="{Math.max(9, Math.round(size * 0.52))}px"
  >{label}</span>
{/if}

<style>
  .qlogo {
    flex: none;
    display: grid;
    place-items: center;
    object-fit: cover;
    overflow: hidden;
  }
  /* 无 icon 的字母块：与快应用管理页的空态头像同族（反色底 + 反色字） */
  .qlogo.mono {
    background: var(--bg-invert);
    color: var(--fg-invert);
    font-weight: 700;
    line-height: 1;
    user-select: none;
  }
</style>
