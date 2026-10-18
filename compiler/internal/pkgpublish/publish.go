// Package pkgpublish 实现 kaulac pkg publish：把本地库一键发布到
// kaula-universe 双注册源（gitee / github 组织仓库）。
//
// 设计目标与约定：
//
//   - 包元数据读取包目录 kaula.json（name / version / description / license），
//     version 可用 --tag 显式覆盖；name 缺省取目录名。
//   - 每个目标源（gitee / github）独立处理：先探测仓库是否已存在（API），
//     不存在则通过平台 API 在组织下建仓，再推送代码与 v<version> tag。
//   - 鉴权：--gitee-token / --github-token 旗标 > KAULA_GITEE_TOKEN /
//     KAULA_GITHUB_TOKEN 环境变量 > ~/.kaula/config.json 的 gitee_token /
//     github_token 字段。
//   - 幂等与校验：远端已有同版本 tag 且指向相同 commit → 跳过该源；
//     同版本 tag 指向不同 commit → 报错（要求 --force 覆盖）；
//     push 后用 git ls-remote 复核 tag 落点，全程不留未确认状态。
//   - 安全：token 经带凭据的 URL 传给 git 子进程，但所有人类可读输出
//     一律打码，不落日志。
package pkgpublish

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"kaula/internal/semver"
)

// 默认组织：与拉取侧 DefaultRegistry / MirrorRegistry 对齐。
const (
	DefaultOrg        = "kaula-universe"
	DefaultTargets    = "gitee,github"
	DefaultPrimaryRef = "master"
	DefaultMirrorRef  = "main"
	GlobalConfigDir   = ".kaula"
	GlobalConfigFile  = "config.json"
	EnvGiteeToken     = "KAULA_GITEE_TOKEN"
	EnvGithubToken    = "KAULA_GITHUB_TOKEN"
	KaulaConfigFile   = "kaula.json"
)

// Target 发布目标源。
type Target string

const (
	TargetGitee  Target = "gitee"
	TargetGithub Target = "github"
)

// PublishOptions kaulac pkg publish 的命令行参数（已解析）。
type PublishOptions struct {
	Dir         string // 包目录（默认 "."）
	Targets     []Target
	OrgOverride string // --org 覆盖两个源的默认组织
	Tag         string // --tag 显式指定发布版本（覆盖 kaula.json version）
	GiteeToken  string // --gitee-token
	GithubToken string // --github-token
	Force       bool   // 允许覆盖已存在的同版本 tag
	DryRun      bool   // 只打印计划，不执行任何写操作
}

// PackageMeta kaula.json 中与发布相关的元数据。
type PackageMeta struct {
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	License     string `json:"license,omitempty"`
}

// resolvedTarget 解析后的单个发布目标（含鉴权信息）。
type resolvedTarget struct {
	Name Target
	Org  string
	// Token 该源的访问令牌（gitee 为私人令牌，github 为 PAT）。
	Token string
}

// RepoResult 单个目标的发布结果。
type RepoResult struct {
	Target Target
	Org    string
	Repo   string // 仓库名
	URL    string // 远端 URL（打码）
	Status string // "created" | "pushed" | "tagged" | "up-to-date" | "skipped"
	Tag    string // 发布版本 tag（v<version>）
}

// UsageError 表示 CLI 用法的错误（退出码 1，不打印 stack）。
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func usageErrorf(format string, args ...interface{}) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// ParsePublishArgs 解析 kaulac pkg publish 的原始参数。
//
// 支持形式：
//
//	kaulac pkg publish [<dir>] [--target gitee,github] [--org <org>]
//	                   [--tag <v1.2.3>] [--gitee-token <t>] [--github-token <t>]
//	                   [--force] [--dry-run]
//
// 未知旗标报错（fail-fast，避免写错参数后糊里糊涂发到错误的地方）。
func ParsePublishArgs(args []string) (*PublishOptions, error) {
	opts := &PublishOptions{Dir: "."}
	targetsStr := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			if !strings.HasPrefix(arg, ".") && !filepath.IsAbs(arg) && strings.ContainsAny(arg, ":/\\") {
				return nil, usageErrorf("unexpected argument %q (expected a package directory, then flags)", arg)
			}
			if opts.Dir != "." {
				return nil, usageErrorf("duplicate package directory %q", arg)
			}
			opts.Dir = arg
			continue
		}
		// 支持 --k=v 与 --k v 两种写法
		key, val, hasVal := splitFlag(arg)
		switch key {
		case "--target":
			if !hasVal {
				i++
				if i >= len(args) {
					return nil, usageErrorf("--target requires a value (gitee,github)")
				}
				val = args[i]
			}
			targetsStr = val
		case "--org":
			if !hasVal {
				i++
				if i >= len(args) {
					return nil, usageErrorf("--org requires a value")
				}
				val = args[i]
			}
			opts.OrgOverride = val
		case "--tag":
			if !hasVal {
				i++
				if i >= len(args) {
					return nil, usageErrorf("--tag requires a value")
				}
				val = args[i]
			}
			opts.Tag = val
		case "--gitee-token":
			if !hasVal {
				i++
				if i >= len(args) {
					return nil, usageErrorf("--gitee-token requires a value")
				}
				val = args[i]
			}
			opts.GiteeToken = val
		case "--github-token":
			if !hasVal {
				i++
				if i >= len(args) {
					return nil, usageErrorf("--github-token requires a value")
				}
				val = args[i]
			}
			opts.GithubToken = val
		case "--force":
			if hasVal {
				b, err := parseBool(val)
				if err != nil {
					return nil, usageErrorf("--force: %v", err)
				}
				opts.Force = b
			} else {
				opts.Force = true
			}
		case "--dry-run":
			if hasVal {
				b, err := parseBool(val)
				if err != nil {
					return nil, usageErrorf("--dry-run: %v", err)
				}
				opts.DryRun = b
			} else {
				opts.DryRun = true
			}
		case "-h", "--help":
			return nil, ErrHelp
		default:
			return nil, usageErrorf("unknown flag %q for publish", key)
		}
	}

	targets, err := ParseTargets(targetsStr)
	if err != nil {
		return nil, err
	}
	opts.Targets = targets

	if opts.Tag != "" {
		if _, err := semver.Parse(opts.Tag); err != nil {
			return nil, usageErrorf("invalid --tag %q: %v", opts.Tag, err)
		}
	}
	return opts, nil
}

// ErrHelp 表示用户请求帮助文本（不当作错误处理）。
var ErrHelp = &UsageError{Msg: "help requested"}

// UsageText publish 子命令的帮助文本。
const UsageText = `usage: kaulac pkg publish [<package-dir>] [options]

Publish a Kaula library to the kaula-universe organization repositories
on Gitee and/or GitHub with a single command.

options:
  --target <gitee,github>   registries to publish to (default: gitee,github)
  --org <org>               organization to publish into (default: kaula-universe)
  --tag <version>           version to tag (default: "version" from kaula.json)
  --gitee-token <token>     Gitee private token (else KAULA_GITEE_TOKEN / ~/.kaula/config.json)
  --github-token <token>    GitHub personal access token (else KAULA_GITHUB_TOKEN / ~/.kaula/config.json)
  --force                   overwrite an existing version tag that points elsewhere
  --dry-run                 show the plan without executing anything

examples:
  kaulac pkg publish                        # publish ./ to gitee + github
  kaulac pkg publish ../mylib --tag 0.2.0   # publish specific package and version
  kaulac pkg publish --target github        # publish to github only
`

func splitFlag(arg string) (key, val string, hasVal bool) {
	if i := strings.Index(arg, "="); i >= 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", s)
	}
}

// ParseTargets 解析 --target 的值（gitee,github 逗号分隔，可去重与保序）。
func ParseTargets(s string) ([]Target, error) {
	if strings.TrimSpace(s) == "" {
		s = DefaultTargets
	}
	var out []Target
	seen := map[Target]bool{}
	for _, part := range strings.Split(s, ",") {
		t := Target(strings.ToLower(strings.TrimSpace(part)))
		if t == "" {
			continue
		}
		switch t {
		case TargetGitee, TargetGithub:
		default:
			return nil, usageErrorf("unknown target %q (expected gitee or github)", part)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, usageErrorf("--target requires at least one of: gitee, github")
	}
	return out, nil
}

// LoadPackageMeta 读取包目录的 kaula.json。文件缺失/字段缺失不报错
// （name 回退目录名，version 回退空串调用方再决策）。
func LoadPackageMeta(dir string) (*PackageMeta, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	meta := &PackageMeta{}
	path := filepath.Join(abs, KaulaConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			meta.Name = filepath.Base(abs)
			return meta, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, meta); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if meta.Name == "" {
		meta.Name = filepath.Base(abs)
	}
	return meta, nil
}

// ResolveVersion 决定本次发布的版本：
//  1. --tag 显式指定；
//  2. kaula.json "version"；
//  3. 包目录所在 git 仓库最近一个版本的 tag（git describe --tags），
//     无 tag 则报错（不随意发明版本号）。
func ResolveVersion(dir, tagFlag, metaVersion string) (string, error) {
	if tagFlag != "" {
		return strings.TrimPrefix(tagFlag, "v"), nil
	}
	if v := strings.TrimSpace(metaVersion); v != "" {
		if _, err := semver.Parse(v); err != nil {
			return "", fmt.Errorf("invalid version %q in kaula.json: %v", v, err)
		}
		return strings.TrimPrefix(v, "v"), nil
	}
	// 从 git describe 取最近 tag 作为版本（未发布过则无法确定版本）
	cmd := exec.Command("git", "-C", dir, "describe", "--tags", "--abbrev=0")
	out, err := cmd.CombinedOutput()
	if err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			if _, perr := semver.Parse(v); perr == nil {
				return strings.TrimPrefix(v, "v"), nil
			}
		}
	}
	return "", fmt.Errorf("no version specified: pass --tag <version> or set \"version\" in %s, or create a version tag in the git repo first", KaulaConfigFile)
}

// ResolveTargets 组装每个发布源的目标信息（组织 + 令牌）。
// 令牌优先级：旗标 > 环境变量 > 全局配置 ~/.kaula/config.json。
func ResolveTargets(opts *PublishOptions, meta *PackageMeta) ([]resolvedTarget, error) {
	globals := loadGlobalConfig()
	var out []resolvedTarget
	for _, t := range opts.Targets {
		rt := resolvedTarget{Name: t}
		rt.Org = DefaultOrg
		if opts.OrgOverride != "" {
			rt.Org = opts.OrgOverride
		}
		switch t {
		case TargetGitee:
			rt.Token = firstNonEmpty(opts.GiteeToken, os.Getenv(EnvGiteeToken), globals.GiteeToken)
			if rt.Token == "" {
				return nil, fmt.Errorf("gitee token required: pass --gitee-token, set %s, or add \"gitee_token\" to ~/%s/%s",
					EnvGiteeToken, GlobalConfigDir, GlobalConfigFile)
			}
		case TargetGithub:
			rt.Token = firstNonEmpty(opts.GithubToken, os.Getenv(EnvGithubToken), globals.GithubToken)
			if rt.Token == "" {
				return nil, fmt.Errorf("github token required: pass --github-token, set %s, or add \"github_token\" to ~/%s/%s",
					EnvGithubToken, GlobalConfigDir, GlobalConfigFile)
			}
		}
		out = append(out, rt)
	}
	return out, nil
}

// globalConfig ~/.kaula/config.json 的本地结构。
type globalConfig struct {
	GiteeToken  string `json:"gitee_token,omitempty"`
	GithubToken string `json:"github_token,omitempty"`
}

// loadGlobalConfigFn 可注入（测试中替换），默认读取 ~/.kaula/config.json。
var loadGlobalConfigFn = func() globalConfig {
	var cfg globalConfig
	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(filepath.Join(home, GlobalConfigDir, GlobalConfigFile))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func loadGlobalConfig() globalConfig {
	return loadGlobalConfigFn()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// giteeAPIBase / githubAPIBase 平台 API 根地址（测试中可重定向）。
var (
	giteeAPIBase  = "https://gitee.com/api/v5"
	githubAPIBase = "https://api.github.com"
)

// escapeUserinfo 转义 URL userinfo 段（user:pass@）中的保留字符，
// 保证 token 里出现 @ : / % ? # + 空格等字符时不会破坏 URL 结构或泄露语义。
// 对全部 sub-delims 与 gen-delims（RFC 3986）一律编码，token 内容无论如何
// 都不会被误解析为凭证分隔符或路径片段。
func escapeUserinfo(s string) string {
	r := strings.NewReplacer(
		"%", "%25",
		"@", "%40",
		":", "%3A",
		"/", "%2F",
		"\\", "%5C",
		"?", "%3F",
		"#", "%23",
		" ", "%20",
		"+", "%2B",
		"!", "%21",
		"$", "%24",
		"&", "%26",
		"'", "%27",
		"(", "%28",
		")", "%29",
		"*", "%2A",
		",", "%2C",
		";", "%3B",
		"=", "%3D",
	)
	return r.Replace(s)
}

// repoRemoteURL 构造带凭据的 git 远端 URL。
// gitee 支持 oauth2:<token>@ 形式；github 用 x-access-token:<token>@（PAT 兼容）。
// 变量形式以支持测试注入（指向本地 bare 仓库）。
var repoRemoteURL = func(t resolvedTarget, repo string) string {
	switch t.Name {
	case TargetGitee:
		return fmt.Sprintf("https://oauth2:%s@gitee.com/%s/%s.git", escapeUserinfo(t.Token), t.Org, repo)
	case TargetGithub:
		return fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", escapeUserinfo(t.Token), t.Org, repo)
	}
	return ""
}

// publicRepoURL 不带凭据的仓库页面地址（用于展示；可注入）。
var publicRepoURL = func(t resolvedTarget, repo string) string {
	switch t.Name {
	case TargetGitee:
		return fmt.Sprintf("https://gitee.com/%s/%s", t.Org, repo)
	case TargetGithub:
		return fmt.Sprintf("https://github.com/%s/%s", t.Org, repo)
	}
	return ""
}

// maskURL 把 URL 中的 token 打码（用于日志与控制台输出）。
func maskURL(u string) string {
	// 形如 scheme://user:token@host/... → scheme://user:***@host/...
	idx := strings.Index(u, "@")
	if idx < 0 {
		return u
	}
	head := u[:idx]
	colon := strings.LastIndex(head, ":")
	if colon < 0 {
		// 無 user: 段，直接打码整个说明段
		return "***@" + u[idx+1:]
	}
	return head[:colon+1] + "***@" + u[idx+1:]
}

// sortTargets 固定处理顺序（gitee -> github），保证输出稳定。
func sortTargets(ts []Target) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i] < ts[j] })
}
