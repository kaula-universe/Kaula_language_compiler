package codegen

import (
	"regexp"
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/config"
	"kaula/internal/sema"
)

// nestedFnCode 生成 C 代码并返回（同时断言 codegen 无内部错误）
func nestedFnCode(t *testing.T, src string) string {
	t.Helper()
	prog := parse(src)
	sa := sema.NewSemanticAnalyzer()
	sa.Analyze(prog)
	if sa.ErrorCollector().HasErrors() {
		t.Fatalf("sema: %v", sa.ErrorCollector().Errors())
	}
	cg := NewCodeGenerator(config.DefaultConfig())
	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen: %v", cg.Errors())
	}
	return cCode
}

var wsRe = regexp.MustCompile(`[ \t]+`)

// hasDefOutsideMain 判断 "<type> K_name(params) {" 形式的定义是否出现在 main 之外
func hasDefOutsideMain(cCode, header string) bool {
	norm := wsRe.ReplaceAllString(cCode, " ")
	mainIdx := strings.Index(norm, "int main()")
	defIdx := strings.Index(norm, header+" {")
	if defIdx < 0 || mainIdx < 0 {
		return false
	}
	return defIdx > mainIdx
}

// 嵌套函数（fn 定义在某个函数体内）必须提升为文件级定义：
// C 不允许函数内嵌定义，就地生成会得到
// "function definition is not allowed here" + 调用点隐式声明错误。
func TestNestedFuncHoistedOutOfFunctionBody(t *testing.T) {
	cCode := nestedFnCode(t, "fn main() {\n"+
		"    fn add2(i64 x) i64 {\n"+
		"        return x + 2\n"+
		"    }\n"+
		"    i64 v = add2(40)\n"+
		"}\n")

	// 1) 定义必须提升到 main 之外
	if !hasDefOutsideMain(cCode, "int64_t K_add2(int64_t x)") {
		t.Errorf("nested function must be hoisted out of main's body; got:\n%s", cCode)
	}
	// 2) 调用点前必须有原型声明（否则 clang 报隐式声明）
	norm := wsRe.ReplaceAllString(cCode, " ")
	if !strings.Contains(norm, "int64_t K_add2(int64_t x);") {
		t.Errorf("forward declaration for nested function missing; got:\n%s", cCode)
	}
	// 3) main 的函数体内部不得出现函数定义
	if idx := strings.Index(norm, "int main() {"); idx >= 0 {
		body := norm[idx:]
		end := matchBraceEnd(body)
		if i := strings.Index(body[:end], "K_add2(int64_t x) {"); i >= 0 {
			t.Errorf("nested function definition appears inside main's body:\n%s", cCode)
		}
	}
}

// 两层嵌套：内层的嵌套函数也必须被提升（pendingNestedFuncs 按层隔离）
func TestNestedFuncNestedTwice(t *testing.T) {
	cCode := nestedFnCode(t, "fn main() {\n"+
		"    fn inner(i64 y) i64 {\n"+
		"        return y * 2\n"+
		"    }\n"+
		"    fn outer(i64 x) i64 {\n"+
		"        fn deeper(i64 z) i64 {\n"+
		"            return z + 1\n"+
		"        }\n"+
		"        return deeper(inner(x))\n"+
		"    }\n"+
		"    i64 v = outer(20)\n"+
		"}\n")

	for _, header := range []string{
		"int64_t K_inner(int64_t y)",
		"int64_t K_outer(int64_t x)",
		"int64_t K_deeper(int64_t z)",
	} {
		if !hasDefOutsideMain(cCode, header) {
			t.Errorf("%s must be hoisted out of main's body; got:\n%s", header, cCode)
		}
	}
	// 两层嵌套的原型声明同样必须存在
	norm := wsRe.ReplaceAllString(cCode, " ")
	for _, proto := range []string{
		"int64_t K_inner(int64_t y);",
		"int64_t K_outer(int64_t x);",
		"int64_t K_deeper(int64_t z);",
	} {
		if !strings.Contains(norm, proto) {
			t.Errorf("forward declaration %q missing:\n%s", proto, cCode)
		}
	}
}

// 嵌套函数捕获外层变量：捕获参数按值转发，定义仍在文件级
func TestNestedFuncCaptureHoisted(t *testing.T) {
	cCode := nestedFnCode(t, "fn main() {\n"+
		"    i64 seed = 40\n"+
		"    fn addSeed(i64 x) i64 {\n"+
		"        return x + seed\n"+
		"    }\n"+
		"    i64 v = addSeed(2)\n"+
		"}\n")
	norm := wsRe.ReplaceAllString(cCode, " ")
	// 捕获参数的具体形态由 capture.go 决定（值捕获 / 指针捕获），
	// 这里只要求：定义（以 ") {" 结尾）在 main 之外 + 调用点转发捕获变量
	defIdx := defHeaderIndex(norm, "K_addSeed(")
	if defIdx < 0 {
		t.Fatalf("capturing nested function definition not found; got:\n%s", cCode)
	}
	if mainIdx := strings.Index(norm, "int main()"); mainIdx < 0 || defIdx < mainIdx {
		t.Errorf("capturing nested function must be hoisted out of main's body (def@%d main@%d):\n%s",
			defIdx, mainIdx, cCode)
	}
	if !strings.Contains(norm, "K_addSeed(2,") {
		t.Errorf("captured call must forward the captured variable; got:\n%s", cCode)
	}
}

// 顶层函数定义不受影响（不得被误当作嵌套函数提升）
func TestTopLevelFuncNotHoisted(t *testing.T) {
	cCode := nestedFnCode(t, "fn helper(i64 x) i64 {\n    return x + 1\n}\n"+
		"fn main() {\n    i64 v = helper(1)\n}\n")
	norm := wsRe.ReplaceAllString(cCode, " ")
	mainIdx := strings.Index(norm, "int main()")
	defIdx := strings.Index(norm, "int64_t K_helper(int64_t x) {")
	if defIdx < 0 || mainIdx < 0 || defIdx > mainIdx {
		t.Errorf("top-level function must stay at file scope before main (def@%d main@%d):\n%s",
			defIdx, mainIdx, cCode)
	}
}

// defHeaderIndex 找到**定义**（形参列表后紧跟 "{"）而非原型声明（以 ";" 结尾）的位置
func defHeaderIndex(norm, namePrefix string) int {
	from := 0
	for {
		i := strings.Index(norm[from:], namePrefix)
		if i < 0 {
			return -1
		}
		abs := from + i
		rest := norm[abs:]
		if j := strings.Index(rest, "{"); j >= 0 {
			// 定义与函数名之间不能出现分号（分号说明这是原型声明）
			if !strings.Contains(rest[:j], ";") {
				return abs
			}
		}
		from = abs + len(namePrefix)
	}
}

// matchBraceEnd 返回 s 中第一个 '{' 匹配到 '}' 的结束位置
func matchBraceEnd(s string) int {
	start := strings.Index(s, "{")
	if start < 0 {
		return len(s)
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

// 编译期即可发现：C 函数定义不得出现在任何函数体内部
func TestNoFunctionDefinitionNestedInAnyBody(t *testing.T) {
	// 顶层 fn + 嵌套 fn 混合，覆盖"嵌套在 if 块内"的提升路径
	cCode := nestedFnCode(t, "fn outer(i64 x) i64 {\n"+
		"    if x > 0 {\n"+
		"        fn branch(i64 y) i64 {\n"+
		"            return y + 1\n"+
		"        }\n"+
		"        return branch(x)\n"+
		"    }\n"+
		"    return 0\n"+
		"}\n"+
		"fn main() {\n    i64 v = outer(1)\n}\n")
	norm := wsRe.ReplaceAllString(cCode, " ")
	mainIdx := strings.Index(norm, "int main()")
	for _, header := range []string{
		"int64_t K_outer(int64_t x)",
		"int64_t K_branch(int64_t y)",
	} {
		i := strings.Index(norm, header+" {")
		if i < 0 {
			t.Errorf("definition %q missing:\n%s", header, cCode)
			continue
		}
		if i > mainIdx {
			t.Errorf("%s must be defined before main (file scope), got def@%d main@%d:\n%s",
				header, i, mainIdx, cCode)
		}
	}
}

// 确认测试用的是真实 AST 形态：嵌套 fn 确实在 main 的 body 里
func TestNestedFuncAstShape(t *testing.T) {
	prog := parse("fn main() {\n    fn add2(i64 x) i64 { return x + 2 }\n    i64 v = add2(40)\n}\n")
	fn, ok := prog.Statements[0].(*ast.FunctionStatement)
	if !ok {
		t.Fatalf("top[0] = %T, want *ast.FunctionStatement", prog.Statements[0])
	}
	if len(fn.Body) != 2 {
		t.Fatalf("main body len = %d, want 2", len(fn.Body))
	}
	if _, ok := fn.Body[0].(*ast.FunctionStatement); !ok {
		t.Errorf("main body[0] = %T, want *ast.FunctionStatement (nested fn)", fn.Body[0])
	}
}
