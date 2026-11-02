// 回归：结构宏变参 `$( ... ),*` 匹配与重复展开（Task #3）。
//
// 模板用 `$( $x ),*` 声明接受零个或多个**位置实参**；调用点 `@Name(1, 2, 3)`
// 的位置实参在展开块开头各求值一次，绑到 __auto_type 临时变量；模板里的
// 重复占位 `$( <语句> ),*` / `$( <表达式> ),*` 按实参个数重复展开。
package codegen

import (
	"strings"
	"testing"

	"kaula/internal/ast"
)

// nonTempStmts 过滤掉绑定临时变量的声明（prelude），只留真正的展开语句
func nonTempStmts(stmts []ast.Statement) []ast.Statement {
	var out []ast.Statement
	for _, s := range stmts {
		if vd, ok := s.(*ast.VariableDeclaration); ok && vd.IsAuto && strings.HasPrefix(vd.Name, "__kaula_b") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// TestVarArgExpressionRepeatInCall：`println($( $x ),*)` 按实参个数重复并逗号连接
func TestVarArgExpressionRepeatInCall(t *testing.T) {
	src := `
prefix Print {
    $( $x ),*
    println($( $x ),*)
}

fn main() {
    @Print(1, 2, 3)
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	body := nonTempStmts(exp.Stmts)
	// 展开结果应为一条 println 调用，实参列表 = 3 个（按位置实参个数重复）
	if len(body) != 1 {
		t.Fatalf("期望 1 条语句，实际 %d", len(body))
	}
	es, ok := body[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("第 0 条不是表达式语句: %T", body[0])
	}
	callExpr, ok := es.Expression.(*ast.CallExpression)
	if !ok {
		t.Fatalf("不是调用表达式: %T", es.Expression)
	}
	if len(callExpr.Args) != 3 {
		t.Fatalf("println 实参个数应为 3，实际 %d", len(callExpr.Args))
	}
}

// TestVarArgStatementRepeat：`$( println($x) ),*` 重复整条语句
func TestVarArgStatementRepeat(t *testing.T) {
	src := `
prefix Emit {
    $( $x ),*
    $( println($x) ),*
}

fn main() {
    @Emit(10, 20, 30)
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	body := nonTempStmts(exp.Stmts)
	if len(body) != 3 {
		t.Fatalf("期望 3 条重复语句，实际 %d", len(body))
	}
	if code := blockCode(cg, exp.Stmts); !strings.Contains(code, "10") ||
		!strings.Contains(code, "20") || !strings.Contains(code, "30") {
		t.Fatalf("展开代码应包含三个实参值，实际: %s", code)
	}
}

// TestVarArgZeroArgs：零个位置实参 → 重复占位展开为空
func TestVarArgZeroArgs(t *testing.T) {
	src := `
prefix Emit {
    $( $x ),*
    $( println($x) ),*
}

fn main() {
    @Emit()
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	if len(exp.Stmts) != 0 {
		t.Fatalf("零实参应展开为空，实际 %d 条语句", len(exp.Stmts))
	}
}

// TestVarArgPositionalWithoutMatcher：模板无匹配声明却收到位置实参 → 报错
func TestVarArgPositionalWithoutMatcher(t *testing.T) {
	src := `
prefix Plain {
    println("hi")
}

fn main() {
    @Plain(1, 2)
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	found := false
	for _, err := range exp.Errors {
		if strings.Contains(err, "不接受位置实参") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报「不接受位置实参」错误，实际: %v", exp.Errors)
	}
}

// TestVarArgMixedNamedPositional：命名参数与位置实参混用 → 报错
func TestVarArgMixedNamedPositional(t *testing.T) {
	src := `
prefix Emit {
    $( $x ),*
    $( println($x) ),*
}

fn main() {
    @Emit(1, tag = "x")
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	found := false
	for _, err := range exp.Errors {
		if strings.Contains(err, "混用") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报「混用」错误，实际: %v", exp.Errors)
	}
}

// TestVarArgEvaluatedOnce：带副作用实参只求值一次（绑临时变量，不内联多份）
func TestVarArgEvaluatedOnce(t *testing.T) {
	src := `
prefix Twice {
    $( $x ),*
    println($( $x + $x ),*)
}

fn main() {
    i64 calls = 0
    @Twice(counter(calls))
}`
	call, cg := macroCallIn(t, src, 1)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	// 临时变量声明应存在（__kaula_b{seq}_va0），且骨架里引用的是临时变量而非
	// 两份 counter(calls)
	code := blockCode(cg, exp.Stmts)
	if !strings.Contains(code, "__kaula_b") {
		t.Fatalf("应生成绑定临时变量，实际: %s", code)
	}
	if strings.Count(code, "counter(") != 1 {
		t.Fatalf("counter 应只求值一次（绑定声明处），实际出现 %d 次: %s",
			strings.Count(code, "counter("), code)
	}
}

// TestVarArgUnusedNotEvaluated：模板未消费变参时位置实参不求值、不绑定
func TestVarArgUnusedNotEvaluated(t *testing.T) {
	src := `
prefix Ignore {
    $( $x ),*
    println("nothing")
}

fn main() {
    @Ignore(counter())
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	code := blockCode(cg, exp.Stmts)
	if strings.Contains(code, "__kaula_b") {
		t.Fatalf("未消费的变参不应绑定临时变量: %s", code)
	}
	if strings.Contains(code, "counter") {
		t.Fatalf("未消费的位置实参不应求值: %s", code)
	}
}

// TestVarArgNestedRepeatError：重复片段内嵌套重复占位 → 报错
func TestVarArgNestedRepeatError(t *testing.T) {
	src := `
prefix Bad {
    $( $x ),*
    $( println($( $x ),*) ),*
}

fn main() {
    @Bad(1, 2)
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	found := false
	for _, err := range exp.Errors {
		if strings.Contains(err, "嵌套") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报「嵌套」错误，实际: %v", exp.Errors)
	}
}

// TestVarArgStrayRefError：重复占位外的裸 `$x` 引用 → 报错
func TestVarArgStrayRefError(t *testing.T) {
	src := `
prefix Bad {
    $( $x ),*
    println($x)
}

fn main() {
    @Bad(1)
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	found := false
	for _, err := range exp.Errors {
		if strings.Contains(err, "只能通过") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报「只能通过重复占位引用」错误，实际: %v", exp.Errors)
	}
}

// TestVarArgNestedStatementRepeat：重复占位出现在 if 语句体内可正常展开
func TestVarArgNestedStatementRepeat(t *testing.T) {
	src := `
prefix Maybe {
    $( $x ),*
    if true {
        $( println($x) ),*
    }
}

fn main() {
    @Maybe(7)
}`
	call, cg := macroCallIn(t, src, 0)
	exp := expand(t, call, cg)
	if len(exp.Errors) != 0 {
		t.Fatalf("展开报错: %v", exp.Errors)
	}
	body := nonTempStmts(exp.Stmts)
	if len(body) != 1 {
		t.Fatalf("期望 1 条 if 语句，实际 %d", len(body))
	}
	if code := blockCode(cg, exp.Stmts); !strings.Contains(code, "7") {
		t.Fatalf("if 体内的重复应展开出实参值，实际: %s", code)
	}
}

// TestVarArgDistinctTempsPerCall：不同调用点得到不同的临时变量序号（不碰撞）
func TestVarArgDistinctTempsPerCall(t *testing.T) {
	src := `
prefix Emit {
    $( $x ),*
    $( println($x) ),*
}

fn main() {
    @Emit(1, 2)
    @Emit(3)
}`
	cg, errs := compileMacro(t, src)
	if len(errs) != 0 {
		t.Fatalf("编译报错: %v", errs)
	}
	call1, _ := macroCallIn(t, src, 0)
	call2, _ := macroCallIn(t, src, 1)
	exp1 := expand(t, call1, cg)
	exp2 := expand(t, call2, cg)
	if len(exp1.Errors) != 0 || len(exp2.Errors) != 0 {
		t.Fatalf("展开报错: %v / %v", exp1.Errors, exp2.Errors)
	}
	code1 := blockCode(cg, exp1.Stmts)
	code2 := blockCode(cg, exp2.Stmts)
	if strings.Contains(code1, code2) {
		t.Fatalf("两次调用的展开不应互相包含: %s / %s", code1, code2)
	}
}
