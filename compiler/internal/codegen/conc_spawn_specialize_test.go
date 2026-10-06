package codegen

import (
	"regexp"
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/config"
)

/*
 * spawn 常量特化的签名一致性回归。
 *
 * == 缺陷 ==
 *
 * specializeSpawnConsts 把 `spawn f(5)` 改指克隆版 `f_spc_N`，
 * 同时把克隆版的 Params/ParamTypes/ParamNullables 清空（理由是
 * "实参已代入常量，Frame 不该再有参数字段"）。
 *
 * 但 emitter 是**从 AST 的 Params 建的**（generateConcurrentFunction），
 * 而调用点的实参是按**原签名**生成的。清空 Params 于是让两侧错位：
 *
 *   - Frame 少了参数字段，spawn 仍照写 __frame->rounds
 *   - shim 定义成了 K_x_spc_N_as_fn(void)，调用点却传实参
 *
 * clang 报 "too many arguments to function call, expected 0, have 1"，
 * 整个 TU 编不过 —— 而触发条件只是"spawn 的实参全是字面量"。
 * 实测 test/concurrency/test_for_range_suspend.kl 的
 * `spawn loop_nested(5)` 必现（loop_nested 唯一实参是字面量 5）。
 *
 * == 判据 ==
 *
 * 生成代码里，特化版 shim 的**定义**参数个数必须与**调用点**传的
 * 实参个数一致；且特化版 Frame 必须保留形参字段。
 */

// spawnSpecializeSrc 一份最小编译单元：spawn 的实参全是字面量。
//
// 用 loop_nested 的形状而不是随手写的玩具函数 —— 它是真实用例里
// 踩到该缺陷的那一个（字面量实参 + 无挂起 → 走 spawn 内联路径）。
const spawnSpecializeSrc = `
import std.time

conc fn inner(i64 x) -> i64 {
    std.time.time_sleep(1)
    return x + 10
}

conc fn loop_nested(i64 rounds) -> i64 {
    i64 acc = 0
    for i in range(rounds) {
        i64 v = inner(i)
        acc += v
    }
    return acc
}

fn main() -> i32 {
    Handle h = spawn loop_nested(5)
    i64 r = h.await
    return 0
}
`

// countCParams 数一个 C 参数列表里的形参个数。
// "" 与 "void" 都算 0 个。
func countCParams(list string) int {
	s := strings.TrimSpace(list)
	if s == "" || s == "void" {
		return 0
	}
	return len(strings.Split(s, ","))
}

// TestSpawnSpecialize_ShimArityMatchesCallSite 是本组的主判据。
func TestSpawnSpecialize_ShimArityMatchesCallSite(t *testing.T) {
	c, errs := genConc(t, spawnSpecializeSrc)
	for _, e := range errs {
		t.Errorf("不应报错，实际：%s", e)
	}

	// 确认确实发生了特化（否则这个用例什么都没测到）
	if !strings.Contains(c, "loop_nested_spc_0") {
		t.Fatalf("本用例应当触发 spawn 常量特化（期望出现 loop_nested_spc_0）：\n%s", c)
	}

	/*
	 * shim 定义：`int64_t  K_loop_nested_spc_0_as_fn(<参数>) {`
	 * 抓参数列表，数逗号分隔的形参个数。
	 */
	defRe := regexp.MustCompile(`(?m)^\S+\s+K_loop_nested_spc_0_as_fn\(([^)]*)\)\s*\{`)
	def := defRe.FindStringSubmatch(c)
	if def == nil {
		t.Fatalf("找不到特化版 shim 的定义：\n%s", c)
	}
	defParams := countCParams(def[1])

	/*
	 * 调用点：spawn 内联路径生成 `K_loop_nested_spc_0_as_fn(5)`。
	 * 匹配**非定义**的那一处（定义后面紧跟 ` {`，调用点后面跟 `;`）。
	 */
	callRe := regexp.MustCompile(`K_loop_nested_spc_0_as_fn\(([^)]*)\);`)
	call := callRe.FindStringSubmatch(c)
	if call == nil {
		// 也可能走非内联路径（帧分配 + kaula_spawn_*）。
		// 那条路径不直接调 shim，改为校验 Frame 字段。
		if !strings.Contains(c, "__frame->rounds") {
			t.Fatalf("既没有 shim 调用点也没有 __frame->rounds 写入，用例形状不对：\n%s", c)
		}
		return
	}
	callArgs := countCParams(call[1])

	if callArgs != defParams {
		t.Errorf("特化版 shim 定义收 %d 个参数，调用点传 %d 个实参 —— "+
			"清空 clone.Params 会让两侧错位（clang: too many arguments）：\n%s",
			defParams, callArgs, c)
	}
	if defParams == 0 {
		t.Errorf("特化版 shim 不应当是无参：调用了 spawn loop_nested(5)，"+
			"形参必须保留：\n%s", c)
	}
}

// TestSpawnSpecialize_FrameKeepsParamFields 校验特化版 Frame 保留形参字段。
//
// 与之配套的 spawn 写帧是 __frame-><形参名> = <实参>；
// Frame 里没有那个字段就是 C 的 "no member named 'rounds'"。
func TestSpawnSpecialize_FrameKeepsParamFields(t *testing.T) {
	c, _ := genConc(t, spawnSpecializeSrc)

	if !strings.Contains(c, "loop_nested_spc_0") {
		t.Fatalf("本用例应当触发特化：\n%s", c)
	}

	// 从特化版 Frame 定义里取字段块
	re := regexp.MustCompile(
		`(?s)Frame for conc fn loop_nested_spc_0.*?typedef struct \{(.*?)\} K_loop_nested_spc_0_Frame;`)
	m := re.FindStringSubmatch(c)
	if m == nil {
		t.Fatalf("找不到特化版 Frame 定义：\n%s", c)
	}
	if !regexp.MustCompile(`\brounds\b`).MatchString(m[1]) {
		t.Errorf("特化版 Frame 必须保留形参字段 rounds"+
			"（调用点仍按原签名写 __frame->rounds）：\n%s", m[1])
	}
}

// TestSpawnSpecialize_ParamTablesStayAligned 校验三张参数表等长。
//
// Params / ParamTypes / ParamNullables 按下标并行访问
// （generateConcurrentFunction 里的 `stmt.ParamTypes[i]`），
// 长短不一就是越界 panic 或静默取到错位类型。
func TestSpawnSpecialize_ParamTablesStayAligned(t *testing.T) {
	prog := parse(spawnSpecializeSrc)

	cg := NewCodeGenerator(config.DefaultConfig())
	cg.specializeSpawnConsts(prog)

	sawSpecialized := false
	for _, st := range prog.Statements {
		fn, ok := st.(*ast.FunctionStatement)
		if !ok || !strings.Contains(fn.Name, "_spc_") {
			continue
		}
		sawSpecialized = true

		if len(fn.Params) != 1 {
			t.Errorf("%s: 形参个数应为 1（rounds），实际 %d", fn.Name, len(fn.Params))
		}
		if len(fn.ParamTypes) < len(fn.Params) {
			t.Errorf("%s: ParamTypes 短于 Params（%d < %d）—— "+
				"下游按下标访问会越界",
				fn.Name, len(fn.ParamTypes), len(fn.Params))
		}
		if len(fn.ParamNullables) < len(fn.Params) {
			t.Errorf("%s: ParamNullables 短于 Params（%d < %d）",
				fn.Name, len(fn.ParamNullables), len(fn.Params))
		}
	}
	if !sawSpecialized {
		t.Fatalf("没有产生特化版函数，用例失效")
	}
}
