/*
schedule 是定时回调的计划表达式：一次（once）、固定间隔（interval）与
5 段 cron。时间一律按本机时区解释；cron 语义与常见实现一致：分 时 日 月 周，
周日=0；日与周都受限时按“或”匹配，任一为 * 时按“与”匹配。
*/
package callback

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

/* 计划类型。 */
const (
	ScheduleOnce     = "once"     // 单次：at 指定本地时间
	ScheduleInterval = "interval" // 固定间隔：every 为 Go duration（30m/2h）
	ScheduleCron     = "cron"     // 日历：cron 为 5 段表达式
)

const (
	minInterval = 10 * time.Second
	maxInterval = 365 * 24 * time.Hour
	cronHorizon = 5 * 366 * 24 * time.Hour // 下次匹配的搜索上限
)

/* Schedule 是计划表达式（落盘与 API 共用）。 */
type Schedule struct {
	Kind  string `json:"kind" desc:"once | interval | cron"`
	At    string `json:"at,omitempty" desc:"once：本地时间，如 2026-11-01 09:00"`
	Every string `json:"every,omitempty" desc:"interval：Go 时长，如 30m、2h、24h（≥10s）"`
	Cron  string `json:"cron,omitempty" desc:"cron：5 段「分 时 日 月 周」，如 0 9 * * *"`
}

/* Normalize 去除空白并归一化类型与 cron 表达式。 */
func (s *Schedule) Normalize() {
	s.Kind = strings.ToLower(strings.TrimSpace(s.Kind))
	s.At = strings.TrimSpace(s.At)
	s.Every = strings.TrimSpace(s.Every)
	s.Cron = strings.Join(strings.Fields(s.Cron), " ")
}

/* Validate 校验计划表达式（不判断时间是否已过）。 */
func (s Schedule) Validate() error {
	switch s.Kind {
	case ScheduleOnce:
		if _, err := parseOnce(s.At); err != nil {
			return err
		}
	case ScheduleInterval:
		_, err := parseInterval(s.Every)
		return err
	case ScheduleCron:
		_, err := parseCron(s.Cron)
		return err
	default:
		return fmt.Errorf("schedule.kind 需为 once、interval 或 cron")
	}
	return nil
}

/*
Next 返回严格晚于 after 的下一次运行时间；一次性计划已过期返回零值。
interval 从 after 起算（执行完按完成时刻重新计时）。
*/
func (s Schedule) Next(after time.Time) (time.Time, error) {
	switch s.Kind {
	case ScheduleOnce:
		at, err := parseOnce(s.At)
		if err != nil {
			return time.Time{}, err
		}
		if !at.After(after) {
			return time.Time{}, nil
		}
		return at, nil
	case ScheduleInterval:
		d, err := parseInterval(s.Every)
		if err != nil {
			return time.Time{}, err
		}
		return after.Add(d), nil
	case ScheduleCron:
		c, err := parseCron(s.Cron)
		if err != nil {
			return time.Time{}, err
		}
		return c.next(after), nil
	default:
		return time.Time{}, fmt.Errorf("schedule.kind 需为 once、interval 或 cron")
	}
}

/* Summary 渲染人可读的计划摘要（system 清单、页面与工具结果共用）。 */
func (s Schedule) Summary() string {
	switch s.Kind {
	case ScheduleOnce:
		if at, err := parseOnce(s.At); err == nil {
			return "一次性 " + at.Format("2006-01-02 15:04")
		}
		return "一次性 " + s.At
	case ScheduleInterval:
		if d, err := parseInterval(s.Every); err == nil {
			return humanInterval(d)
		}
		return "每 " + s.Every
	case ScheduleCron:
		return cronSummary(s.Cron)
	}
	return s.Kind
}

/* onceLayouts 是 once 可接受的本地时间写法（RFC3339 带时区，其余按本机时区）。 */
var onceLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	"2006-01-02 15:04:05",
}

/* parseOnce 解析一次性时间。 */
func parseOnce(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("schedule.at 不能为空（如 2026-11-01 09:00）")
	}
	for _, layout := range onceLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("schedule.at 时间格式无法识别: %q", s)
}

/* parseInterval 解析固定间隔。 */
func parseInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("schedule.every 不能为空（如 30m、2h）")
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("schedule.every 需要 Go 时长（如 30m、2h）: %w", err)
	}
	if d < minInterval {
		return 0, fmt.Errorf("schedule.every 不能小于 %s", minInterval)
	}
	if d > maxInterval {
		return 0, fmt.Errorf("schedule.every 不能大于 %s", maxInterval)
	}
	return d, nil
}

/* humanInterval 把间隔渲染成中文短语。 */
func humanInterval(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("每 %d 天", int(d/(24*time.Hour)))
	case d%time.Hour == 0:
		return fmt.Sprintf("每 %d 小时", int(d/time.Hour))
	case d%time.Minute == 0:
		return fmt.Sprintf("每 %d 分钟", int(d/time.Minute))
	case d%time.Second == 0:
		return fmt.Sprintf("每 %d 秒", int(d/time.Second))
	default:
		return "每 " + d.String()
	}
}

/* cronExpr 是解析后的 5 段 cron（位图，位下标即字段值）。 */
type cronExpr struct {
	minute, hour, dom, month, dow uint64
	minuteStar, hourStar          bool
	domStar, monthStar, dowStar   bool
}

/* parseCron 解析 5 段 cron：分 时 日 月 周。 */
func parseCron(expr string) (cronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronExpr{}, fmt.Errorf("cron 需要 5 段（分 时 日 月 周）: %q", expr)
	}
	var c cronExpr
	var err error
	if c.minute, c.minuteStar, err = parseCronField(fields[0], 0, 59); err != nil {
		return cronExpr{}, fmt.Errorf("cron 分字段: %w", err)
	}
	if c.hour, c.hourStar, err = parseCronField(fields[1], 0, 23); err != nil {
		return cronExpr{}, fmt.Errorf("cron 时字段: %w", err)
	}
	if c.dom, c.domStar, err = parseCronField(fields[2], 1, 31); err != nil {
		return cronExpr{}, fmt.Errorf("cron 日字段: %w", err)
	}
	if c.month, c.monthStar, err = parseCronField(fields[3], 1, 12); err != nil {
		return cronExpr{}, fmt.Errorf("cron 月字段: %w", err)
	}
	if c.dow, c.dowStar, err = parseCronField(fields[4], 0, 6); err != nil {
		return cronExpr{}, fmt.Errorf("cron 周字段: %w", err)
	}
	return c, nil
}

// parseCronField 解析单个字段：星号、单值、范围、步长与逗号列表。
// 返回值 star 表示整个字段是星号（日/周的“或/与”语义要用）。
func parseCronField(field string, min, max int) (uint64, bool, error) {
	if field == "*" {
		var v uint64
		for i := min; i <= max; i++ {
			v |= 1 << uint(i)
		}
		return v, true, nil
	}
	var out uint64
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return 0, false, fmt.Errorf("空字段片段")
		}
		rangePart, step, err := splitCronStep(part)
		if err != nil {
			return 0, false, err
		}
		lo, hi, err := splitCronRange(rangePart, min, max)
		if err != nil {
			return 0, false, err
		}
		for v := lo; v <= hi; v += step {
			out |= 1 << uint(v)
		}
	}
	if out == 0 {
		return 0, false, fmt.Errorf("字段没有有效值")
	}
	return out, false, nil
}

/* splitCronStep 拆出 "/步长" 部分。 */
func splitCronStep(part string) (string, int, error) {
	rangePart := part
	step := 1
	if i := strings.Index(part, "/"); i >= 0 {
		rangePart = strings.TrimSpace(part[:i])
		n, err := strconv.Atoi(strings.TrimSpace(part[i+1:]))
		if err != nil || n <= 0 {
			return "", 0, fmt.Errorf("非法步长 %q", part[i+1:])
		}
		step = n
	}
	return rangePart, step, nil
}

/* splitCronRange 解析单值、范围或 *。 */
func splitCronRange(s string, min, max int) (int, int, error) {
	if s == "*" {
		return min, max, nil
	}
	if i := strings.Index(s, "-"); i >= 0 {
		lo, err1 := strconv.Atoi(strings.TrimSpace(s[:i]))
		hi, err2 := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		if err1 != nil || err2 != nil || lo < min || hi > max || lo > hi {
			return 0, 0, fmt.Errorf("非法范围 %q（允许 %d-%d）", s, min, max)
		}
		return lo, hi, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < min || n > max {
		return 0, 0, fmt.Errorf("非法取值 %q（允许 %d-%d）", s, min, max)
	}
	return n, n, nil
}

/* next 返回严格晚于 after 的下一个匹配分钟（找不到返回零值）。 */
func (c cronExpr) next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(cronHorizon)
	for !t.After(limit) {
		if c.match(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

/* match 判断某时刻是否匹配（日/周双受限时按标准 cron 的“或”语义）。 */
func (c cronExpr) match(t time.Time) bool {
	if !hasBit(c.minute, t.Minute()) || !hasBit(c.hour, t.Hour()) || !hasBit(c.month, int(t.Month())) {
		return false
	}
	domOK := hasBit(c.dom, t.Day())
	dowOK := hasBit(c.dow, int(t.Weekday()))
	if c.domStar || c.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK
}

/* hasBit 判断位图是否含某值。 */
func hasBit(set uint64, v int) bool { return set&(1<<uint(v)) != 0 }

/* cronSummary 把常见 cron 渲染成中文，其余回退原文。 */
func cronSummary(expr string) string {
	c, err := parseCron(expr)
	if err != nil {
		return "cron " + expr
	}
	if c.minuteStar && c.hourStar && c.domStar && c.monthStar && c.dowStar {
		return "每分钟"
	}
	if !c.minuteStar && !c.hourStar && c.domStar && c.monthStar && c.dowStar &&
		bits.OnesCount64(c.minute) == 1 && bits.OnesCount64(c.hour) == 1 {
		return fmt.Sprintf("每天 %02d:%02d", bits.TrailingZeros64(c.hour), bits.TrailingZeros64(c.minute))
	}
	return "cron " + expr
}
