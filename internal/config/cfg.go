/*
Package cfg 加载 ezharness 运行配置（.env + 环境变量）。
环境变量优先于 .env；缺省值与 ezloop examples/chat 对齐。
*/
package config

import (
	"fmt"
	"os"
	"strings"
)

/* Config 是 ezharness 启动配置。 */
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	Addr    string
}

/* Load 读取 .env（已设置的环境变量不覆盖）并构造 Config。 */
func Load() (Config, error) {
	loadDotEnv()
	c := Config{
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		BaseURL: env("OPENAI_BASE_URL", "https://api.siliconflow.cn/v1"),
		Model:   env("EZLOOP_MODEL", "deepseek-ai/DeepSeek-V3.2"),
		Addr:    env("EZHARNESS_ADDR", ":5260"),
	}
	if c.APIKey == "" {
		return c, fmt.Errorf("请先配置 OPENAI_API_KEY（复制 .env.example 为 .env 填入，或设置环境变量）")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k == "" || os.Getenv(k) != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
}
