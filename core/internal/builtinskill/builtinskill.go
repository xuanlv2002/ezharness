/*
Package builtinskill 内嵌随应用分发的技能（操作手册类，教模型完成
应用自身的管理操作：配 MCP、装技能等），不占用户技能目录、升级即新。
buildSystemBase 把它与用户技能（memory/skills/）合并进 <skills> 清单；
目录名是身份，与用户技能共用 DisabledSkills 禁用名单。
*/
package builtinskill

import (
	"embed"
	"sync"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
)

//go:embed skills
var embedded embed.FS

var list = sync.OnceValue(func() []skill.Skill {
	entries, err := embedded.ReadDir("skills")
	if err != nil {
		return nil
	}
	var out []skill.Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, rerr := embedded.ReadFile("skills/" + e.Name() + "/" + skill.SkillFile)
		if rerr != nil {
			continue
		}
		out = append(out, skill.ParseSkill(e.Name(), data))
	}
	return out
})

/* Skills 返回内建技能（skills/<目录>/SKILL.md，无 SKILL.md 的目录跳过）。 */
func Skills() []skill.Skill { return list() }

/* IsBuiltin 报告技能目录名是否属于内建技能。 */
func IsBuiltin(dir string) bool {
	for _, s := range list() {
		if skill.DirOf(s.Path) == dir {
			return true
		}
	}
	return false
}
