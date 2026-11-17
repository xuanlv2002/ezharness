/*
media 多媒体生成工具：图片/语音/视频统一一个工具，action 分派（生成
走 image/audio/video，任务查询走 status）。对应类别在 media.json
配置并启用后可用——未配置/已关闭时工具仍然存在，返回「当前无 xxx
能力」让模型如实告知用户，而不是抛工具错误。实现是 service.MediaService
的薄壳（结构类型接口注入，避免包循环）；配额/落盘/错误文案都在
service 层，与 OpenAPI 同一入口。

贵价模型不该阻塞对话循环：工具提交后短等待（toolWait），未完成返回
任务 id，模型持 id 用 action=status 轮询（隔开时间，不密轮询）。
*/
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/domain"
)

/* MediaJobs 是多媒体模块对工具层暴露的能力（*service.MediaService 结构实现）。 */
type MediaJobs interface {
	SubmitImage(ctx context.Context, model, prompt, negative, size string, batch int, seed int64) (*domain.MediaJob, error)
	SubmitSpeech(ctx context.Context, model, input, voice, format string, speed float64) (*domain.MediaJob, error)
	SubmitVideo(ctx context.Context, model, prompt, firstFrame, lastFrame string, duration int, resolution, ratio string) (*domain.MediaJob, error)
	WaitJob(id string, d time.Duration) (*domain.MediaJob, bool)
	Job(id string) (*domain.MediaJob, bool)
	Jobs(limit int) []domain.MediaJob
	HasCapability(kind string) bool
}

/*
	toolWait 工具内短等待：快任务（图/音）通常十几秒内完成直接拿产物；

视频几乎必然超时走任务 id 路径——这正是异步化的意义。
*/
const toolWait = 15 * time.Second

/* Media 返回统一的媒体工具（一个工具按 action 分派四类动作）。 */
func Media(m MediaJobs) types.Tool { return mediaTool{m} }

type mediaTool struct{ m MediaJobs }

func (mediaTool) Name() string { return "media" }

func (mediaTool) Description() string {
	return "多媒体生成统一入口（图片/语音/视频；贵价：默认每次需用户审批，且有每日配额）。" +
		"action=image/audio/video 提交异步任务；action=status 查询任务（省略 id 列最近任务）。" +
		"提交后工具内部短等待约 15 秒，完成直接返回产物路径；未完成返回任务 id——之后隔 1-2 分钟用 action=status 查一次，不要密轮询。" +
		"先用 system <media> 段确认当前可用的能力：未配置/已关闭的能力调用会返回「当前无…能力」，如实告知用户即可，不要重试或改用别的方式硬凑。" +
		"各 action 的参数、成本与 OpenAPI 用法先 load_skill media 查看。"
}

func (mediaTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":` +
		`{"action":{"type":"string","enum":["image","audio","video","status"],"description":"image=文生图，audio=语音合成，video=文生视频，status=查询任务"},` +
		`"prompt":{"type":"string","description":"image/video 的画面或镜头描述（具体、含风格/构图/光线；video 200 词以内）"},` +
		`"negative_prompt":{"type":"string","description":"image：不想要的元素（可选）"},` +
		`"image_size":{"type":"string","description":"image：分辨率，如 1024x1024（可选默认模型自带）"},` +
		`"batch_size":{"type":"integer","description":"image：张数 1-4（默认 1；张数=费用）"},` +
		`"seed":{"type":"integer","description":"image：随机种子（相同可复现，可选）"},` +
		`"input":{"type":"string","description":"audio：待合成文本（≤5000 字符）"},` +
		`"voice":{"type":"string","description":"audio：音色短名，男 alex/benjamin/charles/david，女 anna/bella/claire/diana（默认 alex）"},` +
		`"format":{"type":"string","description":"audio：输出格式 mp3/wav/opus/pcm（默认 mp3）"},` +
		`"speed":{"type":"number","description":"audio：语速 0.25-4.0（默认 1.0）"},` +
		`"duration":{"type":"integer","description":"video：时长 5-15 秒（按秒计费，默认 5）"},` +
		`"resolution":{"type":"string","description":"video：480p 或 768p（默认 768p；480p 省钱）"},` +
		`"ratio":{"type":"string","description":"video：画面比例，如 16:9 / 9:16 / 1:1（可选）"},` +
		`"first_frame":{"type":"string","description":"video：首帧图（本机路径或 http URL，可选）"},` +
		`"last_frame":{"type":"string","description":"video：尾帧图（本机路径或 http URL，可选）"},` +
		`"id":{"type":"string","description":"status：任务 id（省略=列出最近任务）"}},` +
		`"required":["action"]}`)
}

func (t mediaTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Action     string  `json:"action"`
		Prompt     string  `json:"prompt"`
		Negative   string  `json:"negative_prompt"`
		Size       string  `json:"image_size"`
		Batch      int     `json:"batch_size"`
		Seed       int64   `json:"seed"`
		Input      string  `json:"input"`
		Voice      string  `json:"voice"`
		Format     string  `json:"format"`
		Speed      float64 `json:"speed"`
		Duration   int     `json:"duration"`
		Resolution string  `json:"resolution"`
		Ratio      string  `json:"ratio"`
		FirstFrame string  `json:"first_frame"`
		LastFrame  string  `json:"last_frame"`
		ID         string  `json:"id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(a.Action)) {
	case domain.GenImage:
		if !t.m.HasCapability(domain.GenImage) {
			return noCapability(domain.GenImage), nil
		}
		if strings.TrimSpace(a.Prompt) == "" {
			return "", fmt.Errorf("media: action=image 需要 prompt（画面描述）")
		}
		job, err := t.m.SubmitImage(ctx, "", a.Prompt, a.Negative, a.Size, a.Batch, a.Seed)
		if err != nil {
			return "", err
		}
		final, _ := t.m.WaitJob(job.ID, toolWait)
		return jobResult(final, kindName(domain.GenImage), "图片一般 10-60 秒完成，"), nil
	case domain.GenAudio:
		if !t.m.HasCapability(domain.GenAudio) {
			return noCapability(domain.GenAudio), nil
		}
		if strings.TrimSpace(a.Input) == "" {
			return "", fmt.Errorf("media: action=audio 需要 input（待合成文本）")
		}
		job, err := t.m.SubmitSpeech(ctx, "", a.Input, a.Voice, a.Format, a.Speed)
		if err != nil {
			return "", err
		}
		final, _ := t.m.WaitJob(job.ID, toolWait)
		return jobResult(final, kindName(domain.GenAudio), "合成一般几秒完成，"), nil
	case domain.GenVideo:
		if !t.m.HasCapability(domain.GenVideo) {
			return noCapability(domain.GenVideo), nil
		}
		if strings.TrimSpace(a.Prompt) == "" {
			return "", fmt.Errorf("media: action=video 需要 prompt（镜头脚本）")
		}
		job, err := t.m.SubmitVideo(ctx, "", a.Prompt, a.FirstFrame, a.LastFrame, a.Duration, a.Resolution, a.Ratio)
		if err != nil {
			return "", err
		}
		final, _ := t.m.WaitJob(job.ID, toolWait)
		return jobResult(final, kindName(domain.GenVideo), "视频生成需数分钟，"), nil
	case "status":
		return t.status(a.ID)
	default:
		return "", fmt.Errorf("media: action 须为 image / audio / video / status")
	}
}

/*
	noCapability 是能力缺失的正面答复（正常结果，不是工具错误）：让模型

直接转告用户，并指向配置入口。
*/
func noCapability(kind string) string {
	return fmt.Sprintf("当前无%s能力（对应生成模型未配置或已关闭）。请如实告知用户，可到「多媒体 · 模型与限额」配置并启用；不要重试或改用其他方式硬凑。", kindName(kind))
}

/* status 查询单个任务；无 id 时列最近任务。 */
func (t mediaTool) status(rawID string) (string, error) {
	id := strings.TrimSpace(rawID)
	if id == "" {
		jobs := t.m.Jobs(10)
		if len(jobs) == 0 {
			return "（暂无生成任务）", nil
		}
		var b strings.Builder
		b.WriteString("最近生成任务（新→旧）：\n")
		for _, j := range jobs {
			fmt.Fprintf(&b, "- %s [%s] %s · %s\n", j.ID, j.Kind, statusLabel(j.Status), j.Brief)
		}
		b.WriteString("用 media {\"action\":\"status\",\"id\":\"...\"} 查单个任务的产物/原因。")
		return b.String(), nil
	}
	j, ok := t.m.Job(id)
	if !ok {
		return "", fmt.Errorf("media: 任务 %s 不存在（可能已被清理；不带 id 可列最近任务）", id)
	}
	return jobResult(j, kindName(j.Kind), ""), nil
}

/* jobResult 统一的结果文案：完成给产物路径，未完成给任务 id 与查询方式。 */
func jobResult(j *domain.MediaJob, kindLabel, etaNote string) string {
	switch {
	case j == nil:
		return "任务不存在（可能已被清理）"
	case j.Status == domain.JobFailed:
		msg := j.Err
		if msg == "" {
			msg = "生成失败"
		}
		return fmt.Sprintf("%s生成失败：%s", kindLabel, msg)
	case j.Status == domain.JobDone:
		var b strings.Builder
		if len(j.Files) == 1 {
			fmt.Fprintf(&b, "%s已生成：%s（%s KB）", kindLabel, j.Files[0].Path, sizeKB(j.Files[0].Size))
		} else {
			fmt.Fprintf(&b, "%s已生成 %d 个产物：\n", kindLabel, len(j.Files))
			for _, f := range j.Files {
				fmt.Fprintf(&b, "- %s（%s KB）\n", f.Path, sizeKB(f.Size))
			}
		}
		if j.Err != "" { // 部分成功的备注
			b.WriteString(j.Err + "\n")
		}
		fmt.Fprintf(&b, "（今日配额已用 %d/%d）", j.Quota.Used, j.Quota.Limit)
		return b.String()
	default: // 未完成（超时返回时）
		return fmt.Sprintf("%s任务已提交，仍在生成中（id: %s）。%s不要空转轮询：先做别的或告知用户，隔开时间后再用 media {\"action\":\"status\",\"id\": %q} 查询；完成会拿到产物落盘路径。",
			kindLabel, j.ID, etaNote, j.ID)
	}
}

func sizeKB(n int) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1f", float64(n)/1024/1024) + "M"
	}
	return fmt.Sprintf("%d", n/1024)
}

func statusLabel(st string) string {
	switch st {
	case domain.JobQueued:
		return "排队中"
	case domain.JobRunning:
		return "生成中"
	case domain.JobDone:
		return "已完成"
	default:
		return "失败"
	}
}

func kindName(kind string) string {
	switch kind {
	case domain.GenImage:
		return "图片"
	case domain.GenAudio:
		return "语音"
	default:
		return "视频"
	}
}
