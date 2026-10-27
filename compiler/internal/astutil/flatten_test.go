package astutil_test

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/astutil"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

func parseForFlatten(t *testing.T, src string) *ast.Program {
	t.Helper()
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	prog := p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		t.Fatalf("解析产生 %d 个错误", n)
	}
	return prog
}

// TestFlattenDeclarationsNested 验证多变量声明在各种嵌套位置都能展平为独立语句：
// 顶层、函数体、if 分支、裸块，并且重复调用幂等。
func TestFlattenDeclarationsNested(t *testing.T) {
	src := "i64 g1 = 1, g2 = 2\n" +
		"fn main() {\n" +
		"    i64 a = 1, b = 2\n" +
		"    if true {\n" +
		"        i64 c = 3, d = 4\n" +
		"    }\n" +
		"    {\n" +
		"        i64 e = 5, f = 6\n" +
		"    }\n" +
		"}\n"
	prog := parseForFlatten(t, src)

	astutil.FlattenDeclarations(prog)

	// 顶层：g1, g2, fn
	if len(prog.Statements) != 3 {
		t.Fatalf("顶层语句数 = %d，期望 3", len(prog.Statements))
	}
	g1, ok := prog.Statements[0].(*ast.VariableDeclaration)
	if !ok {
		t.Fatalf("顶层首语句是 %T，期望 VariableDeclaration", prog.Statements[0])
	}
	if g1.Name != "g1" || len(g1.More) != 0 {
		t.Errorf("g1 = %+v，期望独立声明且 More 已清空", g1)
	}
	g2, ok := prog.Statements[1].(*ast.VariableDeclaration)
	if !ok || g2.Name != "g2" {
		t.Errorf("顶层第 2 条语句 = %v %T，期望 g2", prog.Statements[1], prog.Statements[1])
	}

	mainFn, ok := prog.Statements[2].(*ast.FunctionStatement)
	if !ok {
		t.Fatalf("顶层第 3 条语句是 %T，期望 FunctionStatement", prog.Statements[2])
	}
	// 函数体：a, b, if, block
	if len(mainFn.Body) != 4 {
		t.Fatalf("main 语句数 = %d，期望 4", len(mainFn.Body))
	}
	ifStmt, ok := mainFn.Body[2].(*ast.IfStatement)
	if !ok {
		t.Fatalf("第 3 条语句是 %T，期望 IfStatement", mainFn.Body[2])
	}
	if len(ifStmt.Body) != 2 {
		t.Errorf("if 分支语句数 = %d，期望 2", len(ifStmt.Body))
	}
	block, ok := mainFn.Body[3].(*ast.BlockStatement)
	if !ok {
		t.Fatalf("第 4 条语句是 %T，期望 BlockStatement", mainFn.Body[3])
	}
	if len(block.Statements) != 2 {
		t.Errorf("块内语句数 = %d，期望 2", len(block.Statements))
	}

	// 幂等：再次展平不改变语句数
	before := len(prog.Statements)
	astutil.FlattenDeclarations(prog)
	if len(prog.Statements) != before {
		t.Errorf("重复展平后顶层语句数 = %d，期望仍是 %d", len(prog.Statements), before)
	}
	if len(mainFn.Body) != 4 {
		t.Errorf("重复展平后 main 语句数 = %d，期望仍是 4", len(mainFn.Body))
	}
}

// TestFlattenDeclarationsNil 验证 nil 入参不 panic
func TestFlattenDeclarationsNil(t *testing.T) {
	astutil.FlattenDeclarations(nil)
}
