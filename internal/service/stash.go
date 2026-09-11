/*
stash 是附件暂存：拖入/粘贴/画板产物在进入输入框时就落盘工作目录
tmp/ 拿真身路径（"拖入即暂存"），发送时只传路径引用——chip 立即可
点开编辑（图片进画板、文本进文件页写回真身），base64 链路不复存在。
*/
package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"ezharness/internal/domain"
)

/* attSeq 是同秒内附件落盘的序号（防重名覆盖）。 */
var attSeq atomic.Int64

/* StashInput 是一次暂存的单文件输入（原始名 + 原始内容）。 */
type StashInput struct {
	Name string
	Data []byte
}

/*
	StashFiles 把文件写入 <工作目录>/tmp/，返回绝对路径列表（正斜杠

风格，与 <reference_file> 记录、read_file 的路径约定一致）。
*/
func StashFiles(ctx context.Context, hub *domain.Hub, files []StashInput) ([]string, error) {
	dir := filepath.ToSlash(filepath.Join(
		ResolveWorkDir(hub.SettingsSnapshot().WorkDir), "tmp"))
	seq := time.Now().Format("20060102-150405")
	paths := make([]string, 0, len(files))
	for _, f := range files {
		name := fmt.Sprintf("%s/att-%s-%d-%s", dir, seq,
			attSeq.Add(1), sanitizeName(f.Name))
		if err := hub.Fsys.Write(ctx, name, f.Data); err != nil {
			return nil, fmt.Errorf("附件 %s 落盘失败: %w", f.Name, err)
		}
		paths = append(paths, name)
	}
	return paths, nil
}

/*
	sanitizeName 净化原始文件名为安全的落盘名（去路径分隔符与 Windows

非法字符，截断超长名）。
*/
func sanitizeName(name string) string {
	name = filepath.Base(filepath.FromSlash(name))
	var b strings.Builder
	for _, r := range name {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			r = '_'
		}
		b.WriteRune(r)
	}
	out := b.String()
	if n := len(out); n > 80 {
		dot := strings.LastIndex(out, ".")
		if dot > 0 && n-dot <= 12 { // 保留短扩展名
			out = out[:72] + "…" + out[dot:]
		} else {
			out = out[:80]
		}
	}
	if out == "" || out == "." {
		out = "file"
	}
	return out
}
