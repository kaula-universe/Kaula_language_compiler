package sema

import (
	"strings"
	"testing"
)

// TestExternFuncCallAllowed 回归：extern fn 声明注册符号时 Type 记的是返回类型，
// Scope 为 "extern_func"。调用检查必须按 Scope 放行，否则所有 FFI 调用都会被
// 误判成 calling_non_callable（官方 test/bench 的 order_server.kl 即因此报错）。
func TestExternFuncCallAllowed(t *testing.T) {
	src := `extern fn putchar(c: i32) -> i32

fn main() -> i32 {
    putchar(65)
    return 0
}
`
	prog := parseForSema(t, src)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	summary := sa.errorCollector.GetErrorSummary()
	if strings.Contains(summary, "不是函数") {
		t.Fatalf("extern fn 调用被误判为非函数: %s", summary)
	}
	if sa.errorCollector.HasErrors() {
		t.Fatalf("extern fn 调用不应产生语义错误，实际: %s", summary)
	}
}

// TestNonCallableStillRejected 确保豁免只作用于 extern_func，
// 普通变量被当函数调用仍然报错（防止放宽过度导致回归）。
func TestNonCallableStillRejected(t *testing.T) {
	src := `fn main() -> i32 {
    i32 n = 42
    n(1)
    return 0
}
`
	prog := parseForSema(t, src)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	if !strings.Contains(sa.errorCollector.GetErrorSummary(), "不是函数") {
		t.Fatal("普通变量被当函数调用仍应报 calling_non_callable")
	}
}
