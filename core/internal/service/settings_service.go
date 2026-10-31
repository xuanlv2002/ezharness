/*
SettingsService 与 MemoryService：运行配置与长期记忆的读写用例。
*/
package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/builtinskill"
	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
	"ezharness/core/internal/tools"
	"ezharness/core/internal/warp/modeldump"
)

/* SettingsService 设置用例。 */
type SettingsService struct {
	Hub    *domain.Hub
	Agents *AgentService
}

/*
	SettingsView 是设置页行为设置视图（模型归 /api/models）。

TrimPercent/WorkDir/CloseToTray/MaxIterations 用指针：区分"未提交该
字段"与"提交空值（0=禁用压缩 / 空=工作目录回默认 / false=关闭托盘
常驻 / 0=迭代次数回默认 12）。
*/
type SettingsView struct {
	SystemExtra     string  `json:"systemExtra"`
	TrimPercent     *int    `json:"compactPercent,omitempty"`
	WorkDir         *string `json:"workDir,omitempty"`
	CloseToTray     *bool   `json:"closeToTray,omitempty"`
	PetEnabled      *bool   `json:"petEnabled,omitempty"`
	MaxIterations   *int    `json:"maxIterations,omitempty"`
	DebugMode       *bool   `json:"debugMode,omitempty"`
	ReasoningEffort *string `json:"reasoningEffort,omitempty"` // 思考档位（''=默认 | none | low | high | max）
	ApproveMode     *string `json:"approveMode,omitempty"`     // 审批档位（''=跟随安全配置 | off=全放行 | strict=全审）
	OffloadThreshold *int   `json:"offloadThreshold,omitempty"` // 工具结果卸载阈值（字节，0=默认 4096）
}

/* Get 返回当前行为设置。 */
func (s *SettingsService) Get() SettingsView {
	st := s.Hub.SettingsSnapshot()
	p := st.TrimPercent
	w := st.WorkDir
	return SettingsView{SystemExtra: st.SystemExtra, TrimPercent: &p, WorkDir: &w,
		CloseToTray: &st.CloseToTray, PetEnabled: &st.PetEnabled, DebugMode: &st.DebugMode, ReasoningEffort: &st.ReasoningEffort,
		ApproveMode: &st.ApproveMode, MaxIterations: &st.MaxIterations, OffloadThreshold: &st.OffloadThreshold} // 回读必须带——漏了它前端回落默认 64，表现为"切页后被重置"
}

/* Update 保存行为设置并重建 agent（busy 时拒绝；水位随 Reassemble 生效）。 */
func (s *SettingsService) Update(ctx context.Context, v SettingsView) error {
	st := s.Hub.SettingsSnapshot()
	st.SystemExtra = v.SystemExtra
	if v.TrimPercent != nil {
		if *v.TrimPercent < 0 || *v.TrimPercent > 100 {
			return errors.New("压缩水位百分比需在 0-100 之间")
		}
		st.TrimPercent = *v.TrimPercent
	}
	if v.WorkDir != nil {
		st.WorkDir = strings.TrimSpace(*v.WorkDir)
	}
	if v.CloseToTray != nil {
		st.CloseToTray = *v.CloseToTray
	}
	if v.PetEnabled != nil {
		st.PetEnabled = *v.PetEnabled
	}
	if v.DebugMode != nil {
		st.DebugMode = *v.DebugMode
	}
	if v.MaxIterations != nil {
		if *v.MaxIterations < 0 || *v.MaxIterations > 128 {
			return errors.New("最大迭代次数需在 0-128 之间（0 = 默认 64）")
		}
		st.MaxIterations = *v.MaxIterations
	}
	if v.ReasoningEffort != nil {
		if !validReasoningEffort(*v.ReasoningEffort) {
			return errors.New("思考档位取值非法（空/none/low/medium/high/xhigh/max）")
		}
		st.ReasoningEffort = *v.ReasoningEffort
	}
	if v.OffloadThreshold != nil {
		if *v.OffloadThreshold < 0 || *v.OffloadThreshold > 1048576 {
			return errors.New("工具卸载阈值需在 0-1048576 字节之间（0 = 默认 4096）")
		}
		st.OffloadThreshold = *v.OffloadThreshold
	}
	if st.WorkDir != "" {
		if abs, err := filepath.Abs(st.WorkDir); err == nil {
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("工作目录不可用: %w", err)
			}
		}
	}
	if err := SaveSettings(ctx, s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	modeldump.SetEnabled(st.DebugMode) // 调试模式即改即生效，不必等 Reassemble
	return s.Agents.Reassemble(st)
}

/*
ModelCall 是 HTTP /api/open/model/call 的用例（直调大模型，与 mcp/call
同级的系统内置能力）：model 空 = 主模型；images 路径按目标模型视觉能力附带。
*/
func (s *SettingsService) ModelCall(ctx context.Context, model, prompt string, images []string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("prompt required")
	}
	m := s.Hub.ModelsSnapshot()
	entry := m.ActiveMain()
	if model != "" {
		if e := findModelEntry(m, model); e != nil {
			entry = e
		} else {
			return "", fmt.Errorf("unknown model %q", model)
		}
	}
	if entry == nil || entry.Name == "" {
		return "", errors.New("no model configured")
	}
	msg := types.Message{Role: types.RoleUser, Content: prompt}
	if len(images) > 0 {
		if !entry.Vision {
			return "", fmt.Errorf("模型 %s 不支持视觉输入（images 被拒绝）", entry.Name)
		}
		parts, err := tools.LoadImages(ctx, s.Hub.Fsys, images)
		if err != nil {
			return "", err
		}
		msg.Images = parts
	}
	return OneShotInvoke(ctx, *entry, types.ModelRequest{Messages: []types.Message{msg}})
}

/* validReasoningEffort 校验思考档位：Claude Code / Codex 分档 + none + 空（请求侧再归一为 low/high/max）。 */
func validReasoningEffort(e string) bool {
	switch e {
	case "", "none", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

/* SetReasoningEffort 设置思考档位并落盘（请求侧闭包实时读取，即改即生效，无需重建 agent）。 */
func (s *SettingsService) SetReasoningEffort(ctx context.Context, effort string) error {
	if !validReasoningEffort(effort) {
		return errors.New("思考档位取值非法（空/none/low/medium/high/xhigh/max）")
	}
	st := s.Hub.SettingsSnapshot()
	st.ReasoningEffort = effort
	if err := SaveSettings(ctx, s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	return nil
}

/* validApproveMode 校验审批档位：''（跟随安全配置）/ off（全放行）/ strict（全审）。 */
func validApproveMode(m string) bool {
	switch m {
	case "", "off", "strict":
		return true
	}
	return false
}

/* SetApproveMode 设置审批档位并落盘（工具审批实时读全局快照，即改即生效，无需重建 agent）。 */
func (s *SettingsService) SetApproveMode(ctx context.Context, mode string) error {
	if !validApproveMode(mode) {
		return errors.New("审批档位取值非法（空/off/strict）")
	}
	st := s.Hub.SettingsSnapshot()
	st.ApproveMode = mode
	if err := SaveSettings(ctx, s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	return nil
}

/* GetModels 返回模型三槽（空槽归一为 [] 而非 null）。 */
func (s *SettingsService) GetModels() domain.ModelsConfig {
	return normalizeModels(s.Hub.ModelsSnapshot())
}

/* normalizeModels 把 nil 槽归一为空切片（JSON null → []）。 */
func normalizeModels(m domain.ModelsConfig) domain.ModelsConfig {
	if m.Main == nil {
		m.Main = []domain.ModelEntry{}
	}
	if m.Image == nil {
		m.Image = []domain.ModelEntry{}
	}
	if m.Audio == nil {
		m.Audio = []domain.ModelEntry{}
	}
	return m
}

/*
	UpdateModels 保存模型三槽（每槽至多一条启用，main 槽不可为空且必须

有启用条目——能力槽允许全部停用）并重建 agent（busy 时拒绝）。
*/
func (s *SettingsService) UpdateModels(ctx context.Context, m domain.ModelsConfig) error {
	if len(m.Main) == 0 {
		return errors.New("main 槽至少需要一个模型")
	}
	mainOn := false
	for _, e := range m.Main {
		if e.Enabled {
			mainOn = true
			break
		}
	}
	if !mainOn {
		m.Main[0].Enabled = true // 兜底：对话必须由主模型驱动，与 ActiveMain 取首条语义对齐
	}
	for slot, entries := range map[string][]domain.ModelEntry{
		"main": m.Main, "image": m.Image, "audio": m.Audio,
	} {
		enabled := 0
		for _, e := range entries {
			if e.Enabled {
				enabled++
			}
		}
		if enabled > 1 {
			return fmt.Errorf("%s 槽至多启用一个模型", slot)
		}
	}
	if err := SaveModelsConfig(ctx, s.Hub.Fsys, normalizeModels(m)); err != nil {
		return err
	}
	s.Hub.ApplyModels(m)
	return s.Agents.Reassemble(s.Hub.SettingsSnapshot())
}

/*
	SecurityRules 返回当前审批策略：用户档为准，DefaultToolRules 补缺。

DefaultToolRules 同时是「合法键全集」——落盘档里的键对不上它的，一律不展示：
工具/动作已经删了，它的策略也就没有了（安全页只呈现真实存在的工具面）。
旧键留在文件里不碍事，用户下次保存时自然写掉。
*/
func (s *SettingsService) SecurityRules() []domain.ToolRule {
	defaults := domain.DefaultToolRules()
	known := make(map[string]bool, len(defaults))
	for _, r := range defaults {
		known[r.Key] = true
	}
	seen := map[string]bool{}
	out := make([]domain.ToolRule, 0, len(defaults))
	for _, r := range s.Hub.ToolRulesSnapshot() {
		if r.Key == "" || !known[r.Key] {
			continue // 无 key 的旧格式残档 / 已删掉的工具与动作，都由内置默认补位
		}
		out = append(out, r)
		seen[r.Key] = true
	}
	for _, r := range defaults {
		if !seen[r.Key] {
			out = append(out, r)
		}
	}
	return out
}

/* UpdateSecurity 保存审批策略。needsApprove 运行时读设置快照，即时生效。 */
func (s *SettingsService) UpdateSecurity(ctx context.Context, rules []domain.ToolRule) error {
	for _, r := range rules {
		if r.Key == "" {
			return errors.New("工具策略缺少 key")
		}
		if !domain.ValidLevel(r.Key, r.Level) {
			return fmt.Errorf("非法档位 %q（%s）", r.Level, r.Key)
		}
	}
	if err := SaveToolRules(ctx, s.Hub.Fsys, rules); err != nil {
		return err
	}
	s.Hub.ApplyToolRules(rules)
	return nil
}

/* MemoryService 记忆用例。 */
type MemoryService struct {
	Hub *domain.Hub
}

/* FileInfoView 是记忆文件条目。 */
type FileInfoView struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"`
}

/* SkillEntryView 是能力记忆（skill）条目。 */
type SkillEntryView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Enabled bool   `json:"enabled"`
	Builtin bool   `json:"builtin,omitempty"` // 内建技能：可禁用不可删
}

/*
	MemoryConfigView 是记忆页数据：长期记忆/能力记忆两个文件夹的清单。

话题记忆（完整会话树）走独立端点 GET /api/memory/tree。
*/
type MemoryConfigView struct {
	Skills struct {
		Dir   string           `json:"dir"`
		Items []SkillEntryView `json:"items"`
	} `json:"skills"`
	Topics struct {
		Dir string `json:"dir"`
	} `json:"topics"`
}

/* Config 汇总记忆页数据（目录缺失容错为空列表）。 */
func (m *MemoryService) Config(ctx context.Context) MemoryConfigView {
	var v MemoryConfigView
	v.Skills.Dir = hooks.SkillsDir
	v.Topics.Dir = hooks.SessionsDir

	disabled := m.Hub.SettingsSnapshot().DisabledSkills
	for _, s := range builtinskill.Skills() {
		id := hooks.SkillDirOf(s.Path)
		v.Skills.Items = append(v.Skills.Items, SkillEntryView{
			ID: id, Name: s.Name, Desc: s.Description,
			Enabled: !slices.Contains(disabled, id), Builtin: true,
		})
	}
	if entries, err := skill.LoadDir(ctx, m.Hub.Fsys, hooks.SkillsDir); err == nil {
		for _, s := range entries {
			id := hooks.SkillDirOf(s.Path)
			// 与内建技能同名的用户技能让位（内建优先）：id 是前端 keyed each 的 key，撞名崩整页
			if slices.ContainsFunc(v.Skills.Items, func(it SkillEntryView) bool { return it.ID == id }) {
				continue
			}
			v.Skills.Items = append(v.Skills.Items, SkillEntryView{
				ID:      id,
				Name:    s.Name,
				Desc:    s.Description,
				Enabled: !slices.Contains(disabled, id),
			})
		}
	}
	if v.Skills.Items == nil {
		v.Skills.Items = []SkillEntryView{}
	}
	return v
}

/* SkillContentView 是技能详情：清单字段 + SKILL.md 正文（已剥离 frontmatter）。 */
type SkillContentView struct {
	SkillEntryView
	Content string `json:"content"`
	Path    string `json:"path"`
}

/* SkillContent 读取单个技能的指令正文；内建优先，与 Config 的归置一致。 */
func (m *MemoryService) SkillContent(ctx context.Context, id string) (SkillContentView, error) {
	if !validSkillID(id) {
		return SkillContentView{}, fmt.Errorf("非法技能名 %q", id)
	}
	disabled := m.Hub.SettingsSnapshot().DisabledSkills
	view := func(s skill.Skill, builtin bool) SkillContentView {
		return SkillContentView{
			SkillEntryView: SkillEntryView{
				ID:      hooks.SkillDirOf(s.Path),
				Name:    s.Name,
				Desc:    s.Description,
				Enabled: !slices.Contains(disabled, hooks.SkillDirOf(s.Path)),
				Builtin: builtin,
			},
			Content: s.Instructions,
			Path:    s.Path,
		}
	}
	for _, s := range builtinskill.Skills() {
		if hooks.SkillDirOf(s.Path) == id {
			return view(s, true), nil
		}
	}
	if entries, err := skill.LoadDir(ctx, m.Hub.Fsys, hooks.SkillsDir); err == nil {
		for _, s := range entries {
			if hooks.SkillDirOf(s.Path) == id {
				return view(s, false), nil
			}
		}
	}
	return SkillContentView{}, fmt.Errorf("技能 %q 不存在", id)
}

func fileMtime(path string) string {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime().Format("2006-01-02 15:04")
	}
	return ""
}

/* ── 长期记忆（条目式，与 memory 工具同一存储层）── */

/* LongtermView 是条目式长期记忆全量数据。 */
type LongtermView struct {
	User     []hooks.MemoryItem   `json:"user"`
	Soul     []hooks.MemoryItem   `json:"soul"`
	Projects []hooks.ProjectEntry `json:"projects"`
}

/* Longterm 返回 user/soul 条目与项目索引。 */
func (m *MemoryService) Longterm(ctx context.Context) (*LongtermView, error) {
	user, err := hooks.LoadItems(ctx, m.Hub.Fsys, hooks.UserJson)
	if err != nil {
		return nil, err
	}
	soul, err := hooks.LoadItems(ctx, m.Hub.Fsys, hooks.SoulJson)
	if err != nil {
		return nil, err
	}
	projects, err := hooks.LoadProjects(ctx, m.Hub.Fsys)
	if err != nil {
		return nil, err
	}
	return &LongtermView{User: user, Soul: soul, Projects: projects}, nil
}

/*
SaveLongtermItems 整组保存条目（记忆页编辑器保存）：新增条目 id 为空时
按 NextItemID 补号，updatedAt 为零值时按当前时间补齐。
*/
func (m *MemoryService) SaveLongtermItems(ctx context.Context, target string, items []hooks.MemoryItem) error {
	var path, prefix string
	switch target {
	case "user":
		path, prefix = hooks.UserJson, "u"
	case "soul":
		path, prefix = hooks.SoulJson, "s"
	default:
		return fmt.Errorf("target 需为 user 或 soul")
	}
	now := time.Now()
	base := hooks.MaxItemNumber(items, prefix)
	for i := range items {
		items[i].Text = strings.TrimSpace(items[i].Text)
		if items[i].ID == "" {
			base++ // 多条新增依次递增，不复用
			items[i].ID = fmt.Sprintf("%s%d", prefix, base)
		}
		if items[i].UpdatedAt.IsZero() {
			items[i].UpdatedAt = now
		}
	}
	return hooks.SaveItems(ctx, m.Hub.Fsys, path, items)
}

/* ProjectDetailView 是项目记忆详情（索引条目 + 全文）。 */
type ProjectDetailView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

/* GetProject 返回项目记忆详情。 */
func (m *MemoryService) GetProject(ctx context.Context, id string) (*ProjectDetailView, error) {
	projects, err := hooks.LoadProjects(ctx, m.Hub.Fsys)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(projects, func(p hooks.ProjectEntry) bool { return p.ID == id })
	if i < 0 {
		return nil, os.ErrNotExist
	}
	v := &ProjectDetailView{ID: projects[i].ID, Name: projects[i].Name, Description: projects[i].Description}
	if data, rerr := m.Hub.Fsys.Read(ctx, hooks.ProjectContentPath(id)); rerr == nil {
		v.Content = string(data)
	}
	return v, nil
}

/* SaveProject 新建/整体更新项目记忆（id 空时由 name 生成 slug），返回实际 id。 */
func (m *MemoryService) SaveProject(ctx context.Context, id, name, description, content string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name required")
	}
	if id = strings.TrimSpace(id); id == "" {
		id = hooks.ProjectSlug(name)
	}
	if id == "" {
		return "", fmt.Errorf("项目名无法生成有效 id")
	}
	projects, err := hooks.LoadProjects(ctx, m.Hub.Fsys)
	if err != nil {
		return "", err
	}
	if i := slices.IndexFunc(projects, func(p hooks.ProjectEntry) bool { return p.ID == id }); i >= 0 {
		projects[i].Name = name
		projects[i].Description = strings.TrimSpace(description)
	} else {
		projects = append(projects, hooks.ProjectEntry{ID: id, Name: name, Description: strings.TrimSpace(description)})
	}
	if err := hooks.SaveProjects(ctx, m.Hub.Fsys, projects); err != nil {
		return "", err
	}
	if err := m.Hub.Fsys.Write(ctx, hooks.ProjectContentPath(id), []byte(strings.TrimSpace(content)+"\n")); err != nil {
		return "", err
	}
	return id, nil
}

/* DeleteProject 删除项目（索引 + 全文）。 */
func (m *MemoryService) DeleteProject(ctx context.Context, id string) error {
	projects, err := hooks.LoadProjects(ctx, m.Hub.Fsys)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(projects, func(p hooks.ProjectEntry) bool { return p.ID == id })
	if i < 0 {
		return os.ErrNotExist
	}
	projects = slices.Delete(projects, i, i+1)
	if err := hooks.SaveProjects(ctx, m.Hub.Fsys, projects); err != nil {
		return err
	}
	if err := os.Remove(filepath.FromSlash(hooks.ProjectContentPath(id))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

/* ── 记忆待确认操作（memory 工具与 distill 落档，通知栏确认）── */

/* MemoryPendingNotices 待确认操作 → 通知条目（跨重启持久）。 */
func (m *MemoryService) MemoryPendingNotices(ctx context.Context) []domain.PendingNotice {
	ops, err := hooks.LoadPendingOps(ctx, m.Hub.Fsys)
	if err != nil {
		return nil
	}
	out := make([]domain.PendingNotice, 0, len(ops))
	for _, op := range ops {
		args, _ := json.Marshal(map[string]string{"question": op.DescribeDetail()})
		out = append(out, domain.PendingNotice{
			CallID: op.ID,
			Kind:   "memory",
			Tool:   "记忆待确认",
			Args:   string(args),
			Ts:     op.CreatedAt.UnixMilli(),
		})
	}
	return out
}

/* DecideMemoryPending 确认（应用）或取消（丢弃）一条待确认记忆操作。 */
func (m *MemoryService) DecideMemoryPending(ctx context.Context, id string, accept bool) (string, error) {
	return hooks.DecidePendingOp(ctx, m.Hub.Fsys, id, accept)
}

/* ── 技能管理：启停 / 删除 / zip 新建 ── */

/*
validSkillID 校验技能目录名（同时用作磁盘目录与 URL 参数）：拒绝空、
. ..、路径分隔符与盘符（防穿越）、首尾空格/点（Windows 会剥离导致名实
不符）与保留设备名（CON/NUL/COM1-9 等）；其余字符（含中文）放行。
*/
func validSkillID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\:`) {
		return false
	}
	if id != strings.Trim(id, " .") {
		return false
	}
	upper := strings.ToUpper(id)
	switch upper {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) &&
		upper[3] >= '1' && upper[3] <= '9' {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

/*
ToggleSkill 切换技能启停（DisabledSkills 名单按目录名增删）。load_skill
与状态面板经闭包实时读取设置快照，即时生效；system 清单是 session 级
快照，下个 session 生效（与 skill 文件编辑同语义）。
*/
func (m *MemoryService) ToggleSkill(ctx context.Context, id string, enabled bool) error {
	if !validSkillID(id) {
		return fmt.Errorf("非法技能名 %q", id)
	}
	st := m.Hub.SettingsSnapshot()
	cur := slices.Contains(st.DisabledSkills, id)
	switch {
	case enabled && cur:
		st.DisabledSkills = slices.DeleteFunc(st.DisabledSkills, func(s string) bool { return s == id })
	case !enabled && !cur:
		st.DisabledSkills = append(st.DisabledSkills, id)
	default:
		return nil
	}
	if err := SaveSettings(ctx, m.Hub.Fsys, st); err != nil {
		return err
	}
	m.Hub.ApplySettings(st)
	return nil
}

/* DeleteSkill 删除技能目录（连同 scripts 等子资源），并清理禁用名单残留。 */
func (m *MemoryService) DeleteSkill(ctx context.Context, id string) error {
	if !validSkillID(id) {
		return fmt.Errorf("非法技能名 %q", id)
	}
	if builtinskill.IsBuiltin(id) {
		return fmt.Errorf("内建技能 %q 不可删除（可禁用）", id)
	}
	dir := hooks.SkillsDir + "/" + id
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("技能 %q 不存在", id)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除技能目录失败: %w", err)
	}
	if st := m.Hub.SettingsSnapshot(); slices.Contains(st.DisabledSkills, id) {
		st.DisabledSkills = slices.DeleteFunc(st.DisabledSkills, func(s string) bool { return s == id })
		if err := SaveSettings(ctx, m.Hub.Fsys, st); err != nil {
			return err
		}
		m.Hub.ApplySettings(st)
	}
	return nil
}

/* 技能 zip 上传的大小护栏：压缩包 ≤ 20MB，解压后总内容 ≤ 10MB。 */
const (
	skillZipMax      = 20 << 20
	skillUnzippedMax = 10 << 20
)

/*
CreateSkill 从 zip 压缩包新建技能，目录名自动推导：单文件夹整体压缩
取外层文件夹名，平铺取 SKILL.md frontmatter 的 name。解压写入
memory/skills/<名>/，zip 需含根级 SKILL.md（唯一必需文件）。
*/
func (m *MemoryService) CreateSkill(ctx context.Context, zipData []byte) error {
	if len(zipData) > skillZipMax {
		return errors.New("压缩包超过 20MB 上限")
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("压缩包无法读取: %w", err)
	}
	name, files, err := unzipSkill(zr)
	if err != nil {
		return err
	}
	if !validSkillID(name) {
		return fmt.Errorf("无法从压缩包确定可用的技能名（%q）：单文件夹压缩取文件夹名，平铺需在 SKILL.md frontmatter 提供 name", name)
	}
	if _, err := os.Stat(hooks.SkillsDir + "/" + name); err == nil {
		return fmt.Errorf("技能 %q 已存在", name)
	}
	for _, f := range files {
		if err := m.Hub.Fsys.Write(ctx, hooks.SkillsDir+"/"+name+"/"+f.name, f.data); err != nil {
			return fmt.Errorf("写入 %q 失败: %w", f.name, err)
		}
	}
	return nil
}

/* skillZipEntry 是解压出的待写文件。 */
type skillZipEntry struct {
	name string
	data []byte
}

/*
unzipSkill 解析技能 zip 为待写文件清单与推导的技能目录名：条目名规范
为 / 分隔，拒绝 ..、绝对路径与盘符（zip-slip 防护）；跳过 __MACOSX/
.DS_Store；所有条目共享同一顶级目录时剥掉该层（"压缩整个文件夹"形态，
目录名取该层）；平铺形态取 SKILL.md frontmatter 的 name。
*/
func unzipSkill(zr *zip.Reader) (string, []skillZipEntry, error) {
	var list []skillZipEntry
	total := 0
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() {
			continue
		}
		drop := name == ""
		for _, p := range strings.Split(name, "/") {
			if p == "" || p == "." || p == ".." || p == "__MACOSX" || p == ".DS_Store" || strings.Contains(p, ":") {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", nil, fmt.Errorf("读取压缩包条目 %q 失败: %w", f.Name, err)
		}
		data, rerr := io.ReadAll(io.LimitReader(rc, skillUnzippedMax+1))
		rc.Close()
		if rerr != nil {
			return "", nil, fmt.Errorf("解压 %q 失败: %w", f.Name, rerr)
		}
		if total += len(data); total > skillUnzippedMax {
			return "", nil, errors.New("解压内容超过 10MB 上限")
		}
		list = append(list, skillZipEntry{name: name, data: data})
	}
	if len(list) == 0 {
		return "", nil, errors.New("压缩包为空")
	}
	skillName := ""
	// 共享顶级目录剥层：首个条目的第一段在其余所有条目路径中出现才剥。
	if i := strings.Index(list[0].name, "/"); i >= 0 {
		prefix := list[0].name[:i+1]
		shared := true
		for _, e := range list {
			if !strings.HasPrefix(e.name, prefix) {
				shared = false
				break
			}
		}
		if shared {
			skillName = prefix[:len(prefix)-1]
			for i := range list {
				list[i].name = list[i].name[len(prefix):]
			}
		}
	}
	for _, e := range list {
		if e.name == skill.SkillFile {
			if skillName == "" {
				skillName = frontmatterName(string(e.data))
			}
			return skillName, list, nil
		}
	}
	return "", nil, errors.New("压缩包缺少 SKILL.md（技能的唯一必需文件）")
}

/*
	frontmatterName 提取 SKILL.md frontmatter 的 name 字段（扁平

`name: xx` 行；无 frontmatter 或无 name 返回空）。
*/
func frontmatterName(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, l := range lines[1:] {
		t := strings.TrimSpace(l)
		if t == "---" {
			return ""
		}
		if strings.HasPrefix(t, "name:") {
			return strings.TrimSpace(strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "name:")), `"'`))
		}
	}
	return ""
}

/*
ActivateMainModel 把 main 槽切换到指定模型（唯一启用该条目）并重建
agent（busy 时拒绝，运行中的分支保留旧装配到轮结束）。输入框模型
切换器用：下一次输入即走新模型。
*/
func (s *SettingsService) ActivateMainModel(ctx context.Context, name string) (domain.ModelEntry, error) {
	m := LoadModelsConfig(ctx, s.Hub.Fsys)
	found := false
	for i := range m.Main {
		m.Main[i].Enabled = m.Main[i].Name == name
		if m.Main[i].Enabled {
			m.Main[i].Disabled = false
			found = true
		}
	}
	if !found {
		return domain.ModelEntry{}, fmt.Errorf("main 槽没有名为 %s 的模型", name)
	}
	if err := SaveModelsConfig(ctx, s.Hub.Fsys, normalizeModels(m)); err != nil {
		return domain.ModelEntry{}, err
	}
	s.Hub.ApplyModels(m)
	if err := s.Agents.Reassemble(s.Hub.SettingsSnapshot()); err != nil {
		return domain.ModelEntry{}, err
	}
	out := domain.ModelEntry{}
	for _, e := range m.Main {
		if e.Enabled {
			out = e
			break
		}
	}
	return out, nil
}
