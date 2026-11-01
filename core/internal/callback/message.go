/*
message 定义定时回调投递到会话的 user 消息格式：开头一条
<scheduled_callback> JSON 元数据，换行后是任务正文。前端据此把这条
用户消息渲染成带任务名与触发时间的回调卡；模型把标签后的正文当作
本轮指令。
*/
package callback

import (
	"encoding/json"
	"strings"
	"time"
)

/* MessageTag 是定时回调 user 消息的元数据标签名。 */
const MessageTag = "scheduled_callback"

/* MessageMeta 是标签内的 JSON 元数据（前端特殊展示用）。 */
type MessageMeta struct {
	TaskID string `json:"taskId"`
	Title  string `json:"title"`
	At     string `json:"at"`
}

/* FormatMessage 把任务正文包装成带元数据的 user 消息（时间按本机时区）。 */
func FormatMessage(task Task, body string, at time.Time) string {
	meta, _ := json.Marshal(MessageMeta{
		TaskID: task.ID,
		Title:  task.Title,
		At:     at.Format("2006-01-02 15:04:05"),
	})
	return "<" + MessageTag + ">" + string(meta) + "</" + MessageTag + ">\n" + body
}

/* MessageBody 去掉回调消息的元数据标签，返回正文（非回调消息原样返回）。 */
func MessageBody(content string) string {
	open := "<" + MessageTag + ">"
	if !strings.HasPrefix(content, open) {
		return content
	}
	closeTag := "</" + MessageTag + ">"
	end := strings.Index(content, closeTag)
	if end < 0 {
		return content
	}
	body := content[end+len(closeTag):]
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return strings.TrimSpace(body)
}
