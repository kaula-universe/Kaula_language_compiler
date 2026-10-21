// 回归：结构宏的四个结构性保证（AST 参数绑定 / 自动卫生 / tree 约束 / 表达式返回）。
//
// 这些用例直接断言"展开后的 AST 与代码"，而不是只跑编译——宏的坑几乎都在
// 展开细节上（变量捕获、参数被提前固化成文本、约束不生效），到了 C 层才发现
// 就只剩一条难懂的行号。每个用例的前缀定义都对应 docs/tutorials/prefix-system.md
// 里的一节。
package codegen

import (
	"strings"
	"testing"
	"time"

	"kaula/internal/ast"
	"kaula/internal/config"
	"kaula/internal/lexer"
	"kaula/internal/parser"
	"kaula/internal/sema"
)

// compileMacro 解析 + 语义 + 生成，返回生成器与错误列表
func compileMacro(t *testing.T, src string) (*CodeGenerator, []string) {
	t.Helper()
	p := parser.NewParser(lexer.NewLexer(src))
	prog := p.Parse()
	if p.HasErrors() {
		t.Fatalf("parse errors: %v", p.GetErrorCollector().Errors())
	}
	sa := sema.NewSemanticAnalyzer()
	sa.Analyze(prog)
	cg := NewCodeGenerator(config.DefaultConfig())
	cg.Generate(prog)
	return cg, cg.Errors()
}

// macroCallIn 取 main 里第 n 条语句（0 起）作为前缀调用
func macroCallIn(t *testing.T, src string, idx int) (*ast.PrefixCallExpression, *CodeGenerator) {
	t.Helper()
	p := parser.NewParser(lexer.NewLexer(src))
	prog := p.Parse()
	if p.HasErrors() {
		t.Fatalf("parse errors: %v", p.GetErrorCollector().Errors())
	}
	cg := NewCodeGenerator(config.DefaultConfig())
	cg.Generate(prog)
	fn := prog.FindFunction("main")
	if fn == nil || idx >= len(fn.Body) {
		t.Fatalf("main 的第 %d 条语句不存在", idx)
	}
	es, ok := fn.Body[idx].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("main 的第 %d 条语句不是表达式语句: %T", idx, fn.Body[idx])
	}
	call, ok := es.Expression.(*ast.PrefixCallExpression)
	if !ok {
		t.Fatalf("main 的第 %d 条语句不是前缀调用: %T", idx, es.Expression)
	}
	return call, cg
}

// expand 直接调用结构宏展开器，返回展开结果
func expand(t *testing.T, call *ast.PrefixCallExpression, cg *CodeGenerator) *macroExpansion {
	t.Helper()
	sg := cg.statementGenerator
	exp, ok := sg.expandStructureMacro(call)
	if !ok {
		t.Fatal("expandStructureMacro 返回 false：前缀未被识别为模板")
	}
	return exp
}

// collectLocalDeclNames 收集展开结果里所有 VariableDeclaration 的名字（含嵌套块）
func collectLocalDeclNames(stmts []ast.Statement) map[string]bool {
	out := map[string]bool{}
	var walk func([]ast.Statement)
	walk = func(list []ast.Statement) {
		for _, s := range list {
			switch n := s.(type) {
			case *ast.VariableDeclaration:
				for _, d := range n.AllDeclarators() {
					if d != nil {
						out[d.Name] = true
					}
				}
			case *ast.BlockStatement:
				walk(n.Statements)
			case *ast.IfStatement:
				walk(n.Body)
				walk(n.Else)
			case *ast.WhileStatement:
				walk(n.Body)
			case *ast.ForStatement:
				walk(n.Body)
			case *ast.ForInStatement:
				walk(n.Body)
			}
		}
	}
	walk(stmts)
	return out
}

// TestMacroAlphaRenameAvoidsCapture：宏体内的局部变量展开后唯一化，
// 不再踩调用方同名变量（旧实现直接生成 `int64_t tmp = ...`，覆盖调用方 tmp）。
func TestMacroAlphaRenameAvoidsCapture(t *testing.T) {
	src := `
prefix Swap {
    $a
    $b
    i64 tmp = $a
    $a = $b
    $b = tmp
}

fn main() {
    i64 x = 1
    i64 y = 2
    @Swap(a = x, b = y)
}`
	call, cg := macroCallIn(t, src, 2)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	decls := collectLocalDeclNames(exp.Stmts)
	if decls["tmp"] {
		t.Fatalf("宏本地变量 tmp 未被重命名，会捕获调用方变量: %v", decls)
	}
	renamed := ""
	for name := range decls {
		if strings.HasPrefix(name, "__kaula_m") {
			renamed = name
		}
	}
	if renamed == "" {
		t.Fatalf("没有找到重命名后的宏本地变量: %v", decls)
	}
	if exp.Renames["tmp"] != renamed {
		t.Fatalf("Renames[tmp] = %q, 期望 %q", exp.Renames["tmp"], renamed)
	}
	// 模板 AST 必须保持原样（展开不能改共享只读的模板）
	if strings.Contains(renamed, " ") || renamed == "tmp" {
		t.Fatalf("非法重命名: %q", renamed)
	}
}

// TestMacroExpansionDoesNotMutateTemplate：两次展开（不同调用点）得到不同的
// 唯一名，且模板里的名字从未被改写。
func TestMacroExpansionDoesNotMutateTemplate(t *testing.T) {
	src := `
prefix Two {
    $a
    i64 tmp = $a
    println(tmp)
}

fn main() {
    i64 k = 1
    @Two(a = k)
    @Two(a = k)
}`
	call1, cg := macroCallIn(t, src, 1)
	call2, _ := macroCallIn(t, src, 2)
	exp1 := expand(t, call1, cg)
	exp2 := expand(t, call2, cg)
	name1 := exp1.Renames["tmp"]
	name2 := exp2.Renames["tmp"]
	if name1 == "" || name2 == "" {
		t.Fatalf("两次展开都应有重命名: %q %q", name1, name2)
	}
	if name1 == name2 {
		t.Fatalf("不同展开点得到同一个唯一名 %q：嵌套展开时会互相覆盖", name1)
	}
	prefix := cg.findPrefixStatement("Two")
	if prefix == nil {
		t.Fatal("找不到模板 Two")
	}
	decl := prefix.Body[1].(*ast.VariableDeclaration)
	if decl.Name != "tmp" {
		t.Fatalf("模板 AST 被改写：tmp → %q", decl.Name)
	}
}

// TestMacroParamBindingIsAST：参数绑定载荷是实参 AST（不是预生成的 C 文本），
// 因此宏"看得到"结构——`$a += 1` 这类左值参数能正确生成，而不是变成
// `(x + 1) += 1` 这样的非法 C。
func TestMacroParamBindingIsAST(t *testing.T) {
	src := `
prefix Bump {
    $a
    $a += 1
}

fn main() {
    i64 x = 41
    @Bump(a = x)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("生成错误: %v", errs)
	}
	_ = cg
	call, cg2 := macroCallIn(t, src, 1)
	exp := expand(t, call, cg2)
	if _, ok := exp.Params["a"]; !ok {
		t.Fatalf("参数 a 没有绑定: %v", exp.Params)
	}
	if _, isIdent := exp.Params["a"].(*ast.Identifier); !isIdent {
		t.Fatalf("绑定载荷应是实参 AST（Identifier），实际 %T", exp.Params["a"])
	}
}

// TestMacroDefaultValueIsASTAndLazy：默认值以 AST 绑定，可以引用其他参数
// （`$b = $a`），且只有真正用到时才生成。
func TestMacroDefaultValueIsASTAndLazy(t *testing.T) {
	src := `
prefix Mirror {
    $a
    $b = $a
    println($b)
}

fn main() {
    i64 v = 7
    @Mirror(a = v)
}`
	call, cg := macroCallIn(t, src, 1)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	bound, ok := exp.Params["b"]
	if !ok {
		t.Fatal("默认值 b 没有绑定")
	}
	// `$b = $a`：默认值是"引用另一个参数"的 AST，展开时应解析到实参 v
	if id, isIdent := bound.(*ast.Identifier); !isIdent || !id.IsPrefixVar || id.Name != "a" {
		t.Fatalf("默认值应绑定为 `$a` 的 AST，实际 %#v", bound)
	}
}

// TestMacroStatementLevelParam：整条语句是一个 `$p` 时按参数展开——
// 表达式参数成为表达式语句，调用体内联（$body 语义）。
func TestMacroStatementLevelParam(t *testing.T) {
	src := `
prefix Emit {
    $expr
    println($expr)
}

prefix Wrap {
    $body
    println("before")
    $body
    println("after")
}

fn main() {
    i64 a = 1
    @Emit(expr = a + 1)
    @Wrap() {
        println("inside")
    }
}`
	call, cg := macroCallIn(t, src, 1)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("Emit 展开报错: %v", exp.Errors)
	}
	if len(exp.Stmts) != 1 {
		t.Fatalf("Emit 展开应只有 println 一条语句，实际 %d", len(exp.Stmts))
	}

	call2, cg2 := macroCallIn(t, src, 2)
	exp2 := expand(t, call2, cg2)
	if len(exp2.Errors) != 0 {
		t.Fatalf("Wrap 展开报错: %v", exp2.Errors)
	}
	// before / inside / after 共 3 条
	if len(exp2.Stmts) != 3 {
		t.Fatalf("Wrap 展开应有 3 条语句，实际 %d", len(exp2.Stmts))
	}
}

// TestMacroExpressionReturn：`@Name(...)` 出现在表达式位置时，模板体最后一个
// 表达式语句就是宏的值（P1 表达式级宏返回）。
func TestMacroExpressionReturn(t *testing.T) {
	src := `
#[prefix]
fn Double(x: i64) {
    i64 tmp = $x * 2
    tmp + 0
}

fn main() {
    i64 v = @Double(x = 21)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("表达式级宏生成失败: %v", errs)
	}
	p := parser.NewParser(lexer.NewLexer(src))
	prog := p.Parse()
	decl := prog.FindFunction("main").Body[0].(*ast.VariableDeclaration)
	call, ok := decl.Value.(*ast.PrefixCallExpression)
	if !ok {
		t.Fatalf("main 里应有表达式位置的前缀调用，实际 %T", decl.Value)
	}
	code := cg.expressionGenerator.GenerateExpression(call)
	if !strings.Contains(code, "({") || !strings.Contains(code, "})") {
		t.Fatalf("多语句宏应以语句表达式成组，实际: %s", code)
	}
	if !strings.Contains(code, "__kaula_m") {
		t.Fatalf("宏本地变量应被唯一化，实际: %s", code)
	}
}

// TestMacroExpressionReturnRejectsNonExpression：模板体末尾不是表达式时，
// 在表达式位置使用必须报错（而不是静默生成非法 C / 0）。
func TestMacroExpressionReturnRejectsNonExpression(t *testing.T) {
	src := `
#[prefix]
fn NoValue(x: i64) {
    println($x)
}

fn main() {
    i64 v = @NoValue(x = 1)
}`
	_, errs := compileMacro(t, src)
	if len(errs) == 0 {
		t.Fatal("模板末尾不是表达式时，表达式位置使用应报错")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "没给出返回值") {
			found = true
		}
	}
	if !found {
		t.Fatalf("错误信息应指出没有返回值，实际: %v", errs)
	}
}

// TestMacroTreeConstraintAcceptsAndRejects：tree 结构约束在展开前校验实参结构。
func TestMacroTreeConstraintAcceptsAndRejects(t *testing.T) {
	okSrc := `
#[prefix]
fn WantInt(tree(i64), x: i64) {
    println($x)
}

fn main() {
    @WantInt(x = 42)
}`
	if _, errs := compileMacro(t, okSrc); len(errs) > 0 {
		t.Fatalf("结构满足约束时不应报错: %v", errs)
	}

	badSrc := `
#[prefix]
fn WantInt(tree(i64), x: i64) {
    println($x)
}

fn main() {
    @WantInt(x = "not an int")
}`
	_, errs := compileMacro(t, badSrc)
	if len(errs) == 0 {
		t.Fatal("结构不满足约束时应报错")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "结构不匹配") && !strings.Contains(joined, "类型不匹配") {
		t.Fatalf("错误信息应说明结构/类型不匹配，实际: %v", errs)
	}
}

// TestMacroTreeConstraintExpression：`tree(expression)` 要求实参是表达式族节点。
func TestMacroTreeConstraintExpression(t *testing.T) {
	src := `
#[prefix]
fn Pick(tree(expression), a: i64, b: i64) {
    println($a)
}

fn main() {
    @Pick(a = 1 + 2, b = 3)
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("表达式实参应满足 tree(expression): %v", exp.Errors)
	}
	if exp.Sig.IsEmpty() {
		t.Fatal("tree 注解没有解析成结构签名")
	}
	if c := exp.Sig.ConstraintFor("a"); c == nil {
		t.Fatalf("参数 a 缺少约束: %v", exp.Sig)
	}
}

// TestMacroASTNodeTypeMapping：AST → 结构类型的映射覆盖主要节点类别
func TestMacroASTNodeTypeMapping(t *testing.T) {
	cases := []struct {
		node    ast.Node
		pattern string
	}{
		{&ast.IntegerLiteral{Value: 1}, "literal"},
		{&ast.Identifier{Name: "x"}, "identifier"},
		{&ast.CallExpression{Function: &ast.Identifier{Name: "f"}}, "call"},
		{&ast.ReturnStatement{}, "return"},
		{&ast.BlockStatement{}, "block"},
		{&ast.IfStatement{}, "if"},
		{&ast.WhileStatement{}, "while"},
	}
	for _, c := range cases {
		_, pattern := ASTNodeType(c.node)
		if pattern != c.pattern {
			t.Fatalf("%T → %q, 期望 %q", c.node, pattern, c.pattern)
		}
	}
}

// TestMacroCrossLayerFreeReference：内层模板直接把外层参数写成裸名 `fwd`，
// 展开时必须解析到外层实参，而不是生成外层函数体里的未知标识符。
func TestMacroCrossLayerFreeReference(t *testing.T) {
	src := `
#[prefix]
fn Inner() {
    println(fwd)
}

#[prefix]
fn Outer(v: i64) {
    i64 fwd = $v + 1
    @Inner()
}

fn main() {
    @Outer(v = 1)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("跨层传递应能展开: %v", errs)
	}
	call, _ := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("Outer 展开报错: %v", exp.Errors)
	}
	code := cg.statementGenerator.generateBlockStatement(&ast.BlockStatement{Statements: exp.Stmts})
	if !strings.Contains(code, "__kaula_m") {
		t.Fatalf("内层应引用外层重命名后的 fwd，实际: %s", code)
	}
}

// TestMacroAssignIsNotMistakenForDefault：`$p = expr` 的两种含义必须分开。
//
//	$p = expr 既可能是「带默认值的参数声明」（不生成代码），
//	          也可能是「普通赋值」（`$target = $target + 1`，必须生成）。
//
// 判据是调用点有没有为 p 传实参：传了就不是声明。这条用例曾经红过——
// 骨架阶段把整个 `$target = $target + $value` 当成参数声明丢掉，宏静默不生效。
func TestMacroAssignIsNotMistakenForDefault(t *testing.T) {
	src := `
#[prefix]
fn AddTo(target: i64, value: i64) {
    $target = $target + $value
}

#[prefix]
fn WithDefault(x: i64) {
    $limit = 10
    println($x + $limit)
}

fn main() {
    i64 acc = 100
    @AddTo(target = acc, value = 42)
    @WithDefault(x = acc)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("展开报错: %v", errs)
	}

	// 1) 赋值形态：必须留下一条语句，且被改写成对实参 acc 的赋值
	call, _ := macroCallIn(t, src, 1)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("AddTo 展开报错: %v", exp.Errors)
	}
	if len(exp.Stmts) != 1 {
		t.Fatalf("`$target = $target + $value` 应展开为 1 条赋值语句，实际 %d 条", len(exp.Stmts))
	}
	code := cg.statementGenerator.generateBlockStatement(&ast.BlockStatement{Statements: exp.Stmts})
	if !strings.Contains(code, "acc") || !strings.Contains(code, "42") {
		t.Fatalf("赋值应作用于实参 acc 与 42，实际: %s", code)
	}

	// 2) 默认值形态：`$limit = 10` 不生成代码，$limit 以默认值内联
	call2, _ := macroCallIn(t, src, 2)
	exp2 := expand(t, call2, cg)
	if len(exp2.Errors) != 0 {
		t.Fatalf("WithDefault 展开报错: %v", exp2.Errors)
	}
	if len(exp2.Stmts) != 1 {
		t.Fatalf("`$limit = 10` 是参数声明，应只留下 println 一条语句，实际 %d 条", len(exp2.Stmts))
	}
	code2 := cg.statementGenerator.generateBlockStatement(&ast.BlockStatement{Statements: exp2.Stmts})
	if !strings.Contains(code2, "10") {
		t.Fatalf("默认值 10 应内联进表达式，实际: %s", code2)
	}
}

// TestPrefixBodyEmitsNoUndeclaredIdent：前缀函数体不能漏出未声明的标识符。
//
// 前缀函数除了在调用点展开，还会被原样生成为一个同名 C 函数（兼容"前缀也能
// 当普通函数调用"）。那个函数体里没有 `$limit` 这种东西，早期实现会把
// `$limit = 10` 直译成 `limit = 10;`，于是整个程序在 clang 阶段因
// "use of undeclared identifier" 失败——报错位置与用户写的前缀毫无关系。
func TestPrefixBodyEmitsNoUndeclaredIdent(t *testing.T) {
	src := `
#[prefix]
fn WithDefault(x: i64) {
    $limit = 10
    println($x + $limit)
}

#[prefix]
fn Plain(x: i64) {
    println($x)
    $body
}

fn main() {
    @WithDefault(x = 1)
    @Plain(x = 2)
}`
	_, errs := compileMacro(t, src)
	if len(errs) > 0 {
		t.Fatalf("编译报错: %v", errs)
	}
}

// TestMacroSelfRecursionIsBounded：自我调用的前缀必须报错，而不是让编译器
// 栈溢出或挂死。
//
// 两道闸门：语义阶段 analyzePrefixStatement 的重入保护（否则 sema 先栈溢出），
// 以及生成阶段的展开层数上限（展开一层的产物里仍含宏调用，会一路递归下去）。
func TestMacroSelfRecursionIsBounded(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 模板体只有一条 @Self：sema 的补分析路径最容易在这里无限递归
			name: "只为参数的自引用",
			src: `
prefix Loop {
    $v
    @Loop(v = $v)
}
fn main() {
    i64 x = 1
    @Loop(v = x)
}`,
		},
		{
			// 带宽高语句：展开结果的生成阶段会再进展开，形成另一条递归链
			name: "带实语句的自引用",
			src: `
prefix Loop2 {
    $v
    println($v)
    @Loop2(v = $v)
}
fn main() {
    i64 x = 1
    @Loop2(v = x)
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done := make(chan []string, 1)
			go func() {
				_, errs := compileMacro(t, c.src)
				done <- errs
			}()
			select {
			case errs := <-done:
				if len(errs) == 0 {
					t.Fatal("自引用前缀应报错")
				}
				joined := strings.Join(errs, "\n")
				if !strings.Contains(joined, "展开层数超过上限") {
					t.Fatalf("应报展开层数超限，实际: %v", errs)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("自引用前缀导致编译挂起（既没报错也没退出）")
			}
		})
	}
}

/* ── 「实参只求值一次」（let 绑定，Task #4）── */

// TestMacroLetBindingBindsRepeatedRValueParam：模板骨架里右值引用 ≥2 次的
// $参数，实参求值一次绑到 __auto_type 临时变量，骨架引用改指临时变量。
// 这是 Task #4 的核心语义：带副作用的实参（IO、计数器、写变量）不再因为
// 内联两次而执行两遍。
func TestMacroLetBindingBindsRepeatedRValueParam(t *testing.T) {
	src := `
i64 g_count = 0
fn bump() -> i64 {
    g_count = g_count + 1
    return g_count
}
prefix Twice {
    $x
    println("sum=", $x + $x, " count=", g_count)
}
fn main() {
    @Twice(x = bump())
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	temp, ok := exp.Lets["x"]
	if !ok {
		t.Fatalf("x 引用两次应触发绑定，实际 Lets=%v", exp.Lets)
	}
	if !strings.HasPrefix(temp, "__kaula_b") {
		t.Fatalf("绑定临时变量应使用 __kaula_b 前缀，实际 %q", temp)
	}
	// 第一条语句是绑定声明：__auto_type <temp> = <实参>
	if len(exp.Stmts) == 0 {
		t.Fatal("展开结果为空")
	}
	decl, ok := exp.Stmts[0].(*ast.VariableDeclaration)
	if !ok || !decl.IsAuto || decl.Name != temp {
		t.Fatalf("第一条语句应是绑定声明 %s，实际 %T %v", temp, exp.Stmts[0], exp.Stmts[0])
	}
	if decl.Type != "__auto_type" {
		t.Fatalf("绑定声明类型应为 __auto_type，实际 %q", decl.Type)
	}
	code := cg.statementGenerator.generateBlockStatement(&ast.BlockStatement{Statements: exp.Stmts})
	if strings.Count(code, "bump()") != 1 {
		t.Fatalf("实参 bump() 应只求值一次（绑定声明里），实际出现 %d 次:\n%s",
			strings.Count(code, "bump()"), code)
	}
	// 骨架引用应改指临时变量（不是原样内联实参）
	if !strings.Contains(code, temp) || !strings.Contains(code, temp+"), (int64_t)("+temp+")") {
		t.Fatalf("骨架引用应改指临时变量 %s:\n%s", temp, code)
	}
}

// TestMacroLetBindingKeepsStringKind：绑定临时变量以 __auto_type 登记，
// isStringTyped/isPointerTyped 均判 false（作用域出口不生成 free，规避
// String 浅拷贝双释放），其真实类型由 prefixLetKinds 提示表补上——
// `$s` 绑定后 `$s + "!"` 仍走字符串拼接，而不是被判成数字加法。
func TestMacroLetBindingKeepsStringKind(t *testing.T) {
	src := `
fn greet() -> string {
    return "hi"
}
prefix Loud {
    $s
    println($s + "!")
    println($s + "!!")
}
fn main() {
    @Loud(s = greet())
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	temp, ok := exp.Lets["s"]
	if !ok {
		t.Fatalf("s 引用两次应触发绑定，实际 Lets=%v", exp.Lets)
	}
	if kind := cg.prefixLetKinds[temp]; kind != "s" {
		t.Fatalf("绑定临时变量 %s 的类型提示应为 s，实际 %q（prefixLetKinds=%v）",
			temp, kind, cg.prefixLetKinds)
	}
	code := cg.statementGenerator.generateBlockStatement(&ast.BlockStatement{Statements: exp.Stmts})
	if !strings.Contains(code, "string_concat("+temp) {
		t.Fatalf("`$s + \"!\"` 应走字符串拼接（string_concat），实际:\n%s", code)
	}
	if strings.Contains(code, "survivor_free("+temp) {
		t.Fatalf("绑定临时变量不应在作用域出口被 free（规避浅拷贝双释放），实际:\n%s", code)
	}
}

// TestMacroLetBindingSkipsLValueParams：左值参数（`$target = $target + $value`）
// 绝不绑定——绑定会把赋值目标悄悄改到临时变量上，宏改写调用点变量的语义
// 完全错误。
func TestMacroLetBindingSkipsLValueParams(t *testing.T) {
	src := `
#[prefix]
fn AddTo(target: i64, value: i64) {
    $target = $target + $value
    println($target)
    println($target)
}
fn main() {
    i64 acc = 100
    @AddTo(target = acc, value = 42)
}`
	call, cg := macroCallIn(t, src, 1)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	if _, ok := exp.Lets["target"]; ok {
		t.Fatalf("左值参数 target 不应被绑定，实际 Lets=%v", exp.Lets)
	}
	if _, ok := exp.Lets["value"]; ok {
		t.Fatalf("value 不应被绑定（引用次数不足），实际 Lets=%v", exp.Lets)
	}
	code := cg.statementGenerator.generateBlockStatement(&ast.BlockStatement{Statements: exp.Stmts})
	// 赋值必须仍作用于实参 acc
	if !strings.Contains(code, "acc") {
		t.Fatalf("赋值应作用于实参 acc，实际:\n%s", code)
	}
	if strings.Contains(code, "__kaula_b") {
		t.Fatalf("左值参数不应产生绑定临时变量，实际:\n%s", code)
	}
}

// TestMacroLetBindingSkipsSingleRefAndNonBindable：单次引用、字面量实参、
// void 调用实参、宏调用实参一律不绑定（无副作用无需绑定；无值可绑）。
func TestMacroLetBindingSkipsSingleRefAndNonBindable(t *testing.T) {
	src := `
fn bump() -> i64 {
    return 1
}
prefix Mix {
    $a
    $b
    $c
    $d
    println($a, $b, $c)
    println($b)
    println($c)
}
fn main() {
    @Mix(a = bump(), b = 42, c = println("void"), d = bump())
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	if len(exp.Lets) != 0 {
		t.Fatalf("单次引用/字面量/void 调用实参均不应绑定，实际 Lets=%v", exp.Lets)
	}
}

// TestMacroLetBindingSameCallTwiceGetsDistinctTemps：同一前缀在同一程序里
// 展开两次，绑定临时变量互不撞名（与 alpha 重命名同一套保证）。
func TestMacroLetBindingSameCallTwiceGetsDistinctTemps(t *testing.T) {
	src := `
i64 g_count = 0
fn bump() -> i64 {
    g_count = g_count + 1
    return g_count
}
prefix Twice {
    $x
    println($x + $x)
}
fn main() {
    @Twice(x = bump())
    @Twice(x = bump())
}`
	call1, cg := macroCallIn(t, src, 0)
	call2, _ := macroCallIn(t, src, 1)
	exp1 := expand(t, call1, cg)
	exp2 := expand(t, call2, cg)
	temp1 := exp1.Lets["x"]
	temp2 := exp2.Lets["x"]
	if temp1 == "" || temp2 == "" {
		t.Fatalf("两次展开都应有绑定: %q %q", temp1, temp2)
	}
	if temp1 == temp2 {
		t.Fatalf("两次展开得到同一个绑定临时变量 %q：嵌套展开时会互相覆盖", temp1)
	}
}
