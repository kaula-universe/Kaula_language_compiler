package codegen

import (
	"kaula/internal/ast"
	"strings"
	"testing"
)

func TestAdoptTypeGeneration(t *testing.T) {
	tg := &TypeGenerator{
		clibTypeMap:   make(map[string]string),
		structTypes:   make(map[string]bool),
		classTypes:    make(map[string]bool),
		enumTypes:     make(map[string]bool),
		typeAliasTypes: make(map[string]bool),
	}

	tests := []struct {
		name     string
		stmt     *ast.TypeAliasStatement
		expected string
	}{
		{
			name: "外部 C 类型 size_t（编译器自动推断透传）",
			stmt: &ast.TypeAliasStatement{Name: "Size", IsAdopt: true, AdoptUnderlying: "size_t", UnderlyingType: "size_t"},
			expected: "typedef size_t Size;",
		},
		{
			name: "外部 C 类型 int",
			stmt: &ast.TypeAliasStatement{Name: "CInt", IsAdopt: true, AdoptUnderlying: "int", UnderlyingType: "int"},
			expected: "typedef int CInt;",
		},
		{
			name: "外部 C 指针类型 FILE*",
			stmt: &ast.TypeAliasStatement{Name: "FileHandle", IsAdopt: true, AdoptUnderlying: "FILE*", UnderlyingType: "FILE*"},
			expected: "typedef FILE* FileHandle;",
		},
		{
			name: "Kaula 数值类型 i64（走 Kaula 类型映射）",
			stmt: &ast.TypeAliasStatement{Name: "Count", IsAdopt: true, AdoptUnderlying: "i64", UnderlyingType: "i64"},
			expected: "typedef int64_t Count;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tg.GenerateTypeAliasStatement(tt.stmt)
			t.Logf("生成:\n%s", got)
			if !strings.Contains(got, tt.expected) {
				t.Errorf("期望包含 %q, 实际:\n%s", tt.expected, got)
			}
			if !tg.typeAliasTypes[tt.stmt.Name] {
				t.Errorf("类型 %s 未注册到 typeAliasTypes", tt.stmt.Name)
			}
		})
	}
}

func TestAdoptUnwrapNoOp(t *testing.T) {
	cg := &CodeGenerator{pluginManager: NewPluginManager()}
	eg := NewExpressionGenerator(cg)
	// adopt(42) 应直接输出 42（no-op，用整数字面量避免 symbolTable 依赖）
	adoptCall := &ast.CallExpression{
		Function: &ast.Identifier{Name: "adopt"},
		Args:     []ast.Expression{&ast.IntegerLiteral{Value: 42}},
	}
	got := eg.generateCallExpression(adoptCall)
	if got != "42" {
		t.Errorf("adopt(42) 期望 '42', 实际 %q", got)
	}

	// unwrap(42) 应直接输出 42（no-op）
	unwrapCall := &ast.CallExpression{
		Function: &ast.Identifier{Name: "unwrap"},
		Args:     []ast.Expression{&ast.IntegerLiteral{Value: 42}},
	}
	got = eg.generateCallExpression(unwrapCall)
	if got != "42" {
		t.Errorf("unwrap(42) 期望 '42', 实际 %q", got)
	}
}
