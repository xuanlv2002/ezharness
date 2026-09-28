/* 记忆树目录布局、入口模板与初始化（注入走 buildSystemBase 的 <memory> 段）。 */
package hooks

import (
	"context"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"
)

/* 记忆体系目录布局（工作目录相对）。 */
const (
	MemoryDir   = "memory"
	LongtermDir = "memory/longterm"
	SkillsDir   = "memory/skills"
	HarnessMd   = LongtermDir + "/harness.md" // 记忆入口：树说明 + 读写纪律（常驻）
	UserMd      = LongtermDir + "/user.md"    // 用户个人信息（常驻）
	SoulMd      = LongtermDir + "/soul.md"    // agent 工作习惯（常驻）
	ProjectMd   = LongtermDir + "/project.md" // 项目记忆索引（常驻）
)

/* SkillDirOf 从 SKILL.md 路径提取技能目录名（技能的稳定身份）。 */
func SkillDirOf(path string) string {
	return skill.DirOf(path)
}

/* DefaultHarnessMd 是记忆入口文件的初始模板（缺失时落盘，此后归 agent 维护）。 */
const DefaultHarnessMd = `# ezharness 长期记忆入口

记忆树（四个文件都在本文件同目录；各自绝对路径见 system <memory> 段的段标题，
文件工具一律用其给出的绝对路径）：
- user.md — 用户个人信息：偏好与习惯、背景、工作环境、约定（归档时系统自动沉淀）
- soul.md — agent 自己的工作习惯：本环境下顺手的做法、工具与验证方式偏好（归档时系统自动沉淀）
- project.md — 项目记忆索引：一行一个项目（- 项目名 — 项目描述 | 记忆: 记忆文件绝对地址）
- project-<名>.md — 单个项目的记忆（背景、约定、进度），按需读取

读写纪律：
- user/soul/项目索引随上下文常驻；project-<名>.md 相关时才 read_file，不主动全读
- 对话中出现值得长期保留的用户信息或工作习惯，直接编辑 user.md/soul.md，无需声明
- 项目记忆：判断值得跨会话保留时写 project-<名>.md（同目录），并在 project.md 加/改索引行；项目结束或信息过时及时更新
- 更新一律覆盖式：读旧全文 → 融合新信息 → 写新全文；宁精勿杂，过时即删
`

/* EnsureHarnessMd 确保记忆入口文件存在（缺失写默认模板），返回当前内容。 */
func EnsureHarnessMd(ctx context.Context, fsys fs.FileSystem) string {
	if data, err := fsys.Read(ctx, HarnessMd); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return string(data)
	}
	_ = fsys.Write(ctx, HarnessMd, []byte(DefaultHarnessMd))
	return DefaultHarnessMd
}
