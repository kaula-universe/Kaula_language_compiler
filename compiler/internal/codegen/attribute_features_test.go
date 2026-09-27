package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// parseUnitTestSource 解析仅含 #[test] 函数（可能没有 main）的源码。
// 必须关掉 main 检查，否则 parser 会因「找不到 main 函数」报错——
// CLI 在测试模式下同样调用 SetSkipMainCheck(true)。
func parseUnitTestSource(t *testing.T, src string) string {
	t.Helper()
	p := parser.NewParser(lexer.NewLexer(src))
	p.SetSkipMainCheck(true)
	prog := p.Parse()
	if p.HasErrors() {
		t.Fatalf("parse failed: %v", p.GetErrorCollector().Errors())
	}
	cg := NewCodeGenerator(config.DefaultConfig())
	cg.TestMode = true
	return cg.Generate(prog)
}

// 生成 TestMode 输出，供下列断言共享
func genTestMode(t *testing.T, src string) string {
	t.Helper()
	return parseUnitTestSource(t, src)
}

// 回归：#[should_panic] 必须真的影响判定，而不是被静默忽略。
// 此前 ShouldPanic 被收集却从不出现在生成代码里，语义与 #[test] 完全相同。
func TestShouldPanicIsWiredIntoRunner(t *testing.T) {
	out := genTestMode(t, `
#[test]
#[should_panic]
fn test_expected_to_fail() {
    assert(1 == 2)
}
`)
	if !strings.Contains(out, `__kaula_run_test("test_expected_to_fail", NULL, K_test_expected_to_fail, 0, 1)`) {
		t.Errorf("should_panic test must be dispatched with should_panic=1, generated:\n%s", out)
	}
	if !strings.Contains(out, "if (should_panic) {") {
		t.Errorf("runner must branch on should_panic, generated:\n%s", out)
	}
}

// 非 should_panic 的测试必须传 0，避免被误判。
func TestNormalTestsDispatchWithShouldPanicZero(t *testing.T) {
	out := genTestMode(t, `
#[test]
fn test_plain() {
    assert(1 == 1)
}
`)
	if !strings.Contains(out, `__kaula_run_test("test_plain", NULL, K_test_plain, 0, 0)`) {
		t.Errorf("plain test must be dispatched with should_panic=0, generated:\n%s", out)
	}
}

// 回归：#[test("描述")] 的描述此前被收集后丢弃，输出里看不到。
func TestDescriptionIsEmitted(t *testing.T) {
	out := genTestMode(t, `
#[test("两数相加应得五")
fn test_described() {
    assert(1 + 1 == 2)
}
`)
	if !strings.Contains(out, `__kaula_run_test("test_described", "两数相加应得五"`) {
		t.Errorf("description must be passed to runner, generated:\n%s", out)
	}
}

// 回归：描述会被直接拼进生成的 C 源码，未转义时一个裸引号就能提前闭合字符串字面量、
// 破坏甚至注入生成的代码。注意 lexer 扫描 #[...] 内容时遇 \n 即止，跨行描述本就不被支持。
// 属性参数不做反转义，描述里的 \" 是字面的「反斜杠+引号」两个字符。
func TestDescriptionQuotesAreEscaped(t *testing.T) {
	out := genTestMode(t, `
#[test("a " b")]
fn test_odd_quote() {
    assert(1 == 1)
}
`)
	// 奇数个裸引号必须被转义成 \"，否则生成的 C 字符串会提前闭合
	if !strings.Contains(out, `"a \" b"`) {
		t.Errorf("bare quote in description must be escaped, generated:\n%s", out)
	}
	// 转义后 run_test 的实参必须是完整闭合的字符串字面量
	if !strings.Contains(out, `__kaula_run_test("test_odd_quote", "a \" b", K_test_odd_quote, 0, 0)`) {
		t.Errorf("escaped description must remain one well-formed C literal, generated:\n%s", out)
	}
}

// 描述中的单反斜杠必须被转义成双反斜杠，否则 C 编译器会把它当转义起始符
func TestDescriptionBackslashPreserved(t *testing.T) {
	out := genTestMode(t, `
#[test("a \\b")]
fn test_backslash() {
    assert(1 == 1)
}
`)
	if !strings.Contains(out, `"a \\\\b"`) {
		t.Errorf("single backslash in description must be doubled, generated:\n%s", out)
	}
}

// cStringLiteral 直接决定描述如何进入生成的 C 源码
func TestCStringLiteral(t *testing.T) {
	cases := map[string]string{
		"":           `NULL`,
		"plain":      `"plain"`,
		`a"b`:        `"a\"b"`,
		`a\b`:        `"a\\b"`,
		"a\nb":       `"a\nb"`,
		"a\tb":       `"a\tb"`,
		"a\x00b":     `"a\x00b"`,
		"a\x07b":     `"a\x07b"`,
		"a\x7fb":     `"a\x7fb"`,
		"emoji 🎉 ok": `"emoji 🎉 ok"`,
	}
	for in, want := range cases {
		if got := cStringLiteral(in); got != want {
			t.Errorf("cStringLiteral(%q) = %s, want %s", in, got, want)
		}
	}
}

// 回归：TestMode 下用户 main 必须被 runner 替换，否则会出现两个 main。
func TestTestModeReplacesMain(t *testing.T) {
	out := genTestMode(t, `
#[test]
fn t_ok() { assert(1 == 1) }

fn main() { println("user main") }
`)
	if strings.Contains(out, "user main") {
		t.Errorf("user main must be dropped in test mode, generated:\n%s", out)
	}
	if !strings.Contains(out, "===== Auto-generated Test Runner =====") {
		t.Errorf("test runner must be generated, generated:\n%s", out)
	}
}

// 回归：空描述应传 NULL，让 runner 跳过描述行
func TestEmptyDescriptionPassesNull(t *testing.T) {
	out := genTestMode(t, `
#[test]
fn t_none() { assert(1 == 1) }
`)
	if !strings.Contains(out, `__kaula_run_test("t_none", NULL,`) {
		t.Errorf("absent description must pass NULL, generated:\n%s", out)
	}
}

// #[ignore] 的测试不得被调用，且以 should_panic=1 传参也不应影响 IGNORED 判定
func TestIgnoredTestIsNotInvoked(t *testing.T) {
	out := genTestMode(t, `
#[test]
#[ignore]
fn t_skipped() { assert(1 == 2) }
`)
	if !strings.Contains(out, `__kaula_run_test("t_skipped", NULL, NULL, 1, 0)`) {
		t.Errorf("ignored test must pass NULL fn and ignored=1, generated:\n%s", out)
	}
}
