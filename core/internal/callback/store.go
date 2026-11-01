/*
store 是 callbacks.json 的唯一读写口：配置与运行态同文件，所有变更
在进程内 mutex 下 load-modify-save；osfs 的写入是 tmp+rename 原子写。
读侧另提供 LoadTasks，供 system 清单与 resource_change 免写读取。
*/
package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	ezfs "github.com/xuanlv2002/ezloop/ext/fs"
)

/* ErrNotFound 表示任务 ID 不存在。 */
var ErrNotFound = errors.New("callback not found")

/* FileName 是定时回调数据文件名（数据目录相对）。 */
const FileName = "callbacks.json"

/* FS 是 store 需要的最小文件能力（osfs.OS 实现）。 */
type FS interface {
	ezfs.FileSystem
	Write(ctx context.Context, path string, data []byte) error
}

/* Store 持有全部任务（配置 + 运行态）。 */
type Store struct {
	mu    sync.Mutex
	fsys  FS
	tasks []Task
}

/* New 创建空 store（不读盘）。 */
func New(fsys FS) *Store { return &Store{fsys: fsys} }

/*
Load 读取 store：文件缺失或为空视为无任务；内容损坏返回错误，
调用方决定降级（当前代仍可启动，下一次保存会覆盖损坏文件）。
*/
func Load(ctx context.Context, fsys FS) (*Store, error) {
	s := &Store{fsys: fsys}
	data, err := fsys.Read(ctx, FileName)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.tasks); err != nil {
		return s, fmt.Errorf("%s 解析失败: %w", FileName, err)
	}
	return s, nil
}

/* LoadTasks 只读全部任务（system 清单与资源变更用；损坏视为空）。 */
func LoadTasks(ctx context.Context, fsys ezfs.FileSystem) []Task {
	data, err := fsys.Read(ctx, FileName)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var out []Task
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

/* List 返回全部任务的副本。 */
func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.tasks))
	for i := range s.tasks {
		out[i] = cloneTask(s.tasks[i])
	}
	return out
}

/* Get 按 ID 取任务副本。 */
func (s *Store) Get(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return cloneTask(s.tasks[i]), true
		}
	}
	return Task{}, false
}

/* Create 追加任务并落盘（ID 为空时生成）。 */
func (s *Store) Create(ctx context.Context, t Task) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if t.ID == "" {
		t.ID = NewID(now)
	}
	if t.CreatedAt == 0 {
		t.CreatedAt = now.UnixMilli()
	}
	t.UpdatedAt = now.UnixMilli()
	s.tasks = append(s.tasks, t)
	if err := s.saveLocked(ctx); err != nil {
		s.tasks = s.tasks[:len(s.tasks)-1]
		return Task{}, err
	}
	return cloneTask(t), nil
}

/* Update 原地修改任务并落盘；fn 返回错误时不落盘。 */
func (s *Store) Update(ctx context.Context, id string, fn func(*Task) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID != id {
			continue
		}
		before := cloneTask(s.tasks[i])
		if err := fn(&s.tasks[i]); err != nil {
			s.tasks[i] = before // 校验失败不留半更新
			return Task{}, err
		}
		s.tasks[i].UpdatedAt = time.Now().UnixMilli()
		if err := s.saveLocked(ctx); err != nil {
			s.tasks[i] = before
			return Task{}, err
		}
		return cloneTask(s.tasks[i]), nil
	}
	return Task{}, ErrNotFound
}

/* Delete 删除任务并落盘。 */
func (s *Store) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID != id {
			continue
		}
		removed := s.tasks[i]
		s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
		if err := s.saveLocked(ctx); err != nil {
			s.tasks = append(s.tasks, Task{})
			copy(s.tasks[i+1:], s.tasks[i:])
			s.tasks[i] = removed
			return err
		}
		return nil
	}
	return ErrNotFound
}

/*
SetRuntime 只更新运行态字段（不碰配置、不推进 UpdatedAt）：调度器
执行前/后写 next/last/status/run count，页面的编辑操作不会与之互踩。
*/
func (s *Store) SetRuntime(ctx context.Context, id string, fn func(*Task)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID != id {
			continue
		}
		before := cloneTask(s.tasks[i])
		fn(&s.tasks[i])
		if err := s.saveLocked(ctx); err != nil {
			s.tasks[i] = before
			return err
		}
		return nil
	}
	return ErrNotFound
}

/* Save 覆盖写当前任务集。 */
func (s *Store) Save(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(ctx)
}

func (s *Store) saveLocked(ctx context.Context) error {
	if s.tasks == nil {
		s.tasks = []Task{}
	}
	data, err := json.MarshalIndent(s.tasks, "", "  ")
	if err != nil {
		return err
	}
	return s.fsys.Write(ctx, FileName, data)
}

/*
Normalize 在启动装配时校正运行态：上一次进程中断的 running 归为
skipped；错过的一次性任务标记 missed 并停用；间隔/cron 错过不补跑，
从 now 重新计算下次。
*/
func (s *Store) Normalize(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := range s.tasks {
		t := &s.tasks[i]
		if t.LastStatus == StatusRunning {
			t.LastStatus = StatusSkipped
			t.LastResult = "上次执行因应用退出中断"
			changed = true
		}
		if !t.Enabled {
			if t.NextRunAt != 0 {
				t.NextRunAt = 0
				changed = true
			}
			continue
		}
		next := t.NextRunAt
		if next > 0 && next > now.UnixMilli() {
			continue // 下次时间仍有效（间隔/cron 可保留）
		}
		if t.Schedule.Kind == ScheduleOnce {
			// 已触发过（有 LastRunAt）或已过期：一次性任务不再重排
			t.Enabled = false
			t.NextRunAt = 0
			if t.LastStatus == "" {
				t.LastStatus = StatusMissed
				t.LastResult = "计划时间已过（应用未运行）"
			}
			changed = true
			continue
		}
		n, err := t.Schedule.Next(now)
		if err != nil || n.IsZero() {
			t.Enabled = false
			t.NextRunAt = 0
			t.LastStatus = StatusError
			if err != nil {
				t.LastResult = err.Error()
			} else {
				t.LastResult = "计划表达式没有可运行的下一次"
			}
			changed = true
			continue
		}
		if n.UnixMilli() != t.NextRunAt {
			t.NextRunAt = n.UnixMilli()
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked(context.Background())
}

/* cloneTask 深拷贝任务（嵌套目标与触发记录不共享）。 */
func cloneTask(t Task) Task {
	out := t
	if t.Session != nil {
		s := *t.Session
		out.Session = &s
	}
	if t.App != nil {
		a := *t.App
		out.App = &a
	}
	if t.Script != nil {
		s := *t.Script
		out.Script = &s
	}
	if len(t.Runs) > 0 {
		out.Runs = append([]Run(nil), t.Runs...)
	}
	return out
}
