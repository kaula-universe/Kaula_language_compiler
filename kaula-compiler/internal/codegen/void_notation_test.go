package codegen

import (
	"strings"
	"testing"
)

// TestVoidNotationMapping 验证 void(T...)R 类型记法映射到 C 类型
func TestVoidNotationMapping(t *testing.T) {
	// void() 记法不需要 codegen，可直接创建 TypeGenerator
	tg := &TypeGenerator{
		clibTypeMap:   make(map[string]string),
		structTypes:   make(map[string]bool),
		activeTypeMap: nil,
	}

	tests := []struct {
		kaulaType string
		expected  string
	}{
		// 基本数据指针
		{"void()", "void*"},
		// const void() → const void* (需要递归映射修复)
		{"const void()", "const void*"},
		// 函数指针: void (*)(void*)
		{"void(void())void", "void (*)(void*)"},
		// 函数指针: void* (*)(void*)
		{"void(void())void()", "void* (*)(void*)"},
		// 比较函数: int (*)(const void*, const void*) — cint 映射为 C 的 int
		{"void(const void(), const void())cint", "int (*)(const void*, const void*)"},
	}

	for _, tt := range tests {
		got := tg.MapKaulaTypeToC(tt.kaulaType)
		t.Logf("MapKaulaTypeToC(%q) = %q (expected %q)", tt.kaulaType, got, tt.expected)
		if got != tt.expected {
			t.Errorf("MapKaulaTypeToC(%q) = %q, expected %q", tt.kaulaType, got, tt.expected)
		}
	}
}

// TestVoidNotationContains 验证函数指针映射包含正确的 C 类型片段
func TestVoidNotationContains(t *testing.T) {
	tg := &TypeGenerator{
		clibTypeMap:   make(map[string]string),
		structTypes:   make(map[string]bool),
		activeTypeMap: nil,
	}

	// void(void(), size_t)void → void (*)(void*, size_t) 或 void (*)(size_t, void*)
	got := tg.MapKaulaTypeToC("void(void(), size_t)void")
	if !strings.Contains(got, "void (*)(void*") && !strings.Contains(got, "void*), size_t") {
		t.Errorf("void(void(), size_t)void mapped to %q, expected to contain void* and size_t", got)
	}
	t.Logf("void(void(), size_t)void → %q", got)
}
