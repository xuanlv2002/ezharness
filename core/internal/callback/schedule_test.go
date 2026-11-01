package callback

import (
	"testing"
	"time"
)

/* 一次性：未来时间可算下次，过期返回零值；摘要带本地时间。 */
func TestScheduleOnce(t *testing.T) {
	base := time.Date(2026, 11, 1, 8, 30, 0, 0, time.Local)
	s := Schedule{Kind: ScheduleOnce, At: "2026-11-01 09:00"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	next, err := s.Next(base)
	if err != nil {
		t.Fatal(err)
	}
	if next.Format("2006-01-02 15:04") != "2026-11-01 09:00" {
		t.Fatalf("next = %v", next)
	}
	if next, _ := s.Next(next); !next.IsZero() {
		t.Fatalf("past once must be zero, got %v", next)
	}
	if got := s.Summary(); got != "一次性 2026-11-01 09:00" {
		t.Fatalf("summary = %q", got)
	}
	for _, bad := range []string{"", "明早", "2026-13-01 09:00"} {
		if err := (Schedule{Kind: ScheduleOnce, At: bad}).Validate(); err == nil {
			t.Fatalf("At %q must fail", bad)
		}
	}
}

/* 固定间隔：从起点加时长；短于 10 秒拒绝。 */
func TestScheduleInterval(t *testing.T) {
	base := time.Date(2026, 11, 1, 8, 30, 0, 0, time.Local)
	s := Schedule{Kind: ScheduleInterval, Every: "30m"}
	next, err := s.Next(base)
	if err != nil {
		t.Fatal(err)
	}
	if next.Sub(base) != 30*time.Minute {
		t.Fatalf("next delta = %v", next.Sub(base))
	}
	if got := s.Summary(); got != "每 30 分钟" {
		t.Fatalf("summary = %q", got)
	}
	if err := (Schedule{Kind: ScheduleInterval, Every: "5s"}).Validate(); err == nil {
		t.Fatal("5s must fail")
	}
	if err := (Schedule{Kind: ScheduleInterval, Every: "nope"}).Validate(); err == nil {
		t.Fatal("bad duration must fail")
	}
}

/* cron：标准 5 段、步长、日/周组合与非法字段。 */
func TestScheduleCron(t *testing.T) {
	base := time.Date(2026, 11, 1, 8, 30, 0, 0, time.Local) // 2026-11-01 是周日
	cases := []struct {
		expr string
		want string
	}{
		{"0 9 * * *", "2026-11-01 09:00"},
		{"*/15 * * * *", "2026-11-01 08:45"},
		{"0 0 1 * *", "2026-12-01 00:00"},
		{"0 9 * * 1", "2026-11-02 09:00"},
		{"0 9 * * 0", "2026-11-01 09:00"}, // 今天就是周日，09:00 尚未到
	}
	for _, c := range cases {
		s := Schedule{Kind: ScheduleCron, Cron: c.expr}
		if err := s.Validate(); err != nil {
			t.Fatalf("%q validate: %v", c.expr, err)
		}
		next, err := s.Next(base)
		if err != nil {
			t.Fatalf("%q next: %v", c.expr, err)
		}
		if got := next.Format("2006-01-02 15:04"); got != c.want {
			t.Fatalf("%q next = %s; want %s", c.expr, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "0 24 * * *", "0 9 * * 7", "a b c d e"} {
		if err := (Schedule{Kind: ScheduleCron, Cron: bad}).Validate(); err == nil {
			t.Fatalf("%q must fail", bad)
		}
	}
	if got := (Schedule{Kind: ScheduleCron, Cron: "0 9 * * *"}).Summary(); got != "每天 09:00" {
		t.Fatalf("summary = %q", got)
	}
	if got := (Schedule{Kind: ScheduleCron, Cron: "* * 1 * *"}).Summary(); got == "每分钟" {
		t.Fatalf("restricted day must not summarize as every-minute: %q", got)
	}
}
