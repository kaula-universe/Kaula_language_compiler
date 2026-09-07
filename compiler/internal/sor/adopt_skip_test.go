package sor

import (
	"kaula/internal/ast"
	"testing"
)

// ============================================================================
// 套用类型（adopt）不进入 SOR 借用/所有权系统测试
// ============================================================================

// TestAdoptTypeSkippedFromSOR 验证套用类型变量不会被提取为 SOR 语句
func TestAdoptTypeSkippedFromSOR(t *testing.T) {
	program := &ast.Program{
		Statements: []ast.Statement{
			// 套用类型声明
			&ast.TypeAliasStatement{
				Name:       "FileHandle",
				IsAdopt:    true,
				AdoptCType: "FILE*",
			},
			// 普通类型变量（应被 SOR 追踪）
			&ast.VariableDeclaration{
				Name: "normalVar",
				Type: "i32",
			},
			// 套用类型变量（不应被 SOR 追踪）
			&ast.VariableDeclaration{
				Name: "handle",
				Type: "FileHandle",
			},
		},
	}

	stmts := extractStmtsFromAST(program)

	// 验证：normalVar 应存在，handle 不应存在
	foundNormal := false
	foundHandle := false
	for _, s := range stmts {
		if s.Kind == StmtLet {
			if s.VarName == "normalVar" {
				foundNormal = true
			}
			if s.VarName == "handle" {
				foundHandle = true
			}
		}
	}

	if !foundNormal {
		t.Error("普通类型变量 normalVar 应被 SOR 提取，但未找到")
	}
	if foundHandle {
		t.Error("套用类型变量 handle 不应被 SOR 提取，但被提取了")
	}
}

// TestCollectAdoptTypes 验证 collectAdoptTypes 正确收集套用类型
func TestCollectAdoptTypes(t *testing.T) {
	program := &ast.Program{
		Statements: []ast.Statement{
			&ast.TypeAliasStatement{
				Name:       "Size",
				IsAdopt:    true,
				AdoptCType: "size_t",
			},
			&ast.TypeAliasStatement{
				Name:           "MyInt",
				UnderlyingType: "i32",
				IsAdopt:        false,
			},
			&ast.TypeAliasStatement{
				Name:       "Handle",
				IsAdopt:    true,
				AdoptCType: "void*",
			},
		},
	}

	adoptTypes := collectAdoptTypes(program)

	if !adoptTypes["Size"] {
		t.Error("Size 应被收集为套用类型")
	}
	if !adoptTypes["Handle"] {
		t.Error("Handle 应被收集为套用类型")
	}
	if adoptTypes["MyInt"] {
		t.Error("MyInt 不是套用类型，不应被收集")
	}
}
