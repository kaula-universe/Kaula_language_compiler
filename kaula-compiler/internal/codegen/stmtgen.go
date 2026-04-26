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
	default:
		return ""
	}
}

// generateVariableDeclaration 生成变量声明代码
func (sg *StatementGenerator) generateVariableDeclaration(stmt *ast.VariableDeclaration) string {
	// 将变量添加到当前作用域的符号表
	sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local", stmt.Pos.Line, stmt.Pos.Column)
	
	var builder strings.Builder
	builder.Grow(64)
	
	// 生成 C 风格的变量声明
	switch stmt.Type {
	case "int":
		builder.WriteString("int ")
	case "float":
		builder.WriteString("float ")
	case "double":
		builder.WriteString("double ")
	case "bool":
		builder.WriteString("bool ")
	case "char":
		builder.WriteString("char ")
	case "string":
		builder.WriteString("char* ")
	case "i64":
		builder.WriteString("int64_t ")
	case "u64":
		builder.WriteString("uint64_t ")
	case "i32":
		builder.WriteString("int32_t ")
	case "u32":
		builder.WriteString("uint32_t ")
	default:
		// 自定义类型或关键字映射到C类型
		builder.WriteString(stmt.Type)
		builder.WriteByte(' ')
	}
	
	builder.WriteString(stmt.Name)
	
	if stmt.Value != nil {
		builder.WriteString(" = ")
		builder.WriteString(sg.codegen.expressionGenerator.GenerateExpression(stmt.Value))
	} else if stmt.Nullable {
		// 对于可空类型，如果没有初始化值，初始化为 NULL
		builder.WriteString(" = NULL")
	}
	builder.WriteString(";\n")
	return builder.String()
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
	code := fmt.Sprintf("VOModule* vo = vo_create(%d);\n", sg.codegen.config.VOCacheSize)
	if stmt.Value != nil {
		code += "// Load data\n"
		code += "vo_data_load(vo, 0, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
		code += ");\n"
	}
	if stmt.Code != nil {
		code += "// Load code\n"
		code += "vo_code_load(vo, -1, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Code)
		code += ");\n"
	}
	// 处理 associate 操作
	code += "// Associate data and code\n"
	code += "vo_associate(vo, 0, -1);\n"
	if stmt.Access != nil {
		code += "// Access data\n"
		code += "void* result = vo_access(vo, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Access)
		code += ");\n"
	}
	code += "// VO cleanup handled by KMM\n"
	return code
}

// generateSpendStatement 生成 spend 语句代码
// spend 锁定对象，call 必须被调用与元素数量对应的次数
// 语法：
// spend(obj1){
//     call(1){
//         // 处理第 1 个元素
//     }
//     call(2){
//         // 处理第 2 个元素
//     }
// }
func (sg *StatementGenerator) generateSpendStatement(stmt *ast.SpendStatement) string {
	code := ""

	if stmt.Target == nil {
		return "// Error: spend statement missing target\n"
	}

	// 生成目标表达式
	targetCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Target)

	// 生成 spendable 创建和锁定
	code += "// Spend: 锁定并开启消费流程\n"
	code += fmt.Sprintf("{\n")
	sg.codegen.indent++

	// 创建 Spendable 并添加元素
	code += sg.codegen.indentString()
	code += fmt.Sprintf("Spendable* sp = spendable_create(16);  // capacity=16\n")

	// 假设目标是数组，需要遍历添加元素
	// 这里简化处理，实际需要根据目标类型生成不同的添加逻辑
	code += sg.codegen.indentString()
	code += fmt.Sprintf("// 添加目标元素到 spendable\n")
	code += sg.codegen.indentString()
	code += fmt.Sprintf("spendable_add(sp, %s);\n", targetCode)

	code += sg.codegen.indentString()
	code += "spend_lock(sp);  // 锁定目标\n"

	// 生成 call 子句
	code += "\n"
	code += sg.codegen.indentString()
	code += "// Call: 消费元素（次数必须与元素数量匹配）\n"

	for i, callClause := range stmt.Calls {
		indexCode := sg.codegen.expressionGenerator.GenerateExpression(callClause.Index)
		code += "\n"
		code += sg.codegen.indentString()
		code += fmt.Sprintf("// Call %d: 消费索引 %s\n", i+1, indexCode)

		// 调用消费指定索引的元素
		code += sg.codegen.indentString()
		code += fmt.Sprintf("void* element_%d = spend_call(sp, %s);\n", i+1, indexCode)

		// 生成处理逻辑
		if len(callClause.Body) > 0 {
			code += sg.codegen.indentString()
			code += "{\n"
			sg.codegen.indent++

			// 将 element 变量添加到当前作用域
			code += sg.codegen.indentString()
			code += fmt.Sprintf("void* element = element_%d;\n", i+1)

			for _, bodyStmt := range callClause.Body {
				code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
			}

			sg.codegen.indent--
			code += sg.codegen.indentString()
			code += "}\n"
		}
	}

	code += "\n"
	code += sg.codegen.indentString()
	code += "spend_unlock(sp);  // 解除锁定\n"

	// 清理 Spendable
	code += sg.codegen.indentString()
	code += "spendable_destroy(sp);\n"

	sg.codegen.indent--
	code += sg.codegen.indentString()
	code += "}\n"

	return code
}

// generateTaskStatement 生成 task 语句代码
func (sg *StatementGenerator) generateTaskStatement(stmt *ast.TaskStatement) string {
	code := fmt.Sprintf("PriorityQueue* pq = priority_queue_create(%d);\n", sg.codegen.config.QueueSize)
	code += "if (pq == NULL) { return -1; }\n"
	code += "// Add task to priority queue\n"
	code += "priority_queue_add(pq, "
	code += fmt.Sprintf("%d", stmt.Priority)
	code += ", "
	if stmt.Func != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Func)
	} else {
		code += "NULL"
	}
	code += ", "
	if stmt.Arg != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Arg)
	} else {
		code += "NULL"
	}
	code += ");\n"
	code += "// Execute task\n"
	code += "void* result = priority_queue_execute_next(pq);\n"
	code += "// Task cleanup\n"
	code += "priority_queue_destroy(pq);\n"
	return code
}

// generatePrefixStatement 生成 prefix 语句代码
func (sg *StatementGenerator) generatePrefixStatement(stmt *ast.PrefixStatement) string {
	// 在 PrefixManager 中创建前缀上下文
	sg.codegen.prefixManager.CreatePrefix(stmt.Name, core.PrefixAnnotationPrefix)

	// 转义名称，防止 C 字符串注入
	safeName := escapeCString(stmt.Name)

	// 生成 C 代码，使用标准库中的前缀系统实现
	code := "PrefixSystem* prefix_system = prefix_system_create();\n"
	code += fmt.Sprintf("prefix_enter(\"%s\");\n", safeName)

	// 生成前缀体内的代码
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "prefix_leave();\n"
	code += "prefix_system_destroy(prefix_system);\n"
	return code
}

// generatePrefixCallBody 生成前缀调用体的代码 - AST 直接插入
func (sg *StatementGenerator) generatePrefixCallBody(e *ast.PrefixCallExpression) string {
	code := ""

	// 查找前缀函数的声明
	funcDecl := sg.findFunctionDeclaration(e.Name)
	if funcDecl == nil {
		// 如果找不到函数声明，说明还没有解析到前缀定义，跳过
		// 或者前缀来自导入
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
		code += "// Root tree definition (global matching priority)\n"
		code += sg.generateRootTreeDefinition(stmt)
	} else if annotation == ast.TreeAnnotationPrefix || annotation == ast.TreeAnnotationPrefixTree {
		code += "// Prefix tree definition\n"
		code += sg.generatePrefixTreeDefinition(stmt)
	} else if annotation == ast.TreeAnnotationTree {
		if sg.codegen.treeManager != nil {
			rootTree := sg.codegen.treeManager.GetRootTree()
			if rootTree == nil {
				code += "// ERROR: Orphan tree - no root tree defined\n"
				code += sg.generateOrphanTreeError(stmt)
			} else {
				code += "// Tree structure (validated against root)\n"
				code += sg.generateValidatedTree(stmt, rootTree)
			}
		} else {
			code += "// Tree structure implementation\n"
			code += sg.generateTreeImplementation(stmt)
		}
	} else {
		code += "// Tree structure implementation\n"
		code += sg.generateTreeImplementation(stmt)
	}

	return code
}

func (sg *StatementGenerator) generateRootTreeDefinition(stmt *ast.TreeStatement) string {
	code := "// Root tree: defines global structure for all trees\n"
	code += "TreeDefinition* root_tree = tree_define_root();\n"

	if stmt.Root != nil {
		code += "// Root value\n"
		code += "tree_set_root_value(root_tree, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Root)
		code += ");\n"
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	return code
}

func (sg *StatementGenerator) generatePrefixTreeDefinition(stmt *ast.TreeStatement) string {
	code := "// Prefix tree: function-level reuse without import\n"

	if stmt.Root != nil {
		if ident, ok := stmt.Root.(*ast.Identifier); ok {
			prefixName := ident.Name
			code += fmt.Sprintf("PrefixTree* %s_tree = tree_define_prefix(\"%s\");\n", prefixName, prefixName)
		}
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	return code
}

func (sg *StatementGenerator) generateValidatedTree(stmt *ast.TreeStatement, rootTree *core.Tree) string {
	code := "// Tree structure (validated against root)\n"
	code += "Tree* tree = tree_create();\n"

	if stmt.Root != nil {
		code += "// Set root value\n"
		code += "tree_set_root(tree, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Root)
		code += ");\n"
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "// Tree validated against root tree structure\n"
	return code
}

func (sg *StatementGenerator) generateOrphanTreeError(stmt *ast.TreeStatement) string {
	code := fmt.Sprintf("// ERROR: Tree at line %d is orphan - no root tree defined\n", stmt.Pos.Line)
	code += "// Consider wrapping this tree in a prefix or class, or marking it with #[root,tree]\n"
	code += "// Example: #[prefix,tree] fn wrap() { ... tree code ... }\n"
	return code
}

func (sg *StatementGenerator) generateTreeImplementation(stmt *ast.TreeStatement) string {
	code := "// Tree structure implementation\n"
	code += "Tree* tree = tree_create();\n"
	if stmt.Root != nil {
		code += "// Set root value\n"
		code += "tree_set_root(tree, "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Root)
		code += ");\n"
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "// Tree cleanup handled by KMM\n"
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
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString()
		code += sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}"
	if len(stmt.Else) > 0 {
		code += " else {\n"
		sg.codegen.indent++
		for _, elseStmt := range stmt.Else {
			code += sg.codegen.indentString()
			code += sg.codegen.generateStatement(elseStmt)
		}
		sg.codegen.indent--
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
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString()
		code += sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// generateForStatement 生成 for 语句代码
func (sg *StatementGenerator) generateForStatement(stmt *ast.ForStatement) string {
	code := "for ("
	if stmt.Init != nil {
		if exprStmt, ok := stmt.Init.(*ast.ExpressionStatement); ok {
			code += sg.codegen.expressionGenerator.GenerateExpression(exprStmt.Expression)
		} else {
			code += sg.codegen.generateStatement(stmt.Init)
			code = strings.TrimSuffix(code, ";\n")
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
		if exprStmt, ok := stmt.Update.(*ast.ExpressionStatement); ok {
			code += sg.codegen.expressionGenerator.GenerateExpression(exprStmt.Expression)
		} else {
			code += sg.codegen.generateStatement(stmt.Update)
			code = strings.TrimSuffix(code, ";\n")
		}
	} else {
		code += ""
	}
	code += ") {\n"
	sg.codegen.indent++
	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.indentString()
		code += sg.codegen.generateStatement(bodyStmt)
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
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
		sg.codegen.indent--
	}
	if len(stmt.Default) > 0 {
		code += sg.codegen.indentString() + "default:\n"
		sg.codegen.indent++
		for _, bodyStmt := range stmt.Default {
			code += sg.codegen.indentString()
			code += sg.codegen.generateStatement(bodyStmt)
		}
		sg.codegen.indent--
	}
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// generateReturnStatement 生成 return 语句代码
func (sg *StatementGenerator) generateReturnStatement(stmt *ast.ReturnStatement) string {
	code := "return "
	if stmt.Value != nil {
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
	} else {
		code += "0"
	}
	code += ";\n"
	return code
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
	
	code := "{\n"
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
	code += sg.codegen.indentString() + "}\n"
	
	// 退出块作用域
	sg.codegen.ExitScope()
	return code
}
