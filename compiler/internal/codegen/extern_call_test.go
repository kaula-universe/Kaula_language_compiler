package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
)

// F15 回归：extern fn 声明的函数是用户引入的 C 符号，调用点必须保持原名。
// 此前 max/min/abs 的防 C 宏重定向（kaula_abs 等）先于 extern 判定，
// 导致 `extern fn abs` 声明生成 extern abs(...)，调用却生成 kaula_abs(...)，
// 任何与运行时惯用名同名的 extern 函数都无法编译。
func TestExternCallKeepsOriginalSymbol(t *testing.T) {
	src := `
extern fn abs(n: i32) -> i32

fn main() {
    i32 v = abs(-42)
    println("v=", v)
}
`
	prog := parse(src)
	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	out := cg.Generate(prog)
	if strings.Contains(out, "kaula_abs(") {
		t.Errorf("extern abs call must not be redirected to kaula_abs, generated:\n%s", out)
	}
	if !strings.Contains(out, "abs(-42)") {
		t.Errorf("extern abs call should keep symbol name abs, generated:\n%s", out)
	}
}
