/*
Settings 是用户可改的运行配置（settings.json）：模型/端点/系统提示追加/
话题轮换水位/shell。持久化到工作目录。
*/
package domain

import (
	"context"
	"encoding/json"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* Settings 是可热更的运行配置。 */
type Settings struct {
	Model           string `json:"model"`
	BaseURL         string `json:"baseUrl"`
	SystemExtra     string `json:"systemExtra"`
	RotateThreshold int    `json:"rotateThreshold"` // prompt tokens，<=0 禁用自动轮换
	Shell           string `json:"shell"`           // "" / "auto" / "bash" / "pwsh" / "cmd"
}

/* DefaultSettings 给出出厂值（模型/端点继承自启动配置）。 */
func DefaultSettings(model, baseURL string) Settings {
	return Settings{
		Model:           model,
		BaseURL:         baseURL,
		SystemExtra:     "",
		RotateThreshold: 80000,
		Shell:           "auto",
	}
}

/* LoadSettings 读 settings.json，缺失或空字段回落默认值。 */
func LoadSettings(fsys fs.FileSystem, def Settings) Settings {
	out := def
	data, err := fsys.Read(context.Background(), "settings.json")
	if err != nil {
		return out
	}
	var s Settings
	if json.Unmarshal(data, &s) != nil {
		return out
	}
	if s.Model != "" {
		out.Model = s.Model
	}
	if s.BaseURL != "" {
		out.BaseURL = s.BaseURL
	}
	if s.Shell != "" {
		out.Shell = s.Shell
	}
	out.SystemExtra = s.SystemExtra
	out.RotateThreshold = s.RotateThreshold
	return out
}

/* SaveSettings 落盘设置。 */
func SaveSettings(fsys fs.FileSystem, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(context.Background(), "settings.json", data)
}
