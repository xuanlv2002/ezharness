/*
Package config 加载 ezharness 运行配置。

两层配置：
  - 应用配置 ezharness.json（应用根目录）：端口 port、数据目录 data_dir。
    应用根 = EZHARNESS_ROOT 环境变量 > exe 所在目录（dev 时用前者指向项目目录）。
  - 模型配置 .env / 环境变量：APIKey/BaseURL/Model。

数据目录 dataDir 是所有运行时文件的根（settings.json、topics.json、
memory.md、mcp.json、sessions/），启动即 chdir 到此，ezloop 各 hook
的相对路径存储自动落入。
*/
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

/* DefaultPort 是缺省监听端口。 */
const DefaultPort = 5260

/* Config 是 ezharness 启动配置（应用配置 + 模型配置的合并视图）。 */
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	Port    int
	DataDir string // 绝对路径，进程 cwd 即此
}

/* appFile 是 ezharness.json 的落盘结构。 */
type appFile struct {
	Port    int    `json:"port"`
	DataDir string `json:"data_dir"`
}

var (
	rootOnce sync.Once
	rootAbs  string
)

/* Root 返回应用根目录（配置文件所在）。首次调用即锚定为绝对路径
（在进程 chdir 到数据目录之前），此后不受 cwd 变化影响。 */
func Root() string {
	rootOnce.Do(func() {
		v := os.Getenv("EZHARNESS_ROOT")
		if v == "" {
			exe, err := os.Executable()
			if err != nil {
				v = "."
			} else {
				v = filepath.Dir(exe)
			}
		}
		if abs, err := filepath.Abs(v); err == nil {
			rootAbs = abs
		} else {
			rootAbs = v
		}
	})
	return rootAbs
}

/* Load 读取 .env 与 ezharness.json 并构造 Config。环境变量优先于文件。 */
func Load() (Config, error) {
	loadDotEnv()
	af := appFile{}
	if data, err := os.ReadFile(filepath.Join(Root(), "ezharness.json")); err == nil {
		_ = json.Unmarshal(data, &af)
	}
	c := Config{
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		BaseURL: env("OPENAI_BASE_URL", "https://api.siliconflow.cn/v1"),
		Model:   env("EZLOOP_MODEL", "deepseek-ai/DeepSeek-V3.2"),
		Port:    envInt("EZHARNESS_PORT", orDefault(af.Port, DefaultPort)),
		DataDir: ResolveDataDir(env("EZHARNESS_DATA_DIR", af.DataDir)),
	}
	if c.APIKey == "" {
		return c, fmt.Errorf("请先配置 OPENAI_API_KEY（复制 .env.example 为 .env 填入，或设置环境变量）")
	}
	return c, nil
}

/* SaveApp 持久化应用配置到应用根的 ezharness.json。 */
func SaveApp(port int, dataDir string) error {
	data, err := json.MarshalIndent(appFile{Port: port, DataDir: dataDir}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Root(), "ezharness.json"), data, 0o644)
}

/* ResolveDataDir 把 data_dir 配置解析为绝对路径：空 → 应用根下 data/，
相对 → 相对应用根，绝对 → 原样。 */
func ResolveDataDir(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return absJoin(Root(), "data")
	}
	if filepath.IsAbs(spec) {
		return filepath.Clean(spec)
	}
	return absJoin(Root(), spec)
}

func absJoin(base, rel string) string {
	abs, err := filepath.Abs(filepath.Join(base, rel))
	if err != nil {
		return filepath.Join(base, rel)
	}
	return abs
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func loadDotEnv() {
	data, err := os.ReadFile(filepath.Join(Root(), ".env"))
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
