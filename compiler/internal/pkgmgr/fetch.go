package pkgmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"compiler/internal/semver"
)

// 注册源: gitee 组织。包仓库命名约定: kaula-universe/<name>，
// 版本经 git tag 发布（v1.2.3 或 1.2.3 均可识别）。
const DefaultRegistry = "https://gitee.com/kaula-universe"

// CacheRoot 返回包缓存根目录 (~/.kaula/pkglib)。
func CacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".kaula", "pkglib"), nil
}

// pkgDir 返回缓存中某包的工作目录: ~/.kaula/pkglib/<name>/
// 布局与本地 pkglib 一致（<name>/ 直接是库根），
// 解析锁 resolved.json 记录当前版本；换版本时删旧目录重克隆。
func pkgDir(root, name string) string {
	return filepath.Join(root, name)
}

// lockFile 返回包的解析锁路径: ~/.kaula/pkglib/<name>/resolved.json
func lockFile(root, name string) string {
	return filepath.Join(pkgDir(root, name), "resolved.json")
}

// ResolvedLock 记录某包已解析的版本（防重复 fetch）
type ResolvedLock struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Tag     string `json:"tag,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// LoadResolved 读取包解析锁；不存在返回 nil。
func LoadResolved(root, name string) (*ResolvedLock, error) {
	data, err := os.ReadFile(lockFile(root, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var l ResolvedLock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse resolved lock: %w", err)
	}
	return &l, nil
}

// SaveResolved 写回包解析锁。
func SaveResolved(root, name string, l *ResolvedLock) error {
	dir := pkgDir(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockFile(root, name), data, 0644)
}

// RegistryURL 返回包的注册源仓库 URL。
func RegistryURL(registry, name string) string {
	return fmt.Sprintf("%s/%s.git", strings.TrimSuffix(registry, "/"), name)
}

// ListTags 用 git ls-remote 列出远端仓库的版本 tag（v1.2.3 / 1.2.3）。
func ListTags(repoURL string) ([]string, error) {
	cmd := exec.Command("git", "ls-remote", "--tags", repoURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote %s: %w (%s)", repoURL, err, strings.TrimSpace(string(out)))
	}
	seen := map[string]bool{}
	var tags []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := fields[1]
		// 跳过 ^{} 剥离后缀行
		if strings.HasSuffix(ref, "^{}") {
			continue
		}
		// 只取 refs/tags/ 下的，跳过非版本 tag
		if !strings.HasPrefix(ref, "refs/tags/") {
			continue
		}
		tag := strings.TrimPrefix(ref, "refs/tags/")
		// 跳过带斜杠的嵌套 tag 名
		if strings.Contains(tag, "/") {
			continue
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}

// CloneTag 浅克隆远端仓库指定 tag 到 dst（目录已存在则跳过）。
func CloneTag(repoURL, tag, dst string) error {
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		return nil // 已克隆
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	// 浅克隆指定 tag（--depth 1 --branch tag）
	args := []string{"clone", "--depth", "1", "--branch", tag, "--single-branch", repoURL, dst}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// 浅克隆失败时清理残留目录（如 tag 名非法时 git 可能建了空目录）
		_ = os.RemoveAll(dst)
		return fmt.Errorf("git clone --branch %s: %w (%s)", tag, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// FetchResult 拉取结果
type FetchResult struct {
	Name    string
	Version string
	Tag     string
	Dir     string
	Source  string // "cache" | "fetched"
}

// Fetch 解析并拉取一个包依赖。
// 流程: 已解析锁满足约束 -> 复用; 否则列出远端 tags -> semver 选最高 -> 浅克隆 -> 写锁。
// 返回包所在目录（供 pkglib 加载）与解析结果。
func Fetch(registry, name, constraintStr string) (*FetchResult, error) {
	root, err := CacheRoot()
	if err != nil {
		return nil, err
	}
	constraint, err := semver.ParseConstraint(constraintStr)
	if err != nil {
		return nil, fmt.Errorf("dependency %s: %w", name, err)
	}

	// 1. 已有解析锁且版本满足约束 -> 直接复用
	if locked, err := LoadResolved(root, name); err == nil && locked != nil {
		if lv, perr := semver.Parse(locked.Version); perr == nil && constraint.Matches(lv) {
			d := pkgDir(root, name)
			if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
				return &FetchResult{Name: name, Version: locked.Version, Tag: locked.Tag, Dir: d, Source: "cache"}, nil
			}
		}
	}

	// 2. 列远端 tags 并匹配
	repoURL := RegistryURL(registry, name)
	tags, err := ListTags(repoURL)
	if err != nil {
		return nil, fmt.Errorf("dependency %s: %w", name, err)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("dependency %s: no version tags in %s (tag versions as v1.2.3)", name, repoURL)
	}
	best := semver.BestMatch(constraint, tags)
	if best == "" {
		return nil, fmt.Errorf("dependency %s: no tag in %s satisfies constraint %q (available: %s)",
			name, repoURL, constraintStr, strings.Join(tags, ", "))
	}
	norm := strings.TrimPrefix(best, "v")
	norm = strings.TrimPrefix(norm, "V")

	// 3. 拉取到缓存（单版本布局: 目录已存在且版本不同 -> 删旧重克隆）
	dst := pkgDir(root, name)
	fmt.Printf("[pkg] fetching %s@%s from %s\n", name, norm, repoURL)
	if locked, _ := LoadResolved(root, name); locked != nil && locked.Version != norm {
		_ = os.RemoveAll(dst)
	}
	if err := CloneTag(repoURL, best, dst); err != nil {
		return nil, fmt.Errorf("dependency %s: %w", name, err)
	}

	// 4. 写解析锁
	commit := ""
	if out, cerr := exec.Command("git", "-C", dst, "rev-parse", "HEAD").CombinedOutput(); cerr == nil {
		commit = strings.TrimSpace(string(out))
	}
	lock := &ResolvedLock{Name: name, Version: norm, Tag: best, Commit: commit}
	if err := SaveResolved(root, name, lock); err != nil {
		fmt.Printf("[pkg] warning: failed to write resolved lock: %v\n", err)
	}

	return &FetchResult{Name: name, Version: norm, Tag: best, Dir: dst, Source: "fetched"}, nil
}
