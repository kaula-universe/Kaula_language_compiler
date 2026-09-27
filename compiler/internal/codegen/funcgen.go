package codegen

import (
	"fmt"
	"kaula/internal/ast"
	"sort"
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

	// 非 SOR 模式：递归扫描 body 中的变量类型（包括循环/if 内部）
	// Phase 2：循环内的 survivor 变量也提升到函数级 checkpoint
	return bodyHasSurvivorType(bodyStmts)
}

// bodyHasAllocationCall 扫描函数体是否包含 KMM 分配调用
// 纯计算函数（无 kmm_v4 分配 / yield / release / extract）可跳过 scope 管理
// 注意：hosted 模式下 std_malloc 是系统 malloc，不消耗 KMM 池，不触发 scope
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
		// std_malloc 仅在 freestanding 模式下消耗 KMM 池（被重写为 kmm_v4_alloc_auto）；
		// hosted 模式下它是系统 malloc，不触发 KMM scope 管理
		isFree := fg.codegen.IsFreestanding()
		if ident, ok := e.Function.(*ast.Identifier); ok {
			name := ident.Name
			if (name == "std_malloc" && isFree) || name == "kmm_v4_malloc" || name == "kmm_v4_alloc_auto" {
				return true
			}
		}
		// 检查 std.memory.std_malloc 等链式调用
		if member, ok := e.Function.(*ast.MemberAccessExpression); ok {
			if (member.Member == "std_malloc" && isFree) || member.Member == "kmm_v4_malloc" {
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

	// 优化：扫描函数体是否包含分配调用或 survivor 类型变量（递归扫描循环/if内部）
	// Phase 2：循环内的 survivor 变量也提升到函数级 checkpoint
	if !fg.bodyHasAllocationCall(bodyStmts) && !bodyHasSurvivorType(bodyStmts) {
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

	// 非 SOR 模式：递归扫描 body 中的变量类型（包括循环/if 内部）
	// Phase 2 优化：循环内的 survivor 变量也提升到函数级 checkpoint，
	// 避免每次循环迭代都重复 survivor checkpoint/rewind 开销
	if bodyHasSurvivorType(bodyStmts) {
		return true
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
// 注意：hosted 模式下 std_malloc 是系统 malloc，不在此列；
// freestanding 下它被重写为 kmm_v4_alloc_auto，由下方 "kmm_v4" 内容检测覆盖
func (fg *FunctionGenerator) needsKMMScopeNonSOR(bodyCode string) bool {
	if !strings.Contains(bodyCode, "kmm_v4") &&
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
	// 函数作用域边界隔离：return 路径的 per-object survivor 回收（
	// generateSurvivorFreeForReturn 遍历当前作用域到根的作用域链）必须停在
	// 当前函数的外层作用域，否则嵌套函数的 return 会回收外层函数的存活变量，
	// 造成 use-after-free。保存进入函数前的外层 scope，生成后恢复。
	prevStopScope := fg.codegen.survivorStopScope
	fg.codegen.survivorStopScope = fg.codegen.GetCurrentScope()
	fg.codegen.EnterScope("function_" + stmt.Name)

	// 根治 #1：SOR 决策按 (函数, 变量名) 解析——进入函数时切换决策上下文，
	// 退出时恢复。变量名只在词法作用域内唯一，跨函数同名是常态；
	// 不切换上下文会让形参命中调用方同名局部变量的决策。
	if adapter := fg.codegen.GetSORAdapter(); adapter != nil {
		prevSORFunc := adapter.CurrentFunc()
		adapter.SetCurrentFunc(stmt.Name)
		defer adapter.SetCurrentFunc(prevSORFunc)
	}

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
	// 先收集函数体内的嵌套函数名，用于 detectCaptures 区分函数调用与变量引用
	nestedFuncNames := collectNestedFuncNames(stmt.Body)
	directCaptures, directCalls := fg.codegen.detectCaptures(stmt, nestedFuncNames)
	// 预注册函数体内的其他嵌套函数：确保调用点在定义语句之前时
	// 也能找到捕获信息（生成调用实参）。预注册包含传递闭包。
	fg.codegen.preRegisterNestedFuncs(stmt.Body)
	// 当前函数的捕获传递闭包：若当前函数调用了体内嵌套函数 B，
	// 则当前函数也需要捕获 B 所需的外层变量，以便在调用 B 时转发捕获指针。
	captures := make([]string, len(directCaptures))
	copy(captures, directCaptures)
	for _, called := range directCalls {
		if info, ok := fg.codegen.nestedFuncCaptures[called]; ok {
			for _, cap := range info.Captures {
				if !containsStr(captures, cap) {
					captures = append(captures, cap)
				}
			}
		}
	}
	sort.Strings(captures)
	if fg.codegen.inFunctionBody {
		// 嵌套函数提升为文件级后，调用点（所在外层函数体）先于其定义出现，
		// 必须在文件头部追加原型声明（forward declaration），否则 clang 报隐式声明错误
		var proto strings.Builder
		proto.WriteString(fg.mapReturnType(stmt.ReturnType))
		proto.WriteByte(' ')
		proto.WriteString(fg.codegen.UserFuncCName(stmt.Name))
		proto.WriteByte('(')
		for i, param := range stmt.Params {
			if i > 0 {
				proto.WriteString(", ")
			}
			paramType := "int64_t"
			if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
				isNullable := false
				if i < len(stmt.ParamNullables) {
					isNullable = stmt.ParamNullables[i]
				}
				if ctype := fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], isNullable); ctype != "" {
					paramType = ctype
				}
			}
			// 函数指针类型参数：R (*)(args) name → R (*name)(args)
			if strings.Contains(paramType, "(*)") {
				proto.WriteString(formatCVarDecl(paramType, param))
			} else {
				proto.WriteString(paramType)
				proto.WriteByte(' ')
				proto.WriteString(param)
			}
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

	// 用户函数统一 K_ 前缀（避开 C 保留字/库符号）；main、extern、内置保持原名。
	// max/min/abs 若为用户定义则 K_max，否则重定向 kaula_max，均由 UserFuncCName 统一处理。
	// export 函数保持原名（FFI 友好，外部 C 代码直接用原名调用）。
	var safeName string
	if stmt.IsExported {
		safeName = stmt.Name
	} else {
		safeName = fg.codegen.UserFuncCName(stmt.Name)
	}

	if stmt.IsGeneric() {
		fg.codegen.ExitScope()
		return ""
	}

	// 设置当前函数返回类型供 return 语句生成使用
	// 嵌套函数生成会覆盖此值，必须保存外层值并在结束时恢复，
	// 否则外层函数的 return 语句会误判为 void 生成空 return。
	prevReturnType := fg.codegen.currentFunctionReturnType
	fg.codegen.currentFunctionReturnType = stmt.ReturnType

	// 嵌套函数作用域隔离：函数体生成期间 kmmScopeDepth 必须归零，
	// 否则嵌套函数会继承外层函数的活跃作用域深度，导致其 return 语句
	// 注入孤立的 kmm_v4_scope_pop()，运行期 KMM 作用域栈下溢直接 abort。
	// 与外层隔离：生成后恢复，保证外层后续语句的作用域深度不受影响。
	savedScopeDepth := fg.codegen.kmmScopeDepth
	savedEmitsPromote := fg.codegen.funcEmitsPromote
	savedHasCheckpoint := fg.codegen.funcHasKMMCheckpoint
	fg.codegen.kmmScopeDepth = 0
	fg.codegen.funcEmitsPromote = false
	fg.codegen.funcHasKMMCheckpoint = false

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
		isNullable := false
		if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
			if i < len(stmt.ParamNullables) {
				isNullable = stmt.ParamNullables[i]
			}
			paramType = fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], isNullable)
		}
		// 函数指针类型参数：R (*)(args) name → R (*name)(args)（C 语法要求参数名在 (*) 中间）
		if strings.Contains(paramType, "(*)") {
			builder.WriteString(formatCVarDecl(paramType, param))
		} else {
			builder.WriteString(fmt.Sprintf("%s %s", paramType, param))
		}
		fg.codegen.AddSymbol(param, stmt.ParamTypes[i], isNullable, "parameter", stmt.Pos.Line, stmt.Pos.Column)
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
	// 尾调用改写状态是"每函数"的：先取出外层（可能来自嵌套调用栈）的状态清空，
	// 生成完本函数后再恢复，避免内层函数误用外层的尾调用标签。
	prevTCO := fg.codegen.TakeTCO()
	// 尾调用改写方案（受作用域托管时才计算；诊断在函数体生成后据此判定）
	plan := tcoPlan{}
	// 静态分析：在函数体生成前运行全部6个pass
	analysisResult := fg.codegen.RunStaticAnalysis(stmt.Name, stmt.Body, stmt.Params, stmt.ParamTypes)
	// 将分析结果暴露给语句生成器（liveness 提前释放/回收路径的别名感知）
	prevFuncAnalysis := fg.codegen.currentFuncAnalysis
	fg.codegen.currentFuncAnalysis = analysisResult
	defer func() {
		fg.codegen.currentFuncAnalysis = prevFuncAnalysis
	}()
	if shouldUseKMM {
		// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
		// 跨函数分析：结合函数签名判断是否需要 KMM
		useKMM := fg.shouldUseKMMScopeForFunc(stmt.Name, stmt.Body)
		if useKMM {
			fg.codegen.EnterKMMScope()
			// Phase 2：在生成函数体之前设置函数级 checkpoint 标志，
			// 使循环 codegen 能检测到并降级为 offset 回收
			fg.codegen.funcHasKMMCheckpoint = true
			// 尾调用改写（TCO）：受作用域托管的自递归函数，把 `return f(...)`
			// 变成"回收本层 → 实参写回形参 → 跳回循环顶部"，复用同一 C 栈帧，
			// 使递归深度不再受 KMM_V4_MAX_SCOPE_DEPTH / C 栈限制。
			// 嵌套函数（含捕获）不参与：形参回写无法覆盖捕获指针参数。
			plan = fg.tcoTailCandidates(stmt)
			if len(captures) == 0 && len(plan.sites) > 0 {
				fg.codegen.SetTCO("__kaula_tco_"+fg.codegen.UserFuncCName(stmt.Name), stmt.Params, plan.sites)
				defer fg.codegen.RestoreTCO(prevTCO)
			}
		}
		// 方案 C：重置 promote 标志，body 生成期间若发射 promote_p 则置 true
		fg.codegen.funcEmitsPromote = false

		var bodyBuilder strings.Builder
		bodyIndent := fg.codegen.indentString()
		fg.codegen.indent++
		prevInFuncBody := fg.codegen.inFunctionBody
		fg.codegen.inFunctionBody = true
		// 注入静态分析警告
		bodyBuilder.WriteString(fg.codegen.ReportAnalysisWarnings(stmt.Name, analysisResult))
		// 用户显式释放的指针：所有权已转移，回收路径统一跳过（否则 double free）
		fg.codegen.ResetUserFreedVars()
		for name := range fg.codegen.statementGenerator.CollectUserFreedVars(stmt.Body) {
			fg.codegen.MarkUserFreed(name)
		}
		// 结构化所有权基础层：使用带 liveness 的语句生成，实现提前释放（能力③）
		bodyBuilder.WriteString(fg.codegen.statementGenerator.generateStmtsWithLiveness(stmt.Body, nil))
		// 方案 A：per-object survivor 回收（在 KMM_V4_SCOPE_END 前发射）
		// 若末尾是 return，return 中已通过 generateSurvivorFreeForReturn 释放，此处为死代码，跳过
		if !lastStmtIsReturn(stmt.Body) {
			bodyBuilder.WriteString(fg.codegen.statementGenerator.generateSurvivorFreeForScope())
		}
		fg.codegen.inFunctionBody = prevInFuncBody
		fg.codegen.indent--

		if useKMM {
			fg.codegen.ExitKMMScope()
		}

		bodyCode := bodyBuilder.String()

		// 诊断：递归 + 每层 KMM 作用域 → 嵌套深度受 KMM_V4_MAX_SCOPE_DEPTH
		// 约束（默认 1024 层），超出即运行期 FATAL abort。
		// 判定在函数体生成之后：需要知道实际改写了多少个尾调用点（实参引用本帧
		// 会被回收的对象时会在生成期退回真实调用），以及是否处于互递归环
		// （互递归每层同样 push 作用域，深度一样受限）。
		if plan.selfCalls > 0 || fg.inRecursionCycle(stmt.Name) {
			tco := fg.codegen.TcoState()
			if tco == nil || tco.converted < plan.selfCalls {
				fg.codegen.AddRecursionScopeNote(stmt.Name)
			}
		}

		if useKMM {
			// 修复 #20：删除批量 bump 优化路径（_batch_ptr 空转问题）
			// per-thread heap 已实现单次 CAS 批量获取的效果，无需额外优化
			// 修复 #22：使用 SCOPE_START/END，scope_pop 在 do-while(0) 后执行
			// 提前退出（return/break/continue）的修复在 stmtgen.go 中处理
			// 方案 C：函数入口 checkpoint，出口 rewind（仅对不向外 promote 的函数）
			fg.codegen.funcHasKMMCheckpoint = true
			// 尾调用改写：把"每轮一帧"变成"复用同一帧"——作用域包裹放进
			// for(;;) 循环，尾调用点回收本层后 goto 回到循环顶部重新 push。
			// 没有尾调用落到循环底部 → break 退出（与改写前的顺序语义一致）。
			tco := fg.codegen.TcoState()
			if tco != nil {
				builder.WriteString(bodyIndent)
				builder.WriteString(tco.label + ": /* tco: 每轮复用同一栈帧 */\n")
				builder.WriteString(bodyIndent)
				builder.WriteString("for (;;) {\n")
			}
			builder.WriteString(bodyIndent + "    ")
			builder.WriteString("kmm_survivor_checkpoint_t __surv_cp = kmm_v4_survivor_checkpoint();\n")
			builder.WriteString(bodyIndent + "    ")
			// 命名作用域：作用域栈溢出（递归深度超过 KMM_V4_MAX_SCOPE_DEPTH）时
			// 运行期能打印出是哪个函数每层都在分配
			builder.WriteString(fg.kmmScopeStartNamed(stmt.Name))
			builder.WriteString(bodyCode)
			builder.WriteString(bodyIndent + "    ")
			builder.WriteString("} KMM_V4_SCOPE_END;\n")
			if !fg.codegen.funcEmitsPromote {
				builder.WriteString(bodyIndent + "    ")
				builder.WriteString("kmm_v4_survivor_rewind(__surv_cp); /* survivor: reclaim */\n")
			}
			if tco != nil {
				builder.WriteString(bodyIndent + "    break;\n")
				builder.WriteString(bodyIndent + "}\n")
			}
		} else {
		fg.codegen.funcHasKMMCheckpoint = false
		builder.WriteString(bodyCode)
	}
} else {
		prevInFuncBody := fg.codegen.inFunctionBody
		fg.codegen.inFunctionBody = true
		// 注入静态分析警告
		builder.WriteString(fg.codegen.ReportAnalysisWarnings(stmt.Name, analysisResult))
		// 结构化所有权基础层：使用带 liveness 的语句生成
		builder.WriteString(fg.codegen.statementGenerator.generateStmtsWithLiveness(stmt.Body, nil))
		fg.codegen.inFunctionBody = prevInFuncBody
	}

	// DEBUG: 打印函数体语句类型（已移除）
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
	fg.codegen.currentFunctionReturnType = prevReturnType
	// 恢复外层函数的 kmmScopeDepth 等 per-function 状态（嵌套函数隔离的对称恢复）
	fg.codegen.kmmScopeDepth = savedScopeDepth
	fg.codegen.funcEmitsPromote = savedEmitsPromote
	fg.codegen.funcHasKMMCheckpoint = savedHasCheckpoint
	// 恢复外层函数的 survivor 回收边界
	fg.codegen.survivorStopScope = prevStopScope
	return builder.String()
}

func (fg *FunctionGenerator) generatePrefixFunction(stmt *ast.FunctionStatement) string {
	var code strings.Builder
	code.Grow(1024)
	code.WriteString("// Prefix function: AST generation for cross-file reuse\n")
	fmt.Fprintf(&code, "int64_t %s", fg.codegen.UserFuncCName(stmt.Name))

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
	fmt.Fprintf(&code, "int64_t %s", fg.codegen.UserFuncCName(stmt.Name))

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
	fmt.Fprintf(&code, "int64_t %s", fg.codegen.UserFuncCName(stmt.Name))

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

// lastStmtIsReturn 检查语句列表的最后一条是否是 return。
// 方案 A：若末尾是 return，则作用域末尾的 generateSurvivorFreeForScope 是死代码
//（return 中已通过 generateSurvivorFreeForReturn 遍历作用域链释放），应跳过。
func lastStmtIsReturn(stmts []ast.Statement) bool {
	if len(stmts) == 0 {
		return false
	}
	last := stmts[len(stmts)-1]
	if last == nil {
		return false
	}
	_, ok := last.(*ast.ReturnStatement)
	return ok
}

// lastStmtIsControlFlow 检查语句列表的最后一条是否是控制流语句（return/break/continue）。
// 用于 match 分支：若末尾已是控制流语句，则后面的 break 是死代码，应跳过。
func lastStmtIsControlFlow(stmts []ast.Statement) bool {
	if len(stmts) == 0 {
		return false
	}
	last := stmts[len(stmts)-1]
	if last == nil {
		return false
	}
	switch last.(type) {
	case *ast.ReturnStatement, *ast.BreakStatement, *ast.ContinueStatement:
		return true
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

	// 静态分析：与普通函数路径保持一致，暴露别名组给回收路径，
	// 防止 main 中别名变量被重复释放（double-free）
	mainAnalysis := fg.codegen.RunStaticAnalysis(stmt.Name, stmt.Body, stmt.Params, stmt.ParamTypes)
	prevFuncAnalysis := fg.codegen.currentFuncAnalysis
	fg.codegen.currentFuncAnalysis = mainAnalysis
	defer func() {
		fg.codegen.currentFuncAnalysis = prevFuncAnalysis
	}()

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
		// Phase 2：在生成函数体之前设置函数级 checkpoint 标志，
		// 使循环 codegen 能检测到并降级为 offset 回收
		fg.codegen.funcHasKMMCheckpoint = true
	}
	// 方案 C：重置 promote 标志
	fg.codegen.funcEmitsPromote = false

	var bodyBuilder strings.Builder
	// 用户显式释放的指针：作用域末尾 per-object 回收必须跳过（否则 double free）
	fg.codegen.ResetUserFreedVars()
	for name := range fg.codegen.statementGenerator.CollectUserFreedVars(stmt.Body) {
		fg.codegen.MarkUserFreed(name)
	}
	// main 的函数体同样处于"函数体内"：函数体内的嵌套函数必须提升为文件级
	// 定义（C 不允许函数内嵌定义），否则生成 "function definition is not
	// allowed here" + 调用点隐式声明错误（历史上只在普通函数路径设置了该标志）
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
	// 方案 A：per-object survivor 回收（在 KMM_V4_SCOPE_END 前发射）
	// 若末尾是 return，return 中已通过 generateSurvivorFreeForReturn 释放，此处为死代码，跳过
	if !lastStmtIsReturn(stmt.Body) {
		bodyBuilder.WriteString(fg.codegen.statementGenerator.generateSurvivorFreeForScope())
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
		fg.codegen.funcHasKMMCheckpoint = true
		code.WriteString(fg.codegen.indentString())
		code.WriteString("kmm_survivor_checkpoint_t __surv_cp = kmm_v4_survivor_checkpoint();\n")
		code.WriteString(fg.codegen.indentString())
		code.WriteString(fg.kmmScopeStartNamed(stmt.Name))
		code.WriteString(finalBody.String())
		code.WriteString(fg.codegen.indentString())
		code.WriteString("} KMM_V4_SCOPE_END;\n")
		if !fg.codegen.funcEmitsPromote {
			code.WriteString(fg.codegen.indentString())
			code.WriteString("kmm_v4_survivor_rewind(__surv_cp); /* survivor: reclaim */\n")
		}
	} else {
		fg.codegen.funcHasKMMCheckpoint = false
		code.WriteString(finalBody.String())
	}

	// 只有当用户没有显式写 return 时才自动添加 return 0;
	if !hasReturnStatement(stmt.Body) {
		code.WriteString(fg.codegen.indentString())
		code.WriteString("return 0;\n")
	}
	fg.codegen.indent--
	code.WriteString("}\n")

	// 提升后的嵌套函数定义排在本函数定义**闭合之后**输出
	// （C 不允许函数内嵌定义；调用点先于定义出现由文件头部的原型声明解决）
	for _, nestedCode := range fg.codegen.pendingNestedFuncs {
		code.WriteString(nestedCode)
	}
	fg.codegen.pendingNestedFuncs = nil

	fg.codegen.ExitScope()
	fg.codegen.currentFunctionReturnType = ""
	return code.String()
}

// kmmScopeStartNamed 生成函数级的 KMM 作用域起始行（带归属标记）。
// 归属名只在 KMM 作用域栈溢出（KMM_V4_MAX_SCOPE_DEPTH）时打印，用于定位
// "递归且每层都分配"的函数；TLS 只多一个指针，不影响每帧布局。
func (fg *FunctionGenerator) kmmScopeStartNamed(kaulaFuncName string) string {
	return fmt.Sprintf("KMM_V4_SCOPE_NAMED(\"%s\") {\n", fg.codegen.UserFuncCName(kaulaFuncName))
}

// inRecursionCycle 判断函数是否处于递归环中：直接自递归，或经由其他函数
// 互相递归。互递归的每一层同样会 push KMM 作用域，深度一样受
// KMM_V4_MAX_SCOPE_DEPTH 约束，因此"递归 + 每层分配"诊断必须覆盖它
// （尾调用改写只处理直接自递归，不处理互递归）。
func (fg *FunctionGenerator) inRecursionCycle(funcName string) bool {
	prog := fg.codegen.getProgram()
	if prog == nil {
		return false
	}
	self := prog.FindFunction(funcName)
	if self == nil {
		return false
	}
	visited := map[string]bool{funcName: true}
	var reachesTarget func(target string, fn *ast.FunctionStatement) bool
	reachesTarget = func(target string, fn *ast.FunctionStatement) bool {
		for _, callee := range collectCalleeNames(fn) {
			if callee == target {
				return true
			}
			if visited[callee] {
				continue
			}
			visited[callee] = true
			if next := prog.FindFunction(callee); next != nil {
				if reachesTarget(target, next) {
					return true
				}
			}
		}
		return false
	}
	return reachesTarget(funcName, self)
}

// collectCalleeNames 收集函数体（不含嵌套函数定义）里调用的函数名
func collectCalleeNames(fn *ast.FunctionStatement) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(e ast.Expression) {
		if call, ok := e.(*ast.CallExpression); ok {
			if id, ok := call.Function.(*ast.Identifier); ok && id.Name != "" && !seen[id.Name] {
				seen[id.Name] = true
				out = append(out, id.Name)
			}
		}
	}
	var walkExpr func(e ast.Expression)
	var walkBody func(list []ast.Statement)
	walkExpr = func(e ast.Expression) {
		switch x := e.(type) {
		case *ast.CallExpression:
			add(x)
			for _, a := range x.Args {
				walkExpr(a)
			}
		case *ast.TypeCastExpression:
			walkExpr(x.Expression)
		case *ast.MemberAccessExpression:
			walkExpr(x.Object)
		case *ast.IndexExpression:
			walkExpr(x.Object)
			walkExpr(x.Index)
		case *ast.BinaryExpression:
			walkExpr(x.Left)
			walkExpr(x.Right)
		case *ast.UnaryExpression:
			walkExpr(x.Right)
		case *ast.ParenExpression:
			walkExpr(x.Inner)
		}
	}
	walkBody = func(list []ast.Statement) {
		for _, s := range list {
			switch x := s.(type) {
			case *ast.VariableDeclaration:
				walkExpr(x.Value)
			case *ast.ExpressionStatement:
				walkExpr(x.Expression)
			case *ast.ReturnStatement:
				walkExpr(x.Value)
			case *ast.IfStatement:
				walkExpr(x.Condition)
				walkBody(x.Body)
				if x.Else != nil {
					walkBody(x.Else)
				}
			case *ast.WhileStatement:
				walkExpr(x.Condition)
				walkBody(x.Body)
			case *ast.ForStatement:
				walkBody(x.Body)
			case *ast.ForInStatement:
				walkBody(x.Body)
			case *ast.BlockStatement:
				walkBody(x.Statements)
			}
		}
	}
	walkBody(fn.Body)
	return out
}

// voidFuncFor 判断函数是否无返回值（未声明返回类型或显式 void）
func voidFuncFor(stmt *ast.FunctionStatement) bool {
	return stmt.ReturnType == "" || isVoidType(stmt.ReturnType)
}

// tcoPlan 是一个函数的尾调用改写方案：可改写的 self-call 站点 + 函数体内
// self-call 总数（用于判断"是否全部改写完"——只要还剩一个真实递归调用，递归
// 深度就仍然受 KMM_V4_MAX_SCOPE_DEPTH 约束，诊断告警照旧）。
type tcoPlan struct {
	sites     map[*ast.CallExpression]bool
	selfCalls int
}

// tcoTailCandidates 找出可以改写成尾跳转的 self-call（仅"受 KMM 作用域托管的
// 自递归函数"才成立：这类函数每层都要 scope_pop，C 后端无法做 TCO）。
//
// 判定条件：
//   - 位于 `return f(...)` 的返回值位置，实参个数与形参一致；
//   - 实参是**无副作用的简单表达式**（标识符/字面量/一元/二元/括号/类型转换）。
//     改写后实参在本层回收**之后**才求值写回形参，所以不能含函数调用；
//     下标/成员访问同样排除（可能读已被本层回收的对象）。
//
// 含指针/字符串局部变量的实参留给生成期再校验（见 generateReturnStatement 的
// 尾调用分支）：那时符号表已建立，能识别本帧会被 survivor_free 的对象。
func (fg *FunctionGenerator) tcoTailCandidates(stmt *ast.FunctionStatement) tcoPlan {
	plan := tcoPlan{sites: make(map[*ast.CallExpression]bool)}
	if len(stmt.Params) == 0 {
		return plan
	}
	// 无返回类型（""）与显式 void 都算无返回值——与 generateReturnStatement
	// 里的 isVoid 判定保持一致（isVoidType 只认显式 "void"/"Void"）
	voidFunc := voidFuncFor(stmt)
	var simple func(e ast.Expression) bool
	simple = func(e ast.Expression) bool {
		switch x := e.(type) {
		case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral,
			*ast.BooleanLiteral, *ast.CharLiteral:
			return true
		case *ast.UnaryExpression:
			return simple(x.Right)
		case *ast.ParenExpression:
			return simple(x.Inner)
		case *ast.TypeCastExpression:
			return simple(x.Expression)
		case *ast.BinaryExpression:
			return simple(x.Left) && simple(x.Right)
		}
		return false
	}
	var walkExpr func(e ast.Expression)
	var walkBody func(list []ast.Statement)
	walkExpr = func(e ast.Expression) {
		// 遍历嵌套调用：统计 self-call 总数（尾调用点本身在 Return 分支处理）
		switch x := e.(type) {
		case *ast.CallExpression:
			if id, ok := x.Function.(*ast.Identifier); ok && id.Name == stmt.Name {
				plan.selfCalls++
			}
			for _, a := range x.Args {
				walkExpr(a)
			}
		case *ast.TypeCastExpression:
			walkExpr(x.Expression)
		case *ast.MemberAccessExpression:
			walkExpr(x.Object)
		case *ast.IndexExpression:
			walkExpr(x.Object)
			walkExpr(x.Index)
		case *ast.BinaryExpression:
			walkExpr(x.Left)
			walkExpr(x.Right)
		case *ast.UnaryExpression:
			walkExpr(x.Right)
		case *ast.ParenExpression:
			walkExpr(x.Inner)
		}
	}
	walkBody = func(list []ast.Statement) {
		for _, s := range list {
			switch x := s.(type) {
			case *ast.VariableDeclaration:
				walkExpr(x.Value)
			case *ast.ExpressionStatement:
				// void 函数的 `f(...)` 语句形式就是尾调用（无返回值可求值）。
				// 非 void 函数不处理：语句形式的调用会丢弃返回值，后续代码
				// 仍会执行，不构成尾位置。
				tailCall := false
				if voidFunc {
					if call, ok := x.Expression.(*ast.CallExpression); ok {
						if id, ok := call.Function.(*ast.Identifier); ok &&
							id.Name == stmt.Name && len(call.Args) == len(stmt.Params) {
							allSimple := true
							for _, a := range call.Args {
								if !simple(a) {
									allSimple = false
									break
								}
							}
							if allSimple {
								plan.selfCalls++
								plan.sites[call] = true
								tailCall = true
							}
						}
					}
				}
				if !tailCall {
					walkExpr(x.Expression)
				}
			case *ast.ReturnStatement:
				// 尾调用点：self-call 计数与改写判定都在这里完成，
				// 因此不能再交给 walkExpr（否则同一个调用会被重复计数）
				if call, ok := x.Value.(*ast.CallExpression); ok {
					if id, ok := call.Function.(*ast.Identifier); ok && id.Name == stmt.Name {
						plan.selfCalls++
						if len(call.Args) == len(stmt.Params) {
							allSimple := true
							for _, a := range call.Args {
								if !simple(a) {
									allSimple = false
									break
								}
							}
							if allSimple {
								plan.sites[call] = true
							}
						}
					}
					for _, a := range call.Args {
						walkExpr(a)
					}
				} else {
					walkExpr(x.Value)
				}
			case *ast.IfStatement:
				walkExpr(x.Condition)
				walkBody(x.Body)
				if x.Else != nil {
					walkBody(x.Else)
				}
			case *ast.WhileStatement:
				walkExpr(x.Condition)
				walkBody(x.Body)
			case *ast.ForStatement:
				walkBody(x.Body)
			case *ast.ForInStatement:
				walkBody(x.Body)
			case *ast.BlockStatement:
				walkBody(x.Statements)
			}
		}
	}
	walkBody(stmt.Body)
	return plan
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

// isMallocCall 检查调用是否是 KMM 分配函数（std_malloc 仅在 freestanding 下属于 KMM 分配）
func (fg *FunctionGenerator) isMallocCall(call *ast.CallExpression) bool {
	if call == nil || call.Function == nil {
		return false
	}
	isFree := fg.codegen.IsFreestanding()
	switch fn := call.Function.(type) {
	case *ast.Identifier:
		return (fn.Name == "std_malloc" && isFree) || fn.Name == "kmm_v4_malloc" || fn.Name == "kmm_v4_alloc_auto"
	case *ast.MemberAccessExpression:
		return (fn.Member == "std_malloc" && isFree) || fn.Member == "kmm_v4_malloc"
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

	// 用户函数统一 K_ 前缀（避开 C 保留字/库符号）；main、extern、内置保持原名。
	// max/min/abs 若为用户定义则 K_max，否则重定向 kaula_max，均由 UserFuncCName 统一处理。
	// export 函数保持原名（FFI 友好，外部 C 代码直接用原名调用）。
	var safeName string
	if stmt.IsExported {
		safeName = stmt.Name
	} else {
		safeName = fg.codegen.UserFuncCName(stmt.Name)
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
			isNullable := false
			if i < len(stmt.ParamNullables) {
				isNullable = stmt.ParamNullables[i]
			}
			paramType = fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], isNullable)
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
