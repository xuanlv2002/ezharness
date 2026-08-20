<script lang="ts">
  /* 原型骨架：纯渲染层。
  路径与端口由后端下发——后端启动时检测当前文件夹有无配置文件，
  没有则以 exe 首次执行所在文件夹为默认新建。接口待业务开发接入，
  原型阶段 cfg 为 null 占位。 */
  interface PathEntry {
    label: string
    file: string
    path: string
  }

  interface AppConfig {
    port: number
    paths: PathEntry[]
    memory: { longterm: string; skills: string; topics: string }
  }

  let cfg = $state<AppConfig | null>(null)
  let port = $state<number | ''>('')
</script>

<div class="page">
  <h1>设置</h1>

  <section>
    <h2>服务端配置</h2>
    <label class="field">
      <span>端口</span>
      <input type="number" bind:value={port} min="1" max="65535" placeholder={cfg ? String(cfg.port) : '—'} />
    </label>
    <button class="save" disabled>保存</button>
    <p class="hint">变更端口后需重启服务端生效。</p>
  </section>

  <section>
    <h2>数据与配置文件</h2>
    <p class="hint">
      应用根 = ezharness.exe 首次执行时所在文件夹；全部数据与配置记录存放于其下。
    </p>
    {#if cfg}
      <div class="paths">
        {#each cfg.paths as p (p.file)}
          <div class="path-row">
            <span class="label">{p.label}<i>{p.file}</i></span>
            <span class="value">{p.path}</span>
          </div>
        {/each}
      </div>
    {:else}
      <div class="paths pending">
        <span>路径由服务端启动时生成——待接入</span>
      </div>
    {/if}
  </section>

  <section>
    <h2>记忆文件夹</h2>
    <p class="hint">初始为 memory/ 下三个子文件夹，可分别指向独立位置。</p>
    <div class="paths">
      {#if cfg}
        <div class="path-row">
          <span class="label">长期记忆<i>harness.md 索引 + 文件</i></span>
          <span class="value">{cfg.memory.longterm}</span>
        </div>
        <div class="path-row">
          <span class="label">能力记忆<i>skills</i></span>
          <span class="value">{cfg.memory.skills}</span>
        </div>
        <div class="path-row">
          <span class="label">话题记忆<i>历史 session</i></span>
          <span class="value">{cfg.memory.topics}</span>
        </div>
      {:else}
        <div class="path-row">
          <span class="label">长期记忆<i>harness.md 索引 + 文件</i></span>
          <span class="value">—</span>
        </div>
        <div class="path-row">
          <span class="label">能力记忆<i>skills</i></span>
          <span class="value">—</span>
        </div>
        <div class="path-row">
          <span class="label">话题记忆<i>历史 session</i></span>
          <span class="value">—</span>
        </div>
      {/if}
    </div>
  </section>
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
    gap: 36px;
  }
  h1 {
    font-size: 18px;
    font-weight: 700;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h2 {
    font-size: 13px;
    font-weight: 700;
    color: var(--muted);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 12px;
    color: var(--muted);
    max-width: 240px;
  }
  input {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12.5px;
    outline: none;
    background: var(--bg);
    color: var(--fg);
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  input:focus {
    border-color: var(--line-strong);
  }
  .save {
    align-self: flex-start;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 8px 16px;
    font-size: 13px;
    font-weight: 550;
  }
  .save:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
  }
  .paths {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .path-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    padding: 10px 14px;
  }
  .path-row + .path-row {
    border-top: 1px solid var(--line);
  }
  .label {
    display: flex;
    flex-direction: column;
    font-size: 12px;
    color: var(--fg);
    white-space: nowrap;
  }
  .label i {
    font-style: normal;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
  }
  .value {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
    text-align: right;
    overflow-wrap: anywhere;
  }
  .pending {
    padding: 18px 14px;
    font-size: 12px;
    color: var(--faint);
    font-family: var(--font-mono);
  }
</style>
