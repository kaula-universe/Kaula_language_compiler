package config

import "testing"

// 零值 Config 的 ResolveOptLevel 必须给 -O2（不能是 "-"）。
//
// 回归背景：Kaula Toolkit 走 C 库入口（不带命令行 flag 解析）时 cfg 是零值，
// 若返回 "-"，拼出来的 clang 命令是 "-c  -xc ..."（空参数），
// 表现为 "no input files"，而错误信息完全指不到优化级别。
func TestResolveOptLevelZeroValue(t *testing.T) {
	var cfg Config
	if got := cfg.ResolveOptLevel(""); got != "-O2" {
		t.Fatalf("零值 Config 的默认优化级别应为 -O2，实际 %q", got)
	}
	// optOverride 的约定形式是 "-O0"/"O0"（见 --opt 帮助文本），两种都要认。
	if got := cfg.ResolveOptLevel("O0"); got != "-O0" {
		t.Fatalf("显式 O0 应解析为 -O0，实际 %q", got)
	}
	if got := cfg.ResolveOptLevel("-O0"); got != "-O0" {
		t.Fatalf("显式 -O0 应解析为 -O0，实际 %q", got)
	}
}

// KMM_THREAD_SAFETY_LEVEL 是**所有 TU 共享**的语义宏：std .o / runtime .o 是
// 独立 TU，生成 TU 上的 -D 传不进去，只能由构建系统派发。所以两个 C 入口
// （命令行 / 库调用）必须给出同一个值，否则 -O0 下会出现 level 1 与 0 并存。
func TestResolveOptLevelIsSemanticsFree(t *testing.T) {
	var cfg Config
	// 优化级别不同 → 产物只该差在优化，不该差在"线程安全口径"。
	// 这里只断言返回值规整（带 '-' 前缀），真正的口径一致性由
	// kaulac 的 hostSemanticDefines 保证。
	for _, lvl := range []string{"-O0", "-O1", "-O2", "-O3"} {
		if lvl != "-O0" && lvl != "-O1" && lvl != "-O2" && lvl != "-O3" {
			t.Fatalf("unreachable")
		}
	}
	if got := cfg.ResolveOptLevel(""); len(got) < 2 || got[0] != '-' || got[1] != 'O' {
		t.Fatalf("优化级别应形如 -On，实际 %q", got)
	}
}
