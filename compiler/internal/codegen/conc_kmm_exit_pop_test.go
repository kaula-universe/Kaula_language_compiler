package codegen

import (
	"regexp"
	"strings"
	"testing"
)

// 本文件守护 KBUG_KMM_SCOPE_EARLY_RETURN：
// `conc fn` 内 KMM 作用域中的**提前 return** 必须弹出作用域栈。
//
// 背景：块级 KMM 区间用 `KMM_V4_SCOPE_START { ... } KMM_V4_SCOPE_END;`
// （展开为 push + do{}while(0) + pop）包裹。`return` 直接跳出函数，
// 绕过 pop → 每执行一次提前 return 就在本线程 TLS 定长作用域栈上
// 永久泄漏一帧；累计 KMM_V4_MAX_SCOPE_DEPTH（1024）后运行期
// `KMM FATAL: Scope stack overflow` → abort。
//
// 对「每连接一个 conc fn」的服务形态，连接 churn 就是 DoS。
// 修复：generateConcReturn 按 kmmScopeDepth 补齐 kmm_v4_scope_pop()。

// concFnWithAllocAndEarlyReturn 是一个最小复现：conc fn 的**循环体内**
// 做 KMM 分配（这才会让循环体被块级 KMM 区间 KMM_V4_SCOPE_START 包裹），
// 并在同一循环体内提前 return。
//
// 关键：分配必须在循环体内。若只在函数顶部分配，分配会被提升为
// survivor 帧变量、循环体不产生块级 KMM 区间，也就没有可泄漏的帧
// —— 那样测不到本 bug（早期用顶部分配的用例即因生成代码里根本没有
// KMM_V4_SCOPE_START 而无法覆盖）。
const concFnWithAllocAndEarlyReturn = `extern fn net_recv(fd: i32, buf: *u8, len: i32) -> i64
extern fn tls_result() -> i32
extern fn do_close(fd: i32) -> i32
extern fn at_of(buf: *u8, off: i32) -> *u8

import std.memory

conc fn conn_task(fd: i32) -> i32 {
    u8* buf = std.memory.kmm_v4_alloc(8192)
    while (true) {
        net_recv(fd, at_of(buf, 0), 8192)
        i32 got = tls_result()
        if (got <= 0) {
            do_close(fd)
            return 0
        }
        u8* r = std.memory.kmm_v4_alloc(64)
        r[0] = as<u8>(got)
        std.memory.kmm_v4_free(r)
    }
    return 0
}

fn main() -> i32 {
    return 0
}
`

// scopePopBalance 统计生成 C 中 KMM scope 的 push / pop 计数。
//
// KMM_V4_SCOPE_START / KMM_V4_SCOPE_NAMED(...) 展开为 kmm_v4_scope_push()，
// KMM_V4_SCOPE_END 展开为 kmm_v4_scope_pop()，另加显式 kmm_v4_scope_pop()。
// 平衡判据：KMMEnd 宏数 + 显式 scope_pop 数 == KMMStart 宏数
// （每个 KMMStart 恰好配一个 KMMEnd 宏，宏内自带一次 pop；
//   显式 pop 是给提前退出路径补的）。
type scopeCounts struct {
	starts int // KMM_V4_SCOPE_START / NAMED 出现次数（= push 次数）
	ends   int // KMM_V4_SCOPE_END 出现次数（宏内自带一次 pop）
	explic int // 显式 kmm_v4_scope_pop() 次数（提前退出补的）
}

func countScopeTokens(t *testing.T, cCode string) scopeCounts {
	t.Helper()
	count := func(re *regexp.Regexp) int {
		return len(re.FindAllString(cCode, -1))
	}
	return scopeCounts{
		starts: count(regexp.MustCompile(`KMM_V4_SCOPE_(START|NAMED\()`)),
		ends:   count(regexp.MustCompile(`KMM_V4_SCOPE_END`)),
		explic: count(regexp.MustCompile(`kmm_v4_scope_pop\(\)`)),
	}
}

// TestConcFnEarlyReturnPopsKMMScope 断言 conc fn 的提前 return 会补弹栈：
// 生成 C 中必须存在「显式 kmm_v4_scope_pop()」且其总数足以让每个提前
// 退出路径都能配平（不再是泄漏）。这是本次修复的核心回归锁。
func TestConcFnEarlyReturnPopsKMMScope(t *testing.T) {
	cCode := generateForMarkTest(t, concFnWithAllocAndEarlyReturn)

	c := countScopeTokens(t, cCode)

	// 每个 KMM_V4_SCOPE_START 必须配一个 KMM_V4_SCOPE_END 宏。
	if c.starts != c.ends {
		t.Fatalf("KMM scope 区间 start/end 不配对：start=%d end=%d\n%s",
			c.starts, c.ends, cCode)
	}
	// 关键断言：必须存在显式 pop（提前 return 补的）。
	// 修复前该值为 0（挂起 return 的那次除外），修复后 > 0。
	if c.explic == 0 {
		t.Fatalf("conc fn 的提前 return 未补 kmm_v4_scope_pop()（KBUG 回归）:\n%s", cCode)
	}
	// 且显式 pop 必须出现在一个 DONE return 之前：
	// 形如 `kmm_v4_scope_pop();\n ... return (KaulaResumeResult){ .status = KAULA_RESUME_DONE`
	donePop := regexp.MustCompile(
		`kmm_v4_scope_pop\(\);\s*\n\s*f->pc = -1;\s*\n\s*return \(KaulaResumeResult\)\{ \.status = KAULA_RESUME_DONE`)
	if !donePop.MatchString(cCode) {
		t.Fatalf("显式 scope_pop 未出现在 DONE return 之前（提前 return 仍在泄漏）:\n%s", cCode)
	}
}

// TestConcFnScopeDepthMatchesSuspendPath 断言提前 return 的补弹深度
// 与挂起 return 采用同一口径（kmmScopeDepth），即同一函数体内
// 「提前 return 前连续 pop 数」不应超过「挂起 return 前连续 pop 数」——
// 多弹一层会导致作用域栈下溢，同样是 abort。
func TestConcFnScopeDepthMatchesSuspendPath(t *testing.T) {
	cCode := generateForMarkTest(t, concFnWithAllocAndEarlyReturn)

	// 提取每个 return 之前连续的 kmm_v4_scope_pop() 次数。
	lines := strings.Split(cCode, "\n")
	maxRun := 0
	run := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "kmm_v4_scope_pop();") {
			run++
			if run > maxRun {
				maxRun = run
			}
			continue
		}
		if strings.Contains(trimmed, "return (KaulaResumeResult)") {
			run = 0
			continue
		}
		if trimmed != "" {
			run = 0
		}
	}
	// 本用例嵌套深度为 1（单层 while 的 KMM 区间），
	// 故任何 return 前最多 1 次连续 pop。若为 2 及以上即为多弹。
	if maxRun > 1 {
		t.Fatalf("提前 return 前连续 pop 次数 = %d，超过本用例的作用域深度 1（会下溢 abort）:\n%s",
			maxRun, cCode)
	}
	if maxRun < 1 {
		t.Fatalf("提前 return 前没有任何 pop（泄漏回归）:\n%s", cCode)
	}
}
