/*
pinboard 是便签板的存取用例：整块板（待办卡片 + 已完成归档）以 JSON
原样落盘。布局、拖拽、归档与迁移全部在前端，后端只保证原子写与首次
使用的空板默认值。
*/
package service

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"

	"ezharness/core/internal/osfs"
)

/* PinboardService 便签板存取。 */
type PinboardService struct {
	Fsys osfs.OS
}

/* pinboardFile 数据目录下的板文件。 */
const pinboardFile = "pinboard.json"

/* MaxPinboardBytes 板上限（几十张卡片量级，2MB 绰绰有余）。 */
const MaxPinboardBytes = 2 << 20

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
