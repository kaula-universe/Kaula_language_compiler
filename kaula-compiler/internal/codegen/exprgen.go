package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"regexp"
	"strings"
)

// escapeCString 转义字符串中的特殊字符，防止 C 字符串注入
func escapeCString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\x00", "\\0")
	return s
}

// escapeCIdentifier 转义 C 标识符中的特殊字符，防止代码注入
func escapeCIdentifier(s string) string {
	var builder strings.Builder
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			builder.WriteRune(ch)
		}
	}
	result := builder.String()
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}
	if result == "" {
		result = "_invalid"
	}
	return result
}

// isIntegerLiteral 检查字符串是否是整数常量
func isIntegerLiteral(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, ch := range s {
		if i == 0 && (ch == '-' || ch == '+') {
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
		// 通用泛型适配方案：处理泛型参数解析与类型实例化逻辑
		return eg.generateCallExpression(e)
	case *ast.IndexExpression:
		return eg.GenerateExpression(e.Object) + "[" + eg.GenerateExpression(e.Index) + "]"
	case *ast.PrefixCallExpression:
		return eg.generatePrefixCallExpression(e)
	case *ast.MemberAccessExpression:
		return eg.generateMemberAccessExpression(e)
	case *ast.TypeCastExpression:
		return eg.generateTypeCastExpression(e)
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
		if ident.Name == "int" || ident.Name == "i64" || ident.Name == "f64" || 
		   ident.Name == "double" || ident.Name == "float" || ident.Name == "bool" ||
		   ident.Name == "char" || ident.Name == "void" {
			// 这是一个变量声明
			// 检查右边是否是赋值表达式
			if binaryExpr, ok := e.Right.(*ast.BinaryExpression); ok && binaryExpr.Operator == "ASSIGN" {
				// int x = 10 被解析为: Binary(int, ?, Binary(x, ASSIGN, 10))
				varName := eg.GenerateExpression(binaryExpr.Left)
				value := eg.GenerateExpression(binaryExpr.Right)
				return eg.mapTypeToC(ident.Name) + " " + varName + " = " + value
			}
			// 处理只有类型的情况，如 int i
			return eg.mapTypeToC(ident.Name) + " " + eg.GenerateExpression(e.Right)
		}
	}
	
	// 处理赋值操作（已经处理，不应该再次进入这里）
	
	// 预先计算左右表达式，减少重复调用
	left := eg.GenerateExpression(e.Left)
	right := eg.GenerateExpression(e.Right)
	
	// 常量折叠：如果左右都是整数常量，直接在编译期计算
	if isIntegerLiteral(left) && isIntegerLiteral(right) {
		var leftVal, rightVal int64
		fmt.Sscanf(left, "%d", &leftVal)
		fmt.Sscanf(right, "%d", &rightVal)
		
		var result int64
		var hasResult bool
		
		switch e.Operator {
		case "PLUS":
			result = leftVal + rightVal
			hasResult = true
		case "MINUS":
			result = leftVal - rightVal
			hasResult = true
		case "MULTIPLY":
			result = leftVal * rightVal
			hasResult = true
		case "DIVIDE":
			if rightVal != 0 {
				result = leftVal / rightVal
				hasResult = true
			}
		case "MOD":
			if rightVal != 0 {
				result = leftVal % rightVal
				hasResult = true
			}
		case "EQ", "==":
			result = 1
			if leftVal != rightVal {
				result = 0
			}
			hasResult = true
		case "NE", "!=":
			result = 0
			if leftVal != rightVal {
				result = 1
			}
			hasResult = true
		case "LT", "<":
			result = 0
			if leftVal < rightVal {
				result = 1
			}
			hasResult = true
		case "GT", ">":
			result = 0
			if leftVal > rightVal {
				result = 1
			}
			hasResult = true
		case "LE", "<=":
			result = 0
			if leftVal <= rightVal {
				result = 1
			}
			hasResult = true
		case "GE", ">=":
			result = 0
			if leftVal >= rightVal {
				result = 1
			}
			hasResult = true
		case "AND", "&&":
			result = 0
			if leftVal != 0 && rightVal != 0 {
				result = 1
			}
			hasResult = true
		case "OR", "||":
			result = 0
			if leftVal != 0 || rightVal != 0 {
				result = 1
			}
			hasResult = true
		}
		
		if hasResult {
			return fmt.Sprintf("%d", result)
		}
	}
	
	// 生成正常的二元表达式
	switch e.Operator {
	case "ASSIGN":
		return left + " = " + right
	case "PLUS", "+":
		return left + " + " + right
	case "MINUS", "-":
		return left + " - " + right
	case "MULTIPLY", "*":
		return left + " * " + right
	case "DIVIDE", "/":
		return left + " / " + right
	case "MOD", "%":
		return left + " % " + right
	case "EQ", "==":
		return left + " == " + right
	case "NE", "!=":
		return left + " != " + right
	case "LT", "<":
		return left + " < " + right
	case "GT", ">":
		return left + " > " + right
	case "LE", "<=":
		return left + " <= " + right
	case "GE", ">=":
		return left + " >= " + right
	case "SHIFT_LEFT", "<<":
		return left + " << " + right
	case "SHIFT_RIGHT", ">>":
		return left + " >> " + right
	case "AND", "&&":
		return left + " && " + right
	case "OR", "||":
		return left + " || " + right
	default:
		// 对于未知的操作符，尝试使用原始符号
		return left + " " + e.Operator + " " + right
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

// generateCallExpression 生成函数调用表达式代码（支持泛型调用）
func (eg *ExpressionGenerator) generateCallExpression(e *ast.CallExpression) string {
	// 检查是否是方法调用，如 obj.method() 或 module.function()
	if memberAccess, ok := e.Function.(*ast.MemberAccessExpression); ok {
		return eg.generateMethodCall(memberAccess, e.Args)
	}
	
	funcName := eg.GenerateExpression(e.Function)
	
	// 通用泛型适配：如果存在类型参数，则触发实例化
	if len(e.TypeArgs) > 0 {
		// 触发泛型实例化
		code, err := eg.codegen.InstantiateGeneric(funcName, e.TypeArgs, e.Pos.Line)
		if err != nil {
			// 如果实例化失败，回退到简单拼接
			funcName = "kaula_" + funcName + "_" + strings.Join(e.TypeArgs, "_")
		} else if code != "" {
			// 实例化成功，在代码生成早期阶段注入实例化代码
			// 这里我们只返回实例化后的函数名
			funcName = "kaula_" + funcName + "_" + strings.Join(e.TypeArgs, "_")
		} else {
			// 已经实例化过，直接使用
			funcName = "kaula_" + funcName + "_" + strings.Join(e.TypeArgs, "_")
		}
	}
	
	// 避免与C标准库宏冲突（如 max, min）
	if funcName == "max" || funcName == "min" || funcName == "abs" {
		funcName = "kaula_" + funcName
	}
	
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
	
	// 根据参数数量选择不同的调用方式
	if len(e.Args) == 0 {
		// 无参数调用
		return funcName + "()"
	} else if len(e.Args) == 1 {
		// 单个参数调用，直接传递参数
		argCode := eg.GenerateExpression(e.Args[0])
		return funcName + "(" + argCode + ")"
	} else {
		// 多个参数调用，使用 C99 复合字面量 (compound literal) 传递数组
		// 语法: funcName((int64_t[]){arg1, arg2, ...}, arg_count)
		argsList := "(int64_t[]){"
		for i, arg := range e.Args {
			if i > 0 {
				argsList += ", "
			}
			argsList += eg.GenerateExpression(arg)
		}
		argsList += "}"
		
		return funcName + "(" + argsList + ", " + fmt.Sprintf("%d", len(e.Args)) + ")"
	}
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
		// 支持两种键格式: "io" 和 "std.io"
		stdlibKey := moduleName
		if !strings.HasPrefix(stdlibKey, "std.") {
			stdlibKey = "std." + moduleName
		}
		
		if module, exists := eg.codegen.stdlibConfig.Modules[stdlibKey]; exists {
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
	if len(args) == 1 {
		argCode := eg.GenerateExpression(args[0])
		switch methodName {
		case "add":
			return "int_object_add(" + object + ", " + argCode + ")"
		case "subtract":
			return "int_object_subtract(" + object + ", " + argCode + ")"
		case "multiply":
			return "int_object_multiply(" + object + ", " + argCode + ")"
		case "divide":
			return "int_object_divide(" + object + ", " + argCode + ")"
		case "concat":
			return "string_object_concat(" + object + ", " + argCode + ")"
		case "equals":
			return "object_equals((Object*)" + object + ", (Object*)" + argCode + ")"
		}
	}
	
	switch methodName {
	case "length":
		return "string_object_length(" + object + ")"
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
		return "putchar('\\n')"
	}

	// 检查第一个参数是否是字符串字面量
	if strLit, ok := args[0].(*ast.StringLiteral); ok {
		str := strLit.Value

		// 转义字符串中的特殊字符，防止 C 字符串注入
		strEscaped := escapeCString(strings.TrimSuffix(str, "\\n"))

		// 如果只有字符串参数且没有格式符，使用 puts (比 printf 更快)
		if len(args) == 1 && !strings.Contains(str, "%") {
			return fmt.Sprintf("puts(\"%s\")", strEscaped)
		}

		// 有多个参数或有格式符
		if len(args) == 1 {
			// 只有字符串，直接输出 (使用 puts 自动添加换行)
			return fmt.Sprintf("puts(\"%s\")", strEscaped)
		} else {
			// 类型推导：自动判断格式化参数
			return eg.generateTypeInferredPrintf(args)
		}
	}

	// 第一个参数不是字符串字面量，按普通方式处理
	if len(args) == 1 {
		argCode := eg.GenerateExpression(args[0])
		argType := eg.inferType(args[0])
		
		// 对于整数类型，使用更高效的 putchar/puts 组合
		if argType == "d" {
			// 检查是否是常量，如果是则直接输出
			if isIntegerLiteral(argCode) {
				return fmt.Sprintf("printf(\"%s\\n\")", argCode)
			}
		}
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

// generateTypeCastExpression 生成类型转换表达式代码
func (eg *ExpressionGenerator) generateTypeCastExpression(e *ast.TypeCastExpression) string {
	exprCode := eg.GenerateExpression(e.Expression)
	
	// 将 Kaula 类型映射到 C 类型
	cType := eg.mapTypeToC(e.TargetType)
	
	// 生成 C 风格的类型转换: (cType)(expr)
	return fmt.Sprintf("(%s)(%s)", cType, exprCode)
}

// mapTypeToC 将 Kaula 类型映射到 C 类型
func (eg *ExpressionGenerator) mapTypeToC(kaulaType string) string {
	// 标准化类型名称（转小写）
	typeLower := strings.ToLower(kaulaType)
	
	// 类型映射表
	typeMap := map[string]string{
		// 整数类型
		"i8":   "int8_t",
		"i16":  "int16_t",
		"i32":  "int32_t",
		"int8":   "int8_t",
		"int16":  "int16_t",
		"int32":  "int32_t",
		"int64":  "int64_t",
		"int":    "int64_t",
		"i64":    "int64_t",
		"long":   "int64_t",
		
		// 无符号整数类型
		"u8":   "uint8_t",
		"u16":  "uint16_t",
		"u32":  "uint32_t",
		"uint8":  "uint8_t",
		"uint16": "uint16_t",
		"uint32": "uint32_t",
		"uint64": "uint64_t",
		"uint":   "uint64_t",
		"u64":    "uint64_t",
		
		// 浮点类型
		"float":  "float",
		"f32":    "float",
		"double": "double",
		"f64":    "double",
		
		// 其他类型
		"bool":   "int",
		"char":   "char",
		"void":   "void",
	}
	
	if cType, ok := typeMap[typeLower]; ok {
		return cType
	}
	
	// 默认返回 int64_t
	return "int64_t"
}
