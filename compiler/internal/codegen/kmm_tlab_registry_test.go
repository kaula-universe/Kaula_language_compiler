package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// findRuntimeTestCC 返回可用于编译 allocator C 测试的编译器。
// CI/Linux 走 PATH；Windows 开发机常见 clang 安装在 D:\LLVM。
func findRuntimeTestCC(t *testing.T) string {
	t.Helper()
	if cc, err := exec.LookPath("clang"); err == nil {
		return cc
	}
	for _, p := range []string{`D:\LLVM\bin\clang.exe`, `C:\Program Files\LLVM\bin\clang.exe`} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	t.Skip("clang unavailable")
	return ""
}

const tlabRegistryHarness = `
#include "kmm_scoped_allocator_v4.h"
#include <stdint.h>
#include <stdio.h>

static int failures = 0;
#define CHECK(cond, msg) do { \
    if (!(cond)) { fprintf(stderr, "FAIL: %s\n", msg); failures++; } \
} while (0)

int main(void) {
    kmm_v4_reset();

    /* 1) 首个 TLAB A + 活跃 mark */
    unsigned char* p = (unsigned char*)kmm_v4_alloc_auto(64);
    CHECK(p != NULL, "alloc first");
    unsigned char* baseA = g_kmm_v4_thread_heap.base;
    kmm_v4_mark_t m = kmm_v4_mark_save();
    CHECK(m.base == baseA, "mark captures base A");

    /* 2) 填满 A，再分配触发 refill 到 B；A 进入 suspended */
    size_t remainA = g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset;
    CHECK(kmm_v4_alloc_auto(remainA) != NULL, "fill A");
    CHECK(kmm_v4_alloc_auto(64) != NULL, "alloc triggers refill");
    unsigned char* baseB = g_kmm_v4_thread_heap.base;
    CHECK(baseB != baseA, "refill switches to B");
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 1, "A suspended after refill");

    /* 3) 跨 base 回卷：不抢占当前 B；A 只收缩存活水位 */
    size_t global_after_B = (size_t)KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    kmm_v4_mark_restore(m);
    CHECK(g_kmm_v4_thread_heap.base == baseB, "restore keeps current B");
    CHECK(g_kmm_v4_thread_heap.offset == 0, "restore reclaims B prefix");
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 1, "A remains suspended");
    CHECK(g_kmm_v4_tlab_registry.free_count == 0, "A prefix still alive -> not freed");

    /* 4) 回收 A 的存活前缀后（offset=0）整块退役；当前 B 仍原地复用 */
    kmm_v4_tlab_restore_to(baseA, 0);
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 0, "A retired");
    CHECK(g_kmm_v4_tlab_registry.free_count == 1, "A in free_list");
    CHECK(g_kmm_v4_thread_heap.base == baseB, "current B unchanged by retire");

    /* 5) 下一次 refill 复用 A，不推进全局 offset */
    CHECK(kmm_v4_alloc_auto(64) != NULL, "alloc in B");
    remainA = g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset;
    CHECK(kmm_v4_alloc_auto(remainA) != NULL, "fill B");
    CHECK(kmm_v4_alloc_auto(64) != NULL, "refill from free_list");
    CHECK(g_kmm_v4_thread_heap.base == baseA, "reuses retired A");
    CHECK((size_t)KMM_ATOMIC_LOAD(g_kmm_v4_offset) == global_after_B,
          "free_list reuse does not grow global offset");
    CHECK(g_kmm_v4_tlab_registry.free_count == 0, "free_list consumed");

    /* 6) scope_push/pop 共享同一恢复路径：保持当前 C，A 收缩后继续挂起 */
    size_t remainA2 = g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset;
    CHECK(kmm_v4_alloc_auto(remainA2) != NULL, "fill A again");
    kmm_v4_scope_push();
    CHECK(kmm_v4_alloc_auto(64) != NULL, "scope alloc refills to C");
    unsigned char* baseC = g_kmm_v4_thread_heap.base;
    CHECK(baseC != baseA, "scope refill switches to C");
    kmm_v4_scope_pop();
    CHECK(g_kmm_v4_thread_heap.base == baseC, "scope_pop keeps current C");
    CHECK(g_kmm_v4_thread_heap.offset == 0, "scope_pop reclaims C prefix");
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 2, "A and older B stay suspended after scope_pop");

    /* 7) 目标未命中：安全回退到当前 TLAB offset=0 */
    unsigned char* fake = (unsigned char*)(uintptr_t)0x100000;
    kmm_v4_tlab_restore_to(fake, 0);
    CHECK(g_kmm_v4_thread_heap.base == baseC, "unknown target keeps current base");
    CHECK(g_kmm_v4_thread_heap.offset == 0, "unknown target falls back to offset 0");

    /* 8) base==NULL：suspended 全部退役，当前 TLAB 保留并从 0 复用 */
    keep_alive(baseC);
    CHECK(kmm_v4_alloc_auto(64) != NULL, "alloc before null restore");
    size_t free_before_null = g_kmm_v4_tlab_registry.free_count;
    size_t susp_before_null = g_kmm_v4_tlab_registry.suspended_count;
    size_t cap_before_null = g_kmm_v4_thread_heap.capacity;
    size_t global_before_null = (size_t)KMM_ATOMIC_LOAD(g_kmm_v4_offset);
    kmm_v4_tlab_restore_to(NULL, 0);
    CHECK(g_kmm_v4_thread_heap.base == baseC, "null restore keeps current TLAB");
    CHECK(g_kmm_v4_thread_heap.capacity == cap_before_null, "null restore keeps capacity");
    CHECK(g_kmm_v4_thread_heap.offset == 0, "null restore rewinds offset");
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 0, "null restore retires suspended");
    CHECK(g_kmm_v4_tlab_registry.free_count == free_before_null + susp_before_null,
          "null restore moves suspended to free_list");

    /* 9) 回卷后原地复用当前 TLAB，不推进全局 offset */
    unsigned char* again = (unsigned char*)kmm_v4_alloc_auto(128);
    CHECK(again != NULL, "alloc after null restore");
    CHECK(g_kmm_v4_thread_heap.base == baseC, "null restore reuses current TLAB");
    CHECK((size_t)KMM_ATOMIC_LOAD(g_kmm_v4_offset) == global_before_null,
          "null restore does not grow global offset");
    again[0] = 1;
    again[127] = 2;
    CHECK(again[0] == 1 && again[127] == 2, "payload writable after null restore");

    /* 10) 回卷到更老目标时，seq 更新的 suspended 条目整体退役 */
    unsigned char* baseD = g_kmm_v4_thread_heap.base;
    kmm_v4_mark_t md = kmm_v4_mark_save();
    size_t remainD = g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset;
    CHECK(kmm_v4_alloc_auto(remainD) != NULL, "fill D");
    CHECK(kmm_v4_alloc_auto(64) != NULL, "refill D -> E");
    CHECK(g_kmm_v4_thread_heap.base != baseD, "E allocated");
    size_t remainE = g_kmm_v4_thread_heap.capacity - g_kmm_v4_thread_heap.offset;
    CHECK(kmm_v4_alloc_auto(remainE) != NULL, "fill E");
    CHECK(kmm_v4_alloc_auto(64) != NULL, "refill E -> F");
    unsigned char* baseF = g_kmm_v4_thread_heap.base;
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 2, "D and E suspended");
    size_t free_before = g_kmm_v4_tlab_registry.free_count;
    kmm_v4_tlab_restore_to(baseD, md.offset);
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 1, "D kept with live prefix");
    CHECK(g_kmm_v4_tlab_registry.free_count == free_before + 1, "newer E retired");
    CHECK(g_kmm_v4_thread_heap.base == baseF, "current F still reused");
    CHECK(g_kmm_v4_thread_heap.offset == 0, "current F prefix reclaimed");
    kmm_v4_tlab_restore_to(baseD, 0);
    CHECK(g_kmm_v4_tlab_registry.suspended_count == 0, "D retired at offset 0");
    CHECK(g_kmm_v4_tlab_registry.free_count == free_before + 2, "D enters free_list");

    if (failures != 0) {
        fprintf(stderr, "%d TLAB registry check(s) failed\n", failures);
        return 1;
    }
    printf("TLAB registry: all checks passed\n");
    return 0;
}
`

// TestTLABRegistryReclaimsRegions 编译并运行 allocator C 级测试，验证：
//   - refill 换下的 TLAB 进 suspended[]，跨 base 回卷能重新激活；
//   - 回卷整体死亡的 TLAB 进 free_list[]，下一次 refill 原地复用且不推进
//     全局 offset；
//   - scope_pop 与 mark_restore 共享同一恢复路径；
//   - 目标未命中回退到 offset=0；base==NULL 保留当前 TLAB 并退役 suspended；
//   - 元数据只在 TLS 登记表，payload 不写 header，回卷后仍可写。
func TestTLABRegistryReclaimsRegions(t *testing.T) {
	cc := findRuntimeTestCC(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(root, "src")
	runtimeC := filepath.Join(srcDir, "kmm_scoped_allocator_v4.c")
	if st, err := os.Stat(runtimeC); err != nil || st.IsDir() {
		t.Fatalf("runtime source missing: %v", err)
	}

	dir := t.TempDir()
	cFile := filepath.Join(dir, "tlab_registry_test.c")
	exe := filepath.Join(dir, "tlab_registry_test.exe")
	// keep_alive 阻止优化器把 baseB 相关逻辑折叠掉，并证明 payload 可写。
	harness := strings.Replace(tlabRegistryHarness,
		"int main(void) {",
		"static void keep_alive(void* p) { if (p) ((unsigned char*)p)[0] = 0; }\nint main(void) {", 1)
	if err := os.WriteFile(cFile, []byte(harness), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-std=gnu11", "-O2", "-Wno-deprecated-declarations",
		"-I", srcDir, cFile, runtimeC, "-o", exe,
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("C compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(exe).CombinedOutput(); err != nil {
		t.Fatalf("C execution: %v\n%s", err, out)
	}
}
