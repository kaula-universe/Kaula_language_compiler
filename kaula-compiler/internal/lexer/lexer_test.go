package lexer

import (
	"testing"
)

func TestLexerBasic(t *testing.T) {
	testCases := []struct {
		Name     string
		Input    string
		Expected string
	}{
		{
			Name:  "Basic tokens",
			Input: "fn main() { println(42); }",
			Expected: "",
		},
		{
			Name:  "Operators",
			Input: "a = b + c * d - e / f",
			Expected: "",
		},
		{
			Name:  "Comparisons",
			Input: "a == b && c != d || e < f || g > h || i <= j || k >= l",
			Expected: "",
		},
		{
			Name:  "Strings",
			Input: `"hello world"`,
			Expected: "",
		},
		{
			Name:  "Numbers",
			Input: "42 3.14",
			Expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			lex := NewLexer(tc.Input)
			for {
				tok := lex.Next()
				if tok.Type == TOKEN_EOF {
					break
				}
			}
		})
	}
}
