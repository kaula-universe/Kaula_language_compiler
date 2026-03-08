package lexer

import (
	"kaula-compiler/internal/test"
	"testing"
)

func TestLexerBasic(t *testing.T) {
	testCases := []test.TestCase{
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

	test.RunLexerTest(t, testCases)
}
