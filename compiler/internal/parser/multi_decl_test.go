package parser_test

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// parseMultiDecl 解析源码并在有语法错误时直接失败
func parseMultiDecl(t *testing.T, src string) *ast.Program {
	t.Helper()
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	prog := p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		for _, e := range p.GetErrorCollector().Errors() {
			t.Logf("parse error %d:%d %s", e.Line, e.Column, e.Message)
		}
		t.Fatalf("解析产生 %d 个错误", n)
	}
	return prog
}

// declInMain 返回 main 函数体第 index 条语句的变量声明
func declInMain(t *testing.T, prog *ast.Program, index int) *ast.VariableDeclaration {
	t.Helper()
	mainFn := findFunction(t, prog, "main")
	if index >= len(mainFn.Body) {
		t.Fatalf("main 函数体只有 %d 条语句，取不到第 %d 条", len(mainFn.Body), index)
	}
	vd, ok := mainFn.Body[index].(*ast.VariableDeclaration)
	if !ok {
		t.Fatalf("main 第 %d 条语句不是 VariableDeclaration，是 %T", index, mainFn.Body[index])
	}
	return vd
}

// TestMultiVarDeclBasic 验证 `i64 a = 1, b = 2` 解析为一条声明语句：
// 主声明承载 a，More 承载 b，BaseType 记录共享类型。
func TestMultiVarDeclBasic(t *testing.T) {
	src := "fn main() {\n    i64 a = 1, b = 2\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if vd.Name != "a" || vd.Type != "i64" {
		t.Errorf("主声明 = %s %s，期望 i64 a", vd.Type, vd.Name)
	}
	if len(vd.More) != 1 {
		t.Fatalf("More 长度 = %d，期望 1", len(vd.More))
	}
	if vd.More[0].Name != "b" {
		t.Errorf("后续声明符名 = %q，期望 b", vd.More[0].Name)
	}
	if vd.More[0].Type != "i64" {
		t.Errorf("后续声明符类型 = %q，期望 i64", vd.More[0].Type)
	}
	if vd.BaseType != "i64" {
		t.Errorf("BaseType = %q，期望 i64", vd.BaseType)
	}
	if got := len(vd.AllDeclarators()); got != 2 {
		t.Errorf("AllDeclarators 数量 = %d，期望 2", got)
	}
	// 初值表达式
	if lit, ok := vd.Value.(*ast.IntegerLiteral); !ok || lit.Value != 1 {
		t.Errorf("a 的初值 = %v，期望 1", vd.Value)
	}
	if lit, ok := vd.More[0].Value.(*ast.IntegerLiteral); !ok || lit.Value != 2 {
		t.Errorf("b 的初值 = %v，期望 2", vd.More[0].Value)
	}
}

// TestMultiVarDeclNoInitializer 验证 `i64 c, d`：后续声明符可以没有初值
func TestMultiVarDeclNoInitializer(t *testing.T) {
	src := "fn main() {\n    i64 c, d\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if vd.Name != "c" || vd.Value != nil {
		t.Errorf("主声明 = %s %v，期望 c 无初值", vd.Name, vd.Value)
	}
	if len(vd.More) != 1 || vd.More[0].Name != "d" {
		t.Fatalf("More = %+v，期望单个 d", vd.More)
	}
	if vd.More[0].Value != nil {
		t.Errorf("d 的初值 = %v，期望 nil", vd.More[0].Value)
	}
}

// TestMultiVarDeclThreeAndMixed 验证三个以上声明符、混合有无初值
func TestMultiVarDeclThreeAndMixed(t *testing.T) {
	src := "fn main() {\n    i64 a = 1, b = 2, c\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if len(vd.More) != 2 {
		t.Fatalf("More 长度 = %d，期望 2", len(vd.More))
	}
	if vd.More[1].Name != "c" || vd.More[1].Value != nil {
		t.Errorf("第 3 个声明符 = %+v，期望 c 无初值", vd.More[1])
	}
}

// TestMultiVarDeclKeepsFollowingStatement 验证逗号列表不会吞掉后续语句
func TestMultiVarDeclKeepsFollowingStatement(t *testing.T) {
	src := "fn foo() {}\n" +
		"fn main() {\n" +
		"    i64 a = 1, b = 2\n" +
		"    foo()\n" +
		"}\n"
	prog := parseMultiDecl(t, src)

	mainFn := findFunction(t, prog, "main")
	if len(mainFn.Body) != 2 {
		t.Fatalf("main 语句数 = %d，期望 2", len(mainFn.Body))
	}
	if _, ok := mainFn.Body[1].(*ast.ExpressionStatement); !ok {
		t.Errorf("第 2 条语句是 %T，期望 ExpressionStatement", mainFn.Body[1])
	}
}

// TestMultiVarDeclTypeSuffix 验证首个声明符带类型后缀时共享类型与后缀分离：
// `i64 a[4], b` → 主声明 [4]i64、共享 i64、b 为 i64
func TestMultiVarDeclTypeSuffix(t *testing.T) {
	src := "fn main() {\n    i64 a[4], b\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if vd.Type != "[4]i64" {
		t.Errorf("a 的类型 = %q，期望 [4]i64", vd.Type)
	}
	if vd.BaseType != "i64" {
		t.Errorf("BaseType = %q，期望 i64", vd.BaseType)
	}
	if len(vd.More) != 1 || vd.More[0].Type != "i64" {
		t.Errorf("b 的类型 = %+v，期望单个 i64", vd.More)
	}
}

// TestMultiVarDeclPointerSuffix 验证指针后缀：`i64 p*, q`
func TestMultiVarDeclPointerSuffix(t *testing.T) {
	src := "fn main() {\n    i64 p*, q\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if vd.Type != "i64*" {
		t.Errorf("p 的类型 = %q，期望 i64*", vd.Type)
	}
	if vd.BaseType != "i64" {
		t.Errorf("BaseType = %q，期望 i64", vd.BaseType)
	}
	if len(vd.More) != 1 || vd.More[0].Type != "i64" {
		t.Errorf("q 的类型 = %+v，期望单个 i64", vd.More)
	}
}

// TestMultiVarDeclAuto 验证 `auto a = 1, b = 2`（各声明符独立推导类型）
func TestMultiVarDeclAuto(t *testing.T) {
	src := "fn main() {\n    auto a = 1, b = 2\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if !vd.IsAuto {
		t.Error("主声明应为 IsAuto")
	}
	if len(vd.More) != 1 || !vd.More[0].IsAuto {
		t.Fatalf("后续声明符 = %+v，期望 auto", vd.More)
	}
	if vd.More[0].Name != "b" {
		t.Errorf("后续声明符名 = %q，期望 b", vd.More[0].Name)
	}
}

// TestMultiVarDeclGlobal 验证顶层（全局）多变量声明
func TestMultiVarDeclGlobal(t *testing.T) {
	src := "i64 g1 = 1, g2 = 2\n" +
		"fn main() {}\n"
	prog := parseMultiDecl(t, src)

	vd, ok := prog.Statements[0].(*ast.VariableDeclaration)
	if !ok {
		t.Fatalf("顶层首语句是 %T，期望 VariableDeclaration", prog.Statements[0])
	}
	if len(vd.More) != 1 || vd.More[0].Name != "g2" {
		t.Errorf("More = %+v，期望单个 g2", vd.More)
	}
}

// TestMultiVarDeclConst 验证 const 多变量声明 `const i64 a = 1, b = 2`
func TestMultiVarDeclConst(t *testing.T) {
	src := "fn main() {\n    const i64 a = 1, b = 2\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if !vd.IsConst {
		t.Error("主声明应为 IsConst")
	}
	if len(vd.More) != 1 || !vd.More[0].IsConst {
		t.Fatalf("后续声明符 = %+v，期望 const", vd.More)
	}
}

// TestMultiVarDeclUntypedConst 验证无类型常量的多变量声明 `const a = 1, b = 2`
func TestMultiVarDeclUntypedConst(t *testing.T) {
	src := "fn main() {\n    const a = 1, b = 2\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if !vd.IsConst || vd.Type != "" {
		t.Errorf("主声明 = const=%v type=%q，期望无类型 const", vd.IsConst, vd.Type)
	}
	if len(vd.More) != 1 || vd.More[0].Name != "b" || !vd.More[0].IsConst {
		t.Errorf("后续声明符 = %+v，期望 const b", vd.More)
	}
}

// TestMultiVarDeclPubPropagates 验证语句级修饰符传播到每个声明符：
// `pub i64 a = 1, b = 2` 的 b 同样是 pub
func TestMultiVarDeclPubPropagates(t *testing.T) {
	src := "pub i64 a = 1, b = 2\n" +
		"fn main() {}\n"
	prog := parseMultiDecl(t, src)

	vd, ok := prog.Statements[0].(*ast.VariableDeclaration)
	if !ok {
		t.Fatalf("顶层首语句是 %T，期望 VariableDeclaration", prog.Statements[0])
	}
	if !vd.IsPublic {
		t.Error("主声明应为 IsPublic")
	}
	if len(vd.More) != 1 || !vd.More[0].IsPublic {
		t.Errorf("后续声明符 = %+v，期望 IsPublic", vd.More)
	}
}

// TestMultiVarDeclStaticPropagates 验证 static 修饰符传播到每个声明符
func TestMultiVarDeclStaticPropagates(t *testing.T) {
	src := "fn main() {\n    static i64 a = 1, b = 2\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if !vd.IsStatic {
		t.Error("主声明应为 IsStatic")
	}
	if len(vd.More) != 1 || !vd.More[0].IsStatic {
		t.Errorf("后续声明符 = %+v，期望 IsStatic", vd.More)
	}
}

// TestSingleVarDeclUnchanged 验证单变量声明路径行为不变（More/BaseType 均为空）
func TestSingleVarDeclUnchanged(t *testing.T) {
	src := "fn main() {\n    i64 a = 1\n    i64 buf[64]\n}\n"
	prog := parseMultiDecl(t, src)

	vd := declInMain(t, prog, 0)
	if len(vd.More) != 0 {
		t.Errorf("More 长度 = %d，期望 0", len(vd.More))
	}
	if vd.BaseType != "" {
		t.Errorf("BaseType = %q，期望空", vd.BaseType)
	}
	buf := declInMain(t, prog, 1)
	if buf.Type != "[64]i64" || buf.Name != "buf" {
		t.Errorf("buf = %s %s，期望 [64]i64 buf", buf.Type, buf.Name)
	}
}
