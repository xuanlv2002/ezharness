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
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

/* FileName 是应用声明文件名（目录内有它才算快应用）。 */
const FileName = "app.quick"

/* DefaultEntry 是 entry 缺省值。 */
const DefaultEntry = "index.html"

/* DefaultDocument 是应用文档缺省路径（调用文档，供 agent 按需加载）。 */
const DefaultDocument = "APP.md"

/* AppsDir 是快应用根目录（工作目录相对）。 */
const AppsDir = "apps"

/* Spec 是 app.quick 声明。 */
type Spec struct {
	Title       string     `json:"title,omitempty"`       // 卡片标题；缺省回落 entry 的 <title>，再回落目录名
	Description string     `json:"description,omitempty"` // 一句话描述（应用清单展示）
	Version     string     `json:"version,omitempty"`     // 语义化版本（建议）
	Entry       string     `json:"entry,omitempty"`       // 前端入口（相对应用目录），缺省 index.html
	Backend     string     `json:"backend,omitempty"`     // 后端启动命令，在应用目录里执行；空 = 纯前端
	Icon        string     `json:"icon,omitempty"`        // 卡片头像
	Document    string     `json:"document,omitempty"`    // 应用文档（调用约定），缺省 APP.md
	Callbacks   []Callback `json:"callbacks,omitempty"`   // 定时调用端点（回调中心按名引用）
}

/*
Callback 是快应用声明的可定时调用端点：回调任务用 app.name + callback.name
定位，按 method/url/headers/body 发起 HTTP 请求。声明只描述接口，不携带
计划——计划归回调中心的任务（callbacks.json）。
*/
type Callback struct {
	Name        string            `json:"name"`                  // 稳定标识（app 内唯一）
	Title       string            `json:"title,omitempty"`       // 人可读名称
	Description string            `json:"description,omitempty"` // 一句话说明
	Method      string            `json:"method,omitempty"`      // 缺省 POST
	URL         string            `json:"url"`                   // 绝对 http(s) 地址
	Headers     map[string]string `json:"headers,omitempty"`     // 附加请求头
	Body        json.RawMessage   `json:"body,omitempty"`        // JSON 请求体
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

/* DocumentOr 返回应用文档路径（缺省 APP.md）。 */
func (s Spec) DocumentOr() string {
	if strings.TrimSpace(s.Document) == "" {
		return DefaultDocument
	}
	return s.Document
}

/* FindCallback 按名字找声明的回调端点。 */
func (s Spec) FindCallback(name string) (Callback, bool) {
	name = strings.TrimSpace(name)
	for _, c := range s.Callbacks {
		if c.Name == name {
			return c, true
		}
	}
	return Callback{}, false
}

/* Normalize 归一化回调声明（补齐默认方法、去空白）。 */
func (c *Callback) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Title = strings.TrimSpace(c.Title)
	c.Description = strings.TrimSpace(c.Description)
	c.URL = strings.TrimSpace(c.URL)
	c.Method = strings.ToUpper(strings.TrimSpace(c.Method))
	if c.Method == "" {
		c.Method = http.MethodPost
	}
	if len(c.Headers) > 0 {
		headers := make(map[string]string, len(c.Headers))
		for k, v := range c.Headers {
			if key := strings.TrimSpace(k); key != "" {
				headers[key] = strings.TrimSpace(v)
			}
		}
		c.Headers = headers
	}
}

/* ValidateCallback 校验单个回调声明。 */
func ValidateCallback(c Callback) error {
	if c.Name == "" {
		return fmt.Errorf("callback.name 不能为空")
	}
	if len(c.Name) > 64 {
		return fmt.Errorf("callback.name 不能超过 64 字符")
	}
	for _, r := range c.Name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("callback.name 只能包含字母、数字、-_.: %q", c.Name)
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("callback.url 需要绝对 http(s) 地址: %q", c.URL)
	}
	switch c.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return fmt.Errorf("callback.method 不支持 %q", c.Method)
	}
	if len(c.Body) > 0 && !json.Valid(c.Body) {
		return fmt.Errorf("callback.body 不是合法 JSON")
	}
	return nil
}

/* ValidateCallbacks 校验回调声明并确保名字唯一。 */
func ValidateCallbacks(list []Callback) error {
	seen := map[string]bool{}
	for i := range list {
		list[i].Normalize()
		if err := ValidateCallback(list[i]); err != nil {
			return err
		}
		if seen[list[i].Name] {
			return fmt.Errorf("callback.name 重复: %q", list[i].Name)
		}
		seen[list[i].Name] = true
	}
	return nil
}

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
