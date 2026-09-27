/*
Package config 的日志落盘：进程日志写 <数据目录>/logs/core-YYYYMMDD.log
（按天滚动，保留 7 天），与 stderr 双写——dev 终端可见；打包版 Electron
是 GUI 进程无控制台，文件是唯一出口。main 起始处把标准 log、gin、
modeldump 的输出统一接管到 OpenLogWriter 返回的 writer；换代切数据目录
时经 FollowDataDir 跟随（multiWriter 引用同一实例，接管点无需重设）。
*/
package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/* logKeepDays 是日志文件保留天数。 */
const logKeepDays = 7

/* logWriter 是按天滚动的日志文件 writer。 */
type logWriter struct {
	mu   sync.Mutex
	dir  string
	day  string
	file *os.File
}

var stdLog = &logWriter{}

/*
OpenLogWriter 初始化日志落盘并返回接管 writer。失败不致命（磁盘异常时
退化 stderr-only，后续写入会重试开文件）。
*/
func OpenLogWriter(dataDir string) io.Writer {
	stdLog.mu.Lock()
	_ = stdLog.openLocked(dataDir)
	stdLog.mu.Unlock()
	pruneLogs(dataDir)
	// 文件必须在前：MultiWriter 遇错短路，GUI 进程的 stderr 是无效句柄，
	// 放前面会挡住文件写入
	return io.MultiWriter(stdLog, os.Stderr)
}

/* FollowDataDir 换代切数据目录后让日志跟随新目录。 */
func FollowDataDir(dataDir string) {
	stdLog.mu.Lock()
	defer stdLog.mu.Unlock()
	if dataDir == stdLog.dir {
		return
	}
	_ = stdLog.openLocked(dataDir)
}

func (w *logWriter) Write(p []byte) (int, error) {
	day := time.Now().Format("20060102")
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || day != w.day {
		if err := w.openLocked(w.dir); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

/* openLocked 打开（或跨天/换目录时切换）日志文件；caller 持锁。 */
func (w *logWriter) openLocked(dir string) error {
	if dir == "" {
		return nil // OpenLogWriter 之前的极早期输出只走 stderr
	}
	day := time.Now().Format("20060102")
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(logDir, "core-"+day+".log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if w.file != nil {
		_ = w.file.Close()
	}
	w.dir, w.day, w.file = dir, day, f
	return nil
}

/* pruneLogs 清理过期日志（按文件名日期，只认 core-YYYYMMDD.log）。 */
func pruneLogs(dataDir string) {
	entries, err := os.ReadDir(filepath.Join(dataDir, "logs"))
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -logKeepDays).Format("20060102")
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "core-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, "core-"), ".log")
		if len(day) == 8 && day < cutoff {
			_ = os.Remove(filepath.Join(dataDir, "logs", name))
		}
	}
}
