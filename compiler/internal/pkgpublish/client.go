package pkgpublish

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient 可注入的 HTTP 客户端（测试中替换）。
var httpClient = &http.Client{Timeout: 30 * time.Second}

// apiError 平台 API 返回的非 2xx 错误。
type apiError struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *apiError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	return fmt.Sprintf("%s %s -> HTTP %d: %s", e.Method, e.URL, e.Status, body)
}

// apiResult 探测/建仓的公共结果。
type apiResult struct {
	exists bool
}

// giteeRepoExists 探测 gitee 组织仓库是否存在。
// GET /repos/{owner}/{repo}?access_token=... (私有仓库也可见)；
// 404 表示不存在，其余错误照实返回。
func giteeRepoExists(t resolvedTarget, repo string) (bool, error) {
	u := fmt.Sprintf("%s/repos/%s/%s?access_token=%s", giteeAPIBase, t.Org, repo, urlQueryEscape(t.Token))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("gitee API: %w", err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, &apiError{Method: "GET", URL: maskQueryTokens(u), Status: resp.StatusCode, Body: string(buf)}
	}
}

// giteeCreateOrgRepo 在 gitee 组织下创建仓库。
// POST /orgs/{org}/repos，body 见 https://gitee.com/api/v5/swagger#/postV5OrgsOrgRepos。
func giteeCreateOrgRepo(t resolvedTarget, repoName string, meta *PackageMeta, private bool) error {
	u := fmt.Sprintf("%s/orgs/%s/repos", giteeAPIBase, t.Org)
	body := map[string]string{
		"access_token": t.Token,
		"name":         repoName,
		"path":         repoName,
		"description":  truncate(metaDesc(meta), 255),
		"private":      boolStr(private),
		"auto_init":    "false",
	}
	// Gitee 组织仓库 name/path 必须一致
	return giteePostRepo(t, u, body)
}

// giteeCreateUserRepo 在 gitee 令牌主人的个人命名空间下创建仓库。
// POST /user/repos（个人仓库接口）。
func giteeCreateUserRepo(t resolvedTarget, repoName string, meta *PackageMeta, private bool) error {
	u := fmt.Sprintf("%s/user/repos", giteeAPIBase)
	body := map[string]string{
		"access_token": t.Token,
		"name":         repoName,
		"path":         repoName,
		"description":  truncate(metaDesc(meta), 255),
		"private":      boolStr(private),
		"auto_init":    "false",
	}
	return giteePostRepo(t, u, body)
}

// metaDesc 安全读取包元数据描述（meta 可能为 nil）。
func metaDesc(meta *PackageMeta) string {
	if meta == nil {
		return ""
	}
	return meta.Description
}

func giteePostRepo(t resolvedTarget, u string, body map[string]string) error {
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gitee create repo: %w", err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return &apiError{Method: "POST", URL: maskQueryTokens(u), Status: resp.StatusCode, Body: string(buf)}
	}
	return nil
}

// giteeNamespaceKind 探测 gitee 命名空间的类型："org" / "user" / "unknown"。
// GET /orgs/{org} 返回 200 表示组织；GET /user 返回的 login 等于 org 时是令牌主人
// 的个人命名空间；两者都不是则无法发布到该 org。
func giteeNamespaceKind(t resolvedTarget) (string, error) {
	// 先探测组织
	orgURL := fmt.Sprintf("%s/orgs/%s?access_token=%s", giteeAPIBase, t.Org, urlQueryEscape(t.Token))
	req, err := http.NewRequest("GET", orgURL, nil)
	if err != nil {
		return "unknown", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return "org", nil
	}
	if resp != nil {
		resp.Body.Close()
	}

	// 再探测令牌主人
	meURL := fmt.Sprintf("%s/user?access_token=%s", giteeAPIBase, urlQueryEscape(t.Token))
	req, err = http.NewRequest("GET", meURL, nil)
	if err != nil {
		return "unknown", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err = httpClient.Do(req)
	if err != nil {
		return "unknown", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "unknown", nil
	}
	var me struct {
		Login string `json:"login"`
	}
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(buf, &me); err != nil {
		return "unknown", nil
	}
	if me.Login == t.Org {
		return "user", nil
	}
	return "unknown", nil
}

// githubRepoExists 探测 github 组织仓库是否存在。
func githubRepoExists(t resolvedTarget, repo string) (bool, error) {
	u := fmt.Sprintf("%s/repos/%s/%s", githubAPIBase, t.Org, repo)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("github API: %w", err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, &apiError{Method: "GET", URL: u, Status: resp.StatusCode, Body: string(buf)}
	}
}

// githubCreateOrgRepo 在 github 组织下创建仓库。
// POST /orgs/{org}/repos，要求 token 有 Organization 管理范围。
func githubCreateOrgRepo(t resolvedTarget, repoName string, meta *PackageMeta, private bool) error {
	u := fmt.Sprintf("%s/orgs/%s/repos", githubAPIBase, t.Org)
	return githubPostRepo(t, u, repoName, meta, private)
}

// githubCreateUserRepo 在 github 令牌主人的个人命名空间下创建仓库。
// POST /user/repos，要求 token 有 repo 范围。
func githubCreateUserRepo(t resolvedTarget, repoName string, meta *PackageMeta, private bool) error {
	u := fmt.Sprintf("%s/user/repos", githubAPIBase)
	return githubPostRepo(t, u, repoName, meta, private)
}

func githubPostRepo(t resolvedTarget, u, repoName string, meta *PackageMeta, private bool) error {
	body := map[string]interface{}{
		"name":        repoName,
		"description": truncate(metaDesc(meta), 255),
		"private":     private,
		"auto_init":   false,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github create repo: %w", err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return &apiError{Method: "POST", URL: u, Status: resp.StatusCode, Body: string(buf)}
	}
	return nil
}

// githubNamespaceKind 探测 github 命名空间的类型："org" / "user" / "unknown"。
// GET /orgs/{org} 返回 200 表示组织；GET /user 返回的 login 等于 org 时是令牌
// 主人的个人命名空间；两者都不是则无法发布到该 org。
func githubNamespaceKind(t resolvedTarget) (string, error) {
	orgURL := fmt.Sprintf("%s/orgs/%s", githubAPIBase, t.Org)
	req, _ := http.NewRequest("GET", orgURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return "org", nil
		}
	}

	meURL := fmt.Sprintf("%s/user", githubAPIBase)
	req, _ = http.NewRequest("GET", meURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err = httpClient.Do(req)
	if err != nil {
		return "unknown", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "unknown", nil
	}
	var me struct {
		Login string `json:"login"`
	}
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(buf, &me); err != nil {
		return "unknown", nil
	}
	if me.Login == t.Org {
		return "user", nil
	}
	return "unknown", nil
}

// urlQueryEscape 与 url.QueryEscape 等价（避免重复 import）。
func urlQueryEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(
		s, "+", "%2B"), "%", "%25"), " ", "%20")
}

// maskQueryTokens 把 URL query 里的 access_token=xxx 打码。
func maskQueryTokens(u string) string {
	idx := strings.Index(u, "access_token=")
	if idx < 0 {
		return u
	}
	head := u[:idx+len("access_token=")]
	rest := u[idx+len("access_token="):]
	if i := strings.Index(rest, "&"); i >= 0 {
		return head + "***&" + rest[i+1:]
	}
	return head + "***"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
