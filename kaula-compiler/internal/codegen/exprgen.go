package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

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
	// 特殊处理变量声明，如 int x = 10
	if ident, ok := e.Left.(*ast.Identifier); ok {
		if ident.Name == "int" {
			// 这是一个变量声明
			if binaryExpr, ok := e.Right.(*ast.BinaryExpression); ok && binaryExpr.Operator == "ASSIGN" {
				return "int " + eg.GenerateExpression(binaryExpr.Left) + " = " + eg.GenerateExpression(binaryExpr.Right)
			}
			// 处理只有类型的情况，如 int i
			return "int " + eg.GenerateExpression(e.Right)
		}
	}
	
	// 处理对象操作
	operator := e.Operator
	switch operator {
	case "ASSIGN":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " = " + right
	case "PLUS":
		return eg.generatePlusOperation(e.Left, e.Right)
	case "MINUS":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return "int_object_subtract(" + left + ", " + right + ")"
	case "MULTIPLY":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return "int_object_multiply(" + left + ", " + right + ")"
	case "DIVIDE":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return "int_object_divide(" + left + ", " + right + ")"
	case "EQ":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return "object_equals((Object*)" + left + ", (Object*)" + right + ")"
	case "NE":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return "!object_equals((Object*)" + left + ", (Object*)" + right + ")"
	case "LT", "GT", "LE", "GE":
		return "0"
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
	
	// 其他函数调用
	code := funcName + "("
	for i, arg := range e.Args {
		if i > 0 {
			code += ", "
		}
		code += eg.GenerateExpression(arg)
	}
	code += ")"
	return code
}

// generateMethodCall 生成方法调用代码
func (eg *ExpressionGenerator) generateMethodCall(memberAccess *ast.MemberAccessExpression, args []ast.Expression) string {
	object := eg.GenerateExpression(memberAccess.Object)
	methodName := memberAccess.Member
	
	// 检查是否是标准库模块调用
	if ident, ok := memberAccess.Object.(*ast.Identifier); ok {
		moduleName := ident.Name
		
		if eg.codegen.stdlibConfig != nil {
			if _, exists := eg.codegen.stdlibConfig.Modules[moduleName]; exists {
				// 生成标准库函数调用
				code := methodName + "("
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

// generatePrintlnCall 生成 println 调用代码
func (eg *ExpressionGenerator) generatePrintlnCall(args []ast.Expression) string {
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
			code := "printf(\"%s\\n\", " + arg + ")"
			return code
		}
	} else {
		code := ""
		for i, arg := range args {
			argExpr := eg.GenerateExpression(arg)
			if i > 0 {
				code += "printf(\" \");\n"
			}
			code += argExpr + ";\n"
		}
		code += "printf(\"\\n\")"
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
	
	if _, ok := e.Object.(*ast.Identifier); ok {
		return object + "->" + e.Member
	}
	
	return object + "." + e.Member
}
