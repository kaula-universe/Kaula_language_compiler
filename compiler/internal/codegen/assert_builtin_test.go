package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
)

// F12 回归：#[test] 必须能真正失败。
// 语言此前没有任何内置方式让断言失败被 runner 记录——
// 文档推荐的 if+println 写法测试永远 ok、kaulac test 永远 exit 0。
// assert(cond)：测试模式绑定 __kaula_test_assert 失败记录。
func TestAssertBuiltinBoundToTestRunner(t *testing.T) {
	src := `
#[test]
fn t_math() {
    i64 a = 1
    assert(a == 2)
}

fn main() { println("x") }
`
	prog := parse(src)
	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	cg.TestMode = true
	out := cg.Generate(prog)
	// 必须出现在测试函数体内的调用点（runner 自身定义含同名串，不能只查前缀）
	if !strings.Contains(out, "__kaula_test_assert((int)(a == 2)") {
		t.Errorf("assert(cond) in test mode must call __kaula_test_assert at site, generated:\n%s", out)
	}
}

// 普通模式 assert 失败必须终止进程（非零退出），不得静默。
func TestAssertBuiltinNormalModeFailsLoud(t *testing.T) {
	src := `
fn main() {
    i64 a = 1
    assert(a == 2)
}
`
	prog := parse(src)
	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	out := cg.Generate(prog)
	// 失败路径走 kaula_assert_failed（双环境：托管 exit(1)，裸机弱钩子 + trap），
	// 不能在生成代码里直接写 fprintf/exit —— 裸机下二者都不存在。
	if !strings.Contains(out, "kaula_assert_failed") {
		t.Errorf("normal-mode assert must abort via kaula_assert_failed, generated:\n%s", out)
	}
}
