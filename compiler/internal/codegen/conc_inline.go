// Kaula 并发内核 · 嵌套 conc fn 调用的 AST 层内联（Future 扁平化）
//
// 设计说明书 §2.6：conc fn 内顺序调用另一个 conc fn 时，两个状态机应
// 合并为一个（挂起点提升）。旧实现把嵌套 conc fn 调用重定向到 coercion
// shim —— shim 在调用者上下文中按"顺序阻塞"语义驱动目标（SUSPEND 时
// 就地 blocking_wait）。顺序路径（普通 fn 调用 conc fn）这是正确的直觉；
// 但 conc fn 状态机内（Worker 上执行）就是灾难：慢调用的 sleep 直接在
// Worker 上阻塞，挂起数完全失效。
//
// 本文件实现修复：**在 AST 层把 conc fn 体内的嵌套 conc fn 调用展开成
// 目标函数体**（tail-call 形态）。展开后目标的挂起点（time_sleep 等）直接
// 落入父函数体，父的挂起分析（conc.BodyAnalysis）与 io_rewrite 自然接管
// —— 生成的是真正的扁平状态机，与用户手写 inline 完全等价（实测 5.1s
// → 613ms 的等价形态）。
//
// 保守边界（不满足即不展开，退回 shim —— 语义正确只是阻塞）：
//   - 只处理 tail call（return <conc-fn-call>）与非尾调用的"赋值形态"暂
//     不支持（let x = slow(i) 需要 return 改写，见 TODO）。
//   - 只内联同文件可见、无递归（调用图无环）、函数体无嵌套函数定义、
//     形参只读的目标。
//   - 只内联"函数体含挂起点"的目标（无挂起点 conc fn 走 shim 本就不阻塞）。
package codegen

import (
	"reflect"

	"kaula/internal/ast"
	"kaula/internal/codegen/conc"
)

// inlineConcFns 对 program 内所有 conc fn 做嵌套 tail-call 内联（幂等）。
func (cg *CodeGenerator) inlineConcFns(program *ast.Program) {
	fns := collectTopLevelFns(program)
	if len(fns) == 0 {
		return
	}
	nConc := 0
	for _, fn := range fns {
		if fn.IsConcurrent {
			nConc++
		}
	}

	// 递归展开路径（环检测）
	stack := make(map[string]bool)

	for _, fn := range fns {
		if !fn.IsConcurrent {
			continue
		}
		expandBody := expandFnBody(fn, fns, stack, 0)
		if expandBody != nil {
			fn.Body = expandBody
		}
	}
}

// collectTopLevelFns 收集顶层函数（不含嵌套函数定义）。
func collectTopLevelFns(program *ast.Program) map[string]*ast.FunctionStatement {
	m := make(map[string]*ast.FunctionStatement)
	for _, st := range program.Statements {
		if fn, ok := st.(*ast.FunctionStatement); ok {
			m[fn.Name] = fn
		}
	}
	return m
}

// hasNestedFnDef 判断函数体是否含嵌套函数定义（保守：有则不内联）。
func hasNestedFnDef(body []ast.Statement) bool {
	for _, st := range body {
		if _, ok := st.(*ast.FunctionStatement); ok {
			return true
		}
		if blk, ok := st.(*ast.BlockStatement); ok && hasNestedFnDef(blk.Statements) {
			return true
		}
		if ifs, ok := st.(*ast.IfStatement); ok {
			if hasNestedFnDef(ifs.Body) || (ifs.Else != nil && hasNestedFnDef(ifs.Else)) {
				return true
			}
		}
	}
	return false
}

// bodyHasSuspendCall 递归判断函数体是否含挂起调用（std 可插桩 IO /
// blocking 标注 / 嵌套 conc fn 的挂起点）。保守：无法确定的一律视为有。
func bodyHasSuspendCall(fn *ast.FunctionStatement, fns map[string]*ast.FunctionStatement) bool {
	var scan func(stmts []ast.Statement) bool
	scan = func(stmts []ast.Statement) bool {
		for _, st := range stmts {
			if st == nil {
				continue
			}
			switch s := st.(type) {
			case *ast.ExpressionStatement:
				if exprHasSuspendCall(s.Expression, fns) {
					return true
				}
			case *ast.VariableDeclaration:
				if exprHasSuspendCall(s.Value, fns) {
					return true
				}
			case *ast.ReturnStatement:
				if exprHasSuspendCall(s.Value, fns) {
					return true
				}
			case *ast.BlockStatement:
				if scan(s.Statements) {
					return true
				}
			case *ast.IfStatement:
				if exprHasSuspendCall(s.Condition, fns) || scan(s.Body) ||
					(s.Else != nil && scan(s.Else)) {
					return true
				}
			case *ast.WhileStatement:
				if exprHasSuspendCall(s.Condition, fns) || scan(s.Body) {
					return true
				}
			case *ast.ForStatement:
				if s.Init != nil {
					if vd, ok := s.Init.(*ast.VariableDeclaration); ok && exprHasSuspendCall(vd.Value, fns) {
						return true
					}
				}
				if exprHasSuspendCall(s.Condition, fns) || scan(s.Body) {
					return true
				}
			case *ast.ForInStatement:
				if scan(s.Body) {
					return true
				}
			}
		}
		return false
	}
	return scan(fn.Body)
}

func exprHasSuspendCall(e ast.Expression, fns map[string]*ast.FunctionStatement) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *ast.CallExpression:
		name := calleeNameOf(x)
		if name == "" {
			return true // 方法调用/复杂目标：保守
		}
		k := conc.Lookup(name)
		if k != conc.SuspendNone {
			return true
		}
		// 用户 conc fn：递归查其函数体
		if target, ok := fns[name]; ok && target.IsConcurrent {
			return bodyHasSuspendCall(target, fns)
		}
		// 未知函数：保守视为挂起
		return true
	case *ast.BinaryExpression:
		return exprHasSuspendCall(x.Left, fns) || exprHasSuspendCall(x.Right, fns)
	case *ast.UnaryExpression:
		return exprHasSuspendCall(x.Right, fns)
	case *ast.IndexExpression:
		return exprHasSuspendCall(x.Index, fns) || exprHasSuspendCall(x.Object, fns)
	case *ast.MemberAccessExpression:
		return exprHasSuspendCall(x.Object, fns)
	case *ast.MemberExpression:
		return exprHasSuspendCall(x.Object, fns)
	case *ast.TypeCastExpression:
		return exprHasSuspendCall(x.Expression, fns)
	case *ast.SpawnExpression:
		return false // spawn 是并发提交，不阻塞当前函数体
	case *ast.AwaitExpression:
		return true
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral,
		*ast.StringLiteral, *ast.CharLiteral, *ast.BooleanLiteral:
		return false
	}
	return true // 未知表达式：保守
}

// calleeNameOf 提取 CallExpression 的 callee 名（仅限直接标识符或
// std 前缀成员调用形态，如 slow / std.time.time_sleep）。
func calleeNameOf(call *ast.CallExpression) string {
	if id, ok := call.Function.(*ast.Identifier); ok {
		return id.Name
	}
	// std.time.time_sleep 形态：取最后一段成员名
	if ma, ok := call.Function.(*ast.MemberAccessExpression); ok {
		if _, ok := ma.Object.(*ast.Identifier); ok {
			return ma.Member
		}
	}
	return ""
}

// paramsAssigned 检查目标函数体内形参是否被赋值（保守：有则不内联，
// 因为实参表达式替换进赋值目标语义会错）。
func paramsAssigned(fn *ast.FunctionStatement, params []string) bool {
	names := make(map[string]bool)
	for _, p := range params {
		names[p] = true
	}
	if len(names) == 0 {
		return false
	}
	var scan func(stmts []ast.Statement) bool
	scan = func(stmts []ast.Statement) bool {
		for _, st := range stmts {
			if st == nil {
				continue
			}
			switch s := st.(type) {
			case *ast.VariableDeclaration:
				if names[s.Name] {
					return true
				}
			case *ast.ExpressionStatement:
				if exprAssigns(s.Expression, names) {
					return true
				}
			case *ast.BlockStatement:
				if scan(s.Statements) {
					return true
				}
			case *ast.IfStatement:
				if scan(s.Body) || (s.Else != nil && scan(s.Else)) {
					return true
				}
			case *ast.WhileStatement:
				if scan(s.Body) {
					return true
				}
			case *ast.ForStatement:
				if scan(s.Body) {
					return true
				}
			case *ast.ForInStatement:
				if names[s.Variable.Name] {
					return true
				}
				if scan(s.Body) {
					return true
				}
			}
		}
		return false
	}
	return scan(fn.Body)
}

// exprAssigns 判断表达式是否为「对 names 中标识符的赋值写入」。
// 只认真正的赋值左值（= 及复合赋值），读（操作数/返回值）不算。
func exprAssigns(e ast.Expression, names map[string]bool) bool {
	if e == nil {
		return false
	}
	if b, ok := e.(*ast.BinaryExpression); ok {
		if isAssignOp(b.Operator) {
			return lvalueRefs(b.Left, names)
		}
		return false
	}
	return false
}

func isAssignOp(op string) bool {
	if op == "=" {
		return true
	}
	if len(op) > 1 && op[len(op)-1] == '=' {
		// 排除比较类运算符
		switch op {
		case "==", "!=", "<=", ">=":
			return false
		}
		return true
	}
	return false
}

// lvalueRefs 判断左值表达式是否引用 names 中的标识符。
func lvalueRefs(e ast.Expression, names map[string]bool) bool {
	if e == nil {
		return false
	}
	if id, ok := e.(*ast.Identifier); ok {
		return names[id.Name]
	}
	if idx, ok := e.(*ast.IndexExpression); ok {
		return lvalueRefs(idx.Object, names)
	}
	if ma, ok := e.(*ast.MemberAccessExpression); ok {
		return lvalueRefs(ma.Object, names)
	}
	return false
}

// tryExpandTailCall 尝试展开一条 return tail-call（return <conc-fn-call>）。
// 成功返回展开后的语句列表 + true；不可展开返回 nil + false（调用方保留原语句）。
func tryExpandTailCall(ret *ast.ReturnStatement,
	fns map[string]*ast.FunctionStatement, stack map[string]bool, depth int) ([]ast.Statement, bool) {

	call, isCall := ret.Value.(*ast.CallExpression)
	if !isCall {
		return nil, false
	}
	name := calleeNameOf(call)
	if name == "" {
		return nil, false
	}
	target, ok := fns[name]
	if !ok || !target.IsConcurrent {
		return nil, false
	}
	// 环检测
	if stack[name] {
		return nil, false
	}
	// 只内联有挂起点的目标（无挂起点 conc fn 走 shim 不阻塞，不展开）
	if !bodyHasSuspendCall(target, fns) {
		return nil, false
	}
	// 形参只读
	if paramsAssigned(target, target.Params) {
		return nil, false
	}
	// 实参数目匹配
	if len(call.Args) != len(target.Params) {
		return nil, false
	}

	// 展开：克隆目标函数体 + 形参替换为实参
	stack[name] = true
	body := expandFnBody(target, fns, stack, depth+1)
	stack[name] = false
	src := target.Body
	if body != nil {
		src = body
	}

	cloned := make([]ast.Statement, 0, len(src))
	for _, s := range src {
		cloned = append(cloned, cloneStatement(s).(ast.Statement))
	}

	// 形参 → 实参替换
	repl := make(map[string]ast.Expression)
	for i, p := range target.Params {
		repl[p] = call.Args[i]
	}
	if len(repl) > 0 {
		for i, s := range cloned {
			cloned[i] = replaceIdentsInStatement(s, repl)
		}
	}

	// tail call：内联体的 return 即父 return —— 直接平铺（return 语义保留）。
	return cloned, true
}

// expandStmtList 对语句列表递归展开嵌套 conc fn tail-call；
// 返回新列表与是否发生变更。语句容器（if/while/for/forin/block）递归处理。
func expandStmtList(stmts []ast.Statement,
	fns map[string]*ast.FunctionStatement, stack map[string]bool, depth int) ([]ast.Statement, bool) {

	if depth > 8 {
		return stmts, false
	}

	out := make([]ast.Statement, 0, len(stmts))
	changed := false

	for _, st := range stmts {
		if st == nil {
			out = append(out, st)
			continue
		}
		switch s := st.(type) {
		case *ast.ReturnStatement:
			if expanded, ok := tryExpandTailCall(s, fns, stack, depth); ok {
				out = append(out, expanded...)
				changed = true
				continue
			}
			out = append(out, st)

		case *ast.IfStatement:
			if body, ok := expandStmtList(s.Body, fns, stack, depth+1); ok {
				s.Body = body
				changed = true
			}
			if s.Else != nil {
				if body, ok := expandStmtList(s.Else, fns, stack, depth+1); ok {
					s.Else = body
					changed = true
				}
			}
			out = append(out, st)

		case *ast.WhileStatement:
			if body, ok := expandStmtList(s.Body, fns, stack, depth+1); ok {
				s.Body = body
				changed = true
			}
			out = append(out, st)

		case *ast.ForStatement:
			if body, ok := expandStmtList(s.Body, fns, stack, depth+1); ok {
				s.Body = body
				changed = true
			}
			out = append(out, st)

		case *ast.ForInStatement:
			if body, ok := expandStmtList(s.Body, fns, stack, depth+1); ok {
				s.Body = body
				changed = true
			}
			out = append(out, st)

		case *ast.BlockStatement:
			if body, ok := expandStmtList(s.Statements, fns, stack, depth+1); ok {
				s.Statements = body
				changed = true
			}
			out = append(out, st)

		case *ast.ExpressionStatement:
			// 非 return 形态的嵌套调用暂不展开（最小可行版只做 tail call）
			out = append(out, st)

		default:
			out = append(out, st)
		}
	}

	if !changed {
		return nil, false
	}
	return out, true
}

// expandFnBody 返回内联展开后的函数体；无可内联时返回 nil（调用方保持原样）。
func expandFnBody(fn *ast.FunctionStatement, fns map[string]*ast.FunctionStatement,
	stack map[string]bool, depth int) []ast.Statement {

	if depth > 8 {
		return nil
	}
	if hasNestedFnDef(fn.Body) {
		return nil
	}

	out, changed := expandStmtList(fn.Body, fns, stack, depth)
	if !changed {
		return nil
	}
	return out
}

// ===== 反射克隆 =====

// cloneNode 对任意 AST 节点做深拷贝（reflect 通用实现）。
func cloneNode(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			return nil
		}
		elem := cloneNode(rv.Elem().Interface())
		if elem == nil {
			return nil
		}
		np := reflect.New(rv.Elem().Type())
		np.Elem().Set(reflect.ValueOf(elem))
		return np.Interface()
	case reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return cloneNode(rv.Elem().Interface())
	case reflect.Struct:
		ns := reflect.New(rv.Type()).Elem()
		t := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Field(i)
			if !f.CanInterface() {
				ns.Field(i).Set(f)
				continue
			}
			ft := t.Field(i)
			// 匿名接口字段（Expression/Statement）：克隆具体值
			if f.Kind() == reflect.Interface {
				if f.IsNil() {
					ns.Field(i).Set(reflect.Zero(ft.Type))
					continue
				}
				cloned := cloneNode(f.Interface())
				if cloned == nil {
					ns.Field(i).Set(reflect.Zero(ft.Type))
					continue
				}
				ns.Field(i).Set(reflect.ValueOf(cloned))
				continue
			}
			switch f.Kind() {
			case reflect.Slice:
				if f.IsNil() {
					ns.Field(i).Set(reflect.Zero(ft.Type))
					continue
				}
				nsl := reflect.MakeSlice(ft.Type, f.Len(), f.Len())
				for j := 0; j < f.Len(); j++ {
					elem := f.Index(j)
					if elem.Kind() == reflect.Interface || elem.Kind() == reflect.Ptr {
						cloned := cloneNode(elem.Interface())
						if cloned == nil {
							continue
						}
						nsl.Index(j).Set(reflect.ValueOf(cloned))
					} else {
						nsl.Index(j).Set(elem)
					}
				}
				ns.Field(i).Set(nsl)
			case reflect.Ptr:
				if f.IsNil() {
					ns.Field(i).Set(reflect.Zero(ft.Type))
					continue
				}
				cloned := cloneNode(f.Interface())
				if cloned == nil {
					ns.Field(i).Set(reflect.Zero(ft.Type))
					continue
				}
				ns.Field(i).Set(reflect.ValueOf(cloned))
			default:
				ns.Field(i).Set(f)
			}
		}
		return ns.Interface()
	case reflect.Slice:
		nsl := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for j := 0; j < rv.Len(); j++ {
			elem := rv.Index(j)
			if elem.Kind() == reflect.Interface || elem.Kind() == reflect.Ptr {
				cloned := cloneNode(elem.Interface())
				if cloned == nil {
					continue
				}
				nsl.Index(j).Set(reflect.ValueOf(cloned))
			} else {
				nsl.Index(j).Set(elem)
			}
		}
		return nsl.Interface()
	case reflect.Map:
		nm := reflect.MakeMap(rv.Type())
		iter := rv.MapRange()
		for iter.Next() {
			kv := iter.Value()
			if kv.Kind() == reflect.Interface || kv.Kind() == reflect.Ptr {
				cloned := cloneNode(kv.Interface())
				if cloned == nil {
					nm.SetMapIndex(iter.Key(), reflect.Zero(rv.Type().Elem()))
					continue
				}
				nm.SetMapIndex(iter.Key(), reflect.ValueOf(cloned))
			} else {
				nm.SetMapIndex(iter.Key(), kv)
			}
		}
		return nm.Interface()
	default:
		return v
	}
}

func cloneExpression(e ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	c := cloneNode(e)
	if c == nil {
		return nil
	}
	if ex, ok := c.(ast.Expression); ok {
		return ex
	}
	return e // 兜底：克隆失败用原值（调用方保守处理）
}

func cloneStatement(s ast.Statement) ast.Statement {
	if s == nil {
		return nil
	}
	c := cloneNode(s)
	if c == nil {
		return nil
	}
	if st, ok := c.(ast.Statement); ok {
		return st
	}
	return s
}

// ===== 形参替换 =====

// replaceIdentsInStatement 把语句内匹配 names 的 Identifier 替换为实参表达式。
func replaceIdentsInStatement(s ast.Statement, repl map[string]ast.Expression) ast.Statement {
	if s == nil {
		return nil
	}
	switch x := s.(type) {
	case *ast.ReturnStatement:
		x.Value = replaceIdentsInExpr(x.Value, repl)
	case *ast.ExpressionStatement:
		x.Expression = replaceIdentsInExpr(x.Expression, repl)
	case *ast.VariableDeclaration:
		// 声明名是绑定，不替换；初值替换
		x.Value = replaceIdentsInExpr(x.Value, repl)
	case *ast.BlockStatement:
		for i, st := range x.Statements {
			x.Statements[i] = replaceIdentsInStatement(st, repl)
		}
	case *ast.IfStatement:
		x.Condition = replaceIdentsInExpr(x.Condition, repl)
		for i, st := range x.Body {
			x.Body[i] = replaceIdentsInStatement(st, repl)
		}
		for i, st := range x.Else {
			x.Else[i] = replaceIdentsInStatement(st, repl)
		}
	case *ast.WhileStatement:
		x.Condition = replaceIdentsInExpr(x.Condition, repl)
		for i, st := range x.Body {
			x.Body[i] = replaceIdentsInStatement(st, repl)
		}
	case *ast.ForStatement:
		if vd, ok := x.Init.(*ast.VariableDeclaration); ok {
			vd.Value = replaceIdentsInExpr(vd.Value, repl)
		}
		x.Condition = replaceIdentsInExpr(x.Condition, repl)
		for i, st := range x.Body {
			x.Body[i] = replaceIdentsInStatement(st, repl)
		}
	case *ast.ForInStatement:
		for i, st := range x.Body {
			x.Body[i] = replaceIdentsInStatement(st, repl)
		}
	}
	return s
}

func replaceIdentsInExpr(e ast.Expression, repl map[string]ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	switch x := e.(type) {
	case *ast.Identifier:
		if r, ok := repl[x.Name]; ok {
			return cloneExpression(r)
		}
	case *ast.CallExpression:
		x.Function = replaceIdentsInExpr(x.Function, repl)
		for i, a := range x.Args {
			x.Args[i] = replaceIdentsInExpr(a, repl)
		}
	case *ast.BinaryExpression:
		x.Left = replaceIdentsInExpr(x.Left, repl)
		x.Right = replaceIdentsInExpr(x.Right, repl)
	case *ast.UnaryExpression:
		x.Right = replaceIdentsInExpr(x.Right, repl)
	case *ast.IndexExpression:
		x.Object = replaceIdentsInExpr(x.Object, repl)
		x.Index = replaceIdentsInExpr(x.Index, repl)
	case *ast.MemberAccessExpression:
		x.Object = replaceIdentsInExpr(x.Object, repl)
	case *ast.MemberExpression:
		x.Object = replaceIdentsInExpr(x.Object, repl)
	case *ast.TypeCastExpression:
		x.Expression = replaceIdentsInExpr(x.Expression, repl)
	case *ast.SpawnExpression:
		x.Function = replaceIdentsInExpr(x.Function, repl)
		for i, a := range x.Args {
			x.Args[i] = replaceIdentsInExpr(a, repl)
		}
	case *ast.AwaitExpression:
		x.Handle = replaceIdentsInExpr(x.Handle, repl)
	}
	return e
}

