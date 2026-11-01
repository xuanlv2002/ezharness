/*
plan 是会话级任务计划与进度（plan 工具维护、用户输入框上方展示、
每轮随 agent_status 注入模型）。一套 plan 对应一条分支（RootID）：
compact 换代不丢，分叉各自独立。clear 写空内容，LoadPlan 视为无计划。

计划是结构化三段：Goal（目标）、Approach（方案）、Progress（步骤与状态），
前端卡片按字段分区渲染，不再解析正文里的行。
*/
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
)

/* 进度状态（前端与 agent_status 展示共用）。 */
const (
	PlanPending   = "pending"   // 待执行
	PlanRunning   = "running"   // 执行中
	PlanCompleted = "completed" // 已完成
)

/* PlanFileName 是计划文件名（sessions/<rootID>/plan.json）。 */
const PlanFileName = "plan.json"

/* PlanProgress 是一条进度步骤。 */
type PlanProgress struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

/*
SessionPlan 是会话计划全文（三段式：目标 / 方案 / 步骤）。
clear 后为空结构，LoadPlan 返回 nil。
*/
type SessionPlan struct {
	Goal      string         `json:"goal"`     // 目标：要达成什么（一句话）
	Approach  string         `json:"approach"` // 方案：怎么做与取舍
	Progress  []PlanProgress `json:"progress"` // 步骤：标题 + 状态
	UpdatedAt time.Time      `json:"updatedAt"`
}

/* PlanPath 返回分支的计划文件路径。 */
func PlanPath(rootID string) string {
	return SessionsDir + "/" + rootID + "/" + PlanFileName
}

/* LoadPlan 读计划（无计划/空计划返回 nil）。 */
func LoadPlan(ctx context.Context, fsys fs.FileSystem, rootID string) (*SessionPlan, error) {
	if rootID == "" {
		return nil, nil
	}
	data, err := fsys.Read(ctx, PlanPath(rootID))
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var p SessionPlan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Goal == "" && p.Approach == "" && len(p.Progress) == 0 {
		return nil, nil
	}
	return &p, nil
}

/* SavePlan 覆盖写计划。 */
func SavePlan(ctx context.Context, fsys fs.FileSystem, rootID string, p *SessionPlan) error {
	if p == nil {
		p = &SessionPlan{}
	}
	p.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return fsys.Write(ctx, PlanPath(rootID), b)
}
