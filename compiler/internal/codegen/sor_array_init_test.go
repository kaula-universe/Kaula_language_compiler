package codegen

import (
	"regexp"
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// sorArrayDecisionMap 为数组变量构造一个 SOR 决策（模拟 --sor 下被跟踪的数组）
func sorArrayDecisionMap(varName, objID string) map[string]interface{} {
	return map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name": varName, "obj_id": objID, "func_name": "main",
				"alloc_kind": "BumpPool", "alloc_kind_id": float64(1),
				"drop_action": "ScopeEnd", "drop_action_id": float64(1),
				"scope_id_int": float64(0), "scope_id": "0",
			},
		},
		"escape": map[string]interface{}{varName: "None"},
		"sizes":  map[string]interface{}{objID: float64(40)},
	}
}

// 固定大小数组 + 数组字面量：SOR 模式下也必须生成 brace 初始化器。
// C 不允许用复合字面量初始化固定数组（int arr[10] = ((int64_t[]){...})），
// clang 会报 "cannot initialize array of type 'int[10]' with array of type
// 'int64_t[10]'"。数组本不需要堆分配，必须留在基础层路径上。
func TestSORFixedArrayUsesBraceInitializer(t *testing.T) {
	src := "fn main() {\n    i32 arr[3] = [1, 2, 3]\n    i64 s = 0\n    for i in range(3) {\n        s = s + arr[i]\n    }\n}\n"
	prog := parse(src)
	sa := sema.NewSemanticAnalyzer()
	sa.Analyze(prog)

	cg := NewCodeGenerator(config.DefaultConfig())
	cg.SetSORResult(sorArrayDecisionMap("arr", "obj_arr"))
	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen: %v", cg.Errors())
	}
	norm := regexp.MustCompile(`[ \t]+`).ReplaceAllString(cCode, " ")
	if !strings.Contains(norm, "int32_t arr[3] = {1, 2, 3};") {
		t.Errorf("fixed array with literal init must use a brace initializer; got:\n%s", cCode)
	}
	if strings.Contains(norm, "arr[3] = ((") {
		t.Errorf("composite literal initializer is invalid for a fixed-size array; got:\n%s", cCode)
	}
	// 数组不应被分配到堆
	if strings.Contains(norm, "kmm_v4_alloc_auto(sizeof(int32_t))") ||
		strings.Contains(norm, "kmm_v4_alloc_global(40)") {
		t.Errorf("arrays must not be heap-allocated; got:\n%s", cCode)
	}
}
