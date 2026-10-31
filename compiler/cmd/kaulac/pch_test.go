package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---- 家族判定（纯函数） ----

func TestClassifyCCVersionOutput(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want ccFamily
		ok   bool
	}{
		{"gcc ubuntu", "gcc (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0\nCopyright (C) 2023 Free Software Foundation, Inc.", ccFamilyGCC, true},
		{"gcc plain", "gcc version 12.2.0 (GCC)", ccFamilyGCC, true},
		{"gcc only fsf", "Free Software Foundation compiler 11.4", ccFamilyGCC, true},
		{"clang ubuntu", "Ubuntu clang version 18.1.3 (1ubuntu1)", ccFamilyClang, true},
		{"clang plain", "clang version 17.0.6", ccFamilyClang, true},
		{"apple clang", "Apple clang version 15.0.0 (clang-1500.3.9.4)", ccFamilyClang, true},
		{"unknown", "tcc 0.9.27", ccFamilyClang, false},
		{"empty", "", ccFamilyClang, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := classifyCCVersionOutput(c.out)
			if ok != c.ok || got != c.want {
				t.Fatalf("classifyCCVersionOutput(%q) = (%v, %v), want (%v, %v)", c.out, got, ok, c.want, c.ok)
			}
		})
	}
}

// ---- 家族探测（假编译器，避免依赖宿主机装了什么） ----

// writeFakeCC 写一个可执行的假编译器：--version 打印 versionOut。
func writeFakeCC(t *testing.T, dir, name, versionOut string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake compiler 用 sh 脚本，Windows 下跳过（真实编译器用例另行覆盖）")
	}
	p := filepath.Join(dir, name)
	script := "#!/bin/sh\ncat <<'EOF'\n" + versionOut + "\nEOF\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cc %s: %v", p, err)
	}
	return p
}

func TestDetectCCFamilyFakeCompilers(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name       string
		binary     string
		versionOut string
		want       ccFamily
	}{
		{"gcc 名字+输出", "gcc", "gcc (Debian 12.2.0-14) 12.2.0", ccFamilyGCC},
		{"交叉 gcc 名字", "x86_64-linux-gnu-gcc-12", "gcc version 12.2.0", ccFamilyGCC},
		{"cc 名字靠 --version", "cc", "cc (GCC) 11.4.0", ccFamilyGCC},
		{"clang 名字+输出", "clang", "clang version 18.1.8", ccFamilyClang},
		{"clang-18", "clang-18", "Ubuntu clang version 18.1.3", ccFamilyClang},
		// clang2gcc 是"吃 clang 命令行、翻译给 gcc"的翻译器：输出带 gcc，
		// 但参数语法是 clang 侧的，必须按 clang 判（名字捷径优先于探测）。
		{"clang2gcc 翻译器按 clang 判", "clang2gcc", "gcc (translator wrapper) 13.2.0", ccFamilyClang},
		// --version 失败时按名字兜底。
		{"无输出按名字兜底 gcc", "gcc-nooutput", "", ccFamilyGCC},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cc := writeFakeCC(t, dir, c.binary, c.versionOut)
			if c.versionOut == "" {
				// 让 --version 失败：chmod 000 不可靠（root 照样读），
				// 改成让脚本以非零退出——exec 看 err，不看输出。
				_ = os.WriteFile(cc, []byte("#!/bin/sh\nexit 3\n"), 0o755)
			}
			if got := detectCCFamily(cc); got != c.want {
				t.Fatalf("detectCCFamily(%s) = %v, want %v", c.binary, got, c.want)
			}
		})
	}
}

// 探测结果必须进程内缓存：每次构建多起一个 --version 子进程是可避免的
// 固定税（多 TU 路径虽只探一次，库调用/格式化等路径会重复进来）。
func TestDetectCCFamilyCachesResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		// 本用例用 `#!/bin/sh` 脚本伪造编译器来数探测次数：Windows 无 sh，
		// cmd 会把脚本当成批处理执行并失败，detectCCFamily 回退到 clang，
		// 与被测的缓存行为无关。缓存语义由 Linux/macOS CI 覆盖。
		t.Skip("fake #!/bin/sh compiler is POSIX-only; cache logic is covered on Linux/macOS")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "probe.count")
	cc := filepath.Join(dir, "countcc")
	script := "#!/bin/sh\necho x >> " + counter + "\necho \"gcc (Debian) 12.2.0\"\n"
	if err := os.WriteFile(cc, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cc: %v", err)
	}
	for i := 0; i < 3; i++ {
		if got := detectCCFamily(cc); got != ccFamilyGCC {
			t.Fatalf("detectCCFamily = %v, want gcc", got)
		}
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if n := strings.Count(string(data), "x"); n != 1 {
		t.Fatalf("--version 被调用了 %d 次，缓存失效（应为 1 次）", n)
	}
}

// 宿主机真实编译器（存在时才验）：fake 之外的一道端到端确认。
func TestDetectCCFamilyRealCompilers(t *testing.T) {
	for _, name := range []string{"gcc", "clang", "cc"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		fam := detectCCFamily(p)
		if fam != ccFamilyClang && fam != ccFamilyGCC {
			t.Fatalf("detectCCFamily(%s) 返回了非法家族 %v", name, fam)
		}
		// gcc/clang 的名字与输出必须自洽：名字带 clang 的不可能是 gcc 家族。
		if strings.Contains(strings.ToLower(filepath.Base(p)), "clang") && fam != ccFamilyClang {
			t.Fatalf("%s 名字带 clang，却探成 %v", p, fam)
		}
	}
}

// ---- 消费侧参数 ----

func TestPchConsumeFlags(t *testing.T) {
	gch := filepath.Join("cache", "std-objects", "host-x86_64-O2-abcdef", "kaula.h.gch")

	clang := pchConsumeFlags(ccFamilyClang, gch)
	if len(clang) != 2 || clang[0] != "-include-pch" || clang[1] != gch {
		t.Fatalf("clang 侧应显式 -include-pch 点名 .gch，得到 %v", clang)
	}

	gcc := pchConsumeFlags(ccFamilyGCC, gch)
	if len(gcc) != 2 || gcc[0] != "-I" || gcc[1] != filepath.Dir(gch) {
		t.Fatalf("gcc 侧应把 .gch 所在目录用 -I 传给搜索路径，得到 %v", gcc)
	}
	// gcc 的 -I 必须指向 .gch 旁边（GCC 的 PCH 查找是 <dir>/<name>.h.gch）
	if !strings.HasSuffix(gcc[1], filepath.Join("std-objects", "host-x86_64-O2-abcdef")) {
		t.Fatalf("gcc 的 -I 指错了目录: %v", gcc)
	}
}

// ---- 判活指纹 ----

// pchStampTestTree 搭一套最小的 PCH 源头树：kaula.h + 嵌套的 std/io/io.h。
// 嵌套那层是重点——kaula.h 里有 #include "../std/io/io.h"，旧判据只扫
// src/ 顶层 .h，改它不会判死。
func pchStampTestTree(t *testing.T) (roots []string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	std := filepath.Join(t.TempDir(), "std")
	write := func(p, content string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	write(filepath.Join(src, "kaula.h"), "#include \"../std/io/io.h\"\nint kaula;\n")
	write(filepath.Join(std, "io", "io.h"), "void println(const char*);\n")
	write(filepath.Join(std, "memory", "memory.h"), "void* kmm_alloc(unsigned long);\n")
	return []string{src, std}
}

func TestPchStampDetectsCompilerSwitch(t *testing.T) {
	roots := pchStampTestTree(t)
	in := pchStampInputs{optLevel: "-O2", semanticArgs: []string{"-fwrapv"}, headerRoots: roots}

	clangIn := in
	clangIn.cc, clangIn.family, clangIn.versionKey = "/usr/bin/clang", ccFamilyClang, "ver-clang18"
	gccIn := in
	gccIn.cc, gccIn.family, gccIn.versionKey = "/usr/bin/gcc", ccFamilyGCC, "ver-gcc13"

	clangStamp := pchStampValue(clangIn)
	if clangStamp == pchStampValue(gccIn) {
		t.Fatal("不同编译器必须得到不同指纹：clang 的 .gch 被 gcc 放行后，clang 再吃它是硬错误")
	}
	// 同族不同路径也要分开（升级/换编译器是常事）
	other := clangIn
	other.cc = "/usr/bin/clang-18"
	if pchStampValue(other) == clangStamp {
		t.Error("编译器路径必须参与指纹")
	}
	// 同路径升级版本也要分开（AST 格式不兼容，clang 侧是硬错误）
	newer := clangIn
	newer.versionKey = "ver-clang19"
	if pchStampValue(newer) == clangStamp {
		t.Error("编译器版本指纹必须参与判活")
	}
	// 同一组输入必须稳定
	if again := pchStampValue(clangIn); again != clangStamp {
		t.Fatal("同一组输入必须得到同一指纹（否则每次全量重编）")
	}
}

func TestPchStampDetectsNestedHeaderChange(t *testing.T) {
	roots := pchStampTestTree(t)
	src, std := roots[0], roots[1]
	in := pchStampInputs{cc: "/usr/bin/clang", family: ccFamilyClang, optLevel: "-O2",
		semanticArgs: []string{"-fwrapv"}, headerRoots: roots}

	base := pchStampValue(in)

	// 改 src/kaula.h → 必须变
	if err := os.WriteFile(filepath.Join(src, "kaula.h"), []byte("#include \"../std/io/io.h\"\nint kaula2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pchStampValue(in) == base {
		t.Error("kaula.h 内容变化必须改变指纹")
	}

	// 改嵌套的 std/io/io.h → 必须变（旧 mtime 扫描只覆盖 src/ 顶层，这里漏判）
	nested := filepath.Join(std, "io", "io.h")
	if err := os.WriteFile(nested, []byte("void println(const char*);\nvoid print_int(int);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pchStampValue(in) == base {
		t.Error("嵌套 std/io/io.h 变化必须改变指纹（kaula.h 经 #include 冻结了它）")
	}
}

// mtime 变、内容不变：指纹也必须变。
//
// 这不是过度敏感——clang 用 -include-pch 时会核对 kaula.h 的 mtime 与
// .gch 里记录的是否一致，bare touch / git checkout / rsync 都会改 mtime，
// 不一致就是致命错误（"has been modified since the precompiled header
// was built: mtime changed"）。纯内容指纹对此失明：内容没变 → 放行 →
// clang 硬失败。所以 mtime 必须进指纹，让这类变化触发重编而不是复用。
// （gcc 反向安全：.gch 旧于 .h 时它只是跳过，但重编同样正确。）
func TestPchStampDetectsMtimeTouch(t *testing.T) {
	roots := pchStampTestTree(t)
	in := pchStampInputs{cc: "/usr/bin/clang", family: ccFamilyClang, optLevel: "-O2",
		semanticArgs: []string{"-fwrapv"}, headerRoots: roots}

	before := pchStampValue(in)
	// 只推 mtime，不碰内容
	future := time.Now().Add(48 * time.Hour)
	for _, root := range roots {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				_ = os.Chtimes(p, future, future)
			}
			return nil
		})
	}
	touched := pchStampValue(in)
	if touched == before {
		t.Fatal("mtime 变化必须改变指纹：否则 clang 的 -include-pch mtime 硬检查会把构建打挂")
	}
	// 再改内容 → 又不同（mtime 与内容两条路径独立生效）
	if err := os.WriteFile(filepath.Join(roots[0], "kaula.h"), []byte("int kaula3;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := pchStampValue(in); again == before || again == touched {
		t.Error("内容变化必须独立于 mtime 改变指纹")
	}
}

func TestPchStampDetectsSemanticChanges(t *testing.T) {
	roots := pchStampTestTree(t)
	in := pchStampInputs{cc: "/usr/bin/clang", family: ccFamilyClang, optLevel: "-O2",
		semanticArgs: []string{"-fwrapv"}, headerRoots: roots}
	base := pchStampValue(in)

	changed := in
	changed.semanticArgs = []string{"-fwrapv", "-fno-strict-aliasing"}
	if pchStampValue(changed) == base {
		t.Error("语义参数变化必须改变指纹（clang 把 -fwrapv 状态写进 .gch）")
	}
	opt := in
	opt.optLevel = "-O0"
	if pchStampValue(opt) == base {
		t.Error("优化级别必须参与指纹（决定 __OPTIMIZE__ → KMM_V4_OPT_LEVEL）")
	}
	fam := in
	fam.family = ccFamilyGCC
	if pchStampValue(fam) == base {
		t.Error("家族必须参与指纹")
	}
}

// 根路径拼写差异不能造成误判：workDir 相对根与 KAULA_HOME 根指向同一批
// 文件时（测试运行器每个用例一个 outdir，两种拼写会交替出现），指纹必须一致。
func TestPchHeadersFingerprintStableAcrossRootSpelling(t *testing.T) {
	roots := pchStampTestTree(t)
	src := roots[0]

	single := pchHeadersFingerprint([]string{src})
	// 前面插一个不存在的根、再重复一次 src：文件集合不变，指纹必须不变
	duped := pchHeadersFingerprint([]string{filepath.Join(t.TempDir(), "nonexistent"), src, src})
	if single != duped {
		t.Fatal("根路径拼写/重复/不存在不应影响指纹（否则每个 outdir 都全量重编 PCH）")
	}
}

// 首个根命中（与预处理器搜索顺序一致）：同名 .h 在多个根下内容不同时，
// 靠前的根胜出，靠后的不改变指纹。
func TestPchHeadersFingerprintFirstRootWins(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "x.h"), []byte("int a;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "x.h"), []byte("int bbbbbb;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := pchHeadersFingerprint([]string{dirA})
	firstWins := pchHeadersFingerprint([]string{dirA, dirB})
	if only != firstWins {
		t.Fatal("靠前根的内容应胜出，靠后根不应改变指纹")
	}
	// 反序则相反（证明"顺序"确实被建模了，而不是碰巧相等）
	if pchHeadersFingerprint([]string{dirB, dirA}) == only {
		t.Fatal("换序后指纹应反映新的胜出者")
	}
}

func TestPchStampSidecar(t *testing.T) {
	dir := t.TempDir()
	gch := filepath.Join(dir, "kaula.h.gch")
	// 造一份"clang 格式"的 .gch
	if err := os.WriteFile(gch, []byte("CPCH\x00clang-ast-bits"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pchStampMatches(gch, "abc") {
		t.Fatal("没有 sidecar 时不应匹配")
	}
	writePchStamp(gch, "abc")
	if !pchStampMatches(gch, "abc") {
		t.Fatal("写入后应匹配")
	}
	if pchStampMatches(gch, "abd") {
		t.Fatal("指纹不同不应匹配")
	}
	// sidecar 路径必须紧贴 .gch（与 .o 的 .stamp 同一传统）
	if pchStampPath(gch) != gch+".stamp" {
		t.Fatalf("stamp 路径错: %s", pchStampPath(gch))
	}
	// 残留的 .stamp.tmp 不允许出现（写必须原子、留最终名）
	if _, err := os.Stat(gch + ".stamp.tmp"); err == nil {
		t.Fatal("writePchStamp 必须 rename 到位，不应留下 .stamp.tmp")
	}
}

// 并发交错场景：stamp 的输入指纹与 A 进程一致，但磁盘上的 .gch 已被 B 进程
// 换成另一格式（A rename → B rename → B 写 stamp → A 写 stamp 的交错结果）。
// 此时必须判失配重编——否则 clang 会拿 gcc 格式的 .gch 硬失败。
func TestPchStampDetectsGchSwap(t *testing.T) {
	dir := t.TempDir()
	gch := filepath.Join(dir, "kaula.h.gch")
	if err := os.WriteFile(gch, []byte("CPCH\x00clang-ast-bits"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePchStamp(gch, "fp-clang")
	if !pchStampMatches(gch, "fp-clang") {
		t.Fatal("前提：刚写的 stamp 必须匹配")
	}
	// B 进程把 .gch 换成 gcc 格式（rename 覆盖，mtime 刷新）
	if err := os.WriteFile(gch, []byte("gpch\x00gcc-ast-bits-different-length"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pchStampMatches(gch, "fp-clang") {
		t.Fatal(".gch 被换后必须失配：否则 clang 吃 gcc 格式 .gch 直接硬失败")
	}
	// 手动删 .gch 只留 stamp：同样必须失配（重建而非复用空气）
	if err := os.Remove(gch); err != nil {
		t.Fatal(err)
	}
	if pchStampMatches(gch, "fp-clang") {
		t.Fatal(".gch 不存在时必须失配")
	}
}

// 回归（PCH 指纹挂死）：freestanding 搜索路径的 parent 上溯链末端可能是文件
// 系统根 "/"。一旦把它喂给 WalkDir 全量递归，会遍历整盘（含 /proc、/sys 与
// 海量宿主文件），表现为编译卡在 PCH 指纹阶段、CPU 极低且长时间无产物。
// 护栏必须直接跳过 "/"；根目录本就不含 Kaula 库头，跳过不改变指纹。
func TestPchHeadersFingerprintSkipsFilesystemRoot(t *testing.T) {
	roots := pchStampTestTree(t)
	base := pchHeadersFingerprint(roots)

	// 用 goroutine + 超时兜底：护栏若失效，WalkDir("/") 会挂住，测试必须在此
	// 暴露为失败而不是把整个 go test 卡住。
	withRootCh := make(chan string, 1)
	go func() {
		withRootCh <- pchHeadersFingerprint(append(append([]string{}, roots...), "/"))
	}()
	select {
	case withRoot := <-withRootCh:
		if withRoot != base {
			t.Fatal("文件系统根 / 不含 Kaula 头，加入它不应改变指纹")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("指纹遍历到了文件系统根 /（护栏失效），5s 内未返回")
	}

	if got := pchHeadersFingerprint(append(append([]string{}, roots...), "/")); got != base {
		t.Fatal("显式传入文件系统根 / 不应改变指纹")
	}
}
