/*
MediaController 多媒体模块的 HTTP 面：

  - OpenAPI（/api/open/*，与 model/call 同级同鉴权）：POST /image|audio|video/call
    提交任务（wait_seconds 短等待，默认 10 秒、上限 60——完成直接带产物，
    未完成带任务 id），GET /media/jobs[/:id] 查询任务。配额超限 429、
    参数/配置问题 400。
  - 会话面（/api/media）：多媒体生成模型配置（media.json）读写——多媒体页
    「模型与限额」区的数据源；保存重建 agent（busy 拒绝，与 models 同规）。

agent 工具与 OpenAPI 共用 service.MediaService（任务引擎、每日配额、
落盘、错误文案单一来源）。
*/
package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/service"
)

/* MediaController 多媒体模块。 */
type MediaController struct {
	Svc      *service.MediaService
	Settings *service.SettingsService // 配置读写（media.json 挂在设置服务下共用 Reassemble）
}

/*
ImageCall POST /api/open/image/call：{prompt, model?, negative_prompt?,
image_size?, batch_size?(1-4), seed?, wait_seconds?(0-60，默认 10)}。
model 缺省 = image 类生效条目。
*/
func (c *MediaController) ImageCall(g *gin.Context) {
	var req struct {
		Model       string `json:"model"`
		Prompt      string `json:"prompt"`
		Negative    string `json:"negative_prompt"`
		Size        string `json:"image_size"`
		Batch       int    `json:"batch_size"`
		Seed        int64  `json:"seed"`
		WaitSeconds *int   `json:"wait_seconds"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Prompt == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "prompt required"})
		return
	}
	job, err := c.Svc.SubmitImage(g.Request.Context(), req.Model, req.Prompt, req.Negative, req.Size, req.Batch, req.Seed)
	respondJob(g, c.Svc, job, req.WaitSeconds, err)
}

/*
AudioCall POST /api/open/audio/call：{input, model?, voice?(短名
alex/anna…), response_format?(mp3/wav/opus/pcm), speed?, wait_seconds?}。
*/
func (c *MediaController) AudioCall(g *gin.Context) {
	var req struct {
		Model       string  `json:"model"`
		Input       string  `json:"input"`
		Voice       string  `json:"voice"`
		Format      string  `json:"response_format"`
		Speed       float64 `json:"speed"`
		WaitSeconds *int    `json:"wait_seconds"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Input == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "input required"})
		return
	}
	job, err := c.Svc.SubmitSpeech(g.Request.Context(), req.Model, req.Input, req.Voice, req.Format, req.Speed)
	respondJob(g, c.Svc, job, req.WaitSeconds, err)
}

/*
VideoCall POST /api/open/video/call：{prompt, model?, duration?(5-15),
resolution?(480p/768p), ratio?, first_frame?/last_frame?(URL 或本机路径),
wait_seconds?}。视频生成需数分钟：wait_seconds 只影响本次响应等多久，
任务本身继续在后台跑，之后用 jobs 端点查询。
*/
func (c *MediaController) VideoCall(g *gin.Context) {
	var req struct {
		Model       string `json:"model"`
		Prompt      string `json:"prompt"`
		Duration    int    `json:"duration"`
		Resolution  string `json:"resolution"`
		Ratio       string `json:"ratio"`
		FirstFrame  string `json:"first_frame"`
		LastFrame   string `json:"last_frame"`
		WaitSeconds *int   `json:"wait_seconds"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Prompt == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "prompt required"})
		return
	}
	job, err := c.Svc.SubmitVideo(g.Request.Context(), req.Model, req.Prompt, req.FirstFrame, req.LastFrame, req.Duration, req.Resolution, req.Ratio)
	respondJob(g, c.Svc, job, req.WaitSeconds, err)
}

/* respondJob 统一提交响应：短等待后回 {ok, job}；配额 429、参数 400、其他 502。 */
func respondJob(g *gin.Context, svc *service.MediaService, job *domain.MediaJob, waitSeconds *int, err error) {
	if err != nil {
		if errors.Is(err, service.ErrQuotaExceeded) {
			g.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	wait := 10 * time.Second
	if waitSeconds != nil {
		if *waitSeconds < 0 {
			*waitSeconds = 0
		}
		if *waitSeconds > 60 {
			*waitSeconds = 60
		}
		wait = time.Duration(*waitSeconds) * time.Second
	}
	if wait > 0 {
		job, _ = svc.WaitJob(job.ID, wait)
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "job": job})
}

/* JobGET GET /api/open/media/jobs/:id：任务详情（done 带产物）。 */
func (c *MediaController) JobGET(g *gin.Context) {
	job, ok := c.Svc.Job(g.Param("id"))
	if !ok {
		g.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	g.JSON(http.StatusOK, gin.H{"job": job})
}

/* JobsGET GET /api/open/media/jobs?limit=：最近任务（新→旧，默认 20）。 */
func (c *MediaController) JobsGET(g *gin.Context) {
	limit := 20
	if v := g.Query("limit"); v != "" {
		n := 0
		for _, ch := range v {
			if ch < '0' || ch > '9' {
				n = -1
				break
			}
			n = n*10 + int(ch-'0')
		}
		if n > 0 {
			limit = n
		}
	}
	jobs := c.Svc.Jobs(limit)
	/* 清单是轮询接口：剥掉图片/音频的内嵌 base64（可达数 MB/条），
	   产物预览走 /api/workspace/file 或按 id 取详情。Files 是浅拷贝
	   共享的切片，先复制再清空，别改到任务档案里的原始条目 */
	for i := range jobs {
		if len(jobs[i].Files) == 0 {
			continue
		}
		files := make([]domain.GenFile, len(jobs[i].Files))
		copy(files, jobs[i].Files)
		for k := range files {
			files[k].B64 = ""
		}
		jobs[i].Files = files
	}
	g.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

/* QuotaGET GET /api/media/quota：三类生成任务的当日已用/限额（工作台配额条）。 */
func (c *MediaController) QuotaGET(g *gin.Context) {
	g.JSON(http.StatusOK, c.Svc.QuotaViews())
}

/* ConfigGET GET /api/media：多媒体生成模型配置（多媒体页数据源）。 */
func (c *MediaController) ConfigGET(g *gin.Context) {
	m := c.Settings.Hub.MediaSnapshot()
	if m.Image == nil {
		m.Image = []domain.ModelEntry{}
	}
	if m.Audio == nil {
		m.Audio = []domain.ModelEntry{}
	}
	if m.Video == nil {
		m.Video = []domain.ModelEntry{}
	}
	g.JSON(http.StatusOK, m)
}

/* ConfigPUT POST /api/media：保存多媒体配置并重建 agent（busy 拒绝）。 */
func (c *MediaController) ConfigPUT(g *gin.Context) {
	var m domain.MediaConfig
	if err := g.ShouldBindJSON(&m); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.UpdateMedia(g.Request.Context(), m); err != nil {
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
