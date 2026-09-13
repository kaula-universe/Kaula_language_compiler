package sema

import (
	"kaula/internal/ast"
	"testing"
)

func TestNumericSpellingValidation(t *testing.T) {
	for _, typ := range []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "void(i32)f64", "*u8", "[8]i16"} {
		sa := NewSemanticAnalyzer()
		sa.validateNumericSpellings(&ast.Program{Statements: []ast.Statement{&ast.VariableDeclaration{Name: "value", Type: typ}}})
		if sa.ErrorCollector().HasErrors() {
			t.Errorf("canonical type rejected: %s", typ)
		}
	}
	for _, typ := range []string{"int", "uint8", "float", "double", "byte", "f8", "*int32", "[8]float64", "void(int)double"} {
		sa := NewSemanticAnalyzer()
		sa.validateNumericSpellings(&ast.Program{Statements: []ast.Statement{&ast.VariableDeclaration{Name: "value", Type: typ}}})
		if !sa.ErrorCollector().HasErrors() {
			t.Errorf("noncanonical numeric spelling accepted: %s", typ)
		}
	}
}
