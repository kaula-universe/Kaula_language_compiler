package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/stdlib"
)

// println 注册函数回归：stdlib 注册表中返回 "String"（大写，真实
// stdlib.json 的形态）的函数，以点号全名 std.string.xxx() 形式直接
// 传给 println 时，必须走字符串格式化；当前实现按短名查表 + 只比小写
// "string"，两条都 miss 后退化为 printf("%d\n", String值) 打垃圾。
func TestPrintlnRegisteredStringCall(t *testing.T) {
	src := `
import std.io
import std.string

fn main() {
    auto sb = std.string.string_builder_create()
    println(std.string.string_builder_to_string(sb))
}
`
	prog := parse(src)
	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	sc := &stdlib.StdlibConfig{
		Modules: map[string]stdlib.Module{
			"std.string": {
				Header: "string/string.h",
				Functions: map[string]stdlib.Function{
					"string_builder_create":    {Return: "StringBuilder*"},
					"string_builder_to_string": {Args: []string{"StringBuilder*"}, Return: "String"},
				},
			},
		},
	}
	cg.SetStdlibConfig(sc)
	out := cg.Generate(prog)
	if strings.Contains(out, `"%d\n", string_builder_to_string(`) ||
		strings.Contains(out, "(int64_t)(string_builder_to_string(") {
		t.Errorf("registered String-returning call must print as string, generated:\n%s", out)
	}
}
