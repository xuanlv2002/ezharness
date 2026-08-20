<script lang="ts">
  /* 原型骨架：纯渲染层。
  「主模型 + 能力槽」设计：主模型驱动 agent 循环（可直接用多模态模型），
  其余三类是能力槽——主模型缺乏该能力时按需调用的工具后端。
  每类单选启用一个，条目含用量与花费。数据由后端下发（models.json
  演进后），接口待业务开发接入，原型阶段 cfg 为 null 占位。 */
  interface ModelEntry {
    id: string
    name: string
    provider: string
    baseUrl: string
    enabled: boolean
    usage: { tokens: number; cost: number }
  }

  interface ModelsConfig {
    main: ModelEntry[]
    vision: ModelEntry[]
    image: ModelEntry[]
    audio: ModelEntry[]
  }

  let cfg = $state<ModelsConfig | null>(null)

  const kinds = [
    { key: 'main', title: '主模型', hint: '驱动对话与工具循环，可直接选用多模态模型' },
    { key: 'vision', title: '多模态模型', hint: '主模型无视觉能力时，用于图片识别转写' },
    { key: 'image', title: '图片生成', hint: '主模型按需调用的文生图工具' },
    { key: 'audio', title: '声音生成', hint: '主模型按需调用的语音合成工具' },
  ] as const
</script>

<div class="page">
  <h1>模型</h1>

  {#each kinds as k (k.key)}
    <section>
      <h2>{k.title}</h2>
      <p class="hint">{k.hint} · 每类启用一个。</p>
      <div class="list">
        {#if cfg}
          {#each cfg[k.key] as m (m.id)}
            <div class="model" class:enabled={m.enabled}>
              <span class="dot" aria-hidden="true"></span>
              <div class="info">
                <span class="name">{m.name}</span>
                <span class="meta">{m.provider} · {m.baseUrl}</span>
              </div>
              <div class="usage">
                <span class="tokens">{m.usage.tokens.toLocaleString()} tokens</span>
                <span class="cost">${m.usage.cost.toFixed(2)}</span>
              </div>
            </div>
          {/each}
        {:else}
          <div class="model pending">
            <span class="dot" aria-hidden="true"></span>
            <div class="info">
              <span class="name">—</span>
              <span class="meta">待服务端下发</span>
            </div>
            <div class="usage">
              <span class="tokens">— tokens</span>
              <span class="cost">—</span>
            </div>
          </div>
        {/if}
        <button class="add" disabled>+ 添加模型</button>
      </div>
    </section>
  {/each}
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    max-width: 720px;
    width: 100%;
    margin: 0 auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 30px;
  }
  h1 {
    font-size: 18px;
    font-weight: 700;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  h2 {
    font-size: 13px;
    font-weight: 700;
    color: var(--muted);
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
    margin-top: -4px;
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .model {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 14px;
  }
  .model + .model,
  .model + .add,
  .add + .model {
    border-top: 1px solid var(--line);
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid var(--faint);
  }
  .model.enabled {
    background: color-mix(in srgb, var(--accent) 5%, var(--bg));
  }
  .model.enabled .dot {
    border-color: var(--accent);
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .name {
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .meta {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    overflow-wrap: anywhere;
  }
  .usage {
    flex: none;
    display: flex;
    align-items: baseline;
    gap: 12px;
  }
  .tokens {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
  }
  .cost {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--fg);
    font-weight: 600;
    white-space: nowrap;
  }
  .model.pending .name,
  .model.pending .tokens,
  .model.pending .cost {
    color: var(--faint);
  }
  .add {
    border: none;
    background: transparent;
    padding: 10px 14px;
    text-align: left;
    font-size: 12px;
    color: var(--faint);
  }
  .add:disabled {
    cursor: default;
  }
</style>
