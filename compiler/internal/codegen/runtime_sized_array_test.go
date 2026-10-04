package codegen

import (
	"strings"
	"testing"
)

/*
 * 运行期长度的局部数组必须走堆，不能走栈。
 *
 * `[n]T buf` 的 n 是运行期变量，编译期算不出字节数，而局部数组默认
 * 是栈声明 —— C 侧就成了变长数组（VLA），占的是栈。
 *
 * 栈预算不随 n 增长，于是这是一个**编译期无法拦截**的越界：
 *
 *     fn f(i64 n) {
 *         Handle hs[n]     // n=100000 → 1.6 MB
 *         ...
 *     }
 *
 * 实测（test/concurrency/test_vla_stack_probe.kl，KaulaHandle 16 字节）：
 *
 *     v_alloc_only  (同形数组，只 spawn 不 await)      通过
 *     v_forin_decl  (同形数组 + for..in + 声明位置 await) 0xC00000FD
 *     v_while_decl  (同形数组 + while + 声明位置 await)    0xC00000FD
 *
 * 同样的数组声明，一个通过一个崩 —— 差别只是调用链多深了几帧。
 * 也就是说「这段代码会不会崩」取决于无关代码的改动，
 * 这是最坏的一类缺陷：不可复现、不可推理。
 *
 * 定长（字面量长度）另有 `_Static_assert` 兜底
 * （见 stack_array_budget_test.go）；运行期长度那条路只能靠堆分配。
 */

// 运行期长度 → 堆分配，且类型退化成指针（下标语义不变）
func TestRuntimeSizedLocalArrayIsHeapAllocated(t *testing.T) {
	src := "fn main() -> i32 {\n" +
		"    i64 n = 8\n" +
		"    [n]i64 hs\n" +
		"    hs[0] = 1\n" +
		"    return 0\n" +
		"}\n"

	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}

	if !strings.Contains(cCode, "kmm_v4_alloc") {
		t.Errorf("运行期长度的局部数组应走堆分配（kmm_v4_alloc*），"+
			"栈预算不随 n 增长。实际 C：\n%s", firstLineOf(cCode))
	}
	/*
	 * 必须走 survivor（kmm_v4_alloc_global），不能走 TLAB（kmm_v4_alloc_auto）。
	 *
	 * TLAB 是固定额度的 bump 区，超出后行为未定义；survivor 段对大块有
	 * malloc 兜底。运行期长度的数组在编译期没有尺寸上界，只能走后者。
	 *
	 * 实测（Handle 16 字节、池 2 MB）：TLAB 路径 n=60000 正确，
	 * n=100000（1.6 MB）起答案全错 —— 静默数据损坏。
	 */
	if strings.Contains(cCode, "kmm_v4_alloc_auto(sizeof(int64_t) * (n)") {
		t.Errorf("运行期长度的局部数组不得走 TLAB（kmm_v4_alloc_auto）："+
			"尺寸无上界，超额后是静默数据损坏而不是报错。实际 C：\n%s", firstLineOf(cCode))
	}
	if !strings.Contains(cCode, "kmm_v4_alloc_global(sizeof(int64_t) * (n)") {
		t.Errorf("运行期长度的局部数组应走 survivor（kmm_v4_alloc_global，" +
			"对大块有 malloc 兜底）：\n" + firstLineOf(cCode))
	}
	// 不能再是 VLA
	if strings.Contains(cCode, "int64_t hs[n];") {
		t.Errorf("运行期长度的局部数组不得生成 VLA 栈声明 int64_t hs[n];：\n%s",
			firstLineOf(cCode))
	}
}

// 堆分配后下标仍可用：不能把数组的其余用法改坏
func TestHeapAllocatedArrayKeepsIndexing(t *testing.T) {
	src := "fn main() -> i32 {\n" +
		"    i64 n = 8\n" +
		"    [n]i64 hs\n" +
		"    hs[0] = 1\n" +
		"    hs[3] = 4\n" +
		"    return 0\n" +
		"}\n"

	cCode, _ := generateErrorsForTest(t, src)

	if !strings.Contains(cCode, "hs[0] = 1;") || !strings.Contains(cCode, "hs[3] = 4;") {
		t.Errorf("堆分配不应改变下标赋值的写法：\n%s", firstLineOf(cCode))
	}
}

// 定长数组仍走栈（小数组留在栈上是对的，没有分配开销）
func TestLiteralSizedLocalArrayStaysOnStack(t *testing.T) {
	src := "fn main() -> i32 {\n" +
		"    [16]i64 buf\n" +
		"    buf[0] = 1\n" +
		"}\n"

	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	if !strings.Contains(cCode, "buf[16]") {
		t.Errorf("定长小数组应留在栈上（无分配开销）：\n%s", firstLineOf(cCode))
	}
	if strings.Contains(cCode, "kmm_v4_alloc(sizeof(int64_t) * (16)") {
		t.Errorf("定长小数组不该改成堆分配：\n%s", firstLineOf(cCode))
	}
}
