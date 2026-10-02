package conc

import (
	"fmt"

	"kaula/internal/ast"
)

// 跨 suspend 点的所有权校验。
//
// 背景：conc fn 的局部变量一旦跨挂起点存活就会被提升进 Frame（见 AnalyzeBody）。
// Frame 由内核在任务完成后 retire，于是「跨 suspend 存活的变量」在语义上
// 就是「跨越一次可能不再回来的生命周期边界的对象」—— 和 SOR 语境下需要
// 所有权追踪的东西是同一批。
//
// 但此前 conc lowering 完全不消费 SOR 决策：谁决定 Frame 字段的分配、谁校验
// 跨挂起点的所有权、谁检查悬垂，全靠 conc.AnalyzeBody 一家说了算，而它只回答
// 「哪些变量要进 Frame」，不回答「这些变量的所有权用法是否安全」。
//
// 这个文件补上后者。刻意保守：只报**语义上确定**有问题的情形，不做推测，
// 宁可漏报也不要误报 —— 误报会把合法的并发程序判死。

// OwnershipIssueKind 是所有权问题的种类
type OwnershipIssueKind int

const (
	// IssueUseAfterSuspendYield：变量在挂起前被 yield（所有权转移走），
	// 却在挂起之后仍被读取。挂起时 Frame 被快照并跨 Worker 边界存活，
	// 恢复后读到的是一个已被移空的槽位。
	IssueUseAfterSuspendYield OwnershipIssueKind = iota
)

func (k OwnershipIssueKind) String() string {
	switch k {
	case IssueUseAfterSuspendYield:
		return "跨挂起点的所有权违规"
	default:
		return "所有权问题"
	}
}

// OwnershipIssue 是所有权校验发现的一个问题
type OwnershipIssue struct {
	VarName string              // 涉及的变量名
	Line    int                 // 问题所在行
	Kind    OwnershipIssueKind  // 问题种类
	Message string              // 人类可读的说明
	Hint    string              // 修改建议
}

// ValidateOwnership 校验 conc fn 体内跨 suspend 存活变量的所有权安全性。
//
// 当前只覆盖一类确定性错误：yield 之后跨挂起点读取。
// 这类错误不依赖任何运行时假设 —— 语义上就是 use-after-move，
// 无论 Frame 何时回收都读不到原值。
//
// 尚未覆盖、需要先决定「Frame 字段的分配由谁裁决」才能做的方向：
//   - release 之后跨挂起点读取
//   - 挂起点之后才声明、却在挂起点之前被读取（初始化顺序倒置）
//   - 同一变量在不同 resume 分支上所有权分歧
func ValidateOwnership(fnName string, body []ast.Statement) []OwnershipIssue {
	res := AnalyzeBody(body)
	if !res.NeedsSuspend || len(res.SuspendSites) == 0 {
		// 无挂起点 = 无 Frame = 变量不出作用域，不存在跨挂起点的所有权问题
		return nil
	}

	// 按源码顺序遍历，用单调递增的位置序号给语句编号
	// （位置序号而非行号：同一行可能有多条语句，行号不足以定序）
	movedAt := make(map[string]int)   // 变量 -> 被 yield 的位置
	readAt := make(map[string][]int) // 变量 -> 被读取的位置列表
	firstSuspend := -1

	pos := 0
	var walk func(stmts []ast.Statement)
	walk = func(stmts []ast.Statement) {
		for _, st := range stmts {
			cur := pos
			pos++

			switch s := st.(type) {
			case *ast.YieldStatement:
				if src := rootName(s.Source); src != "" {
					movedAt[src] = cur
				}
				// yield 的源是被「转移走」，不是被读取，不计入 readAt

			case *ast.VariableDeclaration:
				// 初始化表达式里的挂起点由 findSuspends 负责登记，
				// 这里只记读取；声明名本身不算读取。
				recordReads(s.Value, cur, readAt)

			case *ast.ExpressionStatement:
				if firstSuspend < 0 && HasSuspendCall(s.Expression) {
					firstSuspend = cur
				}
				recordReads(s.Expression, cur, readAt)

			case *ast.ReturnStatement:
				recordReads(s.Value, cur, readAt)

			case *ast.BlockStatement:
				walk(s.Statements)

			case *ast.IfStatement:
				recordReads(s.Condition, cur, readAt)
				walk(s.Body)
				walk(s.Else)

			case *ast.WhileStatement:
				recordReads(s.Condition, cur, readAt)
				walk(s.Body)

			case *ast.ForStatement:
				recordReads(s.Condition, cur, readAt)
				walk(s.Body)

			case *ast.ForInStatement:
				walk(s.Body)

			case *ast.CallStatement:
				walk(s.Body)
			}
		}
	}
	walk(body)

	if firstSuspend < 0 {
		return nil
	}

	var issues []OwnershipIssue
	// 按变量名排序输出，保证诊断顺序稳定可复现
	for _, v := range sortedKeys(movedAt) {
		mPos := movedAt[v]
		if mPos >= firstSuspend {
			// 在挂起之后才 yield：不属于「跨挂起点的所有权违规」
			continue
		}
		for _, rPos := range readAt[v] {
			if rPos > firstSuspend {
				issues = append(issues, OwnershipIssue{
					VarName: v,
					Line:    mPos,
					Kind:    IssueUseAfterSuspendYield,
					Message: fmt.Sprintf(
						"conc fn '%s':变量 '%s' 在挂起前被 yield 转移走所有权，却在挂起之后仍被读取。"+
							"该变量跨挂起点存放在 Frame 里，恢复后读到的是已移空的槽位。",
						fnName, v),
					Hint: "请在挂起前完成读取，或 yield 之后不再使用该变量",
				})
				break
			}
		}
	}
	return issues
}

// recordReads 收集表达式里被读取的变量名
func recordReads(expr ast.Expression, at int, out map[string][]int) {
	for _, name := range readRootNames(expr) {
		out[name] = append(out[name], at)
	}
}

// readRootNames 找出表达式中作为「值」被读取的标识符。
//
// 刻意保守地不做别名 / 下标 / 成员追踪：拿不准就不记，宁可漏报。
func readRootNames(expr ast.Expression) []string {
	if expr == nil {
		return nil
	}
	var out []string
	seen := make(map[string]bool)

	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}

	var walk func(e ast.Expression)
	walk = func(e ast.Expression) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case *ast.Identifier:
			add(v.Name)

		case *ast.BinaryExpression:
			// `x = expr`：左侧是被写入，不是被读取；只跟右侧。
			// 其余二元运算（a + b）两侧都读。
			if v.Operator == "=" || v.Operator == "ASSIGN" {
				walk(v.Right)
			} else {
				walk(v.Left)
				walk(v.Right)
			}

		case *ast.UnaryExpression:
			walk(v.Right)

		case *ast.ParenExpression:
			walk(v.Inner)

		case *ast.MemberAccessExpression:
			// obj.member 读的是 obj 的内容，算对 obj 的读取
			walk(v.Object)

		case *ast.IndexExpression:
			// arr[i] 读的是 arr 的内容，算对 arr 的读取
			walk(v.Object)

		case *ast.TypeCastExpression:
			walk(v.Expression)

		case *ast.ConditionalExpression:
			walk(v.Condition)
			walk(v.TrueExpr)
			walk(v.FalseExpr)

		case *ast.CallExpression:
			for _, a := range v.Args {
				walk(a)
			}

		case *ast.AwaitExpression:
			walk(v.Handle)

		case *ast.SpawnExpression:
			for _, a := range v.Args {
				walk(a)
			}

		default:
			// 未知表达式类型：放弃，不猜
		}
	}
	walk(expr)
	return out
}

// rootName 取表达式的根标识符名，非标识符返回空串
func rootName(expr ast.Expression) string {
	if id, ok := expr.(*ast.Identifier); ok {
		return id.Name
	}
	return ""
}

// sortedKeys 返回 map 的键，按字典序 —— 保证诊断输出稳定可复现
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// 插入排序：键数量是局部变量个数，个位数，不需要引入 sort
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}