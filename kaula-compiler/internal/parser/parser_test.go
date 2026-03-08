package parser

import (
	"kaula-compiler/internal/test"
	"testing"
)

func TestParserBasic(t *testing.T) {
	testCases := []test.TestCase{
		{
			Name:  "Basic function",
			Input: "fn main() { println(42); }",
			Expected: "",
		},
		{
			Name:  "Variable declaration",
			Input: "int x = 42;",
			Expected: "",
		},
		{
			Name:  "If statement",
			Input: `if (x > 0) { println("Positive"); } else { println("Non-positive"); }`,
			Expected: "",
		},
		{
			Name:  "While loop",
			Input: "while (x > 0) { x = x - 1; }",
			Expected: "",
		},
		{
			Name:  "For loop",
			Input: "for (int i = 0; i < 10; i = i + 1) { println(i); }",
			Expected: "",
		},
	}

	test.RunParserTest(t, testCases)
}
