/* 桌面壳判定：桌面窗口 URL 带 ?desktop=1（TitleBar/看板入口等据此区分） */
export const isDesktop = new URLSearchParams(location.search).has('desktop')

/* openExternal 用系统默认浏览器打开外部 URL：桌面壳走 preload 的 openUrl
   （主进程 shell.openExternal），web 端退化为 window.open。 */
export function openExternal(url: string) {
  if (!/^https?:\/\//i.test(url)) return
  const desktopWindow = (window as any).ez?.window
  if (desktopWindow?.openUrl) desktopWindow.openUrl(url)
  else window.open(url, '_blank', 'noopener')
}
