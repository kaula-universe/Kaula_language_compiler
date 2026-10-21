package astutil

import (
	"regexp"
	"sort"
	"strconv"

	"kaula/internal/ast"
)

// AlphaRenameTemplate 结构宏卫生性：把模板体内的**局部变量声明**重命名为
// 展开唯一的名，返回改写后的语句序列、重命名表与按新名索引的声明表。
//
// 为什么不改 $参数：`$name` 是显式参数通道，语义由调用点实参决定，重命名会
// 切断绑定。这里只处理"模板自己声明的临时变量"——它们才是变量捕获的来源：
//
//	prefix Swap {
//	    $a
//	    $b
//	    i64 tmp = $a     // 宏的临时变量
//	    $a = $b
//	    $b = tmp
//	}
//	fn main() { i64 tmp = 99; @Swap(a = x, b = y) }   // 旧实现里 tmp 被宏踩掉
//
// 展开为 `i64 __kaula_m0_tmp = x;`，调用方的 tmp 不被触碰。
//
// 作用域规则（与语言本身一致）：
//   - 只有**本层**声明的变量参与重命名，并沿嵌套块向下生效；
//   - 内层重新声明同名变量时遮蔽外层映射（内层换用自己的唯一名）；
//   - 嵌套函数 / lambda 的参数与体内声明各自独立（函数边界重置映射）；
//   - 裸 `$param` 语句与 `$param = 默认值` 是参数声明，不参与重命名。
//
// 名字格式 `__kaula_m<序号>_<原名>`：保留原名便于阅读生成代码与调试；
// 前缀含双下划线且带序号，与用户标识符的碰撞概率可忽略，且不依赖调用点
// 作用域（同一模板在任意上下文展开都得到相同的唯一名，便于缓存与对比）。
func AlphaRenameTemplate(stmts []ast.Statement, seq int) ([]ast.Statement, map[string]string, map[string]*ast.VariableDeclaration) {
	decls := make(map[string]*ast.VariableDeclaration)
	if stmts == nil {
		return nil, map[string]string{}, decls
	}
	r := &alphaRenamer{seq: seq, decls: decls}
	out := r.rewriteStmts(stmts, nil)
	return out, r.renames, decls
}

// MacroLocalName 生成宏展开局部变量名（供测试与诊断复用）
func MacroLocalName(seq int, name string) string {
	return "__kaula_m" + strconv.Itoa(seq) + "_" + name
}

type alphaRenamer struct {
	seq     int
	decls   map[string]*ast.VariableDeclaration
	renames map[string]string
}

func (r *alphaRenamer) renameTable() map[string]string {
	if r.renames == nil {
		r.renames = make(map[string]string)
	}
	return r.renames
}

// rewriteStmts 在同一绑定环境下处理一组语句。
//
// 顺序语义：C/Kaula 的块内声明必须先于使用才可见，但宏展开后语句顺序与模板
// 一致，所以「先扫描本层全部声明建立映射，再改写整个块」是安全的下近似：
// 声明必然出现在使用之前，提前建立映射不会改变语义，反而让 `i64 a = 1, b = a`
// 这种同行互相引用也能正确改写。
func (r *alphaRenamer) rewriteStmts(stmts []ast.Statement, env map[string]string) []ast.Statement {
	local := make(map[string]string, len(env))
	for k, v := range env {
		local[k] = v
	}
	for _, s := range stmts {
		r.collectDecls(s, local)
	}
	out := make([]ast.Statement, 0, len(stmts))
	for _, s := range stmts {
		if s == nil {
			continue
		}
		out = append(out, r.rewriteStmt(s, local))
	}
	return out
}

// collectDecls 收集本层声明（不进入嵌套块：块内声明由块自己处理）
func (r *alphaRenamer) collectDecls(s ast.Statement, local map[string]string) {
	switch n := s.(type) {
	case *ast.VariableDeclaration:
		for _, d := range n.AllDeclarators() {
			if d == nil || d.Name == "" || d.IsStatic || d.IsConst {
				// static / const 通常是宏有意外露的配置点，保持原名
				continue
			}
			if _, done := r.renameTable()[d.Name]; done {
				continue
			}
			r.renameTable()[d.Name] = MacroLocalName(r.seq, d.Name)
		}
		// 映射按声明逐个写入 local（外层已重命名过的同名变量：内层声明遮蔽）
		for _, d := range n.AllDeclarators() {
			if d == nil || d.Name == "" || d.IsStatic || d.IsConst {
				continue
			}
			local[d.Name] = r.renames[d.Name]
			r.decls[r.renames[d.Name]] = d
		}
	case *ast.ForInStatement:
		if id := n.Variable; id != nil && id.Name != "" {
			r.renameTable()[id.Name] = MacroLocalName(r.seq, id.Name)
			local[id.Name] = r.renames[id.Name]
		}
	}
}

func (r *alphaRenamer) rewriteStmt(s ast.Statement, local map[string]string) ast.Statement {
	if s == nil {
		return nil
	}
	// 嵌套块 / 函数 / 类方法体：块内声明对块外不可见，但块外映射向下生效
	sub := func(body []ast.Statement) []ast.Statement {
		return r.rewriteStmts(body, local)
	}
	// 嵌套函数 / lambda：函数边界，映射重置（参数与体内声明完全独立）
	funcBody := func(body []ast.Statement) []ast.Statement {
		return r.rewriteStmts(body, nil)
	}

	switch n := s.(type) {
	case *ast.VariableDeclaration:
		return r.rewriteDecl(n, local)
	case *ast.ExpressionStatement:
		c := *n
		c.Expression = r.rewriteExpr(n.Expression, local)
		return &c
	case *ast.ReturnStatement:
		c := *n
		c.Value = r.rewriteExpr(n.Value, local)
		return &c
	case *ast.YieldStatement:
		c := *n
		c.Source = r.rewriteExpr(n.Source, local)
		return &c
	case *ast.ReleaseStatement:
		c := *n
		c.Source = r.rewriteExpr(n.Source, local)
		return &c
	case *ast.ExtractStatement:
		c := *n
		c.Source = r.rewriteExpr(n.Source, local)
		c.Index = r.rewriteExpr(n.Index, local)
		return &c
	case *ast.IfStatement:
		c := *n
		c.Condition = r.rewriteExpr(n.Condition, local)
		c.Body = sub(n.Body)
		c.Else = sub(n.Else)
		return &c
	case *ast.WhileStatement:
		c := *n
		c.Condition = r.rewriteExpr(n.Condition, local)
		c.Body = sub(n.Body)
		return &c
	case *ast.ForStatement:
		c := *n
		inner := sub(appendNonNil(nil, n.Init))
		if len(inner) == 1 {
			c.Init = inner[0]
		}
		c.Condition = r.rewriteExpr(n.Condition, local)
		upd := sub(appendNonNil(nil, n.Update))
		if len(upd) == 1 {
			c.Update = upd[0]
		}
		c.Body = sub(n.Body)
		return &c
	case *ast.ForInStatement:
		c := *n
		c.Iterable = r.rewriteExpr(n.Iterable, local)
		inner := make(map[string]string, len(local))
		for k, v := range local {
			inner[k] = v
		}
		if id := n.Variable; id != nil {
			ni := *id
			if repl, ok := local[ni.Name]; ok {
				ni.Name = repl
			}
			c.Variable = &ni
			inner[id.Name] = ni.Name
		}
		c.Body = r.rewriteStmts(n.Body, inner)
		return &c
	case *ast.BlockStatement:
		c := *n
		c.Statements = sub(n.Statements)
		return &c
	case *ast.CallStatement:
		c := *n
		c.Target = r.rewriteExpr(n.Target, local)
		c.Body = sub(n.Body)
		return &c
	case *ast.SpendStatement:
		c := *n
		c.Target = r.rewriteExpr(n.Target, local)
		if n.Calls != nil {
			c.Calls = make([]*ast.CallClause, len(n.Calls))
			for i, cl := range n.Calls {
				if cl == nil {
					continue
				}
				nc := *cl
				nc.Index = r.rewriteExpr(cl.Index, local)
				nc.Body = sub(cl.Body)
				c.Calls[i] = &nc
			}
		}
		return &c
	case *ast.NonLocalStatement:
		c := *n
		c.Value = r.rewriteExpr(n.Value, local)
		return &c
	case *ast.ObjectStatement:
		c := *n
		c.Fields = rewriteExprs(n.Fields, func(e ast.Expression) ast.Expression { return r.rewriteExpr(e, local) })
		c.Value = r.rewriteExpr(n.Value, local)
		return &c
	case *ast.SwitchStatement:
		c := *n
		c.Scrutinee = r.rewriteExpr(n.Scrutinee, local)
		c.Cases = make([]*ast.SwitchCase, len(n.Cases))
		for i, cs := range n.Cases {
			if cs == nil {
				continue
			}
			nc := *cs
			nc.Values = rewriteExprs(cs.Values, func(e ast.Expression) ast.Expression { return r.rewriteExpr(e, local) })
			nc.Body = sub(cs.Body)
			c.Cases[i] = &nc
		}
		c.Default = sub(n.Default)
		return &c
	case *ast.PrefixStatement:
		c := *n
		c.Body = sub(n.Body)
		return &c
	case *ast.TreeStatement:
		c := *n
		c.Root = r.rewriteExpr(n.Root, local)
		c.Body = sub(n.Body)
		return &c
	case *ast.FunctionStatement:
		c := *n
		c.Body = funcBody(n.Body)
		return &c
	default:
		// 其余语句（import / export / break / continue / 类型声明…）不含可重命名的局部变量
		return s
	}
}

func (r *alphaRenamer) rewriteDecl(d *ast.VariableDeclaration, local map[string]string) *ast.VariableDeclaration {
	nd := *d
	// 初值先按外层映射改写（`i64 a = 1, b = a` 里 b 的初值引用的是 a）
	nd.Value = r.rewriteExpr(d.Value, local)
	if d.Name != "" {
		if repl, ok := r.renameTable()[d.Name]; ok && !d.IsStatic && !d.IsConst {
			nd.Name = repl
			if nd.BaseType == "" {
				nd.BaseType = d.Type
			}
			// 把重命名后的声明登记下来，供结构约束（tree）做类型/结构检查
			shadow := nd
			r.decls[repl] = &shadow
		}
	}
	nd.More = nil
	for _, m := range d.More {
		if m == nil {
			continue
		}
		nd.More = append(nd.More, r.rewriteDecl(m, local))
	}
	return &nd
}

func (r *alphaRenamer) rewriteExpr(e ast.Expression, local map[string]string) ast.Expression {
	if e == nil {
		return nil
	}
	// 先深度改写子节点（作用域映射对子表达式一致），再处理本节点
	inner := RewriteExpressionChildren(e, func(n ast.Node) ast.Node {
		switch v := n.(type) {
		case ast.Expression:
			return r.rewriteExpr(v, local)
		case ast.Statement:
			return r.rewriteStmt(v, local)
		}
		return nil
	})
	id, ok := inner.(*ast.Identifier)
	if !ok || id == nil || id.IsPrefixVar {
		return inner
	}
	if repl, ok := local[id.Name]; ok && repl != id.Name {
		c := *id
		c.Name = repl
		return &c
	}
	return inner
}

func rewriteExprs(in []ast.Expression, fn func(ast.Expression) ast.Expression) []ast.Expression {
	if in == nil {
		return nil
	}
	out := make([]ast.Expression, len(in))
	for i, e := range in {
		out[i] = fn(e)
	}
	return out
}

func appendNonNil(out []ast.Statement, s ast.Statement) []ast.Statement {
	if s == nil {
		return out
	}
	return append(out, s)
}

// ScanFreeReferences 扫描模板体中的「直引」：既不是 `$参数`，也不是模板本地
// 声明的标识符名。返回按名字排序的去重列表，供展开时做跨越引用解析。
//
// 下列名字不算直引：
//   - 后跟 `(` / `.` 的标识符（函数名、模块名、结构体字面量类型名）——
//     它们由调用点语义解析，按名字重命名只会破坏解析；
//   - 字段名位置（obj.field 的 field）、结构体字面量字段名、match 模式；
//   - 典型的内建名字（true/false/null/NULL/self/auto/…）。
func ScanFreeReferences(stmts []ast.Statement) []string {
	found := make(map[string]bool)
	locals := make(map[string]bool)
	for _, s := range stmts {
		collectLocalNames(s, locals)
	}
	var walkExpr func(e ast.Expression)
	var walkStmt func(s ast.Statement)

	walkExpr = func(e ast.Expression) {
		switch n := e.(type) {
		case nil:
			return
		case *ast.Identifier:
			if n.IsPrefixVar || locals[n.Name] || isBuiltinName(n.Name) || n.Name == "" {
				return
			}
			found[n.Name] = true
		case *ast.MemberAccessExpression:
			walkExpr(n.Object)
		case *ast.MemberExpression:
			walkExpr(n.Object)
		case *ast.CallExpression:
			// 被调名是函数名，不按变量重命名；只下钻实参
			if m, ok := n.Function.(*ast.MemberAccessExpression); ok {
				walkExpr(m.Object)
			}
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *ast.SpawnExpression:
			if m, ok := n.Function.(*ast.MemberAccessExpression); ok {
				walkExpr(m.Object)
			}
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *ast.IndexExpression:
			walkExpr(n.Object)
			walkExpr(n.Index)
		case *ast.BinaryExpression:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *ast.UnaryExpression:
			if n.Operator != "&" {
				// &x 的 x 是取址目标，仍是变量引用；统一都下钻
			}
			walkExpr(n.Right)
		case *ast.ParenExpression:
			walkExpr(n.Inner)
		case *ast.ConditionalExpression:
			walkExpr(n.Condition)
			walkExpr(n.TrueExpr)
			walkExpr(n.FalseExpr)
		case *ast.TypeCastExpression:
			walkExpr(n.Expression)
		case *ast.AwaitExpression:
			walkExpr(n.Handle)
		case *ast.ArrayLiteral:
			for _, el := range n.Elements {
				walkExpr(el)
			}
		case *ast.ObjectLiteral:
			for _, f := range n.Fields {
				walkExpr(f.Value)
			}
		case *ast.StructLiteral:
			for _, f := range n.Fields {
				walkExpr(f.Value)
			}
		case *ast.ComptimeExpression:
			walkExpr(n.Inner)
		case *ast.LiteralExpression:
			if v, ok := n.Value.(ast.Expression); ok {
				walkExpr(v)
			}
		case *ast.LambdaExpression:
			for _, s := range n.Body {
				walkStmt(s)
			}
		case *ast.MatchExpression:
			walkExpr(n.Target)
			for _, arm := range n.Arms {
				if arm == nil {
					continue
				}
				for _, s := range arm.Body {
					walkStmt(s)
				}
			}
		case *ast.PrefixCallExpression:
			for _, v := range n.Params {
				walkExpr(v)
			}
			for _, s := range n.Body {
				walkStmt(s)
			}
		}
	}

	walkStmt = func(s ast.Statement) {
		switch n := s.(type) {
		case nil:
			return
		case *ast.ExpressionStatement:
			walkExpr(n.Expression)
		case *ast.VariableDeclaration:
			for _, d := range n.AllDeclarators() {
				if d == nil {
					continue
				}
				walkExpr(d.Value)
			}
		case *ast.ReturnStatement:
			walkExpr(n.Value)
		case *ast.YieldStatement:
			walkExpr(n.Source)
		case *ast.ReleaseStatement:
			walkExpr(n.Source)
		case *ast.ExtractStatement:
			walkExpr(n.Source)
			walkExpr(n.Index)
		case *ast.IfStatement:
			walkExpr(n.Condition)
			for _, c := range n.Body {
				walkStmt(c)
			}
			for _, c := range n.Else {
				walkStmt(c)
			}
		case *ast.WhileStatement:
			walkExpr(n.Condition)
			for _, c := range n.Body {
				walkStmt(c)
			}
		case *ast.ForStatement:
			walkStmt(n.Init)
			walkExpr(n.Condition)
			walkStmt(n.Update)
			for _, c := range n.Body {
				walkStmt(c)
			}
		case *ast.ForInStatement:
			walkExpr(n.Iterable)
			for _, c := range n.Body {
				walkStmt(c)
			}
		case *ast.BlockStatement:
			for _, c := range n.Statements {
				walkStmt(c)
			}
		case *ast.CallStatement:
			walkExpr(n.Target)
			for _, c := range n.Body {
				walkStmt(c)
			}
		case *ast.SpendStatement:
			walkExpr(n.Target)
			for _, cl := range n.Calls {
				if cl == nil {
					continue
				}
				walkExpr(cl.Index)
				for _, c := range cl.Body {
					walkStmt(c)
				}
			}
		case *ast.SwitchStatement:
			walkExpr(n.Scrutinee)
			for _, cs := range n.Cases {
				if cs == nil {
					continue
				}
				for _, v := range cs.Values {
					walkExpr(v)
				}
				for _, c := range cs.Body {
					walkStmt(c)
				}
			}
			for _, c := range n.Default {
				walkStmt(c)
			}
		case *ast.FunctionStatement:
			// 嵌套函数体属于函数边界：其引用由函数自身的捕获分析负责
		case *ast.ObjectStatement:
			for _, f := range n.Fields {
				walkExpr(f)
			}
			walkExpr(n.Value)
		}
	}

	for _, s := range stmts {
		walkStmt(s)
	}

	out := make([]string, 0, len(found))
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// collectLocalNames 收集模板体内的本地声明名（含嵌套块；不含 $参数声明）
func collectLocalNames(s ast.Statement, locals map[string]bool) {
	switch n := s.(type) {
	case nil:
		return
	case *ast.VariableDeclaration:
		for _, d := range n.AllDeclarators() {
			if d != nil && d.Name != "" {
				locals[d.Name] = true
			}
		}
	case *ast.ForInStatement:
		if n.Variable != nil {
			locals[n.Variable.Name] = true
		}
		for _, c := range n.Body {
			collectLocalNames(c, locals)
		}
	case *ast.IfStatement:
		for _, c := range n.Body {
			collectLocalNames(c, locals)
		}
		for _, c := range n.Else {
			collectLocalNames(c, locals)
		}
	case *ast.WhileStatement:
		for _, c := range n.Body {
			collectLocalNames(c, locals)
		}
	case *ast.ForStatement:
		collectLocalNames(n.Init, locals)
		for _, c := range n.Body {
			collectLocalNames(c, locals)
		}
	case *ast.BlockStatement:
		for _, c := range n.Statements {
			collectLocalNames(c, locals)
		}
	case *ast.FunctionStatement:
		for _, p := range n.Params {
			locals[p] = true
		}
	case *ast.CallStatement:
		for _, c := range n.Body {
			collectLocalNames(c, locals)
		}
	}
}

// PrefixValidName 判断名字能否直接作为 C 标识符出现在展开结果里。
// 形如 `std.string` 之类的直引无法重命名，保持原样（由语义阶段决定可见性）。
var prefixValidName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsRenamableName 判断直引名是否可以参与重命名
func IsRenamableName(name string) bool {
	return prefixValidName.MatchString(name) && !isBuiltinName(name)
}

// builtinIdentifiers 展开期不做重定向的内建名字
var builtinIdentifiers = map[string]bool{
	"true": true, "false": true, "null": true, "NULL": true,
	"self": true, "this": true, "auto": true, "void": true,
	"spawn": true, "await": true,
}

func isBuiltinName(name string) bool {
	return builtinIdentifiers[name]
}
