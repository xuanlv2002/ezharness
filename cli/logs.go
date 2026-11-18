/*
日志跟随：先回放最新日志文件尾部 40 行，再轮询增量；core 换代换文件时
自动切到最新。
*/
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

/* newestLog 最新修改的 *.log；无日志返回空。 */
func newestLog(logDir string) string {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Slice(names, func(i, j int) bool {
		a, _ := entriesByName(entries, names[i])
		b, _ := entriesByName(entries, names[j])
		return a.ModTime().After(b.ModTime())
	})
	return filepath.Join(logDir, names[0])
}

func entriesByName(entries []os.DirEntry, name string) (os.FileInfo, error) {
	for _, e := range entries {
		if e.Name() == name {
			return e.Info()
		}
	}
	return nil, os.ErrNotExist
}

/* tailLogs 回放尾部并持续跟随，Ctrl+C 退出。 */
func tailLogs(logDir string) {
	cur := ""
	var offset int64
	printBack := func(path string) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		var lines []string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if len(lines) > 40 {
			lines = lines[len(lines)-40:]
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		st, _ := f.Stat()
		if st != nil {
			offset = st.Size()
		}
	}
	for {
		latest := newestLog(logDir)
		if latest == "" {
			fmt.Fprintf(os.Stderr, "日志目录里没有 *.log：%s\n", logDir)
			return
		}
		if latest != cur {
			cur = latest
			fmt.Printf("── 跟随 %s ──\n", filepath.Base(cur))
			printBack(cur)
		} else if st, err := os.Stat(cur); err == nil && st.Size() != offset {
			if st.Size() < offset {
				offset = 0 // 文件被清空/重建：从头跟
			}
			f, err := os.Open(cur)
			if err == nil {
				buf := make([]byte, st.Size()-offset)
				if n, err := f.ReadAt(buf, offset); err == nil || n > 0 {
					fmt.Print(string(buf[:n]))
				}
				f.Close()
				offset = st.Size()
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}
