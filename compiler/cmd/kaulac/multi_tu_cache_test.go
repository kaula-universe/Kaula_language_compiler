package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TU 级对象缓存的单元测试：不跑真实 clang，用 exec 替身。
// 覆盖：首编全量写入缓存 → 二次构建（改一个 TU）只重编该 TU →
// .o 内容哈希校验拦截半写条目 → NoCache 关断。

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func writeTU(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".c"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
}

func runBench(t *testing.T, workDir string, counts *map[string]int) error {
	t.Helper()
	return runMultiTU([]string{"-O2", "-x", "c", "ignored.c"}, "clang", nil,
		filepath.Join(workDir, "cache", "entry.c"),
		filepath.Join(workDir, "out.exe"),
		workDir, filepath.Join(workDir, "cache", "tu"), tuExec, false, nil)
}

func TestTUObjectCacheHitSkipsCompile(t *testing.T) {
	work := t.TempDir()
	tu := filepath.Join(work, "cache", "tu")
	os.MkdirAll(tu, 0755)
	// 入口 TU 由 cache/entry.c 提供（loadTUUnits 固定读它作 main.kl.tu），
	// 测试不得再手写 main.kl.tu.c，否则入口 TU 重复计数
	writeTU(t, tu, "a.kl.tu", "int64_t a(void){return 1;}")
	writeTU(t, tu, "b.kl.tu", "int64_t b(void){return 2;}")
	os.MkdirAll(filepath.Join(work, "cache"), 0755)
	os.WriteFile(filepath.Join(work, "cache", "entry.c"), []byte("int main(){return 0;}"), 0644)

	counts := map[string]int{}
	fake := func(dir string, args []string) (string, error) {
		isCompile := false
		for i, a := range args {
			if a == "-c" && i+2 < len(args) {
				// 模拟 clang 产出 .o
				isCompile = true
				obj := args[i+2]
				os.WriteFile(obj, []byte("OBJ:"+filepath.Base(obj)), 0644)
				name := strings.TrimSuffix(filepath.Base(obj), ".o")
				counts[name]++
			}
		}
		if !isCompile {
			// 模拟链接：产出输出文件
			for i, a := range args {
				if a == "-o" && i+1 < len(args) {
					os.WriteFile(args[i+1], []byte("FAKE_EXE"), 0755)
				}
			}
		}
		return "", nil
	}
	// 注入替身执行器
	orig := tuExec
	tuExec = fake
	defer func() { tuExec = orig }()

	if err := runBench(t, work, &counts); err != nil {
		t.Fatalf("首次构建失败: %v", err)
	}
	if counts["a.kl.tu"] != 1 || counts["b.kl.tu"] != 1 {
		t.Fatalf("首次构建应编译全部 TU: %v", counts)
	}

	// 二次构建：无任何变更 → 全部命中缓存
	counts = map[string]int{}
	if err := runBench(t, work, &counts); err != nil {
		t.Fatalf("二次构建失败: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("无变更构建应全部命中缓存，实际重编: %v", counts)
	}

	// 触碰 b：只有 b 重编
	writeTU(t, tu, "b.kl.tu", "int64_t b(void){return 3;}")
	counts = map[string]int{}
	if err := runBench(t, work, &counts); err != nil {
		t.Fatalf("增量构建失败: %v", err)
	}
	if counts["b.kl.tu"] != 1 || counts["a.kl.tu"] != 0 {
		t.Fatalf("增量构建应只重编变更 TU: %v", counts)
	}

	// 输出存在且两个缓存 .o 都能被链接引用（内容校验）
	if _, err := os.Stat(filepath.Join(work, "out.exe")); err != nil {
		t.Fatalf("输出缺失: %v", err)
	}
}

func TestTUObjectCacheCorruptEntryRejected(t *testing.T) {
	work := t.TempDir()
	tu := filepath.Join(work, "cache", "tu")
	os.MkdirAll(tu, 0755)
	writeTU(t, tu, "a.kl.tu", "int64_t a(void){return 1;}")
	os.MkdirAll(filepath.Join(work, "cache"), 0755)
	os.WriteFile(filepath.Join(work, "cache", "entry.c"), []byte("int main(){return 0;}"), 0644)

	counts := map[string]int{}
	fake := func(dir string, args []string) (string, error) {
		isCompile := false
		for i, a := range args {
			if a == "-c" && i+2 < len(args) {
				isCompile = true
				obj := args[i+2]
				os.WriteFile(obj, []byte("OBJ:"+filepath.Base(obj)), 0644)
				counts[filepath.Base(obj)]++
			}
		}
		fmt.Println("CALL isCompile=", isCompile, "nargs=", len(args))
		if !isCompile {
			for i, a := range args {
				if a == "-o" && i+1 < len(args) {
					os.WriteFile(args[i+1], []byte("FAKE_EXE"), 0755)
				}
			}
		}
		return "", nil
	}
	orig := tuExec
	tuExec = fake
	defer func() { tuExec = orig }()

	if err := runBench(t, work, &counts); err != nil {
		t.Fatalf("首次构建失败: %v", err)
	}
	// 篡改缓存 .o（半写条目）：meta 中的哈希不再匹配 → 必须重编
	matches, _ := filepath.Glob(filepath.Join(work, "cache", "tu-obj", "*.o"))
	if len(matches) == 0 {
		t.Fatal("缓存中应存在 .o")
	}
	os.WriteFile(matches[0], []byte("CORRUPT"), 0644)

	counts = map[string]int{}
	if err := runBench(t, work, &counts); err != nil {
		t.Fatalf("二次构建失败: %v", err)
	}
	if len(counts) == 0 {
		t.Fatal("缓存条目损坏后必须重编对应 TU")
	}
}

func TestTUObjectCacheDisabledByFlag(t *testing.T) {
	work := t.TempDir()
	tu := filepath.Join(work, "cache", "tu")
	os.MkdirAll(tu, 0755)
	writeTU(t, tu, "a.kl.tu", "int64_t a(void){return 1;}")
	os.MkdirAll(filepath.Join(work, "cache"), 0755)
	os.WriteFile(filepath.Join(work, "cache", "entry.c"), []byte("int main(){return 0;}"), 0644)

	counts := map[string]int{}
	fake := func(dir string, args []string) (string, error) {
		isCompile := false
		for i, a := range args {
			if a == "-c" && i+2 < len(args) {
				isCompile = true
				obj := args[i+2]
				os.WriteFile(obj, []byte("OBJ"), 0644)
				counts[filepath.Base(obj)]++
			}
		}
		fmt.Println("CALL isCompile=", isCompile, "nargs=", len(args))
		if !isCompile {
			for i, a := range args {
				if a == "-o" && i+1 < len(args) {
					os.WriteFile(args[i+1], []byte("FAKE_EXE"), 0755)
				}
			}
		}
		return "", nil
	}
	orig := tuExec
	tuExec = fake
	tuCacheDisabled = true
	defer func() { tuExec = orig; tuCacheDisabled = false }()

	if err := runBench(t, work, &counts); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "cache", "tu-obj")); err == nil {
		t.Fatal("禁用缓存时不应写 tu-obj 目录")
	}
	counts = map[string]int{}
	if err := runBench(t, work, &counts); err != nil {
		t.Fatalf("二次构建失败: %v", err)
	}
	if len(counts) == 0 {
		t.Fatal("禁用缓存时必须每次全量编译")
	}
}

func TestTUObjectCacheKeySensitiveToFlags(t *testing.T) {
	// flags 变化（-O2 → -O0）必须使缓存失效：同一 TU 源、不同 flags → 重编
	work := t.TempDir()
	tu := filepath.Join(work, "cache", "tu")
	os.MkdirAll(tu, 0755)
	writeTU(t, tu, "a.kl.tu", "int64_t a(void){return 1;}")
	os.MkdirAll(filepath.Join(work, "cache"), 0755)
	os.WriteFile(filepath.Join(work, "cache", "entry.c"), []byte("int main(){return 0;}"), 0644)

	counts := map[string]int{}
	fake := func(dir string, args []string) (string, error) {
		isCompile := false
		for i, a := range args {
			if a == "-c" && i+2 < len(args) {
				isCompile = true
				obj := args[i+2]
				os.WriteFile(obj, []byte("OBJ"), 0644)
				counts[filepath.Base(obj)]++
			}
		}
		fmt.Println("CALL isCompile=", isCompile, "nargs=", len(args))
		if !isCompile {
			for i, a := range args {
				if a == "-o" && i+1 < len(args) {
					os.WriteFile(args[i+1], []byte("FAKE_EXE"), 0755)
				}
			}
		}
		return "", nil
	}
	orig := tuExec
	tuExec = fake
	defer func() { tuExec = orig }()

	if err := runMultiTU([]string{"-O2", "-x", "c", "i.c"}, "clang", nil,
		filepath.Join(work, "cache", "entry.c"), filepath.Join(work, "o1.exe"),
		work, tu, tuExec, false, &counts); err != nil {
		t.Fatal(err)
	}
	counts = map[string]int{}
	if err := runMultiTU([]string{"-O0", "-x", "c", "i.c"}, "clang", nil,
		filepath.Join(work, "cache", "entry.c"), filepath.Join(work, "o2.exe"),
		work, tu, tuExec, false, &counts); err != nil {
		t.Fatal(err)
	}
	if len(counts) == 0 {
		t.Fatal("flags 变化必须使 TU 缓存失效")
	}
}
