/*
图片识别工具（image_recognize）：设置·模型·图片识别槽启用后装配。
用户发送的图片附件保存在工作目录 tmp/（read_file 按路径读取；主模型
多模态时自动进上下文），本工具让无视觉的模型按路径识别任意图片
（含用户落盘的、终端/脚本产物）。
依赖倒置：tools 只依赖 RecognizeIO 接口，service 层用图片识别槽的
模型实现（service 已 import tools，反向引用会循环）。
*/
package tools

import (
	"context"
	"encoding/json"

	"github.com/xuanlv2002/ezloop/types"
)

/* ImageRecognizeTool 是图片识别工具的注册名。 */
const ImageRecognizeTool = "image_recognize"

/* RecognizeIO 是图片识别能力面（service 层实现：图片识别槽模型调用）。 */
type RecognizeIO interface {
	RecognizeImage(ctx context.Context, path, question string) (string, error)
}

/* ImageRecognize 返回图片识别工具集。 */
func ImageRecognize(io RecognizeIO) []types.Tool {
	return []types.Tool{recognizeTool{io}}
}

type recognizeTool struct{ io RecognizeIO }

func (recognizeTool) Name() string { return ImageRecognizeTool }
func (recognizeTool) Description() string {
	return "识别一张图片文件并返回详细文字描述（由图片识别模型驱动）。适用于以文件形式存在的图片：" +
		"本机任意路径的图片、终端/脚本产物、用户上传附件（工作目录 tmp/，read_file 读不到图片内容时可改用本工具）。" +
		"可用 question 参数指定识别侧重点（如逐字转录文字、还原页面布局与配色），缺省为通用描述。"
}

func (recognizeTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "图片文件的完整绝对路径（png/jpg/webp/gif）"},
			"question": {"type": "string", "description": "识别侧重点（可选）：想从图片获得什么，如\"逐字转录全部文字\"、\"还原页面布局与配色\"；缺省为通用描述"}
		},
		"required": ["path"]
	}`)
}

func (t recognizeTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Path     string `json:"path"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	return t.io.RecognizeImage(ctx, a.Path, a.Question)
}
