/*
mcp.json 装配与配置用例：ezharness 用文件描述 MCP server（http/stdio），
经 Reload 钩子热加载——文件改动下一轮迭代即生效。Enabled=false 的
服务器不装配。连接状态对 http 做 2s 探活，stdio 不探测（视为未知→可达）。
*/
package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
)

/* McpFile 是 mcp.json 的形状。 */
type McpFile struct {
	Servers []McpServerFile `json:"servers"`
}

/* McpServerFile 描述单个 server 接入。Enabled 缺省（nil）视为 true。 */
type McpServerFile struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "http" | "stdio"
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Args    []string          `json:"args"`
	Allow   []string          `json:"allow"`
	Enabled *bool             `json:"enabled,omitempty"`
}

/* IsEnabled 报告服务器是否启用。 */
func (s McpServerFile) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

/* McpServerView 是 MCP 页卡片数据。Tools 未知为 0（前端显示 —）。 */
type McpServerView struct {
	Name      string `json:"name"`
	Transport string `json:"transport"` // http | stdio
	Endpoint  string `json:"endpoint"`  // http url 或启动命令
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Tools     int    `json:"tools"`
}

/* McpService MCP 配置用例。 */
type McpService struct {
	Fsys fs.FileSystem
}

/* List 返回 MCP 页数据（含 http 探活）。 */
func (s *McpService) List() []McpServerView {
	f := loadMcpFileOrNil(s.Fsys)
	if f == nil {
		return nil
	}
	client := &http.Client{Timeout: 2 * time.Second}
	out := make([]McpServerView, 0, len(f.Servers))
	for _, srv := range f.Servers {
		v := McpServerView{
			Name:      srv.Name,
			Transport: srv.Type,
			Endpoint:  srv.URL,
			Enabled:   srv.IsEnabled(),
			Connected: true,
		}
		if srv.Type == "stdio" {
			v.Endpoint = srv.Name + " " + joinArgs(srv.Args)
		} else if srv.Type == "http" && srv.URL != "" && srv.IsEnabled() {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
			if err == nil {
				resp, err := client.Do(req)
				if err != nil {
					v.Connected = false
				} else {
					_ = resp.Body.Close()
				}
			}
		}
		out = append(out, v)
	}
	return out
}

/* Update 保存 mcp.json（Reload 钩子下一轮生效）。 */
func (s *McpService) Update(f McpFile) error {
	for _, srv := range f.Servers {
		if srv.Name == "" {
			return errMcp("server name required")
		}
		if srv.Type != "http" && srv.Type != "stdio" {
			return errMcp("type must be http or stdio")
		}
		if srv.Type == "http" && srv.URL == "" {
			return errMcp("http server requires url")
		}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return s.Fsys.Write(context.Background(), "mcp.json", data)
}

type mcpErr string

func (e mcpErr) Error() string { return string(e) }

func errMcp(msg string) error { return mcpErr("mcp.json: " + msg) }

func joinArgs(args []string) string { return strings.Join(args, " ") }

/* McpNames 返回已启用的 server 名（状态栏显示用）。 */
func McpNames(fsys fs.FileSystem) []string {
	f := loadMcpFileOrNil(fsys)
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(f.Servers))
	for _, s := range f.Servers {
		if s.IsEnabled() {
			out = append(out, s.Name)
		}
	}
	return out
}

/* NewMcpHook 从 mcp.json 构造 mcp hook（文件缺失=空集不报错；禁用项不装配）。 */
func NewMcpHook(fsys fs.FileSystem) *mcp.Hook {
	return mcp.NewHook(mcp.Config{
		Servers: buildServers(loadMcpFileOrNil(fsys)),
		Reload: func(ctx context.Context) ([]mcp.ServerConfig, error) {
			return buildServers(loadMcpFileOrNil(fsys)), nil
		},
	})
}

func loadMcpFileOrNil(fsys fs.FileSystem) *McpFile {
	data, err := fsys.Read(context.Background(), "mcp.json")
	if err != nil {
		return nil
	}
	var f McpFile
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	return &f
}

func buildServers(f *McpFile) []mcp.ServerConfig {
	if f == nil {
		return nil
	}
	out := make([]mcp.ServerConfig, 0, len(f.Servers))
	for _, s := range f.Servers {
		if !s.IsEnabled() {
			continue
		}
		sc := mcp.ServerConfig{Name: s.Name, Allow: s.Allow}
		switch s.Type {
		case "http":
			sc.Factory = mcp.StreamableHTTP(s.URL, s.Headers)
		case "stdio":
			sc.Factory = mcp.Stdio(s.Name, s.Args...)
		default:
			continue
		}
		out = append(out, sc)
	}
	return out
}
