package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// generateForTest 走完整链路：parse → sema → codegen，返回生成的 C 代码
func generateForTest(t *testing.T, src string) string {
	t.Helper()
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}
	return cCode
}

// TestMultiVarDeclLocalCodegen 验证函数体内 `i64 a = 1, b = 2`
// 生成两条独立的 C 声明。
func TestMultiVarDeclLocalCodegen(t *testing.T) {
	src := "fn main() {\n    i64 a = 1, b = 2\n}\n"
	cCode := generateForTest(t, src)

	for _, want := range []string{"int64_t a = 1;", "int64_t b = 2;"} {
		if !strings.Contains(cCode, want) {
			t.Errorf("生成的 C 代码缺少 %q", want)
			t.Logf("generated C:\n%s", cCode)
		}
	}
}

// TestMultiVarDeclGlobalCodegen 验证顶层 `i64 g1 = 1, g2 = 2` 的全局变量输出
func TestMultiVarDeclGlobalCodegen(t *testing.T) {
	src := "i64 g1 = 1, g2 = 2\n" +
		"fn main() {\n" +
		"    g1 = g1 + g2\n" +
		"}\n"
	cCode := generateForTest(t, src)

	for _, want := range []string{"int64_t g1 = 1;", "int64_t g2 = 2;"} {
		if !strings.Contains(cCode, want) {
			t.Errorf("生成的 C 代码缺少 %q", want)
			t.Logf("generated C:\n%s", cCode)
		}
	}
}

// TestMultiVarDeclNoInitializerCodegen 验证 `i64 c, d`（无初值）两个变量都被声明
func TestMultiVarDeclNoInitializerCodegen(t *testing.T) {
	src := "fn main() {\n    i64 c, d\n    c = 1\n    d = c\n}\n"
	cCode := generateForTest(t, src)

	for _, want := range []string{"int64_t c;", "int64_t d;"} {
		if !strings.Contains(cCode, want) {
			t.Errorf("生成的 C 代码缺少 %q", want)
			t.Logf("generated C:\n%s", cCode)
		}
	}
}

// TestMultiVarDeclAutoCodegen 验证 `auto a = 1, b = 2` 各自推导类型后生成
func TestMultiVarDeclAutoCodegen(t *testing.T) {
	src := "fn main() {\n    auto a = 1, b = 2\n    a = b\n}\n"
	cCode := generateForTest(t, src)

	// 字面量 1/2 <= 255 → u8
	for _, want := range []string{"uint8_t a = 1;", "uint8_t b = 2;"} {
		if !strings.Contains(cCode, want) {
			t.Errorf("生成的 C 代码缺少 %q", want)
			t.Logf("generated C:\n%s", cCode)
		}
	}
}
