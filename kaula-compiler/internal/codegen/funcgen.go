package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

type FunctionGenerator struct {
	codegen *CodeGenerator
}

func NewFunctionGenerator(cg *CodeGenerator) *FunctionGenerator {
	return &FunctionGenerator{
		codegen: cg,
	}
}

// needsKMMScope 检查函数体是否需要 KMM scope
func (fg *FunctionGenerator) needsKMMScope(bodyCode string) bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if fg.codegen.IsInKMMScope() {
		return false
	}

	adapter := fg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		currentScope := fg.codegen.GetCurrentScope()
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
	return fg.needsKMMScopeNonSOR(bodyCode)
}

// shouldUseKMMScopeForBody 基于 AST body 语句列表预判断是否需要 KMM scope
// 解决符号表在 body 生成前未填充的问题：直接扫描 AST 变量声明
// 跨函数分析优化：纯函数（无指针所有权传递）可以跳过 KMM
func (fg *FunctionGenerator) shouldUseKMMScopeForBody(bodyStmts []ast.Statement) bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if fg.codegen.IsInKMMScope() {
		return false
	}

	adapter := fg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		// 跨函数分析：纯函数跳过 KMM
		// 注意：此处不使用 IsPureFunction，因为即使函数是纯函数，
		// 函数体内部仍可能声明需要 KMM 的局部变量（指针/字符串）
		// SOR 模式：收集 body 中的变量名
		var varNames []string
		for _, stmt := range bodyStmts {
			if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
				varNames = append(varNames, varDecl.Name)
			}
		}
		return adapter.NeedsKMMScopeByVars(varNames)
	}

	// 非 SOR 模式：扫描 body 中的变量类型
	for _, stmt := range bodyStmts {
		if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
			if needsKMMForType(varDecl.Type) {
				return true
			}
		}
	}
	return false
}

// bodyHasAllocationCall 扫描函数体是否包含内存分配调用
// 纯计算函数（无 std_malloc/kmm_v4/yeide/release/extract）可跳过 scope 管理
func (fg *FunctionGenerator) bodyHasAllocationCall(bodyStmts []ast.Statement) bool {
	for _, stmt := range bodyStmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.ExpressionStatement:
			if s != nil && s.Expression != nil {
				if fg.exprHasAllocCall(s.Expression) {
					return true
				}
			}
		case *ast.VariableDeclaration:
			if s != nil && s.Value != nil {
				if fg.exprHasAllocCall(s.Value) {
					return true
				}
			}
		case *ast.ReturnStatement:
			if s != nil && s.Value != nil {
				if fg.exprHasAllocCall(s.Value) {
					return true
				}
			}
		case *ast.IfStatement:
			if s != nil && fg.bodyHasAllocationCall(s.Body) {
				return true
			}
			if s != nil && s.Else != nil && fg.bodyHasAllocationCall(s.Else) {
				return true
			}
		case *ast.WhileStatement:
			if s != nil && fg.bodyHasAllocationCall(s.Body) {
				return true
			}
		case *ast.ForStatement:
			if s != nil && fg.bodyHasAllocationCall(s.Body) {
				return true
			}
		}
	}
	return false
}

func (fg *FunctionGenerator) exprHasAllocCall(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.CallExpression:
		if ident, ok := e.Function.(*ast.Identifier); ok {
			name := ident.Name
			if name == "std_malloc" || name == "kmm_v4_malloc" || name == "kmm_v4_alloc_auto" {
				return true
			}
		}
		// 检查 std.memory.std_malloc 等链式调用
		if member, ok := e.Function.(*ast.MemberAccessExpression); ok {
			if member.Member == "std_malloc" || member.Member == "kmm_v4_malloc" {
				return true
			}
		}
	case *ast.BinaryExpression:
		return fg.exprHasAllocCall(e.Left) || fg.exprHasAllocCall(e.Right)
	case *ast.UnaryExpression:
		return fg.exprHasAllocCall(e.Right)
	}
	return false
}

// shouldUseKMMScopeForFunc 函数级别的 KMM 判断，结合跨函数分析
// 如果 SOR 跨函数分析确定函数是纯函数且函数体无 KMM 变量，则跳过 KMM
func (fg *FunctionGenerator) shouldUseKMMScopeForFunc(funcName string, bodyStmts []ast.Statement) bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if fg.codegen.IsInKMMScope() {
		return false
	}

	// 优化：扫描函数体是否包含分配调用，纯计算函数跳过 scope
	if !fg.bodyHasAllocationCall(bodyStmts) {
		return false
	}

	adapter := fg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		// SOR 模式：先基于 SOR 决策判断
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
		// （如 std_malloc 分配的指针，SOR 不追踪但需要 KMM 管理）
		for _, stmt := range bodyStmts {
			if stmt == nil {
				continue
			}
			if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
				if needsKMMForType(varDecl.Type) {
					return true
				}
			}
		}
		return false
	}

	// 非 SOR 模式：扫描 body 中的变量类型
	for _, stmt := range bodyStmts {
		if stmt == nil {
			continue
		}
		varDecl, ok := stmt.(*ast.VariableDeclaration)
		if !ok || varDecl == nil {
			continue
		}
		if needsKMMForType(varDecl.Type) {
			return true
		}
	}
	return false
}

// shouldUseKMMScope 预判断：在生成 body 之前基于符号表判断是否需要 KMM scope
func (fg *FunctionGenerator) shouldUseKMMScope() bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if fg.codegen.IsInKMMScope() {
		return false
	}

	adapter := fg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		currentScope := fg.codegen.GetCurrentScope()
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

	// 非 SOR 模式：基于符号表类型分析
	currentScope := fg.codegen.GetCurrentScope()
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

// needsKMMScopeNonSOR 非 SOR 模式下的智能 KMM 判断
// 基于符号表分析变量类型，判断是否需要 KMM scope
func (fg *FunctionGenerator) needsKMMScopeNonSOR(bodyCode string) bool {
	if !strings.Contains(bodyCode, "std_malloc") && 
	   !strings.Contains(bodyCode, "kmm_v4") &&
	   !strings.Contains(bodyCode, "string_concat") &&
	   !strings.Contains(bodyCode, "string_dup") {
		return false
	}

	currentScope := fg.codegen.GetCurrentScope()
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

	if strings.Contains(bodyCode, "kmm_v4_alloc_auto") {
		return true
	}

	return false
}

func (fg *FunctionGenerator) GenerateFunctionStatement(stmt *ast.FunctionStatement) string {
	fg.codegen.EnterScope("function_" + stmt.Name)

	annotation := stmt.GetAnnotation()

	if annotation == ast.TreeAnnotationPrefix || annotation == ast.TreeAnnotationPrefixTree {
		return fg.generatePrefixFunction(stmt)
	}

	if annotation == ast.TreeAnnotationTree {
		return fg.generateTreeFunction(stmt)
	}

	if annotation == ast.TreeAnnotationRoot || annotation == ast.TreeAnnotationRootTree {
		return fg.generateRootTreeFunction(stmt)
	}

	if stmt.Name == "main" {
		return fg.generateMainFunction(stmt)
	}

	hasTaskParams := len(stmt.TaskParams) > 0
	hasAsyncParams := len(stmt.AsyncParams) > 0

	var builder strings.Builder
	builder.Grow(1024)

	if stmt.Inline {
		builder.WriteString("__attribute__((always_inline)) ")
	}
	
	safeName := stmt.Name
	if safeName == "max" || safeName == "min" || safeName == "abs" {
		safeName = "kaula_" + safeName
	}
	
	if stmt.IsGeneric() {
		fg.codegen.ExitScope()
		return ""
	}
	
	// 设置当前函数返回类型供 return 语句生成使用
	fg.codegen.currentFunctionReturnType = stmt.ReturnType
	
	returnType := fg.mapReturnType(stmt.ReturnType)
	builder.WriteString(returnType)
	builder.WriteString(safeName)
	if hasTaskParams {
		builder.WriteString("_task(void* arg) {\n")
	} else if hasAsyncParams {
		builder.WriteString("_async(void* arg) {\n")
	} else {
		// 生成原生 C 参数列表
		builder.WriteString("(")
		for i, param := range stmt.Params {
			if i > 0 {
				builder.WriteString(", ")
			}
			// 获取参数类型
			paramType := "int64_t" // 默认类型
			if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
				paramType = fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], false)
			}
			builder.WriteString(fmt.Sprintf("%s %s", paramType, param))
			fg.codegen.AddSymbol(param, stmt.ParamTypes[i], false, "parameter", stmt.Pos.Line, stmt.Pos.Column)
		}
		if len(stmt.Params) == 0 {
			builder.WriteString("void")
		}
		builder.WriteString(") {\n")
	}
	fg.codegen.indent++

	if hasTaskParams {
		indent := fg.codegen.indentString()
		builder.WriteString(indent)
		builder.WriteString("if (arg == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("TaskParam* tp = (TaskParam*)arg;\n")
		builder.WriteString(indent)
		builder.WriteString("if (tp == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("int priority = tp->priority;\n")
		builder.WriteString(indent)
		builder.WriteString("void* result = tp->data;\n")

		for i := range stmt.TaskParams {
			priorityCode := fg.codegen.expressionGenerator.GenerateExpression(stmt.TaskParams[i].Priority)
			builder.WriteString(indent)
			fmt.Fprintf(&builder, "// Task参数 %d: 优先级=%s (索引: %d)\n", i+1, priorityCode, i)
		}
		
		if len(stmt.TaskParams) > 0 {
			builder.WriteString(indent)
			for i := range stmt.TaskParams {
				paramName := fmt.Sprintf("task_param_%d", i)
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = ((int64_t*)tp->data)[%d];\n", paramName, i)
				fg.codegen.AddSymbol(paramName, "int64_t", false, "task_param", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
	} else if hasAsyncParams {
		indent := fg.codegen.indentString()
		builder.WriteString(indent)
		builder.WriteString("if (arg == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("AsyncParam* ap = (AsyncParam*)arg;\n")
		builder.WriteString(indent)
		builder.WriteString("if (ap == NULL) { return -1; }\n")
		builder.WriteString(indent)
		builder.WriteString("void* async_value = ap->data;\n")

		for i := range stmt.AsyncParams {
			valueCode := fg.codegen.expressionGenerator.GenerateExpression(stmt.AsyncParams[i].Value)
			builder.WriteString(indent)
			fmt.Fprintf(&builder, "// Async参数 %d: 值=%s (索引: %d)\n", i+1, valueCode, i)
		}
		
		if len(stmt.AsyncParams) > 0 {
			builder.WriteString(indent)
			for i := range stmt.AsyncParams {
				paramName := fmt.Sprintf("async_param_%d", i)
				builder.WriteString(indent)
				fmt.Fprintf(&builder, "int64_t %s = ((int64_t*)ap->data)[%d];\n", paramName, i)
				fg.codegen.AddSymbol(paramName, "int64_t", false, "async_param", stmt.Pos.Line, stmt.Pos.Column)
			}
		}

		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
		} else {
			shouldUseKMM := !stmt.NoKMM && !stmt.Inline
			if shouldUseKMM {
				// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
				// 跨函数分析：结合函数签名判断是否需要 KMM
				useKMM := fg.shouldUseKMMScopeForFunc(stmt.Name, stmt.Body)
				if useKMM {
					fg.codegen.EnterKMMScope()
				}

				var bodyBuilder strings.Builder
				bodyIndent := fg.codegen.indentString()
				fg.codegen.indent++
				for _, bodyStmt := range stmt.Body {
					if bodyStmt == nil {
						continue
					}
					bodyBuilder.WriteString(fg.codegen.indentString())
					bodyBuilder.WriteString(fg.codegen.generateStatement(bodyStmt))
				}
				fg.codegen.indent--

				if useKMM {
					fg.codegen.ExitKMMScope()
				}

				bodyCode := bodyBuilder.String()

				if useKMM {
					// 批量分配优化：尝试将多次 malloc 合并为一次 bump
					canBatch, totalSize, _ := fg.analyzeBodyMallocs(stmt.Body)
					if canBatch && totalSize > 0 {
						// 优化路径：单次 bump 分配 + 直接 offset 恢复
						builder.WriteString(bodyIndent)
						builder.WriteString("{\n")
						builder.WriteString(bodyIndent)
						builder.WriteString("    size_t _scope_start = kmm_v4_offset_save();\n")
						builder.WriteString(bodyIndent)
						fmt.Fprintf(&builder, "    void* _batch_ptr = kmm_v4_bump(%d);\n", totalSize)
						builder.WriteString(bodyCode)
						builder.WriteString(bodyIndent)
						builder.WriteString("    kmm_v4_offset_restore(_scope_start);\n")
						builder.WriteString(bodyIndent)
						builder.WriteString("}\n")
					} else {
						// 标准路径：scope push/pop
						builder.WriteString(bodyIndent)
						builder.WriteString("KMM_V4_SCOPE_START {\n")
						builder.WriteString(bodyCode)
						builder.WriteString(bodyIndent)
						builder.WriteString("} KMM_V4_SCOPE_END;\n")
					}
				} else {
					builder.WriteString(bodyCode)
				}
			} else {
				indent := fg.codegen.indentString()
				for _, bodyStmt := range stmt.Body {
					if bodyStmt == nil {
						continue
					}
					builder.WriteString(indent)
					builder.WriteString(fg.codegen.generateStatement(bodyStmt))
				}
			}
		}

	if !hasReturnStatement(stmt.Body) && stmt.ReturnType != "" && !isVoidType(stmt.ReturnType) {
		builder.WriteString(fg.codegen.indentString())
		builder.WriteString("return 0;\n")
	}
	fg.codegen.indent--
	builder.WriteString("}\n")

	fg.codegen.ExitScope()
	fg.codegen.currentFunctionReturnType = ""
	return builder.String()
}

func (fg *FunctionGenerator) generatePrefixFunction(stmt *ast.FunctionStatement) string {
	var code strings.Builder
	code.Grow(1024)
	code.WriteString("// Prefix function: AST generation for cross-file reuse\n")
	fmt.Fprintf(&code, "int64_t %s", stmt.Name)

	if len(stmt.Params) > 0 {
		code.WriteString("(int64_t arg) {\n")
	} else {
		code.WriteString("(void) {\n")
	}

	fg.codegen.indent++

	if stmt.PrefixName != "" {
		code.WriteString(fg.codegen.indentString())
		fmt.Fprintf(&code, "prefix_enter(\"%s\");\n", stmt.PrefixName)
	}

	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code.WriteString(fg.codegen.indentString())
		code.WriteString(fg.codegen.generateStatement(bodyStmt))
	}

	if stmt.PrefixName != "" {
		code.WriteString(fg.codegen.indentString())
		code.WriteString("prefix_leave();\n")
	}

	fg.codegen.indent--
	code.WriteString("}\n")

	fg.codegen.ExitScope()
	return code.String()
}

func (fg *FunctionGenerator) generateTreeFunction(stmt *ast.FunctionStatement) string {
	var code strings.Builder
	code.Grow(1024)
	code.WriteString("// Tree function: AST generation with root validation\n")
	fmt.Fprintf(&code, "int64_t %s", stmt.Name)

	if len(stmt.Params) > 0 {
		code.WriteString("(int64_t arg) {\n")
	} else {
		code.WriteString("(void) {\n")
	}

	fg.codegen.indent++

	rootTree := fg.codegen.treeManager.GetRootTree()
	if rootTree == nil {
		code.WriteString(fg.codegen.indentString())
		code.WriteString("// ERROR: Tree function but no root tree defined\n")
	}

	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code.WriteString(fg.codegen.indentString())
		code.WriteString(fg.codegen.generateStatement(bodyStmt))
	}

	fg.codegen.indent--
	code.WriteString("}\n")

	fg.codegen.ExitScope()
	return code.String()
}

func (fg *FunctionGenerator) generateRootTreeFunction(stmt *ast.FunctionStatement) string {
	var code strings.Builder
	code.Grow(1024)
	code.WriteString("// Root tree function: defines global tree structure\n")
	fmt.Fprintf(&code, "int64_t %s", stmt.Name)

	if len(stmt.Params) > 0 {
		code.WriteString("(int64_t arg) {\n")
	} else {
		code.WriteString("(void) {\n")
	}

	fg.codegen.indent++

	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		code.WriteString(fg.codegen.indentString())
		code.WriteString(fg.codegen.generateStatement(bodyStmt))
	}

	fg.codegen.indent--
	code.WriteString("}\n")

	fg.codegen.ExitScope()
	return code.String()
}

func hasReturnStatement(stmts []ast.Statement) bool {
	for _, s := range stmts {
		if _, ok := s.(*ast.ReturnStatement); ok {
			return true
		}
		if block, ok := s.(*ast.BlockStatement); ok {
			if hasReturnStatement(block.Statements) {
				return true
			}
		}
		if ifStmt, ok := s.(*ast.IfStatement); ok {
			if hasReturnStatement(ifStmt.Body) || hasReturnStatement(ifStmt.Else) {
				return true
			}
		}
	}
	return false
}

func isVoidType(typeName string) bool {
	return typeName == "void" || typeName == "Void"
}

func (fg *FunctionGenerator) generateMainFunction(stmt *ast.FunctionStatement) string {
	var code strings.Builder
	code.Grow(2048)
	if stmt.Inline {
		code.WriteString("__attribute__((always_inline)) ")
	}
	code.WriteString("int main() {\n")
	fg.codegen.indent++
	
	// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
	useKMM := !stmt.NoKMM && fg.shouldUseKMMScopeForBody(stmt.Body)
	if useKMM {
		fg.codegen.EnterKMMScope()
	}

	var bodyBuilder strings.Builder
	for _, bodyStmt := range stmt.Body {
		if bodyStmt == nil {
			continue
		}
		bodyBuilder.WriteString(fg.codegen.indentString())
		bodyBuilder.WriteString(fg.codegen.generateStatement(bodyStmt))
	}

	if useKMM {
		fg.codegen.ExitKMMScope()
	}

	bodyCode := bodyBuilder.String()

	if useKMM {
		code.WriteString(fg.codegen.indentString())
		code.WriteString("KMM_V4_SCOPE_START {\n")
		code.WriteString(bodyCode)
		code.WriteString(fg.codegen.indentString())
		code.WriteString("} KMM_V4_SCOPE_END;\n")
	} else {
		code.WriteString(bodyCode)
	}
	
	code.WriteString(fg.codegen.indentString())
	code.WriteString("return 0;\n")
	fg.codegen.indent--
	code.WriteString("}\n")
	
	fg.codegen.ExitScope()
	return code.String()
}

func (fg *FunctionGenerator) mapReturnType(returnType string) string {
	if returnType == "" {
		return "void "
	}
	
	switch returnType {
	case "int":
		return "int "
	case "i64":
		return "int64_t "
	case "u64":
		return "uint64_t "
	case "i32":
		return "int32_t "
	case "u32":
		return "uint32_t "
	case "i16":
		return "int16_t "
	case "u16":
		return "uint16_t "
	case "i8":
		return "int8_t "
	case "u8":
		return "uint8_t "
	case "float":
		return "float "
	case "f32":
		return "float "
	case "double":
		return "double "
	case "f64":
		return "double "
	case "bool":
		return "int "
	case "char":
		return "char "
	case "void":
		return "void "
	case "string":
		return "char* "
	default:
		return returnType + " "
	}
}

// ============================================================================
// 批量分配优化：将同一 scope 内的多次 malloc 合并为一次 bump
// ============================================================================

// mallocInfo 记录一个 malloc 调用的信息
type mallocInfo struct {
	varName   string // 变量名
	sizeExpr  string // 大小表达式（编译期可确定时为具体数字）
	sizeBytes int    // 编译期可确定时的字节数，0 表示未知
}

// analyzeBodyMallocs 分析函数体中的 malloc 调用，尝试计算总大小
// 返回是否可以使用批量分配，以及总大小（如果可计算）
func (fg *FunctionGenerator) analyzeBodyMallocs(bodyStmts []ast.Statement) (canBatch bool, totalSize int, mallocs []mallocInfo) {
	mallocs = make([]mallocInfo, 0)
	totalSize = 0
	allKnown := true

	for _, stmt := range bodyStmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.VariableDeclaration:
			if s.Value != nil {
				if call, ok := s.Value.(*ast.CallExpression); ok {
					if fg.isMallocCall(call) {
						sizeExpr, sizeBytes := fg.extractMallocSize(call)
						mallocs = append(mallocs, mallocInfo{
							varName:   s.Name,
							sizeExpr:  sizeExpr,
							sizeBytes: sizeBytes,
						})
						if sizeBytes > 0 {
							totalSize += sizeBytes
						} else {
							allKnown = false
						}
					}
				}
			}
		case *ast.ExpressionStatement:
			if s.Expression != nil {
				if call, ok := s.Expression.(*ast.CallExpression); ok {
					if fg.isMallocCall(call) {
						sizeExpr, sizeBytes := fg.extractMallocSize(call)
						mallocs = append(mallocs, mallocInfo{
							varName:   "",
							sizeExpr:  sizeExpr,
							sizeBytes: sizeBytes,
						})
						if sizeBytes > 0 {
							totalSize += sizeBytes
						} else {
							allKnown = false
						}
					}
				}
			}
		}
	}

	// 至少有 2 个 malloc 调用才值得批量分配
	canBatch = len(mallocs) >= 2 && allKnown
	return
}

// isMallocCall 检查调用是否是 malloc 类函数
func (fg *FunctionGenerator) isMallocCall(call *ast.CallExpression) bool {
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

// extractMallocSize 从 malloc 调用中提取大小表达式
// 如果是编译期可确定的常量，返回具体字节数
func (fg *FunctionGenerator) extractMallocSize(call *ast.CallExpression) (string, int) {
	if call == nil || len(call.Args) == 0 {
		return "", 0
	}
	arg := call.Args[0]
	// 尝试解析整数字面量
	if lit, ok := arg.(*ast.IntegerLiteral); ok {
		return "", int(lit.Value)
	}
	// 尝试解析 sizeof 表达式
	if callExpr, ok := arg.(*ast.CallExpression); ok {
		if ident, ok := callExpr.Function.(*ast.Identifier); ok {
			if ident.Name == "sizeof" && len(callExpr.Args) == 1 {
				// sizeof(Type) — 编译器可以确定大小
				return "", 8 // 默认 8 字节，实际应根据类型推导
			}
		}
	}
	// 无法编译期确定
	return fg.codegen.expressionGenerator.GenerateExpression(arg), 0
}
