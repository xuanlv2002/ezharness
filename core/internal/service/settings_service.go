/*
SettingsService 与 MemoryService：运行配置与长期记忆的读写用例。
*/
package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/hooks"
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
	SystemExtra   string  `json:"systemExtra"`
	TrimPercent   *int    `json:"compactPercent,omitempty"`
	WorkDir       *string `json:"workDir,omitempty"`
	CloseToTray   *bool   `json:"closeToTray,omitempty"`
	MaxIterations *int    `json:"maxIterations,omitempty"`
}

/* Get 返回当前行为设置。 */
func (s *SettingsService) Get() SettingsView {
	st := s.Hub.SettingsSnapshot()
	p := st.TrimPercent
	w := st.WorkDir
	return SettingsView{SystemExtra: st.SystemExtra, TrimPercent: &p, WorkDir: &w, CloseToTray: &st.CloseToTray}
}

/* Update 保存行为设置并重建 agent（busy 时拒绝；水位随 Reassemble 生效）。 */
func (s *SettingsService) Update(v SettingsView) error {
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
	if v.MaxIterations != nil {
		if *v.MaxIterations < 0 || *v.MaxIterations > 50 {
			return errors.New("最大迭代次数需在 0-50 之间（0 = 默认 12）")
		}
		st.MaxIterations = *v.MaxIterations
	}
	if st.WorkDir != "" {
		if abs, err := filepath.Abs(st.WorkDir); err == nil {
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("工作目录不可用: %w", err)
			}
		}
	}
	if err := domain.SaveSettings(s.Hub.Fsys, st); err != nil {
		return err
	}
	s.Hub.ApplySettings(st)
	return s.Agents.Reassemble(st)
}

/* GetModels 返回模型四槽（空槽归一为 [] 而非 null）。 */
func (s *SettingsService) GetModels() domain.ModelsConfig {
	return normalizeModels(s.Hub.ModelsSnapshot())
}

/* normalizeModels 把 nil 槽归一为空切片（JSON null → []）。 */
func normalizeModels(m domain.ModelsConfig) domain.ModelsConfig {
	if m.Main == nil {
		m.Main = []domain.ModelEntry{}
	}
	if m.Vision == nil {
		m.Vision = []domain.ModelEntry{}
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
	UpdateModels 保存模型四槽（每槽至多一条启用，main 槽不可为空且必须

有启用条目——能力槽允许全部停用）并重建 agent（busy 时拒绝）。
*/
func (s *SettingsService) UpdateModels(m domain.ModelsConfig) error {
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
		"main": m.Main, "vision": m.Vision, "image": m.Image, "audio": m.Audio,
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
	if err := domain.SaveModelsConfig(s.Hub.Fsys, normalizeModels(m)); err != nil {
		return err
	}
	s.Hub.ApplyModels(m)
	return s.Agents.Reassemble(s.Hub.SettingsSnapshot())
}

/*
	SecurityRules 返回当前审批策略：用户档为准，DefaultToolRules 补缺

（精简档没覆盖的新工具按内置默认档出现，安全页可配全部工具）。
*/
func (s *SettingsService) SecurityRules() []domain.ToolRule {
	rules := s.Hub.ToolRulesSnapshot()
	seen := map[string]bool{}
	out := make([]domain.ToolRule, 0, len(rules)+4)
	for _, r := range rules {
		out = append(out, r)
		seen[r.Tool] = true
	}
	for _, r := range domain.DefaultToolRules() {
		if !seen[r.Tool] {
			out = append(out, r)
		}
	}
	return out
}

/* UpdateSecurity 保存审批策略。needsApprove 运行时读设置快照，即时生效。 */
func (s *SettingsService) UpdateSecurity(rules []domain.ToolRule) error {
	valid := map[domain.Level]bool{
		domain.LevelAsk: true, domain.LevelBlack: true,
		domain.LevelWhite: true, domain.LevelAuto: true,
	}
	for _, r := range rules {
		if !valid[r.Level] {
			return fmt.Errorf("非法档位 %q（tool %s）", r.Level, r.Tool)
		}
		if r.Tool == "task" && (r.Level == domain.LevelBlack || r.Level == domain.LevelWhite) {
			return errors.New("task 只支持 审批/免审（分身继承主 agent 策略）")
		}
	}
	if err := domain.SaveToolRules(s.Hub.Fsys, rules); err != nil {
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
}

/*
	MemoryConfigView 是记忆页数据：长期记忆/能力记忆两个文件夹的清单。

话题记忆（完整会话树）走独立端点 GET /api/memory/tree。
*/
type MemoryConfigView struct {
	Longterm struct {
		Dir       string         `json:"dir"`
		HarnessMd *FileInfoView  `json:"harnessMd"`
		Files     []FileInfoView `json:"files"`
	} `json:"longterm"`
	Skills struct {
		Dir   string           `json:"dir"`
		Items []SkillEntryView `json:"items"`
	} `json:"skills"`
	Topics struct {
		Dir string `json:"dir"`
	} `json:"topics"`
}

/* Config 汇总记忆页数据（目录缺失容错为空列表）。 */
func (m *MemoryService) Config() MemoryConfigView {
	var v MemoryConfigView
	v.Longterm.Dir = hooks.LongtermDir
	v.Skills.Dir = hooks.SkillsDir
	v.Topics.Dir = hooks.SessionsDir

	if entries, err := m.Hub.Fsys.List(context.Background(), hooks.LongtermDir); err == nil {
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			fi := FileInfoView{Name: e.Name, Size: e.Size, Mtime: fileMtime(hooks.LongtermDir + "/" + e.Name)}
			if e.Name == "harness.md" {
				v.Longterm.HarnessMd = &fi
			} else {
				v.Longterm.Files = append(v.Longterm.Files, fi)
			}
		}
	}
	if entries, err := skill.LoadDir(context.Background(), m.Hub.Fsys, hooks.SkillsDir); err == nil {
		disabled := m.Hub.SettingsSnapshot().DisabledSkills
		for _, s := range entries {
			id := hooks.SkillDirOf(s.Path)
			v.Skills.Items = append(v.Skills.Items, SkillEntryView{
				ID:      id,
				Name:    s.Name,
				Desc:    s.Description,
				Enabled: !slices.Contains(disabled, id),
			})
		}
	}
	if v.Longterm.Files == nil {
		v.Longterm.Files = []FileInfoView{}
	}
	if v.Skills.Items == nil {
		v.Skills.Items = []SkillEntryView{}
	}
	return v
}

func fileMtime(path string) string {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime().Format("2006-01-02 15:04")
	}
	return ""
}

/* GetMemory 返回 harness.md 内容。 */
func (m *MemoryService) GetMemory() string {
	data, _ := m.Hub.Fsys.Read(context.Background(), hooks.HarnessMd)
	return string(data)
}

/* SaveMemory 写入 harness.md（下一轮对话即注入 system）。 */
func (m *MemoryService) SaveMemory(content string) error {
	return m.Hub.Fsys.Write(context.Background(), hooks.HarnessMd, []byte(content))
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
func (m *MemoryService) ToggleSkill(id string, enabled bool) error {
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
	if err := domain.SaveSettings(m.Hub.Fsys, st); err != nil {
		return err
	}
	m.Hub.ApplySettings(st)
	return nil
}

/* DeleteSkill 删除技能目录（连同 scripts 等子资源），并清理禁用名单残留。 */
func (m *MemoryService) DeleteSkill(id string) error {
	if !validSkillID(id) {
		return fmt.Errorf("非法技能名 %q", id)
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
		if err := domain.SaveSettings(m.Hub.Fsys, st); err != nil {
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
func (m *MemoryService) CreateSkill(zipData []byte) error {
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
	ctx := context.Background()
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
