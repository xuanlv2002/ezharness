<script lang="ts">
  /* 时间线行渲染的唯一实现：主会话（Timeline）与分身抽屉（ForkPanel）共用，
     保证两处块类型、工具卡、记忆 diff、资源变更、图片与收尾提示完全一致。
     两处差异只有头像尺寸/神态和是否播放历史入场动画，由 props 控制。 */
  import { store } from '../lib/store.svelte'
  import { TOOL_GROUP_MIN, type Row } from '../lib/rows'
  import EzAvatar from './EzAvatar.svelte'
  import MessageItem from './MessageItem.svelte'
  import ToolBlock from './ToolBlock.svelte'
  import ToolGroup from './ToolGroup.svelte'
  import ForkCard from './ForkCard.svelte'
  import DecisionCard from './DecisionCard.svelte'
  import StatusTagCard from './StatusTagCard.svelte'
  import ResChangeCard from './ResChangeCard.svelte'

  let {
    rows,
    avatarSize = 48,
    avatarKind = 'main',
    avatarOutside = false,
    showAvatar = true,
    liveBusy = false,
    reveal,
    openGroups,
    onToggleGroup,
  }: {
    rows: Row[]
    avatarSize?: number
    avatarKind?: 'main' | 'task'
    avatarOutside?: boolean
    showAvatar?: boolean
    liveBusy?: boolean
    reveal?: Set<number>
    openGroups: Set<number>
    onToggleGroup: (key: number) => void
  } = $props()

  /* 整理条点击原地展开/收起摘要全文（与工具卡一致，不用弹窗） */
  let openTrim = $state<number | null>(null)

  /* 摘要按【小节】切块（旧格式没有小节标题时整段一条） */
  function trimSections(full: string): { title: string; lines: string[] }[] {
    const out: { title: string; lines: string[] }[] = []
    for (const raw of full.split(/\r?\n/)) {
      const line = raw.trim()
      if (!line) continue
      const m = line.match(/^【(.+?)】\s*(.*)$/)
      if (m) {
        out.push({ title: m[1], lines: m[2] ? [m[2].replace(/^[-•]\s*/, '')] : [] })
      } else if (out.length) {
        out[out.length - 1].lines.push(line.replace(/^[-•]\s*/, ''))
      } else {
        out.push({ title: '', lines: [line.replace(/^[-•]\s*/, '')] })
      }
    }
    return out
  }
</script>

<div class="rows" style="--av-size:{avatarSize}px">
  {#each rows as row, i (row.key)}
    {#if row.kind === 'user'}
      {@const ub = row.b}
      <div class:reveal={reveal?.has(ub.uid)} data-uid={ub.uid}>
        <MessageItem
          text={ub.text}
          images={ub.images}
          files={ub.files}
          fileRefs={ub.fileRefs}
          callback={ub.callback}
          role="user"
          onFork={ub.owner && ub.msgIdx !== undefined ? () => void store.forkFrom(ub.owner!, ub.msgIdx!) : undefined}
        />
      </div>
    {:else}
      <div class="grp" class:leader={row.kind === 'leader'} class:avatar-out={avatarOutside}>
        {#if row.kind === 'group' && showAvatar}
          {@const live = i === rows.length - 1}
          <span class="tag">
            <EzAvatar size={avatarSize} kind={avatarKind} run={live && liveBusy} animate={live}>ez</EzAvatar>
          </span>
        {/if}
        <div class="gbody">
          {#each row.segs as seg}
            {#if seg.type === 'tools'}
              {@const key = seg.blocks[0].uid}
              {#if seg.blocks.length >= TOOL_GROUP_MIN}
                <ToolGroup blocks={seg.blocks} open={openGroups.has(key)} onToggle={() => onToggleGroup(key)} />
              {:else}
                {#each seg.blocks as b (b.uid)}
                  <div class:reveal={reveal?.has(b.uid)}>
                    <ToolBlock data={b} />
                  </div>
                {/each}
              {/if}
            {:else if seg.b.kind === 'tool'}
              <div class:reveal={reveal?.has(seg.b.uid)}>
                <ToolBlock data={seg.b} />
              </div>
            {:else if seg.b.kind === 'assistant'}
              {@const ab = seg.b}
              <div class:reveal={reveal?.has(ab.uid)}>
                <MessageItem
                  text={ab.text}
                  reasoning={ab.reasoning}
                  streaming={ab.streaming}
                  role="assistant"
                  bare
                  onFork={ab.owner && ab.msgIdx !== undefined && !ab.streaming
                    ? () => void store.forkFrom(ab.owner!, ab.msgIdx!)
                    : undefined}
                />
              </div>
            {:else if seg.b.kind === 'fork'}
              <div class:reveal={reveal?.has(seg.b.uid)}>
                <ForkCard fork={store.forks[seg.b.forkId]} />
              </div>
            {:else if seg.b.kind === 'decision'}
              <div id={`decision-${seg.b.id}`} class:reveal={reveal?.has(seg.b.uid)}>
                <DecisionCard data={seg.b} />
              </div>
            {:else if seg.b.kind === 'status'}
              <div class:reveal={reveal?.has(seg.b.uid)}>
                <StatusTagCard data={seg.b.data} raw={seg.b.text} />
              </div>
            {:else if seg.b.kind === 'resourcechange'}
              <div class:reveal={reveal?.has(seg.b.uid)}>
                <ResChangeCard items={seg.b.items} />
              </div>
            {:else if seg.b.kind === 'note'}
              <div class="note" class:reveal={reveal?.has(seg.b.uid)}>
                <span class="line"></span>
                {seg.b.text}
                <span class="line"></span>
              </div>
            {:else if seg.b.kind === 'trim'}
              {@const tb = seg.b}
              {@const expanded = openTrim === tb.uid}
              <div class="trim" class:folded={tb.folded} class:reveal={reveal?.has(tb.uid)}>
                <button type="button" class="tbar" onclick={() => (openTrim = expanded ? null : tb.uid)}
                  title={expanded ? '收起整理内容' : '展开整理内容'}>
                  <span class="tarrow" class:open={expanded}>{expanded ? '▾' : '▸'}</span>
                  <svg class="ticon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M4 6h16M4 12h16M4 18h10" />
                  </svg>
                  {#if tb.folded}<span class="tlabel">向上滚动加载早期对话</span>{/if}
                  <span class="tsum">{tb.text}</span>
                </button>
                {#if expanded}
                  <div class="tbody">
                    {#each trimSections(tb.full || tb.text) as sec}
                      {#if sec.title}<p class="tsec">{sec.title}</p>{/if}
                      {#each sec.lines as line}<p class="tline">{line}</p>{/each}
                    {/each}
                  </div>
                {/if}
              </div>
            {:else if seg.b.kind === 'imgload'}
              <div
                class="imgload"
                class:reveal={reveal?.has(seg.b.uid)}
                title={seg.b.paths.map((p, j) => (seg.b.notes?.[j] ? `${p}（${seg.b.notes[j]}）` : p)).join('\n')}
              >
                {#each seg.b.paths as p, j (p)}
                  {@const snap = seg.b.images?.[j]}
                  <img
                    src={snap
                      ? `data:${snap.mimeType};base64,${snap.data}`
                      : `/api/workspace/file?path=${encodeURIComponent(p)}`}
                    alt={p}
                    loading="lazy"
                    onclick={() => store.openFileAt(p)}
                    onerror={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
                    title={`${p}（点击打开文件，可编辑保存写回；缩略图为当时进上下文的快照）`}
                  />
                {/each}
                <span class="label">{seg.b.notes?.filter((n) => n).join(' · ') || '已加载上下文'}</span>
              </div>
            {:else if seg.b.kind === 'endtick'}
              <div class="endtick" class:reveal={reveal?.has(seg.b.uid)} title={seg.b.title}>
                <span class="dot" class:warn={seg.b.icon === '⚠'}>{seg.b.icon}</span>
                <span class="rtext">{seg.b.title}</span>
              </div>
            {/if}
          {/each}
        </div>
      </div>
    {/if}
  {/each}
</div>

<style>
  .rows {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  /* 拉出的上一会话内容：自上而下展开 */
  .reveal {
    animation: reveal-down 0.36s var(--ease-out) both;
  }
  @keyframes reveal-down {
    from {
      opacity: 0;
      transform: translateY(-10px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .note {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: var(--font-size-ui);
    color: var(--faint);
    padding: 4px 0;
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .note .line {
    flex: 1;
    height: 1px;
    background: var(--line);
  }
  /* 上下文整理标记：单行头 + 点开原地展开摘要全文（与工具卡一致的展开
     方式，不用弹窗）；折叠区的早期对话仍走上拉懒加载，条本身不承载
     展开/收起折叠区的语义 */
  .trim {
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg-soft);
    overflow: hidden;
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .trim.folded {
    border-style: dashed;
  }
  .tbar {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 12px;
    font: inherit;
    font-size: var(--font-size-ui);
    color: var(--faint);
    text-align: left;
  }
  .tbar:hover {
    background: color-mix(in srgb, var(--fg) 4%, transparent);
  }
  .tbar:focus {
    outline: none;
  }
  .tbar:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--accent) 35%, transparent);
    outline-offset: -2px;
  }
  .tarrow {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .ticon {
    flex: none;
    width: 14px;
    height: 14px;
    color: var(--muted);
  }
  .tlabel {
    flex: none;
    color: var(--muted);
    font-size: var(--font-size-small);
    white-space: nowrap;
  }
  .tsum {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  /* 展开区：按【小节】排的摘要正文，超高内滚，不把时间线撑出屏 */
  .tbody {
    border-top: 1px solid var(--line);
    max-height: 360px;
    overflow-y: auto;
    padding: 8px 12px 10px;
  }
  .tsec {
    margin: 10px 0 2px;
    font-size: var(--font-size-small);
    font-weight: 600;
    color: var(--fg);
  }
  .tsec:first-child {
    margin-top: 0;
  }
  .tline {
    position: relative;
    margin: 0;
    padding-left: 12px;
    font-size: var(--font-size-ui);
    line-height: 1.65;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .tline::before {
    content: '';
    position: absolute;
    left: 3px;
    top: 0.68em;
    width: 3px;
    height: 3px;
    border-radius: 50%;
    background: var(--line-strong);
  }
  /* 回复组：一个用户输入对应一个头像，组内过程块紧凑排列（间隙 7px）。
     --proc-indent 是过程卡缩进基准：组内 0（对齐正文），引导组回落 40px */
  .grp {
    display: flex;
    gap: 14px;
    align-items: flex-start;
  }
  /* 主时间线：头像挂在内容列左侧的留白里，不占内容宽度——内容左缘因此
     与输入框对齐；头像与文字留 10px 间隙，连同 48px 头像正好占满左侧
     内边距（58px），窄窗也不越界 */
  .grp.avatar-out {
    position: relative;
  }
  .grp.avatar-out > .tag {
    position: absolute;
    top: 1px;
    right: calc(100% + 10px);
    margin-top: 0;
  }
  .grp .tag {
    flex: none;
    display: grid;
    place-items: center;
    width: var(--av-size);
    height: var(--av-size);
    margin-top: 1px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    font-weight: 700;
    border: 1px solid var(--line-strong);
    border-radius: 10px;
    background: var(--bg);
    color: var(--fg);
  }
  /* 表情图上屏后去掉徽章底：头像直接露出（回退 ez 字样仍是徽章）。
     img 来自子组件，:global 逃逸作用域才能命中 */
  .grp .tag:has(> :global(img)) {
    border-color: transparent;
    background: transparent;
  }
  .gbody {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding-top: 2px;
  }
  .grp:not(.leader) .gbody {
    --proc-indent: 0px;
  }
  .grp.leader .gbody {
    gap: 10px;
  }
  /* 轮次收尾行：靠左紧凑「图标 + 结束原因」，宽约 30% 截断，悬浮看全文 */
  .endtick {
    display: flex;
    align-items: center;
    gap: 7px;
    max-width: 30%;
    padding-left: calc(var(--proc-indent, 40px) + 8px);
    font-size: var(--font-size-small);
    color: var(--faint);
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .endtick .dot {
    flex: none;
    font-size: var(--font-size-small);
    line-height: 1;
    color: var(--line-strong);
    user-select: none;
  }
  .endtick .dot.warn {
    color: #f0883e;
  }
  .endtick .rtext {
    flex: 0 1 auto;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* <image_loaded> 图片消息：缩略图小行（read_file 读图已进上下文） */
  .imgload {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-left: calc(var(--proc-indent, 40px) + 8px);
    font-size: var(--font-size-small);
    color: var(--faint);
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .imgload img {
    width: 22px;
    height: 22px;
    object-fit: cover;
    border-radius: 5px;
    border: 1px solid var(--line);
    cursor: zoom-in;
  }
  .imgload .label {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
</style>
