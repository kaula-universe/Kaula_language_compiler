package codegen

import (
	"strings"
	"testing"
)

/*
 * 表达式位置的 await 只有在能确定结果类型时才安全。
 *
 * kaula_handle_await 返回的是「结果所在内存的地址」，指向 Task 的
 * Frame 字段。Frame 在任务完成时就随任务 retire，地址有效期只到
 * 「epoch 判定没人再读」为止 —— 所以解引用它会安静地返回已回收
 * Frame 里残留的旧值，不崩，只是答案错。
 *
 * generateAwaitExpression 的类型推断原本只认 *ast.Identifier。
 * `hs[i].await`（Handle 数组元素）、`obj.h.await`（字段）、
 * `f().await`（调用结果）都不是 Identifier，于是全部落到兜底分支 ——
 * 而那个兜底只是往生成的 C 里塞一句注释：
 *
 *     ((int64_t)kaula_handle_await(hs[i]) /* 错误：请改为声明式 ... *​/)
 *
 * 注释不阻止编译。于是这份代码**编得过、跑得动、答案错**。
 *
 * 实测（test/concurrency/test_vla_stack_probe.kl，n=10，期望 45）：
 *
 *     for i in range(n) { i64 r = hs[i].await; acc = acc + r }   →  45  正确
 *     for i in range(n) { acc += hs[i].await }                  →  98125744  垃圾
 *
 * 判据：
 *  1. Handle 数组元素 `hs[i].await` 必须走 await_into（能推出版本）
 *  2. 结果类型确实推不出来时必须是**编译错误**，不能只留注释
 */

// Handle 数组元素上的表达式位置 await：应推出元素的结果类型，走 await_into
func TestAwaitOnHandleArrayElementUsesAwaitInto(t *testing.T) {
	src := `
import std.time

conc fn noop(i64 x) -> i64 { return x }

fn main() -> i32 {
    i64 n = 8
    [n]Handle hs
    for i in range(n) {
        hs[i] = spawn noop(i)
    }
    i64 acc = 0
    for i in range(n) {
        acc += hs[i].await
    }
    return 0
}
`
	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	if strings.Contains(cCode, "kaula_handle_await(") {
		t.Errorf("Handle 数组元素的 await 必须走 kaula_handle_await_into —— "+
			"kaula_handle_await 返回的是已 retire Frame 里的地址，解引用它返回垃圾值")
	}
	if !strings.Contains(cCode, "kaula_handle_await_into(hs[i], &__t") {
		t.Errorf("应生成 kaula_handle_await_into(hs[i], &__t, sizeof(...))：\n%s", firstLineOf(cCode))
	}
}

// 结果类型真的推不出来时：必须是编译错误，不能只往 C 里塞注释
func TestAwaitWithUnknownResultTypeIsCompileError(t *testing.T) {
	// 经由函数调用返回的 Handle：编译器无从知道它的结果类型
	src := `
conc fn noop(i64 x) -> i64 { return x }

fn make() -> Handle { return spawn noop(1) }

fn main() -> i32 {
    i64 acc = 0
    acc += make().await
    return 0
}
`
	_, errs := generateErrorsForTest(t, src)

	if len(errs) == 0 {
		t.Fatalf("结果类型未知时表达式位置 await 必须是编译错误；" +
			"实际编译通过 —— 生成的 C 会解引用一个已 retire Frame 的地址，答案错但不崩")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "await") {
		t.Errorf("诊断应点明是 await 的问题，实际：%s", joined)
	}
}

// 回归：普通标识符上的表达式位置 await 仍然可用
func TestAwaitOnPlainIdentifierStillWorks(t *testing.T) {
	src := `
conc fn noop(i64 x) -> i64 { return x }

fn main() -> i32 {
    Handle h = spawn noop(7)
    i64 acc = 0
    acc += h.await
    return 0
}
`
	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("标识符上的 await 应正常编译：%v", errs)
	}
	if strings.Contains(cCode, "kaula_handle_await(") {
		t.Errorf("标识符上的 await 应走 await_into：\n%s", firstLineOf(cCode))
	}
}

/*
 * 容器不是标量句柄：不得对它发 handle_release。
 *
 * handleResultTypes 兼任两张表的职责：
 *   - 表达式位置 await 查它推结果类型
 *   - isHandleVar 查它判断"这个局部量本身是 Handle"，据此在作用域出口
 *     生成 kaula_handle_release(name)
 *
 * 于是给数组登记结果类型时若图省事写进同一张表，作用域出口就会生成
 *
 *     kaula_handle_release(hs);   // hs 是 Handle[100000]
 *
 * C 里数组会退化成 KaulaHandle*，与形参类型 KaulaHandle 不符 ——
 * 编译直接失败（passing 'KaulaHandle *' to parameter of incompatible
 * type 'KaulaHandle'）。
 */
func TestHandleArrayNotTreatedAsScalarHandleForRelease(t *testing.T) {
	src := `
import std.time

conc fn noop(i64 x) -> i64 { return x }

fn main() -> i32 {
    i64 n = 8
    [n]Handle hs
    for i in range(n) {
        hs[i] = spawn noop(i)
    }
    i64 acc = 0
    for i in range(n) {
        i64 r = hs[i].await
        acc = acc + r
    }
    return 0
}
`
	cCode, errs := generateErrorsForTest(t, src)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	if strings.Contains(cCode, "kaula_handle_release(hs)") {
		t.Errorf("hs 是数组不是标量句柄，作用域出口不得对它发 handle_release"+
			"（C 里数组退化成指针，与 KaulaHandle 形参不符，编译会失败）：\n%s",
			firstLineOf(cCode))
	}
	// 标量句柄仍要发 —— 由 TestConcHandleReleaseAtScopeExit 覆盖
}
