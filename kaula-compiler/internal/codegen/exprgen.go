package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/stdlib"
	"regexp"
	"strings"
)

// isIntegerLiteral 检查是否是整数常量
func isIntegerLiteral(code string) bool {
	// 匹配整数常量（包括负数）
	matched, _ := regexp.MatchString(`^-?\d+$`, code)
	return matched
}

// ExpressionGenerator 负责表达式相关的代码生成
type ExpressionGenerator struct {
	codegen *CodeGenerator
}

// NewExpressionGenerator 创建一个新的表达式生成器
func NewExpressionGenerator(cg *CodeGenerator) *ExpressionGenerator {
	return &ExpressionGenerator{
		codegen: cg,
	}
}

// GenerateExpression 生成表达式代码
func (eg *ExpressionGenerator) GenerateExpression(expr ast.Expression) string {
	// 如果表达式为 nil，返回 0
	if expr == nil {
		return "0"
	}
	
	// 首先尝试使用插件生成代码
	if code, ok := eg.codegen.pluginManager.GenerateExpression(expr, eg.codegen); ok {
		return code
	}
	
	switch e := expr.(type) {
	case *ast.Identifier:
		return eg.generateIdentifier(e)
	case *ast.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *ast.FloatLiteral:
		return fmt.Sprintf("%f", e.Value)
	case *ast.StringLiteral:
		return fmt.Sprintf("\"%s\"", e.Value)
	case *ast.BooleanLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *ast.BinaryExpression:
		return eg.generateBinaryExpression(e)
	case *ast.CallExpression:
		return eg.generateCallExpression(e)
	case *ast.IndexExpression:
		return eg.GenerateExpression(e.Object) + "[" + eg.GenerateExpression(e.Index) + "]"
	case *ast.PrefixCallExpression:
		return eg.generatePrefixCallExpression(e)
	case *ast.MemberAccessExpression:
		return eg.generateMemberAccessExpression(e)
	default:
		return "0"
	}
}

// generateIdentifier 生成标识符代码
func (eg *ExpressionGenerator) generateIdentifier(e *ast.Identifier) string {
	// 检查是否是 null 关键字
	if e.Name == "null" {
		return "NULL"
	}
	
	// 检查当前作用域是否是构造函数或方法
	if strings.HasPrefix(eg.codegen.currentScope.GetScopeName(), "constructor") || 
	   strings.HasPrefix(eg.codegen.currentScope.GetScopeName(), "method_") {
		// 检查是否是 self 关键字
		if e.Name == "self" {
			return e.Name
		}
		// 检查是否是参数名
		if eg.codegen.currentScope.HasLocalSymbol(e.Name) {
			return e.Name
		}
		// 否则，假设是成员变量
		return "self->" + e.Name
	}
	
	return e.Name
}

// generateBinaryExpression 生成二元表达式代码
func (eg *ExpressionGenerator) generateBinaryExpression(e *ast.BinaryExpression) string {
	// 处理二元表达式操作
	operator := e.Operator
	switch operator {
	case "ASSIGN":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " = " + right
	case "PLUS":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " + " + right
	case "MINUS":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " - " + right
	case "MULTIPLY":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " * " + right
	case "DIVIDE":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " / " + right
	case "MOD":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " % " + right
	case "EQ":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		// 检查是否是整数比较
		if isIntegerLiteral(left) || isIntegerLiteral(right) {
			return left + " == " + right
		}
		return "object_equals((Object*)" + left + ", (Object*)" + right + ")"
	case "NE":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		// 检查是否是整数比较
		if isIntegerLiteral(left) || isIntegerLiteral(right) {
			return left + " != " + right
		}
		return "!object_equals((Object*)" + left + ", (Object*)" + right + ")"
	case "LT", "<":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " < " + right
	case "GT", ">":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " > " + right
	case "LE", "<=":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " <= " + right
	case "GE", ">=":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " >= " + right
	case "AND":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return "bool_object_and(" + left + ", " + right + ")"
	case "OR":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return "bool_object_or(" + left + ", " + right + ")"
	default:
		return eg.GenerateExpression(e.Left) + " " + operator + " " + eg.GenerateExpression(e.Right)
	}
}

// generatePlusOperation 生成加法操作代码
func (eg *ExpressionGenerator) generatePlusOperation(left, right ast.Expression) string {
	leftStr := eg.GenerateExpression(left)
	rightStr := eg.GenerateExpression(right)
	
	// 检查是否是字符串连接
	if strings.HasPrefix(leftStr, "\"") && strings.HasSuffix(leftStr, "\"") {
		return eg.generateStringConcat(leftStr, rightStr)
	} else if strings.HasPrefix(rightStr, "\"") && strings.HasSuffix(rightStr, "\"") {
		return eg.generateStringConcat(rightStr, leftStr)
	} else {
		// 假设是整数加法
		return "int_object_add(" + leftStr + ", " + rightStr + ")"
	}
}

// generateStringConcat 生成字符串连接代码
func (eg *ExpressionGenerator) generateStringConcat(strLiteral, other string) string {
	// 检查另一个操作数是否是函数调用
	if strings.HasPrefix(other, "system_get_os_name()") {
		return "printf(\"%s%s\", " + strLiteral + ", " + other + ")"
	} else if strings.HasPrefix(other, "system_get_cpu_count()") || 
	          strings.HasPrefix(other, "system_get_total_memory()") || 
	          strings.HasPrefix(other, "system_get_available_memory()") {
		return "printf(\"%s%zu\", " + strLiteral + ", " + other + ")"
	} else if strings.HasPrefix(other, "math_sin(") || 
	          strings.HasPrefix(other, "math_cos(") || 
	          strings.HasPrefix(other, "math_tan(") {
		return "printf(\"%s%f\", " + strLiteral + ", " + other + ")"
	} else if strings.HasPrefix(other, "sin_pi") || 
	          strings.HasPrefix(other, "cos_pi") || 
	          strings.HasPrefix(other, "tan_pi") {
		return "printf(\"%s%f\", " + strLiteral + ", " + other + ")"
	} else {
		return "printf(\"%s%s\", " + strLiteral + ", " + other + ")"
	}
}

// generateCallExpression 生成函数调用表达式代码
func (eg *ExpressionGenerator) generateCallExpression(e *ast.CallExpression) string {
	// 检查是否是方法调用，如 obj.method() 或 module.function()
	if memberAccess, ok := e.Function.(*ast.MemberAccessExpression); ok {
		return eg.generateMethodCall(memberAccess, e.Args)
	}
	
	funcName := eg.GenerateExpression(e.Function)
	
	// 直接使用标准库中定义的 println 函数
	if funcName == "println" {
		return eg.generatePrintlnCall(e.Args)
	}
	
	// 检查是否是第三方库函数
	if eg.codegen.stdlibConfig != nil {
		if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(funcName); isThirdParty {
			// 生成第三方库函数调用
			return eg.generateThirdPartyCall(funcName, e.Args, lib)
		}
	}
	
	// 其他函数调用
	code := funcName + "("
	// 如果没有参数，传递 NULL
	if len(e.Args) == 0 {
		code += "NULL"
	} else {
		// 传递第一个参数，并进行类型转换（假设只有一个参数）
		argCode := eg.GenerateExpression(e.Args[0])
		// 检查参数是否是整数类型或整数常量，如果是，需要转换为 void*
		if strings.HasPrefix(argCode, "i64") || strings.HasPrefix(argCode, "int") {
			code += "(void*)(intptr_t)" + argCode
		} else if isIntegerLiteral(argCode) {
			// 整数常量也需要转换
			code += "(void*)(intptr_t)(" + argCode + ")"
		} else {
			code += argCode
		}
	}
	code += ")"
	return code
}

// generateMethodCall 生成方法调用代码
func (eg *ExpressionGenerator) generateMethodCall(memberAccess *ast.MemberAccessExpression, args []ast.Expression) string {
	object := eg.GenerateExpression(memberAccess.Object)
	methodName := memberAccess.Member
	
	// 检查是否是标准库模块调用（如 std.io.println）
	// 处理多级成员访问：获取实际的模块名
	moduleName := ""
	if ident, ok := memberAccess.Object.(*ast.Identifier); ok {
		// 一级成员访问：io.println 或 std.println
		moduleName = ident.Name
	} else if nestedMember, ok := memberAccess.Object.(*ast.MemberAccessExpression); ok {
		// 多级成员访问：std.io.println，methodName 是 "println"
		// 模块名应该是 nestedMember.Member，即 "io"
		moduleName = nestedMember.Member
	}
	
	if moduleName != "" && eg.codegen.stdlibConfig != nil {
		if module, exists := eg.codegen.stdlibConfig.Modules[moduleName]; exists {
			// 生成标准库函数调用
			funcName := methodName
			
			// 检查 stdlib.json 中是否有这个函数
			if _, funcExists := module.Functions[funcName]; funcExists {
				code := funcName + "("
				for i, arg := range args {
					if i > 0 {
						code += ", "
					}
					code += eg.GenerateExpression(arg)
				}
				code += ")"
				return code
			}
		}
		
		// 检查是否是第三方库模块调用（如 zlib.compress）
		if lib := eg.codegen.stdlibConfig.GetThirdPartyLibrary(moduleName); lib != nil {
			if _, funcExists := lib.Functions[methodName]; funcExists {
				// 标记该第三方库已被使用
				eg.codegen.usedThirdPartyLibs[lib.Name] = true
				// 生成第三方库函数调用
				return eg.generateThirdPartyCall(methodName, args, lib)
			}
		}
	}
	
	// 处理基本类型的方法调用
	switch methodName {
	case "add":
		if len(args) == 1 {
			return "int_object_add(" + object + ", " + eg.GenerateExpression(args[0]) + ")"
		}
	case "subtract":
		if len(args) == 1 {
			return "int_object_subtract(" + object + ", " + eg.GenerateExpression(args[0]) + ")"
		}
	case "multiply":
		if len(args) == 1 {
			return "int_object_multiply(" + object + ", " + eg.GenerateExpression(args[0]) + ")"
		}
	case "divide":
		if len(args) == 1 {
			return "int_object_divide(" + object + ", " + eg.GenerateExpression(args[0]) + ")"
		}
	case "concat":
		if len(args) == 1 {
			return "string_object_concat(" + object + ", " + eg.GenerateExpression(args[0]) + ")"
		}
	case "length":
		return "string_object_length(" + object + ")"
	case "equals":
		if len(args) == 1 {
			return "object_equals((Object*)" + object + ", (Object*)" + eg.GenerateExpression(args[0]) + ")"
		}
	case "toString":
		return "object_to_string((Object*)" + object + ")"
	default:
		return eg.generateObjectMethodCall(object, methodName, args)
	}
	
	return ""
}

// generateThirdPartyCall 生成第三方库函数调用代码
func (eg *ExpressionGenerator) generateThirdPartyCall(funcName string, args []ast.Expression, lib *stdlib.ThirdPartyLibrary) string {
	// 检查该第三方库是否已被导入
	if !eg.codegen.usedThirdPartyLibs[lib.Name] {
		// 库未被导入，标记为已使用（兼容性考虑，仍然生成代码）
		eg.codegen.usedThirdPartyLibs[lib.Name] = true
	}
	
	// 获取函数定义
	funcDef, exists := lib.Functions[funcName]
	if !exists {
		// 函数不存在，返回空调用
		return funcName + "()"
	}
	
	// 生成函数调用
	code := funcName + "("
	
	// 如果没有参数且函数不需要参数
	if len(args) == 0 && len(funcDef.Args) == 0 {
		// 空参数列表
	} else {
		for i, arg := range args {
			if i > 0 {
				code += ", "
			}
			argCode := eg.GenerateExpression(arg)
			
			// 根据函数定义的类型进行转换
			if i < len(funcDef.Args) {
				expectedType := funcDef.Args[i]
				// 如果期望类型是整数类型，而参数是整数常量，需要转换
				if (expectedType == "int" || expectedType == "i64" || expectedType == "i32") && isIntegerLiteral(argCode) {
					code += "(" + expectedType + ")(" + argCode + ")"
				} else {
					code += argCode
				}
			} else {
				code += argCode
			}
		}
	}
	
	code += ")"
	return code
}

// generateObjectMethodCall 生成对象方法调用代码
func (eg *ExpressionGenerator) generateObjectMethodCall(object, methodName string, args []ast.Expression) string {
	className := ""
	
	// 尝试从符号表中获取类型
	// 这里 object 已经是字符串形式的表达式，无法直接推断类型
	// 暂时使用默认类名
	className = "Object"
	
	code := className + "_" + methodName + "("
	code += object
	
	for _, arg := range args {
		code += ", " + eg.GenerateExpression(arg)
	}
	code += ")"
	return code
}

// escapeCString 转义 C 字符串中的特殊字符
func escapeCString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// generatePrintlnCall 生成 println 调用代码
func (eg *ExpressionGenerator) generatePrintlnCall(args []ast.Expression) string {
	if len(args) == 0 {
		return "printf(\"\\n\")"
	}
	
	if len(args) == 1 {
		arg := eg.GenerateExpression(args[0])
		if strings.HasPrefix(arg, "printf(") {
			return arg + ";\nprintf(\"\\n\")"
		} else if strings.HasPrefix(arg, "string_object_create(") {
			start := strings.Index(arg, "(") + 1
			end := strings.LastIndex(arg, ")")
			if start > 0 && end > start {
				strContent := arg[start:end]
				code := "printf(" + strContent[:len(strContent)-1] + "\\n\")"
				return code
			}
		} else {
			// 检查是否是字符串字面量
			if strings.HasPrefix(arg, "\"") && strings.HasSuffix(arg, "\"") {
				// 字符串字面量，提取内容并转义
				strContent := arg[1 : len(arg)-1]
				escapedContent := escapeCString(strContent)
				code := "printf(\"" + escapedContent + "\\n\")"
				return code
			}
			// 其他类型，使用 %d 格式（假设是整数）
			code := "printf(\"%d\\n\", " + arg + ")"
			return code
		}
	} else {
		// 多参数处理：构建格式字符串和参数列表
		formatParts := []string{}
		argList := []string{}
		
		for _, arg := range args {
			argExpr := eg.GenerateExpression(arg)
			if strings.HasPrefix(argExpr, "\"") && strings.HasSuffix(argExpr, "\"") {
				// 字符串字面量，提取内容并转义
				strContent := argExpr[1 : len(argExpr)-1]
				escapedContent := escapeCString(strContent)
				formatParts = append(formatParts, escapedContent)
			} else {
				// 其他表达式，使用 %d 格式
				formatParts = append(formatParts, "%d")
				argList = append(argList, argExpr)
			}
		}
		
		formatStr := strings.Join(formatParts, " ") + "\\n"
		code := "printf(\"" + formatStr + "\""
		for _, arg := range argList {
			code += ", " + arg
		}
		code += ")"
		return code
	}
	
	return ""
}

// generatePrefixCallExpression 生成前缀调用表达式代码
func (eg *ExpressionGenerator) generatePrefixCallExpression(e *ast.PrefixCallExpression) string {
	code := "// Prefix call: " + e.Name + "\n"
	code += "prefix_enter(\"" + e.Name + "\");\n"
	for _, bodyStmt := range e.Body {
		code += eg.codegen.generateStatement(bodyStmt)
	}
	code += "prefix_leave();\n"
	return code
}

// generateMemberAccessExpression 生成成员访问表达式代码
func (eg *ExpressionGenerator) generateMemberAccessExpression(e *ast.MemberAccessExpression) string {
	object := eg.GenerateExpression(e.Object)
	
	if object == "self" {
		return object + "->" + e.Member
	}
	
	// 对于标识符，检查是否是 struct 类型（使用 .）还是指针类型（使用 ->）
	if ident, ok := e.Object.(*ast.Identifier); ok {
		// 检查符号表，确定是否是 struct 类型
		if sym := eg.codegen.symbolTable.GetSymbol(ident.Name); sym != nil {
			// 如果类型包含 *，使用 ->
			if strings.Contains(sym.Type, "*") {
				return object + "->" + e.Member
			}
			// 否则使用 .
			return object + "." + e.Member
		}
		// 默认使用 .
		return object + "." + e.Member
	}
	
	return object + "." + e.Member
}
