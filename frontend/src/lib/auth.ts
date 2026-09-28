/* 鉴权令牌注入：?token= 转 sessionStorage；REST 走全局 fetch 补丁，SSE/WS 走 authUrl。 */

let token = ''

export function initToken(): void {
  const q = new URLSearchParams(location.search).get('token')
  if (q) {
    token = q
    sessionStorage.setItem('ez-token', q)
    history.replaceState(null, '', location.pathname)
  } else {
    token = sessionStorage.getItem('ez-token') || ''
  }
  if (!token) return
  const orig = window.fetch
  window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers)
    headers.set('X-EZ-Token', token)
    return orig(input, { ...init, headers })
  }
}

/* EventSource/WebSocket 的连接地址（token 进 query）。 */
export function authUrl(u: string): string {
  return token ? `${u}${u.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}` : u
}

/* 当前令牌。 */
export function getToken(): string {
  return token
}
