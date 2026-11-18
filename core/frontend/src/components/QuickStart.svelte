<script lang="ts">
  /* 快速开始示例：点击把示例指令填进输入框（不直接发送，可改后再发）。 */
  import { store } from '../lib/store.svelte'

  const examples = [
    {
      label: '读懂这个项目',
      prompt: '读一遍当前项目，告诉我它是做什么的、怎么跑起来，以及各目录的职责。',
    },
    {
      label: '整理下载文件夹',
      prompt: '把下载文件夹里的文件按类型归档，先给我整理计划，确认后再动手。',
    },
    {
      label: '做个小网页工具',
      prompt: '写一个单文件 HTML 的番茄钟，支持自定时长、开始暂停和提示音，存到桌面。',
    },
  ]

  async function pick(prompt: string) {
    if (!store.activeId) await store.newChatTab()
    store.pendingPrompt = prompt
  }
</script>

<div class="quickstart">
  {#each examples as ex (ex.label)}
    <button class="qs" onclick={() => void pick(ex.prompt)} title="填入输入框（可修改后再发送）">
      <span class="qs-label">{ex.label}</span>
      <span class="qs-prompt">{ex.prompt}</span>
    </button>
  {/each}
</div>

<style>
  .quickstart {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
    width: min(560px, 100%);
    margin-top: 14px;
    text-align: left;
  }
  .qs {
    display: flex;
    flex-direction: column;
    gap: 5px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: transparent;
    padding: 11px 13px;
    color: var(--muted);
    transition:
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .qs:hover {
    border-color: var(--line-strong);
    background: var(--bg-soft);
    color: var(--fg);
  }
  .qs-label {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
  }
  .qs-prompt {
    font-size: var(--font-size-ui);
    line-height: 1.55;
    color: var(--muted);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
</style>
