package conc

import (
	"strings"
	"testing"

	"kaula/internal/ast"
)

// 构造一个「先 yield 掉x， 挂起，再读 x」的 conc fn 体
func mkYieldAcrossSuspend() []ast.Statement {
	return []ast.Statement{
		// buf 被声明（此处用占位名字，代表任意聚合对象）
		&ast.VariableDeclaration{
			Name:  "buf",
			Value: &ast.Identifier{Name: "buf", Pos: ast.Position{Line: 2}},
			Pos:   ast.Position{Line: 2},
		},
		// yield buf -> owner：所有权转移走
		&ast.YieldStatement{
			Source: &ast.Identifier{Name: "buf", Pos: ast.Position{Line: 3}},
			Target: "owner",
			Pos:    ast.Position{Line: 3},
		},
		// 挂起点
		&ast.ExpressionStatement{
			Expression: &ast.CallExpression{
				Function: &ast.Identifier{Name: "time_sleep", Pos: ast.Position{Line: 4}},
				Args:     []ast.Expression{&ast.IntegerLiteral{Value: 1, Pos: ast.Position{Line: 4}}},
				Pos:      ast.Position{Line: 4},
			},
			Pos: ast.Position{Line: 4},
		},
		// 挂起之后仍读取 buf → 应报错
		&ast.ExpressionStatement{
			Expression: &ast.Identifier{Name: "buf", Pos: ast.Position{Line: 5}},
			Pos:        ast.Position{Line: 5},
		},
	}
}

// 该形态确定违规：yield 之后跨挂起点读取
func TestOwnership_YieldThenReadAfterSuspend(t *testing.T) {
	issues := ValidateOwnership("worker", mkYieldAcrossSuspend())
	if len(issues) == 0 {
		t.Fatal("yield 后跨挂起点读取应当被检出")
	}
	found := false
	for _, is := range issues {
		if is.VarName == "buf" && is.Kind == IssueUseAfterSuspendYield {
			found = true
		}
	}
	if !found {
		t.Errorf("应报告 buf 的跨挂起点所有权违规，实得: %+v", issues)
	}
}

// 挂起之前就读完 buf，之后不再使用 → 合法，不该报
func TestOwnership_ReadBeforeSuspendThenYieldIsFine(t *testing.T) {
	body := []ast.Statement{
		&ast.VariableDeclaration{Name: "buf", Pos: ast.Position{Line: 2}},
		// 挂起前读取
		&ast.ExpressionStatement{
			Expression: &ast.Identifier{Name: "buf", Pos: ast.Position{Line: 3}},
			Pos:        ast.Position{Line: 3},
		},
		// 挂起
		&ast.ExpressionStatement{
			Expression: &ast.CallExpression{
				Function: &ast.Identifier{Name: "time_sleep"},
				Args:     []ast.Expression{&ast.IntegerLiteral{Value: 1}},
			},
			Pos: ast.Position{Line: 4},
		},
		// 挂起后才 yield，且不再读回
		&ast.YieldStatement{
			Source: &ast.Identifier{Name: "buf"},
			Target: "owner",
			Pos:    ast.Position{Line: 5},
		},
	}
	if issues := ValidateOwnership("worker", body); len(issues) != 0 {
		t.Errorf("挂起前读完、挂起后才 yield 不该报错，实得: %+v", issues)
	}
}

// 无挂起点的 conc fn（单一形态）没有 Frame，不存在跨挂起点问题
func TestOwnership_NoSuspendNoFrame(t *testing.T) {
	body := []ast.Statement{
		&ast.VariableDeclaration{Name: "buf", Pos: ast.Position{Line: 2}},
		&ast.YieldStatement{
			Source: &ast.Identifier{Name: "buf"},
			Target: "owner",
			Pos:    ast.Position{Line: 3},
		},
		// 无挂起点，但之后仍读 buf
		&ast.ExpressionStatement{
			Expression: &ast.Identifier{Name: "buf"},
			Pos:        ast.Position{Line: 4},
		},
	}
	if issues := ValidateOwnership("worker", body); len(issues) != 0 {
		t.Errorf("无挂起点（无 Frame）不该报跨挂起点问题，实得: %+v", issues)
	}
}

// 赋值左侧不是「读取」：`buf = other` 不应被当成读取 buf
func TestOwnership_AssignmentTargetNotARead(t *testing.T) {
	body := []ast.Statement{
		&ast.VariableDeclaration{Name: "buf", Pos: ast.Position{Line: 2}},
		&ast.YieldStatement{
			Source: &ast.Identifier{Name: "buf"},
			Target: "owner",
			Pos:    ast.Position{Line: 3},
		},
		&ast.ExpressionStatement{
			Expression: &ast.CallExpression{
				Function: &ast.Identifier{Name: "time_sleep"},
				Args:     []ast.Expression{&ast.IntegerLiteral{Value: 1}},
			},
			Pos: ast.Position{Line: 4},
		},
		// buf = 0 是写入，不是读取，不应触发 use-after-move
		&ast.ExpressionStatement{
			Expression: &ast.BinaryExpression{
				Left:     &ast.Identifier{Name: "buf"},
				Operator: "=",
				Right:    &ast.IntegerLiteral{Value: 0},
			},
			Pos: ast.Position{Line: 5},
		},
	}
	if issues := ValidateOwnership("worker", body); len(issues) != 0 {
		t.Errorf("仅对 buf 赋值不应触发 use-after-move，实得: %+v", issues)
	}
}

// 诊断信息要可读：含变量名与建议
func TestOwnership_IssueMessageIsActionable(t *testing.T) {
	issues := ValidateOwnership("worker", mkYieldAcrossSuspend())
	if len(issues) == 0 {
		t.Fatal("应有问题")
	}
	msg := issues[0].Message
	for _, want := range []string{"buf", "yield", "worker"} {
		if !strings.Contains(msg, want) {
			t.Errorf("诊断信息缺少 %q: %s", want, msg)
		}
	}
	if issues[0].Hint == "" {
		t.Error("应给出修改建议")
	}
}

// 多个变量同时违规：诊断顺序应稳定（不依赖 map 随机序）
func TestOwnership_DiagnosticOrderIsStable(t *testing.T) {
	body := []ast.Statement{
		&ast.VariableDeclaration{Name: "zeta", Pos: ast.Position{Line: 2}},
		&ast.VariableDeclaration{Name: "alpha", Pos: ast.Position{Line: 3}},
		&ast.YieldStatement{Source: &ast.Identifier{Name: "zeta"}, Target: "o1", Pos: ast.Position{Line: 4}},
		&ast.YieldStatement{Source: &ast.Identifier{Name: "alpha"}, Target: "o2", Pos: ast.Position{Line: 5}},
		&ast.ExpressionStatement{
			Expression: &ast.CallExpression{
				Function: &ast.Identifier{Name: "time_sleep"},
				Args:     []ast.Expression{&ast.IntegerLiteral{Value: 1}},
			},
			Pos: ast.Position{Line: 6},
		},
		&ast.ExpressionStatement{Expression: &ast.Identifier{Name: "zeta"}, Pos: ast.Position{Line: 7}},
		&ast.ExpressionStatement{Expression: &ast.Identifier{Name: "alpha"}, Pos: ast.Position{Line: 8}},
	}

	var first []string
	for run := 0; run < 5; run++ {
		issues := ValidateOwnership("worker", body)
		var got []string
		for _, is := range issues {
			got = append(got, is.VarName)
		}
		if run == 0 {
			first = got
			continue
		}
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("诊断顺序不稳定: %v vs %v", got, first)
		}
	}
	// 应按字典序：alpha 在 zeta 之前
	if len(first) != 2 || first[0] != "alpha" || first[1] != "zeta" {
		t.Errorf("期望按字典序 [alpha zeta]，实得 %v", first)
	}
}