/*
Ctrl+F 的键盘归属（资源查看器 ↔ 页面查找）。

两处都在 window 捕获阶段收 Ctrl+F，靠注册顺序决定谁先跑不可靠，改用
共享标志：

  - file：最近一次指针按下是否落在资源页内（点预览/目录/tab 都算；
    点在聊天列即归零）——点过右侧再按 Ctrl+F，就是文件内查找。
  - enabled：资源页当前有没有可查找的文本 tab（图片/差异/抽屉收起时
    没有），由 ResourcePane 的响应式状态持续刷新。

FindBar 看到 file && enabled 就让位；ResourcePane 看到同一条件就接管。
HTML 预览的 iframe 有独立文档，父页收不到它的指针/键盘事件，由
HtmlViewer 在 frame 文档里补转发（置位 + Ctrl+F 回调）。
*/

export const findScope = {
  file: false,
  enabled: false,
}
