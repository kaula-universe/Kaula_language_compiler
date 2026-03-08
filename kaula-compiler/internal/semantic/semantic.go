package semantic

import (
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/lexer"
)

// Analyzer 表示语义分析器
type Analyzer struct {
	errorCollector *errors.ErrorCollector
	currentScope   *Scope
	rootScope      *Scope
}

// Scope 表示作用域
type Scope struct {
	name     string
	parent   *Scope
	symbols  map[string]*Symbol
	depth    int
}

// Symbol 表示符号
type Symbol struct {
	name     string
	typeName string
	nullable bool
	line     int
	column   int
}

// NewAnalyzer 创建一个新的语义分析器
func NewAnalyzer(errorCollector *errors.ErrorCollector) *Analyzer {
	rootScope := NewScope("global", nil)
	return &Analyzer{
		errorCollector: errorCollector,
		currentScope:   rootScope,
		rootScope:      rootScope,
	}
}

// NewScope 创建一个新的作用域
func NewScope(name string, parent *Scope) *Scope {
	depth := 0
	if parent != nil {
		depth = parent.depth + 1
	}
	return &Scope{
		name:    name,
		parent:  parent,
		symbols: make(map[string]*Symbol),
		depth:   depth,
	}
}

// EnterScope 进入一个新的作用域
func (a *Analyzer) EnterScope(name string) {
	newScope := NewScope(name, a.currentScope)
	a.currentScope = newScope
}

// ExitScope 退出当前作用域
func (a *Analyzer) ExitScope() {
	if a.currentScope != a.rootScope {
		a.currentScope = a.currentScope.parent
	}
}

// AddSymbol 添加一个符号到当前作用域
func (a *Analyzer) AddSymbol(name, typeName string, nullable bool, line, column int) {
	if _, exists := a.currentScope.symbols[name]; exists {
		a.errorCollector.AddSemanticError("Variable already declared: " + name, line, column, "", "Rename the variable or remove the duplicate declaration")
		return
	}
	a.currentScope.symbols[name] = &Symbol{
		name:     name,
		typeName: typeName,
		nullable: nullable,
		line:     line,
		column:   column,
	}
}

// GetSymbol 获取一个符号
func (a *Analyzer) GetSymbol(name string) *Symbol {
	for scope := a.currentScope; scope != nil; scope = scope.parent {
		if symbol, exists := scope.symbols[name]; exists {
			return symbol
		}
	}
	return nil
}

// HasSymbol 检查是否存在符号
func (a *Analyzer) HasSymbol(name string) bool {
	return a.GetSymbol(name) != nil
}

// Analyze 分析程序的语义
func (a *Analyzer) Analyze(program *ast.Program) bool {
	// 分析所有语句
	for _, stmt := range program.Statements {
		a.analyzeStatement(stmt)
	}
	return !a.errorCollector.HasErrors()
}

// analyzeStatement 分析语句的语义
func (a *Analyzer) analyzeStatement(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		a.analyzeVariableDeclaration(s)
	case *ast.FunctionStatement:
		a.analyzeFunctionStatement(s)
	case *ast.IfStatement:
		a.analyzeIfStatement(s)
	case *ast.WhileStatement:
		a.analyzeWhileStatement(s)
	case *ast.ForStatement:
		a.analyzeForStatement(s)
	case *ast.BlockStatement:
		a.analyzeBlockStatement(s)
	case *ast.ExpressionStatement:
		a.analyzeExpression(s.Expression)
	case *ast.ReturnStatement:
		a.analyzeReturnStatement(s)
	default:
		// 其他语句类型暂时不做语义分析
	}
}

// analyzeVariableDeclaration 分析变量声明的语义
func (a *Analyzer) analyzeVariableDeclaration(stmt *ast.VariableDeclaration) {
	// 检查变量名是否合法
	if !isValidIdentifier(stmt.Name) {
		a.errorCollector.AddSemanticError("Invalid variable name: " + stmt.Name, stmt.Pos.Line, stmt.Pos.Column, "", "Use a valid identifier starting with a letter or underscore")
		return
	}
	
	// 检查类型是否合法
	if !isValidType(stmt.Type) {
		a.errorCollector.AddSemanticError("Invalid type: " + stmt.Type, stmt.Pos.Line, stmt.Pos.Column, "", "Use a valid type (int, float, bool, string)")
		return
	}
	
	// 添加变量到符号表
	a.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, stmt.Pos.Line, stmt.Pos.Column)
	
	// 分析初始化表达式
	if stmt.Value != nil {
		a.analyzeExpression(stmt.Value)
		// TODO: 类型检查
	}
}

// analyzeFunctionStatement 分析函数声明的语义
func (a *Analyzer) analyzeFunctionStatement(stmt *ast.FunctionStatement) {
	// 检查函数名是否合法
	if !isValidIdentifier(stmt.Name) {
		a.errorCollector.AddError(errors.Error{
			Type:     errors.ErrorTypeSemantic,
			Message:  "Invalid function name: " + stmt.Name,
			Line:     stmt.Pos.Line,
			Column:   stmt.Pos.Column,
			Suggestion: "Use a valid identifier starting with a letter or underscore",
		})
		return
	}
	
	// 添加函数到符号表
	a.AddSymbol(stmt.Name, "function", false, stmt.Pos.Line, stmt.Pos.Column)
	
	// 进入函数作用域
	a.EnterScope("function_" + stmt.Name)
	
	// 分析函数参数
	for _, param := range stmt.Params {
		if !isValidIdentifier(param) {
			a.errorCollector.AddError(errors.Error{
				Type:     errors.ErrorTypeSemantic,
				Message:  "Invalid parameter name: " + param,
				Line:     stmt.Pos.Line,
				Column:   stmt.Pos.Column,
				Suggestion: "Use a valid identifier starting with a letter or underscore",
			})
			continue
		}
		a.AddSymbol(param, "void*", false, stmt.Pos.Line, stmt.Pos.Column)
	}
	
	// 分析函数体
	for _, bodyStmt := range stmt.Body {
		a.analyzeStatement(bodyStmt)
	}
	
	// 退出函数作用域
	a.ExitScope()
}

// analyzeIfStatement 分析if语句的语义
func (a *Analyzer) analyzeIfStatement(stmt *ast.IfStatement) {
	// 分析条件表达式
	a.analyzeExpression(stmt.Condition)
	
	// 分析if块
	a.EnterScope("if")
	for _, bodyStmt := range stmt.Body {
		a.analyzeStatement(bodyStmt)
	}
	a.ExitScope()
	
	// 分析else块
	if len(stmt.Else) > 0 {
		a.EnterScope("else")
		for _, elseStmt := range stmt.Else {
			a.analyzeStatement(elseStmt)
		}
		a.ExitScope()
	}
}

// analyzeWhileStatement 分析while语句的语义
func (a *Analyzer) analyzeWhileStatement(stmt *ast.WhileStatement) {
	// 分析条件表达式
	a.analyzeExpression(stmt.Condition)
	
	// 分析循环体
	a.EnterScope("while")
	for _, bodyStmt := range stmt.Body {
		a.analyzeStatement(bodyStmt)
	}
	a.ExitScope()
}

// analyzeForStatement 分析for语句的语义
func (a *Analyzer) analyzeForStatement(stmt *ast.ForStatement) {
	// 分析初始化语句
	if stmt.Init != nil {
		a.analyzeStatement(stmt.Init)
	}
	
	// 分析条件表达式
	if stmt.Condition != nil {
		a.analyzeExpression(stmt.Condition)
	}
	
	// 分析更新语句
	if stmt.Update != nil {
		a.analyzeStatement(stmt.Update)
	}
	
	// 分析循环体
	a.EnterScope("for")
	for _, bodyStmt := range stmt.Body {
		a.analyzeStatement(bodyStmt)
	}
	a.ExitScope()
}

// analyzeBlockStatement 分析块语句的语义
func (a *Analyzer) analyzeBlockStatement(stmt *ast.BlockStatement) {
	// 进入块作用域
	a.EnterScope("block")
	
	// 分析块内语句
	for _, bodyStmt := range stmt.Statements {
		a.analyzeStatement(bodyStmt)
	}
	
	// 退出块作用域
	a.ExitScope()
}

// analyzeReturnStatement 分析return语句的语义
func (a *Analyzer) analyzeReturnStatement(stmt *ast.ReturnStatement) {
	// 分析返回表达式
	if stmt.Value != nil {
		a.analyzeExpression(stmt.Value)
	}
}

// analyzeExpression 分析表达式的语义
func (a *Analyzer) analyzeExpression(expr ast.Expression) {
	switch e := expr.(type) {
	case *ast.Identifier:
		a.analyzeIdentifier(e)
	case *ast.BinaryExpression:
		a.analyzeBinaryExpression(e)
	case *ast.CallExpression:
		a.analyzeCallExpression(e)
	case *ast.IndexExpression:
		a.analyzeIndexExpression(e)
	default:
		// 其他表达式类型暂时不做语义分析
	}
}

// analyzeIdentifier 分析标识符的语义
func (a *Analyzer) analyzeIdentifier(expr *ast.Identifier) {
	// 检查是否是null关键字
	if expr.Name == "null" {
		return
	}
	
	// 检查变量是否已声明
	if !a.HasSymbol(expr.Name) {
		a.errorCollector.AddError(errors.Error{
			Type:     errors.ErrorTypeSemantic,
			Message:  "Undefined variable: " + expr.Name,
			Line:     expr.Pos.Line,
			Column:   expr.Pos.Column,
			Suggestion: "Declare the variable before using it",
		})
	}
}

// analyzeBinaryExpression 分析二元表达式的语义
func (a *Analyzer) analyzeBinaryExpression(expr *ast.BinaryExpression) {
	// 分析左操作数
	a.analyzeExpression(expr.Left)
	
	// 分析右操作数
	a.analyzeExpression(expr.Right)
	
	// TODO: 类型检查
}

// analyzeCallExpression 分析函数调用表达式的语义
func (a *Analyzer) analyzeCallExpression(expr *ast.CallExpression) {
	// 分析函数表达式
	a.analyzeExpression(expr.Function)
	
	// 分析参数表达式
	for _, arg := range expr.Args {
		a.analyzeExpression(arg)
	}
}

// analyzeIndexExpression 分析索引表达式的语义
func (a *Analyzer) analyzeIndexExpression(expr *ast.IndexExpression) {
	// 分析对象表达式
	a.analyzeExpression(expr.Object)
	
	// 分析索引表达式
	a.analyzeExpression(expr.Index)
}

// isValidIdentifier 检查标识符是否合法
func isValidIdentifier(name string) bool {
	if len(name) == 0 {
		return false
	}
	
	// 第一个字符必须是字母或下划线
	firstChar := name[0]
	if !((firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z') || firstChar == '_') {
		return false
	}
	
	// 后续字符必须是字母、数字或下划线
	for i := 1; i < len(name); i++ {
		char := name[i]
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_') {
			return false
		}
	}
	
	return true
}

// isValidType 检查类型是否合法
func isValidType(typeName string) bool {
	validTypes := map[string]bool{
		"int":    true,
		"float":  true,
		"bool":   true,
		"string": true,
	}
	
	return validTypes[typeName]
}
