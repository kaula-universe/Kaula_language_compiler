package codegen

import (
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/config"
	"testing"
	"time"
)

/*
 * PLUS 代码生成护栏
 *
 * 历史上 generateBinaryExpression 先生成好 left/right，随后 PLUS 分支却把
 * 它们丢掉、转调 generatePlusOperation(e.Left, e.Right)，而后者内部又对两棵
 * 子树各调了一次 GenerateExpression。每棵子树被生成两次，递归下去
 * T(n) = 2·T(n-1)，n 层左结合的 + 链就是 O(2ⁿ)：
 *
 *     实测（修复前，-release）  16 → 0.20s  20 → 0.52s  24 → 5.59s
 *                              26 → 21.77s  28 → 86.94s  32 → 约 18 分钟
 *
 * 同一处绕行还顺带跳过了调用方的 astBinaryNeedsParens 括号修正，于是
 * release 模式（无溢出检查包装、裸 " + " 拼接）下会把
 *     (a | b) + c     生成成     a | b + c
 * C 按 a | (b + c) 解析，语义静默改变。debug 模式因为溢出检查把它包成
 * kaula_add_i64((int64_t)(a | b), ...)，cast 的括号恰好掩盖了这个错误，
 * 所以只有生产构建会出错 —— 最危险的一类。
 *
 * 本文件用白盒方式锁死这两点，避免回归：
 *   1. 30 层 + 链必须在 2s 内生成完毕（指数爆炸回归会立刻超时）
 *   2. 低优先级操作数必须带上括号（括号修正回归会立刻断言失败）
 */

// newPlusTestEG 构造一个仅用于表达式生成的 ExpressionGenerator。
// release=true 时关闭整数溢出检查，走裸 " + " 拼接路径 —— 括号问题只在这条
// 路径上暴露，debug 路径被 (int64_t) cast 的括号掩盖了。
func newPlusTestEG(release bool) *ExpressionGenerator {
	cfg := &config.Config{Release: release}
	return NewExpressionGenerator(NewCodeGenerator(cfg))
}

// buildLeftAssocPlusChain 构造 ((((a0 + a1) + a2) ...) + a_{n-1})，共 n-1 个 + 节点。
func buildLeftAssocPlusChain(n int) *ast.BinaryExpression {
	var e ast.Expression = &ast.Identifier{Name: "a0"}
	for i := 1; i < n; i++ {
		e = &ast.BinaryExpression{
			Left:     e,
			Operator: "PLUS",
			Right:    &ast.Identifier{Name: fmt.Sprintf("a%d", i)},
		}
	}
	return e.(*ast.BinaryExpression)
}

// TestPlusChainIsNotExponential 30 层左结合 + 链必须在线性时间内生成完。
//
// 修复前这是 2^30 量级的调用，几秒钟都跑不完；这里用 2s 的硬截止，回归时
// 立刻失败而不是把 go test 挂住。
func TestPlusChainIsNotExponential(t *testing.T) {
	for _, release := range []bool{false, true} {
		eg := newPlusTestEG(release)
		chain := buildLeftAssocPlusChain(30)

		done := make(chan string, 1)
		start := time.Now()
		go func() {
			defer func() {
				// 生成路径里的 panic 不能让测试进程崩掉，转成一次空结果，
				// 由下面的断言统一报错。
				if r := recover(); r != nil {
					done <- ""
				}
			}()
			done <- eg.generateBinaryExpression(chain)
		}()

		select {
		case got := <-done:
			if got == "" {
				t.Fatalf("release=%v: 30 层 + 链生成 panic", release)
			}
			elapsed := time.Since(start)
			if elapsed > 2*time.Second {
				t.Fatalf("release=%v: 30 层 + 链生成耗时 %v —— 疑似指数爆炸回归（应为毫秒级）",
					release, elapsed)
			}
			// 每个操作数都应该恰好出现一次：重复生成会让 a0 出现 2^29 次。
			if c := countOccurrences(got, "a0"); c != 1 {
				t.Fatalf("release=%v: a0 在输出中出现 %d 次，期望 1 次（子树被重复生成）",
					release, c)
			}
			t.Logf("release=%v: 30 层 + 链 %v，输出长度 %d", release, elapsed, len(got))
		case <-time.After(2 * time.Second):
			t.Fatalf("release=%v: 30 层 + 链生成超过 2s —— 指数爆炸回归", release)
		}
	}
}

// TestPlusOperandKeepsParentheses 低优先级操作数必须保留调用方的括号修正。
//
// 这是 release 模式下的静默语义错误护栏：C 里 | 的优先级低于 +，丢掉括号
// 会把 (a | b) + c 变成 a | (b + c)。
func TestPlusOperandKeepsParentheses(t *testing.T) {
	// (a | b) + c —— 左侧 | 优先级(4) 低于 +(10)，必须加括号
	bitOr := &ast.BinaryExpression{
		Left:     &ast.Identifier{Name: "a"},
		Operator: "BITWISE_OR",
		Right:    &ast.Identifier{Name: "b"},
	}
	plus := &ast.BinaryExpression{
		Left:     bitOr,
		Operator: "PLUS",
		Right:    &ast.Identifier{Name: "c"},
	}

	eg := newPlusTestEG(true) // release：裸 " + " 拼接，暴露括号问题
	got := eg.generateBinaryExpression(plus)
	want := "(a | b) + c"
	if got != want {
		t.Fatalf("release 模式括号修正丢失:\n  得到: %q\n  期望: %q\n  （丢括号后 C 会按 a | (b + c) 解析，语义静默改变）",
			got, want)
	}
}

// TestPlusRightOperandKeepsParentheses 右侧低优先级操作数同样需要括号。
//
// c + (a | b)：右侧判定用 innerPrec <= outerPrec，| (4) <= + (10) 成立。
func TestPlusRightOperandKeepsParentheses(t *testing.T) {
	bitOr := &ast.BinaryExpression{
		Left:     &ast.Identifier{Name: "a"},
		Operator: "BITWISE_OR",
		Right:    &ast.Identifier{Name: "b"},
	}
	plus := &ast.BinaryExpression{
		Left:     &ast.Identifier{Name: "c"},
		Operator: "PLUS",
		Right:    bitOr,
	}

	eg := newPlusTestEG(true)
	got := eg.generateBinaryExpression(plus)
	want := "c + (a | b)"
	if got != want {
		t.Fatalf("release 模式右操作数括号丢失:\n  得到: %q\n  期望: %q", got, want)
	}
}

// countOccurrences 统计子串出现次数（不重叠）。
func countOccurrences(s, sub string) int {
	if sub == "" {
		return 0
	}
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
			i += len(sub) - 1
		}
	}
	return n
}
