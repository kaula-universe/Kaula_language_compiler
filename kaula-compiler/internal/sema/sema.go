package sema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/comptime"
	"kaula-compiler/internal/core"
	"kaula-compiler/internal/errors"
	"kaula-compiler/internal/stdlib"
	"kaula-compiler/internal/symbol"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type SemanticAnalyzer struct {
	symbolTable        *symbol.SymbolTable
	scope              int
	errorCollector     *errors.ErrorCollector
	currentFunction    *ast.FunctionStatement
	sorGlobalEnabled   bool // 全局 SOR 模式（--sor 标志）
	program            *ast.Program
	stdlibConfig       *stdlib.StdlibConfig
	genericStack       []*ast.FunctionStatement
	typeConstraints    map[string][]string
	exportedSymbols    map[string]bool
	treeManager        *core.TreeManager
	prefixManager      *core.PrefixManager
	rootTreeFound      bool
	source             string                         // 源码用于错误上下文
	prefixSymbolTables map[string]*symbol.SymbolTable // 前缀名 -> 前缀符号表
	currentPrefixTable *symbol.SymbolTable            // 当前 @前缀块 的符号表
	comptime           *comptime.Evaluator            // 编译期表达式评估器
	importedModules    map[string]bool                // 记录实际导入的模块
	thirdPartyTypeSet  map[string]bool                // 第三方库签名中的类型名缓存（lazy 构建）
	funcReturnTypes    map[string]string              // 函数名 -> 返回类型（用于调用表达式类型推断）
	arrayLens          map[string]int                 // 数组变量名 -> 元素个数（用于 spend 全集证明）
	localImportFuncs   map[string]bool                // 本地 import 的 pub 函数名
	localModuleFuncs   map[string]bool                // 本地 import 模块的全部函数名(含非 pub, 导出检查用)
	localPubVars       map[string]bool                // 本地 import / export 的变量名（跨文件变量引用）
	inElseIfChain      bool                           // 当前是否在 else-if 链的内层分支 (避免重复的 match 建议警告)
}

// SetLocalImportFuncs 注册本地 import 的 pub 函数（跨文件调用）
func (sa *SemanticAnalyzer) SetLocalImportFuncs(funcs map[string]bool) {
	sa.localImportFuncs = funcs
}

// SetLocalModuleFuncs 注册本地 import 模块的全部函数名（导出检查用）
// 调用存在于被 import 模块但非 pub 的函数时, 报"未导出"错误
func (sa *SemanticAnalyzer) SetLocalModuleFuncs(funcs map[string]bool) {
	sa.localModuleFuncs = funcs
}

// SetLocalPubVars 注册本地 import / export 的变量名（跨文件变量引用）
func (sa *SemanticAnalyzer) SetLocalPubVars(vars map[string]bool) {
	sa.localPubVars = vars
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
		symbolTable:        globalSymbolTable,
		scope:              1,
		errorCollector:     errorCollector,
		currentFunction:    nil,
		stdlibConfig:       stdlibConfig,
		genericStack:       make([]*ast.FunctionStatement, 0),
		typeConstraints:    make(map[string][]string),
		exportedSymbols:    make(map[string]bool),
		treeManager:        core.NewTreeManager(),
		prefixManager:      core.NewPrefixManager(),
		rootTreeFound:      false,
		prefixSymbolTables: make(map[string]*symbol.SymbolTable),
		comptime:           nil,
		importedModules:    make(map[string]bool),
		arrayLens:          make(map[string]int),
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

	// 第一遍：将所有函数和变量添加到符号表（不分析函数体/方法体/构造函数体）
	sa.funcReturnTypes = make(map[string]string)
	for _, stmt := range program.Statements {
		if funcStmt, ok := stmt.(*ast.FunctionStatement); ok {
			sa.funcReturnTypes[funcStmt.Name] = funcStmt.ReturnType
		}
		sa.analyzeStatement(stmt)
	}

	// 第二遍：分析函数体
	for _, stmt := range program.Statements {
		if funcStmt, ok := stmt.(*ast.FunctionStatement); ok {
			sa.analyzeFunctionBody(funcStmt)
		}
		// 第二遍：分析类的方法体和构造函数体
		if classStmt, ok := stmt.(*ast.ClassStatement); ok {
			sa.analyzeClassMembersBody(classStmt)
		}
	}
}

// analyzeClassMembersBody 分析类方法体和构造函数体（第二遍调用，类符号已注册）
func (sa *SemanticAnalyzer) analyzeClassMembersBody(classStmt *ast.ClassStatement) {
	fmt.Fprintf(os.Stderr, "[SEMA DEBUG] analyzeClassMembersBody: class=%s, %d ctors, %d methods\n",
		classStmt.Name, len(classStmt.Constructors), len(classStmt.Methods))
	// 分析构造函数
	for _, ctor := range classStmt.Constructors {
		oldSymbolTable := sa.symbolTable
		sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "constructor_"+classStmt.Name)
		sa.scope++

		// 注册 self：类型为类的指针类型
		sa.symbolTable.AddSymbol("self", classStmt.Name+"*", false, "local", classStmt.Pos.Line, classStmt.Pos.Column)

		// 类字段不再注册为作用域符号：成员必须通过 self.field 显式访问，
		// 以消除参数/局部变量与字段同名的歧义。

		// 注册构造函数参数
		for _, param := range ctor.Params {
			sa.symbolTable.AddSymbol(param.Name, param.Type, param.Nullable, "parameter", classStmt.Pos.Line, classStmt.Pos.Column)
		}

		// 分析构造函数体
		for _, bodyStmt := range ctor.Body {
			sa.analyzeStatement(bodyStmt)
		}

		// 未使用检查已注释
		// sa.checkUnusedSymbols()

		sa.symbolTable = oldSymbolTable
		sa.scope--
	}

	// 分析方法体
	for _, method := range classStmt.Methods {
		oldSymbolTable := sa.symbolTable
		sa.symbolTable = symbol.NewSymbolTable(sa.symbolTable, "method_"+classStmt.Name+"_"+method.Name)
		sa.scope++

		sa.funcReturnTypes[classStmt.Name+"_"+method.Name] = method.ReturnType

		// 注册 self
		sa.symbolTable.AddSymbol("self", classStmt.Name+"*", false, "local", method.Pos.Line, method.Pos.Column)

		// 类字段不再注册为作用域符号：成员必须通过 self.field 显式访问。

		// 注册方法参数
		for _, param := range method.Params {
			sa.symbolTable.AddSymbol(param.Name, param.Type, param.Nullable, "parameter", method.Pos.Line, method.Pos.Column)
		}

		// 分析方法体
		for _, bodyStmt := range method.Body {
			sa.analyzeStatement(bodyStmt)
		}

		// 未使用检查已注释
		// sa.checkUnusedSymbols()

		sa.symbolTable = oldSymbolTable
		sa.scope--
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
	for i, param := range stmt.Params {
		if paramMap[param] {
			sa.error(fmt.Sprintf("duplicate parameter %s in function %s", param, stmt.Name), stmt.Pos.Line, stmt.Pos.Column)
		} else {
			paramMap[param] = true
			// 用参数声明的实际类型注册(而非硬编码 void*)
			paramType := "void*"
			if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
				paramType = stmt.ParamTypes[i]
			}
			sa.symbolTable.AddSymbol(param, paramType, false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
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

	// 未使用检查(warning)已注释：函数带 #[unused] 注解则整体豁免
	// if !ast.HasAttribute(stmt.Attributes, "unused") {
	// 	sa.checkUnusedSymbols()
	// }

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
	case *ast.SpendStatement:
		sa.analyzeSpendStatement(s)
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
	case *ast.ForInStatement:
		sa.analyzeForInStatement(s)
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
	if moduleName != "" {
		sa.symbolTable.AddSymbol(moduleName, "module", false, "global", stmt.Pos.Line, stmt.Pos.Column)
	}

	if stmt.Path != "" {
		// 路径导入（import "lib" / import "file"）：
		// 跨文件可调用符号由本地文件收集器注册（funcs 的 pub 函数），
		// 不参与 stdlib/第三方库配置解析
		sa.importedModules[stmt.Path] = true
		return
	}

	// 记录实际导入的模块
	sa.importedModules[moduleName] = true

	if sa.stdlibConfig != nil {
		// 检查是否是标准库模块
		// 支持三种导入格式: `io`、`std.io` 和 `freestanding.io`
		stdlibKey := moduleName
		if !strings.HasPrefix(stdlibKey, "std.") && !strings.HasPrefix(stdlibKey, "freestanding.") {
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
			// 注册模块中声明的类型
			for typeName := range mod.Types {
				sa.symbolTable.AddSymbol(typeName, "type", false, "global", 0, 0)
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
			// 剥离 UTF-8 BOM（json.Unmarshal 不识别 BOM）
			data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
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

// analyzeSpendStatement 分析spend语句 - 强制消费流全集证明
// 保证：target 的所有元素必须被 call 子句恰好覆盖一次；无法在编译期证明全消费则报错
func (sa *SemanticAnalyzer) analyzeSpendStatement(stmt *ast.SpendStatement) {
	if stmt.Target != nil {
		sa.analyzeExpression(stmt.Target)
	}

	// 1. 解析消费目标：确定元素总数（编译期可知）与消费模式
	count, known, enumName := sa.resolveSpendTarget(stmt.Target)
	enumMode := enumName != ""
	if enumName == "" {
		// 首次出现枚举变体时确定枚举模式
		for _, call := range stmt.Calls {
			if call.IsDefault || call.Index == nil {
				continue
			}
			if id, ok := call.Index.(*ast.Identifier); ok {
				if sym := sa.symbolTable.GetSymbol(id.Name); sym != nil &&
					strings.HasPrefix(sym.Type, "enum_variant:") {
					enumName = strings.TrimPrefix(sym.Type, "enum_variant:")
					enumMode = true
					break
				}
			}
		}
	}

	// 2. 逐个子句：索引合法性 + 唯一性
	used := make(map[int]bool) // 已使用的元素序号（1-based）
	hasDefault := false        // 是否存在兜底子句
	maxIndex := 0
	enumVariantOrder := map[string]int{} // 变体名 -> 序号（1-based）
	if enumName != "" {
		enumStmt := sa.program.FindEnum(enumName)
		if enumStmt != nil {
			for i, v := range enumStmt.Variants {
				enumVariantOrder[v.Name] = i + 1
			}
		}
	}

	for i, call := range stmt.Calls {
		if call.IsDefault {
			if hasDefault {
				sa.errorCollector.AddSemanticError(
					"spend 语句中最多只能有一个 call(default) 子句",
					call.Pos.Line, call.Pos.Column, "spend_multiple_default",
					"删除多余的 call(default) 子句",
				)
			}
			if enumMode {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("枚举消费模式（%s）必须穷尽所有变体，不允许 call(default)", enumName),
					call.Pos.Line, call.Pos.Column, "spend_default_on_enum",
					"为每个枚举变体编写 call 子句",
				)
			}
			hasDefault = true
		} else {
			sa.analyzeCallIndex(call, enumMode, used, enumVariantOrder, &maxIndex)
		}

		// 分析子句体
		for _, bodyStmt := range call.Body {
			sa.analyzeStatement(bodyStmt)
		}
		// 强制消费流：禁止提前退出（跳过剩余元素消费）
		sa.checkSpendBodyExit(i, len(stmt.Calls), call)
	}

	// 3. 全集覆盖证明
	if enumMode {
		// 枚举模式：所有变体必须被覆盖；不支持带数据的变体
		enumStmt := sa.program.FindEnum(enumName)
		if enumStmt != nil {
			for _, v := range enumStmt.Variants {
				if len(v.FieldTypes) > 0 {
					sa.errorCollector.AddSemanticError(
						fmt.Sprintf("spend 枚举模式暂不支持带数据的变体：%s.%s", enumName, v.Name),
						stmt.Pos.Line, stmt.Pos.Column, "spend_enum_with_data",
						"带数据的枚举请使用 match 表达式",
					)
				}
				idx := enumVariantOrder[v.Name]
				if idx == 0 {
					// 变体序列表在循环后才确定时，回退线性查找
					for i, ev := range enumStmt.Variants {
						if ev.Name == v.Name {
							idx = i + 1
							break
						}
					}
				}
				if !used[idx] {
					sa.errorCollector.AddSemanticError(
						fmt.Sprintf("spend 未穷尽枚举 '%s'：变体 %s 未被消费", enumName, v.Name),
						stmt.Pos.Line, stmt.Pos.Column, "spend_missing_variant",
						fmt.Sprintf("添加 call(%s) 子句", v.Name),
					)
				}
			}
		}
		return
	}

	// 数组模式：1..count 必须全部覆盖
	if known {
		if !hasDefault {
			for i := 1; i <= count; i++ {
				if !used[i] {
					sa.errorCollector.AddSemanticError(
						fmt.Sprintf("spend 未全量消费：元素 %d 未被消费（共 %d 个元素）", i, count),
						stmt.Pos.Line, stmt.Pos.Column, "spend_missing_element",
						fmt.Sprintf("添加 call(%d) 子句，或使用 call(default) 兜底", i),
					)
				}
			}
		}
		if maxIndex > count {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("call 索引 %d 超出元素总数 %d", maxIndex, count),
				stmt.Pos.Line, stmt.Pos.Column, "spend_index_out_of_range",
				"call 索引必须是 1..元素总数 范围内的元素",
			)
		}
	} else {
		// 编译期无法确定元素总数：必须有兜底子句
		if !hasDefault {
			sa.errorCollector.AddSemanticError(
				"spend 目标元素数量无法在编译期确定，必须提供 call(default) 子句",
				stmt.Pos.Line, stmt.Pos.Column, "spend_unknown_count",
				"使用数组字面量目标，或添加 call(default) 兜底子句",
			)
		}
	}
}

// resolveSpendTarget 解析 spend 目标的元素总数与模式
// 返回: (count, known, enumName)；known=true 表示 count 有效；enumName 非空表示枚举模式
func (sa *SemanticAnalyzer) resolveSpendTarget(target ast.Expression) (int, bool, string) {
	switch t := target.(type) {
	case *ast.ArrayLiteral:
		return len(t.Elements), true, ""
	case *ast.Identifier:
		sym := sa.symbolTable.GetSymbol(t.Name)
		if sym == nil {
			return 0, false, ""
		}
		if sa.program.FindEnum(sym.Type) != nil {
			enumStmt := sa.program.FindEnum(sym.Type)
			return len(enumStmt.Variants), true, sym.Type
		}
		if n, ok := sa.arrayLens[t.Name]; ok {
			return n, true, ""
		}
		return 0, false, ""
	default:
		return 0, false, ""
	}
}

// analyzeCallIndex 分析单个 call 子句的索引
func (sa *SemanticAnalyzer) analyzeCallIndex(call *ast.CallClause, enumMode bool, used map[int]bool, enumVariantOrder map[string]int, maxIndex *int) {
	if call.Index == nil {
		return
	}
	sa.analyzeExpression(call.Index)

	switch idx := call.Index.(type) {
	case *ast.IntegerLiteral:
		if enumMode {
			sa.errorCollector.AddSemanticError(
				"枚举消费模式必须使用变体名作为索引（如 call(Red)）",
				call.Pos.Line, call.Pos.Column, "spend_int_on_enum",
				"使用枚举变体名替换整数字面量",
			)
			return
		}
		index := int(idx.Value)
		if index < 1 {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("call 索引 %d 必须 >= 1", index),
				call.Pos.Line, call.Pos.Column, "spend_index_less_than_one",
				"call 索引从 1 开始",
			)
			return
		}
		if used[index] {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("call 索引 %d 重复消费", index),
				call.Pos.Line, call.Pos.Column, "spend_duplicate_index",
				"每个元素只能被消费一次，删除重复的 call 子句",
			)
			return
		}
		used[index] = true
		if index > *maxIndex {
			*maxIndex = index
		}
	case *ast.Identifier:
		sym := sa.symbolTable.GetSymbol(idx.Name)
		if sym == nil || !strings.HasPrefix(sym.Type, "enum_variant:") {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("call 索引 '%s' 必须是整数字面量、枚举变体或 default", idx.Name),
				call.Pos.Line, call.Pos.Column, "spend_invalid_index",
				"使用 call(<数字>) 或 call(<枚举变体>)",
			)
			return
		}
		// 枚举变体索引：定位变体序号
		variantEnum := strings.TrimPrefix(sym.Type, "enum_variant:")
		ordinal := enumVariantOrder[idx.Name]
		if ordinal == 0 {
			// 变体所属枚举尚未解析为枚举模式（首个变体），回退查找
			enumStmt := sa.program.FindEnum(variantEnum)
			if enumStmt != nil {
				for i, v := range enumStmt.Variants {
					if v.Name == idx.Name {
						ordinal = i + 1
						break
					}
				}
			}
		}
		if ordinal == 0 {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("枚举变体 '%s' 未找到", idx.Name),
				call.Pos.Line, call.Pos.Column, "spend_unknown_variant",
				"检查枚举定义",
			)
			return
		}
		if used[ordinal] {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("call(%s) 重复消费", idx.Name),
				call.Pos.Line, call.Pos.Column, "spend_duplicate_index",
				"每个变体只能被消费一次",
			)
			return
		}
		used[ordinal] = true
		if ordinal > *maxIndex {
			*maxIndex = ordinal
		}
	default:
		sa.errorCollector.AddSemanticError(
			"call 索引必须是整数字面量、枚举变体或 default",
			call.Pos.Line, call.Pos.Column, "spend_dynamic_index",
			"spend 的强制消费证明需要静态索引；元素数量编译期未知时请使用 call(default)",
		)
	}
}

// checkSpendBodyExit 检查 call 子句体中的提前退出
// 规则：break/continue 禁止；return 只允许出现在最后一个 call 子句（后面没有未消费元素）
func (sa *SemanticAnalyzer) checkSpendBodyExit(clauseIdx, totalClauses int, call *ast.CallClause) {
	lastClause := clauseIdx == totalClauses-1
	for _, bodyStmt := range call.Body {
		switch s := bodyStmt.(type) {
		case *ast.BreakStatement, *ast.ContinueStatement:
			sa.errorCollector.AddSemanticError(
				"spend 消费流内不允许 break/continue（会跳过剩余元素消费）",
				call.Pos.Line, call.Pos.Column, "spend_early_exit",
				"移除 break/continue；spend 必须消费全部元素后才能退出",
			)
		case *ast.ReturnStatement:
			if !lastClause {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("call 子句 %d 内不允许 return（会跳过后续元素的消费）", clauseIdx+1),
					s.Pos.Line, s.Pos.Column, "spend_early_return",
					"return 只允许出现在最后一个 call 子句中",
				)
			}
		}
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
	// 注册成员字段（scope=field_<类名>，供方法体 self.xxx / 裸字段名引用）
	for _, field := range stmt.Fields {
		if field == nil {
			continue
		}
		sa.symbolTable.AddSymbol(field.Name, field.Type, field.Nullable, "field_"+stmt.Name, field.Pos.Line, field.Pos.Column)
	}
	// 注册方法返回类型: "类名.方法名" -> 返回类型 (供 v.method() 调用类型推断)
	if sa.funcReturnTypes == nil {
		sa.funcReturnTypes = make(map[string]string)
	}
	for _, m := range stmt.Methods {
		if m != nil {
			sa.funcReturnTypes[stmt.Name+"."+m.Name] = m.ReturnType
		}
	}
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

	// [unused 检查已注释] 声明带 #[unused] 注解 → 豁免 unused 检查
	// if ast.HasAttribute(stmt.Attributes, "unused") {
	// 	if s := sa.symbolTable.GetSymbol(stmt.Name); s != nil {
	// 		s.Unused = true
	// 	}
	// }

	// 记录数组字面量长度，供 spend 全集证明使用
	if arrLit, ok := stmt.Value.(*ast.ArrayLiteral); ok {
		sa.arrayLens[stmt.Name] = len(arrLit.Elements)
	}

	// 3. 分析初始化表达式
	if stmt.Value != nil {
		sa.analyzeExpression(stmt.Value)
		// true/false 右值限布尔语境: 只能赋给布尔变量
		if !stmt.IsAuto {
			declType := stmt.Type
			isDeclBool := declType == "bool" || declType == "boolean"
			if isBoolLiteralValue(stmt.Value) && !isDeclBool {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("true/false 只能赋给布尔类型变量, 不能赋给 '%s'", declType),
					stmt.Pos.Line, stmt.Pos.Column, "bool_value",
					"布尔值只能用于布尔变量、布尔参数或比较表达式",
				)
			}
		}
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
		// FFI 边界约束：禁止泛型参数外露（void(T) 中 T 为单字母大写 = 疑似泛型参数）
		// extern 对应 C ABI，C 函数非泛型，签名必须全部为具体类型
		if voidSignatureHasGenericParam(stmt.ReturnType) {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("extern fn 返回类型 '%s' 含泛型参数，FFI 边界禁止泛型外露", stmt.ReturnType),
				stmt.Pos.Line, stmt.Pos.Column, "",
				"extern 对应 C ABI，签名必须为具体类型；请用具体类型或 type 别名替换泛型参数",
			)
		}
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
			if voidSignatureHasGenericParam(pType) {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("extern fn 参数 %d 类型 '%s' 含泛型参数，FFI 边界禁止泛型外露", i+1, pType),
					stmt.Pos.Line, stmt.Pos.Column, "",
					"extern 对应 C ABI，签名必须为具体类型；请用具体类型或 type 别名替换泛型参数",
				)
			}
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

	// 记录数组字面量长度，供 spend 全集证明使用
	if arrLit, ok := stmt.Value.(*ast.ArrayLiteral); ok {
		sa.arrayLens[stmt.Name] = len(arrLit.Elements)
	}

	// 添加到符号表
	sa.symbolTable.AddSymbol(stmt.Name, stmt.Type, false, "local", stmt.Pos.Line, stmt.Pos.Column)

	// [unused 检查已注释] 声明带 #[unused] 注解 → 豁免 unused 检查
	// if ast.HasAttribute(stmt.Attributes, "unused") {
	// 	if s := sa.symbolTable.GetSymbol(stmt.Name); s != nil {
	// 		s.Unused = true
	// 	}
	// }
}

// isTypeValid 检查类型是否有效
func (sa *SemanticAnalyzer) isTypeValid(typeName string) bool {
	// void(T...)R 签名记法：数据指针或函数指针
	// 数据指针 void() / void(T) → 合法（T 仅类型系统跟踪，运行时 void*）
	// 函数指针 void(T1,T2)R → 递归校验参数与返回类型
	if strings.HasPrefix(typeName, "void(") {
		return sa.isVoidSignatureValid(typeName)
	}

	// 基本类型
	basicTypes := map[string]bool{
		"int":     true,
		"integer": true,
		"i8":      true,
		"i16":     true,
		"i32":     true,
		"i64":     true,
		"int8":    true,
		"int16":   true,
		"int32":   true,
		"int64":   true,
		"u8":      true,
		"u16":     true,
		"u32":     true,
		"u64":     true,
		"uint8":   true,
		"uint16":  true,
		"uint32":  true,
		"uint64":  true,
		"uint":    true,
		"uchar":   true,
		"ushort":  true,
		"ulong":   true,
		"float":   true,
		"f32":     true,
		"single":  true,
		"f64":     true,
		"double":  true,
		"real":    true,
		"bool":    true,
		"boolean": true,
		"char":    true,
		"byte":    true,
		"sbyte":   true,
		"string":  true,
		"str":     true,
		"cstring": true,
		"void":    true,
		"any":     true,
		"long":    true,
		"short":   true,
		"size":    true,
		"ssize":   true,
		"intptr":  true,
		"uintptr": true,
		"File":    true, // std.io 的文件句柄类型（C FILE*）
		"object":  true, // 动态对象类型（Object*）
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

	// 检查是否是固定大小数组类型（如 [16]u8）
	if len(typeName) > 0 && typeName[0] == '[' {
		closeBracket := strings.Index(typeName, "]")
		if closeBracket > 0 {
			innerType := typeName[closeBracket+1:]
			return sa.isTypeValid(innerType)
		}
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

	// 检查是否是第三方库函数签名中出现的类型（如 C 库 typedef：cJSON、sqlite3 等）
	if sa.thirdPartyTypeSet == nil {
		sa.thirdPartyTypeSet = map[string]bool{}
		if sa.stdlibConfig != nil {
			for _, lib := range sa.stdlibConfig.ThirdParty {
				for _, fn := range lib.Functions {
					sa.collectTypeName(sa.thirdPartyTypeSet, fn.Return)
					for _, arg := range fn.Args {
						sa.collectTypeName(sa.thirdPartyTypeSet, arg)
					}
				}
			}
		}
	}
	if sa.thirdPartyTypeSet[typeName] {
		return true
	}

	// 检查是否是泛型类型（如 Box<int>）
	if idx := strings.Index(typeName, "<"); idx > 0 {
		baseType := typeName[:idx]
		return sa.isTypeValid(baseType)
	}

	return false
}

// voidSignatureHasGenericParam 检测类型字符串中是否含疑似泛型参数：
// void(...)R 记法内的独立单字母大写标识符（如 T、E、A、B）。
// 用于 FFI 边界约束：extern 签名必须为具体类型，禁止泛型参数外露到 C ABI。
// 例：void(T) → true；void(i32) → false；void(A,B)R → true；void(sqlite3) → false。
func voidSignatureHasGenericParam(typeName string) bool {
	isWordChar := func(b byte) bool {
		return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
	}
	for i := 0; i < len(typeName); i++ {
		c := typeName[i]
		if c < 'A' || c > 'Z' {
			continue
		}
		// 检查是否为独立单字母大写标识符（前后非单词字符）
		prev := byte(0)
		if i > 0 {
			prev = typeName[i-1]
		}
		next := byte(0)
		if i+1 < len(typeName) {
			next = typeName[i+1]
		}
		if !isWordChar(prev) && !isWordChar(next) {
			return true
		}
	}
	return false
}

// splitTopLevelCommasSem 在顶层（不计嵌套括号/尖括号/方括号）按逗号分割。
// 用于解析 void(T1,T2,...)R 的参数列表，正确处理嵌套签名 void(void(i32)i32, i32)。
func splitTopLevelCommasSem(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '<', '[':
			depth++
		case ')', '>', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// isVoidSignatureValid 校验 void(T...)R 签名记法。
//
//   - 数据指针 void() / void(T)：合法。T 作为幻影类型标记可为任意标识符
//     （典型用法：void(sqlite3) 中 sqlite3 是不透明类型，无需预先定义）。
//   - 函数指针 void(T1,T2)R：递归校验每个参数类型与返回类型 R 均合法。
//
// 判定：')' 后无返回类型 → 数据指针；有 → 函数指针。
func (sa *SemanticAnalyzer) isVoidSignatureValid(typeName string) bool {
	// 定位匹配 void( 的右括号
	depth := 1
	i := 5 // 跳过 "void("
	for i < len(typeName) {
		switch typeName[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				goto foundClose
			}
		}
		i++
	}
	return false // 括号不匹配

foundClose:
	argsStr := typeName[5:i]
	retStr := strings.TrimSpace(typeName[i+1:])

	// 数据指针：')' 后无返回类型 → 合法（不校验幻影 T）
	if retStr == "" {
		return true
	}

	// 函数指针：递归校验参数类型
	if argsStr != "" {
		for _, part := range splitTopLevelCommasSem(argsStr) {
			if !sa.isTypeValid(strings.TrimSpace(part)) {
				return false
			}
		}
	}
	// 递归校验返回类型
	return sa.isTypeValid(retStr)
}

// collectTypeName 提取类型字符串中的基础类型名（剥 const/指针后缀），
// 用于把第三方库函数签名中的 C typedef 类型登记为合法类型
func (sa *SemanticAnalyzer) collectTypeName(set map[string]bool, typeStr string) {
	t := strings.TrimSpace(typeStr)
	if t == "" {
		return
	}
	set[t] = true
	t = strings.TrimPrefix(t, "const ")
	for strings.HasSuffix(t, "*") || strings.HasSuffix(t, "const") {
		t = strings.TrimSuffix(strings.TrimSpace(t), "*")
		t = strings.TrimSuffix(strings.TrimSpace(t), "const")
		t = strings.TrimSpace(t)
		if t == "" {
			break
		}
		set[t] = true
	}
}

// analyzeIfStatement 分析 if 语句
func (sa *SemanticAnalyzer) analyzeIfStatement(stmt *ast.IfStatement) {
	// 检测 if-else if 链中的连续单变量 == 比较, 建议改用 match
	// 只在链的最外层根 if 检测一次, 内层 else-if 跳过 (避免重复警告)
	if !sa.inElseIfChain {
		sa.checkIfChainForMatch(stmt)
	}
	if stmt.Condition != nil {
		sa.analyzeExpression(stmt.Condition)
		// 条件类型检查: 只允许布尔表达式 (bool 变量/true/false 或比较/逻辑运算)
		sa.validateCondition(stmt.Condition)
		// 空指针安全：检测 if x != null 模式，标记 x 为已检查
		sa.markNullCheckedInCondition(stmt.Condition)
	}
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
	// 退出 if body 后，null checked 标记不再有效（简化实现：不清除，因为 else 分支可能也需要）
	for _, elseStmt := range stmt.Else {
		// else-if 嵌套: 作为链的一部分, 跳过独立检测
		if nested, ok := elseStmt.(*ast.IfStatement); ok {
			oldInChain := sa.inElseIfChain
			sa.inElseIfChain = true
			sa.analyzeStatement(nested)
			sa.inElseIfChain = oldInChain
			continue
		}
		sa.analyzeStatement(elseStmt)
	}
}

// checkIfChainForMatch 检测 if-else if 链中的连续单变量 == 比较
// 例如: if (x == a) { ... } else if (x == b) { ... } else if (x == c) { ... }
// 触发 warning, 建议改用 match 表达式
func (sa *SemanticAnalyzer) checkIfChainForMatch(stmt *ast.IfStatement) {
	// 收集链上所有分支: 每个分支的条件是否是 同一变量 == 值
	var chainVars []string
	cur := stmt
	for cur != nil {
		varName := ""
		if bin, ok := cur.Condition.(*ast.BinaryExpression); ok && bin.Operator == "==" {
			if id, ok := bin.Left.(*ast.Identifier); ok {
				varName = id.Name
			} else if id, ok := bin.Right.(*ast.Identifier); ok {
				varName = id.Name
			}
		}
		chainVars = append(chainVars, varName)

		// 寻找下一个 else if (Else 中嵌套的 IfStatement)
		var next *ast.IfStatement
		for _, elseStmt := range cur.Else {
			if nested, ok := elseStmt.(*ast.IfStatement); ok {
				next = nested
				break
			}
		}
		cur = next
	}

	// 至少 2 个分支且所有分支都是同一变量的 == 比较
	if len(chainVars) < 2 {
		return
	}
	first := chainVars[0]
	if first == "" {
		return
	}
	for _, v := range chainVars[1:] {
		if v != first {
			return
		}
	}

	sa.errorCollector.AddSemanticWarning(
		fmt.Sprintf("连续 %d 个分支对同一变量 '%s' 做 == 比较, 建议改用 match 表达式", len(chainVars), first),
		stmt.Pos.Line, stmt.Pos.Column,
		"if_chain_should_use_match",
		fmt.Sprintf("建议改写为 match(%s) { ... } 形式, 更清晰且可穷尽所有情况", first),
	)
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

	// x != null (大写 NULL 兼容)
	if binExpr.Operator == "!=" {
		if ident, ok := binExpr.Left.(*ast.Identifier); ok {
			if right, ok := binExpr.Right.(*ast.Identifier); ok && (right.Name == "null" || right.Name == "NULL") {
				isNullCheck = true
				checkedVar = ident.Name
			}
		}
	}
	// null != x (大写 NULL 兼容)
	if binExpr.Operator == "!=" {
		if left, ok := binExpr.Left.(*ast.Identifier); ok && (left.Name == "null" || left.Name == "NULL") {
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
		// 条件类型检查：只允许布尔表达式
		sa.validateCondition(stmt.Condition)
	}
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
}

// validateCondition 校验 if/while 条件的合法性。
// 规则:
//   - 允许: bool 变量裸名、true/false 字面量、比较表达式(== != < > <= >=)
//   - 比较表达式的左操作数必须是变量/成员/调用等非字面量
//   - 右操作数可以是数字字面量或变量, 但不能是 true/false
//   - 禁止: 非 bool 变量裸名、数字字面量裸名、字面量作比较左值
func (sa *SemanticAnalyzer) validateCondition(cond ast.Expression) {
	if cond == nil {
		return
	}
	switch e := cond.(type) {
	case *ast.Identifier:
		// true / false 字面量直接允许
		if e.Name == "true" || e.Name == "false" {
			return
		}
		// 数字字面量裸名禁止 (如 if (1))
		if _, err := strconv.ParseInt(e.Name, 10, 64); err == nil {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("if/while 条件不能是数字字面量 '%s'", e.Name),
				e.Pos.Line, e.Pos.Column, "condition_type",
				"请使用比较表达式, 如: if (x != 0)、if (x > 0)")
			return
		}
		// bool 变量裸名允许
		t := sa.inferExpressionType(cond)
		if t == "bool" {
			return
		}
		// 其余裸名禁止, 给出改写建议
		suggestion := fmt.Sprintf("请使用比较表达式, 如: if (%s != null)", e.Name)
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("if/while 条件必须是布尔值, 不能直接使用变量 '%s' (类型: %s)", e.Name, t),
			e.Pos.Line, e.Pos.Column, "condition_type",
			suggestion)
	case *ast.BinaryExpression:
		// 比较表达式允许
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			// 左操作数必须是变量/成员/调用等, 不能是字面量
			if isLiteralExpr(e.Left) {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("比较表达式的左侧不能是字面量 (运算符 '%s')", e.Operator),
					e.Pos.Line, e.Pos.Column, "condition_type",
					"请把字面量放到比较运算符右侧, 如: if (x == 5)")
			}
			// 右操作数不能是 true/false（除非左侧是布尔表达式, 如 if (flag == true))
			if isBoolLiteralValue(e.Right) {
				leftType := sa.inferExpressionType(e.Left)
				if leftType != "bool" {
					sa.errorCollector.AddSemanticError(
						"比较表达式的右侧不能使用 true/false, 直接使用布尔变量或比较结果",
						e.Pos.Line, e.Pos.Column, "condition_type",
						"如: if (flag)、if (a > b)")
				}
			}
		default:
			// 算术/赋值等表达式禁止作为条件
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("if/while 条件不能是算术/赋值表达式 (运算符 '%s')", e.Operator),
				e.Pos.Line, e.Pos.Column, "condition_type",
				"请使用比较表达式或布尔变量")
		}
	default:
		// 其他表达式 (函数调用返回非 bool、索引、成员访问等) 需推断类型
		t := sa.inferExpressionType(cond)
		if t == "bool" {
			return
		}
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("if/while 条件必须是布尔值, 当前表达式类型: %s", t),
			e.GetPosition().Line, e.GetPosition().Column, "condition_type",
			"请使用比较表达式或布尔变量")
	}
}

// isLiteralExpr 判断表达式是否为字面量 (数字/字符串/布尔/null)
func isLiteralExpr(expr ast.Expression) bool {
	switch expr.(type) {
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral, *ast.BooleanLiteral:
		return true
	case *ast.Identifier:
		id := expr.(*ast.Identifier)
		return id.Name == "true" || id.Name == "false" || id.Name == "null"
	case *ast.LiteralExpression:
		return true
	default:
		return false
	}
}

// isBoolLiteralValue 判断表达式是否为 true/false 字面量
func isBoolLiteralValue(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.BooleanLiteral:
		return true
	case *ast.Identifier:
		return e.Name == "true" || e.Name == "false"
	case *ast.LiteralExpression:
		return e.Kind == "bool" || e.Kind == "boolean"
	default:
		return false
	}
}

// analyzeForInStatement 分析 range-based for 迭代语句
// 支持两种形式：
//   - for x in range(...) { body }：循环变量类型为 int (i64)
//   - for x in arr { body }：从数组/切片类型推断元素类型
func (sa *SemanticAnalyzer) analyzeForInStatement(stmt *ast.ForInStatement) {
	// 注意：不要对 stmt.Variable 调用 analyzeExpression，因为它是被 for-in 声明的循环变量，
	// 在注册到符号表之前不应被当作普通标识符检查。
	if stmt.Iterable != nil {
		sa.analyzeExpression(stmt.Iterable)
	}
	// 推断循环变量类型
	if stmt.Variable != nil && stmt.Iterable != nil {
		if isRangeCallExpr(stmt.Iterable) {
			// range(...) 产生的值为整数类型
			sa.symbolTable.AddSymbol(stmt.Variable.Name, "int", false, "local", stmt.Pos.Line, stmt.Pos.Column)
		} else {
			iterType := sa.inferExpressionType(stmt.Iterable)
			elemType := inferElementType(iterType)
			if elemType != "" {
				sa.symbolTable.AddSymbol(stmt.Variable.Name, elemType, false, "local", stmt.Pos.Line, stmt.Pos.Column)
			}
		}
	}
	for _, bodyStmt := range stmt.Body {
		sa.analyzeStatement(bodyStmt)
	}
}

// isRangeCallExpr 判断表达式是否为 range(...) 调用
func isRangeCallExpr(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpression)
	if !ok || call == nil {
		return false
	}
	id, ok := call.Function.(*ast.Identifier)
	return ok && id.Name == "range" && len(call.Args) >= 1 && len(call.Args) <= 3
}

// inferElementType 从数组/切片类型字符串中提取元素类型
// [3]i32 → i32, []string → string, []i8 → i8
func inferElementType(typeStr string) string {
	if len(typeStr) > 0 && typeStr[0] == '[' {
		closeBracket := strings.Index(typeStr, "]")
		if closeBracket > 0 {
			return typeStr[closeBracket+1:]
		}
	}
	return ""
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
	case *ast.MemberAccessExpression:
		// 成员访问 (obj.field / obj.method()): 分析 Object, 标记符号为已使用
		if e.Object != nil {
			sa.analyzeExpression(e.Object)
		}
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
	case *ast.ObjectLiteral:
		// 动态对象字面量：检查字段重名，分析字段值
		seen := make(map[string]bool)
		for _, field := range e.Fields {
			if seen[field.Name] {
				sa.errorCollector.AddSemanticError(
					fmt.Sprintf("动态对象字面量中字段 '%s' 重复定义", field.Name),
					field.Pos.Line,
					field.Pos.Column,
					"duplicate_object_field",
					"删除重复的字段或改名",
				)
			}
			seen[field.Name] = true
			if field.Value != nil {
				sa.analyzeExpression(field.Value)
			}
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
	case *ast.TypeCastExpression:
		sa.analyzeTypeCastExpression(e)
	}
}

// analyzeIdentifier 分析标识符
func (sa *SemanticAnalyzer) analyzeIdentifier(expr *ast.Identifier) {
	if expr == nil {
		return
	}
	if expr.Name == "null" || expr.Name == "NULL" || expr.Name == "true" || expr.Name == "false" {
		return
	}

	// range 是内置迭代器构造，仅在 for-in 上下文中作为 range(...) 使用
	if expr.Name == "range" {
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
	// 类字段符号只能通过 self.field 显式访问（强制 self 策略）。
	// 字段在 analyzeClassStatement 中以 scope="field_<类名>" 注册到全局符号表，
	// 裸名引用会解析到它，此处拦截并提示改用 self.field。
	if symbol != nil && strings.HasPrefix(symbol.Scope, "field_") {
		className := strings.TrimPrefix(symbol.Scope, "field_")
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("未定义的变量: '%s'", expr.Name),
			expr.Pos.Line,
			expr.Pos.Column,
			"undefined_variable",
			fmt.Sprintf("类 '%s' 的字段必须通过 self.%s 显式访问", className, expr.Name),
		)
		return
	}
	if symbol == nil {
		// 本地 import 的 pub 函数（跨文件调用）不算未定义
		if sa.localImportFuncs[expr.Name] {
			return
		}
		// 本地 import / export 的变量（跨文件变量引用）不算未定义
		if sa.localPubVars[expr.Name] {
			return
		}
		// 函数存在于被 import 模块但未导出(pub): 报"未导出"错误
		if sa.localModuleFuncs[expr.Name] {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("函数 '%s' 存在于被导入的模块, 但未通过 pub 导出", expr.Name),
				expr.Pos.Line,
				expr.Pos.Column,
				"not_exported",
				fmt.Sprintf("在 '%s' 的定义前添加 pub 修饰符: pub fn %s(...)", expr.Name, expr.Name),
			)
			return
		}
		// 类方法/构造函数作用域内：若该名字恰好是当前类的字段，
		// 提示必须通过 self.field 显式访问（强制 self 策略）。
		if hint := sa.classFieldAccessHint(expr.Name); hint != "" {
			sa.errorCollector.AddSemanticError(
				fmt.Sprintf("未定义的变量: '%s'", expr.Name),
				expr.Pos.Line,
				expr.Pos.Column,
				"undefined_variable",
				hint,
			)
			return
		}
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("未定义的变量: '%s'", expr.Name),
			expr.Pos.Line,
			expr.Pos.Column,
			"undefined_variable",
			"请确保变量已声明后再使用",
		)
		return
	}
	// 标记符号为已使用（unused 检查已注释）
	// symbol.Referenced = true
}

// classFieldAccessHint 在类方法/构造函数作用域内，若 name 是当前类的字段，
// 返回提示用户改用 self.name 的字符串；否则返回空串。
func (sa *SemanticAnalyzer) classFieldAccessHint(name string) string {
	if sa.program == nil {
		return ""
	}
	scopeName := sa.symbolTable.GetScopeName()
	className := ""
	if strings.HasPrefix(scopeName, "method_") {
		rest := scopeName[len("method_"):]
		if idx := strings.LastIndex(rest, "_"); idx > 0 {
			className = rest[:idx]
		}
	} else if strings.HasPrefix(scopeName, "constructor_") {
		className = scopeName[len("constructor_"):]
	}
	if className == "" {
		return ""
	}
	classStmt := sa.program.FindClass(className)
	if classStmt == nil {
		return ""
	}
	for _, f := range classStmt.Fields {
		if f != nil && f.Name == name {
			return fmt.Sprintf("类 '%s' 的字段必须通过 self.%s 显式访问", className, name)
		}
	}
	return ""
}

// checkUnusedSymbols 检查当前函数/方法/构造函数作用域中未被引用的局部变量和参数。
// 产生 warning 级诊断；声明带 #[unused] 注解的符号豁免。
// [已注释] unused 检查暂未启用
/*
func (sa *SemanticAnalyzer) checkUnusedSymbols() {
	if sa.symbolTable == nil {
		return
	}
	for _, sym := range sa.symbolTable.Symbols() {
		// self 是隐式指针，不检查
		if sym.Name == "self" {
			continue
		}
		// 泛型类型参数/类型符号不检查
		if sym.Type == "type" {
			continue
		}
		// 类字段通过 self.field 访问，不在此检查（裸字段名引用会正常标记）
		if strings.HasPrefix(sym.Scope, "class_field:") {
			continue
		}
		// 全局/外部符号不在函数作用域检查范围内
		if sym.Scope == "global" || sym.Scope == "extern" || sym.Scope == "extern_func" {
			continue
		}
		if sym.Referenced || sym.Unused {
			continue
		}
		sa.errorCollector.AddSemanticWarning(
			fmt.Sprintf("未使用的变量: '%s'", sym.Name),
			sym.Line, sym.Column, "unused_variable",
			"如果该声明有意保留未使用，请添加 #[unused] 注解",
		)
	}
}
*/

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
			case "==", "!=":
				isEquality := true
				isNullCompare := isEquality && (leftType == "null" || rightType == "null")
				isStrCompare := isEquality && isStringLikeType(leftType) && isStringLikeType(rightType)
				isPtrCompare := isEquality && isPointerType(leftType) && isPointerType(rightType)
				if leftType != rightType && !(isNumericType(leftType) && isNumericType(rightType)) && !isNullCompare && !isStrCompare && !isPtrCompare {
					sa.errorCollector.AddSemanticError(
						fmt.Sprintf("比较运算符 '%s' 不能用于类型 '%s' 和 '%s'", expr.Operator, leftType, rightType),
						expr.Pos.Line,
						expr.Pos.Column,
						"type_mismatch",
						"比较运算符两侧的类型必须兼容",
					)
				}
			case "<", ">", "<=", ">=":
				// 关系比较只允许数值类型
				if !(isNumericType(leftType) && isNumericType(rightType)) {
					sa.errorCollector.AddSemanticError(
						fmt.Sprintf("关系运算符 '%s' 只能用于数值类型, 当前 '%s' 和 '%s'", expr.Operator, leftType, rightType),
						expr.Pos.Line,
						expr.Pos.Column,
						"type_mismatch",
						"关系比较仅支持数值; 字符串/指针请用 ==/!= 比较",
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

// analyzeTypeCastExpression 分析类型转换表达式 as<T>(expr)
// 规则: 禁止布尔与其他类型互转 (bool 只能来自布尔字面量/变量/比较表达式)
func (sa *SemanticAnalyzer) analyzeTypeCastExpression(expr *ast.TypeCastExpression) {
	if expr == nil {
		return
	}
	sa.analyzeExpression(expr.Expression)
	targetType := expr.TargetType
	srcType := sa.inferExpressionType(expr.Expression)
	if srcType == "" {
		return
	}
	isTargetBool := targetType == "bool" || targetType == "boolean"
	isSrcBool := srcType == "bool"
	if isTargetBool != isSrcBool {
		// 布尔与非布尔互转一律禁止
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("禁止布尔与其他类型互相转换: as<%s>(%s 类型)", targetType, srcType),
			expr.Pos.Line, expr.Pos.Column, "bool_cast",
			"布尔值只能来自布尔字面量、布尔变量或比较表达式; 请使用 if (x != null)、if (x > 0) 等比较形式",
		)
	}
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
	// 解析模块路径：std.module.function / freestanding.module.function -> module
	var moduleName string
	isFreeModuleCall := false

	// 检查是否是 std.module.function / freestanding.module.function 形式
	if nestedMember, ok := memberAccess.Object.(*ast.MemberAccessExpression); ok {
		if innerIdent, ok := nestedMember.Object.(*ast.Identifier); ok {
			if innerIdent.Name == "std" {
				moduleName = nestedMember.Member
			} else if innerIdent.Name == "freestanding" {
				moduleName = nestedMember.Member
				isFreeModuleCall = true
			}
		}
	}

	// 如果没有找到模块名，不是标准库调用
	if moduleName == "" {
		return
	}

	// 检查模块是否已导入（使用 importedModules 而不是符号表）
	namespace := "std."
	if isFreeModuleCall {
		namespace = "freestanding."
	}
	stdlibKey := namespace + moduleName
	if !sa.importedModules[moduleName] && !sa.importedModules[stdlibKey] {
		sa.errorCollector.AddSemanticError(
			fmt.Sprintf("%s模块 '%s' 未导入，请添加 'import %s%s;' 语句", namespace, moduleName, namespace, moduleName),
			pos.Line,
			pos.Column,
			"missing_import",
			fmt.Sprintf("在文件顶部添加: import %s%s;", namespace, moduleName),
		)
	}
}

// analyzeIndexExpression 分析索引表达式
func (sa *SemanticAnalyzer) analyzeIndexExpression(expr *ast.IndexExpression) {
	if expr == nil {
		return
	}
	sa.checkNullableDereference(expr.Object, expr.Pos.Line, expr.Pos.Column)
	sa.analyzeExpression(expr.Object)
	sa.analyzeExpression(expr.Index)

	// 编译期常量索引越界检查
	if lit, ok := expr.Index.(*ast.IntegerLiteral); ok {
		objType := sa.inferExpressionType(expr.Object)
		idx := lit.Value
		// 固定大小数组 [N]T：检查索引是否 >= N
		if len(objType) > 0 && objType[0] == '[' {
			closeBracket := strings.Index(objType, "]")
			if closeBracket > 0 {
				sizeStr := objType[1:closeBracket]
				if arraySize, err := strconv.ParseUint(sizeStr, 10, 64); err == nil {
					if uint64(idx) >= arraySize {
						sa.error(fmt.Sprintf("index %d out of bounds for array of size %d", idx, arraySize), expr.Pos.Line, expr.Pos.Column)
					}
				}
			}
		}
	}
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

// memberObjectClassName 推断成员访问对象表达式的类名。
// 支持 self（method_<类名>_<方法名>/constructor_<类名> 作用域或符号表）、
// 已声明类类型变量、嵌套成员链 (a.b.c, a/b 均为类类型)。
func (sa *SemanticAnalyzer) memberObjectClassName(obj ast.Expression) string {
	if sa.program == nil {
		return ""
	}
	switch e := obj.(type) {
	case *ast.Identifier:
		if e.Name == "self" {
			if sym := sa.symbolTable.GetSymbol("self"); sym != nil && sym.Type != "" {
				name := strings.TrimSuffix(sym.Type, "*")
				if sa.program.FindClass(name) != nil {
					return name
				}
			}
			// 兜底：从当前作用域名解析（method_<类名>_<方法名> / constructor_<类名>）
			scopeName := sa.symbolTable.GetScopeName()
			if strings.HasPrefix(scopeName, "method_") {
				rest := strings.TrimPrefix(scopeName, "method_")
				if idx := strings.LastIndex(rest, "_"); idx > 0 {
					return rest[:idx]
				}
			} else if strings.HasPrefix(scopeName, "constructor_") {
				return strings.TrimPrefix(scopeName, "constructor_")
			}
			return ""
		}
		if sym := sa.symbolTable.GetSymbol(e.Name); sym != nil && sym.Type != "" {
			name := strings.TrimSuffix(sym.Type, "*")
			if sa.program.FindClass(name) != nil {
				return name
			}
		}
	case *ast.MemberAccessExpression:
		fieldType := sa.classFieldType(sa.memberObjectClassName(e.Object), e.Member)
		if fieldType != "" {
			name := strings.TrimSuffix(fieldType, "*")
			if sa.program.FindClass(name) != nil {
				return name
			}
		}
	}
	return ""
}

// classFieldType 在已定义类中查找字段的 Kaula 类型名；找不到时返回空串
func (sa *SemanticAnalyzer) classFieldType(className, fieldName string) string {
	if className == "" || sa.program == nil {
		return ""
	}
	classStmt := sa.program.FindClass(className)
	if classStmt == nil {
		return ""
	}
	for _, f := range classStmt.Fields {
		if f != nil && f.Name == fieldName {
			return f.Type
		}
	}
	return ""
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
		if e.Name == "null" || e.Name == "NULL" {
			return "null"
		}
		symbol := sa.symbolTable.GetSymbol(e.Name)
		if symbol != nil {
			return symbol.Type
		}
		return ""
	case *ast.ParenExpression:
		return sa.inferExpressionType(e.Inner)
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
		// 比较/逻辑运算符的结果类型是 bool（不是数值）
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "bool"
		}
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
			if retType, ok := sa.funcReturnTypes[ident.Name]; ok && retType != "" {
				return retType
			}
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
			// class 方法调用 (v.method()): Object 类型是类名, 查 "类名.方法名"
			if objIdent, ok := member.Object.(*ast.Identifier); ok {
				if sym := sa.symbolTable.GetSymbol(objIdent.Name); sym != nil && sym.Type != "" {
					if retType, ok := sa.funcReturnTypes[sym.Type+"."+member.Member]; ok && retType != "" {
						return retType
					}
				}
			}
		}
		return ""
	case *ast.ArrayLiteral:
		if len(e.Elements) > 0 {
			elemType := sa.inferExpressionType(e.Elements[0])
			// C 端数组字面量固定生成 int[]（见 exprgen），小整数类型统一提升为 int
			if elemType == "u8" || elemType == "u16" || elemType == "u32" {
				elemType = "int"
			}
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
	case *ast.StructLiteral:
		return "struct"
	case *ast.ObjectLiteral:
		// 动态对象字面量：类型为 object
		return "object"
	case *ast.MemberAccessExpression:
		// 动态对象成员访问（obj.field）的类型是 object
		if sa.inferExpressionType(e.Object) == "object" {
			return "object"
		}
		// 类字段访问（self.field / obj.field / 嵌套成员链）: 从类定义查字段类型
		if className := sa.memberObjectClassName(e.Object); className != "" {
			if fieldType := sa.classFieldType(className, e.Member); fieldType != "" {
				return fieldType
			}
		}
		return ""
	case *ast.IndexExpression:
		// 动态对象下标访问（obj["key"]）的类型是 object
		if sa.inferExpressionType(e.Object) == "object" {
			return "object"
		}
		return ""
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

// isStringLikeType 检查类型是否为字符串类型 (string/cstr/char*)
func isStringLikeType(typeName string) bool {
	return typeName == "string" || typeName == "cstring" || typeName == "cstr" ||
		typeName == "char*" || strings.HasSuffix(typeName, "char*")
}

// isPointerType 检查类型是否为指针类型 (以 * 结尾, 但不含 char* 之类已归为字符串的)
func isPointerType(typeName string) bool {
	if typeName == "" {
		return false
	}
	if isStringLikeType(typeName) {
		return false
	}
	// 排除函数类型 void(...)R 记法
	if strings.HasPrefix(typeName, "void(") {
		return false
	}
	return strings.HasSuffix(typeName, "*") || typeName == "void()" ||
		strings.HasPrefix(typeName, "const ")
}

func (sa *SemanticAnalyzer) error(msg string, line, column int) {
	suggestion := errors.GenerateSuggestion(msg)
	context, sourceLine, lineNumStr := errors.ExtractSourceContext(sa.source, line, column)
	err := &errors.Error{
		Type:          errors.ErrorSemantic,
		Message:       msg,
		Line:          line,
		Column:        column,
		File:          "",
		Suggestion:    suggestion,
		SourceContext: context,
		SourceLine:    sourceLine,
		LineNumberStr: lineNumStr,
		Highlight:     errors.BuildHighlight(sa.source, line, column, 0, "", errors.ErrorSemantic, msg),
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
		case *ast.ForInStatement:
			forInStmt := stmt.(*ast.ForInStatement)
			if sa.hasSORPrimitives(forInStmt.Body) {
				return true
			}
		}
	}
	return false
}
