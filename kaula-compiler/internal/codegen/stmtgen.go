package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/core"
	"strings"
)

// StatementGenerator 负责语句相关的代码生成
type StatementGenerator struct {
	codegen *CodeGenerator
}

// NewStatementGenerator 创建一个新的语句生成器
func NewStatementGenerator(cg *CodeGenerator) *StatementGenerator {
	return &StatementGenerator{
		codegen: cg,
	}
}

// GenerateStatement 生成语句代码
func (sg *StatementGenerator) GenerateStatement(stmt ast.Statement) string {
	// 首先尝试使用插件生成代码
	if code, ok := sg.codegen.pluginManager.GenerateStatement(stmt, sg.codegen); ok {
		return code
	}
	
	switch s := stmt.(type) {
	case *ast.VOStatement:
		return sg.generateVOStatement(s)
	case *ast.SpendCallStatement:
		return sg.generateSpendCallStatement(s)
	case *ast.TaskStatement:
		return sg.generateTaskStatement(s)
	case *ast.PrefixStatement:
		return sg.generatePrefixStatement(s)
	case *ast.TreeStatement:
		return sg.generateTreeStatement(s)
	case *ast.ObjectStatement:
		return sg.generateObjectStatement(s)
	case *ast.FunctionStatement:
		return sg.codegen.functionGenerator.GenerateFunctionStatement(s)
	case *ast.ClassStatement:
		return sg.codegen.typeGenerator.GenerateClassStatement(s)
	case *ast.InterfaceStatement:
		return sg.codegen.typeGenerator.GenerateInterfaceStatement(s)
	case *ast.StructStatement:
		return sg.codegen.typeGenerator.GenerateStructStatement(s)
	case *ast.IfStatement:
		return sg.generateIfStatement(s)
	case *ast.WhileStatement:
		return sg.generateWhileStatement(s)
	case *ast.ForStatement:
		return sg.generateForStatement(s)
	case *ast.SwitchStatement:
		return sg.generateSwitchStatement(s)
	case *ast.ReturnStatement:
		return sg.generateReturnStatement(s)
	case *ast.ImportStatement:
		return sg.generateImportStatement(s)
	case *ast.NonLocalStatement:
		return sg.generateNonLocalStatement(s)
	case *ast.VariableDeclaration:
		return sg.generateVariableDeclaration(s)
	case *ast.ExpressionStatement:
		// 检查是否是模块调用（MemberAccessExpression 作为 CallExpression 的函数部分）
		if callExpr, ok := s.Expression.(*ast.CallExpression); ok {
			if _, isMemberAccess := callExpr.Function.(*ast.MemberAccessExpression); isMemberAccess {
				// 这是模块函数调用，直接生成函数调用代码
				return sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
			}
		}
		// 其他表达式语句
		return sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
	case *ast.BlockStatement:
		return sg.generateBlockStatement(s)
	default:
		return ""
	}
}

// generateVariableDeclaration 生成变量声明代码
func (sg *StatementGenerator) generateVariableDeclaration(stmt *ast.VariableDeclaration) string {
	// 将变量添加到当前作用域的符号表
	sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local", stmt.Pos.Line, stmt.Pos.Column)
	
	var code string
	
	// 生成 C 风格的变量声明
	switch stmt.Type {
	case "int":
		code = "int " + stmt.Name
	case "float":
		code = "float " + stmt.Name
	case "double":
		code = "double " + stmt.Name
	case "bool":
		code = "bool " + stmt.Name
	case "char":
		code = "char " + stmt.Name
	case "string":
		code = "char* " + stmt.Name
	default:
		// 自定义类型
		code = stmt.Type + " " + stmt.Name
	}
	
	if stmt.Value != nil {
		code += " = " + sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
	} else if stmt.Nullable {
		// 对于可空类型，如果没有初始化值，初始化为 NULL
		code += " = NULL"
	}
	code += ";\n"
	return code
}

// generateVOStatement 生成 VO 语句代码
func (sg *StatementGenerator) generateVOStatement(stmt *ast.VOStatement) string {
	code := fmt.Sprintf("VO* vo = std_vo_create(%d);\n", sg.codegen.config.VOCacheSize)
	if stmt.Value != nil {
		code += "// Load data\n"
		code += "std_vo_data_load(vo, 0, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
		code += ");\n"
	}
	if stmt.Code != nil {
		code += "// Load code\n"
		code += "std_vo_code_load(vo, -1, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Code)
		code += ");\n"
	}
	// 处理 associate 操作
	code += "// Associate data and code\n"
	code += "std_vo_associate(vo, 0, -1);\n"
	if stmt.Access != nil {
		code += "// Access data\n"
		code += "void* result = std_vo_access(vo, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Access)
		code += ");\n"
	}
	code += "std_vo_destroy(vo);\n"
	return code
}

// generateSpendCallStatement 生成 spend/call 语句代码
func (sg *StatementGenerator) generateSpendCallStatement(stmt *ast.SpendCallStatement) string {
	code := fmt.Sprintf("Spendable* sp = spendable_create(%d);\n", sg.codegen.config.SpendableSize)
	if stmt.Spend != nil {
		code += "// Add components\n"
		code += "spendable_add(sp, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Spend)
		code += ");\n"
	}
	for i, callStmt := range stmt.Calls {
		code += "// Call component " + fmt.Sprintf("%d\n", i+1)
		code += "void* component = spendable_call(sp);\n"
		code += "// Process component\n"
		// 处理 call 语句的 body
		if len(callStmt.Body) > 0 {
			code += sg.codegen.indentString() + "{\n"
			sg.codegen.indent++
			for _, bodyStmt := range callStmt.Body {
				code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
			}
			sg.codegen.indent--
			code += sg.codegen.indentString() + "}\n"
		}
	}
	return code
}

// generateTaskStatement 生成 task 语句代码
func (sg *StatementGenerator) generateTaskStatement(stmt *ast.TaskStatement) string {
	code := fmt.Sprintf("PriorityQueue* pq = priority_queue_create(%d);\n", sg.codegen.config.QueueSize)
	code += "// Add task to priority queue\n"
	code += "priority_queue_add(pq, "
	code += fmt.Sprintf("%d", stmt.Priority)
	code += ", "
	if stmt.Func != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Func)
	} else {
		code += "NULL"
	}
	code += ", "
	if stmt.Arg != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Arg)
	} else {
		code += "NULL"
	}
	code += ");\n"
	code += "// Execute task\n"
	code += "priority_queue_execute_next(pq);\n"
	// 添加内存释放逻辑
	code += "// Cleanup\n"
	code += "// Note: PriorityQueue uses fast_alloc, no need to free\n"
	return code
}

// generatePrefixStatement 生成 prefix 语句代码
func (sg *StatementGenerator) generatePrefixStatement(stmt *ast.PrefixStatement) string {
	// 在 PrefixManager 中创建前缀上下文
	sg.codegen.prefixManager.CreatePrefix(stmt.Name)
	
	// 生成 C 代码，使用标准库中的前缀系统实现
	code := "PrefixSystem* prefix_system = prefix_system_create();\n"
	code += fmt.Sprintf("prefix_enter(\"%s\");\n", stmt.Name)
	
	// 生成前缀体内的代码
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}
	
	code += "prefix_leave();\n"
	code += "prefix_system_destroy(prefix_system);\n"
	return code
}

// generateTreeStatement 生成 tree 语句代码
func (sg *StatementGenerator) generateTreeStatement(stmt *ast.TreeStatement) string {
	// 在 TreeManager 中创建树结构
	if stmt.Root != nil {
		rootValue := sg.codegen.expressionGenerator.GenerateExpression(stmt.Root)
		rootNode := core.NewTreeNode(rootValue)
		sg.codegen.treeManager.AddNode(sg.codegen.treeManager.Root, rootNode)
	}
	
	// 生成 C 代码，使用简单的实现
	code := "// Tree structure implementation\n"
	code += "// Create a simple tree structure\n"
	
	// 创建根节点
	if stmt.Root != nil {
		code += "// Create root node\n"
		code += "int root_value = " + sg.codegen.expressionGenerator.GenerateExpression(stmt.Root) + ";\n"
		code += "// Print tree structure\n"
		code += "printf(\"Tree root value: %d\\n\", root_value);\n"
	}
	
	return code
}

// generateObjectStatement 生成 object 语句代码
func (sg *StatementGenerator) generateObjectStatement(stmt *ast.ObjectStatement) string {
	code := fmt.Sprintf("// Object: %s of type %s\n", stmt.Name, stmt.Type)
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for i := range stmt.Fields {
		code += fmt.Sprintf("    void* field%d;\n", i+1)
	}
	code += fmt.Sprintf("} %s;\n", stmt.Name)
	// 声明全局变量
	varName := stmt.Name + "_obj"
	code += fmt.Sprintf("%s* %s;\n", stmt.Name, varName)
	return code
}

// generateIfStatement 生成 if 语句代码
func (sg *StatementGenerator) generateIfStatement(stmt *ast.IfStatement) string {
	code := "if ("
	code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Condition)
	code += ") {\n"
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString()
		code += sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}"
	if len(stmt.Else) > 0 {
		code += " else {\n"
		sg.codegen.indent++
		for _, elseStmt := range stmt.Else {
			code += sg.codegen.indentString()
			code += sg.codegen.generateStatement(elseStmt)
		}
		sg.codegen.indent--
		code += sg.codegen.indentString() + "}"
	}
	code += "\n"
	return code
}

// generateWhileStatement 生成 while 语句代码
func (sg *StatementGenerator) generateWhileStatement(stmt *ast.WhileStatement) string {
	code := "while ("
	code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Condition)
	code += ") {\n"
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString()
		code += sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// generateForStatement 生成 for 语句代码
func (sg *StatementGenerator) generateForStatement(stmt *ast.ForStatement) string {
	code := "for ("
	if stmt.Init != nil {
		if exprStmt, ok := stmt.Init.(*ast.ExpressionStatement); ok {
			code += sg.codegen.expressionGenerator.GenerateExpression(exprStmt.Expression)
		} else {
			code += sg.codegen.generateStatement(stmt.Init)
			code = strings.TrimSuffix(code, ";\n")
		}
	} else {
		code += ""
	}
	code += "; "
	if stmt.Condition != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Condition)
	} else {
		code += ""
	}
	code += "; "
	if stmt.Update != nil {
		if exprStmt, ok := stmt.Update.(*ast.ExpressionStatement); ok {
			code += sg.codegen.expressionGenerator.GenerateExpression(exprStmt.Expression)
		} else {
			code += sg.codegen.generateStatement(stmt.Update)
			code = strings.TrimSuffix(code, ";\n")
		}
	} else {
		code += ""
	}
	code += ") {\n"
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString()
		code += sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// generateSwitchStatement 生成 switch 语句代码
func (sg *StatementGenerator) generateSwitchStatement(stmt *ast.SwitchStatement) string {
	code := "switch ("
	if stmt.Expression != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Expression)
	}
	code += ") {\n"
	sg.codegen.indent++
	// 生成 switch 语句体中的其他语句（如变量声明）
	for _, bodyStmt := range stmt.Statements {
		code += sg.codegen.indentString()
		code += sg.codegen.generateStatement(bodyStmt)
	}
	for _, caseStmt := range stmt.Cases {
		code += sg.codegen.indentString() + "case "
		code += sg.codegen.expressionGenerator.GenerateExpression(caseStmt.Value)
		code += ":\n"
		sg.codegen.indent++
		for _, bodyStmt := range caseStmt.Body {
			code += sg.codegen.indentString()
			code += sg.codegen.generateStatement(bodyStmt)
		}
		sg.codegen.indent--
	}
	if len(stmt.Default) > 0 {
		code += sg.codegen.indentString() + "default:\n"
		sg.codegen.indent++
		for _, bodyStmt := range stmt.Default {
			code += sg.codegen.indentString()
			code += sg.codegen.generateStatement(bodyStmt)
		}
		sg.codegen.indent--
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// generateReturnStatement 生成 return 语句代码
func (sg *StatementGenerator) generateReturnStatement(stmt *ast.ReturnStatement) string {
	code := "return "
	if stmt.Value != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
	} else {
		code += "NULL"
	}
	code += ";\n"
	return code
}

// generateImportStatement 生成 import 语句代码
func (sg *StatementGenerator) generateImportStatement(stmt *ast.ImportStatement) string {
	// import 语句在 C 中不需要特殊处理
	return ""
}

// generateNonLocalStatement 生成 nonlocal 语句代码
func (sg *StatementGenerator) generateNonLocalStatement(stmt *ast.NonLocalStatement) string {
	code := "// Non-local variable\n"
	code += stmt.Type + " " + stmt.Name
	if stmt.Value != nil {
		code += " = " + sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
	}
	code += ";\n"
	return code
}

// generateBlockStatement 生成块语句代码
func (sg *StatementGenerator) generateBlockStatement(stmt *ast.BlockStatement) string {
	// 进入块作用域
	sg.codegen.EnterScope("block")
	
	code := "{\n"
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Statements {
		code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
	}
	
	// 生成内存释放代码
	code += sg.codegen.indentString() + "// Free allocated memory\n"
	for name, symbol := range sg.codegen.currentScope.GetAllSymbols() {
		if symbol.Nullable {
			code += sg.codegen.indentString()
			if symbol.Type == "string" {
				code += "if (" + name + " != NULL) { free(" + name + "); }\n"
			} else if symbol.Type == "int" || symbol.Type == "float" || symbol.Type == "bool" {
				code += "if (" + name + " != NULL) { free(" + name + "); }\n"
			}
		}
	}
	
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	
	// 退出块作用域
	sg.codegen.ExitScope()
	return code
}
