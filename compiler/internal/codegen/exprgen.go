package codegen

import (
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/astutil"
	"kaula/internal/comptime"
	"kaula/internal/stdlib"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// cOperatorPrecedence
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
	s = strings.ReplaceAll(s, "\x00", "\\000")
	return s
}

// decodedCStringLen 计算含转义序列字符串解码后的真实字节数。
// 词法器保留 \" \n \\ 等转义序列的原始形式（每个序列占 2 个源字符），
// 而 String.len 必须是解码后的长度，否则 string_concat/index_of 等按 len
// 操作的运行时会多拷/多比较字节（把 '\0' 也计入）。
func decodedCStringLen(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n++
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
	}
	return n
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
	closureAliases map[string]string
	codegen        *CodeGenerator
	typeCache      map[ast.Expression]string // 表达式 → 推导类型缓存
	comptime       *comptime.Evaluator
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
		return formatFloatLiteral(e.Value)
	case *ast.StringLiteral:
		escaped := escapeCString(e.Value)
		slen := decodedCStringLen(escaped)
		// SSO：<=15 字节用内嵌缓冲区，零堆分配
		if slen <= 15 {
			return fmt.Sprintf("((String){.len=%d, .sso=\"%s\"})", slen, escaped)
		}
		return fmt.Sprintf("((String){.len=%d, .ptr=\"%s\"})", slen, escaped)
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
	if v, ok := eg.closureAliases[e.Name]; ok {
		return v
	}
	// 检查是否是 null 关键字（小写 null 与兼容 C 习惯的大写 NULL）
	if e.Name == "null" || e.Name == "NULL" {
		return "NULL"
	}

	// 嵌套函数捕获重写：外层局部变量 → (*_cap_N)
	if eg.codegen.currentCaptureSet != nil {
		if idx, ok := eg.codegen.currentCaptureSet[e.Name]; ok {
			return "(*_cap_" + strconv.Itoa(idx) + ")"
		}
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
		// 强制 self 策略：类成员必须通过 self.field 显式访问。
		// 裸名不再自动解析为 self 字段；未解析的裸名落到末尾原样输出
		// （sema 阶段已对类作用域内的裸字段引用报错并提示改用 self.field）。
	}

	// 检查是否是枚举变体
	if sym := eg.codegen.GetSymbol(e.Name); sym != nil && strings.HasPrefix(sym.Type, "enum_variant:") {
		enumName := strings.TrimPrefix(sym.Type, "enum_variant:")
		return enumName + "_Kind_" + e.Name
	}

	// 用户自定义函数名作为值引用（回调/函数指针实参，如 start(server, handler)）：
	// 与函数定义一致使用 K_ 前缀。若同名局部变量/参数遮蔽了函数名，则按变量原样输出。
	if eg.codegen.isUserKaulaFunc(e.Name) {
		if sym := eg.codegen.GetSymbol(e.Name); sym != nil && sym.Type != "function" {
			return e.Name
		}
		return eg.codegen.UserFuncCName(e.Name)
	}

	return e.Name
}

// compoundAssignToBinaryOp 将复合赋值运算符映射为对应的二元运算符
// 返回空字符串表示不是复合赋值
func compoundAssignToBinaryOp(op string) string {
	switch op {
	case "+=", "PLUS_ASSIGN":
		return "+"
	case "-=", "MINUS_ASSIGN":
		return "-"
	case "*=", "MULTIPLY_ASSIGN":
		return "*"
	case "/=", "DIVIDE_ASSIGN":
		return "/"
	case "%=", "MOD_ASSIGN":
		return "%"
	case "&=", "BITAND_ASSIGN":
		return "&"
	case "|=", "BITOR_ASSIGN":
		return "|"
	case "^=", "XOR_ASSIGN":
		return "^"
	case "<<=", "LSHIFT_ASSIGN":
		return "<<"
	case ">>=", "RSHIFT_ASSIGN":
		return ">>"
	}
	return ""
}

// generateBinaryExpression 生成二元表达式代码
func (eg *ExpressionGenerator) generateBinaryExpression(e *ast.BinaryExpression) string {
	// 复合赋值转换：a += b → a = a + b
	if op := compoundAssignToBinaryOp(e.Operator); op != "" {
		converted := &ast.BinaryExpression{
			Left:     e.Left,
			Operator: "=",
			Right: &ast.BinaryExpression{
				Left:     e.Left,
				Operator: op,
				Right:    e.Right,
				Pos:      e.Pos,
			},
			Pos: e.Pos,
		}
		return eg.generateBinaryExpression(converted)
	}

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

	// 修复 #25：赋值语句登记 malloc 常量大小（声明与赋值分离场景）
	// 如 tmp_buf = as<int*>(std_malloc(64)) → 后续 promote 需要 64
	if e.Operator == "ASSIGN" || e.Operator == "=" {
		if ident, ok := e.Left.(*ast.Identifier); ok {
			eg.codegen.RecordAllocSizeFromAST(ident.Name, e.Right)
		}
	}

	// 字符串拼接: "a" + x / x + "a" → string_concat 系列(内联运行时)
	// 必须在预计算左右表达式之前, 避免 String 值被当作指针算术
	if e.Operator == "PLUS" || e.Operator == "+" {
		lt := eg.inferType(e.Left)
		rt := eg.inferType(e.Right)
		if eg.isStringLikeFmt(lt) || eg.isStringLikeFmt(rt) {
			return eg.generateStringConcat(e, lt, rt)
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
		leftVal, errL := strconv.ParseInt(left, 10, 64)
		rightVal, errR := strconv.ParseInt(right, 10, 64)
		if errL != nil || errR != nil {
			goto skip_constant_fold // 解析失败（如溢出），回退运行时计算
		}

		var result int64
		var hasResult bool

		// 溢出检测：溢出时回退运行时（C 整数溢出为 UB）
		addOverflow := func(a, b int64) bool {
			if b > 0 && a > math.MaxInt64-b {
				return true
			}
			if b < 0 && a < math.MinInt64-b {
				return true
			}
			return false
		}
		mulOverflow := func(a, b int64) bool {
			if a == 0 || b == 0 {
				return false
			}
			if a == math.MinInt64 || b == math.MinInt64 {
				return true
			}
			return a*b/b != a
		}

		switch e.Operator {
		case "PLUS":
			if !addOverflow(leftVal, rightVal) {
				result = leftVal + rightVal
				hasResult = true
			}
		case "MINUS":
			if !addOverflow(leftVal, -rightVal) {
				result = leftVal - rightVal
				hasResult = true
			}
		case "MULTIPLY":
			if !mulOverflow(leftVal, rightVal) {
				result = leftVal * rightVal
				hasResult = true
			}
		case "DIVIDE":
			if rightVal != 0 && !(leftVal == math.MinInt64 && rightVal == -1) {
				result = leftVal / rightVal
				hasResult = true
			}
		case "MOD":
			if rightVal != 0 && !(leftVal == math.MinInt64 && rightVal == -1) {
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
			// 位移量必须在 [0,63]，否则为 UB，回退运行时
			if rightVal >= 0 && rightVal < 64 {
				result = leftVal << uint(rightVal)
				hasResult = true
			}
		case "RSHIFT", ">>":
			if rightVal >= 0 && rightVal < 64 {
				result = leftVal >> uint(rightVal)
				hasResult = true
			}
		case "XOR", "^", "BITWISE_XOR", "CARET":
			result = leftVal ^ rightVal
			hasResult = true
		}

		if hasResult {
			return strconv.FormatInt(result, 10)
		}
	}
skip_constant_fold:

	// 生成正常的二元表达式（直接字符串拼接，避免 fmt.Sprintf 开销）
	switch e.Operator {
	case "ASSIGN", "=":
		return left + " = " + right
	case "PLUS", "+":
		return eg.generatePlusOperation(e.Left, e.Right)
	case "MINUS", "-":
		// 缺口4：整数溢出检查（debug 模式默认启用）
		if eg.codegen.enableOverflowCheck && eg.isIntegerExpr(e.Left) && eg.isIntegerExpr(e.Right) {
			if eg.isUnsignedIntegerExpr(e.Left) || eg.isUnsignedIntegerExpr(e.Right) {
				return "kaula_sub_u64((uint64_t)(" + left + "), (uint64_t)(" + right + "))"
			}
			return "kaula_sub_i64((int64_t)(" + left + "), (int64_t)(" + right + "))"
		}
		return wrapIfNeeded(left, "-", "left") + " - " + wrapIfNeeded(right, "-", "right")
	case "MULTIPLY", "*":
		// 缺口4：整数溢出检查（debug 模式默认启用）
		if eg.codegen.enableOverflowCheck && eg.isIntegerExpr(e.Left) && eg.isIntegerExpr(e.Right) {
			if eg.isUnsignedIntegerExpr(e.Left) || eg.isUnsignedIntegerExpr(e.Right) {
				return "kaula_mul_u64((uint64_t)(" + left + "), (uint64_t)(" + right + "))"
			}
			return "kaula_mul_i64((int64_t)(" + left + "), (int64_t)(" + right + "))"
		}
		return wrapIfNeeded(left, "*", "left") + " * " + wrapIfNeeded(right, "*", "right")
	case "DIVIDE", "/":
		return wrapIfNeeded(left, "/", "left") + " / " + wrapIfNeeded(right, "/", "right")
	case "MOD", "%":
		return wrapIfNeeded(left, "%", "left") + " % " + wrapIfNeeded(right, "%", "right")
	case "EQ", "==":
		leftType := eg.inferType(e.Left)
		rightType := eg.inferType(e.Right)
		// String 与 null/NULL 比较（x == null）: 比较底层指针
		if leftType == "s" && isNullLiteralExpr(e.Right) {
			return left + ".ptr == NULL"
		}
		if rightType == "s" && isNullLiteralExpr(e.Left) {
			return right + ".ptr == NULL"
		}
		if (leftType == "cstr" && rightType == "s") || (leftType == "s" && rightType == "cstr") {
			if leftType == "s" {
				return "strcmp(string_cdata(" + left + "), " + right + ") == 0"
			}
			return "strcmp(" + left + ", string_cdata(" + right + ")) == 0"
		}
		if leftType == "s" && rightType == "s" {
			// 两侧都是 String (结构体/字面量): 用 string_cdata() 处理 SSO, 避免直接访问 .ptr
			return "strcmp(string_cdata(" + left + "), string_cdata(" + right + ")) == 0"
		}
		if leftType == "cstr" && rightType == "cstr" {
			return "strcmp(" + left + ", " + right + ") == 0"
		}
		return wrapIfNeeded(left, "==", "left") + " == " + wrapIfNeeded(right, "==", "right")
	case "NE", "!=":
		leftType := eg.inferType(e.Left)
		rightType := eg.inferType(e.Right)
		// String 与 null/NULL 比较（x != null）: 比较底层指针
		if leftType == "s" && isNullLiteralExpr(e.Right) {
			return left + ".ptr != NULL"
		}
		if rightType == "s" && isNullLiteralExpr(e.Left) {
			return right + ".ptr != NULL"
		}
		if (leftType == "cstr" && rightType == "s") || (leftType == "s" && rightType == "cstr") {
			if leftType == "s" {
				return "strcmp(string_cdata(" + left + "), " + right + ") != 0"
			}
			return "strcmp(" + left + ", string_cdata(" + right + ")) != 0"
		}
		if leftType == "s" && rightType == "s" {
			return "strcmp(string_cdata(" + left + "), string_cdata(" + right + ")) != 0"
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
				escaped := escapeCString(merged)
				slen := decodedCStringLen(escaped)
				if slen <= 15 {
					return fmt.Sprintf("((String){.len=%d, .sso=\"%s\"})", slen, escaped)
				}
				return fmt.Sprintf("((String){.len=%d, .ptr=\"%s\"})", slen, escaped)
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
	// 缺口4：整数溢出检查（debug 模式默认启用，零开销 __builtin_add_overflow）
	if eg.codegen.enableOverflowCheck {
		if eg.isUnsignedIntegerExpr(left) || eg.isUnsignedIntegerExpr(right) {
			return "kaula_add_u64((uint64_t)(" + leftStr + "), (uint64_t)(" + rightStr + "))"
		}
		return "kaula_add_i64((int64_t)(" + leftStr + "), (int64_t)(" + rightStr + "))"
	}
	return leftStr + " + " + rightStr
}

// generateCallExpression 生成函数调用表达式代码（支持泛型调用）
func (eg *ExpressionGenerator) generateCallExpression(e *ast.CallExpression) string {
	if v, ok := eg.generateClosureCall(e); ok {
		return v
	}
	// adopt() / unwrap() 内置转换运算符：编译期 no-op，直接输出参数
	if ident, ok := e.Function.(*ast.Identifier); ok && (ident.Name == "adopt" || ident.Name == "unwrap") {
		if len(e.Args) > 0 {
			return eg.GenerateExpression(e.Args[0])
		}
		return "/* adopt/unwrap: no argument */"
	}

	// 检查是否是方法调用，如 obj.method() 或 module.function()
	if memberAccess, ok := e.Function.(*ast.MemberAccessExpression); ok {
		return eg.generateMethodCall(memberAccess, e.Args)
	}

	// 函数名：直接从 AST 提取 Kaula 名（语义查询用，如 captureArgsFor / stdlib 签名 / 泛型实例化），
	// C 侧符号名在最终输出时通过 UserFuncCName 转换。此前用 GenerateExpression 会对用户函数返回
	// K_ 前缀名，导致 nestedFuncCaptures 等以 Kaula 名为 key 的表查询失配，嵌套函数调用缺捕获实参。
	funcName := ""
	if ident, ok := e.Function.(*ast.Identifier); ok {
		funcName = ident.Name
	} else {
		funcName = eg.GenerateExpression(e.Function)
	}

	// std_malloc 与 kmm_v4 系列是两个完全不同的分配器，禁止混用：
	// - hosted 模式（默认）：std_malloc 保留为系统 malloc 包装（std_malloc/std_free 符号，
	//   由 std/memory/memory.c 实现，内存需用 std_free 手动释放），不重写。
	// - freestanding 模式：无 libc，std_malloc 是从静态池分配的唯一路径，
	//   此时重写为 kmm_v4_alloc_auto（为便于区分，新代码应直接使用 kmm_v4_alloc）。
	if (funcName == "std_malloc" || funcName == "std.memory.std_malloc") &&
		eg.codegen.IsFreestanding() {
		if len(e.Args) == 1 {
			sizeArg := eg.GenerateExpression(e.Args[0])
			return "kmm_v4_alloc_auto(" + sizeArg + ")"
		}
	}

	// 检查是否是结构体构造函数调用（无参数的类型名调用）
	if ident, ok := e.Function.(*ast.Identifier); ok && len(e.Args) == 0 && len(e.TypeArgs) == 0 {
		if eg.codegen.IsStructType(ident.Name) {
			// 类型名走 mapTypeToC 映射(跨文件 struct 生成 K_<Name>)
			return "(" + eg.mapTypeToC(ident.Name) + "){0}"
		}
	}

	// 检查是否是类构造函数调用：ClassName(args) → ClassName_new(args)
	// 同时处理无参的 ClassName() 情况（如果类有默认构造函数或无参构造）
	// 泛型类/结构体：Box<int>(args) → Box_int_new(args)，先触发类型实例化
	//（typedef 与构造器体经 InstantiateGenericType 生成，构造器体进入
	// classImplCode 延迟段），调用点改用 mangled 实例名。
	if ident, ok := e.Function.(*ast.Identifier); ok {
		tg := eg.codegen.typeGenerator
		// 泛型类型定义（struct/class <T>）在实例化前不进 structTypes/classTypes
		// 注册表，此处直接查 AST 判定
		isGenericTypeDef := false
		if prog := eg.codegen.program; prog != nil {
			if cs := prog.FindClass(ident.Name); cs != nil && cs.Generic {
				isGenericTypeDef = true
			}
			if ss := prog.FindStruct(ident.Name); ss != nil && ss.Generic {
				isGenericTypeDef = true
			}
		}
		isCtorType := eg.codegen.IsClassType(ident.Name) ||
			(tg != nil && tg.structTypes[ident.Name]) ||
			isGenericTypeDef
		if isCtorType {
			ctorTypeName := ident.Name
			if len(e.TypeArgs) > 0 {
				if _, err := eg.codegen.InstantiateGenericType(ident.Name, e.TypeArgs, e.Pos.Line); err != nil {
					return "/* generic type ctor error: " + err.Error() + " */ " + ident.Name + "_new()"
				}
				ctorTypeName = MangleGenericTypeName(ident.Name, e.TypeArgs)
			}
			// 值语义 struct（非 class）：不生成 _new（其返回指针与值语义冲突），
			// 改为 C 复合字面量按字段位置初始化：(K_X){ .f1 = a1, ... }
			if eg.codegen.program != nil {
				ss := eg.codegen.program.FindStruct(ident.Name)
				isValueStruct := ss != nil && eg.codegen.program.FindClass(ident.Name) == nil
				if isValueStruct {
					cName := kaulaStructTag(ctorTypeName)
					parts := make([]string, 0, len(e.Args))
					for i, arg := range e.Args {
						if i >= len(ss.Fields) {
							break
						}
						parts = append(parts, fmt.Sprintf(".%s = %s", ss.Fields[i].Name, eg.GenerateExpression(arg)))
					}
					if len(parts) == 0 {
						return "(" + cName + "){0}"
					}
					return "(" + cName + "){ " + strings.Join(parts, ", ") + " }"
				}
			}
			ctorFuncName := ctorTypeName + "_new"
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
		if _, instErr := eg.codegen.InstantiateGeneric(funcName, e.TypeArgs, e.Pos.Line); instErr != nil {
			eg.codegen.error(fmt.Sprintf("泛型实例化失败: %v", instErr))
		}
		// 无论首次实例化还是已缓存，调用点都使用 mangled 名称
		funcName = MangleGenericName(funcName, e.TypeArgs)
	}

	// 用户自定义函数在 C 侧统一加 K_ 前缀（避开 C 保留字 int/double 等与 C 库符号）。
	// 语义查询（捕获参数、stdlib 签名、println 判断）仍用原始 Kaula 名 funcName，
	// 只有最终拼 C 输出时才用 cFuncName，避免查询表失配。
	cFuncName := eg.codegen.UserFuncCName(funcName)

	// 追踪第三方库的使用
	if eg.codegen.stdlibConfig != nil {
		if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(funcName); isThirdParty {
			eg.codegen.usedThirdPartyLibs[lib.Name] = true
		}
	}

	// assert 内置：测试模式绑定 runner 的失败记录函数（F12：
	// 此前语言层无任何方式让 #[test] 真正失败），普通模式失败即退出非零
	if funcName == "assert" {
		return eg.generateAssertCall(e.Args)
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
		capArgs := eg.codegen.captureArgsFor(funcName)
		if capArgs != "" {
			return cFuncName + "(" + capArgs + ")"
		}
		return cFuncName + "()"
	} else {
		// 尝试查找 stdlib 函数签名以生成类型正确的参数
		var sig *stdlib.Function
		if eg.codegen.stdlibConfig != nil {
			sig = eg.codegen.stdlibConfig.GetAnyFunctionSignature(funcName)
		}
		code := cFuncName + "(" + eg.generateStdlibArgs(e.Args, sig)
		if capArgs := eg.codegen.captureArgsFor(funcName); capArgs != "" {
			code += ", " + capArgs
		}
		code += ")"
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

// isNullLiteralExpr 判断表达式是否为 null/NULL 字面量
func isNullLiteralExpr(expr ast.Expression) bool {
	id, ok := expr.(*ast.Identifier)
	return ok && (id.Name == "null" || id.Name == "NULL")
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
				// 参数期望 C 字符串：字符串字面量直接输出，String 变量用 string_cdata()（SSO 感知）
				if strLit, ok := arg.(*ast.StringLiteral); ok {
					escaped := escapeCString(strLit.Value)
					code += "\"" + escaped + "\""
				} else {
					argCode := eg.GenerateExpression(arg)
					argType := eg.inferType(arg)
					if argType == "s" {
						code += "string_cdata(" + argCode + ")"
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

// generateEnumConstructor 生成枚举变体构造的 C 代码。
// EnuName.Variant(args) → (K_EnuName){.kind=EnuName_Kind_Variant, .data.Variant_val=arg}
// EnuName.Variant()      → (K_EnuName){.kind=EnuName_Kind_Variant}
// 泛型枚举（Option<int>）按参数表达式推断具体类型参数并实例化。
func (eg *ExpressionGenerator) generateEnumConstructor(enumBase string, variant *ast.EnumVariant, variantName string, args []ast.Expression) string {
	enumStmt := eg.codegen.program.FindEnum(enumBase)
	instName := enumBase

	// 泛型枚举：从构造参数推断类型参数（如 Option<int>.Some(42) → int）
	if enumStmt != nil && enumStmt.Generic {
		var argTypes []string
		for i, arg := range args {
			t := eg.inferConcreteType(arg)
			if t == "" && i < len(variant.FieldTypes) {
				t = variant.FieldTypes[i]
			}
			if t != "" {
				argTypes = append(argTypes, t)
			}
		}
		if len(argTypes) == 0 {
			argTypes = []string{"i32"}
		}
		eg.codegen.InstantiateGenericType(enumBase, argTypes, 0)
		instName = MangleGenericTypeName(enumBase, argTypes)
	}

	tag := kaulaStructTag(instName)
	if len(variant.FieldTypes) == 0 {
		// 无载荷变体：仅设置 kind
		return "(" + tag + "){.kind=" + instName + "_Kind_" + variantName + "}"
	}

	// 带载荷变体：设置 kind 并填充 data 字段
	parts := []string{".kind=" + instName + "_Kind_" + variantName}
	for i, arg := range args {
		fieldName := variantName + "_val"
		if len(variant.FieldNames) > i && variant.FieldNames[i] != "" {
			fieldName = variant.FieldNames[i]
		} else if len(variant.FieldTypes) > 1 {
			fieldName = fmt.Sprintf("%s_val%d", variantName, i)
		}
		fieldPart := ".data." + fieldName + "=" + eg.GenerateExpression(arg)
		parts = append(parts, fieldPart)
	}
	return "(" + tag + "){\n        " + strings.Join(parts, ",\n        ") + "\n    }"
}

// inferConcreteType 从表达式推断 Kaula 类型名（用于泛型实例化参数推断）。
// 注意：必须返回规范类型名（i32/f64 等），与 sema 的 numeric_names 规范保持一致，
// 否则泛型实例化 key 会出现 K_Option_int 与 K_Option_i32 不一致的 C 类型冲突。
func (eg *ExpressionGenerator) inferConcreteType(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return "i32"
	case *ast.FloatLiteral:
		return "f64"
	case *ast.StringLiteral:
		return "string"
	case *ast.BooleanLiteral:
		return "bool"
	case *ast.Identifier:
		if sym := eg.codegen.GetSymbol(e.Name); sym != nil {
			return sym.Type
		}
	case *ast.CallExpression:
		return "i64"
	case *ast.MemberAccessExpression:
		return "i32"
	case *ast.UnaryExpression:
		return eg.inferConcreteType(e.Right)
	case *ast.BinaryExpression:
		return eg.inferConcreteType(e.Right)
	}
	return ""
}

// generateMethodCall 生成方法调用代码
func (eg *ExpressionGenerator) generateMethodCall(memberAccess *ast.MemberAccessExpression, args []ast.Expression) string {
	// 枚举变体构造：EnuName.VariantName(args) 或 EnuName.VariantName
	// 生成 tagged union 字面量（如 (K_Option_int){.kind=Option_int_Kind_Some, .data.Some_val=42}）
	if ident, ok := memberAccess.Object.(*ast.Identifier); ok {
		if eg.codegen.IsEnumType(ident.Name) {
			if variant := eg.findEnumVariant(ident.Name, memberAccess.Member); variant != nil {
				return eg.generateEnumConstructor(ident.Name, variant, memberAccess.Member, args)
			}
		}
	}

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

	// ===== 链式/平铺标准库调用统一生成 =====
	// 支持 std.<mod>.<fn>() 平铺与 std.<mod>.<seg1>().<seg2>() 链式两种形式：
	// 链段用 "_" 拼接后配合模块前缀解析出 C 函数名（如 compare+ignore_case → string_compare_ignore_case）。
	// 命中后直接生成 C 调用并返回，避免内层链段被当作独立调用生成无效代码。
	if modKey, segs, isChain := astutil.ExtractStdlibChain(memberAccess); isChain && eg.codegen.stdlibConfig != nil {
		if fn, cName, resolved := eg.codegen.stdlibConfig.ResolveChainedFunction(modKey, segs); resolved {
			lastSeg := segs[len(segs)-1]

			// 特殊处理 println：多参数使用类型推导版本（与平铺路径行为一致）
			if lastSeg == "println" && len(args) > 1 {
				return eg.generatePrintlnMulti(args)
			}

			// 追踪第三方库使用（若链段名命中第三方库函数）
			if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(lastSeg); isThirdParty {
				eg.codegen.usedThirdPartyLibs[lib.Name] = true
			}

			// 修复 #21：仅在 freestanding 模式下将 std_malloc 重写为
			// kmm_v4_alloc_auto（无 libc）。hosted 模式保留 std_malloc
			// 系统 malloc 语义，与 kmm_v4 系列分配器严格区分。
			if (cName == "std_malloc" || lastSeg == "std_malloc") && eg.codegen.IsFreestanding() {
				if len(args) == 1 {
					sizeArg := eg.GenerateExpression(args[0])
					return "kmm_v4_alloc_auto(" + sizeArg + ")"
				}
			}

			return cName + "(" + eg.generateStdlibArgs(args, fn) + ")"
		}
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

				// 修复 #21：仅在 freestanding 模式下将 std_malloc 重写为
				// kmm_v4_alloc_auto（无 libc）。hosted 模式保留 std_malloc
				// 系统 malloc 语义，与 kmm_v4 系列分配器严格区分。
				if (cFuncName == "std_malloc" || methodName == "std_malloc") &&
					eg.codegen.IsFreestanding() {
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
				if funcSig, funcExists := lib.Functions[methodName]; funcExists {
					eg.codegen.usedThirdPartyLibs[libName] = true
					// 使用签名生成参数（const char* 参数自动拆包 String）
					code := methodName + "(" + eg.generateStdlibArgs(args, &funcSig) + ")"
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
	switch e := expr.(type) {
	case *ast.Identifier:
		// 直接的变量引用：查符号表获取其 Kaula 类型名
		sym := eg.codegen.currentScope.GetSymbol(e.Name)
		if sym != nil {
			typeName := strings.TrimSuffix(sym.Type, "*")
			if typeName != "" && eg.codegen.findClassStmt(typeName) != nil {
				return typeName
			}
		}
	case *ast.MemberAccessExpression:
		// obj.field：解析字段类型。class 字段（含 A* 指针字段）在 C 中始终是指针，
		// 若字段本身是 class 类型，则返回该类名，使上层成员访问改用 ->。
		objType := eg.inferClassType(e.Object)
		if objType == "" {
			return ""
		}
		classStmt := eg.codegen.findClassStmt(objType)
		if classStmt == nil {
			return ""
		}
		for _, f := range classStmt.Fields {
			if f.Name == e.Member {
				fieldType := strings.TrimSuffix(f.Type, "*")
				if eg.codegen.findClassStmt(fieldType) != nil {
					return fieldType
				}
				return ""
			}
		}
		return ""
	case *ast.IndexExpression:
		// arr[i] / ptr[i]：元素类型与数组变量类型相同（class 指针字段的数组元素）
		elemType := eg.inferClassType(e.Object)
		if elemType != "" && eg.codegen.findClassStmt(elemType) != nil {
			return elemType
		}
		return ""
	case *ast.CallExpression:
		// ClassName(args...) 或 ClassName_new(...)：返回调用的类名
		if ident, ok := e.Function.(*ast.Identifier); ok {
			name := strings.TrimSuffix(ident.Name, "_new")
			if eg.codegen.findClassStmt(name) != nil {
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

// generatePrintlnCall 生成 println 调用代码
// 支持类型推导自动判断格式化参数
// generateAssertCall 生成 assert(cond) 内置调用（F12）。
// 测试模式：绑定 runner 的 __kaula_test_assert，失败被记录、测试计数正确、
// 进程最终返回非零；普通模式：失败打印断言文本并 exit(1)。
// cond 的 C 文本兼作断言消息（内嵌进字符串前把双引号降级为单引号防转义破坏）。
func (eg *ExpressionGenerator) generateAssertCall(args []ast.Expression) string {
	if len(args) != 1 {
		eg.codegen.error("assert 需要恰好 1 个参数")
		return "assert(/* arity error */)"
	}
	cond := eg.GenerateExpression(args[0])
	msg := strings.ReplaceAll(cond, "\"", "'")
	if eg.codegen.testRunnerActive {
		return fmt.Sprintf("__kaula_test_assert((int)(%s), \"%s\", __FILE__, __LINE__)", cond, msg)
	}
	return fmt.Sprintf("if (!(%s)) { fprintf(stderr, \"assertion failed: (%s) at %%s:%%d\\n\", __FILE__, __LINE__); exit(1); }", cond, msg)
}

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

// isStringLikeFmt 判断 inferType 结果是否为字符串类("s"=String 值, "cstr"=char*)
func (eg *ExpressionGenerator) isStringLikeFmt(t string) bool {
	return t == "s" || t == "cstr"
}

// generateStringConcat 生成字符串拼接: "a" + x / x + "a" / "a" + "b"
// 映射到内联运行时 string_concat / string_concat_int / string_concat_float
func (eg *ExpressionGenerator) generateStringConcat(e *ast.BinaryExpression, lt, rt string) string {
	eg.codegen.needsStringConcat = true
	left := eg.GenerateExpression(e.Left)
	right := eg.GenerateExpression(e.Right)
	// 左侧是字符串: string_concat[_int|_float](left, right)
	if eg.isStringLikeFmt(lt) {
		if eg.isStringLikeFmt(rt) {
			return "string_concat(" + left + ", " + right + ")"
		}
		if rt == "f" {
			return "string_concat_float(" + left + ", " + right + ")"
		}
		return "string_concat_int(" + left + ", " + right + ")"
	}
	// 左侧非字符串, 右侧是字符串: 交换操作数(数字放右侧)
	if lt == "f" {
		return "string_concat_float(" + right + ", " + left + ")"
	}
	return "string_concat_int(" + right + ", " + left + ")"
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
		argType := eg.inferType(args[i])
		argCode := eg.GenerateExpression(args[i])
		// string 变量(String*)需要解包为 char* 才能用 %s 打印
		if argType == "s" || argType == "cstr" {
			argCode = eg.maybeUnwrapString(argCode, argType)
		}
		b.WriteString(", ")
		b.WriteString(argCode)
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

// maybeUnwrapString 对 string 类型表达式用 string_cdata() 以匹配 C %s 约定（SSO 感知）
// cstr 类型（char*）已经是 C 字符串指针，不需要转换
func (eg *ExpressionGenerator) maybeUnwrapString(code string, typeHint string) string {
	if typeHint == "s" {
		return "string_cdata(" + code + ")"
	}
	return code
}

// inferType 推导表达式的类型（带缓存）
// qualifiedRegisteredInferType 解析 std.<mod>.f(...) / <pkg>.f(...) 形式的
// 限定调用：沿 MemberAccess 链取末端函数名，查 stdlib/pkglib 注册签名的
// 返回类型。仅当链根是已知模块前缀（std.* / freestanding.*）时命中，
// 避免与 obj.method 实例方法混淆。
func (eg *ExpressionGenerator) qualifiedRegisteredInferType(fn ast.Expression) (string, bool) {
	if eg.codegen.stdlibConfig == nil {
		return "", false
	}
	var parts []string
	cur := fn
	for {
		ma, ok := cur.(*ast.MemberAccessExpression)
		if !ok {
			break
		}
		parts = append([]string{ma.Member}, parts...)
		cur = ma.Object
	}
	root, ok := cur.(*ast.Identifier)
	if !ok {
		return "", false
	}
	parts = append([]string{root.Name}, parts...)
	if len(parts) < 3 {
		return "", false
	}
	if parts[0] != "std" && parts[0] != "freestanding" {
		return "", false
	}
	short := parts[len(parts)-1]
	for _, mod := range eg.codegen.stdlibConfig.Modules {
		if sig, ok := mod.Functions[short]; ok && sig.Return != "" {
			if t, hit := registeredReturnInferType(sig.Return); hit {
				return t, true
			}
			return "d", true
		}
	}
	for _, lib := range eg.codegen.stdlibConfig.ThirdParty {
		if sig, ok := lib.Functions[short]; ok && sig.Return != "" {
			if t, hit := registeredReturnInferType(sig.Return); hit {
				return t, true
			}
			return "d", true
		}
	}
	return "", false
}

// registeredReturnInferType 把 stdlib/pkglib 注册的返回类型映射为
// inferType 类型码；忽略大小写以兼容 stdlib.json 的 "String" 真实形态。
func registeredReturnInferType(rt string) (string, bool) {
	switch strings.ToLower(rt) {
	case "string", "str":
		return "s", true
	case "float", "float64", "float32", "f32", "f64", "double", "single":
		return "f", true
	}
	t := strings.ToLower(rt)
	if normalizePtrType(t) == "char*" || t == "cstring" || t == "const char*" {
		return "cstr", true
	}
	return "", false
}

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
		// 字符串 + 拼接: 嵌套表达式 (如 a + b + c) 两侧推断为 "s" 时结果仍为 string
		if (e.Operator == "+" || e.Operator == "PLUS") && (leftType == "s" || rightType == "s") {
			return "s"
		}
		if leftType == "f" || rightType == "f" {
			return "f"
		}
		return "d"
	case *ast.ParenExpression:
		return eg.inferType(e.Inner)
	case *ast.MemberAccessExpression:
		// 动态对象成员访问
		if eg.isObjectTyped(e.Object) {
			return "obj"
		}
		// 类字段成员访问: 从类定义中查字段的 Kaula 类型
		if fieldType := eg.memberFieldKaulaType(e); fieldType != "" {
			switch {
			case fieldType == "string" || fieldType == "str" || fieldType == "String":
				return "s"
			case fieldType == "cstring" || fieldType == "cstr":
				return "cstr"
			case isFloatTypeToken(fieldType):
				return "f"
			default:
				if normalizePtrType(fieldType) == "char*" {
					return "cstr"
				}
				return "d"
			}
		}
		return "d"
	case *ast.IndexExpression:
		// 索引访问: 从对象类型推断元素类型(f64* → f64 → %f)
		elemType := eg.indexElementKaulaType(e.Object)
		if isFloatTypeToken(elemType) {
			return "f"
		}
		if elemType == "string" || elemType == "str" {
			return "s"
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
		// 模块限定调用 std.<mod>.f(...)：e.Function 是 MemberAccess 链，
		// 此前只处理 Identifier，整类限定调用（含注册的 String 返回函数）
		// 推断落空 → println 用整型格式直传 String 值打非确定垃圾
		if t, hit := eg.qualifiedRegisteredInferType(e.Function); hit {
			return t
		}
		if ident, ok := e.Function.(*ast.Identifier); ok {
			funcName := ident.Name
			// 用户定义的顶层/类方法函数：program 中直查返回类型。
			// （此前仅查本地符号表与 stdlib，普通 Kaula 函数调用返回
			//   string 时推断失败 fallback 到 %d，println 打印出结构体垃圾）
			if prog := eg.codegen.getProgram(); prog != nil {
				if fn := prog.FindFunction(funcName); fn != nil && fn.ReturnType != "" {
					switch fn.ReturnType {
					case "string", "str":
						return "s"
					case "float", "float64", "float32", "f32", "f64", "double", "single":
						return "f"
					case "bool":
						return "d"
					}
					t := strings.ToLower(fn.ReturnType)
					if normalizePtrType(t) == "char*" || t == "cstring" {
						return "cstr"
					}
				}
			}
			// 跨文件 import 的 pub/export 函数：定义不在本文件 AST 中，
			// 从预收集的返回类型映射回查
			if rt, ok := eg.codegen.localImportReturns[funcName]; ok && rt != "" {
				switch rt {
				case "string", "str":
					return "s"
				case "float", "float64", "float32", "f32", "f64", "double", "single":
					return "f"
				case "bool":
					return "d"
				}
				t := strings.ToLower(rt)
				if normalizePtrType(t) == "char*" || t == "cstring" {
					return "cstr"
				}
			}
			// 先查本地函数符号表/extern fn 声明以获得返回类型注解；
			// 本地函数找不到再查 stdlib
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
				// stdlib/pkglib 按短名注册；点号全名调用（std.string.xxx()）
				// 需回退末段匹配；返回类型比对忽略大小写（stdlib.json 真实
				// 形态为大写 "String"，此前精确比较 "string" 恒 miss，
				// println 退化为 printf 整型格式直传 String 值（打非确定垃圾）
				shortName := funcName
				if i := strings.LastIndex(shortName, "."); i >= 0 {
					shortName = shortName[i+1:]
				}
				for _, mod := range eg.codegen.stdlibConfig.Modules {
					sig, ok := mod.Functions[funcName]
					if !ok && shortName != funcName {
						sig, ok = mod.Functions[shortName]
					}
					if ok && sig.Return != "" {
						if t, hit := registeredReturnInferType(sig.Return); hit {
							return t
						}
						return "d"
					}
				}
				// 第三方库（pkglib）的函数签名单独存放
				for _, lib := range eg.codegen.stdlibConfig.ThirdParty {
					sig, ok := lib.Functions[funcName]
					if !ok && shortName != funcName {
						sig, ok = lib.Functions[shortName]
					}
					if ok && sig.Return != "" {
						if t, hit := registeredReturnInferType(sig.Return); hit {
							return t
						}
						return "d"
					}
				}
			}
		}
		// 方法调用 obj.method(...)：从类定义查方法返回类型，
		// 否则 println 等方法调用点的 printf 格式会把 f64 返回值当 %d
		if member, ok := e.Function.(*ast.MemberAccessExpression); ok {
			if retType := eg.memberMethodReturnType(member); retType != "" {
				switch {
				case retType == "string" || retType == "str":
					return "s"
				case isFloatTypeToken(retType):
					return "f"
				case normalizePtrType(retType) == "char*" || retType == "cstring":
					return "cstr"
				default:
					return "d"
				}
			}
			// stdlib 模块限定函数调用(std.math.math_sqrt)返回类型
			if sigRet := eg.stdlibMemberFunctionSig(member); sigRet != "" {
				switch {
				case sigRet == "string" || sigRet == "str":
					return "s"
				case isFloatTypeToken(sigRet):
					return "f"
				case normalizePtrType(sigRet) == "char*" || sigRet == "cstring":
					return "cstr"
				default:
					return "d"
				}
			}
		}
		return "d"
	default:
		return "d"
	}
}

// indexElementKaulaType 返回索引访问的对象元素 Kaula 类型:
//
//	self.values(f64* 字段) → "f64"; [3]f64 / []f64 变量 → "f64"
//
// 用于 println(data[i]) 的格式推断(否则 f64 索引访问被当 %d 打印成 0)
func (eg *ExpressionGenerator) indexElementKaulaType(obj ast.Expression) string {
	t := ""
	switch o := obj.(type) {
	case *ast.MemberAccessExpression:
		t = eg.memberFieldKaulaType(o)
	case *ast.Identifier:
		if sym := eg.codegen.currentScope.GetSymbol(o.Name); sym != nil {
			t = sym.Type
		}
	}
	t = strings.TrimSpace(t)
	if strings.HasSuffix(t, "*") {
		return strings.TrimSuffix(t, "*")
	}
	if strings.HasPrefix(t, "[") {
		if idx := strings.Index(t, "]"); idx >= 0 {
			return t[idx+1:]
		}
	}
	return t
}

// isFloatTypeToken 判断 Kaula 类型名是否为浮点类型
// formatFloatLiteral 格式化浮点字面量,整数值保留小数点(1000.0 -> "1000.0"),
// 避免被 C 当成整数(整数除法等语义陷阱);指数形式(如 1e-300)保持短表示,
// 不用定点展开
func formatFloatLiteral(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func isFloatTypeToken(t string) bool {
	switch t {
	case "float", "float32", "float64", "f32", "f64", "double", "single", "real":
		return true
	}
	return false
}

// memberFieldKaulaType 返回类字段的 Kaula 类型名。
// 找不到类/字段时返回空串。
// 支持 self.field、类变量.field 以及嵌套成员链 (a.b.c, a/b 均为类类型)。
func (eg *ExpressionGenerator) memberFieldKaulaType(e *ast.MemberAccessExpression) string {
	typeName := eg.memberObjectClassName(e.Object)
	if typeName == "" {
		return ""
	}
	// 先查 class 字段
	if classStmt := eg.codegen.findClassStmt(typeName); classStmt != nil {
		for _, f := range classStmt.Fields {
			if f != nil && f.Name == e.Member {
				return f.Type
			}
		}
	}
	// 再查 struct 字段（F7 修复：println(a.owner) 等成员访问类型推断）
	if prog := eg.codegen.getProgram(); prog != nil {
		if structStmt := prog.FindStruct(typeName); structStmt != nil {
			for _, f := range structStmt.Fields {
				if f != nil && f.Name == e.Member {
					return f.Type
				}
			}
		}
	}
	return ""
}

// memberMethodReturnType 返回类方法的 Kaula 返回类型名。
// 找不到类/方法时返回空串。支持 self.method() / 类变量.method()。
func (eg *ExpressionGenerator) memberMethodReturnType(e *ast.MemberAccessExpression) string {
	if eg.codegen.program == nil {
		return ""
	}
	className := eg.memberObjectClassName(e.Object)
	if className == "" {
		return ""
	}
	classStmt := eg.codegen.program.FindClass(className)
	if classStmt == nil {
		return ""
	}
	for _, m := range classStmt.Methods {
		if m != nil && m.Name == e.Member {
			return m.ReturnType
		}
	}
	return ""
}

// stdlibMemberFunctionSig 返回 stdlib 模块限定函数(std.math.math_sqrt)的 Kaula 返回类型名。
// 解析规则与 generateMethodCall 的模块识别一致；查不到返回空串。
func (eg *ExpressionGenerator) stdlibMemberFunctionSig(e *ast.MemberAccessExpression) string {
	if eg.codegen.stdlibConfig == nil {
		return ""
	}
	methodName := e.Member
	moduleName := ""
	isFreeModuleCall := false
	if ident, ok := e.Object.(*ast.Identifier); ok {
		moduleName = ident.Name
	} else if nested, ok := e.Object.(*ast.MemberAccessExpression); ok {
		moduleName = nested.Member
		if innerIdent, ok := nested.Object.(*ast.Identifier); ok && innerIdent.Name == "freestanding" {
			isFreeModuleCall = true
		}
	}
	if moduleName == "" {
		return ""
	}
	stdlibKey := moduleName
	if isFreeModuleCall {
		stdlibKey = "freestanding." + moduleName
	} else if !strings.HasPrefix(stdlibKey, "std.") {
		stdlibKey = "std." + moduleName
	}
	if module, exists := eg.codegen.stdlibConfig.Modules[stdlibKey]; exists {
		if sig, ok := module.Functions[methodName]; ok {
			return sig.Return
		}
	}
	if isThirdParty, lib := eg.codegen.stdlibConfig.IsThirdPartyFunction(methodName); isThirdParty {
		if sig, ok := lib.Functions[methodName]; ok {
			return sig.Return
		}
	}
	return ""
}

// memberObjectClassName 推断成员访问对象表达式的类名。
// 找不到已定义的类时返回空串。
func (eg *ExpressionGenerator) memberObjectClassName(obj ast.Expression) string {
	// 辅助函数：检查类型名是 class 还是 struct，是则返回
	isKnownType := func(name string) string {
		if eg.codegen.findClassStmt(name) != nil {
			return name
		}
		if prog := eg.codegen.getProgram(); prog != nil && prog.FindStruct(name) != nil {
			return name
		}
		return ""
	}
	switch e := obj.(type) {
	case *ast.Identifier:
		if e.Name == "self" {
			if eg.codegen.currentClassName != "" {
				return eg.codegen.currentClassName
			}
			if sym := eg.codegen.currentScope.GetSymbol("self"); sym != nil {
				name := strings.TrimSuffix(sym.Type, "*")
				if t := isKnownType(name); t != "" {
					return t
				}
			}
			return ""
		}
		if sym := eg.codegen.currentScope.GetSymbol(e.Name); sym != nil {
			name := strings.TrimSuffix(sym.Type, "*")
			if t := isKnownType(name); t != "" {
				return t
			}
		}
	case *ast.MemberAccessExpression:
		fieldType := eg.memberFieldKaulaType(e)
		if fieldType != "" {
			name := strings.TrimSuffix(fieldType, "*")
			if t := isKnownType(name); t != "" {
				return t
			}
		}
	}
	return ""
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

// isIntegerExpr 判断表达式是否为整数类型（用于溢出检查）。
// 整数类型：int、i8/i16/i32/i64、u8/u16/u32/u64、整数字面量、布尔值。
func (eg *ExpressionGenerator) isIntegerExpr(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.IntegerLiteral, *ast.BooleanLiteral:
		return true
	case *ast.Identifier:
		if sym := eg.codegen.GetSymbol(e.Name); sym != nil {
			return isIntegerTypeName(sym.Type)
		}
		if sym := eg.codegen.currentScope.GetSymbol(e.Name); sym != nil {
			return isIntegerTypeName(sym.Type)
		}
	case *ast.BinaryExpression:
		// 算术运算结果为整数（如果操作数都是整数）
		if e.Operator == "PLUS" || e.Operator == "+" || e.Operator == "MINUS" || e.Operator == "-" ||
			e.Operator == "MULTIPLY" || e.Operator == "*" || e.Operator == "DIVIDE" || e.Operator == "/" ||
			e.Operator == "MOD" || e.Operator == "%" {
			return eg.isIntegerExpr(e.Left) && eg.isIntegerExpr(e.Right)
		}
		// 比较/逻辑运算结果为布尔值（整数）
		return true
	case *ast.CallExpression:
		// 函数调用保守认为可能是整数
		return true
	}
	return false
}

// isUnsignedIntegerExpr 判断表达式是否为无符号整数类型（用于选择 u64 版本溢出检查）。
func (eg *ExpressionGenerator) isUnsignedIntegerExpr(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		if sym := eg.codegen.GetSymbol(e.Name); sym != nil {
			return isUnsignedTypeName(sym.Type)
		}
		if sym := eg.codegen.currentScope.GetSymbol(e.Name); sym != nil {
			return isUnsignedTypeName(sym.Type)
		}
	case *ast.BinaryExpression:
		// 算术运算：如果任一操作数是无符号，结果为无符号
		if e.Operator == "PLUS" || e.Operator == "+" || e.Operator == "MINUS" || e.Operator == "-" ||
			e.Operator == "MULTIPLY" || e.Operator == "*" {
			return eg.isUnsignedIntegerExpr(e.Left) || eg.isUnsignedIntegerExpr(e.Right)
		}
	}
	return false
}

// isIntegerTypeName 判断类型名是否为整数类型
func isIntegerTypeName(typeName string) bool {
	switch typeName {
	case "int", "int8", "int16", "int32", "int64",
		"i8", "i16", "i32", "i64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"u8", "u16", "u32", "u64",
		"bool", "char":
		return true
	}
	return false
}

// isUnsignedTypeName 判断类型名是否为无符号整数类型
func isUnsignedTypeName(typeName string) bool {
	switch typeName {
	case "uint", "uint8", "uint16", "uint32", "uint64",
		"u8", "u16", "u32", "u64":
		return true
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
		return "dynobj_box_f64(" + formatFloatLiteral(e.Value) + ")"
	case *ast.BooleanLiteral:
		if e.Value {
			return "dynobj_box_bool(1)"
		}
		return "dynobj_box_bool(0)"
	case *ast.StringLiteral:
		return "dynobj_box_cstr(\"" + escapeCString(e.Value) + "\")"
	case *ast.LambdaExpression:
		eg.codegen.needsObjRuntime = true
		eg.codegen.error("lambda boxing into object requires an environment-aware object ABI")
		lambdaName := "_unsupported_lambda"
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

	// 检查是否是枚举常量引用（如 Color.Red 或 Option.None）
	if ident, ok := e.Object.(*ast.Identifier); ok {
		if eg.codegen.IsEnumType(ident.Name) {
			// 静态访问枚举类型：使用实例化后的 C 名称
			cEnumName := eg.getEnumCName(ident.Name)
			// 查找变体信息，判断是否需要生成 compound literal
			if variant := eg.findEnumVariant(ident.Name, e.Member); variant != nil {
				// 检查枚举是否为 tagged union（有数据变体）
				enumStmt := eg.codegen.program.FindEnum(ident.Name)
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
					// Tagged union：生成 compound literal
					tag := kaulaStructTag(cEnumName)
					if len(variant.FieldTypes) == 0 {
						return "(" + tag + "){.kind=" + cEnumName + "_Kind_" + e.Member + "}"
					}
					// 有载荷变体静态访问不应直接使用（需函数调用语法）
				}
			}
			// 简单枚举或有载荷变体的静态引用：使用 enum 常量
			return cEnumName + "_Kind_" + e.Member
		}
	}

	isPtr := false
	if ident, ok := e.Object.(*ast.Identifier); ok {
		if sym := eg.codegen.GetSymbol(ident.Name); sym != nil {
			typeStr := sym.Type
			if strings.HasSuffix(typeStr, "*") || strings.HasPrefix(typeStr, "*") {
				isPtr = true
			}
			// 类型别名可能包裹指针（type P = *Node）：符号类型记录的是别名 PNode，
			// 需解析别名底层类型，若底层是指针则同样按 -> 访问。
			if !isPtr {
				if ta := eg.codegen.program.FindTypeAlias(typeStr); ta != nil {
					ut := ta.UnderlyingType
					if ta.IsAdopt && ta.AdoptUnderlying != "" {
						ut = ta.AdoptUnderlying
					}
					ut = strings.TrimSpace(ut)
					if strings.HasPrefix(ut, "*") || strings.HasSuffix(ut, "*") {
						isPtr = true
					}
				}
			}
			// 类类型变量始终按指针传递和存储（K_ClassName*），
			// 即使符号表中类型名未显式带 *，也视为指针访问。
			if !isPtr && eg.codegen.IsClassType(typeStr) {
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

	// String.ptr 特殊处理：SSO 感知，用 string_cdata() 替代直接访问 .ptr
	if e.Member == "ptr" {
		objType := eg.inferType(e.Object)
		if objType == "s" || objType == "string" {
			return "string_cdata(" + object + ")"
		}
	}

	return object + "." + e.Member
}

// generateTypeCastExpression 生成类型转换表达式代码
func (eg *ExpressionGenerator) generateTypeCastExpression(e *ast.TypeCastExpression) string {
	exprCode := eg.GenerateExpression(e.Expression)
	cType := eg.mapTypeToC(e.TargetType)
	srcKind := eg.inferType(e.Expression)
	switch e.TargetType {
	case "cstring", "cstr":
		// String 结构体 → const char*: 用 string_cdata()（SSO 感知）
		if srcKind == "s" {
			return "string_cdata(" + exprCode + ")"
		}
		// 成员访问表达式（如 enum 变体字段 r.data.Err_val）类型推断可能失败，
		// 若目标是 cstring/cstr，也用 string_cdata()
		if _, ok := e.Expression.(*ast.MemberAccessExpression); ok {
			if e.TargetType == "cstring" || e.TargetType == "cstr" {
				return "string_cdata(" + exprCode + ")"
			}
		}
		// 源已是 C 字符串（字面量 cstring / cstring 变量 / 指针），直接透传
		if srcKind == "cstr" {
			return exprCode
		}
	case "string", "str":
		// 源已是 String 结构体，直接透传
		if srcKind == "s" {
			return exprCode
		}
	}
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
//
// GenerateMatchAssign 生成 match 表达式作为赋值源的 C 代码：
//
//	switch (x) {
//	case 1: { targetVar = expr; break; }
//	...
//	}
//
// 用于 `type v = match(x) { ... }`（C 的 switch 不能作为表达式值）。
// generateStringMatchAssign 生成字符串匹配赋值的 C 代码（if-else 链）
func (eg *ExpressionGenerator) generateStringMatchAssign(match *ast.MatchExpression, targetVar, targetCode string) string {
	var code strings.Builder
	first := true
	for _, arm := range match.Arms {
		if arm.Pattern == nil {
			continue
		}
		if arm.Pattern.Kind == ast.PatternString {
			if first {
				code.WriteString("if (")
				first = false
			} else {
				code.WriteString(" else if (")
			}
			code.WriteString("strcmp(string_cdata(")
			code.WriteString(targetCode)
			code.WriteString("), \"")
			code.WriteString(arm.Pattern.StrValue)
			code.WriteString("\") == 0")
			code.WriteString(") {\n")
			// 生成分支体语句；最后一条表达式语句作为结果赋值给 targetVar
			for i, bodyStmt := range arm.Body {
				if i == len(arm.Body)-1 {
					if es, ok := bodyStmt.(*ast.ExpressionStatement); ok {
						code.WriteString("    ")
						code.WriteString(targetVar)
						code.WriteString(" = ")
						code.WriteString(eg.GenerateExpression(es.Expression))
						code.WriteString(";\n")
						continue
					}
				}
				code.WriteString("    ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("}\n")
		} else if arm.Pattern.Kind == ast.PatternWildcard {
			code.WriteString("else {\n")
			for i, bodyStmt := range arm.Body {
				if i == len(arm.Body)-1 {
					if es, ok := bodyStmt.(*ast.ExpressionStatement); ok {
						code.WriteString("    ")
						code.WriteString(targetVar)
						code.WriteString(" = ")
						code.WriteString(eg.GenerateExpression(es.Expression))
						code.WriteString(";\n")
						continue
					}
				}
				code.WriteString("    ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("}\n")
		}
	}
	return code.String()
}

func (eg *ExpressionGenerator) GenerateMatchAssign(match *ast.MatchExpression, targetVar string) string {
	return eg.lowerMatch(match, targetVar)

}

// generateStringMatchExpression 生成字符串匹配的 C 代码（if-else 链）
func (eg *ExpressionGenerator) generateStringMatchExpression(e *ast.MatchExpression, targetCode string) string {
	var code strings.Builder
	first := true
	// 判断目标类型: char*/cstr 直接用裸指针, 其他(string 等)用 string_cdata() (SSO 感知)
	targetType := eg.inferType(e.Target)
	useCdata := targetType != "cstr"
	for _, arm := range e.Arms {
		if arm.Pattern == nil {
			continue
		}
		if arm.Pattern.Kind == ast.PatternString {
			if first {
				code.WriteString("if (")
				first = false
			} else {
				code.WriteString(" else if (")
			}
			code.WriteString("strcmp(")
			if useCdata {
				code.WriteString("string_cdata(" + targetCode + ")")
			} else {
				code.WriteString(targetCode)
			}
			code.WriteString(", \"")
			code.WriteString(escapeCString(arm.Pattern.StrValue))
			code.WriteString("\") == 0")
			code.WriteString(") {\n")
			for _, bodyStmt := range arm.Body {
				code.WriteString("    ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("}\n")
		} else if arm.Pattern.Kind == ast.PatternWildcard {
			code.WriteString("else {\n")
			for _, bodyStmt := range arm.Body {
				code.WriteString("    ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			code.WriteString("}\n")
		}
	}
	return code.String()
}

func (eg *ExpressionGenerator) generateMatchExpression(e *ast.MatchExpression) string {
	targetCode := eg.GenerateExpression(e.Target)

	// 检查是否为字符串匹配：任一分支包含字符串模式
	isStringMatch := false
	for _, arm := range e.Arms {
		if arm.Pattern != nil && arm.Pattern.Kind == ast.PatternString {
			isStringMatch = true
			break
		}
	}

	if isStringMatch {
		return eg.generateStringMatchExpression(e, targetCode)
	}

	var code strings.Builder

	// 尝试从目标表达式推断枚举类型名
	enumName := eg.inferEnumName(e.Target)
	cEnumName := eg.inferEnumCName(e.Target)
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
			if !lastStmtIsControlFlow(arm.Body) {
				code.WriteString("        break;\n")
			}
			code.WriteString("    }\n")

		case ast.PatternVariant:
			// VariantName(x, y) → case Enum_Kind_VariantName: { auto_type x = ...; ... break; }
			// 支持 "枚举名.变体名" 形式 (如 Color.Red)，去掉枚举名前缀
			caseLabel := "    case "
			if cEnumName != "" {
				caseLabel += cEnumName + "_Kind_"
			}
			variantName := arm.Pattern.VariantName
			if idx := strings.LastIndex(variantName, "."); idx >= 0 {
				variantName = variantName[idx+1:]
			}
			caseLabel += variantName + ":\n"
			code.WriteString(caseLabel)
			code.WriteString("    {\n")

			// 生成绑定变量
			if len(arm.Pattern.Bindings) > 0 {
				// 查找枚举变体的字段类型
				variant := eg.findEnumVariant(enumName, variantName)
				for i, binding := range arm.Pattern.Bindings {
					fieldAccess := targetCode + ".data." + variantName + "_val"
					if variant != nil && len(variant.FieldTypes) > 1 {
						// 多字段时使用具体字段名
						if i < len(variant.FieldNames) && variant.FieldNames[i] != "" {
							fieldAccess = targetCode + ".data." + variant.FieldNames[i]
						} else {
							// 多字段但无字段名时，使用 _val0, _val1 格式
							fieldAccess = fmt.Sprintf("%s.data.%s_val%d", targetCode, arm.Pattern.VariantName, i)
						}
					}
					bindType := "auto"
					bindKaulaType := ""
					if variant != nil && i < len(variant.FieldTypes) {
						bindType = eg.matchBindingCType(variant, i, enumName, e.Target)
						// 同步注册到 codegen 符号表：println/inferType 等依赖
						// 标识符的类型记录决定格式化行为（否则 String 绑定
						// 会 fallback 到 %d 打印出指针/长度垃圾）
						if i < len(variant.FieldTypes) {
							bindKaulaType = variant.FieldTypes[i]
						}
					}
					code.WriteString(fmt.Sprintf("        %s %s = %s;\n", bindType, binding, fieldAccess))
					if binding != "_" && bindKaulaType != "" {
						eg.codegen.AddSymbol(binding, bindKaulaType, false, "local", e.Pos.Line, e.Pos.Column)
					}
				}
			}

			// 生成分支体
			for _, bodyStmt := range arm.Body {
				code.WriteString("        ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			if !lastStmtIsControlFlow(arm.Body) {
				code.WriteString("        break;\n")
			}
			code.WriteString("    }\n")

		case ast.PatternInteger:
			// 整数字面量模式
			code.WriteString(fmt.Sprintf("    case %d:\n", arm.Pattern.IntValue))
			code.WriteString("    {\n")
			for _, bodyStmt := range arm.Body {
				code.WriteString("        ")
				code.WriteString(eg.codegen.generateStatement(bodyStmt))
			}
			if !lastStmtIsControlFlow(arm.Body) {
				code.WriteString("        break;\n")
			}
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
			if !lastStmtIsControlFlow(arm.Body) {
				code.WriteString("        break;\n")
			}
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

// inferEnumName 从目标表达式推断枚举类型名（源码名，如 Option/Status）。
// 泛型枚举（如 Option<int>）返回基名 Option；C 枚举名（如 Option_int）由
// inferEnumCName 单独计算，用于生成 case 标签前缀。
func (eg *ExpressionGenerator) inferEnumName(expr ast.Expression) string {
	if ident, ok := expr.(*ast.Identifier); ok {
		// 从符号表查找变量类型
		sym := eg.codegen.GetSymbol(ident.Name)
		if sym != nil && eg.codegen.program != nil {
			// 检查类型是否是已定义的枚举
			if enumStmt := eg.codegen.program.FindEnum(sym.Type); enumStmt != nil {
				return enumStmt.Name
			}
			// 泛型枚举类型：Option<int> → 剥离泛型参数后查基定义
			if lt := strings.Index(sym.Type, "<"); lt > 0 {
				baseName := sym.Type[:lt]
				if enumStmt := eg.codegen.program.FindEnum(baseName); enumStmt != nil {
					return enumStmt.Name
				}
			}
		}
	}
	return ""
}

// inferEnumCName 返回目标枚举的 C 类型名（用于 case 标签前缀）。
// 非泛型枚举返回其名（如 Status → Status_Kind_Success）；
// 泛型枚举返回实例化名（如 Option<int> → Option_int_Kind_Some）。
func (eg *ExpressionGenerator) inferEnumCName(expr ast.Expression) string {
	baseName := eg.inferEnumName(expr)
	if baseName == "" {
		return ""
	}
	if ident, ok := expr.(*ast.Identifier); ok {
		sym := eg.codegen.GetSymbol(ident.Name)
		if sym != nil && strings.Contains(sym.Type, "<") {
			// 确保泛型类型已实例化（生成 typedef）
			if lt := strings.Index(sym.Type, "<"); lt > 0 {
				argsStr := sym.Type[lt+1 : len(sym.Type)-1]
				var args []string
				for _, part := range splitTopLevelCommas(argsStr) {
					if strings.TrimSpace(part) != "" {
						args = append(args, strings.TrimSpace(part))
					}
				}
				if len(args) > 0 {
					eg.codegen.InstantiateGenericType(baseName, args, 0)
					return MangleGenericTypeName(baseName, args)
				}
			}
		}
	}
	return baseName
}

// getEnumCName 从枚举基名获取实例化后的 C 类型名（用于静态访问如 Option.None）。
// 查找 genericTypeCache 中该枚举的首个实例化（如 Option → Option_int）。
func (eg *ExpressionGenerator) getEnumCName(enumBase string) string {
	if eg.codegen == nil {
		return enumBase
	}
	// 在缓存中查找该枚举的实例化
	for key := range eg.codegen.genericTypeCache {
		if strings.HasPrefix(key, enumBase+"<") {
			// 从缓存键提取实例化名（去掉基名和<>）
			// key 格式: "Option<int>" → 实例化名 "Option_int"
			if idx := strings.Index(key, "<"); idx > 0 {
				argsStr := key[idx+1 : len(key)-1]
				var args []string
				for _, part := range splitTopLevelCommas(argsStr) {
					if strings.TrimSpace(part) != "" {
						args = append(args, strings.TrimSpace(part))
					}
				}
				if len(args) > 0 {
					return MangleGenericTypeName(enumBase, args)
				}
			}
		}
	}
	return enumBase
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

// matchBindingCType 计算 match 绑定变量的 C 类型。
// 变体字段类型中的泛型参数（如 Option<T> 的 T）按目标变量的实际类型参数替换
// （如 Option<int> → int → int64_t）。
func (eg *ExpressionGenerator) matchBindingCType(variant *ast.EnumVariant, fieldIndex int, enumName string, target ast.Expression) string {
	if variant == nil || fieldIndex >= len(variant.FieldTypes) {
		return "auto"
	}
	fieldType := variant.FieldTypes[fieldIndex]
	if eg.codegen.program == nil || eg.codegen.typeGenerator == nil {
		return fieldType
	}

	// 解析目标变量的实际类型参数（如 Option<int>）
	var typeArgs []string
	if ident, ok := target.(*ast.Identifier); ok {
		if sym := eg.codegen.GetSymbol(ident.Name); sym != nil {
			if lt := strings.Index(sym.Type, "<"); lt > 0 {
				argsStr := sym.Type[lt+1 : len(sym.Type)-1]
				for _, part := range splitTopLevelCommas(argsStr) {
					if strings.TrimSpace(part) != "" {
						typeArgs = append(typeArgs, strings.TrimSpace(part))
					}
				}
			}
		}
	}

	// 若字段类型中残留泛型参数（如 T），按枚举声明的参数顺序替换为实际类型参数
	if len(typeArgs) > 0 && enumName != "" {
		if enumStmt := eg.codegen.program.FindEnum(enumName); enumStmt != nil {
			for i, tp := range enumStmt.TypeParams {
				if i < len(typeArgs) && strings.Contains(fieldType, tp.Name) {
					fieldType = strings.ReplaceAll(fieldType, tp.Name, typeArgs[i])
				}
			}
		}
	}

	// 映射到 C 类型（int → int64_t、string → String 等）
	return eg.codegen.typeGenerator.MapKaulaTypeToC(fieldType)
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
		if sl.TypeName != "" {
			return fmt.Sprintf("(K_%s){0}", sl.TypeName)
		}
		return "{0}"
	}
	var parts []string
	for _, f := range sl.Fields {
		val := eg.GenerateExpression(f.Value)
		parts = append(parts, fmt.Sprintf(".%s=%s", f.Name, val))
	}
	inner := "{" + strings.Join(parts, ",") + "}"
	if sl.TypeName != "" {
		return fmt.Sprintf("(K_%s)%s", sl.TypeName, inner)
	}
	return inner
}
