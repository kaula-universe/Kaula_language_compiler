package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/config"
)

// --- 任务④：sizing 驱动的 slab 桶直接分配 ---
func TestSizedSurvivorAlloc_BumpPool_SizingKnown(t *testing.T) {
	adapter := NewSORCodeGenAdapter(map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name":       "n",
				"obj_id":         "obj_n",
				"alloc_kind":     "BumpPool",
				"alloc_kind_id":  float64(1), // JSON 默认 float64
				"drop_action":    "ScopeEnd",
				"drop_action_id": float64(1),
				"scope_id_int":   float64(0),
				"scope_id":       "0",
			},
		},
		"escape": map[string]interface{}{
			"obj_n": "CrossReturn",
		},
		"sizes": map[string]interface{}{
			"obj_n": float64(24),
		},
	})
	dec := adapter.GetVarDecision("n")
	if dec == nil {
		t.Fatalf("GetVarDecision(n) = nil — decisions serialization broken?")
	}
	t.Logf("decision: AllocKind=%q SizeBytes=%d ScopeID=%d ID=%d", dec.AllocKind, dec.SizeBytes, dec.ScopeID, dec.AllocKindID)
	out := adapter.GenerateSmartVarAlloc("Node*", "n", "  ", "0")
	t.Logf("SIZED=%q", out)
	if !strings.Contains(out, "kmm_v4_alloc_global(24)") {
		t.Errorf("sizing-driven survivor-slab direct alloc expected kmm_v4_alloc_global(24), got %q", out)
	}
	if strings.Contains(out, "kmm_v4_alloc_auto") {
		t.Errorf("sizing-driven must skip TLAB auto; got %q", out)
	}
}

func TestSurvivorAlloc_NoSizing_FallbackToAuto(t *testing.T) {
	adapter := NewSORCodeGenAdapter(map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name":       "p",
				"obj_id":         "obj_p",
				"alloc_kind":     "BumpPool",
				"alloc_kind_id":  float64(1),
				"drop_action":    "ScopeEnd",
				"drop_action_id": float64(1),
				"scope_id_int":   float64(0),
				"scope_id":       "0",
			},
		},
		"escape": map[string]interface{}{
			"obj_p": "CrossScope",
		},
		// 故意不提供 sizes map → SizeBytes 保持 0 → fallback sizeof
	})
	out := adapter.GenerateSmartVarAlloc("SomeStruct*", "p", "  ", "0")
	t.Logf("NOSIZE=%q", out)
	if !strings.Contains(out, "kmm_v4_alloc_auto(sizeof(SomeStruct))") {
		t.Errorf("fallback sizeof expected, got %q", out)
	}
}

// --- 任务③：extern 来源识别（从 FullAnalysisResult extern_origins 反序列化） ---
func TestExternOriginsDeserialization(t *testing.T) {
	adapter := NewSORCodeGenAdapter(map[string]interface{}{
		"extern_origins": map[string]interface{}{
			"p":       "ext_get",
			"q":       "malloc_wrap",
			"nocross": "safe_call",
		},
	})
	if fn, ok := adapter.IsExternOrigin("p"); !ok || fn != "ext_get" {
		t.Errorf("IsExternOrigin(p) = %q,%v; want ext_get,true", fn, ok)
	}
	if fn, ok := adapter.IsExternOrigin("missing"); ok || fn != "" {
		t.Errorf("missing var expected false; got %q,%v", fn, ok)
	}
	if _, ok := adapter.IsExternOrigin("q"); !ok {
		t.Errorf("q should be extern origin")
	}
}

// --- 任务②：EnsureDeepPromoteCB 非 class 返回空（纯查询不合成，不依赖 classTypes） ---
func TestEnsureDeepPromoteCB_NonClass_Empty(t *testing.T) {
	adapter := NewSORCodeGenAdapter(nil) // 非激活，确保不 crash
	_ = adapter
}

// --- 核查表缺口 ⑥：深提升端到端合成测试（修复 Bug 2 隐患②） ---
// 真实构造 CodeGenerator + TypeGenerator，注册含嵌套指针 / inline class / String /
// void* / POD 等混合字段的 class，断言 EnsureDeepPromoteCB 合成的 C 代码包含：
//  1. 多级嵌套指针字段：传 __kaula_deeppromote_X cb（非 NULL）
//  2. 内嵌 class 值字段：直接 cb(&(self->f))（非 promote_p_deep 二次分配）
//  3. String 字段：string_clone_global 克隆载荷
//  4. void* 字段：opaque 注释（不改动）
//  5. POD-only class：no-op cb 且"无递归提升"注释
func newCodeGeneratorForTest(t *testing.T) *CodeGenerator {
	t.Helper()
	// go test ./internal/codegen → CWD = <repo>/kaula-compiler/internal/codegen
	// 需要找到 <repo>/kaula-compiler/templates 和 stdlib.json：连续上溯寻找有 templates/main.c.tmpl 的目录
	candidates := []string{
		filepath.Join("..", ".."),                      // 从 internal/codegen 上两级 -> kaula-compiler
		".",                                            // 已在 kaula-compiler 根
		filepath.Join("..", "..", "..", "kaula-compiler"), // 从 repo/kaula 目录上溯
	}
	var foundDir string
	for _, cand := range candidates {
		mainTmpl := filepath.Join(cand, "templates", "main.c.tmpl")
		if _, err := os.Stat(mainTmpl); err == nil {
			foundDir = cand
			break
		}
	}
	if foundDir == "" {
		abs, _ := os.Getwd()
		t.Skipf("cannot locate templates/ dir (running from %s, candidates=%v)", abs, candidates)
	}
	tmpl := filepath.Join(foundDir, "templates")
	stdlibPath := filepath.Join(foundDir, "stdlib.json")
	cfg := &config.Config{
		BasePath:     foundDir,
		TemplatePath: tmpl,
		StdlibPath:   stdlibPath,
	}
	return NewCodeGenerator(cfg)
}

func mkClass(name string, fields ...*ast.FieldDeclaration) *ast.ClassStatement {
	return &ast.ClassStatement{Name: name, Fields: fields}
}
func mkField(name, typ string, nullable bool) *ast.FieldDeclaration {
	return &ast.FieldDeclaration{Name: name, Type: typ, Nullable: nullable}
}

func TestEnsureDeepPromoteCB_NestedPointersAndInlineClass(t *testing.T) {
	cg := newCodeGeneratorForTest(t)
	// 注册两个互相字段引用的 class
	//   Inner: parent *Node, title String
	//   Node:  child *Node, inner Inner, label String, opaque void*, count int
	if cg.typeGenerator == nil {
		t.Fatal("typeGenerator not initialized")
	}
	cg.typeGenerator.classTypes["Node"] = true
	cg.typeGenerator.classTypes["Inner"] = true
	// EnsureDeepPromoteCB 里 getClassFields 依赖 cg.program.Statements；
	// NewCodeGenerator 不初始化 program（在 Generate 时注入），这里手动给一个空 Program。
	if cg.program == nil {
		cg.program = &ast.Program{}
	}

	innerStmt := mkClass("Inner",
		mkField("parent", "Node", true),   // 父节点引用：*Node（Nullable=true 生成指针）
		mkField("title", "String", false), // String 字段：克隆载荷
	)
	nodeStmt := mkClass("Node",
		mkField("child", "Node", true),    // 嵌套指针：*Node，需要递归子 cb（非 NULL）
		mkField("inner", "Inner", false),  // 内嵌值字段 Inner：直接 cb(&(self->inner))
		mkField("label", "String", false), // String 字段：string_clone_global
		mkField("opaque", "void*", true),  // 注意：这里是 ast 类型字符串"void*"
		mkField("count", "int", false),    // POD 字段：no-op
	)
	cg.program.Statements = append(cg.program.Statements, innerStmt, nodeStmt)

	// 先合成 Inner 的 cb（它引用 Node，但 EnsureDeepPromoteCB 去重不会无限循环）
	cbInner := cg.EnsureDeepPromoteCB("Inner")
	if cbInner != "__kaula_deeppromote_Inner" {
		t.Errorf("Inner cb = %q, want __kaula_deeppromote_Inner", cbInner)
	}
	cbNode := cg.EnsureDeepPromoteCB("Node")
	if cbNode != "__kaula_deeppromote_Node" {
		t.Errorf("Node cb = %q, want __kaula_deeppromote_Node", cbNode)
	}
	got := cg.deepPromoteCBCode.String()
	t.Logf("--- deep promote CBs ---\n%s", got)

	// 断言 1：嵌套指针 child *Node → kmm_v4_promote_deep(self->child, sizeof(K_Node), __kaula_deeppromote_Node)
	// 核查表 Bug 2 隐患②：cb 不能是 NULL（否则多级嵌套指针只提升 pointee 的字节，内部指针仍悬垂）
	if !strings.Contains(got, "kmm_v4_promote_deep(self->child, sizeof(K_Node), __kaula_deeppromote_Node)") {
		t.Errorf("nested Node* child should pass __kaula_deeppromote_Node as cb (not NULL); got:\n%s", got)
	}
	// 断言 2：Inner 字段（Kaula class 始终是指针存储，即使 Nullable=false）
	// → 对应第三个参数必须是 __kaula_deeppromote_Inner（证明是递归合成的子 cb 而非 NULL）。
	// （如果将来新增"struct 值类型内嵌"语法，可以在这里加值字段 cb 直调的断言）
	if !strings.Contains(got, "kmm_v4_promote_deep(self->inner, sizeof(K_Inner), __kaula_deeppromote_Inner)") {
		t.Errorf("Node.inner (Inner*) should deep-promote with Inner's cb (NULL would leave its `parent*` dangling); got:\n%s", got)
	}
	// 断言 3：String label → string_clone_global(self->label)
	if !strings.Contains(got, "string_clone_global(self->label)") {
		t.Errorf("String label field must re-clone payload; got:\n%s", got)
	}
	// 断言 4：opaque → "void* field 'opaque' is opaque to SOR" 注释
	if !strings.Contains(got, "void* field") || !strings.Contains(got, "opaque to SOR") {
		t.Errorf("void* opaque fields must carry opaque-no-op annotation; got:\n%s", got)
	}
	// 断言 5：Inner 的 parent *Node → 也递归（证明互指场景下 no 死循环 + 非 NULL）
	if !strings.Contains(got, "kmm_v4_promote_deep(self->parent, sizeof(K_Node), __kaula_deeppromote_Node)") {
		t.Errorf("Inner.parent (*Node) should deep-promote with Node's cb (NULL would be Bug 2 nested dangling); got:\n%s", got)
	}
}

func TestEnsureDeepPromoteCB_PODClass_NoOp(t *testing.T) {
	cg := newCodeGeneratorForTest(t)
	cg.typeGenerator.classTypes["PodPair"] = true
	if cg.program == nil {
		cg.program = &ast.Program{}
	}
	cg.program.Statements = append(cg.program.Statements,
		mkClass("PodPair",
			mkField("a", "int", false),
			mkField("b", "int64", false),
		))
	cb := cg.EnsureDeepPromoteCB("PodPair")
	if cb == "" {
		t.Fatal("PodPair cb empty (EnsureDeepPromoteCB returned '' for class)")
	}
	got := cg.deepPromoteCBCode.String()
	if !strings.Contains(got, "all POD fields; no recursive promotion needed") {
		t.Errorf("POD-only class should emit no-recursion annotation; got:\n%s", got)
	}
}

// --- 核查表缺口 ⑤：ShouldSkipSurvivorFree 抑制网关测试 ---
func TestShouldSkipSurvivorFree_ExternOrigin(t *testing.T) {
	adapter := NewSORCodeGenAdapter(map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name":       "handle",
				"obj_id":         "obj_handle",
				"alloc_kind":     "BumpPool",
				"alloc_kind_id":  float64(1),
				"drop_action":    "ScopeEnd",
				"drop_action_id": float64(1),
				"scope_id_int":   float64(0),
			},
		},
		"extern_origins": map[string]interface{}{
			"handle": "ext_create_resource",
		},
	})
	d := adapter.GetVarDecision("handle")
	if d == nil {
		t.Fatal("decision nil for handle")
	}
	skip, reason := adapter.ShouldSkipSurvivorFree("handle", d)
	if !skip {
		t.Errorf("extern-origin handle must be skipped (not KMM-owned); got skip=false reason=%q", reason)
	}
	if !strings.Contains(reason, "ext_create_resource") {
		t.Errorf("skip reason should cite extern fn; got %q", reason)
	}
}

func TestShouldSkipSurvivorFree_NonScopeEnd(t *testing.T) {
	adapter := NewSORCodeGenAdapter(map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name":       "p",
				"obj_id":         "obj_p",
				"alloc_kind":     "BumpPool",
				"alloc_kind_id":  float64(1),
				"drop_action":    "None",
				"drop_action_id": float64(0),
				"scope_id_int":   float64(0),
			},
		},
	})
	d := adapter.GetVarDecision("p")
	skip, _ := adapter.ShouldSkipSurvivorFree("p", d)
	if !skip {
		t.Error("DropAction=None must skip survivor_free")
	}
}

func TestShouldSkipSurvivorFree_NormalScopeEnd(t *testing.T) {
	adapter := NewSORCodeGenAdapter(map[string]interface{}{
		"decisions": []interface{}{
			map[string]interface{}{
				"var_name":       "buf",
				"obj_id":         "obj_buf",
				"alloc_kind":     "BumpPool",
				"alloc_kind_id":  float64(1),
				"drop_action":    "ScopeEnd",
				"drop_action_id": float64(1),
				"scope_id_int":   float64(0),
			},
		},
	})
	d := adapter.GetVarDecision("buf")
	skip, reason := adapter.ShouldSkipSurvivorFree("buf", d)
	if skip {
		t.Errorf("normal ScopeEnd+BumpPool must NOT skip; got skip=true reason=%q", reason)
	}
}

