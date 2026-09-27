package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ============================================================================
// C 工具链子进程的临时目录保障与失败诊断
//
// 背景：clang 在编译时需要在临时目录创建中间文件（IR、汇编、PCH 等）。
// 当 %TEMP%/%TMP% 不可写（受限账号、只读家目录、沙箱）时，clang 报
//   clang: error: unable to make temporary file: permission denied
// 且不给出任何定位信息——无法判断是临时目录、输出目录还是参数问题。
//
// 这里做两件事：
//  1. compilerCommand：显式解析一个**真实探测过可写性**的临时目录并注入子进程
//     环境（TMPDIR/TEMP/TMP 三个键全设，兼容各平台 clang 的查找顺序），
//     彻底消除"落到不可写默认目录"这一类失败。
//  2. formatCommandFailure：失败时打印完整命令行、工作目录、临时目录与 stderr，
//     让同类问题一次定位。
// ============================================================================

// kaulaCacheDir 缓存目录（main() 中赋值），作为临时目录的最后回退：
// 与中间产物同盘同权限，避免落到不可写的系统目录。
var kaulaCacheDir string

var (
	resolvedTempDirOnce sync.Once
	resolvedTempDir     string
)

// tempDirWritable 真实写入探测：目录可创建临时文件才算可用
func tempDirWritable(dir string) bool {
	if dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, "kaula_probe_*.tmp")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// resolveWritableTempDir 解析一个当前进程可写的临时目录。
// 顺序：TMPDIR → TEMP → TMP → <缓存目录>/tmp。
// 全部不可用时返回 ""，此时不覆盖子进程环境，沿用平台默认行为。
func resolveWritableTempDir() string {
	var candidates []string
	for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
		if v := os.Getenv(key); v != "" {
			candidates = append(candidates, v)
		}
	}
	if kaulaCacheDir != "" {
		candidates = append(candidates, filepath.Join(kaulaCacheDir, "tmp"))
	}
	for _, dir := range candidates {
		if tempDirWritable(dir) {
			return dir
		}
	}
	return ""
}

func compilerTempDir() string {
	resolvedTempDirOnce.Do(func() {
		resolvedTempDir = resolveWritableTempDir()
	})
	return resolvedTempDir
}

// compilerEnv 构造 C 工具链子进程的环境变量。
// nil 表示"完全继承当前环境"（找不到可写临时目录时）。
func compilerEnv() []string {
	dir := compilerTempDir()
	if dir == "" {
		return nil
	}
	env := os.Environ()
	// 三个键全设：不同平台 clang 查找临时目录的顺序不同（TMP/TEMP/TMPDIR）
	for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
		env = append(env, key+"="+dir)
	}
	return env
}

// compilerCommand 创建 C 工具链子进程命令（clang/ld/ar 等），注入可写临时目录。
// 统一走这个构造函数，避免某条路径漏掉环境注入。
func compilerCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = compilerEnv()
	return cmd
}

// formatCommandFailure 生成可诊断的失败信息：完整命令行 + 工作目录 + 临时目录 + stderr
func formatCommandFailure(stage string, cmd *exec.Cmd, err error, output []byte) string {
	var b strings.Builder
	if stage != "" {
		b.WriteString(stage)
		b.WriteString(": ")
	}
	if err != nil {
		b.WriteString(err.Error())
	}
	if cmd != nil {
		if cmd.Dir != "" {
			fmt.Fprintf(&b, "\n  cwd:  %s", cmd.Dir)
		} else {
			fmt.Fprintf(&b, "\n  cwd:  %s (inherited)", mustGetwd())
		}
		fmt.Fprintf(&b, "\n  tmp:  %s", tempDirDescription())
		if len(cmd.Args) > 0 {
			fmt.Fprintf(&b, "\n  cmd:  %s", strings.Join(cmd.Args, " "))
		}
	}
	if out := strings.TrimSpace(string(output)); out != "" {
		fmt.Fprintf(&b, "\n  output:\n%s", out)
	}
	return b.String()
}

func tempDirDescription() string {
	if dir := compilerTempDir(); dir != "" {
		return dir + " (verified writable)"
	}
	return "<none resolved, compiler default>"
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "<unavailable>"
	}
	return dir
}
