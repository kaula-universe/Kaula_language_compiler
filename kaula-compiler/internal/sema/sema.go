package sema

import (
	"encoding/json"
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/comptime"
	"kaula-compiler/internal/core"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/symbol"
	"kaula-compiler/internal/stdlib"
	"os"
	"path/filepath"
	"strings"
)

type SemanticAnalyzer struct {
	symbolTable      *symbol.SymbolTable
	scope            int
	errorCollector   *errors.ErrorCollector
	currentFunction *ast.FunctionStatement
	sorGlobalEnabled bool // 全局 SOR 模式（--sor 标志）
	program         *ast.Program
	stdlibConfig    *stdlib.StdlibConfig
	genericStack    []*ast.FunctionStatement
	typeConstraints map[string][]string
	exportedSymbols map[string]bool
	treeManager     *core.TreeManager
	prefixManager   *core.PrefixManager
	rootTreeFound   bool
	source          string // 源码用于错误上下文
	prefixSymbolTables map[string]*symbol.SymbolTable // 前缀名 -> 前缀符号表
	currentPrefixTable *symbol.SymbolTable             // 当前 @前缀块 的符号表
	comptime *comptime.Evaluator                        // 编译期表达式评估器
	importedModules map[string]bool                     // 记录实际导入的模块
}

// NewSemanticAnalyzer 创建一个新的语义分析器
// SetSOREnabled 设置是否启用全局 SOR 模式（--sor 标志）
func (sa *SemanticAnalyzer) SetSOREnabled(enabled bool) {
	sa.sorGlobalEnabled = enabled
}

// isFunctionSOR 判断当前函数是否启用了 SOR
// 优先级：--sor 全局标志 > #[sor] 函数注解
func (sa *SemanticAnalyzer) isFunctionSOR() bool {
	if sa.sorGlobalEnabled {
		return true
	}
	if sa.currentFunction != nil && sa.currentFunction.SOREnabled {
		return true
	}
	return false
}

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
		prefixSymbolTables: make(map[string]*symbol.SymbolTable),
		comptime:          nil,
		importedModules:   make(map[string]bool),
	}
}

// Analyze 分析程序（两遍分析）
func (sa *SemanticAnalyzer) Analyze(program *ast.Program) {
	// 保存 program 引用以便后续查找
	sa.program = program
	
	// 从 AST 获取源码
	if program != nil {
		sa.source = program.Source
	}

	sa.comptime = comptime.NewEvaluator()

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

	// 检查 SOR 函数是否使用了 SOR 原语
	// 全局 SOR 模式下所有函数都必须使用 SOR 原语，或单独标注 #[sor] 的函数也必须使用
	if sa.isFunctionSOR() {
		if !sa.hasSORPrimitives(stmt.Body) {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("函数 '%s' 在 SOR 模式下未使用任何 SOR 原语 (yield/release/extract)", stmt.Name),
				stmt.Pos.Line, stmt.Pos.Column, "",
				"在函数体内使用 yield/release/extract 原语，或使用 --no-sor 禁用 SOR 模式")
		}
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
	case *ast.EnumStatement:
		sa.analyzeEnumStatement(s)
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
		if s == nil {
			return
		}
		if s.IsAuto {
			sa.analyzeAutoDeclaration(s)
		} else if s.IsConst && s.Type == "" {
			sa.analyzeAutoDeclaration(s)
		} else if s.Type != "" {
			sa.analyzeVariableDeclaration(s)
		}
	case *ast.TypeAliasStatement:
		sa.analyzeTypeAliasStatement(s)
	case *ast.ExternStatement:
		if s == nil {
			return
		}
		sa.analyzeExternStatement(s)
	case *ast.ImportStatement:
		sa.analyzeImportStatement(s)
	case *ast.ExportStatement:
		sa.analyzeExportStatement(s)
	case *ast.PackageStatement:
		// package 声明：记录包名，不需要额外分析
		// 包名仅用于语义层面，不影响 C 代码生成
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
	case *ast.YieldStatement:
		sa.analyzeYieldStatement(s)
	case *ast.ReleaseStatement:
		sa.analyzeReleaseStatement(s)
	case *ast.ExtractStatement:
		sa.analyzeExtractStatement(s)
	}
}

// analyzeImportStatement 分析导入语句
func (sa *SemanticAnalyzer) analyzeImportStatement(stmt *ast.ImportStatement) {
	moduleName := stmt.Module
	sa.symbolTable.AddSymbol(moduleName, "module", false, "global", stmt.Pos.Line, stmt.Pos.Column)
	
	// 记录实际导入的模块
	sa.importedModules[moduleName] = true

	if sa.stdlibConfig != nil {
		// 检查是否是标准库模块
		// 支持两种导入格式: `io` 和 `std.io`
		stdlibKey := moduleName
		if !strings.HasPrefix(stdlibKey, "std.") {
			stdlibKey = "std." + moduleName
		}
		
		// 记录标准库模块名
		sa.importedModules[stdlibKey] = true

		if mod, ok := sa.stdlibConfig.Modules[stdlibKey]; ok {
			for funcName := range mod.Functions {
				qualifiedName := fmt.Sprintf("%s.%s", stdlibKey, funcName)
				sa.symbolTable.AddSymbol(qualifiedName, "stdlib_function", false, "global", 0, 0)
				// 也注册无限定名，允许直接使用函数名
				sa.symbolTable.AddSymbol(funcName, "stdlib_function", false, "global", 0, 0)
			}
		} else if lib := sa.stdlibConfig.GetThirdPartyLibrary(moduleName); lib != nil {
			// 检查是否是第三方库
			for funcName := range lib.Functions {
				qualifiedName := fmt.Sprintf("%s.%s", moduleName, funcName)
				sa.symbolTable.AddSymbol(qualifiedName, "third_party_function", false, "global", 0, 0)
				// 也注册无限定名，允许直接使用函数名
				sa.symbolTable.AddSymbol(funcName, "third_party_function", false, "global", 0, 0)
			}
		} else {
			// 未找到库配置，尝试按需分析 pkglib 中的库
			sa.tryAnalyzeMissingPackage(moduleName)
		}
	}
}

// tryAnalyzeMissingPackage 尝试按需分析 pkglib 中存在但尚未生成配置的库
func (sa *SemanticAnalyzer) tryAnalyzeMissingPackage(moduleName string) {
	if sa.stdlibConfig == nil {
		return
	}
	// 尝试在已知 pkglib 路径中查找该库
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)
	pkglibPaths := []string{
		filepath.Join(exeDir, "pkglib"),
		filepath.Join(exeDir, "..", "pkglib"),
		"pkglib",
	}

	for _, pkglibPath := range pkglibPaths {
		libDir := filepath.Join(pkglibPath, moduleName)
		if info, err := os.Stat(libDir); err == nil && info.IsDir() {
			// pkglib 中存在该库目录，但没有配置文件，按需分析
			fmt.Printf("Import '%s' not found in config, auto-analyzing from %s...\n", moduleName, libDir)
			result, analyzeErr := stdlib.AnalyzePackage(libDir)
			if analyzeErr != nil {
				fmt.Printf("Warning: Failed to auto-analyze %s: %v\n", moduleName, analyzeErr)
				return
			}
			if writeErr := result.WriteConfig(libDir); writeErr != nil {
				fmt.Printf("Warning: Failed to write config for %s: %v\n", moduleName, writeErr)
				return
			}

			// 重新加载该库配置到 stdlibConfig
			configFile := filepath.Join(libDir, moduleName+".json")
			data, err := os.ReadFile(configFile)
			if err != nil {
				return
			}
			var libConfig stdlib.ThirdPartyLibrary
			if err := json.Unmarshal(data, &libConfig); err != nil {
				return
			}
			if libConfig.Name == "" {
				libConfig.Name = moduleName
			}

			// 添加到 ThirdParty 列表
			sa.stdlibConfig.ThirdParty = append(sa.stdlibConfig.ThirdParty, libConfig)

			// 注册函数到符号表
			for funcName := range libConfig.Functions {
				qualifiedName := fmt.Sprintf("%s.%s", moduleName, funcName)
				sa.symbolTable.AddSymbol(qualifiedName, "third_party_function", false, "global", 0, 0)
			}
			fmt.Printf("Auto-analyzed and loaded: %s (%d functions)\n", moduleName, len(libConfig.Functions))
			return
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

	// 保存前缀的符号表，以便 @前缀块 中引用 $变量
	sa.prefixSymbolTables[stmt.Name] = sa.symbolTable

	sa.symbolTable = oldSymbolTable
	sa.scope--
}

// analyzePrefixCallExpression 分析前缀调用表达式
// 分析调用体内的语句并检测变量遮蔽等潜在问题
func (sa *SemanticAnalyzer) analyzePrefixCallExpression(expr *ast.PrefixCallExpression) {
	// 获取前缀函数定义
	funcDecl := sa.findFunctionDeclaration(expr.Name)
	if funcDecl == nil {
		// 尝试查找前缀定义（prefix 语句）
		if prefixTable, ok := sa.prefixSymbolTables[expr.Name]; ok {
			oldPrefixTable := sa.currentPrefixTable
			sa.currentPrefixTable = prefixTable
			for _, bodyStmt := range expr.Body {
				sa.analyzeStatement(bodyStmt)
			}
			sa.currentPrefixTable = oldPrefixTable
			return
		}
		// 如果找不到前缀定义，仍然需要分析调用体内的语句
		for _, bodyStmt := range expr.Body {
			sa.analyzeStatement(bodyStmt)
		}
		return
	}

	// 检查是否是前缀函数
	annotation := funcDecl.GetAnnotation()
	if annotation != ast.TreeAnnotationPrefix &&
		annotation != ast.TreeAnnotationPrefixTree {
		// 不是前缀函数，但仍然需要分析调用体内的语句
		// 尝试查找前缀定义（prefix 语句）
		if prefixTable, ok := sa.prefixSymbolTables[expr.Name]; ok {
			oldPrefixTable := sa.currentPrefixTable
			sa.currentPrefixTable = prefixTable
			for _, bodyStmt := range expr.Body {
				sa.analyzeStatement(bodyStmt)
			}
			sa.currentPrefixTable = oldPrefixTable
			return
		}
		for _, bodyStmt := range expr.Body {
			sa.analyzeStatement(bodyStmt)
		}
		return
	}

	// 收集前缀函数的参数列表
	prefixParams := make(map[string]bool)
	for _, param := range funcDecl.Params {
		prefixParams[param] = true
	}

	// 分析调用体内的语句，检测变量遮蔽
	for _, bodyStmt := range expr.Body {
		sa.analyzeStatement(bodyStmt)
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

func (sa *SemanticAnalyzer) analyzeEnumStatement(stmt *ast.EnumStatement) {
	sa.symbolTable.AddSymbol(stmt.Name, "enum", false, "global", stmt.Pos.Line, stmt.Pos.Column)
	// 注册枚举变体到符号表，存储父枚举名
	for _, variant := range stmt.Variants {
		sa.symbolTable.AddSymbol(variant.Name, "enum_variant:"+stmt.Name, false, "global", stmt.Pos.Line, stmt.Pos.Column)
	}
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

	// 4. const 必须有初始值
	if stmt.IsConst && stmt.Value == nil {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("const 声明 '%s' 必须有初始值", stmt.Name),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"const name: type = value",
		)
	}
}

// analyzeTypeAliasStatement 分析类型别名声明
func (sa *SemanticAnalyzer) analyzeTypeAliasStatement(stmt *ast.TypeAliasStatement) {
	sa.symbolTable.AddSymbol(stmt.Name, "type", false, "global", stmt.Pos.Line, stmt.Pos.Column)
}

// analyzeExternStatement 分析 extern 外部符号/函数声明
func (sa *SemanticAnalyzer) analyzeExternStatement(stmt *ast.ExternStatement) {
	if stmt.IsFunction {
		// extern fn 函数声明
		// 检查返回类型
		if stmt.ReturnType != "" && stmt.ReturnType != "void" && !sa.isTypeValid(stmt.ReturnType) {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("extern fn 返回类型 '%s' 未知", stmt.ReturnType),
				stmt.Pos.Line,
				stmt.Pos.Column,
				"",
				"检查返回类型名称",
			)
		}
		// 检查参数类型
		for i, pType := range stmt.ParamTypes {
			if !sa.isTypeValid(pType) {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("extern fn 参数 %d 类型 '%s' 未知", i+1, pType),
					stmt.Pos.Line,
					stmt.Pos.Column,
					"",
					"检查参数类型名称",
				)
			}
		}
		// 注册为函数符号
		sa.symbolTable.AddSymbol(stmt.Name, stmt.ReturnType, false, "extern_func", stmt.Pos.Line, stmt.Pos.Column)
	} else {
		// extern 变量声明
		// 1. 检查类型是否存在
		if !sa.isTypeValid(stmt.Type) {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("未知类型 '%s'，extern 声明必须使用已定义的类型", stmt.Type),
				stmt.Pos.Line,
				stmt.Pos.Column,
				"",
				"检查类型名称是否正确",
			)
		}

		// 2. 添加外部符号到符号表（标记为 extern）
		sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "extern", stmt.Pos.Line, stmt.Pos.Column)
	}
}

// analyzeAutoDeclaration 分析 auto 声明（类型推导）
func (sa *SemanticAnalyzer) analyzeAutoDeclaration(stmt *ast.VariableDeclaration) {
	if stmt.Value == nil {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("auto 声明 '%s' 必须有初始值用于类型推导", stmt.Name),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"auto name = value",
		)
		return
	}
	
	// 分析表达式
	sa.analyzeExpression(stmt.Value)
	
	// 推导类型
	inferredType := sa.inferExpressionType(stmt.Value)
	if inferredType == "" {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("无法推导变量 '%s' 的类型", stmt.Name),
			stmt.Pos.Line,
			stmt.Pos.Column,
			"",
			"使用明确类型的初始值",
		)
		return
	}
	
	stmt.Type = inferredType
	
	// 添加到符号表
	sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, false, "local", stmt.Pos.Line, stmt.Pos.Column)
}

// isTypeValid 检查类型是否有效
func (sa *SemanticAnalyzer) isTypeValid(typeName string) bool {
	// 基本类型
	basicTypes := map[string]bool{
		"int":    true,
		"integer": true,
		"i8":     true,
		"i16":    true,
		"i32":    true,
		"i64":    true,
		"int8":   true,
		"int16":  true,
		"int32":  true,
		"int64":  true,
		"u8":     true,
		"u16":    true,
		"u32":    true,
		"u64":    true,
		"uint8":  true,
		"uint16": true,
		"uint32": true,
		"uint64": true,
		"uint":   true,
		"uchar":  true,
		"ushort": true,
		"ulong":  true,
		"float":  true,
		"f32":    true,
		"single": true,
		"f64":    true,
		"double": true,
		"real":   true,
		"bool":   true,
		"boolean": true,
		"char":   true,
		"byte":   true,
		"sbyte":  true,
		"string": true,
		"str":    true,
		"cstring": true,
		"void":   true,
		"any":    true,
		"long":   true,
		"short":  true,
		"size":   true,
		"ssize":  true,
		"intptr": true,
		"uintptr": true,
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
	
	// 检查是否是数组类型（如 []int）
	if strings.HasPrefix(typeName, "[]") {
		innerType := typeName[2:]
		return sa.isTypeValid(innerType)
	}
	
	// 检查是否是const类型（如 const char*）
	if strings.HasPrefix(typeName, "const ") {
		innerType := typeName[6:]
		return sa.isTypeValid(innerType)
	}
	
	// 检查符号表中是否有该类型（类、结构体、枚举、接口等）
	symbol := sa.symbolTable.GetSymbol(typeName)
	if symbol != nil && (symbol.Type == "class" || symbol.Type == "struct" || symbol.Type == "enum" || symbol.Type == "interface" || symbol.Type == "type") {
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
		// 空指针安全：检测 if x != null 模式，标记 x 为已检查
		sa.markNullCheckedInCondition(stmt.Condition)
	}
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
	// 退出 if body 后，null checked 标记不再有效（简化实现：不清除，因为 else 分支可能也需要）
	for _, elseStmt := range stmt.Else {
		sa.analyzeStatement(elseStmt)
	}
}

// markNullCheckedInCondition 检测 if 条件中的 null 检查模式
// 支持: if x != null, if null != x, if x == null (else 分支中 x 已检查)
func (sa *SemanticAnalyzer) markNullCheckedInCondition(cond ast.Expression) {
	binExpr, ok := cond.(*ast.BinaryExpression)
	if !ok {
		return
	}
	isNullCheck := false
	var checkedVar string

	// x != null
	if binExpr.Operator == "!=" {
		if ident, ok := binExpr.Left.(*ast.Identifier); ok {
			if right, ok := binExpr.Right.(*ast.Identifier); ok && right.Name == "null" {
				isNullCheck = true
				checkedVar = ident.Name
			}
		}
	}
	// null != x
	if binExpr.Operator == "!=" {
		if left, ok := binExpr.Left.(*ast.Identifier); ok && left.Name == "null" {
			if ident, ok := binExpr.Right.(*ast.Identifier); ok {
				isNullCheck = true
				checkedVar = ident.Name
			}
		}
	}

	if isNullCheck && checkedVar != "" {
		symbol := sa.symbolTable.GetSymbol(checkedVar)
		if symbol != nil && symbol.Nullable {
			symbol.NullChecked = true
		}
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

// analyzeExpression 分析表达式（遍历所有表达式节点并检查）
func (sa *SemanticAnalyzer) analyzeExpression(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		sa.analyzeIdentifier(e)
	case *ast.BinaryExpression:
		sa.analyzeBinaryExpression(e)
	case *ast.UnaryExpression:
		sa.analyzeUnaryExpression(e)
	case *ast.CallExpression:
		sa.analyzeCallExpression(e)
	case *ast.IndexExpression:
		sa.analyzeIndexExpression(e)
	case *ast.MemberExpression:
		sa.analyzeMemberExpression(e)
	case *ast.LiteralExpression:
		// 字面量不需要额外分析
	case *ast.ParenExpression:
		sa.analyzeExpression(e.Inner)
	case *ast.ConditionalExpression:
		sa.analyzeExpression(e.Condition)
		sa.analyzeExpression(e.TrueExpr)
		sa.analyzeExpression(e.FalseExpr)
	case *ast.ArrayLiteral:
		for _, elem := range e.Elements {
			sa.analyzeExpression(elem)
		}
	case *ast.PrefixCallExpression:
		// 前缀调用表达式已在 analyzePrefixCallExpression 中处理
	case *ast.SizeOfExpression, *ast.AlignOfExpression, *ast.OffsetOfExpression:
		// 编译期内建函数，类型检查在 inferExpressionType 中处理
	case *ast.ComptimeExpression:
		sa.analyzeExpression(e.Inner)
	case *ast.TypeNameExpression, *ast.FieldCountExpression, *ast.TypeKindExpression:
		// 编译期反射函数，不需要额外分析
	case *ast.FieldNameExpression:
		sa.analyzeExpression(e.Index)
	case *ast.FieldTypeExpression:
		sa.analyzeExpression(e.Index)
	case *ast.AttributeExpression:
		// 表达式级属性（#[asm(...)]、#[atomic_load(...)] 等）
		// 参数是字符串字面量或变量引用，由 codegen 负责生成 C 代码
		// 这里做基本的名称校验
		if e.Attr != nil {
			switch e.Attr.Name {
			case "asm", "volatile_load", "volatile_store",
				"atomic_load", "atomic_store", "atomic_cas", "atomic_faa", "fence":
				// 已知的属性表达式，跳过参数分析（参数可以是表达式或字符串）
			default:
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("未知的属性表达式: #[%s]", e.Attr.Name),
					e.Pos.Line,
					e.Pos.Column,
					"",
					"支持的属性表达式: asm, volatile_load, volatile_store, atomic_load, atomic_store, atomic_cas, atomic_faa, fence",
				)
			}
		}
	}
}

// analyzeIdentifier 分析标识符
func (sa *SemanticAnalyzer) analyzeIdentifier(expr *ast.Identifier) {
	if expr == nil {
		return
	}
	if expr.Name == "null" || expr.Name == "true" || expr.Name == "false" {
		return
	}

	// 处理前缀变量（$var）
	if expr.IsPrefixVar {
		if sa.currentPrefixTable != nil {
			symbol := sa.currentPrefixTable.GetSymbol(expr.Name)
			if symbol == nil {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("未定义的前缀变量: '$%s'", expr.Name),
					expr.Pos.Line,
					expr.Pos.Column,
					"undefined_variable",
					"请确保前缀变量已声明后再使用",
				)
			}
			return
		}
		// 如果不在 @前缀块 中，$var 引用是不合法的
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("前缀变量 '$%s' 只能在 @前缀块 中使用", expr.Name),
			expr.Pos.Line,
			expr.Pos.Column,
			"undefined_variable",
			"请确保在 @前缀名 { } 块中使用 $ 前缀变量",
		)
		return
	}

	symbol := sa.symbolTable.GetSymbol(expr.Name)
	if symbol == nil {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("未定义的变量: '%s'", expr.Name),
			expr.Pos.Line,
			expr.Pos.Column,
			"undefined_variable",
			"请确保变量已声明后再使用",
		)
	}
}

// analyzeBinaryExpression 分析二元表达式
func (sa *SemanticAnalyzer) analyzeBinaryExpression(expr *ast.BinaryExpression) {
	if expr == nil {
		return
	}
	sa.analyzeExpression(expr.Left)
	sa.analyzeExpression(expr.Right)
	// 类型检查
	if expr.Left != nil && expr.Right != nil {
		leftType := sa.inferExpressionType(expr.Left)
		rightType := sa.inferExpressionType(expr.Right)
		if leftType != "" && rightType != "" && leftType != rightType {
			switch expr.Operator {
			case "+", "-", "*", "/", "%":
				if !isNumericType(leftType) || !isNumericType(rightType) {
					sa.errorCollector.AddSemanticError(
						fmt.Sprintf("运算符 '%s' 不能用于类型 '%s' 和 '%s'", expr.Operator, leftType, rightType),
						expr.Pos.Line,
						expr.Pos.Column,
						"type_mismatch",
						"确保运算符两侧的类型兼容",
					)
				}
			case "==", "!=", "<", ">", "<=", ">=":
				if leftType != rightType && !(isNumericType(leftType) && isNumericType(rightType)) {
					sa.errorCollector.AddSemanticError(
						fmt.Sprintf("比较运算符 '%s' 不能用于类型 '%s' 和 '%s'", expr.Operator, leftType, rightType),
						expr.Pos.Line,
						expr.Pos.Column,
						"type_mismatch",
						"比较运算符两侧的类型必须兼容",
					)
				}
			}
		}
	}
}

// analyzeUnaryExpression 分析一元表达式
func (sa *SemanticAnalyzer) analyzeUnaryExpression(expr *ast.UnaryExpression) {
	if expr == nil {
		return
	}
	sa.analyzeExpression(expr.Right)
}

// analyzeCallExpression 分析函数调用表达式
func (sa *SemanticAnalyzer) analyzeCallExpression(expr *ast.CallExpression) {
	if expr == nil {
		return
	}
	
	// 检查是否是标准库函数调用，验证是否已导入对应模块
	if memberAccess, ok := expr.Function.(*ast.MemberAccessExpression); ok {
		sa.checkStdlibImport(memberAccess, expr.Pos)
	}
	
	sa.analyzeExpression(expr.Function)
	for _, arg := range expr.Args {
		sa.analyzeExpression(arg)
	}
}

// checkStdlibImport 检查标准库模块是否已导入
func (sa *SemanticAnalyzer) checkStdlibImport(memberAccess *ast.MemberAccessExpression, pos ast.Position) {
	// 解析模块路径：std.module.function -> module
	var moduleName string
	
	// 检查是否是 std.module.function 形式
	if nestedMember, ok := memberAccess.Object.(*ast.MemberAccessExpression); ok {
		if innerIdent, ok := nestedMember.Object.(*ast.Identifier); ok && innerIdent.Name == "std" {
			moduleName = nestedMember.Member
		}
	}
	
	// 如果没有找到模块名，不是标准库调用
	if moduleName == "" {
		return
	}
	
	// 检查模块是否已导入（使用 importedModules 而不是符号表）
	stdlibKey := "std." + moduleName
	if !sa.importedModules[moduleName] && !sa.importedModules[stdlibKey] {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("标准库模块 '%s' 未导入，请添加 'import std.%s;' 语句", moduleName, moduleName),
			pos.Line,
			pos.Column,
			"missing_import",
			fmt.Sprintf("在文件顶部添加: import std.%s;", moduleName),
		)
	}
}

// analyzeIndexExpression 分析索引表达式
func (sa *SemanticAnalyzer) analyzeIndexExpression(expr *ast.IndexExpression) {
	if expr == nil {
		return
	}
	// 空指针安全：对 Nullable 类型解引用时发出警告
	sa.checkNullableDereference(expr.Object, expr.Pos.Line, expr.Pos.Column)
	sa.analyzeExpression(expr.Object)
	sa.analyzeExpression(expr.Index)
}

// analyzeMemberExpression 分析成员访问表达式
func (sa *SemanticAnalyzer) analyzeMemberExpression(expr *ast.MemberExpression) {
	if expr == nil {
		return
	}
	// 空指针安全：对 Nullable 类型解引用时发出警告
	sa.checkNullableDereference(expr.Object, expr.Pos.Line, expr.Pos.Column)
	sa.analyzeExpression(expr.Object)
}

// checkNullableDereference 检查是否对 Nullable 类型的值进行了解引用
// 如果 expr 是一个 Nullable 类型的标识符，且未在 if 条件中做过 null 检查，发出警告
func (sa *SemanticAnalyzer) checkNullableDereference(expr ast.Expression, line, column int) {
	if expr == nil {
		return
	}
	ident, ok := expr.(*ast.Identifier)
	if !ok {
		return
	}
	symbol := sa.symbolTable.GetSymbol(ident.Name)
	if symbol == nil {
		return
	}
	if symbol.Nullable && !symbol.NullChecked {
		sa.error(fmt.Sprintf("nullable variable '%s' may be null, add null check before access (e.g., if %s != null { ... })", ident.Name, ident.Name), line, column)
	}
}

// inferExpressionType 推断表达式的类型
func (sa *SemanticAnalyzer) inferExpressionType(expr ast.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if e.Name == "true" || e.Name == "false" {
			return "bool"
		}
		if e.Name == "null" {
			return "null"
		}
		symbol := sa.symbolTable.GetSymbol(e.Name)
		if symbol != nil {
			return symbol.Type
		}
		return ""
	case *ast.IntegerLiteral:
		if e.Value <= 255 {
			return "u8"
		}
		if e.Value <= 65535 {
			return "u16"
		}
		if e.Value <= 4294967295 {
			return "u32"
		}
		return "u64"
	case *ast.FloatLiteral:
		return "f64"
	case *ast.StringLiteral:
		return "string"
	case *ast.BooleanLiteral:
		return "bool"
	case *ast.LiteralExpression:
		return sa.inferLiteralType(e)
	case *ast.BinaryExpression:
		leftType := sa.inferExpressionType(e.Left)
		rightType := sa.inferExpressionType(e.Right)
		if leftType != "" && rightType != "" {
			if isFloatType(leftType) || isFloatType(rightType) {
				return "f64"
			}
			if isIntegerType(leftType) && isIntegerType(rightType) {
				return promoteIntegerType(leftType, rightType)
			}
			if leftType == "string" || rightType == "string" {
				return "string"
			}
			return leftType
		}
		if leftType != "" {
			return leftType
		}
		return rightType
	case *ast.UnaryExpression:
		operandType := sa.inferExpressionType(e.Right)
		if e.Operator == "!" {
			return "bool"
		}
		if e.Operator == "-" || e.Operator == "+" {
			// 负号/正号运算：确保结果为有符号类型
			if operandType == "u8" || operandType == "u16" || operandType == "u32" || operandType == "u64" {
				// 无符号类型提升为有符号
				switch operandType {
				case "u8":
					return "i8"
				case "u16":
					return "i16"
				case "u32":
					return "i32"
				default:
					return "i64"
				}
			}
		}
		return operandType
	case *ast.CallExpression:
		funcExpr := e.Function
		if ident, ok := funcExpr.(*ast.Identifier); ok {
			symbol := sa.symbolTable.GetSymbol(ident.Name)
			if symbol != nil && symbol.Type == "function" {
				return "int"
			}
		}
		if member, ok := funcExpr.(*ast.MemberAccessExpression); ok {
			if sa.stdlibConfig != nil {
				funcName := member.Member
				for _, mod := range sa.stdlibConfig.Modules {
					if sig, ok := mod.Functions[funcName]; ok && sig.Return != "" {
						return sig.Return
					}
				}
			}
		}
		return ""
	case *ast.ArrayLiteral:
		if len(e.Elements) > 0 {
			elemType := sa.inferExpressionType(e.Elements[0])
			if elemType != "" {
				return "[]" + elemType
			}
		}
		return "[]any"
	case *ast.ConditionalExpression:
		trueType := sa.inferExpressionType(e.TrueExpr)
		falseType := sa.inferExpressionType(e.FalseExpr)
		if trueType == falseType {
			return trueType
		}
		if trueType != "" {
			return trueType
		}
		return falseType
	case *ast.SizeOfExpression, *ast.AlignOfExpression, *ast.OffsetOfExpression:
		return "u64"
	case *ast.ComptimeExpression:
		return sa.inferExpressionType(e.Inner)
	case *ast.TypeNameExpression, *ast.FieldNameExpression, *ast.FieldTypeExpression, *ast.TypeKindExpression:
		return "string"
	case *ast.FieldCountExpression:
		return "u64"
	default:
		return ""
	}
}

// inferLiteralType 推断字面量的类型
func (sa *SemanticAnalyzer) inferLiteralType(expr *ast.LiteralExpression) string {
	if expr == nil {
		return ""
	}
	switch expr.Kind {
	case "int":
		if val, ok := expr.Value.(int64); ok {
			if val >= 0 && val <= 255 {
				return "u8"
			}
			if val >= -128 && val <= 127 {
				return "i8"
			}
			if val >= 0 && val <= 65535 {
				return "u16"
			}
			if val >= -32768 && val <= 32767 {
				return "i16"
			}
			if val >= 0 && val <= 2147483647 {
				return "i32"
			}
			return "i64"
		}
		return "int"
	case "float":
		return "f64"
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "char":
		return "char"
	case "null":
		return "null"
	default:
		return ""
	}
}

// isFloatType 检查是否是浮点类型
func isFloatType(typeName string) bool {
	return typeName == "float" || typeName == "f32" || typeName == "f64" || 
		typeName == "double" || typeName == "real" || typeName == "single"
}

// isIntegerType 检查是否是整数类型
func isIntegerType(typeName string) bool {
	return typeName == "int" || typeName == "integer" || 
		typeName == "i8" || typeName == "i16" || typeName == "i32" || typeName == "i64" ||
		typeName == "int8" || typeName == "int16" || typeName == "int32" || typeName == "int64" ||
		typeName == "u8" || typeName == "u16" || typeName == "u32" || typeName == "u64" ||
		typeName == "uint8" || typeName == "uint16" || typeName == "uint32" || typeName == "uint64" ||
		typeName == "uint" || typeName == "uchar" || typeName == "ushort" || typeName == "ulong" ||
		typeName == "byte" || typeName == "sbyte" || typeName == "long" || typeName == "short"
}

// promoteIntegerType 整数类型提升
func promoteIntegerType(left, right string) string {
	precision := map[string]int{
		"i8": 1, "int8": 1, "sbyte": 1, "byte": 1, "u8": 1, "uint8": 1, "uchar": 1,
		"i16": 2, "int16": 2, "short": 2, "u16": 2, "uint16": 2, "ushort": 2,
		"i32": 3, "int32": 3, "u32": 3, "uint32": 3,
		"i64": 4, "int64": 4, "int": 4, "integer": 4, "long": 4,
		"u64": 5, "uint64": 5, "uint": 5, "ulong": 5,
	}
	leftPrec := precision[left]
	rightPrec := precision[right]
	if leftPrec >= rightPrec {
		return left
	}
	return right
}

// isNumericType 检查类型是否为数值类型
func isNumericType(typeName string) bool {
	return typeName == "int" || typeName == "float" || typeName == "i64" ||
		typeName == "i32" || typeName == "i16" || typeName == "i8" ||
		typeName == "u64" || typeName == "u32" || typeName == "u16" ||
		typeName == "u8" || typeName == "f64" || typeName == "f32"
}

func (sa *SemanticAnalyzer) error(msg string, line, column int) {
	suggestion := errors.GenerateSuggestion(msg)
	context, sourceLine, lineNumStr := errors.ExtractSourceContext(sa.source, line, column)
	err := &errors.Error{
		Type:       errors.ErrorSemantic,
		Message:    msg,
		Line:       line,
		Column:     column,
		File:       "",
		Suggestion: suggestion,
		SourceContext: context,
		SourceLine: sourceLine,
		LineNumberStr: lineNumStr,
	}
	sa.errorCollector.AddErrorInstance(err)
}

// checkTypeConstraint 检查类型约束
func (sa *SemanticAnalyzer) checkTypeConstraint(typeName, constraint string, line, column int) bool {
	if constraint == "" || constraint == "any" {
		return true
	}

	// 标准化类型名：映射 Kaula 类型别名
	normalizedType := typeName
	switch typeName {
	case "i8", "i16", "i32", "i64", "int", "integer", "long":
		normalizedType = "int"
	case "u8", "u16", "u32", "u64", "uint", "uchar", "ushort", "ulong":
		normalizedType = "int" // 无符号整数也是可比较/有序的
	case "f32", "f64", "float", "double", "real", "single":
		normalizedType = "float"
	case "bool", "boolean":
		normalizedType = "bool"
	case "char", "byte", "sbyte":
		normalizedType = "int"
	case "string", "str", "cstring":
		normalizedType = "string"
	}

	// 检查类型是否满足约束
	switch constraint {
	case "comparable":
		// 可比较类型：基本类型、指针等
		switch normalizedType {
		case "int", "float", "string", "bool":
			return true
		default:
			// 指针类型也可比较
			if strings.HasSuffix(typeName, "*") {
				return true
			}
		}
	case "ordered":
		// 有序类型：可以进行大小比较
		switch normalizedType {
		case "int", "float", "string":
			return true
		}
	case "number":
		// 数值类型
		switch normalizedType {
		case "int", "float":
			return true
		}
	case "integer":
		// 整数类型
		if normalizedType == "int" {
			return true
		}
	case "floating":
		// 浮点类型
		if normalizedType == "float" {
			return true
		}
	}

	sa.error(fmt.Sprintf("type %s does not satisfy constraint %s", typeName, constraint), line, column)
	return false
}

// validateGenericInstantiation 验证泛型实例化
// 查找函数的泛型参数定义，检查每个实参类型是否满足对应的约束
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

	// 查找函数定义中的泛型参数约束
	// 从 genericStack 中找到对应的函数定义
	var paramConstraints []map[string][]string
	for _, fn := range sa.genericStack {
		if fn.Name == funcName && fn.IsGeneric() {
			constraints := make(map[string][]string)
			for i, tp := range fn.TypeParams {
				if tp.Constraint != "" && tp.Constraint != "any" {
					constraints[tp.Name] = append(constraints[tp.Name], tp.Constraint)
				}
				// 如果类型参数数量与实参数量匹配，直接按位置对应
				if i < len(typeArgs) && len(constraints[tp.Name]) > 0 {
					for _, c := range constraints[tp.Name] {
						if !sa.checkTypeConstraint(typeArgs[i], c, line, column) {
							return false
						}
					}
				}
			}
			paramConstraints = append(paramConstraints, constraints)
		}
	}

	// 也检查全局类型约束表（兼容旧的注册方式）
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

// analyzeYieldStatement 分析 yield 语句
func (sa *SemanticAnalyzer) analyzeYieldStatement(stmt *ast.YieldStatement) {
	if !sa.isFunctionSOR() {
		sa.errorCollector.AddSemanticError(
			"未定义: 'yield' 是 SOR 扩展的原语，需要使用 #[sor] 注解或 --sor 标志才能使用",
			stmt.Pos.Line, stmt.Pos.Column, "",
			"在函数上添加 #[sor] 注解，或使用 'kaulac --sor <文件>' 编译")
		return
	}
	if stmt.Source != nil {
		sa.analyzeExpression(stmt.Source)
	}
}

// analyzeReleaseStatement 分析 release 语句
func (sa *SemanticAnalyzer) analyzeReleaseStatement(stmt *ast.ReleaseStatement) {
	if !sa.isFunctionSOR() {
		sa.errorCollector.AddSemanticError(
			"未定义: 'release' 是 SOR 扩展的原语，需要使用 #[sor] 注解或 --sor 标志才能使用",
			stmt.Pos.Line, stmt.Pos.Column, "",
			"在函数上添加 #[sor] 注解，或使用 'kaulac --sor <文件>' 编译")
		return
	}
	if stmt.Source != nil {
		sa.analyzeExpression(stmt.Source)
	}
}

// analyzeExtractStatement 分析 extract 语句
func (sa *SemanticAnalyzer) analyzeExtractStatement(stmt *ast.ExtractStatement) {
	if !sa.isFunctionSOR() {
		sa.errorCollector.AddSemanticError(
			"未定义: 'extract' 是 SOR 扩展的原语，需要使用 #[sor] 注解或 --sor 标志才能使用",
			stmt.Pos.Line, stmt.Pos.Column, "",
			"在函数上添加 #[sor] 注解，或使用 'kaulac --sor <文件>' 编译")
		return
	}
	if stmt.Source != nil {
		sa.analyzeExpression(stmt.Source)
	}
	if stmt.Index != nil {
		sa.analyzeExpression(stmt.Index)
	}
}

// hasSORPrimitives 递归检查函数体中是否使用了 SOR 原语 (yield/release/extract)
func (sa *SemanticAnalyzer) hasSORPrimitives(body []ast.Statement) bool {
	for _, stmt := range body {
		if stmt == nil {
			continue
		}
		switch stmt.(type) {
		case *ast.YieldStatement:
			return true
		case *ast.ReleaseStatement:
			return true
		case *ast.ExtractStatement:
			return true
		case *ast.BlockStatement:
			block := stmt.(*ast.BlockStatement)
			if sa.hasSORPrimitives(block.Statements) {
				return true
			}
		case *ast.IfStatement:
			ifStmt := stmt.(*ast.IfStatement)
			if sa.hasSORPrimitives(ifStmt.Body) || sa.hasSORPrimitives(ifStmt.Else) {
				return true
			}
		case *ast.WhileStatement:
			whileStmt := stmt.(*ast.WhileStatement)
			if sa.hasSORPrimitives(whileStmt.Body) {
				return true
			}
		case *ast.ForStatement:
			forStmt := stmt.(*ast.ForStatement)
			if sa.hasSORPrimitives(forStmt.Body) {
				return true
			}
		}
	}
	return false
}