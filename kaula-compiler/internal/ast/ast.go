package ast

import (
	"fmt"
	"strconv"
	"strings"
)

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
	Source     string // 完整源码
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

// FindEnum 查找枚举声明
func (p *Program) FindEnum(name string) *EnumStatement {
	for _, stmt := range p.Statements {
		if enumStmt, ok := stmt.(*EnumStatement); ok && enumStmt.Name == name {
			return enumStmt
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
	case *SpendStatement:
		if n.Target != nil {
			traverseNode(n.Target, visitor)
		}
		for _, call := range n.Calls {
			traverseNode(call.Index, visitor)
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
		for _, tp := range n.TypeParams {
			if tp != nil {
				visitor(tp)
			}
		}
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
	case *TypeCastExpression:
		if n.Expression != nil {
			traverseNode(n.Expression, visitor)
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
		for _, tp := range n.TypeParams {
			if tp != nil {
				visitor(tp)
			}
		}
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
	case *LambdaExpression:
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
	case *MemberAccessExpression:
		if n.Object != nil {
			traverseNode(n.Object, visitor)
		}
	case *ImplementsClause:
		// 无子节点
	case *Param:
		// 无子节点
	case *YieldStatement:
		if n.Source != nil {
			traverseNode(n.Source, visitor)
		}
	case *ReleaseStatement:
		if n.Source != nil {
			traverseNode(n.Source, visitor)
		}
	case *ExtractStatement:
		if n.Source != nil {
			traverseNode(n.Source, visitor)
		}
		if n.Index != nil {
			traverseNode(n.Index, visitor)
		}
	case *EnumStatement:
		for _, variant := range n.Variants {
			visitor(variant)
		}
	case *MatchExpression:
		if n.Target != nil {
			traverseNode(n.Target, visitor)
		}
		for _, arm := range n.Arms {
			traverseNode(arm, visitor)
		}
	case *MatchArm:
		if n.Pattern != nil {
			visitor(n.Pattern)
		}
		for _, stmt := range n.Body {
			traverseNode(stmt, visitor)
		}
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

// SpendStatement 表示spend语句 - 锁定并开启一个对象的消费流程
// spend 用于锁定对象，call 必须被调用与元素数量对应的次数
type SpendStatement struct {
	Target  Expression     // 消费目标对象（如数组）
	Calls   []*CallClause // call 子句列表
	Pos     Position
}

// CallClause 表示call子句 - 消费一个元素
// index 是从 1 开始的元素索引
type CallClause struct {
	Index Expression // 元素索引（1-based）
	Body  []Statement // 处理逻辑
	Pos   Position
}

// statementNode 实现Statement接口
func (s *SpendStatement) statementNode() {}

// String 实现Node接口
func (s *SpendStatement) String() string {
	return fmt.Sprintf("SpendStatement(target=%s, calls=%d)", s.Target, len(s.Calls))
}

// GetPosition 实现Node接口
func (s *SpendStatement) GetPosition() Position {
	return s.Pos
}

// SetPosition 实现Node接口
func (s *SpendStatement) SetPosition(pos Position) {
	s.Pos = pos
}

// statementNode 实现Statement接口
func (c *CallClause) statementNode() {}

// String 实现Node接口
func (c *CallClause) String() string {
	return fmt.Sprintf("CallClause(index=%s)", c.Index)
}

// GetPosition 实现Node接口
func (c *CallClause) GetPosition() Position {
	return c.Pos
}

// SetPosition 实现Node接口
func (c *CallClause) SetPosition(pos Position) {
	c.Pos = pos
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

// TreeAnnotationType tree annotation types
type TreeAnnotationType int

const (
	TreeAnnotationNone TreeAnnotationType = iota
	TreeAnnotationPrefix
	TreeAnnotationTree
	TreeAnnotationPrefixTree
	TreeAnnotationRoot
	TreeAnnotationRootTree
)

func (t TreeAnnotationType) String() string {
	switch t {
	case TreeAnnotationNone:
		return "none"
	case TreeAnnotationPrefix:
		return "prefix"
	case TreeAnnotationTree:
		return "tree"
	case TreeAnnotationPrefixTree:
		return "prefix,tree"
	case TreeAnnotationRoot:
		return "root"
	case TreeAnnotationRootTree:
		return "root,tree"
	default:
		return "unknown"
	}
}

func ParseTreeAnnotation(s string) TreeAnnotationType {
	switch s {
	case "prefix":
		return TreeAnnotationPrefix
	case "tree":
		return TreeAnnotationTree
	case "prefix,tree", "tree,prefix":
		return TreeAnnotationPrefixTree
	case "root":
		return TreeAnnotationRoot
	case "root,tree", "tree,root":
		return TreeAnnotationRootTree
	default:
		return TreeAnnotationNone
	}
}

// TreeStatement 表示tree语句
type TreeStatement struct {
	Annotation TreeAnnotationType
	Root       Expression
	Body       []Statement
	Pos        Position
}

// statementNode 实现Statement接口
func (t *TreeStatement) statementNode() {}

// String 实现Node接口
func (t *TreeStatement) String() string {
	return "TreeStatement(" + t.Annotation.String() + ")"
}

// GetPosition 实现Node接口
func (t *TreeStatement) GetPosition() Position {
	return t.Pos
}

// SetPosition 实现Node接口
func (t *TreeStatement) SetPosition(pos Position) {
	t.Pos = pos
}

// GetAnnotation 获取注解类型
func (t *TreeStatement) GetAnnotation() TreeAnnotationType {
	return t.Annotation
}

// IsRootTree 检查是否是root tree
func (t *TreeStatement) IsRootTree() bool {
	return t.Annotation == TreeAnnotationRoot || t.Annotation == TreeAnnotationRootTree
}

// IsPrefixTree 检查是否是prefix tree
func (t *TreeStatement) IsPrefixTree() bool {
	return t.Annotation == TreeAnnotationPrefix || t.Annotation == TreeAnnotationPrefixTree
}

// IsOrphan 检查是否是孤儿tree（没有root且不是prefix）
func (t *TreeStatement) IsOrphan() bool {
	return t.Annotation == TreeAnnotationTree
}

// AddStatement 添加语句到tree body
func (t *TreeStatement) AddStatement(stmt Statement) {
	t.Body = append(t.Body, stmt)
}

// GetBody 获取tree body
func (t *TreeStatement) GetBody() []Statement {
	return t.Body
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

// TypeParameter 表示泛型类型参数
type TypeParameter struct {
	Name      string
	Constraint string // 类型约束，如 "any", "comparable" 等
	Pos       Position
}

// GetPosition 实现 Node 接口
func (tp *TypeParameter) GetPosition() Position {
	return tp.Pos
}

// SetPosition 实现 Node 接口
func (tp *TypeParameter) SetPosition(pos Position) {
	tp.Pos = pos
}

// String 实现 Node 接口
func (tp *TypeParameter) String() string {
	return "TypeParameter(" + tp.Name + ")"
}

// TypeArgument 表示泛型类型实参
type TypeArgument struct {
	Type string
	Pos  Position
}

// GetPosition 实现 Node 接口
func (ta *TypeArgument) GetPosition() Position {
	return ta.Pos
}

// SetPosition 实现 Node 接口
func (ta *TypeArgument) SetPosition(pos Position) {
	ta.Pos = pos
}

// String 实现 Node 接口
func (ta *TypeArgument) String() string {
	return "TypeArgument(" + ta.Type + ")"
}

// GenericInstance 表示泛型实例
type GenericInstance struct {
	OriginalName   string
	TypeArguments  []TypeArgument
	InstantiatedName string
	Pos            Position
}

// GetPosition 实现 Node 接口
func (gi *GenericInstance) GetPosition() Position {
	return gi.Pos
}

// SetPosition 实现 Node 接口
func (gi *GenericInstance) SetPosition(pos Position) {
	gi.Pos = pos
}

// String 实现 Node 接口
func (gi *GenericInstance) String() string {
	return "GenericInstance(" + gi.OriginalName + ")"
}

// statementNode 实现 Statement 接口
func (gi *GenericInstance) statementNode() {}

// FunctionStatement 表示函数语句
type FunctionStatement struct {
	Name          string
	TypeParams    []*TypeParameter // 泛型类型参数
	Params        []string         // 参数名称列表
	ParamTypes    []string         // 参数类型列表
	Body          []Statement
	AsmBody       string           // asm 函数的原始函数体内容
	ReturnType    string
	Generic       bool      // 是否是泛型函数
	NoKMM         bool      // 是否禁用 KMM 内存管理（兼容旧 #[no_kmm]）
	Inline        bool      // 是否内联函数（兼容旧 #[inline]）
	Annotation    TreeAnnotationType // 函数注解 (prefix,tree, root,tree)
	SOREnabled    bool      // 函数级 SOR 启用（兼容旧 #[sor] 注解）
	Attributes    []*Attribute // 统一属性列表（新）
	IsPublic      bool      // pub 修饰符：导出给其他 .kl 文件
	PrefixName    string    // 如果使用prefix，记录prefix名称
	TaskParams    []*TaskParam // 任务参数列表（如 task(1)）
	AsyncParams   []*AsyncParam // 异步参数列表（如 async(value)）
	IsAsm         bool      // 是否是 asm 函数（#[asm] 注解）
	Pos           Position
}

// TaskParam 表示任务参数
// 用于函数定义中的 task(优先级) 语法
type TaskParam struct {
	Priority Expression // 优先级表达式
	Pos     Position
}

// AsyncParam 表示异步参数
// 用于函数定义中的 async(值) 语法
type AsyncParam struct {
	Value Expression // 异步值表达式
	Pos   Position
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

// AddTypeParam 添加类型参数
func (f *FunctionStatement) AddTypeParam(tp *TypeParameter) {
	f.TypeParams = append(f.TypeParams, tp)
	f.Generic = true
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

// IsGeneric 检查是否是泛型函数
func (f *FunctionStatement) IsGeneric() bool {
	return f.Generic || len(f.TypeParams) > 0
}

// GetAnnotation 获取注解类型
func (f *FunctionStatement) GetAnnotation() TreeAnnotationType {
	return f.Annotation
}

// IsPrefixFunction 检查是否使用prefix
func (f *FunctionStatement) IsPrefixFunction() bool {
	return f.Annotation == TreeAnnotationPrefix || f.Annotation == TreeAnnotationPrefixTree
}

// IsTreeFunction 检查是否是tree函数
func (f *FunctionStatement) IsTreeFunction() bool {
	return f.Annotation == TreeAnnotationTree || f.Annotation == TreeAnnotationPrefixTree || f.Annotation == TreeAnnotationRootTree
}

// HasPrefixVar 检查是否有前缀变量（$开头）
func (f *FunctionStatement) HasPrefixVar(name string) bool {
	for _, stmt := range f.Body {
		if exprStmt, ok := stmt.(*ExpressionStatement); ok {
			if ident, ok := exprStmt.Expression.(*Identifier); ok {
				if len(ident.Name) > 0 && ident.Name[0] == '$' && ident.Name[1:] == name {
					return true
				}
			}
		}
	}
	return false
}

// GetGenericSignature 获取泛型签名
func (f *FunctionStatement) GetGenericSignature() string {
	if !f.IsGeneric() {
		return ""
	}
	sig := "["
	for i, tp := range f.TypeParams {
		if i > 0 {
			sig += ", "
		}
		sig += tp.Name
		if tp.Constraint != "" && tp.Constraint != "any" {
			sig += ": " + tp.Constraint
		}
	}
	sig += "]"
	return sig
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

// BreakStatement 表示 break 语句
type BreakStatement struct {
	Pos Position
}

func (b *BreakStatement) statementNode() {}
func (b *BreakStatement) String() string {
	return "BreakStatement"
}
func (b *BreakStatement) GetPosition() Position {
	return b.Pos
}
func (b *BreakStatement) SetPosition(pos Position) {
	b.Pos = pos
}

// ContinueStatement 表示 continue 语句
type ContinueStatement struct {
	Pos Position
}

func (c *ContinueStatement) statementNode() {}
func (c *ContinueStatement) String() string {
	return "ContinueStatement"
}
func (c *ContinueStatement) GetPosition() Position {
	return c.Pos
}
func (c *ContinueStatement) SetPosition(pos Position) {
	c.Pos = pos
}

// ImportStatement 表示import语句
type ImportStatement struct {
	Module   string // 模块名（如 "std.io" 或 "utils"）
	IsLocal  bool   // 是否是本地 .kl 文件导入
	LocalPath string // 解析后的本地 .kl 文件路径（仅 IsLocal=true 时有效）
	Pos      Position
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

// SetPosition 实现 Node 接口
func (i *ImportStatement) SetPosition(pos Position) {
	i.Pos = pos
}

// ExportStatement 表示 export 语句
type ExportStatement struct {
	Name string    // 导出的名称（函数、类、对象等）
	Type string    // 导出类型："function", "class", "object", "variable"
	Pos  Position  // 源代码位置
}

// statementNode 实现 Statement 接口
func (e *ExportStatement) statementNode() {}

// String 实现 Node 接口
func (e *ExportStatement) String() string {
	return "ExportStatement"
}

// GetPosition 实现 Node 接口
func (e *ExportStatement) GetPosition() Position {
	return e.Pos
}

// SetPosition 实现 Node 接口
func (e *ExportStatement) SetPosition(pos Position) {
	e.Pos = pos
}

// PackageStatement 表示 package 声明
type PackageStatement struct {
	Name string   // 包名（如 "utils", "math.geometry"）
	Pos  Position
}

func (p *PackageStatement) statementNode() {}
func (p *PackageStatement) String() string  { return "PackageStatement" }
func (p *PackageStatement) GetPosition() Position { return p.Pos }
func (p *PackageStatement) SetPosition(pos Position) { p.Pos = pos }

// NonLocalStatement 表示 nonlocal 语句
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
	Type       string
	Name       string
	Value      Expression
	Nullable   bool
	IsAuto     bool
	IsPublic   bool // pub 修饰符
	IsStatic   bool // static 修饰符：函数内静态变量，生命周期为整个程序
	IsConst    bool // const 修饰符：编译期常量，不可修改
	Attributes []*Attribute // 声明注解：#[volatile], #[section("...")], #[aligned(N)] 等
	Pos        Position
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
	IsPrefixVar bool
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
	Value uint64
	Pos   Position
}

// expressionNode 实现Expression接口
func (i *IntegerLiteral) expressionNode() {}

// String 实现Node接口
func (i *IntegerLiteral) String() string {
	return "IntegerLiteral(" + strconv.FormatUint(i.Value, 10) + ")"
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

// expressionNode 实现 Expression 接口
func (s *StringLiteral) expressionNode() {}

// String 实现 Node 接口
func (s *StringLiteral) String() string {
	return "StringLiteral(" + s.Value + ")"
}

// GetPosition 实现 Node 接口
func (s *StringLiteral) GetPosition() Position {
	return s.Pos
}

// SetPosition 实现 Node 接口
func (s *StringLiteral) SetPosition(pos Position) {
	s.Pos = pos
}

// CharLiteral 表示字符字面量
type CharLiteral struct {
	Value string
	Pos   Position
}

func (c *CharLiteral) expressionNode() {}
func (c *CharLiteral) String() string {
	return "CharLiteral(" + c.Value + ")"
}
func (c *CharLiteral) GetPosition() Position {
	return c.Pos
}
func (c *CharLiteral) SetPosition(pos Position) {
	c.Pos = pos
}

// BooleanLiteral 表示布尔字面量
type BooleanLiteral struct {
	Value bool
	Pos   Position
}

// expressionNode 实现 Expression 接口
func (b *BooleanLiteral) expressionNode() {}

// String 实现 Node 接口
func (b *BooleanLiteral) String() string {
	if b.Value {
		return "BooleanLiteral(true)"
	}
	return "BooleanLiteral(false)"
}

// GetPosition 实现 Node 接口
func (b *BooleanLiteral) GetPosition() Position {
	return b.Pos
}

// SetPosition 实现 Node 接口
func (b *BooleanLiteral) SetPosition(pos Position) {
	b.Pos = pos
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
	Function  Expression
	TypeArgs  []string   // 泛型类型参数
	Args      []Expression
	Pos       Position
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
// 语法: @PrefixName(param1=value1, param2=value2) { body }
type PrefixCallExpression struct {
	Name       string
	Params     map[string]Expression
	Body       []Statement
	Annotation TreeAnnotationType
	Pos        Position
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

// GetAnnotation 获取注解类型
func (p *PrefixCallExpression) GetAnnotation() TreeAnnotationType {
	return p.Annotation
}

// IsPrefixCall 检查是否是前缀调用
func (p *PrefixCallExpression) IsPrefixCall() bool {
	return true
}

// GetParam 获取参数值
func (p *PrefixCallExpression) GetParam(name string) (Expression, bool) {
	if p.Params == nil {
		return nil, false
	}
	expr, ok := p.Params[name]
	return expr, ok
}

// AddParam 添加参数
func (p *PrefixCallExpression) AddParam(name string, expr Expression) {
	if p.Params == nil {
		p.Params = make(map[string]Expression)
	}
	p.Params[name] = expr
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
	TypeParams  []*TypeParameter // 泛型类型参数
	Fields      []*FieldDeclaration
	Methods     []*MethodStatement
	Constructors []*ConstructorStatement
	Implements  []string
	Generic     bool // 是否是泛型类
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
	BitWidth int    // 位域宽度（0=普通字段，>0=位域，如 flags: u32 : 5 → BitWidth=5）
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

// TypeCastExpression 表示类型转换表达式
// 语法: (type)(expr) 例如 (i64)(result)
type TypeCastExpression struct {
	TargetType string     // 目标类型，如 "i64", "f64" 等
	Expression Expression // 被转换的表达式
	Pos        Position
}

// 让 TypeCastExpression 实现 expressionNode 方法
func (t *TypeCastExpression) expressionNode() {}

// GetPosition 实现Node接口
func (t *TypeCastExpression) GetPosition() Position {
	return t.Pos
}

// SetPosition 实现Node接口
func (t *TypeCastExpression) SetPosition(pos Position) {
	t.Pos = pos
}

// String 实现Node接口
func (t *TypeCastExpression) String() string {
	return fmt.Sprintf("(%s)(%s)", t.TargetType, t.Expression.String())
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
	Name       string
	TypeParams []*TypeParameter // 泛型类型参数
	Fields     []*FieldDeclaration
	Generic    bool // 是否是泛型结构体
	Attributes []*Attribute // 声明注解：#[packed], #[aligned(N)] 等
	Pos        Position
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

// TypeParam 表示函数类型参数
type TypeFuncParam struct {
	Type     string
	Nullable bool
	Pos      Position
}

// TypeAliasStatement 表示 type 关键字自定义类型定义
type TypeAliasStatement struct {
	Name           string           // 新类型名称
	UnderlyingType string           // 底层类型（基本类型、指针、数组等）
	TypeParams     []*TypeParameter // 泛型类型参数
	Generic        bool             // 是否是泛型类型别名
	IsFuncType     bool             // 是否是函数类型
	FuncReturnType string           // 函数返回类型（仅用于函数类型）
	FuncParams     []*TypeFuncParam // 函数参数列表（仅用于函数类型）
	Pos            Position
}

// statementNode 实现 Statement 接口
func (ta *TypeAliasStatement) statementNode() {}

// String 实现 Node 接口
func (ta *TypeAliasStatement) String() string {
	return "TypeAliasStatement(" + ta.Name + " = " + ta.UnderlyingType + ")"
}

// GetPosition 实现 Node 接口
func (ta *TypeAliasStatement) GetPosition() Position {
	return ta.Pos
}

// SetPosition 实现 Node 接口
func (ta *TypeAliasStatement) SetPosition(pos Position) {
	ta.Pos = pos
}

// UnaryExpression 表示一元表达式
type UnaryExpression struct {
	Operator string
	Right    Expression
	Pos      Position
}

// expressionNode 实现Expression接口
func (u *UnaryExpression) expressionNode() {}

// String 实现Node接口
func (u *UnaryExpression) String() string {
	return "UnaryExpression(" + u.Operator + ")"
}

// GetPosition 实现Node接口
func (u *UnaryExpression) GetPosition() Position {
	return u.Pos
}

// SetPosition 实现Node接口
func (u *UnaryExpression) SetPosition(pos Position) {
	u.Pos = pos
}

// MemberExpression 表示成员访问表达式
type MemberExpression struct {
	Object Expression
	Member string
	Pos    Position
}

// expressionNode 实现Expression接口
func (m *MemberExpression) expressionNode() {}

// String 实现Node接口
func (m *MemberExpression) String() string {
	return "MemberExpression(" + m.Member + ")"
}

// GetPosition 实现Node接口
func (m *MemberExpression) GetPosition() Position {
	return m.Pos
}

// SetPosition 实现Node接口
func (m *MemberExpression) SetPosition(pos Position) {
	m.Pos = pos
}

// LiteralExpression 表示字面量表达式
type LiteralExpression struct {
	Kind  string
	Value interface{}
	Pos   Position
}

// expressionNode 实现Expression接口
func (l *LiteralExpression) expressionNode() {}

// String 实现Node接口
func (l *LiteralExpression) String() string {
	return "LiteralExpression(" + l.Kind + ")"
}

// GetPosition 实现Node接口
func (l *LiteralExpression) GetPosition() Position {
	return l.Pos
}

// SetPosition 实现Node接口
func (l *LiteralExpression) SetPosition(pos Position) {
	l.Pos = pos
}

// ParenExpression 表示括号表达式
type ParenExpression struct {
	Inner Expression
	Pos   Position
}

// expressionNode 实现Expression接口
func (p *ParenExpression) expressionNode() {}

// String 实现Node接口
func (p *ParenExpression) String() string {
	return "ParenExpression"
}

// GetPosition 实现Node接口
func (p *ParenExpression) GetPosition() Position {
	return p.Pos
}

// SetPosition 实现Node接口
func (p *ParenExpression) SetPosition(pos Position) {
	p.Pos = pos
}

// ConditionalExpression 表示条件表达式
type ConditionalExpression struct {
	Condition Expression
	TrueExpr  Expression
	FalseExpr Expression
	Pos       Position
}

// expressionNode 实现Expression接口
func (c *ConditionalExpression) expressionNode() {}

// String 实现Node接口
func (c *ConditionalExpression) String() string {
	return "ConditionalExpression"
}

// GetPosition 实现Node接口
func (c *ConditionalExpression) GetPosition() Position {
	return c.Pos
}

// SetPosition 实现Node接口
func (c *ConditionalExpression) SetPosition(pos Position) {
	c.Pos = pos
}

// LambdaExpression 表示 lambda/闭包表达式
// 语法: fn(参数列表) { 函数体 } 或 fn(参数列表) -> 返回类型 { 函数体 }
type LambdaExpression struct {
	Params     []string   // 参数名列表
	ParamTypes []string   // 参数类型列表
	ReturnType string     // 返回类型（空=void）
	Body       []Statement
	Captures   []string   // 捕获的外部变量名（由语义分析填充）
	Pos        Position
}

func (l *LambdaExpression) expressionNode() {}
func (l *LambdaExpression) String() string  { return "LambdaExpression" }
func (l *LambdaExpression) GetPosition() Position { return l.Pos }
func (l *LambdaExpression) SetPosition(pos Position) { l.Pos = pos }

// ArrayLiteral 表示数组字面量
type ArrayLiteral struct {
	Elements []Expression
	Pos      Position
}

// expressionNode 实现Expression接口
func (a *ArrayLiteral) expressionNode() {}

// String 实现Node接口
func (a *ArrayLiteral) String() string {
	return "ArrayLiteral"
}

// GetPosition 实现Node接口
func (a *ArrayLiteral) GetPosition() Position {
	return a.Pos
}

// SetPosition 实现Node接口
func (a *ArrayLiteral) SetPosition(pos Position) {
	a.Pos = pos
}

// IsGeneric 检查是否是泛型类型别名
func (ta *TypeAliasStatement) IsGeneric() bool {
	return ta.Generic || len(ta.TypeParams) > 0
}

// YieldStatement 表示 yield（所有权转移）语句
// 语法: yield source -> target
type YieldStatement struct {
	Source Expression // 被转移的对象
	Target string    // 目标变量名
	Pos    Position
}

func (y *YieldStatement) statementNode() {}
func (y *YieldStatement) String() string { return "YieldStatement" }
func (y *YieldStatement) GetPosition() Position { return y.Pos }
func (y *YieldStatement) SetPosition(pos Position) { y.Pos = pos }

// ReleaseStatement 表示 release（所有权分发）语句
// 语法: release source -> [holder1, holder2, ...]
type ReleaseStatement struct {
	Source  Expression // 被分发的对象
	Holders []string  // 持有者变量名列表
	Pos     Position
}

func (r *ReleaseStatement) statementNode() {}
func (r *ReleaseStatement) String() string { return "ReleaseStatement" }
func (r *ReleaseStatement) GetPosition() Position { return r.Pos }
func (r *ReleaseStatement) SetPosition(pos Position) { r.Pos = pos }

// ExtractStatement 表示 extract（子结构提取）语句
// 语法: extract source[index] -> target
type ExtractStatement struct {
	Source Expression // 源对象
	Index  Expression // 提取的索引
	Target string    // 目标变量名
	Pos    Position
}

func (e *ExtractStatement) statementNode() {}
func (e *ExtractStatement) String() string { return "ExtractStatement" }
func (e *ExtractStatement) GetPosition() Position { return e.Pos }
func (e *ExtractStatement) SetPosition(pos Position) { e.Pos = pos }

type SizeOfExpression struct {
	TargetType string
	Pos        Position
}

func (s *SizeOfExpression) expressionNode() {}
func (s *SizeOfExpression) String() string { return "SizeOfExpression" }
func (s *SizeOfExpression) GetPosition() Position { return s.Pos }
func (s *SizeOfExpression) SetPosition(pos Position) { s.Pos = pos }

type AlignOfExpression struct {
	TargetType string
	Pos        Position
}

func (a *AlignOfExpression) expressionNode() {}
func (a *AlignOfExpression) String() string { return "AlignOfExpression" }
func (a *AlignOfExpression) GetPosition() Position { return a.Pos }
func (a *AlignOfExpression) SetPosition(pos Position) { a.Pos = pos }

type OffsetOfExpression struct {
	TargetType string
	FieldName  string
	Pos        Position
}

func (o *OffsetOfExpression) expressionNode() {}
func (o *OffsetOfExpression) String() string { return "OffsetOfExpression" }
func (o *OffsetOfExpression) GetPosition() Position { return o.Pos }
func (o *OffsetOfExpression) SetPosition(pos Position) { o.Pos = pos }

type ComptimeExpression struct {
	Inner Expression
	Pos   Position
}

func (c *ComptimeExpression) expressionNode() {}
func (c *ComptimeExpression) String() string { return "ComptimeExpression" }
func (c *ComptimeExpression) GetPosition() Position { return c.Pos }
func (c *ComptimeExpression) SetPosition(pos Position) { c.Pos = pos }

type TypeNameExpression struct {
	TargetType string
	Pos        Position
}

func (t *TypeNameExpression) expressionNode() {}
func (t *TypeNameExpression) String() string { return "TypeNameExpression" }
func (t *TypeNameExpression) GetPosition() Position { return t.Pos }
func (t *TypeNameExpression) SetPosition(pos Position) { t.Pos = pos }

type FieldCountExpression struct {
	TargetType string
	Pos        Position
}

func (f *FieldCountExpression) expressionNode() {}
func (f *FieldCountExpression) String() string { return "FieldCountExpression" }
func (f *FieldCountExpression) GetPosition() Position { return f.Pos }
func (f *FieldCountExpression) SetPosition(pos Position) { f.Pos = pos }

type FieldNameExpression struct {
	TargetType string
	Index      Expression
	Pos        Position
}

func (f *FieldNameExpression) expressionNode() {}
func (f *FieldNameExpression) String() string { return "FieldNameExpression" }
func (f *FieldNameExpression) GetPosition() Position { return f.Pos }
func (f *FieldNameExpression) SetPosition(pos Position) { f.Pos = pos }

type FieldTypeExpression struct {
	TargetType string
	Index      Expression
	Pos        Position
}

func (f *FieldTypeExpression) expressionNode() {}
func (f *FieldTypeExpression) String() string { return "FieldTypeExpression" }
func (f *FieldTypeExpression) GetPosition() Position { return f.Pos }
func (f *FieldTypeExpression) SetPosition(pos Position) { f.Pos = pos }

type TypeKindExpression struct {
	TargetType string
	Pos        Position
}

func (t *TypeKindExpression) expressionNode() {}
func (t *TypeKindExpression) String() string { return "TypeKindExpression" }
func (t *TypeKindExpression) GetPosition() Position { return t.Pos }
func (t *TypeKindExpression) SetPosition(pos Position) { t.Pos = pos }

// ==================== 属性注解系统 ====================

// Attribute 表示声明注解
// 语法: #[name] 或 #[name(arg1, arg2)]
// 用途: 修饰紧随其后的声明（fn/struct/var/type 等）
type Attribute struct {
	Name  string   // 属性名："packed", "aligned", "volatile", "section", "naked", "inline", "deprecated", "weak", "no_kmm", "sor"
	Args  []string // 参数列表：#[section(".isr_vector")] → [".isr_vector"]，#[aligned(16)] → ["16"]
	Pos   Position
}

func (a *Attribute) String() string {
	if len(a.Args) > 0 {
		return fmt.Sprintf("Attribute(%s, args=%v)", a.Name, a.Args)
	}
	return fmt.Sprintf("Attribute(%s)", a.Name)
}

func (a *Attribute) GetPosition() Position { return a.Pos }
func (a *Attribute) SetPosition(pos Position) { a.Pos = pos }

// ParseAttributeString 解析属性内容字符串
// 输入: "packed" → Attribute{Name:"packed"}
// 输入: "aligned(16)" → Attribute{Name:"aligned", Args:["16"]}
// 输入: "section(\".isr_vector\")" → Attribute{Name:"section", Args:[".isr_vector"]}
func ParseAttributeString(content string) *Attribute {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	attr := &Attribute{}

	// 检查是否有参数括号
	parenIdx := strings.Index(content, "(")
	if parenIdx < 0 {
		// 无参数: #[packed]
		attr.Name = content
		return attr
	}

	// 有参数: #[aligned(16)] 或 #[section(".isr_vector")]
	attr.Name = strings.TrimSpace(content[:parenIdx])

	// 提取括号内的参数
	rest := content[parenIdx+1:]
	// 找到匹配的右括号
	depth := 1
	i := 0
	for i < len(rest) && depth > 0 {
		if rest[i] == '(' {
			depth++
		} else if rest[i] == ')' {
			depth--
		}
		i++
	}
	argsStr := strings.TrimSpace(rest[:i-1]) // 去掉尾部的 )

	if argsStr != "" {
		// 分割参数，注意处理带引号的字符串参数
		attr.Args = parseAttributeArgs(argsStr)
	}

	return attr
}

// parseAttributeArgs 解析属性参数列表
// "16" → ["16"]
// ".isr_vector" → [".isr_vector"]
// "4, 2" → ["4", "2"]
func parseAttributeArgs(argsStr string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(argsStr); i++ {
		c := argsStr[i]
		if inQuotes {
			current.WriteByte(c)
			if c == quoteChar {
				inQuotes = false
			}
		} else if c == '"' || c == '\'' {
			inQuotes = true
			quoteChar = c
			current.WriteByte(c)
		} else if c == ',' {
			arg := strings.TrimSpace(current.String())
			if arg != "" {
				args = append(args, arg)
			}
			current.Reset()
		} else {
			current.WriteByte(c)
		}
	}

	arg := strings.TrimSpace(current.String())
	if arg != "" {
		args = append(args, arg)
	}

	return args
}

// ParseAttributeList 解析 #[attr1, attr2(arg)] 格式的属性列表
func ParseAttributeList(content string) []*Attribute {
	var attrs []*Attribute

	// 逐个分割，注意不要在括号内分割
	depth := 0
	start := 0
	for i := 0; i < len(content); i++ {
		c := content[i]
		if c == '(' {
			depth++
		} else if c == ')' {
			depth--
		} else if c == ',' && depth == 0 {
			part := strings.TrimSpace(content[start:i])
			if part != "" {
				if attr := ParseAttributeString(part); attr != nil {
					attrs = append(attrs, attr)
				}
			}
			start = i + 1
		}
	}
	// 最后一个
	part := strings.TrimSpace(content[start:])
	if part != "" {
		if attr := ParseAttributeString(part); attr != nil {
			attrs = append(attrs, attr)
		}
	}

	return attrs
}

// HasAttribute 检查属性列表中是否有指定名称的属性
func HasAttribute(attrs []*Attribute, name string) bool {
	for _, a := range attrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// GetAttribute 获取指定名称的属性
func GetAttribute(attrs []*Attribute, name string) *Attribute {
	for _, a := range attrs {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// ==================== 表达式级属性（Attribute Expression） ====================
// 特殊操作统一使用 #[name(args)] 作为表达式嵌入，避免为每个操作发明新语法
// 示例:
//   let cr3 = #[asm("mov %cr3, %rax")]
//   let val = #[volatile_load(ptr)]
//   let old = #[atomic_cas(ptr, expected, new)]
//   #[fence()]
type AttributeExpression struct {
	Attr *Attribute  // 属性定义（名称 + 参数列表）
	Pos  Position
}

func (a *AttributeExpression) expressionNode() {}
func (a *AttributeExpression) String() string {
	if a.Attr != nil {
		return "AttributeExpression(" + a.Attr.Name + ")"
	}
	return "AttributeExpression"
}
func (a *AttributeExpression) GetPosition() Position { return a.Pos }
func (a *AttributeExpression) SetPosition(pos Position) { a.Pos = pos }

// ==================== extern 外部符号声明 ====================
// 变量语法: extern name: type
// 函数语法: extern fn name(params) -> return_type
// 用于声明链接脚本符号或外部 C/汇编函数，不分配存储
// 示例:
//   extern bss_start: u8
//   extern stack_top: u64
//   extern fn boot_main() -> void
//   extern fn memset(dst: *u8, c: i32, n: usize) -> *u8
type ExternStatement struct {
	Name        string
	Type        string   // 变量类型（函数时为返回类型）
	Nullable    bool
	IsFunction  bool     // 是否是函数声明
	Params      []string // 参数名列表（函数时有效）
	ParamTypes  []string // 参数类型列表（函数时有效）
	ReturnType  string   // 返回类型（函数时有效）
	Pos         Position
}

func (e *ExternStatement) statementNode() {}
func (e *ExternStatement) String() string { return "ExternStatement(" + e.Name + ": " + e.Type + ")" }
func (e *ExternStatement) GetPosition() Position { return e.Pos }
func (e *ExternStatement) SetPosition(pos Position) { e.Pos = pos }

// ==================== ADT（代数数据类型）+ match（模式匹配） ====================

// EnumStatement 表示带数据的枚举定义（代数数据类型）
// 语法: enum Name { Variant1, Variant2(Type), Variant3(Type1, Type2) }
type EnumStatement struct {
	Name       string          // 枚举名
	Variants   []*EnumVariant  // 变体列表
	TypeParams []*TypeParameter // 泛型类型参数（如 enum Result<T, E>）
	Generic    bool
	Attributes []*Attribute
	Pos        Position
}

// EnumVariant 表示枚举变体
type EnumVariant struct {
	Name       string     // 变体名
	FieldTypes []string   // 关联数据的类型列表（空=无数据变体）
	FieldNames []string   // 关联数据的字段名（可选）
}

func (e *EnumStatement) statementNode() {}
func (e *EnumStatement) String() string { return "EnumStatement(" + e.Name + ")" }
func (e *EnumStatement) GetPosition() Position { return e.Pos }
func (e *EnumStatement) SetPosition(pos Position) { e.Pos = pos }

func (v *EnumVariant) String() string {
	if len(v.FieldTypes) == 0 {
		return "EnumVariant(" + v.Name + ")"
	}
	return "EnumVariant(" + v.Name + "(" + strings.Join(v.FieldTypes, ", ") + "))"
}
func (v *EnumVariant) GetPosition() Position { return Position{} }
func (v *EnumVariant) SetPosition(pos Position) {}

// MatchExpression 表示模式匹配表达式
// 语法: match(expr) { Pattern1 => body1, Pattern2(x) => body2, _ => default_body }
type MatchExpression struct {
	Target    Expression      // 被匹配的表达式
	Arms      []*MatchArm     // 匹配分支
	Pos       Position
}

// MatchArm 表示模式匹配的一个分支
type MatchArm struct {
	Pattern   *MatchPattern   // 模式
	Body      []Statement     // 匹配后执行的语句
	Pos       Position
}

// MatchPattern 表示模式匹配的模式
type MatchPattern struct {
	Kind      MatchPatternKind // 模式类型
	VariantName string         // 变体名（Kind=Variant 时有效）
	Bindings []string          // 绑定的变量名（Kind=Variant/Variable 时有效）
	IntValue int64             // 整数字面量值（Kind=Integer 时有效）
	StrValue string            // 字符串字面量值（Kind=String 时有效）
	Pos      Position
}

// MatchPatternKind 模式类型
type MatchPatternKind int

const (
	PatternWildcard  MatchPatternKind = iota // _ 通配符
	PatternVariant                            // EnumVariant(x, y)
	PatternInteger                            // 整数字面量
	PatternString                             // 字符串字面量
	PatternVariable                           // 变量绑定
	PatternBoolean                            // true/false
)

func (e *MatchExpression) expressionNode() {}
func (e *MatchExpression) String() string { return "MatchExpression" }
func (e *MatchExpression) GetPosition() Position { return e.Pos }
func (e *MatchExpression) SetPosition(pos Position) { e.Pos = pos }

func (a *MatchArm) statementNode() {}
func (a *MatchArm) String() string { return "MatchArm" }
func (a *MatchArm) GetPosition() Position { return a.Pos }
func (a *MatchArm) SetPosition(pos Position) { a.Pos = pos }

func (p *MatchPattern) String() string {
	switch p.Kind {
	case PatternWildcard:
		return "PatternWildcard(_)"
	case PatternVariant:
		if len(p.Bindings) > 0 {
			return "PatternVariant(" + p.VariantName + "(" + strings.Join(p.Bindings, ", ") + "))"
		}
		return "PatternVariant(" + p.VariantName + ")"
	case PatternInteger:
		return fmt.Sprintf("PatternInteger(%d)", p.IntValue)
	case PatternString:
		return "PatternString(" + p.StrValue + ")"
	case PatternVariable:
		return "PatternVariable(" + strings.Join(p.Bindings, ", ") + ")"
	case PatternBoolean:
		return "PatternBoolean"
	default:
		return "PatternUnknown"
	}
}
func (p *MatchPattern) GetPosition() Position { return p.Pos }
func (p *MatchPattern) SetPosition(pos Position) { p.Pos = pos }
