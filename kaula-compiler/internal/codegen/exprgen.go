package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/comptime"
	"regexp"
	"strconv"
	"strings"
)

// escapeCString 转义字符串中的特殊字符，防止 C 字符串注入

// cOperatorPrecedence C 运算符优先级（值越大优先级越高）
var cOperatorPrecedence = map[string]int{
	"=": 1, "+=": 1, "-=": 1,
	"||": 2,
	"&&": 3,
	"|": 4,
	"^": 5,
	"&": 6,
	"==": 7, "!=": 7,
	"<": 8, ">": 8, "<=": 8, ">=": 8,
	"<<": 9, ">>": 9,
	"+": 10, "-": 10,
	"*": 11, "/": 11, "%": 11,
}

// 预排序的运算符列表（从长到短，避免短运算符误匹配长运算符的子串）
var sortedOps = []string{
	"==", "!=", "<=", ">=", "<<", ">>", "&&", "||",
	"+", "-", "*", "/", "%", "|", "^", "&", "<", ">",
}

// wrapIfNeeded 如果表达式是低优先级的二元表达式，用括号包裹
func wrapIfNeeded(expr string, op string, side string) string {
	if side != "left" {
		return expr
	}
	outerPrec := cOperatorPrecedence[op]
	// 快速跳过：简单标识符/字面量不需要检查
	if len(expr) == 0 {
		return expr
	}
	lastChar := expr[len(expr)-1]
	if (lastChar >= 'a' && lastChar <= 'z') || (lastChar >= 'A' && lastChar <= 'Z') ||
		(lastChar >= '0' && lastChar <= '9') || lastChar == '_' || lastChar == ')' {
		// 以标识符/数字/右括号结尾，可能包含运算符，需要检查
	}
	// 以分号、换行等结尾，不太可能是表达式
	if lastChar == ';' || lastChar == '\n' {
		return expr
	}
	for _, opChar := range sortedOps {
		pattern := " " + opChar + " "
		if strings.Contains(expr, pattern) {
			if cOperatorPrecedence[opChar] < outerPrec {
				return "(" + expr + ")"
			}
			break // 找到最高优先级的运算符就够了
		}
	}
	return expr
}
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
	codegen   *CodeGenerator
	typeCache map[ast.Expression]string // 表达式 → 推导类型缓存
	comptime  *comptime.Evaluator
}

// NewExpressionGenerator 创建一个新的表达式生成器
func NewExpressionGenerator(cg *CodeGenerator) *ExpressionGenerator {
	return &ExpressionGenerator{
		codegen:   cg,
		typeCache: make(map[ast.Expression]string),
		comptime:  comptime.NewEvaluator(),
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
		return strconv.FormatInt(e.Value, 10)
	case *ast.FloatLiteral:
		return strconv.FormatFloat(e.Value, 'f', -1, 64)
	case *ast.StringLiteral:
		return "\"" + e.Value + "\""
	case *ast.CharLiteral:
		return "'" + e.Value + "'"
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
	case *ast.TypeCastExpression:
		return eg.generateTypeCastExpression(e)
	case *ast.UnaryExpression:
		return eg.generateUnaryExpression(e)
	case *ast.SizeOfExpression:
		return eg.generateSizeOfExpression(e)
	case *ast.AlignOfExpression:
		return eg.generateAlignOfExpression(e)
	case *ast.OffsetOfExpression:
		return eg.generateOffsetOfExpression(e)
	case *ast.ComptimeExpression:
		return eg.generateComptimeExpression(e)
	case *ast.TypeNameExpression:
		return eg.generateTypeNameExpression(e)
	case *ast.FieldCountExpression:
		return eg.generateFieldCountExpression(e)
	case *ast.FieldNameExpression:
		return eg.generateFieldNameExpression(e)
	case *ast.FieldTypeExpression:
		return eg.generateFieldTypeExpression(e)
	case *ast.TypeKindExpression:
		return eg.generateTypeKindExpression(e)
	case *ast.ParenExpression:
		return "(" + eg.GenerateExpression(e.Inner) + ")"
	case *ast.ConditionalExpression:
		cond := eg.GenerateExpression(e.Condition)
		trueExpr := eg.GenerateExpression(e.TrueExpr)
		falseExpr := eg.GenerateExpression(e.FalseExpr)
		return "(" + cond + " ? " + trueExpr + " : " + falseExpr + ")"
	case *ast.ArrayLiteral:
		return eg.generateArrayLiteral(e)
	case *ast.LambdaExpression:
		return eg.GenerateLambdaExpression(e)
	case *ast.MatchExpression:
		return eg.generateMatchExpression(e)
	case *ast.AttributeExpression:
		return eg.generateAttributeExpression(e)
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

	// 编译期常量内联：如果在常量表中找到，直接返回字面量
	// 这实现了真正的编译期常量求值，const 变量引用会被替换为求值后的字面量
	if val, ok := eg.codegen.constTable[e.Name]; ok {
		return val
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
			if binaryExpr, ok := e.Right.(*ast.BinaryExpression); ok && binaryExpr.Operator == "ASSIGN" {
				varName := eg.GenerateExpression(binaryExpr.Left)
				value := eg.GenerateExpression(binaryExpr.Right)
				return eg.mapTypeToC(ident.Name) + " " + varName + " = " + value
			}
			return eg.mapTypeToC(ident.Name) + " " + eg.GenerateExpression(e.Right)
		}
	}
	
	// 处理赋值操作（已经处理，不应该再次进入这里）
	
	// 预先计算左右表达式，减少重复调用
	left := eg.GenerateExpression(e.Left)
	right := eg.GenerateExpression(e.Right)
	
	// 常量折叠：如果左右都是整数常量，直接在编译期计算
	if isIntegerLiteral(left) && isIntegerLiteral(right) {
		leftVal, _ := strconv.ParseInt(left, 10, 64)
		rightVal, _ := strconv.ParseInt(right, 10, 64)
		
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
		case "LSHIFT", "<<":
			result = leftVal << uint(rightVal)
			hasResult = true
		case "RSHIFT", ">>":
			result = leftVal >> uint(rightVal)
			hasResult = true
		case "XOR", "^", "BITWISE_XOR", "CARET":
			result = leftVal ^ rightVal
			hasResult = true
		}
		
		if hasResult {
			return strconv.FormatInt(result, 10)
		}
	}
	
	// 生成正常的二元表达式（直接字符串拼接，避免 fmt.Sprintf 开销）
	switch e.Operator {
	case "ASSIGN":
		return left + " = " + right
	case "PLUS", "+":
		return wrapIfNeeded(left, "+", "left") + " + " + right
	case "MINUS", "-":
		return wrapIfNeeded(left, "-", "left") + " - " + right
	case "MULTIPLY", "*":
		return wrapIfNeeded(left, "*", "left") + " * " + right
	case "DIVIDE", "/":
		return wrapIfNeeded(left, "/", "left") + " / " + right
	case "MOD", "%":
		return wrapIfNeeded(left, "%", "left") + " % " + right
	case "EQ", "==":
		return wrapIfNeeded(left, "==", "left") + " == " + right
	case "NE", "!=":
		return wrapIfNeeded(left, "!=", "left") + " != " + right
	case "LT", "<":
		return wrapIfNeeded(left, "<", "left") + " < " + right
	case "GT", ">":
		return wrapIfNeeded(left, ">", "left") + " > " + right
	case "LE", "<=":
		return wrapIfNeeded(left, "<=", "left") + " <= " + right
	case "GE", ">=":
		return wrapIfNeeded(left, ">=", "left") + " >= " + right
	case "SHIFT_LEFT", "<<", "LSHIFT":
		return wrapIfNeeded(left, "<<", "left") + " << " + right
	case "SHIFT_RIGHT", ">>", "RSHIFT":
		return wrapIfNeeded(left, ">>", "left") + " >> " + right
	case "AND", "&&":
		return wrapIfNeeded(left, "&&", "left") + " && " + right
	case "OR", "||":
		return wrapIfNeeded(left, "||", "left") + " || " + right
	case "BITWISE_AND", "AMPERSAND", "&":
		return wrapIfNeeded(left, "&", "left") + " & " + right
	case "BITWISE_OR", "PIPE", "|":
		return wrapIfNeeded(left, "|", "left") + " | " + right
	case "BITWISE_XOR", "CARET", "^", "XOR":
		return wrapIfNeeded(left, "^", "left") + " ^ " + right
	case "BITWISE_NOT", "TILDE", "~":
		return "~" + left
	default:
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
// 优化: 两个字面量在编译期拼接，运行时值用更高效的 printf
func (eg *ExpressionGenerator) generateStringConcat(strLiteral, other string) string {
	// 编译期拼接: 两个字符串字面量直接合并
	if strings.HasPrefix(other, "\"") && strings.HasSuffix(other, "\"") {
		inner1 := strLiteral[1 : len(strLiteral)-1]
		inner2 := other[1 : len(other)-1]
		return "\"" + inner1 + inner2 + "\""
	}

	// 运行时拼接: 根据 other 的类型选择格式符
	if strings.HasSuffix(other, "()") {
		if strings.HasPrefix(other, "system_get_os_name") {
			return "printf(\"%s%s\", " + strLiteral + ", " + other + ")"
		} else if strings.HasPrefix(other, "system_get_cpu_count") ||
			strings.HasPrefix(other, "system_get_total_memory") ||
			strings.HasPrefix(other, "system_get_available_memory") {
			return "printf(\"%s%zu\", " + strLiteral + ", " + other + ")"
		} else if strings.HasPrefix(other, "math_sin") ||
			strings.HasPrefix(other, "math_cos") ||
			strings.HasPrefix(other, "math_tan") ||
			strings.HasPrefix(other, "sin_pi") ||
			strings.HasPrefix(other, "cos_pi") ||
			strings.HasPrefix(other, "tan_pi") {
			return "printf(\"%s%f\", " + strLiteral + ", " + other + ")"
		}
	}
	return "printf(\"%s%s\", " + strLiteral + ", " + other + ")"
}

// generateCallExpression 生成函数调用表达式代码（支持泛型调用）
func (eg *ExpressionGenerator) generateCallExpression(e *ast.CallExpression) string {
	// 检查是否是方法调用，如 obj.method() 或 module.function()
	if memberAccess, ok := e.Function.(*ast.MemberAccessExpression); ok {
		return eg.generateMethodCall(memberAccess, e.Args)
	}

	funcName := eg.GenerateExpression(e.Function)

	// 自动将 std_malloc 重写为 kmm_v4_alloc_auto（当外层已有 KMM scope 时）
	// 这样内存会在作用域结束时自动回收，无需手动 free
	if eg.codegen.IsInKMMScope() && (funcName == "std_malloc" || funcName == "std.memory.std_malloc") {
		if len(e.Args) == 1 {
			sizeArg := eg.GenerateExpression(e.Args[0])
			return "kmm_v4_alloc_auto(" + sizeArg + ")"
		}
	}
	
	// 检查是否是结构体构造函数调用（无参数的类型名调用）
	if ident, ok := e.Function.(*ast.Identifier); ok && len(e.Args) == 0 && len(e.TypeArgs) == 0 {
		if eg.codegen.IsStructType(ident.Name) {
			return "(" + ident.Name + "){0}"
		}
	}
	
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
	} else {
		// 直接传递参数列表（支持任意数量参数）
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
}

// generateMethodCall 生成方法调用代码
func (eg *ExpressionGenerator) generateMethodCall(memberAccess *ast.MemberAccessExpression, args []ast.Expression) string {
	object := eg.GenerateExpression(memberAccess.Object)
	methodName := memberAccess.Member
	
	// 检查是否是标准库模块调用（如 std.io.println）
	// 处理多级成员访问：获取实际的模块名
	moduleName := ""
	isStdModuleCall := false
	if ident, ok := memberAccess.Object.(*ast.Identifier); ok {
		// 一级成员访问：io.println 或 std.println
		moduleName = ident.Name
	} else if nestedMember, ok := memberAccess.Object.(*ast.MemberAccessExpression); ok {
		// 多级成员访问：std.io.println，methodName 是 "println"，nestedMember.Member 是 "io"
		moduleName = nestedMember.Member
		// 检查是否是 std.module.function 模式
		if innerIdent, ok := nestedMember.Object.(*ast.Identifier); ok {
			if innerIdent.Name == "std" {
				isStdModuleCall = true
			}
		}
	}
	
	if moduleName != "" && eg.codegen.stdlibConfig != nil {
		// 支持两种键格式: "io" 和 "std.io"
		stdlibKey := moduleName
		if !strings.HasPrefix(stdlibKey, "std.") {
			stdlibKey = "std." + moduleName
		}
		
		if module, exists := eg.codegen.stdlibConfig.Modules[stdlibKey]; exists {
			// 特殊处理 println：使用类型推导版本
			if methodName == "println" && len(args) > 1 {
				return eg.generatePrintlnMulti(args)
			}

			// 检查 stdlib.json 中是否有这个函数
			if _, funcExists := module.Functions[methodName]; funcExists {
				// 追踪第三方库的使用
				if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(methodName); isThirdParty {
					eg.codegen.usedThirdPartyLibs[lib.Name] = true
				}
				
			// 使用 GetCFunctionName 自动添加模块前缀
			cFuncName := eg.codegen.stdlibConfig.GetCFunctionName(stdlibKey, methodName)

			// 自动将 std_malloc 重写为 kmm_v4_alloc_auto（当外层已有 KMM scope 时）
			if eg.codegen.IsInKMMScope() && (cFuncName == "std_malloc" || methodName == "std_malloc") {
				if len(args) == 1 {
					sizeArg := eg.GenerateExpression(args[0])
					return "kmm_v4_alloc_auto(" + sizeArg + ")"
				}
			}

			code := cFuncName + "("
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

	// 检查是否是本地导入的 pub 函数调用（如 utils.add(a, b)）
	if eg.codegen.localImportFuncs[methodName] {
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

	// 检查是否是第三方库函数调用（如 stb_image.stbi_load(...)）
	if ident, ok := memberAccess.Object.(*ast.Identifier); ok {
		if eg.codegen.stdlibConfig != nil {
			libName := ident.Name
			if lib := eg.codegen.stdlibConfig.GetThirdPartyLibrary(libName); lib != nil {
				if _, funcExists := lib.Functions[methodName]; funcExists {
					eg.codegen.usedThirdPartyLibs[libName] = true
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
		// 对于 std.module.function() 模式，直接调用函数
		if isStdModuleCall {
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
		return eg.generateObjectMethodCall(object, methodName, args)
	}
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
		strEscaped := escapeCString(strings.TrimSuffix(str, "\\n"))

		if len(args) == 1 && !strings.Contains(str, "%") {
			return "puts(\"" + strEscaped + "\")"
		}

		if len(args) == 1 {
			return "puts(\"" + strEscaped + "\")"
		} else {
			return eg.generatePrintlnMulti(args)
		}
	}

	// 第一个参数不是字符串字面量，按普通方式处理
	if len(args) == 1 {
		argCode := eg.GenerateExpression(args[0])
		argType := eg.inferType(args[0])

		if argType == "d" && isIntegerLiteral(argCode) {
			return "printf(\"" + argCode + "\\n\")"
		}
		return "printf(\"%" + argType + "\\n\", " + argCode + ")"
	} else {
		return eg.generatePrintlnMulti(args)
	}
}

// generatePrintlnMulti 生成 println_multi 调用代码
// 使用 println_multi(arg_count, type1, arg1, type2, arg2, ...)
func (eg *ExpressionGenerator) generatePrintlnMulti(args []ast.Expression) string {
	if len(args) == 0 {
		return "putchar('\\n')"
	}

	var b strings.Builder
	b.WriteString("println_multi(")
	b.WriteString(strconv.Itoa(len(args)))

	for _, arg := range args {
		argType := eg.inferType(arg)
		argCode := eg.GenerateExpression(arg)

		b.WriteString(", ")
		switch argType {
		case "d":
			b.WriteString("0, (int64_t)(")
			b.WriteString(argCode)
			b.WriteString(")")
		case "f":
			b.WriteString("1, ")
			b.WriteString(argCode)
		case "s":
			b.WriteString("2, ")
			b.WriteString(argCode)
		default:
			b.WriteString("0, (int64_t)(")
			b.WriteString(argCode)
			b.WriteString(")")
		}
	}

	b.WriteString(")")
	return b.String()
}

// inferType 推导表达式的类型（带缓存）
func (eg *ExpressionGenerator) inferType(expr ast.Expression) string {
	// 缓存命中：同一表达式指针直接返回上次推导结果
	if cached, ok := eg.typeCache[expr]; ok {
		return cached
	}
	result := eg.inferTypeUncached(expr)
	eg.typeCache[expr] = result
	return result
}

// inferTypeUncached 无缓存的类型推导实现
func (eg *ExpressionGenerator) inferTypeUncached(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return "d"
	case *ast.FloatLiteral:
		return "f"
	case *ast.StringLiteral:
		return "s"
	case *ast.BooleanLiteral:
		return "d"
	case *ast.Identifier:
		sym := eg.codegen.currentScope.GetSymbol(e.Name)
		if sym != nil {
			switch sym.Type {
			case "int", "int64", "int32", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64":
				return "d"
			case "float", "float64", "float32", "f32", "f64", "double":
				return "f"
			case "string":
				return "s"
			case "bool":
				return "d"
			default:
				t := strings.ToLower(sym.Type)
				if strings.HasPrefix(t, "i") || strings.HasPrefix(t, "u") || t == "int" || t == "int64" || t == "int32" {
					return "d"
				}
				if strings.HasPrefix(t, "f") || t == "float" || t == "double" {
					return "f"
				}
				if t == "string" || t == "char*" || strings.HasSuffix(t, "*") {
					return "s"
				}
				if t == "bool" {
					return "d"
				}
				cType := eg.codegen.typeGenerator.convertType(sym.Type, false)
				if strings.Contains(cType, "double") || strings.Contains(cType, "float") {
					return "f"
				}
				if strings.Contains(cType, "char*") {
					return "s"
				}
			}
		}
		return "d"
	case *ast.BinaryExpression:
		leftType := eg.inferType(e.Left)
		rightType := eg.inferType(e.Right)
		if leftType == "f" || rightType == "f" {
			return "f"
		}
		return "d"
	case *ast.UnaryExpression:
		operandType := eg.inferType(e.Right)
		if e.Operator == "!" {
			return "d"
		}
		return operandType
	case *ast.CallExpression:
		if ident, ok := e.Function.(*ast.Identifier); ok {
			if eg.codegen.stdlibConfig != nil {
				funcName := ident.Name
				for _, mod := range eg.codegen.stdlibConfig.Modules {
					if sig, ok := mod.Functions[funcName]; ok && sig.Return != "" {
						if sig.Return == "string" {
							return "s"
						}
						if sig.Return == "float" || sig.Return == "f64" || sig.Return == "f32" || sig.Return == "double" {
							return "f"
						}
						return "d"
					}
				}
			}
		}
		return "d"
	default:
		return "d"
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
	cType := eg.mapTypeToC(e.TargetType)
	return "(" + cType + ")(" + exprCode + ")"
}

// mapTypeToC 将 Kaula 类型映射到 C 类型
func (eg *ExpressionGenerator) mapTypeToC(kaulaType string) string {
	return MapKaulaTypeToC(kaulaType)
}

func (eg *ExpressionGenerator) generateSizeOfExpression(e *ast.SizeOfExpression) string {
	cType := eg.mapTypeToC(e.TargetType)
	return "sizeof(" + cType + ")"
}

func (eg *ExpressionGenerator) generateAlignOfExpression(e *ast.AlignOfExpression) string {
	cType := eg.mapTypeToC(e.TargetType)
	return "_Alignof(" + cType + ")"
}

func (eg *ExpressionGenerator) generateOffsetOfExpression(e *ast.OffsetOfExpression) string {
	cType := eg.mapTypeToC(e.TargetType)
	return "offsetof(" + cType + ", " + e.FieldName + ")"
}

func (eg *ExpressionGenerator) generateArrayLiteral(e *ast.ArrayLiteral) string {
	elems := make([]string, len(e.Elements))
	for i, elem := range e.Elements {
		elems[i] = eg.GenerateExpression(elem)
	}
	return "((int[]){ " + strings.Join(elems, ", ") + " })"
}

func (eg *ExpressionGenerator) generateComptimeExpression(e *ast.ComptimeExpression) string {
	val, err := eg.comptime.Eval(e)
	if err == nil {
		return eg.comptimeValueToC(val)
	}
	return eg.GenerateExpression(e.Inner)
}

func (eg *ExpressionGenerator) comptimeValueToC(val *comptime.Value) string {
	switch val.Kind {
	case comptime.KindInt:
		return fmt.Sprintf("%d", val.IntVal)
	case comptime.KindFloat:
		return fmt.Sprintf("%f", val.FloatVal)
	case comptime.KindBool:
		if val.BoolVal {
			return "true"
		}
		return "false"
	case comptime.KindString:
		return "\"" + escapeCString(val.StringVal) + "\""
	default:
		return "NULL"
	}
}

func (eg *ExpressionGenerator) generateTypeNameExpression(e *ast.TypeNameExpression) string {
	return "\"" + escapeCString(e.TargetType) + "\""
}

func (eg *ExpressionGenerator) generateFieldCountExpression(e *ast.FieldCountExpression) string {
	count := eg.getStructFieldCount(e.TargetType)
	return fmt.Sprintf("%d", count)
}

func (eg *ExpressionGenerator) generateFieldNameExpression(e *ast.FieldNameExpression) string {
	idx := eg.evalIntExpr(e.Index)
	if idx < 0 {
		return "\"\""
	}
	name := eg.getStructFieldName(e.TargetType, idx)
	return "\"" + escapeCString(name) + "\""
}

func (eg *ExpressionGenerator) generateFieldTypeExpression(e *ast.FieldTypeExpression) string {
	idx := eg.evalIntExpr(e.Index)
	if idx < 0 {
		return "\"\""
	}
	typ := eg.getStructFieldType(e.TargetType, idx)
	return "\"" + escapeCString(typ) + "\""
}

func (eg *ExpressionGenerator) generateTypeKindExpression(e *ast.TypeKindExpression) string {
	kind := eg.getTypeKind(e.TargetType)
	return "\"" + escapeCString(kind) + "\""
}

func (eg *ExpressionGenerator) getStructFieldCount(typeName string) int {
	if eg.codegen.program == nil {
		return 0
	}
	for _, stmt := range eg.codegen.program.Statements {
		if structStmt, ok := stmt.(*ast.StructStatement); ok {
			if structStmt.Name == typeName {
				return len(structStmt.Fields)
			}
		}
	}
	return 0
}

func (eg *ExpressionGenerator) getStructFieldName(typeName string, idx int) string {
	if eg.codegen.program == nil {
		return ""
	}
	for _, stmt := range eg.codegen.program.Statements {
		if structStmt, ok := stmt.(*ast.StructStatement); ok {
			if structStmt.Name == typeName {
				if idx >= 0 && idx < len(structStmt.Fields) {
					return structStmt.Fields[idx].Name
				}
				return ""
			}
		}
	}
	return ""
}

func (eg *ExpressionGenerator) getStructFieldType(typeName string, idx int) string {
	if eg.codegen.program == nil {
		return ""
	}
	for _, stmt := range eg.codegen.program.Statements {
		if structStmt, ok := stmt.(*ast.StructStatement); ok {
			if structStmt.Name == typeName {
				if idx >= 0 && idx < len(structStmt.Fields) {
					return structStmt.Fields[idx].Type
				}
				return ""
			}
		}
	}
	return ""
}

func (eg *ExpressionGenerator) getTypeKind(typeName string) string {
	switch typeName {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "int":
		return "int"
	case "f32", "f64", "float", "double":
		return "float"
	case "bool":
		return "bool"
	case "char":
		return "char"
	case "string":
		return "string"
	case "void":
		return "void"
	default:
		if eg.codegen.program != nil {
			for _, stmt := range eg.codegen.program.Statements {
				if structStmt, ok := stmt.(*ast.StructStatement); ok {
					if structStmt.Name == typeName {
						return "struct"
					}
				}
			}
		}
		return "unknown"
	}
}

func (eg *ExpressionGenerator) evalIntExpr(expr ast.Expression) int {
	if intLit, ok := expr.(*ast.IntegerLiteral); ok {
		return int(intLit.Value)
	}
	val, err := eg.comptime.Eval(expr)
	if err == nil && val.Kind == comptime.KindInt {
		return int(val.IntVal)
	}
	return -1
}

// generateUnaryExpression 生成一元表达式代码
func (eg *ExpressionGenerator) generateUnaryExpression(e *ast.UnaryExpression) string {
	right := eg.GenerateExpression(e.Right)
	switch e.Operator {
	case "&":
		return "&" + right
	case "!":
		return "!" + right
	case "-":
		return "-" + right
	default:
		return e.Operator + right
	}
}

// GenerateLambdaExpression 生成 lambda/闭包表达式的 C 代码
// 无捕获 lambda 在 C 中就是普通的静态函数指针
func (eg *ExpressionGenerator) GenerateLambdaExpression(expr *ast.LambdaExpression) string {
	// 为 lambda 生成唯一名称
	lambdaName := fmt.Sprintf("_kaula_lambda_%d", eg.codegen.lambdaCounter)
	eg.codegen.lambdaCounter++

	// 生成参数类型列表
	var cParamTypes []string
	for i, param := range expr.Params {
		paramType := "int64_t" // 默认类型
		if i < len(expr.ParamTypes) && expr.ParamTypes[i] != "auto" && expr.ParamTypes[i] != "" {
			paramType = eg.codegen.typeGenerator.convertType(expr.ParamTypes[i], false)
		}
		cParamTypes = append(cParamTypes, fmt.Sprintf("%s %s", paramType, param))
	}

	// 生成返回类型
	returnType := "void"
	if expr.ReturnType != "" {
		returnType = eg.codegen.typeGenerator.convertType(expr.ReturnType, false)
	}

	// 生成函数体
	var bodyCode strings.Builder
	for _, stmt := range expr.Body {
		bodyCode.WriteString(eg.codegen.statementGenerator.GenerateStatement(stmt))
	}

	// 构建完整函数定义，存入延迟输出队列
	var funcDef strings.Builder
	funcDef.WriteString(fmt.Sprintf("static %s %s(%s) {\n", returnType, lambdaName, strings.Join(cParamTypes, ", ")))
	funcDef.WriteString(bodyCode.String())
	funcDef.WriteString("}\n")

	// 存入 lambda 定义队列（在生成的 C 文件中函数定义之前输出）
	eg.codegen.lambdaDefinitions = append(eg.codegen.lambdaDefinitions, funcDef.String())

	// 返回函数指针
	return lambdaName
}

// generateMatchExpression 生成 match 表达式的 C 代码
// match 编译为 C 的 switch + 变体数据绑定
//
//	match(result) {
//	    Ok(value) => println(value)
//	    Err(msg) => println(msg)
//	}
//
// 编译为:
//
//	switch (result.kind) {
//	    case Result_Kind_Ok: {
//	        auto_type value = result.data.Ok_val;
//	        println(value);
//	        break;
//	    }
//	    case Result_Kind_Err: {
//	        auto_type msg = result.data.Err_val;
//	        println(msg);
//	        break;
//	    }
//	}
func (eg *ExpressionGenerator) generateMatchExpression(e *ast.MatchExpression) string {
	targetCode := eg.GenerateExpression(e.Target)

	var code strings.Builder
	code.WriteString("switch (")
	code.WriteString(targetCode)
	code.WriteString(".kind) {\n")

	// 尝试从目标表达式推断枚举类型名
	enumName := eg.inferEnumName(e.Target)

	for _, arm := range e.Arms {
		if arm.Pattern == nil {
			continue
		}

		switch arm.Pattern.Kind {
		case ast.PatternWildcard:
			// _ 通配符 → default
			code.WriteString("    default:\n")
			code.WriteString("    {\n")
			for _, bodyStmt := range arm.Body {
				code.WriteString("        ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("        break;\n")
			code.WriteString("    }\n")

		case ast.PatternVariant:
			// VariantName(x, y) → case Enum_Kind_VariantName: { auto_type x = ...; ... break; }
			caseLabel := "    case "
			if enumName != "" {
				caseLabel += enumName + "_Kind_"
			}
			caseLabel += arm.Pattern.VariantName + ":\n"
			code.WriteString(caseLabel)
			code.WriteString("    {\n")

			// 生成绑定变量
			if len(arm.Pattern.Bindings) > 0 {
				// 查找枚举变体的字段类型
				variant := eg.findEnumVariant(enumName, arm.Pattern.VariantName)
				for i, binding := range arm.Pattern.Bindings {
					fieldAccess := targetCode + ".data." + arm.Pattern.VariantName + "_val"
					if variant != nil && len(variant.FieldTypes) > 1 {
						// 多字段时使用具体字段名
						if i < len(variant.FieldNames) && variant.FieldNames[i] != "" {
							fieldAccess = targetCode + ".data." + variant.FieldNames[i]
						} else {
							// 多字段但无字段名时，使用 _val0, _val1 格式
							fieldAccess = fmt.Sprintf("%s.data.%s_val%d", targetCode, arm.Pattern.VariantName, i)
						}
					}
					code.WriteString(fmt.Sprintf("        auto_type %s = %s;\n", binding, fieldAccess))
				}
			}

			// 生成分支体
			for _, bodyStmt := range arm.Body {
				code.WriteString("        ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("        break;\n")
			code.WriteString("    }\n")

		case ast.PatternInteger:
			// 整数字面量模式
			code.WriteString(fmt.Sprintf("    case %d:\n", arm.Pattern.IntValue))
			code.WriteString("    {\n")
			for _, bodyStmt := range arm.Body {
				code.WriteString("        ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("        break;\n")
			code.WriteString("    }\n")

		case ast.PatternString:
			// 字符串字面量模式 - 在 C 中不能直接 switch 字符串，生成 if-else
			// 这里简单处理，在 switch 外面用 if 包裹
			code.WriteString("    /* string pattern: ")
			code.WriteString(arm.Pattern.StrValue)
			code.WriteString(" */\n")

		case ast.PatternBoolean:
			// true/false 模式
			code.WriteString("    case ")
			if arm.Pattern.VariantName == "true" {
				code.WriteString("1")
			} else {
				code.WriteString("0")
			}
			code.WriteString(":\n")
			code.WriteString("    {\n")
			for _, bodyStmt := range arm.Body {
				code.WriteString("        ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("        break;\n")
			code.WriteString("    }\n")

		case ast.PatternVariable:
			// 变量绑定 - 在 switch 中无法直接处理，生成注释
			code.WriteString("    /* variable pattern: ")
			code.WriteString(strings.Join(arm.Pattern.Bindings, ", "))
			code.WriteString(" */\n")
		}
	}

	code.WriteString("}\n")
	return code.String()
}

// inferEnumName 从目标表达式推断枚举类型名
func (eg *ExpressionGenerator) inferEnumName(expr ast.Expression) string {
	if ident, ok := expr.(*ast.Identifier); ok {
		// 从符号表查找变量类型
		sym := eg.codegen.GetSymbol(ident.Name)
		if sym != nil {
			// 检查类型是否是已定义的枚举
			if eg.codegen.program != nil {
				if enumStmt := eg.codegen.program.FindEnum(sym.Type); enumStmt != nil {
					return enumStmt.Name
				}
			}
			return sym.Type
		}
	}
	return ""
}

// findEnumVariant 查找枚举变体信息
func (eg *ExpressionGenerator) findEnumVariant(enumName, variantName string) *ast.EnumVariant {
	if eg.codegen.program == nil {
		return nil
	}
	enumStmt := eg.codegen.program.FindEnum(enumName)
	if enumStmt == nil {
		return nil
	}
	for _, v := range enumStmt.Variants {
		if v.Name == variantName {
			return v
		}
	}
	return nil
}

// generateAttributeExpression 生成表达式级属性的 C 代码
// 这是 Kaula 特殊操作的统一语法：asm/volatile/atomic/fence 等都通过此机制实现
// 语法: #[name(arg1, arg2, ...)]
// 支持的属性:
//   - #[asm("template", output, input, clobbers) ]: 内联汇编（GCC extended asm 风格）
//   - #[volatile_load(ptr)]: volatile 加载
//   - #[volatile_store(ptr, val)]: volatile 存储
//   - #[atomic_load(ptr)]: 原子加载
//   - #[atomic_store(ptr, val)]: 原子存储
//   - #[atomic_cas(ptr, expected, new)]: 原子比较交换，返回旧值
//   - #[atomic_faa(ptr, val)]: 原子 fetch-and-add，返回旧值
//   - #[fence()]: 内存屏障（全屏障）
func (eg *ExpressionGenerator) generateAttributeExpression(expr *ast.AttributeExpression) string {
	if expr.Attr == nil {
		return "0"
	}

	attr := expr.Attr
	args := attr.Args

	switch attr.Name {
	case "asm":
		// #[asm("template")] 或 #[asm("template", "output", "input", "clobbers")]
		// 简化版：直接生成 __asm__ __volatile__("...")
		if len(args) == 0 {
			eg.codegen.error("asm attribute requires at least a template string")
			return "0"
		}
		if len(args) == 1 {
			// 简单形式: #[asm("mov %cr3, %rax")]
			return fmt.Sprintf("__asm__ __volatile__(%s)", args[0])
		}
		// 扩展形式: 有输出/输入/破坏列表
		template := args[0]
		output := ""
		input := ""
		clobbers := ""
		if len(args) > 1 {
			output = args[1]
		}
		if len(args) > 2 {
			input = args[2]
		}
		if len(args) > 3 {
			clobbers = args[3]
		}
		// GCC extended asm 格式: asm volatile (template : output : input : clobbers)
		return fmt.Sprintf("({ __asm__ __volatile__(%s : %s : %s : %s); })",
			template, output, input, clobbers)

	case "volatile_load":
		// #[volatile_load(ptr)] - volatile 指针解引用读
		if len(args) < 1 {
			eg.codegen.error("volatile_load requires a pointer argument")
			return "0"
		}
		return fmt.Sprintf("(*(volatile typeof(*%s)*)(%s))", args[0], args[0])

	case "volatile_store":
		// #[volatile_store(ptr, val)] - volatile 指针解引用写
		if len(args) < 2 {
			eg.codegen.error("volatile_store requires pointer and value arguments")
			return "0"
		}
		return fmt.Sprintf("(*(volatile typeof(*%s)*)(%s) = (%s))", args[0], args[0], args[1])

	case "atomic_load":
		// #[atomic_load(ptr)] - 原子加载（seq_cst）
		if len(args) < 1 {
			eg.codegen.error("atomic_load requires a pointer argument")
			return "0"
		}
		return fmt.Sprintf("__atomic_load_n((%s), __ATOMIC_SEQ_CST)", args[0])

	case "atomic_store":
		// #[atomic_store(ptr, val)] - 原子存储（seq_cst）
		if len(args) < 2 {
			eg.codegen.error("atomic_store requires pointer and value arguments")
			return "0"
		}
		return fmt.Sprintf("(__atomic_store_n((%s), (%s), __ATOMIC_SEQ_CST), (%s))", args[0], args[1], args[1])

	case "atomic_cas":
		// #[atomic_cas(ptr, expected, new)] - 原子比较交换
		// 返回布尔值：true 表示成功
		if len(args) < 3 {
			eg.codegen.error("atomic_cas requires pointer, expected, and new arguments")
			return "0"
		}
		return fmt.Sprintf("__atomic_compare_exchange_n((%s), &(%s), (%s), 0, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST)",
			args[0], args[1], args[2])

	case "atomic_faa":
		// #[atomic_faa(ptr, val)] - 原子 fetch-and-add，返回旧值
		if len(args) < 2 {
			eg.codegen.error("atomic_faa requires pointer and value arguments")
			return "0"
		}
		return fmt.Sprintf("__atomic_fetch_add((%s), (%s), __ATOMIC_SEQ_CST)", args[0], args[1])

	case "fence":
		// #[fence()] - 全内存屏障
		return "__atomic_thread_fence(__ATOMIC_SEQ_CST)"

	default:
		eg.codegen.error(fmt.Sprintf("unknown attribute expression: #[%s]", attr.Name))
		return "0"
	}
}