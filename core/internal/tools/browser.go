/*
共享浏览器工具组(browser_tab/browser_action/browser_read):AI 与用户
操控魔法看板里的同一批真实 Chromium 标签,全局共享(个人助手语义)。
页面镜像到工作区抽屉·浏览器页供用户实时共见;用户接管在抽屉页原生
完成(标签条/地址栏直接操作)。检索、查资料、操作网页用本组工具(与
terminal 互补:那是本机命令,这是真实浏览器);截图经 image_loaded
标记转持久化图片消息(多模态模型直接看图)。
按 AI 心智分三面:tab 管标签生命周期、action 操作页面、read 读页面
内容。依赖倒置:tools 只依赖 BrowserIO 接口,由 service.BrowserService
实现(service 已 import tools,反向引用会循环)。
*/
package tools

import (
	"context"
	"fmt"

	"github.com/xuanlv2002/ezloop/types"
)

/* BrowserIO 是共享浏览器服务的能力面(service.BrowserService 实现)。 */
type BrowserIO interface {
	/* StartBrowser 新建标签(描述必填)并可选导航 */
	StartBrowser(desc, url string, timeoutMs int) (string, error)
	/* NavigateBrowser 跳转并等加载 */
	NavigateBrowser(tabID, url string, timeoutMs int) (string, error)
	/* ClickBrowser 点击(selector 优先,否则视口截图坐标) */
	ClickBrowser(tabID, selector string, x, y int) (string, error)
	/* TypeBrowser 输入文本(selector 定位输入框,submit 回车提交) */
	TypeBrowser(tabID, selector, text string, submit bool) (string, error)
	/* PressBrowserKey 按键/组合键("Enter"、"Control+A") */
	PressBrowserKey(tabID, combo string) (string, error)
	/* ScrollBrowser 滚动(up|down) */
	ScrollBrowser(tabID, direction string, amountPx int) (string, error)
	/* ReadBrowser 读正文/链接清单 */
	ReadBrowser(tabID, mode string, chars int) (string, error)
	/* ScreenshotBrowser 截图(落盘+image_loaded 标记) */
	ScreenshotBrowser(tabID string, fullPage bool) (string, error)
	/* CloseBrowserTab 关闭标签 */
	CloseBrowserTab(tabID string) error
	/* ListBrowserTabsJSON 标签清单(JSON 文本) */
	ListBrowserTabsJSON() string
	/* ModelSeesImages 主模型是否多模态(截图返回形态裁决) */
	ModelSeesImages() bool
}

type browserTabArgs struct {
	Action    string `json:"action" desc:"open=新建标签(可带首跳网址);list=列出全部标签;close=关闭标签"`
	Desc      string `json:"desc,omitempty" desc:"open:标签描述/名称(如 查竞品、看文档),list 与看板中展示"`
	URL       string `json:"url,omitempty" desc:"open:创建后立即打开的网址(可选)"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"open:导航等待上限毫秒,默认 20000;首次使用会自动下载 Chromium,此时给 600000"`
	TabID     string `json:"tabId,omitempty" desc:"close:要关闭的标签 id"`
}

type browserActionArgs struct {
	Action    string `json:"action" desc:"navigate=打开新网址;click=点击元素;type=输入文本;key=按键;scroll=滚动"`
	TabID     string `json:"tabId,omitempty" desc:"目标标签 id(browser_tab 的 list 查看),省略=最近使用的标签"`
	URL       string `json:"url,omitempty" desc:"navigate:要打开的网址"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"navigate:等待上限毫秒,默认 20000"`
	Selector  string `json:"selector,omitempty" desc:"click/type:CSS 选择器(如 a.login、#search button;type 省略=在当前焦点处输入)"`
	X         int    `json:"x,omitempty" desc:"click:横坐标(无 selector 时用;视口截图的像素坐标,先 browser_read screenshot 看图再定)"`
	Y         int    `json:"y,omitempty" desc:"click:纵坐标(同上)"`
	Text      string `json:"text,omitempty" desc:"type:要输入的文本(支持中文)"`
	Submit    bool   `json:"submit,omitempty" desc:"type:输入后回车提交(搜索框/登录框常用)"`
	Combo     string `json:"combo,omitempty" desc:"key:按键,如 Enter、Escape、Tab、ArrowDown、Control+A、Shift+ArrowDown"`
	Direction string `json:"direction,omitempty" desc:"scroll:方向 up 或 down"`
	AmountPx  int    `json:"amountPx,omitempty" desc:"scroll:像素量,默认 600"`
}

type browserReadArgs struct {
	TabID string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	Mode  string `json:"mode,omitempty" desc:"text=页面正文(默认);links=链接清单(文字→地址);screenshot=视口截图;full_page=整页截图"`
	Chars int    `json:"chars,omitempty" desc:"text/links:返回字符数上限,默认 4000,上限 20000"`
}

/* SharedBrowser 构造浏览器三工具(b 为 nil 返回 nil,测试装配可不注入)。 */
func SharedBrowser(b BrowserIO) []types.Tool {
	if b == nil {
		return nil
	}
	return []types.Tool{
		types.NewTool("browser_tab",
			"浏览器标签管理(真实 Chromium,画面镜像到工作区抽屉·浏览器页,用户实时共见可接管)。"+
				"action=open 新建标签并可选导航——desc 是标签描述,url 可选,首次使用会自动下载 Chromium(约 150MB,timeoutMs 给 600000);"+
				"action=list 列出全部标签(用户手动开的与 AI 新开的都在内,所有会话共享);action=close 关闭标签(用完及时关,释放资源)。",
			func(ctx context.Context, in *browserTabArgs) (string, error) {
				switch in.Action {
				case "open":
					return b.StartBrowser(in.Desc, in.URL, in.TimeoutMs)
				case "list":
					return b.ListBrowserTabsJSON(), nil
				case "close":
					if err := b.CloseBrowserTab(in.TabID); err != nil {
						return "", err
					}
					return "closed " + in.TabID, nil
				}
				return "", fmt.Errorf("未知 action %q(open/list/close)", in.Action)
			}),
		types.NewTool("browser_action",
			"操作浏览器页面。action=navigate 打开新网址并等加载,返回页面标题;"+
				"action=click 点击——优先给 CSS 选择器(等元素出现后点击,稳定),无法定位时先 browser_read screenshot 看图再给视口像素坐标 x/y;"+
				"action=type 输入文本(selector 定位输入框,省略=当前焦点处,submit=true 回车提交);"+
				"action=key 按键或组合键;action=scroll 滚动(direction=up|down,amountPx 默认 600)。",
			func(ctx context.Context, in *browserActionArgs) (string, error) {
				switch in.Action {
				case "navigate":
					return b.NavigateBrowser(in.TabID, in.URL, in.TimeoutMs)
				case "click":
					return b.ClickBrowser(in.TabID, in.Selector, in.X, in.Y)
				case "type":
					return b.TypeBrowser(in.TabID, in.Selector, in.Text, in.Submit)
				case "key":
					return b.PressBrowserKey(in.TabID, in.Combo)
				case "scroll":
					return b.ScrollBrowser(in.TabID, in.Direction, in.AmountPx)
				}
				return "", fmt.Errorf("未知 action %q(navigate/click/type/key/scroll)", in.Action)
			}),
		types.NewTool("browser_read",
			"读取浏览器页面内容。mode=text 返回页面正文(默认);mode=links 返回链接清单(文字→地址,适合找下一步入口);"+
				"mode=screenshot 截当前视口(坐标点击以此为准);mode=full_page 截整页(只用于观察)。"+
				"截图经 image_loaded 转图片消息,多模态模型直接看图定位。",
			func(ctx context.Context, in *browserReadArgs) (string, error) {
				switch in.Mode {
				case "", "text", "links":
					mode := in.Mode
					if mode == "" {
						mode = "text"
					}
					return b.ReadBrowser(in.TabID, mode, in.Chars)
				case "screenshot":
					return b.ScreenshotBrowser(in.TabID, false)
				case "full_page":
					return b.ScreenshotBrowser(in.TabID, true)
				}
				return "", fmt.Errorf("未知 mode %q(text/links/screenshot/full_page)", in.Mode)
			}),
	}
}
