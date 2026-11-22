<script lang="ts">
  /*
  表格查看器（xlsx/xls/csv/ods）：SheetJS 解析为 HTML 表格。只读预览,
  超大表截断显示（500 行 × 40 列）并提示。底部 sheet 切换条。
  */

  import { onMount } from 'svelte'
  import type { ResTab } from '../registry'

  let { tab }: { tab: ResTab; active?: boolean } = $props()

  const MAX_ROWS = 500
  const MAX_COLS = 40

  interface Sheet {
    name: string
    rows: string[][]
    truncated: boolean
  }

  let sheets = $state<Sheet[]>([])
  let cur = $state(0)
  let loading = $state(true)
  let err = $state('')

  onMount(async () => {
    try {
      const XLSX = await import('xlsx')
      const buf = await (await fetch('/api/workspace/file?path=' + encodeURIComponent(tab.path ?? ''))).arrayBuffer()
      const wb = XLSX.read(buf, { type: 'array' })
      sheets = wb.SheetNames.map((name) => {
        const grid = XLSX.utils.sheet_to_json<unknown[]>(wb.Sheets[name], { header: 1, blankrows: false, defval: '' })
        const rows = grid.slice(0, MAX_ROWS).map((r: unknown[]) => {
          const cells: string[] = []
          for (let c = 0; c < Math.min(r.length, MAX_COLS); c++) {
            const v = r[c]
            cells.push(v instanceof Date ? v.toLocaleString() : String(v ?? ''))
          }
          return cells
        })
        return { name, rows, truncated: grid.length > MAX_ROWS }
      })
    } catch (e) {
      err = `表格解析失败：${(e as Error).message}`
    } finally {
      loading = false
    }
  })
</script>

<div class="viewer">
  {#if loading}
    <div class="veil">解析中…</div>
  {:else if err}
    <div class="veil err">{err}</div>
  {:else if sheets[cur]}
    {@const sh = sheets[cur]}
    <div class="table-wrap">
      <table>
        {#each sh.rows as row, r}
          <tr>
            <td class="ln">{r + 1}</td>
            {#each row as cell}
              <td>{cell}</td>
            {/each}
          </tr>
        {/each}
      </table>
    </div>
    {#if sh.truncated}<div class="cap">仅显示前 {MAX_ROWS} 行</div>{/if}
    {#if sheets.length > 1}
      <div class="sheetbar">
        {#each sheets as s, i (s.name)}
          <button class:active={i === cur} onclick={() => (cur = i)}>{s.name}</button>
        {/each}
      </div>
    {/if}
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
  .table-wrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }
  table {
    border-collapse: collapse;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
  }
  td {
    border: 1px solid var(--line);
    padding: 3px 10px;
    white-space: pre;
    max-width: 480px;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--fg);
  }
  td.ln {
    position: sticky;
    left: 0;
    background: var(--bg-soft);
    color: var(--faint);
    text-align: right;
    user-select: none;
  }
  tr:first-child td {
    background: var(--bg-soft);
    font-weight: 600;
  }
  .cap {
    flex: none;
    padding: 4px 12px;
    font-size: var(--font-size-small);
    color: var(--faint);
    border-top: 1px solid var(--line);
  }
  .sheetbar {
    flex: none;
    display: flex;
    gap: 4px;
    padding: 6px 8px;
    border-top: 1px solid var(--line);
    overflow-x: auto;
  }
  .sheetbar button {
    border: 1px solid var(--line);
    background: transparent;
    border-radius: 999px;
    padding: 2px 12px;
    font-size: var(--font-size-ui);
    font-family: var(--font-mono);
    color: var(--muted);
    cursor: pointer;
    white-space: nowrap;
  }
  .sheetbar button.active {
    border-color: var(--accent-soft, var(--accent));
    color: var(--fg);
    background: var(--bg-soft);
  }
  .veil {
    flex: 1;
    display: grid;
    place-items: center;
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .veil.err {
    color: #e07a4f;
    padding: 0 20px;
    text-align: center;
  }
</style>
