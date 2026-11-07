package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
)

// TestReturnEscapeChainCodegen 锁死链构造器的生成形态：
//   - 函数自身不建立 KMM 作用域（节点归调用方 bump 区）；
//   - 不做逐对象 promote / survivor_free；
//   - if/块内不发射 mark_restore（否则分支退出就回收子树）；
//   - 调用方 main 仍保留作用域与循环 mark_restore（整条链由调用方回收）。
func TestReturnEscapeChainCodegen(t *testing.T) {
	prog := parse(chainScanTreeSrc)
	cg := NewCodeGenerator(config.DefaultConfig())
	out := cg.Generate(prog)

	if !strings.Contains(out, "Node* bottomUpTree(int64_t depth)") {
		t.Fatal("未生成 bottomUpTree 函数")
	}
	start := strings.Index(out, "Node* bottomUpTree(int64_t depth)")
	end := strings.Index(out[start+1:], "int64_t itemCheck(")
	if end < 0 {
		t.Fatal("未找到 itemCheck 边界")
	}
	body := out[start : start+1+end]

	for _, banned := range []string{
		`KMM_V4_SCOPE_NAMED("bottomUpTree")`,
		"kmm_v4_scope_promote_p",
		"kmm_v4_survivor_free",
		"kmm_v4_mark_restore",
	} {
		if strings.Contains(body, banned) {
			t.Errorf("链构造器函数体不应包含 %q:\n%s", banned, body)
		}
	}
	for _, want := range []string{
		"kmm_v4_alloc(16)",
		"bottomUpTree(kaula_sub_i64",
		"n->left = l",
		"n->right = r",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("链构造器函数体缺少 %q:\n%s", want, body)
		}
	}
	if strings.Contains(out, "kmm_v4_scope_promote_p") {
		t.Errorf("整条链已留在调用方 bump 区，不应再发射 promote:\n%s", out)
	}
	if !strings.Contains(out, `KMM_V4_SCOPE_NAMED("main")`) {
		t.Error("main 仍应建立 KMM 作用域，整条链由它统一回收")
	}
	if !strings.Contains(out, "kmm_v4_mark_restore(_loop_scope_start)") {
		t.Error("main 循环仍应发射 mark_restore 回收每轮链")
	}
}
