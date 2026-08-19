<script lang="ts">
  let {
    text,
    reasoning = '',
    streaming = false,
    role,
  }: {
    text: string
    reasoning?: string
    streaming?: boolean
    role: 'user' | 'assistant'
  } = $props()
</script>

{#if role === 'user'}
  <div class="user enter-rise">
    <span class="tag">你</span>
    <div class="bubble">{text}</div>
  </div>
{:else}
  <div class="assistant">
    <span class="tag">ez</span>
    <div class="body">
      {#if reasoning}
        <details class="reasoning">
          <summary>思考过程</summary>
          <div class="reasoning-text">{reasoning}</div>
        </details>
      {/if}
      {#if text}
        <div class="text">{text}</div>
      {:else if streaming}
        <div class="text"><span class="caret"></span></div>
      {/if}
      {#if streaming && text}<span class="caret"></span>{/if}
    </div>
  </div>
{/if}

<style>
  .user,
  .assistant {
    display: flex;
    gap: 14px;
    align-items: flex-start;
  }
  .tag {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    margin-top: 2px;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    border: 1px solid var(--line-strong);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
  }
  .user .tag {
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-color: var(--bg-invert);
  }
  .bubble {
    background: var(--bg-invert);
    color: var(--fg-invert);
    padding: 10px 14px;
    border-radius: 12px;
    white-space: pre-wrap;
    word-break: break-word;
    max-width: 86%;
  }
  .body {
    min-width: 0;
    flex: 1;
    padding-top: 3px;
  }
  .text {
    white-space: pre-wrap;
    word-break: break-word;
  }
  .caret {
    display: inline-block;
    width: 8px;
    height: 17px;
    margin-left: 2px;
    vertical-align: -2px;
    background: var(--fg);
    animation: caret 1s steps(1) infinite;
  }
  .reasoning {
    margin-bottom: 8px;
    border-left: 2px solid var(--line);
    padding-left: 12px;
    color: var(--muted);
    font-size: 13px;
  }
  .reasoning summary {
    cursor: pointer;
    user-select: none;
    font-size: 12px;
    letter-spacing: 0.02em;
  }
  .reasoning-text {
    white-space: pre-wrap;
    margin-top: 6px;
    line-height: 1.6;
  }
</style>
