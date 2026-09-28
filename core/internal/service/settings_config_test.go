package service

import (
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
	if err := SaveSettings(fsys, st); err != nil {
		t.Fatal(err)
	}

	got := LoadSettings(fsys)
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

	// 越界值落盘后按上限读回
	st.MaxIterations = 999
	st.TrimPercent = 999
	_ = SaveSettings(fsys, st)
	if got = LoadSettings(fsys); got.MaxIterations != 128 || got.TrimPercent != 100 {
		t.Fatalf("越界未收敛: %+v", got)
	}
}

/* 无 settings.json 的目录读取出厂值。 */
func TestSettingsDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	got := LoadSettings(osfs.OS{})
	if got.SystemExtra != "" || got.TrimPercent != 75 || got.CloseToTray ||
		got.MaxIterations != 0 || got.DebugMode || len(got.DisabledSkills) != 0 {
		t.Fatalf("缺失文件应回落默认: %+v", got)
	}
}
