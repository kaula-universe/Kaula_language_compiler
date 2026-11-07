package opt

import (
	"fmt"

	"kaula/internal/ast"
)

/*
 * ifChainOptimizer 把「等值 if 链」改写成跳转表（SwitchStatement）。
 *
 * 识别形态：
 *
 *	if (x == c1) { B1 }
 *	else if (x == c2) { B2 }
 *	...
 *	else { Bdef }
 *
 * 其中 x 是同一个标识符，c1..cN 是编译期整型常量。改写后：
 *
 *	switch (x) { case c1: B1; case c2: B2; ... default: Bdef; }
 *
 * 收益：N 次顺序比较降为一次分派；密集整型区间由 Clang 落成真正的跳转表
 * （O(1)），稀疏区间退化为二分查找，两种情况下语义都与原 if 链等价。
 *
 * 安全守卫（任一不满足即放弃改写，保留原 AST）：
 *  1. 条件必须是 `x == <整型常量>`（含常量在左的写法、含负号常量）；
 *  2. 全链必须是同一个标识符，中间不能插入别的条件形态；
 *  3. 分支体不能含"会绑定到外层循环的" break/continue —— C 里 break 在
 *     switch 内会穿透成"跳出 switch"，与原语义不同，这是最危险的静默错误；
 *  4. 分支体含看不透的容器（spend/prefix）时放弃，避免漏判控制流；
 *  5. 重复常量标签去重（C 不允许重复 case；原链首个命中后后续本就不可达）；
 *  6. 并发/前缀/asm 函数整体跳过：它们有独立的状态机与展开机制。
 */
type ifChainOptimizer struct {
	opts      Options
	converted int
}

func (p *ifChainOptimizer) run(program *ast.Program) {
	if program == nil {
		return
	}
	p.stmtList(program.Statements)
}

// stmtList 自顶向下处理语句列表：先尝试把整条链折叠掉，再递归进子语句。
//
// 必须先尝试本层：自底向上会先把内层子链折叠掉（长度 ≥ 阈值时），
// 外层就再也看不到完整的链，导致本可合并的表被拆成嵌套 switch。
func (p *ifChainOptimizer) stmtList(list []ast.Statement) {
	for i, s := range list {
		if s == nil {
			continue
		}
		if ifs, ok := s.(*ast.IfStatement); ok {
			if sw, ok := p.trySwitch(ifs); ok {
				list[i] = sw
				p.converted++
				// 继续下钻分支体里的嵌套 if 链
				p.stmt(sw)
				continue
			}
		}
		p.stmt(s)
	}
}

// stmt 递归进语句的子语句列表（不做本层改写）。
func (p *ifChainOptimizer) stmt(s ast.Statement) {
	switch x := s.(type) {
	case *ast.FunctionStatement:
		if p.skipFunction(x) {
			return
		}
		p.stmtList(x.Body)
	case *ast.MethodStatement:
		p.stmtList(x.Body)
	case *ast.ConstructorStatement:
		p.stmtList(x.Body)
	case *ast.ClassStatement:
		for _, m := range x.Methods {
			p.stmt(m)
		}
		for _, c := range x.Constructors {
			p.stmt(c)
		}
	case *ast.IfStatement:
		p.stmtList(x.Body)
		p.stmtList(x.Else)
	case *ast.WhileStatement:
		p.stmtList(x.Body)
	case *ast.ForStatement:
		p.stmtList(x.Body)
	case *ast.ForInStatement:
		p.stmtList(x.Body)
	case *ast.BlockStatement:
		p.stmtList(x.Statements)
	case *ast.SwitchStatement:
		for _, c := range x.Cases {
			if c == nil {
				continue
			}
			p.stmtList(c.Body)
		}
		p.stmtList(x.Default)
	}
}

// skipFunction 判断该函数是否整体跳过前端优化。
func (p *ifChainOptimizer) skipFunction(fn *ast.FunctionStatement) bool {
	if fn == nil {
		return true
	}
	// asm 函数体是裸汇编文本，不是语句列表
	if fn.IsAsm {
		return true
	}
	// 并发函数有独立的守卫状态机（conc guard），改写分支形状风险高
	if fn.IsConcurrent {
		return true
	}
	// 前缀/树注解函数走独立的展开机制
	if fn.Annotation != ast.TreeAnnotationNone {
		return true
	}
	// 含挂起点（await/spawn）的函数体由状态机接管，整体跳过
	return containsSuspend(fn.Body)
}

// trySwitch 尝试把一条 if 链改写成 switch，失败返回 false（保留原 AST）。
func (p *ifChainOptimizer) trySwitch(ifs *ast.IfStatement) (*ast.SwitchStatement, bool) {
	if ifs == nil {
		return nil, false
	}

	var cases []*ast.SwitchCase
	var defBody []ast.Statement
	var scrut *ast.Identifier

	cur := ifs
	// 深度上限防止病态长链导致的线性膨胀（正常代码远达不到）
	for depth := 0; depth < 1024; depth++ {
		id, constVal, ok := eqConstOnIdent(cur.Condition)
		if !ok {
			return nil, false
		}
		if scrut == nil {
			scrut = id
		} else if scrut.Name != id.Name {
			// 换了一个被比较的变量 → 不是单一分派点，放弃
			return nil, false
		}
		cases = append(cases, &ast.SwitchCase{
			Values: []ast.Expression{constVal},
			Body:   cur.Body,
			Pos:    cur.Pos,
		})

		// 链的下一节
		if len(cur.Else) == 0 {
			break // 无 else：switch 不带 default，未命中时向下继续（与原语义一致）
		}
		if len(cur.Else) == 1 {
			if next, ok := cur.Else[0].(*ast.IfStatement); ok {
				cur = next
				continue
			}
		}
		// else 块（可能含多条语句）→ default 分支
		defBody = cur.Else
		break
	}

	if len(cases) < p.opts.MinJumpCases {
		return nil, false
	}

	// 守卫 3/4：分支体不能含会绑定到外层循环的 break/continue
	for _, c := range cases {
		if hasUnboundLoopControl(c.Body) {
			return nil, false
		}
	}
	if hasUnboundLoopControl(defBody) {
		return nil, false
	}

	// 守卫 5：去重（C 不允许重复 case 标签；原链首个命中后后续本就不可达）
	seen := make(map[string]bool, len(cases))
	dedup := make([]*ast.SwitchCase, 0, len(cases))
	for _, c := range cases {
		key, ok := constKey(c.Values[0])
		if !ok {
			return nil, false
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		dedup = append(dedup, c)
	}
	if len(dedup) < p.opts.MinJumpCases {
		return nil, false
	}

	return &ast.SwitchStatement{
		Scrutinee: scrut,
		Cases:     dedup,
		Default:   defBody,
		Pos:       ifs.Pos,
	}, true
}

// eqConstOnIdent 判断条件是否为 `x == <整型常量>`（允许常量在左侧）。
// 命中时返回那个标识符节点本身（复用原节点，保留 IsPrefixVar 等标记）与常量表达式。
func eqConstOnIdent(cond ast.Expression) (*ast.Identifier, ast.Expression, bool) {
	be, ok := cond.(*ast.BinaryExpression)
	if !ok || be == nil {
		return nil, nil, false
	}
	if be.Operator != "==" && be.Operator != "EQ" {
		return nil, nil, false
	}
	if id, ok := be.Left.(*ast.Identifier); ok && id != nil {
		if isIntConstExpr(be.Right) {
			return id, be.Right, true
		}
	}
	if id, ok := be.Right.(*ast.Identifier); ok && id != nil {
		if isIntConstExpr(be.Left) {
			return id, be.Left, true
		}
	}
	return nil, nil, false
}

// isIntConstExpr 判断是否为可作 C case 标签的编译期整型常量。
//
// 浮点字面量必须排除：C 的 case 标签要求是整型常量表达式，`case 1.5:`
// 会直接编译失败。布尔字面量同样排除（依赖 stdbool，且布尔链只有两个分支，
// 本就达不到跳转表阈值）。
func isIntConstExpr(e ast.Expression) bool {
	switch x := e.(type) {
	case *ast.IntegerLiteral:
		return x != nil
	case *ast.CharLiteral:
		return x != nil
	case *ast.UnaryExpression:
		// 负常量：-5 解析成 UnaryExpression("-", IntegerLiteral(5))
		return x != nil && x.Operator == "-" && isIntConstExpr(x.Right)
	}
	return false
}

// constKey 返回常量的规范化字符串键，用于重复 case 去重。
func constKey(e ast.Expression) (string, bool) {
	switch x := e.(type) {
	case *ast.IntegerLiteral:
		if x == nil {
			return "", false
		}
		return fmt.Sprintf("i:%d", x.Value), true
	case *ast.CharLiteral:
		if x == nil {
			return "", false
		}
		return "c:" + x.Value, true
	case *ast.UnaryExpression:
		if x == nil || x.Operator != "-" {
			return "", false
		}
		inner, ok := constKey(x.Right)
		if !ok {
			return "", false
		}
		return "n:" + inner, true
	}
	return "", false
}

/*
 * hasUnboundLoopControl 判断语句列表里是否有"会绑定到外层作用域"的
 * break/continue。
 *
 * 这类控制流是本次改写唯一的语义陷阱：原 if 链里 `if (x == c) { break; }`
 * 跳出的是外层循环；改成 switch 后同一个 break 会变成"跳出 switch"，
 * 静默改变程序行为。因此只要存在就会放弃改写。
 *
 * 循环/switch/嵌套函数内部的 break/continue 有自己的绑定目标，不算数。
 * 看不透的容器（spend/prefix，内部可能藏着语句体）一律保守判定为"有"，
 * 宁可少优化也不能改错语义。
 */
func hasUnboundLoopControl(stmts []ast.Statement) bool {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch x := s.(type) {
		case *ast.BreakStatement, *ast.ContinueStatement:
			return true
		case *ast.WhileStatement, *ast.ForStatement, *ast.ForInStatement, *ast.SwitchStatement:
			// 内层 break/continue 绑定到本构造，不影响外层
			continue
		case *ast.FunctionStatement, *ast.MethodStatement, *ast.ConstructorStatement:
			// 嵌套函数体的控制流只在函数内生效
			continue
		case *ast.IfStatement:
			if hasUnboundLoopControl(x.Body) || hasUnboundLoopControl(x.Else) {
				return true
			}
		case *ast.BlockStatement:
			if hasUnboundLoopControl(x.Statements) {
				return true
			}
		case *ast.SpendStatement, *ast.PrefixStatement:
			// 内含语句体但形态特殊，保守放弃
			return true
		}
	}
	return false
}

// containsSuspend 判断语句列表（含嵌套）是否包含挂起点 await/spawn。
func containsSuspend(stmts []ast.Statement) bool {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch x := s.(type) {
		case *ast.ExpressionStatement:
			if exprHasSuspend(x.Expression) {
				return true
			}
		case *ast.VariableDeclaration:
			if exprHasSuspend(x.Value) {
				return true
			}
		case *ast.ReturnStatement:
			if exprHasSuspend(x.Value) {
				return true
			}
		case *ast.IfStatement:
			if exprHasSuspend(x.Condition) || containsSuspend(x.Body) || containsSuspend(x.Else) {
				return true
			}
		case *ast.WhileStatement:
			if exprHasSuspend(x.Condition) || containsSuspend(x.Body) {
				return true
			}
		case *ast.ForStatement:
			if containsSuspend([]ast.Statement{x.Init}) || exprHasSuspend(x.Condition) || containsSuspend(x.Body) {
				return true
			}
		case *ast.ForInStatement:
			if containsSuspend(x.Body) {
				return true
			}
		case *ast.BlockStatement:
			if containsSuspend(x.Statements) {
				return true
			}
		case *ast.SwitchStatement:
			if exprHasSuspend(x.Scrutinee) {
				return true
			}
			for _, c := range x.Cases {
				if c == nil {
					continue
				}
				if containsSuspend(c.Body) {
					return true
				}
			}
			if containsSuspend(x.Default) {
				return true
			}
		}
	}
	return false
}

// exprHasSuspend 判断表达式树中是否含 await/spawn。
func exprHasSuspend(e ast.Expression) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *ast.AwaitExpression:
		return true
	case *ast.SpawnExpression:
		return true
	case *ast.BinaryExpression:
		return exprHasSuspend(x.Left) || exprHasSuspend(x.Right)
	case *ast.UnaryExpression:
		return exprHasSuspend(x.Right)
	case *ast.ParenExpression:
		return exprHasSuspend(x.Inner)
	case *ast.CallExpression:
		for _, a := range x.Args {
			if exprHasSuspend(a) {
				return true
			}
		}
	case *ast.IndexExpression:
		return exprHasSuspend(x.Object) || exprHasSuspend(x.Index)
	case *ast.MemberAccessExpression:
		return exprHasSuspend(x.Object)
	}
	return false
}
