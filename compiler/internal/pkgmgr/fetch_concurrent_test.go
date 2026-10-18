package pkgmgr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kaula/internal/config"
)

// 用本地 git 仓库验证 UpdateDependencies / FetchDependencies 对
// git 依赖（dep: {git: "file://..."}）的并发拉取路径。
// git 依赖不走 registry 探测，直接 clone URL，本地 file:// 可完整闭环。
func TestFetchDependencies_ConcurrentGitDeps(t *testing.T) {
	root := t.TempDir()

	makePkg := func(name string) string {
		dir := filepath.Join(root, name)
		mustRun(t, "git", "init", "-q", dir)
		mustRun(t, "git", "-C", dir, "config", "user.email", "t@t.com")
		mustRun(t, "git", "-C", dir, "config", "user.name", "t")
		os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{"name":"`+name+`"}`), 0644)
		mustRun(t, "git", "-C", dir, "add", ".")
		mustRun(t, "git", "-C", dir, "commit", "-qm", "v1.0.0")
		mustRun(t, "git", "-C", dir, "tag", "v1.0.0")
		return dir
	}

	alphaDir := makePkg("alpha")
	betaDir := makePkg("beta")
	gammaDir := makePkg("gamma")

	deps := map[string]config.Dependency{
		"alpha": {Git: "file://" + filepath.ToSlash(alphaDir)},
		"beta":  {Git: "file://" + filepath.ToSlash(betaDir)},
		"gamma": {Git: "file://" + filepath.ToSlash(gammaDir)},
	}

	projectDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", t.TempDir())
	defer os.Setenv("HOME", oldHome)

	results, lock, err := FetchDependencies("", deps, projectDir)
	if err != nil {
		t.Fatalf("FetchDependencies: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	want := []string{"alpha", "beta", "gamma"}
	got := []string{results[0].Name, results[1].Name, results[2].Name}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	for _, r := range results {
		if r.Version == "" {
			t.Fatalf("%s has empty version", r.Name)
		}
		if _, err := os.Stat(filepath.Join(r.Dir, r.Name+".json")); err != nil {
			t.Fatalf("%s pkg file missing: %v", r.Name, err)
		}
	}
	for _, name := range want {
		if ver := lock.GetVersion(name); ver == "" {
			t.Fatalf("lock[%s] empty", name)
		}
	}
}

// UpdateDependencies 并发更新 git 依赖。
func TestUpdateDependencies_ConcurrentGitDeps(t *testing.T) {
	root := t.TempDir()
	makePkg := func(name string) string {
		dir := filepath.Join(root, name)
		mustRun(t, "git", "init", "-q", dir)
		mustRun(t, "git", "-C", dir, "config", "user.email", "t@t.com")
		mustRun(t, "git", "-C", dir, "config", "user.name", "t")
		os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{"name":"`+name+`"}`), 0644)
		mustRun(t, "git", "-C", dir, "add", ".")
		mustRun(t, "git", "-C", dir, "commit", "-qm", "v1.0.0")
		mustRun(t, "git", "-C", dir, "tag", "v1.0.0")
		return dir
	}
	alphaDir := makePkg("alpha")
	betaDir := makePkg("beta")

	deps := map[string]config.Dependency{
		"alpha": {Git: "file://" + filepath.ToSlash(alphaDir)},
		"beta":  {Git: "file://" + filepath.ToSlash(betaDir)},
	}
	projectDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", t.TempDir())
	defer os.Setenv("HOME", oldHome)

	results, lock, err := UpdateDependencies("", deps, projectDir)
	if err != nil {
		t.Fatalf("UpdateDependencies: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Name != "alpha" || results[1].Name != "beta" {
		t.Fatalf("order wrong: %v", []string{results[0].Name, results[1].Name})
	}
	for _, name := range []string{"alpha", "beta"} {
		if ver := lock.GetVersion(name); ver == "" {
			t.Fatalf("lock[%s] empty", name)
		}
	}
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}
