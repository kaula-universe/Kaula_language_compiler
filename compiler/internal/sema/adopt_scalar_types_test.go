package sema

import (
	"testing"
)

// TestCScalarTypeBoolT 回归：freestanding/base/types.h 与 std/base/types.h 把
// bool_t / char_t 定义成标量 typedef（typedef bool bool_t; typedef char char_t;）。
// isCScalarType 白名单原先漏了它们，导致 std/ctypes.kl 里
//   #[transparent] type BoolT = adopt bool_t
// 被判成 non-scalar，整个 `import std.ctypes` 直接报
// adopt_transparent_non_scalar，编译无法开始。
func TestCScalarTypeBoolT(t *testing.T) {
	for _, name := range []string{"bool_t", "char_t", "bool", "_Bool"} {
		if !isCScalarType(name) {
			t.Errorf("isCScalarType(%q) 应为 true（官方头里的标量 typedef）", name)
		}
	}
	// 指针仍然不是标量，防止白名单放宽过度（回归保护）
	for _, name := range []string{"void*", "char*", "bool_t*"} {
		if isCScalarType(name) {
			t.Errorf("isCScalarType(%q) 应为 false（指针非标量）", name)
		}
	}
}

// TestTransparentAdoptScalarTypedefsAllowed 走完整 parse + Analyze，
// 确认透明套用标量 typedef 不再产生语义错误。
func TestTransparentAdoptScalarTypedefsAllowed(t *testing.T) {
	src := `#[transparent]
type BoolT = adopt bool_t

#[transparent]
type CharT = adopt char_t

fn main() -> i32 {
    return 0
}
`
	prog := parseForSema(t, src)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	if sa.errorCollector.HasErrors() {
		t.Fatalf("透明套用标量 typedef 不应产生语义错误，实际: %s",
			sa.errorCollector.GetErrorSummary())
	}
}

// TestTransparentAdoptPointerStillRejected 确保放宽只针对标量，
// 指针底层标 #[transparent] 依然被拒绝（防止回归）。
func TestTransparentAdoptPointerStillRejected(t *testing.T) {
	src := `#[transparent]
type Raw = adopt void*

fn main() -> i32 {
    return 0
}
`
	prog := parseForSema(t, src)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	if !sa.errorCollector.HasErrors() {
		t.Fatal("指针底层标 #[transparent] 仍应报 adopt_transparent_non_scalar")
	}
}
