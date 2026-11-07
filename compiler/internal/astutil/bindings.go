package astutil

import "kaula/internal/ast"

// Bindings 宏展开期的名字重写表：原始标识符名 → 展开后的唯一名。
//
// 三类映射共用一张表，按下列优先级查询（结构宏卫生性的全部规则都在这里）：
//
//  1. 模板本地变量（自动 alpha 重命名）：宏体内 `auto tmp = ...` 这类声明，
//     展开时为每个调用点生成唯一名（见 AlphaRenameTemplate）。
//  2. `$参数`：绑定为调用点实参的 AST 节点，展开为实参的**克隆**。
//     同一实参被引用多次 = 多次求值，因此实参有副作用或用后即变的表达式
//     （如 `arr.pop()`）不会被悄悄共享。
//  3. 模板直引（free reference / 跨越引用）：宏体里既不是本地声明、也没有 `$`
//     标记的名字，展开时重写为「调用点所在作用域的可见名」——同名直引即
//     原样使用，否则生成调用点解析出的唯一名（见 PrefixFreeRewrite）。
//
// `$参数` 名以 `$` 存储（与 ast.Identifier.Name 中 IsPrefixVar 的写法一致，
// 解析器把 `$x` 解析成 Name="x" + IsPrefixVar=true；这里两种形态都接受）。
type Bindings struct {
	Params map[string]ast.Expression
	Locals map[string]string
	Frees  map[string]string
}

// NewBindings 创建空重写表
func NewBindings() *Bindings {
	return &Bindings{
		Params: map[string]ast.Expression{},
		Locals: map[string]string{},
		Frees:  map[string]string{},
	}
}

// Clone 深拷贝重写表（Params 里的 AST 共享，克隆发生在展开时）
func (b *Bindings) Clone() *Bindings {
	out := NewBindings()
	if b == nil {
		return out
	}
	for k, v := range b.Params {
		out.Params[k] = v
	}
	for k, v := range b.Locals {
		out.Locals[k] = v
	}
	for k, v := range b.Frees {
		out.Frees[k] = v
	}
	return out
}

// RewriteTemplate 按重写表深拷贝并改写模板语句序列。
func (b *Bindings) RewriteTemplate(stmts []ast.Statement) []ast.Statement {
	if b == nil {
		return CloneStatements(stmts)
	}
	return MapStatements(stmts, b.substitute)
}

// RewriteTemplateExpr 按重写表改写单个表达式。
func (b *Bindings) RewriteTemplateExpr(e ast.Expression) ast.Expression {
	if b == nil || e == nil {
		return CloneExpression(e)
	}
	return MapExpression(e, b.substitute)
}

// substitute 是展开期唯一的标识符改写点。返回 nil 表示保留原节点。
func (b *Bindings) substitute(node ast.Node) ast.Node {
	id, ok := node.(*ast.Identifier)
	if !ok || id == nil {
		return nil
	}
	return b.RewriteIdentifier(id)
}

// RewriteIdentifier 按重写表改写单个标识符节点，返回替换后的表达式（nil 表示不变）。
//
// 判定顺序即语义优先级：$参数 > 本地变量 > 直引。`$` 标记优先，是因为模板里
// 出现 `$x` 时它的含义**只能**是参数绑定，不应被同名局部变量截获。
func (b *Bindings) RewriteIdentifier(id *ast.Identifier) ast.Expression {
	if b == nil || id == nil {
		return nil
	}
	if id.IsPrefixVar {
		if bound, ok := b.Params[id.Name]; ok && bound != nil {
			return CloneExpression(bound)
		}
		return nil
	}
	if repl, ok := b.Locals[id.Name]; ok && repl != id.Name {
		c := *id
		c.Name = repl
		return &c
	}
	if repl, ok := b.Frees[id.Name]; ok && repl != id.Name {
		c := *id
		c.Name = repl
		return &c
	}
	return nil
}

// CloneStatements 深拷贝语句序列（模板 AST 是共享只读的，展开必须拷贝）
func CloneStatements(stmts []ast.Statement) []ast.Statement {
	if stmts == nil {
		return nil
	}
	out := make([]ast.Statement, 0, len(stmts))
	for _, s := range stmts {
		if s == nil {
			continue
		}
		out = append(out, RewriteStatementChildren(s, cloneTransform))
	}
	return out
}

// CloneExpression 深拷贝表达式树
func CloneExpression(e ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	return RewriteExpressionChildren(e, cloneTransform)
}

// cloneTransform 克隆专用改写器：原样返回节点（父节点据此重建副本）。
func cloneTransform(node ast.Node) ast.Node {
	switch node.(type) {
	case ast.Expression, ast.Statement:
		return node
	}
	return nil
}
