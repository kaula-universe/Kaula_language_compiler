package sema

import (
	"strings"
	"testing"
)

// 裸机（--freestanding / --wasm）没有托管 std 运行时，`object` 全局动态对象声明
// 生成的代码依赖 dynobj_create / dynobj_set / dynobj_get / dynobj_box_*
// （实现于 std/obj/dynobj.c，建立在 std_malloc 与 StringObject 之上），
// freestanding 库里一个都没有。
//
// 修复前这个缺口一路放行到链接期才炸 `undefined symbol: dynobj_create`，
// 用户看到的是一个和自己写的代码毫无关系的 C 链接错误。本测试锁定修复后的
// 行为：sema 阶段就给出点明原因的诊断，不再依赖链接期暴露。
func TestObjectStatementRejectedInFreestanding(t *testing.T) {
	src := "object Config {\n" +
		"    name: \"kaula\"\n" +
		"    port: 8080\n" +
		"}\n\n" +
		"fn main() {}\n"
	prog := parseForSema(t, src)

	sa := NewSemanticAnalyzer()
	sa.SetFreestanding(true)
	sa.Analyze(prog)

	errs := sa.ErrorCollector().Errors()
	if len(errs) == 0 {
		t.Fatalf("裸机下 object 语句必须报语义错误，实际 0 个错误 —— 缺口又漏回链接期了")
	}

	joined := ""
	for _, e := range errs {
		joined += e.Message + "\n"
	}
	if !strings.Contains(joined, "object") {
		t.Errorf("诊断消息必须点出 object，实际: %s", joined)
	}
	if !strings.Contains(joined, "裸机") && !strings.Contains(joined, "freestanding") {
		t.Errorf("诊断消息必须说明是裸机模式限制，实际: %s", joined)
	}
}

// 托管编译不受影响：同一份源码在非 freestanding 下必须零错误，
// 否则这次修复就把自己变成新的回归。
func TestObjectStatementAllowedInHosted(t *testing.T) {
	src := "object Config {\n" +
		"    name: \"kaula\"\n" +
		"    port: 8080\n" +
		"}\n\n" +
		"fn main() {}\n"
	prog := parseForSema(t, src)

	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	if errs := sa.ErrorCollector().Errors(); len(errs) > 0 {
		for _, e := range errs {
			t.Logf("sema error %d:%d %s", e.Line, e.Column, e.Message)
		}
		t.Fatalf("托管模式下 object 语句不应报错，实际 %d 个错误", len(errs))
	}
}

// 默认值必须是托管：漏接 --freestanding 接线时，宁可少报也不能误报。
func TestFreestandingDefaultsToHosted(t *testing.T) {
	src := "object Config {\n    port: 8080\n}\n\nfn main() {}\n"
	prog := parseForSema(t, src)

	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)
	if errs := sa.ErrorCollector().Errors(); len(errs) > 0 {
		t.Errorf("未调用 SetFreestanding 时不应按裸机处理，实际 %d 个错误", len(errs))
	}
}
