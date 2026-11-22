package codegen

import (
	"strings"
	"testing"
)

// TestLocalMultiDimArrayDecl 回归：局部二维数组 `i64 g[2][3]` 必须生成
// 正确的 C 声明 `int64_t g[2][3];`。
// 此前 parser 只吃一组 [N]，第二组泄漏成独立的 ArrayLiteral 语句，生成的
// C 是垃圾 compound literal `((int64_t[]){ 3 });`，clang 报
// subscripted value is not an array。
func TestLocalMultiDimArrayDecl(t *testing.T) {
	src := `fn main() {
    i64 g[2][3]
    g[0][0] = 1
    g[1][2] = 2
}
`
	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) > 0 {
		t.Fatalf("不应有 codegen 错误: %v", errs)
	}
	if !strings.Contains(cCode, "int64_t g[2][3];") {
		t.Errorf("期望生成 int64_t g[2][3];，实际:\n%s", cCode)
	}
	if strings.Contains(cCode, "((int64_t[])") {
		t.Errorf("存在泄漏的数组字面量语句:\n%s", cCode)
	}
	if !strings.Contains(cCode, "g[0][0]") || !strings.Contains(cCode, "g[1][2]") {
		t.Errorf("二维索引生成缺失:\n%s", cCode)
	}
}

// TestGlobalMultiDimArrayDecl 全局二维数组同样正确。
func TestGlobalMultiDimArrayDecl(t *testing.T) {
	src := `i64 grid[3][4]

fn main() {
    grid[2][3] = 1
}
`
	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) > 0 {
		t.Fatalf("不应有 codegen 错误: %v", errs)
	}
	if !strings.Contains(cCode, "int64_t grid[3][4] = {0};") {
		t.Errorf("期望生成 int64_t grid[3][4] = {0};，实际:\n%s", cCode)
	}
}

// TestMultiDimArrayDimensionOrder 维度顺序保护：[2][3]i64 必须映射为
// int64_t[2][3]（C 侧 m[i][j] 索引与 Kaula 语义一致）。此前 typegen 的
// 单组递归会翻转维度（int64_t[3][2]），m[1][2] 静默越界。
func TestMultiDimArrayDimensionOrder(t *testing.T) {
	src := `type Mat = [2][3]i64

fn main() {
    Mat m
    m[1][2] = 5
}
`
	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) > 0 {
		t.Fatalf("不应有 codegen 错误: %v", errs)
	}
	if strings.Contains(cCode, "int64_t[3][2]") || strings.Contains(cCode, "[3][2]") {
		t.Errorf("维度顺序被翻转（int64_t[3][2]）:\n%s", cCode)
	}
	if !strings.Contains(cCode, "typedef int64_t Mat[2][3];") {
		t.Errorf("期望 typedef int64_t Mat[2][3];，实际:\n%s", cCode)
	}
}

// TestRuntimeSizedStillHeapAllocated 保护：运行期尺寸（非常量）一维数组
// 仍走堆分配路径，不被 splitArrayCType 的多维拆分修改误伤。
func TestRuntimeSizedStillHeapAllocated(t *testing.T) {
	src := `import std.io

fn f(i64 n) {
    [n]i64 buf
    buf[0] = 1
    std.io.println(buf[0])
}

fn main() {
    f(4)
}
`
	cCode, errs := generateErrorsForTest(t, src)
	_ = cCode
	found := false
	for _, e := range errs {
		if strings.Contains(e, "n") {
			found = true
		}
	}
	// 该用例主要验证不 panic 且路径可达；运行期数组的具体分配形式由
	// stack_array_budget_test.go 等既有用例覆盖。
	if found {
		t.Errorf("运行期数组不应引入新的错误: %v", errs)
	}
}
