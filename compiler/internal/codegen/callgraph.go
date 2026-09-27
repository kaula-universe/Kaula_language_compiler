package codegen

import "kaula/internal/ast"

// 调用图 + 递归环判定。
//
// 为什么需要它：编译期要诊断"递归 + 每层 KMM 作用域"的函数（受
// KMM_V4_MAX_SCOPE_DEPTH 约束，超出即运行期 FATAL abort）。判定条件是
// "该函数是否处于递归环中"，其中**互递归**只能靠调用图可达性发现。
//
// 曾经的实现是"每个托管函数做一次从自身出发的 DFS"，配合
// ast.Program.FindFunction 的线性查找，复杂度是单函数 O(V·E)、全程序
// O(V³)：实测 600 个函数的调用链形状下，codegen 阶段从 86ms 涨到 625ms
// （7.3 倍），且随函数数超线性恶化。
//
// 现在改为：整程序**一次性**建图 + Tarjan 求强连通分量，"是否在环内"变成
// O(1) 查表。总代价 O(V+E)，与函数数量线性相关。

// ensureCallGraph 惰性建立调用图并求出递归环节点集合（只做一次）
func (cg *CodeGenerator) ensureCallGraph() {
	if cg.callGraphReady {
		return
	}
	cg.callGraphReady = true
	cg.callGraph = make(map[string][]string)
	cg.cycleNodes = make(map[string]bool)

	prog := cg.getProgram()
	if prog == nil {
		return
	}

	// 收集所有函数节点（顶层函数 + 嵌套函数，后者会被提升为文件级函数）
	var addFunc func(fn *ast.FunctionStatement)
	addFunc = func(fn *ast.FunctionStatement) {
		if fn == nil || fn.Name == "" {
			return
		}
		if _, seen := cg.callGraph[fn.Name]; seen {
			return
		}
		callees, nested := collectCalls(fn)
		cg.callGraph[fn.Name] = callees
		// 先占位再递归，避免互相调用导致无限展开
		for _, n := range nested {
			addFunc(n)
		}
	}
	for _, stmt := range prog.Statements {
		if fn, ok := stmt.(*ast.FunctionStatement); ok {
			addFunc(fn)
		}
	}

	// Tarjan：SCC 内节点数 > 1，或存在自环 → 该节点处于递归环中
	index := make(map[string]int, len(cg.callGraph))
	lowlink := make(map[string]int, len(cg.callGraph))
	onStack := make(map[string]bool, len(cg.callGraph))
	var stack []string
	counter := 0

	var strongConnect func(v string)
	strongConnect = func(v string) {
		index[v] = counter
		lowlink[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range cg.callGraph[v] {
			if _, ok := cg.callGraph[w]; !ok {
				continue // 调用了非本程序定义的函数（std/外��）→ 无需分析
			}
			if _, seen := index[w]; !seen {
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && index[w] < lowlink[v] {
				lowlink[v] = index[w]
			}
		}
		if lowlink[v] != index[v] {
			return
		}
		// 弹出一个 SCC
		var scc []string
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		if len(scc) > 1 {
			for _, w := range scc {
				cg.cycleNodes[w] = true
			}
			return
		}
		// 单点 SCC：只有自环才算递归
		for _, w := range cg.callGraph[v] {
			if w == v {
				cg.cycleNodes[v] = true
				return
			}
		}
	}
	for v := range cg.callGraph {
		if _, seen := index[v]; !seen {
			strongConnect(v)
		}
	}
}

// InRecursionCycle 判断函数是否处于递归环中（直接自递归或互递归）。
// 首次调用时建图，之后是 O(1) 查表。
func (cg *CodeGenerator) InRecursionCycle(name string) bool {
	if name == "" {
		return false
	}
	cg.ensureCallGraph()
	return cg.cycleNodes[name]
}

// collectCalls 收集函数体内调用的函数名 + 体内定义的嵌套函数。
// 不进入 std 模块成员调用（被调者不是本程序定义的函数，建图时会被忽略）。
func collectCalls(fn *ast.FunctionStatement) (callees []string, nested []*ast.FunctionStatement) {
	seen := make(map[string]bool)
	var walkExpr func(e ast.Expression)
	var walkBody func(list []ast.Statement)
	walkExpr = func(e ast.Expression) {
		switch x := e.(type) {
		case *ast.CallExpression:
			if id, ok := x.Function.(*ast.Identifier); ok && id.Name != "" && !seen[id.Name] {
				seen[id.Name] = true
				callees = append(callees, id.Name)
			}
			for _, a := range x.Args {
				walkExpr(a)
			}
		case *ast.TypeCastExpression:
			walkExpr(x.Expression)
		case *ast.MemberAccessExpression:
			walkExpr(x.Object)
		case *ast.IndexExpression:
			walkExpr(x.Object)
			walkExpr(x.Index)
		case *ast.BinaryExpression:
			walkExpr(x.Left)
			walkExpr(x.Right)
		case *ast.UnaryExpression:
			walkExpr(x.Right)
		case *ast.ParenExpression:
			walkExpr(x.Inner)
		}
	}
	walkBody = func(list []ast.Statement) {
		for _, s := range list {
			switch x := s.(type) {
			case *ast.FunctionStatement:
				// 嵌套函数：收集为独立节点（已提升为文件级函数），
				// 但不把它的调用算到外层函数头上
				nested = append(nested, x)
			case *ast.VariableDeclaration:
				walkExpr(x.Value)
			case *ast.ExpressionStatement:
				walkExpr(x.Expression)
			case *ast.ReturnStatement:
				walkExpr(x.Value)
			case *ast.IfStatement:
				walkExpr(x.Condition)
				walkBody(x.Body)
				if x.Else != nil {
					walkBody(x.Else)
				}
			case *ast.WhileStatement:
				walkExpr(x.Condition)
				walkBody(x.Body)
			case *ast.ForStatement:
				walkBody(x.Body)
			case *ast.ForInStatement:
				walkBody(x.Body)
			case *ast.BlockStatement:
				walkBody(x.Statements)
			}
		}
	}
	walkBody(fn.Body)
	return callees, nested
}
