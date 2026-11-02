/*
资源页目录树的跨组件共享态（Svelte 5 runes 模块）：ExplorerTree 写入
当前根目录与所在 git 仓库信息，ResourcePane 读取（git chip 计数、变更
清单、树内高亮等）。
*/
export const explorer = $state({
  dir: '', // 树当前根目录（斜杠风格绝对路径）
  repo: '', // 所在 git 仓库根（空 = 无 git 可观测）
  changed: {} as Record<string, string>, // 仓库相对路径 → porcelain 状态码
  activeFile: '', // 资源面板当前打开的文件（树内高亮）
})

/*
目录树面板显隐与摆位：开关在「资源」标题栏（抽屉模式，WorkspaceDrawer
渲染）；弹出独立窗没有标题栏，ResourcePane 面板内自留开关。open 不跨
会话记忆——每次进入资源抽屉默认收起（ResourcePane 激活时收起），手动
展开后当次有效。docked = 钉住（面板成为抽屉左侧实体栏，占据布局宽）；
否则悬浮盖在预览区上，摆位记忆在 localStorage。
*/
export const explorerUI = $state({
  open: false,
  /* 默认钉住：悬浮盖住预览区反而碍事（资源抽屉有最小宽，横向滚动
     本就可看）；明确切回悬浮（存 '0'）的尊重其选择 */
  docked: localStorage.getItem('ez-explorer-docked') !== '0',
})

export function setExplorerOpen(v: boolean) {
  explorerUI.open = v
}

export function setExplorerDocked(v: boolean) {
  explorerUI.docked = v
  localStorage.setItem('ez-explorer-docked', v ? '1' : '0')
}
