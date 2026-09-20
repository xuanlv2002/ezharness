/*
mcp.json 装配与配置用例：ezharness 用文件描述 MCP server（http/sse/stdio），
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
	Type        string            `json:"type"`                  // "http" | "sse" | "stdio"
	URL         string            `json:"url"`                   // http 与 sse 的服务地址
	Headers     map[string]string `json:"headers"`               // http 与 sse 的附加请求头
	Command     string            `json:"command"`               // stdio 要执行的命令（如 python3、npx）
	Args        []string          `json:"args"`                  // stdio 传给命令的参数
	Env         map[string]string `json:"env"`                   // stdio 附加环境变量（继承父进程环境之上合并）
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
	Transport   string            `json:"transport"` // http | sse | stdio
	Endpoint    string            `json:"endpoint"`  // http/sse url 或启动命令行
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Headers     map[string]string `json:"headers"`
	Enabled     bool              `json:"enabled"`
	Connected   bool              `json:"connected"` // 页面手动会话已建立
	Tools       int               `json:"tools"`
	Allow       []string          `json:"allow,omitempty"`
}

/* McpToolView 是连接后返回的工具清单条目。 */
type McpToolView struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ArgsSchema  json.RawMessage `json:"args_schema,omitempty"`
}

/*
McpService MCP 配置用例。连接全部走全局 router（系统级单例，
与 agent 的 mcp hook 共用同一连接池）；本服务只保留 mcp.json
读写与页面展示缓存（toolDefs）。
*/
type McpService struct {
	Fsys   fs.FileSystem
	Router *mcp.Router

	mu       sync.Mutex
	toolDefs map[string][]mcp.ToolDef // 页面连过的工具清单缓存（工具数展示）
}

/* NewMcpService 构造（main 装配用，router 为系统级单例）。 */
func NewMcpService(fsys fs.FileSystem, router *mcp.Router) *McpService {
	return &McpService{
		Fsys:     fsys,
		Router:   router,
		toolDefs: map[string][]mcp.ToolDef{},
	}
}

/* List 返回 MCP 页数据（连接状态以全局 router 为准）。 */
func (s *McpService) List() []McpServerView {
	f := loadMcpFileOrNil(s.Fsys)
	out := make([]McpServerView, 0)
	if f == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range f.Servers {
		connected := s.Router.Connected(srv.Name)
		out = append(out, McpServerView{
			Name:        srv.Name,
			Description: srv.Description,
			Transport:   srv.Type,
			Endpoint:    endpointOf(srv),
			Command:     srv.Command,
			Args:        srv.Args,
			Env:         srv.Env,
			Headers:     nonNilHeaders(srv.Headers),
			Enabled:     srv.IsEnabled(),
			Connected:   connected,
			Tools:       len(s.toolDefs[srv.Name]),
			Allow:       srv.Allow,
		})
	}
	return out
}

/*
Connect 经全局 router 建立会话并返回工具清单（连接前按 mcp.json
现值热替换，手改文件也即时生效）；连接失效自动逐出重建一次。
禁用的 server 不装配进 router，页面同样不可连（禁用即禁用）。
*/
func (s *McpService) Connect(ctx context.Context, name string) ([]McpToolView, error) {
	SyncMcpServers(s.Router, s.Fsys)
	defs, err := s.Router.Tools(ctx, name)
	if err != nil {
		s.Router.Drop(name) // 连接失效：逐出重建再试一次
		if defs, err = s.Router.Tools(ctx, name); err != nil {
			return nil, errMcp(name + ": " + err.Error())
		}
	}
	s.mu.Lock()
	s.toolDefs[name] = defs
	s.mu.Unlock()
	return toolViews(defs), nil
}

/* Disconnect 断开该 server 的全局连接（stdio 子进程随之释放）；未连接时幂等。 */
func (s *McpService) Disconnect(name string) {
	s.Router.Drop(name)
	s.mu.Lock()
	delete(s.toolDefs, name)
	s.mu.Unlock()
}

/*
Call 经全局 router 调用工具，未连接时懒建立（冷启动直调——HTTP 消费
者如快应用不必先 connect）。不查 allow——白名单约束 agent 的
mcp_router 路径，页面与 API 直调是用户操作。
*/
func (s *McpService) Call(ctx context.Context, server, tool string, args json.RawMessage) (string, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	out, err := s.Router.Call(ctx, server, tool, args)
	if err != nil {
		return "", errMcp(server + ": " + err.Error())
	}
	return out, nil
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
		switch srv.Type {
		case "http", "sse":
			if srv.URL == "" {
				return errMcp(srv.Type + " server requires url")
			}
		case "stdio":
			if srv.Command == "" {
				return errMcp("stdio server requires command")
			}
		default:
			return errMcp("type must be http, sse or stdio")
		}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := s.Fsys.Write(context.Background(), "mcp.json", data); err != nil {
		return err
	}
	// 即时热替换：被删除/禁用 server 的连接随之关闭，agent 与页面立即可见
	SyncMcpServers(s.Router, s.Fsys)
	s.mu.Lock()
	defer s.mu.Unlock()
	for name := range s.toolDefs {
		keep := false
		for _, srv := range f.Servers {
			if srv.Name == name && srv.IsEnabled() {
				keep = true
				break
			}
		}
		if !keep {
			delete(s.toolDefs, name)
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

func endpointOf(srv McpServerFile) string {
	if srv.Type == "stdio" {
		return srv.Command + " " + joinArgs(srv.Args)
	}
	return srv.URL
}

type mcpErr string

func (e mcpErr) Error() string { return string(e) }

func errMcp(msg string) error { return mcpErr(msg) }

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

/*
NewMcpRouter 构造系统级 router（全局唯一，连接跨 session 常驻；
main 装配一次，换代复用）。SyncMcpServers 按当前 mcp.json 热替换
server 列表（换代切数据目录、保存配置后即时生效共用此入口）。
*/
func NewMcpRouter(fsys fs.FileSystem) *mcp.Router {
	return mcp.NewRouter(buildServers(loadMcpFileOrNil(fsys)))
}

func SyncMcpServers(r *mcp.Router, fsys fs.FileSystem) {
	r.ReplaceServers(buildServers(loadMcpFileOrNil(fsys)))
}

/* NewMcpHook 注入全局 router 构造 mcp hook（连接生命周期归 router，hook 只使用）。
OnLoop Reload 兜底手改 mcp.json 的热加载；禁用项不装配。 */
func NewMcpHook(fsys fs.FileSystem, router *mcp.Router) *mcp.Hook {
	return mcp.NewHookWithRouter(router, func(context.Context) ([]mcp.ServerConfig, error) {
		return buildServers(loadMcpFileOrNil(fsys)), nil
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
	case "sse":
		sc.Factory = mcp.SSE(s.URL, s.Headers)
	case "stdio":
		if s.Command == "" {
			return sc, false
		}
		sc.Factory = mcp.Stdio(s.Command, s.Env, s.Args...)
	default:
		return sc, false
	}
	return sc, true
}
