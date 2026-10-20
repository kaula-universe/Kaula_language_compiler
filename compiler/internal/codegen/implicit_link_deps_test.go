package codegen

import (
	"kaula/internal/config"
	"kaula/internal/sema"
	"slices"
	"testing"
)

// 隐式链接依赖回归
//
// 代码生成器会无条件产出一些 std 符号，即使源码没有 import 对应模块：
//   - String 变量在作用域出口生成 string_survivor_free(...)  → std/string/string.c
//   - 多参数 println 生成 println_multi(...)                  → std/io/io.c
//
// usedModules 只来自直接 import，若不补挂，链接阶段就会 undefined symbol。
// 此前所有字符串/多参打印用例都恰好 import 了对应模块，缺陷长期未被发现。
func TestImplicitStdModuleDeps(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		module string
	}{
		{
			name:   "string literal without import",
			src:    "fn main() {\n    auto a = \"hello\"\n    println(a)\n}\n",
			module: "std.string",
		},
		{
			name:   "multi-arg println without import",
			src:    "fn main() {\n    println(1, 2)\n}\n",
			module: "std.io",
		},
		{
			name:   "reflection string result without import",
			src:    "struct P {\n    x: i64\n}\n\nfn main() {\n    auto s = type_name(P)\n    println(s)\n}\n",
			module: "std.string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parse(tc.src)
			// 必须跑真实语义分析：auto 变量的类型推导决定作用域出口是否生成
			// string_survivor_free，跳过 sema 会让用例假阴性。
			sema.NewSemanticAnalyzer().Analyze(prog)
			cg := NewCodeGenerator(config.DefaultConfig())
			cg.Generate(prog)

			if !slices.Contains(cg.GetUsedModules(), tc.module) {
				t.Errorf("usedModules = %v, want %q (would cause undefined symbol at link)",
					cg.GetUsedModules(), tc.module)
			}
		})
	}
}

// 显式 import 时不得重复登记同一模块：重复 .o 会让链接器报重复定义
// （Windows 下是 LNK2005）。
func TestImplicitStdModuleNoDuplicate(t *testing.T) {
	src := "import std.string\nimport std.io\n\nfn main() {\n    auto a = \"hello\"\n    println(a, 1)\n}\n"
	prog := parse(src)
	sema.NewSemanticAnalyzer().Analyze(prog)
	cg := NewCodeGenerator(config.DefaultConfig())
	cg.Generate(prog)

	seen := map[string]int{}
	for _, m := range cg.GetUsedModules() {
		seen[m]++
	}
	for _, m := range []string{"std.string", "std.io"} {
		if seen[m] != 1 {
			t.Errorf("module %q registered %d times, want exactly 1 (usedModules=%v)",
				m, seen[m], cg.GetUsedModules())
		}
	}
}
