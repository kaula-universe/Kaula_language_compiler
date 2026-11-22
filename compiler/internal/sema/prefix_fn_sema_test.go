package sema

import (
	"strings"
	"testing"
)

// parseForSema 由 extern_func_call_test.go 提供：lexer→parser→AST。
// （本文件只依赖该辅助函数与 NewSemanticAnalyzer/Analyze。）

// prefixMacroSrc 是教程《前缀系统》"表达式级宏"一节的完整示例
// （docs/tutorials/prefix-system.md，verify_tutorials.py 编号 prefix-system#12）。
const prefixMacroSrc = `import std.io

#[prefix]
fn Lower(c: char) -> i64 {
    i64 base = $c - 'A'
    base + 'a'          // 最后一条表达式 = 宏的值
}

fn main() {
    i64 v = @Lower(c = 'C') + 1
    std.io.println(v)
}
`

// TestPrefixFnTrailingExpressionIsReturnValue 回归：missing_return 误伤表达式级宏。
//
// 前端严格化的 missing_return 检查不认识 `#[prefix] fn` 的"最后一条表达式
// 语句就是宏的返回值"约定（教程《前缀系统》§表达式级宏），把教程示例
// prefix-system#12 判成 missing_return 错误，CI 的 verify_tutorials.py
// 因此连续挂掉。表达式级宏的尾表达式等价于 return <表达式>，必须豁免。
func TestPrefixFnTrailingExpressionIsReturnValue(t *testing.T) {
	prog := parseForSema(t, prefixMacroSrc)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	summary := sa.errorCollector.GetErrorSummary()
	if strings.Contains(summary, "不经过 return 的执行路径") {
		t.Fatalf("#[prefix] fn 的尾表达式被 missing_return 误判:\n%s", summary)
	}
	if sa.errorCollector.HasErrors() {
		t.Fatalf("表达式级宏不应产生语义错误，实际:\n%s", summary)
	}
}

// TestNormalFnStillRequiresReturn 保护：missing_return 豁免只作用于
// #[prefix] 模板（模板体是宏骨架，不是函数体）；普通函数缺 return
// 仍必须报错，防止放宽过度让真缺 return 的函数漏过。
func TestNormalFnStillRequiresReturn(t *testing.T) {
	src := `fn get_value() -> i64 {
    i64 v = 1
    v
}

fn main() {
    std.io.println(get_value())
}
`
	prog := parseForSema(t, src)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	if !sa.errorCollector.HasErrors() {
		t.Fatal("普通函数缺 return 仍应报 missing_return")
	}
	summary := sa.errorCollector.GetErrorSummary()
	if !strings.Contains(summary, "不经过 return 的执行路径") {
		t.Fatalf("期望 missing_return 错误，实际:\n%s", summary)
	}
}

// TestPrefixParamReferencedViaDollarName 回归：$param 引用未标记形参已使用。
//
// `$name` 的解析走 currentPrefixTable（analyzeIdentifier 的 IsPrefixVar 分支
// 提前 return），不经过普通标识符的 Referenced 标记路径——checkUnusedSymbols
// 会对只被 `$name` 引用的形参误报 unused_parameter。标记打通后该警告消失；
// 真正未使用的形参仍然报警告。
func TestPrefixParamReferencedViaDollarName(t *testing.T) {
	prog := parseForSema(t, prefixMacroSrc)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	summary := sa.errorCollector.GetErrorSummary()
	if strings.Contains(summary, "unused_parameter") || strings.Contains(summary, "未使用的参数") {
		t.Fatalf("形参 c 通过 $c 引用，不应报 unused_parameter:\n%s", summary)
	}
}

// TestPrefixMacroValueNotUnusedValue 回归：宏值表达式不应报 unused_value。
// 函数体最后一条表达式是宏的返回值（结果未被丢弃），与普通"纯值表达式
// 语句"不同。
func TestPrefixMacroValueNotUnusedValue(t *testing.T) {
	prog := parseForSema(t, prefixMacroSrc)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	summary := sa.errorCollector.GetErrorSummary()
	if strings.Contains(summary, "unused_value") || strings.Contains(summary, "表达式结果未使用") {
		t.Fatalf("#[prefix] fn 尾表达式被误报 unused_value:\n%s", summary)
	}
}
