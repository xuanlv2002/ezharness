/*
共享终端工具组(term_*):AI 与用户共写魔法看板里的同一批终端,全局
共享——任何会话都能操作全部终端(个人助手语义)。与 filetools 的
terminal(独立进程一次性命令)并存:需用户可见、交互式、状态保留
(长驻程序、跨命令 cd/环境变量)的场景用本组工具。
依赖倒置:tools 只依赖 TermIO 接口,由 service.TerminalService 实现
(service 已 import tools,反向引用会循环)。
*/
package tools

import (
	"context"

	"github.com/xuanlv2002/ezloop/types"
)

/* TermIO 是共享终端服务的能力面(service.TerminalService 实现)。 */
type TermIO interface {
	/* StartTerm 新建终端(描述必填),可选立即运行命令并等静默返回输出 */
	StartTerm(desc, command string, quietMs, timeoutMs int) (string, error)
	/* Send 发送命令并等输出静默,返回本次新增输出(游标推进) */
	Send(ctx context.Context, id, cmd string, quietMs, timeoutMs int) (string, error)
	/* ReadTerm 游标式读取新输出(读即消费) */
	ReadTerm(id string, chars int) (string, error)
	/* CloseTerm 关闭终端 */
	CloseTerm(id string) error
	/* ListTermsJSON 终端清单(JSON 文本) */
	ListTermsJSON() string
}

type startArgs struct {
	Desc      string `json:"desc" desc:"终端描述/名称(如 build-server、日志监控),term_list 与看板中展示"`
	Command   string `json:"command,omitempty" desc:"创建后立即运行的命令(可选);带此参数时会等待输出静默并直接返回"`
	QuietMs   int    `json:"quietMs,omitempty" desc:"输出静默多少毫秒后认为命令完成,默认 800"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"总等待上限毫秒,默认 30000,超时返回已得输出"`
}

type sendArgs struct {
	TermID    string `json:"termId,omitempty" desc:"目标终端 id(term_list 查看),省略=最近使用的终端"`
	Command   string `json:"command" desc:"要发送的命令或原始键入(\\u0003=Ctrl+C;纯控制输入不会自动补回车)"`
	QuietMs   int    `json:"quietMs,omitempty" desc:"输出静默多少毫秒后认为命令完成,默认 800"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"总等待上限毫秒,默认 30000,超时返回已得输出并提示续读"`
}

type readArgs struct {
	TermID string `json:"termId,omitempty" desc:"目标终端 id,省略=最近使用的终端"`
	Chars  int    `json:"chars,omitempty" desc:"返回字符数上限,默认 4000,上限 20000"`
}

type closeArgs struct {
	TermID string `json:"termId" desc:"要关闭的终端 id"`
}

/* SharedTerm 构造 term_* 工具组(t 为 nil 返回 nil,测试装配可不注入)。 */
func SharedTerm(t TermIO) []types.Tool {
	if t == nil {
		return nil
	}
	return []types.Tool{
		types.NewTool("term_start",
			"新建一个终端并可选立即运行命令。终端全局共享,用户可在魔法看板实时看到并接管。"+
				"desc 是终端描述,用于 term_list 与看板展示。需要长驻程序、交互式程序、想让用户看到过程时用本工具;"+
				"一次性无状态命令优先用 terminal 工具(更快)。",
			func(ctx context.Context, in *startArgs) (string, error) {
				return t.StartTerm(in.Desc, in.Command, in.QuietMs, in.TimeoutMs)
			}),
		types.NewTool("term_send",
			"向终端发送命令并等待输出静默后返回本次新增输出。与用户看板是同一会话:输出对用户实时可见,cd/环境变量跨命令有效。"+
				"也可发原始控制输入(如 \\u0003=Ctrl+C 中断当前命令,此时不自动补回车)。termId 省略=最近使用的终端。",
			func(ctx context.Context, in *sendArgs) (string, error) {
				return t.Send(ctx, in.TermID, in.Command, in.QuietMs, in.TimeoutMs)
			}),
		types.NewTool("term_read",
			"游标式读取终端的新输出:每次只返回上次读取之后新增的内容(读即消费,不重复)。"+
				"term_send 超时、长驻程序持续输出(如 tail -f、构建日志)时用本工具续读。",
			func(ctx context.Context, in *readArgs) (string, error) {
				return t.ReadTerm(in.TermID, in.Chars)
			}),
		types.NewTool("term_list",
			"列出全部共享终端(id、名称、运行状态、最近命令)。用户手动建的与 AI 新建的都在内,所有会话共享。",
			func(ctx context.Context, _ *struct{}) (string, error) {
				return t.ListTermsJSON(), nil
			}),
		types.NewTool("term_close",
			"关闭一个终端(结束 shell 进程树)。长驻程序用完、终端不再需要时关闭,防止资源泄漏。",
			func(ctx context.Context, in *closeArgs) (string, error) {
				if err := t.CloseTerm(in.TermID); err != nil {
					return "", err
				}
				return "closed " + in.TermID, nil
			}),
	}
}
