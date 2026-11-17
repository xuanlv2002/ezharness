/*
Package tools 提供 ezharness 自有的增量工具：save_app 与 sleep；
文件读写、搜索与终端工具复用 ezloop 的 filetools hook。
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
	return "保存快应用到 apps/<名>/（app.quick 声明 + 前端入口）。只传要改的字段：未传的字段保持原值，传空串/空数组则清除。" +
		"新建应用必须给 html；更新时不给 html 就不动入口文件。" +
		"description（一句话）与 version（语义化版本）写进应用清单；document 指向应用文档（默认 APP.md），" +
		"文档含「Agent 调用」一节才开放给 agent（用 load_app 读取）。" +
		"需要后端进程时给 backend（在应用目录里执行的命令，如 python probe.py）；" +
		"卡片头像给 icon（应用目录内的图片文件名，如 icon.svg，先用 write_file 写进同目录）；" +
		"需要被定时回调调用时给 callbacks（端点声明，回调任务按 name 引用）；其余文件用 write_file 补进同一目录"
}

func (t saveAppTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "应用名（英文短名，如 certprobe）"},
			"title": {"type": "string", "description": "卡片标题（可读名；不传保持原值，空串回落入口 <title>/目录名）"},
			"description": {"type": "string", "description": "一句话描述（应用清单展示与 agent 发现用）"},
			"version": {"type": "string", "description": "语义化版本（如 1.2.0）"},
			"document": {"type": "string", "description": "应用文档路径（默认 APP.md；含「Agent 调用」一节才开放调用）"},
			"html": {"type": "string", "description": "完整 html 内容（新建必填；更新时不传则不改入口文件）"},
			"backend": {"type": "string", "description": "后端启动命令（cwd 是应用目录，如 python probe.py）"},
			"icon": {"type": "string", "description": "卡片头像：应用目录内的图片文件名（如 icon.svg，先用 write_file 写好）"},
			"callbacks": {
				"type": "array",
				"description": "定时调用端点声明（不传保持原值，传 [] 清空）",
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
		"required": ["name"]
	}`)
}

func (t saveAppTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Name        string               `json:"name"`
		Title       *string              `json:"title"`
		Description *string              `json:"description"`
		Version     *string              `json:"version"`
		Document    *string              `json:"document"`
		HTML        *string              `json:"html"`
		Backend     *string              `json:"backend"`
		Icon        *string              `json:"icon"`
		Callbacks   *[]quickapp.Callback `json:"callbacks"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(strings.TrimSpace(a.Name), ".html")
	if !quickapp.ValidName(name) {
		return "", errors.New("invalid app name")
	}
	info, err := quickapp.Load(ctx, t.fsys, name)
	isNew := errors.Is(err, quickapp.ErrNotFound)
	if err != nil && !isNew {
		return "", err
	}
	if isNew && (a.HTML == nil || strings.TrimSpace(*a.HTML) == "") {
		return "", errors.New("新建应用需要 html（前端入口）")
	}
	spec := info.Spec
	if a.Title != nil {
		spec.Title = strings.TrimSpace(*a.Title)
	}
	if a.Description != nil {
		spec.Description = strings.TrimSpace(*a.Description)
	}
	if a.Version != nil {
		spec.Version = strings.TrimSpace(*a.Version)
	}
	if a.Document != nil {
		spec.Document = strings.TrimSpace(*a.Document)
	}
	if a.Backend != nil {
		spec.Backend = strings.TrimSpace(*a.Backend)
	}
	if a.Icon != nil {
		spec.Icon = strings.TrimSpace(*a.Icon)
	}
	if a.Callbacks != nil {
		spec.Callbacks = *a.Callbacks
	}
	if err := quickapp.ValidateCallbacks(spec.Callbacks); err != nil {
		return "", fmt.Errorf("callbacks %w", err)
	}
	if spec.Icon != "" {
		if _, err := quickapp.SafeEntry(spec.Icon); err != nil {
			return "", fmt.Errorf("icon %w", err)
		}
	}
	if spec.Document != "" {
		if _, err := quickapp.SafeEntry(spec.Document); err != nil {
			return "", fmt.Errorf("document %w", err)
		}
	}
	meta, err := spec.Marshal()
	if err != nil {
		return "", err
	}
	dir := appsDir + "/" + name
	if err := t.fsys.Write(ctx, dir+"/"+quickapp.FileName, meta); err != nil {
		return "", err
	}
	if a.HTML != nil {
		if err := t.fsys.Write(ctx, dir+"/"+spec.EntryOr(), []byte(*a.HTML)); err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(filepath.FromSlash(dir))
	if err != nil {
		abs = dir
	}
	abs = filepath.ToSlash(abs)
	changes := make([]string, 0, 8)
	if a.Title != nil {
		changes = append(changes, "title")
	}
	if a.Description != nil {
		changes = append(changes, "description")
	}
	if a.Version != nil {
		changes = append(changes, "version")
	}
	if a.Document != nil {
		changes = append(changes, "document")
	}
	if a.Backend != nil {
		changes = append(changes, "backend")
	}
	if a.Icon != nil {
		changes = append(changes, "icon")
	}
	if a.Callbacks != nil {
		changes = append(changes, "callbacks")
	}
	if a.HTML != nil {
		changes = append(changes, "前端")
	}
	action := "已更新"
	if isNew {
		action = "已创建"
	}
	summary := fmt.Sprintf("快应用%s：%s（%s；入口 %s/%s）", action, name, strings.Join(changes, "、"), abs, spec.EntryOr())
	if len(changes) == 0 {
		summary = fmt.Sprintf("快应用%s：%s（未传可改字段；入口 %s/%s）", action, name, abs, spec.EntryOr())
	}
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
	return summary + backend + callbacks, nil
}
