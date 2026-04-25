package sema

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/core"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/symbol"
	"kaula-compiler/internal/stdlib"
	"strings"
)

type SemanticAnalyzer struct {
	symbolTable      *symbol.SymbolTable
	scope            int
	errorCollector   *errors.ErrorCollector
	currentFunction *ast.FunctionStatement
	program         *ast.Program
	stdlibConfig    *stdlib.StdlibConfig
	genericStack    []*ast.FunctionStatement
	typeConstraints map[string][]string
	exportedSymbols map[string]bool
	treeManager     *core.TreeManager
	prefixManager   *core.PrefixManager
	rootTreeFound   bool
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
		// 只添加模块名，不自动添加函数
		// 函数必须通过显式 import 导入
		for moduleName := range stdlibConfig.Modules {
			globalSymbolTable.AddSymbol(moduleName, "module", false, "global", 0, 0)
		}
	} else {
		// 如果 stdlib.json 加载失败，至少添加 println
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
		treeManager:     core.NewTreeManager(),
		prefixManager:   core.NewPrefixManager(),
		rootTreeFound:   false,
	}
}

// Analyze 分析程序（两遍分析）
func (sa *SemanticAnalyzer) Analyze(program *ast.Program) {
	// 保存 program 引用以便后续查找
	sa.program = program

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
	case *ast.SpendStatement:
		sa.analyzeSpendStatement(s)
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
		if s == nil || s.Type == "" {
			return
		}
		sa.analyzeVariableDeclaration(s)
	case *ast.ImportStatement:
		sa.analyzeImportStatement(s)
	case *ast.ExportStatement:
		sa.analyzeExportStatement(s)
	case *ast.ExpressionStatement:
		if s == nil || s.Expression == nil {
			return
		}
		// 检查是否是前缀调用表达式
		if prefixCall, ok := s.Expression.(*ast.PrefixCallExpression); ok {
			sa.analyzePrefixCallExpression(prefixCall)
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
		// 支持两种导入格式: `io` 和 `std.io`
		stdlibKey := moduleName
		if !strings.HasPrefix(stdlibKey, "std.") {
			stdlibKey = "std." + moduleName
		}
		
		if mod, ok := sa.stdlibConfig.Modules[stdlibKey]; ok {
			for funcName := range mod.Functions {
				qualifiedName := fmt.Sprintf("%s.%s", stdlibKey, funcName)
				sa.symbolTable.AddSymbol(qualifiedName, "stdlib_function", false, "global", 0, 0)
			}
		} else if lib := sa.stdlibConfig.GetThirdPartyLibrary(moduleName); lib != nil {
			// 检查是否是第三方库
			for funcName := range lib.Functions {
				qualifiedName := fmt.Sprintf("%s.%s", moduleName, funcName)
				sa.symbolTable.AddSymbol(qualifiedName, "third_party_function", false, "global", 0, 0)
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

// analyzeSpendStatement 分析spend语句 - 锁定并开启消费流程
func (sa *SemanticAnalyzer) analyzeSpendStatement(stmt *ast.SpendStatement) {
	if stmt.Target != nil {
		sa.analyzeExpression(stmt.Target)
	}

	// 分析每个 call 子句
	for _, call := range stmt.Calls {
		if call.Index != nil {
			sa.analyzeExpression(call.Index)
		}
		for _, bodyStmt := range call.Body {
			sa.analyzeStatement(bodyStmt)
		}
	}

	// 验证 call 次数与目标元素数量匹配
	// 这需要在运行时验证，但可以做一些静态检查
	expectedCalls := -1 // -1 表示未知，需要运行时确定
	for _, call := range stmt.Calls {
		// 检查索引是否为常量
		if intLit, ok := call.Index.(*ast.IntegerLiteral); ok {
			index := int(intLit.Value)
			if expectedCalls == -1 {
				expectedCalls = index
			} else if index > expectedCalls {
				sa.errorCollector.AddSemanticWarning(
					fmt.Sprintf("call index %d exceeds expected number of calls", index),
					call.Pos.Line,
					call.Pos.Column,
					"spend_call_mismatch",
					"ensure call indices match target element count",
				)
			}
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

// analyzePrefixCallExpression 分析前缀调用表达式
// 检测变量遮蔽等潜在问题
func (sa *SemanticAnalyzer) analyzePrefixCallExpression(expr *ast.PrefixCallExpression) {
	// 获取前缀函数定义
	funcDecl := sa.findFunctionDeclaration(expr.Name)
	if funcDecl == nil {
		return
	}

	// 检查是否是前缀函数
	annotation := funcDecl.GetAnnotation()
	if annotation != ast.TreeAnnotationPrefix &&
		annotation != ast.TreeAnnotationPrefixTree {
		return
	}

	// 收集前缀函数的参数列表
	prefixParams := make(map[string]bool)
	for _, param := range funcDecl.Params {
		prefixParams[param] = true
	}

	// 分析调用体内的语句，检测变量遮蔽
	for _, bodyStmt := range expr.Body {
		sa.checkPrefixVariableShadowing(bodyStmt, prefixParams)
	}
}

// checkPrefixVariableShadowing 检查前缀调用体内的变量遮蔽
func (sa *SemanticAnalyzer) checkPrefixVariableShadowing(stmt ast.Statement, prefixParams map[string]bool) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		// 检查变量名是否与前缀参数相同
		if prefixParams[s.Name] {
			sa.errorCollector.AddSemanticWarning(
				fmt.Sprintf("variable '%s' shadows prefix parameter with same name - use explicit $%s to disambiguate if intended", s.Name, s.Name),
				s.Pos.Line,
				s.Pos.Column,
				"prefix_shadowing",
				"prefix variable with same name",
			)
		}

		// 递归检查初始化表达式
		if s.Value != nil {
			sa.checkExpressionForShadowing(s.Value, prefixParams)
		}

	case *ast.ExpressionStatement:
		if s.Expression != nil {
			sa.checkExpressionForShadowing(s.Expression, prefixParams)
		}
	}
}

// checkExpressionForShadowing 检查表达式中的变量引用
func (sa *SemanticAnalyzer) checkExpressionForShadowing(expr ast.Expression, prefixParams map[string]bool) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.Identifier:
		// 检查是否使用了 $ 前缀但没有 $
		if prefixParams[e.Name] && !e.IsPrefixVar {
			sa.errorCollector.AddSemanticWarning(
				fmt.Sprintf("identifier '%s' matches prefix parameter but not using $ prefix - did you mean $%s?", e.Name, e.Name),
				e.Pos.Line,
				e.Pos.Column,
				"missing_prefix_var",
				"use $ prefix to access prefix variable",
			)
		}

	case *ast.CallExpression:
		for _, arg := range e.Args {
			sa.checkExpressionForShadowing(arg, prefixParams)
		}

	case *ast.BinaryExpression:
		sa.checkExpressionForShadowing(e.Left, prefixParams)
		sa.checkExpressionForShadowing(e.Right, prefixParams)
	}
}

// findFunctionDeclaration 查找函数声明
func (sa *SemanticAnalyzer) findFunctionDeclaration(name string) *ast.FunctionStatement {
	if sa.program == nil {
		return nil
	}
	for _, stmt := range sa.program.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == name {
				return fnStmt
			}
		}
	}
	return nil
}

// analyzeTreeStatement 分析 tree 语句
func (sa *SemanticAnalyzer) analyzeTreeStatement(stmt *ast.TreeStatement) {
	annotation := stmt.GetAnnotation()

	if annotation == ast.TreeAnnotationRoot || annotation == ast.TreeAnnotationRootTree {
		sa.analyzeRootTree(stmt)
	} else if annotation == ast.TreeAnnotationPrefix || annotation == ast.TreeAnnotationPrefixTree {
		sa.analyzePrefixTree(stmt)
	} else if annotation == ast.TreeAnnotationTree {
		sa.analyzeOrphanTree(stmt)
	}

	if stmt.Root != nil {
		sa.analyzeExpression(stmt.Root)
	}

	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
}

func (sa *SemanticAnalyzer) analyzeRootTree(stmt *ast.TreeStatement) {
	if sa.rootTreeFound {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("root tree 已经存在，只能定义一个 root tree"),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"删除多余的 root tree 定义，或将其改为普通 tree",
		)
		return
	}

	tree := core.NewTreeWithName("root")
	tree.SetAnnotation(core.AnnotationRootTree)
	if err := sa.treeManager.RegisterTree(tree); err != nil {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("注册 root tree 失败: %v", err),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"",
		)
	}
	sa.rootTreeFound = true
}

func (sa *SemanticAnalyzer) analyzePrefixTree(stmt *ast.TreeStatement) {
	var prefixName string
	if ident, ok := stmt.Root.(*ast.Identifier); ok {
		prefixName = ident.Name
	}

	tree := core.NewTreeWithName(prefixName)
	tree.SetAnnotation(core.AnnotationPrefixTree)
	if err := sa.treeManager.RegisterTree(tree); err != nil {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("注册 prefix tree '%s' 失败: %v", prefixName, err),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"",
		)
	}

	if prefixName != "" {
		sa.prefixManager.CreatePrefix(prefixName, core.PrefixAnnotationPrefixTree)
	}
}

func (sa *SemanticAnalyzer) analyzeOrphanTree(stmt *ast.TreeStatement) {
	var treeName string
	if ident, ok := stmt.Root.(*ast.Identifier); ok {
		treeName = ident.Name
	}

	tree := core.NewTreeWithName(treeName)
	tree.SetAnnotation(core.AnnotationTree)

	if !sa.rootTreeFound {
		tree.MarkOrphan()
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("孤儿 tree '%s' - 没有定义 root tree，所有 tree 必须匹配 root tree 结构", treeName),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"定义 #[root,tree] 来指定全局 root tree，或将 tree 包裹在 prefix 或 class 中",
		)
	} else {
		if err := sa.treeManager.RegisterTree(tree); err != nil {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("注册 tree '%s' 失败: %v", treeName, err),
				stmt.Pos.Line,
				stmt.Pos.Column,
				"",
				"",
			)
		}
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

func (sa *SemanticAnalyzer) ErrorCollector() *errors.ErrorCollector {
	return sa.errorCollector
}

func (sa *SemanticAnalyzer) GetStdlibConfig() *stdlib.StdlibConfig {
	return sa.stdlibConfig
}

func (sa *SemanticAnalyzer) SetStdlibConfig(cfg *stdlib.StdlibConfig) {
	sa.stdlibConfig = cfg
}

func (sa *SemanticAnalyzer) HasErrors() bool {
	return sa.errorCollector.HasErrors()
}
