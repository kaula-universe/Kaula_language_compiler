package main

import (
	"fmt"
	"strings"
	"testing"
)

// 链接命令行里出现裸 `-`（stdin 占位符）会让 clang 直接报
// "error: -E or -x required when input is from standard input"。
//
// 这个函数的判定曾经靠"下一个参数不以 - 开头"，而 `-` 自己就以 - 开头，
// 于是入口输入被当成选项漏到链接侧。凡是"-x <lang> <input>"形状的序列，
// 三个 token 必须一起消失。
func TestLinkFlagsForLinkDropsEntryInput(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "stdin 占位符",
			in:   []string{"-x", "c", "-", "-o", "out", "-O2", "-pipe"},
			want: []string{"-O2", "-pipe"},
		},
		{
			name: "真实源路径",
			in:   []string{"-x", "c", "a.c", "-O2", "-o", "out"},
			want: []string{"-O2"},
		},
		{
			// 真实形态：-include-pch 插在输入**之前**，-x c 紧邻其输入
			name: "预编译头只对编译有效",
			in:   []string{"-x", "c", "-include-pch", "/tmp/k.h.gch", "-", "-o", "out", "-O2"},
			want: []string{"-O2"},
		},
		{
			// -x 后紧跟的是选项而非输入时，后面的 `-` 不属于 -x 的输入，
			// 但它同样是入口输入，必须被识别出来。
			name: "选项夹在 -x 与输入之间",
			in:   []string{"-x", "c", "-include-pch", "/tmp/k.h.gch", "-"},
			want: []string{},
		},
		{
			name: "合并写法的 -include-pch=",
			in:   []string{"-include-pch=/tmp/k.h.gch", "-O2"},
			want: []string{"-O2"},
		},
	}
	for _, c := range cases {
		got := linkFlagsForLink(c.in)
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: linkFlagsForLink(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

// splitLinkInputs 必须把入口输入从 compileFlags 里摘干净：链接侧只拿
// `-x none` 之后的真输入，编译侧只拿 flags。入口描述单独返回。
func TestSplitLinkInputsSeparatesEntryInput(t *testing.T) {
	args := []string{"-x", "c", "-", "-o", "prog", "-O2", "-x", "none", "-lm", "lib.a"}
	linkInputs, entryInput, compileFlags := splitLinkInputs(args)

	if got := strings.Join(linkInputs, " "); got != "-lm lib.a" {
		t.Errorf("linkInputs = %q", got)
	}
	if got := strings.Join(entryInput, " "); got != "-x c -" {
		t.Errorf("entryInput = %q", got)
	}
	if got := strings.Join(compileFlags, " "); got != "-o prog -O2" {
		t.Errorf("compileFlags = %q", got)
	}
	// compileFlags 里绝不能残留输入占位符
	for _, f := range compileFlags {
		if f == "-" {
			t.Fatalf("compileFlags 残留 stdin 占位符: %v", compileFlags)
		}
	}
}

// 没有 `-x none` 时退化为"链接只加对象文件"，但入口输入仍必须被摘出来。
func TestSplitLinkInputsWithoutNone(t *testing.T) {
	linkInputs, entryInput, compileFlags := splitLinkInputs([]string{"-x", "c", "-", "-O2"})
	if len(linkInputs) != 0 {
		t.Errorf("linkInputs 应为空, got %v", linkInputs)
	}
	if got := strings.Join(entryInput, " "); got != "-x c -" {
		t.Errorf("entryInput = %q", got)
	}
	if got := strings.Join(compileFlags, " "); got != "-O2" {
		t.Errorf("compileFlags = %q", got)
	}
}

// 每个 TU 的编译参数必须：去掉 -o <exe>、末尾追加自己的源与输出对象，
// 且 `-x c` 紧邻源路径（clang 的 -x 位置敏感）。
func TestCompileArgsForTUPutsSourceLast(t *testing.T) {
	flags := []string{"-o", "prog", "-O2", "-I", "/inc"}
	got := compileArgsForTU(flags, "/tu/a.c", "/tu/a.o")
	want := "-O2 -I /inc -x c /tu/a.c -c -o /tu/a.o"
	if strings.Join(got, " ") != want {
		t.Fatalf("compileArgsForTU = %q, want %q", strings.Join(got, " "), want)
	}
	for i, a := range got {
		if a == "prog" {
			t.Fatalf("残留 -o 的值: %v", got)
		}
		if a == "-x" && (i+1 >= len(got) || got[i+1] != "c") {
			t.Fatalf("-x 后必须是 c: %v", got)
		}
	}
}

// 并行度解析只接受纯十进制；"4x" 之类不能被静默接受成 4。
func TestParsePositiveInt(t *testing.T) {
	for _, ok := range []string{"1", "16", "128"} {
		if _, err := parsePositiveInt(ok); err != nil {
			t.Errorf("parsePositiveInt(%q) 应成功: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "0x10", "4x", "-1", " 4", "99999999999999"} {
		if _, err := parsePositiveInt(bad); err == nil {
			t.Errorf("parsePositiveInt(%q) 应失败", bad)
		}
	}
}

// 并行度不得超过上限：CPU 再多，收益也被 clang 启动开销与峰值内存吃掉。
func TestTUWorkersCapped(t *testing.T) {
	if got := tuWorkers(100000); got > tuMaxJobs {
		t.Fatalf("tuWorkers 超过上限 %d: %d", tuMaxJobs, got)
	}
	if got := tuWorkers(1); got != 1 {
		t.Fatalf("单元数少于 worker 时应等于单元数, got %d", got)
	}
}

// 失败信息必须一次报全、且与完成顺序无关（按名字排序）。
func TestFormatTUFailuresSorted(t *testing.T) {
	failures := []tuFailure{
		{Name: "z.tu", Detail: "boom-z"},
		{Name: "a.tu", Detail: "boom-a"},
	}
	err := formatTUFailures(3, failures)
	if err == nil {
		t.Fatal("有失败时必须返回错误")
	}
	msg := err.Error()
	ai := strings.Index(msg, "a.tu")
	zi := strings.Index(msg, "z.tu")
	if ai < 0 || zi < 0 || ai > zi {
		t.Fatalf("失败信息未按名字排序: %s", msg)
	}
	if !strings.Contains(msg, fmt.Sprintf("%d", 3)) {
		t.Errorf("失败信息应包含 TU 总数: %s", msg)
	}
}
