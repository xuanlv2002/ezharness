/*
Package tools 提供 ezharness 自有的增量工具：save_app（快应用生成）与
sleep（阻塞等待）。read_file / write_file / edit_file / terminal 复用
ezloop 的 filetools hook（原生 shell 执行，Windows 为 cmd），在
agent_service 装配。
*/
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/osfs"
	"ezharness/core/internal/quickapp"
)

/* appsDir 是快应用目录（工作目录相对）。 */
const appsDir = "apps"

/* SaveApp 返回快应用工具集。 */
func SaveApp(fsys osfs.OS) []types.Tool {
	return []types.Tool{saveAppTool{fsys}}
}

/* Sleep 返回阻塞等待工具。 */
func Sleep() types.Tool { return sleepTool{} }

/*
sleepTool 阻塞等待：Windows 的 timeout 在工具进程里 stdin 非控制台会
立即报错退出（>nul 连报错都看不见），terminal/term_action 里根本睡不住
——定时等待只有这条路是真的。
*/
type sleepTool struct{}

const sleepMaxSeconds = 900

func (sleepTool) Name() string { return "sleep" }

func (sleepTool) Description() string {
	return "阻塞等待 seconds 秒后再返回（等后端就绪、轮询间隔、长任务出结果前的间隙）。" +
		"不要用 terminal 或 term_action send 跑 timeout/ping 之类命令去睡——" +
		"Windows 的 timeout 在工具进程里立即退出，睡不住还白跑一趟。上限 900 秒；用户停止会立即中断"
}

func (sleepTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"seconds": {"type": "integer", "description": "等待秒数（1-900）"}
		},
		"required": ["seconds"]
	}`)
}

func (sleepTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Seconds int `json:"seconds"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if a.Seconds <= 0 || a.Seconds > sleepMaxSeconds {
		return "", fmt.Errorf("seconds 需在 1-%d 之间", sleepMaxSeconds)
	}
	select {
	case <-time.After(time.Duration(a.Seconds) * time.Second):
		return fmt.Sprintf("已等待 %d 秒", a.Seconds), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type saveAppTool struct{ fsys osfs.OS }

func (saveAppTool) Name() string { return "save_app" }
func (saveAppTool) Description() string {
	return "把一个快应用保存到 apps/<名>/（app.quick 声明 + index.html 前端）。" +
		"description（一句话）与 version（语义化版本）写进应用清单；document 指向应用文档（默认 APP.md），" +
		"文档含「Agent 调用」一节才开放给 agent（用 load_app 读取）。" +
		"需要后端进程时给 backend（在应用目录里执行的命令，如 python probe.py），" +
		"卡片头像给 icon（应用目录内的图片文件名，如 icon.svg，先用 write_file 写进同目录）；" +
		"需要被定时回调调用时给 callbacks（端点声明，回调任务按 name 引用；不影响文档调用面）；" +
		"后端脚本等其余文件同样用 write_file 补进同一目录。" +
		"注意：save_app 整页重建 app.quick——省略 callbacks/document/backend/icon 会清掉对应声明"
}

func (t saveAppTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "应用名（英文短名，如 certprobe）"},
			"title": {"type": "string", "description": "卡片标题（可读名；缺省用 name）"},
			"description": {"type": "string", "description": "一句话描述（应用清单展示与 agent 发现用）"},
			"version": {"type": "string", "description": "语义化版本（如 1.2.0，可选）"},
			"document": {"type": "string", "description": "应用文档路径（默认 APP.md；写清 agent 调用方式，含「Agent 调用」一节才开放调用）"},
			"html": {"type": "string", "description": "完整 html 文档内容（前端入口 index.html）"},
			"backend": {"type": "string", "description": "后端启动命令（可选；cwd 是应用目录，如 python probe.py）"},
			"icon": {"type": "string", "description": "卡片头像：应用目录内的图片文件名（可选，如 icon.svg/png，先用 write_file 写好）"},
			"callbacks": {
				"type": "array",
				"description": "定时调用端点声明（可选；回调中心按 name 引用）",
				"items": {
					"type": "object",
					"properties": {
						"name": {"type": "string", "description": "稳定标识（app 内唯一，字母数字 -_.）"},
						"title": {"type": "string", "description": "人可读名称"},
						"description": {"type": "string", "description": "一句话说明"},
						"method": {"type": "string", "description": "HTTP 方法（缺省 POST）"},
						"url": {"type": "string", "description": "绝对 http(s) 地址"},
						"headers": {"type": "object", "additionalProperties": {"type": "string"}, "description": "附加请求头"},
						"body": {"description": "JSON 请求体（对象/数组/标量）"}
					},
					"required": ["name", "url"]
				}
			}
		},
		"required": ["name", "html"]
	}`)
}

func (t saveAppTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Name        string              `json:"name"`
		Title       string              `json:"title"`
		Description string              `json:"description"`
		Version     string              `json:"version"`
		Document    string              `json:"document"`
		HTML        string              `json:"html"`
		Backend     string              `json:"backend"`
		Icon        string              `json:"icon"`
		Callbacks   []quickapp.Callback `json:"callbacks"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if err := quickapp.ValidateCallbacks(a.Callbacks); err != nil {
		return "", fmt.Errorf("callbacks %w", err)
	}
	name := strings.TrimSuffix(strings.TrimSpace(a.Name), ".html")
	if !quickapp.ValidName(name) {
		return "", errors.New("invalid app name")
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = name
	}
	if icon := strings.TrimSpace(a.Icon); icon != "" {
		if _, err := quickapp.SafeEntry(icon); err != nil {
			return "", fmt.Errorf("icon %w", err)
		}
	}
	document := strings.TrimSpace(a.Document)
	if document != "" {
		if _, err := quickapp.SafeEntry(document); err != nil {
			return "", fmt.Errorf("document %w", err)
		}
	}
	spec := quickapp.Spec{
		Title:       title,
		Description: strings.TrimSpace(a.Description),
		Version:     strings.TrimSpace(a.Version),
		Entry:       quickapp.DefaultEntry,
		Backend:     strings.TrimSpace(a.Backend),
		Icon:        strings.TrimSpace(a.Icon),
		Document:    document,
		Callbacks:   a.Callbacks,
	}
	meta, err := spec.Marshal()
	if err != nil {
		return "", err
	}
	dir := appsDir + "/" + name
	if err := t.fsys.Write(ctx, dir+"/"+quickapp.FileName, meta); err != nil {
		return "", err
	}
	if err := t.fsys.Write(ctx, dir+"/"+quickapp.DefaultEntry, []byte(a.HTML)); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.FromSlash(dir))
	if err != nil {
		abs = dir
	}
	abs = filepath.ToSlash(abs)
	backend := ""
	if spec.HasBackend() {
		backend = fmt.Sprintf("；已声明后端，用户点启动会在内置终端执行 `%s`，后端脚本等文件写进 %s/", spec.Backend, abs)
	}
	callbacks := ""
	if len(spec.Callbacks) > 0 {
		names := make([]string, 0, len(spec.Callbacks))
		for _, c := range spec.Callbacks {
			names = append(names, c.Name)
		}
		callbacks = "；定时回调端点：" + strings.Join(names, "、")
	}
	return fmt.Sprintf("快应用已保存：%s（前端 %s/%s，用户可一键启动）%s%s",
		name, abs, quickapp.DefaultEntry, backend, callbacks), nil
}
