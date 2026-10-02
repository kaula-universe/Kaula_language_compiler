package codegen

import (
	"strings"
	"testing"
)

// 表达式位置的 await（`h1.await + h2.await`）曾降级成
//
//	*(int64_t*)kaula_handle_await(h1)
//
// 而 kaula_handle_await 返回的是「结果所在内存的地址」——那个地址指向 Task 的
// Frame 字段，Frame 在任务完成时随任务 retire（见 kaula_conc_task.c:302）。
// 于是解引用的是一个随时悬垂的地址：表现为安静地返回一个已回收 Frame 里
// 残留的旧值，不崩，只是答案错。实测 `h1.await + h2.await + h3.await` 曾读出
// 0 和 7568452288656 这类垃圾。
//
// 正确做法与声明位置一致：走 kaula_handle_await_into 把结果 memcpy 到一个
// 大小明确、类型正确、且确定存活到表达式结束的临时变量里。
func TestAwaitExpression_UsesAwaitIntoNotDanglingDeref(t *testing.T) {
	const src = `
conc fn compute_sum(i64 a, i64 b) -> i64 {
    return a + b
}

fn main() -> i32 {
    Handle h1 = spawn compute_sum(1, 1)
    Handle h2 = spawn compute_sum(2, 2)
    i64 total = h1.await + h2.await
    return 0
}
`
	c, _ := genWithNewCG(t, src)

	// 回归点：表达式位置曾经生成悬垂解引用。
	if strings.Contains(c, "*)kaula_handle_await(") {
		t.Error("表达式位置仍在解引用 kaula_handle_await 返回的裸指针（悬垂）")
	}
	if n := strings.Count(c, "kaula_handle_await("); n != 0 {
		t.Errorf("不应再调用 kaula_handle_await，实测 %d 处", n)
	}
	// 两个 await 都在表达式位置，各自拷进一个临时变量
	if n := strings.Count(c, "kaula_handle_await_into("); n != 2 {
		t.Errorf("期望 2 处 await_into（两个表达式位置 await），实测 %d 处", n)
	}
	// 声明位置不受本次改动影响，一并守住
	if !strings.Contains(c, "kaula_handle_await_into(h1, &__t") {
		t.Error("表达式位置的 h1.await 应走 await_into")
	}
}

// await 结果类型未知时不能瞎拷：必须显式暴露问题，而不是静默给一个
// 可能悬垂的地址。
func TestAwaitExpression_UnknownTypeNotSilentlyDeref(t *testing.T) {
	const src = `
conc fn worker(i64 a) -> i64 {
    return a
}

fn main() -> i32 {
    Handle h = spawn worker(1)
    i64 x = h.await
    return x
}
`
	c, _ := genWithNewCG(t, src)

	if strings.Contains(c, "*)kaula_handle_await(") {
		t.Error("不应解引用 kaula_handle_await 的裸指针")
	}
	if !strings.Contains(c, "kaula_handle_await_into(") {
		t.Error("已知类型时应走 kaula_handle_await_into")
	}
}