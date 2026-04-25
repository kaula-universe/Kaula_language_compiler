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
	InstantiatedAt int // 实例化位置
}

// CodeGenerator 表示代码生成器
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

	// 模块化生成器
	typeGenerator       *TypeGenerator
	functionGenerator   *FunctionGenerator
	expressionGenerator *ExpressionGenerator
	statementGenerator  *StatementGenerator

	// 追踪使用的第三方库
	usedThirdPartyLibs map[string]bool
	
	// 泛型实例缓存
	genericCache       map[string]*GenericInstanceCache
	genericInstantiated map[string]bool // 已实例化的泛型
	currentFuncTypeParams []*ast.TypeParameter // 当前函数的泛型参数

	// Task/Async 闭环检测
	currentFunctionName string
	callStack           map[string]bool
}

// error 报告错误
func (cg *CodeGenerator) error(message string) {
	cg.errors = append(cg.errors, message)
}

// Errors 返回错误列表
func (cg *CodeGenerator) Errors() []string {
	return cg.errors
}

// HasErrors 检查是否有错误
func (cg *CodeGenerator) HasErrors() bool {
	return len(cg.errors) > 0
}

// SetStdlibConfig 设置 stdlib 配置
func (cg *CodeGenerator) SetStdlibConfig(cfg *stdlib.StdlibConfig) {
	cg.stdlibConfig = cfg
}

// GetStdlibConfig 获取 stdlib 配置（用于调试）
func (cg *CodeGenerator) GetStdlibConfig() *stdlib.StdlibConfig {
	return cg.stdlibConfig
}

// IsGenericInstantiated 检查泛型是否已实例化
func (cg *CodeGenerator) IsGenericInstantiated(name string) bool {
	return cg.genericInstantiated[name]
}

// MarkGenericInstantiated 标记泛型已实例化
func (cg *CodeGenerator) MarkGenericInstantiated(name string) {
	if cg.genericInstantiated == nil {
		cg.genericInstantiated = make(map[string]bool)
	}
	cg.genericInstantiated[name] = true
}

// GetUsedModules 获取已使用的模块列表
func (cg *CodeGenerator) GetUsedModules() []string {
	return cg.usedModules
}

// NewCodeGenerator 创建一个新的代码生成器
func NewCodeGenerator(cfg *config.Config) *CodeGenerator {
	tm := NewTemplateManager()
	templatePath := filepath.Join(cfg.TemplatePath, "main.c.tmpl")
	tm.LoadTemplate("main", templatePath)

	pm := NewPluginManager()

	// 使用配置中的 stdlibPath（与语义分析器保持一致）
	stdlibPath := cfg.StdlibPath
	if stdlibPath == "" {
		// 回退到默认路径
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

	// 初始化 Tree 和 Prefix 管理器
	treeManager := core.NewTreeManager()
	prefixManager := core.NewPrefixManager()

	// 初始化符号表
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
	
	// 初始化模块化生成器
	cg.typeGenerator = NewTypeGenerator(cg)
	cg.functionGenerator = NewFunctionGenerator(cg)
	cg.expressionGenerator = NewExpressionGenerator(cg)
	cg.statementGenerator = NewStatementGenerator(cg)
	
	return cg
}

// Generate 生成代码
func (cg *CodeGenerator) Generate(program *ast.Program) string {
	// 保存 program 引用以便后续查找
	cg.program = program
	// 重置第三方库使用追踪
	cg.usedThirdPartyLibs = make(map[string]bool)
	
	typeCode := ""
	functionCode := ""
	hasMain := false
	mainCode := ""
	
	for _, stmt := range program.Statements {
		// 跳过 import 语句，不生成 C 代码
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
	
	// 生成基础包含语句
	// 硬编码 src/kaula.h 路径，确保生成的代码能正确找到头文件
	baseIncludes := "#include <stdint.h>\n#include <stdbool.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"src/kaula.h\"\n"
	
	// 收集所有导入的模块
	importedModules := make(map[string]bool)
	for _, stmt := range program.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok {
			importedModules[importStmt.Module] = true
		}
	}

	// 存储使用的模块列表
	cg.usedModules = make([]string, 0, len(importedModules))
	for moduleName := range importedModules {
		cg.usedModules = append(cg.usedModules, moduleName)
	}

	// 只添加实际使用的第三方库头文件
	thirdPartyIncludes := ""
	if cg.stdlibConfig != nil {
		// 遍历所有导入的模块
		for moduleName := range importedModules {
			// 检查是否是标准库模块
			module, ok := cg.stdlibConfig.Modules[moduleName]
			if ok {
				// 添加模块对应的头文件
				if module.Header != "" {
					thirdPartyIncludes += "#include \"" + module.Header + "\"\n"
				}
			} else {
				// 检查是否是第三方库
				found := false
				for _, lib := range cg.stdlibConfig.ThirdParty {
					if lib.Name == moduleName {
						found = true
						// 添加库的头文件
						for _, header := range lib.Headers {
							// 头文件已经包含 <> 或""，直接使用
							// 如果路径以 ../ 开头，去掉它（因为生成的 C 文件和源文件在同一目录）
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
					// 未找到模块或库
				}
			}
		}
	}
	
	allIncludes := baseIncludes + thirdPartyIncludes
	
	// 写入调试信息（仅在目录存在或可创建时）
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

// generateStatement 生成语句代码（委托给 statementGenerator）
func (cg *CodeGenerator) generateStatement(stmt ast.Statement) string {
	return cg.statementGenerator.GenerateStatement(stmt)
}

// generateExpression 生成表达式代码（委托给 expressionGenerator）
func (cg *CodeGenerator) generateExpression(expr ast.Expression) string {
	return cg.expressionGenerator.GenerateExpression(expr)
}

// indentString 生成缩进字符串（使用缓存优化性能）
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
	
	// 检查是否已经实例化
	instName := funcName + "__"
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
	instName += "__"
	
	if cg.genericInstantiated[instName] {
		return "", nil // 已经实例化过
	}
	
	// 获取原始函数
	program := cg.getProgram() // 需要从某个地方获取 program
	if program == nil {
		return "", fmt.Errorf("cannot find program for generic instantiation")
	}
	
	fnStmt := program.FindFunction(funcName)
	if fnStmt == nil || !fnStmt.IsGeneric() {
		return "", fmt.Errorf("function %s is not generic", funcName)
	}
	
	// 创建实例化后的函数
	instFunc := &ast.FunctionStatement{
		Name:       instName,
		Params:     fnStmt.Params,
		Body:       fnStmt.Body,
		ReturnType: fnStmt.ReturnType,
		Generic:    false,
	}
	
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
