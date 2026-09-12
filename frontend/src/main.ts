import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { isDesktop } from './lib/desktop'

/* 启动横幅：isometric1 等轴测 3D 字体，逐行青→紫渐变（ez 系列 logo 同款色系） */
const LOGO = [
  '     ___           ___           ___           ___           ___           ___           ___           ___           ___     ',
  '    /\\  \\         /\\  \\         /\\__\\         /\\  \\         /\\  \\         /\\__\\         /\\  \\         /\\  \\         /\\  \\    ',
  '   /::\\  \\        \\:\\  \\       /:/  /        /::\\  \\       /::\\  \\       /::|  |       /::\\  \\       /::\\  \\       /:/\\ \\  \\   ',
  '  /:/\\:\\  \\        \\:\\  \\     /:/__/        /:/\\:\\  \\     /:/\\:\\  \\     /:|:|  |      /:/\\:\\  \\     /:/\\ \\  \\     /:/\\ \\  \\  ',
  ' /::\\~\\:\\  \\        \\:\\  \\   /::\\  \\ ___   /::\\~\\:\\  \\   /::\\~\\:\\  \\   /:/|:|  |__   /::\\~\\:\\  \\   _\\:\\~\\ \\  \\   _\\:\\~\\ \\  \\ ',
  ' /:/\\:\\ \\:\\__\\ _______\\:\\__\\ /:/\\:\\  /\\__\\ /:/\\:\\ \\:\\__\\ /:/\\:\\ \\:\\__\\ /:/ |:| /\\__\\ /:/\\:\\ \\:\\__\\ /\\ \\:\\ \\ \\__\\ /\\ \\:\\ \\ \\__\\',
  ' \\:\\~\\:\\ \\/__/ \\::::::::/__/ \\/__\\:\\/:/  / \\/__\\:\\/:/  / \\/_|::\\/:/  / \\/__|:|/:/  / \\:\\~\\:\\ \\/__/ \\:\\ \\:\\ \\/__/ \\:\\ \\:\\ \\/__/',
  '  \\:\\ \\:\\__\\    \\:\\~~\\~~          \\::/  /       \\::/  /     |:|::/  /      |:/:/  /   \\:\\ \\:\\__\\    \\:\\ \\:\\__\\    \\:\\ \\:\\__\\  ',
  '   \\:\\ \\/__/     \\:\\  \\           /:/  /        /:/  /      |:|\\/__/       |::/  /     \\:\\ \\/__/     \\:\\/:/  /     \\:\\/:/  /  ',
  '    \\:\\__\\        \\:\\__\\         /:/  /        /:/  /       |:|  |         /:/  /       \\:\\__\\        \\::/  /       \\::/  /   ',
  '     \\/__/         \\/__/         \\/__/         \\/__/         \\|__|         \\/__/         \\/__/         \\/__/         \\/__/    ',
]

const lerpHex = (a: number, b: number, t: number) =>
  Math.round(a + (b - a) * t)
    .toString(16)
    .padStart(2, '0')
const grad = (t: number) =>
  `#${lerpHex(0x0e, 0x8b, t)}${lerpHex(0xa5, 0x5c, t)}${lerpHex(0xe9, 0xf6, t)}`

LOGO.forEach((line, i) => {
  console.log(`%c${line}`, `color:${grad(i / (LOGO.length - 1))};font-family:ui-monospace,Consolas,monospace`)
})

const app = mount(App, { target: document.getElementById('app')! })

/* 外部链接拦截（捕获阶段）：点击消息/页面里的外链在系统浏览器打开。
   桌面壳窗口当前页导航会离开应用（SPA 被顶掉无法返回），绝不能放行；
   浏览器模式直接 window.open；同源相对链接（快应用等）不拦。 */
document.addEventListener(
  'click',
  (e) => {
    const a = (e.target as HTMLElement)?.closest?.('a[href]')
    if (!(a instanceof HTMLAnchorElement)) return
    const href = a.getAttribute('href') || ''
    if (!/^https?:\/\//i.test(href)) return
    let origin = ''
    try {
      origin = new URL(href, location.href).origin
    } catch {
      return
    }
    if (origin === location.origin) return
    e.preventDefault()
    e.stopPropagation()
    const desktopWindow = (window as any).ez?.window
    if (isDesktop && desktopWindow) {
      desktopWindow.openUrl(href)
    } else {
      window.open(href, '_blank', 'noopener')
    }
  },
  true,
)

export default app
