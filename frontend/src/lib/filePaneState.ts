/*
资源页 tab 状态的跨窗口胶囊：弹出/回流时在两个渲染进程间经主进程搬运。
serialize/restore 由当前窗口挂载的 ResourcePane 实例注册（每窗口至多一个）。
*/
export const filePane = {
  serialize: (): string | null => null,
  restore: (_json: string) => {},
}
