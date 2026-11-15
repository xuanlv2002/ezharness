/*
MediaService 多媒体模块：生成类模型（image 文生图 / audio 语音合成 /
video 视频生成）的配置消费方与异步任务引擎。三类模型按次计费且单价高，
与语言模型（models.json）彻底分离（配置在 media.json 的 MediaConfig，
不进对话模型取值域）。

任务模型：Submit* 校验+占额+入队即返回 MediaJob，实际生成在后台
goroutine 执行（视频是上游异步任务：提交→轮询→下载成片）；调用方
（agent 工具 / OpenAPI）短等待拿结果，未完成持任务 id 轮询
（WaitJob/Job/Jobs）。配额在提交时占用、彻底失败（无产物）退还——
失败的调用不吃当日额度。产物写 <工作目录>/tmp/（与附件暂存同区）。
任务档案持久（mediajobs.json 封顶 200 条）；core 重启时把未完成任务
标记失败并退还配额（上游任务 id 已随进程丢失，v1 不续查）。
*/
package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ezharness/core/internal/domain"
)

/* 生成类调用的护栏：请求/轮询/下载各自超时（视频整体上限最长）。 */
const (
	imageTimeout    = 3 * time.Minute
	speechTimeout   = 2 * time.Minute
	videoWaitMax    = 10 * time.Minute
	videoPollStep   = 5 * time.Second
	genInputMax     = 5000 // TTS 输入字符上限（按 UTF-8 字节计费的闸）
	jobsArchiveFile = "mediajobs.json"
	jobsKeep        = 200 // 任务档案封顶（终态任务超出丢最旧；活跃任务不丢）
)

const quotaFile = "mediaquota.json"

/* ErrQuotaExceeded 是配额超限的哨兵错误（OpenAPI 面据此回 429；文案主体在包裹错误里）。 */
var ErrQuotaExceeded = errors.New("gen quota exceeded")

/* MediaService 多媒体生成用例（配置、配额、任务档案的单一家园）。 */
type MediaService struct {
	Hub  *domain.Hub
	http *http.Client

	quotaMu sync.Mutex
	quota   genQuotaFile

	jobsMu sync.Mutex
	jobs   []*domain.MediaJob // 提交序（旧→新）

	genSeq atomic.Int64 // 同秒产物序号（防重名覆盖）
}

/* genQuotaFile 是配额计数落盘形态（数据目录 mediaquota.json）。 */
type genQuotaFile struct {
	Day   string `json:"day"` // YYYY-MM-DD（本机时区）
	Image int    `json:"image"`
	Audio int    `json:"audio"`
	Video int    `json:"video"`
}

type jobsArchive struct {
	Jobs []domain.MediaJob `json:"jobs"`
}

/*
	NewMediaService 构造：配额与任务档案从盘上恢复；上次进程未完成的

任务标记失败并退还配额（goroutine 已不存在）。
*/
func NewMediaService(hub *domain.Hub) *MediaService {
	s := &MediaService{Hub: hub, http: &http.Client{}}
	if data, err := hub.Fsys.Read(context.Background(), quotaFile); err == nil {
		_ = json.Unmarshal(data, &s.quota)
	}
	if data, err := hub.Fsys.Read(context.Background(), jobsArchiveFile); err == nil {
		var f jobsArchive
		if json.Unmarshal(data, &f) == nil {
			for i := range f.Jobs {
				j := f.Jobs[i]
				if j.Status == domain.JobQueued || j.Status == domain.JobRunning {
					j.Status = domain.JobFailed
					j.Err = "core 重启，任务中断（未扣当日配额；如仍需要请重新提交）"
					j.DoneAt = time.Now().UnixMilli()
					s.refund(j.Kind, j.Units)
				}
				s.jobs = append(s.jobs, &j)
			}
		}
	}
	return s
}

/* ── 配额 ── */

/* quotaLimit 取该类别的当日限额（设置页可改，0=默认）。 */
func (s *MediaService) quotaLimit(kind string) int {
	st := s.Hub.SettingsSnapshot()
	switch kind {
	case domain.GenImage:
		if st.GenQuotaImage > 0 {
			return st.GenQuotaImage
		}
		return domain.DefaultGenQuotaImage
	case domain.GenAudio:
		if st.GenQuotaAudio > 0 {
			return st.GenQuotaAudio
		}
		return domain.DefaultGenQuotaAudio
	default:
		if st.GenQuotaVideo > 0 {
			return st.GenQuotaVideo
		}
		return domain.DefaultGenQuotaVideo
	}
}

func (s *MediaService) quotaUsed(kind string) int {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	s.rollDayLocked()
	switch kind {
	case domain.GenImage:
		return s.quota.Image
	case domain.GenAudio:
		return s.quota.Audio
	default:
		return s.quota.Video
	}
}

/* QuotaOf 返回配额视图（任务与 OpenAPI 响应附带，调用方见余量）。 */
func (s *MediaService) QuotaOf(kind string) domain.GenQuota {
	return domain.GenQuota{Kind: kind, Used: s.quotaUsed(kind), Limit: s.quotaLimit(kind)}
}

/* HasCapability 报告某类生成能力当前是否可用（有启用且未关闭的模型）。 */
func (s *MediaService) HasCapability(kind string) bool {
	_, err := s.slotEntry(kind, "")
	return err == nil
}

/* QuotaViews 三类配额的当日视图（多媒体工作台配额条；与任务响应同一计数）。 */
func (s *MediaService) QuotaViews() map[string]domain.GenQuota {
	return map[string]domain.GenQuota{
		domain.GenImage: s.QuotaOf(domain.GenImage),
		domain.GenAudio: s.QuotaOf(domain.GenAudio),
		domain.GenVideo: s.QuotaOf(domain.GenVideo),
	}
}

/* rollDayLocked 跨日清零（调用方持锁）。 */
func (s *MediaService) rollDayLocked() {
	today := time.Now().Format("2006-01-02")
	if s.quota.Day != today {
		s.quota = genQuotaFile{Day: today}
	}
}

/*
	reserve 原子占额：超额返回带余量与改法提示的错误（审批卡与工具结果

都会展示它）。refund 在生成彻底失败（无产物）时退还。
*/
func (s *MediaService) reserve(kind string, units int) error {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	s.rollDayLocked()
	limit := s.quotaLimit(kind)
	var used *int
	switch kind {
	case domain.GenImage:
		used = &s.quota.Image
	case domain.GenAudio:
		used = &s.quota.Audio
	default:
		used = &s.quota.Video
	}
	if *used+units > limit {
		return fmt.Errorf("%w：%s 生成今日配额已用完（%d/%d）：明天自动重置，或在 多媒体·模型与限额 调整",
			ErrQuotaExceeded, kindLabel(kind), *used, limit)
	}
	*used += units
	s.persistLocked()
	return nil
}

func (s *MediaService) refund(kind string, units int) {
	if units <= 0 {
		return
	}
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	s.rollDayLocked()
	switch kind {
	case domain.GenImage:
		s.quota.Image -= units
		if s.quota.Image < 0 {
			s.quota.Image = 0
		}
	case domain.GenAudio:
		s.quota.Audio -= units
		if s.quota.Audio < 0 {
			s.quota.Audio = 0
		}
	default:
		s.quota.Video -= units
		if s.quota.Video < 0 {
			s.quota.Video = 0
		}
	}
	s.persistLocked()
}

func (s *MediaService) persistLocked() {
	if data, err := json.Marshal(s.quota); err == nil {
		_ = s.Hub.Fsys.WritePrivate(context.Background(), quotaFile, data)
	}
}

func kindLabel(kind string) string {
	switch kind {
	case domain.GenImage:
		return "图片"
	case domain.GenAudio:
		return "语音"
	default:
		return "视频"
	}
}

/* ── 任务档案 ── */

/* newJobID 生成短 id：<kind>-<6hex>。 */
func newJobID(kind string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return kind + "-" + hex.EncodeToString(b)
}

/* addJob 入档并持久（封顶丢最旧终态）。 */
func (s *MediaService) addJob(j *domain.MediaJob) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	s.jobs = append(s.jobs, j)
	s.persistJobsLocked()
}

/* updateJob 终态回写（done/failed）并持久。 */
func (s *MediaService) finishJob(id, status string, files []domain.GenFile, quota domain.GenQuota, errText string, doneAt int64) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.Status = status
			j.Files = files
			j.Quota = quota
			j.Err = errText
			j.DoneAt = doneAt
			break
		}
	}
	s.persistJobsLocked()
}

/* setRunning queued → running（不持久化也行，但重启恢复语义统一：落盘）。 */
func (s *MediaService) setRunning(id string) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.Status = domain.JobRunning
			break
		}
	}
	s.persistJobsLocked()
}

/* persistJobsLocked 落盘任务档案（调用方持锁）；终态超出 jobsKeep 丢最旧。 */
func (s *MediaService) persistJobsLocked() {
	done := 0
	for _, j := range s.jobs {
		if j.Status == domain.JobDone || j.Status == domain.JobFailed {
			done++
		}
	}
	if over := done - jobsKeep; over > 0 {
		out := make([]*domain.MediaJob, 0, len(s.jobs)-over)
		for _, j := range s.jobs {
			if over > 0 && (j.Status == domain.JobDone || j.Status == domain.JobFailed) {
				over-- // 丢最旧的终态任务
				continue
			}
			out = append(out, j)
		}
		s.jobs = out
	}
	snap := make([]domain.MediaJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		snap = append(snap, *j)
	}
	if data, err := json.Marshal(jobsArchive{Jobs: snap}); err == nil {
		_ = s.Hub.Fsys.WritePrivate(context.Background(), jobsArchiveFile, data)
	}
}

/* Job 按id取任务快照。 */
func (s *MediaService) Job(id string) (*domain.MediaJob, bool) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			snap := *j
			return &snap, true
		}
	}
	return nil, false
}

/* Jobs 最近任务（新→旧）。 */
func (s *MediaService) Jobs(limit int) []domain.MediaJob {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	out := make([]domain.MediaJob, 0, len(s.jobs))
	for i := len(s.jobs) - 1; i >= 0; i-- {
		out = append(out, *s.jobs[i])
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

/* WaitJob 等待任务到终态或超时；返回任务快照与是否终态。 */
func (s *MediaService) WaitJob(id string, d time.Duration) (*domain.MediaJob, bool) {
	deadline := time.Now().Add(d)
	for {
		j, ok := s.Job(id)
		if !ok {
			return nil, false
		}
		if j.Status == domain.JobDone || j.Status == domain.JobFailed {
			return j, true
		}
		if time.Now().After(deadline) {
			return j, false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

/* ── 模型条目解析（media.json）── */

/*
	slotEntry 取本次调用的模型条目：model 空取该类生效条目（未配置/未

启用给配置引导）；显式名在该类找、再去三类里找（跨类复用同一条配置）。
*/
func (s *MediaService) slotEntry(kind, model string) (*domain.ModelEntry, error) {
	mc := s.Hub.MediaSnapshot()
	slot := mc.Image
	switch kind {
	case domain.GenAudio:
		slot = mc.Audio
	case domain.GenVideo:
		slot = mc.Video
	}
	if model == "" {
		if e := domain.ActiveSlot(slot); e != nil {
			return e, nil
		}
		return nil, fmt.Errorf("%s 类未配置生成模型：请在 多媒体·模型与限额 添加并启用（SiliconFlow 兼容端点，填 baseUrl 与 apiKey）",
			kindLabel(kind))
	}
	for i := range slot {
		if slot[i].Name == model && !slot[i].Disabled {
			return &slot[i], nil
		}
	}
	for _, entries := range [][]domain.ModelEntry{mc.Image, mc.Audio, mc.Video} {
		for i := range entries {
			if entries[i].Name == model && !entries[i].Disabled {
				return &entries[i], nil
			}
		}
	}
	return nil, fmt.Errorf("unknown model %q（多媒体三类里都没有它）", model)
}

/* ── 上游调用底座 ── */

const defaultGenBase = "https://api.siliconflow.cn/v1"

func genEndpoint(entry *domain.ModelEntry, path string) string {
	base := strings.TrimSpace(entry.BaseURL)
	if base == "" {
		base = defaultGenBase
	}
	return strings.TrimSuffix(base, "/") + path
}

func genHeaders(entry *domain.ModelEntry) map[string]string {
	h := map[string]string{"Authorization": "Bearer " + entry.APIKey}
	for k, v := range entry.Headers {
		h[k] = v
	}
	return h
}

/* postJSON POST JSON 载荷收响应。 */
func (s *MediaService) postJSON(ctx context.Context, entry *domain.ModelEntry, path string, body any) ([]byte, int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, genEndpoint(entry, path), bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range genHeaders(entry) {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 300 {
		return out, resp.StatusCode, fmt.Errorf("上游 %d：%s", resp.StatusCode, upstreamBrief(out))
	}
	return out, resp.StatusCode, nil
}

/* getJSON GET（视频状态轮询）。 */
func (s *MediaService) getJSON(ctx context.Context, entry *domain.ModelEntry, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, genEndpoint(entry, path), nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range genHeaders(entry) {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 300 {
		return out, resp.StatusCode, fmt.Errorf("上游 %d：%s", resp.StatusCode, upstreamBrief(out))
	}
	return out, resp.StatusCode, nil
}

/* downloadBinary 下载提供方产物（图片 URL / 成片 mp4）。 */
func (s *MediaService) downloadBinary(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("产物下载失败 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

/* upstreamBrief 截取上游错误正文（apiKey/额度/参数问题的原话最省事）。 */
func upstreamBrief(body []byte) string {
	s := strings.TrimSpace(string(body))
	var w struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &w) == nil && w.Error != nil {
		if b, err := json.Marshal(w.Error); err == nil {
			s = string(b)
		}
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

/* ── 产物落盘（与 StashFiles 同区同风格：<工作目录>/tmp/）── */

func (s *MediaService) saveGen(ctx context.Context, prefix, ext, mime string, data []byte, url string, embed bool) (domain.GenFile, error) {
	dir := filepath.ToSlash(filepath.Join(ResolveWorkDir(s.Hub.SettingsSnapshot().WorkDir), "tmp"))
	name := fmt.Sprintf("gen-%s-%d-%s.%s", time.Now().Format("20060102-150405"), s.genSeq.Add(1), prefix, ext)
	path := dir + "/" + name
	if err := s.Hub.Fsys.Write(ctx, path, data); err != nil {
		return domain.GenFile{}, fmt.Errorf("产物落盘失败: %w", err)
	}
	f := domain.GenFile{Name: name, Path: path, Size: len(data), Mime: mime, URL: url}
	if embed {
		f.B64 = base64.StdEncoding.EncodeToString(data)
	}
	return f, nil
}

/* ── 文生图：POST /images/generations ── */

/*
SubmitImage 提交文生图任务：prompt 必填；size 形如 1024x1024（空=模型
默认）；batch 1-4（默认 1）；seed 可复现。按张计数；立即返回任务，
生成在后台执行。
*/
func (s *MediaService) SubmitImage(_ context.Context, model, prompt, negative, size string, batch int, seed int64) (*domain.MediaJob, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("prompt required")
	}
	if batch < 1 {
		batch = 1
	}
	if batch > 4 {
		batch = 4
	}
	entry, err := s.slotEntry(domain.GenImage, model)
	if err != nil {
		return nil, err
	}
	if err := s.reserve(domain.GenImage, batch); err != nil {
		return nil, err
	}
	job := &domain.MediaJob{
		ID: newJobID(domain.GenImage), Kind: domain.GenImage, Status: domain.JobQueued,
		Model: entry.Name, Brief: briefOf(prompt), CreatedAt: time.Now().UnixMilli(),
		Quota: s.QuotaOf(domain.GenImage), Units: batch,
	}
	s.addJob(job)
	go s.runImage(job, *entry, prompt, negative, size, batch, seed)
	return job, nil
}

func (s *MediaService) runImage(job *domain.MediaJob, entry domain.ModelEntry, prompt, negative, size string, batch int, seed int64) {
	s.setRunning(job.ID)
	body := map[string]any{"model": entry.Name, "prompt": prompt, "batch_size": batch}
	if negative = strings.TrimSpace(negative); negative != "" {
		body["negative_prompt"] = negative
	}
	if size = strings.TrimSpace(size); size != "" {
		body["image_size"] = size
	}
	if seed != 0 {
		body["seed"] = seed
	}
	ctx, cancel := context.WithTimeout(context.Background(), imageTimeout)
	defer cancel()
	raw, _, err := s.postJSON(ctx, &entry, "/images/generations", body)
	if err != nil {
		s.failJob(job, fmt.Errorf("图片生成失败：%w", err))
		return
	}
	var resp struct {
		Images []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"images"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Images) == 0 {
		s.failJob(job, fmt.Errorf("图片生成失败：响应里没有图像（%s）", upstreamBrief(raw)))
		return
	}
	files := make([]domain.GenFile, 0, len(resp.Images))
	for i, im := range resp.Images {
		var data []byte
		var mt string
		switch {
		case im.B64JSON != "":
			var derr error
			data, derr = base64.StdEncoding.DecodeString(im.B64JSON)
			if derr != nil {
				s.failJobPartial(job, files, batch, fmt.Errorf("第 %d 张 base64 解码失败: %w", i+1, derr))
				return
			}
			mt = "image/png"
		case im.URL != "":
			dctx, dcancel := context.WithTimeout(context.Background(), imageTimeout)
			var derr error
			data, mt, derr = s.downloadBinary(dctx, im.URL)
			dcancel()
			if derr != nil {
				s.failJobPartial(job, files, batch, fmt.Errorf("第 %d 张：%w", i+1, derr))
				return
			}
		default:
			continue
		}
		ext, m := sniffImage(mt, im.URL)
		f, werr := s.saveGen(context.Background(), fmt.Sprintf("img-%d", i+1), ext, m, data, im.URL, true)
		if werr != nil {
			s.failJobPartial(job, files, batch, werr)
			return
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		s.failJob(job, errors.New("图片生成失败：响应图像为空"))
		return
	}
	/* 部分成功：按实际张数对账退额 */
	if n := batch - len(files); n > 0 {
		s.refund(domain.GenImage, n)
	}
	s.finishJob(job.ID, domain.JobDone, files, s.QuotaOf(domain.GenImage), "", time.Now().UnixMilli())
}

/* failJob 彻底失败：退额 + 终态。 */
func (s *MediaService) failJob(job *domain.MediaJob, err error) {
	s.refund(job.Kind, job.Units)
	s.finishJob(job.ID, domain.JobFailed, nil, s.QuotaOf(job.Kind), err.Error(), time.Now().UnixMilli())
}

/* failJobPartial 部分产物后失败：已有产物交付（done），缺的张数退额。 */
func (s *MediaService) failJobPartial(job *domain.MediaJob, files []domain.GenFile, batch int, err error) {
	if len(files) == 0 {
		s.failJob(job, err)
		return
	}
	if n := batch - len(files); n > 0 {
		s.refund(job.Kind, n)
	}
	s.finishJob(job.ID, domain.JobDone, files, s.QuotaOf(job.Kind),
		fmt.Sprintf("部分成功（%d/%d 张），缺额已退还：%s", len(files), batch, err.Error()), time.Now().UnixMilli())
}

/* briefOf 任务摘要（清单展示）。 */
func briefOf(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

/* sniffImage 从 Content-Type 或 URL 推断扩展名与规范 mime。 */
func sniffImage(ct, url string) (ext, mimeType string) {
	ct = strings.TrimSpace(strings.Split(ct, ";")[0])
	if strings.HasPrefix(ct, "image/") {
		mimeType = ct
		if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
			ext = strings.TrimPrefix(exts[0], ".")
		}
	}
	if ext == "" {
		if i := strings.LastIndex(url, "."); i >= 0 && len(url)-i <= 6 {
			ext = strings.ToLower(url[i+1:])
			if e := strings.TrimSuffix(ext, "?"); e != "" {
				ext = e
			}
		}
	}
	switch ext {
	case "jpg", "jpeg", "webp", "gif", "bmp":
		if mimeType == "" {
			mimeType = "image/" + ext
		}
	default:
		ext, mimeType = "png", "image/png"
	}
	return ext, mimeType
}

/* ── 语音合成：POST /audio/speech（响应体即音频字节）── */

/*
SubmitSpeech 提交语音合成任务：input 上限 genInputMax 字符（按字节
计费的闸）；voice 预置音色可只给短名（alex/anna…，自动补 "<model>:"
前缀）；format mp3/wav/opus/pcm（默认 mp3）。按次计数。
*/
func (s *MediaService) SubmitSpeech(_ context.Context, model, input, voice, format string, speed float64) (*domain.MediaJob, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("input required")
	}
	if len([]rune(input)) > genInputMax {
		return nil, fmt.Errorf("文本过长（%d 字符，上限 %d）：请分段合成", len([]rune(input)), genInputMax)
	}
	switch format {
	case "":
		format = "mp3"
	case "mp3", "wav", "opus", "pcm":
	default:
		return nil, fmt.Errorf("response_format 非法（mp3/wav/opus/pcm）：%s", format)
	}
	entry, err := s.slotEntry(domain.GenAudio, model)
	if err != nil {
		return nil, err
	}
	voice = strings.TrimSpace(voice)
	if voice == "" {
		voice = "alex"
	}
	/* 预置音色须带 "<model>:" 前缀；自定义（speech:xxx）与已带前缀的原样放行 */
	if !strings.Contains(voice, ":") {
		voice = entry.Name + ":" + voice
	}
	if err := s.reserve(domain.GenAudio, 1); err != nil {
		return nil, err
	}
	job := &domain.MediaJob{
		ID: newJobID(domain.GenAudio), Kind: domain.GenAudio, Status: domain.JobQueued,
		Model: entry.Name, Brief: briefOf(input), CreatedAt: time.Now().UnixMilli(),
		Quota: s.QuotaOf(domain.GenAudio), Units: 1,
	}
	s.addJob(job)
	go s.runSpeech(job, *entry, input, voice, format, speed)
	return job, nil
}

func (s *MediaService) runSpeech(job *domain.MediaJob, entry domain.ModelEntry, input, voice, format string, speed float64) {
	s.setRunning(job.ID)
	body := map[string]any{
		"model": entry.Name, "input": input, "voice": voice,
		"response_format": format,
	}
	if speed > 0 {
		body["speed"] = speed
	}
	ctx, cancel := context.WithTimeout(context.Background(), speechTimeout)
	defer cancel()
	raw, _, err := s.postJSON(ctx, &entry, "/audio/speech", body)
	if err != nil {
		s.failJob(job, fmt.Errorf("语音合成失败：%w", err))
		return
	}
	if len(raw) == 0 {
		s.failJob(job, errors.New("语音合成失败：响应为空"))
		return
	}
	mimeType := "audio/" + map[string]string{"mp3": "mpeg", "wav": "wav", "opus": "ogg", "pcm": "pcm"}[format]
	f, err := s.saveGen(context.Background(), "tts", format, mimeType, raw, "", true)
	if err != nil {
		s.failJob(job, err)
		return
	}
	s.finishJob(job.ID, domain.JobDone, []domain.GenFile{f}, s.QuotaOf(domain.GenAudio), "", time.Now().UnixMilli())
}

/* ── 视频生成：POST /video/submissions + GET /video/submissions/{id}/status ── */

/*
SubmitVideo 提交文生视频任务（可带首/尾帧图）：上游异步出片，后台
提交→轮询（每 videoPollStep 一次，整体上限 videoWaitMax）→下载成片。
duration 5-15 秒（按秒计费，默认 5）；resolution 480p/768p（默认 768p）；
帧图给 http(s) URL 或本机路径（读盘转 data URL）。按次计数。
*/
func (s *MediaService) SubmitVideo(_ context.Context, model, prompt, firstFrame, lastFrame string, duration int, resolution, ratio string) (*domain.MediaJob, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("prompt required")
	}
	if duration == 0 {
		duration = 5
	}
	if duration < 5 || duration > 15 {
		return nil, errors.New("duration 需在 5-15 秒之间")
	}
	if resolution == "" {
		resolution = "768p"
	}
	if resolution != "480p" && resolution != "768p" {
		return nil, fmt.Errorf("resolution 非法（480p/768p）：%s", resolution)
	}
	entry, err := s.slotEntry(domain.GenVideo, model)
	if err != nil {
		return nil, err
	}
	if err := s.reserve(domain.GenVideo, 1); err != nil {
		return nil, err
	}
	job := &domain.MediaJob{
		ID: newJobID(domain.GenVideo), Kind: domain.GenVideo, Status: domain.JobQueued,
		Model: entry.Name, Brief: briefOf(prompt), CreatedAt: time.Now().UnixMilli(),
		Quota: s.QuotaOf(domain.GenVideo), Units: 1,
	}
	s.addJob(job)
	go s.runVideo(job, *entry, prompt, firstFrame, lastFrame, duration, resolution, ratio)
	return job, nil
}

func (s *MediaService) runVideo(job *domain.MediaJob, entry domain.ModelEntry, prompt, firstFrame, lastFrame string, duration int, resolution, ratio string) {
	s.setRunning(job.ID)
	body := map[string]any{
		"model": entry.Name, "prompt": prompt,
		"duration": duration, "resolution": resolution,
	}
	for role, ref := range map[string]string{"first_frame": firstFrame, "last_frame": lastFrame} {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		u, rerr := s.frameRef(ref)
		if rerr != nil {
			s.failJob(job, fmt.Errorf("%s 帧图：%w", role, rerr))
			return
		}
		if body["images"] == nil {
			body["images"] = []map[string]string{}
		}
		body["images"] = append(body["images"].([]map[string]string), map[string]string{"role": role, "url": u})
	}
	if ratio = strings.TrimSpace(ratio); ratio != "" {
		body["ratio"] = ratio
	}

	sctx, scancel := context.WithTimeout(context.Background(), time.Minute)
	raw, _, err := s.postJSON(sctx, &entry, "/video/submissions", body)
	scancel()
	if err != nil {
		s.failJob(job, fmt.Errorf("视频任务提交失败：%w", err))
		return
	}
	var sub struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &sub); err != nil || sub.ID == "" {
		s.failJob(job, fmt.Errorf("视频任务提交失败：响应无 id（%s）", upstreamBrief(raw)))
		return
	}

	/* 轮询到出片：Succeed 取成片，Failed 带原因退额，超时也退额 */
	wctx, wcancel := context.WithTimeout(context.Background(), videoWaitMax)
	defer wcancel()
	videoURL := ""
	for videoURL == "" {
		pctx, pcancel := context.WithTimeout(wctx, 30*time.Second)
		raw, _, err := s.getJSON(pctx, &entry, "/video/submissions/"+sub.ID+"/status")
		pcancel()
		if err != nil {
			s.failJob(job, fmt.Errorf("视频任务 %s 状态查询失败：%w", sub.ID, err))
			return
		}
		var st struct {
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Results struct {
				Videos []struct {
					URL string `json:"url"`
				} `json:"videos"`
			} `json:"results"`
		}
		if jerr := json.Unmarshal(raw, &st); jerr != nil {
			s.failJob(job, fmt.Errorf("视频任务状态解析失败：%w", jerr))
			return
		}
		switch st.Status {
		case "Succeed":
			if len(st.Results.Videos) == 0 || st.Results.Videos[0].URL == "" {
				s.failJob(job, errors.New("视频任务完成但无成片 URL"))
				return
			}
			videoURL = st.Results.Videos[0].URL
		case "Failed":
			reason := st.Reason
			if reason == "" {
				reason = upstreamBrief(raw)
			}
			s.failJob(job, fmt.Errorf("视频生成失败：%s", reason))
			return
		default: // InQueue / InProgress
			select {
			case <-wctx.Done():
				s.failJob(job, fmt.Errorf("视频生成超时（上游任务 %s 仍在 %s，未扣当日配额；上游可能仍在出片，勿盲目重复提交）",
					sub.ID, st.Status))
				return
			case <-time.After(videoPollStep):
			}
		}
	}

	dctx, dcancel := context.WithTimeout(context.Background(), videoWaitMax)
	data, ct, derr := s.downloadBinary(dctx, videoURL)
	dcancel()
	if derr != nil {
		/* 出片但下载失败：任务已计费，不再退还配额 */
		s.finishJob(job.ID, domain.JobFailed, nil, s.QuotaOf(domain.GenVideo),
			fmt.Sprintf("成片下载失败：%v（原始地址 %s）", derr, videoURL), time.Now().UnixMilli())
		return
	}
	f, err := s.saveGen(context.Background(), "video", "mp4", videoMime(ct), data, videoURL, false)
	if err != nil {
		s.failJob(job, err)
		return
	}
	s.finishJob(job.ID, domain.JobDone, []domain.GenFile{f}, s.QuotaOf(domain.GenVideo), "", time.Now().UnixMilli())
}

/* videoMime 规范化成片 mime（默认 mp4）。 */
func videoMime(ct string) string {
	ct = strings.TrimSpace(strings.Split(ct, ";")[0])
	if strings.HasPrefix(ct, "video/") {
		return ct
	}
	return "video/mp4"
}

/* frameRef 帧图引用归一：http(s)/data URL 原样；其余按本机路径读盘转 data URL。 */
func (s *MediaService) frameRef(ref string) (string, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "data:") {
		return ref, nil
	}
	data, err := s.Hub.Fsys.Read(context.Background(), ref)
	if err != nil {
		return "", fmt.Errorf("读取 %s: %w", ref, err)
	}
	mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(ref)))
	if mt == "" || !strings.HasPrefix(mt, "image/") {
		mt = "image/png"
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

/* GenFilesSize 产物总字节数（工具结果摘要用）。 */
func GenFilesSize(files []domain.GenFile) int {
	n := 0
	for _, f := range files {
		n += f.Size
	}
	return n
}
