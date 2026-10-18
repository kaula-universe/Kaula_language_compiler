package pkgmgr

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 注册源定义。
//
// kaula-universe 组织在 gitee 与 github 各有一份同名仓库镜像（ml/algebra/eigen/
// tensor/termus/kaulaweb 等，仓库命名一致）。拉取包时默认在这两个源间自动测速，
// 选取最快的源拉取；若选中源失败则按延迟降级到下一个可用源。
//
// 兼容性约定：
//   - registry 为空 或 等于 DefaultRegistry（gitee，历史默认）时，
//     视为"未显式覆盖"，启用双源择优；
//   - registry 为其他自定义值（用户显式配置的第三方源/私有 git 根）时，
//     尊重用户意图，仅使用该单一源。
const (
	// MirrorRegistry github 镜像组织源。DefaultRegistry 定义于 fetch.go。
	MirrorRegistry = "https://github.com/kaula-universe"
)

// RegistryCandidates 返回给定 registry 实际应使用的候选源列表（按优先顺序）。
//
//   - registry == "" || registry == DefaultRegistry → [gitee, github]（双源择优）
//   - 其他值 → 仅该来源（显式自定义，不做择优）
func RegistryCandidates(registry string) []string {
	reg := strings.TrimSpace(registry)
	if reg == "" || reg == DefaultRegistry {
		return []string{DefaultRegistry, MirrorRegistry}
	}
	return []string{reg}
}

// sourceProbe 单源探测结果：某源对指定包的 tag 列表与测速耗时。
type sourceProbe struct {
	registry string // 源 URL（不带尾斜杠）
	repoURL  string // 完整仓库 URL（<registry>/<name>.git）
	tags     []string
	latency  time.Duration
	err      error
}

// ProbeFunc 单源探测函数。repoURL 为 <registry>/<name>.git。
// 返回该源的 tag 列表与实际耗时；出错时返回 err。
type ProbeFunc func(repoURL string) (tags []string, latency time.Duration, err error)

// gitProbe 是默认的 ProbeFunc 实现：对远端仓库执行 `git ls-remote --tags` 并计时。
// 返回的 tags 语义与 ListTags 一致（过滤标签、排序）。
func gitProbe(repoURL string) ([]string, time.Duration, error) {
	start := time.Now()
	tags, err := ListTags(repoURL)
	return tags, time.Since(start), err
}

// probeBestSource 并发探测所有候选源，按延迟升序返回探测结果（失败的排在末尾）。
// 若全部源失败，返回一个包含每个源错误说明的聚合错误。
func probeBestSource(probe ProbeFunc, repoURLs []string) ([]sourceProbe, error) {
	results := make([]sourceProbe, len(repoURLs))
	type idxResult struct {
		i int
	}
	done := make(chan idxResult, len(repoURLs))

	for i, u := range repoURLs {
		go func(i int, u string) {
			tags, lat, err := probe(u)
			results[i] = sourceProbe{repoURL: u, tags: tags, latency: lat, err: err}
			done <- idxResult{i: i}
		}(i, u)
	}
	for range repoURLs {
		<-done
	}

	var ok []sourceProbe
	var failed []sourceProbe
	for _, r := range results {
		if r.err == nil {
			ok = append(ok, r)
		} else {
			failed = append(failed, r)
		}
	}

	// 按延迟升序（最快优先）
	sort.SliceStable(ok, func(i, j int) bool {
		return ok[i].latency < ok[j].latency
	})
	// 失败源按原始顺序排在末尾
	ordered := append(ok, failed...)

	if len(ok) == 0 {
		var parts []string
		for _, r := range failed {
			parts = append(parts, fmt.Sprintf("%s: %v", r.repoURL, r.err))
		}
		return ordered, fmt.Errorf("no registry reachable for %s (%s)",
			repoURLs[0], strings.Join(parts, "; "))
	}
	return ordered, nil
}

// pickBestSource 从测试结果中选择最佳源，输出测速汇总信息。
func pickBestSource(probes []sourceProbe) (best sourceProbe) {
	if len(probes) == 0 {
		return sourceProbe{}
	}
	// 优先返回第一个可达且延迟最低的源
	for _, p := range probes {
		if p.err == nil {
			return p
		}
	}
	return probes[0]
}

// describeLatency 将单源耗时格式化为人类可读字符串。
func describeLatency(d time.Duration) string {
	if d <= 0 {
		return "?"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.3fms", float64(d)/float64(time.Millisecond))
	}
	if d < time.Second {
		return fmt.Sprintf("%.0fms", float64(d)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
