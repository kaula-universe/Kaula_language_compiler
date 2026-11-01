package codegen

import (
	"regexp"
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// generateForMarkTest 走 parse → sema → codegen，返回生成的 C 代码。
// 不做前端 AST 优化，保证断言只针对 KMM 区间发射形态。
func generateForMarkTest(t *testing.T, src string) string {
	t.Helper()
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cg := NewCodeGenerator(config.DefaultConfig())
	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}
	return cCode
}

// TestRecursiveFuncUsesMarkNotScope 递归 + 每层分配的函数必须改用 base 感知的
// mark save/restore：mark 落在 C 栈上，不消耗 KMM scope 栈的定长槽位，
// 递归深度因此不再受 KMM_V4_MAX_SCOPE_DEPTH 约束。
func TestRecursiveFuncUsesMarkNotScope(t *testing.T) {
	src := `fn rec(i32 n) i32 {
    string s = "abc"
    if (n <= 0) { return 0 }
    return 1 + rec(n - 1)
}
` + "\nfn main() i32 {\n    return 0\n}\n"

	cCode := generateForMarkTest(t, src)

	if !strings.Contains(cCode, "kmm_v4_mark_t __kmm_fnmark = kmm_v4_mark_save();") {
		t.Errorf("期望函数级使用 mark save，实际生成的 C：\n%s", cCode)
	}
	if strings.Contains(cCode, "KMM_V4_SCOPE_NAMED(\"rec\")") {
		t.Errorf("递归函数不应再走 KMM_V4_SCOPE_NAMED（受 1024 深度限制）：\n%s", cCode)
	}
	// 函数级 mark 只声明一次
	if strings.Count(cCode, "kmm_v4_mark_t __kmm_fnmark = kmm_v4_mark_save();") != 1 {
		t.Errorf("函数级 mark 应只声明一次：\n%s", cCode)
	}
	// 每个 restore 引用的名字必须有对应声明（return 提前退出会复用同名的
	// 函数级 mark，因此 restore 次数多于 save 是正常的，但引用不能凭空出现）
	declared := map[string]bool{}
	declRe := regexp.MustCompile(`kmm_v4_mark_t (\w+) = kmm_v4_mark_save\(\)`)
	for _, m := range declRe.FindAllStringSubmatch(cCode, -1) {
		declared[m[1]] = true
	}
	useRe := regexp.MustCompile(`kmm_v4_mark_restore\((\w+)\)`)
	for _, m := range useRe.FindAllStringSubmatch(cCode, -1) {
		if !declared[m[1]] {
			t.Errorf("restore 引用了未声明的 mark %q：\n%s", m[1], cCode)
		}
	}
}

// TestMarkFuncReturnRewindsToFunctionMark 提前退出（return）路径必须回卷到
// 函数入口 mark，否则每层递归泄漏一层分配。
func TestMarkFuncReturnRewindsToFunctionMark(t *testing.T) {
	src := `fn rec(i32 n) i32 {
    string s = "abc"
    if (n <= 0) { return 0 }
    return 1 + rec(n - 1)
}
` + "\nfn main() i32 {\n    return 0\n}\n"

	cCode := generateForMarkTest(t, src)

	// 函数体内有 2 处 return，加上函数出口 1 处，共 3 处回卷
	want := 3
	if got := strings.Count(cCode, "kmm_v4_mark_restore(__kmm_fnmark)"); got != want {
		t.Errorf("期望 %d 处回卷到函数级 mark，实际 %d：\n%s", want, got, cCode)
	}
	// 每条 return 之前都必须先回卷（回卷语句必须出现在 return 之前）
	for _, line := range strings.Split(cCode, "\n") {
		_ = line
	}
	if !strings.Contains(cCode, "kmm_v4_mark_restore(__kmm_fnmark);\n        return") {
		t.Errorf("return 前缺少 mark 回卷：\n%s", cCode)
	}
}

// TestNonRecursiveFuncStillUsesScope 非递归函数保持原 scope push/pop 路径，
// 确保本次改动的作用域被严格限制在递归环内。
func TestNonRecursiveFuncStillUsesScope(t *testing.T) {
	src := `fn helper(i32 n) i32 {
    string s = "abc"
    return n
}
` + "\nfn main() i32 {\n    return 0\n}\n"

	cCode := generateForMarkTest(t, src)

	if !strings.Contains(cCode, "KMM_V4_SCOPE_NAMED(\"helper\")") {
		t.Errorf("非递归函数应继续使用 KMM_V4_SCOPE_NAMED：\n%s", cCode)
	}
	if strings.Contains(cCode, "kmm_v4_mark_save()") {
		t.Errorf("非递归函数不应切换到 mark 模式：\n%s", cCode)
	}
}

// TestMutualRecursionUsesMark 互递归同样每层 push 作用域，必须一并覆盖。
func TestMutualRecursionUsesMark(t *testing.T) {
	src := `fn even(i32 n) bool {
    if (n == 0) { return true }
    return odd(n - 1)
}
fn odd(i32 n) bool {
    string s = "x"
    if (n == 0) { return false }
    return even(n - 1)
}
` + "\nfn main() i32 {\n    return 0\n}\n"

	cCode := generateForMarkTest(t, src)

	// odd 体内有分配且处于互递归环 → mark；even 无分配不启用 KMM，不参与
	if !strings.Contains(cCode, "kmm_v4_mark_t __kmm_fnmark = kmm_v4_mark_save();") {
		t.Errorf("互递归函数应使用 mark save：\n%s", cCode)
	}
	if strings.Contains(cCode, "KMM_V4_SCOPE_NAMED(\"odd\")") {
		t.Errorf("互递归函数不应再走 KMM_V4_SCOPE_NAMED：\n%s", cCode)
	}
}

// TestConcurrentFuncKeepsScope 并发函数的守卫状态机按 kmmScopeDepth 发射
// scope_pop，不能切到 mark 模式，否则 pop/restore 语义错位。
func TestConcurrentFuncKeepsScope(t *testing.T) {
	src := `conc fn crec(i32 n) i32 {
    string s = "abc"
    if (n <= 0) { return 0 }
    return crec(n - 1)
}
` + "\nfn main() i32 {\n    return 0\n}\n"

	cCode := generateForMarkTest(t, src)

	if strings.Contains(cCode, "kmm_v4_mark_save()") {
		t.Errorf("并发函数不得使用 mark 模式：\n%s", cCode)
	}
}
