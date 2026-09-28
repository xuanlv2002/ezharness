/*
Package osfs 提供无沙箱的全权限文件系统：ezharness 在哪里启动，
就能操作整台设备（任意盘符/任意目录）。

不委托 fs.NewLocal(卷根)：其 resolve 的 rootAbs+Separator 前缀检查
在根挂载（"/" 或 "C:\"）下会误杀所有路径（前缀变成 "//"），
故此处直连 os 实现，语义与 fs.Local 对齐。
*/
package osfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ezfs "github.com/xuanlv2002/ezloop/ext/fs"
)

var _ ezfs.FileSystem = OS{}

/* OS 是全权限文件系统，零状态可并行使用。 */
type OS struct{}

/* abs1 把正斜杠风格路径解析为本机绝对路径（相对路径按进程 cwd）。 */
func abs1(p string) string {
	a, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		return filepath.Clean(filepath.FromSlash(p))
	}
	return a
}

/* Read 读文件。 */
func (o OS) Read(_ context.Context, p string) ([]byte, error) {
	return os.ReadFile(abs1(p))
}

/* Write 原子写文件（tmp+rename，自动建目录）：中途崩溃不留半文件。 */
func (o OS) Write(_ context.Context, p string, data []byte) error {
	target := abs1(p)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

/* Append 追加写（自动建目录）：jsonl 档案的只增不改写入通道。 */
func (o OS) Append(_ context.Context, p string, data []byte) error {
	target := abs1(p)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

/* List 列目录。 */
func (o OS) List(_ context.Context, dir string) ([]ezfs.Entry, error) {
	items, err := os.ReadDir(abs1(dir))
	if err != nil {
		return nil, err
	}
	out := make([]ezfs.Entry, 0, len(items))
	for _, it := range items {
		info, ierr := it.Info()
		if ierr != nil {
			continue
		}
		out = append(out, ezfs.Entry{Name: it.Name(), Size: info.Size(), IsDir: it.IsDir()})
	}
	return out, nil
}

/* Edit 单文件查找替换：全部命中都替换，返回次数。 */
func (o OS) Edit(ctx context.Context, p, oldText, newText string) (int, error) {
	data, err := o.Read(ctx, p)
	if err != nil {
		return 0, err
	}
	if oldText == "" {
		return 0, fmt.Errorf("osfs: empty old_text")
	}
	n := strings.Count(string(data), oldText)
	if n == 0 {
		return 0, fmt.Errorf("osfs: old_text not found in %s", p)
	}
	return n, o.Write(ctx, p, []byte(strings.ReplaceAll(string(data), oldText, newText)))
}
