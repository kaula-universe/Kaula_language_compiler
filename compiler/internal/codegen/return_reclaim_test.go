package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
)

// F17 回归：return 路径的结构化回收不得先于返回表达式求值。
// 缺陷形态：`return arr[0] + arr[1]` 之前发射 survivor_free(raw)，
// arr 与 raw 同块内存 → use-after-free，结果静默出错（合法写法触发）。
func TestReturnReclaimAfterExprEvaluation(t *testing.T) {
	src := `
import std.io
import std.memory

fn sum_via_ptr() i64 {
    auto raw = std.memory.kmm_v4_alloc(24)
    i64* arr = raw
    arr[0] = 3
    arr[1] = 1
    arr[2] = 4
    return arr[0] + arr[1] + arr[2]
}

fn main() { println("s=", sum_via_ptr()) }
`
	prog := parse(src)
	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	out := cg.Generate(prog)
	start := strings.Index(out, "K_sum_via_ptr(void)")
	if start < 0 {
		t.Fatalf("function body not generated:\n%s", out)
	}
	end := strings.Index(out[start:], "\n}\n")
	region := out[start : start+end]
	iFree := strings.Index(region, "survivor_free(")
	iRewind := strings.Index(region, "survivor_rewind(")
	if iFree < 0 && iRewind < 0 {
		t.Logf("no return-path reclaim emitted; nothing to assert")
		return
	}
	iReclaim := iFree
	if iReclaim < 0 || (iRewind >= 0 && iRewind < iReclaim) {
		iReclaim = iRewind
	}
	iLastRead := strings.LastIndex(region, "arr[")
	if iLastRead >= 0 && iReclaim < iLastRead {
		t.Errorf("return-path reclaim emitted BEFORE return-expression reads freed memory (UAF); region:\n%s", region)
	}
}
