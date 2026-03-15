package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
)

// FunctionGenerator 负责函数相关的代码生成
type FunctionGenerator struct {
	codegen *CodeGenerator
}

// NewFunctionGenerator 创建一个新的函数生成器
func NewFunctionGenerator(cg *CodeGenerator) *FunctionGenerator {
	return &FunctionGenerator{
		codegen: cg,
	}
}

// GenerateFunctionStatement 生成函数语句代码
func (fg *FunctionGenerator) GenerateFunctionStatement(stmt *ast.FunctionStatement) string {
	// 进入函数作用域
	fg.codegen.EnterScope("function_" + stmt.Name)
	
	// 特殊处理 main 函数
	if stmt.Name == "main" {
		return fg.generateMainFunction(stmt)
	}
	
	// 生成普通函数定义
	code := "void* "
	code += stmt.Name
	code += "(void* arg) {\n"
	fg.codegen.indent++
	
	// ========== 新增：注入 ScopedAllocator 作用域入口 ==========
	code += fg.codegen.indentString()
	code += "// KMM ScopedAllocator 作用域开始\n"
	code += fg.codegen.indentString()
	code += "kaula_scope_enter();\n"
	// =========================================================
	
	// 生成参数处理并添加到符号表
	for _, param := range stmt.Params {
		code += fg.codegen.indentString()
		code += fmt.Sprintf("i64 %s = (i64)(intptr_t)arg;\n", param)
		// 添加参数到符号表
		fg.codegen.AddSymbol(param, "i64", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
	}
	
	// 生成函数体
	for _, bodyStmt := range stmt.Body {
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}
	
	// ========== 新增：注入 ScopedAllocator 作用域出口 ==========
	code += fg.codegen.indentString()
	code += "// KMM ScopedAllocator 作用域结束\n"
	code += fg.codegen.indentString()
	code += "kaula_scope_exit();\n"
	// =========================================================
	
	// 添加默认返回语句
	code += fg.codegen.indentString() + "return NULL;\n"
	fg.codegen.indent--
	code += "}\n"
	
	// 退出函数作用域
	fg.codegen.ExitScope()
	return code
}

// generateMainFunction 生成 main 函数代码
func (fg *FunctionGenerator) generateMainFunction(stmt *ast.FunctionStatement) string {
	// 生成 main 函数定义
	code := "int main() {\n"
	fg.codegen.indent++
	
	// ========== 新增：注入 ScopedAllocator 作用域入口 ==========
	code += fg.codegen.indentString()
	code += "// KMM ScopedAllocator 作用域开始\n"
	code += fg.codegen.indentString()
	code += "kaula_scope_enter();\n"
	// =========================================================
	
	// 生成函数体
	for _, bodyStmt := range stmt.Body {
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}
	
	// ========== 新增：注入 ScopedAllocator 作用域出口 ==========
	code += fg.codegen.indentString()
	code += "// KMM ScopedAllocator 作用域结束\n"
	code += fg.codegen.indentString()
	code += "kaula_scope_exit();\n"
	// =========================================================
	
	// 添加默认返回语句
	code += fg.codegen.indentString() + "return 0;\n"
	fg.codegen.indent--
	code += "}\n"
	
	// 退出函数作用域
	fg.codegen.ExitScope()
	return code
}
