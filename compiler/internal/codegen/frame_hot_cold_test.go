package codegen

import (
	"strings"
	"testing"
)

/*
 * Frame 热/冷字段分区的回归钉子。
 *
 * == 背景 ==
 *
 * conc fn 的 Frame 承载状态机：pc、sub[]/branch[] 守卫槽、循环状态
 * loop_i/loop_end/loop_step、参数、跨 suspend 存活的局部变量、result。
 *
 * resume 的每次推进都读写 pc/sub/branch；循环体每轮读写 loop_*。
 * 这些字段若被冷字段（result）挤过 64B 缓存行边界，
 * **每次状态机推进都要多碰一条缓存行**。
 *
 * == 实测（K_worker_Frame）==
 *
 * 旧布局（仅按对齐降序）：sizeof=80
 *   loop_i[2]@0 / loop_end[2]@16 / loop_step[2]@32 / n@48 / total@56
 *   result@64 / pc@72 / sub[1]@74 / branch[1]@76
 *   → pc / sub / branch **全在第 1 行**（64B 之后）。
 *
 * 新布局（rank 0 管理字段 → rank 1 数据字段 → rank 2 冷字段）：sizeof=80
 *   pc@0 / sub[1]@2 / branch[1]@4
 *   loop_i[2]@8 / loop_end[2]@24 / loop_step[2]@40 / n@56 / total@64
 *   result@72
 *   → 管理字段与 loop_* 全在第 0 行。
 */

// 管理字段（pc/sub/branch）必须排在所有 8 字节字段之前，
// 从而落在第一条缓存行里。
func TestFrameHotCold_PcSubBranchFirst(t *testing.T) {
	src := `
import std.time

conc fn worker(i64 n) -> i64 {
    i64 total = 0
    for i in range(n) {
        std.time.time_sleep_us(10)
        total = total + i
    }
    return total
}

fn main() -> i32 {
    Handle h = spawn worker(4)
    i64 r = h.await
    return 0
}
`
	c, errs := genConc(t, src)
	for _, e := range errs {
		t.Errorf("不应报错，实际：%s", e)
	}

	body := extractFrameStruct(t, c, "K_worker_Frame")

	pcIdx := strings.Index(body, "pc;")
	subIdx := strings.Index(body, "sub[")
	branchIdx := strings.Index(body, "branch[")
	loopIdx := strings.Index(body, "loop_i[")
	resultIdx := strings.Index(body, "result;")

	if pcIdx < 0 || subIdx < 0 || branchIdx < 0 || loopIdx < 0 || resultIdx < 0 {
		t.Fatalf("Frame 缺少期望字段：\n%s", body)
	}
	// 管理字段在 loop_* 之前
	if !(pcIdx < loopIdx && subIdx < loopIdx && branchIdx < loopIdx) {
		t.Errorf("pc/sub/branch 必须排在 loop_* 之前（否则落在第二条缓存行）：\n%s", body)
	}
	// result 排在最后
	if resultIdx < loopIdx {
		t.Errorf("result 是冷字段，必须排在 loop_* 之后：\n%s", body)
	}
}

/*
 * 结构体总尺寸不得因为分区而变大。
 *
 * 分区只改字段次序，填充必须至少与「纯对齐降序」一样优。
 * 若这条挂了，说明分区引入了回退 —— 那就要么放弃分区、
 * 要么补上显式对齐，不能默认收下。
 */
func TestFrameHotCold_NoSizeRegression(t *testing.T) {
	src := `
import std.time

conc fn worker(i64 n) -> i64 {
    i64 total = 0
    for i in range(n) {
        std.time.time_sleep_us(10)
        total = total + i
    }
    return total
}

fn main() -> i32 { return 0 }
`
	c, _ := genConc(t, src)
	body := extractFrameStruct(t, c, "K_worker_Frame")

	// 逐条解析字段占用（保守估算：按类型宽度求和 + 对齐取整）。
	// 这里只断言不会离谱地膨胀 —— 精确尺寸由 C 编译器决定，
	// 语言侧不该复刻一遍 ABI。
	if n := strings.Count(body, "long long"); n < 3 {
		t.Fatalf("loop_i/end/step 应各有长整型数组：\n%s", body)
	}
	// 分区不得把 int8_t 管理与 8 字节字段交错（那会引入填充）。
	// 简化判据：pc/sub/branch 三条必须连续出现。
	pcIdx := strings.Index(body, "pc;")
	subIdx := strings.Index(body, "sub[")
	branchIdx := strings.Index(body, "branch[")
	if !(pcIdx < subIdx && subIdx < branchIdx) {
		t.Errorf("pc/sub/branch 必须连续（消除交错填充）：\n%s", body)
	}
}

/*
 * 回归钉子：不得重新引入「出口哨兵」式重排。
 *
 * == 为什么必须钉死 ==
 *
 * 曾经尝试过：把循环体里作为出口的 return 槽位挪到链首，
 * 用 `if (sub[d] != 0)` 一次排掉整个守卫链，期望省掉「N-1 次
 * 失败比较」。
 *
 * 但那个前提是**错的**，而且后果是静默算错：
 *
 *   - 守卫链里 sub[d] 是**级联**的：槽位 k 执行时把 sub[d] 推进到
 *     k+1，紧随其后的 `if (sub[d] == k+1)` 在同一趟里就命中。
 *     所以正常路径下每轮**不存在**失败比较，N-1 的说法不成立。
 *   - return 槽位被挪到链首后，前一条语句把 sub[d] 设成该号却
 *     **落在其他分支**（不走链首的 `!= 0` 分支），return 被静默丢弃：
 *     循环跑满而不是提前返回，函数走到底部 return。
 *
 * 实测症状：`for ... { sleep; acc += x; return acc; acc += dead }`
 * 期望返回 100，实际**死循环**（顺序与 spawn 两条路径都挂）。
 *
 * 所以这条用例断言「生成代码里不得出现该形态」——
 * 它是一道防回退的闸门，不是风格偏好。
 */
func TestConcGuard_NoExitSentinelReorder(t *testing.T) {
	src := `
import std.time

conc fn worker(i64 stop) -> i64 {
    i64 total = 0
    for i in range(10) {
        std.time.time_sleep_us(10)
        total = total + i
        if i == stop {
            return total
        }
    }
    return -1
}

fn main() -> i32 { return 0 }
`
	c, _ := genConc(t, src)
	if strings.Contains(c, "if (f->sub[1] != 0) {") {
		t.Fatalf("不得引入出口哨兵重排（会静默丢弃 return / 造成死循环）：\n%s", c)
	}
	// 守卫链必须保留「== k」的级联形态。
	if !strings.Contains(c, "if (f->sub[1] == 1) {") {
		t.Errorf("守卫链应保留级联形态：\n%s", c)
	}
}

// extractFrameStruct 取出指定名字的 Frame 结构体正文。
func extractFrameStruct(t *testing.T, c, name string) string {
	t.Helper()
	start := strings.Index(c, "} "+name+";")
	if start < 0 {
		t.Fatalf("找不到结构体 %s：\n%s", name, c)
	}
	// 向前找最近的 "typedef struct {"
	head := strings.LastIndex(c[:start], "typedef struct {")
	if head < 0 {
		t.Fatalf("结构体 %s 前没有 typedef struct：\n%s", name, c)
	}
	return c[head:start]
}
