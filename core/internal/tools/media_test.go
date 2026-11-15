package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ezharness/core/internal/domain"
)

/* fakeMedia 实现 MediaJobs 的最小桩：能力位可配，提交路径不应被调用。 */
type fakeMedia struct {
	cap  map[string]bool
	jobs []domain.MediaJob
}

func (f fakeMedia) SubmitImage(context.Context, string, string, string, string, int, int64) (*domain.MediaJob, error) {
	panic("submit image should not be called without capability")
}
func (f fakeMedia) SubmitSpeech(context.Context, string, string, string, string, float64) (*domain.MediaJob, error) {
	panic("submit audio should not be called without capability")
}
func (f fakeMedia) SubmitVideo(context.Context, string, string, string, string, int, string, string) (*domain.MediaJob, error) {
	panic("submit video should not be called without capability")
}
func (f fakeMedia) WaitJob(string, time.Duration) (*domain.MediaJob, bool) { return nil, false }
func (f fakeMedia) Job(string) (*domain.MediaJob, bool)                    { return nil, false }
func (f fakeMedia) Jobs(int) []domain.MediaJob                             { return f.jobs }
func (f fakeMedia) HasCapability(kind string) bool                         { return f.cap[kind] }

/* 未配置的能力调用（工具仍在）要返回“当前无 X 能力”，而不是工具错误。 */
func TestMediaUnconfiguredCapability(t *testing.T) {
	tool := Media(fakeMedia{cap: map[string]bool{}})
	cases := []struct {
		args string
		want string
	}{
		{`{"action":"image","prompt":"x"}`, "当前无图片能力"},
		{`{"action":"audio","input":"x"}`, "当前无语音能力"},
		{`{"action":"video","prompt":"x"}`, "当前无视频能力"},
	}
	for _, c := range cases {
		out, err := tool.Invoke(context.Background(), json.RawMessage(c.args))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.args, err)
		}
		if !strings.Contains(out, c.want) {
			t.Fatalf("%s: got %q, want contains %q", c.args, out, c.want)
		}
	}
}

/* status 不依赖能力配置；空任务清单给占位。 */
func TestMediaStatusEmpty(t *testing.T) {
	tool := Media(fakeMedia{cap: map[string]bool{}})
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"action":"status"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "暂无生成任务") {
		t.Fatalf("got %q", out)
	}
}
