<script lang="ts">
  /*
  音视频查看器：原生 <video>/<audio> 直指工作目录文件服务（后端
  ServeContent 原生支持 Range,拖动进度条可用）。容器/编码 Chromium
  不支持时给出明确提示（mkv/avi 内编码常见不支持）。
  */

  import type { ResTab } from '../registry'

  let { tab }: { tab: ResTab; active?: boolean } = $props()

  const isAudio = /\.(mp3|wav|ogg|flac|m4a|aac)$/i.test(tab.path ?? '')
  const url = '/api/workspace/file?path=' + encodeURIComponent(tab.path ?? '')
  let err = $state('')
</script>

<div class="viewer">
  {#if err}
    <div class="veil err">
      <p>{err}</p>
      <p class="sub">该容器/编码不受内嵌 Chromium 支持——可在资源管理器定位后用本地播放器打开</p>
    </div>
  {:else if isAudio}
    <div class="stage"><audio controls src={url} onerror={() => (err = '音频无法解码')}></audio></div>
  {:else}
    <video controls src={url} onerror={() => (err = '视频无法解码')}></video>
  {/if}
</div>

<style>
  .viewer {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    overflow: hidden;
  }
  video {
    flex: 1;
    min-height: 0;
    width: 100%;
    background: #000;
  }
  .stage {
    flex: 1;
    display: grid;
    place-items: center;
    padding: 24px;
  }
  audio {
    width: min(480px, 100%);
  }
  .veil {
    flex: 1;
    display: grid;
    place-content: center;
    gap: 6px;
    color: var(--muted);
    font-size: var(--font-size-section);
    text-align: center;
    padding: 0 24px;
  }
  .veil.err {
    color: #e07a4f;
  }
  .veil .sub {
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
</style>
