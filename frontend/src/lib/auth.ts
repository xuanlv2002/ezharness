/* 鉴权令牌注入：
   - REST/SSE 走 X-EZ-Token 请求头（全局 fetch 补丁 + fetch-event-source）；
   - 浏览器 WebSocket 走 Sec-WebSocket-Protocol（不能设自定义头）；
   - 页面导航、<img>/<video>/PDF、快应用等浏览器自动发起的请求走 HttpOnly
     cookie（ez_token）——这些请求加不了请求头。
   入口 URL 的 ?token= 只用于一次性换 cookie，随后立即从地址栏摘掉。 */

let token = ''

/* installFetchPatch 给同源 /api/ 请求补 X-EZ-Token（cookie 之外的显式通道）。 */
function installFetchPatch(): void {
  const orig = window.fetch
  window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    let u: URL
    try {
      u = new URL(raw, location.href)
    } catch {
      return orig(input, init)
    }
    /* 只给本机 API 补令牌：data:/跨域请求不塞自定义头（会触发无谓的 CORS 预检） */
    if (!token || u.origin !== location.origin || !u.pathname.startsWith('/api/')) return orig(input, init)
    const headers = new Headers(init?.headers)
    headers.set('X-EZ-Token', token)
    return orig(input, { ...init, headers })
  }
}

/* initToken 解析入口令牌并确保 cookie 就绪；mount 前 await，避免首屏图片/
   接口抢跑。桌面壳由主进程直接写 cookie（URL 不带 token）；浏览器入口 URL
   带一次 token，换 HttpOnly cookie 后从地址栏摘掉。 */
export async function initToken(): Promise<void> {
  installFetchPatch()
  const params = new URLSearchParams(location.search)
  const q = params.get('token')
  token = q || sessionStorage.getItem('ez-token') || ''
  if (q) {
    sessionStorage.setItem('ez-token', q)
    /* 只摘 token，其余参数（desktop/popout）留给后读的组件（App 的 popout
       分支在 mount 时才取 location.search，全清会把弹窗渲染成主界面） */
    params.delete('token')
    const rest = params.toString()
    history.replaceState(null, '', rest ? `${location.pathname}?${rest}` : location.pathname)
  } else {
    /* 桌面壳没有 URL token：用已下发的 cookie 取回当前令牌；同时刷新
       sessionStorage 里可能残留的旧令牌（core 每次启动会换令牌）。 */
    try {
      const r = await window.fetch('/api/app/token', { signal: AbortSignal.timeout(3000) })
      if (r.ok) {
        const j = (await r.json()) as { token?: string }
        if (j.token) {
          token = j.token
          sessionStorage.setItem('ez-token', j.token)
        }
      }
    } catch {
      /* cookie 不可用：保持现状，后续请求会 401 */
    }
  }
  /* 刷新/补发 cookie：浏览器自动发起的导航、图片、PDF、快应用请求靠它鉴权 */
  if (token) {
    try {
      await window.fetch('/api/auth/cookie', {
        method: 'POST',
        headers: { 'X-EZ-Token': token },
        signal: AbortSignal.timeout(3000),
      })
    } catch {
      /* 换 cookie 失败不阻塞：请求头通道仍可用 */
    }
  }
}

/* WebSocket 认证：浏览器 WebSocket 不能设置请求头，把令牌编码成
   `ez-token.<token>` 放进 Sec-WebSocket-Protocol；core 的 authRequired
   会从该头解析。令牌是 hex，符合子协议 token 字符集。 */
export function wsProtocols(): string[] {
  return token ? [`ez-token.${token}`] : []
}

/* 当前令牌（重启跳转等少数场景需要显式携带）。 */
export function getToken(): string {
  return token
}
