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
	
	// 检查函数是否有参数
	hasParams := len(stmt.Params) > 0
	
	// 生成函数签名（支持 inline 注解）
	code := ""
	if stmt.Inline {
		code += "__attribute__((always_inline)) "
	}
	code += "int64_t "
	code += stmt.Name
	if hasParams {
		code += "(int64_t arg) {\n"
	} else {
		code += "(void) {\n"
	}
	fg.codegen.indent++
	
	// ========== KMM Enhanced V4 作用域分配器入口 ==========
	// 如果指定了 no_kmm 注解，则不插入 KMM 内存管理代码
	if !stmt.NoKMM {
		code += fg.codegen.indentString()
		code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理开始\n"
		code += fg.codegen.indentString()
		code += "KMM_V4_SCOPE_START {\n"
		fg.codegen.indent++
	}
	// ===========================================
	
	// 生成参数处理并添加到符号表
	// 优化：直接使用 arg 作为参数名，避免拷贝，让编译器优化
	if len(stmt.Params) == 1 {
		paramName := stmt.Params[0]
		// 直接重命名参数为实际参数名，不使用宏定义
		code += fg.codegen.indentString()
		code += fmt.Sprintf("int64_t %s = arg;\n", paramName)
		// 添加参数到符号表
		fg.codegen.AddSymbol(paramName, "int64_t", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
	} else {
		// 多个参数或无参数，使用原有方式
		for i, param := range stmt.Params {
			if len(stmt.Params) > 1 {
				code += fg.codegen.indentString()
				code += fmt.Sprintf("int64_t %s = args[%d];\n", param, i)
			} else {
				code += fg.codegen.indentString()
				code += fmt.Sprintf("int64_t %s = arg;\n", param)
			}
			// 添加参数到符号表
			fg.codegen.AddSymbol(param, "int64_t", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
		}
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
	if !stmt.NoKMM {
		fg.codegen.indent--
		code += fg.codegen.indentString()
		code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理结束\n"
		code += fg.codegen.indentString()
		code += "} KMM_V4_SCOPE_END;\n"
		// ===========================================
	}
	
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
	// 生成 main 函数定义（支持 inline 注解）
	code := ""
	if stmt.Inline {
		code += "__attribute__((always_inline)) "
	}
	code += "int main() {\n"
	fg.codegen.indent++
	
	// ========== KMM Enhanced V4 作用域分配器入口 ==========
	// 如果指定了 no_kmm 注解，则不插入 KMM 内存管理代码
	if !stmt.NoKMM {
		code += fg.codegen.indentString()
		code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理开始\n"
		code += fg.codegen.indentString()
		code += "KMM_V4_SCOPE_START {\n"
		fg.codegen.indent++
	}
	// ===========================================
	
	// 生成函数体
	for _, bodyStmt := range stmt.Body {
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}
	
	// ========== KMM Enhanced V4 作用域分配器出口 ==========
	if !stmt.NoKMM {
		fg.codegen.indent--
		code += fg.codegen.indentString()
		code += "// KMM Enhanced V4 ScopedAllocator: 自动内存管理结束\n"
		code += fg.codegen.indentString()
		code += "} KMM_V4_SCOPE_END;\n"
		// ===========================================
	}
	
	// 添加默认返回语句
	code += fg.codegen.indentString() + "return 0;\n"
	fg.codegen.indent--
	code += "}\n"
	
	// 退出函数作用域
	fg.codegen.ExitScope()
	return code
}
