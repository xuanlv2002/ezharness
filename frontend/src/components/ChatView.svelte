<script lang="ts">
  import Timeline from './Timeline.svelte'
  import InputBar from './InputBar.svelte'
  import StatusCard from './StatusCard.svelte'
</script>

<div class="chat">
  <Timeline />
  <StatusCard />
  <InputBar />
</div>

<style>
  .chat {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
</style>
