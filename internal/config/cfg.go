/*
Package config 管理 ezharness 的结构配置（应用根 ezharness.json）：
端口与数据目录——只描述 harness 本质结构，不含任何配置记录。

模型、行为设置、mcp、session、记忆等配置记录与数据全部存放于
数据目录（models.json / settings.json / mcp.json / memory.md /
sessions/），结构随数据建模独立演进。

应用根 = EZHARNESS_ROOT 环境变量 > exe 所在目录（dev 时用前者指向
项目目录）。数据目录 dataDir 启动即 chdir，ezloop 各 hook 的相对
路径存储自动落入。文件不存在时自动创建默认（零配置可启动）。
*/
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

/* DefaultPort 是缺省监听端口。 */
const DefaultPort = 5260

/* Config 是 ezharness 启动配置（结构配置）。 */
type Config struct {
	Port    int
	DataDir string // 绝对路径，进程 cwd 即此
}

/* appFile 是 ezharness.json 的落盘结构。 */
type appFile struct {
	Port    int    `json:"port"`
	DataDir string `json:"dataDir"`
}

var (
	rootOnce sync.Once
	rootAbs  string
	fileMu   sync.Mutex
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

/* Load 读取 ezharness.json；不存在则创建默认文件，损坏则备份后重建。 */
func Load() (Config, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	return loadLocked()
}

func loadLocked() (Config, error) {
	path := filepath.Join(Root(), "ezharness.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		c := Config{Port: DefaultPort, DataDir: ResolveDataDir("data")}
		if err := saveLocked(c); err != nil {
			return c, err
		}
		return c, nil
	}
	if err != nil {
		return Config{}, err
	}
	var af appFile
	if err := json.Unmarshal(data, &af); err != nil {
		fmt.Printf("ezharness.json 损坏（%v），已备份为 ezharness.json.bak 并重建默认\n", err)
		_ = os.Rename(path, path+".bak")
		c := Config{Port: DefaultPort, DataDir: ResolveDataDir("data")}
		if err := saveLocked(c); err != nil {
			return c, err
		}
		return c, nil
	}
	port := af.Port
	if port <= 0 {
		port = DefaultPort
	}
	return Config{Port: port, DataDir: ResolveDataDir(af.DataDir)}, nil
}

/* Save 持久化结构配置到应用根的 ezharness.json。 */
func Save(c Config) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	return saveLocked(c)
}

func saveLocked(c Config) error {
	data, err := json.MarshalIndent(appFile{Port: c.Port, DataDir: c.DataDir}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Root(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Root(), "ezharness.json"), data, 0o644)
}

/* ResolveDataDir 把 dataDir 配置解析为绝对路径：空 → 应用根下 data/，
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
