package pkgpublish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Publish 执行一次完整发布。返回每条目标源的详细信息。
//
// 流程（每个目标源独立）：
//  1. 探测远端仓库是否存在；不存在则经平台 API 建仓；
//  2. git init（若本地尚无仓库）；
//  3. 检查远端版本 tag 状态（存在/指向）；
//  4. push 代码到远端默认分支；
//  5. 打 v<version> tag（如未发布）并 push；
//  6. ls-remote 复核 tag 落点。
//
// dryRun 时只打印计划，不执行任何写操作。
func Publish(opts *PublishOptions) ([]RepoResult, error) {
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("package directory %q is not a directory", dir)
	}

	meta, err := LoadPackageMeta(dir)
	if err != nil {
		return nil, err
	}
	version, err := ResolveVersion(dir, opts.Tag, meta.Version)
	if err != nil {
		return nil, err
	}
	tag := "v" + version

	targets, err := ResolveTargets(opts, meta)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[pkg publish] %s v%s -> %d target(s)\n", meta.Name, version, len(targets))
	if opts.DryRun {
		fmt.Println("[pkg publish] DRY-RUN: no changes will be made")
	}

	// 准备一次性 git 仓库（若未初始化）
	if !opts.DryRun {
		if err := ensureGitRepo(dir); err != nil {
			return nil, err
		}
	}

	var results []RepoResult
	for _, t := range targets {
		res, err := publishToTarget(dir, t, meta, meta.Name, tag, version, opts)
		if err != nil {
			return results, fmt.Errorf("%s: %w", t.Name, err)
		}
		results = append(results, res)
	}

	if opts.DryRun {
		fmt.Println("[pkg publish] DRY-RUN complete (nothing executed)")
	}
	return results, nil
}

// publishToTarget 发布到单个目标源。
func publishToTarget(dir string, t resolvedTarget, meta *PackageMeta, repoName, tag, version string, opts *PublishOptions) (RepoResult, error) {
	res := RepoResult{Target: t.Name, Org: t.Org, Repo: repoName, Tag: tag}
	if opts.DryRun {
		exists, err := repoExists(t, repoName)
		if err != nil {
			return res, err
		}
		if exists {
			res.Status = "would-push"
		} else {
			res.Status = "would-create"
		}
		res.URL = publicRepoURL(t, repoName)
		fmt.Printf("[pkg publish] [dry-run] %s: %s %s/%s (tag %s)\n",
			t.Name, res.Status, t.Org, repoName, tag)
		return res, nil
	}

	// 1. 探测/建仓
	exists, err := repoExists(t, repoName)
	if err != nil {
		return res, err
	}
	if !exists {
		fmt.Printf("[pkg publish] %s: creating repository %s/%s...\n", t.Name, t.Org, repoName)
		if err := createOrgRepo(t, repoName, meta, false); err != nil {
			return res, err
		}
		res.Status = "created"
	} else {
		res.Status = "exists"
	}
	res.URL = publicRepoURL(t, repoName)
	remote := repoRemoteURL(t, repoName)
	masked := maskURL(remote)

	// 2. 检查远端 tag 状态
	localHead, err := gitHead(dir)
	if err != nil {
		return res, fmt.Errorf("no commits in %s (%v): commit at least once before publishing", dir, err)
	}
	remoteTag, err := remoteTagCommit(remote, tag)
	if err != nil {
		return res, err
	}
	overwriteTag := opts.Force

	switch {
	case remoteTag == "":
		// 未发布过该版本
		fmt.Printf("[pkg publish] %s: tag %s not published yet\n", t.Name, tag)
		res.Status = "tagged"
	case remoteTag == localHead:
		// 已存在且指向本地 HEAD：幂等跳过
		fmt.Printf("[pkg publish] %s: tag %s already at HEAD (%s...), nothing to do\n",
			t.Name, tag, shortSHA(localHead))
		res.Status = "up-to-date"
		res.Tag = tag
		return res, nil
	default:
		// 已存在但指向不同 commit
		if !overwriteTag {
			return res, fmt.Errorf("tag %s already exists at %s (local HEAD is %s); pass --force to overwrite",
				tag, shortSHA(remoteTag), shortSHA(localHead))
		}
		fmt.Printf("[pkg publish] %s: overwriting tag %s (%s -> %s)\n",
			t.Name, tag, shortSHA(remoteTag), shortSHA(localHead))
		res.Status = "tagged"
	}

	// 3. push 代码
	branch := remoteBranchName(t)
	if err := gitPushBranch(dir, remote, branch); err != nil {
		return res, err
	}
	fmt.Printf("[pkg publish] %s: pushed %s -> %s (%s)\n", t.Name, shortSHA(localHead), branch, masked)

	// 4. 打 tag 并 push（未发布或 --force 覆盖）
	if remoteTag == "" || overwriteTag {
		if err := gitPushTag(dir, remote, tag, overwriteTag); err != nil {
			return res, err
		}
		fmt.Printf("[pkg publish] %s: pushed tag %s\n", t.Name, tag)
	}

	// 5. 复核
	verified, err := remoteTagCommit(remote, tag)
	if err != nil {
		return res, fmt.Errorf("verification failed: %w", err)
	}
	if verified == "" {
		return res, fmt.Errorf("verification failed: tag %s does not exist on remote", tag)
	}
	if verified != localHead && !opts.Force && remoteTag != localHead {
		return res, fmt.Errorf("verification failed: remote tag %s points to %s, expected %s",
			tag, shortSHA(verified), shortSHA(localHead))
	}
	fmt.Printf("[pkg publish] %s: OK — %s (%s)\n", t.Name, tag, shortSHA(verified))
	return res, nil
}

// repoExists 平台无关的仓库探测（测试中注入）。
var repoExists = func(t resolvedTarget, repo string) (bool, error) {
	switch t.Name {
	case TargetGitee:
		return giteeRepoExists(t, repo)
	case TargetGithub:
		return githubRepoExists(t, repo)
	}
	return false, fmt.Errorf("unknown target %q", t.Name)
}

// createOrgRepo 平台无关的建仓（测试中注入）。
// 自动区分命名空间类型：组织存在走组织建仓；等于令牌主人的 login 走个人建仓；
// 两者都不是则报错（避免误发到无法控制的命名空间）。
var createOrgRepo = func(t resolvedTarget, repo string, meta *PackageMeta, private bool) error {
	kind := "org"
	var err error
	switch t.Name {
	case TargetGitee:
		k, kerr := giteeNamespaceKind(t)
		if kerr != nil {
			return kerr
		}
		kind = k
		switch kind {
		case "org":
			return giteeCreateOrgRepo(t, repo, meta, private)
		case "user":
			return giteeCreateUserRepo(t, repo, meta, private)
		}
		if t.Org != DefaultOrg {
			err = fmt.Errorf("namespace %q is not an organization you belong to nor your own account; create it manually or pick another --org", t.Org)
		} else {
			err = fmt.Errorf("gitee org %q not found/member-check failed", t.Org)
		}
	case TargetGithub:
		k, kerr := githubNamespaceKind(t)
		if kerr != nil {
			return kerr
		}
		kind = k
		switch kind {
		case "org":
			return githubCreateOrgRepo(t, repo, meta, private)
		case "user":
			return githubCreateUserRepo(t, repo, meta, private)
		}
		err = fmt.Errorf("namespace %q is not an organization you belong to nor your own account; create it manually or pick another --org", t.Org)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("unknown target %q", t.Name)
}

// remoteBranchName 目标源的远端默认分支名。
func remoteBranchName(t resolvedTarget) string {
	if t.Name == TargetGitee {
		return DefaultPrimaryRef
	}
	return DefaultMirrorRef
}

// ensureGitRepo 在目录内初始化 git 仓库（已存在则跳过）。
func ensureGitRepo(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	cmd := exec.Command("git", "-C", dir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitHead 返回本地仓库 HEAD commit（未初始化或无提交时报错）。
func gitHead(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// remoteTagCommit 返回远端 tag 指向的 commit（不存在返回空串）。
func remoteTagCommit(remote, tag string) (string, error) {
	cmd := exec.Command("git", "ls-remote", "--tags", remote, "refs/tags/"+tag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s: %w (%s)", maskURL(remote), err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// 取剥壳后的真实 commit（tag^{}），无则取 tag 自身
		if strings.HasSuffix(fields[1], "^{}") {
			return fields[0], nil
		}
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimSuffix(fields[1], "^{}") == "refs/tags/"+tag {
			return fields[0], nil
		}
	}
	return "", nil
}

// gitPushBranch push 本地 HEAD 到远端指定分支。
func gitPushBranch(dir, remote, branch string) error {
	cmd := exec.Command("git", "-C", dir, "push", remote, "HEAD:refs/heads/"+branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push %s: %w (%s)", maskURL(remote), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitPushTag push 指定 tag 到远端；overwrite 时强制覆盖。
func gitPushTag(dir, remote, tag string, overwrite bool) error {
	// 本地 tag 若不存在则创建（指向 HEAD）；overwrite 时把本地 tag 也移到 HEAD
	if err := ensureLocalTag(dir, tag, overwrite); err != nil {
		return err
	}
	args := []string{"-C", dir, "push"}
	if overwrite {
		args = append(args, "--force")
	}
	args = append(args, remote, "refs/tags/"+tag)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push tag %s: %w (%s)", tag, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ensureLocalTag 确保本地存在指定 tag（指向 HEAD）。
// 已存在且指向 HEAD → 复用；已存在但指向别处 → 报错（overwrite 时移动本地 tag）。
func ensureLocalTag(dir, tag string, overwrite bool) error {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/tags/"+tag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// tag 不存在：创建
		c := exec.Command("git", "-C", dir, "tag", tag)
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("git tag %s: %w (%s)", tag, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	local := strings.TrimSpace(string(out))
	head, err := gitHead(dir)
	if err != nil {
		return err
	}
	if local != head {
		if overwrite {
			c := exec.Command("git", "-C", dir, "tag", "-f", tag)
			if out, err := c.CombinedOutput(); err != nil {
				return fmt.Errorf("git tag -f %s: %w (%s)", tag, err, strings.TrimSpace(string(out)))
			}
			return nil
		}
		return fmt.Errorf("local tag %s already exists at %s (HEAD is %s); delete it or use --force",
			tag, shortSHA(local), shortSHA(head))
	}
	return nil
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// shortSHA 截取 commit 短哈希（非法输入原样返回）。
func shortSHA(s string) string {
	if shaPattern.MatchString(s) {
		return s[:12]
	}
	return s
}
