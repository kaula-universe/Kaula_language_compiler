package sema

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/errors"
)

// ---------- 测试辅助 ----------

func mkConcFn(name string, params ...string) *ast.FunctionStatement {
	fs := &ast.FunctionStatement{
		Name:         name,
		IsConcurrent: true,
		Params:       params,
		ParamTypes:   make([]string, len(params)),
		Pos:          ast.Position{Line: 1},
	}
	for i := range params {
		fs.ParamTypes[i] = "i64"
	}
	fs.ReturnType = "i64"
	fs.Body = []ast.Statement{
		&ast.ReturnStatement{
			Value: &ast.Identifier{Name: params[0]},
			Pos:   ast.Position{Line: 2},
		},
	}
	return fs
}

func mkPlainFn(name string) *ast.FunctionStatement {
	return &ast.FunctionStatement{
		Name:       name,
		ReturnType: "i64",
		Body: []ast.Statement{
			&ast.ReturnStatement{
				Value: &ast.IntegerLiteral{Value: 1},
				Pos:   ast.Position{Line: 2},
			},
		},
	}
}

func mkSpawn(target string, line int, args ...ast.Expression) *ast.SpawnExpression {
	return &ast.SpawnExpression{
		Function: &ast.Identifier{Name: target},
		Args:     args,
		Pos:      ast.Position{Line: line, Column: 1},
	}
}

// newSemaWithFns 建一个只含指定函数的分析器。
// findFunctionDeclaration 遍历 sa.program.Statements，
// 所以必须把函数塞进 program，而不是某个 map。
func newSemaWithFns(fns ...*ast.FunctionStatement) *SemanticAnalyzer {
	sa := NewSemanticAnalyzer()
	sa.program = &ast.Program{}
	for _, f := range fns {
		sa.program.Statements = append(sa.program.Statements, f)
	}
	return sa
}

// hasErrCode 检查是否报出了指定错误码
func hasErrCode(ec *errors.ErrorCollector, code string) bool {
	for _, e := range ec.GetErrorsByType(errors.ErrorSemantic) {
		if e.Code == code {
			return true
		}
	}
	return false
}

// ---------- 正常路径 ----------

func TestSpawnConcFn_Accepted(t *testing.T) {
	sa := newSemaWithFns(mkConcFn("worker", "x"))

	sa.analyzeSpawnExpression(mkSpawn("worker", 10, &ast.IntegerLiteral{Value: 1}))

	if hasErrCode(sa.errorCollector, "spawn_requires_conc_fn") {
		t.Error("spawn 一个 conc fn 不应报错")
	}
	if hasErrCode(sa.errorCollector, "spawn_target_not_found") {
		t.Error("已声明的 conc fn 不应报 not_found")
	}
}

// ---------- 普通 fn 不能 spawn ----------

func TestSpawnPlainFn_Rejected(t *testing.T) {
	sa := newSemaWithFns(mkPlainFn("helper"))

	sa.analyzeSpawnExpression(mkSpawn("helper", 10))

	if !hasErrCode(sa.errorCollector, "spawn_requires_conc_fn") {
		t.Error("spawn 普通 fn 应报 spawn_requires_conc_fn")
	}
}

// ---------- 未知目标 ----------

func TestSpawnUnknownTarget_Rejected(t *testing.T) {
	sa := newSemaWithFns()

	sa.analyzeSpawnExpression(mkSpawn("does_not_exist", 10))

	if !hasErrCode(sa.errorCollector, "spawn_target_not_found") {
		t.Error("spawn 未声明的函数应报 spawn_target_not_found")
	}
}

// ---------- 非标识符目标 ----------

func TestSpawnNonIdentifierTarget_Rejected(t *testing.T) {
	sa := newSemaWithFns()

	expr := &ast.SpawnExpression{
		Function: &ast.MemberAccessExpression{
			Object: &ast.Identifier{Name: "obj"},
			Member: "method",
		},
		Pos: ast.Position{Line: 10, Column: 1},
	}
	sa.analyzeSpawnExpression(expr)

	if !hasErrCode(sa.errorCollector, "invalid_spawn_target") {
		t.Error("spawn 方法调用应报 invalid_spawn_target")
	}
}

// ---------- 空表达式防御 ----------

func TestAnalyzeSpawnExpressionNilSafe(t *testing.T) {
	sa := newSemaWithFns()
	sa.analyzeSpawnExpression(nil)
	if sa.errorCollector.HasErrors() {
		t.Error("nil spawn 不应产生错误")
	}
}

// ---------- 无实参 ----------

func TestSpawnWithoutArgs(t *testing.T) {
	sa := newSemaWithFns(mkConcFn("noop", "x"))

	sa.analyzeSpawnExpression(mkSpawn("noop", 10))

	if hasErrCode(sa.errorCollector, "spawn_requires_conc_fn") {
		t.Error("无实参的 spawn conc fn 应通过")
	}
}

// ---------- 实参仍被分析 ----------
//
// spawn 的目标是 conc fn，但实参里的错误仍要报出来。
// 这条容易被忽略：`if isConcFn { return }` 的早退会让实参永远不被检查。

func TestSpawnArgsStillAnalyzed(t *testing.T) {
	sa := newSemaWithFns(mkConcFn("worker", "x"))

	sa.analyzeSpawnExpression(mkSpawn("worker", 10, &ast.Identifier{Name: "undeclared_zzz"}))

	if !sa.errorCollector.HasErrors() {
		t.Error("spawn 的实参应参与常规语义分析（未声明标识符应报错）")
	}
	// 目标本身合法，不应因此报 spawn 错误
	if hasErrCode(sa.errorCollector, "spawn_requires_conc_fn") ||
		hasErrCode(sa.errorCollector, "spawn_target_not_found") {
		t.Error("目标合法时不应报 spawn 目标相关错误")
	}
}