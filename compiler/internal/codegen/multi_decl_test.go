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

	// 整数字面量默认 int（int64_t）：不再按数值大小收窄成 uint8_t，
	// 否则 `auto acc = 0` 累加时会静默回绕。
	for _, want := range []string{"int64_t a = 1;", "int64_t b = 2;"} {
		if !strings.Contains(cCode, want) {
			t.Errorf("生成的 C 代码缺少 %q", want)
			t.Logf("generated C:\n%s", cCode)
		}
	}
}

// TestAutoIntegerLiteralIsInt 回归：整数字面量的 auto 推导默认 int（int64_t）。
//
// 此前按数值大小收窄（0~255 → uint8_t），导致
//
//	auto acc = 0
//	acc = acc + 1000     // 258 % 256 = 2，实测输出 232
//
// 静默给出错误结果，且负数累加直接下溢。语言文档
// （docs/tutorials/variables-and-types.md）明确 int = int64_t，
// 需要窄类型应显式标注 u8/i32。
func TestAutoIntegerLiteralIsInt(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"fn main() {\n    auto a = 0\n}\n", "int64_t a = 0;"},
		{"fn main() {\n    auto b = 300\n}\n", "int64_t b = 300;"},
		{"fn main() {\n    auto c = 100000\n}\n", "int64_t c = 100000;"},
		{"fn main() {\n    auto d = -1\n}\n", "int64_t d = -1;"},
	}
	for _, tc := range cases {
		cCode := generateForTest(t, tc.src)
		if !strings.Contains(cCode, tc.want) {
			t.Errorf("缺少 %q，生成:\n%s", tc.want, cCode)
		}
		for _, narrow := range []string{"uint8_t", "uint16_t", "uint32_t", "int8_t", "int16_t"} {
			if strings.Contains(cCode, narrow) {
				t.Errorf("整数字面量不应被收窄为 %s，生成:\n%s", narrow, cCode)
			}
		}
	}
}
