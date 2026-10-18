package pkgmgr

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// --- RegistryCandidates 测试 ---

func TestRegistryCandidates(t *testing.T) {
	cases := []struct {
		name     string
		registry string
		want     []string
	}{
		{"empty defaults to dual", "", []string{DefaultRegistry, MirrorRegistry}},
		{"default(gitee) expands to dual", DefaultRegistry, []string{DefaultRegistry, MirrorRegistry}},
		{"whitespace default expands to dual", " " + DefaultRegistry + " ", []string{DefaultRegistry, MirrorRegistry}},
		{"custom single source is preserved", "https://git.example.org/kaula-universe", []string{"https://git.example.org/kaula-universe"}},
		{"github mirror explicitly stays single", MirrorRegistry, []string{MirrorRegistry}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RegistryCandidates(c.registry)
			if len(got) != len(c.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(c.want), got)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("got[%d] = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// --- probeBestSource 测试（使用可控假 ProbeFunc，无网络） ---

// fakeProbe 构造一个按 URL 返回固定延迟/错误/tags 的探针。
func fakeProbe(routes map[string]fakeResult) ProbeFunc {
	return func(repoURL string) ([]string, time.Duration, error) {
		r, ok := routes[repoURL]
		if !ok {
			return nil, 0, fmt.Errorf("unknown repo %s", repoURL)
		}
		if r.err != "" {
			return nil, r.latency, errors.New(r.err)
		}
		return r.tags, r.latency, nil
	}
}

type fakeResult struct {
	latency time.Duration
	tags    []string
	err     string
}

func TestProbeBestSource_ChoosesFastest(t *testing.T) {
	probe := fakeProbe(map[string]fakeResult{
		"https://gitee.com/kaula-universe/ml.git":  {latency: 200 * time.Millisecond, tags: []string{"v1.2.0"}},
		"https://github.com/kaula-universe/ml.git": {latency: 50 * time.Millisecond, tags: []string{"v1.2.0"}},
	})
	probes, err := probeBestSource(probe, []string{
		"https://gitee.com/kaula-universe/ml.git",
		"https://github.com/kaula-universe/ml.git",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 最快的 github 应排第一位
	if probes[0].repoURL != "https://github.com/kaula-universe/ml.git" {
		t.Fatalf("fastest = %s, want github", probes[0].repoURL)
	}
	// 两个都可达
	if len(probes) != 2 {
		t.Fatalf("len = %d, want 2", len(probes))
	}
}

func TestProbeBestSource_DegradesOnFailure(t *testing.T) {
	probe := fakeProbe(map[string]fakeResult{
		"https://gitee.com/kaula-universe/ml.git":  {latency: 30 * time.Millisecond, tags: []string{"v1.0.0"}},
		"https://github.com/kaula-universe/ml.git": {err: "cert revoked"},
	})
	probes, err := probeBestSource(probe, []string{
		"https://gitee.com/kaula-universe/ml.git",
		"https://github.com/kaula-universe/ml.git",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 唯一可达的 gitee 应排第一
	if probes[0].repoURL != "https://gitee.com/kaula-universe/ml.git" {
		t.Fatalf("survivor = %s, want gitee", probes[0].repoURL)
	}
	if probes[0].err != nil {
		t.Fatalf("survivor should not have err: %v", probes[0].err)
	}
	// 失败源排末尾
	last := probes[len(probes)-1]
	if last.err == nil {
		t.Fatalf("last source should have err")
	}
}

func TestProbeBestSource_AllFail(t *testing.T) {
	probe := fakeProbe(map[string]fakeResult{
		"https://gitee.com/kaula-universe/ml.git":  {err: "timeout"},
		"https://github.com/kaula-universe/ml.git": {err: "cert revoked"},
	})
	_, err := probeBestSource(probe, []string{
		"https://gitee.com/kaula-universe/ml.git",
		"https://github.com/kaula-universe/ml.git",
	})
	if err == nil {
		t.Fatalf("expected error when all sources fail")
	}
	// 错误信息应包含两个源
	msg := err.Error()
	if !strings.Contains(msg, "gitee.com") || !strings.Contains(msg, "github.com") {
		t.Fatalf("error should mention both sources, got: %v", msg)
	}
}

func TestProbeBestSource_StableOrdering(t *testing.T) {
	probe := fakeProbe(map[string]fakeResult{
		"https://gitee.com/kaula-universe/ml.git":  {latency: 100 * time.Millisecond, tags: []string{"v1.0.0"}},
		"https://github.com/kaula-universe/ml.git": {latency: 100 * time.Millisecond, tags: []string{"v1.0.0"}},
	})
	probes, err := probeBestSource(probe, []string{
		"https://gitee.com/kaula-universe/ml.git",
		"https://github.com/kaula-universe/ml.git",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 同延迟都应保留且顺序稳定（不崩溃）
	if len(probes) != 2 {
		t.Fatalf("len = %d, want 2", len(probes))
	}
}

// --- pickBestSource / describeLatency 测试 ---

func TestPickBestSource(t *testing.T) {
	probes := []sourceProbe{
		{repoURL: "https://github.com/a.git", err: errors.New("boom")},
		{repoURL: "https://gitee.com/b.git", latency: 5 * time.Millisecond},
	}
	best := pickBestSource(probes)
	if best.repoURL != "https://gitee.com/b.git" {
		t.Fatalf("best = %s, want gitee", best.repoURL)
	}
	// 全部失败时返回第一个（以备降级处理）
	allFail := []sourceProbe{
		{repoURL: "https://a.git", err: errors.New("x")},
		{repoURL: "https://b.git", err: errors.New("y")},
	}
	if got := pickBestSource(allFail).repoURL; got != "https://a.git" {
		t.Fatalf("all-fail best = %s, want a.git", got)
	}
}

func TestDescribeLatency(t *testing.T) {
	if got := describeLatency(0); got != "?" {
		t.Fatalf("zero = %q, want ?", got)
	}
	if got := describeLatency(500 * time.Microsecond); !strings.Contains(got, "ms") {
		t.Fatalf("sub-ms should render ms, got %q", got)
	}
	if got := describeLatency(2500 * time.Millisecond); !strings.Contains(got, "s") {
		t.Fatalf(">1s should render seconds, got %q", got)
	}
}

// --- RegistryURL 拼接正确性（含镜像源） ---

func TestRegistryURL(t *testing.T) {
	cases := []struct {
		registry, name, want string
	}{
		{DefaultRegistry, "ml", "https://gitee.com/kaula-universe/ml.git"},
		{MirrorRegistry, "ml", "https://github.com/kaula-universe/ml.git"},
		{"https://x.com/org/", "a", "https://x.com/org/a.git"},
	}
	for _, c := range cases {
		if got := RegistryURL(c.registry, c.name); got != c.want {
			t.Fatalf("RegistryURL(%q,%q) = %q, want %q", c.registry, c.name, got, c.want)
		}
	}
}

// --- 稳定性: 排序结果确定且不依赖 map 遍历顺序 ---
func TestProbeBestSource_Deterministic(t *testing.T) {
	probe := fakeProbe(map[string]fakeResult{
		"https://gitee.com/kaula-universe/ml.git":  {latency: 120 * time.Millisecond, tags: []string{"v1.2.0"}},
		"https://github.com/kaula-universe/ml.git": {latency: 80 * time.Millisecond, tags: []string{"v1.2.0"}},
	})
	var first string
	for i := 0; i < 50; i++ {
		probes, err := probeBestSource(probe, []string{
			"https://gitee.com/kaula-universe/ml.git",
			"https://github.com/kaula-universe/ml.git",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if i == 0 {
			first = probes[0].repoURL
		} else if probes[0].repoURL != first {
			t.Fatalf("non-deterministic ordering: iteration %d gave %s, first was %s", i, probes[0].repoURL, first)
		}
	}
}

// --- 排序函数本身稳定性 ---
func TestSortProbesStable(t *testing.T) {
	probes := []sourceProbe{
		{repoURL: "a", latency: 7 * time.Millisecond},
		{repoURL: "b", latency: 5 * time.Millisecond},
		{repoURL: "c", latency: 9 * time.Millisecond},
		{repoURL: "d", latency: 5 * time.Millisecond},
	}
	sort.SliceStable(probes, func(i, j int) bool { return probes[i].latency < probes[j].latency })
	got := []string{probes[0].repoURL, probes[1].repoURL, probes[2].repoURL, probes[3].repoURL}
	want := []string{"b", "d", "a", "c"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("stable sort = %v, want %v", got, want)
	}
}
