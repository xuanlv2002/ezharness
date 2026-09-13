/*
Stats 是跨会话累计的生命体征（data/stats.json）：首启时间（已服务
天数基准）、累计 usage、轮数。缓存命中率 = cached/prompt。
*/
package domain

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/osfs"
)

const statsFile = "stats.json"

/* Stats 累计生命体征。 */
type Stats struct {
	mu        sync.Mutex
	fsys      osfs.OS
	firstSeen time.Time
	total     types.Usage
	turns     int
}

/* StatsView 是 stats 的下发视图。 */
type StatsView struct {
	FirstSeen int64 `json:"firstSeen"`
	Turns     int   `json:"turns"`
}

func (s *Stats) viewLocked() StatsView {
	return StatsView{FirstSeen: s.firstSeen.UnixMilli(), Turns: s.turns}
}

type statsFileLayout struct {
	FirstSeen int64       `json:"firstSeen"` // UnixMilli
	Turns     int         `json:"turns"`
	Total     types.Usage `json:"total"`
}

/* NewStats 读 stats.json；不存在则以当前时间为首启写盘。 */
func NewStats(fsys osfs.OS) *Stats {
	s := &Stats{fsys: fsys, firstSeen: time.Now()}
	if data, err := fsys.Read(context.Background(), statsFile); err == nil {
		var f statsFileLayout
		if json.Unmarshal(data, &f) == nil && f.FirstSeen > 0 {
			s.firstSeen = time.UnixMilli(f.FirstSeen)
			s.turns = f.Turns
			s.total = f.Total
			return s
		}
	}
	s.saveLocked()
	return s
}

/* AddTurn 累加一轮用量并落盘。 */
func (s *Stats) AddTurn(u *types.Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u != nil {
		s.total.Add(*u)
	}
	s.turns++
	s.saveLocked()
}

/* Total 返回累计用量副本。 */
func (s *Stats) Total() types.Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

/* Turns 返回累计轮数。 */
func (s *Stats) Turns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns
}

/* DaysServed 返回已服务天数（首启起算，当天为 0）。 */
func (s *Stats) DaysServed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int(time.Since(s.firstSeen).Hours() / 24)
}

/* CacheHitRate 返回缓存命中率（cached/prompt，prompt 为 0 时 0）。 */
func (s *Stats) CacheHitRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total.PromptTokens == 0 {
		return 0
	}
	return float64(s.total.CachedTokens) / float64(s.total.PromptTokens)
}

func (s *Stats) saveLocked() {
	data, err := json.MarshalIndent(statsFileLayout{
		FirstSeen: s.firstSeen.UnixMilli(),
		Turns:     s.turns,
		Total:     s.total,
	}, "", "  ")
	if err != nil {
		return
	}
	_ = s.fsys.Write(context.Background(), statsFile, data)
}
