/*
	WorkspaceController：工作目录文件预览与文本保存（附件 chips 缩略图、

文件查看源、supper_url file:// 编辑器）。
*/
package controller

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/service"
)

/* WorkspaceController 工作目录文件服务（读预览 + 文本写保存）。 */
type WorkspaceController struct {
	Hub *domain.Hub
}

/*
File GET /api/workspace/file?path=<绝对路径>：输出工作目录内的文件
（inline 预览）。仅放行工作目录内路径——防路径穿越读取数据文件
（models.json/sessions/ 等在数据目录，不在工作目录内）。
*/
func (c *WorkspaceController) File(g *gin.Context) {
	workDir := service.ResolveWorkDir(c.Hub.SettingsSnapshot().WorkDir)
	abs, err := filepath.Abs(filepath.FromSlash(g.Query("path")))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}
	rel, err := filepath.Rel(workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		g.JSON(http.StatusForbidden, gin.H{"error": "path outside workspace"})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		g.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(abs))
	if ct == "" {
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		ct = http.DetectContentType(buf[:n])
		if _, err := f.Seek(0, 0); err != nil {
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	g.Header("Content-Type", ct)
	g.Header("Content-Disposition", "inline")
	/* 图片等被画板原地写回：允许缓存但强制 revalidate（未变 304 几乎
	零开销，写回后 mtime 变必拿新图）——防启发式缓存直出旧图 */
	g.Header("Cache-Control", "no-cache")
	/* 真实 mtime 作 modtime：Last-Modified/304 语义正确，前端文件页
	轮询 HEAD 对比即可感知磁盘变更（AI write_file 等） */
	http.ServeContent(g.Writer, g.Request, filepath.Base(abs), info.ModTime(), f)
}

/* maxSaveContent 保存内容上限（与前端 file:// 编辑器预检同值）。 */
const maxSaveContent = 2 << 20

/*
Save POST /api/workspace/save {path, content}：写工作目录内文本文件
（supper_url file:// 编辑器的保存通道）。沙箱与 File 逐项对齐——
沙箱外路径 403，防借端点改写数据目录/系统文件。后端不校验后缀
（门禁在前端 chip 入口），大小上限 2MB。
*/
func (c *WorkspaceController) Save(g *gin.Context) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Path == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if len(req.Content) > maxSaveContent {
		g.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "content too large"})
		return
	}
	workDir := service.ResolveWorkDir(c.Hub.SettingsSnapshot().WorkDir)
	abs, err := filepath.Abs(filepath.FromSlash(req.Path))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}
	rel, err := filepath.Rel(workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		g.JSON(http.StatusForbidden, gin.H{"error": "path outside workspace"})
		return
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		g.JSON(http.StatusBadRequest, gin.H{"error": "is a directory"})
		return
	}
	if err := c.Hub.Fsys.Write(g.Request.Context(), abs, []byte(req.Content)); err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* maxStashSize 单文件暂存上限（20MB）；二进制写回同限。 */
const maxStashSize = 20 << 20

/*
Stash POST /api/workspace/stash（multipart，字段名 file 可多个）：把
拖入/粘贴/画板产物写入工作目录 tmp/ 暂存区，返回落盘路径——"拖入即
暂存"：输入框附件立刻有真身路径（chip 可点开编辑），发送时只传路径引用。
*/
func (c *WorkspaceController) Stash(g *gin.Context) {
	form, err := g.MultipartForm()
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart"})
		return
	}
	heads := form.File["file"]
	if len(heads) == 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": "no file"})
		return
	}
	for _, fh := range heads {
		if fh.Size > maxStashSize {
			g.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("文件 %s 过大（不超过 20MB）", fh.Filename)})
			return
		}
	}
	inputs, err := readMultipart(heads)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	paths, err := service.StashFiles(g.Request.Context(), c.Hub, inputs)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "files": paths})
}

/*
SaveBin POST /api/workspace/save-bin（multipart：path + file）：二进制
写回工作目录内已有文件（画板编辑图片的原地保存通道——一切皆资源，
编辑即写回真身）。沙箱与 Save 一致，大小上限 20MB。
*/
func (c *WorkspaceController) SaveBin(g *gin.Context) {
	path := g.PostForm("path")
	if path == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	fh, err := g.FormFile("file")
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	if fh.Size > maxStashSize {
		g.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "文件过大（不超过 20MB）"})
		return
	}
	workDir := service.ResolveWorkDir(c.Hub.SettingsSnapshot().WorkDir)
	abs, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}
	rel, err := filepath.Rel(workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		g.JSON(http.StatusForbidden, gin.H{"error": "path outside workspace"})
		return
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		g.JSON(http.StatusBadRequest, gin.H{"error": "target not found"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := c.Hub.Fsys.Write(g.Request.Context(), abs, data); err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* readMultipart 把 multipart 文件头读成暂存输入（大小预检在调用方）。 */
func readMultipart(heads []*multipart.FileHeader) ([]service.StashInput, error) {
	inputs := make([]service.StashInput, 0, len(heads))
	for _, fh := range heads {
		f, err := fh.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(f)
		f.Close()
		if readErr != nil {
			return nil, readErr
		}
		inputs = append(inputs, service.StashInput{Name: fh.Filename, Data: data})
	}
	return inputs, nil
}
