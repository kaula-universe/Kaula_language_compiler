package ast

import "strconv"

// Node 表示AST节点的接口
type Node interface {
	String() string
	GetPosition() Position
	SetPosition(pos Position)
}

// Position 表示节点在源代码中的位置
type Position struct {
	Line   int
	Column int
	File   string
}

// Program 表示整个程序
type Program struct {
	Statements []Statement
	Pos        Position
}

// String 实现Node接口
func (p *Program) String() string {
	return "Program"
}

// GetPosition 实现Node接口
func (p *Program) GetPosition() Position {
	return p.Pos
}

// SetPosition 实现Node接口
func (p *Program) SetPosition(pos Position) {
	p.Pos = pos
}

// AddStatement 添加语句
func (p *Program) AddStatement(stmt Statement) {
	p.Statements = append(p.Statements, stmt)
}

// GetStatement 获取指定位置的语句
func (p *Program) GetStatement(index int) Statement {
	if index >= 0 && index < len(p.Statements) {
		return p.Statements[index]
	}
	return nil
}

// StatementCount 获取语句数量
func (p *Program) StatementCount() int {
	return len(p.Statements)
}

// FindFunction 查找函数声明
func (p *Program) FindFunction(name string) *FunctionStatement {
	for _, stmt := range p.Statements {
		if fnStmt, ok := stmt.(*FunctionStatement); ok && fnStmt.Name == name {
			return fnStmt
		}
	}
	return nil
}

// FindPrefix 查找前缀声明
func (p *Program) FindPrefix(name string) *PrefixStatement {
	for _, stmt := range p.Statements {
		if prefixStmt, ok := stmt.(*PrefixStatement); ok && prefixStmt.Name == name {
			return prefixStmt
		}
	}
	return nil
}

// FindObject 查找对象声明
func (p *Program) FindObject(name string) *ObjectStatement {
	for _, stmt := range p.Statements {
		if objStmt, ok := stmt.(*ObjectStatement); ok && objStmt.Name == name {
			return objStmt
		}
	}
	return nil
}

// FindClass 查找类声明
func (p *Program) FindClass(name string) *ClassStatement {
	for _, stmt := range p.Statements {
		if classStmt, ok := stmt.(*ClassStatement); ok && classStmt.Name == name {
			return classStmt
		}
	}
	return nil
}

// FindInterface 查找接口声明
func (p *Program) FindInterface(name string) *InterfaceStatement {
	for _, stmt := range p.Statements {
		if interfaceStmt, ok := stmt.(*InterfaceStatement); ok && interfaceStmt.Name == name {
			return interfaceStmt
		}
	}
	return nil
}

// FindStruct 查找结构体声明
func (p *Program) FindStruct(name string) *StructStatement {
	for _, stmt := range p.Statements {
		if structStmt, ok := stmt.(*StructStatement); ok && structStmt.Name == name {
			return structStmt
		}
	}
	return nil
}

// Traverse 遍历所有节点
func (p *Program) Traverse(visitor func(Node)) {
	visitor(p)
	for _, stmt := range p.Statements {
		traverseNode(stmt, visitor)
	}
}

// traverseNode 递归遍历节点
func traverseNode(node Node, visitor func(Node)) {
	visitor(node)
	
	// 根据节点类型进行不同的处理
	switch n := node.(type) {
	case *Program:
		for _, stmt := range n.Statements {
			traverseNode(stmt, visitor)
		}
	case *VOStatement:
		if n.Value != nil {
			traverseNode(n.Value, visitor)
		}
		if n.Code != nil {
			traverseNode(n.Code, visitor)
		}
		if n.Access != nil {
			traverseNode(n.Access, visitor)
		}
	case *SpendCallStatement:
		if n.Spend != nil {
			traverseNode(n.Spend, visitor)
		}
		for _, call := range n.Calls {
			traverseNode(&call, visitor)
			if call.Target != nil {
				traverseNode(call.Target, visitor)
			}
			for _, stmt := range call.Body {
				traverseNode(stmt, visitor)
			}
		}
	case *TaskStatement:
		if n.Func != nil {
			traverseNode(n.Func, visitor)
		}
		if n.Arg != nil {
			traverseNode(n.Arg, visitor)
		}
	case *PrefixStatement:
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *TreeStatement:
		if n.Root != nil {
			traverseNode(n.Root, visitor)
		}
	case *ObjectStatement:
		for _, field := range n.Fields {
			traverseNode(field, visitor)
		}
		if n.Value != nil {
			traverseNode(n.Value, visitor)
		}
	case *FunctionStatement:
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *IfStatement:
		if n.Condition != nil {
			traverseNode(n.Condition, visitor)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
		for _, stmt := range n.Else {
			traverseNode(stmt, visitor)
		}
	case *WhileStatement:
		if n.Condition != nil {
			traverseNode(n.Condition, visitor)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *ForStatement:
		if n.Init != nil {
			traverseNode(n.Init, visitor)
		}
		if n.Condition != nil {
			traverseNode(n.Condition, visitor)
		}
		if n.Update != nil {
			traverseNode(n.Update, visitor)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *ReturnStatement:
		if n.Value != nil {
			traverseNode(n.Value, visitor)
		}
	case *ImportStatement:
		// 无子节点
	case *NonLocalStatement:
		if n.Value != nil {
			traverseNode(n.Value, visitor)
		}
	case *VariableDeclaration:
		if n.Value != nil {
			traverseNode(n.Value, visitor)
		}
	case *SwitchStatement:
		if n.Expression != nil {
			traverseNode(n.Expression, visitor)
		}
		for _, stmt := range n.Statements {
			traverseNode(stmt, visitor)
		}
		for _, caseStmt := range n.Cases {
			traverseNode(&caseStmt, visitor)
			if caseStmt.Value != nil {
				traverseNode(caseStmt.Value, visitor)
			}
			for _, stmt := range caseStmt.Body {
				traverseNode(stmt, visitor)
			}
		}
		for _, stmt := range n.Default {
			traverseNode(stmt, visitor)
		}
	case *CaseStatement:
		if n.Value != nil {
			traverseNode(n.Value, visitor)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *ExpressionStatement:
		if n.Expression != nil {
			traverseNode(n.Expression, visitor)
		}
	case *Identifier:
		// 无子节点
	case *IntegerLiteral:
		// 无子节点
	case *FloatLiteral:
		// 无子节点
	case *StringLiteral:
		// 无子节点
	case *BinaryExpression:
		if n.Left != nil {
			traverseNode(n.Left, visitor)
		}
		if n.Right != nil {
			traverseNode(n.Right, visitor)
		}
	case *CallExpression:
		if n.Function != nil {
			traverseNode(n.Function, visitor)
		}
		for _, arg := range n.Args {
			traverseNode(arg, visitor)
		}
	case *IndexExpression:
		if n.Object != nil {
			traverseNode(n.Object, visitor)
		}
		if n.Index != nil {
			traverseNode(n.Index, visitor)
		}
	case *PrefixCallExpression:
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *BlockStatement:
		for _, stmt := range n.Statements {
			traverseNode(stmt, visitor)
		}
	case *CallStatement:
		if n.Target != nil {
			traverseNode(n.Target, visitor)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *ClassStatement:
		for _, field := range n.Fields {
			traverseNode(field, visitor)
		}
		for _, method := range n.Methods {
			traverseNode(method, visitor)
		}
		for _, constructor := range n.Constructors {
			traverseNode(constructor, visitor)
		}
	case *FieldDeclaration:
		// 无子节点
	case *MethodStatement:
		for _, param := range n.Params {
			traverseNode(param, visitor)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *ConstructorStatement:
		for _, param := range n.Params {
			traverseNode(param, visitor)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *InterfaceStatement:
		for _, method := range n.Methods {
			traverseNode(method, visitor)
		}
	case *StructStatement:
		for _, field := range n.Fields {
			traverseNode(field, visitor)
		}
	case *MemberAccessExpression:
		if n.Object != nil {
			traverseNode(n.Object, visitor)
		}
	case *ImplementsClause:
		// 无子节点
	case *Param:
		// 无子节点
	}
}

// Statement 表示语句
type Statement interface {
	Node
	statementNode()
}

// Expression 表示表达式
type Expression interface {
	Node
	expressionNode()
}

// VOStatement 表示VO语句
type VOStatement struct {
	Value  Expression
	Code   Expression
	Access Expression
	Pos    Position
}

// statementNode 实现Statement接口
func (v *VOStatement) statementNode() {}

// String 实现Node接口
func (v *VOStatement) String() string {
	return "VOStatement"
}

// GetPosition 实现Node接口
func (v *VOStatement) GetPosition() Position {
	return v.Pos
}

// SetPosition 实现Node接口
func (v *VOStatement) SetPosition(pos Position) {
	v.Pos = pos
}

// SpendCallStatement 表示spend/call语句
type SpendCallStatement struct {
	Spend Expression
	Calls []CallStatement
	Pos   Position
}

// statementNode 实现Statement接口
func (s *SpendCallStatement) statementNode() {}

// String 实现Node接口
func (s *SpendCallStatement) String() string {
	return "SpendCallStatement"
}

// GetPosition 实现Node接口
func (s *SpendCallStatement) GetPosition() Position {
	return s.Pos
}

// SetPosition 实现Node接口
func (s *SpendCallStatement) SetPosition(pos Position) {
	s.Pos = pos
}

// TaskStatement 表示task语句
type TaskStatement struct {
	Priority int
	Func     Expression
	Arg      Expression
	Pos      Position
}

// statementNode 实现Statement接口
func (t *TaskStatement) statementNode() {}

// String 实现Node接口
func (t *TaskStatement) String() string {
	return "TaskStatement"
}

// GetPosition 实现Node接口
func (t *TaskStatement) GetPosition() Position {
	return t.Pos
}

// SetPosition 实现Node接口
func (t *TaskStatement) SetPosition(pos Position) {
	t.Pos = pos
}

// PrefixStatement 表示prefix语句
type PrefixStatement struct {
	Name   string
	Body   []Statement
	Pos    Position
}

// statementNode 实现Statement接口
func (p *PrefixStatement) statementNode() {}

// String 实现Node接口
func (p *PrefixStatement) String() string {
	return "PrefixStatement"
}

// GetPosition 实现Node接口
func (p *PrefixStatement) GetPosition() Position {
	return p.Pos
}

// SetPosition 实现Node接口
func (p *PrefixStatement) SetPosition(pos Position) {
	p.Pos = pos
}

// TreeStatement 表示tree语句
type TreeStatement struct {
	Root Expression
	Pos  Position
}

// statementNode 实现Statement接口
func (t *TreeStatement) statementNode() {}

// String 实现Node接口
func (t *TreeStatement) String() string {
	return "TreeStatement"
}

// GetPosition 实现Node接口
func (t *TreeStatement) GetPosition() Position {
	return t.Pos
}

// SetPosition 实现Node接口
func (t *TreeStatement) SetPosition(pos Position) {
	t.Pos = pos
}

// ObjectStatement 表示object语句
type ObjectStatement struct {
	Type   string
	Name   string
	Fields []Expression
	Value  Expression
	Pos    Position
}

// statementNode 实现Statement接口
func (o *ObjectStatement) statementNode() {}

// String 实现Node接口
func (o *ObjectStatement) String() string {
	return "ObjectStatement"
}

// GetPosition 实现Node接口
func (o *ObjectStatement) GetPosition() Position {
	return o.Pos
}

// SetPosition 实现Node接口
func (o *ObjectStatement) SetPosition(pos Position) {
	o.Pos = pos
}

// FunctionStatement 表示函数语句
type FunctionStatement struct {
	Name   string
	Params []string
	Body   []Statement
	Pos    Position
}

// statementNode 实现Statement接口
func (f *FunctionStatement) statementNode() {}

// String 实现Node接口
func (f *FunctionStatement) String() string {
	return "FunctionStatement"
}

// GetPosition 实现Node接口
func (f *FunctionStatement) GetPosition() Position {
	return f.Pos
}

// SetPosition 实现Node接口
func (f *FunctionStatement) SetPosition(pos Position) {
	f.Pos = pos
}

// AddParam 添加参数
func (f *FunctionStatement) AddParam(param string) {
	f.Params = append(f.Params, param)
}

// AddStatement 添加语句到函数体
func (f *FunctionStatement) AddStatement(stmt Statement) {
	f.Body = append(f.Body, stmt)
}

// ParamCount 获取参数数量
func (f *FunctionStatement) ParamCount() int {
	return len(f.Params)
}

// GetParam 获取指定位置的参数
func (f *FunctionStatement) GetParam(index int) string {
	if index >= 0 && index < len(f.Params) {
		return f.Params[index]
	}
	return ""
}

// HasParam 检查是否包含指定参数
func (f *FunctionStatement) HasParam(name string) bool {
	for _, param := range f.Params {
		if param == name {
			return true
		}
	}
	return false
}

// StatementCount 获取函数体语句数量
func (f *FunctionStatement) StatementCount() int {
	return len(f.Body)
}

// GetStatement 获取函数体中指定位置的语句
func (f *FunctionStatement) GetStatement(index int) Statement {
	if index >= 0 && index < len(f.Body) {
		return f.Body[index]
	}
	return nil
}

// Traverse 遍历函数节点及其子节点
func (f *FunctionStatement) Traverse(visitor func(Node)) {
	traverseNode(f, visitor)
}

// IfStatement 表示if语句
type IfStatement struct {
	Condition Expression
	Body      []Statement
	Else      []Statement
	Pos       Position
}

// statementNode 实现Statement接口
func (i *IfStatement) statementNode() {}

// String 实现Node接口
func (i *IfStatement) String() string {
	return "IfStatement"
}

// GetPosition 实现Node接口
func (i *IfStatement) GetPosition() Position {
	return i.Pos
}

// SetPosition 实现Node接口
func (i *IfStatement) SetPosition(pos Position) {
	i.Pos = pos
}

// SetCondition 设置条件表达式
func (i *IfStatement) SetCondition(cond Expression) {
	i.Condition = cond
}

// AddIfStatement 添加语句到if体
func (i *IfStatement) AddIfStatement(stmt Statement) {
	i.Body = append(i.Body, stmt)
}

// AddElseStatement 添加语句到else体
func (i *IfStatement) AddElseStatement(stmt Statement) {
	i.Else = append(i.Else, stmt)
}

// HasElse 检查是否有else体
func (i *IfStatement) HasElse() bool {
	return len(i.Else) > 0
}

// IfStatementCount 获取if体语句数量
func (i *IfStatement) IfStatementCount() int {
	return len(i.Body)
}

// ElseStatementCount 获取else体语句数量
func (i *IfStatement) ElseStatementCount() int {
	return len(i.Else)
}

// GetIfStatement 获取if体中指定位置的语句
func (i *IfStatement) GetIfStatement(index int) Statement {
	if index >= 0 && index < len(i.Body) {
		return i.Body[index]
	}
	return nil
}

// GetElseStatement 获取else体中指定位置的语句
func (i *IfStatement) GetElseStatement(index int) Statement {
	if index >= 0 && index < len(i.Else) {
		return i.Else[index]
	}
	return nil
}

// Traverse 遍历if节点及其子节点
func (i *IfStatement) Traverse(visitor func(Node)) {
	traverseNode(i, visitor)
}

// WhileStatement 表示while语句
type WhileStatement struct {
	Condition Expression
	Body      []Statement
	Pos       Position
}

// statementNode 实现Statement接口
func (w *WhileStatement) statementNode() {}

// String 实现Node接口
func (w *WhileStatement) String() string {
	return "WhileStatement"
}

// GetPosition 实现Node接口
func (w *WhileStatement) GetPosition() Position {
	return w.Pos
}

// SetPosition 实现Node接口
func (w *WhileStatement) SetPosition(pos Position) {
	w.Pos = pos
}

// ForStatement 表示for语句
type ForStatement struct {
	Init      Statement
	Condition Expression
	Update    Statement
	Body      []Statement
	Pos       Position
}

// statementNode 实现Statement接口
func (f *ForStatement) statementNode() {}

// String 实现Node接口
func (f *ForStatement) String() string {
	return "ForStatement"
}

// GetPosition 实现Node接口
func (f *ForStatement) GetPosition() Position {
	return f.Pos
}

// SetPosition 实现Node接口
func (f *ForStatement) SetPosition(pos Position) {
	f.Pos = pos
}

// ReturnStatement 表示return语句
type ReturnStatement struct {
	Value Expression
	Pos   Position
}

// statementNode 实现Statement接口
func (r *ReturnStatement) statementNode() {}

// String 实现Node接口
func (r *ReturnStatement) String() string {
	return "ReturnStatement"
}

// GetPosition 实现Node接口
func (r *ReturnStatement) GetPosition() Position {
	return r.Pos
}

// SetPosition 实现Node接口
func (r *ReturnStatement) SetPosition(pos Position) {
	r.Pos = pos
}

// ImportStatement 表示import语句
type ImportStatement struct {
	Module string
	Pos    Position
}

// statementNode 实现Statement接口
func (i *ImportStatement) statementNode() {}

// String 实现Node接口
func (i *ImportStatement) String() string {
	return "ImportStatement"
}

// GetPosition 实现Node接口
func (i *ImportStatement) GetPosition() Position {
	return i.Pos
}

// SetPosition 实现Node接口
func (i *ImportStatement) SetPosition(pos Position) {
	i.Pos = pos
}

// NonLocalStatement 表示nonlocal语句
type NonLocalStatement struct {
	Type  string
	Name  string
	Value Expression
	Pos   Position
}

// statementNode 实现Statement接口
func (n *NonLocalStatement) statementNode() {}

// String 实现Node接口
func (n *NonLocalStatement) String() string {
	return "NonLocalStatement"
}

// GetPosition 实现Node接口
func (n *NonLocalStatement) GetPosition() Position {
	return n.Pos
}

// SetPosition 实现Node接口
func (n *NonLocalStatement) SetPosition(pos Position) {
	n.Pos = pos
}

// VariableDeclaration 表示变量声明语句
type VariableDeclaration struct {
	Type     string
	Name     string
	Value    Expression
	Nullable bool
	Pos      Position
}

// statementNode 实现Statement接口
func (v *VariableDeclaration) statementNode() {}

// String 实现Node接口
func (v *VariableDeclaration) String() string {
	return "VariableDeclaration"
}

// GetPosition 实现Node接口
func (v *VariableDeclaration) GetPosition() Position {
	return v.Pos
}

// SetPosition 实现Node接口
func (v *VariableDeclaration) SetPosition(pos Position) {
	v.Pos = pos
}

// SwitchStatement 表示switch语句
type SwitchStatement struct {
	Expression Expression
	Statements []Statement
	Cases      []CaseStatement
	Default    []Statement
	Pos        Position
}

// statementNode 实现Statement接口
func (s *SwitchStatement) statementNode() {}

// String 实现Node接口
func (s *SwitchStatement) String() string {
	return "SwitchStatement"
}

// GetPosition 实现Node接口
func (s *SwitchStatement) GetPosition() Position {
	return s.Pos
}

// SetPosition 实现Node接口
func (s *SwitchStatement) SetPosition(pos Position) {
	s.Pos = pos
}

// CaseStatement 表示case语句
type CaseStatement struct {
	Value Expression
	Body  []Statement
	Pos   Position
}

// statementNode 实现Statement接口
func (c *CaseStatement) statementNode() {}

// String 实现Node接口
func (c *CaseStatement) String() string {
	return "CaseStatement"
}

// GetPosition 实现Node接口
func (c *CaseStatement) GetPosition() Position {
	return c.Pos
}

// SetPosition 实现Node接口
func (c *CaseStatement) SetPosition(pos Position) {
	c.Pos = pos
}



// ExpressionStatement 表示表达式语句
type ExpressionStatement struct {
	Expression Expression
	Pos        Position
}

// statementNode 实现Statement接口
func (e *ExpressionStatement) statementNode() {}

// String 实现Node接口
func (e *ExpressionStatement) String() string {
	return "ExpressionStatement"
}

// GetPosition 实现Node接口
func (e *ExpressionStatement) GetPosition() Position {
	return e.Pos
}

// SetPosition 实现Node接口
func (e *ExpressionStatement) SetPosition(pos Position) {
	e.Pos = pos
}

// Identifier 表示标识符
type Identifier struct {
	Name string
	Pos  Position
}

// expressionNode 实现Expression接口
func (i *Identifier) expressionNode() {}

// String 实现Node接口
func (i *Identifier) String() string {
	return "Identifier(" + i.Name + ")"
}

// GetPosition 实现Node接口
func (i *Identifier) GetPosition() Position {
	return i.Pos
}

// SetPosition 实现Node接口
func (i *Identifier) SetPosition(pos Position) {
	i.Pos = pos
}

// IntegerLiteral 表示整数字面量
type IntegerLiteral struct {
	Value int64
	Pos   Position
}

// expressionNode 实现Expression接口
func (i *IntegerLiteral) expressionNode() {}

// String 实现Node接口
func (i *IntegerLiteral) String() string {
	return "IntegerLiteral(" + strconv.FormatInt(i.Value, 10) + ")"
}

// GetPosition 实现Node接口
func (i *IntegerLiteral) GetPosition() Position {
	return i.Pos
}

// SetPosition 实现Node接口
func (i *IntegerLiteral) SetPosition(pos Position) {
	i.Pos = pos
}

// FloatLiteral 表示浮点数字面量
type FloatLiteral struct {
	Value float64
	Pos   Position
}

// expressionNode 实现Expression接口
func (f *FloatLiteral) expressionNode() {}

// String 实现Node接口
func (f *FloatLiteral) String() string {
	return "FloatLiteral(" + strconv.FormatFloat(f.Value, 'g', -1, 64) + ")"
}

// GetPosition 实现Node接口
func (f *FloatLiteral) GetPosition() Position {
	return f.Pos
}

// SetPosition 实现Node接口
func (f *FloatLiteral) SetPosition(pos Position) {
	f.Pos = pos
}

// StringLiteral 表示字符串字面量
type StringLiteral struct {
	Value string
	Pos   Position
}

// expressionNode 实现Expression接口
func (s *StringLiteral) expressionNode() {}

// String 实现Node接口
func (s *StringLiteral) String() string {
	return "StringLiteral(" + s.Value + ")"
}

// GetPosition 实现Node接口
func (s *StringLiteral) GetPosition() Position {
	return s.Pos
}

// SetPosition 实现Node接口
func (s *StringLiteral) SetPosition(pos Position) {
	s.Pos = pos
}

// BinaryExpression 表示二元表达式
type BinaryExpression struct {
	Left     Expression
	Operator string
	Right    Expression
	Pos      Position
}

// expressionNode 实现Expression接口
func (b *BinaryExpression) expressionNode() {}

// String 实现Node接口
func (b *BinaryExpression) String() string {
	return "BinaryExpression(" + b.Operator + ")"
}

// GetPosition 实现Node接口
func (b *BinaryExpression) GetPosition() Position {
	return b.Pos
}

// SetPosition 实现Node接口
func (b *BinaryExpression) SetPosition(pos Position) {
	b.Pos = pos
}

// GetOperator 获取操作符
func (b *BinaryExpression) GetOperator() string {
	return b.Operator
}

// GetLeft 获取左操作数
func (b *BinaryExpression) GetLeft() Expression {
	return b.Left
}

// GetRight 获取右操作数
func (b *BinaryExpression) GetRight() Expression {
	return b.Right
}

// SetLeft 设置左操作数
func (b *BinaryExpression) SetLeft(left Expression) {
	b.Left = left
}

// SetRight 设置右操作数
func (b *BinaryExpression) SetRight(right Expression) {
	b.Right = right
}

// SetOperator 设置操作符
func (b *BinaryExpression) SetOperator(op string) {
	b.Operator = op
}

// Traverse 遍历二元表达式节点及其子节点
func (b *BinaryExpression) Traverse(visitor func(Node)) {
	traverseNode(b, visitor)
}

// CallExpression 表示函数调用表达式
type CallExpression struct {
	Function Expression
	Args     []Expression
	Pos      Position
}

// expressionNode 实现Expression接口
func (c *CallExpression) expressionNode() {}

// String 实现Node接口
func (c *CallExpression) String() string {
	return "CallExpression"
}

// GetPosition 实现Node接口
func (c *CallExpression) GetPosition() Position {
	return c.Pos
}

// SetPosition 实现Node接口
func (c *CallExpression) SetPosition(pos Position) {
	c.Pos = pos
}

// IndexExpression 表示索引表达式
type IndexExpression struct {
	Object Expression
	Index  Expression
	Pos    Position
}

// expressionNode 实现Expression接口
func (i *IndexExpression) expressionNode() {}

// String 实现Node接口
func (i *IndexExpression) String() string {
	return "IndexExpression"
}

// GetPosition 实现Node接口
func (i *IndexExpression) GetPosition() Position {
	return i.Pos
}

// SetPosition 实现Node接口
func (i *IndexExpression) SetPosition(pos Position) {
	i.Pos = pos
}

// PrefixCallExpression 表示前缀调用表达式
type PrefixCallExpression struct {
	Name string
	Body []Statement
	Pos  Position
}

// expressionNode 实现Expression接口
func (p *PrefixCallExpression) expressionNode() {}

// String 实现Node接口
func (p *PrefixCallExpression) String() string {
	return "PrefixCallExpression(" + p.Name + ")"
}

// GetPosition 实现Node接口
func (p *PrefixCallExpression) GetPosition() Position {
	return p.Pos
}

// SetPosition 实现Node接口
func (p *PrefixCallExpression) SetPosition(pos Position) {
	p.Pos = pos
}

// BlockStatement 表示块语句
type BlockStatement struct {
	Statements []Statement
	Pos        Position
}

// statementNode 实现Statement接口
func (b *BlockStatement) statementNode() {}

// String 实现Node接口
func (b *BlockStatement) String() string {
	return "BlockStatement"
}

// GetPosition 实现Node接口
func (b *BlockStatement) GetPosition() Position {
	return b.Pos
}

// SetPosition 实现Node接口
func (b *BlockStatement) SetPosition(pos Position) {
	b.Pos = pos
}

// CallStatement 表示call语句
type CallStatement struct {
	Target  Expression
	Body    []Statement
	Pos     Position
}

// statementNode 实现Statement接口
func (c *CallStatement) statementNode() {}

// String 实现Node接口
func (c *CallStatement) String() string {
	return "CallStatement"
}

// GetPosition 实现Node接口
func (c *CallStatement) GetPosition() Position {
	return c.Pos
}

// SetPosition 实现Node接口
func (c *CallStatement) SetPosition(pos Position) {
	c.Pos = pos
}

// Param 表示参数

type Param struct {
	Name     string
	Type     string
	Nullable bool
	Pos      Position
}

// GetPosition 实现Node接口
func (p *Param) GetPosition() Position {
	return p.Pos
}

// SetPosition 实现Node接口
func (p *Param) SetPosition(pos Position) {
	p.Pos = pos
}

// String 实现Node接口
func (p *Param) String() string {
	return "Param(" + p.Name + ": " + p.Type + ")"
}

// ClassStatement 表示类定义
type ClassStatement struct {
	Name        string
	Fields      []*FieldDeclaration
	Methods     []*MethodStatement
	Constructors []*ConstructorStatement
	Implements  []string
	Pos         Position
}

// statementNode 实现Statement接口
func (c *ClassStatement) statementNode() {}

// String 实现Node接口
func (c *ClassStatement) String() string {
	return "ClassStatement(" + c.Name + ")"
}

// GetPosition 实现Node接口
func (c *ClassStatement) GetPosition() Position {
	return c.Pos
}

// SetPosition 实现Node接口
func (c *ClassStatement) SetPosition(pos Position) {
	c.Pos = pos
}

// FieldDeclaration 表示字段声明
type FieldDeclaration struct {
	Name     string
	Type     string
	Nullable bool
	Pos      Position
}

// statementNode 实现Statement接口
func (f *FieldDeclaration) statementNode() {}

// String 实现Node接口
func (f *FieldDeclaration) String() string {
	return "FieldDeclaration(" + f.Name + ": " + f.Type + ")"
}

// GetPosition 实现Node接口
func (f *FieldDeclaration) GetPosition() Position {
	return f.Pos
}

// SetPosition 实现Node接口
func (f *FieldDeclaration) SetPosition(pos Position) {
	f.Pos = pos
}

// MethodStatement 表示方法定义
type MethodStatement struct {
	Name       string
	Params     []*Param
	ReturnType string
	Body       []Statement
	Pos        Position
}

// statementNode 实现Statement接口
func (m *MethodStatement) statementNode() {}

// String 实现Node接口
func (m *MethodStatement) String() string {
	return "MethodStatement(" + m.Name + ")"
}

// GetPosition 实现Node接口
func (m *MethodStatement) GetPosition() Position {
	return m.Pos
}

// SetPosition 实现Node接口
func (m *MethodStatement) SetPosition(pos Position) {
	m.Pos = pos
}

// ConstructorStatement 表示构造函数
type ConstructorStatement struct {
	Params []*Param
	Body   []Statement
	Pos    Position
}

// statementNode 实现Statement接口
func (c *ConstructorStatement) statementNode() {}

// String 实现Node接口
func (c *ConstructorStatement) String() string {
	return "ConstructorStatement"
}

// GetPosition 实现Node接口
func (c *ConstructorStatement) GetPosition() Position {
	return c.Pos
}

// SetPosition 实现Node接口
func (c *ConstructorStatement) SetPosition(pos Position) {
	c.Pos = pos
}

// InterfaceStatement 表示接口定义
type InterfaceStatement struct {
	Name    string
	Methods []*MethodStatement
	Pos     Position
}

// statementNode 实现Statement接口
func (i *InterfaceStatement) statementNode() {}

// String 实现Node接口
func (i *InterfaceStatement) String() string {
	return "InterfaceStatement(" + i.Name + ")"
}

// GetPosition 实现Node接口
func (i *InterfaceStatement) GetPosition() Position {
	return i.Pos
}

// SetPosition 实现Node接口
func (i *InterfaceStatement) SetPosition(pos Position) {
	i.Pos = pos
}

// MemberAccessExpression 表示成员访问表达式
type MemberAccessExpression struct {
	Object Expression
	Member string
	Pos    Position
}

// expressionNode 实现Expression接口
func (m *MemberAccessExpression) expressionNode() {}

// String 实现Node接口
func (m *MemberAccessExpression) String() string {
	return "MemberAccessExpression(" + m.Member + ")"
}

// GetPosition 实现Node接口
func (m *MemberAccessExpression) GetPosition() Position {
	return m.Pos
}

// SetPosition 实现Node接口
func (m *MemberAccessExpression) SetPosition(pos Position) {
	m.Pos = pos
}

// ImplementsClause 表示实现子句
type ImplementsClause struct {
	Interfaces []string
	Pos        Position
}

// statementNode 实现 Statement 接口
func (i *ImplementsClause) statementNode() {}

// String 实现 Node 接口
func (i *ImplementsClause) String() string {
	return "ImplementsClause"
}

// GetPosition 实现 Node 接口
func (i *ImplementsClause) GetPosition() Position {
	return i.Pos
}

// SetPosition 实现 Node 接口
func (i *ImplementsClause) SetPosition(pos Position) {
	i.Pos = pos
}

// StructStatement 表示结构体定义
type StructStatement struct {
	Name   string
	Fields []*FieldDeclaration
	Pos    Position
}

// statementNode 实现 Statement 接口
func (s *StructStatement) statementNode() {}

// String 实现 Node 接口
func (s *StructStatement) String() string {
	return "StructStatement(" + s.Name + ")"
}

// GetPosition 实现 Node 接口
func (s *StructStatement) GetPosition() Position {
	return s.Pos
}

// SetPosition 实现 Node 接口
func (s *StructStatement) SetPosition(pos Position) {
	s.Pos = pos
}
