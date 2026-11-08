/*
三个工具页的 tab 数（顶栏按钮角标）：各 pane 挂载期间自报、卸载清零，
只反映本窗口（弹出窗/便签小窗各有自己的实例）。
*/
export const paneTabs = $state({ term: 0, browser: 0, file: 0 })
