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
	
	genericCache       map[string]*GenericInstanceCache
	genericInstantiated map[string]bool
	currentFuncTypeParams []*ast.TypeParameter

	currentFunctionName string
	callStack           map[string]bool
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
		genericCache:       make(map[string]*GenericInstanceCache),
		genericInstantiated: make(map[string]bool),
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
	
	typeCode := ""
	functionCode := ""
	hasMain := false
	mainCode := ""
	
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*ast.ImportStatement); ok {
			continue
		}
		
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok {
			if fnStmt.Name == "main" {
				hasMain = true
				functionCode += cg.generateStatement(stmt) + "\n"
			} else {
				functionCode += cg.generateStatement(stmt) + "\n"
			}
		} else if _, ok := stmt.(*ast.ClassStatement); ok {
			typeCode += cg.generateStatement(stmt) + "\n"
		} else if _, ok := stmt.(*ast.InterfaceStatement); ok {
			typeCode += cg.generateStatement(stmt) + "\n"
		} else if _, ok := stmt.(*ast.StructStatement); ok {
			typeCode += cg.generateStatement(stmt) + "\n"
		} else {
			mainCode += cg.indentString() + cg.generateStatement(stmt)
		}
	}
	
	baseIncludes := "#include <stdint.h>\n#include <stdbool.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"../src/kaula.h\"\n"
	
	importedModules := make(map[string]bool)
	for _, stmt := range program.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok {
			importedModules[importStmt.Module] = true
		}
	}

	cg.usedModules = make([]string, 0, len(importedModules))
	for moduleName := range importedModules {
		cg.usedModules = append(cg.usedModules, moduleName)
	}

	thirdPartyIncludes := ""
	if cg.stdlibConfig != nil {
		for moduleName := range importedModules {
			module, ok := cg.stdlibConfig.Modules[moduleName]
			if ok {
				if module.Header != "" {
					header := module.Header
					if len(header) >= 3 && header[0] == '.' && header[1] == '.' && header[2] == '/' {
						header = header[3:]
					} else if len(header) >= 4 && header[0] == 's' && header[1] == 't' && header[2] == 'd' && header[3] == '/' {
						header = "../" + header
					}
					thirdPartyIncludes += "#include \"" + header + "\"\n"
				}
			} else {
				found := false
				for _, lib := range cg.stdlibConfig.ThirdParty {
					if lib.Name == moduleName {
						found = true
						for _, header := range lib.Headers {
							cleanHeader := header
							if len(header) > 3 && header[0] == '"' && header[1] == '.' && header[2] == '.' && header[3] == '/' {
								cleanHeader = "\"" + header[4:]
							}
							thirdPartyIncludes += "#include " + cleanHeader + "\n"
						}
						break
					}
				}
				if !found {
				}
			}
		}
	}
	
	allIncludes := baseIncludes + thirdPartyIncludes
	
	cacheDir := "cache"
	if err := os.MkdirAll(cacheDir, 0755); err == nil {
		os.WriteFile(filepath.Join(cacheDir, "all_includes.txt"), []byte(allIncludes), 0644)
	}
	
	if !hasMain {
		template, ok := cg.templateManager.GetTemplate("main")
		if !ok {
			template = "{{includes}}\n\n{{type_code}}\n{{function_code}}\n\nint main() {\n    {{main_code}}\n    return 0;\n}\n"
		}
		
		result := template
		result = strings.ReplaceAll(result, "{{includes}}", allIncludes)
		result = strings.ReplaceAll(result, "{{type_code}}", typeCode)
		result = strings.ReplaceAll(result, "{{function_code}}", functionCode)
		result = strings.ReplaceAll(result, "{{main_code}}", mainCode)
		result = strings.ReplaceAll(result, "{{code}}", "")
		
		return result
	} else {
		return allIncludes + "\n" + typeCode + functionCode
	}
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
func (cg *CodeGenerator) EnterScope(scopeName string) {
	newScope := symbol.NewSymbolTable(cg.currentScope, scopeName)
	cg.currentScope = newScope
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
