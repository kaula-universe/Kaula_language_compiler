package codegen

import (
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/config"
	"kaula-compiler/internal/core"
	"kaula-compiler/internal/stdlib"
	"kaula-compiler/internal/symbol"
	"os"
	"path/filepath"
	"strings"
)

// CodeGenerator 表示代码生成器
type CodeGenerator struct {
	output          string
	indent          int
	templateManager *TemplateManager
	config          *config.Config
	pluginManager   *PluginManager
	stdlibConfig    *stdlib.StdlibConfig
	treeManager     *core.Tree
	prefixManager   *core.PrefixManager
	symbolTable     *symbol.SymbolTable
	currentScope    *symbol.SymbolTable
	errors          []string
	
	// 模块化生成器
	typeGenerator       *TypeGenerator
	functionGenerator   *FunctionGenerator
	expressionGenerator *ExpressionGenerator
	statementGenerator  *StatementGenerator
	
	// 追踪使用的第三方库
	usedThirdPartyLibs map[string]bool
}

// NewCodeGenerator 创建一个新的代码生成器
func NewCodeGenerator(cfg *config.Config) *CodeGenerator {
	tm := NewTemplateManager()
	templatePath := filepath.Join(cfg.TemplatePath, "main.c.tmpl")
	tm.LoadTemplate("main", templatePath)

	pm := NewPluginManager()

	// 尝试从多个路径加载 stdlib.json
	stdlibPath := "stdlib.json"
	if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
		stdlibPath = "kaula-compiler/stdlib.json"
		if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
			stdlibPath = "../stdlib.json"
		}
	}
	stdlibConfig, _ := stdlib.LoadStdlibConfig(stdlibPath)

	// 初始化 Tree 和 Prefix 管理器
	treeManager := core.NewTree()
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
	}
	
	// 初始化模块化生成器
	cg.typeGenerator = NewTypeGenerator(cg)
	cg.functionGenerator = NewFunctionGenerator(cg)
	cg.expressionGenerator = NewExpressionGenerator(cg)
	cg.statementGenerator = NewStatementGenerator(cg)
	
	return cg
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

// Generate 生成代码
func (cg *CodeGenerator) Generate(program *ast.Program) string {
	// 重置第三方库使用追踪
	cg.usedThirdPartyLibs = make(map[string]bool)
	
	typeCode := ""
	functionCode := ""
	hasMain := false
	mainCode := ""
	
	for _, stmt := range program.Statements {
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
	baseIncludes := "#include <stdint.h>\n#include <stdbool.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"std/std.h\"\n#include \"std/memory/memory.h\"\n"
	
	// 只添加实际使用的第三方库头文件
	thirdPartyIncludes := ""
	if cg.stdlibConfig != nil && len(cg.usedThirdPartyLibs) > 0 {
		for _, lib := range cg.stdlibConfig.ThirdParty {
			if cg.usedThirdPartyLibs[lib.Name] {
				for _, header := range lib.Headers {
					// 头文件已经包含 <> 或 ""，直接使用
					// 如果路径以 ../ 开头，去掉它（因为生成的 C 文件和源文件在同一目录）
					cleanHeader := header
					if len(header) > 3 && header[0] == '"' && header[1] == '.' && header[2] == '.' && header[3] == '/' {
						cleanHeader = "\"" + header[4:]
					}
					thirdPartyIncludes += "#include " + cleanHeader + "\n"
				}
			}
		}
	}
	
	allIncludes := baseIncludes + thirdPartyIncludes
	
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

// indentString 生成缩进字符串
func (cg *CodeGenerator) indentString() string {
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
