package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
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

	// 检查是否是前缀变量（以 $ 开头）
	if e.IsPrefixVar || strings.HasPrefix(e.Name, "$") {
		// 前缀变量：$device -> device（去掉 $ 前缀）
		// 在 generatePrefixCallBody 中已经通过参数设置了 device = 0
		varName := e.Name
		if strings.HasPrefix(varName, "$") {
			varName = varName[1:] // 去掉 $ 前缀
		}
		return varName
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
	case "EQ", "==":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " == " + right
	case "NE", "!=":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " != " + right
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
	case "SHIFT_LEFT", "<<":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " << " + right
	case "SHIFT_RIGHT", ">>":
		left := eg.GenerateExpression(e.Left)
		right := eg.GenerateExpression(e.Right)
		return left + " >> " + right
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
	
	// 追踪第三方库的使用
	if eg.codegen.stdlibConfig != nil {
		if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(funcName); isThirdParty {
			eg.codegen.usedThirdPartyLibs[lib.Name] = true
		}
	}
	
	// 直接使用标准库中定义的 println 函数
	if funcName == "println" {
		return eg.generatePrintlnCall(e.Args)
	}
	
	// 其他函数调用
	code := funcName + "("
	// 生成所有参数
	for i, arg := range e.Args {
		if i > 0 {
			code += ", "
		}
		argCode := eg.GenerateExpression(arg)
		code += argCode
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
		// 多级成员访问：std.io.println，methodName 是 "println"，nestedMember.Member 是 "io"
		moduleName = nestedMember.Member
	}
	
	if moduleName != "" && eg.codegen.stdlibConfig != nil {
		if module, exists := eg.codegen.stdlibConfig.Modules[moduleName]; exists {
			// 生成标准库函数调用
			funcName := methodName
			
			// 检查 stdlib.json 中是否有这个函数
			if _, funcExists := module.Functions[funcName]; funcExists {
				// 追踪第三方库的使用
				if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(funcName); isThirdParty {
					eg.codegen.usedThirdPartyLibs[lib.Name] = true
				}
				
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
// 支持类型推导自动判断格式化参数
func (eg *ExpressionGenerator) generatePrintlnCall(args []ast.Expression) string {
	if len(args) == 0 {
		return "printf(\"\\n\")"
	}

	// 检查第一个参数是否是字符串字面量
	if strLit, ok := args[0].(*ast.StringLiteral); ok {
		str := strLit.Value

		// 如果只有字符串参数且以 \n 结尾且没有格式符，使用 puts
		if len(args) == 1 && strings.HasSuffix(str, "\\n") && !strings.Contains(str, "%") {
			strClean := strings.TrimSuffix(str, "\\n")
			return fmt.Sprintf("puts(\"%s\")", strClean)
		}

		// 有多个参数或有格式符
		if len(args) == 1 {
			// 只有字符串，直接输出
			strClean := strings.TrimSuffix(str, "\\n")
			return fmt.Sprintf("printf(\"%s\\n\")", strClean)
		} else {
			// 类型推导：自动判断格式化参数
			return eg.generateTypeInferredPrintf(args)
		}
	}

	// 第一个参数不是字符串字面量，按普通方式处理
	if len(args) == 1 {
		argCode := eg.GenerateExpression(args[0])
		argType := eg.inferType(args[0])
		return fmt.Sprintf("printf(\"%%%s\\n\", %s)", argType, argCode)
	} else {
		// 类型推导处理多参数
		return eg.generateTypeInferredPrintf(args)
	}
}

// inferType 推导表达式的类型
func (eg *ExpressionGenerator) inferType(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return "d"
	case *ast.FloatLiteral:
		return "f"
	case *ast.StringLiteral:
		return "s"
	case *ast.Identifier:
		// 尝试从符号表获取类型
		sym := eg.codegen.symbolTable.GetSymbol(e.Name)
		if sym != nil {
			switch sym.Type {
			case "int", "int64", "int32":
				return "d"
			case "float", "float64", "float32":
				return "f"
			case "string":
				return "s"
			}
		}
		return "d" // 默认整数
	case *ast.BinaryExpression:
		// 二元表达式根据操作符推断类型
		if e.Operator == "+" || e.Operator == "-" || e.Operator == "*" || e.Operator == "/" {
			return "d"
		}
		return "d"
	default:
		return "d" // 默认整数
	}
}

// generateTypeInferredPrintf 生成带类型推导的 printf 调用
func (eg *ExpressionGenerator) generateTypeInferredPrintf(args []ast.Expression) string {
	if len(args) == 0 {
		return "printf(\"\\n\")"
	}

	strLit, isStrLit := args[0].(*ast.StringLiteral)
	var formatStr string
	var argStartIdx int

	if isStrLit {
		formatStr = strLit.Value
		argStartIdx = 1
	} else {
		// 第一个参数不是字符串，需要生成格式字符串
		formatStr = ""
		argStartIdx = 0
	}

	// 解析格式字符串中的格式说明符
	specifiers := eg.parseFormatSpecifiers(formatStr)
	expectedCount := len(specifiers)
	actualCount := len(args) - argStartIdx

	// 如果格式说明符数量与参数数量不匹配，或者没有格式说明符，自动推断
	if expectedCount != actualCount || expectedCount == 0 {
		// 自动生成格式字符串
		newFormat := ""
		for i := argStartIdx; i < len(args); i++ {
			if i > argStartIdx {
				newFormat += " "
			}
			argType := eg.inferType(args[i])
			newFormat += "%" + argType
		}
		if !strings.HasSuffix(newFormat, "\\n") {
			newFormat += "\\n"
		}
		formatStr = newFormat
		argStartIdx = 0 // 所有参数都作为格式化参数
	}

	// 生成 printf 调用
	code := "printf(\""
	if !isStrLit {
		// 需要先输出格式字符串
		code += formatStr + "\\n\", "
	} else {
		// 清理格式字符串并添加换行
		formatStr = strings.TrimSuffix(formatStr, "\\n")
		code += formatStr + "\\n\", "
	}

	for i := argStartIdx; i < len(args); i++ {
		if i > argStartIdx {
			code += ", "
		}
		code += eg.GenerateExpression(args[i])
	}
	code += ")"
	return code
}

// parseFormatSpecifiers 解析格式字符串中的说明符
func (eg *ExpressionGenerator) parseFormatSpecifiers(formatStr string) []string {
	specifiers := make([]string, 0)
	i := 0
	for i < len(formatStr) {
		if formatStr[i] == '%' {
			if i+1 < len(formatStr) {
				nextChar := formatStr[i+1]
				// 检查是否是转义字符 %%
				if nextChar == '%' {
					i += 2
					continue
				}
				// 收集格式说明符
				spec := "%"
				j := i + 1
				for j < len(formatStr) && !eg.isFormatSpecifierChar(formatStr[j]) {
					spec += string(formatStr[j])
					j++
				}
				if j < len(formatStr) {
					spec += string(formatStr[j])
					specifiers = append(specifiers, spec)
					i = j + 1
				} else {
					i++
				}
			} else {
				i++
			}
		} else {
			i++
		}
	}
	return specifiers
}

// isFormatSpecifierChar 判断是否是格式说明符字符
func (eg *ExpressionGenerator) isFormatSpecifierChar(c byte) bool {
	return c == 'd' || c == 'i' || c == 'u' || c == 'o' || c == 'x' || c == 'X' ||
		c == 'f' || c == 'F' || c == 'e' || c == 'E' || c == 'g' || c == 'G' ||
		c == 'c' || c == 's' || c == 'p' || c == 'n' || c == 'l' || c == 'h'
}

// isIdentifier 检查是否是标识符（变量）
func isIdentifier(code string) bool {
	// 匹配标识符（字母开头，后跟字母、数字或下划线）
	matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_]*$`, code)
	return matched
}

// generatePrefixCallExpression 生成前缀调用表达式代码（作为表达式返回空）
// 注意：PrefixCallExpression 应该作为语句处理，在 stmtgen.go 中处理
func (eg *ExpressionGenerator) generatePrefixCallExpression(e *ast.PrefixCallExpression) string {
	// 这个方法不应该被调用，因为 PrefixCallExpression 应该在语句层面处理
	return "// ERROR: PrefixCallExpression should be handled as a statement\n"
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
