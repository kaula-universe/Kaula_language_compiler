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
				if sym.Scope != "parameter" && sym.Scope != "param" {
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
// 纯计算函数（无 std_malloc/kmm_v4/yield/release/extract）可跳过 scope 管理
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
		case *ast.ForInStatement:
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
				if sym.Scope != "parameter" && sym.Scope != "param" {
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
			if sym.Scope == "parameter" || sym.Scope == "param" {
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

	if stmt.IsAsm {
		return fg.generateAsmFunction(stmt)
	}

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

	// 嵌套函数：在参数注册之后检测捕获的外层局部变量
	// （main/prefix/tree/asm 均不会有外层函数作用域，捕获集为空）
	captures := fg.codegen.detectCaptures(stmt)
	if fg.codegen.inFunctionBody {
		// 嵌套函数提升为文件级后，调用点（所在外层函数体）先于其定义出现，
		// 必须在文件头部追加原型声明（forward declaration），否则 clang 报隐式声明错误
		var proto strings.Builder
		proto.WriteString(fg.mapReturnType(stmt.ReturnType))
		proto.WriteByte(' ')
		proto.WriteString(stmt.Name)
		proto.WriteByte('(')
		for i, param := range stmt.Params {
			if i > 0 {
				proto.WriteString(", ")
			}
			paramType := "int64_t"
			if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
				if ctype := fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], false); ctype != "" {
					paramType = ctype
				}
			}
			proto.WriteString(paramType)
			proto.WriteByte(' ')
			proto.WriteString(param)
		}
		for i := range captures {
			if i > 0 || len(stmt.Params) > 0 {
				proto.WriteString(", ")
			}
			capType := "int64_t"
			if info := fg.codegen.nestedFuncCaptures[stmt.Name]; i < len(info.Types) && info.Types[i] != "" {
				if ctype := fg.codegen.typeGenerator.convertType(info.Types[i], false); ctype != "" {
					capType = ctype
				}
			}
			proto.WriteString(capType)
			proto.WriteString("*")
		}
		proto.WriteString(");\n")
		fg.codegen.nestedFuncPrototypes.WriteString(proto.String())
	}
	if len(captures) > 0 {
		info := nestedFuncInfo{Captures: captures, Types: make([]string, 0, len(captures))}
		// 记录捕获变量的 Kaula 类型（_cap_N 指针的基类型）
		for _, name := range captures {
			sym := fg.codegen.lookupOuterSymbol(name)
			if sym != nil {
				info.Types = append(info.Types, sym.Type)
			} else {
				info.Types = append(info.Types, "")
			}
		}
		if fg.codegen.nestedFuncCaptures == nil {
			fg.codegen.nestedFuncCaptures = make(map[string]nestedFuncInfo)
		}
		fg.codegen.nestedFuncCaptures[stmt.Name] = info
		stmt.Captures = captures
	}
	// 预注册函数体内的其他嵌套函数：确保调用点在定义语句之前时
	// 也能找到捕获信息（生成调用实参）
	fg.codegen.preRegisterNestedFuncs(stmt.Body)

	if stmt.Name == "main" {
		return fg.generateMainFunction(stmt)
	}

	var builder strings.Builder
	builder.Grow(1024)

	// 生成函数属性前缀
	attrPrefix := generateFuncAttributes(stmt)
	if attrPrefix != "" {
		builder.WriteString(attrPrefix)
		builder.WriteByte(' ')
	}

	// export 修饰符：C 级导出声明
	if stmt.IsExported {
		builder.WriteString("KAULA_EXPORT ")
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
	// 嵌套函数：追加捕获参数（按引用：指针）
	for i, capName := range captures {
		if len(stmt.Params) > 0 || i > 0 {
			builder.WriteString(", ")
		}
		capType := "int64_t"
		if info := fg.codegen.nestedFuncCaptures[stmt.Name]; i < len(info.Types) && info.Types[i] != "" {
			if ctype := fg.codegen.typeGenerator.convertType(info.Types[i], false); ctype != "" {
				capType = ctype
			}
		}
		builder.WriteString(fmt.Sprintf("%s* _cap_%d", capType, i))
		_ = capName
	}
	if len(stmt.Params) == 0 && len(captures) == 0 {
		builder.WriteString("void")
	}
	builder.WriteString(") {\n")
	fg.codegen.indent++

	// 设置捕获重写上下文（函数体内的 identifier 重写为 (*_cap_N)）
	prevCaptures, prevCaptureSet := fg.codegen.currentCaptures, fg.codegen.currentCaptureSet
	if len(captures) > 0 {
		set := make(map[string]int, len(captures))
		for i, name := range captures {
			set[name] = i
		}
		fg.codegen.currentCaptures = captures
		fg.codegen.currentCaptureSet = set
	}
	defer func() {
		fg.codegen.currentCaptures = prevCaptures
		fg.codegen.currentCaptureSet = prevCaptureSet
	}()

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
		prevInFuncBody := fg.codegen.inFunctionBody
		fg.codegen.inFunctionBody = true
		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			bodyBuilder.WriteString(fg.codegen.indentString())
			bodyBuilder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
		fg.codegen.inFunctionBody = prevInFuncBody
		fg.codegen.indent--

		if useKMM {
			fg.codegen.ExitKMMScope()
		}

		bodyCode := bodyBuilder.String()

		if useKMM {
			// 修复 #20：删除批量 bump 优化路径（_batch_ptr 空转问题）
			// per-thread heap 已实现单次 CAS 批量获取的效果，无需额外优化
			// 修复 #22：使用 SCOPE_START/END，scope_pop 在 do-while(0) 后执行
			// 提前退出（return/break/continue）的修复在 stmtgen.go 中处理
			builder.WriteString(bodyIndent)
			builder.WriteString("KMM_V4_SCOPE_START {\n")
			builder.WriteString(bodyCode)
			builder.WriteString(bodyIndent)
			builder.WriteString("} KMM_V4_SCOPE_END;\n")
		} else {
			builder.WriteString(bodyCode)
		}
	} else {
		indent := fg.codegen.indentString()
		prevInFuncBody := fg.codegen.inFunctionBody
		fg.codegen.inFunctionBody = true
		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			builder.WriteString(indent)
			builder.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
		fg.codegen.inFunctionBody = prevInFuncBody
	}

	if !hasReturnStatement(stmt.Body) && stmt.ReturnType != "" && !isVoidType(stmt.ReturnType) {
		builder.WriteString(fg.codegen.indentString())
		builder.WriteString("return 0;\n")
	}
	fg.codegen.indent--
	builder.WriteString("}\n")

	// 提升嵌套函数定义到文件级（在所在外层函数之后输出）
	// 调用点先于定义出现的问题由文件头部的原型声明（nestedFuncPrototypes）解决
	for _, nestedCode := range fg.codegen.pendingNestedFuncs {
		builder.WriteString(nestedCode)
	}
	fg.codegen.pendingNestedFuncs = nil

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

// generateFuncAttributes 将函数属性转换为 C 的 __attribute__ 前缀
// #[inline] → __attribute__((always_inline))
// #[naked] → __attribute__((naked))
// #[section(".text.boot")] → __attribute__((section(".text.boot")))
// #[weak] → __attribute__((weak))
// #[deprecated] → __attribute__((deprecated))
func generateFuncAttributes(stmt *ast.FunctionStatement) string {
	var parts []string

	// 兼容旧字段
	if stmt.Inline {
		parts = append(parts, "__attribute__((always_inline))")
	}

	// 从统一属性列表中读取新属性
	for _, attr := range stmt.Attributes {
		switch attr.Name {
		case "inline":
			// 已通过旧字段处理，跳过
		case "asm":
			// asm 是语义标记，由 generateAsmFunction 单独处理，不映射到 C __attribute__
		case "no_kmm", "sor", "prefix", "tree", "root":
			// 这些是语义注解，不映射到 C __attribute__，跳过
		case "naked":
			parts = append(parts, "__attribute__((naked))")
		case "section":
			if len(attr.Args) > 0 {
				parts = append(parts, fmt.Sprintf("__attribute__((section(%s)))", attr.Args[0]))
			}
		case "weak":
			parts = append(parts, "__attribute__((weak))")
		case "deprecated":
			if len(attr.Args) > 0 {
				parts = append(parts, fmt.Sprintf("__attribute__((deprecated(%s)))", attr.Args[0]))
			} else {
				parts = append(parts, "__attribute__((deprecated))")
			}
		}
	}

	return strings.Join(parts, " ")
}

func (fg *FunctionGenerator) generateMainFunction(stmt *ast.FunctionStatement) string {
	var code strings.Builder
	code.Grow(2048)
	attrPrefix := generateFuncAttributes(stmt)
	if attrPrefix != "" {
		code.WriteString(attrPrefix)
		code.WriteByte(' ')
	}

	// main 始终返回 int，设置返回类型供 return 语句生成使用
	fg.codegen.currentFunctionReturnType = "int"

	funcName := "main"
	if fg.codegen.config != nil && fg.codegen.config.Freestanding {
		funcName = "kaula_main"
	}
	code.WriteString("int " + funcName + "() {\n")
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

	// 注入动态对象初始化代码（在函数体生成后，确保 AddPreludeInit 已被调用）
	var finalBody strings.Builder
	finalBody.Grow(len(bodyCode) + 256)
	if fg.codegen.preludeInitCode.Len() > 0 {
		finalBody.WriteString(fg.codegen.indentString())
		finalBody.WriteString("// --- dynobj init ---\n")
		finalBody.WriteString(fg.codegen.indentString())
		finalBody.WriteString(fg.codegen.preludeInitCode.String())
		finalBody.WriteString(fg.codegen.indentString())
		finalBody.WriteString("// --- end init ---\n")
	}
	finalBody.WriteString(bodyCode)

	// 重置 preludeInitCode 避免重复注入
	fg.codegen.preludeInitCode.Reset()

	if useKMM {
		code.WriteString(fg.codegen.indentString())
		code.WriteString("KMM_V4_SCOPE_START {\n")
		code.WriteString(finalBody.String())
		code.WriteString(fg.codegen.indentString())
		code.WriteString("} KMM_V4_SCOPE_END;\n")
	} else {
		code.WriteString(finalBody.String())
	}

	code.WriteString(fg.codegen.indentString())
	code.WriteString("return 0;\n")
	fg.codegen.indent--
	code.WriteString("}\n")

	fg.codegen.ExitScope()
	fg.codegen.currentFunctionReturnType = ""
	return code.String()
}

func (fg *FunctionGenerator) mapReturnType(returnType string) string {
	if returnType == "" {
		return "void "
	}
	return tgReturnTypeToC(fg.codegen.typeGenerator, returnType) + " "
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

	// 优化策略（激进触发）：
	// 1. 单个 malloc 且大小已知 -> 直接 bump + offset_restore (零 scope 栈开销)
	// 2. 多个 malloc 且大小都已知 -> 批量 bump + offset_restore (单次 bump)
	canBatch = len(mallocs) >= 1 && allKnown
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

func (fg *FunctionGenerator) generateAsmFunction(stmt *ast.FunctionStatement) string {
	var builder strings.Builder
	builder.Grow(1024)

	// 生成函数属性前缀（naked, section, inline, weak 等）
	// #[asm] 本身是语义标记，不映射到 C __attribute__，
	// 但 #[naked]/#[section(...)]/#[inline] 等需要透传给 Clang
	// 这对裸机/系统级代码至关重要：asm 函数通常需要 naked 省略 prologue/epilogue，
	// 需要 section 控制放置位置（如 .text.boot）
	attrPrefix := generateFuncAttributes(stmt)
	if attrPrefix != "" {
		builder.WriteString(attrPrefix)
		builder.WriteByte(' ')
	}

	safeName := stmt.Name
	if safeName == "max" || safeName == "min" || safeName == "abs" {
		safeName = "kaula_" + safeName
	}

	returnType := fg.mapReturnType(stmt.ReturnType)
	builder.WriteString(returnType)
	builder.WriteString(safeName)
	builder.WriteString("(")
	for i, param := range stmt.Params {
		if i > 0 {
			builder.WriteString(", ")
		}
		paramType := "int64_t"
		if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
			paramType = fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], false)
		}
		builder.WriteString(fmt.Sprintf("%s %s", paramType, param))
	}
	if len(stmt.Params) == 0 {
		builder.WriteString("void")
	}
	builder.WriteString(") {\n")
	builder.WriteString(stmt.AsmBody)
	builder.WriteString("}\n")

	fg.codegen.ExitScope()
	return builder.String()
}
