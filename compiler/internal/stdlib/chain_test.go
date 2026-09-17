package stdlib

import (
	"testing"
)

func TestResolveChainedFunction(t *testing.T) {
	sc := &StdlibConfig{
		Modules: map[string]Module{
			"std.string": {
				Functions: map[string]Function{
					"string_create":             {Args: []string{"const char*"}, Return: "String"},
					"string_compare":            {Args: []string{"String", "String"}, Return: "int"},
					"string_compare_ignore_case": {Args: []string{"String", "String"}, Return: "int"},
				},
			},
			"std.io": {
				Functions: map[string]Function{
					"println":  {Args: []string{"const char*"}, VarArgs: true},
					"print_int": {Args: []string{"i64"}},
				},
			},
			"std.prefix": {
				Prefix: "pre_",
				Functions: map[string]Function{
					"pre_init": {Args: []string{}},
				},
			},
		},
	}

	tests := []struct {
		name     string
		module   string
		segments []string
		wantFn   string
		wantOK   bool
	}{
		{"single segment joined exact", "std.string", []string{"create"}, "string_create", true},
		{"multi segment with module prefix", "std.string", []string{"compare", "ignore_case"}, "string_compare_ignore_case", true},
		{"joined exact match in io", "std.io", []string{"println"}, "println", true},
		{"joined with prefix module", "std.io", []string{"print", "int"}, "print_int", true},
		{"explicit prefix field", "std.prefix", []string{"init"}, "pre_init", true},
		{"flat old-style full name", "std.string", []string{"string_create"}, "string_create", true},
		{"missing module", "std.nope", []string{"create"}, "", false},
		{"empty segments", "std.string", nil, "", false},
		{"unresolvable", "std.string", []string{"builder", "create"}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn, cName, ok := sc.ResolveChainedFunction(tt.module, tt.segments)
			if ok != tt.wantOK {
				t.Fatalf("ResolveChainedFunction(%q, %v) ok = %v, want %v", tt.module, tt.segments, ok, tt.wantOK)
			}
			if ok && cName != tt.wantFn {
				t.Fatalf("ResolveChainedFunction(%q, %v) cName = %q, want %q", tt.module, tt.segments, cName, tt.wantFn)
			}
			if ok && fn == nil {
				t.Fatalf("ResolveChainedFunction(%q, %v) returned nil Function with ok=true", tt.module, tt.segments)
			}
		})
	}
}
