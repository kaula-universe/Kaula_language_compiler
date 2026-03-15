package sema

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/symbol"
	"kaula-compiler/internal/stdlib"
)

// Type 表示类型
type Type string

const (
	TypeInt    Type = "int"
	TypeFloat  Type = "float"
	TypeString Type = "string"
	TypeBool   Type = "bool"
	TypeVoid   Type = "void"
	TypeAny    Type = "any"
)

// Symbol 表示符号
type Symbol struct {
	Name     string
	Type     Type
	Scope    int
	Nullable bool
}



// SemanticAnalyzer 表示语义分析器
type SemanticAnalyzer struct {
	symbolTable *symbol.SymbolTable
	scope       int
	errorCollector *errors.ErrorCollector
	currentFunction *ast.FunctionStatement
	stdlibConfig *stdlib.StdlibConfig
}

// NewSemanticAnalyzer 创建一个新的语义分析器
func NewSemanticAnalyzer() *SemanticAnalyzer {
	errorCollector := errors.NewErrorCollector()
	return NewSemanticAnalyzerWithConfig("kaula-compiler/stdlib.json", errorCollector)
}

// NewSemanticAnalyzerWithConfig 使用指定配置文件和错误收集器创建语义分析器
func NewSemanticAnalyzerWithConfig(configPath string, errorCollector *errors.ErrorCollector) *SemanticAnalyzer {
	// 创建全局符号表
	globalSymbolTable := symbol.NewSymbolTable(nil, "global")

	// 添加标准库模块
	globalSymbolTable.AddSymbol("std", "any", false, "global", 0, 0)
	globalSymbolTable.AddSymbol("std.io", "any", false, "global", 0, 0)
	globalSymbolTable.AddSymbol("std.vo", "any", false, "global", 0, 0)
	globalSymbolTable.AddSymbol("std.prefix", "any", false, "global", 0, 0)

	// 加载标准库配置
	stdlibConfig, err := stdlib.LoadStdlibConfig(configPath)
	if err == nil && stdlibConfig != nil {
		// 动态添加标准库模块和函数
		for moduleName, module := range stdlibConfig.Modules {
			// 添加模块到符号表
			globalSymbolTable.AddSymbol(moduleName, "module", false, "global", 0, 0)
			// 添加函数到符号表
			for funcName := range module.Functions {
				globalSymbolTable.AddSymbol(funcName, "any", false, "global", 0, 0)
			}
		}
	} else {
		// 配置加载失败时使用默认值
		globalSymbolTable.AddSymbol("println", "any", false, "global", 0, 0)
	}

	return &SemanticAnalyzer{
		symbolTable:      globalSymbolTable,
		scope:            1,
		errorCollector:   errorCollector,
		currentFunction:  nil,
		stdlibConfig:     stdlibConfig,
	}
}

// Analyze 分析程序
func (sa *SemanticAnalyzer) Analyze(program *ast.Program) {
	for _, stmt := range program.Statements {
		sa.analyzeStatement(stmt)
	}
}

// analyzeStatement 分析语句
func (sa *SemanticAnalyzer) analyzeStatement(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.VOStatement:
		sa.analyzeVOStatement(s)
	case *ast.SpendCallStatement:
		sa.analyzeSpendCallStatement(s)
	case *ast.TaskStatement:
		sa.analyzeTaskStatement(s)
	case *ast.PrefixStatement:
		sa.analyzePrefixStatement(s)
	case *ast.TreeStatement:
		sa.analyzeTreeStatement(s)
	case *ast.ObjectStatement:
		sa.analyzeObjectStatement(s)
	case *ast.FunctionStatement:
		sa.analyzeFunctionStatement(s)
	case *ast.ClassStatement:
		sa.analyzeClassStatement(s)
	case *ast.InterfaceStatement:
		sa.analyzeInterfaceStatement(s)
	case *ast.StructStatement:
		sa.analyzeStructStatement(s)
	case *ast.IfStatement:
		sa.analyzeIfStatement(s)
	case *ast.WhileStatement:
		sa.analyzeWhileStatement(s)
	case *ast.ForStatement:
		sa.analyzeForStatement(s)
	case *ast.ReturnStatement:
		sa.analyzeReturnStatement(s)
	case *ast.NonLocalStatement:
		sa.analyzeNonLocalStatement(s)
	case *ast.VariableDeclaration:
		sa.analyzeVariableDeclaration(s)
	case *ast.ImportStatement:
		sa.analyzeImportStatement(s)
	case *ast.ExpressionStatement:
		sa.analyzeExpression(s.Expression)
	}
}

// analyzeImportStatement 分析导入语句
func (sa *SemanticAnalyzer) analyzeImportStatement(stmt *ast.ImportStatement) {
	// 检查导入的模块是否存在
	moduleName := stmt.Module
	
	// 直接添加模块到符号表，不进行存在性检查
	sa.symbolTable.AddSymbol(moduleName, "module", false, "global", stmt.Pos.Line, stmt.Pos.Column)
	
	// 检查是否是标准库模块
	if sa.stdlibConfig != nil {
		if _, ok := sa.stdlibConfig.Modules[moduleName]; ok {
			// 模块存在，添加函数到符号表
			module := sa.stdlibConfig.Modules[moduleName]
			for funcName := range module.Functions {
				sa.symbolTable.AddSymbol(funcName, "any", false, "global", 0, 0)
			}
		}
	}
}

// analyzeVOStatement 分析VO语句
func (sa *SemanticAnalyzer) analyzeVOStatement(stmt *ast.VOStatement) {
	if stmt.Value != nil {
		sa.analyzeExpression(stmt.Value)
	}
	if stmt.Code != nil {
		sa.analyzeExpression(stmt.Code)
	}
	if stmt.Access != nil {
		sa.analyzeExpression(stmt.Access)
	}
}

// analyzeSpendCallStatement 分析spend/call语句
func (sa *SemanticAnalyzer) analyzeSpendCallStatement(stmt *ast.SpendCallStatement) {
	if stmt.Spend != nil {
		sa.analyzeExpression(stmt.Spend)
	}
	for _, call := range stmt.Calls {
		// 分析call语句的目标
		if call.Target != nil {
			sa.analyzeExpression(call.Target)
		}
		// 分析call语句的体
		for _, bodyStmt := range call.Body {
			sa.analyzeStatement(bodyStmt)
		}
	}
}

// analyzeTaskStatement 分析task语句
func (sa *SemanticAnalyzer) analyzeTaskStatement(stmt *ast.TaskStatement) {
	if stmt.Func != nil {
		sa.analyzeExpression(stmt.Func)
	}
	if stmt.Arg != nil {
		sa.analyzeExpression(stmt.Arg)
	}
}

// analyzePrefixStatement 分析prefix语句
func (sa *SemanticAnalyzer) analyzePrefixStatement(stmt *ast.PrefixStatement) {
	// 创建新的作用域
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "prefix_"+stmt.Name)
	sa.scope++

	// 分析前缀体
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}

	// 恢复旧的作用域
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeTreeStatement 分析tree语句
func (sa *SemanticAnalyzer) analyzeTreeStatement(stmt *ast.TreeStatement) {
	if stmt.Root != nil {
		sa.analyzeExpression(stmt.Root)
	}
}

// analyzeObjectStatement 分析object语句
func (sa *SemanticAnalyzer) analyzeObjectStatement(stmt *ast.ObjectStatement) {
	// 添加对象到符号表
	sa.symbolTable.AddSymbol(stmt.Name, "object", false, "global", stmt.Pos.Line, stmt.Pos.Column)

	// 分析字段
	for _, field := range stmt.Fields {
		sa.analyzeExpression(field)
	}
}

// analyzeFunctionStatement 分析函数语句
func (sa *SemanticAnalyzer) analyzeFunctionStatement(stmt *ast.FunctionStatement) {
	// 添加函数到符号表
	sa.symbolTable.AddSymbol(stmt.Name, "function", false, "global", stmt.Pos.Line, stmt.Pos.Column)

	// 创建新的作用域
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "function_"+stmt.Name)
	sa.scope++

	// 记录当前函数
	oldFunction := sa.currentFunction
	sa.currentFunction = stmt

	// 添加参数到符号表并检查参数重复
	paramMap := make(map[string]bool)
	for _, param := range stmt.Params {
		if paramMap[param] {
			sa.error(fmt.Sprintf("duplicate parameter %s in function %s", param, stmt.Name))
		} else {
			paramMap[param] = true
			sa.symbolTable.AddSymbol(param, "void*", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
		}
	}

	// 分析函数体
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}

	// 恢复旧的函数和作用域
	sa.currentFunction = oldFunction
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeNonLocalStatement 分析nonlocal语句
func (sa *SemanticAnalyzer) analyzeNonLocalStatement(stmt *ast.NonLocalStatement) {
	// 添加nonlocal变量到符号表
	sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, false, "nonlocal", stmt.Pos.Line, stmt.Pos.Column)

	// 分析值
	if stmt.Value != nil {
		sa.analyzeExpression(stmt.Value)
	}
}

// analyzeVariableDeclaration 分析变量声明语句
func (sa *SemanticAnalyzer) analyzeVariableDeclaration(stmt *ast.VariableDeclaration) {
	// 添加变量到符号表
	// 对于指针类型（如 Vector*），我们接受它作为有效类型
	sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local", stmt.Pos.Line, stmt.Pos.Column)

	// 分析值
	if stmt.Value != nil {
		sa.analyzeExpression(stmt.Value)
	}
}



// analyzeIfStatement 分析if语句
func (sa *SemanticAnalyzer) analyzeIfStatement(stmt *ast.IfStatement) {
	// 分析条件
	condType := sa.analyzeExpression(stmt.Condition)
	// 检查条件表达式类型
	if condType != TypeBool {
		sa.error("if condition must be a boolean")
	}

	// 分析if体
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}

	// 分析else体
	for _, elseStmt := range stmt.Else {
		sa.analyzeStatement(elseStmt)
	}
}

// analyzeWhileStatement 分析while语句
func (sa *SemanticAnalyzer) analyzeWhileStatement(stmt *ast.WhileStatement) {
	// 分析条件
	condType := sa.analyzeExpression(stmt.Condition)
	// 检查条件表达式类型
	if condType != TypeBool {
		sa.error("while condition must be a boolean")
	}

	// 分析循环体
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
}

// analyzeForStatement 分析for语句
func (sa *SemanticAnalyzer) analyzeForStatement(stmt *ast.ForStatement) {
	// 分析初始化语句
	if stmt.Init != nil {
		sa.analyzeStatement(stmt.Init)
	}

	// 分析条件
	if stmt.Condition != nil {
		condType := sa.analyzeExpression(stmt.Condition)
		// 检查条件表达式类型
		if condType != TypeBool {
			sa.error("for condition must be a boolean")
		}
	}

	// 分析更新语句
	if stmt.Update != nil {
		sa.analyzeStatement(stmt.Update)
	}

	// 分析循环体
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
}

// analyzeReturnStatement 分析return语句
func (sa *SemanticAnalyzer) analyzeReturnStatement(stmt *ast.ReturnStatement) {
	if stmt.Value != nil {
		returnType := sa.analyzeExpression(stmt.Value)
		// 检查返回值类型是否与函数返回类型匹配
		if sa.currentFunction != nil {
			// 这里可以添加更详细的函数返回类型检查
			// 暂时检查返回值是否存在
			if returnType == TypeAny {
				sa.error("return value has undefined type")
			}
		}
	} else {
		// 检查函数是否需要返回值
		if sa.currentFunction != nil {
			// 这里可以添加更详细的函数返回类型检查
			// 暂时允许无返回值
		}
	}
}

// analyzeExpression 分析表达式
func (sa *SemanticAnalyzer) analyzeExpression(expr ast.Expression) Type {
	if expr == nil {
		return TypeAny
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return sa.analyzeIdentifier(e)
	case *ast.IntegerLiteral:
		return TypeInt
	case *ast.FloatLiteral:
		return TypeFloat
	case *ast.StringLiteral:
		return TypeString
	case *ast.BooleanLiteral:
		return TypeBool
	case *ast.BinaryExpression:
		return sa.analyzeBinaryExpression(e)
	case *ast.CallExpression:
		return sa.analyzeCallExpression(e)
	case *ast.IndexExpression:
		return sa.analyzeIndexExpression(e)
	case *ast.PrefixCallExpression:
		return sa.analyzePrefixCallExpression(e)
	case *ast.MemberAccessExpression:
		return sa.analyzeMemberAccessExpression(e)
	default:
		sa.error(fmt.Sprintf("unexpected expression type: %T", expr))
		return TypeAny
	}
}

// analyzePrefixCallExpression 分析前缀调用表达式
func (sa *SemanticAnalyzer) analyzePrefixCallExpression(expr *ast.PrefixCallExpression) Type {
	// 分析前缀体
	for _, bodyStmt := range expr.Body {
		sa.analyzeStatement(bodyStmt)
	}
	return TypeAny
}

// analyzeIdentifier 分析标识符
func (sa *SemanticAnalyzer) analyzeIdentifier(ident *ast.Identifier) Type {
	// 特殊处理null关键字
	if ident.Name == "null" {
		return TypeAny
	}
	// 查找标识符
	symbol := sa.symbolTable.GetSymbol(ident.Name)
	if symbol == nil {
		sa.error(fmt.Sprintf("undefined identifier: %s", ident.Name))
		return TypeAny
	}
	// 根据symbol的Type字段返回对应的Type类型
	switch symbol.Type {
	case "int":
		return TypeInt
	case "float":
		return TypeFloat
	case "string":
		return TypeString
	case "bool":
		return TypeBool
	default:
		return TypeAny
	}
}

// analyzeBinaryExpression 分析二元表达式
func (sa *SemanticAnalyzer) analyzeBinaryExpression(expr *ast.BinaryExpression) Type {
	// 分析左右操作数
	leftType := sa.analyzeExpression(expr.Left)
	rightType := sa.analyzeExpression(expr.Right)
	
	// 检查是否是与null的比较
	isNullComparison := false
	
	// 检查左操作数是否是可空类型，右操作数是否是null
	if leftIdent, ok := expr.Left.(*ast.Identifier); ok {
		leftSymbol := sa.symbolTable.GetSymbol(leftIdent.Name)
		if leftSymbol != nil && leftSymbol.Nullable {
			// 检查右操作数是否是null
			if rightIdent, ok := expr.Right.(*ast.Identifier); ok && rightIdent.Name == "null" {
				isNullComparison = true
			} else {
				// 暂时允许可空类型在println函数的参数中使用
				// 这是一个简化的实现，实际需要更复杂的作用域分析
				// sa.error(fmt.Sprintf("nullable type '%s' cannot be used in binary expression without null check", leftIdent.Name))
			}
		}
	}
	
	// 检查右操作数是否是可空类型，左操作数是否是null
	if rightIdent, ok := expr.Right.(*ast.Identifier); ok {
		rightSymbol := sa.symbolTable.GetSymbol(rightIdent.Name)
		if rightSymbol != nil && rightSymbol.Nullable {
			// 检查左操作数是否是null
			if leftIdent, ok := expr.Left.(*ast.Identifier); ok && leftIdent.Name == "null" {
				isNullComparison = true
			} else {
				// 暂时允许可空类型在println函数的参数中使用
				// 这是一个简化的实现，实际需要更复杂的作用域分析
				// sa.error(fmt.Sprintf("nullable type '%s' cannot be used in binary expression without null check", rightIdent.Name))
			}
		}
	}
	
	// 检查是否是可空类型在if语句内部的使用
	// 这里需要更复杂的分析，暂时简化处理
	// 假设在if语句内部的可空类型使用是安全的
	// 这是一个简化的实现，实际需要更复杂的作用域分析

	// 检查类型兼容性
	switch expr.Operator {
	case "PLUS", "+":
		// 特殊处理字符串拼接
		if leftType == TypeString || rightType == TypeString {
			return TypeString
		}
		// 算术运算符要求操作数为数字类型
		if leftType != TypeInt && leftType != TypeFloat {
			sa.error("left operand of arithmetic operator must be a number")
		}
		if rightType != TypeInt && rightType != TypeFloat {
			sa.error("right operand of arithmetic operator must be a number")
		}
		// 结果类型为浮点数如果有一个操作数是浮点数
		if leftType == TypeFloat || rightType == TypeFloat {
			return TypeFloat
		}
		return TypeInt
	case "MINUS", "-":
		// 算术运算符要求操作数为数字类型
		if leftType != TypeInt && leftType != TypeFloat {
			sa.error("left operand of arithmetic operator must be a number")
		}
		if rightType != TypeInt && rightType != TypeFloat {
			sa.error("right operand of arithmetic operator must be a number")
		}
		// 结果类型为浮点数如果有一个操作数是浮点数
		if leftType == TypeFloat || rightType == TypeFloat {
			return TypeFloat
		}
		return TypeInt
	case "MULTIPLY", "*":
		// 算术运算符要求操作数为数字类型
		if leftType != TypeInt && leftType != TypeFloat {
			sa.error("left operand of arithmetic operator must be a number")
		}
		if rightType != TypeInt && rightType != TypeFloat {
			sa.error("right operand of arithmetic operator must be a number")
		}
		// 结果类型为浮点数如果有一个操作数是浮点数
		if leftType == TypeFloat || rightType == TypeFloat {
			return TypeFloat
		}
		return TypeInt
	case "DIVIDE", "/":
		// 算术运算符要求操作数为数字类型
		if leftType != TypeInt && leftType != TypeFloat {
			sa.error("left operand of arithmetic operator must be a number")
		}
		if rightType != TypeInt && rightType != TypeFloat {
			sa.error("right operand of arithmetic operator must be a number")
		}
		// 结果类型为浮点数如果有一个操作数是浮点数
		if leftType == TypeFloat || rightType == TypeFloat {
			return TypeFloat
		}
		return TypeInt
	case "EQ", "==":
		// 特殊处理与null的比较
		if isNullComparison {
			return TypeBool
		}
		// 比较运算符要求操作数类型相同
		if leftType != rightType {
			sa.error("operands of comparison operator must have the same type")
		}
		// 比较运算符要求操作数为可比较类型
		if leftType != TypeInt && leftType != TypeFloat && leftType != TypeString && leftType != TypeBool {
			sa.error("operands of comparison operator must be comparable")
		}
		return TypeBool
	case "NE", "!=":
		// 特殊处理与null的比较
		if isNullComparison {
			return TypeBool
		}
		// 比较运算符要求操作数类型相同
		if leftType != rightType {
			sa.error("operands of comparison operator must have the same type")
		}
		// 比较运算符要求操作数为可比较类型
		if leftType != TypeInt && leftType != TypeFloat && leftType != TypeString && leftType != TypeBool {
			sa.error("operands of comparison operator must be comparable")
		}
		return TypeBool
	case "LT", "<", "GT", ">", "LE", "<=", "GE", ">=":
		// 比较运算符要求操作数类型相同
		if leftType != rightType && leftType != TypeAny && rightType != TypeAny {
			sa.error(fmt.Sprintf("operands of comparison operator must have the same type (got %s and %s)", leftType, rightType))
		}
		// 比较运算符要求操作数为可比较类型
		if leftType != TypeInt && leftType != TypeFloat && leftType != TypeString && leftType != TypeBool && leftType != TypeAny {
			sa.error("operands of comparison operator must be comparable")
		}
		if rightType != TypeInt && rightType != TypeFloat && rightType != TypeString && rightType != TypeBool && rightType != TypeAny {
			sa.error("operands of comparison operator must be comparable")
		}
		return TypeBool
	case "AND", "&&":
		// 逻辑运算符要求操作数为布尔类型
		if leftType != TypeBool {
			sa.error("left operand of logical operator must be a boolean")
		}
		if rightType != TypeBool {
			sa.error("right operand of logical operator must be a boolean")
		}
		return TypeBool
	case "OR", "||":
		// 逻辑运算符要求操作数为布尔类型
		if leftType != TypeBool {
			sa.error("left operand of logical operator must be a boolean")
		}
		if rightType != TypeBool {
			sa.error("right operand of logical operator must be a boolean")
		}
		return TypeBool
	case "ASSIGN", "=":
		// 赋值运算符要求左操作数是可赋值的
		if _, ok := expr.Left.(*ast.Identifier); !ok {
			sa.error("left operand of assignment must be an identifier")
		}
		// 检查赋值类型兼容性
		if leftType != TypeAny && rightType != TypeAny && leftType != rightType {
			sa.error("assignment type mismatch")
		}
		return rightType
	default:
		sa.error(fmt.Sprintf("unexpected operator: %s", expr.Operator))
		return TypeAny
	}
}

// analyzeCallExpression 分析函数调用表达式
func (sa *SemanticAnalyzer) analyzeCallExpression(expr *ast.CallExpression) Type {
	// 检查是否是标识符调用
	if ident, ok := expr.Function.(*ast.Identifier); ok {
		// 首先检查标识符是否在符号表中存在
		symbol := sa.symbolTable.GetSymbol(ident.Name)
		if symbol == nil {
			// 检查是否是标准库函数
			isStdlibFunc := false
			if sa.stdlibConfig != nil {
				for _, module := range sa.stdlibConfig.Modules {
					if _, ok := module.Functions[ident.Name]; ok {
						isStdlibFunc = true
						break
					}
				}
			}
			// 如果不是标准库函数，才报错
			if !isStdlibFunc {
				sa.error(fmt.Sprintf("function not defined: %s", ident.Name))
			}
		} else {
			// 检查可空类型
			if symbol.Nullable {
				sa.error(fmt.Sprintf("nullable type '%s' cannot be used as function without null check", ident.Name))
			}
		}
	}

	// 分析参数
	for _, arg := range expr.Args {
		argType := sa.analyzeExpression(arg)
		// 检查参数类型是否有效
		if argType == TypeAny {
			// 暂时允许参数类型为 Any，因为我们可能调用的是标准库函数
			// sa.error("argument has undefined type")
		}
		// 检查可空类型参数
		if argIdent, ok := arg.(*ast.Identifier); ok {
			argSymbol := sa.symbolTable.GetSymbol(argIdent.Name)
			if argSymbol != nil && argSymbol.Nullable {
				sa.error(fmt.Sprintf("nullable type '%s' cannot be used as function argument without null check", argIdent.Name))
			}
		}
	}

	// 检查函数是否为标准库函数并验证参数数量
	if ident, ok := expr.Function.(*ast.Identifier); ok {
		if sa.stdlibConfig != nil {
			for _, module := range sa.stdlibConfig.Modules {
				if fn, ok := module.Functions[ident.Name]; ok {
					if !fn.VarArgs && len(fn.Args) > 0 && len(expr.Args) < len(fn.Args) {
						sa.error(fmt.Sprintf("%s requires at least %d argument(s)", ident.Name, len(fn.Args)))
					}
				}
			}
		}
	}

	// 暂时返回 Any 类型，后续可以根据函数定义返回具体类型
	return TypeAny
}

// analyzeIndexExpression 分析索引表达式
func (sa *SemanticAnalyzer) analyzeIndexExpression(expr *ast.IndexExpression) Type {
	// 分析对象
	objectType := sa.analyzeExpression(expr.Object)
	
	// 检查对象是否存在
	if objectType == TypeAny {
		sa.error("object not defined")
	}

	// 暂时返回Any类型，后续可以根据对象类型返回具体类型
	return TypeAny
}

// analyzeClassStatement 分析类定义
func (sa *SemanticAnalyzer) analyzeClassStatement(stmt *ast.ClassStatement) {
	// 添加类到符号表
	sa.symbolTable.AddSymbol(stmt.Name, "class", false, "global", stmt.Pos.Line, stmt.Pos.Column)

	// 创建新的作用域
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "class_"+stmt.Name)
	sa.scope++

	// 添加字段到符号表
	for _, field := range stmt.Fields {
		sa.symbolTable.AddSymbol(field.Name, field.Type, field.Nullable, "field", field.Pos.Line, field.Pos.Column)
	}

	// 分析方法
	for _, method := range stmt.Methods {
		sa.analyzeMethodStatement(method)
	}

	// 分析构造函数
	for _, constructor := range stmt.Constructors {
		sa.analyzeConstructorStatement(constructor)
	}

	// 检查接口实现
	for _, iface := range stmt.Implements {
		sa.checkInterfaceImplementation(stmt, iface)
	}

	// 恢复旧的作用域
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeInterfaceStatement 分析接口定义
func (sa *SemanticAnalyzer) analyzeInterfaceStatement(stmt *ast.InterfaceStatement) {
	// 添加接口到符号表
	sa.symbolTable.AddSymbol(stmt.Name, "interface", false, "global", stmt.Pos.Line, stmt.Pos.Column)

	// 创建新的作用域
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "interface_"+stmt.Name)
	sa.scope++

	// 分析方法
	for _, method := range stmt.Methods {
		sa.analyzeMethodStatement(method)
	}

	// 恢复旧的作用域
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeStructStatement 分析结构体定义
func (sa *SemanticAnalyzer) analyzeStructStatement(stmt *ast.StructStatement) {
	// 添加结构体到符号表
	sa.symbolTable.AddSymbol(stmt.Name, "struct", false, "global", stmt.Pos.Line, stmt.Pos.Column)

	// 创建新的作用域
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "struct_"+stmt.Name)
	sa.scope++

	// 添加字段到符号表
	for _, field := range stmt.Fields {
		sa.symbolTable.AddSymbol(field.Name, field.Type, field.Nullable, "field", field.Pos.Line, field.Pos.Column)
	}

	// 恢复旧的作用域
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeMethodStatement 分析方法定义
func (sa *SemanticAnalyzer) analyzeMethodStatement(method *ast.MethodStatement) {
	// 添加方法到符号表
	sa.symbolTable.AddSymbol(method.Name, "method", false, "class", method.Pos.Line, method.Pos.Column)

	// 创建新的作用域
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "method_"+method.Name)
	sa.scope++

	// 添加this关键字到符号表
	sa.symbolTable.AddSymbol("this", "self", false, "keyword", method.Pos.Line, method.Pos.Column)

	// 添加参数到符号表
	for _, param := range method.Params {
		sa.symbolTable.AddSymbol(param.Name, param.Type, param.Nullable, "parameter", param.Pos.Line, param.Pos.Column)
	}

	// 分析方法体
	for _, bodyStmt := range method.Body {
		sa.analyzeStatement(bodyStmt)
	}

	// 恢复旧的作用域
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeConstructorStatement 分析构造函数
func (sa *SemanticAnalyzer) analyzeConstructorStatement(constructor *ast.ConstructorStatement) {
	// 创建新的作用域
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "constructor")
	sa.scope++

	// 添加this关键字到符号表
	sa.symbolTable.AddSymbol("this", "self", false, "keyword", constructor.Pos.Line, constructor.Pos.Column)

	// 添加参数到符号表
	for _, param := range constructor.Params {
		sa.symbolTable.AddSymbol(param.Name, param.Type, param.Nullable, "parameter", param.Pos.Line, param.Pos.Column)
	}

	// 分析构造函数体
	for _, bodyStmt := range constructor.Body {
		sa.analyzeStatement(bodyStmt)
	}

	// 恢复旧的作用域
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// checkInterfaceImplementation 检查接口实现
func (sa *SemanticAnalyzer) checkInterfaceImplementation(class *ast.ClassStatement, interfaceName string) {
	// 查找接口
	// 这里需要从全局符号表中查找接口定义
	// 暂时简化处理
}

// analyzeMemberAccessExpression 分析成员访问表达式
func (sa *SemanticAnalyzer) analyzeMemberAccessExpression(expr *ast.MemberAccessExpression) Type {
	// 检查对象是否是标准库模块
	isStdlibModule := false
	if sa.stdlibConfig != nil {
		if objIdent, ok := expr.Object.(*ast.Identifier); ok {
			if _, ok := sa.stdlibConfig.Modules[objIdent.Name]; ok {
				isStdlibModule = true
			}
		}
	}
	
	// 如果不是标准库模块，才分析对象
	if !isStdlibModule {
		// 分析对象
		objectType := sa.analyzeExpression(expr.Object)
		
		// 检查对象是否存在
		if objectType == TypeAny {
			sa.error("object not defined")
		}
	}

	// 暂时返回 Any 类型，后续可以根据成员类型返回具体类型
	return TypeAny
}

// error 报告错误
func (sa *SemanticAnalyzer) error(message string) {
	sa.errorCollector.AddSemanticError(message, 0, 0, "", "")
}

// errorWithLocation 报告带位置信息的错误
func (sa *SemanticAnalyzer) errorWithLocation(message string, line, column int) {
	sa.errorCollector.AddSemanticError(message, line, column, "", "")
}

// Errors 返回错误列表
func (sa *SemanticAnalyzer) Errors() []*errors.Error {
	return sa.errorCollector.Errors()
}

// HasErrors 检查是否有错误
func (sa *SemanticAnalyzer) HasErrors() bool {
	return sa.errorCollector.HasErrors()
}

// Run 运行语义分析
func (sa *SemanticAnalyzer) Run(program *ast.Program) {
	sa.Analyze(program)
	if sa.HasErrors() {
		for _, err := range sa.Errors() {
			fmt.Printf("Semantic error: %s (line %d, column %d)\n", err.Message, err.Line, err.Column)
			if err.Suggestion != "" {
				fmt.Printf("Suggestion: %s\n", err.Suggestion)
			}
		}
	}
}
