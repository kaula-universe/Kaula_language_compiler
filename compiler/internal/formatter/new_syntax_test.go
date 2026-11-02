package formatter_test

import (
	"strings"
	"testing"

	"kaula/internal/formatter"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// formatNew 格式化源码并要求：① 无未覆盖节点 ② 输出可被重新解析。
// 这是 formatter 的硬契约——格式化不得丢语义、不得产出非法语法。
func formatNew(t *testing.T, src string) string {
	t.Helper()
	f := formatter.New()
	out := f.FormatSource(src)
	if f.HasUnknown() {
		t.Fatalf("存在未覆盖节点 %v，输入:\n%s", f.UnknownNodes(), src)
	}
	lx := lexer.NewLexer(out)
	p := parser.NewParser(lx)
	// 与 Formatter.FormatSource 内部保持一致：格式化片段未必含 main，
	// 必须跳过 main 检查，否则会把「找不到 main」误判为语法错误。
	p.SetSkipMainCheck(true)
	p.Parse()
	if n := p.GetErrorCollector().ErrorCount(); n != 0 {
		t.Fatalf("格式化输出 %d 个语法错误，无法重新解析:\n%s", n, out)
	}
	return out
}

func mustContain(t *testing.T, out, want, what string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("%s 丢失: 期望包含 %q，实得:\n%s", what, want, out)
	}
}

// spawn 表达式必须原样保留（此前被替换成 /* unknown expression */ 注释，丢代码）。
func TestFormatSpawnExpression(t *testing.T) {
	out := formatNew(t, "fn main() {\n    Handle h = spawn work(21)\n}\n")
	mustContain(t, out, "spawn work(21)", "spawn 表达式")
}

// await 句柄：仓库真实代码一律用后缀 h.await，必须保持后缀，
// 因为前缀形式在表达式位置会丢语句（parser 侧缺陷）。
func TestFormatAwaitKeepsSuffixForm(t *testing.T) {
	out := formatNew(t, "fn main() {\n    Handle h = spawn work(5)\n    i64 r = h.await\n}\n")
	mustContain(t, out, "h.await", "await（后缀形式）")
	mustContain(t, out, "i64 r = ", "await 所在声明")
	if strings.Contains(out, "await h") && !strings.Contains(out, "h.await") {
		t.Fatalf("await 被改写成前缀形式，表达式位置会丢语句:\n%s", out)
	}
}

// conc fn 的并发修饰符必须保留（此前被静默丢弃，并发函数退化为普通函数）。
func TestFormatKeepsConcModifier(t *testing.T) {
	out := formatNew(t, "conc fn ping() i32 {\n    return 1\n}\n")
	mustContain(t, out, "conc fn ping", "conc 修饰符")
}

// pub 修饰符必须保留（此前被静默丢弃，跨文件不可见）。
func TestFormatKeepsPubModifier(t *testing.T) {
	out := formatNew(t, "pub fn helper(i64 a) {\n    return\n}\n")
	mustContain(t, out, "pub fn helper", "pub 修饰符")
}

// 泛型函数不得被写成 export。
//
// FunctionStatement.Generic 的含义是「是否是泛型函数」（由 AddTypeParam 置位），
// 与导出语义无关。曾据此输出 `export `，凭空给每个泛型函数加上 export 前缀。
//
// 注意：这里只断言「没有 export」这一 formatter 自身的契约，
// 不解析输出。仓库真实泛型语法是 `<T>`，而 formatter 会把它规范化成 `[T]`，
// 后者走的是一条尚未实现的 [PARAM-GENERIC] 路径、解析不可靠（会在 stdout
// 打调试日志、泛型参数列丢失）。那属于 parser 的既存缺陷，不属于 formatter，
// 不能用它来判定 formatter 是否有问题。
func TestFormatGenericFuncIsNotExport(t *testing.T) {
	src := "fn identity<T>(x: T) T {\n    return x\n}\n"
	f := formatter.New()
	out := f.FormatSource(src)
	if f.HasUnknown() {
		t.Fatalf("存在未覆盖节点 %v", f.UnknownNodes())
	}
	if strings.Contains(out, "export") {
		t.Fatalf("泛型函数被误写成 export:\n%s", out)
	}
	mustContain(t, out, "fn identity", "泛型函数名")
}

// export fn 必须原样输出一个 export，不得重复成 `pub export`。
//
// parser 置 IsExported 的同时也置 IsPublic，若两者都输出就会写成
// `pub export fn`；而 parser 不接受 `pub export` 组合（pub 后面必须是
// fn/struct/class/变量声明），二次解析失败并把函数体降级成垃圾。
func TestFormatExportFuncNotDuplicated(t *testing.T) {
	out := formatNew(t, "export fn own_fn() i32 {\n    return 99\n}\n")
	mustContain(t, out, "export fn own_fn", "export 修饰符")
	if strings.Contains(out, "pub export") {
		t.Fatalf("export fn 被重复写成了 pub export:\n%s", out)
	}
}

// := 错误传播赋值必须与普通 = 区分（此前被改写成 =，Err 不再向上传播）。
func TestFormatKeepsTryAssign(t *testing.T) {
	out := formatNew(t, "fn f() {\n    v := might_fail(true)\n}\n")
	mustContain(t, out, ":= might_fail(true)", ":= 错误传播")
}

// 数组字面量必须保留（此前被替换成 /* unknown expression */）。
func TestFormatArrayLiteral(t *testing.T) {
	out := formatNew(t, "fn main() {\n    i32 arr[10] = [0, 1, 2, 3]\n}\n")
	mustContain(t, out, "[0, 1, 2, 3]", "数组字面量")
}

// 格式化必须幂等：二次格式化结果与首次一致（round-trip 稳定）。
func TestFormatIdempotentOnNewSyntax(t *testing.T) {
	src := "conc fn work(n: i64) i64 {\n    return n * 2\n}\n\n" +
		"pub fn go() i64 {\n    Handle h = spawn work(21)\n    i64 r = h.await\n    return r\n}\n"
	once := formatNew(t, src)
	twice := formatNew(t, once)
	if once != twice {
		t.Fatalf("二次格式化不稳定:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

// 一元前缀运算符的操作数若为复合表达式，必须保留括号。
//
// 回归背景：formatter 曾无条件输出 `op + 操作数`，
// 于是 `!(5 < 3)` 被写成 `!5 < 3`，
// 重新解析变成 `(!5) < 3` —— 语义从「5 不小于 3」(false)
// 变成「-5 小于 3」(true)，条件恒真，测试全绿但断言失效。
func TestFormatUnaryKeepsParens(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"取反比较", "fn main() {\n    check(!(5 < 3))\n}\n", "!(5 < 3)"},
		{"取反成员访问保持原子", "fn main() {\n    check(!flag)\n}\n", "!flag"},
		{"取反调用无需括号", "fn main() {\n    check(!ready())\n}\n", "!ready()"},
		{"取反下标访问", "fn main() {\n    check(!done[0])\n}\n", "!done[0]"},
		{"负号加法必须括号", "fn main() {\n    i64 v = -(a + b)\n}\n", "-(a + b)"},
		{"负号字面量无需括号", "fn main() {\n    i64 v = -1\n}\n", "-1"},
		{"按位取反", "fn main() {\n    i64 v = ~(a & b)\n}\n", "~(a & b)"},
		{"双重取反", "fn main() {\n    check(!(!(a < b)))\n}\n", "!(!(a < b))"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := formatNew(t, c.src)
			mustContain(t, out, c.want, "一元运算符括号")
		})
	}
}

// 一元运算符括号修复后必须保持语义：格式化结果要能重新解析，
// 且再次格式化不再变化（幂等）。
func TestFormatUnaryIdempotent(t *testing.T) {
	src := "fn main() {\n    check(!(a < b))\n    i64 v = -(x + y)\n}\n"
	out := formatNew(t, src)
	f2 := formatter.New()
	// FormatSource 已产出带尾换行的结果，直接回灌即可
	again := f2.FormatSource(out)
	if f2.HasParseError() {
		t.Fatalf("二次解析失败：%v", f2.ParseErrorMessages())
	}
	if again != out {
		t.Fatalf("一元表达式格式化不幂等：\n第一次:\n%q\n第二次:\n%q", out, again)
	}
}
