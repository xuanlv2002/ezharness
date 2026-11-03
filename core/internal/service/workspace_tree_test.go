package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

/* gitAvailable 无 git 环境（精简容器/CI）跳过 git 用例。 */
func gitAvailable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
}

/* initRepo 建临时 git 仓库并提交基线（独立 user 配置，不碰全局）。 */
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir,
			"-c", "user.email=test@test", "-c", "user.name=test"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestParsePorcelain(t *testing.T) {
	got := parsePorcelain([]byte("M  a.txt\x00?? sub/b.txt\x00\x00"))
	if got == nil || len(got) != 2 {
		t.Fatalf("want 2 条记录，got %v", got)
	}
	if got["a.txt"] != "M " || got["sub/b.txt"] != "??" {
		t.Fatalf("状态码不符: %v", got)
	}
	if parsePorcelain(nil) != nil {
		t.Fatal("空输出应为 nil")
	}
}

func TestTreeAndGitBlob(t *testing.T) {
	gitAvailable(t)
	dir := initRepo(t)
	svc := &WorkspaceService{}

	/* 工作区：a.txt 未提交修改 + untracked 新文件 */
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old line\nnew line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	view, err := svc.Tree(context.Background(), dir)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if view.Path != filepath.ToSlash(dir) {
		t.Fatalf("path 归一不符: %s", view.Path)
	}
	if len(view.Entries) != 2 || view.Entries[0].Name != "a.txt" {
		t.Fatalf("条目应为 a.txt/new.txt（目录优先、字典序）: %+v", view.Entries)
	}
	if len(view.Git) != 1 || view.Git[0].Root != filepath.ToSlash(dir) {
		t.Fatalf("git 信息缺失: %+v", view.Git)
	}
	if view.Git[0].Status["a.txt"] != " M" || view.Git[0].Status["new.txt"] != "??" {
		t.Fatalf("工作区状态不符: %v", view.Git[0].Status)
	}

	/* GitBlob：a.txt 的 HEAD 原文是旧内容；new.txt 未纳入 HEAD */
	old, err := svc.GitBlob(context.Background(), filepath.Join(dir, "a.txt"))
	if err != nil || old != "old line\n" {
		t.Fatalf("HEAD 原文不符: %q %v", old, err)
	}
	if _, err := svc.GitBlob(context.Background(), filepath.Join(dir, "new.txt")); err == nil {
		t.Fatal("untracked 文件应返回 ErrNotExist")
	}
}

func TestTreeWithoutGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &WorkspaceService{}
	view, err := svc.Tree(context.Background(), dir)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(view.Git) != 0 {
		t.Fatalf("周边无仓库 Git 应为空: %+v", view.Git)
	}
	if len(view.Entries) != 1 || view.Entries[0].Name != "plain.txt" {
		t.Fatalf("条目不符: %+v", view.Entries)
	}
}

/*
TestTreeContainerRepos 容器目录（多仓库并列的父目录）：一级子目录里

	的仓库根各报一条（按根排序），非仓库子目录不出现。
*/
func TestTreeContainerRepos(t *testing.T) {
	gitAvailable(t)
	container := t.TempDir()
	for i, src := range []string{initRepo(t), initRepo(t)} {
		if err := os.Rename(src, filepath.Join(container, fmt.Sprintf("repo%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(container, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(container, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	/* repo0 造一条未提交修改，repo0 的状态表应出现它；repo1 保持干净 */
	if err := os.WriteFile(filepath.Join(container, "repo0", "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	view, err := (&WorkspaceService{}).Tree(context.Background(), container)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(view.Git) != 2 {
		t.Fatalf("应报 2 个一级子仓库: %+v", view.Git)
	}
	if view.Git[0].Root >= view.Git[1].Root {
		t.Fatalf("仓库应按根排序: %+v", view.Git)
	}
	if view.Git[0].Status["a.txt"] != " M" {
		t.Fatalf("repo0 工作区状态不符: %v", view.Git[0].Status)
	}
	if view.Git[1].Status == nil || len(view.Git[1].Status) != 0 {
		t.Fatalf("干净的 repo1 应为非 nil 空表: %+v", view.Git[1])
	}
}
