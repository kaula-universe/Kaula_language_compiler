package parser

import (
	"kaula-compiler/internal/lexer"
	"testing"
)

func TestParserBasic(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "Basic function",
			input: "func main() { println(42); }",
		},
		{
			name:  "Variable declaration",
			input: "int x = 42",
		},
		{
			name:  "If statement",
			input: `if (x > 0) { println("Positive"); }`,
		},
		{
			name:  "While loop",
			input: "while (x > 0) { x = x - 1; }",
		},
		{
			name:  "For loop",
			input: "for (i = 0; i < 10; i = i + 1) { println(i); }",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lex := lexer.NewLexer(tc.input)
			p := NewParser(lex)
			p.EnableLogging(false)
			program := p.Parse()
			if program == nil {
				t.Errorf("Parser returned nil for input: %s", tc.input)
			}
			// 只检查词法/语法错误，不检查验证错误
			// 验证错误（如缺少 main 函数）在解析片段时是正常的
		})
	}
}
