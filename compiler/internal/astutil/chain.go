package astutil

import "kaula/internal/ast"

// ExtractStdlibChain 从表达式中提取标准库链式调用的模块键与链段。
//
// 支持两种形式（含 freestanding 命名空间）：
//   - 平铺: std.string.string_create(...)          → ("std.string", ["string_create"])
//   - 链式: std.string.compare().ignore_case(...)  → ("std.string", ["compare", "ignore_case"])
//   - 多级: std.string.builder().append(...).free() → ("std.string", ["builder", "append", "free"])
//
// 链段拼接规则：joined = strings.Join(segments, "_")，交给 stdlib.ResolveChainedFunction
// 先按 joined 精确匹配，再用 "<模块名>_" 前缀匹配，从而把
// std.string.compare().ignore_case() 解析为 string_compare_ignore_case。
//
// 传入表达式既可以是最终调用的 CallExpression，也可以是它最外层的
// MemberAccessExpression（两种调用方式由调用方自行选择）。非标准库表达式
// （如 obj.method()、io.println 一级访问、普通函数调用）返回 ok=false，
// 由调用方回退到原有处理逻辑，保证向后兼容。
func ExtractStdlibChain(expr ast.Expression) (moduleKey string, segments []string, ok bool) {
	var members []string
	cur := expr
	for {
		switch node := cur.(type) {
		case *ast.MemberAccessExpression:
			members = append(members, node.Member)
			cur = node.Object
		case *ast.CallExpression:
			ma, isMA := node.Function.(*ast.MemberAccessExpression)
			if !isMA {
				return "", nil, false
			}
			members = append(members, ma.Member)
			cur = ma.Object
		case *ast.Identifier:
			if node.Name != "std" && node.Name != "freestanding" {
				return "", nil, false
			}
			// members 逆序收集：[fn, ..., module]，至少需要模块名 + 一个函数段
			if len(members) < 2 {
				return "", nil, false
			}
			moduleName := members[len(members)-1]
			segs := make([]string, 0, len(members)-1)
			for i := len(members) - 2; i >= 0; i-- {
				segs = append(segs, members[i])
			}
			return node.Name + "." + moduleName, segs, true
		default:
			return "", nil, false
		}
	}
}
