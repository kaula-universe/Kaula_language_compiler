package sema

import (
	"testing"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
)

func TestSemanticBasic(t *testing.T) {
	testCases := []struct {
		Name     string
		Input    string
		Expected string
	}{
		{
			Name:  "Basic function",
			Input: "fn main() { println(42); }",
			Expected: "",
		},
		{
			Name:  "Variable declaration",
			Input: "int x = 42; println(x);",
			Expected: "",
		},
		{
			Name:  "Nullable variable",
			Input: "int? x = null; if (x != null) { println(*x); }",
			Expected: "",
		},
		{
			Name:  "Function with parameters",
			Input: "fn add(a, b) { return a + b; } fn main() { println(add(1, 2)); }",
			Expected: "",
		},
		{
			Name:  "Scoping",
			Input: "int x = 42; { int x = 100; println(x); } println(x);",
			Expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			lex := lexer.NewLexer(tc.Input)
			p := parser.NewParser(lex)
			program := p.Parse()
			
			analyzer := NewSemanticAnalyzer()
			analyzer.Analyze(program)
			
			if analyzer.errorCollector.HasErrors() {
				t.Errorf("Semantic analysis failed: %v", analyzer.errorCollector.Errors())
			}
		})
	}
}
