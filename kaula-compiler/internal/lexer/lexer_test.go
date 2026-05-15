package lexer

import (
	"testing"
)

func TestLexerBasic(t *testing.T) {
	testCases := []struct {
		Name           string
		Input          string
		ExpectedCount  int
		ExpectedTypes  []TokenType
	}{
		{
			Name:          "Basic tokens",
			Input:         "fn main() { println(42); }",
			ExpectedCount: 11,
		},
		{
			Name:          "Operators",
			Input:         "a = b + c * d - e / f",
			ExpectedTypes: []TokenType{TOKEN_IDENT, TOKEN_ASSIGN, TOKEN_IDENT, TOKEN_PLUS, TOKEN_IDENT, TOKEN_MULTIPLY, TOKEN_IDENT, TOKEN_MINUS, TOKEN_IDENT, TOKEN_DIVIDE, TOKEN_IDENT},
		},
		{
			Name:          "Comparisons",
			Input:         "a == b && c != d || e < f || g > h || i <= j || k >= l",
			ExpectedTypes: []TokenType{TOKEN_IDENT, TOKEN_EQ, TOKEN_IDENT, TOKEN_AND, TOKEN_IDENT, TOKEN_NE, TOKEN_IDENT, TOKEN_OR, TOKEN_IDENT, TOKEN_LT, TOKEN_IDENT, TOKEN_OR, TOKEN_IDENT, TOKEN_GT, TOKEN_IDENT, TOKEN_OR, TOKEN_IDENT, TOKEN_LE, TOKEN_IDENT, TOKEN_OR, TOKEN_IDENT, TOKEN_GE, TOKEN_IDENT},
		},
		{
			Name:           "Strings",
			Input:          `"hello world"`,
			ExpectedTypes:  []TokenType{TOKEN_STRING},
		},
		{
			Name:           "Numbers",
			Input:          "42 3.14",
			ExpectedTypes:  []TokenType{TOKEN_LITERAL_INT, TOKEN_LITERAL_FLOAT},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			lex := NewLexer(tc.Input)
			tokens := []TokenType{}
			for {
				tok := lex.Next()
				if tok.Type == TOKEN_EOF {
					break
				}
				tokens = append(tokens, tok.Type)
			}
			if tc.ExpectedCount > 0 {
				if len(tokens) != tc.ExpectedCount {
					t.Fatalf("Expected %d tokens, got %d", tc.ExpectedCount, len(tokens))
				}
			}
			if tc.ExpectedTypes != nil {
				if len(tokens) != len(tc.ExpectedTypes) {
					t.Fatalf("Expected %d tokens, got %d", len(tc.ExpectedTypes), len(tokens))
				}
				for i, expected := range tc.ExpectedTypes {
					if tokens[i] != expected {
						t.Errorf("Token[%d]: expected %v, got %v", i, expected, tokens[i])
					}
				}
			}
		})
	}
}

func TestLexerEmptyInput(t *testing.T) {
	lex := NewLexer("")
	tok := lex.Next()
	if tok.Type != TOKEN_EOF {
		t.Errorf("Expected EOF token for empty input, got %v", tok.Type)
	}
}

func TestLexerWhitespace(t *testing.T) {
	lex := NewLexer("  \n\t  ")
	tok := lex.Next()
	if tok.Type != TOKEN_EOF {
		t.Errorf("Expected EOF token for whitespace input, got %v", tok.Type)
	}
}

func TestLexerComments(t *testing.T) {
	lex := NewLexer("// This is a comment\nfn main() { }")
	tokens := 0
	for {
		tok := lex.Next()
		if tok.Type == TOKEN_EOF {
			break
		}
		tokens++
	}
	if tokens == 0 {
		t.Error("Expected at least one token after comment")
	}
}

func TestLexerKeywords(t *testing.T) {
	input := "fn if else while for switch case default return import export class struct let type"
	lex := NewLexer(input)
	tokens := []TokenType{}
	for {
		tok := lex.Next()
		if tok.Type == TOKEN_EOF {
			break
		}
		tokens = append(tokens, tok.Type)
	}
	
	if len(tokens) == 0 {
		t.Error("Expected tokens for keyword input")
	}
}
