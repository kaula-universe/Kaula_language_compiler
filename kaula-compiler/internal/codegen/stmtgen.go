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
	case *ast.GenericInstance:
		return sg.generateGenericInstantiation(s)
	case *ast.VOStatement:
		return sg.generateVOStatement(s)
	case *ast.SpendStatement:
		return sg.generateSpendStatement(s)
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
	case *ast.EnumStatement:
		return sg.codegen.typeGenerator.GenerateEnumStatement(s)
	case *ast.TypeAliasStatement:
		return sg.codegen.typeGenerator.GenerateTypeAliasStatement(s)
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
	
	// const 无类型变量：编译期常量，不需要生成 C 变量
	if stmt.IsConst && stmt.Type == "" {
		return ""
	}
	
	sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local", stmt.Pos.Line, stmt.Pos.Column)
	
	cType := sg.codegen.typeGenerator.convertType(stmt.Type, stmt.Nullable)
	
	var builder strings.Builder
	builder.Grow(128)

	// SOR Arena 路由落地：如果 SOR 分析启用且变量是指针类型且决策为 arena/bump pool，
	// 用 kmm_v4_alloc_auto 分配，由 KMM 作用域自动回收
	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		if decision := adapter.GetVarDecision(stmt.Name); decision != nil {
			isPointer := strings.HasSuffix(cType, "*")
			isArenaOrPool := decision.AllocKind == "ArenaTiny" ||
				decision.AllocKind == "ArenaSmall" ||
				decision.AllocKind == "ArenaMedium" ||
				decision.AllocKind == "BumpPool"
			// 指针类型变量走 KMM 分配器
			if isPointer && isArenaOrPool && stmt.Value == nil {
				// 去掉末尾的 * 得到基类型用于 sizeof
				baseType := strings.TrimRight(cType, "*")
				builder.WriteString(cType)
				builder.WriteByte(' ')
				builder.WriteString(stmt.Name)
				builder.WriteString(" = (")
				builder.WriteString(cType)
				builder.WriteString(")kmm_v4_alloc_auto(sizeof(")
				builder.WriteString(baseType)
				builder.WriteString(")); /* sor: ")
				builder.WriteString(decision.AllocKind)
				builder.WriteString(" */\n")
				return builder.String()
			}
			// 值类型：保持栈分配，在注释中标注 SOR 决策
			if !isPointer {
				builder.WriteString(cType)
				builder.WriteByte(' ')
				builder.WriteString(stmt.Name)
				if stmt.Value != nil {
					builder.WriteString(" = ")
					builder.WriteString(sg.codegen.expressionGenerator.GenerateExpression(stmt.Value))
				}
				builder.WriteString("; /* sor: ")
				builder.WriteString(decision.AllocKind)
				builder.WriteString(" */\n")
				return builder.String()
			}
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

// generateGenericInstantiation 生成泛型实例化代码（编译期特化，零成本）
func (sg *StatementGenerator) generateGenericInstantiation(inst *ast.GenericInstance) string {
	var code string
	
	// 1. 查找原始泛型函数定义
	origFunc := sg.codegen.findFunctionByName(inst.OriginalName)
	if origFunc == nil {
		return fmt.Sprintf("// Error: Generic function '%s' not found for instantiation\n", inst.OriginalName)
	}
	
	// 2. 将 TypeArguments 转换为字符串数组
	typeArgStrings := make([]string, len(inst.TypeArguments))
	for i, ta := range inst.TypeArguments {
		typeArgStrings[i] = ta.Type
	}
	
	// 3. 生成特化后的函数名（添加 kaula_ 前缀避免与 C 宏冲突）
	specializedName := "kaula_" + inst.OriginalName + "_" + strings.Join(typeArgStrings, "_")
	
	// 4. 检查是否已实例化过（避免重复生成）
	if sg.codegen.IsGenericInstantiated(specializedName) {
		return ""
	}
	sg.codegen.MarkGenericInstantiated(specializedName)
	
	// 5. 构建类型映射
	typeMap := make(map[string]string)
	for i, tp := range origFunc.TypeParams {
		if i < len(typeArgStrings) {
			typeMap[tp.Name] = typeArgStrings[i]
		}
	}
	
	// 6. 实例化返回类型
	specializedReturnType := sg.resolveSpecializedType(origFunc.ReturnType, typeArgStrings)
	
	// 7. 生成特化函数签名
	code += fmt.Sprintf("// 泛型特化实例: %s<%s>\n", inst.OriginalName, strings.Join(typeArgStrings, ", "))
	code += fmt.Sprintf("static inline %s %s(", specializedReturnType, specializedName)
	
	// 8. 生成特化后的参数列表
	for i, param := range origFunc.Params {
		if i > 0 { code += ", " }
		// 参数名保持不变，但类型需要特化
		// 注意：这里的 param 是参数名，类型信息需要从原函数获取
		// 简化处理：使用 int64_t 作为默认类型（实际应该从 AST 获取参数类型）
		code += fmt.Sprintf("int64_t %s", param)
	}
	code += ") {\n"
	
	// 9. 生成函数体（精确替换泛型类型，避免误替换）
	sg.codegen.indent++
	for _, bodyStmt := range origFunc.Body {
		generated := sg.generateStatementForGeneric(bodyStmt, typeMap)
		code += sg.codegen.indentString() + generated
	}
	sg.codegen.indent--
	
	code += "}\n\n"
	return code
}

// generateStatementForGeneric 在泛型实例化中生成语句代码（精确类型替换）
func (sg *StatementGenerator) generateStatementForGeneric(stmt ast.Statement, typeMap map[string]string) string {
	generated := sg.codegen.generateStatement(stmt)
	
	// 精确替换类型声明中的泛型参数
	// 匹配模式： "T " (类型后跟空格) 或 "T*" (类型后跟指针) 或 "T;" (类型后跟分号)
	for origType, newType := range typeMap {
		// 替换变量声明中的类型
		generated = strings.ReplaceAll(generated, origType+" ", newType+" ")
		generated = strings.ReplaceAll(generated, origType+"*", newType+"*")
		generated = strings.ReplaceAll(generated, origType+";", newType+";")
	}
	
	return generated
}

// resolveSpecializedType 解析特化后的类型
func (sg *StatementGenerator) resolveSpecializedType(typeName string, typeArgs []string) string {
	for i, tp := range sg.codegen.currentFuncTypeParams {
		if typeName == tp.Name {
			if i < len(typeArgs) {
				return typeArgs[i]
			}
		}
	}
	return typeName
}

// generateVOStatement 生成 VO 语句代码
func (sg *StatementGenerator) generateVOStatement(stmt *ast.VOStatement) string {
	code := "/* vo block */\n"
	if stmt.Value != nil {
		code += "/* data: "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
		code += " */\n"
	}
	if stmt.Code != nil {
		code += "/* code: "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Code)
		code += " */\n"
	}
	code += "/* end vo */\n"
	return code
}

// generateSpendStatement 生成 spend 语句代码
func (sg *StatementGenerator) generateSpendStatement(stmt *ast.SpendStatement) string {
	code := "/* spend block */\n"

	if stmt.Target != nil {
		code += "/* target: "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Target)
		code += " */\n"
	}

	for i, callClause := range stmt.Calls {
		indexCode := sg.codegen.expressionGenerator.GenerateExpression(callClause.Index)
		code += fmt.Sprintf("/* call %d: index %s */\n", i+1, indexCode)
		for _, bodyStmt := range callClause.Body {
			code += sg.codegen.generateStatement(bodyStmt)
		}
		code += "/* end call */\n"
	}

	code += "/* end spend */\n"
	return code
}

// generateTaskStatement 生成 task 语句代码
func (sg *StatementGenerator) generateTaskStatement(stmt *ast.TaskStatement) string {
	code := "/* task */\n"
	code += fmt.Sprintf("/* priority: %d */\n", stmt.Priority)
	if stmt.Func != nil {
		code += "/* func: "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Func)
		code += " */\n"
	}
	if stmt.Arg != nil {
		code += "/* arg: "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Arg)
		code += " */\n"
	}
	code += "/* end task */\n"
	return code
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

// trimForClause 去除 for 循环子句末尾的分号、SOR 注释和换行
func trimForClause(code string) string {
	code = strings.TrimSuffix(code, "\n")
	code = strings.TrimSpace(code)
	// 去除 SOR 行尾注释 /* sor: ... */
	if idx := strings.LastIndex(code, "/* sor:"); idx != -1 {
		code = code[:idx]
		code = strings.TrimSpace(code)
	}
	code = strings.TrimSuffix(code, ";")
	code = strings.TrimSpace(code)
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
				if sym.Scope != "parameter" && sym.Scope != "param" && 
				   sym.Scope != "async_param" && sym.Scope != "task_param" {
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
				if sym.Scope != "parameter" && sym.Scope != "param" && 
				   sym.Scope != "async_param" && sym.Scope != "task_param" {
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
			if sym.Scope == "parameter" || sym.Scope == "param" || 
			   sym.Scope == "async_param" || sym.Scope == "task_param" {
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
						return sd.UsesBumpPool || sd.UsesArena != ""
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
			if sym.Scope == "parameter" || sym.Scope == "param" || 
			   sym.Scope == "async_param" || sym.Scope == "task_param" {
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

// optimizeForUpdate 检测 for 循环 update 表达式的常见模式并特化
// x = x + 1 → x++, x = x - 1 → x--
func optimizeForUpdate(stmt ast.Statement) string {
	exprStmt, ok := stmt.(*ast.ExpressionStatement)
	if !ok {
		return ""
	}
	binExpr, ok := exprStmt.Expression.(*ast.BinaryExpression)
	if !ok {
		return ""
	}
	// 模式: x = x + 1 或 x = x - 1
	if binExpr.Operator == "ASSIGN" {
		if rightBin, ok := binExpr.Right.(*ast.BinaryExpression); ok {
			if ident, ok := binExpr.Left.(*ast.Identifier); ok {
				if rightIdent, ok := rightBin.Left.(*ast.Identifier); ok {
					if ident.Name == rightIdent.Name {
						if intLit, ok := rightBin.Right.(*ast.IntegerLiteral); ok && intLit.Value == 1 {
							switch rightBin.Operator {
							case "PLUS", "+":
								return ident.Name + "++"
							case "MINUS", "-":
								return ident.Name + "--"
							}
						}
					}
				}
			}
		}
	}
	return ""
}

// generateForStatement 生成 for 语句代码
func (sg *StatementGenerator) generateForStatement(stmt *ast.ForStatement) string {
	code := "for ("
	if stmt.Init != nil {
		if exprStmt, ok := stmt.Init.(*ast.ExpressionStatement); ok {
			code += sg.codegen.expressionGenerator.GenerateExpression(exprStmt.Expression)
		} else {
			initCode := sg.codegen.generateStatement(stmt.Init)
			code += trimForClause(initCode)
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
		// 尝试特化常见增量模式: i = i + 1 → i++
		if optimized := optimizeForUpdate(stmt.Update); optimized != "" {
			code += optimized
		} else if exprStmt, ok := stmt.Update.(*ast.ExpressionStatement); ok {
			code += sg.codegen.expressionGenerator.GenerateExpression(exprStmt.Expression)
		} else {
			updateCode := sg.codegen.generateStatement(stmt.Update)
			code += trimForClause(updateCode)
		}
	} else {
		code += ""
	}
	code += ") {\n"

	// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
	sg.codegen.indent++
	sg.codegen.EnterScope("for_body")

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

// shouldUseKMMScopeForForBody 预判断 for 循环体是否需要 KMM scope
// 保留用于兼容，实际已被 shouldUseKMMScopeForBody 替代
func (sg *StatementGenerator) shouldUseKMMScopeForForBody() bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if sg.codegen.IsInKMMScope() {
		return false
	}

	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		currentScope := sg.codegen.GetCurrentScope()
		if currentScope == nil {
			return false
		}
		var varNames []string
		for name, sym := range currentScope.GetAllSymbols() {
			if sym.Scope == "local" {
				varNames = append(varNames, name)
			}
		}
		return adapter.NeedsKMMScopeByVars(varNames)
	}
	// 非 SOR 模式：基于符号表类型分析
	currentScope := sg.codegen.GetCurrentScope()
	if currentScope != nil {
		for _, sym := range currentScope.GetAllSymbols() {
			if sym.Scope == "local" && needsKMMForType(sym.Type) {
				return true
			}
		}
	}
	return false
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
		code += sg.codegen.indentString() + "break;\n"
		sg.codegen.indent--
	}
	if len(stmt.Default) > 0 {
		code += sg.codegen.indentString() + "default:\n"
		sg.codegen.indent++
		for _, bodyStmt := range stmt.Default {
			code += sg.codegen.indentString()
			code += sg.codegen.generateStatement(bodyStmt)
		}
		code += sg.codegen.indentString() + "break;\n"
		sg.codegen.indent--
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// generateReturnStatement 生成 return 语句代码
func (sg *StatementGenerator) generateReturnStatement(stmt *ast.ReturnStatement) string {
	retType := sg.codegen.currentFunctionReturnType
	isVoid := (retType == "" || retType == "void" || retType == "Void")
	
	if isVoid {
		code := "return;\n"
		return sg.codegen.indentString() + code
	}
	
	code := "return "
	if stmt.Value != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
	} else {
		code += "0"
	}
	code += ";\n"
	return sg.codegen.indentString() + code
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
	// 进入块作用域
	sg.codegen.EnterScope("block")

	indent := sg.codegen.indentString()

	// 分析块内的 malloc 调用
	canBatch, totalSize, mallocCount := sg.analyzeBlockMallocs(stmt.Statements)

	// 优化策略（激进触发，让单对象场景也能走最快路径）：
	// 1. 单个 malloc 且大小已知 -> 直接 bump + offset_restore (零 scope 栈开销)
	// 2. 多个 malloc 且大小都已知 -> 批量 bump + offset_restore (单次 bump 分配)
	// 3. 有 malloc 但大小未知 -> offset_save/restore (比 scope_push/pop 快)
	// 4. 无 malloc -> scope_push/pop (标准路径)
	if canBatch && totalSize > 0 && mallocCount >= 1 {
		// 批量/单对象分配优化：单次 bump + 直接 offset 恢复
		// 当 mallocCount == 1 时，totalSize 就是该 malloc 的大小
		// 当 mallocCount >= 2 时，totalSize 是所有 malloc 大小之和
		code := "{\n"
		sg.codegen.indent++
		sg.codegen.EnterOffsetScope()
		code += sg.codegen.indentString() + "size_t _scope_start = kmm_v4_offset_save();\n"
		code += sg.codegen.indentString()
		code += fmt.Sprintf("void* _batch_ptr = kmm_v4_bump(%d);\n", totalSize)
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

	if mallocCount >= 1 {
		// 有 malloc 但大小未知 -> offset_save/restore (轻量级，无 scope 栈操作)
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

	// 标准路径：scope push/pop (无 malloc 时)
	// 相邻 scope 合并优化：如果已在 offset scope 内，跳过 KMM scope 包裹，直接内联
	if sg.codegen.IsInOffsetScope() {
		// 已在 offset scope 内，直接内联块内容，避免重复 scope 开销
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

	// 生成内存释放代码
	code += sg.codegen.indentString() + "// Free allocated memory\n"
	for name, symbol := range sg.codegen.currentScope.GetAllSymbols() {
		if symbol.Nullable {
			if symbol.Type == "string" {
				code += sg.codegen.indentString()
				code += "if (" + name + " != NULL) { free(" + name + "); }\n"
			}
		}
	}

	sg.codegen.indent--
	code += indent + "} KMM_V4_SCOPE_END;\n"

	// 退出块作用域
	sg.codegen.ExitScope()
	return code
}

// mallocAnalysis 分析结果结构
type mallocAnalysis struct {
	count       int   // malloc 调用总数
	totalSize   int   // 所有 malloc 大小之和（仅计算已知大小）
	allKnown    bool  // 是否所有 malloc 大小都已知
	sizes       []int // 每个 malloc 的大小（0 表示未知）
	isContiguous bool // malloc 是否连续出现（中间无其他语句）
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
		count:       0,
		totalSize:   0,
		allKnown:    true,
		sizes:       make([]int, 0),
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