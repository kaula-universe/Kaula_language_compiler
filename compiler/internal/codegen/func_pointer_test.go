package codegen

import (
	"testing"

	"kaula/internal/config"
)

// TestFormatCVarDeclFuncPointer 验证函数指针类型变量声明的 C 语法：
// R (*)(args) + name → R (*name)(args)，变量名必须插入 (*) 中间。
func TestFormatCVarDeclFuncPointer(t *testing.T) {
	tests := []struct {
		cType   string
		varName string
		want    string
	}{
		{"int64_t (*)(int64_t, int64_t)", "f", "int64_t (*f)(int64_t, int64_t)"},
		{"void (*)(String)", "cb", "void (*cb)(String)"},
		{"int64_t (*)()", "g", "int64_t (*g)()"},
		// 普通类型不受影响
		{"int64_t", "x", "int64_t x"},
		{"String", "s", "String s"},
		// 数组类型不受影响
		{"uint8_t[128]", "buf", "uint8_t buf[128]"},
	}
	for _, tt := range tests {
		if got := formatCVarDecl(tt.cType, tt.varName); got != tt.want {
			t.Errorf("formatCVarDecl(%q, %q) = %q，期望 %q", tt.cType, tt.varName, got, tt.want)
		}
	}
}

// TestParseVoidSignatureFuncPointer 验证 void(T...)R 类型字符串映射为 C 函数指针。
func TestParseVoidSignatureFuncPointer(t *testing.T) {
	cg := NewCodeGenerator(config.DefaultConfig())
	tg := cg.typeGenerator
	tests := []struct {
		kaulaType string
		want      string
	}{
		{"void(int,int)int", "int64_t (*)(int64_t, int64_t)"},
		{"void(string)void", "void (*)(String)"},
		{"void()int", "int64_t (*)(void)"},
		// 无返回类型仍是数据指针 void*（既有行为）
		{"void(int)", "void*"},
	}
	for _, tt := range tests {
		got := tg.convertType(tt.kaulaType, false)
		if got != tt.want {
			t.Errorf("convertType(%q) = %q，期望 %q", tt.kaulaType, got, tt.want)
		}
	}
}
