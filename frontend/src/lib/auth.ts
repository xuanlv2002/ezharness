/* 鉴权：core API 令牌注入。
进入方式：URL ?token=（desktop 壳/快应用窗口带参打开）→ 转存 sessionStorage
并清掉地址栏；刷新后从 sessionStorage 复取。REST 经全局 fetch 补丁统一带头，
EventSource/WebSocket 不能带头，走 authUrl 进 query。 */

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

/* EventSource/WebSocket 的连接地址（token 进 query） */
export function authUrl(u: string): string {
  return token ? `${u}${u.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}` : u
}

/* 当前令牌（换端口重启跳转新 origin 时需带回 query） */
export function getToken(): string {
  return token
}
