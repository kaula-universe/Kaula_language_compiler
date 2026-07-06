package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/config"
	"kaula-compiler/internal/core"
	"kaula-compiler/internal/stdlib"
	"kaula-compiler/internal/symbol"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// GenericInstanceCache 泛型实例缓存
type GenericInstanceCache struct {
	OriginalName   string
	TypeArguments  []string
	GeneratedCode  string
	InstantiatedAt int
}

type CodeGenerator struct {
	output          string
	indent          int
	program         *ast.Program
	templateManager *TemplateManager
	config          *config.Config
	pluginManager   *PluginManager
	stdlibConfig    *stdlib.StdlibConfig
	treeManager     *core.TreeManager
	prefixManager   *core.PrefixManager
	symbolTable     *symbol.SymbolTable
	currentScope    *symbol.SymbolTable
	errors          []string
	usedModules     []string

	typeGenerator       *TypeGenerator
	functionGenerator   *FunctionGenerator
	expressionGenerator *ExpressionGenerator
	statementGenerator  *StatementGenerator

	usedThirdPartyLibs map[string]bool
	localImportFuncs   map[string]bool

	genericCache       map[string]*GenericInstanceCache
	genericInstantiated map[string]bool
	currentFuncTypeParams []*ast.TypeParameter

	currentFunctionName string
	currentFunctionReturnType string
	callStack           map[string]bool

	sorAdapter *SORCodeGenAdapter

	kmmScopeDepth int

	sourceMap *SourceMap
	sourceFile string
}

func (cg *CodeGenerator) error(message string) {
	cg.errors = append(cg.errors, message)
}

func (cg *CodeGenerator) Errors() []string {
	return cg.errors
}

func (cg *CodeGenerator) HasErrors() bool {
	return len(cg.errors) > 0
}

func (cg *CodeGenerator) SetStdlibConfig(cfg *stdlib.StdlibConfig) {
	cg.stdlibConfig = cfg
}

// SetSORResult 设置 SOR 分析结果，供代码生成阶段使用
func (cg *CodeGenerator) SetSORResult(result map[string]interface{}) {
	cg.sorAdapter = NewSORCodeGenAdapter(result)
}

// GetSORAdapter 获取 SOR CodeGen 适配器
func (cg *CodeGenerator) GetSORAdapter() *SORCodeGenAdapter {
	return cg.sorAdapter
}

// IsInKMMScope 当前是否在 KMM 作用域内
func (cg *CodeGenerator) IsInKMMScope() bool {
	return cg.kmmScopeDepth > 0
}

// GetSourceMap 获取源代码映射
func (cg *CodeGenerator) GetSourceMap() *SourceMap {
	return cg.sourceMap
}

// SetSourceFile 设置源文件名
func (cg *CodeGenerator) SetSourceFile(filename string) {
	cg.sourceFile = filename
}

// EnterKMMScope 进入 KMM 作用域
func (cg *CodeGenerator) EnterKMMScope() {
	cg.kmmScopeDepth++
}

// ExitKMMScope 退出 KMM 作用域
func (cg *CodeGenerator) ExitKMMScope() {
	if cg.kmmScopeDepth > 0 {
		cg.kmmScopeDepth--
	}
}

func (cg *CodeGenerator) GetStdlibConfig() *stdlib.StdlibConfig {
	return cg.stdlibConfig
}

func (cg *CodeGenerator) IsGenericInstantiated(name string) bool {
	return cg.genericInstantiated[name]
}

func (cg *CodeGenerator) MarkGenericInstantiated(name string) {
	if cg.genericInstantiated == nil {
		cg.genericInstantiated = make(map[string]bool)
	}
	cg.genericInstantiated[name] = true
}

func (cg *CodeGenerator) GetUsedModules() []string {
	return cg.usedModules
}

// SetLocalImportFuncs 注册本地导入的 pub 函数名
func (cg *CodeGenerator) SetLocalImportFuncs(funcs map[string]bool) {
	cg.localImportFuncs = funcs
}

func NewCodeGenerator(cfg *config.Config) *CodeGenerator {
	tm := NewTemplateManager()
	templatePath := filepath.Join(cfg.TemplatePath, "main.c.tmpl")
	tm.LoadTemplate("main", templatePath)

	pm := NewPluginManager()

	stdlibPath := cfg.StdlibPath
	if stdlibPath == "" {
		stdlibPath = "stdlib.json"
		if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
			stdlibPath = "kaula-compiler/stdlib.json"
			if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
				stdlibPath = "../stdlib.json"
			}
		}
	}
	stdlibConfig, err := stdlib.LoadStdlibConfig(stdlibPath)
	if err != nil {
		fmt.Printf("Warning: Failed to load stdlib.json from %s: %v\n", stdlibPath, err)
	} else {
		fmt.Printf("Loaded stdlib.json from %s, modules: %d\n", stdlibPath, len(stdlibConfig.Modules))
	}

	treeManager := core.NewTreeManager()
	prefixManager := core.NewPrefixManager()

	symbolTable := symbol.NewSymbolTable(nil, "global")

	cg := &CodeGenerator{
		output:          "",
		indent:          0,
		templateManager: tm,
		config:          cfg,
		pluginManager:   pm,
		stdlibConfig:    stdlibConfig,
		treeManager:     treeManager,
		prefixManager:   prefixManager,
		symbolTable:     symbolTable,
		currentScope:    symbolTable,
		errors:          []string{},
		usedThirdPartyLibs: make(map[string]bool),
		localImportFuncs:   make(map[string]bool),
		genericCache:       make(map[string]*GenericInstanceCache),
		genericInstantiated: make(map[string]bool),
		sourceMap:          NewSourceMap("", ""),
	}
	
	cg.typeGenerator = NewTypeGenerator(cg)
	cg.functionGenerator = NewFunctionGenerator(cg)
	cg.expressionGenerator = NewExpressionGenerator(cg)
	cg.statementGenerator = NewStatementGenerator(cg)
	
	return cg
}

func (cg *CodeGenerator) Generate(program *ast.Program) string {
	cg.program = program
	cg.usedThirdPartyLibs = make(map[string]bool)

	type rawEntry struct {
		section   string
		relLine   int
		srcLine   int
		srcCol    int
		kind      string
		symbol    string
	}
	var rawEntries []rawEntry
	var typeLine, globalLine, funcLine, mainLine int

	addEntry := func(section string, srcLine, srcCol int, kind, symbol string, lineCount int) {
		if srcLine > 0 {
			var baseLine *int
			switch section {
			case "type":
				baseLine = &typeLine
			case "global":
				baseLine = &globalLine
			case "func":
				baseLine = &funcLine
			case "main":
				baseLine = &mainLine
			}
			if baseLine != nil {
				rawEntries = append(rawEntries, rawEntry{
					section: section,
					relLine: *baseLine + 1,
					srcLine: srcLine,
					srcCol:  srcCol,
					kind:    kind,
					symbol:  symbol,
				})
			}
			*baseLine += lineCount
		} else {
			var baseLine *int
			switch section {
			case "type":
				baseLine = &typeLine
			case "global":
				baseLine = &globalLine
			case "func":
				baseLine = &funcLine
			case "main":
				baseLine = &mainLine
			}
			if baseLine != nil {
				*baseLine += lineCount
			}
		}
	}

	var typeCode strings.Builder
	var globalVars strings.Builder
	var functionCode strings.Builder
	var mainCode strings.Builder
	typeCode.Grow(4096)
	globalVars.Grow(1024)
	functionCode.Grow(8192)
	mainCode.Grow(4096)

	hasMain := false

	importedModules := make(map[string]bool)

	for _, stmt := range program.Statements {
		if stmt == nil {
			continue
		}
		if importStmt, ok := stmt.(*ast.ImportStatement); ok {
			importedModules[importStmt.Module] = true
			continue
		}
		if _, ok := stmt.(*ast.PackageStatement); ok {
			continue
		}
		if _, ok := stmt.(*ast.ExportStatement); ok {
			continue
		}

		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == "main" {
				hasMain = true
			}
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("func", fnStmt.Pos.Line, fnStmt.Pos.Column, "function", fnStmt.Name, lines)
			functionCode.WriteString(code)
		} else if classStmt, ok := stmt.(*ast.ClassStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", classStmt.Pos.Line, classStmt.Pos.Column, "class", classStmt.Name, lines)
			typeCode.WriteString(code)
		} else if ifaceStmt, ok := stmt.(*ast.InterfaceStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", ifaceStmt.Pos.Line, ifaceStmt.Pos.Column, "interface", ifaceStmt.Name, lines)
			typeCode.WriteString(code)
		} else if structStmt, ok := stmt.(*ast.StructStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", structStmt.Pos.Line, structStmt.Pos.Column, "struct", structStmt.Name, lines)
			typeCode.WriteString(code)
		} else if typeStmt, ok := stmt.(*ast.TypeAliasStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", typeStmt.Pos.Line, typeStmt.Pos.Column, "type", typeStmt.Name, lines)
			typeCode.WriteString(code)
		} else if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
			cType := cg.typeGenerator.convertType(varDecl.Type, varDecl.Nullable)
			initValue := cg.generateExpression(varDecl.Value)
			if varDecl.IsAuto {
				cType = "auto"
			}
			code := fmt.Sprintf("%s %s = %s;\n", cType, varDecl.Name, initValue)
			lines := strings.Count(code, "\n")
			addEntry("global", varDecl.Pos.Line, varDecl.Pos.Column, "variable", varDecl.Name, lines)
			globalVars.WriteString(code)
		} else {
			code := cg.indentString() + cg.generateStatement(stmt)
			lines := strings.Count(code, "\n")
			if pos := getStmtPos(stmt); pos != nil {
				addEntry("main", pos.Line, pos.Column, "statement", "", lines)
			} else {
				addEntry("main", 0, 0, "statement", "", lines)
			}
			mainCode.WriteString(code)
		}
	}

	cg.usedModules = make([]string, 0, len(importedModules))
	for moduleName := range importedModules {
		cg.usedModules = append(cg.usedModules, moduleName)
	}

	var allIncludes strings.Builder
	allIncludes.Grow(2048)
	allIncludes.WriteString("#include <stdint.h>\n#include <stdbool.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"kaula.h\"\n")

	if cg.stdlibConfig != nil {
		for moduleName := range importedModules {
			module, ok := cg.stdlibConfig.Modules[moduleName]
			if ok {
				if module.Header != "" {
					header := module.Header
					if len(header) >= 4 && header[0] == 's' && header[1] == 't' && header[2] == 'd' && header[3] == '/' {
						header = header[4:]
					}
					allIncludes.WriteString("#include \"")
					allIncludes.WriteString(header)
					allIncludes.WriteString("\"\n")
				}
			} else {
				for _, lib := range cg.stdlibConfig.ThirdParty {
					if lib.Name == moduleName {
						if lib.Type == "single_header" && lib.ImplementMacro != "" {
							allIncludes.WriteString("#define ")
							allIncludes.WriteString(lib.ImplementMacro)
							allIncludes.WriteByte('\n')
						}
						for _, header := range lib.Headers {
							allIncludes.WriteString("#include ")
							allIncludes.WriteString(header)
							allIncludes.WriteByte('\n')
						}
						break
					}
				}
			}
		}
	}

	var forwardDecls strings.Builder
	forwardDecls.Grow(1024)
	for _, stmt := range program.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok && fnStmt.IsPublic && fnStmt.Name != "main" {
			returnType := cg.typeGenerator.convertType(fnStmt.ReturnType, false)
			if returnType == "" {
				returnType = "void"
			}
			forwardDecls.WriteString(returnType)
			forwardDecls.WriteByte(' ')
			forwardDecls.WriteString(fnStmt.Name)
			forwardDecls.WriteByte('(')
			for i, pType := range fnStmt.ParamTypes {
				if i > 0 {
					forwardDecls.WriteString(", ")
				}
				cType := cg.typeGenerator.convertType(pType, false)
				if cType == "" {
					cType = "void*"
				}
				forwardDecls.WriteString(cType)
				forwardDecls.WriteByte(' ')
				forwardDecls.WriteString(fnStmt.Params[i])
			}
			forwardDecls.WriteString(");\n")
		}
	}

	cacheDir := "cache"
	if err := os.MkdirAll(cacheDir, 0755); err == nil {
		os.WriteFile(filepath.Join(cacheDir, "all_includes.txt"), []byte(allIncludes.String()), 0644)
	}

	var result string
	var typeOffset, globalOffset, funcOffset, mainOffset int

	if !hasMain {
		template, ok := cg.templateManager.GetTemplate("main")
		if !ok {
			var resultBuilder strings.Builder
			resultBuilder.Grow(allIncludes.Len() + forwardDecls.Len() + typeCode.Len() + functionCode.Len() + mainCode.Len() + 256)
			resultBuilder.WriteString(allIncludes.String())
			resultBuilder.WriteString("\n\n")
			resultBuilder.WriteString(forwardDecls.String())
			resultBuilder.WriteByte('\n')

			typeOffset = strings.Count(allIncludes.String(), "\n") + 3 + strings.Count(forwardDecls.String(), "\n") + 1

			resultBuilder.WriteString(typeCode.String())
			resultBuilder.WriteByte('\n')

			globalOffset = typeOffset + strings.Count(typeCode.String(), "\n") + 1

			funcOffset = globalOffset + strings.Count(globalVars.String(), "\n") + 1
			resultBuilder.WriteString(functionCode.String())

			mainHeader := "\n\nint main() {\n    "
			mainOffset = funcOffset + strings.Count(functionCode.String(), "\n") + strings.Count(mainHeader, "\n")
			resultBuilder.WriteString(mainHeader)
			resultBuilder.WriteString(mainCode.String())
			resultBuilder.WriteString("\n    return 0;\n}\n")
			result = resultBuilder.String()
		} else {
			result = template
			result = strings.ReplaceAll(result, "{{includes}}", allIncludes.String())
			result = strings.ReplaceAll(result, "{{forward_decls}}", forwardDecls.String())
			result = strings.ReplaceAll(result, "{{global_vars}}", globalVars.String())
			result = strings.ReplaceAll(result, "{{type_code}}", typeCode.String())
			result = strings.ReplaceAll(result, "{{function_code}}", functionCode.String())
			result = strings.ReplaceAll(result, "{{main_code}}", mainCode.String())
			result = strings.ReplaceAll(result, "{{code}}", "")

			idxIncludes := strings.Index(result, allIncludes.String())
			idxForward := strings.Index(result, forwardDecls.String())
			idxType := strings.Index(result, typeCode.String())
			idxGlobal := strings.Index(result, globalVars.String())
			idxFunc := strings.Index(result, functionCode.String())
			idxMain := strings.Index(result, mainCode.String())

			typeOffset = strings.Count(result[:idxType], "\n") + 1
			globalOffset = strings.Count(result[:idxGlobal], "\n") + 1
			funcOffset = strings.Count(result[:idxFunc], "\n") + 1
			mainOffset = strings.Count(result[:idxMain], "\n") + 1
			_ = idxIncludes
			_ = idxForward
		}
	} else {
		var resultBuilder strings.Builder
		resultBuilder.Grow(allIncludes.Len() + forwardDecls.Len() + globalVars.Len() + typeCode.Len() + functionCode.Len() + 16)
		resultBuilder.WriteString(allIncludes.String())
		resultBuilder.WriteString("\n")
		resultBuilder.WriteString(forwardDecls.String())
		resultBuilder.WriteString("\n")

		globalOffset = strings.Count(allIncludes.String(), "\n") + 2 + strings.Count(forwardDecls.String(), "\n") + 1
		resultBuilder.WriteString(globalVars.String())
		resultBuilder.WriteString("\n")

		typeOffset = globalOffset + strings.Count(globalVars.String(), "\n") + 1
		resultBuilder.WriteString(typeCode.String())

		funcOffset = typeOffset + strings.Count(typeCode.String(), "\n")
		resultBuilder.WriteString(functionCode.String())
		result = resultBuilder.String()
	}

	cg.sourceMap = NewSourceMap(cg.sourceFile, "")
	for _, e := range rawEntries {
		var genLine int
		switch e.section {
		case "type":
			genLine = typeOffset + e.relLine - 1
		case "global":
			genLine = globalOffset + e.relLine - 1
		case "func":
			genLine = funcOffset + e.relLine - 1
		case "main":
			genLine = mainOffset + e.relLine - 1
		}
		if genLine > 0 {
			cg.sourceMap.AddEntry(genLine, cg.sourceFile, e.srcLine, e.srcCol, e.kind, e.symbol)
		}
	}

	return result
}

func (cg *CodeGenerator) generateStatement(stmt ast.Statement) string {
	return cg.statementGenerator.GenerateStatement(stmt)
}

func (cg *CodeGenerator) generateExpression(expr ast.Expression) string {
	return cg.expressionGenerator.GenerateExpression(expr)
}

var indentCache = []string{
	"",
	"    ",
	"        ",
	"            ",
	"                ",
	"                    ",
	"                        ",
	"                            ",
	"                                ",
	"                                    ",
}

func (cg *CodeGenerator) indentString() string {
	if cg.indent < len(indentCache) {
		return indentCache[cg.indent]
	}
	// 超出缓存范围，动态生成
	indent := ""
	for i := 0; i < cg.indent; i++ {
		indent += "    "
	}
	return indent
}

// RegisterPlugin 注册插件
func (cg *CodeGenerator) RegisterPlugin(plugin Plugin) {
	cg.pluginManager.RegisterPlugin(plugin)
}

// EnterScope 进入一个新的作用域
// 如果 SOR 适配器启用，同时注册作用域 ID 映射
func (cg *CodeGenerator) EnterScope(scopeName string) {
	newScope := symbol.NewSymbolTable(cg.currentScope, scopeName)
	cg.currentScope = newScope
	// 注册 SOR 作用域 ID 映射
	if cg.sorAdapter != nil && cg.sorAdapter.IsActive {
		cg.sorAdapter.RegisterScope(scopeName)
	}
}

// ExitScope 退出当前作用域
func (cg *CodeGenerator) ExitScope() {
	if cg.currentScope != cg.symbolTable {
		cg.currentScope = cg.currentScope.GetParent()
	}
}

// GetCurrentScope 获取当前作用域
func (cg *CodeGenerator) GetCurrentScope() *symbol.SymbolTable {
	return cg.currentScope
}

// AddSymbol 添加一个符号到当前作用域
func (cg *CodeGenerator) AddSymbol(name, symbolType string, nullable bool, scope string, line, column int) {
	cg.currentScope.AddSymbol(name, symbolType, nullable, scope, line, column)
}

// GetSymbol 获取一个符号
func (cg *CodeGenerator) GetSymbol(name string) *symbol.Symbol {
	return cg.currentScope.GetSymbol(name)
}

// HasSymbol 检查是否存在符号
func (cg *CodeGenerator) HasSymbol(name string) bool {
	return cg.currentScope.HasSymbol(name)
}

// GetLocalSymbol 获取当前作用域中的符号
func (cg *CodeGenerator) GetLocalSymbol(name string) *symbol.Symbol {
	return cg.currentScope.GetLocalSymbol(name)
}

// HasLocalSymbol 检查当前作用域是否存在符号
func (cg *CodeGenerator) HasLocalSymbol(name string) bool {
	return cg.currentScope.HasLocalSymbol(name)
}

// InstantiateGeneric 实例化泛型函数
func (cg *CodeGenerator) InstantiateGeneric(funcName string, typeArgs []string, line int) (string, error) {
	// 生成缓存键
	cacheKey := funcName + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			cacheKey += ","
		}
		cacheKey += arg
	}
	cacheKey += ">"
	
	// 检查缓存
	if cached, ok := cg.genericCache[cacheKey]; ok {
		return cached.GeneratedCode, nil
	}
	
	// 生成实例化后的函数名: kaula_max_int64 (添加 kaula_ 前缀避免与 C 宏冲突)
	instName := "kaula_" + funcName + "_"
	for i, arg := range typeArgs {
		if i > 0 {
			instName += "_"
		}
		// 替换类型参数中的特殊字符，避免冲突
		for _, ch := range arg {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
				instName += string(ch)
			} else {
				instName += fmt.Sprintf("_%d_", ch)
			}
		}
	}
	
	if cg.genericInstantiated[instName] {
		return "", nil // 已经实例化过
	}
	
	// 获取原始函数
	program := cg.getProgram()
	if program == nil {
		return "", fmt.Errorf("cannot find program for generic instantiation")
	}
	
	fnStmt := program.FindFunction(funcName)
	if fnStmt == nil || !fnStmt.IsGeneric() {
		return "", fmt.Errorf("function %s is not generic", funcName)
	}
	
	// 创建实例化后的函数（复制并替换类型参数）
	instFunc := cg.instantiateGenericFunction(fnStmt, typeArgs, instName)
	
	// 生成代码
	code := cg.functionGenerator.GenerateFunctionStatement(instFunc)
	
	// 添加到缓存
	cg.genericCache[cacheKey] = &GenericInstanceCache{
		OriginalName:   funcName,
		TypeArguments:  typeArgs,
		GeneratedCode:  code,
		InstantiatedAt: line,
	}
	cg.genericInstantiated[instName] = true
	
	return code, nil
}

// instantiateGenericFunction 创建泛型函数的实例化版本
func (cg *CodeGenerator) instantiateGenericFunction(fnStmt *ast.FunctionStatement, typeArgs []string, instName string) *ast.FunctionStatement {
	// 创建类型参数映射：T -> int64_t
	typeMap := make(map[string]string)
	for i, tp := range fnStmt.TypeParams {
		if i < len(typeArgs) {
			typeMap[tp.Name] = typeArgs[i]
		}
	}
	
	// 实例化返回类型
	returnType := fnStmt.ReturnType
	if mappedType, ok := typeMap[returnType]; ok {
		returnType = mappedType
	}
	
	// 创建新的函数语句
	instFunc := &ast.FunctionStatement{
		Name:       instName,
		Params:     make([]string, len(fnStmt.Params)),
		Body:       fnStmt.Body,
		ReturnType: returnType,
		Generic:    false,
		NoKMM:      fnStmt.NoKMM,
		Inline:     fnStmt.Inline,
		Annotation: fnStmt.Annotation,
	}
	
	// 复制参数（不需要替换，因为参数名不变，只是类型在返回类型中体现）
	copy(instFunc.Params, fnStmt.Params)
	
	return instFunc
}

// getProgram 获取程序 AST（简化实现，实际需要从编译器获取）
func (cg *CodeGenerator) getProgram() *ast.Program {
	return cg.program
}

// findFunctionByName 在程序中查找函数声明
func (cg *CodeGenerator) findFunctionByName(name string) *ast.FunctionStatement {
	if cg.program == nil {
		return nil
	}
	for _, stmt := range cg.program.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == name {
				return fnStmt
			}
		}
	}
	return nil
}

// findPrefixStatement 在程序中查找 prefix 语句
func (cg *CodeGenerator) findPrefixStatement(name string) *ast.PrefixStatement {
	if cg.program == nil {
		return nil
	}
	for _, stmt := range cg.program.Statements {
		if prefixStmt, ok := stmt.(*ast.PrefixStatement); ok {
			if prefixStmt.Name == name {
				return prefixStmt
			}
		}
	}
	return nil
}

// IsStructType 检查指定名称是否是已定义的结构体类型
func (cg *CodeGenerator) IsStructType(name string) bool {
	if cg.program == nil {
		return false
	}
	for _, stmt := range cg.program.Statements {
		if structStmt, ok := stmt.(*ast.StructStatement); ok {
			if structStmt.Name == name {
				return true
			}
		}
	}
	return false
}

// GetGenericCachedCode 获取缓存的泛型代码
func (cg *CodeGenerator) GetGenericCachedCode(funcName string, typeArgs []string) (string, bool) {
	cacheKey := funcName + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			cacheKey += ","
		}
		cacheKey += arg
	}
	cacheKey += ">"
	
	if cached, ok := cg.genericCache[cacheKey]; ok {
		return cached.GeneratedCode, true
	}
	return "", false
}

func getStmtPos(stmt ast.Statement) *ast.Position {
	if stmt == nil {
		return nil
	}
	v := reflect.ValueOf(stmt)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.IfStatement:
		return &s.Pos
	case *ast.WhileStatement:
		return &s.Pos
	case *ast.ForStatement:
		return &s.Pos
	case *ast.ReturnStatement:
		return &s.Pos
	case *ast.ExpressionStatement:
		return &s.Pos
	case *ast.VOStatement:
		return &s.Pos
	case *ast.SpendStatement:
		return &s.Pos
	case *ast.TaskStatement:
		return &s.Pos
	case *ast.PrefixStatement:
		return &s.Pos
	case *ast.TreeStatement:
		return &s.Pos
	case *ast.ObjectStatement:
		return &s.Pos
	case *ast.YeideStatement:
		return &s.Pos
	case *ast.ReleaseStatement:
		return &s.Pos
	case *ast.ExtractStatement:
		return &s.Pos
	case *ast.BreakStatement:
		return &s.Pos
	case *ast.ContinueStatement:
		return &s.Pos
	case *ast.VariableDeclaration:
		return &s.Pos
	case *ast.FunctionStatement:
		return &s.Pos
	case *ast.ClassStatement:
		return &s.Pos
	case *ast.InterfaceStatement:
		return &s.Pos
	case *ast.StructStatement:
		return &s.Pos
	case *ast.TypeAliasStatement:
		return &s.Pos
	case *ast.SwitchStatement:
		return &s.Pos
	case *ast.CallStatement:
		return &s.Pos
	case *ast.NonLocalStatement:
		return &s.Pos
	case *ast.BlockStatement:
		return &s.Pos
	}
	return nil
}