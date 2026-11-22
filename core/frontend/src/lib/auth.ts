/* 会话令牌：REST/SSE 走 EzHarnessToken 请求头（fetch 补丁统一注入），WebSocket
   走子协议 ["agent-auth", token]，浏览器自动请求（img/video/PDF）走登录时下发
   的 HttpOnly cookie。令牌持久（core 重启不换），存 localStorage 跨标签页共享。
   当前阶段三端都用默认密码静默登录，不向用户展示登录界面。 */

const DEFAULT_PASSWORD = 'ezharness'

let token = ''

/* installFetchPatch 给同源 /api/ 请求补 EzHarnessToken（cookie 之外的显式通道）。 */
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
    headers.set('EzHarnessToken', token)
    return orig(input, { ...init, headers }).then((res) => {
      /* 会话凭证失效（被吊销/格式化）：广播跳登录。auth 端点自身的 401 是
         密码错误，open 面 401 是令牌问题，都不算会话失效 */
      if (
        res.status === 401 &&
        !u.pathname.startsWith('/api/auth/') &&
        !u.pathname.startsWith('/api/open/')
      ) {
        window.dispatchEvent(new CustomEvent('ez-auth-expired'))
      }
      return res
    })
  }
}

/* loadToken 装载本地令牌（mount 前调用：首屏接口就要带头）。已有令牌顺带
   同步给桌面壳：壳的 session.json 丢失/升级残留时，浏览器桥不必等下次登录。 */
export function loadToken(): void {
  installFetchPatch()
  token = localStorage.getItem('ez-token') || ''
  if (token) (window as any).ez?.auth?.save?.(token)
}

/* ensureAuth 保证挂载前有可用令牌：本地令牌仍有效就复用，失效/缺失时用
   默认密码换一枚。core 启动时已备好默认密码，正常情况下不会失败。 */
export async function ensureAuth(): Promise<void> {
  if (token) {
    const res = await fetch('/api/auth/sessions', { headers: { EzHarnessToken: token } })
    if (res.ok) return
  }
  const client = (window as any).ez?.desktop ? 'desktop' : 'web'
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: DEFAULT_PASSWORD, client }),
  })
  if (!res.ok) throw new Error(`自动登录失败（HTTP ${res.status}）`)
  const { token: t } = await res.json()
  setToken(t)
}

/* setToken 保存静默登录签发的令牌，并同步给桌面壳主进程（浏览器桥要用）。 */
export function setToken(t: string): void {
  token = t
  localStorage.setItem('ez-token', t)
  ;(window as any).ez?.auth?.save?.(t)
}

/* clearToken 登出/凭证失效时清空。 */
export function clearToken(): void {
  token = ''
  localStorage.removeItem('ez-token')
  ;(window as any).ez?.auth?.save?.('')
}

/* WebSocket 认证：浏览器 WebSocket 不能设置请求头，把令牌作为第二个子协议
   传入——new WebSocket(url, ["agent-auth", token])；core 从该头解析校验。 */
export function wsProtocols(): string[] {
  return token ? ['agent-auth', token] : []
}

/* 当前令牌（重启跳转等少数场景需要显式携带）。 */
export function getToken(): string {
  return token
}
