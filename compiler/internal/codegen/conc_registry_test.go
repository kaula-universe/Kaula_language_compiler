package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// genWithNewCG 走 parse → sema → codegen，并且**用一个全新的 CodeGenerator**。
//
// 用新实例而不是复用，是这里的关键：真实的一次编译里每个 local import
// 文件都有自己的 CodeGenerator（见 kaulac main.go 的 localCG），
// 而 conc fn 签名表曾经是包级全局，于是文件之间互相看见对方的 conc fn。
func genWithNewCG(t *testing.T, src string) (string, *CodeGenerator) {
	t.Helper()
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cg := NewCodeGenerator(config.DefaultConfig())
	c := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}
	return c, cg
}

// TestConcFuncRegistry_PerCodeGenerator 验证 conc fn 签名表是 per-CodeGenerator 的。
//
// 回归背景：签名表曾是包级 map。A 文件生成的 conc fn 会留在表里，
// 于是编译 B 文件时 B 里的 `spawn worker(...)` 能查到 A 的签名 ——
// 而 spawn 只按 Frame 的**字段名**（形参名）写实参，形参名对不上不报 C 错误，
// 只是把实参塞进错误的字段，await 恒取到零值。
//
// 所以正确行为是查不到 → generateSpawnExpression 走明确的错误 handle。
func TestConcFuncRegistry_PerCodeGenerator(t *testing.T) {
	const defineConc = `
conc fn worker(i64 a, i64 b) -> i64 {
    return a + b
}

fn main() -> i32 {
    return 0
}
`
	const caller = `
fn main() -> i32 {
    Handle h = spawn worker(1, 2)
    return 0
}
`

	// 先编译「定义 conc fn 的那个文件」：它的签名必须留在自己的表里
	defCode, cgDef := genWithNewCG(t, defineConc)
	if !strings.Contains(defCode, "K_worker_Frame") {
		t.Fatalf("第一个 TU 应当生成 K_worker 的 Frame 定义，实际：\n%s", defCode)
	}
	if cgDef.LookupConcFunc("K_worker") == nil {
		t.Fatalf("定义 conc fn 的 TU 必须能查到自己登记的签名")
	}

	// 再编译「另一个文件」：它没有定义 conc fn，不该看见上一个的签名
	callerCode, cgCaller := genWithNewCG(t, caller)
	if cgCaller.LookupConcFunc("K_worker") != nil {
		t.Errorf("conc fn 签名表泄漏到了另一个 CodeGenerator：%v", cgCaller.LookupConcFunc("K_worker"))
	}
	if !strings.Contains(callerCode, "不是 conc fn") {
		t.Errorf("查不到签名时必须生成明确的错误 handle，不能猜。实际生成：\n%s", callerCode)
	}
	if strings.Contains(callerCode, "K_worker_Frame") {
		t.Errorf("不得引用别的编译单元的 Frame，实际生成：\n%s", callerCode)
	}
}

// 同一个 CodeGenerator 内重复生成必须仍然查到（表随实例活，不随调用清空）
func TestConcFuncRegistry_SameGeneratorStillResolves(t *testing.T) {
	const src = `
conc fn worker(i64 a) -> i64 {
    return a + 1
}

fn main() -> i32 {
    Handle h = spawn worker(7)
    i64 r = h.await
    return 0
}
`
	prog := parse(src)
	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cg := NewCodeGenerator(config.DefaultConfig())
	c := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}
	if !strings.Contains(c, "K_worker_Frame") {
		t.Errorf("同单元内的 spawn 必须能查到 conc fn 签名，实际生成：\n%s", c)
	}
}
