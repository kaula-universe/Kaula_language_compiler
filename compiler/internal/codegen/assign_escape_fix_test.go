package codegen

import (
	"strings"
	"testing"

	"kaula/internal/ast"
)

// ============================================================================
// KMM 作用域回卷导致声明丢失 —— 赋值逃逸 promote 回归测试
//
// 背景：块级 KMM 作用域（KMM_V4_SCOPE_START/END、offset_save/restore）退出时
// scope_pop 回卷 bump 指针，回收块内 kmm_v4 分配的对象。若块内通过普通赋值
// dst = src 把对象引用传给外层局部变量或全局，会产生「声明还在、内容丢失」
// 的悬垂。修复：generateAssignEscapeStatement 在赋值前把源对象提升到幸存段，
// 并登记 assign-escaped 抑制源变量的块尾 per-object survivor_free。
// ============================================================================

// mkAllocCastExpr 构造 as<S*>(std.memory.kmm_v4_alloc(N)) 形态的初始化表达式，
// 用于触发 RecordAllocSizeFromAST 的常量大小登记。
func mkAllocCastExpr(size int64) ast.Expression {
	return &ast.TypeCastExpression{
		TargetType: "S*",
		Expression: &ast.CallExpression{
			Function: &ast.MemberAccessExpression{
				Object: &ast.MemberAccessExpression{
					Object: &ast.Identifier{Name: "std"},
					Member: "memory",
				},
				Member: "kmm_v4_alloc",
			},
			Args: []ast.Expression{&ast.IntegerLiteral{Value: uint64(size)}},
		},
	}
}

func mkAssignStmt(dst, src string) *ast.ExpressionStatement {
	return &ast.ExpressionStatement{
		Expression: &ast.BinaryExpression{
			Operator: "=",
			Left:     &ast.Identifier{Name: dst},
			Right:    &ast.Identifier{Name: src},
		},
	}
}

// enterScopeAt 模拟作用域嵌套深度，返回退出函数。
func enterScopeAt(t *testing.T, cg *CodeGenerator, depth int) func() {
	t.Helper()
	for i := 0; i < depth; i++ {
		cg.EnterScope("test_block")
	}
	return func() {
		for i := 0; i < depth; i++ {
			cg.ExitScope()
		}
	}
}

// 场景一（主回归）：块内分配的 q 经普通赋值逃逸到全局 G。
// 期望：赋值前发射 kmm_v4_scope_promote_p 提升到幸存段，且 q 被登记为 assign-escaped。
func TestAssignEscapePromote_GlobalEscape(t *testing.T) {
	cg := newCodeGeneratorForTest(t)
	sg := NewStatementGenerator(cg)

	// 函数体顶层(depth1) → if 块(depth2)，q 在 if 块内声明并从 KMM 分配
	exit := enterScopeAt(t, cg, 2)
	defer exit()

	cg.AddSymbol("q", "S*", false, "local", 5, 9)
	cg.RecordAllocSizeFromAST("q", mkAllocCastExpr(16))
	if n := cg.sorInitSizes["q"]; n != 16 {
		t.Fatalf("RecordAllocSizeFromAST did not record kmm_v4_alloc(16): got %d", n)
	}

	out := sg.GenerateStatement(mkAssignStmt("G", "q"))
	t.Logf("GLOBAL_ESCAPE=%q", out)
	if !strings.Contains(out, "kmm_v4_scope_promote_p((void**)&q") {
		t.Errorf("expected cross-scope promote of q before assignment, got %q", out)
	}
	if !strings.Contains(out, "G = q") {
		t.Errorf("expected plain assignment G = q after promote, got %q", out)
	}
	if !cg.IsAssignEscaped("q") {
		t.Error("q must be marked assign-escaped to suppress its block-end survivor_free")
	}
}

// 场景二：同层赋值（两个变量声明在同一作用域）不构成逃逸，不提升。
func TestAssignEscapePromote_SameScopeNoPromote(t *testing.T) {
	cg := newCodeGeneratorForTest(t)
	sg := NewStatementGenerator(cg)

	exit := enterScopeAt(t, cg, 2)
	defer exit()

	cg.AddSymbol("a", "S*", false, "local", 5, 9)
	cg.AddSymbol("b", "S*", false, "local", 6, 9)
	cg.RecordAllocSizeFromAST("b", mkAllocCastExpr(16))

	out := sg.GenerateStatement(mkAssignStmt("a", "b"))
	t.Logf("SAME_SCOPE=%q", out)
	if strings.Contains(out, "promote") {
		t.Errorf("same-scope assignment must not promote, got %q", out)
	}
	if !strings.Contains(out, "a = b") {
		t.Errorf("expected plain assignment a = b, got %q", out)
	}
	if cg.IsAssignEscaped("b") {
		t.Error("same-scope source must not be marked assign-escaped")
	}
}

// 场景三：逃逸到更外层的局部变量（非全局）也需要提升。
func TestAssignEscapePromote_OuterLocalEscape(t *testing.T) {
	cg := newCodeGeneratorForTest(t)
	sg := NewStatementGenerator(cg)

	// 外层局部 out（depth1）
	cg.EnterScope("function")
	cg.AddSymbol("out", "S*", false, "local", 3, 5)
	// 内层块 q（depth2）
	cg.EnterScope("if_body")
	cg.AddSymbol("q", "S*", false, "local", 5, 9)
	cg.RecordAllocSizeFromAST("q", mkAllocCastExpr(16))

	out := sg.GenerateStatement(mkAssignStmt("out", "q"))
	cg.ExitScope() // if_body（保持作用域栈平衡，便于后续断言）
	cg.ExitScope() // function

	t.Logf("OUTER_LOCAL=%q", out)
	if !strings.Contains(out, "kmm_v4_scope_promote_p((void**)&q") {
		t.Errorf("expected promote when escaping to outer local, got %q", out)
	}
	if !cg.IsAssignEscaped("q") {
		t.Error("q must be marked assign-escaped (outer local holder)")
	}
}

// 场景四：值类型（int）赋值不参与指针提升。
func TestAssignEscapePromote_ValueTypesExempted(t *testing.T) {
	cg := newCodeGeneratorForTest(t)
	sg := NewStatementGenerator(cg)

	exit := enterScopeAt(t, cg, 2)
	defer exit()

	cg.AddSymbol("n", "int", false, "local", 5, 9)
	cg.AddSymbol("total", "int", false, "local", 2, 5)

	out := sg.GenerateStatement(mkAssignStmt("total", "n"))
	t.Logf("VALUE_TYPE=%q", out)
	if strings.Contains(out, "promote") {
		t.Errorf("value-type assignment must not promote, got %q", out)
	}
}

// 场景五：反向赋值（外层 → 内层）不构成逃逸——目标先于源消亡。
func TestAssignEscapePromote_ReverseDirectionNoPromote(t *testing.T) {
	cg := newCodeGeneratorForTest(t)
	sg := NewStatementGenerator(cg)

	cg.EnterScope("function")
	cg.AddSymbol("warm", "S*", false, "local", 3, 5)
	cg.RecordAllocSizeFromAST("warm", mkAllocCastExpr(16))
	cg.EnterScope("if_body")
	cg.AddSymbol("local_copy", "S*", false, "local", 5, 9)

	out := sg.GenerateStatement(mkAssignStmt("local_copy", "warm"))
	cg.ExitScope()
	cg.ExitScope()

	t.Logf("REVERSE=%q", out)
	if strings.Contains(out, "promote") {
		t.Errorf("inner-directed assignment must not promote, got %q", out)
	}
}
