package pkgpublish

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- ParsePublishArgs ---

func TestParsePublishArgsDefaults(t *testing.T) {
	opts, err := ParsePublishArgs(nil)
	if err != nil {
		t.Fatalf("ParsePublishArgs(nil) error: %v", err)
	}
	if opts.Dir != "." {
		t.Errorf("Dir = %q, want %q", opts.Dir, ".")
	}
	if len(opts.Targets) != 2 || opts.Targets[0] != TargetGitee || opts.Targets[1] != TargetGithub {
		t.Errorf("Targets = %v, want [gitee github]", opts.Targets)
	}
	if opts.Force || opts.DryRun {
		t.Errorf("Force/DryRun should default false, got %v/%v", opts.Force, opts.DryRun)
	}
}

func TestParsePublishArgsFull(t *testing.T) {
	opts, err := ParsePublishArgs([]string{
		"../mylib",
		"--target", "github,gitee,github",
		"--org", "kaula-universe",
		"--tag", "v1.2.3",
		"--gitee-token", "g_abc",
		"--github-token", "gh_xyz",
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if opts.Dir != "../mylib" {
		t.Errorf("Dir = %q", opts.Dir)
	}
	if len(opts.Targets) != 2 || opts.Targets[0] != TargetGithub || opts.Targets[1] != TargetGitee {
		t.Errorf("Targets dedup/order wrong: %v", opts.Targets)
	}
	if opts.OrgOverride != "kaula-universe" || opts.Tag != "v1.2.3" {
		t.Errorf("org=%q tag=%q", opts.OrgOverride, opts.Tag)
	}
	if opts.GiteeToken != "g_abc" || opts.GithubToken != "gh_xyz" {
		t.Errorf("tokens wrong: %q %q", opts.GiteeToken, opts.GithubToken)
	}
	if !opts.DryRun {
		t.Error("DryRun = false, want true")
	}
}

func TestParsePublishArgsEqualsForms(t *testing.T) {
	opts, err := ParsePublishArgs([]string{"--target=github", "--tag=0.5.0", "--force"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(opts.Targets) != 1 || opts.Targets[0] != TargetGithub {
		t.Errorf("Targets = %v", opts.Targets)
	}
	if opts.Tag != "0.5.0" || !opts.Force {
		t.Errorf("tag=%q force=%v", opts.Tag, opts.Force)
	}
}

func TestParsePublishArgsUnknownFlag(t *testing.T) {
	_, err := ParsePublishArgs([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	var ue *UsageError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *UsageError, got %T: %v", err, err)
	}
}

func TestParsePublishArgsInvalidTag(t *testing.T) {
	_, err := ParsePublishArgs([]string{"--tag", "not-a-version"})
	if err == nil {
		t.Fatal("expected error for invalid tag")
	}
}

func TestParsePublishArgsMissingValue(t *testing.T) {
	if _, err := ParsePublishArgs([]string{"--target"}); err == nil {
		t.Fatal("expected error for --target without value")
	}
	if _, err := ParsePublishArgs([]string{"--org"}); err == nil {
		t.Fatal("expected error for --org without value")
	}
}

func TestParseTargets(t *testing.T) {
	cases := []struct {
		in   string
		want []Target
		err  bool
	}{
		{"", []Target{TargetGitee, TargetGithub}, false},
		{"gitee", []Target{TargetGitee}, false},
		{"github", []Target{TargetGithub}, false},
		{"github,gitee", []Target{TargetGithub, TargetGitee}, false},
		{"github,github", []Target{TargetGithub}, false},
		{" Github , Gitee ", []Target{TargetGithub, TargetGitee}, false},
		{"gitlab", nil, true},
	}
	for _, c := range cases {
		got, err := ParseTargets(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseTargets(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTargets(%q) error: %v", c.in, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("ParseTargets(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseTargets(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

// --- LoadPackageMeta ---

func TestLoadPackageMetaWithConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kaula.json"), `{"name":"mylib","version":"0.3.1","description":"demo","license":"MIT"}`)
	meta, err := LoadPackageMeta(dir)
	if err != nil {
		t.Fatalf("LoadPackageMeta error: %v", err)
	}
	if meta.Name != "mylib" || meta.Version != "0.3.1" || meta.Description != "demo" || meta.License != "MIT" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestLoadPackageMetaMissingConfig(t *testing.T) {
	dir := t.TempDir()
	meta, err := LoadPackageMeta(dir)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if meta.Name != filepath.Base(dir) {
		t.Errorf("name fallback = %q, want dir base %q", meta.Name, filepath.Base(dir))
	}
}

func TestLoadPackageMetaBrokenJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kaula.json"), `{not json`)
	if _, err := LoadPackageMeta(dir); err == nil {
		t.Fatal("expected error for broken json")
	}
}

// --- ResolveVersion ---

func TestResolveVersionTagFlagWins(t *testing.T) {
	v, err := ResolveVersion(t.TempDir(), "v2.0.0", "0.1.0")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", v)
	}
}

func TestResolveVersionFromMeta(t *testing.T) {
	v, err := ResolveVersion(t.TempDir(), "", "1.4.2")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != "1.4.2" {
		t.Errorf("version = %q", v)
	}
}

func TestResolveVersionFromGitDescribe(t *testing.T) {
	dir := t.TempDir()
	gitInitCommitTag(t, dir, "v0.9.0")
	v, err := ResolveVersion(dir, "", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if v != "0.9.0" {
		t.Errorf("version = %q, want 0.9.0", v)
	}
}

func TestResolveVersionNone(t *testing.T) {
	if _, err := ResolveVersion(t.TempDir(), "", ""); err == nil {
		t.Fatal("expected error when no version anywhere")
	}
}

// --- ResolveTargets / token resolution ---

func TestResolveTargetsTokensFromFlags(t *testing.T) {
	opts := &PublishOptions{Dir: ".", Targets: []Target{TargetGitee, TargetGithub},
		GiteeToken: "g_tok", GithubToken: "gh_tok"}
	rs, err := ResolveTargets(opts, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rs[0].Token != "g_tok" || rs[1].Token != "gh_tok" {
		t.Errorf("tokens = %q %q", rs[0].Token, rs[1].Token)
	}
}

func TestResolveTargetsTokensFromEnv(t *testing.T) {
	t.Setenv(EnvGiteeToken, "env_g")
	t.Setenv(EnvGithubToken, "env_gh")
	opts := &PublishOptions{Dir: ".", Targets: []Target{TargetGitee, TargetGithub}}
	rs, err := ResolveTargets(opts, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rs[0].Token != "env_g" || rs[1].Token != "env_gh" {
		t.Errorf("tokens = %q %q", rs[0].Token, rs[1].Token)
	}
}

func TestResolveTargetsTokenMissing(t *testing.T) {
	// 避免环境变量干扰
	t.Setenv(EnvGiteeToken, "")
	t.Setenv(EnvGithubToken, "")
	// 备份并清空全局配置
	backup := loadGlobalConfigFn
	loadGlobalConfigFn = func() globalConfig { return globalConfig{} }
	defer func() { loadGlobalConfigFn = backup }()

	opts := &PublishOptions{Dir: ".", Targets: []Target{TargetGitee}}
	if _, err := ResolveTargets(opts, nil); err == nil {
		t.Fatal("expected error when gitee token missing")
	}
}

func TestResolveTargetsGlobalConfigFallback(t *testing.T) {
	backup := loadGlobalConfigFn
	loadGlobalConfigFn = func() globalConfig {
		return globalConfig{GiteeToken: "cfg_g"}
	}
	defer func() { loadGlobalConfigFn = backup }()

	opts := &PublishOptions{Dir: ".", Targets: []Target{TargetGitee}}
	rs, err := ResolveTargets(opts, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rs[0].Token != "cfg_g" {
		t.Errorf("token = %q, want cfg_g", rs[0].Token)
	}
}

// --- maskURL ---

func TestMaskURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://oauth2:abc123@gitee.com/o/r.git", "https://oauth2:***@gitee.com/o/r.git"},
		{"https://x-access-token:ghp_zzz@github.com/o/r.git", "https://x-access-token:***@github.com/o/r.git"},
		{"https://gitee.com/o/r.git", "https://gitee.com/o/r.git"},
	}
	for _, c := range cases {
		if got := maskURL(c.in); got != c.want {
			t.Errorf("maskURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- remote branch naming ---

func TestRemoteBranchName(t *testing.T) {
	if remoteBranchName(resolvedTarget{Name: TargetGitee}) != "master" {
		t.Error("gitee should default to master")
	}
	if remoteBranchName(resolvedTarget{Name: TargetGithub}) != "main" {
		t.Error("github should default to main")
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func gitInitCommitTag(t *testing.T, dir, tag string) {
	t.Helper()
	run(t, dir, "git", "init", "-q")
	writeFile(t, filepath.Join(dir, "main.kl"), "fn main() {}\n")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "-c", "user.email=test@test", "-c", "user.name=test", "commit", "-q", "-m", "init")
	if tag != "" {
		run(t, dir, "git", "tag", tag)
	}
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}
