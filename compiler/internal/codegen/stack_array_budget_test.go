package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// 局部数组是**栈**分配。
//
// `[100000]Handle hs` 在 C 侧是 `KaulaHandle hs[100000];` —— Handle 是
// {uint64_t task_id; KaulaTask* task;} 共 16 字节，100000 个就是 1.6 MB。
// Windows 默认线程栈 1 MB，于是进 main 之前就 STATUS_STACK_OVERFLOW
// (0xC00000FD / rc=3221225725)，且全程没有任何输出。
//
// 这个失败形态极难定位：不是"某行写错了"，而是"程序莫名其妙崩了"。
// 字节数在编译期就能算出来，所以必须在编译期拦住并说清楚。
//
// 判据：
//  1. 编译期可知的字节数超阈值 → 编译错误，点名变量与字节数
//  2. 阈值内的数组照常生成栈声明（不能把正常代码全拒了）
//  3. 长度是运行期变量时算不出字节数 → 放过（不能误报）

// generateErrorsForTest 与 generateForTest 同链路，但**不**因 codegen 报错
// 而失败 —— 本组用例要断言的正是"应该报错"。
func generateErrorsForTest(t *testing.T, src string) (string, []string) {
	t.Helper()
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	cCode := cg.Generate(prog)
	return cCode, cg.Errors()
}

func TestOversizedLocalArrayIsRejected(t *testing.T) {
	src := "fn main() {\n" +
		"    [100000]i64 hs\n" +
		"    hs[0] = 1\n" +
		"}\n"

	cCode, errs := generateErrorsForTest(t, src)

	if len(errs) == 0 {
		t.Fatalf("100000 个 i64 的局部数组（800 KB）应被编译期拒绝；" +
			"实际编译通过，运行时必然栈溢出。生成的 C 首行：%s", firstLineOf(cCode))
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "hs") {
		t.Errorf("诊断应点名变量 hs，实际：%s", joined)
	}
	if !strings.Contains(joined, "栈") {
		t.Errorf("诊断应说明是栈问题，实际：%s", joined)
	}
}

// 小数组必须照常生成 —— 阈值不是用来把所有数组都拒掉的
func TestSmallLocalArrayStillAllowed(t *testing.T) {
	src := "fn main() {\n" +
		"    [16]i64 buf\n" +
		"    buf[0] = 1\n" +
		"}\n"

	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("16 个 i64 的局部数组应正常编译，实际报错：%v", errs)
	}
	if !strings.Contains(cCode, "buf[16]") && !strings.Contains(cCode, "buf[ 16 ]") {
		t.Errorf("应生成定长栈数组声明，实际 C：\n%s", firstLineOf(cCode))
	}
}

// 长度是运行期变量时算不出字节数，不能误报 —— 否则会拒掉大量正常代码
func TestRuntimeSizedArrayNotRejected(t *testing.T) {
	src := "fn main() -> i32 {\n" +
		"    i64 n = 8\n" +
		"    [n]i64 hs\n" +
		"    hs[0] = 1\n" +
		"    return 0\n" +
		"}\n"

	_, errs := generateErrorsForTest(t, src)

	for _, e := range errs {
		if strings.Contains(e, "hs") && strings.Contains(e, "栈") {
			t.Errorf("运行期长度的数组无法在编译期算出字节数，不应被拒：%s", e)
		}
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
