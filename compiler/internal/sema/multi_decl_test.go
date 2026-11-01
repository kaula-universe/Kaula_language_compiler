package sema

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

func parseForSema(t *testing.T, src string) *ast.Program {
	t.Helper()
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	prog := p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		t.Fatalf("解析产生 %d 个错误", n)
	}
	return prog
}

func findMainFn(t *testing.T, prog *ast.Program) *ast.FunctionStatement {
	t.Helper()
	for _, s := range prog.Statements {
		if fn, ok := s.(*ast.FunctionStatement); ok && fn.Name == "main" {
			return fn
		}
	}
	t.Fatal("找不到 main 函数")
	return nil
}

// TestMultiVarDeclSymbols 验证语义阶段把每个声明符都登记进符号表：
// 顶层 `i64 g1 = 1, g2 = 2` 展平后 g1/g2 都是 i64 全局符号。
func TestMultiVarDeclSymbols(t *testing.T) {
	src := "i64 g1 = 1, g2 = 2\n" +
		"fn main() {}\n"
	prog := parseForSema(t, src)

	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	// 展平：顶层语句变为 g1, g2, main
	if len(prog.Statements) != 3 {
		t.Fatalf("顶层语句数 = %d，期望 3（展平后）", len(prog.Statements))
	}
	for _, name := range []string{"g1", "g2"} {
		sym := sa.symbolTable.GetSymbol(name)
		if sym == nil {
			t.Fatalf("符号表缺少 %s", name)
		}
		if sym.Type != "i64" {
			t.Errorf("%s 的类型 = %q，期望 i64", name, sym.Type)
		}
	}
	if errs := sa.ErrorCollector().Errors(); len(errs) > 0 {
		for _, e := range errs {
			t.Logf("sema error %d:%d %s", e.Line, e.Column, e.Message)
		}
		t.Fatalf("语义分析产生 %d 个错误", len(errs))
	}
}

// TestMultiVarDeclInFunction 验证函数体内多变量声明逐个分析：
// 显式类型保持一致，auto 声明按各自初值独立推导类型。
func TestMultiVarDeclInFunction(t *testing.T) {
	src := "fn main() {\n" +
		"    i64 a = 1, b = 2\n" +
		"    auto p = 1, q = \"text\"\n" +
		"}\n"
	prog := parseForSema(t, src)

	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	mainFn := findMainFn(t, prog)
	// 展平后：a, b, p, q
	if len(mainFn.Body) != 4 {
		t.Fatalf("main 语句数 = %d，期望 4（展平后）", len(mainFn.Body))
	}
	types := map[string]string{}
	for _, s := range mainFn.Body {
		vd, ok := s.(*ast.VariableDeclaration)
		if !ok {
			t.Fatalf("语句是 %T，期望 VariableDeclaration", s)
		}
		types[vd.Name] = vd.Type
	}
	// p 的整数字面量默认 int（int64_t），q 推导为 string —— 两个 auto 各自独立推导
	want := map[string]string{"a": "i64", "b": "i64", "p": "int", "q": "string"}
	for name, expect := range want {
		if got := types[name]; got != expect {
			t.Errorf("%s 的类型 = %q，期望 %q", name, got, expect)
		}
	}
}

// TestMultiVarDeclInitializerReferencesEarlier 验证后续声明符可以引用先声明的变量：
// `i64 a = 1, b = a` 中 b 的初值 a 能解析到符号（不报未定义）。
func TestMultiVarDeclInitializerReferencesEarlier(t *testing.T) {
	src := "fn main() {\n" +
		"    i64 a = 1, b = a\n" +
		"    a = b\n" +
		"}\n"
	prog := parseForSema(t, src)

	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	if errs := sa.ErrorCollector().Errors(); len(errs) > 0 {
		for _, e := range errs {
			t.Logf("sema error %d:%d %s", e.Line, e.Column, e.Message)
		}
		t.Fatalf("语义分析产生 %d 个错误（b = a 应引用到已声明的 a）", len(errs))
	}
}
