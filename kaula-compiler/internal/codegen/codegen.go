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
	"strconv"
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
	trackedModules  map[string]bool

	typeGenerator       *TypeGenerator
	functionGenerator   *FunctionGenerator
	expressionGenerator *ExpressionGenerator
	statementGenerator  *StatementGenerator

	usedThirdPartyLibs map[string]bool
	localImportFuncs   map[string]bool

	genericCache          map[string]*GenericInstanceCache
	genericInstantiated   map[string]bool
	genericFuncCode       strings.Builder // 泛型实例化函数代码
	genericInstDepth      int             // 当前实例化深度（防止无限递归）

	genericTypeCache      map[string]string // 泛型类型实例缓存：Box<int> → K_Box_int
	genericTypeCode       strings.Builder   // 泛型类型实例化代码（typedef 定义）
	genericTypeInstDepth  int               // 泛型类型实例化深度（防止无限递归）

	currentFunctionName       string
	currentFunctionReturnType string
	callStack                 map[string]bool

	sorAdapter *SORCodeGenAdapter

	kmmScopeDepth    int
	offsetScopeDepth int // 跟踪 offset_save/restore scope 嵌套深度，用于相邻 scope 合并优化

	sourceMap  *SourceMap
	sourceFile string

	lambdaCounter     int
	lambdaDefinitions []string

	// 编译期常量表：const 变量名 → 求值后的字面量
	// 用于在 codegen 阶段将 const 引用内联为字面量，实现编译期常量求值
	constTable map[string]string
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

// EnterOffsetScope 进入 offset_save/restore 作用域
// 用于相邻 scope 合并优化：当外层已有 offset scope 时，内层 BlockStatement 跳过重复包裹
func (cg *CodeGenerator) EnterOffsetScope() {
	cg.offsetScopeDepth++
}

// ExitOffsetScope 退出 offset_save/restore 作用域
func (cg *CodeGenerator) ExitOffsetScope() {
	if cg.offsetScopeDepth > 0 {
		cg.offsetScopeDepth--
	}
}

// IsInOffsetScope 当前是否在 offset_save/restore 作用域内
func (cg *CodeGenerator) IsInOffsetScope() bool {
	return cg.offsetScopeDepth > 0
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

// trackModuleUsage 自动追踪模块使用（无需显式 import）
func (cg *CodeGenerator) trackModuleUsage(moduleName string) {
	if cg.trackedModules == nil {
		cg.trackedModules = make(map[string]bool)
	}
	if !cg.trackedModules[moduleName] {
		cg.trackedModules[moduleName] = true
		cg.usedModules = append(cg.usedModules, moduleName)
	}
}

// SetLocalImportFuncs 注册本地导入的 pub 函数名
func (cg *CodeGenerator) SetLocalImportFuncs(funcs map[string]bool) {
	cg.localImportFuncs = funcs
}

func NewCodeGenerator(cfg *config.Config) *CodeGenerator {
	tm := NewTemplateManager()
	templatePath := filepath.Join(cfg.TemplatePath, "main.c.tmpl")
	tm.LoadTemplate("main", templatePath)
	// 加载 freestanding 裸机入口模板（用于 --freestanding 模式）
	freestandingPath := filepath.Join(cfg.TemplatePath, "freestanding.c.tmpl")
	tm.LoadTemplate("freestanding", freestandingPath)

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
		output:              "",
		indent:              0,
		templateManager:     tm,
		config:              cfg,
		pluginManager:       pm,
		stdlibConfig:        stdlibConfig,
		treeManager:         treeManager,
		prefixManager:       prefixManager,
		symbolTable:         symbolTable,
		currentScope:        symbolTable,
		errors:              []string{},
		usedThirdPartyLibs:  make(map[string]bool),
		localImportFuncs:    make(map[string]bool),
		genericCache:        make(map[string]*GenericInstanceCache),
		genericInstantiated: make(map[string]bool),
		genericTypeCache:    make(map[string]string),
		sourceMap:           NewSourceMap("", ""),
		constTable:          make(map[string]string),
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
	cg.lambdaCounter = 0
	cg.lambdaDefinitions = nil

	type rawEntry struct {
		section string
		relLine int
		srcLine int
		srcCol  int
		kind    string
		symbol  string
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
		} else if enumStmt, ok := stmt.(*ast.EnumStatement); ok {
			// 注册枚举变体到符号表
			for _, variant := range enumStmt.Variants {
				cg.symbolTable.AddSymbol(variant.Name, "enum_variant:"+enumStmt.Name, false, "global", enumStmt.Pos.Line, enumStmt.Pos.Column)
			}
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", enumStmt.Pos.Line, enumStmt.Pos.Column, "enum", enumStmt.Name, lines)
			typeCode.WriteString(code)
		} else if typeStmt, ok := stmt.(*ast.TypeAliasStatement); ok {
			code := cg.generateStatement(stmt) + "\n"
			lines := strings.Count(code, "\n")
			addEntry("type", typeStmt.Pos.Line, typeStmt.Pos.Column, "type", typeStmt.Name, lines)
			typeCode.WriteString(code)
		} else if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
			if varDecl == nil {
				continue
			}
			// const 变量：可编译期求值的存入常量表（不生成 C 代码）；
			// 显式类型且无法求值的（如 const char* s = fn()）按普通 C const 变量生成
			if varDecl.IsConst {
				if evaluated := cg.tryEvalConstExpr(varDecl.Value); evaluated != "" {
					cg.constTable[varDecl.Name] = evaluated
					continue
				}
				if varDecl.Type == "" {
					cg.constTable[varDecl.Name] = cg.expressionGenerator.GenerateExpression(varDecl.Value)
					continue
				}
			}
			cType := cg.typeGenerator.convertType(varDecl.Type, varDecl.Nullable)
			initValue := cg.generateExpression(varDecl.Value)
			if varDecl.IsAuto {
				cType = "auto"
			}
			// 全局变量：支持属性、static
			var varPrefix strings.Builder
			if len(varDecl.Attributes) > 0 {
				varPrefix.WriteString(generateVarAttributes(varDecl.Attributes))
			}
			if varDecl.IsStatic {
				varPrefix.WriteString("static ")
			}
			if varDecl.IsConst {
				varPrefix.WriteString("const ")
			}
			prefix := varPrefix.String()
			if prefix != "" {
				code := fmt.Sprintf("%s%s %s = %s;\n", prefix, cType, varDecl.Name, initValue)
				lines := strings.Count(code, "\n")
				addEntry("global", varDecl.Pos.Line, varDecl.Pos.Column, "variable", varDecl.Name, lines)
				globalVars.WriteString(code)
			} else {
				code := fmt.Sprintf("%s %s = %s;\n", cType, varDecl.Name, initValue)
				lines := strings.Count(code, "\n")
				addEntry("global", varDecl.Pos.Line, varDecl.Pos.Column, "variable", varDecl.Name, lines)
				globalVars.WriteString(code)
			}
		} else if externStmt, ok := stmt.(*ast.ExternStatement); ok {
			if externStmt == nil {
				continue
			}
			var code string
			if externStmt.IsFunction {
				// extern 函数声明：生成完整原型
				returnType := cg.typeGenerator.convertType(externStmt.ReturnType, false)
				if returnType == "" {
					returnType = "void"
				}
				var params strings.Builder
				if len(externStmt.ParamTypes) == 0 {
					params.WriteString("void")
				} else {
					for i, pType := range externStmt.ParamTypes {
						if i > 0 {
							params.WriteString(", ")
						}
						cType := cg.typeGenerator.convertType(pType, false)
						if cType == "" {
							cType = "void*"
						}
						params.WriteString(cType)
					}
				}
				code = fmt.Sprintf("extern %s %s(%s);\n", returnType, externStmt.Name, params.String())
			} else {
				cType := cg.typeGenerator.convertType(externStmt.Type, externStmt.Nullable)
				code = fmt.Sprintf("extern %s %s;\n", cType, externStmt.Name)
			}
			lines := strings.Count(code, "\n")
			addEntry("global", externStmt.Pos.Line, externStmt.Pos.Column, "extern", externStmt.Name, lines)
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

	// 将泛型类型实例化代码注入到 typeCode 之前，
	// 确保实例化类型定义在引用之前（如 Box<int> 的 typedef 必须先于使用它的代码）
	if cg.genericTypeCode.Len() > 0 {
		combined := cg.genericTypeCode.String() + typeCode.String()
		typeCode.Reset()
		typeCode.WriteString(combined)
	}

	// 将 lambda 定义插入到 functionCode 之前
	var lambdaCode strings.Builder
	for _, def := range cg.lambdaDefinitions {
		lambdaCode.WriteString(def)
	}
	// 将 lambda 定义合并到 functionCode 前面
	if lambdaCode.Len() > 0 {
		combined := lambdaCode.String() + functionCode.String()
		functionCode.Reset()
		functionCode.WriteString(combined)
	}

	// 将泛型实例化代码注入到 functionCode 之前，
	// 确保定义在 main 中调用之前
	if cg.genericFuncCode.Len() > 0 {
		combined := cg.genericFuncCode.String() + functionCode.String()
		functionCode.Reset()
		functionCode.WriteString(combined)
	}

	var allIncludes strings.Builder
	allIncludes.Grow(2048)
	if cg.config != nil && cg.config.Freestanding {
		// freestanding 模式：只包含 freestanding 安全的标准头文件
		// 不包含 kaula.h（标准库运行时头，裸机环境不可用）
		// 不包含 <stdio.h>/<stdlib.h>/<string.h>，它们在裸机下不存在
		// memset/memcpy 等由 kaula_freestanding_runtime.c 提供
		allIncludes.WriteString("#include <stdint.h>\n#include <stdbool.h>\n#include <stddef.h>\n")
	} else {
		allIncludes.WriteString("#include <stdint.h>\n#include <stdbool.h>\n#include <stddef.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"kaula.h\"\n#include \"kaula_runtime.h\"\n")
	}

	if cg.stdlibConfig != nil {
		// 只为显式导入的模块生成 #include
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

	// freestanding 模式：始终使用裸机入口模板，无论是否存在 main 函数
	useFreestanding := cg.config != nil && cg.config.Freestanding

	if useFreestanding || !hasMain {
		templateName := "main"
		if useFreestanding {
			templateName = "freestanding"
		}
		template, ok := cg.templateManager.GetTemplate(templateName)
		if !ok {
			var resultBuilder strings.Builder
			resultBuilder.Grow(allIncludes.Len() + forwardDecls.Len() + globalVars.Len() + typeCode.Len() + functionCode.Len() + mainCode.Len() + 256)
			resultBuilder.WriteString(allIncludes.String())
			resultBuilder.WriteString("\n\n")
			resultBuilder.WriteString(forwardDecls.String())
			resultBuilder.WriteByte('\n')

			typeOffset = strings.Count(allIncludes.String(), "\n") + 3 + strings.Count(forwardDecls.String(), "\n") + 1

			resultBuilder.WriteString(typeCode.String())
			resultBuilder.WriteByte('\n')

			globalOffset = typeOffset + strings.Count(typeCode.String(), "\n") + 1

			resultBuilder.WriteString(globalVars.String())
			resultBuilder.WriteByte('\n')

			funcOffset = globalOffset + strings.Count(globalVars.String(), "\n") + 1
			resultBuilder.WriteString(functionCode.String())

			if useFreestanding {
				// freestanding 模式：不生成 main 函数，入口由 _start 或自定义 entry 提供
				result = resultBuilder.String()
			} else {
				mainHeader := "\n\nint main() {\n    "
				mainOffset = funcOffset + strings.Count(functionCode.String(), "\n") + strings.Count(mainHeader, "\n")
				resultBuilder.WriteString(mainHeader)
				resultBuilder.WriteString(mainCode.String())
				resultBuilder.WriteString("\n    return 0;\n}\n")
				result = resultBuilder.String()
			}
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
			// freestanding 模板无 {{main_code}} 占位符，idxMain 可能为 -1
			if idxMain >= 0 {
				mainOffset = strings.Count(result[:idxMain], "\n") + 1
			} else {
				mainOffset = funcOffset + strings.Count(functionCode.String(), "\n")
			}
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

// MangleGenericName 生成泛型实例化后的 C 函数名。
// 规则：kaula_<funcName>_<mangled_typeargs>，类型参数中的非字母数字字符
// 转义为 _<codepoint>_ 以保证生成合法的 C 标识符。
// 例：identity<int>      → kaula_identity_int
//     identity<[]int>    → kaula_identity__91__93_int
func MangleGenericName(funcName string, typeArgs []string) string {
	name := "kaula_" + funcName + "_"
	for i, arg := range typeArgs {
		if i > 0 {
			name += "_"
		}
		for _, ch := range arg {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
				name += string(ch)
			} else {
				name += fmt.Sprintf("_%d_", ch)
			}
		}
	}
	return name
}

// MaxGenericInstantiationDepth 泛型实例化深度上限，防止无限递归实例化
// （如 fn f<T>(T x) { f<[]T>([x]) } 会无限展开）
const MaxGenericInstantiationDepth = 32

// InstantiateGeneric 实例化泛型函数
func (cg *CodeGenerator) InstantiateGeneric(funcName string, typeArgs []string, line int) (string, error) {
	// 深度限制：防止无限递归实例化
	if cg.genericInstDepth >= MaxGenericInstantiationDepth {
		return "", fmt.Errorf("generic instantiation depth limit (%d) exceeded at %s<%s> (line %d): possible infinite recursion",
			MaxGenericInstantiationDepth, funcName, strings.Join(typeArgs, ","), line)
	}

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
	instName := MangleGenericName(funcName, typeArgs)

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
	instFunc, typeMap := cg.instantiateGenericFunction(fnStmt, typeArgs, instName)

	// 设置活跃类型映射，使函数体内的类型参数引用（T、[]T、*T 等）
	// 在 convertType 时被替换为具体类型。生成后恢复，避免污染其他函数。
	// 同时递增实例化深度，防止无限递归实例化。
	oldTypeMap := cg.typeGenerator.PushActiveTypeMap(typeMap)
	cg.genericInstDepth++
	code := cg.functionGenerator.GenerateFunctionStatement(instFunc)
	cg.genericInstDepth--
	cg.typeGenerator.PopActiveTypeMap(oldTypeMap)

	// 写入泛型实例化代码缓冲区，稍后插入到 functionCode 之前
	if code != "" {
		cg.genericFuncCode.WriteString(code)
		cg.genericFuncCode.WriteByte('\n')
	}

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

// instantiateGenericFunction 创建泛型函数的实例化版本，返回实例化函数及类型参数映射
func (cg *CodeGenerator) instantiateGenericFunction(fnStmt *ast.FunctionStatement, typeArgs []string, instName string) (*ast.FunctionStatement, map[string]string) {
	// 创建类型参数映射：T -> int（Kaula 类型名，由 convertType 进一步映射为 C 类型）
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

	// 复制参数名
	copy(instFunc.Params, fnStmt.Params)

	// 映射参数类型
	instFunc.ParamTypes = make([]string, len(fnStmt.ParamTypes))
	for i, pt := range fnStmt.ParamTypes {
		instFunc.ParamTypes[i] = pt
		if mappedType, ok := typeMap[pt]; ok {
			instFunc.ParamTypes[i] = mappedType
		}
	}

	return instFunc, typeMap
}

// getProgram 获取程序 AST（简化实现，实际需要从编译器获取）
func (cg *CodeGenerator) getProgram() *ast.Program {
	return cg.program
}

// MangleGenericTypeName 生成泛型类型实例化后的 C 类型名（不含 K_ 前缀）。
// 规则：baseName_<mangled_typeargs>，类型参数中的非字母数字字符
// 转义为 _<codepoint>_ 以保证生成合法的 C 标识符。
// 例：Box<int>           → Box_int
//     Result<int, string> → Result_int_string
//     Pair<[]int, *T>     → Pair__91__93_int__42_T
func MangleGenericTypeName(baseName string, typeArgs []string) string {
	name := baseName
	for _, arg := range typeArgs {
		name += "_"
		for _, ch := range arg {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
				name += string(ch)
			} else {
				name += fmt.Sprintf("_%d_", ch)
			}
		}
	}
	return name
}

// InstantiateGenericType 实例化泛型类型（类/结构体/枚举），返回 C 类型名（含 K_ 前缀）。
// 如 Box<int> → K_Box_int，同时生成对应的 C 类型定义写入 genericTypeCode 缓冲区。
// 机制：找到泛型类型定义，构建类型参数映射（T→具体类型），复制 AST 节点并清除 Generic
// 标记，设置 activeTypeMap 后调用常规 Generate*Statement 路径生成实例化代码。
func (cg *CodeGenerator) InstantiateGenericType(typeName string, typeArgs []string, line int) (string, error) {
	// 深度限制：防止无限递归实例化（如 struct Node<T> { Node<T>* next; }）
	if cg.genericTypeInstDepth >= MaxGenericInstantiationDepth {
		return "", fmt.Errorf("generic type instantiation depth limit (%d) exceeded at %s<%s> (line %d): possible infinite recursion",
			MaxGenericInstantiationDepth, typeName, strings.Join(typeArgs, ","), line)
	}

	// 生成缓存键
	cacheKey := typeName + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			cacheKey += ","
		}
		cacheKey += arg
	}
	cacheKey += ">"

	// 检查缓存
	if cached, ok := cg.genericTypeCache[cacheKey]; ok {
		return cached, nil
	}

	if cg.program == nil {
		return "", fmt.Errorf("program not set for generic type instantiation")
	}

	// 查找泛型类型定义（类/结构体/枚举），构建类型参数映射和生成器
	var typeParams []*ast.TypeParameter
	var generate func(instName string) string

	if classStmt := cg.program.FindClass(typeName); classStmt != nil && classStmt.Generic {
		typeParams = classStmt.TypeParams
		generate = func(instName string) string {
			inst := *classStmt // 浅拷贝：Fields/Methods/Constructors 共享（只读）
			inst.Name = instName
			inst.Generic = false
			inst.TypeParams = nil
			return cg.typeGenerator.GenerateClassStatement(&inst)
		}
	} else if structStmt := cg.program.FindStruct(typeName); structStmt != nil && structStmt.Generic {
		typeParams = structStmt.TypeParams
		generate = func(instName string) string {
			inst := *structStmt
			inst.Name = instName
			inst.Generic = false
			inst.TypeParams = nil
			return cg.typeGenerator.GenerateStructStatement(&inst)
		}
	} else if enumStmt := cg.program.FindEnum(typeName); enumStmt != nil && enumStmt.Generic {
		typeParams = enumStmt.TypeParams
		generate = func(instName string) string {
			inst := *enumStmt
			inst.Name = instName
			inst.Generic = false
			inst.TypeParams = nil
			return cg.typeGenerator.GenerateEnumStatement(&inst)
		}
	} else {
		return "", fmt.Errorf("generic type %s not found or not generic", typeName)
	}

	// 构建类型参数映射：T -> 具体类型（Kaula 类型名，由 MapKaulaTypeToC 进一步映射为 C 类型）
	typeMap := make(map[string]string)
	for i, tp := range typeParams {
		if i < len(typeArgs) {
			typeMap[tp.Name] = typeArgs[i]
		}
	}

	// 实例化后的类型名：Box_int → C tag K_Box_int
	instName := MangleGenericTypeName(typeName, typeArgs)
	cName := kaulaStructTag(instName)

	// 设置活跃类型映射，使字段/方法/构造函数中的类型参数引用被替换为具体类型。
	// 生成后恢复，避免污染其他类型。同时递增实例化深度，防止无限递归实例化。
	oldTypeMap := cg.typeGenerator.PushActiveTypeMap(typeMap)
	cg.typeGenerator.structTypes[instName] = true
	cg.genericTypeInstDepth++
	code := generate(instName)
	cg.genericTypeInstDepth--
	cg.typeGenerator.PopActiveTypeMap(oldTypeMap)

	// 写入泛型类型实例化代码缓冲区，稍后前置注入到 typeCode 之前
	if code != "" {
		cg.genericTypeCode.WriteString(code)
		if !strings.HasSuffix(code, "\n") {
			cg.genericTypeCode.WriteByte('\n')
		}
	}

	cg.genericTypeCache[cacheKey] = cName
	return cName, nil
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

// findPrefixStatement 在程序中查找 prefix 语句（递归搜索）
func (cg *CodeGenerator) findPrefixStatement(name string) *ast.PrefixStatement {
	if cg.program == nil {
		return nil
	}
	return cg.findPrefixInStatements(cg.program.Statements, name)
}

// findPrefixInStatements 递归搜索 statements 中的 prefix 语句
func (cg *CodeGenerator) findPrefixInStatements(stmts []ast.Statement, name string) *ast.PrefixStatement {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.PrefixStatement:
			if s.Name == name {
				return s
			}
		case *ast.FunctionStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.IfStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
			if result := cg.findPrefixInStatements(s.Else, name); result != nil {
				return result
			}
		case *ast.WhileStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.ForStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.ForInStatement:
			if result := cg.findPrefixInStatements(s.Body, name); result != nil {
				return result
			}
		case *ast.BlockStatement:
			if result := cg.findPrefixInStatements(s.Statements, name); result != nil {
				return result
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

// IsEnumType 检查指定名称是否是已定义的枚举类型
func (cg *CodeGenerator) IsEnumType(name string) bool {
	if cg.program == nil {
		return false
	}
	for _, stmt := range cg.program.Statements {
		if enumStmt, ok := stmt.(*ast.EnumStatement); ok {
			if enumStmt.Name == name {
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
	case *ast.ForInStatement:
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
	case *ast.YieldStatement:
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
	case *ast.EnumStatement:
		return &s.Pos
	case *ast.TypeAliasStatement:
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

// tryEvalConstExpr 尝试在编译期求值常量表达式
// 支持：整数/浮点字面量、其他 const 引用、基本算术运算（+ - * / % << >> & | ^）
// 返回求值后的字面量字符串，无法求值时返回空字符串
func (cg *CodeGenerator) tryEvalConstExpr(expr ast.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return strconv.FormatUint(e.Value, 10)
	case *ast.FloatLiteral:
		return strconv.FormatFloat(e.Value, 'f', -1, 64)
	case *ast.BooleanLiteral:
		if e.Value {
			return "1"
		}
		return "0"
	case *ast.Identifier:
		// 引用其他 const 变量
		if val, ok := cg.constTable[e.Name]; ok {
			return val
		}
		return ""
	case *ast.BinaryExpression:
		left := cg.tryEvalConstExpr(e.Left)
		right := cg.tryEvalConstExpr(e.Right)
		if left == "" || right == "" {
			return ""
		}
		return cg.evalBinaryOp(e.Operator, left, right)
	case *ast.ParenExpression:
		return cg.tryEvalConstExpr(e.Inner)
	}
	return ""
}

// evalBinaryOp 执行编译期二元运算
func (cg *CodeGenerator) evalBinaryOp(op, left, right string) string {
	// 尝试整数运算
	lval, lerr := strconv.ParseInt(left, 0, 64)
	rval, rerr := strconv.ParseInt(right, 0, 64)
	if lerr == nil && rerr == nil {
		switch op {
		case "+":
			return strconv.FormatInt(lval+rval, 10)
		case "-":
			return strconv.FormatInt(lval-rval, 10)
		case "*":
			return strconv.FormatInt(lval*rval, 10)
		case "/":
			if rval == 0 {
				return ""
			}
			return strconv.FormatInt(lval/rval, 10)
		case "%":
			if rval == 0 {
				return ""
			}
			return strconv.FormatInt(lval%rval, 10)
		case "<<":
			return strconv.FormatInt(lval<<uint(rval), 10)
		case ">>":
			return strconv.FormatInt(lval>>uint(rval), 10)
		case "&":
			return strconv.FormatInt(lval&rval, 10)
		case "|":
			return strconv.FormatInt(lval|rval, 10)
		case "^":
			return strconv.FormatInt(lval^rval, 10)
		}
	}
	// 浮点运算
	lf, lfErr := strconv.ParseFloat(left, 64)
	rf, rfErr := strconv.ParseFloat(right, 64)
	if lfErr == nil && rfErr == nil {
		switch op {
		case "+":
			return strconv.FormatFloat(lf+rf, 'f', -1, 64)
		case "-":
			return strconv.FormatFloat(lf-rf, 'f', -1, 64)
		case "*":
			return strconv.FormatFloat(lf*rf, 'f', -1, 64)
		case "/":
			if rf == 0 {
				return ""
			}
			return strconv.FormatFloat(lf/rf, 'f', -1, 64)
		}
	}
	return ""
}
