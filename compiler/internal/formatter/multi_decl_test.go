package formatter_test

import (
	"strings"
	"testing"

	"kaula/internal/formatter"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// format 格式化源码，并在输出无法重新解析时报错（保证格式化不破坏语法）
func format(t *testing.T, src string) string {
	t.Helper()
	out := formatter.New().FormatSource(src)

	lx := lexer.NewLexer(out)
	p := parser.NewParser(lx)
	p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		for _, e := range p.GetErrorCollector().Errors() {
			t.Logf("reparse error %d:%d %s", e.Line, e.Column, e.Message)
		}
		t.Fatalf("格式化输出存在 %d 个语法错误，无法重新解析", n)
	}
	return out
}

// TestFormatMultiVarDecl 验证多变量声明保持单条语句输出：i64 a = 1, b = 2
func TestFormatMultiVarDecl(t *testing.T) {
	src := "fn main() {\n    i64 a = 1, b = 2\n}\n"
	out := format(t, src)

	if !strings.Contains(out, "i64 a = 1, b = 2") {
		t.Errorf("格式化输出缺少 i64 a = 1, b = 2：\n%s", out)
	}
	// 幂等：再次格式化结果一致
	again := format(t, out)
	if out != again {
		t.Errorf("格式化非幂等：\n第一次:\n%s\n第二次:\n%s", out, again)
	}
}

// TestFormatMultiVarDeclNoInitializer 验证无初值形式 i64 c, d
func TestFormatMultiVarDeclNoInitializer(t *testing.T) {
	src := "fn main() {\n    i64 c, d\n}\n"
	out := format(t, src)

	if !strings.Contains(out, "i64 c, d") {
		t.Errorf("格式化输出缺少 i64 c, d：\n%s", out)
	}
}

// TestFormatMultiVarDeclAuto 验证 auto 形式 auto a = 1, b = 2
func TestFormatMultiVarDeclAuto(t *testing.T) {
	src := "fn main() {\n    auto a = 1, b = 2\n}\n"
	out := format(t, src)

	if !strings.Contains(out, "auto a = 1, b = 2") {
		t.Errorf("格式化输出缺少 auto a = 1, b = 2：\n%s", out)
	}
}

// TestFormatMultiVarDeclTypeSuffix 验证类型后缀与共享类型还原：
// i64 a[4], b 格式化后仍是 i64 a[4], b（而不是被改写成共享数组类型）
func TestFormatMultiVarDeclTypeSuffix(t *testing.T) {
	src := "fn main() {\n    i64 a[4], b\n}\n"
	out := format(t, src)

	if !strings.Contains(out, "i64 a[4], b") {
		t.Errorf("格式化输出缺少 i64 a[4], b：\n%s", out)
	}
}

// TestFormatSingleVarDeclUnchanged 验证单变量声明的格式化输出不受影响
func TestFormatSingleVarDeclUnchanged(t *testing.T) {
	src := "fn main() {\n    [64]i64 buf\n}\n"
	out := format(t, src)

	if !strings.Contains(out, "[64]i64 buf") {
		t.Errorf("格式化输出缺少 [64]i64 buf：\n%s", out)
	}
}
