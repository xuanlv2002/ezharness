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
	"errors"
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
	OAuth       *McpOAuthFile     `json:"oauth,omitempty"`
}

/* McpOAuthFile 描述一个 HTTP 类（http/sse）server 的 OAuth 接入；Enabled 触发 OAuth。 */
type McpOAuthFile struct {
	Enabled      bool     `json:"enabled"`
	ClientID     string   `json:"client_id,omitempty"`     // 空则走动态客户端注册（DCR）
	ClientSecret string   `json:"client_secret,omitempty"` // 机密客户端才需要
	Scopes       []string `json:"scopes,omitempty"`
	MetadataURL  string   `json:"metadata_url,omitempty"` // 授权服务器元数据地址（发现失败时手工指定）
}

/* IsEnabled 报告服务器是否启用。 */
func (s McpServerFile) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

/*
McpAuthRequiredError 表示某 server 需要 OAuth 授权且当前无有效令牌。
表现层据此回 HTTP 401 + needs_auth 契约。
*/
type McpAuthRequiredError struct {
	Server      string
	MetadataURL string // RFC9728 资源元数据地址（来自 401，可能为空）
}

func (e *McpAuthRequiredError) Error() string {
	return "mcp server " + e.Server + " requires OAuth authorization"
}

/*
McpServerView 是 MCP 页卡片数据。

Allow 必须回传：前端按视图回存单个条目，丢字段会清掉 mcp.json 里的白名单。
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
	Tools       int               `json:"tools"`     // 页面拉取过的工具数；-1 = 会话在但未拉取（agent 共用连接池时常见），0 = 确实无工具
	Allow       []string          `json:"allow,omitempty"`
	OAuth       bool              `json:"oauth"`                  // 该 server 配置了 OAuth
	Authorized  bool              `json:"authorized"`             // OAuth 令牌有效（非 OAuth server 恒 false）
	OAuthConfig *McpOAuthFile     `json:"oauth_config,omitempty"` // 原始 OAuth 配置（编辑/回存透传）
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
	Auth   *McpAuthManager

	mu       sync.Mutex
	toolDefs map[string][]mcp.ToolDef // 页面连过的工具清单缓存（工具数展示）
}

/* NewMcpService 构造（main 装配用，router 为系统级单例，auth 可空）。 */
func NewMcpService(fsys fs.FileSystem, router *mcp.Router, auth *McpAuthManager) *McpService {
	return &McpService{
		Fsys:     fsys,
		Router:   router,
		Auth:     auth,
		toolDefs: map[string][]mcp.ToolDef{},
	}
}

/* List 返回 MCP 页数据（连接状态以全局 router 为准）。 */
func (s *McpService) List(ctx context.Context) []McpServerView {
	f := loadMcpFileOrNil(ctx, s.Fsys)
	out := make([]McpServerView, 0)
	if f == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range f.Servers {
		connected := s.Router.Connected(srv.Name)
		tools := -1 // 会话在但页面没拉过清单(agent 共用连接池连的),数量未知
		if defs, ok := s.toolDefs[srv.Name]; ok {
			tools = len(defs)
		}
		oauth := (srv.OAuth != nil && srv.OAuth.Enabled) || (s.Auth != nil && s.Auth.NeedsOAuth(srv.Name))
		authorized := false
		if oauth && s.Auth != nil {
			authorized, _ = s.Auth.Status(ctx, srv.Name)
		}
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
			Tools:       tools,
			Allow:       srv.Allow,
			OAuth:       oauth,
			Authorized:  authorized,
			OAuthConfig: srv.OAuth,
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
	SyncMcpServers(ctx, s.Router, s.Fsys, s.Auth)
	defs, err := s.Router.ToolsWithAuth(ctx, name)
	if err != nil {
		var ar *mcp.AuthRequired
		if errors.As(err, &ar) {
			return nil, &McpAuthRequiredError{Server: ar.Server, MetadataURL: ar.MetadataURL} // 认证未过，重连无意义
		}
		s.Router.Drop(name) // 连接失效：逐出重建再试一次
		defs, err = s.Router.ToolsWithAuth(ctx, name)
		if err != nil {
			if errors.As(err, &ar) {
				return nil, &McpAuthRequiredError{Server: ar.Server, MetadataURL: ar.MetadataURL}
			}
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
	out, err := s.Router.CallWithAuth(ctx, server, tool, args)
	if err != nil {
		var ar *mcp.AuthRequired
		if errors.As(err, &ar) {
			return "", &McpAuthRequiredError{Server: ar.Server, MetadataURL: ar.MetadataURL}
		}
		return "", errMcp(server + ": " + err.Error())
	}
	return out, nil
}

/* ProbeOAuth 探测一个 server 是否需要 OAuth（新建保存后页面亮未授权徽标用）。 */
func (s *McpService) ProbeOAuth(ctx context.Context, name string) bool {
	if s.Auth == nil {
		return false
	}
	srv, ok := findServer(loadMcpFileOrNil(ctx, s.Fsys), name)
	if !ok {
		return false
	}
	_, hit := s.Auth.DiscoverOAuth(ctx, srv)
	return hit
}

/*
SaveServer 保存单个 server（同名覆盖，不存在则追加）。按条目落盘而不是
整表覆盖：页面上连续操作不同 server 时互不冲掉。
*/
func (s *McpService) SaveServer(ctx context.Context, srv McpServerFile) error {
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
	f := loadMcpFileOrNil(ctx, s.Fsys)
	if f == nil {
		f = &McpFile{}
	}
	replaced := false
	for i := range f.Servers {
		if f.Servers[i].Name == srv.Name {
			f.Servers[i] = srv
			replaced = true
			break
		}
	}
	if !replaced {
		f.Servers = append(f.Servers, srv)
	}
	return s.saveFile(ctx, f)
}

/* DeleteServer 删除单个 server；不存在视为已删。 */
func (s *McpService) DeleteServer(ctx context.Context, name string) error {
	if name == "" {
		return errMcp("server name required")
	}
	f := loadMcpFileOrNil(ctx, s.Fsys)
	if f == nil {
		return nil
	}
	out := f.Servers[:0]
	for _, srv := range f.Servers {
		if srv.Name != name {
			out = append(out, srv)
		}
	}
	f.Servers = out
	return s.saveFile(ctx, f)
}

/* saveFile 落盘并热替换连接，清掉已删除/禁用 server 的工具缓存。 */
func (s *McpService) saveFile(ctx context.Context, f *McpFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := s.Fsys.Write(ctx, "mcp.json", data); err != nil {
		return err
	}
	// 即时热替换：被删除/禁用 server 的连接随之关闭，agent 与页面立即可见
	SyncMcpServers(ctx, s.Router, s.Fsys, s.Auth)
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
func McpNames(ctx context.Context, fsys fs.FileSystem) []string {
	f := loadMcpFileOrNil(ctx, fsys)
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
func NewMcpRouter(ctx context.Context, fsys fs.FileSystem, auth *McpAuthManager) *mcp.Router {
	return mcp.NewRouter(buildServers(ctx, loadMcpFileOrNil(ctx, fsys), auth))
}

func SyncMcpServers(ctx context.Context, r *mcp.Router, fsys fs.FileSystem, auth *McpAuthManager) {
	r.ReplaceServers(buildServers(ctx, loadMcpFileOrNil(ctx, fsys), auth))
}

/*
	NewMcpHook 注入全局 router 构造 mcp hook（连接生命周期归 router，hook 只使用）。

OnLoop Reload 兜底手改 mcp.json 的热加载；禁用项不装配。返回的 channel
供宿主回传 MCP 授权决策（会话内待认证时解除阻塞）。
*/
func NewMcpHook(ctx context.Context, fsys fs.FileSystem, router *mcp.Router, auth *McpAuthManager) (*mcp.Hook, chan<- mcp.AuthDecision) {
	return mcp.NewHookWithRouter(router, func(context.Context) ([]mcp.ServerConfig, error) {
		return buildServers(ctx, loadMcpFileOrNil(ctx, fsys), auth), nil
	})
}

func loadMcpFileOrNil(ctx context.Context, fsys fs.FileSystem) *McpFile {
	data, err := fsys.Read(ctx, "mcp.json")
	if err != nil {
		return nil
	}
	var f McpFile
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	return &f
}

func buildServers(ctx context.Context, f *McpFile, auth *McpAuthManager) []mcp.ServerConfig {
	if f == nil {
		return nil
	}
	out := make([]mcp.ServerConfig, 0, len(f.Servers))
	for _, s := range f.Servers {
		if !s.IsEnabled() {
			continue
		}
		if sc, ok := buildServerConfig(ctx, s, auth); ok {
			out = append(out, sc)
		}
	}
	return out
}

func buildServerConfig(ctx context.Context, s McpServerFile, auth *McpAuthManager) (mcp.ServerConfig, bool) {
	sc := mcp.ServerConfig{Name: s.Name, Description: s.Description, Allow: s.Allow}
	switch s.Type {
	case "http":
		// OAuth 配置在建连时解析（非构建时固化）：DCR 登记的 client id 是
		// 授权过程中才写盘的，固化会使首次授权后的重试仍带空 client id。
		sc.Factory = func(cfg mcp.ServerConfig) (mcp.Client, error) {
			return mcp.StreamableHTTP(s.URL, s.Headers, oauthOpts(ctx, s, auth)...)(cfg)
		}
	case "sse":
		sc.Factory = func(cfg mcp.ServerConfig) (mcp.Client, error) {
			return mcp.SSE(s.URL, s.Headers, oauthOpts(ctx, s, auth)...)(cfg)
		}
	case "stdio":
		if s.Command == "" {
			return sc, false
		}
		sc.Factory = mcp.Stdio(s.Command, s.Env, s.Args...)
	default:
		return sc, false
	}
	// 裸 401 时由宿主探测 OAuth 发现文档（自动识别兜底，http/sse）
	if auth != nil && (s.Type == "http" || s.Type == "sse") {
		sc.DiscoverOAuth = func(c context.Context) (string, bool) { return auth.DiscoverOAuth(c, s) }
	}
	return sc, true
}

/*
oauthOpts 为 http/sse 且需要 OAuth 的 server 组出工厂选项（stdio 不适用）。
判定：mcp.json 显式启用，或管理器已自动检测出需要 OAuth（无需用户配置）。
*/
func oauthOpts(ctx context.Context, s McpServerFile, auth *McpAuthManager) []mcp.Option {
	if auth == nil || (s.Type != "http" && s.Type != "sse") {
		return nil
	}
	explicit := s.OAuth != nil && s.OAuth.Enabled
	if !explicit && !auth.NeedsOAuth(s.Name) {
		return nil
	}
	return []mcp.Option{mcp.WithOAuth(*auth.oauthConfig(ctx, s))}
}
