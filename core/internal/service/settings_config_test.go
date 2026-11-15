package service

import (
	"context"
	"slices"
	"testing"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/osfs"
)

/* 设置读写回路：全字段落盘后重读不丢；文件缺失回落出厂值。 */
func TestSettingsRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	fsys := osfs.OS{}

	st := domain.DefaultSettings()
	st.SystemExtra = "追加段"
	st.TrimPercent = 40
	st.WorkDir = "ws"
	st.CloseToTray = true
	st.DisabledSkills = []string{"pdf", "csv"}
	st.MaxIterations = 100
	st.DebugMode = true
	st.OffloadThreshold = 8192
	st.GenQuotaImage = 30
	st.GenQuotaAudio = 12
	st.GenQuotaVideo = 5
	if err := SaveSettings(context.Background(), fsys, st); err != nil {
		t.Fatal(err)
	}

	got := LoadSettings(context.Background(), fsys)
	if got.SystemExtra != "追加段" || got.TrimPercent != 40 || got.WorkDir != "ws" || !got.CloseToTray {
		t.Fatalf("基础字段: %+v", got)
	}
	if !slices.Equal(got.DisabledSkills, []string{"pdf", "csv"}) {
		t.Fatalf("禁用技能名单丢失: %+v", got.DisabledSkills)
	}
	if got.MaxIterations != 100 {
		t.Fatalf("迭代上限丢失: %d", got.MaxIterations)
	}
	if !got.DebugMode {
		t.Fatal("调试模式丢失")
	}
	if got.OffloadThreshold != 8192 || got.GenQuotaImage != 30 || got.GenQuotaAudio != 12 || got.GenQuotaVideo != 5 {
		t.Fatalf("卸载阈值 / 生成配额丢失: %+v", got)
	}

	// 越界值落盘后按上限读回
	st.MaxIterations = 999
	st.TrimPercent = 999
	st.OffloadThreshold = 9999999
	st.GenQuotaImage = 5000
	_ = SaveSettings(context.Background(), fsys, st)
	if got = LoadSettings(context.Background(), fsys); got.MaxIterations != 128 || got.TrimPercent != 100 ||
		got.OffloadThreshold != 1048576 || got.GenQuotaImage != 1000 {
		t.Fatalf("越界未收敛: %+v", got)
	}
}

/* 无 settings.json 的目录读取出厂值。 */
func TestSettingsDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	got := LoadSettings(context.Background(), osfs.OS{})
	if got.SystemExtra != "" || got.TrimPercent != 75 || got.CloseToTray ||
		got.MaxIterations != 0 || got.DebugMode || len(got.DisabledSkills) != 0 {
		t.Fatalf("缺失文件应回落默认: %+v", got)
	}
}
