/*
TopicService：话题存档的查询与恢复。
*/
package service

import (
	"context"
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

/* Get 读取话题完整存档。 */
func (t *TopicService) Get(ctx context.Context, id string) (TopicDetail, error) {
	for _, e := range t.Hub.Topics.Load() {
		if e.ID == id {
			s, err := localsession.Load(ctx, t.Hub.Fsys, "", id)
			if err != nil {
				return TopicDetail{}, err
			}
			return TopicDetail{Entry: e, Messages: s.Messages}, nil
		}
	}
	return TopicDetail{}, ErrTopicNotFound
}

/* Delete 删除话题存档（索引条目 + session 文件）。 */
func (t *TopicService) Delete(id string) error {
	if !t.Hub.Topics.Remove(id) {
		return ErrTopicNotFound
	}
	_ = os.Remove(filepath.Join(localsession.DefaultDir, id+".json"))
	return nil
}

/* Resume 回到指定话题继续（历史替换 + session 切换）。 */
func (t *TopicService) Resume(ctx context.Context, id string) error {
	s := t.Hub.Active
	if s.Busy() {
		return domain.ErrBusy
	}
	msgs, err := localsession.Load(ctx, t.Hub.Fsys, "", id)
	if err != nil {
		return ErrTopicNotFound
	}
	s.Sess.SetID(id)
	s.SetIdentity(id)
	s.ReplaceHistory(msgs.Messages)
	return nil
}
