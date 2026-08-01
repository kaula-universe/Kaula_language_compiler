package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/core"
	"regexp"
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
	if stmt == nil {
		return ""
	}
	// 首先尝试使用插件生成代码
	if code, ok := sg.codegen.pluginManager.GenerateStatement(stmt, sg.codegen); ok {
		return code
	}

	switch s := stmt.(type) {
	case *ast.SpendStatement:
		return sg.generateSpendStatement(s)
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
	case *ast.EnumStatement:
		return sg.codegen.typeGenerator.GenerateEnumStatement(s)
	case *ast.TypeAliasStatement:
		return sg.codegen.typeGenerator.GenerateTypeAliasStatement(s)
	case *ast.IfStatement:
		return sg.generateIfStatement(s)
	case *ast.WhileStatement:
		return sg.generateWhileStatement(s)
	case *ast.ForInStatement:
		return sg.generateForInStatement(s)
	case *ast.ReturnStatement:
		return sg.generateReturnStatement(s)
	case *ast.BreakStatement:
		return sg.generateBreakStatement(s)
	case *ast.ContinueStatement:
		return sg.generateContinueStatement(s)
	case *ast.ImportStatement:
		return sg.generateImportStatement(s)
	case *ast.ExportStatement:
		return sg.generateExportStatement(s)
	case *ast.NonLocalStatement:
		return sg.generateNonLocalStatement(s)
	case *ast.VariableDeclaration:
		if s == nil {
			return ""
		}
		return sg.generateVariableDeclaration(s)
	case *ast.ExternStatement:
		if s == nil {
			return ""
		}
		return sg.generateExternStatement(s)
	case *ast.ExpressionStatement:
		if s == nil || s.Expression == nil {
			return ""
		}
		// 检查是否是 PrefixCallExpression
		if prefixCall, ok := s.Expression.(*ast.PrefixCallExpression); ok {
			return sg.generatePrefixCallBody(prefixCall)
		}
		// 安全地进行类型断言
		callExpr, isCall := interface{}(s.Expression).(*ast.CallExpression)
		if isCall && callExpr != nil && callExpr.Function != nil {
			if _, isMemberAccess := callExpr.Function.(*ast.MemberAccessExpression); isMemberAccess {
				// 这是模块函数调用，直接生成函数调用代码
				return sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
			}
		}
		// 其他表达式语句
		return sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
	case *ast.BlockStatement:
		return sg.generateBlockStatement(s)
	case *ast.YieldStatement:
		return sg.generateYieldStatement(s)
	case *ast.ReleaseStatement:
		return sg.generateReleaseStatement(s)
	case *ast.ExtractStatement:
		return sg.generateExtractStatement(s)
	default:
		return ""
	}
}

// generateVariableDeclaration 生成变量声明代码
func (sg *StatementGenerator) generateVariableDeclaration(stmt *ast.VariableDeclaration) string {
	if stmt.IsAuto {
		return sg.generateAutoDeclaration(stmt)
	}

	// 记录数组字面量长度，供 spend 强制消费流使用
	if arrLit, ok := stmt.Value.(*ast.ArrayLiteral); ok {
		sg.codegen.arrayLens[stmt.Name] = len(arrLit.Elements)
	}

	// const 变量：纯编译期常量，不参与运行时内存分配
	if stmt.IsConst {
		if evaluated := sg.codegen.tryEvalConstExpr(stmt.Value); evaluated != "" {
			sg.codegen.constTable[stmt.Name] = evaluated
			return ""
		}
		if stmt.Type == "" {
			// 无显式类型的 const name = expr：无法求值则表达式注册到常量表
			sg.codegen.constTable[stmt.Name] = sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
			return ""
		}
		// 有显式类型（C 风格 const type name = 运行时值，如 const char* s = fn()）：
		// 无法编译期求值，按普通变量生成并注册符号
	}

	sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local", stmt.Pos.Line, stmt.Pos.Column)

	cType := sg.codegen.typeGenerator.convertType(stmt.Type, stmt.Nullable)

	var builder strings.Builder
	builder.Grow(128)

	// SOR 智能分配路由
	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		if decision := adapter.GetVarDecision(stmt.Name); decision != nil {
			var initValue string
			if stmt.Value != nil {
				initValue = sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
			}
			return adapter.GenerateSmartVarAlloc(cType, stmt.Name, "", initValue)
		}
	}

	// 生成属性前缀（#[volatile], #[section("...")], #[aligned(N)] 等）
	attrPrefix := generateVarAttributes(stmt.Attributes)

	builder.WriteString(attrPrefix)

	// static 存储修饰符
	if stmt.IsStatic {
		builder.WriteString("static ")
	}

	// const 常量修饰符
	if stmt.IsConst {
		builder.WriteString("const ")
		// 局部 const 也加入常量表，支持编译期常量求值
		if evaluated := sg.codegen.tryEvalConstExpr(stmt.Value); evaluated != "" {
			sg.codegen.constTable[stmt.Name] = evaluated
		}
	}

	builder.WriteString(cType)
	builder.WriteByte(' ')
	builder.WriteString(stmt.Name)

	if stmt.Value != nil {
		builder.WriteString(" = ")
		builder.WriteString(sg.codegen.expressionGenerator.GenerateExpression(stmt.Value))
	} else if stmt.Nullable {
		builder.WriteString(" = NULL")
	}
	builder.WriteString(";\n")
	return builder.String()
}

// generateAutoDeclaration 生成 auto 声明代码（类型推导）
func (sg *StatementGenerator) generateAutoDeclaration(stmt *ast.VariableDeclaration) string {
	if stmt.Type == "" {
		sg.codegen.error(fmt.Sprintf("编译错误：auto 变量 '%s' 类型推导失败，无法生成代码。请显式声明类型。", stmt.Name))
		return fmt.Sprintf("// ERROR: auto '%s' type not inferred - compilation should have stopped\n", stmt.Name)
	}

	sg.codegen.AddSymbol(stmt.Name, stmt.Type, false, "local", stmt.Pos.Line, stmt.Pos.Column)

	// 记录数组字面量长度，供 spend 强制消费流使用
	if arrLit, ok := stmt.Value.(*ast.ArrayLiteral); ok {
		sg.codegen.arrayLens[stmt.Name] = len(arrLit.Elements)
	}

	cType := sg.codegen.typeGenerator.convertType(stmt.Type, false)

	var builder strings.Builder
	builder.Grow(64)

	builder.WriteString(cType)
	builder.WriteByte(' ')
	builder.WriteString(stmt.Name)

	if stmt.Value != nil {
		builder.WriteString(" = ")
		builder.WriteString(sg.codegen.expressionGenerator.GenerateExpression(stmt.Value))
	}
	builder.WriteString(";\n")
	return builder.String()
}

// generateExternStatement 生成 extern 外部符号/函数声明
// 变量: extern name: type → extern type name;
// 函数: extern fn name(params) -> ret → extern ret name(param_types);
func (sg *StatementGenerator) generateExternStatement(stmt *ast.ExternStatement) string {
	sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "extern", stmt.Pos.Line, stmt.Pos.Column)

	var builder strings.Builder
	builder.Grow(256)

	if stmt.IsFunction {
		// extern 函数声明
		returnType := sg.codegen.typeGenerator.convertType(stmt.ReturnType, false)
		if returnType == "" {
			returnType = "void"
		}
		builder.WriteString("extern ")
		builder.WriteString(returnType)
		builder.WriteByte(' ')
		builder.WriteString(stmt.Name)
		builder.WriteByte('(')
		if len(stmt.ParamTypes) == 0 {
			builder.WriteString("void")
		} else {
			for i, pType := range stmt.ParamTypes {
				if i > 0 {
					builder.WriteString(", ")
				}
				cType := sg.codegen.typeGenerator.convertType(pType, false)
				if cType == "" {
					cType = "void*"
				}
				builder.WriteString(cType)
			}
		}
		builder.WriteString(");\n")
	} else {
		// extern 变量声明
		cType := sg.codegen.typeGenerator.convertType(stmt.Type, stmt.Nullable)
		builder.WriteString("extern ")
		builder.WriteString(cType)
		builder.WriteByte(' ')
		builder.WriteString(stmt.Name)
		builder.WriteString(";\n")
	}
	return builder.String()
}

// generateExpressionInMain 在 main 函数体中生成表达式语句
func (sg *StatementGenerator) generateExpressionInMain(expr ast.Expression) string {
	if callExpr, ok := expr.(*ast.CallExpression); ok {
		if ident, ok := callExpr.Function.(*ast.Identifier); ok && ident.Name == "println" {
			if len(callExpr.Args) > 1 {
				return sg.codegen.expressionGenerator.generatePrintlnMulti(callExpr.Args)
			}
		}
	}
	return sg.codegen.expressionGenerator.GenerateExpression(expr) + ";\n"
}

// generateSpendStatement 生成 spend 强制消费流代码
// 数组模式：将目标包装为 Spendable → spend_lock → 逐子句 spend_call（标记消费并校验）
//          → call(default) 时 spend_consume_all → spend_unlock（校验全消费）→ destroy
// 枚举模式：生成 switch 穷尽分发，default 分支为运行时安全网（编译期已证明不可达）
func (sg *StatementGenerator) generateSpendStatement(stmt *ast.SpendStatement) string {
	count, enumName, targetCode := sg.resolveSpendTarget(stmt.Target)
	if enumName != "" {
		return sg.generateSpendEnum(stmt, enumName, targetCode)
	}
	return sg.generateSpendArray(stmt, count, targetCode)
}

// resolveSpendTarget 解析 spend 目标
// 返回: (count, enumName, targetC)；enumName 非空表示枚举模式；count<=0 表示未知长度（带 default 兜底）
func (sg *StatementGenerator) resolveSpendTarget(target ast.Expression) (int, string, string) {
	switch t := target.(type) {
	case *ast.ArrayLiteral:
		return len(t.Elements), "", ""
	case *ast.Identifier:
		if sym := sg.codegen.GetSymbol(t.Name); sym != nil {
			if sg.codegen.IsEnumType(sym.Type) {
				return 0, sym.Type, t.Name
			}
		}
		if n, ok := sg.codegen.arrayLens[t.Name]; ok {
			return n, "", t.Name
		}
		return 0, "", ""
	default:
		return 0, "", ""
	}
}

// generateSpendArray 数组消费模式
func (sg *StatementGenerator) generateSpendArray(stmt *ast.SpendStatement, count int, targetCode string) string {
	sg.codegen.spendCounter++
	spdName := fmt.Sprintf("_spd_%d", sg.codegen.spendCounter)

	var builder strings.Builder
	builder.Grow(512)
	builder.WriteString("/* spend block: 强制消费流 */\n")

	// 数组字面量目标：提升为具名临时数组，避免子表达式重复求值
	arrTemp := ""
	if arrLit, ok := stmt.Target.(*ast.ArrayLiteral); ok {
		arrTemp = spdName + "_arr"
		elems := make([]string, len(arrLit.Elements))
		for i, elem := range arrLit.Elements {
			elems[i] = sg.codegen.expressionGenerator.GenerateExpression(elem)
		}
		builder.WriteString(fmt.Sprintf("int64_t* %s = ((int64_t[]){ %s });\n", arrTemp, strings.Join(elems, ", ")))
		targetCode = arrTemp
	}

	if targetCode == "" {
		// 未知长度：sema 已强制要求 call(default)；这里生成纯语义流（无元素可枚举）
		builder.WriteString("/* 未知长度目标：仅执行 default 兜底 */\n")
		for _, call := range stmt.Calls {
			if call.IsDefault {
				builder.WriteString("{\n")
				for _, bodyStmt := range call.Body {
					builder.WriteString(sg.codegen.generateStatement(bodyStmt))
				}
				builder.WriteString("}\n")
			}
		}
		builder.WriteString("/* end spend */\n")
		return builder.String()
	}

	// 包装元素指针到 Spendable
	builder.WriteString(fmt.Sprintf("Spendable* %s = spendable_create(%d);\n", spdName, count))
	for i := 0; i < count; i++ {
		builder.WriteString(fmt.Sprintf("spendable_add(%s, &(%s)[%d]);\n", spdName, targetCode, i))
	}
	builder.WriteString(fmt.Sprintf("spend_lock(%s);\n", spdName))

	// 逐子句消费
	for _, call := range stmt.Calls {
		if call.IsDefault {
			builder.WriteString(fmt.Sprintf("/* call(default): 消费全部剩余元素 */\n"))
			builder.WriteString(fmt.Sprintf("spend_consume_all(%s);\n", spdName))
		} else {
			index := 0
			if intLit, ok := call.Index.(*ast.IntegerLiteral); ok {
				index = int(intLit.Value)
			}
			builder.WriteString(fmt.Sprintf("/* call(%d) */\n", index))
			builder.WriteString(fmt.Sprintf("spend_call(%s, %d);\n", spdName, index))
		}
		builder.WriteString("{\n")
		for _, bodyStmt := range call.Body {
			builder.WriteString(sg.codegen.generateStatement(bodyStmt))
		}
		builder.WriteString("}\n")
	}

	builder.WriteString(fmt.Sprintf("spend_unlock(%s);\n", spdName))
	builder.WriteString(fmt.Sprintf("spendable_destroy(%s);\n", spdName))
	builder.WriteString("/* end spend */\n")
	return builder.String()
}

// generateSpendEnum 枚举穷尽消费模式：switch 分发 + 安全网
func (sg *StatementGenerator) generateSpendEnum(stmt *ast.SpendStatement, enumName, targetCode string) string {
	enumStmt := sg.codegen.program.FindEnum(enumName)
	if enumStmt == nil {
		return "/* spend: enum " + enumName + " not found */\n"
	}

	var builder strings.Builder
	builder.Grow(512)
	builder.WriteString("/* spend block: 枚举穷尽消费 */\n")
	builder.WriteString(fmt.Sprintf("switch ((int)(%s)) {\n", targetCode))

	variantLabel := map[string]string{}
	for _, v := range enumStmt.Variants {
		variantLabel[v.Name] = enumName + "_Kind_" + v.Name
	}

	for _, call := range stmt.Calls {
		if call.IsDefault || call.Index == nil {
			continue
		}
		var label string
		if id, ok := call.Index.(*ast.Identifier); ok {
			label = variantLabel[id.Name]
		}
		if label == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("case %s:\n", label))
		builder.WriteString("{\n")
		for _, bodyStmt := range call.Body {
			builder.WriteString(sg.codegen.generateStatement(bodyStmt))
		}
		builder.WriteString("}\n")
		builder.WriteString("break;\n")
	}

	// 编译期已穷尽证明；default 为运行时安全网
	builder.WriteString("default:\n")
	builder.WriteString("    /* unreachable: exhaustiveness proven at compile time */\n")
	builder.WriteString("    abort();\n")
	builder.WriteString("}\n")
	builder.WriteString("/* end spend */\n")
	return builder.String()
}

// generatePrefixStatement 生成 prefix 语句代码
// prefix 定义只注册前缀，不内联展开代码（由 @ 调用展开）
func (sg *StatementGenerator) generatePrefixStatement(stmt *ast.PrefixStatement) string {
	// 在 PrefixManager 中创建前缀上下文
	sg.codegen.prefixManager.CreatePrefix(stmt.Name, core.PrefixAnnotationPrefix)

	// 转义名称，防止 C 字符串注入
	safeName := escapeCString(stmt.Name)

	// 只生成注释标记，不展开代码（代码由 @ 调用时展开）
	code := fmt.Sprintf("/* prefix: %s (definition) */\n", safeName)
	return code
}

// generatePrefixCallBody 生成前缀调用体的代码 - AST 直接插入
func (sg *StatementGenerator) generatePrefixCallBody(e *ast.PrefixCallExpression) string {
	code := ""

	// 查找前缀函数的声明
	funcDecl := sg.findFunctionDeclaration(e.Name)
	if funcDecl == nil {
		// 尝试查找 prefix 语句（非函数前缀）
		prefixStmt := sg.codegen.findPrefixStatement(e.Name)
		if prefixStmt != nil {
			// 用块作用域包裹，避免多次调用时变量重定义
			code += "{\n"
			sg.codegen.indent++
			for _, bodyStmt := range prefixStmt.Body {
				code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
			}
			for _, bodyStmt := range e.Body {
				code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
			}
			sg.codegen.indent--
			code += sg.codegen.indentString() + "}\n"
			return code
		}
		// 如果找不到前缀定义，跳过
		return ""
	}

	// 检查是否是前缀函数
	annotation := funcDecl.GetAnnotation()
	if annotation != ast.TreeAnnotationPrefix &&
		annotation != ast.TreeAnnotationPrefixTree &&
		annotation != ast.TreeAnnotationTree {
		// 不是前缀函数，只处理调用体内的语句
		sg.codegen.indent++
		for _, bodyStmt := range e.Body {
			code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
		}
		sg.codegen.indent--
		return code
	}

	// 参数替换：将前缀函数体中的参数引用替换为实际值
	// 使用正则表达式进行精确匹配，避免意外替换子字符串
	paramRegex := make(map[string]*regexp.Regexp)
	for paramName := range e.Params {
		// 匹配 $paramName 后面不是字母数字或下划线的情况
		paramRegex[paramName] = regexp.MustCompile(`\$` + regexp.QuoteMeta(paramName) + `([^a-zA-Z0-9_]|$)`)
	}

	paramMap := make(map[string]string)
	for paramName, paramValue := range e.Params {
		valueCode := sg.codegen.expressionGenerator.GenerateExpression(paramValue)
		paramMap[paramName] = valueCode
	}

	// 辅助函数：执行参数替换
	replaceParams := func(code string) string {
		for paramName, paramValue := range paramMap {
			regex := paramRegex[paramName]
			// 使用 ReplaceAllStringFunc 保留匹配的分隔符
			code = regex.ReplaceAllString(code, paramValue+"$1")
		}
		return code
	}

	// 直接展开前缀函数的函数体
	sg.codegen.indent++
	for _, bodyStmt := range funcDecl.Body {
		generated := sg.codegen.generateStatement(bodyStmt)
		// 参数替换：$device -> 实际值
		generated = replaceParams(generated)
		code += sg.codegen.indentString() + generated
	}
	sg.codegen.indent--

	// 追加调用体内的语句
	for _, bodyStmt := range e.Body {
		generated := sg.codegen.generateStatement(bodyStmt)
		// 参数替换
		generated = replaceParams(generated)
		code += sg.codegen.indentString() + generated
	}

	return code
}

// findFunctionDeclaration 查找前缀函数的声明
func (sg *StatementGenerator) findFunctionDeclaration(name string) *ast.FunctionStatement {
	// 从当前程序中查找前缀函数定义
	// 这里需要访问 AST，可以通过 codegen 的 program 字段
	return sg.codegen.findFunctionByName(name)
}

// generateTreeStatement 生成 tree 语句代码
func (sg *StatementGenerator) generateTreeStatement(stmt *ast.TreeStatement) string {
	code := ""

	annotation := stmt.GetAnnotation()

	if annotation == ast.TreeAnnotationRoot || annotation == ast.TreeAnnotationRootTree {
		code += "// Root tree definition\n"
		code += sg.generateRootTreeDefinition(stmt)
	} else if annotation == ast.TreeAnnotationPrefix || annotation == ast.TreeAnnotationPrefixTree {
		code += "// Prefix tree definition\n"
		code += sg.generatePrefixTreeDefinition(stmt)
	} else {
		code += "// Tree structure implementation\n"
		code += sg.generateTreeImplementation(stmt)
	}

	return code
}

func (sg *StatementGenerator) generateRootTreeDefinition(stmt *ast.TreeStatement) string {
	code := "/* tree: root */\n"

	if stmt.Root != nil {
		code += "/* root: "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Root)
		code += " */\n"
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: root */\n"
	return code
}

func (sg *StatementGenerator) generatePrefixTreeDefinition(stmt *ast.TreeStatement) string {
	code := "/* tree: prefix */\n"

	if stmt.Root != nil {
		if ident, ok := stmt.Root.(*ast.Identifier); ok {
			code += fmt.Sprintf("/* prefix: %s */\n", ident.Name)
		}
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: prefix */\n"
	return code
}

func (sg *StatementGenerator) generateValidatedTree(stmt *ast.TreeStatement, rootTree *core.Tree) string {
	code := "/* tree: validated */\n"

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: validated */\n"
	return code
}

func (sg *StatementGenerator) generateOrphanTreeError(stmt *ast.TreeStatement) string {
	code := fmt.Sprintf("// ERROR: Tree at line %d is orphan - no root tree defined\n", stmt.Pos.Line)
	code += "// Consider wrapping this tree in a prefix or class, or marking it with #[root,tree]\n"
	code += "// Example: #[prefix,tree] fn wrap() { ... tree code ... }\n"
	return code
}

func (sg *StatementGenerator) generateTreeImplementation(stmt *ast.TreeStatement) string {
	code := "/* tree: implementation */\n"

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: implementation */\n"
	return code
}

// generateObjectStatement 生成 object 语句代码
func (sg *StatementGenerator) generateObjectStatement(stmt *ast.ObjectStatement) string {
	code := fmt.Sprintf("// Object: %s of type %s\n", stmt.Name, stmt.Type)
	sg.codegen.typeGenerator.structTypes[stmt.Name] = true
	code += fmt.Sprintf("typedef struct K_%s {\n", stmt.Name)
	for i := range stmt.Fields {
		code += fmt.Sprintf("    void* field%d;\n", i+1)
	}
	code += fmt.Sprintf("} K_%s;\n", stmt.Name)
	// 声明全局变量
	varName := stmt.Name + "_obj"
	code += fmt.Sprintf("K_%s* %s;\n", stmt.Name, varName)
	return code
}

// generateIfStatement 生成 if 语句代码
func (sg *StatementGenerator) generateIfStatement(stmt *ast.IfStatement) string {
	code := "if ("
	condCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Condition)
	code += condCode
	code += ") {\n"

	// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
	sg.codegen.indent++
	sg.codegen.EnterScope("if_body")

	useKMM := sg.shouldUseKMMScopeForBody(stmt.Body)
	// 相邻 scope 合并优化：如果 body 包含分配调用，使用 offset_save/restore 代替 scope_push/pop
	useOffset := !useKMM && bodyContainsAllocation(stmt.Body)

	if useKMM {
		sg.codegen.EnterKMMScope()
	}
	if useOffset {
		sg.codegen.EnterOffsetScope()
	}

	var bodyCode strings.Builder
	for _, bodyStmt := range stmt.Body {
		bodyCode.WriteString(sg.codegen.indentString())
		bodyCode.WriteString(sg.codegen.generateStatement(bodyStmt))
	}

	if useOffset {
		sg.codegen.ExitOffsetScope()
	}
	if useKMM {
		sg.codegen.ExitKMMScope()
	}
	sg.codegen.ExitScope()
	sg.codegen.indent--

	if useOffset {
		code += sg.codegen.indentString() + "size_t _if_scope_start = kmm_v4_offset_save();\n"
		code += bodyCode.String()
		code += sg.codegen.indentString() + "kmm_v4_offset_restore(_if_scope_start);\n"
	} else if useKMM {
		code += sg.codegen.indentString() + "KMM_V4_SCOPE_START {\n"
		code += bodyCode.String()
		code += sg.codegen.indentString() + "} KMM_V4_SCOPE_END;\n"
	} else {
		code += bodyCode.String()
	}

	code += sg.codegen.indentString() + "}"

	if len(stmt.Else) > 0 {
		code += " else {\n"

		sg.codegen.indent++
		sg.codegen.EnterScope("else_body")

		useKMMElse := sg.shouldUseKMMScopeForBody(stmt.Else)
		useOffsetElse := !useKMMElse && bodyContainsAllocation(stmt.Else)

		if useKMMElse {
			sg.codegen.EnterKMMScope()
		}
		if useOffsetElse {
			sg.codegen.EnterOffsetScope()
		}

		var elseCode strings.Builder
		for _, elseStmt := range stmt.Else {
			elseCode.WriteString(sg.codegen.indentString())
			elseCode.WriteString(sg.codegen.generateStatement(elseStmt))
		}

		if useOffsetElse {
			sg.codegen.ExitOffsetScope()
		}
		if useKMMElse {
			sg.codegen.ExitKMMScope()
		}
		sg.codegen.ExitScope()
		sg.codegen.indent--

		if useOffsetElse {
			code += sg.codegen.indentString() + "size_t _else_scope_start = kmm_v4_offset_save();\n"
			code += elseCode.String()
			code += sg.codegen.indentString() + "kmm_v4_offset_restore(_else_scope_start);\n"
		} else if useKMMElse {
			code += sg.codegen.indentString() + "KMM_V4_SCOPE_START {\n"
			code += elseCode.String()
			code += sg.codegen.indentString() + "} KMM_V4_SCOPE_END;\n"
		} else {
			code += elseCode.String()
		}

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

	// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
	sg.codegen.indent++
	sg.codegen.EnterScope("while_body")

	useKMM := sg.shouldUseKMMScopeForBody(stmt.Body)
	// 相邻 scope 合并优化：如果循环体包含分配调用，使用 offset_save/restore 代替 scope_push/pop
	useOffset := !useKMM && bodyContainsAllocation(stmt.Body)

	if useKMM {
		sg.codegen.EnterKMMScope()
	}
	if useOffset {
		sg.codegen.EnterOffsetScope()
	}

	var bodyCode strings.Builder
	for _, bodyStmt := range stmt.Body {
		bodyCode.WriteString(sg.codegen.indentString())
		bodyCode.WriteString(sg.codegen.generateStatement(bodyStmt))
	}

	if useOffset {
		sg.codegen.ExitOffsetScope()
	}
	if useKMM {
		sg.codegen.ExitKMMScope()
	}
	sg.codegen.ExitScope()
	sg.codegen.indent--

	if useOffset {
		// offset_save/restore 路径：轻量级，无 scope 栈操作
		code += sg.codegen.indentString() + "size_t _loop_scope_start = kmm_v4_offset_save();\n"
		code += bodyCode.String()
		code += sg.codegen.indentString() + "kmm_v4_offset_restore(_loop_scope_start);\n"
	} else if useKMM {
		code += sg.codegen.indentString() + "KMM_V4_SCOPE_START {\n"
		code += bodyCode.String()
		code += sg.codegen.indentString() + "} KMM_V4_SCOPE_END;\n"
	} else {
		code += bodyCode.String()
	}

	code += sg.codegen.indentString() + "}\n"
	return code
}

// needsKMMScope 检查一段已生成的代码是否需要 KMM scope
// 优化策略：
// - 作用域合并：如果外层已有 KMM scope，内层不需要再插入
// - SOR 模式：使用精确的变量决策信息判断（从符号表获取当前 scope 的变量，排除函数参数）
// - 非 SOR 模式：基于符号表的类型分析 + 代码内容检测
// 纯 Stack 变量、std_malloc 的变量或函数参数的 scope 可以省略，消除运行时开销
func (sg *StatementGenerator) needsKMMScope(bodyCode string) bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if sg.codegen.IsInKMMScope() {
		return false
	}

	// SOR 模式：使用精确的变量决策
	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		currentScope := sg.codegen.GetCurrentScope()
		var varNames []string
		if currentScope != nil {
			for name, sym := range currentScope.GetAllSymbols() {
				if sym.Scope != "parameter" && sym.Scope != "param" {
					varNames = append(varNames, name)
				}
			}
		}
		return adapter.NeedsKMMScopeByVars(varNames)
	}

	// 非 SOR 模式：智能分析
	return sg.needsKMMScopeNonSOR(bodyCode)
}

// shouldUseKMMScope 预判断：在生成 body 之前基于符号表判断是否需要 KMM scope
// 用于作用域合并优化：如果预判断需要 KMM，先 EnterKMMScope 再生成 body，
// 这样内层作用域的 needsKMMScope 会检测到外层已有 KMM 而跳过插入
func (sg *StatementGenerator) shouldUseKMMScope() bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if sg.codegen.IsInKMMScope() {
		return false
	}

	// SOR 模式：使用精确的变量决策（不依赖 body 代码）
	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		currentScope := sg.codegen.GetCurrentScope()
		var varNames []string
		if currentScope != nil {
			for name, sym := range currentScope.GetAllSymbols() {
				if sym.Scope != "parameter" && sym.Scope != "param" {
					varNames = append(varNames, name)
				}
			}
		}
		return adapter.NeedsKMMScopeByVars(varNames)
	}

	// 非 SOR 模式：基于符号表类型分析（不依赖 body 代码）
	currentScope := sg.codegen.GetCurrentScope()
	if currentScope != nil {
		for _, sym := range currentScope.GetAllSymbols() {
			if sym.Scope == "parameter" || sym.Scope == "param" {
				continue
			}
			if needsKMMForType(sym.Type) {
				return true
			}
		}
	}
	return false
}

// allocFuncNames 需要检测的动态分配函数名
var allocFuncNames = map[string]bool{
	"kmm_v4_malloc":     true,
	"kmm_v4_alloc_auto": true,
	"std_malloc":        true,
	"malloc":            true,
}

// bodyContainsAllocation 递归检测语句列表中是否包含动态分配调用
// 用于在循环体等内存热点处插入 KMM scope，削平峰值
func bodyContainsAllocation(stmts []ast.Statement) bool {
	for _, stmt := range stmts {
		if stmtContainsAllocation(stmt) {
			return true
		}
	}
	return false
}

func stmtContainsAllocation(stmt ast.Statement) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		if s == nil {
			return false
		}
		return exprContainsAllocation(s.Value)
	case *ast.ExpressionStatement:
		if s == nil {
			return false
		}
		return exprContainsAllocation(s.Expression)
	case *ast.IfStatement:
		if s == nil {
			return false
		}
		if exprContainsAllocation(s.Condition) {
			return true
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs) {
				return true
			}
		}
		for _, bs := range s.Else {
			if stmtContainsAllocation(bs) {
				return true
			}
		}
	case *ast.WhileStatement:
		if s == nil {
			return false
		}
		if exprContainsAllocation(s.Condition) {
			return true
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs) {
				return true
			}
		}
	case *ast.ForStatement:
		if s == nil {
			return false
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs) {
				return true
			}
		}
	case *ast.ForInStatement:
		if s == nil {
			return false
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs) {
				return true
			}
		}
	case *ast.BlockStatement:
		if s == nil {
			return false
		}
		for _, bs := range s.Statements {
			if stmtContainsAllocation(bs) {
				return true
			}
		}
	case *ast.ReturnStatement:
		if s == nil {
			return false
		}
		return exprContainsAllocation(s.Value)
	}
	return false
}

func exprContainsAllocation(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.CallExpression:
		if isAllocCall(e.Function) {
			return true
		}
		for _, arg := range e.Args {
			if exprContainsAllocation(arg) {
				return true
			}
		}
	case *ast.BinaryExpression:
		if exprContainsAllocation(e.Left) {
			return true
		}
		if exprContainsAllocation(e.Right) {
			return true
		}
	case *ast.UnaryExpression:
		return exprContainsAllocation(e.Right)
	case *ast.MemberAccessExpression:
		return exprContainsAllocation(e.Object)
	case *ast.ParenExpression:
		return exprContainsAllocation(e.Inner)
	case *ast.TypeCastExpression:
		return exprContainsAllocation(e.Expression)
	case *ast.ConditionalExpression:
		if exprContainsAllocation(e.Condition) {
			return true
		}
		if exprContainsAllocation(e.TrueExpr) {
			return true
		}
		if exprContainsAllocation(e.FalseExpr) {
			return true
		}
	}
	return false
}

func isAllocCall(fn ast.Expression) bool {
	if fn == nil {
		return false
	}
	switch e := fn.(type) {
	case *ast.Identifier:
		return allocFuncNames[e.Name]
	case *ast.MemberAccessExpression:
		return allocFuncNames[e.Member]
	}
	return false
}

// shouldUseKMMScopeForBody 基于 AST body 语句列表预判断是否需要 KMM scope
// 解决符号表在 body 生成前未填充的问题：直接扫描 AST 变量声明
// 优先使用 SOR 作用域决策（如果已注册），否则回退到变量级别判断
// 额外检测动态分配调用（kmm_v4_malloc 等），在内存热点处插入 KMM scope 削平峰值
func (sg *StatementGenerator) shouldUseKMMScopeForBody(bodyStmts []ast.Statement) bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if sg.codegen.IsInKMMScope() {
		return false
	}

	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		// 优先使用作用域级别的 SOR 决策（如果当前作用域已注册）
		currentScope := sg.codegen.GetCurrentScope()
		if currentScope != nil {
			scopeName := currentScope.GetScopeName()
			if scopeName != "" {
				// 尝试作用域级别判断
				scopeID := adapter.GetScopeIDByName(scopeName)
				if scopeID > 0 {
					if sd := adapter.GetScopeDecision(scopeID); sd != nil {
						// 有作用域决策，直接使用
						// 修复 #15：arena 子系统已移除，只需检查 UsesBumpPool
						return sd.UsesBumpPool
					}
				}
			}
		}
		// 回退：变量级别判断
		var varNames []string
		for _, stmt := range bodyStmts {
			if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
				varNames = append(varNames, varDecl.Name)
			}
		}
		if adapter.NeedsKMMScopeByVars(varNames) {
			return true
		}
		// SOR 决策不需要 KMM 时，补充检查 SOR 未追踪的指针类型变量
		for _, stmt := range bodyStmts {
			if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
				if needsKMMForType(varDecl.Type) {
					return true
				}
			}
		}
		// 内存热点检测：body 中含动态分配调用时插入 KMM scope
		// 每次循环迭代结束时 scope_pop 回收 bump 指针，削平峰值
		if bodyContainsAllocation(bodyStmts) {
			return true
		}
		return false
	}

	// 非 SOR 模式：扫描 body 中的变量类型
	for _, stmt := range bodyStmts {
		if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
			if needsKMMForType(varDecl.Type) {
				return true
			}
		}
	}
	// 内存热点检测：body 中含动态分配调用时插入 KMM scope
	if bodyContainsAllocation(bodyStmts) {
		return true
	}
	return false
}

// needsKMMScopeNonSOR 非 SOR 模式下的智能 KMM 判断
// 基于符号表分析变量类型，判断是否需要 KMM scope
func (sg *StatementGenerator) needsKMMScopeNonSOR(bodyCode string) bool {
	// 快速检查：如果代码中没有内存分配相关操作，直接返回 false
	if !strings.Contains(bodyCode, "std_malloc") &&
		!strings.Contains(bodyCode, "kmm_v4") &&
		!strings.Contains(bodyCode, "string_concat") &&
		!strings.Contains(bodyCode, "string_dup") {
		return false
	}

	// 基于符号表分析
	currentScope := sg.codegen.GetCurrentScope()
	if currentScope != nil {
		for _, sym := range currentScope.GetAllSymbols() {
			// 排除函数参数
			if sym.Scope == "parameter" || sym.Scope == "param" {
				continue
			}
			// 检查变量类型是否需要 KMM
			if needsKMMForType(sym.Type) {
				return true
			}
		}
	}

	// 代码内容检测：如果有 kmm_v4_alloc_auto 调用，需要 KMM
	if strings.Contains(bodyCode, "kmm_v4_alloc_auto") {
		return true
	}

	return false
}

// needsKMMForType 判断类型是否需要 KMM 内存管理
// - 指针类型 (*T) 需要 KMM
// - 字符串类型 (string, str) 需要 KMM
// - 复合类型 ([]T, map[K]V) 需要 KMM
// - 值类型 (int, bool, float, double, etc.) 不需要 KMM
func needsKMMForType(typeName string) bool {
	if typeName == "" {
		return false
	}
	// void(T...)R 签名记法：运行时为裸 void*/函数指针，不由 KMM 托管
	if strings.HasPrefix(typeName, "void(") {
		return false
	}
	t := strings.ToLower(typeName)

	// 指针类型
	if strings.Contains(t, "*") {
		return true
	}

	// 字符串类型
	if t == "string" || t == "str" {
		return true
	}

	// 切片类型
	if strings.HasPrefix(t, "[]") {
		return true
	}

	// Map 类型
	if strings.HasPrefix(t, "map[") {
		return true
	}

	// 数组类型（大数组可能需要 KMM，但这里保守处理）
	if strings.HasPrefix(t, "[") {
		return true
	}

	// 值类型不需要 KMM
	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"float", "float32", "float64", "f32", "f64",
		"double", "bool", "char", "byte",
		"isize", "usize", "size", "void":
		return false
	}

	// 其他未知类型，保守处理为需要 KMM
	return true
}

// generateForInStatement 生成 range-based for 迭代代码
//
// 支持两种迭代形式：
//  1. range 迭代:  for x in range(N) | range(start, end[, step]) { body }
//     → for (T _i_ = start; _i_ < end; _i_ += step) { T x = _i_; body }
//  2. 数组/切片迭代: for x in arr { body }
//     → for (size_t _i_ = 0; _i_ < len; _i_++) { T x = arr[_i_]; body }
//
// 索引变量由编译器管理，用户代码不可见，消除越界可能
func (sg *StatementGenerator) generateForInStatement(stmt *ast.ForInStatement) string {
	// 检测 range(...) 调用
	if rng := asRangeCall(stmt.Iterable); rng != nil {
		return sg.generateForRangeStatement(stmt, rng)
	}

	iterCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Iterable)

	// 从符号表获取迭代对象的 Kaula 类型，决定迭代方式
	var iterName string
	if id, ok := stmt.Iterable.(*ast.Identifier); ok {
		iterName = id.Name
	}

	var elemCType string
	var isFixedArray bool
	var arraySize string

	if iterName != "" {
		if sym := sg.codegen.GetSymbol(iterName); sym != nil {
			kt := sym.Type
			elemKaula := inferElementType(kt)
			if elemKaula != "" {
				elemCType = sg.codegen.typeGenerator.convertType(elemKaula, false)
			}
			if len(kt) > 0 && kt[0] == '[' {
				isFixedArray = true
				closeBracket := strings.Index(kt, "]")
				if closeBracket > 0 {
					arraySize = kt[1:closeBracket]
				}
			}
		}
	}

	code := "for (size_t _i_ = 0; _i_ < "
	if isFixedArray && arraySize != "" {
		code += arraySize
	} else {
		code += "(" + iterCode + ").len"
	}
	code += "; _i_++) {\n"

	sg.codegen.indent++
	code += sg.codegen.indentString()
	if elemCType != "" {
		code += elemCType + " " + stmt.Variable.Name + " = " + iterCode
	} else {
		code += "auto " + stmt.Variable.Name + " = " + iterCode
	}
	if isFixedArray {
		code += "[_i_]"
	} else {
		code += ".ptr[_i_]"
	}
	code += ";\n"

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// rangeCallInfo 描述一个 range(...) 调用的参数（已求值为 C 表达式字符串）
type rangeCallInfo struct {
	startExpr string // 起始值（默认 "0"）
	endExpr   string // 结束值（不包含）
	stepExpr  string // 步长（默认 "1"）；可能为负
}

// asRangeCall 判断表达式是否为 range(...) 调用，是则返回参数信息，否则返回 nil
// 仅识别 CallExpression，且其 Function 为 Identifier "range"
func asRangeCall(expr ast.Expression) *rangeCallInfo {
	call, ok := expr.(*ast.CallExpression)
	if !ok || call == nil {
		return nil
	}
	id, ok := call.Function.(*ast.Identifier)
	if !ok || id.Name != "range" {
		return nil
	}
	// range 至少需要 1 个参数，最多 3 个
	if len(call.Args) < 1 || len(call.Args) > 3 {
		return nil
	}
	return &rangeCallInfo{}
}

// generateForRangeStatement 生成 for x in range(...) { body } 的 C 代码
func (sg *StatementGenerator) generateForRangeStatement(stmt *ast.ForInStatement, rng *rangeCallInfo) string {
	call, _ := stmt.Iterable.(*ast.CallExpression)

	// 求值参数
	argExprs := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		argExprs = append(argExprs, sg.codegen.expressionGenerator.GenerateExpression(arg))
	}

	startExpr := "0"
	endExpr := argExprs[0]
	stepExpr := "1"
	switch len(argExprs) {
	case 1:
		// range(N) → 0..N-1, step 1
	case 2:
		// range(start, end) → start..end-1, step 1
		startExpr = argExprs[0]
		endExpr = argExprs[1]
	case 3:
		// range(start, end, step)
		startExpr = argExprs[0]
		endExpr = argExprs[1]
		stepExpr = argExprs[2]
	}

	// 选择索引变量的 C 类型：i64 以匹配 Kaula int 语义
	idxType := "long long"

	// 步长符号决定循环条件：
	//   step >= 0 → _i_ < end
	//   step <  0 → _i_ > end
	// 编译期无法静态确定符号时，生成条件表达式
	var cond string
	if isNegativeLiteral(stepExpr) {
		cond = "_i_ > (" + endExpr + ")"
	} else if isPositiveLiteral(stepExpr) {
		cond = "_i_ < (" + endExpr + ")"
	} else {
		// 运行时判断步长符号
		cond = "((" + stepExpr + ") >= 0 ? _i_ < (" + endExpr + ") : _i_ > (" + endExpr + "))"
	}

	// 使用 do-while(0) 包裹，便于 body 中 break/continue 正常工作
	// 同时保持与 KMM scope 插入逻辑兼容
	code := "for (" + idxType + " _i_ = (" + startExpr + "); " + cond + "; _i_ += (" + stepExpr + ")) {\n"

	sg.codegen.indent++
	code += sg.codegen.indentString()
	code += idxType + " " + stmt.Variable.Name + " = _i_;\n"

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// isPositiveLiteral 判断字符串是否为正整数字面量
func isPositiveLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// 去除可选的 + 号
	if s[0] == '+' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	// "0" 视为非负但非正，这里返回 true（>= 0 分支）
	return true
}

// isNegativeLiteral 判断字符串是否以 '-' 开头的负数字面量
func isNegativeLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	for _, ch := range s[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// inferElementType 从数组/切片类型字符串中提取元素类型
func inferElementType(typeStr string) string {
	if len(typeStr) > 0 && typeStr[0] == '[' {
		closeBracket := strings.Index(typeStr, "]")
		if closeBracket > 0 {
			return typeStr[closeBracket+1:]
		}
	}
	return ""
}

// generateReturnStatement 生成 return 语句代码
// 修复 #22：在 return 前补 kmm_v4_scope_pop()，防止 do-while(0) 作用域泄漏
// KMM_V4_SCOPE_START/END 使用 do-while(0) 包裹，return 会跳过 KMM_V4_SCOPE_END
// 中的 scope_pop，导致作用域栈不弹出。此处按当前 kmmScopeDepth 补齐 pop。
func (sg *StatementGenerator) generateReturnStatement(stmt *ast.ReturnStatement) string {
	retType := sg.codegen.currentFunctionReturnType
	isVoid := (retType == "" || retType == "void" || retType == "Void")

	var b strings.Builder

	// 在 return 前补 scope_pop，防止作用域泄漏
	// kmmScopeDepth 跟踪当前活跃的 do-while(0) KMM 作用域数量
	// （作用域合并优化确保内层不会重复创建 scope，所以 depth 通常为 0 或 1）
	if sg.codegen.kmmScopeDepth > 0 {
		indent := sg.codegen.indentString()
		for i := 0; i < sg.codegen.kmmScopeDepth; i++ {
			b.WriteString(indent)
			b.WriteString("kmm_v4_scope_pop();\n")
		}
	}

	if isVoid {
		b.WriteString(sg.codegen.indentString())
		b.WriteString("return;\n")
		return b.String()
	}

	b.WriteString(sg.codegen.indentString())
	b.WriteString("return ")
	if stmt.Value != nil {
		b.WriteString(sg.codegen.expressionGenerator.GenerateExpression(stmt.Value))
	} else {
		b.WriteString("0")
	}
	b.WriteString(";\n")
	return b.String()
}

// generateBreakStatement 生成 break 语句代码
func (sg *StatementGenerator) generateBreakStatement(stmt *ast.BreakStatement) string {
	return sg.codegen.indentString() + "break;\n"
}

// generateContinueStatement 生成 continue 语句代码
func (sg *StatementGenerator) generateContinueStatement(stmt *ast.ContinueStatement) string {
	return sg.codegen.indentString() + "continue;\n"
}

// generateImportStatement 生成 import 语句代码
func (sg *StatementGenerator) generateImportStatement(stmt *ast.ImportStatement) string {
	// import 语句在 C 中不需要特殊处理
	return ""
}

// generateExportStatement 生成 export 语句代码
func (sg *StatementGenerator) generateExportStatement(stmt *ast.ExportStatement) string {
	code := ""

	// 根据导出类型生成不同的代码
	switch stmt.Type {
	case "function":
		code += fmt.Sprintf("// Export function: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	case "class":
		code += fmt.Sprintf("// Export class: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	case "object":
		code += fmt.Sprintf("// Export object: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s_obj;\n", stmt.Name)
	case "variable":
		code += fmt.Sprintf("// Export variable: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	default:
		code += fmt.Sprintf("// Export: %s (%s)\n", stmt.Name, stmt.Type)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	}

	return code
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
	sg.codegen.EnterScope("block")
	indent := sg.codegen.indentString()

	adapter := sg.codegen.GetSORAdapter()
	needsKMM := false
	if adapter != nil && adapter.IsActive {
		needsKMM = sg.shouldUseKMMScopeForBody(stmt.Statements)
	}

	if needsKMM {
		// SOR 需要 KMM 作用域：offset save/restore
		if sg.codegen.IsInOffsetScope() {
			code := "{\n"
			sg.codegen.indent++
			for _, bodyStmt := range stmt.Statements {
				code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
			}
			sg.codegen.indent--
			code += indent + "}\n"
			sg.codegen.ExitScope()
			return code
		}
		code := "{\n"
		sg.codegen.indent++
		sg.codegen.EnterOffsetScope()
		code += sg.codegen.indentString() + "size_t _scope_start = kmm_v4_offset_save();\n"
		for _, bodyStmt := range stmt.Statements {
			code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
		}
		code += sg.codegen.indentString() + "kmm_v4_offset_restore(_scope_start);\n"
		sg.codegen.ExitOffsetScope()
		sg.codegen.indent--
		code += indent + "}\n"
		sg.codegen.ExitScope()
		return code
	}

	// SOR 不活跃或不需要 KMM：退回到现有启发式
	// 修复 #20：删除批量 bump 优化路径（_batch_ptr 空转问题）
	// per-thread heap 已实现单次 CAS 批量获取的效果，无需额外优化
	_, _, mallocCount := sg.analyzeBlockMallocs(stmt.Statements)

	if mallocCount >= 1 {
		code := "{\n"
		sg.codegen.indent++
		sg.codegen.EnterOffsetScope()
		code += sg.codegen.indentString() + "size_t _scope_start = kmm_v4_offset_save();\n"
		for _, bodyStmt := range stmt.Statements {
			code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
		}
		code += sg.codegen.indentString() + "kmm_v4_offset_restore(_scope_start);\n"
		sg.codegen.ExitOffsetScope()
		sg.codegen.indent--
		code += indent + "}\n"
		sg.codegen.ExitScope()
		return code
	}

	if sg.codegen.IsInOffsetScope() {
		code := "{\n"
		sg.codegen.indent++
		for _, bodyStmt := range stmt.Statements {
			code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
		}
		sg.codegen.indent--
		code += indent + "}\n"
		sg.codegen.ExitScope()
		return code
	}

	code := "KMM_V4_SCOPE_START {\n"
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Statements {
		code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
	}

	code += sg.codegen.indentString() + "// Free allocated memory\n"
	for name, symbol := range sg.codegen.currentScope.GetAllSymbols() {
		if symbol.Nullable {
			if symbol.Type == "string" || symbol.Type == "str" {
				code += sg.codegen.indentString()
				code += "if (" + name + ".ptr != NULL) { free(" + name + ".ptr); }\n"
			}
		}
	}

	sg.codegen.indent--
	code += indent + "} KMM_V4_SCOPE_END;\n"
	sg.codegen.ExitScope()
	return code
}

// mallocAnalysis 分析结果结构
type mallocAnalysis struct {
	count        int   // malloc 调用总数
	totalSize    int   // 所有 malloc 大小之和（仅计算已知大小）
	allKnown     bool  // 是否所有 malloc 大小都已知
	sizes        []int // 每个 malloc 的大小（0 表示未知）
	isContiguous bool  // malloc 是否连续出现（中间无其他语句）
}

// analyzeBlockMallocs 分析块语句中的 malloc 调用，判断是否可以批量分配
// 返回更详细的分配信息以支持激进的单对象优化
func (sg *StatementGenerator) analyzeBlockMallocs(stmts []ast.Statement) (canBatch bool, totalSize int, count int) {
	analysis := sg.analyzeBlockMallocsDetailed(stmts)
	return analysis.allKnown && analysis.count >= 1, analysis.totalSize, analysis.count
}

// analyzeBlockMallocsDetailed 返回详细的 malloc 分析结果
func (sg *StatementGenerator) analyzeBlockMallocsDetailed(stmts []ast.Statement) mallocAnalysis {
	result := mallocAnalysis{
		count:        0,
		totalSize:    0,
		allKnown:     true,
		sizes:        make([]int, 0),
		isContiguous: true,
	}

	lastWasMalloc := false
	hasOtherStmts := false

	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}

		isMalloc := false
		sizeBytes := 0

		switch s := stmt.(type) {
		case *ast.VariableDeclaration:
			if s.Value != nil {
				// 检查是否是 malloc 类调用（包括 std.memory.kmm_v4_malloc）
				if call, ok := s.Value.(*ast.CallExpression); ok {
					if sg.isMallocCallExpr(call) {
						isMalloc = true
						sizeBytes = sg.extractMallocSizeBytes(call)
					}
				}
			}
		case *ast.ExpressionStatement:
			if s.Expression != nil {
				// 检查是否是 malloc 类调用（包括 std.memory.kmm_v4_malloc）
				if call, ok := s.Expression.(*ast.CallExpression); ok {
					if sg.isMallocCallExpr(call) {
						isMalloc = true
						sizeBytes = sg.extractMallocSizeBytes(call)
					}
				}
			}
		}

		if isMalloc {
			result.count++
			result.sizes = append(result.sizes, sizeBytes)
			if sizeBytes > 0 {
				result.totalSize += sizeBytes
			} else {
				result.allKnown = false
			}

			// 检查连续性：如果之前有非 malloc 语句，则不连续
			if hasOtherStmts && !lastWasMalloc {
				result.isContiguous = false
			}
			lastWasMalloc = true
			hasOtherStmts = false
		} else {
			hasOtherStmts = true
			lastWasMalloc = false
		}
	}

	return result
}

// isMallocCallExpr 检查表达式是否是 malloc 类调用
func (sg *StatementGenerator) isMallocCallExpr(call *ast.CallExpression) bool {
	if call == nil || call.Function == nil {
		return false
	}
	switch fn := call.Function.(type) {
	case *ast.Identifier:
		return fn.Name == "std_malloc" || fn.Name == "kmm_v4_malloc" || fn.Name == "kmm_v4_alloc_auto"
	case *ast.MemberAccessExpression:
		return fn.Member == "std_malloc" || fn.Member == "kmm_v4_malloc"
	}
	return false
}

// extractMallocSizeBytes 从 malloc 调用中提取编译期可确定的大小
func (sg *StatementGenerator) extractMallocSizeBytes(call *ast.CallExpression) int {
	if call == nil || len(call.Args) == 0 {
		return 0
	}
	arg := call.Args[0]

	// 整数字面量：直接返回
	if lit, ok := arg.(*ast.IntegerLiteral); ok {
		return int(lit.Value)
	}

	// sizeof 表达式：尝试从类型系统获取大小
	if sizeOf, ok := arg.(*ast.SizeOfExpression); ok {
		// 使用 TypeGenerator 获取类型大小
		if size, ok := sg.codegen.typeGenerator.GetTypeSize(sizeOf.TargetType); ok {
			return size
		}
		// 如果无法确定大小，返回 0（让优化走 offset_save/restore 路径）
		return 0
	}

	return 0
}

// generateYieldStatement 生成 yield 语句代码
// 语法: yield source -> target
// 生成: /* SOR: yield source -> target */ target = source; source = NULL;
// 优化: 当源表达式是字面量 0 时，yield 是纯死代码（target = 0, source = 0 → no-op），直接跳过
func (sg *StatementGenerator) generateYieldStatement(stmt *ast.YieldStatement) string {
	srcCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Source)

	// 死代码消除：源为字面量 0 的 yield 是无操作，跳过生成
	if srcCode == "0" {
		return ""
	}

	var b strings.Builder
	b.WriteString("/* SOR: yield ")
	b.WriteString(srcCode)
	b.WriteString(" -> ")
	b.WriteString(stmt.Target)
	b.WriteString(" */\n")
	b.WriteString(stmt.Target)
	b.WriteString(" = ")
	b.WriteString(srcCode)
	b.WriteString(";\n")
	b.WriteString(srcCode)
	b.WriteString(" = 0; /* SOR: ownership moved */\n")
	return b.String()
}

// generateVarAttributes 将变量属性转换为 C 声明前缀
// #[volatile] i32 x → volatile int64_t x
// #[section(".bss")] → __attribute__((section(".bss")))
// #[aligned(16)] → __attribute__((aligned(16)))
func generateVarAttributes(attrs []*ast.Attribute) string {
	if len(attrs) == 0 {
		return ""
	}
	var prefix strings.Builder
	var suffixParts []string
	for _, attr := range attrs {
		switch attr.Name {
		case "volatile":
			prefix.WriteString("volatile ")
		case "section":
			if len(attr.Args) > 0 {
				suffixParts = append(suffixParts, fmt.Sprintf("__attribute__((section(%s)))", attr.Args[0]))
			}
		case "aligned":
			if len(attr.Args) > 0 {
				suffixParts = append(suffixParts, fmt.Sprintf("__attribute__((aligned(%s)))", attr.Args[0]))
			} else {
				suffixParts = append(suffixParts, "__attribute__((aligned))")
			}
		case "weak":
			suffixParts = append(suffixParts, "__attribute__((weak))")
		case "deprecated":
			if len(attr.Args) > 0 {
				suffixParts = append(suffixParts, fmt.Sprintf("__attribute__((deprecated(%s)))", attr.Args[0]))
			} else {
				suffixParts = append(suffixParts, "__attribute__((deprecated))")
			}
		}
	}
	// __attribute__ 放在类型前面，volatile 也放在类型前面
	if len(suffixParts) > 0 {
		prefix.WriteString(strings.Join(suffixParts, " "))
		prefix.WriteByte(' ')
	}
	return prefix.String()
}

// generateReleaseStatement 生成 release 语句代码
// 语法: release source -> [holder1, holder2, ...]
// 语义: 将 source 的只读访问权分发给各 holder
// 策略:
//   - holder 已预先声明: 直接赋值 (零开销, 只读语义由 SOR 分析器编译期保证)
//   - holder 未声明: 创建 const void* 指针别名 (利用 C const 系统保证只读)
func (sg *StatementGenerator) generateReleaseStatement(stmt *ast.ReleaseStatement) string {
	srcCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Source)
	var b strings.Builder
	b.WriteString("/* SOR: release ")
	b.WriteString(srcCode)
	b.WriteString(" -> [")
	b.WriteString(strings.Join(stmt.Holders, ", "))
	b.WriteString("] */\n")
	for _, holder := range stmt.Holders {
		if sg.codegen.HasSymbol(holder) {
			b.WriteString(holder)
			b.WriteString(" = ")
			b.WriteString(srcCode)
			b.WriteString("; /* SOR: release read-only */\n")
		} else {
			b.WriteString("const void* ")
			b.WriteString(holder)
			b.WriteString(" = &(/* release-read-only */ ")
			b.WriteString(srcCode)
			b.WriteString("); /* SOR: read-only alias */\n")
		}
	}
	return b.String()
}

// generateExtractStatement 生成 extract 语句代码
// 语法: extract source[index] -> target
// 生成: target = source[index]; source[index] = NULL; (真正留下 hollow 状态)
func (sg *StatementGenerator) generateExtractStatement(stmt *ast.ExtractStatement) string {
	// parsePrimaryExpressionIterative 会将 data[2] 解析为 IndexExpression
	// 所以 Source 实际是 IndexExpression{Object: data, Index: 2}
	var baseCode, idxCode string
	if idxExpr, ok := stmt.Source.(*ast.IndexExpression); ok {
		baseCode = sg.codegen.expressionGenerator.GenerateExpression(idxExpr.Object)
		idxCode = sg.codegen.expressionGenerator.GenerateExpression(idxExpr.Index)
	} else {
		baseCode = sg.codegen.expressionGenerator.GenerateExpression(stmt.Source)
		if stmt.Index != nil {
			idxCode = sg.codegen.expressionGenerator.GenerateExpression(stmt.Index)
		} else {
			idxCode = "0"
		}
	}
	var b strings.Builder
	b.WriteString("/* SOR: extract ")
	b.WriteString(baseCode)
	b.WriteString("[")
	b.WriteString(idxCode)
	b.WriteString("] -> ")
	b.WriteString(stmt.Target)
	b.WriteString(" */\n")
	b.WriteString(stmt.Target)
	b.WriteString(" = ")
	b.WriteString(baseCode)
	b.WriteString("[")
	b.WriteString(idxCode)
	b.WriteString("]; /* SOR: extracted value */\n")
	b.WriteString(baseCode)
	b.WriteString("[")
	b.WriteString(idxCode)
	b.WriteString("] = 0; /* SOR: hollow — source slot now null */\n")
	return b.String()
}
