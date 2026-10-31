package tools

import (
	"context"
	"encoding/json"
	"encoding/base64"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
)

/*
modelCallTool 把"调用另一个大模型"做成 agent 工具：主模型把独立子任务
（识别/翻译/总结/风格改写……）一次性交给指定模型，只有文本结果回流。
与 task 分身的分工：task 跑"多步工具循环"（复刻上下文），model_call 跑
"单次问答"（无工具、无上下文）——视觉识别尤其走这里：images 路径按
目标模型的视觉能力决定是否附带（无视觉直接报错，模型据此换选）。
*/
type modelCallTool struct {
	fsys    osfs.OS
	entries func() []domain.ModelEntry
	active  func() *domain.ModelEntry
	invoke  OneShot
}

/* OneShot 用指定模型条目执行一次无工具请求（service 提供：buildProvider + 超时护栏；REST /api/open/model/call 复用）。 */
type OneShot func(ctx context.Context, entry domain.ModelEntry, req types.ModelRequest) (string, error)

/* ModelCall 返回 model_call 工具；entries/active 实时读模型快照（设置页改动即生效）。 */
func ModelCall(fsys osfs.OS, entries func() []domain.ModelEntry, active func() *domain.ModelEntry, invoke OneShot) types.Tool {
	return modelCallTool{fsys: fsys, entries: entries, active: active, invoke: invoke}
}

func (modelCallTool) Name() string { return "model_call" }

func (modelCallTool) Description() string {
	return "调用另一个大模型完成一次独立问答并返回文本结果（无工具、不带本对话上下文）。" +
		"适合：图片/截图识别（images 传路径，须选带视觉的模型）、翻译、摘要、风格改写等一次性子任务。" +
		"model 填模型名——取值以 system <models> 段的可用模型清单为准（省略 = 当前主模型）。提示词必须自包含。"
}

/* callTimeout 单次模型调用的护栏（视觉识别与大文本生成都在分钟内）。 */
const callTimeout = 5 * time.Minute

func (modelCallTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":` +
		`{"model":{"type":"string","description":"模型名（省略=当前主模型）"},` +
		`"prompt":{"type":"string","description":"给目标模型的完整提示词（自包含）"},` +
		`"images":{"type":"array","items":{"type":"string"},"description":"图片绝对路径列表（可选；目标模型须支持视觉）"}}` +
		`,"required":["prompt"]}`)
}

func (t modelCallTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Model  string   `json:"model"`
		Prompt string   `json:"prompt"`
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return "", fmt.Errorf("model_call: prompt required")
	}
	entry := t.active()
	if args.Model != "" {
		if e := findEntry(t.entries(), args.Model); e != nil {
			entry = e
		} else if findAnyEntry(t.entries(), args.Model) != nil {
			return "", fmt.Errorf("model_call: 模型 %s 已关闭,不可调用（可在 设置·模型 重新开启）", args.Model)
		} else {
			return "", fmt.Errorf("model_call: unknown model %q（以 system <models> 清单为准）", args.Model)
		}
	}
	if entry == nil || entry.Name == "" {
		return "", fmt.Errorf("model_call: no model configured")
	}
	msg := types.Message{Role: types.RoleUser, Content: args.Prompt}
	if len(args.Images) > 0 {
		if !entry.Vision {
			return "", fmt.Errorf("model_call: 模型 %s 不支持视觉输入（images 被拒绝）；请换用带视觉能力的模型名", entry.Name)
		}
		parts, err := LoadImages(ctx, t.fsys, args.Images)
		if err != nil {
			return "", err
		}
		msg.Images = parts
	}
	return t.invoke(ctx, *entry, types.ModelRequest{Messages: []types.Message{msg}})
}

/* LoadImages 读盘转内嵌图片（超限与坏文件直接报错，模型可自纠路径）；
model_call 工具与 HTTP /api/open/model/call 共用。 */
func LoadImages(ctx context.Context, fsys osfs.OS, paths []string) ([]types.ImagePart, error) {
	if len(paths) > 4 {
		return nil, fmt.Errorf("model_call: images 最多 4 张")
	}
	out := make([]types.ImagePart, 0, len(paths))
	for _, p := range paths {
		data, err := fsys.Read(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("model_call: read image %s: %w", p, err)
		}
		mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(p)))
		if mt == "" {
			mt = "image/png"
		}
		out = append(out, types.ImagePart{MimeType: mt, Data: base64.StdEncoding.EncodeToString(data)})
	}
	return out, nil
}

/* findEntry 在三槽条目里按名找可用条目（未启用的可被直调,已关闭的不可）。
findAnyEntry 不理会关闭态,用于把"已关闭"与"不存在"区分开报错。 */
func findEntry(entries []domain.ModelEntry, name string) *domain.ModelEntry {
	for i := range entries {
		if entries[i].Name == name && !entries[i].Disabled {
			return &entries[i]
		}
	}
	return nil
}

func findAnyEntry(entries []domain.ModelEntry, name string) *domain.ModelEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}
