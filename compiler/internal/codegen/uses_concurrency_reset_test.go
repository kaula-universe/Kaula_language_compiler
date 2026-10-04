package codegen

import (
	"strings"
	"testing"
)

/*
 * codeUsesConcurrency 是包级全局，且此前**从不复位**。
 *
 * codegen 据它决定要不要 #include "concurrency/kaula_conc.h"。一旦某个
 * TU 用过 conc fn，这个标记就永远为真 —— 同一个 kaulac 进程里之后编译的
 * 每个文件都会被塞进内核头。
 *
 * 后果正是那段注释自己担心的：
 *
 *   // 仅当程序里真的出现 conc fn 时才包含 —— 不用 spawn 的程序
 *   // 链接器会把这整个归档丢弃（设计说明书 §2.7 推论二）
 *
 * 裸机与嵌入式目标连头文件本身都拿不到，于是「先编一个带 conc 的文件，
 * 再编一个裸机文件」就会让后者编译失败 —— 而它自己一行 conc 都没有。
 *
 * 判据：一次 Generate 结束后的标记只反映**本次**编译。
 */

// 同一进程内先编带 conc 的文件，再编纯标量文件：后者不该带内核头
func TestUsesConcurrencyDoesNotLeakAcrossCompilations(t *testing.T) {
	concSrc := "import std.time\n" +
		"conc fn slow(i64 ms) -> i64 {\n" +
		"    std.time.time_sleep(ms)\n" +
		"    return 1\n" +
		"}\n" +
		"fn main() -> i32 {\n" +
		"    return 0\n" +
		"}\n"

	if _, errs := generateErrorsForTest(t, concSrc); len(errs) != 0 {
		t.Fatalf("第一个文件不该有诊断错误：%v", errs)
	}
	if !UsesConcurrency() {
		t.Fatalf("含 conc fn 的文件应标记 UsesConcurrency")
	}

	plainSrc := "fn main() -> i32 {\n" +
		"    [8]i64 buf\n" +
		"    return 0\n" +
		"}\n"

	cCode, errs := generateErrorsForTest(t, plainSrc)
	if len(errs) != 0 {
		t.Fatalf("第二个文件不该有诊断错误：%v", errs)
	}
	if UsesConcurrency() {
		t.Errorf("UsesConcurrency 是上一次编译留下的残留 —— "+
			"它必须在每次 Generate 开头复位，否则同进程内后续每个 TU "+
			"都会被塞进并发内核头（裸机目标会直接编不过）")
	}
	if strings.Contains(cCode, "kaula_conc.h") {
		t.Errorf("不含 conc fn 的文件不该 #include \"concurrency/kaula_conc.h\"：\n%s",
			firstLineOf(cCode))
	}
}

// 反向：含 Handle 数组（不 spawn）的文件也必须标记
func TestUsesConcurrencySetByHandleArrayDecl(t *testing.T) {
	plainSrc := "fn main() -> i32 {\n" +
		"    [8]i64 buf\n" +
		"    return 0\n" +
		"}\n"
	if _, errs := generateErrorsForTest(t, plainSrc); len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}

	handleSrc := "fn main() -> i32 {\n" +
		"    [8]Handle hs\n" +
		"    return 0\n" +
		"}\n"
	cCode, errs := generateErrorsForTest(t, handleSrc)
	if len(errs) != 0 {
		t.Fatalf("不该有诊断错误：%v", errs)
	}
	if !UsesConcurrency() {
		t.Errorf("声明 Handle 数组就该标记 UsesConcurrency（KaulaHandle 由内核头定义）")
	}
	if !strings.Contains(cCode, "kaula_conc.h") {
		t.Errorf("声明 Handle 数组的 TU 必须引入并发内核头")
	}
}
