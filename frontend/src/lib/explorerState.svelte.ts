/*
资源页目录树的跨组件共享态（Svelte 5 runes 模块）：ExplorerTree 写入
当前根目录与所在 git 仓库信息，ResourcePane 读取——决定「变更对比」
开关的可见性与从树里打开文件时是否自动进对比视图。
*/
function loadAutoDiffDirs(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem('ez-explorer-autodiff-dirs') ?? '[]')
    return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : []
  } catch {
    return []
  }
}

export const explorer = $state({
  dir: '', // 树当前根目录（斜杠风格绝对路径）
  repo: '', // 所在 git 仓库根（空 = 无 git 可观测）
  changed: {} as Record<string, string>, // 仓库相对路径 → porcelain 状态码
  activeFile: '', // 资源面板当前打开的文件（树内高亮）
  /* 自动对比按根目录各自记忆：切到别的目录不受影响（git chip 上切换） */
  autoDiffDirs: loadAutoDiffDirs(),
})

/* autoDiffOn 该根目录是否开着自动对比：开启后从目录树打开有未提交
   变更的文本文件直接进 HEAD 对比视图，不必逐个手点「对比」。
   注意只对 git 视角有变更的文件生效（被 .gitignore 忽略的文件没有
   「变更」概念，点了也正常打开）。 */
export function autoDiffOn(dir: string): boolean {
  return explorer.autoDiffDirs.includes(dir)
}

/* setAutoDiff 切换某目录的自动对比（localStorage 记忆，最多留 12 个目录）。 */
export function setAutoDiff(dir: string, on: boolean) {
  const rest = explorer.autoDiffDirs.filter((d) => d !== dir)
  explorer.autoDiffDirs = on ? [dir, ...rest].slice(0, 12) : rest
  localStorage.setItem('ez-explorer-autodiff-dirs', JSON.stringify(explorer.autoDiffDirs))
}

/* removeAutoDiffDir 目录从自动对比记忆里清掉（快捷目录被移除时联动）。 */
export function removeAutoDiffDir(path: string) {
  if (!explorer.autoDiffDirs.includes(path)) return
  explorer.autoDiffDirs = explorer.autoDiffDirs.filter((d) => d !== path)
  localStorage.setItem('ez-explorer-autodiff-dirs', JSON.stringify(explorer.autoDiffDirs))
}

/* hasGitChange 文件（斜杠绝对路径）是否有未提交变更。 */
export function hasGitChange(absPath: string): boolean {
  if (!explorer.repo || !absPath.startsWith(explorer.repo + '/')) return false
  return explorer.changed[absPath.slice(explorer.repo.length + 1)] !== undefined
}
