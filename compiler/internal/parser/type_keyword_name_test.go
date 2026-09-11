package parser_test

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// TestTypeNameKeywordAsFunction 验证函数名与类型关键字同名（int/double/float/
// bool/char/string/void）时 parser 不再整体错位、级联报语法错误，
// 且能正确解析出函数名、参数与调用表达式。
func TestTypeNameKeywordAsFunction(t *testing.T) {
	names := []string{"int", "double", "float", "bool", "char", "string"}
	for _, nm := range names {
		src := "fn " + nm + "(int x) int {\n    return x\n}\n" +
			"fn main() {\n    int a = " + nm + "(1)\n}\n"
		lx := lexer.NewLexer(src)
		p := parser.NewParser(lx)
		prog := p.Parse()
		// 这些源码不应产生任何解析错误（修复前会级联出 4 个语法错误）
		if n := p.GetErrorCollector().ErrorCount(); n != 0 {
			t.Errorf("函数名 %q 产生 %d 个解析错误", nm, n)
			continue
		}
		if len(prog.Statements) == 0 {
			t.Errorf("函数名 %q: 无语句", nm)
			continue
		}
		// 第一个函数语句的名字应被正确识别为关键字本身
		fn, ok := prog.Statements[0].(*ast.FunctionStatement)
		if !ok {
			t.Errorf("函数名 %q: 首语句不是 FunctionStatement", nm)
			continue
		}
		if fn.Name != nm {
			t.Errorf("函数名 %q 被解析为 %q", nm, fn.Name)
		}
		if len(fn.Params) != 1 {
			t.Errorf("函数名 %q: 参数个数=%d, 期望 1", nm, len(fn.Params))
		}
	}
}

// TestNormalFunctionNameUnchanged 普通标识符函数名行为不变（回归）。
func TestNormalFunctionNameUnchanged(t *testing.T) {
	src := "fn twice(int x) int {\n    return x * 2\n}\n" +
		"fn main() {\n    int a = twice(3)\n}\n"
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	prog := p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		t.Fatalf("普通函数名产生 %d 个解析错误", n)
	}
	fn, ok := prog.Statements[0].(*ast.FunctionStatement)
	if !ok || fn.Name != "twice" {
		t.Fatalf("普通函数名解析异常: %#v", prog.Statements[0])
	}
}
