/*
pinboard 是便签板的存取用例：整块板（待办卡片 + 已完成归档）以 JSON
原样落盘；卡面上的图片本体单独落在 pinboard-images/，板里只存 URL。
布局、拖拽、归档与迁移全部在前端，后端只保证原子写与首次使用的空板
默认值。
*/
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"

	"ezharness/core/internal/osfs"
)

/* PinboardService 便签板存取。 */
type PinboardService struct {
	Fsys osfs.OS
}

/* pinboardFile 数据目录下的板文件。 */
const pinboardFile = "pinboard.json"

/* MaxPinboardBytes 板上限（只存布局/正文/图片 URL，几十张卡片量级 2MB 够用）。 */
const MaxPinboardBytes = 2 << 20

/* PinboardImagesDir 便签图片在数据目录下的子目录（图片本体不进板 JSON）。 */
const PinboardImagesDir = "pinboard-images"

/* MaxPinboardImageBytes 单张便签图片上限（前端压到 1600px WebP，8MB 足够）。 */
const MaxPinboardImageBytes = 8 << 20

/* pinImageName 合法的图片文件名：上传时生成的 img-<32 hex>.<ext>，也挡住目录穿越。 */
var pinImageName = regexp.MustCompile(`^img-[0-9a-f]{32}\.(png|jpg|webp|gif)$`)

/* pinImageExt 按内容嗅探图片格式（不信任请求头）。 */
func pinImageExt(data []byte) (string, bool) {
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	}
	return "", false
}

/* pinImageMime 扩展名 → 响应 Content-Type。 */
func pinImageMime(name string) string {
	switch path.Ext(name) {
	case ".png":
		return "image/png"
	case ".jpg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}
	return "application/octet-stream"
}

/* defaultPinboard 空板形状（v 供前端数据迁移）。 */
const defaultPinboard = `{"v":1,"z":10,"cards":[],"done":[]}`

/* Get 读取整块板；文件不存在返回空板（首次使用不是错误）。 */
func (p *PinboardService) Get(ctx context.Context) ([]byte, error) {
	data, err := p.Fsys.Read(ctx, pinboardFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []byte(defaultPinboard), nil
		}
		return nil, err
	}
	return data, nil
}

/*
Save 整块覆写。只接受带 cards/done 数组的 JSON 对象（形状校验）且不
超上限——板上数据是用户唯一副本，非法体一律拒绝，不落脏。
*/
func (p *PinboardService) Save(ctx context.Context, data []byte) error {
	if len(data) > MaxPinboardBytes {
		return errors.New("便签板数据超过 2MB 上限")
	}
	var probe struct {
		Cards []json.RawMessage `json:"cards"`
		Done  []json.RawMessage `json:"done"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return errors.New("便签板数据不是合法 JSON")
	}
	if probe.Cards == nil || probe.Done == nil {
		return errors.New("便签板数据缺少 cards/done 数组")
	}
	return p.Fsys.Write(ctx, pinboardFile, data)
}

/*
SaveImage 保存一张便签图片：按内容嗅探格式、随机命名后落到数据目录
pinboard-images/，返回可给 <img> 用的同源 URL。文件名随机且不可变，前端
把 URL 存进卡片/归档记录，图片本体不进 pinboard.json。
*/
func (p *PinboardService) SaveImage(ctx context.Context, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("图片为空")
	}
	if len(data) > MaxPinboardImageBytes {
		return "", fmt.Errorf("图片超过 %dMB 上限", MaxPinboardImageBytes>>20)
	}
	ext, ok := pinImageExt(data)
	if !ok {
		return "", errors.New("只支持 PNG/JPEG/WebP/GIF 图片")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	name := "img-" + hex.EncodeToString(buf) + ext
	if err := p.Fsys.Write(ctx, path.Join(PinboardImagesDir, name), data); err != nil {
		return "", err
	}
	return "/api/pinboard/images/" + name, nil
}

/* ReadImage 按名读取便签图片；名字不合法按不存在处理。 */
func (p *PinboardService) ReadImage(ctx context.Context, name string) ([]byte, string, error) {
	if !pinImageName.MatchString(name) {
		return nil, "", fs.ErrNotExist
	}
	data, err := p.Fsys.Read(ctx, path.Join(PinboardImagesDir, name))
	if err != nil {
		return nil, "", err
	}
	return data, pinImageMime(name), nil
}
