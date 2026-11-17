/*
Stats 是跨会话累计的生命体征（data/stats.json）：首启时间（已服务
天数基准）、累计 usage、轮数。缓存命中率 = cached/prompt。
*/
package domain

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/osfs"
)

const (
	statsFile  = "stats.json"
	statsKeyFY = "2006-01-02T15" // 小时桶键（本地时区，字典序可排序）
)

/* Stats 累计生命体征。 */
type Stats struct {
	mu        sync.Mutex
	fsys      osfs.OS
	firstSeen time.Time
	total     types.Usage
	turns     int
	hours     map[string]*HourStats
}

/* ModelUsage 单模型单小时用量。 */
type ModelUsage struct {
	In    int `json:"in"`
	Out   int `json:"out"`
	Cache int `json:"cache"`
	Turns int `json:"turns"`
}

/* HourStats 单小时聚合（In=输入,Cache=其中缓存命中,Out=输出;Models 按模型拆分）。 */
type HourStats struct {
	Turns  int                    `json:"turns"`
	In     int                    `json:"in"`
	Out    int                    `json:"out"`
	Cache  int                    `json:"cache"`
	Models map[string]*ModelUsage `json:"models,omitempty"`
}

type statsFileLayout struct {
	FirstSeen int64                 `json:"firstSeen"` // UnixMilli
	Turns     int                   `json:"turns"`
	Total     types.Usage           `json:"total"`
	Hours     map[string]*HourStats `json:"hours,omitempty"`
}

/* NewStats 读 stats.json；不存在则以当前时间为首启写盘。 */
func NewStats(fsys osfs.OS) *Stats {
	s := &Stats{fsys: fsys, firstSeen: time.Now(), hours: map[string]*HourStats{}}
	if data, err := fsys.Read(context.Background(), statsFile); err == nil {
		var f statsFileLayout
		if json.Unmarshal(data, &f) == nil && f.FirstSeen > 0 {
			s.firstSeen = time.UnixMilli(f.FirstSeen)
			s.turns = f.Turns
			s.total = f.Total
			if f.Hours != nil {
				s.hours = f.Hours
			}
			return s
		}
	}
	_ = s.saveLocked()
	return s
}

/* CreateStatsFile 缺失时写入初始 stats.json；已存在不动（返回是否新建）。 */
func CreateStatsFile(fsys osfs.OS) (bool, error) {
	if data, err := fsys.Read(context.Background(), statsFile); err == nil && len(data) > 0 {
		return false, nil
	}
	s := &Stats{fsys: fsys, firstSeen: time.Now(), hours: map[string]*HourStats{}}
	return true, s.saveLocked()
}

/* AddTurn 累加一轮用量并落盘（model 为本轮主模型名,按小时按模型分桶）。 */
func (s *Stats) AddTurn(u *types.Usage, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hour := s.hourLocked()
	if u != nil {
		s.total.Add(*u)
		hour.In += u.PromptTokens
		hour.Cache += u.CachedTokens
		hour.Out += u.CompletionTokens
		if model != "" {
			m := hour.Models[model]
			if m == nil {
				m = &ModelUsage{}
				hour.Models[model] = m
			}
			m.In += u.PromptTokens
			m.Cache += u.CachedTokens
			m.Out += u.CompletionTokens
			m.Turns++
		}
	}
	hour.Turns++
	s.turns++
	s.pruneLocked()
	_ = s.saveLocked()
}

func (s *Stats) hourLocked() *HourStats {
	key := time.Now().Format(statsKeyFY)
	h := s.hours[key]
	if h == nil {
		h = &HourStats{Models: map[string]*ModelUsage{}}
		s.hours[key] = h
	}
	return h
}

/* pruneLocked 只保留最近 30 天的小时桶（更早的留在累计总量里）。 */
func (s *Stats) pruneLocked() {
	if len(s.hours) <= 30*24 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -30).Format(statsKeyFY)
	for k := range s.hours {
		if k < cutoff {
			delete(s.hours, k)
		}
	}
}

/* HourView 小时起点（UnixMilli）+ 该小时聚合（Hourly 的下发条目）。 */
type HourView struct {
	Start int64 `json:"start"`
	HourStats
}

/* Hourly 返回按小时升序的用量序列（模型面板趋势图数据源）。 */
func (s *Stats) Hourly() []HourView {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.hours))
	for k := range s.hours {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]HourView, 0, len(keys))
	for _, k := range keys {
		t, err := time.ParseInLocation(statsKeyFY, k, time.Local)
		if err != nil {
			continue
		}
		out = append(out, HourView{Start: t.UnixMilli(), HourStats: *s.hours[k]})
	}
	return out
}

/* Turns 返回累计轮数。 */
func (s *Stats) Turns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns
}

func (s *Stats) saveLocked() error {
	data, err := json.MarshalIndent(statsFileLayout{
		FirstSeen: s.firstSeen.UnixMilli(),
		Turns:     s.turns,
		Total:     s.total,
		Hours:     s.hours,
	}, "", "  ")
	if err != nil {
		return err
	}
	return s.fsys.Write(context.Background(), statsFile, data)
}
