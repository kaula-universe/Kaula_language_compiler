package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

type FunctionGenerator struct {
	codegen *CodeGenerator
}

func NewFunctionGenerator(cg *CodeGenerator) *FunctionGenerator {
	return &FunctionGenerator{
		codegen: cg,
	}
}

func (fg *FunctionGenerator) GenerateFunctionStatement(stmt *ast.FunctionStatement) string {
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

	if stmt.Name == "main" {
		return fg.generateMainFunction(stmt)
	}

	hasTaskParams := len(stmt.TaskParams) > 0
	hasAsyncParams := len(stmt.AsyncParams) > 0

	var builder strings.Builder
	builder.Grow(1024)

	if stmt.Inline {
		builder.WriteString("__attribute__((always_inline)) ")
	}
	
	safeName := stmt.Name
	if safeName == "max" || safeName == "min" || safeName == "abs" {
		safeName = "kaula_" + safeName
	}
	
	if stmt.IsGeneric() {
		fg.codegen.ExitScope()
		return ""
	}
	
	returnType := fg.mapReturnType(stmt.ReturnType)
	builder.WriteString(returnType)
	builder.WriteString(safeName)
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

	if hasTaskParams {
		indent := fg.codegen.indentString()
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

		for i := range stmt.TaskParams {
			priorityCode := fg.codegen.expressionGenerator.GenerateExpression(stmt.TaskParams[i].Priority)
			builder.WriteString(indent)
			fmt.Fprintf(&builder, "// Task参数 %d: 优先级=%s (索引: %d)\n", i+1, priorityCode, i)
		}
		
		if len(stmt.TaskParams) > 0 {
			builder.WriteString(indent)
			for i := range stmt.TaskParams {
				paramName := fmt.Sprintf("task_param_%d", i)
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = ((int64_t*)tp->data)[%d];\n", paramName, i)
				fg.codegen.AddSymbol(paramName, "int64_t", false, "task_param", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

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
		builder.WriteString("if (arg == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("AsyncParam* ap = (AsyncParam*)arg;\n")
		builder.WriteString(indent)
		builder.WriteString("if (ap == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("void* async_value = ap->data;\n")

		for i := range stmt.AsyncParams {
			valueCode := fg.codegen.expressionGenerator.GenerateExpression(stmt.AsyncParams[i].Value)
			builder.WriteString(indent)
			fmt.Fprintf(&builder, "// Async参数 %d: 值=%s (索引: %d)\n", i+1, valueCode, i)
		}
		
		if len(stmt.AsyncParams) > 0 {
			builder.WriteString(indent)
			for i := range stmt.AsyncParams {
				paramName := fmt.Sprintf("async_param_%d", i)
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = ((int64_t*)ap->data)[%d];\n", paramName, i)
				fg.codegen.AddSymbol(paramName, "int64_t", false, "async_param", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
	} else {
		shouldUseKMM := !stmt.NoKMM && !stmt.Inline
		if shouldUseKMM {
			indent := fg.codegen.indentString()
			builder.WriteString(indent)
			builder.WriteString("KMM_V4_SCOPE_START {\n")
			fg.codegen.indent++
		}

		if len(stmt.Params) == 1 {
			paramName := stmt.Params[0]
			builder.WriteString(fg.codegen.indentString())
			fmt.Fprintf(&builder, "int64_t %s = arg;\n", paramName)
			fg.codegen.AddSymbol(paramName, "int64_t", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
		} else if len(stmt.Params) > 1 {
			indent := fg.codegen.indentString()
			for i, param := range stmt.Params {
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = args[%d];\n", param, i)
				fg.codegen.AddSymbol(param, "int64_t", false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

		indent := fg.codegen.indentString()
		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}

		if shouldUseKMM {
			fg.codegen.indent--
			indent := fg.codegen.indentString()
			builder.WriteString(indent)
			builder.WriteString("} KMM_V4_SCOPE_END;\n")
		}
	}

	if !hasReturnStatement(stmt.Body) && stmt.ReturnType != "" {
		builder.WriteString(fg.codegen.indentString())
		builder.WriteString("return 0;\n")
	}
	fg.codegen.indent--
	builder.WriteString("}\n")

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

func (fg *FunctionGenerator) generateMainFunction(stmt *ast.FunctionStatement) string {
	code := ""
	if stmt.Inline {
		code += "__attribute__((always_inline)) "
	}
	code += "int main() {\n"
	fg.codegen.indent++
	
	if !stmt.NoKMM {
		code += fg.codegen.indentString()
		code += "KMM_V4_SCOPE_START {\n"
		fg.codegen.indent++
	}
	
	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code += fg.codegen.indentString()
		code += fg.codegen.generateStatement(bodyStmt)
	}
	
	if !stmt.NoKMM {
		fg.codegen.indent--
		code += fg.codegen.indentString()
		code += "} KMM_V4_SCOPE_END;\n"
	}
	
	code += fg.codegen.indentString() + "return 0;\n"
	fg.codegen.indent--
	code += "}\n"
	
	fg.codegen.ExitScope()
	return code
}

func (fg *FunctionGenerator) mapReturnType(returnType string) string {
	if returnType == "" {
		return "void "
	}
	
	switch returnType {
	case "int":
		return "int "
	case "i64":
		return "int64_t "
	case "u64":
		return "uint64_t "
	case "i32":
		return "int32_t "
	case "u32":
		return "uint32_t "
	case "i16":
		return "int16_t "
	case "u16":
		return "uint16_t "
	case "i8":
		return "int8_t "
	case "u8":
		return "uint8_t "
	case "float":
		return "float "
	case "f32":
		return "float "
	case "double":
		return "double "
	case "f64":
		return "double "
	case "bool":
		return "int "
	case "char":
		return "char "
	case "void":
		return "void "
	case "string":
		return "char* "
	default:
		return returnType + " "
	}
}
