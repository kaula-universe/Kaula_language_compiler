package astutil

import (
	"testing"

	"kaula/internal/ast"
)

func TestExtractStdlibChain(t *testing.T) {
	std := &ast.Identifier{Name: "std"}
	freestanding := &ast.Identifier{Name: "freestanding"}

	// 工具函数：构造 std.<mod>.<seg> 链，seg 后带 () 则为调用
	member := func(obj ast.Expression, name string) *ast.MemberAccessExpression {
		return &ast.MemberAccessExpression{Object: obj, Member: name}
	}
	call := func(fn ast.Expression) *ast.CallExpression {
		return &ast.CallExpression{Function: fn}
	}
	callArgs := func(fn ast.Expression, args ...ast.Expression) *ast.CallExpression {
		return &ast.CallExpression{Function: fn, Args: args}
	}

	tests := []struct {
		name     string
		expr     ast.Expression
		wantMod  string
		wantSegs []string
		wantOK   bool
	}{
		{
			"flat std.string.string_create",
			member(member(std, "string"), "string_create"),
			"std.string", []string{"string_create"}, true,
		},
		{
			"chained std.string.compare().ignore_case",
			member(call(member(member(std, "string"), "compare")), "ignore_case"),
			"std.string", []string{"compare", "ignore_case"}, true,
		},
		{
			"chained with args on final call",
			callArgs(member(call(member(member(std, "string"), "compare")), "ignore_case"), &ast.Identifier{Name: "a"}, &ast.Identifier{Name: "b"}),
			"std.string", []string{"compare", "ignore_case"}, true,
		},
		{
			"three-level chain",
			member(call(member(call(member(member(std, "string"), "builder")), "append")), "free"),
			"std.string", []string{"builder", "append", "free"}, true,
		},
		{
			"freestanding flat",
			member(member(freestanding, "memory"), "std_malloc"),
			"freestanding.memory", []string{"std_malloc"}, true,
		},
		{
			"single segment",
			member(member(std, "string"), "length"),
			"std.string", []string{"length"}, true,
		},
		{
			"plain object method",
			member(&ast.Identifier{Name: "obj"}, "length"),
			"", nil, false,
		},
		{
			"one-level module io.println",
			member(&ast.Identifier{Name: "io"}, "println"),
			"", nil, false,
		},
		{
			"bare std identifier",
			std,
			"", nil, false,
		},
		{
			"std.module without function",
			member(std, "string"),
			"", nil, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod, segs, ok := ExtractStdlibChain(tt.expr)
			if ok != tt.wantOK {
				t.Fatalf("ExtractStdlibChain ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if mod != tt.wantMod {
				t.Fatalf("module = %q, want %q", mod, tt.wantMod)
			}
			if len(segs) != len(tt.wantSegs) {
				t.Fatalf("segments = %v, want %v", segs, tt.wantSegs)
			}
			for i := range segs {
				if segs[i] != tt.wantSegs[i] {
					t.Fatalf("segments = %v, want %v", segs, tt.wantSegs)
				}
			}
		})
	}
}
