package sor

import (
	"kaula-compiler/internal/ast"
	"testing"
)

// ============================================================================
// extern（opaque C 函数）返回值的外部逃逸来源追踪测试
// ============================================================================

// makeExternLet 构造一条标记为 extern 调用结果的 let 语句
func makeExternLet(line int, name, fn string) Stmt {
	s := LetStmt(line, name+" = "+fn+"(...)", name, "*u8", false)
	s.IsExternCall = true
	s.FuncName = fn
	return s
}

func TestEscapeExternOrigin_YieldWarns(t *testing.T) {
	stmts := []Stmt{
		makeExternLet(1, "p", "alloc_buf"),
		YieldStmt(2, "yield p -> g", "p", "g"),
	}

	ea := NewEscapeAnalyzer()
	results := ea.AnalyzeEscape(stmts)

	// extern 返回值应被提升到 EscReturn（EscapeReturn 语义扩展到 opaque 外部函数）
	if results["p"] != EscReturn {
		t.Errorf("results[p] = %v, want %v", results["p"], EscReturn)
	}

	// 外部来源应传播到 yield 目标
	origins := ea.GetExternOrigins()
	if origins["p"] != "alloc_buf" {
		t.Errorf("origins[p] = %q, want %q", origins["p"], "alloc_buf")
	}
	if origins["g"] != "alloc_buf" {
		t.Errorf("origins[g] = %q, want %q (yield 目标应继承外部来源)", origins["g"], "alloc_buf")
	}

	// 应产生且仅产生一条浅提升警告
	warnings := ea.GetWarnings()
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	w := warnings[0]
	if w.Kind != ErrExternShallowPromote {
		t.Errorf("warning kind = %v, want ErrExternShallowPromote", w.Kind)
	}
	if w.SourceLine != 2 {
		t.Errorf("warning line = %d, want 2", w.SourceLine)
	}
	if w.ObjectID != "p" {
		t.Errorf("warning object = %q, want %q", w.ObjectID, "p")
	}
}

func TestEscapeExternOrigin_ExtractFieldInherits(t *testing.T) {
	stmts := []Stmt{
		makeExternLet(1, "s", "string_create"),
		ExtractStmt(2, "extract s.ptr -> f", "s", ".ptr", "f"),
		YieldStmt(3, "yield f -> g", "f", "g"),
	}

	ea := NewEscapeAnalyzer()
	ea.AnalyzeEscape(stmts)

	origins := ea.GetExternOrigins()
	if origins["f"] != "string_create" {
		t.Errorf("origins[f] = %q, want %q (extract 字段应继承外部来源)", origins["f"], "string_create")
	}
	if origins["g"] != "string_create" {
		t.Errorf("origins[g] = %q, want %q (经别名 yield 应继承外部来源)", origins["g"], "string_create")
	}

	// extract 与 yield 各一条警告（去重后共 2 条）
	warnings := ea.GetWarnings()
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(warnings))
	}
}

func TestEscapeExternOrigin_ReleaseWarnsAllHolders(t *testing.T) {
	stmts := []Stmt{
		makeExternLet(1, "p", "alloc_buf"),
		ReleaseStmt(2, "release p", "p", "a", "b"),
	}

	ea := NewEscapeAnalyzer()
	ea.AnalyzeEscape(stmts)

	origins := ea.GetExternOrigins()
	for _, h := range []string{"a", "b"} {
		if origins[h] != "alloc_buf" {
			t.Errorf("origins[%s] = %q, want %q", h, origins[h], "alloc_buf")
		}
	}
	if got := len(ea.GetWarnings()); got != 2 {
		t.Errorf("expected 2 warnings (one per holder), got %d", got)
	}
}

func TestEscapeNonExtern_NoWarning(t *testing.T) {
	stmts := []Stmt{
		LetStmt(1, "x = ...", "x", "[]int", true),
		YieldStmt(2, "yield x -> y", "x", "y"),
	}

	ea := NewEscapeAnalyzer()
	results := ea.AnalyzeEscape(stmts)

	if len(ea.GetWarnings()) != 0 {
		t.Errorf("non-extern flow should produce no warnings, got %d", len(ea.GetWarnings()))
	}
	if len(ea.GetExternOrigins()) != 0 {
		t.Errorf("non-extern flow should have no extern origins, got %v", ea.GetExternOrigins())
	}
	// 普通局部对象不应被强制提升为 EscReturn
	if results["x"] != EscNone {
		t.Errorf("results[x] = %v, want EscNone", results["x"])
	}
}

func TestEscapeExternOrigin_WriteRebind(t *testing.T) {
	// 赋值右值为 extern 调用：重新标记外部来源
	w := WriteStmt(2, "p = alloc_buf(...)", "p")
	w.IsExternCall = true
	w.FuncName = "alloc_buf"
	stmts := []Stmt{
		LetStmt(1, "p = ...", "p", "*u8", false),
		w,
		YieldStmt(3, "yield p -> g", "p", "g"),
	}

	ea := NewEscapeAnalyzer()
	ea.AnalyzeEscape(stmts)

	if ea.GetExternOrigins()["p"] != "alloc_buf" {
		t.Errorf("origins[p] = %q, want %q (extern 赋值应重新标记来源)", ea.GetExternOrigins()["p"], "alloc_buf")
	}
	if len(ea.GetWarnings()) != 1 {
		t.Errorf("expected 1 warning, got %d", len(ea.GetWarnings()))
	}
}

// TestAdapterExternMarking 端到端（AST -> SOR 语句 -> 逃逸分析）：
// extern 声明 + extern 调用绑定 + yield 入目标变量，应识别并警告
func TestAdapterExternMarking(t *testing.T) {
	program := &ast.Program{
		Statements: []ast.Statement{
			&ast.ExternStatement{
				Name:       "alloc_buf",
				IsFunction: true,
				ReturnType: "*u8",
			},
			&ast.FunctionStatement{
				Name: "main",
				Body: []ast.Statement{
					&ast.VariableDeclaration{
						Name: "p",
						Type: "*u8",
						Value: &ast.CallExpression{
							Function: &ast.Identifier{Name: "alloc_buf"},
							Pos:      ast.Position{Line: 3},
						},
						Pos: ast.Position{Line: 3},
					},
					&ast.YieldStatement{
						Source: &ast.Identifier{Name: "p"},
						Target: "g",
						Pos:    ast.Position{Line: 4},
					},
				},
			},
		},
	}

	adapter := NewASTProgramAdapter(program)
	stmts := adapter.GetStmts()

	// 适配器应把 extern 调用绑定标记为 IsExternCall
	found := false
	for _, s := range stmts {
		if s.Kind == StmtLet && s.VarName == "p" {
			found = true
			if !s.IsExternCall {
				t.Errorf("let p should be marked IsExternCall")
			}
			if s.FuncName != "alloc_buf" {
				t.Errorf("let p FuncName = %q, want alloc_buf", s.FuncName)
			}
		}
	}
	if !found {
		t.Fatalf("adapter did not emit let stmt for p; stmts=%v", stmts)
	}

	// 完整流水线应产出警告
	result := AnalyzeFullFromAST(program)
	if len(result.Warnings) == 0 {
		t.Fatalf("AnalyzeFullFromAST should report extern-escape warnings")
	}
	if result.Warnings[0].Kind != ErrExternShallowPromote {
		t.Errorf("warning kind = %v, want ErrExternShallowPromote", result.Warnings[0].Kind)
	}
	if result.ExternOrigins["p"] != "alloc_buf" {
		t.Errorf("ExternOrigins[p] = %q, want alloc_buf", result.ExternOrigins["p"])
	}
	// extern 来源对象逃逸级别应为 Return（强制 bump pool）
	if result.Escape["p"] != EscReturn {
		t.Errorf("Escape[p] = %v, want Return", result.Escape["p"])
	}
}

// TestAdapterNonExternCall_NotMarked 非 extern 函数调用不应被标记
func TestAdapterNonExternCall_NotMarked(t *testing.T) {
	program := &ast.Program{
		Statements: []ast.Statement{
			&ast.FunctionStatement{
				Name: "make_local",
				Body: []ast.Statement{},
			},
			&ast.FunctionStatement{
				Name: "main",
				Body: []ast.Statement{
					&ast.VariableDeclaration{
						Name: "x",
						Type: "[]int",
						Value: &ast.CallExpression{
							Function: &ast.Identifier{Name: "make_local"},
							Pos:      ast.Position{Line: 2},
						},
						Pos: ast.Position{Line: 2},
					},
				},
			},
		},
	}

	adapter := NewASTProgramAdapter(program)
	for _, s := range adapter.GetStmts() {
		if s.Kind == StmtLet && s.VarName == "x" && s.IsExternCall {
			t.Errorf("local function call should not be marked IsExternCall")
		}
	}

	result := AnalyzeFullFromAST(program)
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings for local calls, got %d", len(result.Warnings))
	}
}
