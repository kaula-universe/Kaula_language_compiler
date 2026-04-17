package sema

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/symbol"
	"kaula-compiler/internal/stdlib"
	"strings"
)

// SemanticAnalyzer 表示语义分析器
type SemanticAnalyzer struct {
	symbolTable     *symbol.SymbolTable
	scope           int
	errorCollector  *errors.ErrorCollector
	currentFunction *ast.FunctionStatement
	stdlibConfig    *stdlib.StdlibConfig
	genericStack    []*ast.FunctionStatement // 泛型函数栈
	typeConstraints map[string][]string      // 类型约束映射
	exportedSymbols map[string]bool          // 导出符号列表
}

// NewSemanticAnalyzer 创建一个新的语义分析器
func NewSemanticAnalyzer() *SemanticAnalyzer {
	errorCollector := errors.NewErrorCollector()
	return NewSemanticAnalyzerWithConfig("kaula-compiler/stdlib.json", errorCollector)
}

// NewSemanticAnalyzerWithConfig 使用指定配置文件和错误收集器创建语义分析器
func NewSemanticAnalyzerWithConfig(configPath string, errorCollector *errors.ErrorCollector) *SemanticAnalyzer {
	globalSymbolTable := symbol.NewSymbolTable(nil, "global")

	globalSymbolTable.AddSymbol("std", "any", false, "global", 0, 0)
	globalSymbolTable.AddSymbol("std.io", "any", false, "global", 0, 0)
	globalSymbolTable.AddSymbol("std.vo", "any", false, "global", 0, 0)
	globalSymbolTable.AddSymbol("std.prefix", "any", false, "global", 0, 0)

	stdlibConfig, err := stdlib.LoadStdlibConfig(configPath)
	if err == nil && stdlibConfig != nil {
		for moduleName, module := range stdlibConfig.Modules {
			globalSymbolTable.AddSymbol(moduleName, "module", false, "global", 0, 0)
			for funcName := range module.Functions {
				globalSymbolTable.AddSymbol(funcName, "any", false, "global", 0, 0)
			}
		}
	} else {
		globalSymbolTable.AddSymbol("println", "any", false, "global", 0, 0)
	}

	return &SemanticAnalyzer{
		symbolTable:     globalSymbolTable,
		scope:           1,
		errorCollector:  errorCollector,
		currentFunction: nil,
		stdlibConfig:    stdlibConfig,
		genericStack:    make([]*ast.FunctionStatement, 0),
		typeConstraints: make(map[string][]string),
		exportedSymbols: make(map[string]bool),
	}
}

// Analyze 分析程序（两遍分析）
func (sa *SemanticAnalyzer) Analyze(program *ast.Program) {
	// 第一遍：将所有函数和变量添加到符号表（不分析函数体）
	for _, stmt := range program.Statements {
		sa.analyzeStatement(stmt)
	}

	// 第二遍：分析函数体
	for _, stmt := range program.Statements {
		if funcStmt, ok := stmt.(*ast.FunctionStatement); ok {
			sa.analyzeFunctionBody(funcStmt)
		}
	}
}

// analyzeFunctionBody 只分析函数体（不重复添加符号）
func (sa *SemanticAnalyzer) analyzeFunctionBody(stmt *ast.FunctionStatement) {
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "function_"+stmt.Name)
	sa.scope++

	oldFunction := sa.currentFunction
	sa.currentFunction = stmt

	// 处理泛型类型参数
	if stmt.IsGeneric() {
		sa.genericStack = append(sa.genericStack, stmt)
		typeParams := make([]string, 0, len(stmt.TypeParams))
		for _, tp := range stmt.TypeParams {
			typeParams = append(typeParams, tp.Name)
			sa.symbolTable.AddGenericSymbol(tp.Name, "type", []string{tp.Name}, false, "parameter", tp.Pos.Line, tp.Pos.Column)
			if tp.Constraint != "" && tp.Constraint != "any" {
				sa.typeConstraints[tp.Name] = []string{tp.Constraint}
			}
		}
	}

	paramMap := make(map[string]bool)
	for _, param := range stmt.Params {
		if paramMap[param] {
			sa.error(fmt.Sprintf("duplicate parameter %s in function %s", param, stmt.Name), stmt.Pos.Line, stmt.Pos.Column)
		} else {
			paramMap[param] = true
			sa.symbolTable.AddSymbol(param, "void*", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
		}
	}

	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}

	// 弹出泛型函数栈
	if stmt.IsGeneric() {
		sa.genericStack = sa.genericStack[:len(sa.genericStack)-1]
	}

	sa.currentFunction = oldFunction
	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeStatement 分析语句
func (sa *SemanticAnalyzer) analyzeStatement(s ast.Statement) {
	if s == nil {
		return
	}
	switch s := s.(type) {
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
		// 第一遍只添加函数到符号表，不分析函数体
		if s.IsGeneric() {
			sa.symbolTable.AddGenericSymbol(s.Name, "function", make([]string, 0, len(s.TypeParams)), false, "global", s.Pos.Line, s.Pos.Column)
		} else {
			sa.symbolTable.AddSymbol(s.Name, "function", false, "global", s.Pos.Line, s.Pos.Column)
		}
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
	case *ast.ExportStatement:
		sa.analyzeExportStatement(s)
	case *ast.ExpressionStatement:
		if s == nil || s.Expression == nil {
			return
		}
		sa.analyzeExpression(s.Expression)
	}
}

// analyzeImportStatement 分析导入语句
func (sa *SemanticAnalyzer) analyzeImportStatement(stmt *ast.ImportStatement) {
	moduleName := stmt.Module
	sa.symbolTable.AddSymbol(moduleName, "module", false, "global", stmt.Pos.Line, stmt.Pos.Column)

	if sa.stdlibConfig != nil {
		// 检查是否是标准库模块
		if _, ok := sa.stdlibConfig.Modules[moduleName]; ok {
			module := sa.stdlibConfig.Modules[moduleName]
			for funcName := range module.Functions {
				sa.symbolTable.AddSymbol(funcName, "any", false, "global", 0, 0)
			}
		} else if lib := sa.stdlibConfig.GetThirdPartyLibrary(moduleName); lib != nil {
			// 检查是否是第三方库
			for funcName := range lib.Functions {
				sa.symbolTable.AddSymbol(funcName, "any", false, "global", 0, 0)
			}
		}
	}
}

// analyzeExportStatement 分析导出语句
func (sa *SemanticAnalyzer) analyzeExportStatement(stmt *ast.ExportStatement) {
	// 1. 检查符号是否已存在
	symbol := sa.symbolTable.GetSymbol(stmt.Name)
	if symbol == nil {
		// 符号还未定义，可能是前向声明，先添加到符号表
		sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, false, "exported", stmt.Pos.Line, stmt.Pos.Column)
		return
	}
	
	// 2. 标记符号为导出
	symbol.Scope = "exported"
	
	// 3. 添加到导出符号列表
	sa.exportedSymbols[stmt.Name] = true
}

// analyzeVOStatement 分析 VO 语句
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
		if call.Target != nil {
			sa.analyzeExpression(call.Target)
		}
		for _, bodyStmt := range call.Body {
			sa.analyzeStatement(bodyStmt)
		}
	}
}

// analyzeTaskStatement 分析 task 语句
func (sa *SemanticAnalyzer) analyzeTaskStatement(stmt *ast.TaskStatement) {
	if stmt.Func != nil {
		sa.analyzeExpression(stmt.Func)
	}
	if stmt.Arg != nil {
		sa.analyzeExpression(stmt.Arg)
	}
}

// analyzePrefixStatement 分析 prefix 语句
func (sa *SemanticAnalyzer) analyzePrefixStatement(stmt *ast.PrefixStatement) {
	oldSymbolTable := sa.symbolTable
	sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "prefix_"+stmt.Name)
	sa.scope++

	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}

	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzeTreeStatement 分析 tree 语句
func (sa *SemanticAnalyzer) analyzeTreeStatement(stmt *ast.TreeStatement) {
	if stmt.Root != nil {
		sa.analyzeExpression(stmt.Root)
	}
}

// analyzeObjectStatement 分析 object 语句
func (sa *SemanticAnalyzer) analyzeObjectStatement(stmt *ast.ObjectStatement) {
	sa.symbolTable.AddSymbol(stmt.Name, "object", false, "global", stmt.Pos.Line, stmt.Pos.Column)
	for _, field := range stmt.Fields {
		sa.analyzeExpression(field)
	}
}

// analyzeClassStatement 分析 class 语句
func (sa *SemanticAnalyzer) analyzeClassStatement(stmt *ast.ClassStatement) {
	sa.symbolTable.AddSymbol(stmt.Name, "class", false, "global", stmt.Pos.Line, stmt.Pos.Column)
}

// analyzeInterfaceStatement 分析 interface 语句
func (sa *SemanticAnalyzer) analyzeInterfaceStatement(stmt *ast.InterfaceStatement) {
	sa.symbolTable.AddSymbol(stmt.Name, "interface", false, "global", stmt.Pos.Line, stmt.Pos.Column)
}

// analyzeStructStatement 分析 struct 语句
func (sa *SemanticAnalyzer) analyzeStructStatement(stmt *ast.StructStatement) {
	sa.symbolTable.AddSymbol(stmt.Name, "struct", false, "global", stmt.Pos.Line, stmt.Pos.Column)
}

// analyzeNonLocalStatement 分析 nonlocal 语句
func (sa *SemanticAnalyzer) analyzeNonLocalStatement(stmt *ast.NonLocalStatement) {
	sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, false, "nonlocal", stmt.Pos.Line, stmt.Pos.Column)
	if stmt.Value != nil {
		sa.analyzeExpression(stmt.Value)
	}
}

// analyzeVariableDeclaration 分析变量声明语句
func (sa *SemanticAnalyzer) analyzeVariableDeclaration(stmt *ast.VariableDeclaration) {
	// 1. 检查类型是否存在
	if !sa.isTypeValid(stmt.Type) {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("未知类型 '%s'，变量声明必须使用已定义的类型", stmt.Type),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"检查类型名称是否正确，或者是否已定义该类型（类、结构体等）",
		)
	}
	
	// 2. 添加变量到符号表
	sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local", stmt.Pos.Line, stmt.Pos.Column)
	
	// 3. 分析初始化表达式
	if stmt.Value != nil {
		sa.analyzeExpression(stmt.Value)
	}
}

// isTypeValid 检查类型是否有效
func (sa *SemanticAnalyzer) isTypeValid(typeName string) bool {
	// 基本类型
	basicTypes := map[string]bool{
		"int":    true,
		"i8":     true,
		"i16":    true,
		"i32":    true,
		"i64":    true,
		"u8":     true,
		"u16":    true,
		"u32":    true,
		"u64":    true,
		"float":  true,
		"f32":    true,
		"f64":    true,
		"double": true,
		"bool":   true,
		"char":   true,
		"string": true,
		"void":   true,
		"any":    true,
	}
	
	// 检查是否是基本类型
	if basicTypes[typeName] {
		return true
	}
	
	// 检查是否是指针类型（如 int*）
	if len(typeName) > 0 && typeName[len(typeName)-1] == '*' {
		baseType := typeName[:len(typeName)-1]
		return sa.isTypeValid(baseType)
	}
	
	// 检查符号表中是否有该类型（类、结构体、接口等）
	symbol := sa.symbolTable.GetSymbol(typeName)
	if symbol != nil && (symbol.Type == "class" || symbol.Type == "struct" || symbol.Type == "interface" || symbol.Type == "type") {
		return true
	}
	
	// 检查是否是泛型类型（如 Box<int>）
	if idx := strings.Index(typeName, "<"); idx > 0 {
		baseType := typeName[:idx]
		return sa.isTypeValid(baseType)
	}
	
	return false
}

// analyzeIfStatement 分析 if 语句
func (sa *SemanticAnalyzer) analyzeIfStatement(stmt *ast.IfStatement) {
	if stmt.Condition != nil {
		sa.analyzeExpression(stmt.Condition)
	}
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
	for _, elseStmt := range stmt.Else {
		sa.analyzeStatement(elseStmt)
	}
}

// analyzeWhileStatement 分析 while 语句
func (sa *SemanticAnalyzer) analyzeWhileStatement(stmt *ast.WhileStatement) {
	if stmt.Condition != nil {
		sa.analyzeExpression(stmt.Condition)
	}
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
}

// analyzeForStatement 分析 for 语句
func (sa *SemanticAnalyzer) analyzeForStatement(stmt *ast.ForStatement) {
	if stmt.Init != nil {
		sa.analyzeStatement(stmt.Init)
	}
	if stmt.Condition != nil {
		sa.analyzeExpression(stmt.Condition)
	}
	// Update 是 Statement 类型，不是 Expression
	if stmt.Update != nil {
		sa.analyzeStatement(stmt.Update)
	}
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
}

// analyzeReturnStatement 分析 return 语句
func (sa *SemanticAnalyzer) analyzeReturnStatement(stmt *ast.ReturnStatement) {
	if stmt.Value != nil {
		sa.analyzeExpression(stmt.Value)
	}
}

// analyzeExpression 分析表达式（简化版本，只遍历不检查类型）
func (sa *SemanticAnalyzer) analyzeExpression(expr ast.Expression) {
	// 简化处理，不进行遍历（避免递归过深）
}

func (sa *SemanticAnalyzer) error(msg string, line, column int) {
	sa.errorCollector.AddSemanticError(msg, line, column, "", "")
}

// checkTypeConstraint 检查类型约束
func (sa *SemanticAnalyzer) checkTypeConstraint(typeName, constraint string, line, column int) bool {
	if constraint == "" || constraint == "any" {
		return true
	}
	
	// 检查类型是否满足约束
	switch constraint {
	case "comparable":
		// 可比较类型：基本类型、指针等
		if typeName == "int" || typeName == "float" || typeName == "string" || 
		   typeName == "bool" || typeName == "char*" {
			return true
		}
	case "ordered":
		// 有序类型：可以进行大小比较
		if typeName == "int" || typeName == "float" || typeName == "string" {
			return true
		}
	case "number":
		// 数值类型
		if typeName == "int" || typeName == "float" || typeName == "double" {
			return true
		}
	}
	
	sa.error(fmt.Sprintf("type %s does not satisfy constraint %s", typeName, constraint), line, column)
	return false
}

// validateGenericInstantiation 验证泛型实例化
func (sa *SemanticAnalyzer) validateGenericInstantiation(funcName string, typeArgs []string, line, column int) bool {
	symbol := sa.symbolTable.GetSymbol(funcName)
	if symbol == nil || !symbol.IsGeneric {
		return false
	}
	
	if symbol.GenericInst != nil {
		expectedCount := len(symbol.GenericInst.TypeArguments)
		if len(typeArgs) != expectedCount {
			sa.error(fmt.Sprintf("expected %d type arguments, got %d", expectedCount, len(typeArgs)), line, column)
			return false
		}
	}
	
	// 检查每个类型参数是否满足约束
	for _, typeArg := range typeArgs {
		if constraints, ok := sa.typeConstraints[typeArg]; ok {
			for _, constraint := range constraints {
				if !sa.checkTypeConstraint(typeArg, constraint, line, column) {
					return false
				}
			}
		}
	}
	
	return true
}
