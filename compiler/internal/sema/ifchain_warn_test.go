package sema

import (
	"testing"

	"kaula/internal/ast"
)

// buildEqIfChain 构造 `if (x == c1) { ... } else if (x == c2) { ... } ...`
// 形态的 if 链；withElse=true 时末尾带 else 块。
func buildEqIfChain(varName string, consts []uint64, withElse bool) *ast.IfStatement {
	cond := func(v uint64) ast.Expression {
		return &ast.BinaryExpression{
			Left:     &ast.Identifier{Name: varName},
			Operator: "==",
			Right:    &ast.IntegerLiteral{Value: v},
		}
	}
	var root *ast.IfStatement
	var last *ast.IfStatement
	for _, v := range consts {
		n := &ast.IfStatement{
			Condition: cond(v),
			Body:      []ast.Statement{},
		}
		if root == nil {
			root = n
		} else {
			last.Else = []ast.Statement{n}
		}
		last = n
	}
	if withElse {
		last.Else = []ast.Statement{}
	}
	return root
}

// TestIfChainMatchWarningSuppressedWhenOptimized 链会被前端跳转表优化自动
// 折叠时，"建议改用 match" 告警必须消失——顺序比较已被编译器消掉，继续提示
// 只会误导用户手工改写成 match（反而丢掉自动优化）。
func TestIfChainMatchWarningSuppressedWhenOptimized(t *testing.T) {
	orig := IfChainWillBeOptimized
	defer func() { IfChainWillBeOptimized = orig }()

	sa := NewSemanticAnalyzer()

	// 1) 未注入（单测/其它调用方的默认状态）：保持原有行为，照旧告警
	IfChainWillBeOptimized = nil
	sa.errorCollector.Clear()
	sa.checkIfChainForMatch(buildEqIfChain("x", []uint64{1, 2, 3}, true))
	if got := sa.errorCollector.WarningCount(); got != 1 {
		t.Fatalf("未注入时应照旧产生 1 条告警，实际 %d", got)
	}

	// 2) 注入且判定为"会被优化"：告警被抑制
	IfChainWillBeOptimized = func(stmt *ast.IfStatement) bool { return true }
	sa.errorCollector.Clear()
	sa.checkIfChainForMatch(buildEqIfChain("x", []uint64{1, 2, 3}, true))
	if got := sa.errorCollector.WarningCount(); got != 0 {
		t.Fatalf("会被自动折叠的链不应再告警，实际 %d", got)
	}

	// 3) 注入但判定为"不会被优化"（短链/含 break/-O0）：仍须告警
	IfChainWillBeOptimized = func(stmt *ast.IfStatement) bool { return false }
	sa.errorCollector.Clear()
	sa.checkIfChainForMatch(buildEqIfChain("x", []uint64{1, 2, 3}, true))
	if got := sa.errorCollector.WarningCount(); got != 1 {
		t.Fatalf("不会被优化的链仍应告警，实际 %d", got)
	}
}

// TestIfChainMatchWarningUnaffectedForOtherChains 抑制只作用于"会被优化"的链，
// 不得把语义分析对其它链的判定也一起关掉。
func TestIfChainMatchWarningUnaffectedForOtherChains(t *testing.T) {
	orig := IfChainWillBeOptimized
	defer func() { IfChainWillBeOptimized = orig }()

	sa := NewSemanticAnalyzer()

	// 不同变量比较：本就不触发该告警（与注入无关）
	IfChainWillBeOptimized = func(stmt *ast.IfStatement) bool { return false }
	sa.errorCollector.Clear()
	mixed := buildEqIfChain("x", []uint64{1, 2}, false)
	if nested, ok := mixed.Else[0].(*ast.IfStatement); ok {
		nested.Condition = &ast.BinaryExpression{
			Left:     &ast.Identifier{Name: "y"},
			Operator: "==",
			Right:    &ast.IntegerLiteral{Value: 2},
		}
	}
	sa.checkIfChainForMatch(mixed)
	if got := sa.errorCollector.WarningCount(); got != 0 {
		t.Fatalf("非同一变量的链本就不应告警，实际 %d", got)
	}
}
