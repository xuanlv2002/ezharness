/*
Package quickapp 定义快应用的专属协议：apps/<名>/app.quick 是应用的声明，
含此文件的目录才是快应用。字段是扁平 JSON，未识别的字段一律忽略——
协议向前兼容，加字段（如卡片头像）不会破旧版。

协议只归 ezharness 使用，不必迁就通用 Web 约定，故形态收敛为"目录即应用"：
app.quick 声明 + entry 指向的前端 + 可选 backend 命令。
*/
package quickapp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

/* FileName 是应用声明文件名（目录内有它才算快应用）。 */
const FileName = "app.quick"

/* DefaultEntry 是 entry 缺省值。 */
const DefaultEntry = "index.html"

/* Spec 是 app.quick 声明。 */
type Spec struct {
	Title   string `json:"title,omitempty"`   // 卡片标题；缺省回落 entry 的 <title>，再回落目录名
	Entry   string `json:"entry,omitempty"`   // 前端入口（相对应用目录），缺省 index.html
	Backend string `json:"backend,omitempty"` // 后端启动命令，在应用目录里执行；空 = 纯前端
	Icon    string `json:"icon,omitempty"`    // 预留：卡片头像
}

/*
Parse 解析声明。未知字段忽略（json 包的默认行为即协议的前向兼容面，
不要换成 DisallowUnknownFields）。
*/
func Parse(data []byte) (Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return Spec{}, err
	}
	return s, nil
}

/* Marshal 序列化声明（2 空格缩进，与数据目录其余 json 一致）。 */
func (s Spec) Marshal() ([]byte, error) { return json.MarshalIndent(s, "", "  ") }

/* EntryOr 返回前端入口（缺省 index.html）。 */
func (s Spec) EntryOr() string {
	if strings.TrimSpace(s.Entry) == "" {
		return DefaultEntry
	}
	return s.Entry
}

/* HasBackend 报告是否声明了后端命令。 */
func (s Spec) HasBackend() bool { return strings.TrimSpace(s.Backend) != "" }

/* ValidName 报告应用名是否可作单段目录名（拒绝分隔符、上溯与保留字符）。 */
func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\:*?"<>|`)
}

/*
SafeEntry 校验前端入口是应用目录内的相对路径：拒绝绝对路径、盘符与 ".."
上溯——声明文件在数据目录里，但仍不可越出该应用目录取文件。
*/
func SafeEntry(entry string) (string, error) {
	e := strings.TrimSpace(entry)
	if e == "" {
		return DefaultEntry, nil
	}
	p := filepath.ToSlash(e)
	if filepath.IsAbs(e) || strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':') {
		return "", fmt.Errorf("entry 必须是相对路径: %q", entry)
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return "", fmt.Errorf("entry 不得上溯: %q", entry)
	}
	return p, nil
}
