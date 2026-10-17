package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这组测试锁死三点，都是"两套 KMM 口径"问题的回归点：
//   1. 生成 TU 与各预编译 TU 必须拿到同一组语义宏（-D 不会自动继承）；
//   2. .o 缓存判据必须包含宏集，否则改宏后复用旧产物，二进制口径不变；
//   3. PCH 侧与生成 TU 侧共用的语义 CFlags（-fwrapv 等）不能漂移。

func TestHostSemanticDefinesPinsThreadSafety(t *testing.T) {
	found := false
	for _, d := range hostSemanticDefines {
		if d == "-DKMM_THREAD_SAFETY_LEVEL=1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hostSemanticDefines 必须包含 -DKMM_THREAD_SAFETY_LEVEL=1，实际为 %v", hostSemanticDefines)
	}
}

func TestModuleStampChangesWithDefines(t *testing.T) {
	src := writeTemp(t, "m.c", "int x;\n")
	base := moduleObjectStamp("clang", nil, "-O0", src, hostSemanticDefines, nil, false)

	// 去掉宏 → 这就是修复前 std .o 的实际情形（-O0 下推导出 level 0）
	without := moduleObjectStamp("clang", nil, "-O0", src, nil, nil, false)
	if base == without {
		t.Fatal("宏集变化必须改变 .o 指纹，否则会跨宏集复用旧产物")
	}

	// 用户 -D / CFlags / 优化级别 同样必须改变指纹
	if base == moduleObjectStamp("clang", nil, "-O0", src, append(append([]string{}, hostSemanticDefines...), "-DFOO=1"), nil, false) {
		t.Error("用户 -D 必须参与指纹")
	}
	if base == moduleObjectStamp("clang", nil, "-O0", src, hostSemanticDefines, []string{"-fno-builtin"}, false) {
		t.Error("CFlags 必须参与指纹")
	}
	if base == moduleObjectStamp("clang", nil, "-O2", src, hostSemanticDefines, nil, false) {
		t.Error("优化级别必须参与指纹")
	}
	if base == moduleObjectStamp("clang", nil, "-O0", src, hostSemanticDefines, nil, true) {
		t.Error("裸机静态池必须参与指纹")
	}
}

// 交叉编译目标（-target / -mcmodel / 架构默认特性）必须参与指纹。
//
// 这是补测的一个真实漏洞：hosted 与 freestanding 的 .o 落在不同缓存目录，
// 但同一 freestanding 目录下 x86_64-none-elf 与 aarch64-none-elf 若共用路径，
// 指纹又不含 -target，就会把 x86_64 的 .o 拿去链 aarch64 的 ELF ——
// 表现为 "xxx.o is incompatible with elf64-littleaarch64"，单独重跑又正常。
func TestModuleStampChangesWithArchArgs(t *testing.T) {
	src := writeTemp(t, "m.c", "int x;\n")
	x86 := moduleObjectStamp("clang", nil, "-O2", src, hostSemanticDefines, nil, true,
		"-target", "x86_64-none-elf")
	arm := moduleObjectStamp("clang", nil, "-O2", src, hostSemanticDefines, nil, true,
		"-target", "aarch64-none-elf")
	if x86 == arm {
		t.Fatal("交叉编译目标必须参与指纹，否则会跨架构复用 .o")
	}

	// 同一目标必须稳定
	again := moduleObjectStamp("clang", nil, "-O2", src, hostSemanticDefines, nil, true,
		"-target", "x86_64-none-elf")
	if x86 != again {
		t.Fatal("同一组架构参数必须得到同一指纹")
	}

	// 不加架构参数（托管路径）不能与加了架构参数的指纹撞车
	if moduleObjectStamp("clang", nil, "-O2", src, hostSemanticDefines, nil, true) == x86 {
		t.Error("空架构参数与带架构参数的指纹必须不同")
	}
}

// runtime .o（KMM 分配器）走同一套判据，单独锁一次，
// 因为它是 -O0 宏口径分叉最直接的受害者。
func TestRuntimeStampChangesWithArchArgs(t *testing.T) {
	src := writeTemp(t, "kmm.c", "int y;\n")
	a := runtimeObjectStamp("clang", nil, "-O0", src, hostSemanticDefines, nil, true, "-target", "x86_64-none-elf")
	b := runtimeObjectStamp("clang", nil, "-O0", src, hostSemanticDefines, nil, true, "-target", "riscv64-none-elf")
	if a == b {
		t.Fatal("runtime .o 的交叉编译目标必须参与指纹")
	}
}

func TestModuleStampStableAcrossCalls(t *testing.T) {
	src := writeTemp(t, "m.c", "int x;\n")
	a := moduleObjectStamp("clang", []string{"-pipe"}, "-O2", src, hostSemanticDefines, nil, false)
	b := moduleObjectStamp("clang", []string{"-pipe"}, "-O2", src, hostSemanticDefines, nil, false)
	if a != b {
		t.Fatalf("同一组输入必须得到同一指纹（否则每次全量重编）: %s != %s", a, b)
	}
}

func TestModuleStampChangesWithSourceContent(t *testing.T) {
	a := moduleObjectStamp("clang", nil, "-O0", writeTemp(t, "a.c", "int x;\n"), hostSemanticDefines, nil, false)
	b := moduleObjectStamp("clang", nil, "-O0", writeTemp(t, "b.c", "int x = 1;\n"), hostSemanticDefines, nil, false)
	if a == b {
		t.Fatal("源码内容变化必须改变指纹（mtime 不可靠：checkout/容器会重置时间）")
	}
}

func TestObjectStampSidecar(t *testing.T) {
	obj := filepath.Join(t.TempDir(), "x.o")
	if objectStampMatches(obj, "abc") {
		t.Fatal("没有 sidecar 时不应匹配")
	}
	writeObjectStamp(obj, "abc")
	if !objectStampMatches(obj, "abc") {
		t.Fatal("写入后应匹配")
	}
	if objectStampMatches(obj, "abd") {
		t.Fatal("指纹不同不应匹配")
	}
}

// PCH 生成侧与生成 TU 编译侧必须引用**同一个**语义 CFlags 来源。
//
// clang 把 -fwrapv 的状态写进 .gch，两侧不一致就直接报
// signed integer overflow handling differs in PCH file vs. current file。
// 本次 rebase 真的漏过一次（生成侧漏了 -fwrapv），表现是 114 个用例全红。
// 这个测试锁住"两处都从 tuSemanticCFlags 取值"的事实：
// 只要有人把某一侧改回字面量或改掉常量内容，就会失败。
func TestTUSemanticCFlagsSharedByPCHAndTU(t *testing.T) {
	want := map[string]bool{"-fwrapv": true, "-fno-strict-aliasing": true}
	for _, f := range tuSemanticCFlags {
		if !want[f] {
			t.Fatalf("tuSemanticCFlags 出现意外项 %q（改动它会同时改变 PCH 与生成 TU 的语义）", f)
		}
		delete(want, f)
	}
	if len(want) != 0 {
		t.Fatalf("tuSemanticCFlags 缺少 %v：clang 会拒绝复用 PCH", want)
	}

	// 源码侧复查：不允许再出现把 -fwrapv 写死的字面量。
	// 这条比断言切片内容更硬——它防的是"某处绕过常量又写了一遍"。
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Skipf("读不到 main.go，跳过源码复查: %v", err)
	}
	if n := strings.Count(string(src), `"-fwrapv", "-fno-strict-aliasing"`); n != 1 {
		t.Fatalf("main.go 里 -fwrapv 字面量出现 %d 次，应当只在 tuSemanticCFlags 定义处出现 1 次", n)
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}
