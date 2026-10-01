package parser_test

import (
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

/*
 * parseConc 解析源码并在有语法错误时直接失败。
 *
 * 两个坑：
 *  1. parser 的后置校验要求程序里有 main，"找不到 main 函数"是校验错误
 *  2. 但用例自己带 main 时不能再补一个，会撞"函数名称重复：main"
 * 所以只在源码里没有 main 时才补。
 */
func parseConc(t *testing.T, src string) *ast.Program {
	t.Helper()
	if !strings.Contains(src, "fn main(") {
		src += "\n\nfn main() {}\n"
	}
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

// parseConcErr 解析源码并返回错误数（不追加 main，用于负例）
func parseConcErr(t *testing.T, src string) int {
	t.Helper()
	lx := lexer.NewLexer(src)
	p := parser.NewParser(lx)
	p.Parse()
	return p.GetErrorCollector().ErrorCount()
}

/*
 * parseAwaitConc 与 parseConc 的区别：
 * await 是后缀一元表达式（`h.await`），解析它要经过成员访问的路径，
 * 因此某些上下文里 primary 解析拿不到后续的 `.await`。
 *
 * 先看 lexer 是否把 `h.await` 切成 IDENT + DOT + AWAIT；
 * 若 AWAIT 紧跟在标识符后，解析器应把它收成后缀表达式。
 * 这个用例锁定该行为，防止将来 lexer 改动把 await 吃掉。
 */
func awaitLexesAsPostfix(t *testing.T, src string) {
	t.Helper()
	lx := lexer.NewLexer(src)

	// 序列应为 IDENT DOT AWAIT，所以要看 AWAIT 前第二个 token
	prev2, prev := lexer.TokenType(0), lexer.TokenType(0)
	sawAwait := false
	for {
		tk := lx.Next()
		if tk.Type == lexer.TOKEN_EOF {
			break
		}
		if tk.Type == lexer.TOKEN_AWAIT {
			if prev == lexer.TOKEN_DOT && prev2 == lexer.TOKEN_IDENT {
				sawAwait = true
			}
			break
		}
		prev2, prev = prev, tk.Type
	}
	if !sawAwait {
		t.Errorf("`%s` 中的 await 未被识别为标识符后的后缀 token", src)
	}
}

func mainBody(prog *ast.Program) []ast.Statement {
	for _, st := range prog.Statements {
		if fn, ok := st.(*ast.FunctionStatement); ok && fn.Name == "main" {
			return fn.Body
		}
	}
	return nil
}

// findConcFn 找第一个 conc fn
func findConcFn(t *testing.T, prog *ast.Program) *ast.FunctionStatement {
	t.Helper()
	for _, st := range prog.Statements {
		if fn, ok := st.(*ast.FunctionStatement); ok && fn.IsConcurrent {
			return fn
		}
	}
	t.Fatal("未找到 conc fn")
	return nil
}

// ---------- conc fn 声明 ----------

func TestParseConcFn(t *testing.T) {
	prog := parseConc(t, `
conc fn fetch(url: string) -> string {
    return url
}
`)
	fn := findConcFn(t, prog)
	if fn.Name != "fetch" {
		t.Errorf("函数名 = %s, want fetch", fn.Name)
	}
	if len(fn.Params) != 1 || fn.Params[0] != "url" {
		t.Errorf("参数 = %v, want [url]", fn.Params)
	}
}

func TestParseConcFnNoParams(t *testing.T) {
	prog := parseConc(t, `
conc fn noop() -> i64 {
    return 0
}
`)
	fn := findConcFn(t, prog)
	if len(fn.Params) != 0 {
		t.Errorf("应无参数，实际 %v", fn.Params)
	}
}

// 普通 fn 不应被标记为 conc
func TestParsePlainFnNotConcurrent(t *testing.T) {
	prog := parseConc(t, `
fn normal(x: i64) -> i64 {
    return x
}
`)
	for _, st := range prog.Statements {
		if fn, ok := st.(*ast.FunctionStatement); ok && fn.Name == "normal" {
			if fn.IsConcurrent {
				t.Error("普通 fn 不应带 IsConcurrent 标记")
			}
			return
		}
	}
	t.Fatal("未找到 normal 函数")
}

// conc 后必须跟 fn
func TestConcWithoutFnIsError(t *testing.T) {
	if n := parseConcErr(t, `
conc 123
`); n == 0 {
		t.Error("conc 后不跟 fn 应报错")
	}
}

// ---------- spawn ----------

func TestParseSpawnExpression(t *testing.T) {
	prog := parseConc(t, `
conc fn work(x: i64) -> i64 { return x }

fn main() {
    let h = spawn work(42)
}
`)
	body := mainBody(prog)
	if len(body) == 0 {
		t.Fatal("main 体为空")
	}

	decl, ok := body[0].(*ast.VariableDeclaration)
	if !ok {
		t.Fatalf("首条应为变量声明，实际 %T", body[0])
	}
	spawn, ok := decl.Value.(*ast.SpawnExpression)
	if !ok {
		t.Fatalf("初始化表达式应为 SpawnExpression，实际 %T", decl.Value)
	}
	if len(spawn.Args) != 1 {
		t.Fatalf("实参数 = %d, want 1", len(spawn.Args))
	}
	if _, ok := spawn.Args[0].(*ast.IntegerLiteral); !ok {
		t.Errorf("实参应为整型字面量，实际 %T", spawn.Args[0])
	}
}

func TestParseSpawnNoArgs(t *testing.T) {
	prog := parseConc(t, `
conc fn noop() -> i64 { return 0 }

fn main() {
    spawn noop()
}
`)
	if len(mainBody(prog)) != 1 {
		t.Error("无实参 spawn 应能解析")
	}
}

func TestParseSpawnMultipleArgs(t *testing.T) {
	prog := parseConc(t, `
conc fn add(a: i64, b: i64) -> i64 { return a + b }

fn main() {
    let h = spawn add(1, 2)
}
`)
	decl := mainBody(prog)[0].(*ast.VariableDeclaration)
	spawn := decl.Value.(*ast.SpawnExpression)
	if len(spawn.Args) != 2 {
		t.Errorf("实参数 = %d, want 2", len(spawn.Args))
	}
}

// ---------- await ----------

func TestParseAwaitExpression(t *testing.T) {
	prog := parseConc(t, `
conc fn work(x: i64) -> i64 { return x }

fn main() {
    let h = spawn work(1)
    let r = h.await
}
`)
	body := mainBody(prog)
	if len(body) != 2 {
		t.Fatalf("main 应有 2 条语句，实际 %d", len(body))
	}
	decl := body[1].(*ast.VariableDeclaration)
	await, ok := decl.Value.(*ast.AwaitExpression)
	if !ok {
		t.Fatalf("应为 AwaitExpression，实际 %T", decl.Value)
	}
	ident, ok := await.Handle.(*ast.Identifier)
	if !ok || ident.Name != "h" {
		t.Errorf("await 的目标应为标识符 h，实际 %#v", await.Handle)
	}
}

func TestAwaitWithoutHandleIsError(t *testing.T) {
	if n := parseConcErr(t, `
fn main() {
    let r = await
}
`); n == 0 {
		t.Error("await 后无 Handle 应报错")
	}
}

// ---------- conc / spawn / await 是关键字 ----------

func TestConcSpawnAwaitAreKeywords(t *testing.T) {
	lx := lexer.NewLexer("conc spawn await")

	found := map[string]bool{}
	for {
		tk := lx.Next()
		if tk.Type == lexer.TOKEN_EOF {
			break
		}
		switch tk.Type {
		case lexer.TOKEN_CONC:
			found["conc"] = true
		case lexer.TOKEN_SPAWN:
			found["spawn"] = true
		case lexer.TOKEN_AWAIT:
			found["await"] = true
		}
	}
	for _, want := range []string{"conc", "spawn", "await"} {
		if !found[want] {
			t.Errorf("%s 应被识别为关键字", want)
		}
	}
}

/*
 * await 是后缀形式（`h.await`），lexer 必须把它切成
 * IDENTIFIER DOT AWAIT 而不是把 await 当普通标识符。
 * 若这一点变了，parser 里的后缀分支就再也匹配不上。
 */
func TestAwaitIsPostfixAfterIdentifier(t *testing.T) {
	awaitLexesAsPostfix(t, "h.await")
}

// ---------- 混编：conc fn 内部可以 spawn ----------

func TestSpawnInsideConcFn(t *testing.T) {
	prog := parseConc(t, `
conc fn inner(x: i64) -> i64 { return x }

conc fn outer(x: i64) -> i64 {
    let h = spawn inner(x)
    return h.await
}
`)
	outer := findConcFn(t, prog)
	if outer.Name != "outer" {
		// 第一个 conc fn 可能是 inner，顺序不定
		for _, st := range prog.Statements {
			if fn, ok := st.(*ast.FunctionStatement); ok && fn.Name == "outer" {
				outer = fn
				break
			}
		}
	}
	if len(outer.Body) != 2 {
		t.Fatalf("outer 体应有 2 条语句，实际 %d", len(outer.Body))
	}
	if _, ok := outer.Body[0].(*ast.VariableDeclaration); !ok {
		t.Error("首条应为变量声明")
	}
	ret, ok := outer.Body[1].(*ast.ReturnStatement)
	if !ok {
		t.Fatalf("次条应为 return，实际 %T", outer.Body[1])
	}
	if _, ok := ret.Value.(*ast.AwaitExpression); !ok {
		t.Errorf("return 的值应为 AwaitExpression，实际 %T", ret.Value)
	}
}