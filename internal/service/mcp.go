/*
mcp.json 装配：ezharness 用文件描述 MCP server（http/stdio），
经 Reload 钩子热加载——文件改动下一轮迭代即生效。
*/
package service

import (
	"context"
	"encoding/json"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
)

/* McpFile 是 mcp.json 的形状。 */
type McpFile struct {
	Servers []McpServerFile `json:"servers"`
}

/* McpServerFile 描述单个 server 接入。 */
type McpServerFile struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "http" | "stdio"
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Args    []string          `json:"args"`
	Allow   []string          `json:"allow"`
}

/* McpNames 返回已配置的 server 名（状态栏显示用）。 */
func McpNames(fsys fs.FileSystem) []string {
	f := loadMcpFileOrNil(fsys)
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(f.Servers))
	for _, s := range f.Servers {
		out = append(out, s.Name)
	}
	return out
}

/* NewMcpHook 从 mcp.json 构造 mcp hook（文件缺失=空集不报错）。 */
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
