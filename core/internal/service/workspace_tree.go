/*
workspace_tree 是资源页目录树的可观测用例：目录列表、所在 git 仓库的
工作区变更清单、文件在 HEAD 的原文（左右对比的左侧）。全部只读——
不做任何 git 写操作；git 缺失或目录不在仓库内时降级为无 git 信息。
*/
package service

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

/* TreeEntry 是目录树的一个条目（目录树懒加载按层拉取，只给名字与类型）。 */
type TreeEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

/* GitInfo 是目录所在仓库的可观测信息：仓库根 + 工作区变更表
（键 = 仓库相对斜杠路径，值 = porcelain 两字符状态码如 "M "/"??"）。
Status 为 nil 表示本次状态获取失败（git 缺失/锁冲突），非"无变更"。 */
type GitInfo struct {
	Root   string            `json:"root"`
	Status map[string]string `json:"status"`
}

/* TreeView 是一层目录的列表响应（Path 归一为正斜杠绝对路径）。 */
type TreeView struct {
	Path    string      `json:"path"`
	Entries []TreeEntry `json:"entries"`
	Git     *GitInfo    `json:"git"`
}

/* DefaultWorkDir 返回解析后的工作目录绝对路径（目录树默认根）。 */
func (w *WorkspaceService) DefaultWorkDir() string {
	return filepath.ToSlash(ResolveWorkDir(w.Hub.SettingsSnapshot().WorkDir))
}

/*
Tree 列出目录的一级条目 + 所在 git 仓库的工作区变更。条目按目录优先、
名称排序；仓库内的 .git 目录不列出。
*/
func (w *WorkspaceService) Tree(ctx context.Context, dir string) (TreeView, error) {
	abs, err := filepath.Abs(filepath.FromSlash(dir))
	if err != nil {
		return TreeView{}, os.ErrInvalid
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return TreeView{}, os.ErrInvalid
	}
	dirs, err := os.ReadDir(abs)
	if err != nil {
		return TreeView{}, err
	}
	entries := make([]TreeEntry, 0, len(dirs))
	for _, d := range dirs {
		if d.IsDir() && d.Name() == ".git" {
			continue // 仓库元数据目录不是资源
		}
		entries = append(entries, TreeEntry{Name: d.Name(), Dir: d.IsDir()})
	}
	sortTreeEntries(entries)
	return TreeView{
		Path:    filepath.ToSlash(abs),
		Entries: entries,
		Git:     gitInfo(ctx, abs),
	}, nil
}

/* sortTreeEntries 目录优先、名称不区分大小写排序。 */
func sortTreeEntries(entries []TreeEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

/*
GitBlob 读取文件在 HEAD 的原文（变更对比左侧）。文件不在 git 仓库内
或未纳入 HEAD（新文件）时返回 os.ErrNotExist，调用方按 404 应答。
*/
func (w *WorkspaceService) GitBlob(ctx context.Context, abs string) (string, error) {
	abs, err := filepath.Abs(filepath.FromSlash(abs))
	if err != nil {
		return "", os.ErrInvalid
	}
	root := findRepoRoot(filepath.Dir(abs))
	if root == "" {
		return "", os.ErrNotExist
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", os.ErrNotExist
	}
	out, err := gitRun(ctx, root, "show", "HEAD:"+filepath.ToSlash(rel))
	if err != nil {
		return "", os.ErrNotExist
	}
	if len(out) > maxGitBlobBytes {
		return "", os.ErrInvalid // 异常大文件不进对比视图
	}
	return string(out), nil
}

/* maxGitBlobBytes HEAD 原文的读取上限（对比视图新侧本就限 2MB 内）。 */
const maxGitBlobBytes = 8 << 20

/*
gitInfo 探测目录所在仓库并取工作区变更。仓库判定只看 .git 存在
（确定性，不闪）；状态命令失败时仍返回 GitInfo 但 Status 为 nil——
调用方（前端）保留上次结果，避免 AI/终端并发用 git 时锁冲突导致
徽标闪烁。
*/
func gitInfo(ctx context.Context, dir string) *GitInfo {
	root := findRepoRoot(dir)
	if root == "" {
		return nil
	}
	/* --no-optional-locks：观测性读取不拿可选锁（不 refresh index），
	   与用户/AI 的真实 git 操作互不阻塞 */
	out, err := gitRun(ctx, root, "--no-optional-locks", "status", "--porcelain=v1", "-z",
		"--untracked-files=all", "--no-renames")
	if err != nil {
		return &GitInfo{Root: filepath.ToSlash(root)}
	}
	status := parsePorcelain(out)
	if status == nil {
		status = map[string]string{}
	}
	return &GitInfo{Root: filepath.ToSlash(root), Status: status}
}

/* findRepoRoot 自目录向上找仓库根（.git 目录或文件皆可，worktree/submodule）。 */
func findRepoRoot(dir string) string {
	dir = filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

/*
gitRun 在 root 执行只读 git 命令并返回 stdout。git 未安装/非仓库时
返回错误（调用方降级）。
*/
func gitRun(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

/*
parsePorcelain 解析 `status --porcelain=v1 -z` 输出：记录以 NUL 分隔，
每条为两字符状态码 + 空格 + 仓库相对路径（--no-renames 下没有重命名
记录的双路径形态）。值保留原始两字符码，展示侧再归一为徽标字母。
*/
func parsePorcelain(out []byte) map[string]string {
	records := strings.Split(string(out), "\x00")
	status := make(map[string]string, len(records))
	for _, r := range records {
		if len(r) < 4 || r[2] != ' ' {
			continue
		}
		code, path := r[:2], filepath.ToSlash(r[3:])
		if code == "!!" || path == "" {
			continue // --ignored 未开不会出现，防御性跳过
		}
		status[path] = code
	}
	if len(status) == 0 {
		return nil
	}
	return status
}
