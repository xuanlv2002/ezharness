/*
plan 是会话级任务计划与进度（plan 工具维护、用户输入框上方展示、
每轮随 agent_status 注入模型）。一套 plan 对应一条分支（RootID）：
compact 换代不丢，分叉各自独立。clear 写空内容，LoadPlan 视为无计划。

计划是一份可执行的方案文档，五段：Goal（目标）、Context（背景与边界）、
Design（方案设计）、Progress（步骤与进度）、Verify（验证方式）——
对照 plan mode 的方案文档，多的只有 Progress 这一个进度维度。
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
SessionPlan 是会话计划全文（五段：目标 / 背景 / 方案 / 步骤 / 验证）。
clear 后为空结构，LoadPlan 返回 nil。
*/
type SessionPlan struct {
	Goal      string         `json:"goal"`     // 目标：要达成什么（一句话）
	Context   string         `json:"context"`  // 背景：为什么做、约束、明确不做什么
	Design    string         `json:"design"`   // 方案：具体怎么做（分节，含涉及文件）
	Progress  []PlanProgress `json:"progress"` // 步骤：标题 + 状态（唯一带进度的维度）
	Verify    string         `json:"verify"`   // 验证：怎么确认做对了
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
	if p.Goal == "" && p.Context == "" && p.Design == "" && p.Verify == "" && len(p.Progress) == 0 {
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
