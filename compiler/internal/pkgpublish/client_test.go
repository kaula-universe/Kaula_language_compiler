package pkgpublish

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGiteeRepoExistsAndCreate 用 httptest 模拟 Gitee API 的探测与建仓流程。
func TestGiteeRepoExistsAndCreate(t *testing.T) {
	var gotCreate struct {
		Name      string `json:"name"`
		Private   string `json:"private"`
		AccessTok string `json:"access_token"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/kaula-universe/mylib"):
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/orgs/kaula-universe/repos"):
			_ = json.NewDecoder(r.Body).Decode(&gotCreate)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"full_name":"kaula-universe/mylib"}`))
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	backup := giteeAPIBase
	giteeAPIBase = server.URL
	defer func() { giteeAPIBase = backup }()

	tgt := resolvedTarget{Name: TargetGitee, Org: "kaula-universe", Token: "secret_token"}
	exists, err := giteeRepoExists(tgt, "mylib")
	if err != nil {
		t.Fatalf("repoExists error: %v", err)
	}
	if exists {
		t.Error("expected repository to not exist")
	}

	if err := giteeCreateOrgRepo(tgt, "mylib", &PackageMeta{Description: "demo"}, false); err != nil {
		t.Fatalf("create error: %v", err)
	}
	if gotCreate.Name != "mylib" {
		t.Errorf("create body name = %q", gotCreate.Name)
	}
	if gotCreate.Private != "false" {
		t.Errorf("create body private = %q", gotCreate.Private)
	}
	if gotCreate.AccessTok != "secret_token" {
		t.Errorf("access_token = %q, want secret_token", gotCreate.AccessTok)
	}
}

// TestGithubRepoExistsAndCreate 模拟 GitHub 探测与建仓。
func TestGithubRepoExistsAndCreate(t *testing.T) {
	var authHeader string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/kaula-universe/mylib"):
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/orgs/kaula-universe/repos"):
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"full_name":"kaula-universe/mylib"}`))
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	backup := githubAPIBase
	githubAPIBase = server.URL
	defer func() { githubAPIBase = backup }()

	tgt := resolvedTarget{Name: TargetGithub, Org: "kaula-universe", Token: "ghp_pat"}
	exists, err := githubRepoExists(tgt, "mylib")
	if err != nil {
		t.Fatalf("repoExists error: %v", err)
	}
	if exists {
		t.Error("expected repository to not exist")
	}
	if err := githubCreateOrgRepo(tgt, "mylib", &PackageMeta{Description: "demo"}, false); err != nil {
		t.Fatalf("create error: %v", err)
	}
	if !strings.Contains(authHeader, "Bearer ghp_pat") {
		t.Errorf("Authorization = %q, want Bearer token", authHeader)
	}
	if gotBody["name"] != "mylib" {
		t.Errorf("create body name = %v", gotBody["name"])
	}
}

// TestRepoRemoteURLAndPublic 验证带凭据 URL 构造成员。
func TestRepoRemoteURLAndPublic(t *testing.T) {
	g := resolvedTarget{Name: TargetGitee, Org: "kaula-universe", Token: "tok@!x"}
	gh := resolvedTarget{Name: TargetGithub, Org: "kaula-universe", Token: "ghp_abc"}

	gurl := repoRemoteURL(g, "mylib")
	if !strings.HasPrefix(gurl, "https://oauth2:") || !strings.HasSuffix(gurl, "@gitee.com/kaula-universe/mylib.git") {
		t.Errorf("gitee remote URL = %q", gurl)
	}
	if strings.Contains(gurl, "tok@!x") {
		t.Error("token leaked into URL unescaped")
	}
	if !strings.Contains(gurl, "tok%40%21x") {
		t.Errorf("token should be URL-escaped in userinfo, got %q", gurl)
	}

	ghurl := repoRemoteURL(gh, "mylib")
	if !strings.HasPrefix(ghurl, "https://x-access-token:") {
		t.Errorf("github remote URL = %q", ghurl)
	}
	if !strings.HasSuffix(publicRepoURL(g, "mylib"), "/kaula-universe/mylib") {
		t.Error("public URL wrong")
	}
}

// TestPublishEndToEnd 完整发布流程（真实 git）：建仓 -> push 代码 -> push tag -> 复核。
// 远端用本地 bare 仓库模拟，repoExists/createOrgRepo/URL 全部注入。
func TestPublishEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kaula.json"), `{"name":"mylib","version":"1.0.0"}`)
	writeFile(t, filepath.Join(dir, "main.kl"), "fn main() {}\n")
	gitInitCommitTag(t, dir, "v1.0.0") // 已打本地 tag

	remote := filepath.Join(t.TempDir(), "remote.git")
	run(t, remote, "git", "init", "--bare", "-q")

	// 注入：仓库视为不存在（走建仓分支）、建仓不做任何事、远端 URL 指向本地 bare
	backupExists := repoExists
	backupCreate := createOrgRepo
	backupRemoteURL := repoRemoteURL
	backupPublicURL := publicRepoURL
	defer func() {
		repoExists = backupExists
		createOrgRepo = backupCreate
		repoRemoteURL = backupRemoteURL
		publicRepoURL = backupPublicURL
	}()
	repoExists = func(t resolvedTarget, repo string) (bool, error) { return false, nil }
	createOrgRepo = func(t resolvedTarget, repo string, meta *PackageMeta, private bool) error { return nil }
	repoRemoteURL = func(t resolvedTarget, repo string) string { return remote }
	publicRepoURL = func(t resolvedTarget, repo string) string { return "file://" + remote }

	opts := &PublishOptions{Dir: dir, Targets: []Target{TargetGitee},
		GiteeToken: "tok"}
	results, err := Publish(opts)
	if err != nil {
		t.Fatalf("Publish error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Status != "tagged" {
		t.Errorf("status = %q, want tagged", results[0].Status)
	}

	head, _ := gitHead(dir)
	got, err := remoteTagCommit(remote, "v1.0.0")
	if err != nil {
		t.Fatalf("remoteTagCommit: %v", err)
	}
	if got != head {
		t.Errorf("remote tag = %s, want HEAD %s", got, head)
	}

	// 幂等：再次发布，应 up-to-date 跳过
	repoExists = func(t resolvedTarget, repo string) (bool, error) { return true, nil }
	results, err = Publish(opts)
	if err != nil {
		t.Fatalf("second Publish error: %v", err)
	}
	if results[0].Status != "up-to-date" {
		t.Errorf("second status = %q, want up-to-date", results[0].Status)
	}
}

// TestPublishDryRun dry-run 不产生任何写操作。
func TestPublishDryRun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kaula.json"), `{"name":"mylib","version":"0.1.0"}`)
	writeFile(t, filepath.Join(dir, "main.kl"), "fn main() {}\n")

	t.Setenv(EnvGiteeToken, "tok")
	t.Setenv(EnvGithubToken, "tok")
	opts := &PublishOptions{Dir: dir, Targets: []Target{TargetGitee, TargetGithub}, DryRun: true}
	backup := repoExists
	repoExists = func(t resolvedTarget, repo string) (bool, error) { return false, nil }
	defer func() { repoExists = backup }()

	if _, err := Publish(opts); err != nil {
		t.Fatalf("Publish dry-run error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Error("dry-run should not create a git repo")
	}
}

// TestEnsureLocalTagForceMove 验证 overwrite 时本地 tag 会被移动到 HEAD。
func TestEnsureLocalTagForceMove(t *testing.T) {
	dir := t.TempDir()
	gitInitCommitTag(t, dir, "v1.0.0")
	// 再来一个 commit（tag 停在旧 commit）
	writeFile(t, filepath.Join(dir, "extra.kl"), "fn extra() {}\n")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "second")

	// 不 overwrite：应报错
	if err := ensureLocalTag(dir, "v1.0.0", false); err == nil {
		t.Fatal("expected error when tag points elsewhere and overwrite=false")
	}
	// overwrite：本地 tag 移到 HEAD
	if err := ensureLocalTag(dir, "v1.0.0", true); err != nil {
		t.Fatalf("ensureLocalTag(overwrite=true): %v", err)
	}
	head, _ := gitHead(dir)
	got := run(t, dir, "git", "rev-parse", "refs/tags/v1.0.0")
	if got != head {
		t.Errorf("tag = %s, want HEAD %s after force move", got, head)
	}
}
