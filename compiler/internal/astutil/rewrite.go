package astutil

// 本文件实现「按父节点改写子节点」的通用遍历：克隆（deep copy）、alpha 重命名
// （变量唯一化）与替换引擎都建立在它之上。
//
// 为什么不用反射：AST 节点的字段类型是强类型的（[]Statement / Expression /
// []Expression …），反射改写需要处理指针、切片、接口三层间接，且容易漏掉
// 新增节点。这里用一张 switch 把「节点的每个子槽位」显式列出来，新增节点
// 类型时忘记同步会被 TestRewriteCoversAllNodeTypes 抓住（该测试用反射比对
// 节点结构体里的 Expression/Statement 字段，要求每个可改写的槽位都能被
// rewriteChildren 触达）。

import "kaula/internal/ast"

// RewriteTransform 改写回调：返回替换后的节点。node 与替换结果的动态类型
// 必须一致（Expression 只能换成 Expression、Statement 只能换成 Statement）。
type RewriteTransform func(node ast.Node) ast.Node

func applyTransform(fn RewriteTransform, node ast.Node) ast.Node {
	if fn == nil || node == nil {
		return node
	}
	if out := fn(node); out != nil {
		return out
	}
	return node
}

// RewriteExpressionChildren 用 fn 改写表达式 e 的直接子表达式，返回新节点。
//
// 语义：
//   - 返回 nil 表示 fn 没有替换该子节点，保留原节点；
//   - 总是返回**新分配**的节点（不修改入参），因此可以安全地对共享的模板
//     AST 反复改写；
//   - 表达式节点落空（nil 子表达式）时保持 nil。
func RewriteExpressionChildren(e ast.Expression, fn RewriteTransform) ast.Expression {
	if e == nil || fn == nil {
		return e
	}
	ex := func(x ast.Expression) ast.Expression {
		if x == nil {
			return nil
		}
		if out := fn(x); out != nil {
			if ne, ok := out.(ast.Expression); ok {
				return ne
			}
		}
		return x
	}
	stmts := func(in []ast.Statement) []ast.Statement {
		return RewriteStatements(in, fn)
	}

	switch n := e.(type) {
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.CharLiteral, *ast.BooleanLiteral, *ast.SizeOfExpression, *ast.AlignOfExpression,
		*ast.OffsetOfExpression, *ast.TypeNameExpression, *ast.FieldCountExpression,
		*ast.FieldNameExpression, *ast.FieldTypeExpression, *ast.TypeKindExpression:
		return e
	case *ast.BinaryExpression:
		c := *n
		c.Left = ex(n.Left)
		c.Right = ex(n.Right)
		return &c
	case *ast.UnaryExpression:
		c := *n
		c.Right = ex(n.Right)
		return &c
	case *ast.ParenExpression:
		c := *n
		c.Inner = ex(n.Inner)
		return &c
	case *ast.ConditionalExpression:
		c := *n
		c.Condition = ex(n.Condition)
		c.TrueExpr = ex(n.TrueExpr)
		c.FalseExpr = ex(n.FalseExpr)
		return &c
	case *ast.CallExpression:
		c := *n
		c.Function = ex(n.Function)
		c.Args = rewriteExprSlice(n.Args, fn)
		return &c
	case *ast.SpawnExpression:
		c := *n
		c.Function = ex(n.Function)
		c.Args = rewriteExprSlice(n.Args, fn)
		return &c
	case *ast.AwaitExpression:
		c := *n
		c.Handle = ex(n.Handle)
		return &c
	case *ast.IndexExpression:
		c := *n
		c.Object = ex(n.Object)
		c.Index = ex(n.Index)
		return &c
	case *ast.MemberAccessExpression:
		c := *n
		c.Object = ex(n.Object)
		return &c
	case *ast.MemberExpression:
		c := *n
		c.Object = ex(n.Object)
		return &c
	case *ast.TypeCastExpression:
		c := *n
		c.Expression = ex(n.Expression)
		return &c
	case *ast.ArrayLiteral:
		c := *n
		c.Elements = rewriteExprSlice(n.Elements, fn)
		return &c
	case *ast.ObjectLiteral:
		c := *n
		c.Fields = make([]ast.ObjectLiteralField, len(n.Fields))
		for i, f := range n.Fields {
			f.Value = ex(f.Value)
			c.Fields[i] = f
		}
		return &c
	case *ast.StructLiteral:
		c := *n
		c.Fields = make([]ast.StructLiteralField, len(n.Fields))
		for i, f := range n.Fields {
			f.Value = ex(f.Value)
			c.Fields[i] = f
		}
		return &c
	case *ast.LambdaExpression:
		c := *n
		c.Body = stmts(n.Body)
		return &c
	case *ast.MatchExpression:
		c := *n
		c.Target = ex(n.Target)
		c.Arms = make([]*ast.MatchArm, len(n.Arms))
		for i, arm := range n.Arms {
			if arm == nil {
				continue
			}
			na := *arm
			na.Body = stmts(arm.Body)
			c.Arms[i] = &na
		}
		return &c
	case *ast.ComptimeExpression:
		c := *n
		c.Inner = ex(n.Inner)
		return &c
	case *ast.AttributeExpression:
		// #[name(args)] 的属性实参只随解析产生，结构改写不再下钻
		return e
	case *ast.PrefixCallExpression:
		c := *n
		c.Params = make(map[string]ast.Expression, len(n.Params))
		for k, v := range n.Params {
			c.Params[k] = ex(v)
		}
		c.VarArgs = rewriteExprSlice(n.VarArgs, fn)
		c.Body = stmts(n.Body)
		return &c
	case *ast.PrefixRepeatExpr:
		c := *n
		c.Fragment = ex(n.Fragment)
		return &c
	case *ast.LiteralExpression:
		// Value 是 interface{}：只在确实是表达式时改写
		c := *n
		if v, ok := n.Value.(ast.Expression); ok {
			c.Value = ex(v)
		}
		return &c
	default:
		return e
	}
}

// RewriteStatementChildren 用 fn 改写语句 s 的直接子语句，返回新节点。
// 与 RewriteExpressionChildren 同一套语义（不修改入参、落空保留原值）。
func RewriteStatementChildren(s ast.Statement, fn RewriteTransform) ast.Statement {
	if s == nil || fn == nil {
		return s
	}
	ex := func(x ast.Expression) ast.Expression {
		if x == nil {
			return nil
		}
		if out := fn(x); out != nil {
			if ne, ok := out.(ast.Expression); ok {
				return ne
			}
		}
		return x
	}
	stmts := func(in []ast.Statement) []ast.Statement {
		return RewriteStatements(in, fn)
	}

	switch n := s.(type) {
	case *ast.ExpressionStatement:
		c := *n
		c.Expression = ex(n.Expression)
		return &c
	case *ast.VariableDeclaration:
		// 逗号分隔的后续声明符（i64 a = 1, b = 2）递归克隆：每一层都是独立的
		// 声明节点，改写必须逐层下钻，否则 More 里的初值仍指向模板 AST。
		var copyDecl func(d *ast.VariableDeclaration) *ast.VariableDeclaration
		copyDecl = func(d *ast.VariableDeclaration) *ast.VariableDeclaration {
			nd := *d
			nd.Value = ex(d.Value)
			nd.More = nil
			for _, m := range d.More {
				if m == nil {
					continue
				}
				nd.More = append(nd.More, copyDecl(m))
			}
			return &nd
		}
		return copyDecl(n)
	case *ast.ReturnStatement:
		c := *n
		c.Value = ex(n.Value)
		return &c
	case *ast.YieldStatement:
		c := *n
		c.Source = ex(n.Source)
		return &c
	case *ast.ReleaseStatement:
		c := *n
		c.Source = ex(n.Source)
		return &c
	case *ast.ExtractStatement:
		c := *n
		c.Source = ex(n.Source)
		c.Index = ex(n.Index)
		return &c
	case *ast.IfStatement:
		c := *n
		c.Condition = ex(n.Condition)
		c.Body = stmts(n.Body)
		c.Else = stmts(n.Else)
		return &c
	case *ast.WhileStatement:
		c := *n
		c.Condition = ex(n.Condition)
		c.Body = stmts(n.Body)
		return &c
	case *ast.ForStatement:
		c := *n
		if n.Init != nil {
			c.Init = RewriteStatementChildren(n.Init, fn)
		}
		c.Condition = ex(n.Condition)
		if n.Update != nil {
			c.Update = RewriteStatementChildren(n.Update, fn)
		}
		c.Body = stmts(n.Body)
		return &c
	case *ast.ForInStatement:
		c := *n
		// 循环变量是声明位置（*Identifier），不是可替换的表达式槽位
		if n.Variable != nil {
			nv := *n.Variable
			c.Variable = &nv
		}
		c.Iterable = ex(n.Iterable)
		c.Body = stmts(n.Body)
		return &c
	case *ast.BlockStatement:
		c := *n
		c.Statements = stmts(n.Statements)
		return &c
	case *ast.CallStatement:
		c := *n
		c.Target = ex(n.Target)
		c.Body = stmts(n.Body)
		return &c
	case *ast.SpendStatement:
		c := *n
		c.Target = ex(n.Target)
		if n.Calls != nil {
			c.Calls = make([]*ast.CallClause, len(n.Calls))
			for i, cl := range n.Calls {
				if cl == nil {
					continue
				}
				nc := *cl
				nc.Index = ex(cl.Index)
				nc.Body = stmts(cl.Body)
				c.Calls[i] = &nc
			}
		}
		return &c
	case *ast.NonLocalStatement:
		c := *n
		c.Value = ex(n.Value)
		return &c
	case *ast.ObjectStatement:
		c := *n
		c.Fields = rewriteExprSlice(n.Fields, fn)
		c.Value = ex(n.Value)
		return &c
	case *ast.SwitchStatement:
		c := *n
		c.Scrutinee = ex(n.Scrutinee)
		c.Cases = make([]*ast.SwitchCase, len(n.Cases))
		for i, cs := range n.Cases {
			if cs == nil {
				continue
			}
			nc := *cs
			nc.Values = rewriteExprSlice(cs.Values, fn)
			nc.Body = stmts(cs.Body)
			c.Cases[i] = &nc
		}
		c.Default = stmts(n.Default)
		return &c
	case *ast.PrefixStatement:
		c := *n
		c.Body = stmts(n.Body)
		return &c
	case *ast.TreeStatement:
		c := *n
		c.Root = ex(n.Root)
		c.Body = stmts(n.Body)
		return &c
	case *ast.FunctionStatement:
		c := *n
		c.Body = stmts(n.Body)
		return &c
	case *ast.ClassStatement:
		c := *n
		c.Methods = nil
		for _, m := range n.Methods {
			if m == nil {
				continue
			}
			c.Methods = append(c.Methods, RewriteStatementChildren(m, fn).(*ast.MethodStatement))
		}
		c.Constructors = nil
		for _, ct := range n.Constructors {
			if ct == nil {
				continue
			}
			c.Constructors = append(c.Constructors, RewriteStatementChildren(ct, fn).(*ast.ConstructorStatement))
		}
		return &c
	case *ast.MethodStatement:
		c := *n
		c.Body = stmts(n.Body)
		return &c
	case *ast.ConstructorStatement:
		c := *n
		c.Body = stmts(n.Body)
		return &c
	default:
		return s
	}
}

// RewriteStatements 逐条改写语句序列；fn 落空的语句原样保留（不丢失）。
func RewriteStatements(in []ast.Statement, fn RewriteTransform) []ast.Statement {
	if in == nil || fn == nil {
		return in
	}
	out := make([]ast.Statement, 0, len(in))
	for _, s := range in {
		if s == nil {
			continue
		}
		r := s
		if replaced, ok := fn(s).(ast.Statement); ok && replaced != nil {
			r = replaced
		}
		out = append(out, r)
	}
	return out
}

func rewriteExprSlice(in []ast.Expression, fn RewriteTransform) []ast.Expression {
	if in == nil || fn == nil {
		return in
	}
	out := make([]ast.Expression, len(in))
	for i, e := range in {
		if e == nil {
			continue
		}
		if replaced, ok := fn(e).(ast.Expression); ok && replaced != nil {
			out[i] = replaced
		} else {
			out[i] = e
		}
	}
	return out
}

// MapExpression 深度改写表达式树：先改写子节点，再交给 fn 处理本节点。
// fn 返回 nil 表示保留本节点。入参不被修改。
func MapExpression(e ast.Expression, fn RewriteTransform) ast.Expression {
	if e == nil || fn == nil {
		return e
	}
	inner := RewriteExpressionChildren(e, func(n ast.Node) ast.Node {
		if ex, ok := n.(ast.Expression); ok {
			return MapExpression(ex, fn)
		}
		return MapStatement(n.(ast.Statement), fn)
	})
	if out, ok := fn(inner).(ast.Expression); ok && out != nil {
		return out
	}
	return inner
}

// MapStatement 深度改写语句及其全部子节点（含嵌套块、嵌套函数体）。
func MapStatement(s ast.Statement, fn RewriteTransform) ast.Statement {
	if s == nil || fn == nil {
		return s
	}
	inner := RewriteStatementChildren(s, func(n ast.Node) ast.Node {
		switch v := n.(type) {
		case ast.Expression:
			return MapExpression(v, fn)
		case ast.Statement:
			return MapStatement(v, fn)
		}
		return nil
	})
	if out, ok := fn(inner).(ast.Statement); ok && out != nil {
		return out
	}
	return inner
}

// MapStatements 深度改写语句序列。
func MapStatements(in []ast.Statement, fn RewriteTransform) []ast.Statement {
	if in == nil || fn == nil {
		return in
	}
	out := make([]ast.Statement, 0, len(in))
	for _, s := range in {
		if s == nil {
			continue
		}
		if r := MapStatement(s, fn); r != nil {
			out = append(out, r)
		}
	}
	return out
}
