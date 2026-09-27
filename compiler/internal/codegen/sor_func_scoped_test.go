package codegen

import (
	"strings"
	"testing"
)

// multiFuncSORResult 构造两个函数各自声明同名变量（tmp）的 SOR 序列化结果：
// main.tmp 逃逸(Heap) 64 字节，worker.tmp 不逃逸(None) 128 字节
func multiFuncSORResult() map[string]interface{} {
	return map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name": "tmp", "obj_id": "obj_1", "func_name": "main",
				"alloc_kind": "BumpPool", "alloc_kind_id": float64(1),
				"drop_action": "ScopeEnd", "drop_action_id": float64(1),
				"scope_id_int": float64(0), "scope_id": "0",
			},
			map[string]interface{}{
				"var_name": "tmp", "obj_id": "obj_2", "func_name": "worker",
				"alloc_kind": "BumpPool", "alloc_kind_id": float64(1),
				"drop_action": "ScopeEnd", "drop_action_id": float64(1),
				"scope_id_int": float64(0), "scope_id": "0",
			},
		},
		"escape": map[string]interface{}{"obj_1": "Heap", "obj_2": "None"},
		"sizes":  map[string]interface{}{"obj_1": float64(64), "obj_2": float64(128)},
	}
}

// 同名变量在不同函数中的决策必须彼此独立（根治 #1）
func TestVarDecision_FunctionScoped(t *testing.T) {
	adapter := NewSORCodeGenAdapter(multiFuncSORResult())

	adapter.SetCurrentFunc("main")
	dMain := adapter.GetVarDecision("tmp")
	if dMain == nil {
		t.Fatal("main.tmp decision missing")
	}
	if dMain.ObjID != "obj_1" || dMain.SizeBytes != 64 || dMain.EscapeLevel != "Heap" {
		t.Errorf("main.tmp got wrong decision: %+v", dMain)
	}

	adapter.SetCurrentFunc("worker")
	dWorker := adapter.GetVarDecision("tmp")
	if dWorker == nil {
		t.Fatal("worker.tmp decision missing")
	}
	if dWorker.ObjID != "obj_2" || dWorker.SizeBytes != 128 || dWorker.EscapeLevel != "None" {
		t.Errorf("worker.tmp got wrong decision: %+v", dWorker)
	}
	if dMain == dWorker {
		t.Error("main.tmp and worker.tmp must not share the same decision object")
	}

	// 切换函数后 CurrentFunc 正确
	if adapter.CurrentFunc() != "worker" {
		t.Errorf("CurrentFunc = %q, want worker", adapter.CurrentFunc())
	}
}

// 未声明该变量的函数不得借用别的函数的同名决策（形参/外来对象的保守路径）
func TestVarDecision_NoCrossFunctionFallback(t *testing.T) {
	adapter := NewSORCodeGenAdapter(multiFuncSORResult())
	adapter.SetCurrentFunc("callee")
	if d := adapter.GetVarDecision("tmp"); d != nil {
		t.Errorf("callee must not see main/worker decisions for tmp, got %+v", d)
	}
	// 显式版本同样不应跨函数命中
	if d := adapter.GetVarDecisionIn("callee", "tmp"); d != nil {
		t.Errorf("GetVarDecisionIn(callee, tmp) = %+v, want nil", d)
	}
	// 显式指定函数可以取到
	if d := adapter.GetVarDecisionIn("main", "tmp"); d == nil || d.ObjID != "obj_1" {
		t.Errorf("GetVarDecisionIn(main, tmp) = %+v, want obj_1", d)
	}
}

// 顶层声明（func_name 为空）对任意函数可见
func TestVarDecision_TopLevelVisibleEverywhere(t *testing.T) {
	adapter := NewSORCodeGenAdapter(map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name": "g", "obj_id": "obj_9", "func_name": "",
				"alloc_kind": "BumpPool", "alloc_kind_id": float64(1),
				"drop_action": "ScopeEnd", "drop_action_id": float64(1),
				"scope_id_int": float64(0), "scope_id": "0",
			},
		},
		"escape": map[string]interface{}{"g": "Global"},
	})
	for _, fn := range []string{"", "main", "other"} {
		adapter.SetCurrentFunc(fn)
		if d := adapter.GetVarDecision("g"); d == nil || d.ObjID != "obj_9" {
			t.Errorf("top-level decision must be visible in func %q, got %+v", fn, d)
		}
	}
}

// 端到端：被调函数的形参不得触发 "promote arg into callee"
// （历史缺陷：形参命中调用方同名局部决策 → 就地 promote 出 sizeof(*p) 截断副本）
func TestSORCallArgPromote_ParameterNotPromoted(t *testing.T) {
	// worker 内的形参 p 与 main 内的局部 p 同名
	result := map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name": "p", "obj_id": "obj_1", "func_name": "main",
				"alloc_kind": "BumpPool", "alloc_kind_id": float64(1),
				"drop_action": "ScopeEnd", "drop_action_id": float64(1),
				"scope_id_int": float64(0), "scope_id": "0",
			},
		},
		"escape": map[string]interface{}{"obj_1": "Heap"},
		"sizes":  map[string]interface{}{"obj_1": float64(64)},
		// worker(p: i64*) 的形参为消费所有权（ModeOwned）
		"func_sigs": map[string]interface{}{
			"worker": map[string]interface{}{
				"params": []interface{}{
					map[string]interface{}{"name": "p", "mode": float64(0)},
				},
			},
		},
	}
	src := "fn worker(p: i64*) {\n    p[0] = 1\n    worker(p)\n}\n" +
		"fn main() {\n    i64* p = kmm_v4_alloc(64)\n    p[0] = 0\n    worker(p)\n}\n"
	cCode := generateWithSORResult(t, src, result)

	// main 侧的实参 promote 是合法的（调用点转移所有权）
	if !strings.Contains(cCode, "promote arg into callee") {
		t.Errorf("caller-side promote expected; got:\n%s", cCode)
	}
	// 被调函数内的形参不得再被 promote（否则递归/再调用会 promote 形参）
	if n := strings.Count(cCode, "promote arg into callee"); n != 1 {
		t.Errorf("parameter must not be promoted (want exactly 1 promote at call site, got %d):\n%s",
			n, cCode)
	}
}
