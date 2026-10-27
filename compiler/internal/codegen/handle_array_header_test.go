package codegen

import (
	"strings"
	"testing"
)

/*
 * 声明一个 Handle 数组就必须引入并发内核头。
 *
 * KaulaHandle 由 concurrency/kaula_conc.h 定义。`codeUsesConcurrency`
 * 原本只在 **用到** spawn / await / 可插桩调用时才置位 ——
 * 于是「只声明 Handle 数组、不 spawn」的 TU 拿不到那个头，
 * 生成的 C 里 KaulaHandle 是个未声明标识符。
 *
 * 实测（test_stack_array_budget.kl）：
 *
 *     [100000]Handle hs
 *     std.io.println("...")
 *
 * clang: use of undeclared identifier 'KaulaHandle'
 *
 * 失败形态具有欺骗性：报的是"未声明的标识符"，而用户的代码明明
 * 声明了 Handle 数组 —— 真实原因是类型定义没被引入。
 */

// 只声明 Handle 数组，不 spawn：仍须引入并发头
func TestHandleArrayDeclIncludesConcurrencyHeader(t *testing.T) {
	src := "fn main() -> i32 {\n" +
		"    [8]Handle hs\n" +
		"    return 0\n" +
		"}\n"

	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	if !strings.Contains(cCode, "kaula_conc.h") {
		t.Errorf("声明 Handle 数组的 TU 必须 #include \"concurrency/kaula_conc.h\"，"+
			"否则 KaulaHandle 未声明。实际 C：\n%s", firstLineOf(cCode))
	}
}

// 标量数组不需要并发头（别把头无差别塞进所有 TU）
func TestScalarArrayDeclDoesNotIncludeConcurrencyHeader(t *testing.T) {
	src := "fn main() -> i32 {\n" +
		"    [8]i64 buf\n" +
		"    return 0\n" +
		"}\n"

	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	if strings.Contains(cCode, "kaula_conc.h") {
		t.Errorf("纯标量数组不该引入并发头（无谓地拉高编译期依赖）：\n%s", firstLineOf(cCode))
	}
}
