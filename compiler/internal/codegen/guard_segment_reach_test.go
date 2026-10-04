package codegen

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

/*
 * 不变量：每个 resume 段都必须有一条 `f->pc = <该段序号>` 的写入。
 *
 * 段是 switch 的 case，而 case 只能通过 pc 到达。没有 pc 写入的段
 * 是**不可达**的 —— 它的语句永远不执行。
 *
 * 这条不变量曾经被破坏：
 *
 *     while i < n { std.time.time_sleep(1); ... }   // 守卫组
 *     std.time.time_sleep(1)                        // 组尾的顶层挂起
 *     if acc == 0 { acc = 42 }                      // 下一组
 *
 * 守卫组里的顶层挂起走"两个相邻槽位"留在**本段**（pc 不推进），
 * 但组尾仍然 finishSegment() 另起一段。于是新段没有 pc 写入 ——
 * 不可达。段 0 恢复后读完 IO 结果就撞上段尾兜底 return DONE，
 * `if acc == 0` 那条语句从未执行。
 *
 * 实测：acc 得到 0，期望 42。不崩、不报错，只是静默给错答案。
 * 见 test/concurrency/test_guard_slot_residue.kl 用例 B。
 */

var pcWriteRe = regexp.MustCompile(`f->pc = (\d+);`)

// switchCaseRe 匹配 resume 里 `case N: {`
var switchCaseRe = regexp.MustCompile(`\n\s*case (\d+): \{`)

// assertEverySegmentReachable 检查生成的 resume：每个 case N（N>0）
// 都必须有一条 `f->pc = N;` 把控制流转过来。
func assertEverySegmentReachable(t *testing.T, c, label string) {
	t.Helper()

	written := map[int]bool{}
	for _, m := range pcWriteRe.FindAllStringSubmatch(c, -1) {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil {
			written[n] = true
		}
	}

	var cases []int
	for _, m := range switchCaseRe.FindAllStringSubmatch(c, -1) {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil {
			cases = append(cases, n)
		}
	}

	for _, n := range cases {
		if n == 0 {
			continue // 入口段天然可达
		}
		if !written[n] {
			t.Errorf("%s：段 %d 不可达 —— 没有任何 `f->pc = %d;` 把它接过来。"+
				"该段的语句永远不会执行（表现为静默返回错值）。\n"+
				"已有的 pc 写入目标：%v", label, n, n, sortedKeys(written))
		}
	}
}

func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// 守卫组 + 组尾顶层挂起 + 后续语句：每段都必须可达
func TestConcGuard_AllSegmentsReachable_GuardedGroupTrailingSuspend(t *testing.T) {
	src := `
import std.time

conc fn case_b(i64 n) -> i64 {
    i64 acc = 0
    i64 i = 0
    while i < n {
        std.time.time_sleep(1)
        i = i + 1
        if i >= n {
            break
        }
    }
    std.time.time_sleep(1)
    if acc == 0 {
        acc = 42
    }
    return acc
}

fn main() -> i32 {
    return 0
}
`
	c, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	assertEverySegmentReachable(t, c, "守卫组尾挂起")
}

// 多段守卫：连续多个顶层挂起，每段都要可达
func TestConcGuard_AllSegmentsReachable_MultipleGuardedSuspends(t *testing.T) {
	src := `
import std.time

conc fn many(i64 n) -> i64 {
    i64 acc = 0
    i64 i = 0
    while i < n {
        std.time.time_sleep(1)
        i = i + 1
    }
    std.time.time_sleep(1)
    if acc == 0 {
        acc = 1
    }
    std.time.time_sleep(1)
    if acc == 1 {
        acc = 2
    }
    std.time.time_sleep(1)
    return acc
}

fn main() -> i32 {
    return 0
}
`
	c, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	assertEverySegmentReachable(t, c, "多段守卫挂起")
}

// 非守卫的经典两段式：每段都必须可达（回归防护）
func TestConcGuard_AllSegmentsReachable_NonGuarded(t *testing.T) {
	src := `
import std.time

conc fn plain(i64 n) -> i64 {
    i64 acc = 0
    std.time.time_sleep(1)
    acc = acc + 1
    std.time.time_sleep(1)
    acc = acc + 1
    return acc
}

fn main() -> i32 {
    return 0
}
`
	c, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	assertEverySegmentReachable(t, c, "非守卫两段式")
	if !strings.Contains(c, "f->pc = 1;") || !strings.Contains(c, "f->pc = 2;") {
		t.Errorf("两个顶层挂起点应分别切到段 1 与段 2：\n%s", c)
	}
}
