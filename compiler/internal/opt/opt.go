// Package opt 实现 Kaula 编译器的**前端 AST 优化 pass**。
//
// 这些 pass 运行在语义分析（sema）与 SOR/逃逸分析之后、C 代码生成（codegen）
// 之前，直接改写 AST。产出的节点必须是 codegen 已支持的构造，这样优化逻辑
// 与庞大的 codegen 解耦：pass 只负责"把 AST 变成更利于分派/循环的形状"，
// 具体的 C 发射仍由 codegen 统一负责。
//
// 设计约束（零回归优先）：
//   - 任何看不透的构造一律放弃改写，保留原 AST，绝不猜测语义；
//   - pass 只在语义等价的前提下改写，不引入新的内存管理路径；
//   - 产出的节点（如 SwitchStatement）与原来 if 分支的作用域/KMM 包裹保持同构。
package opt

import (
	"os"

	"kaula/internal/ast"
)

// Options 前端优化开关。
type Options struct {
	// EnableJumpTable 是否启用「if 链转跳转表」。
	EnableJumpTable bool
	// MinJumpCases 触发跳转表改写所需的最少等值 case 数。
	// 过短的链（1~2 个）改写成 switch 没有收益，还会让 C 侧代码更难读。
	MinJumpCases int
}

// DefaultOptions 返回默认开关：启用跳转表，最少 3 个 case。
func DefaultOptions() Options {
	return Options{
		EnableJumpTable: true,
		MinJumpCases:    3,
	}
}

// Result 一次优化运行的统计，供构建日志/诊断使用。
type Result struct {
	// JumpTables 被改写成跳转表的 if 链数量。
	JumpTables int
}

// Enabled 返回本轮是否应当运行前端优化。
//
// 与后端优化级别对齐：-O0 表示"不要优化"，此时前端 pass 也一并停掉，
// 方便用 -O0 对比「有无前端优化」的行为差异（排障时非常有用）。
// 另外提供 KAULA_OPT_DISABLE=1 环境变量强行关闭，不必改命令行即可二分定位。
func Enabled(optLevel string) bool {
	if os.Getenv("KAULA_OPT_DISABLE") != "" {
		return false
	}
	return optLevel != "-O0" && optLevel != "O0"
}

// OptimizeProgram 对整棵 AST 运行全部已启用的前端优化 pass。
func OptimizeProgram(program *ast.Program, opts Options) Result {
	var res Result
	if program == nil || !opts.EnableJumpTable {
		return res
	}
	if opts.MinJumpCases <= 0 {
		opts.MinJumpCases = DefaultOptions().MinJumpCases
	}

	p := &ifChainOptimizer{opts: opts}
	p.run(program)
	res.JumpTables = p.converted
	return res
}
