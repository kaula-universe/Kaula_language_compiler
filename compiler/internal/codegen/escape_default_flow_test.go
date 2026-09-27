package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// generateWithEscape 走 parse → sema → codegen（非 --sor 默认流程），
// 并注入轻量逃逸分析结果（与 kaulac 默认流程一致：SetEscapeAnalysis）
func generateWithEscape(t *testing.T, src string, escape map[string]interface{}) string {
	t.Helper()
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	if escape != nil {
		cg.SetEscapeAnalysis(map[string]interface{}{"escape": escape})
	}
	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}
	return cCode
}

// 默认流程 + 逃逸级别 None：走 alloc_auto 的 TLAB 快速路径（保持原行为）
func TestDefaultFlowEscape_ScopeLocal_UsesAuto(t *testing.T) {
	src := "fn main() {\n    i64* p\n    p[0] = 1\n}\n"
	cCode := generateWithEscape(t, src, map[string]interface{}{"p": "None"})
	if !strings.Contains(cCode, "kmm_v4_alloc_auto(sizeof(int64_t))") {
		t.Errorf("scope-local var should use alloc_auto fast path; got:\n%s", cCode)
	}
	if strings.Contains(cCode, "kmm_v4_alloc_global(sizeof(int64_t))") {
		t.Errorf("scope-local var must not use survivor slow path; got:\n%s", cCode)
	}
}

// 默认流程 + 逃逸（Arg/Return/CrossScope/Global/Heap）：走 alloc_global survivor 段，
// scope_pop 不回卷，作用域末尾的 per-object survivor_free 精确回收
func TestDefaultFlowEscape_Escaping_UsesGlobal(t *testing.T) {
	for _, level := range []string{"Arg", "Return", "CrossScope", "Global", "Heap"} {
		src := "fn main() {\n    i64* p\n    p[0] = 1\n}\n"
		cCode := generateWithEscape(t, src, map[string]interface{}{"p": level})
		if !strings.Contains(cCode, "kmm_v4_alloc_global(sizeof(int64_t))") {
			t.Errorf("escape=%s should use survivor alloc_global; got:\n%s", level, cCode)
		}
		if strings.Contains(cCode, "kmm_v4_alloc_auto(sizeof(int64_t))") {
			t.Errorf("escape=%s must not use TLAB fast path; got:\n%s", level, cCode)
		}
	}
}

// 逃逸分析未覆盖的变量：保持既有行为（alloc_auto），不因缺信息改写策略
func TestDefaultFlowEscape_UnknownKeepsAuto(t *testing.T) {
	src := "fn main() {\n    i64* p\n    i64* q\n    p[0] = 1\n    q[0] = 2\n}\n"
	cCode := generateWithEscape(t, src, map[string]interface{}{"p": "None"})
	if !strings.Contains(cCode, "kmm_v4_alloc_auto(sizeof(int64_t))") {
		t.Errorf("unknown var should keep alloc_auto; got:\n%s", cCode)
	}
	if strings.Contains(cCode, "kmm_v4_alloc_global(sizeof(int64_t))") {
		t.Errorf("unknown var must not be routed to survivor; got:\n%s", cCode)
	}
}

// 无逃逸分析结果（escapeAvailable=false）时行为与改动前完全一致
func TestDefaultFlowEscape_NoAnalysis_Unchanged(t *testing.T) {
	src := "fn main() {\n    i64* p\n    p[0] = 1\n}\n"
	cCode := generateWithEscape(t, src, nil)
	if !strings.Contains(cCode, "kmm_v4_alloc_auto(sizeof(int64_t))") {
		t.Errorf("without escape analysis the output must be unchanged; got:\n%s", cCode)
	}
	if cg := (&CodeGenerator{}); cg.HasEscapeAnalysis() {
		t.Error("zero-value CodeGenerator must report no escape analysis")
	}
}
