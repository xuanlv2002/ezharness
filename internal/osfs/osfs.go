/*
Package osfs 提供无沙箱的全权限文件系统：ezharness 在哪里启动，
就能操作整台设备（任意盘符/任意目录，exec 开放）。

不委托 fs.NewLocal(卷根)：其 resolve 的 rootAbs+Separator 前缀检查
在根挂载（"/" 或 "C:\"）下会误杀所有路径（前缀变成 "//"），
故此处直连 os 实现，语义与 fs.Local 对齐（含上限与噪声目录跳过）。
*/
package osfs

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	ezfs "github.com/xuanlv2002/ezloop/ext/fs"
)

var (
	_ ezfs.Modifier = OS{}
	_ ezfs.Searcher = OS{}
)

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

/* Write 写文件（自动建目录）。 */
func (o OS) Write(_ context.Context, p string, data []byte) error {
	target := abs1(p)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
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

/* ApplyPatch 原子多文件修改：预检失败不写，应用失败回滚。 */
func (o OS) ApplyPatch(ctx context.Context, ops []ezfs.PatchOp) error {
	if len(ops) == 0 {
		return fmt.Errorf("osfs: empty patch")
	}
	originals := make([][]byte, len(ops))
	for i, op := range ops {
		if op.Path == "" || op.OldText == "" {
			return fmt.Errorf("osfs: op[%d] requires path and old_text", i)
		}
		data, err := o.Read(ctx, op.Path)
		if err != nil {
			return fmt.Errorf("osfs: patch op[%d] %s: %w", i, op.Path, err)
		}
		if !bytes.Contains(data, []byte(op.OldText)) {
			return fmt.Errorf("osfs: patch op[%d] %s: old_text not found", i, op.Path)
		}
		originals[i] = data
	}
	for i, op := range ops {
		replaced := strings.ReplaceAll(string(originals[i]), op.OldText, op.NewText)
		if err := o.Write(ctx, op.Path, []byte(replaced)); err != nil {
			for j := range i {
				_ = o.Write(ctx, ops[j].Path, originals[j])
			}
			return fmt.Errorf("osfs: patch apply %s: %w (rolled back)", op.Path, err)
		}
	}
	return nil
}

/* Grep 按正则搜索文件内容，路径返回绝对正斜杠形式。 */
func (o OS) Grep(ctx context.Context, req ezfs.GrepRequest) ([]ezfs.GrepMatch, error) {
	if req.Pattern == "" {
		return nil, fmt.Errorf("osfs: empty pattern")
	}
	re, err := regexp.Compile(req.Pattern)
	if err != nil {
		return nil, fmt.Errorf("osfs: bad pattern: %w", err)
	}
	var matches []ezfs.GrepMatch
	err = o.walk(ctx, req.Path, func(p string, data []byte) bool {
		if req.Glob != "" && !globMatch(req.Glob, path.Base(p)) {
			return true
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			if len(matches) >= ezfs.MaxGrepResults {
				return false
			}
			if re.MatchString(line) {
				matches = append(matches, ezfs.GrepMatch{
					Path: p, Line: lineNo + 1, Text: strings.TrimRight(line, "\r"),
				})
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

/* Find 按文件名 glob 模式查找（单次遍历限一个起点目录，全盘遍历请分别指定）。 */
func (o OS) Find(ctx context.Context, root, pattern string) ([]string, error) {
	if pattern == "" {
		return nil, fmt.Errorf("osfs: empty pattern")
	}
	var found []string
	err := o.walk(ctx, root, func(p string, _ []byte) bool {
		if len(found) >= ezfs.MaxFindResults {
			return false
		}
		if globMatch(pattern, path.Base(p)) {
			found = append(found, p)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

/*
walk 遍历 root（文件或目录），对每个文件以绝对正斜杠路径调用 fn；
fn 返回 false 时停止。跳过二进制/超大文件与常见噪声目录，
与 fs.Local.walk 语义一致（Grep/Find 每次限单一起点）。
*/
func (o OS) walk(_ context.Context, root string, fn func(p string, data []byte) bool) error {
	abs := abs1(root)
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, ".idea": true, "vendor": true, ".ezloop": true, "sessions": true,
	}
	stop := false
	err := filepath.WalkDir(abs, func(p string, d fs.DirEntry, werr error) error {
		if stop || werr != nil {
			return werr
		}
		if d.IsDir() {
			if p != abs && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > 2<<20 {
			return nil
		}
		data, derr := os.ReadFile(p)
		if derr != nil || bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		if !fn(filepath.ToSlash(p), data) {
			stop = true
		}
		return nil
	})
	if err != nil && stop {
		return nil
	}
	return err
}

func globMatch(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	return err == nil && ok
}
