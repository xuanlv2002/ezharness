/*
内嵌快应用视图状态：只有左侧钉住菜单把应用开在主窗口里（App.svelte 的 app
视图渲染 iframe）；快应用页「启动」、对话 app:// chip、桌面宠物窗都走独立
窗口。current 为空时视图回落提示页。
*/
export interface QuickAppRef {
  name: string
  title: string
  path: string
}

class QuickAppState {
  current = $state<QuickAppRef | null>(null)
}

export const quickApp = new QuickAppState()
