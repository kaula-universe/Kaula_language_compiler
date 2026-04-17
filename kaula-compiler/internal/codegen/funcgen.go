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
	// 使用 int64_t 作为参数和返回类型，支持递归和整数运算
	code := "int64_t "
	code += stmt.Name
	code += "(int64_t arg) {\n"
	fg.codegen.indent++
	
	// ========== KMM Enhanced V4 作用域分配器入口 ==========
	// 使用 KMM_V4_SCOPE_START 宏自动管理内存生命周期（包含 Arena、线程缓存、清理栈）
	code += fg.codegen.indentString()
	code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理开始（Arena + ThreadCache + CleanupStack）\n"
	code += fg.codegen.indentString()
	code += "KMM_V4_SCOPE_START {\n"
	fg.codegen.indent++
	// ===========================================
	
	// 生成参数处理并添加到符号表
	for _, param := range stmt.Params {
		code += fg.codegen.indentString()
		code += fmt.Sprintf("int64_t %s = arg;\n", param)
		// 添加参数到符号表
		fg.codegen.AddSymbol(param, "int64_t", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
	}
	
	// 生成函数体
	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}
	
	// ========== KMM Enhanced V4 作用域分配器出口 ==========
	fg.codegen.indent--
	code += fg.codegen.indentString()
	code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理结束（自动回收）\n"
	code += fg.codegen.indentString()
	code += "} KMM_V4_SCOPE_END;\n"
	// ===========================================
	
	// 添加默认返回语句（非 main 函数返回 0）
	code += fg.codegen.indentString() + "return 0;\n"
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
	
	// ========== KMM Enhanced V4 作用域分配器入口 ==========
	code += fg.codegen.indentString()
	code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理开始（Arena + ThreadCache + CleanupStack）\n"
	code += fg.codegen.indentString()
	code += "KMM_V4_SCOPE_START {\n"
	fg.codegen.indent++
	// ===========================================
	
	// 生成函数体
	for _, bodyStmt := range stmt.Body {
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}
	
	// ========== KMM Enhanced V4 作用域分配器出口 ==========
	fg.codegen.indent--
	code += fg.codegen.indentString()
	code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理结束（自动回收）\n"
	code += fg.codegen.indentString()
	code += "} KMM_V4_SCOPE_END;\n"
	// ===========================================
	
	// 添加默认返回语句
	code += fg.codegen.indentString() + "return 0;\n"
	fg.codegen.indent--
	code += "}\n"
	
	// 退出函数作用域
	fg.codegen.ExitScope()
	return code
}
