/*
Package tools 提供 ezharness 的精简工具集：read_file / write_file /
edit_file / bash / save_app。ezharness 的产品决策是只暴露这四个原子
能力（搜索等由 bash 承担）+ 快应用生成，文件操作经 osfs 全权限文件系统。

bash 的 Windows 根因修复：不硬编码 cmd /c，按 shell 探测顺序选
bash（git-bash）→ pwsh → powershell → cmd；非零退出码不作为工具
错误——输出连同 exit code 一起作为正常结果回传，模型看得见真实
报错才能自纠（仅超时/无法启动才是 error）。
*/
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/osfs"
)

/* readLimit 是单次读文件返回上限，防超大文件撑爆上下文。 */
const readLimit = 64 << 10

/* bashTimeout 是单条命令的执行预算。 */
const bashTimeoutError = "command timed out (60s)"

/* All 返回完整工具集。shell 为空或 "auto" 时按环境探测。 */
func All(fsys osfs.OS, shell string) []types.Tool {
	return []types.Tool{
		readTool{fsys},
		writeTool{fsys},
		editTool{fsys},
		bashTool{shell},
		saveAppTool{fsys},
	}
}

/* ── save_app（快应用） ── */

const appsDir = "apps"

type saveAppTool struct{ fsys osfs.OS }

func (saveAppTool) Name() string        { return "save_app" }
func (saveAppTool) Description() string { return "把一个自包含的 html 小工具保存为快应用（用户可在快应用页一键启动）。name 用英文短名，html 是完整文档" }

func (t saveAppTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "应用名（英文短名，如 pomodoro）"},
			"html": {"type": "string", "description": "完整 html 文档内容（自包含，内联 css/js）"}
		},
		"required": ["name", "html"]
	}`)
}

func (t saveAppTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Name string `json:"name"`
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(strings.TrimSpace(a.Name), ".html")
	if name == "" || strings.ContainsAny(name, `/\:*?"<>|`) {
		return "", errors.New("invalid app name")
	}
	file := name + ".html"
	if err := t.fsys.Write(ctx, appsDir+"/"+file, []byte(a.HTML)); err != nil {
		return "", err
	}
	return fmt.Sprintf("快应用已保存：%s（用户可在快应用页启动，URL /apps/%s）", file, file), nil
}

/* ── read_file ── */

type readTool struct{ fsys osfs.OS }

func (readTool) Name() string        { return "read_file" }
func (readTool) Description() string { return "读取文件内容（全盘任意路径，64KB 截断）" }
func (readTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"文件路径"}},"required":["path"]}`)
}
func (t readTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Path) == "" {
		return "", errors.New("path is required")
	}
	data, err := t.fsys.Read(ctx, in.Path)
	if err != nil {
		return "", err
	}
	if len(data) > readLimit {
		return string(data[:readLimit]) + "\n…[truncated]", nil
	}
	return string(data), nil
}

/* ── write_file ── */

type writeTool struct{ fsys osfs.OS }

func (writeTool) Name() string        { return "write_file" }
func (writeTool) Description() string { return "写入文件（覆盖，自动建目录）" }
func (writeTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"文件路径"},"content":{"type":"string","description":"完整内容"}},"required":["path","content"]}`)
}
func (t writeTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Path) == "" {
		return "", errors.New("path is required")
	}
	if err := t.fsys.Write(ctx, in.Path, []byte(in.Content)); err != nil {
		return "", err
	}
	return "written " + in.Path, nil
}

/* ── edit_file ── */

type editTool struct{ fsys osfs.OS }

func (editTool) Name() string        { return "edit_file" }
func (editTool) Description() string { return "精确编辑：在文件内查找 old_text 并全部替换为 new_text" }
func (editTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"文件路径"},"old_text":{"type":"string","description":"要查找的原文（须精确匹配）"},"new_text":{"type":"string","description":"替换内容"}},"required":["path","old_text","new_text"]}`)
}
func (t editTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(args, &in); err != nil || in.Path == "" || in.OldText == "" {
		return "", errors.New("path and old_text are required")
	}
	n, err := t.fsys.Edit(ctx, in.Path, in.OldText, in.NewText)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced %d occurrence(s) in %s", n, in.Path), nil
}

/* ── bash ── */

type bashTool struct{ shell string }

func (bashTool) Name() string { return "bash" }
func (bashTool) Description() string {
	return "执行 shell 命令，返回合并输出（stdout+stderr）。支持管道、重定向、&& 组合。" +
		"非零退出码时输出与退出码一并返回，据此修正命令。"
}
func (bashTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"完整 shell 命令，如 ls -la | head -20"}},"required":["command"]}`)
}

func (t bashTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Command) == "" {
		return "", errors.New("command is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 60e9)
	defer cancel()

	name, flag := resolveShell(t.shell)
	cmd := exec.CommandContext(ctx, name, append([]string{flag}, in.Command)...)
	out, err := cmd.CombinedOutput()
	text := sanitizeOutput(out)

	// 非零退出：输出 + 退出码作为正常结果回传（模型自纠的依据）。
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return text + fmt.Sprintf("\n[exit code %d]", exitErr.ExitCode()), nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return text, errors.New(bashTimeoutError)
		}
		return text, fmt.Errorf("run: %w", err)
	}
	return text, nil
}

/* resolveShell 解析 shell：显式指定 > 平台探测（bash→pwsh→powershell→cmd/sh）。 */
func resolveShell(pref string) (name, flag string) {
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case "bash":
		return "bash", "-c"
	case "pwsh", "powershell":
		return strings.ToLower(strings.TrimSpace(pref)), "-Command"
	case "cmd":
		return "cmd", "/c"
	}
	if runtime.GOOS != "windows" {
		return "sh", "-c"
	}
	for _, cand := range [][2]string{
		{"bash", "-c"},
		{"pwsh", "-Command"},
		{"powershell", "-Command"},
	} {
		if _, err := exec.LookPath(cand[0]); err == nil {
			return cand[0], cand[1]
		}
	}
	return "cmd", "/c"
}

/* sanitizeOutput 去除无效 UTF-8 尾字节（Windows 控制台输出常见）。 */
func sanitizeOutput(out []byte) string {
	if utf8.Valid(out) {
		return strings.TrimSpace(string(out))
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(out), ""))
}

/* FS 类型断言用（保证 osfs 实现了 fs.FileSystem 全接口）。 */
var _ fs.FileSystem = osfs.OS{}
