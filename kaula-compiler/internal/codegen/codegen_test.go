package codegen

import (
	"testing"
)

func TestCodegenBasic(t *testing.T) {
	testCases := []struct {
		Name  string
		Input string
	}{
		{
			Name:  "Basic function",
			Input: "fn main() { println(42); }",
		},
		{
			Name:  "Variable declaration",
			Input: "int x = 42; println(x);",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
		})
	}
}
