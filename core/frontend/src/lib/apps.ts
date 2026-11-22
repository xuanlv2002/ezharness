/*
快应用启动：入口 URL 不带令牌；页面调 OpenAPI 只用自己的 ez.config 令牌，
登录 cookie 在 OpenAPI 面不生效。两种承载：左侧钉住菜单在主窗口内嵌
展示（quickApp 状态 + app 视图）；快应用页「启动」、对话 app:// chip、
桌面宠物窗都开独立窗口。
*/
import { api } from './api'
import { quickApp } from './quickApp.svelte'

/* openAppInside 只给左侧钉住菜单用：启动后端（如有）后把入口交给 app 视图，
   调用方随后切到 'app' 视图。不顺手拉终端抽屉——后端终端在终端页
   「快应用后端」分组里按需查看。 */
export async function openAppInside(name: string): Promise<void> {
  const r = await api.openApp(name)
  quickApp.current = { name, title: r.title, path: r.path }
}

/* openAppWindow 在独立窗口打开（快应用页启动、对话 app:// chip、桌面宠物窗；
   已弹出则新建，desktop 壳由主进程管）。只开窗口，不顺手拉终端抽屉。 */
export async function openAppWindow(name: string): Promise<void> {
  const r = await api.openApp(name)
  openWindow(r.path, r.title)
}

function openWindow(url: string, title: string) {
  const desktopWindow = (window as any).ez?.window
  if (desktopWindow) desktopWindow.openApp(url, title)
  else window.open(url, '_blank', 'noopener') // 不带 opener：应用页拿不到主界面的 sessionStorage
}
