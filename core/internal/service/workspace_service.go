/* WorkspaceService：工作目录文件的读写用例。 */
package service

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

/* OpenFile 打开文件并取属性。 */
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

/* FileMeta 是附件卡的轻量元数据（聊天里 file:// 卡片展示类型/页数/大小）。 */
type FileMeta struct {
	Size  int64 `json:"size"`
	Pages int   `json:"pages,omitempty"` // pptx 幻灯片数（其余格式不回）
	Dir   bool  `json:"dir,omitempty"`   // 目录卡：前端画文件夹样式
}

/* pptxSlideRe 只匹配幻灯片正文（排除 _rels 与 slideLayouts）。 */
var pptxSlideRe = regexp.MustCompile(`^ppt/slides/slide\d+\.xml$`)

/*
Meta 取文件轻量元数据：大小 +（pptx）幻灯片数。只读 zip 中央目录的
条目名，不解压任何内容——聊天里的附件卡每次渲染最多请求一次，开销可忽略。
*/
func (w *WorkspaceService) Meta(abs string) (FileMeta, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return FileMeta{}, err
	}
	if info.IsDir() {
		return FileMeta{Dir: true}, nil
	}
	m := FileMeta{Size: info.Size()}
	if strings.EqualFold(filepath.Ext(abs), ".pptx") {
		if pages, err := pptxSlides(abs); err == nil {
			m.Pages = pages
		}
	}
	return m, nil
}

/* pptxSlides 数 pptx 幻灯片（zip 中央目录条目名即可）。 */
func pptxSlides(abs string) (int, error) {
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		if pptxSlideRe.MatchString(f.Name) {
			n++
		}
	}
	return n, nil
}

/* StatTarget 检查写回目标存在且是文件。 */
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

/* Mkdir 新建目录（含父级）；目标已存在时报 os.ErrExist，不静默复用。 */
func (w *WorkspaceService) Mkdir(abs string) error {
	if _, err := os.Stat(abs); err == nil {
		return os.ErrExist
	}
	return os.MkdirAll(abs, 0o755)
}

/* CreateFile 新建空文本文件（父级一并创建）；已存在报 os.ErrExist，不静默覆盖。 */
func (w *WorkspaceService) CreateFile(ctx context.Context, abs string) error {
	if _, err := os.Stat(abs); err == nil {
		return os.ErrExist
	}
	return w.Fsys.Write(ctx, abs, nil)
}

/* WriteBin 写二进制文件（画板图片原地写回，原子写）。 */
func (w *WorkspaceService) WriteBin(ctx context.Context, abs string, data []byte) error {
	return w.Fsys.Write(ctx, abs, data)
}

/* Path 绝对化前端提交的路径。 */
func (w *WorkspaceService) Path(p string) (string, error) {
	return filepath.Abs(filepath.FromSlash(p))
}
