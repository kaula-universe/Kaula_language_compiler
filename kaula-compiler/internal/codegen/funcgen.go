package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
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

	annotation := stmt.GetAnnotation()

	if annotation == ast.TreeAnnotationPrefix || annotation == ast.TreeAnnotationPrefixTree {
		return fg.generatePrefixFunction(stmt)
	}

	if annotation == ast.TreeAnnotationTree {
		return fg.generateTreeFunction(stmt)
	}

	if annotation == ast.TreeAnnotationRoot || annotation == ast.TreeAnnotationRootTree {
		return fg.generateRootTreeFunction(stmt)
	}

	// 特殊处理 main 函数
	if stmt.Name == "main" {
		return fg.generateMainFunction(stmt)
	}

	// 检查是否有 task(优先级) 参数
	hasTaskParams := len(stmt.TaskParams) > 0
	// 检查是否有 async(值) 参数
	hasAsyncParams := len(stmt.AsyncParams) > 0

	// 使用 strings.Builder 提高字符串拼接性能
	var builder strings.Builder
	builder.Grow(1024) // 预分配初始容量

	if stmt.Inline {
		builder.WriteString("__attribute__((always_inline)) ")
	}
	builder.WriteString("int64_t ")
	builder.WriteString(stmt.Name)
	if hasTaskParams {
		builder.WriteString("_task(void* arg) {\n")
	} else if hasAsyncParams {
		builder.WriteString("_async(void* arg) {\n")
	} else if len(stmt.Params) > 1 {
		builder.WriteString("(int64_t* args, int arg_count) {\n")
	} else if len(stmt.Params) == 1 {
		builder.WriteString("(int64_t arg) {\n")
	} else {
		builder.WriteString("(void) {\n")
	}
	fg.codegen.indent++

	// 如果有任务参数，生成任务托管代码
	if hasTaskParams {
		indent := fg.codegen.indentString()
		builder.WriteString(indent)
		builder.WriteString("// Task托管：自动将函数提交到任务队列\n")
		builder.WriteString(indent)
		builder.WriteString("if (arg == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("TaskParam* tp = (TaskParam*)arg;\n")
		builder.WriteString(indent)
		builder.WriteString("if (tp == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("int priority = tp->priority;\n")
		builder.WriteString(indent)
		builder.WriteString("void* result = tp->data;\n")

		// 生成参数解包：从 TaskParam 结构中提取参数值
		for i, taskParam := range stmt.TaskParams {
			priorityCode := fg.codegen.expressionGenerator.GenerateExpression(taskParam.Priority)
			// 添加参数索引注释，便于调试
			builder.WriteString(indent)
			fmt.Fprintf(&builder, "// Task参数 %d: 优先级=%s (索引: %d)\n", i+1, priorityCode, i)
			_ = taskParam // suppress unused variable warning
		}
		
		// 为 TaskParams 生成局部变量声明和赋值
		if len(stmt.TaskParams) > 0 {
			builder.WriteString(indent)
			builder.WriteString("// Task参数解包\n")
			for i := range stmt.TaskParams {
				paramName := fmt.Sprintf("task_param_%d", i)
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = ((int64_t*)tp->data)[%d];\n", paramName, i)
				// 将参数添加到符号表
				fg.codegen.AddSymbol(paramName, "int64_t", false, "task_param", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

		// 生成函数体
		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
	} else if hasAsyncParams {
		indent := fg.codegen.indentString()
		builder.WriteString(indent)
		builder.WriteString("// Async托管：自动将函数提交到异步队列\n")
		builder.WriteString(indent)
		builder.WriteString("if (arg == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("AsyncParam* ap = (AsyncParam*)arg;\n")
		builder.WriteString(indent)
		builder.WriteString("if (ap == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("void* async_value = ap->data;\n")

		// 生成参数解包：从 AsyncParam 结构中提取参数值
		for i, asyncParam := range stmt.AsyncParams {
			valueCode := fg.codegen.expressionGenerator.GenerateExpression(asyncParam.Value)
			// 添加参数索引注释，便于调试
			builder.WriteString(indent)
			fmt.Fprintf(&builder, "// Async参数 %d: 值=%s (索引: %d)\n", i+1, valueCode, i)
			_ = asyncParam // suppress unused variable warning
		}
		
		// 为 AsyncParams 生成局部变量声明和赋值
		if len(stmt.AsyncParams) > 0 {
			builder.WriteString(indent)
			builder.WriteString("// Async参数解包\n")
			for i := range stmt.AsyncParams {
				paramName := fmt.Sprintf("async_param_%d", i)
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = ((int64_t*)ap->data)[%d];\n", paramName, i)
				// 将参数添加到符号表
				fg.codegen.AddSymbol(paramName, "int64_t", false, "async_param", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

		// 生成函数体
		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
	} else {
		// ========== KMM Enhanced V4 作用域分配器入口 ==========
		// 如果指定了 no_kmm 注解，则不插入 KMM 内存管理代码
		if !stmt.NoKMM {
			indent := fg.codegen.indentString()
			builder.WriteString(indent)
			builder.WriteString("// KMM Enhanced V4 ScopedAllocator: 自动内存管理开始\n")
			builder.WriteString(indent)
			builder.WriteString("KMM_V4_SCOPE_START {\n")
			fg.codegen.indent++
		}
		// ===========================================

		// 生成参数处理并添加到符号表
		if len(stmt.Params) == 1 {
			paramName := stmt.Params[0]
			builder.WriteString(fg.codegen.indentString())
			fmt.Fprintf(&builder, "int64_t %s = arg;\n", paramName)
			fg.codegen.AddSymbol(paramName, "int64_t", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
		} else if len(stmt.Params) > 1 {
			// 多个参数，使用数组解包
			indent := fg.codegen.indentString()
			for i, param := range stmt.Params {
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = args[%d];\n", param, i)
				fg.codegen.AddSymbol(param, "int64_t", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

		// 生成函数体
		indent := fg.codegen.indentString()
		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}

		// ========== KMM Enhanced V4 作用域分配器出口 ==========
		if !stmt.NoKMM {
			fg.codegen.indent--
			indent := fg.codegen.indentString()
			builder.WriteString(indent)
			builder.WriteString("// KMM Enhanced V4 ScopedAllocator: 自动内存管理结束\n")
			builder.WriteString(indent)
			builder.WriteString("} KMM_V4_SCOPE_END;\n")
			// ===========================================
		}
	}

	// 添加默认返回语句（仅当函数体中不包含 return 语句时）
	if !hasReturnStatement(stmt.Body) {
		builder.WriteString(fg.codegen.indentString())
		builder.WriteString("return 0;\n")
	}
	fg.codegen.indent--
	builder.WriteString("}\n")

	// 退出函数作用域
	fg.codegen.ExitScope()
	return builder.String()
}

func (fg *FunctionGenerator) generatePrefixFunction(stmt *ast.FunctionStatement) string {
	code := "// Prefix function: AST generation for cross-file reuse\n"
	code += fmt.Sprintf("int64_t %s", stmt.Name)

	if len(stmt.Params) > 0 {
		code += "(int64_t arg) {\n"
	} else {
		code += "(void) {\n"
	}

	fg.codegen.indent++

	code += fg.codegen.indentString()
	code += "// Enter prefix context: " + stmt.PrefixName + "\n"

	if stmt.PrefixName != "" {
		code += fg.codegen.indentString()
		code += fmt.Sprintf("prefix_enter(\"%s\");\n", stmt.PrefixName)
	}

	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}

	if stmt.PrefixName != "" {
		code += fg.codegen.indentString()
		code += "prefix_leave();\n"
	}

	fg.codegen.indent--
	code += "}\n"

	fg.codegen.ExitScope()
	return code
}

func (fg *FunctionGenerator) generateTreeFunction(stmt *ast.FunctionStatement) string {
	code := "// Tree function: AST generation with root validation\n"
	code += fmt.Sprintf("int64_t %s", stmt.Name)

	if len(stmt.Params) > 0 {
		code += "(int64_t arg) {\n"
	} else {
		code += "(void) {\n"
	}

	fg.codegen.indent++

	code += fg.codegen.indentString()
	code += "// Tree function body (validated against root tree)\n"

	rootTree := fg.codegen.treeManager.GetRootTree()
	if rootTree == nil {
		code += fg.codegen.indentString()
		code += "// ERROR: Tree function but no root tree defined\n"
	}

	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}

	fg.codegen.indent--
	code += "}\n"

	fg.codegen.ExitScope()
	return code
}

func (fg *FunctionGenerator) generateRootTreeFunction(stmt *ast.FunctionStatement) string {
	code := "// Root tree function: defines global tree structure\n"
	code += fmt.Sprintf("int64_t %s", stmt.Name)

	if len(stmt.Params) > 0 {
		code += "(int64_t arg) {\n"
	} else {
		code += "(void) {\n"
	}

	fg.codegen.indent++

	code += fg.codegen.indentString()
	code += "// Root tree function (all other trees must match this structure)\n"

	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}

	fg.codegen.indent--
	code += "}\n"

	fg.codegen.ExitScope()
	return code
}

// hasReturnStatement 检查函数体是否已包含 return 语句
func hasReturnStatement(stmts []ast.Statement) bool {
	for _, s := range stmts {
		if _, ok := s.(*ast.ReturnStatement); ok {
			return true
		}
		if block, ok := s.(*ast.BlockStatement); ok {
			if hasReturnStatement(block.Statements) {
				return true
			}
		}
		if ifStmt, ok := s.(*ast.IfStatement); ok {
			if hasReturnStatement(ifStmt.Body) || hasReturnStatement(ifStmt.Else) {
				return true
			}
		}
	}
	return false
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
