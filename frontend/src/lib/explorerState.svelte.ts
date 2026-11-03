/*
资源页目录树的跨组件共享态（Svelte 5 runes 模块）：ExplorerTree 写入
当前根目录与视角下的 git 仓库信息，ResourcePane 读取（对比按钮可用性、
树内高亮等）。仓库按根分键存放——根目录与已展开子目录各属各仓，轮询
谁就更新谁，互不覆写（多仓并列不闪）。
*/
export const explorer = $state({
  dir: '', // 树当前根目录（斜杠风格绝对路径）
  repos: {} as Record<string, Record<string, string>>, // 仓库根 → 仓库相对路径 → porcelain 状态码
  activeFile: '', // 资源面板当前打开的文件（树内高亮）
})

/*
repoOf 返回路径所在的已知仓库根（多仓嵌套时取最长前缀；空串 = 不在
任何已知仓库内）。徽标、对比按钮的可用性都从这里判，不再依赖单一
"当前仓库"。
*/
export function repoOf(path: string): string {
  let best = ''
  for (const r of Object.keys(explorer.repos)) {
    if ((path === r || path.startsWith(r + '/')) && r.length > best.length) best = r
  }
  return best
}

/* statusOf 返回路径的工作区状态码（空串 = 无变更/不在已知仓库内）。 */
export function statusOf(path: string): string {
  const r = repoOf(path)
  return r ? (explorer.repos[r][path.slice(r.length + 1)] ?? '') : ''
}

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
