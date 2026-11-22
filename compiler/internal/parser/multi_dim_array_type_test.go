package parser_test

import (
	"testing"

	"kaula/internal/ast"
)

// TestMultiDimArraySuffixDecl 回归：局部二维数组 `i64 g[2][3]` 的类型串
// 必须是 "[2][3]i64"（此前只吃一组 [N]，第二组泄漏成独立的 ArrayLiteral
// 语句，生成的 C 是垃圾 compound literal）。
func TestMultiDimArraySuffixDeclTypeString(t *testing.T) {
	src := `fn main() {
    i64 g[2][3]
    g[0][0] = 1
}
`
	prog := parseProgramForTest(t, src)
	for _, s := range prog.Statements {
		fn, ok := s.(*ast.FunctionStatement)
		if !ok || fn.Name != "main" {
			continue
		}
		if len(fn.Body) != 2 {
			t.Fatalf("main 体应为 2 条语句（声明 + 赋值），实际 %d", len(fn.Body))
		}
		vd, ok := fn.Body[0].(*ast.VariableDeclaration)
		if !ok {
			t.Fatalf("第 1 条语句应是声明，实际 %T", fn.Body[0])
		}
		if vd.Type != "[2][3]i64" {
			t.Fatalf("二维数组类型串 = %q，期望 [2][3]i64", vd.Type)
		}
	}
}
