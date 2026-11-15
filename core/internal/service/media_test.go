/*
media 用例测试：异步任务引擎（提交→短等待→产物/任务id、失败退还、
部分成功对账）、配额（计数/跨日重置/超限/重启恢复退还）、三类端点的
请求-响应回路（httptest 假上游）。
*/
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
)

/* newMediaTestHub 临时数据目录里的 Hub + 配好三类生成模型的 MediaService。 */
func newMediaTestHub(t *testing.T, baseURL string) (*domain.Hub, *MediaService) {
	t.Helper()
	t.Chdir(t.TempDir())
	hub := domain.NewHub()
	hub.Fsys = osfs.OS{}
	entry := func(name string) domain.ModelEntry {
		return domain.ModelEntry{Name: name, BaseURL: baseURL, APIKey: "test-key", Enabled: true}
	}
	hub.ApplyMedia(domain.MediaConfig{
		Image: []domain.ModelEntry{entry("flux-test")},
		Audio: []domain.ModelEntry{entry("tts-test")},
		Video: []domain.ModelEntry{entry("video-test")},
	})
	return hub, NewMediaService(hub)
}

/* waitDone 轮询任务到终态（测试辅助：上限 d）。 */
func waitDone(t *testing.T, svc *MediaService, id string, d time.Duration) *domain.MediaJob {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		j, ok := svc.Job(id)
		if !ok {
			t.Fatalf("任务 %s 不存在", id)
		}
		if j.Status == domain.JobDone || j.Status == domain.JobFailed {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("任务 %s 超时未终态：%+v", id, j)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

/* TestMediaQuota 配额：默认限额、超限哨兵错误、跨日清零、失败退还。 */
func TestMediaQuota(t *testing.T) {
	hub, svc := newMediaTestHub(t, "http://unused.local")
	st := hub.SettingsSnapshot()
	st.GenQuotaVideo = 1 // 视频只许 1 次/日
	hub.ApplySettings(st)

	if err := svc.reserve(domain.GenVideo, 1); err != nil {
		t.Fatalf("首次占额不应失败: %v", err)
	}
	err := svc.reserve(domain.GenVideo, 1)
	if err == nil || !strings.Contains(err.Error(), "配额已用完") {
		t.Fatalf("超限应报配额错误: %v", err)
	}
	svc.refund(domain.GenVideo, 1) // 生成失败退还
	if err := svc.reserve(domain.GenVideo, 1); err != nil {
		t.Fatalf("退还后应可再用: %v", err)
	}

	/* image 按张计：默认 20 */
	for i := 0; i < domain.DefaultGenQuotaImage; i++ {
		if err := svc.reserve(domain.GenImage, 1); err != nil {
			t.Fatalf("第 %d 张不应超限: %v", i+1, err)
		}
	}
	if err := svc.reserve(domain.GenImage, 1); err == nil {
		t.Fatal("默认 20 张用完后应超限")
	}

	/* 跨日清零 */
	svc.quotaMu.Lock()
	svc.quota.Day = "2000-01-01"
	svc.quotaMu.Unlock()
	if err := svc.reserve(domain.GenVideo, 1); err != nil {
		t.Fatalf("跨日应清零: %v", err)
	}
}

/* TestMediaImageJob 文生图任务回路：URL 形态下载 + b64 形态直解 + 配额按张 + 失败退还。 */
func TestMediaImageJob(t *testing.T) {
	png := "iVBORw0KGgo=" // 任意 base64 字节（不校验图像内容）
	dl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fakepngbytes"))
	}))
	defer dl.Close()
	var gotBody map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/images/generations") {
			t.Errorf("端点路径: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("鉴权头缺失")
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"images":[{"url":%q},{"b64_json":%q}]}`, dl.URL+"/a.png", png)))
	}))
	defer up.Close()
	_, svc := newMediaTestHub(t, up.URL)

	job, err := svc.SubmitImage(context.Background(), "", "一只雪豹", "", "1024x1024", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobQueued && job.Status != domain.JobRunning {
		t.Fatalf("提交后应在排队/执行: %+v", job)
	}
	final := waitDone(t, svc, job.ID, 30*time.Second)
	if final.Status != domain.JobDone || len(final.Files) != 2 {
		t.Fatalf("应出 2 张: %+v", final)
	}
	if final.Files[0].Size == 0 || final.Files[1].B64 != png {
		t.Fatalf("产物形态不对: %+v", final.Files)
	}
	if !strings.Contains(final.Files[0].Path, "/tmp/gen-") || !strings.HasSuffix(final.Files[0].Path, ".png") {
		t.Fatalf("落盘路径约定: %s", final.Files[0].Path)
	}
	if final.Quota.Used != 2 || final.Quota.Limit != domain.DefaultGenQuotaImage {
		t.Fatalf("配额视图: %+v", final.Quota)
	}
	if gotBody["image_size"] != "1024x1024" || gotBody["batch_size"].(float64) != 2 {
		t.Fatalf("上游载荷: %+v", gotBody)
	}

	/* 上游失败：配额退还 */
	up.Close()
	job2, err := svc.SubmitImage(context.Background(), "", "x", "", "", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	final2 := waitDone(t, svc, job2.ID, 30*time.Second)
	if final2.Status != domain.JobFailed {
		t.Fatalf("上游关闭应失败: %+v", final2)
	}
	if got := svc.quotaUsed(domain.GenImage); got != 2 {
		t.Fatalf("失败应退还配额（期望 2，得 %d）", got)
	}
}

/* TestMediaSpeechJob 语音任务回路：音色补模型前缀、音频字节落盘、超长同步拒绝。 */
func TestMediaSpeechJob(t *testing.T) {
	var gotBody map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/audio/speech") {
			t.Errorf("端点路径: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte("fake-mp3-bytes"))
	}))
	defer up.Close()
	_, svc := newMediaTestHub(t, up.URL)

	job, err := svc.SubmitSpeech(context.Background(), "", "你好世界", "anna", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	final := waitDone(t, svc, job.ID, 30*time.Second)
	if final.Status != domain.JobDone || len(final.Files) != 1 {
		t.Fatalf("语音应完成: %+v", final)
	}
	f := final.Files[0]
	if !strings.HasSuffix(f.Path, ".mp3") || f.Mime != "audio/mpeg" || f.B64 != base64.StdEncoding.EncodeToString([]byte("fake-mp3-bytes")) {
		t.Fatalf("产物: %+v", f)
	}
	if gotBody["voice"] != "tts-test:anna" {
		t.Fatalf("音色应补模型前缀: %v", gotBody["voice"])
	}
	if _, err := svc.SubmitSpeech(context.Background(), "", strings.Repeat("长", genInputMax+1), "", "", 0); err == nil {
		t.Fatal("超长输入应同步报错")
	}
}

/*
	TestMediaVideoJob 视频任务回路：提交→轮询（两态后成功）→下载；Failed 退额；

短等待未完成时返回执行中快照（异步语义）。
*/
func TestMediaVideoJob(t *testing.T) {
	var polls int
	var submitted map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/video/submissions") && r.Method == "POST":
			_ = json.NewDecoder(r.Body).Decode(&submitted)
			_, _ = w.Write([]byte(`{"id":"task-1"}`))
		case strings.Contains(r.URL.Path, "/video/submissions/task-1/status"):
			polls++
			if polls == 1 {
				_, _ = w.Write([]byte(`{"status":"InQueue"}`))
				return
			}
			doneURL := "http://" + r.Host + "/done.mp4"
			_, _ = w.Write([]byte(`{"status":"Succeed","results":{"videos":[{"url":"` + doneURL + `"}]}}`))
		case strings.HasSuffix(r.URL.Path, "/done.mp4"):
			_, _ = w.Write([]byte("fakemp4bytes"))
		default:
			t.Errorf("意外请求: %s", r.URL.Path)
		}
	}))
	defer up.Close()
	hub, svc := newMediaTestHub(t, up.URL)
	st := hub.SettingsSnapshot()
	st.GenQuotaVideo = 2 // 留出第二个任务的额度
	hub.ApplySettings(st)

	job, err := svc.SubmitVideo(context.Background(), "", "镜头脚本", "", "", 5, "480p", "")
	if err != nil {
		t.Fatal(err)
	}
	/* 短等待必然未完成（首答 InQueue、轮询步 5 秒）——返回的仍是执行中快照 */
	early, done := svc.WaitJob(job.ID, 300*time.Millisecond)
	if done {
		t.Fatalf("视频不应在短等待内完成: %+v", early)
	}
	if early.Status != domain.JobQueued && early.Status != domain.JobRunning {
		t.Fatalf("短等待应拿到执行中状态: %+v", early)
	}
	final := waitDone(t, svc, job.ID, 60*time.Second)
	if final.Status != domain.JobDone || len(final.Files) != 1 {
		t.Fatalf("视频应完成: %+v", final)
	}
	f := final.Files[0]
	if !strings.HasSuffix(f.Path, ".mp4") || f.B64 != "" || f.URL == "" {
		t.Fatalf("视频产物形态（路径+URL，无 b64）: %+v", f)
	}
	if submitted["resolution"] != "480p" || submitted["duration"].(float64) != 5 {
		t.Fatalf("上游载荷: %+v", submitted)
	}
	if got := svc.quotaUsed(domain.GenVideo); got != 1 {
		t.Fatalf("成功任务占 1 次配额（得 %d）", got)
	}

	/* Failed：配额退还 */
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/status") {
			_, _ = w.Write([]byte(`{"status":"Failed","reason":"content policy"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"task-2"}`))
	}))
	defer fail.Close()
	_, svc2 := newMediaTestHub(t, fail.URL)
	job2, err := svc2.SubmitVideo(context.Background(), "", "x", "", "", 5, "", "")
	if err != nil {
		t.Fatal(err)
	}
	final2 := waitDone(t, svc2, job2.ID, 30*time.Second)
	if final2.Status != domain.JobFailed || !strings.Contains(final2.Err, "content policy") {
		t.Fatalf("Failed 应带原因: %+v", final2)
	}
	if got := svc2.quotaUsed(domain.GenVideo); got != 0 {
		t.Fatalf("失败应退还配额（得 %d）", got)
	}
}

/* TestMediaJobListAndStatus 任务清单（新→旧）与按 id 查询。 */
func TestMediaJobListAndStatus(t *testing.T) {
	_, svc := newMediaTestHub(t, "http://unused.local")
	if jobs := svc.Jobs(10); len(jobs) != 0 {
		t.Fatalf("初始应为空: %d", len(jobs))
	}
	svc.addJob(&domain.MediaJob{ID: "image-aaa", Kind: domain.GenImage, Status: domain.JobDone})
	svc.addJob(&domain.MediaJob{ID: "video-bbb", Kind: domain.GenVideo, Status: domain.JobRunning})
	got := svc.Jobs(10)
	if len(got) != 2 || got[0].ID != "video-bbb" { // 新→旧
		t.Fatalf("清单顺序: %+v", got)
	}
	if _, ok := svc.Job("image-aaa"); !ok {
		t.Fatal("按 id 查询失败")
	}
	if _, ok := svc.Job("nope"); ok {
		t.Fatal("不存在的 id 应 miss")
	}
}
