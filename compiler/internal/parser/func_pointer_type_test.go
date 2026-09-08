package parser_test

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// TestFuncPointerTypeInVarDecl 验证变量声明中带返回类型的函数指针类型
// void(int,int)int f 能被正确解析：类型为 "void(int,int)int"，变量名为 "f"。
// 修复前 parser 在声明上下文不吃返回类型，导致 "意外的类型关键字" 语法错误。
func TestFuncPointerTypeInVarDecl(t *testing.T) {
	src := "fn add(int a, int b) int { return a + b }\n" +
		"fn main() {\n" +
		"    void(int,int)int f = add\n" +
		"    int r = f(1, 2)\n" +
		"}\n"
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	prog := p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		t.Fatalf("函数指针变量声明产生 %d 个解析错误", n)
	}
	// main 函数体第一个语句应为变量声明，类型为 void(int,int)int
	mainFn := findFunction(t, prog, "main")
	if len(mainFn.Body) < 1 {
		t.Fatal("main 函数体为空")
	}
	vd, ok := mainFn.Body[0].(*ast.VariableDeclaration)
	if !ok {
		t.Fatalf("main 首语句不是 VariableDeclaration，是 %T", mainFn.Body[0])
	}
	if vd.Name != "f" {
		t.Errorf("变量名 = %q，期望 f", vd.Name)
	}
	if vd.Type != "void(int,int)int" {
		t.Errorf("变量类型 = %q，期望 void(int,int)int", vd.Type)
	}
}

// TestFuncPointerTypeAsParam 验证函数参数中函数指针类型 void(string)void cb
// 能被正确解析：参数名为 cb，类型为 void(string)void。
func TestFuncPointerTypeAsParam(t *testing.T) {
	src := "fn apply(void(string)void cb, string n) {\n    cb(n)\n}\n" +
		"fn main() {}\n"
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	prog := p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		t.Fatalf("函数指针参数产生 %d 个解析错误", n)
	}
	fn := findFunction(t, prog, "apply")
	if len(fn.Params) != 2 {
		t.Fatalf("参数数 = %d，期望 2", len(fn.Params))
	}
	if fn.Params[0] != "cb" {
		t.Errorf("首参数名 = %q，期望 cb", fn.Params[0])
	}
	if fn.ParamTypes[0] != "void(string)void" {
		t.Errorf("首参数类型 = %q，期望 void(string)void", fn.ParamTypes[0])
	}
}

// TestVoidSignatureDataPtrUnchanged 验证无返回类型的 void(int) 仍被解析为
// 数据指针类型（不贪婪吃后面的变量名），保持既有行为不变。
func TestVoidSignatureDataPtrUnchanged(t *testing.T) {
	src := "fn main() {\n    void(int) h = NULL\n}\n"
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	prog := p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		t.Fatalf("数据指针 void(int) 产生 %d 个解析错误", n)
	}
	mainFn := findFunction(t, prog, "main")
	vd, ok := mainFn.Body[0].(*ast.VariableDeclaration)
	if !ok {
		t.Fatalf("首语句不是 VariableDeclaration，是 %T", mainFn.Body[0])
	}
	if vd.Name != "h" {
		t.Errorf("变量名 = %q，期望 h", vd.Name)
	}
	if vd.Type != "void(int)" {
		t.Errorf("变量类型 = %q，期望 void(int)（数据指针不应吃变量名）", vd.Type)
	}
}

func findFunction(t *testing.T, prog *ast.Program, name string) *ast.FunctionStatement {
	t.Helper()
	for _, s := range prog.Statements {
		if fn, ok := s.(*ast.FunctionStatement); ok && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("未找到函数 %q", name)
	return nil
}
