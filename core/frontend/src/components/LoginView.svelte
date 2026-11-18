<script lang="ts">
  /* 登录门：首次（未初始化）走设密码引导，之后走密码登录；成功后令牌
     入 localStorage + HttpOnly cookie（资产请求），桌面壳同步给主进程。 */
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { setToken } from '../lib/auth'

  let { onDone } = $props()

  let mode = $state<'loading' | 'init' | 'login'>('loading')
  let pw = $state('')
  let pw2 = $state('')
  let err = $state('')
  let busy = $state(false)
  const ez = (window as any).ez
  const client = ez?.desktop ? 'desktop' : 'web'

  /* 服务器切换只在桌面壳主窗出现：web 端页面本来就由 core 伺服，宠物/通知/
     弹出窗也不需要换 core。 */
  const params = new URLSearchParams(location.search)
  const showServer =
    !!ez?.desktop &&
    !!ez.server?.get &&
    !!ez.server?.connect &&
    !params.has('pet') &&
    !params.has('notice') &&
    !params.has('pins') &&
    !params.has('popout')
  let server = $state('')
  let currentBase = $state('')
  let serverBusy = $state(false)
  let serverMsg = $state('')

  const isLoopback = (host: string) => host === '127.0.0.1' || host === 'localhost' || host === '::1' || host === '[::1]'

  /* serverOrigin 把输入补成 origin：无协议默认 http、无端口默认 5260。 */
  function serverOrigin(raw: string): string {
    let s = raw.trim()
    if (!s) return ''
    const hasScheme = /^https?:\/\//i.test(s)
    if (!hasScheme && s.includes('://')) return ''
    if (!hasScheme) s = 'http://' + s
    try {
      const u = new URL(s)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') return ''
      if (!hasScheme && !u.port) u.port = '5260'
      return u.origin
    } catch {
      return ''
    }
  }

  /* 127.0.0.1 与 localhost 指向同一个本机 core，不算切换（否则会白丢令牌） */
  function sameBase(a: string, b: string): boolean {
    if (!a || !b) return false
    try {
      const ua = new URL(a)
      const ub = new URL(b)
      return ua.protocol === ub.protocol && ua.port === ub.port && (ua.hostname === ub.hostname || (isLoopback(ua.hostname) && isLoopback(ub.hostname)))
    } catch {
      return false
    }
  }

  /* 有显式端口且不是 https 时省掉协议，默认本机显示成 localhost:5260；
     其它情况保留协议，避免把 https/80 地址误当成本机默认端口的输入。 */
  function displayBase(base: string): string {
    if (!base) return ''
    const u = new URL(base)
    const host = u.host.replace(/^127\.0\.0\.1(?=:|$)/, 'localhost')
    return u.port === '' || u.protocol === 'https:' ? `${u.protocol}//${host}` : host
  }
  const serverDirty = $derived(showServer && !sameBase(serverOrigin(server), currentBase))

  async function connectServer(): Promise<void> {
    if (!showServer) return
    const base = serverOrigin(server)
    if (!base) {
      serverMsg = '地址格式不对，例：localhost:5260'
      return
    }
    if (sameBase(base, currentBase)) {
      serverMsg = ''
      server = displayBase(currentBase)
      return
    }
    serverBusy = true
    serverMsg = ''
    try {
      const r = await ez.server.connect(base)
      if (!r?.ok) {
        serverMsg = r?.error || '连接失败'
        return
      }
      location.href = `${base}${location.pathname}${location.search}`
    } catch (e) {
      serverMsg = e instanceof Error ? e.message : '连接失败'
    } finally {
      serverBusy = false
    }
  }

  /* refreshMode 现问一次服务端：页面可能是在初始化之前打开的，之后密码
     在别的窗口/终端设过，这个页面不能一直卡在"设密码"。 */
  async function refreshMode(): Promise<void> {
    try {
      mode = (await api.authStatus()).initialized ? 'login' : 'init'
      err = ''
    } catch {
      err = '后端不可达，请确认 core 已启动后刷新重试'
    }
  }

  onMount(() => {
    void refreshMode()
    if (showServer) {
      void ez.server
        .get()
        .then((r: { base?: string } | null) => {
          currentBase = r?.base || ''
          server = displayBase(currentBase)
        })
        .catch(() => {})
    }
    /* 切回窗口时重查：别处刚设过密码/刚格式化，这里要跟着变 */
    const onFocus = () => void refreshMode()
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  })

  function switchMode(): void {
    mode = mode === 'init' ? 'login' : 'init'
    err = ''
    pw = ''
    pw2 = ''
  }

  async function submit() {
    if (busy || !pw) return
    if (mode === 'init' && pw !== pw2) {
      err = '两次输入不一致'
      return
    }
    if (showServer && serverDirty) {
      await connectServer()
      return
    }
    busy = true
    err = ''
    try {
      const r = mode === 'init' ? await api.authInit(pw, client) : await api.authLogin(pw, client)
      setToken(r.token)
      onDone()
    } catch (e) {
      err = e instanceof Error ? e.message : String(e)
      if (mode === 'init') {
        /* 密码其实已经存在（别处设过）：自动切到登录，别让人卡死 */
        const st = await api.authStatus().catch(() => null)
        if (st?.initialized) {
          mode = 'login'
          err = '密码已经设置过，请输入密码登录'
          pw = ''
          pw2 = ''
        }
      }
    } finally {
      busy = false
    }
  }
</script>

<div class="page login">
  <h1>{mode === 'init' ? '初始化 ezharness' : '登录 ezharness'}</h1>
  <p class="hint">
    {mode === 'init'
      ? '首次使用：设置一个密码。桌面端、终端、浏览器首次连接都要用它登录，凭证各自保存、丢失后重新登录即可。'
      : '输入密码登录。'}
  </p>
  {#if mode === 'loading'}
    <p class="hint">检查认证状态…</p>
  {:else}
    <form onsubmit={(e) => { e.preventDefault(); void submit() }}>
      {#if showServer}
        <div class="field">
          <span>服务器</span>
          <span class="server-row">
            <input type="text" bind:value={server} placeholder="localhost:5260" spellcheck="false" />
            <button
              type="button"
              class="connect"
              disabled={serverBusy || !currentBase || !serverDirty}
              onclick={() => void connectServer()}
            >
              {serverBusy ? '连接中…' : '连接'}
            </button>
          </span>
        </div>
        {#if serverMsg}
          <p class="err">{serverMsg}</p>
        {/if}
      {/if}
      <label class="field">
        <span>密码</span>
        <input type="password" bind:value={pw} autofocus />
      </label>
      {#if mode === 'init'}
        <label class="field">
          <span>再输一次</span>
          <input type="password" bind:value={pw2} />
        </label>
      {/if}
      <button class="primary" disabled={busy || !pw || (showServer && !currentBase)}>
        {busy ? '提交中…' : mode === 'init' ? '设置并进入' : '登录'}
      </button>
      {#if err}
        <p class="err">{err}</p>
      {/if}
      <button type="button" class="switch" onclick={switchMode}>
        {mode === 'init' ? '已有密码？直接登录' : '还没设过密码？去初始化'}
      </button>
    </form>
  {/if}
</div>

<style>
  .login {
    max-width: 420px;
    margin: 12vh auto 0;
  }

  h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }

  .hint {
    color: var(--muted);
    font-size: 12.6px;
    line-height: 1.6;
  }

  form {
    display: grid;
    gap: 12px;
    margin-top: 16px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: var(--font-size-ui);
    color: var(--muted);
  }

  .field input {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-size: var(--font-size-section);
    outline: none;
    background: var(--bg);
    color: var(--fg);
    transition: border-color var(--dur-fast) var(--ease-out);
  }

  .field input:focus {
    border-color: var(--line-strong);
  }

  .server-row {
    display: flex;
    gap: 8px;
  }

  .server-row input {
    flex: 1;
    min-width: 0;
  }

  .connect {
    flex: none;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 0 12px;
    background: var(--bg-soft);
    color: var(--fg);
    font-size: var(--font-size-ui);
  }

  .connect:hover:not(:disabled) {
    border-color: var(--line-strong);
  }

  .connect:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .primary {
    margin-top: 4px;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 9px 18px;
    font-size: var(--font-size-section);
    font-weight: 500;
  }

  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .err {
    font-size: var(--font-size-ui);
    color: var(--danger);
  }

  .switch {
    justify-self: start;
    padding: 0;
    color: var(--muted);
    font-size: var(--font-size-ui);
    text-decoration: underline;
    text-underline-offset: 3px;
  }

  .switch:hover {
    color: var(--fg);
  }
</style>
