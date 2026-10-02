package codegen

import (
	"strings"
	"testing"
)

// TestConcHandleReleaseAtScopeExit 验证编译器在作用域出口发射 kaula_handle_release。
//
// 回归背景：pin 是在 spawn 返回那一刻拿的（挡的是「任务已完成并 retire，
// 而调用方还没来得及 await」那个窗口），而归还只发生在 await_into 里。
// 于是「句柄被丢弃且从不 await」时那个 pin 永远不还，g_ext_pins 一直 > 0，
// 回收器一律判定不可回收 —— 内存留到 shutdown。
//
// 断言点在「发射了 release」，不在「什么时候释放」：漏发是保守退化（泄漏），
// 早发才是危险（悬垂）。所以这里只要求作用域出口必须有。
func TestConcHandleReleaseAtScopeExit(t *testing.T) {
	src := `
conc fn work(i64 a) -> i64 {
    return a + 1
}

fn main() -> i32 {
    Handle h = spawn work(1)
    i64 r = h.await
    std.io.println("r = ", r)
    return 0
}
`
	c, errs := genConc(t, src)
	for _, e := range errs {
		t.Errorf("不应报错，实际：%s", e)
	}
	if !strings.Contains(c, "kaula_handle_release(h);") {
		t.Errorf("作用域出口必须发射 kaula_handle_release：\n%s", c)
	}
}

// 形参是借用（所有权在调用方），不应在函数内归还
func TestConcHandleRelease_SkipsParameters(t *testing.T) {
	src := `
conc fn work(i64 a) -> i64 {
    return a + 1
}

fn relay(Handle h) -> i64 {
    i64 v = h.await
    return v
}

fn main() -> i32 {
    return 0
}
`
	c, _ := genConc(t, src)
	if strings.Contains(c, "kaula_handle_release(h);") {
		t.Errorf("形参是借用，不得在函数内归还它的 pin：\n%s", c)
	}
}

// 没有 Handle 局部量的函数不应凭空发射
func TestConcHandleRelease_NoHandlesNoEmit(t *testing.T) {
	src := `
fn plain(i64 a) -> i64 {
    i64 b = a + 1
    return b
}

fn main() -> i32 {
    return 0
}
`
	c, _ := genConc(t, src)
	if strings.Contains(c, "kaula_handle_release") {
		t.Errorf("没有 Handle 时不应发射 release：\n%s", c)
	}
}
