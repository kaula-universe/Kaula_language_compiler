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
			name: "值套用 size_t",
			stmt: &ast.TypeAliasStatement{Name: "Size", IsAdopt: true, AdoptCType: "size_t", UnderlyingType: "size_t"},
			expected: "typedef size_t Size;",
		},
		{
			name: "值套用 int",
			stmt: &ast.TypeAliasStatement{Name: "CInt", IsAdopt: true, AdoptCType: "int", UnderlyingType: "int"},
			expected: "typedef int CInt;",
		},
		{
			name: "句柄套用 FILE*",
			stmt: &ast.TypeAliasStatement{Name: "FileHandle", IsAdopt: true, AdoptCType: "FILE*", UnderlyingType: "FILE*"},
			expected: "typedef FILE* FileHandle;",
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
