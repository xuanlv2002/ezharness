<script lang="ts">
  import Logo from './Logo.svelte'
</script>

<div class="timeline">
  <div class="inner">
    <div class="empty">
      <div class="mark"><Logo size={56} /></div>
      <p>向 ezharness 发出第一条指令——它可全权操作本机。</p>
    </div>
  </div>
</div>

<style>
  .timeline {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  .inner {
    max-width: 780px;
    margin: 0 auto;
    padding: 40px 48px 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .empty {
    padding: 120px 0;
    text-align: center;
    color: var(--muted);
  }
  .mark {
    display: inline-grid;
    place-items: center;
    width: 56px;
    height: 56px;
    margin-bottom: 16px;
  }
</style>
