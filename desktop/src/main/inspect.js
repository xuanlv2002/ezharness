/* 右键检查菜单：Electron 默认没有右键菜单；开发者工具一律独立窗口（dock 会挤坏视图 bounds） */
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
