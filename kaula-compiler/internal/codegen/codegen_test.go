package codegen

import (
	"kaula-compiler/internal/config"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"strings"
	"testing"
)

func TestCodegenBasic(t *testing.T) {
	testCases := []struct {
		Name         string
		Input        string
		ContainsFunc string
		ContainsMain bool
	}{
		{
			Name:         "Basic function",
			Input:        "fn main() { println(42); }",
			ContainsFunc: "main",
			ContainsMain: true,
		},
		{
			Name:         "Variable declaration",
			Input:        "int x = 42; println(x);",
			ContainsFunc: "",
			ContainsMain: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			lex := lexer.NewLexer(tc.Input)
			p := parser.NewParser(lex)
			p.EnableLogging(false)
			program := p.Parse()
			if program == nil {
				t.Fatalf("Parser returned nil for input: %s", tc.Input)
			}

			cfg := config.NewConfig()
			cg := NewCodeGenerator(cfg)
			output := cg.Generate(program)

			if output == "" {
				t.Fatal("Code generator produced empty output")
			}

			if !strings.Contains(output, "#include") {
				t.Error("Generated code missing includes")
			}
		})
	}
}

func TestCodegenTypeMapping(t *testing.T) {
	tests := []struct {
		kaulaType string
		expected  string
	}{
		{"int", "int64_t"},
		{"i64", "int64_t"},
		{"i32", "int32_t"},
		{"u8", "uint8_t"},
		{"u64", "uint64_t"},
		{"float", "float"},
		{"f64", "double"},
		{"bool", "int"},
		{"char", "char"},
		{"void", "void"},
	}

	for _, tt := range tests {
		t.Run(tt.kaulaType, func(t *testing.T) {
			result := MapKaulaTypeToC(tt.kaulaType)
			if result != tt.expected {
				t.Errorf("MapKaulaTypeToC(%q) = %q, want %q", tt.kaulaType, result, tt.expected)
			}
		})
	}
}

func TestCodegenFunctionGeneration(t *testing.T) {
	input := "fn main() { println(42); }"
	lex := lexer.NewLexer(input)
	p := parser.NewParser(lex)
	p.EnableLogging(false)
	program := p.Parse()

	if program == nil {
		t.Fatal("Parser returned nil")
	}

	cfg := config.NewConfig()
	cg := NewCodeGenerator(cfg)
	output := cg.Generate(program)

	if !strings.Contains(output, "int main()") && !strings.Contains(output, "main") {
		t.Error("Generated code does not contain main function")
	}
}

func TestCodegenEmptyProgram(t *testing.T) {
	cfg := config.NewConfig()
	cg := NewCodeGenerator(cfg)

	if cg.HasErrors() {
		t.Error("NewCodeGenerator should not have errors initially")
	}
	if len(cg.Errors()) != 0 {
		t.Error("NewCodeGenerator should have empty errors initially")
	}
}
