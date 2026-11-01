// 回归：结构宏多规则按结构分派（Task #1）。
//
// 一个前缀名可以定义多条 #[prefix] fn，每条用不同的 tree(...) 签名声明它
// 接受的实参结构；展开时按声明顺序选择第一条「所有约束都命中」的规则。
// 这验证了 tree(...) 从「断言」变成「分支」：同一个前缀对不同实参形态
// （标识符 / 字面量 / 调用 / 二元表达式）走不同的展开体。
package codegen

import (
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/config"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// TestMacroDispatchByIdentifierVsLiteral：identifier 与 literal 都映射到
// NodeTypeExpression，只有靠 Pattern 才能区分——这是多规则分派的基石。
func TestMacroDispatchByIdentifierVsLiteral(t *testing.T) {
	src := `
#[prefix]
fn Show(tree(identifier), x) {
    println("identifier: ", $x)
}

#[prefix]
fn Show(tree(literal), x) {
    println("literal: ", $x)
}

fn main() {
    i64 a = 1
    @Show(x = a)
    @Show(x = 42)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("编译报错: %v", errs)
	}
	call1, _ := macroCallIn(t, src, 1)
	exp1 := expand(t, call1, cg)
	if len(exp1.Errors) != 0 {
		t.Fatalf("identifier 规则展开报错: %v", exp1.Errors)
	}
	if code := blockCode(cg, exp1.Stmts); !strings.Contains(code, "identifier:") {
		t.Fatalf("标识符实参应命中 identifier 规则，实际: %s", code)
	}

	call2, _ := macroCallIn(t, src, 2)
	exp2 := expand(t, call2, cg)
	if len(exp2.Errors) != 0 {
		t.Fatalf("literal 规则展开报错: %v", exp2.Errors)
	}
	if code := blockCode(cg, exp2.Stmts); !strings.Contains(code, "literal:") {
		t.Fatalf("字面量实参应命中 literal 规则，实际: %s", code)
	}
}

// TestMacroDispatchByCallVsBinary：调用与二元表达式分派，覆盖更多结构维度。
func TestMacroDispatchByCallVsBinary(t *testing.T) {
	src := `
#[prefix]
fn Shape(tree(call), x) {
    println("call")
}

#[prefix]
fn Shape(tree(binary), x) {
    println("binary")
}

fn main() {
    i64 a = 1
    @Shape(x = len("s"))
    @Shape(x = a + 1)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("编译报错: %v", errs)
	}
	call1, _ := macroCallIn(t, src, 1)
	exp1 := expand(t, call1, cg)
	if code := blockCode(cg, exp1.Stmts); !strings.Contains(code, "call") {
		t.Fatalf("调用实参应命中 call 规则，实际: %s", code)
	}

	call2, _ := macroCallIn(t, src, 2)
	exp2 := expand(t, call2, cg)
	if code := blockCode(cg, exp2.Stmts); !strings.Contains(code, "binary") {
		t.Fatalf("二元表达式实参应命中 binary 规则，实际: %s", code)
	}
}

// TestMacroDispatchFallback：无 tree 约束的规则作兜底，最后才命中。
func TestMacroDispatchFallback(t *testing.T) {
	src := `
#[prefix]
fn P(tree(identifier), x) {
    println("ident")
}

#[prefix]
fn P(x) {
    println("fallback")
}

fn main() {
    i64 a = 1
    @P(x = a)
    @P(x = 1 + 2)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("编译报错: %v", errs)
	}
	// a 是标识符 → 命中带约束的 identifier 规则
	call1, _ := macroCallIn(t, src, 1)
	exp1 := expand(t, call1, cg)
	if code := blockCode(cg, exp1.Stmts); !strings.Contains(code, "ident") {
		t.Fatalf("标识符应命中 identifier 规则而非兜底，实际: %s", code)
	}

	// 1 + 2 是二元表达式 → identifier 规则不命中，落到兜底规则
	call2, _ := macroCallIn(t, src, 2)
	exp2 := expand(t, call2, cg)
	if len(exp2.Errors) != 0 {
		t.Fatalf("兜底规则展开报错: %v", exp2.Errors)
	}
	if code := blockCode(cg, exp2.Stmts); !strings.Contains(code, "fallback") {
		t.Fatalf("二元表达式应落到兜底规则，实际: %s", code)
	}
}

// TestMacroDispatchNoRuleMatches：所有规则都带约束且都不命中时，报结构不匹配
// 错误，而不是静默选错规则。
func TestMacroDispatchNoRuleMatches(t *testing.T) {
	src := `
#[prefix]
fn OnlyIdent(tree(identifier), x) {
    println("ident")
}

fn main() {
    i64 a = 1
    @OnlyIdent(x = 1 + 2)
}`
	_, errs := compileMacro(t, src)
	if len(errs) == 0 {
		t.Fatal("二元表达式不满足 identifier 约束时应报错")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "结构不匹配") && !strings.Contains(joined, "类型不匹配") {
		t.Fatalf("错误信息应说明结构/类型不匹配，实际: %v", errs)
	}
}

// TestMacroDispatchCallIsNotBinary：调用与二元表达式必须严格区分。
//
// 这条用例守护一个真实缺陷：判断「Pattern 是结构子类还是值类型名」时查错了
// 表（用了大类表而非子类表），binary/unary/member 这些子类名被当成类型名，
// 走类型校验一律放行——于是 tree(binary) 接受任意实参，调用被误判成二元
// 表达式，多规则分派形同虚设。
func TestMacroDispatchCallIsNotBinary(t *testing.T) {
	src := `
fn twice(x: i64) -> i64 {
    return x * 2
}

#[prefix]
fn OnlyBinary(tree(binary), x) {
    println("binary")
}

fn main() {
    i64 a = 1
    @OnlyBinary(x = twice(a))
}`
	_, errs := compileMacro(t, src)
	if len(errs) == 0 {
		t.Fatal("调用实参不应满足 tree(binary) 约束")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "结构不匹配") && !strings.Contains(joined, "类型不匹配") {
		t.Fatalf("错误信息应说明结构/类型不匹配，实际: %v", errs)
	}
}

// TestMacroDispatchSkipsConflictCFunc：同名前缀的多条规则不生成兼容用的 C 函数。
//
// `#[prefix] fn` 历史上会额外生成同名 C 函数（兼容"前缀也能当普通函数调用"）。
// 多条同名规则签名不同，逐条生成会在 clang 阶段撞 `conflicting types for 'X'`，
// 而报错位置与用户写的前缀毫无关系。规则只在调用点展开，本就不该有 C 符号。
func TestMacroDispatchSkipsConflictCFunc(t *testing.T) {
	src := `
#[prefix]
fn Multi(tree(identifier), x) {
    println("ident")
}

#[prefix]
fn Multi(tree(literal), x) {
    println("literal")
}

fn main() {
    i64 a = 1
    @Multi(x = a)
    @Multi(x = 2)
}`
	p := parser.NewParser(lexer.NewLexer(src))
	prog := p.Parse()
	if p.HasErrors() {
		t.Fatalf("parse errors: %v", p.GetErrorCollector().Errors())
	}
	cg := NewCodeGenerator(config.DefaultConfig())
	code := cg.Generate(prog)
	if len(cg.Errors()) > 0 {
		t.Fatalf("生成报错: %v", cg.Errors())
	}
	// 多规则不应生成同名 C 函数定义/原型（否则 clang: conflicting types）
	if strings.Contains(code, "void Multi(") {
		t.Fatalf("多规则前缀不应生成同名 C 函数，实际生成的 C 含: %s", codeSnippet(code, "void Multi("))
	}
	// 但两条规则都必须真正展开（各出现一次）
	if n := strings.Count(code, `"ident"`); n != 1 {
		t.Fatalf("identifier 规则应展开 1 次，实际 %d 次", n)
	}
	if n := strings.Count(code, `"literal"`); n != 1 {
		t.Fatalf("literal 规则应展开 1 次，实际 %d 次", n)
	}
}

// codeSnippet 从 code 里以 marker 开头截取一小段，用于错误信息
func codeSnippet(code, marker string) string {
	i := strings.Index(code, marker)
	if i < 0 {
		return ""
	}
	end := i + 120
	if end > len(code) {
		end = len(code)
	}
	return code[i:end]
}

// blockCode 生成展开结果语句块的 C 代码（测试辅助）
func blockCode(cg *CodeGenerator, stmts []ast.Statement) string {
	return cg.statementGenerator.generateBlockStatement(&ast.BlockStatement{Statements: stmts})
}
