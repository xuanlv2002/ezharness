/*
主题（浅/暗）的响应式唯一源。初值来自 index.html 的预置脚本（首帧前
已按 localStorage/系统偏好写好根节点 data-theme，这里只认领），切换时
同步根节点与 localStorage；图表、终端等运行时读色的组件引用
theme.current 即可跟随重绘。
*/
export type Theme = 'light' | 'dark'

export const theme = $state<{ current: Theme }>({
  current: (document.documentElement.dataset.theme as Theme) || 'light',
})

export function toggleTheme() {
  theme.current = theme.current === 'dark' ? 'light' : 'dark'
  document.documentElement.dataset.theme = theme.current
  localStorage.setItem('ez-theme', theme.current)
}
