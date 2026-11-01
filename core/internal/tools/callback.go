/*
callback 工具：agent 管理全局定时回调（callbacks.json）的唯一入口。
任务是资源——清单在 system 的 <callbacks> 段，轮内变更由
<resource_change> 报告；本工具负责增删改查、启停与立即运行。
*/
package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/callback"
)

/*
CallbackAPI 是工具所需的回调服务面（service 包实现；tools 不能反向
依赖 service，故用窄接口）。nil 时不注册该工具。
*/
type CallbackAPI interface {
	List() []callback.Task
	Create(ctx context.Context, in callback.CreateInput) (callback.Task, error)
	Update(ctx context.Context, id string, in callback.UpdateInput) (callback.Task, error)
	Delete(ctx context.Context, id string) error
	SetEnabled(ctx context.Context, id string, enabled bool) (callback.Task, error)
	RunNow(id string) error
}

/* callbackArgs 是 callback 工具的参数面。 */
type callbackArgs struct {
	Action      string                 `json:"action" desc:"list | create | update | delete | enable | disable | run"`
	ID          string                 `json:"id,omitempty" desc:"任务 ID（update/delete/enable/disable/run 必填；list 可省略）"`
	Title       string                 `json:"title,omitempty" desc:"标题（create 必填；update 传新值才改）"`
	Description *string                `json:"description,omitempty" desc:"描述（update 可传空串清空）"`
	Kind        string                 `json:"kind,omitempty" desc:"session | app | script（create 必填；update 可改）"`
	Enabled     *bool                  `json:"enabled,omitempty" desc:"是否启用；create 缺省 true"`
	Schedule    *callback.Schedule     `json:"schedule,omitempty" desc:"计划：{kind: once|interval|cron, at/every/cron}"`
	Session     *callback.SessionInput `json:"session,omitempty" desc:"会话目标：{mode: session|new, message}；session 绑定当前会话"`
	App         *callback.AppTarget    `json:"app,omitempty" desc:"快应用目标：{name,callback}；callback 是 app.quick callbacks[].name"`
	Script      *callback.ScriptTarget `json:"script,omitempty" desc:"脚本目标：{command}；在工作目录执行 shell 命令"`
}

/* Callback 返回定时回调工具；currentSession 用于绑定/标注当前叶会话。 */
func Callback(api CallbackAPI, currentSession func() string) types.Tool {
	return types.NewTool("callback",
		"管理全局定时回调（与快应用/skill/MCP 同级的资源；页面在「回调」模块）。"+
			"任务三类：kind=session 定时向会话发消息——mode=session 发到创建任务时的当前会话（到点若正在跑轮由 chat 发送逻辑先打断当前轮；该会话归档或 compact 换代后任务失败，不会继承到新会话），mode=new 每次触发新建会话；"+
			"kind=app 定时触发快应用——app.callback 填 app.quick 的 callbacks[].name，触发时自动启动后端并调用该端点（先用 load_app 查看可用端点）；"+
			"kind=script 在工作目录执行 shell 命令——script.command 写完整命令（如 './sync.sh'、'python tools/sync.py'），捕获输出尾部与退出码。"+
			"计划 schedule：once {at:'2026-11-01 09:00'} | interval {every:'30m'} | cron {cron:'0 9 * * *'}（分 时 日 月 周，本机时区）。"+
			"每次触发（含失败/跳过）写运行记录，页面可看触发时间、目标、内容与结果；快应用触发另进通知栏。一次性任务执行后自动停用。"+
			"create 需要 title/kind/schedule 与对应目标；update 只改传入字段；list 看全部任务与下次时间；enable/disable 启停；run 立即执行一次（不改变计划）。"+
			"不要手改 callbacks.json",
		func(ctx context.Context, in *callbackArgs) (string, error) {
			if api == nil {
				return "", errors.New("定时回调服务不可用")
			}
			switch strings.ToLower(strings.TrimSpace(in.Action)) {
			case "list":
				return renderCallbackList(api.List(), currentSession()), nil
			case "create":
				t, err := api.Create(ctx, callback.CreateInput{
					Title: in.Title, Description: deref(in.Description), Kind: in.Kind,
					Enabled: in.Enabled, Schedule: derefSchedule(in.Schedule),
					Session: in.Session, App: in.App, Script: in.Script,
					BindSession: currentSession(),
				})
				if err != nil {
					return "", err
				}
				return "已创建定时回调 " + t.ID + "：" + t.Summary(), nil
			case "update":
				if in.ID == "" {
					return "", errors.New("update 需要 id")
				}
				up := callback.UpdateInput{
					Enabled: in.Enabled, Schedule: in.Schedule,
					Session: in.Session, App: in.App, Script: in.Script,
					BindSession: currentSession(),
				}
				if in.Title != "" {
					up.Title = &in.Title
				}
				if in.Description != nil {
					up.Description = in.Description
				}
				if in.Kind != "" {
					up.Kind = &in.Kind
				}
				if up.Title == nil && up.Description == nil && up.Kind == nil && up.Enabled == nil &&
					up.Schedule == nil && up.Session == nil && up.App == nil && up.Script == nil {
					return "", errors.New("update 至少给一个要改的字段")
				}
				t, err := api.Update(ctx, in.ID, up)
				if err != nil {
					return "", err
				}
				return "已更新定时回调 " + t.ID + "：" + t.Summary(), nil
			case "delete":
				if in.ID == "" {
					return "", errors.New("delete 需要 id")
				}
				if err := api.Delete(ctx, in.ID); err != nil {
					return "", err
				}
				return "已删除定时回调 " + in.ID, nil
			case "enable", "disable":
				if in.ID == "" {
					return "", errors.New(in.Action + " 需要 id")
				}
				on := strings.EqualFold(in.Action, "enable")
				t, err := api.SetEnabled(ctx, in.ID, on)
				if err != nil {
					return "", err
				}
				return "已" + map[bool]string{true: "启用", false: "停用"}[on] + "定时回调 " + t.ID + "，" + t.Summary(), nil
			case "run":
				if in.ID == "" {
					return "", errors.New("run 需要 id")
				}
				if err := api.RunNow(in.ID); err != nil {
					return "", err
				}
				return "已触发定时回调 " + in.ID + "（后台执行，不改变计划）", nil
			default:
				return "", fmt.Errorf("未知 action %q（list/create/update/delete/enable/disable/run）", in.Action)
			}
		})
}

/* renderCallbackList 渲染任务清单（标注绑定当前会话的会话任务）。 */
func renderCallbackList(tasks []callback.Task, currentSession string) string {
	if len(tasks) == 0 {
		return "暂无定时回调。用 action=create 创建。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "定时回调共 %d 个：", len(tasks))
	for _, t := range tasks {
		title := strings.ReplaceAll(strings.ReplaceAll(t.Title, "\r", ""), "\n", " ")
		b.WriteString("\n- [" + t.ID + "] " + title + "（" + t.Summary())
		if t.Kind == callback.KindSession && t.Session != nil &&
			t.Session.Mode == callback.ModeSession && t.Session.SessionID == currentSession {
			b.WriteString(" · 本会话")
		}
		if t.LastResult != "" {
			b.WriteString(" · " + t.LastResult)
		}
		b.WriteString("）")
	}
	return b.String()
}

/* deref 取指针字符串（nil 为空）。 */
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

/* derefSchedule 取指针计划（nil 为空计划，create 时校验会拒绝）。 */
func derefSchedule(p *callback.Schedule) callback.Schedule {
	if p == nil {
		return callback.Schedule{}
	}
	return *p
}
