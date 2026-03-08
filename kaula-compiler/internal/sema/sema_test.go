package sema

import (
	"kaula-compiler/internal/test"
	"testing"
)

func TestSemanticBasic(t *testing.T) {
	testCases := []test.TestCase{
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

	test.RunSemanticTest(t, testCases)
}
