package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// generateWithSORResult 走 parse → sema → codegen，并注入 SOR 分析结果
// （模拟 kaulac --sor：SetSORResult）
func generateWithSORResult(t *testing.T, src string, result map[string]interface{}) string {
	t.Helper()
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	cg.SetSORResult(result)
	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}
	return cCode
}

// sorDecisionMap 构造一个 BumpPool/ScopeEnd 决策（escape 级别由 escapeLevel 决定）
func sorDecisionMap(varName, objID, escapeLevel string, size float64) map[string]interface{} {
	return map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name":       varName,
				"obj_id":         objID,
				"alloc_kind":     "BumpPool",
				"alloc_kind_id":  float64(1),
				"drop_action":    "ScopeEnd",
				"drop_action_id": float64(1),
				"scope_id_int":   float64(0),
				"scope_id":       "0",
			},
		},
		"escape": map[string]interface{}{varName: escapeLevel},
		"sizes":  map[string]interface{}{objID: size},
	}
}

// 初始值是分配调用时，SOR 路径不再"先分配后覆盖"
// （回归：曾生成 kmm_v4_alloc_auto(...) 后立刻被 kmm_v4_alloc(64) 覆盖）
func TestSORInitProvidesPointer_NoDoubleAlloc(t *testing.T) {
	src := "fn main() {\n    i64* p = kmm_v4_alloc(64)\n    p[0] = 1\n}\n"
	cCode := generateWithSORResult(t, src, sorDecisionMap("p", "obj_p", "None", 64))
	if strings.Contains(cCode, "init provides object") == false {
		t.Logf("generated C:\n%s", cCode)
	}
	if !strings.Contains(cCode, "int64_t* p = kmm_v4_alloc(64);") {
		t.Errorf("expected direct declaration from init, got:\n%s", cCode)
	}
	for _, unwanted := range []string{"kmm_v4_alloc_auto(sizeof(int64_t))", "kmm_v4_alloc_global(64)"} {
		if strings.Contains(cCode, unwanted) {
			t.Errorf("declaration must not allocate again (%s), got:\n%s", unwanted, cCode)
		}
	}
}

// 初始值是指针别名时同样不重复分配
func TestSORInitPointerAlias_NoDoubleAlloc(t *testing.T) {
	src := "fn main() {\n    i64* q = kmm_v4_alloc(64)\n    i64* p = q\n    p[0] = 1\n}\n"
	cCode := generateWithSORResult(t, src, sorDecisionMap("p", "obj_p", "None", 64))
	if !strings.Contains(cCode, "int64_t* p = q;") {
		t.Errorf("expected alias init to be declared directly, got:\n%s", cCode)
	}
	if strings.Contains(cCode, "kmm_v4_alloc_global(64)") || strings.Contains(cCode, "kmm_v4_alloc_auto(sizeof(int64_t))") {
		t.Errorf("alias init must not allocate again, got:\n%s", cCode)
	}
}

// 初始值是标量 0 时必须保留分配（变量需要合法初始指针）
func TestSORInitScalar_KeepsAlloc(t *testing.T) {
	src := "fn main() {\n    i64* p = 0\n    p = kmm_v4_alloc(64)\n    p[0] = 1\n}\n"
	cCode := generateWithSORResult(t, src, sorDecisionMap("p", "obj_p", "None", 64))
	if !strings.Contains(cCode, "kmm_v4_alloc_auto(sizeof(int64_t))") {
		t.Errorf("scalar init must still allocate, got:\n%s", cCode)
	}
}
