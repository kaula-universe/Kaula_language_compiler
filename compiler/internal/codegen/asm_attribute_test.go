package codegen

import (
	"kaula/internal/ast"
	"strings"
	"testing"
)

// TestAsmAttributeOperandLists 覆盖规则：
//   - 1 个参数：只有模板
//   - 2..4 个参数：template : output : input : clobbers（保持历史行为）
//   - 5 个及以上：args[2..n-2] 全部拼成输入列表，最后一个才是破坏列表
//
// 最后一组是本次修复的重点：x86-64 syscall 需要同时约束 rdi/rsi/rdx/r10/r8/r9，
// 旧实现只取 args[2] 做 input，其余输入操作数会被静默丢弃。
func TestAsmAttributeOperandLists(t *testing.T) {
	eg := &ExpressionGenerator{typeCache: make(map[ast.Expression]string)}

	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "仅模板",
			args:     []string{`"nop"`},
			expected: `__asm__ __volatile__("nop")`,
		},
		{
			name:     "模板加输出",
			args:     []string{`"mov %1, %0"`, `"=r"(ret)`},
			expected: `__asm__ __volatile__("mov %1, %0" : "=r"(ret) :  : )`,
		},
		{
			name:     "模板输出输入",
			args:     []string{`"inb %1, %0"`, `"=a"(ret)`, `"d"(port)`},
			expected: `__asm__ __volatile__("inb %1, %0" : "=a"(ret) : "d"(port) : )`,
		},
		{
			name:     "经典四段式",
			args:     []string{`"mov %0, %1"`, `"=r"(out)`, `"r"(in)`, `"memory"`},
			expected: `__asm__ __volatile__("mov %0, %1" : "=r"(out) : "r"(in) : "memory")`,
		},
		{
			name: "多输入操作数不被丢弃",
			args: []string{
				`"syscall"`,
				`"=a"(kq_ret)`,
				`"a"(n)`, `"D"(p)`, `"S"(a2)`, `"d"(a3)`, `"r"(kq_a3)`,
				`"rcx", "r11", "memory"`,
			},
			expected: `__asm__ __volatile__("syscall" : "=a"(kq_ret) : "a"(n), "D"(p), "S"(a2), "d"(a3), "r"(kq_a3) : "rcx", "r11", "memory")`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := &ast.AttributeExpression{
				Attr: &ast.Attribute{Name: "asm", Args: tc.args},
			}
			got := eg.generateAttributeExpression(expr)
			if !strings.Contains(got, tc.expected) {
				t.Fatalf("生成结果不符合预期\n got: %s\nwant: %s", got, tc.expected)
			}
		})
	}
}

// TestAsmAttributeDropsNoOperands 显式断言多输入场景下每个操作数都出现在输出里，
// 避免将来重构时又把中间的输入操作数吃掉。
func TestAsmAttributeDropsNoOperands(t *testing.T) {
	eg := &ExpressionGenerator{typeCache: make(map[ast.Expression]string)}
	inputs := []string{`"a"(n)`, `"D"(p)`, `"S"(a2)`, `"d"(a3)`, `"r"(kq_a3)`, `"r"(kq_a4)`, `"r"(kq_a5)`}

	args := append([]string{`"syscall"`, `"=a"(ret)`}, inputs...)
	args = append(args, `"rcx", "r11", "memory"`)

	expr := &ast.AttributeExpression{Attr: &ast.Attribute{Name: "asm", Args: args}}
	got := eg.generateAttributeExpression(expr)

	for _, in := range inputs {
		if !strings.Contains(got, in) {
			t.Fatalf("输入操作数 %s 被丢弃\n got: %s", in, got)
		}
	}
}
