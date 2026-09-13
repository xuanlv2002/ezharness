/*
mcp.json 装配与配置用例：ezharness 用文件描述 MCP server（http/stdio），
经 Reload 钩子热加载——文件改动下一轮迭代即生效。Enabled=false 的
服务器不装配。

页面侧连接是独立于 agent Router 的调试通道：McpService 维护手动建立的
MCP 会话（connect/disconnect/call），connected 状态以真会话为准。
*/
package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
)

/* McpFile 是 mcp.json 的形状。 */
type McpFile struct {
	Servers []McpServerFile `json:"servers"`
}

/* McpServerFile 描述单个 server 接入。Enabled 缺省（nil）视为 true。 */
type McpServerFile struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"` // 用途说明，mcp_list 时带给模型
	Type        string            `json:"type"`                  // "http" | "stdio"
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Args        []string          `json:"args"`
	Allow       []string          `json:"allow"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

/* IsEnabled 报告服务器是否启用。 */
func (s McpServerFile) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

/*
	McpServerView 是 MCP 页卡片数据。Tools 未连接为 0（前端显示 —）。

Allow 必须回传：前端全量保存，丢字段会清掉 mcp.json 里的白名单。
*/
type McpServerView struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"` // http | stdio
	Endpoint    string            `json:"endpoint"`  // http url 或启动命令
	Enabled     bool              `json:"enabled"`
	Connected   bool              `json:"connected"` // 页面手动会话已建立
	Tools       int               `json:"tools"`
	Headers     map[string]string `json:"headers"`
	Allow       []string          `json:"allow,omitempty"`
}

/* McpToolView 是连接后返回的工具清单条目。 */
type McpToolView struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ArgsSchema  json.RawMessage `json:"args_schema,omitempty"`
}

/*
	McpService MCP 配置用例。clients 是页面手动连接的会话缓存，

与 agent Router 懒建立的连接相互独立。
*/
type McpService struct {
	Fsys fs.FileSystem

	mu       sync.Mutex
	clients  map[string]mcp.Client
	toolDefs map[string][]mcp.ToolDef
}

/* NewMcpService 构造（main 装配用）。 */
func NewMcpService(fsys fs.FileSystem) *McpService {
	return &McpService{
		Fsys:     fsys,
		clients:  map[string]mcp.Client{},
		toolDefs: map[string][]mcp.ToolDef{},
	}
}

/* List 返回 MCP 页数据（连接状态以页面会话缓存为准）。 */
func (s *McpService) List() []McpServerView {
	f := loadMcpFileOrNil(s.Fsys)
	out := make([]McpServerView, 0)
	if f == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range f.Servers {
		_, connected := s.clients[srv.Name]
		out = append(out, McpServerView{
			Name:        srv.Name,
			Description: srv.Description,
			Transport:   srv.Type,
			Endpoint:    endpointOf(srv),
			Enabled:     srv.IsEnabled(),
			Connected:   connected,
			Tools:       len(s.toolDefs[srv.Name]),
			Headers:     nonNilHeaders(srv.Headers),
			Allow:       srv.Allow,
		})
	}
	return out
}

/*
	Connect 建立页面侧 MCP 会话并返回工具清单；已有会话则复用刷新，

失效时重建。禁用的 server 也允许连（页面是调试通道）。
*/
func (s *McpService) Connect(ctx context.Context, name string) ([]McpToolView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	srv := findServer(s.Fsys, name)
	if srv == nil {
		return nil, errMcp("unknown server: " + name)
	}
	if c, live := s.clients[name]; live {
		if defs, err := c.ListTools(ctx); err == nil {
			s.toolDefs[name] = defs
			return toolViews(defs), nil
		}
		s.closeClient(name, c) // 会话失效，走重建
	}
	sc, ok := buildServerConfig(*srv)
	if !ok {
		return nil, errMcp("unsupported server type: " + srv.Type)
	}
	c, err := sc.Factory(sc)
	if err != nil {
		return nil, errMcp(name + ": connect: " + err.Error())
	}
	defs, err := c.ListTools(ctx)
	if err != nil {
		if cl, closer := c.(mcp.Closer); closer {
			_ = cl.Close()
		}
		return nil, errMcp(name + ": list_tools: " + err.Error())
	}
	s.clients[name] = c
	s.toolDefs[name] = defs
	return toolViews(defs), nil
}

/* Disconnect 关闭页面会话（stdio 子进程随之释放）；未连接时幂等。 */
func (s *McpService) Disconnect(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, live := s.clients[name]; live {
		s.closeClient(name, c)
	}
}

/* Call 经页面会话调用工具。不查 allow——白名单约束 agent，页面是用户操作。 */
func (s *McpService) Call(ctx context.Context, server, tool string, args json.RawMessage) (string, error) {
	s.mu.Lock()
	c, live := s.clients[server]
	s.mu.Unlock()
	if !live {
		return "", errMcp("not connected: " + server + " (connect first)")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return c.CallTool(ctx, tool, args)
}

func (s *McpService) closeClient(name string, c mcp.Client) {
	if cl, ok := c.(mcp.Closer); ok {
		_ = cl.Close()
	}
	delete(s.clients, name)
	delete(s.toolDefs, name)
}

/*
	Update 保存 mcp.json（Reload 钩子下一轮生效），并关闭已删除/禁用

server 的页面会话。
*/
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
	if err := s.Fsys.Write(context.Background(), "mcp.json", data); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, c := range s.clients {
		keep := false
		for _, srv := range f.Servers {
			if srv.Name == name && srv.IsEnabled() {
				keep = true
				break
			}
		}
		if !keep {
			s.closeClient(name, c)
		}
	}
	return nil
}

func toolViews(defs []mcp.ToolDef) []McpToolView {
	out := make([]McpToolView, 0, len(defs))
	for _, d := range defs {
		out = append(out, McpToolView{Name: d.Name, Description: d.Description, ArgsSchema: d.ArgsSchema})
	}
	return out
}

func findServer(fsys fs.FileSystem, name string) *McpServerFile {
	f := loadMcpFileOrNil(fsys)
	if f == nil {
		return nil
	}
	for i := range f.Servers {
		if f.Servers[i].Name == name {
			return &f.Servers[i]
		}
	}
	return nil
}

func endpointOf(srv McpServerFile) string {
	if srv.Type == "stdio" {
		return srv.Name + " " + joinArgs(srv.Args)
	}
	return srv.URL
}

type mcpErr string

func (e mcpErr) Error() string { return string(e) }

func errMcp(msg string) error { return mcpErr("mcp.json: " + msg) }

func joinArgs(args []string) string { return strings.Join(args, " ") }

/* nonNilHeaders 避免 nil map 序列化为 null 打崩前端。 */
func nonNilHeaders(h map[string]string) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	return h
}

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
		if sc, ok := buildServerConfig(s); ok {
			out = append(out, sc)
		}
	}
	return out
}

func buildServerConfig(s McpServerFile) (mcp.ServerConfig, bool) {
	sc := mcp.ServerConfig{Name: s.Name, Description: s.Description, Allow: s.Allow}
	switch s.Type {
	case "http":
		sc.Factory = mcp.StreamableHTTP(s.URL, s.Headers)
	case "stdio":
		sc.Factory = mcp.Stdio(s.Name, s.Args...)
	default:
		return sc, false
	}
	return sc, true
}
