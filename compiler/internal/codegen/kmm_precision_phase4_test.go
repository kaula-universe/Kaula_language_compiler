package codegen

import (
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/config"
)

// Phase 4：类方法调用纳入跨函数分配闭包（kmm_precision.go）。
// 正向：不分配的方法调用不再让裸块白套 KMM_V4_SCOPE。
// 下界：分配方法、接口动态派发、同名方法任一分配、未知方法一律保守包壳。

// extractFuncC 取 C 源码中某个函数签名起至函数结尾的片段（用于精确检查目标函数）。
func extractFuncC(c, sig string) string {
	i := strings.Index(c, sig)
	if i < 0 {
		return ""
	}
	j := strings.Index(c[i:], "\n}")
	if j < 0 {
		return c[i:]
	}
	return c[i : i+j+2]
}

func TestPureMethodCallNoBareBlockScope(t *testing.T) {
	src := `class Calc {
    n: i64
    constructor(i64 n) { self.n = n }
    fn twice() i64 { return self.n * 2 }
}
fn bare_method(c: Calc) i64 {
    i64 n = 0
    {
        n = c.twice()
    }
    return n
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	body := extractFuncC(c, "int64_t bare_method(")
	if body == "" {
		t.Fatalf("未找到 bare_method 函数：\n%s", c)
	}
	if strings.Contains(body, "KMM_V4_SCOPE_START") ||
		strings.Contains(body, "kmm_v4_survivor_checkpoint") {
		t.Errorf("不分配的方法调用不应触发裸块 KMM 作用域：\n%s", body)
	}
}

func TestMethodChainClosureNoScope(t *testing.T) {
	// outer → inner，inner 不分配：不动点传播后 outer 仍不分配
	src := `class Calc {
    n: i64
    constructor(i64 n) { self.n = n }
    fn inner() i64 { return self.n * 2 }
    fn outer() i64 { return self.inner() + 1 }
}
fn f(c: Calc) i64 {
    i64 r = 0
    {
        r = c.outer()
    }
    return r
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	body := extractFuncC(c, "int64_t f(")
	if strings.Contains(body, "KMM_V4_SCOPE_START") {
		t.Errorf("方法调用链闭包不分配时不应触发裸块作用域：\n%s", body)
	}
}

func TestAllocatingMethodCallStillWrapped(t *testing.T) {
	// 安全下界：方法体内 stdlib 堆返回调用 → 方法分配 → 调用方裸块仍包壳
	src := `import std.string
class Factory {
    fn make() string {
        return std.string.create("x")
    }
}
fn f(factory: Factory) i64 {
    i64 n = 0
    {
        factory.make()
    }
    return n
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	body := extractFuncC(c, "int64_t f(")
	if body == "" {
		t.Fatalf("未找到 f 函数：\n%s", c)
	}
	if !strings.Contains(body, "KMM_V4_SCOPE_START") {
		t.Errorf("分配方法调用必须维持裸块 KMM 作用域（安全下界）：\n%s", body)
	}
}

func TestInterfaceMethodCallStillWrapped(t *testing.T) {
	// 安全下界：接口动态派发，实现未知 → 方法名恒视为分配
	src := `interface Shape {
    fn area() i64
}
class Square implements Shape {
    n: i64
    constructor(i64 n) { self.n = n }
    fn area() i64 { return self.n }
}
fn calc(s: Shape) i64 {
    i64 r = 0
    {
        r = s.area()
    }
    return r
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	body := extractFuncC(c, "int64_t calc(")
	if body == "" {
		t.Fatalf("未找到 calc 函数：\n%s", c)
	}
	if !strings.Contains(body, "KMM_V4_SCOPE_START") {
		t.Errorf("接口方法动态派发必须保守包壳（安全下界）：\n%s", body)
	}
}

func TestSameNameMethodAnyAllocStillWrapped(t *testing.T) {
	// 安全下界：两个类同名方法，任一分配 → 同名方法整体视为分配
	src := `import std.string
class Pure {
    fn go() i64 { return 1 }
}
class Impure {
    fn go() string { return std.string.create("x") }
}
fn f(p: Pure) i64 {
    i64 r = 0
    {
        r = p.go()
    }
    return r
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	body := extractFuncC(c, "int64_t f(")
	if body == "" {
		t.Fatalf("未找到 f 函数：\n%s", c)
	}
	if !strings.Contains(body, "KMM_V4_SCOPE_START") {
		t.Errorf("同名方法任一分配时必须保守包壳（安全下界）：\n%s", body)
	}
}

func TestConstructorCallRemainsAllocPoint(t *testing.T) {
	// 安全下界：构造器 KMM_V4_ALLOC_ZERO 分配 self，以类名入表恒 true
	src := `class Box {
    n: i64
    constructor(i64 n) { self.n = n }
}
fn f(i64 n) i64 {
    i64 r = 0
    {
        Box b = Box(n)
        r = b.n
    }
    return r
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	if !strings.Contains(c, "KMM_V4_ALLOC_ZERO") {
		t.Errorf("构造器仍应分配 self：\n%s", c)
	}
}

// ---- Phase 4b：match 值赋给 String 的 SSO-only 判定 ----

func TestMatchLiteralValueNoClone(t *testing.T) {
	src := `enum P { A, B }
fn label(p: P) string {
    string s = match p {
        A => "aa",
        B => "bb"
    }
    return s
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	body := extractFuncC(c, "String label(")
	if body == "" {
		t.Fatalf("未找到 label 函数：\n%s", c)
	}
	if strings.Contains(body, "string_clone_global") ||
		strings.Contains(body, "KMM_V4_SCOPE_NAMED") ||
		strings.Contains(body, "kmm_v4_survivor_checkpoint") {
		t.Errorf("match 全字面量分支的 String 应走 SSO move，免 clone/wrapper：\n%s", body)
	}
}

func TestMatchHeapArmStillClones(t *testing.T) {
	// 安全下界：任一分支来自堆返回调用 → 非 SSO，必须保留 clone/wrapper
	src := `import std.string
enum P { A, B }
fn label(p: P) string {
    string s = match p {
        A => std.string.create("aa"),
        B => "bb"
    }
    return s
}
fn main() { std.io.println("probe") }`
	c := generateForTest(t, src)
	body := extractFuncC(c, "String label(")
	if body == "" {
		t.Fatalf("未找到 label 函数：\n%s", c)
	}
	if !strings.Contains(body, "string_clone_global") &&
		!strings.Contains(body, "KMM_V4_SCOPE_NAMED") {
		t.Errorf("match 含堆分配分支必须走保守 clone/wrapper（安全下界）：\n%s", body)
	}
}

// ---- Phase 4c：分配点检测的表达式覆盖（安全加固） ----

func TestExprMayAllocateCoversMoreNodes(t *testing.T) {
	cg := NewCodeGenerator(config.DefaultConfig())
	cases := []struct {
		name string
		expr ast.Expression
		want bool
	}{
		{"spawn always allocates", &ast.SpawnExpression{}, true},
		{"lambda capture may allocate", &ast.LambdaExpression{}, true},
		{"prefix macro opaque", &ast.PrefixCallExpression{Name: "M"}, true},
		{"index of allocating base", &ast.IndexExpression{Object: &ast.SpawnExpression{}}, true},
		{"array literal with allocating element", &ast.ArrayLiteral{Elements: []ast.Expression{&ast.SpawnExpression{}}}, true},
		{"await of allocating handle", &ast.AwaitExpression{Handle: &ast.SpawnExpression{}}, true},
		{"plain index", &ast.IndexExpression{Object: &ast.Identifier{Name: "a"}}, false},
		{"plain array literal", &ast.ArrayLiteral{Elements: []ast.Expression{&ast.Identifier{Name: "a"}}}, false},
	}
	for _, tc := range cases {
		if got := cg.exprMayAllocate(tc.expr); got != tc.want {
			t.Errorf("%s: exprMayAllocate = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStmtMayAllocateCoversCallYieldExtract(t *testing.T) {
	cg := NewCodeGenerator(config.DefaultConfig())
	alloc := &ast.SpawnExpression{}
	cases := []struct {
		name string
		stmt ast.Statement
		want bool
	}{
		{"call statement target", &ast.CallStatement{Target: alloc}, true},
		{"yield source", &ast.YieldStatement{Source: alloc}, true},
		{"extract source", &ast.ExtractStatement{Source: alloc}, true},
		{"extract index", &ast.ExtractStatement{Index: alloc}, true},
	}
	for _, tc := range cases {
		if got := cg.stmtMayAllocate(tc.stmt); got != tc.want {
			t.Errorf("%s: stmtMayAllocate = %v, want %v", tc.name, got, tc.want)
		}
	}
}
