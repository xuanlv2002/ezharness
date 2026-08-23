/*
TopicService：话题存档的查询与恢复。新存档在 sessions/<id>/session.json
（含压缩摘要与原始路径）；旧版平铺 sessions/<id>.json 作回落兼容。
*/
package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/xuanlv2002/ezloop/ext/hook/localsession"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* ErrTopicNotFound 话题存档不存在。 */
var ErrTopicNotFound = errors.New("topic not found")

/* TopicService 话题用例。 */
type TopicService struct {
	Hub *domain.Hub
}

/* List 返回话题索引。 */
func (t *TopicService) List() []hooks.TopicEntry { return t.Hub.Topics.Load() }

/* TopicDetail 是话题完整存档。 */
type TopicDetail struct {
	Entry    hooks.TopicEntry `json:"entry"`
	Messages []types.Message  `json:"messages"`
}

/* Get 读取话题完整存档（新文件夹布局优先，旧平铺回落）。 */
func (t *TopicService) Get(ctx context.Context, id string) (TopicDetail, error) {
	for _, e := range t.Hub.Topics.Load() {
		if e.ID == id {
			if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id); err == nil {
				return TopicDetail{Entry: e, Messages: snap.Messages}, nil
			}
			s, err := localsession.Load(ctx, t.Hub.Fsys, "", id)
			if err != nil {
				return TopicDetail{}, err
			}
			return TopicDetail{Entry: e, Messages: s.Messages}, nil
		}
	}
	return TopicDetail{}, ErrTopicNotFound
}

/* Delete 删除话题存档（索引条目 + session 目录/旧文件）。 */
func (t *TopicService) Delete(id string) error {
	if !t.Hub.Topics.Remove(id) {
		return ErrTopicNotFound
	}
	_ = os.RemoveAll(filepath.Join(hooks.SessionsDir, id))
	_ = os.Remove(filepath.Join(localsession.DefaultDir, id+".json"))
	return nil
}

/*
Resume 回到指定 session 继续话题：同 ID 直接续写（话题页=session 管理，
接受回到过去导致的压缩链后代部分不同步），历史/system/trace/压缩链
整体切换（与 bootstrap 恢复同构），洗净封存标记使其成为活动会话。
*/
func (t *TopicService) Resume(ctx context.Context, id string) error {
	s := t.Hub.Active
	if s.Busy() {
		return domain.ErrBusy
	}
	snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id)
	if err != nil {
		return ErrTopicNotFound
	}
	s.Sess.SetID(id)
	s.SetIdentity(id)
	s.ReplaceHistory(snap.Messages)
	s.Sess.SetResSnap(snap.Snapshot)
	s.Sess.SetLastOutputAt(snap.LastOutputAt)
	s.Sess.SeedUsage(snap.Usage) // SetID 已清零，按目标快照重注
	if snap.CtxTokens > 0 {
		s.SetCtxTokens(snap.CtxTokens)
	} else {
		s.SetCtxTokens(hooks.LastCtxTokens(snap.Messages))
	}
	if snap.PrevSession != "" { // 压缩链（SetID 已清空，需重设）
		s.Sess.SetPrev(snap.PrevSession, snap.CompactSummary)
	}
	if w := s.Wired(); w != nil && w.Trace != nil {
		w.Trace.SetTrace(id)
	}
	if sys := s.SysPromptRef(); sys != nil {
		sys.Set(snap.SystemBase, snap.SummaryBlock)
	}
	if snap.Archived { // 恢复即活动：洗净封存标记
		snap.Archived = false
		t.saveSnap(ctx, snap)
	}
	return nil
}

/* saveSnap 写回会话快照（失败静默）。 */
func (t *TopicService) saveSnap(ctx context.Context, snap *hooks.SessionSnap) {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	_ = t.Hub.Fsys.Write(ctx, hooks.SessionsDir+"/"+snap.ID+"/session.json", data)
}
