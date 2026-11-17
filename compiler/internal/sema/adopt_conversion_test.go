package sema

import (
	"kaula/internal/ast"
	"testing"
)

// newAdoptTestAnalyzer 构造一个注册了测试套用类型的语义分析器
func newAdoptTestAnalyzer(t *testing.T) *SemanticAnalyzer {
	t.Helper()
	sa := NewSemanticAnalyzer()
	// #[transparent] type Size = adopt size_t
	sa.adoptTypes["Size"] = true
	sa.transparentAdopt["Size"] = true
	sa.adoptUnderlying["Size"] = "size_t"
	// #[transparent] type Count = adopt i64
	sa.adoptTypes["Count"] = true
	sa.transparentAdopt["Count"] = true
	sa.adoptUnderlying["Count"] = "i64"
	// 不透明句柄 type CStr = adopt const char*
	sa.adoptTypes["CStr"] = true
	sa.adoptUnderlying["CStr"] = "const char*"
	// 已存在的套用类型变量
	sa.symbolTable.AddSymbol("n", "Size", false, "local", 1, 1)
	sa.symbolTable.AddSymbol("c", "Count", false, "local", 1, 1)
	return sa
}

func intLit() *ast.IntegerLiteral       { return &ast.IntegerLiteral{Value: 1} }
func ident(name string) *ast.Identifier { return &ast.Identifier{Name: name} }
func adoptCall(arg ast.Expression) *ast.CallExpression {
	return &ast.CallExpression{Function: &ast.Identifier{Name: "adopt"}, Args: []ast.Expression{arg}}
}
func unwrapCall(arg ast.Expression) *ast.CallExpression {
	return &ast.CallExpression{Function: &ast.Identifier{Name: "unwrap"}, Args: []ast.Expression{arg}}
}

// TestAdoptImplicitConstructionRejected 进入套用类型必须显式 adopt
func TestAdoptImplicitConstructionRejected(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	// Size n = 1（字面量隐式构造）→ 报错
	sa.checkAdoptConversion("Size", intLit(), ast.Position{Line: 1, Column: 1}, "测试")
	if sa.errorCollector.ErrorCount() != 1 {
		t.Fatalf("字面量隐式构造套用类型应报 1 个错，实际 %d", sa.errorCollector.ErrorCount())
	}

	// 另一个套用类型隐式互转 Count c = n → 报错
	sa.errorCollector.Clear()
	sa.checkAdoptConversion("Count", ident("n"), ast.Position{Line: 1, Column: 1}, "测试")
	if sa.errorCollector.ErrorCount() != 1 {
		t.Fatalf("不同套用类型隐式互转应报错，实际 %d", sa.errorCollector.ErrorCount())
	}
}

// TestAdoptExplicitConstructionAccepted 显式 adopt 构造合法
func TestAdoptExplicitConstructionAccepted(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	// Size n = adopt(1)
	sa.checkAdoptConversion("Size", adoptCall(intLit()), ast.Position{}, "测试")
	// Size m = n（同类型直接传递）
	sa.checkAdoptConversion("Size", ident("n"), ast.Position{}, "测试")
	if sa.errorCollector.ErrorCount() != 0 {
		t.Fatalf("显式 adopt 与同类型传递不应报错，实际 %d", sa.errorCollector.ErrorCount())
	}
}

// TestAdoptImplicitUnwrapRejected 离开套用类型必须显式 unwrap
func TestAdoptImplicitUnwrapRejected(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	// i64 x = n（套用类型隐式解包到底层）→ 报错
	sa.checkAdoptConversion("i64", ident("n"), ast.Position{}, "测试")
	if sa.errorCollector.ErrorCount() != 1 {
		t.Fatalf("套用类型隐式解包应报错，实际 %d", sa.errorCollector.ErrorCount())
	}

	// i64 x = unwrap(n) → 合法
	sa.errorCollector.Clear()
	sa.checkAdoptConversion("i64", unwrapCall(ident("n")), ast.Position{}, "测试")
	if sa.errorCollector.ErrorCount() != 0 {
		t.Fatalf("显式 unwrap 不应报错，实际 %d", sa.errorCollector.ErrorCount())
	}
}

// TestAdoptNullToOpaqueHandle null 可赋给底层为指针的不透明句柄
func TestAdoptNullToOpaqueHandle(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	sa.checkAdoptConversion("CStr", ident("null"), ast.Position{}, "测试")
	if sa.errorCollector.ErrorCount() != 0 {
		t.Fatalf("null 赋给不透明句柄不应报错，实际 %d", sa.errorCollector.ErrorCount())
	}
}

// TestAdoptMisusedAdoptRejected adopt() 不能用于非套用类型目标
func TestAdoptMisusedAdoptRejected(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	// i64 x = adopt(1)，目标不是套用类型 → 报错
	sa.checkAdoptConversion("i64", adoptCall(intLit()), ast.Position{}, "测试")
	if sa.errorCollector.ErrorCount() != 1 {
		t.Fatalf("adopt() 用于非套用类型目标应报错，实际 %d", sa.errorCollector.ErrorCount())
	}
}

// TestAdoptPlainTypeUnaffected 普通类型赋值不受套用类型规则影响
func TestAdoptPlainTypeUnaffected(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	sa.checkAdoptConversion("i64", intLit(), ast.Position{}, "测试")
	sa.checkAdoptConversion("i32", intLit(), ast.Position{}, "测试")
	if sa.errorCollector.ErrorCount() != 0 {
		t.Fatalf("普通类型赋值不应受套用类型规则影响，实际 %d", sa.errorCollector.ErrorCount())
	}
}

// TestTransparentAdoptArithmeticKeepsType transparent 同类型算术结果保持套用类型
func TestTransparentAdoptArithmeticKeepsType(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	// n + n：两个 Size 相加，推导结果应仍为 Size
	expr := &ast.BinaryExpression{
		Operator: "+",
		Left:     ident("n"),
		Right:    ident("n"),
	}
	if got := sa.inferExpressionType(expr); got != "Size" {
		t.Fatalf("transparent 套用类型同类型算术结果应为 Size，实际 %q", got)
	}
}

// TestUnwrapInfersUnderlying unwrap(n) 应推导为底层类型 size_t
func TestUnwrapInfersUnderlying(t *testing.T) {
	sa := newAdoptTestAnalyzer(t)
	if got := sa.inferExpressionType(unwrapCall(ident("n"))); got != "size_t" {
		t.Fatalf("unwrap(n) 应推导为底层 size_t，实际 %q", got)
	}
}
