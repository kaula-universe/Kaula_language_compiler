package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// 只应收录含 #[test] 的 .kl 文件
func TestDiscoverTestFilesFiltersNonTests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.kl"), "#[test]\nfn t() {}\n")
	writeFile(t, filepath.Join(root, "b.kl"), "fn main() {}\n")     // 无 #[test]
	writeFile(t, filepath.Join(root, "c.txt"), "#[test]\nnot kaula") // 非 .kl
	writeFile(t, filepath.Join(root, "sub", "d.kl"), "#[test]\nfn t2() {}\n")

	files, err := discoverTestFiles(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(files) != 2 {
		var names []string
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		t.Fatalf("want 2 test files (a.kl, sub/d.kl), got %d: %v", len(files), names)
	}
	if filepath.Base(files[0]) != "a.kl" || filepath.Base(files[1]) != "d.kl" {
		t.Errorf("results must be sorted: got %v, %v", files[0], files[1])
	}
}

// 构建产物目录必须被跳过，否则会把生成物当测试
func TestDiscoverTestFilesSkipsBuildDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.kl"), "#[test]\nfn t() {}\n")
	writeFile(t, filepath.Join(root, "build", "gen.kl"), "#[test]\nfn t() {}\n")
	writeFile(t, filepath.Join(root, "cache", "c.kl"), "#[test]\nfn t() {}\n")
	writeFile(t, filepath.Join(root, ".git", "g.kl"), "#[test]\nfn t() {}\n")

	files, err := discoverTestFiles(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "real.kl" {
		var names []string
		for _, f := range files {
			names = append(names, f)
		}
		t.Errorf("build/cache/.git must be skipped, got %v", names)
	}
}

// 指向单个文件时应直接返回该文件（不再按目录递归）
func TestDiscoverTestFilesAcceptsSingleFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "solo.kl")
	writeFile(t, target, "#[test]\nfn t() {}\n")
	writeFile(t, filepath.Join(root, "other.kl"), "#[test]\nfn t2() {}\n")

	files, err := discoverTestFiles(target)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(files) != 1 || files[0] != target {
		t.Errorf("want exactly [%s], got %v", target, files)
	}
}

// 不存在的路径必须报错，交给上层输出可读提示
func TestDiscoverTestFilesMissingPath(t *testing.T) {
	if _, err := discoverTestFiles(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("want error for missing path")
	}
}

// 只有「恰好一个已存在的 .kl 文件」才走单文件流程
func TestIsSingleFileTarget(t *testing.T) {
	dir := t.TempDir()
	kl := filepath.Join(dir, "one.kl")
	writeFile(t, kl, "#[test]\nfn t() {}\n")
	other := filepath.Join(dir, "two.kl")
	writeFile(t, other, "#[test]\nfn t() {}\n")

	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"single existing .kl", []string{kl}, true},
		{"no args", nil, false},
		{"directory", []string{dir}, false},
		{"two files", []string{kl, other}, false},
		{"missing file", []string{filepath.Join(dir, "gone.kl")}, false},
		{"non-kl single file", []string{filepath.Join(dir, "x.txt")}, false},
	}
	for _, tc := range cases {
		if got := isSingleFileTarget(tc.args); got != tc.want {
			t.Errorf("%s: isSingleFileTarget(%v) = %v, want %v", tc.name, tc.args, got, tc.want)
		}
	}
}
