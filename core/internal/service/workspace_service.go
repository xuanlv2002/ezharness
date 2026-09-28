/*
WorkspaceService：工作目录文件的读写用例（附件 chips 预览源、file://
编辑器保存、画板二进制写回）。文件系统操作归本层——controller 只做
绑定、校验与响应。
*/
package service

import (
	"context"
	"os"
	"path/filepath"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
)

/* WorkspaceService 工作目录文件服务（读预览 + 文本/二进制写保存）。 */
type WorkspaceService struct {
	Fsys osfs.OS
	Hub  *domain.Hub
}

/* NewWorkspaceService 构造（Hub 供暂存用例取工作目录）。 */
func NewWorkspaceService(fsys osfs.OS, hub *domain.Hub) *WorkspaceService {
	return &WorkspaceService{Fsys: fsys, Hub: hub}
}

/* OpenFile 打开文件并取属性（预览/HEAD 通道：调用方负责关闭与写出）。 */
func (w *WorkspaceService) OpenFile(abs string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f.Close()
		return nil, nil, os.ErrInvalid
	}
	return f, info, nil
}

/* StatTarget 检查写回目标存在且是文件（画板原地保存的前提）。 */
func (w *WorkspaceService) StatTarget(abs string) error {
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return os.ErrInvalid
	}
	return nil
}

/* WriteText 写文本文件（编辑器保存通道，原子写）。 */
func (w *WorkspaceService) WriteText(ctx context.Context, abs, content string) error {
	return w.Fsys.Write(ctx, abs, []byte(content))
}

/* WriteBin 写二进制文件（画板图片原地写回，原子写）。 */
func (w *WorkspaceService) WriteBin(ctx context.Context, abs string, data []byte) error {
	return w.Fsys.Write(ctx, abs, data)
}

/* Path 绝对化前端提交的路径（正斜杠风格 → 本机绝对路径）。 */
func (w *WorkspaceService) Path(p string) (string, error) {
	return filepath.Abs(filepath.FromSlash(p))
}
