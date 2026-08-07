package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/comptime"
	"kaula-compiler/internal/stdlib"
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
	"|":  4,
	"^":  5,
	"&":  6,
	"==": 7, "!=": 7,
	"<": 8, ">": 8, "<=": 8, ">=": 8,
	"<<": 9, ">>": 9,
	"+": 10, "-": 10,
	"*": 11, "/": 11, "%": 11,
}

// kaulaOpToCOp 将 Kaula AST 运算符名映射为 C 运算符（用于优先级判断）
func kaulaOpToCOp(op string) string {
	switch op {
	case "PLUS", "+":
		return "+"
	case "MINUS", "-":
		return "-"
	case "MULTIPLY", "*":
		return "*"
	case "DIVIDE", "/":
		return "/"
	case "MOD", "%":
		return "%"
	case "EQ", "==":
		return "=="
	case "NE", "!=":
		return "!="
	case "LT", "<":
		return "<"
	case "GT", ">":
		return ">"
	case "LE", "<=":
		return "<="
	case "GE", ">=":
		return ">="
	case "LSHIFT", "SHIFT_LEFT", "<<":
		return "<<"
	case "RSHIFT", "SHIFT_RIGHT", ">>":
		return ">>"
	case "AND", "&&":
		return "&&"
	case "OR", "||":
		return "||"
	case "BITWISE_AND", "AMPERSAND", "&":
		return "&"
	case "BITWISE_OR", "PIPE", "|":
		return "|"
	case "BITWISE_XOR", "CARET", "^", "XOR":
		return "^"
	default:
		return ""
	}
}

// astBinaryNeedsParens 基于 AST 判断二元表达式的子表达式是否需要括号，
// 避免字符串匹配方式漏检（如 "a >> b & c" 中 '&' 优先级低于 '<<'）。
func astBinaryNeedsParens(child ast.Expression, outerOp string, side string) bool {
	bin, ok := child.(*ast.BinaryExpression)
	if !ok {
		return false
	}
	innerC := kaulaOpToCOp(bin.Operator)
	outerC := kaulaOpToCOp(outerOp)
	if innerC == "" || outerC == "" {
		return false
	}
	innerPrec := cOperatorPrecedence[innerC]
	outerPrec := cOperatorPrecedence[outerC]
	if side == "left" {
		return innerPrec < outerPrec
	}
	return innerPrec <= outerPrec
}

// 预排序的运算符列表（从长到短，避免短运算符误匹配长运算符的子串）
var sortedOps = []string{
	"==", "!=", "<=", ">=", "<<", ">>", "&&", "||",
	"+", "-", "*", "/", "%", "|", "^", "&", "<", ">",
}

// wrapIfNeeded 如果表达式是低优先级的二元表达式，用括号包裹
// side: "left" 表示左操作数，"right" 表示右操作数
func wrapIfNeeded(expr string, op string, side string) string {
	if len(expr) == 0 {
		return expr
	}
	outerPrec := cOperatorPrecedence[op]

	// 以分号、换行等结尾，不太可能是表达式
	lastChar := expr[len(expr)-1]
	if lastChar == ';' || lastChar == '\n' {
		return expr
	}

	// 检查是否包含需要括号的运算符
	for _, opChar := range sortedOps {
		pattern := " " + opChar + " "
		if strings.Contains(expr, pattern) {
			innerPrec := cOperatorPrecedence[opChar]
			if side == "left" {
				if innerPrec < outerPrec {
					return "(" + expr + ")"
				}
			} else {
				if innerPrec <= outerPrec {
					return "(" + expr + ")"
				}
			}
			break
		}
	}

	// 右操作数特殊处理：如果外层是位运算(&, |, ^)，内层有一元运算符(~, !)或移位(<<, >>)
	// 需要加括号，例如: value & ~(1 << bit)
	if side == "right" {
		if strings.Contains(expr, "~") || strings.Contains(expr, "<<") || strings.Contains(expr, ">>") {
			// 位运算的右操作数如果包含 ~ 或移位，需要加括号
			if op == "&" || op == "|" || op == "^" || op == "+" || op == "-" {
				return "(" + expr + ")"
			}
		}
	}

	return expr
}
// escapeCString 转义字符串为 C 字符串字面量
// 词法器保留 Kaula 转义序列的原始形式（\" \n \\ 等），这些序列透传到 C
// 源码中与 Kaula 语义完全一致（\" 仍是转义引号、\n 仍是换行），
// 因此只处理源码中不可能出现、但 C 里必须转义的真实控制字符。
func escapeCString(s string) string {
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
		return strconv.FormatUint(e.Value, 10)
	case *ast.FloatLiteral:
		return strconv.FormatFloat(e.Value, 'f', -1, 64)
	case *ast.StringLiteral:
		escaped := escapeCString(e.Value)
		return fmt.Sprintf("((String){.len=%d, .ptr=\"%s\"})", len(e.Value), escaped)
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
		objType := eg.inferType(e.Object)
		if objType == "s" {
			return eg.GenerateExpression(e.Object) + ".ptr[" + eg.GenerateExpression(e.Index) + "]"
		}
		if eg.isObjectTyped(e.Object) {
			// 动态对象下标读取 obj["key"] → dynobj_get(obj, key)
			eg.codegen.needsObjRuntime = true
			return "dynobj_get(" + eg.GenerateExpression(e.Object) + ", " + eg.objectKeyCode(e.Index) + ")"
		}
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
	case *ast.ObjectLiteral:
		// 动态对象字面量 object { name: value, ... } / object()
		return eg.generateObjectLiteral(e)
	case *ast.AttributeExpression:
		return eg.generateAttributeExpression(e)
	case *ast.StructLiteral:
		return eg.generateStructLiteral(e)
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

	// 检查当前是否在类方法/构造函数作用域内（两种检测方式，互为兜底）
	inClassScope := eg.codegen.currentClassName != "" ||
		strings.HasPrefix(eg.codegen.currentScope.GetScopeName(), "constructor") ||
		strings.HasPrefix(eg.codegen.currentScope.GetScopeName(), "method_")

	if inClassScope {
		// 检查是否是 self 关键字
		if e.Name == "self" {
			return e.Name
		}
		// 检查是否是参数名或局部变量名
		if eg.codegen.currentScope.HasLocalSymbol(e.Name) {
			return e.Name
		}
		// 内置函数名/全局符号不是成员变量
		switch e.Name {
		case "println", "print", "read_int", "read_line", "read_string", "read_bool",
			"read_float", "read_char", "string_create", "string_to_int",
			"string_concat", "string_equals", "string_free", "kmm_v4_alloc_auto":
			return e.Name
		}
		if eg.codegen.GetSymbol(e.Name) != nil {
			// 全局符号(函数/变量/类型)不是成员变量
			return e.Name
		}
		// 检查是否属于当前类字段（如果能确定当前类）
		className := eg.codegen.currentClassName
		if className == "" {
			// 从 scopeName 中解析类名：method_Class_methodName 或 constructor_Class
			scopeName := eg.codegen.currentScope.GetScopeName()
			if strings.HasPrefix(scopeName, "method_") {
				rest := scopeName[len("method_"):] // Class_methodName
				// 取下划线之前的部分作为类名（注意类名本身不含下划线时安全）
				if idx := strings.LastIndex(rest, "_"); idx > 0 {
					className = rest[:idx]
				}
			} else if strings.HasPrefix(scopeName, "constructor_") {
				className = scopeName[len("constructor_"):]
			}
		}
		if className != "" && eg.isClassField(className, e.Name) {
			return "self->" + e.Name
		}
		// 即使无法确定当前类，也保守地假定为 self 字段（类作用域内的标识符通常是字段）
		// 这是之前行为的兜底，防止遗漏
		if className == "" {
			return "self->" + e.Name
		}
	}

	// 检查是否是枚举变体
	if sym := eg.codegen.GetSymbol(e.Name); sym != nil && strings.HasPrefix(sym.Type, "enum_variant:") {
		enumName := strings.TrimPrefix(sym.Type, "enum_variant:")
		return enumName + "_Kind_" + e.Name
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

	// 动态对象字段写入 obj.field = value → dynobj_set(obj, "field", box(value))
	// 必须在预先计算左右表达式之前检查，避免 value 被生成两次（如 lambda 表达式）
	if e.Operator == "ASSIGN" || e.Operator == "=" {
		if memberAccess, ok := e.Left.(*ast.MemberAccessExpression); ok && eg.isObjectTyped(memberAccess.Object) {
			eg.codegen.needsObjRuntime = true
			return "dynobj_set(" + eg.GenerateExpression(memberAccess.Object) + ", \"" + escapeCString(memberAccess.Member) + "\", " + eg.boxDynamicValue(e.Right) + ")"
		}
		if indexExpr, ok := e.Left.(*ast.IndexExpression); ok && eg.isObjectTyped(indexExpr.Object) {
			eg.codegen.needsObjRuntime = true
			return "dynobj_set(" + eg.GenerateExpression(indexExpr.Object) + ", " + eg.objectKeyCode(indexExpr.Index) + ", " + eg.boxDynamicValue(e.Right) + ")"
		}
	}

	// 预先计算左右表达式
	left := eg.GenerateExpression(e.Left)
	right := eg.GenerateExpression(e.Right)

	// AST 级括号修正：防止 "a >> b & c" 这类字符串匹配漏检导致的优先级错误
	if astBinaryNeedsParens(e.Left, e.Operator, "left") {
		left = "(" + left + ")"
	}
	if astBinaryNeedsParens(e.Right, e.Operator, "right") {
		right = "(" + right + ")"
	}

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
	case "ASSIGN", "=":
		return left + " = " + right
	case "PLUS", "+":
		return eg.generatePlusOperation(e.Left, e.Right)
	case "MINUS", "-":
		return wrapIfNeeded(left, "-", "left") + " - " + wrapIfNeeded(right, "-", "right")
	case "MULTIPLY", "*":
		return wrapIfNeeded(left, "*", "left") + " * " + wrapIfNeeded(right, "*", "right")
	case "DIVIDE", "/":
		return wrapIfNeeded(left, "/", "left") + " / " + wrapIfNeeded(right, "/", "right")
	case "MOD", "%":
		return wrapIfNeeded(left, "%", "left") + " % " + wrapIfNeeded(right, "%", "right")
	case "EQ", "==":
		leftType := eg.inferType(e.Left)
		rightType := eg.inferType(e.Right)
		if (leftType == "cstr" && rightType == "s") || (leftType == "s" && rightType == "cstr") {
			return "strcmp(" + left + ", " + right + ".ptr) == 0"
		}
		if leftType == "cstr" && rightType == "cstr" {
			return "strcmp(" + left + ", " + right + ") == 0"
		}
		return wrapIfNeeded(left, "==", "left") + " == " + wrapIfNeeded(right, "==", "right")
	case "NE", "!=":
		leftType := eg.inferType(e.Left)
		rightType := eg.inferType(e.Right)
		if (leftType == "cstr" && rightType == "s") || (leftType == "s" && rightType == "cstr") {
			return "strcmp(" + left + ", " + right + ".ptr) != 0"
		}
		if leftType == "cstr" && rightType == "cstr" {
			return "strcmp(" + left + ", " + right + ") != 0"
		}
		return wrapIfNeeded(left, "!=", "left") + " != " + wrapIfNeeded(right, "!=", "right")
	case "LT", "<":
		return wrapIfNeeded(left, "<", "left") + " < " + wrapIfNeeded(right, "<", "right")
	case "GT", ">":
		return wrapIfNeeded(left, ">", "left") + " > " + wrapIfNeeded(right, ">", "right")
	case "LE", "<=":
		return wrapIfNeeded(left, "<=", "left") + " <= " + wrapIfNeeded(right, "<=", "right")
	case "GE", ">=":
		return wrapIfNeeded(left, ">=", "left") + " >= " + wrapIfNeeded(right, ">=", "right")
	case "SHIFT_LEFT", "<<", "LSHIFT":
		return wrapIfNeeded(left, "<<", "left") + " << " + wrapIfNeeded(right, "<<", "right")
	case "SHIFT_RIGHT", ">>", "RSHIFT":
		return wrapIfNeeded(left, ">>", "left") + " >> " + wrapIfNeeded(right, ">>", "right")
	case "AND", "&&":
		return wrapIfNeeded(left, "&&", "left") + " && " + wrapIfNeeded(right, "&&", "right")
	case "OR", "||":
		return wrapIfNeeded(left, "||", "left") + " || " + wrapIfNeeded(right, "||", "right")
	case "BITWISE_AND", "AMPERSAND", "&":
		return wrapIfNeeded(left, "&", "left") + " & " + wrapIfNeeded(right, "&", "right")
	case "BITWISE_OR", "PIPE", "|":
		return wrapIfNeeded(left, "|", "left") + " | " + wrapIfNeeded(right, "|", "right")
	case "BITWISE_XOR", "CARET", "^", "XOR":
		return wrapIfNeeded(left, "^", "left") + " ^ " + wrapIfNeeded(right, "^", "right")
	case "BITWISE_NOT", "TILDE", "~":
		return "~" + left
	default:
		return left + " " + e.Operator + " " + right
	}
}

// generatePlusOperation 生成加法操作代码
func (eg *ExpressionGenerator) generatePlusOperation(left, right ast.Expression) string {
	leftType := eg.inferType(left)
	rightType := eg.inferType(right)
	leftStr := eg.GenerateExpression(left)
	rightStr := eg.GenerateExpression(right)

	// 编译期字符串字面量拼接
	if leftType == "s" && rightType == "s" {
		if leftLit, ok := left.(*ast.StringLiteral); ok {
			if rightLit, ok := right.(*ast.StringLiteral); ok {
				merged := leftLit.Value + rightLit.Value
				return fmt.Sprintf("((String){.len=%d, .ptr=\"%s\"})", len(merged), escapeCString(merged))
			}
		}
	}

	// 字符串连接（运行时）
	if leftType == "s" || rightType == "s" {
		return "string_concat(" + leftStr + ", " + rightStr + ")"
	}

	// 动态对象整数加法（obj 类型装箱的整数）
	if leftType == "obj" || rightType == "obj" {
		return "int_object_add(" + leftStr + ", " + rightStr + ")"
	}

	// 浮点加法
	if leftType == "f" || rightType == "f" {
		return leftStr + " + " + rightStr
	}

	// 原始整数加法
	return leftStr + " + " + rightStr
}

// generateCallExpression 生成函数调用表达式代码（支持泛型调用）
func (eg *ExpressionGenerator) generateCallExpression(e *ast.CallExpression) string {
	// 检查是否是方法调用，如 obj.method() 或 module.function()
	if memberAccess, ok := e.Function.(*ast.MemberAccessExpression); ok {
		return eg.generateMethodCall(memberAccess, e.Args)
	}

	funcName := eg.GenerateExpression(e.Function)

	// 修复 #21：全局将 std_malloc 重写为 kmm_v4_alloc_auto
	// 不再依赖 IsInKMMScope() 判断，统一所有动态分配走 KMM pool
	// 作用域内的分配由 scope_pop 自动回收，作用域外的分配由 thread heap refill 回收
	if funcName == "std_malloc" || funcName == "std.memory.std_malloc" {
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

	// 检查是否是类构造函数调用：ClassName(args) → ClassName_new(args)
	// 同时处理无参的 ClassName() 情况（如果类有默认构造函数或无参构造）
	if ident, ok := e.Function.(*ast.Identifier); ok && len(e.TypeArgs) == 0 {
		if eg.codegen.IsClassType(ident.Name) {
			ctorFuncName := ident.Name + "_new"
			if len(e.Args) == 0 {
				return ctorFuncName + "()"
			}
			var sig *stdlib.Function
			if eg.codegen.stdlibConfig != nil {
				sig = eg.codegen.stdlibConfig.GetAnyFunctionSignature(ctorFuncName)
			}
			return ctorFuncName + "(" + eg.generateStdlibArgs(e.Args, sig) + ")"
		}
	}

	// 通用泛型适配：如果存在类型参数，则触发实例化
	if len(e.TypeArgs) > 0 {
		// 触发泛型实例化（实例化代码写入 genericFuncCode 缓冲区，最终前置注入）
		_, _ = eg.codegen.InstantiateGeneric(funcName, e.TypeArgs, e.Pos.Line)
		// 无论首次实例化还是已缓存，调用点都使用 mangled 名称
		funcName = MangleGenericName(funcName, e.TypeArgs)
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

	// print 函数（不换行）同样支持格式化
	if funcName == "print" {
		return eg.generatePrintCall(e.Args)
	}

	// 根据参数数量选择不同的调用方式
	if len(e.Args) == 0 {
		// 无参数调用
		return funcName + "()"
	} else {
		// 尝试查找 stdlib 函数签名以生成类型正确的参数
		var sig *stdlib.Function
		if eg.codegen.stdlibConfig != nil {
			sig = eg.codegen.stdlibConfig.GetAnyFunctionSignature(funcName)
		}
		code := funcName + "(" + eg.generateStdlibArgs(e.Args, sig) + ")"
		return code
	}
}

// normalizePtrType 规范化 C 指针类型字符串：去除 const 与空格，用于宽松匹配
// 如 "const char * const" -> "char*"，"char *" -> "char*"
func normalizePtrType(t string) string {
	n := strings.ReplaceAll(t, "const", "")
	n = strings.ReplaceAll(n, " ", "")
	return n
}

// generateStdlibArgs 根据函数签名生成参数列表
// 对于 const char* 参数，字符串字面量不包装为 String 结构体
func (eg *ExpressionGenerator) generateStdlibArgs(args []ast.Expression, sig *stdlib.Function) string {
	if sig == nil || len(sig.Args) == 0 {
		// 无签名信息，退化为普通生成
		code := ""
		for i, arg := range args {
			if i > 0 {
				code += ", "
			}
			code += eg.GenerateExpression(arg)
		}
		return code
	}

	code := ""
	for i, arg := range args {
		if i > 0 {
			code += ", "
		}
		if i < len(sig.Args) {
			paramType := sig.Args[i]
			if normalizePtrType(paramType) == "char*" {
				// 参数期望 C 字符串：字符串字面量直接输出，String 变量取 .ptr
				if strLit, ok := arg.(*ast.StringLiteral); ok {
					escaped := escapeCString(strLit.Value)
					code += "\"" + escaped + "\""
				} else {
					argCode := eg.GenerateExpression(arg)
					argType := eg.inferType(arg)
					if argType == "s" {
						code += argCode + ".ptr"
					} else {
						code += argCode
					}
				}
			} else {
				code += eg.GenerateExpression(arg)
			}
		} else {
			code += eg.GenerateExpression(arg)
		}
	}
	return code
}

// generateMethodCall 生成方法调用代码
func (eg *ExpressionGenerator) generateMethodCall(memberAccess *ast.MemberAccessExpression, args []ast.Expression) string {
	// 动态对象方法调用 obj.method(args) → dynobj_invoke(obj, "method", nargs, box(arg)...)
	if eg.isObjectTyped(memberAccess.Object) {
		eg.codegen.needsObjRuntime = true
		code := "dynobj_invoke(" + eg.GenerateExpression(memberAccess.Object) + ", \"" + escapeCString(memberAccess.Member) + "\", " + strconv.Itoa(len(args))
		for _, arg := range args {
			code += ", " + eg.boxDynamicValue(arg)
		}
		code += ")"
		return code
	}

	object := eg.GenerateExpression(memberAccess.Object)
	methodName := memberAccess.Member

	// 检查是否是标准库模块调用（如 std.io.println / freestanding.io.println）
	// 处理多级成员访问：获取实际的模块名
	moduleName := ""
	isStdModuleCall := false
	isFreeModuleCall := false
	if ident, ok := memberAccess.Object.(*ast.Identifier); ok {
		// 一级成员访问：io.println 或 std.println
		moduleName = ident.Name
	} else if nestedMember, ok := memberAccess.Object.(*ast.MemberAccessExpression); ok {
		// 多级成员访问：std.io.println 或 freestanding.io.println，methodName 是 "println"
		moduleName = nestedMember.Member
		// 检查是否是 std.module.function 模式
		if innerIdent, ok := nestedMember.Object.(*ast.Identifier); ok {
			if innerIdent.Name == "std" {
				isStdModuleCall = true
			} else if innerIdent.Name == "freestanding" {
				isFreeModuleCall = true
			}
		}
	}

	if moduleName != "" && eg.codegen.stdlibConfig != nil {
		// 支持多种键格式: "io"、"std.io" 和 "freestanding.io"
		stdlibKey := moduleName
		if isFreeModuleCall {
			stdlibKey = "freestanding." + moduleName
		} else if !strings.HasPrefix(stdlibKey, "std.") {
			stdlibKey = "std." + moduleName
		}

		if module, exists := eg.codegen.stdlibConfig.Modules[stdlibKey]; exists {
			// 特殊处理 println：使用类型推导版本
			if methodName == "println" && len(args) > 1 {
				return eg.generatePrintlnMulti(args)
			}

			// 检查 stdlib.json 中是否有这个函数
			if funcSig, funcExists := module.Functions[methodName]; funcExists {
				// 追踪第三方库的使用
				if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(methodName); isThirdParty {
					eg.codegen.usedThirdPartyLibs[lib.Name] = true
				}

				// 使用 GetCFunctionName 自动添加模块前缀
				cFuncName := eg.codegen.stdlibConfig.GetCFunctionName(stdlibKey, methodName)

				// 修复 #21：全局将 std_malloc 重写为 kmm_v4_alloc_auto
				// 统一所有动态分配走 KMM pool，不再依赖 IsInKMMScope() 判断
				if cFuncName == "std_malloc" || methodName == "std_malloc" {
					if len(args) == 1 {
						sizeArg := eg.GenerateExpression(args[0])
						return "kmm_v4_alloc_auto(" + sizeArg + ")"
					}
				}

				code := cFuncName + "(" + eg.generateStdlibArgs(args, &funcSig) + ")"
				return code
			}
		}
	}

	// class 方法调用 (v.method()): object 的符号类型是已定义 class
	// class 实例是指针语义, 直接传对象指针: Class_Method(v, args...)
	if ident, ok := memberAccess.Object.(*ast.Identifier); ok {
		if sym := eg.codegen.GetSymbol(ident.Name); sym != nil {
			if eg.codegen.typeGenerator != nil && eg.codegen.typeGenerator.classTypes[sym.Type] {
				code := sym.Type + "_" + methodName + "(" + eg.GenerateExpression(memberAccess.Object)
				for _, arg := range args {
					code += ", " + eg.GenerateExpression(arg)
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

	// 处理基本类型的方法调用：仅当对象是对应基本类型时才走这个分支；
	// 若对象是 class 类型，将其交给 generateObjectMethodCall 按类方法名解析。
	if len(args) == 1 {
		argCode := eg.GenerateExpression(args[0])
		switch methodName {
		case "add":
			if eg.symbolTypeIsIntLike(memberAccess.Object) {
				return "int_object_add(" + object + ", " + argCode + ")"
			}
		case "subtract":
			if eg.symbolTypeIsIntLike(memberAccess.Object) {
				return "int_object_subtract(" + object + ", " + argCode + ")"
			}
		case "multiply":
			if eg.symbolTypeIsIntLike(memberAccess.Object) {
				return "int_object_multiply(" + object + ", " + argCode + ")"
			}
		case "divide":
			if eg.symbolTypeIsIntLike(memberAccess.Object) {
				return "int_object_divide(" + object + ", " + argCode + ")"
			}
		case "concat":
			if eg.symbolTypeIsStringLike(memberAccess.Object) {
				return "string_object_concat(" + object + ", " + argCode + ")"
			}
		case "equals":
			return "object_equals((Object*)" + object + ", (Object*)" + argCode + ")"
		}
	}

	switch methodName {
	case "length":
		// 仅字符串类型使用 string_object_length；其它类型（如 Vec2.length()）作为类方法解析
		if eg.symbolTypeIsStringLike(memberAccess.Object) {
			return "string_object_length(" + object + ")"
		}
		return eg.generateObjectMethodCall(memberAccess.Object, object, methodName, args)
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
		return eg.generateObjectMethodCall(memberAccess.Object, object, methodName, args)
	}
}

// generateObjectMethodCall 生成对象方法调用代码
// objectAST 是原始的对象表达式 AST，用于从符号表推断真实类名
func (eg *ExpressionGenerator) generateObjectMethodCall(objectAST ast.Expression, object string, methodName string, args []ast.Expression) string {
	className := eg.inferClassType(objectAST)
	if className == "" {
		// 无法推断类型时，降级为 Object
		className = "Object"
	}

	code := className + "_" + methodName + "("
	code += object

	for _, arg := range args {
		code += ", " + eg.GenerateExpression(arg)
	}
	code += ")"
	return code
}

// inferClassType 从 AST 表达式推断其所属的类名（若为 class 类型则返回类名，否则返回空串）
func (eg *ExpressionGenerator) inferClassType(expr ast.Expression) string {
	if eg.codegen.program == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		// 直接的变量引用：查符号表获取其 Kaula 类型名
		sym := eg.codegen.currentScope.GetSymbol(e.Name)
		if sym != nil {
			typeName := strings.TrimSuffix(sym.Type, "*")
			if typeName != "" && eg.codegen.program.FindClass(typeName) != nil {
				return typeName
			}
		}
	case *ast.MemberAccessExpression:
		// obj.field：如果 field 的类型是 class，则返回该类名
		// 暂不深入推断嵌套类型
		return ""
	case *ast.CallExpression:
		// ClassName(args...) 或 ClassName_new(...)：返回调用的类名
		if ident, ok := e.Function.(*ast.Identifier); ok {
			name := strings.TrimSuffix(ident.Name, "_new")
			if eg.codegen.program.FindClass(name) != nil {
				return name
			}
		}
	}
	return ""
}

// symbolTypeIsIntLike 判断表达式符号表中的类型是否为整数类型（用于分派 int_object_xxx）
func (eg *ExpressionGenerator) symbolTypeIsIntLike(expr ast.Expression) bool {
	if ident, ok := expr.(*ast.Identifier); ok {
		sym := eg.codegen.GetSymbol(ident.Name)
		if sym == nil {
			return false
		}
		switch sym.Type {
		case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "int":
			return true
		}
		return false
	}
	// 非标识符：基本类型字面量走 inferType
	switch eg.inferType(expr) {
	case "i", "u":
		return true
	}
	return false
}

// symbolTypeIsStringLike 判断表达式符号表中的类型是否为字符串类型（用于分派 string_object_xxx / length）
func (eg *ExpressionGenerator) symbolTypeIsStringLike(expr ast.Expression) bool {
	if ident, ok := expr.(*ast.Identifier); ok {
		sym := eg.codegen.GetSymbol(ident.Name)
		if sym == nil {
			return false
		}
		return sym.Type == "string"
	}
	// 字符串字面量 inferType 返回 "s"
	return eg.inferType(expr) == "s"
}

// isClassField 检查 name 是否是 className 对应类的字段名
func (eg *ExpressionGenerator) isClassField(className, name string) bool {
	if eg.codegen.program == nil {
		return false
	}
	classStmt := eg.codegen.program.FindClass(className)
	if classStmt == nil {
		return false
	}
	for _, f := range classStmt.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// generatePrintlnCall 生成 println 调用代码
// 支持类型推导自动判断格式化参数
func (eg *ExpressionGenerator) generatePrintlnCall(args []ast.Expression) string {
	// freestanding 模式下没有 libc 的 puts/putchar/printf，
	// 必须使用 freestanding.io 的 println/fs_putchar 函数
	isFreestanding := eg.codegen.config != nil && eg.codegen.config.Freestanding

	if len(args) == 0 {
		if isFreestanding {
			return "fs_putchar('\\n')"
		}
		return "putchar('\\n')"
	}

	// 检查第一个参数是否是字符串字面量
	if strLit, ok := args[0].(*ast.StringLiteral); ok {
		str := strLit.Value
		strEscaped := escapeCString(strings.TrimSuffix(str, "\\n"))

		if len(args) == 1 && !strings.Contains(str, "%") {
			if isFreestanding {
				return "println(\"" + strEscaped + "\")"
			}
			return "puts(\"" + strEscaped + "\")"
		}

		if len(args) == 1 {
			if isFreestanding {
				return "println(\"" + strEscaped + "\")"
			}
			return "puts(\"" + strEscaped + "\")"
		}

		// 有格式字符串和参数时，使用 println 处理 %d/%s/%f 格式
		if strings.Contains(str, "%") {
			return eg.generatePrintlnWithFormat(args)
		}

		return eg.generatePrintlnMulti(args)
	}

	// 第一个参数不是字符串字面量，按普通方式处理
	if len(args) == 1 {
		argCode := eg.GenerateExpression(args[0])
		argType := eg.inferType(args[0])

		// freestanding 模式下用 print + fs_putchar('\n')，否则用 printf
		printFn := "printf"
		if isFreestanding {
			printFn = "print"
		}
		if argType == "d" && isIntegerLiteral(argCode) {
			if isFreestanding {
				return printFn + "(\"" + argCode + "\\n\")"
			}
			return "printf(\"" + argCode + "\\n\")"
		}
		// cstr 类型（char*）对应 %s
		formatSpec := argType
		if formatSpec == "cstr" {
			formatSpec = "s"
		}
		return printFn + "(\"%" + formatSpec + "\\n\", " + eg.maybeUnwrapString(argCode, argType) + ")"
	} else {
		return eg.generatePrintlnMulti(args)
	}
}

// generatePrintlnWithFormat 生成带格式字符串的 println 调用
func (eg *ExpressionGenerator) generatePrintlnWithFormat(args []ast.Expression) string {
	var b strings.Builder
	b.WriteString("println(\"")

	// 输出格式字符串
	if strLit, ok := args[0].(*ast.StringLiteral); ok {
		b.WriteString(escapeCString(strLit.Value))
	}
	b.WriteString("\"")

	// 输出参数
	for i := 1; i < len(args); i++ {
		b.WriteString(", ")
		b.WriteString(eg.GenerateExpression(args[i]))
	}

	b.WriteString(")")
	return b.String()
}

// generatePrintCall 生成 print 调用代码（不换行）
func (eg *ExpressionGenerator) generatePrintCall(args []ast.Expression) string {
	if len(args) == 0 {
		return ""
	}

	// freestanding 模式下没有 libc 的 printf，使用 freestanding.io 的 print
	isFreestanding := eg.codegen.config != nil && eg.codegen.config.Freestanding
	printfName := "printf"
	if isFreestanding {
		printfName = "print"
	}

	// 检查第一个参数是否是字符串字面量
	if strLit, ok := args[0].(*ast.StringLiteral); ok {
		str := strLit.Value
		strEscaped := escapeCString(str)

		if len(args) == 1 {
			return printfName + "(\"" + strEscaped + "\")"
		}

		// 有格式字符串和参数时，使用 printf 处理格式
		if strings.Contains(str, "%") {
			var b strings.Builder
			b.WriteString(printfName + "(\"")
			b.WriteString(strEscaped)
			b.WriteString("\"")
			for i := 1; i < len(args); i++ {
				b.WriteString(", ")
				b.WriteString(eg.GenerateExpression(args[i]))
			}
			b.WriteString(")")
			return b.String()
		}

		return printfName + "(\"" + strEscaped + "\")"
	}

	// 第一个参数不是字符串字面量
	if len(args) == 1 {
		argCode := eg.GenerateExpression(args[0])
		argType := eg.inferType(args[0])
		return printfName + "(\"%" + argType + "\", " + eg.maybeUnwrapString(argCode, argType) + ")"
	}

	// 多个参数
	var b strings.Builder
	b.WriteString(printfName + "(\"")
	for i, arg := range args {
		if i > 0 {
			b.WriteString(" ")
		}
		argType := eg.inferType(arg)
		b.WriteString("%" + argType)
	}
	b.WriteString("\"")
	for _, arg := range args {
		b.WriteString(", ")
		b.WriteString(eg.GenerateExpression(arg))
	}
	b.WriteString(")")
	return b.String()
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
			b.WriteString(eg.maybeUnwrapString(argCode, "s"))
		case "cstr":
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

// maybeUnwrapString 对 string 类型表达式追加 .ptr 以匹配 C %s 约定
// cstr 类型（char*）已经是 C 字符串指针，不需要 .ptr
func (eg *ExpressionGenerator) maybeUnwrapString(code string, typeHint string) string {
	if typeHint == "s" {
		return code + ".ptr"
	}
	return code
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
	case *ast.ObjectLiteral:
		return "obj"
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
			case "object", "Object":
				return "obj"
			default:
				t := strings.ToLower(sym.Type)
				if strings.HasPrefix(t, "i") || strings.HasPrefix(t, "u") || t == "int" || t == "int64" || t == "int32" {
					return "d"
				}
				if strings.HasPrefix(t, "f") || t == "float" || t == "double" {
					return "f"
				}
				if t == "string" {
					return "s"
				}
				if normalizePtrType(t) == "char*" || t == "cstring" {
					return "cstr"
				}
				if strings.HasSuffix(t, "*") {
					return "cstr"
				}
				if t == "bool" {
					return "d"
				}
				cType := eg.codegen.typeGenerator.convertType(sym.Type, false)
				if strings.Contains(cType, "double") || strings.Contains(cType, "float") {
					return "f"
				}
				if strings.Contains(cType, "char*") {
					return "cstr"
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
	case *ast.TypeNameExpression, *ast.FieldNameExpression, *ast.FieldTypeExpression:
		return "s"
	case *ast.CallExpression:
		if ident, ok := e.Function.(*ast.Identifier); ok {
			funcName := ident.Name
			// 优先查本地符号表：extern fn 声明以返回类型注册符号，
			// 本地函数定义同理；查不到再查 stdlib
			if sym := eg.codegen.currentScope.GetSymbol(funcName); sym != nil {
				switch sym.Type {
				case "string", "str":
					return "s"
				case "float", "float64", "float32", "f32", "f64", "double", "single":
					return "f"
				case "bool":
					return "d"
				}
				t := strings.ToLower(sym.Type)
				if normalizePtrType(t) == "char*" || t == "cstring" {
					return "cstr"
				}
			}
			if eg.codegen.stdlibConfig != nil {
				for _, mod := range eg.codegen.stdlibConfig.Modules {
					if sig, ok := mod.Functions[funcName]; ok && sig.Return != "" {
						if sig.Return == "string" {
							return "s"
						}
						if normalizePtrType(sig.Return) == "char*" || sig.Return == "cstring" {
							return "cstr"
						}
						if sig.Return == "float" || sig.Return == "f64" || sig.Return == "f32" || sig.Return == "double" {
							return "f"
						}
						return "d"
					}
				}
				// 第三方库（pkglib）的函数签名单独存放
				for _, lib := range eg.codegen.stdlibConfig.ThirdParty {
					if sig, ok := lib.Functions[funcName]; ok && sig.Return != "" {
						if sig.Return == "string" {
							return "s"
						}
						if normalizePtrType(sig.Return) == "char*" || sig.Return == "cstring" {
							return "cstr"
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

// isObjectTyped 检查表达式是否为动态对象类型
// 判断依据：ObjectLiteral、标识符声明为 object 类型、成员/索引访问基于对象
func (eg *ExpressionGenerator) isObjectTyped(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.ObjectLiteral:
		return true
	case *ast.Identifier:
		if sym := eg.codegen.GetSymbol(e.Name); sym != nil {
			if sym.Type == "object" || sym.Type == "Object" {
				return true
			}
		}
		if sym := eg.codegen.currentScope.GetSymbol(e.Name); sym != nil {
			if sym.Type == "object" || sym.Type == "Object" {
				return true
			}
		}
	case *ast.MemberAccessExpression:
		return eg.isObjectTyped(e.Object)
	case *ast.IndexExpression:
		return eg.isObjectTyped(e.Object)
	case *ast.CallExpression:
		if ident, ok := e.Function.(*ast.Identifier); ok {
			if ident.Name == "object" || ident.Name == "object_create" {
				return true
			}
		}
	}
	return false
}

// objectKeyCode 将索引表达式转换为 C 字符串键
// 字符串索引直接用；整数索引用 itoa 转换
func (eg *ExpressionGenerator) objectKeyCode(index ast.Expression) string {
	if strLit, ok := index.(*ast.StringLiteral); ok {
		return "\"" + escapeCString(strLit.Value) + "\""
	}
	if intLit, ok := index.(*ast.IntegerLiteral); ok {
		return strconv.FormatUint(intLit.Value, 10)
	}
	return eg.GenerateExpression(index)
}

// boxDynamicValue 将 Kaula 值装箱为 Object*（用于 dynobj_set 存储）
func (eg *ExpressionGenerator) boxDynamicValue(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return "dynobj_box_i64(" + strconv.FormatUint(e.Value, 10) + ")"
	case *ast.FloatLiteral:
		return "dynobj_box_f64(" + strconv.FormatFloat(e.Value, 'f', -1, 64) + ")"
	case *ast.BooleanLiteral:
		if e.Value {
			return "dynobj_box_bool(1)"
		}
		return "dynobj_box_bool(0)"
	case *ast.StringLiteral:
		return "dynobj_box_cstr(\"" + escapeCString(e.Value) + "\")"
	case *ast.LambdaExpression:
		eg.codegen.needsObjRuntime = true
		lambdaName := eg.GenerateLambdaExpression(e)
		trampName := eg.generateLambdaTrampoline(e, lambdaName)
		return "(Object*)func_object_create((void*)" + trampName + ")"
	case *ast.ObjectLiteral:
		// 对象字面量已经返回 Object*，无需装箱
		return eg.GenerateExpression(expr)
	case *ast.IndexExpression:
		// 动态对象索引访问返回 Object*，无需再次装箱
		if eg.isObjectTyped(e.Object) {
			return eg.GenerateExpression(expr)
		}
	case *ast.MemberAccessExpression:
		// 动态对象成员访问返回 Object*，无需再次装箱
		if eg.isObjectTyped(e.Object) {
			return eg.GenerateExpression(expr)
		}
	case *ast.CallExpression:
		// 动态对象方法调用返回 Object*，无需再次装箱
		if memberAccess, ok := e.Function.(*ast.MemberAccessExpression); ok && eg.isObjectTyped(memberAccess.Object) {
			return eg.GenerateExpression(expr)
		}
	case *ast.Identifier:
		sym := eg.codegen.GetSymbol(e.Name)
		if sym == nil {
			sym = eg.codegen.currentScope.GetSymbol(e.Name)
		}
		if sym != nil {
			switch sym.Type {
			case "int", "int64", "int32", "i8", "i16", "i32", "i64":
				return "dynobj_box_i64(" + e.Name + ")"
			case "float", "float64", "float32", "f32", "f64", "double":
				return "dynobj_box_f64(" + e.Name + ")"
			case "bool":
				return "dynobj_box_bool(" + e.Name + ")"
			case "string", "str":
				return "dynobj_box_cstr(" + e.Name + ".ptr)"
			case "object", "Object":
				return e.Name
			}
		}
		return eg.boxDynamicValueFallback(expr)
	}
	return eg.boxDynamicValueFallback(expr)
}

// boxDynamicValueFallback 默认装箱逻辑（处理非特殊类型）
func (eg *ExpressionGenerator) boxDynamicValueFallback(expr ast.Expression) string {
	ty := eg.inferType(expr)
	switch ty {
	case "d":
		return "dynobj_box_i64(" + eg.GenerateExpression(expr) + ")"
	case "f":
		return "dynobj_box_f64(" + eg.GenerateExpression(expr) + ")"
	case "s":
		return "dynobj_box_cstr(" + eg.GenerateExpression(expr) + ".ptr)"
	case "cstr":
		return "dynobj_box_cstr(" + eg.GenerateExpression(expr) + ")"
	case "obj":
		return eg.GenerateExpression(expr)
	default:
		return "dynobj_box_i64(" + eg.GenerateExpression(expr) + ")"
	}
}

// generateObjectLiteral 生成动态对象字面量的 C 代码
// object { name: value, ... } → dynobj_create() + dynobj_set() for each field
// object() → dynobj_create() (空对象)
func (eg *ExpressionGenerator) generateObjectLiteral(e *ast.ObjectLiteral) string {
	eg.codegen.needsObjRuntime = true

	if len(e.Fields) == 0 {
		return "(Object*)dynobj_create()"
	}

	objIndex := eg.codegen.objectLiteralCounter
	eg.codegen.objectLiteralCounter++
	varName := fmt.Sprintf("_dynobj_lit_%d", objIndex)

	var builder strings.Builder
	builder.WriteString("(Object*)")
	builder.WriteString(varName)

	// 1. 生成静态变量声明（文件作用域，放在 functionCode 开头）
	eg.codegen.AddObjectDecl(fmt.Sprintf("static Object* %s = NULL;\n", varName))

	// 2. 生成初始化代码（放在 main 函数体内）
	var initCode strings.Builder
	initCode.WriteString(fmt.Sprintf("if (%s == NULL) {\n", varName))
	initCode.WriteString(fmt.Sprintf("    %s = (Object*)dynobj_create();\n", varName))

	for _, field := range e.Fields {
		fieldKey := "\"" + escapeCString(field.Name) + "\""
		fieldBoxed := eg.boxDynamicValue(field.Value)
		initCode.WriteString(fmt.Sprintf("    dynobj_set(%s, %s, %s);\n", varName, fieldKey, fieldBoxed))
	}
	initCode.WriteString("}\n")

	eg.codegen.AddPreludeInit(initCode.String())

	return builder.String()
}

// generateMemberAccessExpression 生成成员访问表达式代码
func (eg *ExpressionGenerator) generateMemberAccessExpression(e *ast.MemberAccessExpression) string {
	// 动态对象字段读取 obj.field → dynobj_get(obj, "field")
	if eg.isObjectTyped(e.Object) {
		eg.codegen.needsObjRuntime = true
		return "dynobj_get(" + eg.GenerateExpression(e.Object) + ", \"" + escapeCString(e.Member) + "\")"
	}

	object := eg.GenerateExpression(e.Object)

	if object == "self" {
		return object + "->" + e.Member
	}

	// 检查是否是枚举常量引用（如 Color.Red）
	if ident, ok := e.Object.(*ast.Identifier); ok {
		if eg.codegen.IsEnumType(ident.Name) {
			return ident.Name + "_Kind_" + e.Member
		}
	}

	isPtr := false
	if ident, ok := e.Object.(*ast.Identifier); ok {
		if sym := eg.codegen.GetSymbol(ident.Name); sym != nil {
			typeStr := sym.Type
			if strings.HasSuffix(typeStr, "*") || strings.HasPrefix(typeStr, "*") {
				isPtr = true
			}
			// 类类型变量始终按指针传递和存储（K_ClassName*），
			// 即使符号表中类型名未显式带 *，也视为指针访问。
			if !isPtr && eg.codegen.IsClassType(typeStr) {
				isPtr = true
			}
			// class_field:Class:fieldName 形式（类方法/构造中的裸字段名），
			// 实际代码生成会被上层处理为 self->field，此处兜底。
			if strings.HasPrefix(typeStr, "class_field:") {
				isPtr = true
			}
		}
	}
	// 兜底：如果对象表达式本身是另一个成员访问，且中间成员类型为类类型，也使用 ->。
	if !isPtr {
		if _, ok := e.Object.(*ast.MemberAccessExpression); ok {
			// 无法静态推导嵌套成员类型时，先按对象指针处理：
			// 若符号名对应对象是 class，成员声明也是 class，递归访问中都是指针。
			// 此处不强制，避免误伤，但对于对象表达式是 MemberAccess 的情况，
			// 先保守判断对象根符号。
			objRoot := e.Object
			for {
				if ma, ok := objRoot.(*ast.MemberAccessExpression); ok {
					objRoot = ma.Object
				} else {
					break
				}
			}
			if rootIdent, ok := objRoot.(*ast.Identifier); ok {
				if rootIdent.Name != "self" {
					if sym := eg.codegen.GetSymbol(rootIdent.Name); sym != nil {
						typeStr := sym.Type
						if eg.codegen.IsClassType(typeStr) {
							// 根对象是类指针，则所有成员访问中访问的是非指针字段使用 .，
							// 但字段若是 class 类型则是指针；这里无法确定，
							// 仅根据根是类指针的判断不充分，故不盲目设置。
							_ = typeStr
						}
					}
				}
			}
		}
	}
	// 进一步检查：对象表达式的推断类型若是 class 指针 → 用 ->
	if !isPtr {
		objType := eg.inferClassType(e.Object)
		if objType != "" && eg.codegen.IsClassType(objType) {
			// 对于类类型变量的字段读取，如果字段不是 class 类型，仍是结构体点访问。
			// 但如果 objType 是类，则 Object 本身是 class，在 C 中是 K_Class*（指针），
			// 所以第一层必定是指针。
			isPtr = true
		}
	}

	if isPtr {
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
	return eg.codegen.typeGenerator.MapKaulaTypeToC(kaulaType)
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
	return "((int64_t[]){ " + strings.Join(elems, ", ") + " })"
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
		if _, isBinary := e.Right.(*ast.BinaryExpression); isBinary {
			return "!(" + right + ")"
		}
		return "!" + right
	case "-":
		if _, isBinary := e.Right.(*ast.BinaryExpression); isBinary {
			return "(-" + right + ")"
		}
		return "-" + right
	case "*":
		if _, isBinary := e.Right.(*ast.BinaryExpression); isBinary {
			return "*(" + right + ")"
		}
		return "*" + right
	case "~":
		if _, isBinary := e.Right.(*ast.BinaryExpression); isBinary {
			return "~(" + right + ")"
		}
		return "~" + right
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

	// 将 lambda 参数添加到当前作用域，使 inferType 等函数能正确推断类型
	eg.codegen.EnterScope("lambda")
	for i, param := range expr.Params {
		paramKaulaType := "int"
		if i < len(expr.ParamTypes) && expr.ParamTypes[i] != "" && expr.ParamTypes[i] != "auto" {
			paramKaulaType = expr.ParamTypes[i]
		}
		eg.codegen.AddSymbol(param, paramKaulaType, false, "lambda_param", 0, 0)
	}

	// 生成函数体
	// 保存并设置当前函数返回类型，使 return 语句正确生成返回值
	savedReturnType := eg.codegen.currentFunctionReturnType
	eg.codegen.currentFunctionReturnType = expr.ReturnType
	var bodyCode strings.Builder
	for _, stmt := range expr.Body {
		bodyCode.WriteString(eg.codegen.statementGenerator.GenerateStatement(stmt))
	}
	eg.codegen.currentFunctionReturnType = savedReturnType

	eg.codegen.ExitScope()

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

// generateLambdaTrampoline 生成 lambda 的 trampoline 函数
// trampoline 签名: Object* (*)(Object* self, size_t nargs, Object** argv)
// 负责：拆箱参数 → 调用 lambda → 装箱返回值
func (eg *ExpressionGenerator) generateLambdaTrampoline(expr *ast.LambdaExpression, lambdaName string) string {
	trampName := fmt.Sprintf("_kaula_tramp_%d", eg.codegen.lambdaCounter)
	eg.codegen.lambdaCounter++

	var funcDef strings.Builder
	funcDef.WriteString(fmt.Sprintf("static Object* %s(Object* self, size_t nargs, Object** argv) {\n", trampName))
	funcDef.WriteString("    (void)self; (void)nargs;\n")

	// 生成参数拆箱代码
	for i, param := range expr.Params {
		var unboxCode string
		if i < len(expr.ParamTypes) {
			switch expr.ParamTypes[i] {
			case "int", "int64", "int32", "i8", "i16", "i32", "i64":
				unboxCode = fmt.Sprintf("int64_t %s = dynobj_unbox_i64(argv[%d]);", param, i)
			case "float", "float64", "float32", "f32", "f64", "double":
				unboxCode = fmt.Sprintf("double %s = dynobj_unbox_f64(argv[%d]);", param, i)
			case "bool":
				unboxCode = fmt.Sprintf("int %s = dynobj_unbox_bool(argv[%d]);", param, i)
			case "string", "str":
				unboxCode = fmt.Sprintf("String %s = string_create(dynobj_unbox_cstr(argv[%d]));", param, i)
			default:
				unboxCode = fmt.Sprintf("int64_t %s = dynobj_unbox_i64(argv[%d]);", param, i)
			}
		} else {
			unboxCode = fmt.Sprintf("int64_t %s = dynobj_unbox_i64(argv[%d]);", param, i)
		}
		funcDef.WriteString("    " + unboxCode + "\n")
	}

	// 生成 lambda 调用和返回值装箱
	if expr.ReturnType != "" && expr.ReturnType != "void" {
		callCode := fmt.Sprintf("%s(", lambdaName)
		for i, param := range expr.Params {
			if i > 0 {
				callCode += ", "
			}
			callCode += param
		}
		callCode += ")"

		switch expr.ReturnType {
		case "int", "int64", "int32", "i8", "i16", "i32", "i64":
			funcDef.WriteString(fmt.Sprintf("    return dynobj_box_i64(%s);\n", callCode))
		case "float", "float64", "float32", "f32", "f64", "double":
			funcDef.WriteString(fmt.Sprintf("    return dynobj_box_f64(%s);\n", callCode))
		case "bool":
			funcDef.WriteString(fmt.Sprintf("    return dynobj_box_bool(%s);\n", callCode))
		case "string", "str":
			funcDef.WriteString(fmt.Sprintf("    String _tmp = %s;\n", callCode))
			funcDef.WriteString("    return dynobj_box_cstr(_tmp.ptr);\n")
		default:
			funcDef.WriteString(fmt.Sprintf("    return dynobj_box_i64((int64_t)%s);\n", callCode))
		}
	} else {
		callCode := fmt.Sprintf("%s(", lambdaName)
		for i, param := range expr.Params {
			if i > 0 {
				callCode += ", "
			}
			callCode += param
		}
		callCode += ")"
		funcDef.WriteString(fmt.Sprintf("    %s;\n", callCode))
		funcDef.WriteString("    return NULL;\n")
	}

	funcDef.WriteString("}\n")
	eg.codegen.lambdaDefinitions = append(eg.codegen.lambdaDefinitions, funcDef.String())

	return trampName
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

	// 尝试从目标表达式推断枚举类型名
	enumName := eg.inferEnumName(e.Target)
	if enumName != "" {
		// 检查枚举是否有数据变体
		enumStmt := eg.codegen.program.FindEnum(enumName)
		hasDataVariants := false
		if enumStmt != nil {
			for _, v := range enumStmt.Variants {
				if len(v.FieldTypes) > 0 {
					hasDataVariants = true
					break
				}
			}
		}
		if hasDataVariants {
			// 带数据的枚举，使用 .kind 访问
			code.WriteString("switch (")
			code.WriteString(targetCode)
			code.WriteString(".kind) {\n")
		} else {
			// 简单枚举，直接使用值
			code.WriteString("switch (")
			code.WriteString(targetCode)
			code.WriteString(") {\n")
		}
	} else {
		code.WriteString("switch (")
		code.WriteString(targetCode)
		code.WriteString(") {\n")
	}

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
		if sym != nil && eg.codegen.program != nil {
			// 检查类型是否是已定义的枚举
			if enumStmt := eg.codegen.program.FindEnum(sym.Type); enumStmt != nil {
				return enumStmt.Name
			}
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
		return fmt.Sprintf("(*(volatile uint8_t*)(%s))", args[0])

	case "volatile_store":
		// #[volatile_store(ptr, val)] - volatile 指针解引用写
		if len(args) < 2 {
			eg.codegen.error("volatile_store requires pointer and value arguments")
			return "0"
		}
		return fmt.Sprintf("(*(volatile uint8_t*)(%s) = (%s))", args[0], args[1])

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

// generateStructLiteral 生成结构体字面量 C 代码 { .field = value, ... }
func (eg *ExpressionGenerator) generateStructLiteral(sl *ast.StructLiteral) string {
	if len(sl.Fields) == 0 {
		return "{0}"
	}
	var parts []string
	for _, f := range sl.Fields {
		val := eg.GenerateExpression(f.Value)
		parts = append(parts, fmt.Sprintf(".%s=%s", f.Name, val))
	}
	return "{" + strings.Join(parts, ",") + "}"
}
