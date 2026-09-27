/*
右键检查菜单：给一个 webContents 挂「检查元素 / 开发者工具」。Electron
默认没有右键菜单（Chromium 那套要应用自己建），浏览器标签与快应用页因此
无法就地查看元素与调试。页面自己 preventDefault 的右键不触发本菜单
（渲染进程不请求菜单，Chromium 直接走页面逻辑），互不打扰。

开发者工具一律独立窗口：dock 模式会改变内容区尺寸，而这些页面（浏览器
标签视图）的 bounds 由呈现层上报，被 dock 挤压后会留下错位。
*/
const { Menu } = require('electron')

/* attachInspectMenu 给 webContents 装右键菜单（可重复调用，无副作用）。 */
function attachInspectMenu(wc) {
  wc.on('context-menu', (_e, params) => {
    const open = wc.isDevToolsOpened()
    Menu.buildFromTemplate([
      { label: '检查元素', click: () => wc.inspectElement(params.x, params.y) },
      {
        label: open ? '关闭开发者工具' : '打开开发者工具',
        click: () => (open ? wc.closeDevTools() : wc.openDevTools({ mode: 'detach' })),
      },
    ]).popup()
  })
}

module.exports = { attachInspectMenu }
