package codegen

import (
	"regexp"
	"strings"
	"testing"
)

// 本文件守护 KBUG_KMM_FREE_NOOP_SURVIVOR_LEAK。
//
// 症状：Kaula CONC 服务在长时压测下 RSS 按请求线性增长（实测 ~194 B/req），
// 且停载后不回落。Go 对照零泄漏。slab 区（195MB）在 20s 内耗尽后降级页式
// malloc，RSS 持续膨胀 —— 直接决定 CONC 能否抗住亿级请求。
//
// 根因（三处 codegen 缺陷，共同作用）：
//
//  1. **kmm_v4_free 是 no-op**：runtime `void kmm_v4_free(void* p){ (void)p; }`
//     （src/kmm_scoped_allocator_v4.c「修复 #6：free 为 no-op，bump allocator
//     靠 scope 回收」）。survivor 段对象（kmm_v4_alloc_global/promote）**只能**
//     靠 kmm_v4_survivor_free 回收。
//
//  2. **用户 free 被误认为已回收**：declaration 收尾时若 IsUserFreed(name)，
//     直接跳过 per-object survivor_free。对 survivor 对象而言这等于永久泄漏。
//
//  3. **concat 守卫降级发射死分配**：conc 守卫路径把 `T* x = <init>` 拆成
//     「声明 + 槽内赋值」，并强制用 Value=nil 复用声明生成以取类型文本。
//     Value=nil + 指针类型会无条件发射
//     `T* x = (T*)kmm_v4_alloc_global(sizeof(T));`，紧接着被恢复副本
//     `x = f->x;` 覆盖 —— 分配结果永久丢失，每次 resume（= 每次请求）泄漏一槽。
//
// 修复（都落在 codegen）：
//   a. RecordVarOriginFromAST 识别 *ast.MemberAccessExpression（`std.memory.x`），
//      此前只认 *ast.MemberExpression，导致 origin 恒为 unknown；
//   b. SOR 路由早退分支补 RecordVarOriginFromAST（普通声明路径有、SOR 路径漏）；
//   c. concGuardedDecl 在降级前用「原始初始化式」登记 origin/size，并以
//      SetInitializerSupplied 标记「初始化式已单独生成」，声明本体只出类型不出
//      缺省分配；
//   d. 用户显式 free 的跳过分支限定为「非 KMM 来源」——KMM 对象必须照常
//      survivor_free（TLAB 指针在该函数内被区间检查安全拒绝）。

// 最小复现（a）：普通函数内 循环 + kmm_v4_alloc 立即 free。
// 用于断言「用户 free 不得抑制 survivor 回收」。
const kmmFreeInLoopSrc = `import std.memory

fn work(n: i32) -> i32 {
    i32 s = 0
    for i in range(n) {
        u8* r = std.memory.kmm_v4_alloc(64)
        r[0] = as<u8>(i)
        s = s + as<i32>(r[0])
        std.memory.kmm_v4_free(r)
    }
    return s
}

fn main() -> i32 {
    return work(10)
}
`

// 最小复现（b）：conc fn 内 循环体 + 分配 + 挂起点 + 用户 free。
// 这份 src 同时触发「守卫降级死分配」与「用户 free 抑制回收」两条路径。
const kmmFreeConcGuardSrc = `extern fn net_recv(fd: i32, buf: *u8, len: i32) -> i64
extern fn tls_result() -> i32
extern fn do_close(fd: i32) -> i32
extern fn at_of(buf: *u8, off: i32) -> *u8
extern fn svc_send(fd: i32, buf: *u8, len: i32) -> i32

import std.memory
import std.time

conc fn conn_task(fd: i32) -> i32 {
    u8* buf = std.memory.kmm_v4_alloc(4096)
    while (true) {
        net_recv(fd, at_of(buf, 0), 4096)
        i32 got = tls_result()
        if (got <= 0) {
            do_close(fd)
            return 0
        }
        u8* r = std.memory.kmm_v4_alloc(64)
        r[0] = as<u8>(got)
        std.memory.kmm_v4_free(r)
        std.time.time_sleep_us(200)
        svc_send(fd, buf, got)
    }
    return 0
}

fn main() -> i32 {
    return 0
}
`

// TestUserFreeDoesNotSuppressSurvivorFree 断言：KMM 来源变量即使用户显式
// 调用 kmm_v4_free，也不能跳过 per-object survivor_free。
//
// 修复前生成 C 中是 `/* structural: skip survivor_free for r (user-freed ...) */`，
// 回收完全消失；修复后该变量在作用域尾必须出现 kmm_v4_survivor_free(r)。
func TestUserFreeDoesNotSuppressSurvivorFree(t *testing.T) {
	cCode := generateForMarkTest(t, kmmFreeInLoopSrc)

	skipRe := regexp.MustCompile(`skip survivor_free for r \(user-freed`)
	freeRe := regexp.MustCompile(`kmm_v4_survivor_free\(r\)`)

	if skipRe.MatchString(cCode) {
		t.Fatalf("KMM 变量 r 的 survivor 回收被「用户已 free」误跳过（KBUG 回归，%s）:\n%s",
			"survivor 槽位将永久泄漏", cCode)
	}
	if !freeRe.MatchString(cCode) {
		t.Fatalf("未对 KMM 变量 r 发射 kmm_v4_survivor_free（KBUG 回归）:\n%s", cCode)
	}
}

// TestNoDeadSurvivorAllocInConcGuard 断言：conc 守卫降级路径不得发射
// 「结果立即被覆盖」的死 survivor 分配。
//
// 修复前生成 C 中每个 resume 段都含
//
//	uint8_t* r = (uint8_t*)kmm_v4_alloc_global(sizeof(uint8_t));
//	r = f->r;
//
// 第一行的分配结果被第二行覆盖 → 每次请求泄漏一槽。
// 修复后第一行必须是纯声明 `uint8_t* r;`（decl only 注释）。
func TestNoDeadSurvivorAllocInConcGuard(t *testing.T) {
	cCode := generateForMarkTest(t, kmmFreeConcGuardSrc)

	// 死分配特征：一行 `X = (...)kmm_v4_alloc_global(...);` 之后（相隔 0~1 行）
	// 出现对同一变量 X 的赋值/恢复副本 `X = ...`。
	// Go 的 regexp 不支持反向引用，故用逐行扫描。
	allocRe := regexp.MustCompile(
		`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\([^)]*\)\s*kmm_v4_alloc_global\(`)
	lines := strings.Split(cCode, "\n")
	for i, ln := range lines {
		m := allocRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		varName := m[1]
		// 向下看 1~2 行，找同变量的立即覆盖
		for j := i + 1; j < len(lines) && j <= i+2; j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), varName+" = ") {
				t.Fatalf("conc 守卫路径发射了被立即覆盖的死 survivor 分配"+
					"（KBUG 回归，每 resume 泄漏一槽）：\n  %s\n  %s",
					strings.TrimSpace(ln), strings.TrimSpace(lines[j]))
			}
		}
	}

	// 正向断言：降级声明必须以 decl only 形式出现（证明标记生效）。
	if !strings.Contains(cCode, "decl only (initializer generated separately)") {
		t.Fatalf("conc 守卫降级声明未走 decl-only 路径（标记未生效）:\n%s", cCode)
	}
}

// TestConcGuardDeclRegistersKMMOrigin 断言：conc 守卫降级路径下
// 变量 origin 必须是 kmm（否则「用户 free」分支会把回收判为可跳过）。
//
// 这是缺陷 (a)+(b) 的回归锁：*ast.MemberAccessExpression 与 SOR 早退分支
// 任一未修，origin 都会回落成 unknown，进而复发泄漏。
func TestConcGuardDeclRegistersKMMOrigin(t *testing.T) {
	cCode := generateForMarkTest(t, kmmFreeConcGuardSrc)

	// 若 origin 仍是 unknown，收尾会走 "unknown origin, safe no-op" 分支
	// 或 skip 分支；正确路径是 "per-object reclaim (kmm)"。
	if strings.Contains(cCode, "per-object reclaim (unknown origin") {
		t.Fatalf("conc 守卫声明未登记 KMM 来源（origin=unknown，KBUG 回归）:\n%s", cCode)
	}
	if !strings.Contains(cCode, "per-object reclaim (kmm)") {
		t.Fatalf("conc 守卫声明的 survivor 回收未走 kmm origin 路径:\n%s", cCode)
	}
}
