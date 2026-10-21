package codegen

import (
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/codegen/conc"
	"os"
	"sort"
	"strings"
)

type FunctionGenerator struct {
	codegen *CodeGenerator

	// lastIoVar 是最近一次 submit 形态生成的 IO 请求变量名，
	// concSuspendReturn 用它填 KaulaResumeResult.io_request
	lastIoVar string
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
	// conc fn: 并发函数，生成 Frame 结构体 + resume 函数
	if stmt.IsConcurrent {
		return fg.generateConcurrentFunction(stmt)
	}

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
		// 同名前缀的多条规则（多规则分派）各自展开，不生成同名 C 函数：
		// 签名不同会在 clang 阶段撞上 conflicting types，而规则本来就不该
		// 被当普通函数调用。详见 CodeGenerator.skipPrefixRuleCFunc。
		if fg.codegen.skipPrefixRuleCFunc(stmt) {
			return ""
		}
		return fg.generatePrefixFunction(stmt)
	}

	if annotation == ast.TreeAnnotationTree {
		if fg.codegen.skipPrefixRuleCFunc(stmt) {
			return ""
		}
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
			// 数组类型参数：T[N] name → T name[N]（C 要求声明符在后）
			proto.WriteString(formatCVarDecl(paramType, param))
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
	savedEmitsInternalPromote := fg.codegen.funcEmitsInternalPromote
	savedHasCheckpoint := fg.codegen.funcHasKMMCheckpoint
	// mark 回收模式同样是 per-function 的：本函数体内若定义嵌套函数，嵌套函数
	// 的生成会改写这些状态，必须在返回前恢复，否则外层后续 KMM 区间的
	// start/end 会错位（mark save 配 scope pop → 生成不可编译的 C）。
	savedUseMark := fg.codegen.kmmUseMark
	savedMarkStack := fg.codegen.kmmMarkStack
	savedFnMark := fg.codegen.kmmFnMark
	fg.codegen.kmmScopeDepth = 0
	fg.codegen.funcEmitsPromote = false
	fg.codegen.funcEmitsInternalPromote = false
	fg.codegen.funcHasKMMCheckpoint = false
	// 形参/幸存副本持有者的登记是 per-function 的：必须在登记本函数形参之前清空，
	// 否则上一个函数的形参名会残留，导致本函数的同名局部变量被误判为"非函数局部"
	// （内部 promote 被错判为向外 → 白白放弃 survivor_rewind → survivor 段泄漏）。
	fg.codegen.ResetFunctionLocalState()
	// 方案 A'：函数级预扫描「安全局部容器」集合。
	// 纯词法 AST 遍历，必须在生成函数体之前完成——stmtgen 在处理写穿
	// （obj.field = p / arr[i] = p）与循环体 mark_restore 时会查询该集合。
	// 详见 safe_container_scan.go 与 docs/perf/P0-survivor-leak.md §6。
	scan := ScanSafeLocalContainers(stmt.Body, stmt.Params)
	fg.codegen.SetSafeLocalContainers(scan.Safe)
	fg.codegen.SetWrittenThroughContainers(scan.WrittenThrough)
	fg.codegen.SetChainLinkedVars(scan.ChainLinked)
	fg.codegen.SetChainEscapingVars(scan.ChainEscaping)
	fg.codegen.SetEscapedVars(scan.Escaped)
	// 进入时必须重置，避免嵌套函数继承外层模式
	fg.codegen.SetKMMUseMark(false)

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
		// 数组类型参数：T[N] name → T name[N]（C 要求声明符在后）
		builder.WriteString(formatCVarDecl(paramType, param))
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
			// 递归环内函数改用 mark 回收：必须在函数体生成之前决定，
			// 因为函数体内所有块级/函数级 KMM 区间都要按同一模式发射。
			fg.codegen.SetKMMUseMark(fg.shouldUseKMMMarkForFunc(stmt))
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
		fg.codegen.funcEmitsInternalPromote = false

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
		// 只有该函数自身每层真的 push KMM 作用域（useKMM）才会消耗槽位；
		// 环上但不 push 的伙伴函数（如互递归里不分配的那一半）不受上限约束，
		// 对它们提示"加 #[no_kmm]"是无效建议，故以前会误报。
		if useKMM && (plan.selfCalls > 0 || fg.inRecursionCycle(stmt.Name)) {
			// mark 回收模式下递归不再消耗 scope 栈槽位，深度不受
			// KMM_V4_MAX_SCOPE_DEPTH 约束，该诊断已不适用——继续提示会误导
			// 用户去加 #[no_kmm] 或放宽上限（两者都是有害的建议）。
			if !fg.codegen.KMMUseMark() {
				tco := fg.codegen.TcoState()
				if tco == nil || tco.converted < plan.selfCalls {
					fg.codegen.AddRecursionScopeNote(stmt.Name)
				}
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
			builder.WriteString(fg.codegen.KMMStart(fg.codegen.UserFuncCName(stmt.Name)))
			builder.WriteString(bodyCode)
			builder.WriteString(bodyIndent + "    ")
			builder.WriteString(fg.codegen.KMMEnd())
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
	fg.codegen.funcEmitsInternalPromote = savedEmitsInternalPromote
	fg.codegen.funcHasKMMCheckpoint = savedHasCheckpoint
	// mark 回收模式的对称恢复（与上面的保存配对）
	fg.codegen.kmmUseMark = savedUseMark
	fg.codegen.kmmMarkStack = savedMarkStack
	fg.codegen.kmmFnMark = savedFnMark
	// 恢复外层函数的 survivor 回收边界
	fg.codegen.survivorStopScope = prevStopScope
	return builder.String()
}

// annotatedFuncSignature 生成 prefix/tree/root-tree 注解函数的 C 签名。
// 这三类函数此前硬编码 "int64_t f(int64_t arg)"，与 forwardDecls 按 Kaula 声明
// 生成的原型冲突：无返回值函数的原型是 "void K_f(void)"、带参原型的参数类型也按
// 声明转换，定义却一律 int64_t → clang 报 conflicting types，程序根本编不过。
// 这里复用普通函数的签名规则（mapReturnType + convertType），保证定义与前向声明一致。
func (fg *FunctionGenerator) annotatedFuncSignature(stmt *ast.FunctionStatement) string {
	var b strings.Builder
	b.WriteString(fg.mapReturnType(stmt.ReturnType))
	b.WriteString(fg.codegen.UserFuncCName(stmt.Name))
	b.WriteByte('(')
	for i, param := range stmt.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		paramType := "int64_t"
		isNullable := false
		if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
			if i < len(stmt.ParamNullables) {
				isNullable = stmt.ParamNullables[i]
			}
			if ctype := fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], isNullable); ctype != "" {
				paramType = ctype
			}
		}
		// 函数指针类型参数：R (*)(args) name → R (*name)(args)
		// 数组类型参数：T[N] name → T name[N]（C 要求声明符在后）
		b.WriteString(formatCVarDecl(paramType, param))
		fg.codegen.AddSymbol(param, stmt.ParamTypes[i], isNullable, "parameter", stmt.Pos.Line, stmt.Pos.Column)
	}
	if len(stmt.Params) == 0 {
		b.WriteString("void")
	}
	b.WriteByte(')')
	return b.String()
}

func (fg *FunctionGenerator) generatePrefixFunction(stmt *ast.FunctionStatement) string {
	var code strings.Builder
	code.Grow(1024)
	code.WriteString("// Prefix function: AST generation for cross-file reuse\n")
	code.WriteString(fg.annotatedFuncSignature(stmt))
	code.WriteString(" {\n")

	// return 语句按当前函数返回类型生成，注解函数同样需要设置并在结束时恢复
	prevReturnType := fg.codegen.currentFunctionReturnType
	fg.codegen.currentFunctionReturnType = stmt.ReturnType
	defer func() { fg.codegen.currentFunctionReturnType = prevReturnType }()

	fg.codegen.indent++

	if stmt.PrefixName != "" {
		code.WriteString(fg.codegen.indentString())
		fmt.Fprintf(&code, "prefix_enter(\"%s\");\n", stmt.PrefixName)
	}

	/* 无返回值的前缀函数不会被当成普通函数调用，这里生成的 C 函数体只是为了
	 * 让"前缀也能被同名调用"这条老写法继续可链接。
	 *
	 * 直译的条件很窄：体内只用到形参。此时 `$x` 换成形参 x 即可，空模板体
	 * （只有裸 `$p` 占位）也能编过。
	 *
	 * 一旦模板体自声明了 `$p`（`$limit = 10`）或用了其他宏专用形态，就没有
	 * 对应的 C 变量可以指代——此时**不生成这个函数体**，只留一个声明。编造
	 * 一个未声明的标识符会让整个程序在 clang 阶段失败，报错与真实原因毫无
	 * 关系；而模板本来就不会被当成普通函数调用，缺实现不影响任何合法调用。 */
	bodyParams := fg.codegen.prefixBodyParamNames(stmt)
	if bodyParams != nil {
		for _, bodyStmt := range stmt.Body {
			if bodyStmt == nil {
				continue
			}
			if _, ok := ast.BarePrefixParamName(bodyStmt); ok {
				continue // 裸 `$p` 是参数占位，不生成代码
			}
			bodyStmt = fg.codegen.rewritePrefixBodyStatement(bodyStmt, bodyParams)
			code.WriteString(fg.codegen.indentString())
			code.WriteString(fg.codegen.generateStatement(bodyStmt))
		}
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
	// 签名必须与 forwardDecls 生成的原型一致（此前硬编码 int64_t，与 void 原型冲突）
	code.WriteString(fg.annotatedFuncSignature(stmt))
	code.WriteString(" {\n")

	prevReturnType := fg.codegen.currentFunctionReturnType
	fg.codegen.currentFunctionReturnType = stmt.ReturnType
	defer func() { fg.codegen.currentFunctionReturnType = prevReturnType }()

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
	// 签名必须与 forwardDecls 生成的原型一致（此前硬编码 int64_t，与 void 原型冲突）
	code.WriteString(fg.annotatedFuncSignature(stmt))
	code.WriteString(" {\n")

	prevReturnType := fg.codegen.currentFunctionReturnType
	fg.codegen.currentFunctionReturnType = stmt.ReturnType
	defer func() { fg.codegen.currentFunctionReturnType = prevReturnType }()

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
		case "hint":
			/*
			 * #[hint(blocking)]：登记到阻塞车道白名单（kl 语法入口）。
			 *
			 * 登记发生在函数生成时；spawn 该函数 / 在 conc fn 内调用
			 * 它的代码都在本 TU 内、且出现在定义之后，能查到这份登记。
			 * 多编译单元场景（blockingHints 是进程内 map）暂不跨 TU 共享。
			 */
			if len(attr.Args) > 0 && attr.Args[0] == "blocking" {
				conc.MarkBlocking(stmt.Name)
			}
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
		// 裸机入口符号名必须与 freestanding.c.tmpl / user.c.tmpl 里的前向声明一致。
		// 两种写法都要落到同一个 C 符号上：
		//   fn main()       → 直接重命名为裸机入口（不能叫 main，裸机无 CRT 启动代码）
		//   fn kaula_main() → UserFuncCName 给出原名 kaula_main，与模板声明一致
		funcName = "kaula_main"
	}
	code.WriteString("int " + funcName + "() {\n")
	fg.codegen.indent++

	// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
	useKMM := !stmt.NoKMM && fg.shouldUseKMMScopeForBody(stmt.Body)
	// main 不参与 mark 回收（不是递归环成员），但必须显式关闭以切断继承
	fg.codegen.SetKMMUseMark(false)
	if useKMM {
		fg.codegen.EnterKMMScope()
		// Phase 2：在生成函数体之前设置函数级 checkpoint 标志，
		// 使循环 codegen 能检测到并降级为 offset 回收
		fg.codegen.funcHasKMMCheckpoint = true
	}
	// 方案 C：重置 promote 标志
	fg.codegen.funcEmitsPromote = false
	fg.codegen.funcEmitsInternalPromote = false

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
		code.WriteString(fg.codegen.KMMStart(funcName))
		code.WriteString(finalBody.String())
		code.WriteString(fg.codegen.indentString())
		code.WriteString(fg.codegen.KMMEnd())
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

// shouldUseKMMMarkForFunc 判定当前函数是否改用 base 感知的
// kmm_v4_mark_save/restore 替代 kmm_v4_scope_push/pop。
//
// 动机：KMM_V4_MAX_SCOPE_DEPTH（默认 1024）是 TLS 里的定长数组，每层递归
// 消耗一个槽位，"递归 + 每层分配"撞上就直接 abort。mark 存在 C 栈局部
// 变量里，不占槽位，深度回到只受 C 栈约束的量级。语义与 scope 等价：
// 退出区间即回卷 thread heap offset 回收区间内分配（每层仍回收，内存行为不变）。
//
// 只对"处于递归环 + 函数级启用 KMM"的普通函数开启，逐项排除无法保证
// start/end 严格配对或依赖作用域栈状态的形态：
//   - 并发函数：conc 守卫状态机/IO 重写会按 kmmScopeDepth 发射 scope_pop；
//   - asm / inline / no_kmm / 注解函数：不走常规函数体包裹；
//   - 嵌套函数（含捕获）：形参回写与提升路径更复杂，保守跳过。
func (fg *FunctionGenerator) shouldUseKMMMarkForFunc(stmt *ast.FunctionStatement) bool {
	if os.Getenv("KAULA_KMM_MARK") == "0" {
		return false
	}
	if stmt.IsConcurrent || stmt.IsAsm || stmt.Inline || stmt.NoKMM {
		return false
	}
	if stmt.Annotation != ast.TreeAnnotationNone {
		return false
	}
	if len(stmt.Captures) > 0 || stmt.Name == "main" {
		return false
	}
	return fg.inRecursionCycle(stmt.Name)
}

// inRecursionCycle 判断函数是否处于递归环中：直接自递归，或经由其他函数
// 互相递归。互递归的每一层同样会 push KMM 作用域，深度一样受
// KMM_V4_MAX_SCOPE_DEPTH 约束，因此"递归 + 每层分配"诊断必须覆盖它
// （尾调用改写只处理直接自递归，不处理互递归）。
//
// 调用图与强连通分量在首次调用时构建一次（见 callgraph.go），这里只查表。
func (fg *FunctionGenerator) inRecursionCycle(funcName string) bool {
	return fg.codegen.InRecursionCycle(funcName)
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

// generateConcurrentFunction 生成并发函数（conc fn）
//
// 产出（文档 §9.1）：
//  1. Frame 结构体 —— 保存跨 suspend 存活的变量
//  2. resume 函数 —— 状态机；无挂起点时退化为单一形态
//  3. coercion shim —— 让 conc fn 能当普通 fn 用（§9.4）
//
// 实际代码生成委托给 conc 包：Frame 形状与状态划分由那里的
// 静态分析决定，这个函数只负责把 Kaula 侧的符号表翻译成 C 类型。
func (fg *FunctionGenerator) generateConcurrentFunction(stmt *ast.FunctionStatement) string {
	// 出现 conc fn 就需要并发内核头
	codeUsesConcurrency = true

	// 分析函数体：找挂起点与需要提升的变量
	analysis := conc.AnalyzeBody(stmt.Body)

	/*
	 * 跨挂起点的所有权校验。
	 *
	 * Frame 让局部变量跨挂起点存活，而挂起意味着当前 Worker 被归还、
	 * 任务可能跨线程恢复 —— 恢复后所有存活性判断都得重新成立。
	 * 这里在生成任何 C 代码之前先校验一遍：确定有问题的用法直接报编译错误，
	 * 而不是让它编过、跑起来安静地读到已移空的槽位。
	 */
	for _, issue := range conc.ValidateOwnership(stmt.Name, stmt.Body) {
		fg.codegen.error(fmt.Sprintf("line %d: %s\n  建议: %s", issue.Line, issue.Message, issue.Hint))
	}

	// 构造 Emitter
	cName := fg.codegen.UserFuncCName(stmt.Name)
	if stmt.IsExported {
		cName = stmt.Name
	}
	emitter := &conc.Emitter{
		FuncName:    stmt.Name,
		CName:       cName,
		ReturnType:  stmt.ReturnType,
		CReturnType: fg.mapReturnType(stmt.ReturnType),
		Params:      stmt.Params,
		ParamTypes:  stmt.ParamTypes,
	}

	for i := range stmt.Params {
		ctype := "int64_t"
		isNullable := false
		if i < len(stmt.ParamNullables) {
			isNullable = stmt.ParamNullables[i]
		}
		if i < len(stmt.ParamTypes) && stmt.ParamTypes[i] != "" {
			if t := fg.codegen.typeGenerator.convertType(stmt.ParamTypes[i], isNullable); t != "" {
				ctype = t
			}
		}
		emitter.ParamCTypes = append(emitter.ParamCTypes, ctype)
		emitter.ParamNullables = append(emitter.ParamNullables, isNullable)
	}

	// 进入函数作用域：让语句生成器能解析参数
	prevReturnType := fg.codegen.currentFunctionReturnType
	fg.codegen.currentFunctionReturnType = stmt.ReturnType
	savedScopeDepth := fg.codegen.kmmScopeDepth
	fg.codegen.kmmScopeDepth = 0
	fg.codegen.EnterScope("conc_" + stmt.Name)
	for i, p := range stmt.Params {
		nullable := false
		if i < len(stmt.ParamNullables) {
			nullable = stmt.ParamNullables[i]
		}
		kType := ""
		if i < len(stmt.ParamTypes) {
			kType = stmt.ParamTypes[i]
		}
		fg.codegen.AddSymbol(p, kType, nullable, "parameter", stmt.Pos.Line, stmt.Pos.Column)
	}

	fg.codegen.ExitScope()
	fg.codegen.kmmScopeDepth = savedScopeDepth
	fg.codegen.currentFunctionReturnType = prevReturnType

	// 登记签名：spawn 生成时需要按形参名写 Frame 字段
	// 同时固化形态判定：GenerateSpawn 可能先于 Generate 被调用
	emitter.SetStateful(analysis.NeedsSuspend)
	emitter.SetGuardLayout(analysis.CompoundDepth, analysis.SuspendMaxDepth, analysis.HasForIn)
	fg.codegen.RegisterConcFunc(emitter)

	// 函数体在 resume 上下文中重新生成（return 需要改写）
	return fg.EmitResume(stmt, emitter, analysis)
}

// statementHasSuspend 报告一条语句里是否含可插桩调用
func statementHasSuspend(s ast.Statement) bool {
	switch v := s.(type) {
	case *ast.ExpressionStatement:
		return conc.HasSuspendCall(v.Expression)
	case *ast.ReturnStatement:
		return conc.HasSuspendCall(v.Value)
	case *ast.VariableDeclaration:
		return conc.HasSuspendCall(v.Value)
	case *ast.YieldStatement:
		return conc.HasSuspendCall(v.Source)
	case *ast.IfStatement:
		if conc.HasSuspendCall(v.Condition) {
			return true
		}
		for _, x := range v.Body {
			if statementHasSuspend(x) {
				return true
			}
		}
		for _, x := range v.Else {
			if statementHasSuspend(x) {
				return true
			}
		}
	case *ast.WhileStatement:
		if conc.HasSuspendCall(v.Condition) {
			return true
		}
		for _, x := range v.Body {
			if statementHasSuspend(x) {
				return true
			}
		}
	case *ast.ForStatement:
		if conc.HasSuspendCall(v.Condition) {
			return true
		}
		for _, x := range v.Body {
			if statementHasSuspend(x) {
				return true
			}
		}
	case *ast.ForInStatement:
		for _, x := range v.Body {
			if statementHasSuspend(x) {
				return true
			}
		}
	case *ast.BlockStatement:
		for _, x := range v.Statements {
			if statementHasSuspend(x) {
				return true
			}
		}
	}
	return false
}

// statementHasBlocking 判断语句内是否调用被 #[hint(blocking)] 标记的函数。
// 结构与 statementHasSuspend 一致 —— 这类调用也是挂起点（BLOCKED 形态），
// 只是提交时不构造 KaulaIoRequest，而是让 Worker 把任务移交 Block Lane。
func statementHasBlocking(s ast.Statement) bool {
	switch v := s.(type) {
	case *ast.ExpressionStatement:
		return conc.HasBlockingCall(v.Expression)
	case *ast.ReturnStatement:
		return conc.HasBlockingCall(v.Value)
	case *ast.VariableDeclaration:
		return conc.HasBlockingCall(v.Value)
	case *ast.YieldStatement:
		return conc.HasBlockingCall(v.Source)
	case *ast.IfStatement:
		if conc.HasBlockingCall(v.Condition) {
			return true
		}
		for _, x := range v.Body {
			if statementHasBlocking(x) {
				return true
			}
		}
		for _, x := range v.Else {
			if statementHasBlocking(x) {
				return true
			}
		}
	case *ast.WhileStatement:
		if conc.HasBlockingCall(v.Condition) {
			return true
		}
		for _, x := range v.Body {
			if statementHasBlocking(x) {
				return true
			}
		}
	case *ast.ForStatement:
		if conc.HasBlockingCall(v.Condition) {
			return true
		}
		for _, x := range v.Body {
			if statementHasBlocking(x) {
				return true
			}
		}
	case *ast.ForInStatement:
		for _, x := range v.Body {
			if statementHasBlocking(x) {
				return true
			}
		}
	case *ast.BlockStatement:
		for _, x := range v.Statements {
			if statementHasBlocking(x) {
				return true
			}
		}
	}
	return false
}

// concSuspendReturn 产出"保存状态并挂起返回"的 C 片段。
//
// idx 是即将挂起的段序号，同时作为 pc 的下一站。
// subNext 是下一段起点的守卫槽位：跨段时恒为 0（段体从槽位 0 重新起跑）。
//
// 它只服务于「顶层语句两段式切分」。复合语句体内部的挂起必须就地发挂起返回
// —— 外层这里的作用域看不到循环体/分支体里生成的 IO 请求局部量，
// 强行引用就是 use of undeclared identifier（见 conc_io_rewrite.go）。
func (fg *FunctionGenerator) concSuspendReturn(stmt *ast.FunctionStatement, idx int,
	analysis *conc.BodyAnalysis, subNext int, blocked bool) string {

	var b strings.Builder

	/*
	 * 保存活到下一段的变量。
	 *
	 * 必须写回 Frame 里每一个在下一段还要读的变量 —— 漏一个，
	 * 恢复时那个局部就是未初始化值。这类 bug 不崩，只是静默算错。
	 */
	bound := -1
	if len(analysis.SuspendSites) > 0 {
		k := idx - 1
		if k >= len(analysis.SuspendSites) {
			k = len(analysis.SuspendSites) - 1
		}
		bound = analysis.SuspendSites[k].Ordinal
	}
	b.WriteString(fg.codegen.concSaveLiveVars(bound))

	b.WriteString(fmt.Sprintf("    f->pc = %d;\n", idx))
	// 复合语句守卫槽位归零只在 Frame 含 sub[] 时生成（Frame 瘦身：
	// 无 if/while/for/for-in 的函数省略 sub/branch，顶层挂起点无需归零）
	if analysis.HasCompound {
		b.WriteString(fmt.Sprintf("    f->sub[0] = %d;\n", subNext))
	}

	/*
	 * 把 IO 请求交给 Worker → IoDriver。
	 *
	 * io_request 字段是 I4 的关键：Worker 读到 SUSPEND 后立刻把它
	 * 投给 IoDriver 并让出自己，绝不原地执行这次 IO。
	 *
	 * 用最近一个 __kaula_io_N：submit 形态生成的就是那个具名局部量，
	 * 生命周期覆盖到这条 return。没有它，IoDriver 收到的是空请求 ——
	 * 类型 0 不是任何合法 op，会直接被拒。
	 *
	 * blocked 形态（#[hint(blocking)]）：不构造请求，返回
	 * KAULA_RESUME_BLOCKED —— Worker 读到后把任务移交 Block Lane
	 * （I5），由专用线程就地执行阻塞操作。
	 */
	if blocked {
		b.WriteString("    return (KaulaResumeResult){ .status = KAULA_RESUME_BLOCKED, "+
			".io_request = {0}, .value = NULL };\n")
	} else if fg.lastIoVar != "" {
		b.WriteString(fmt.Sprintf("    return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND, "+
			".io_request = %s, .value = NULL };\n", fg.lastIoVar))
	} else {
		b.WriteString("    return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND, .value = NULL };\n")
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

/*
 * concBodySegments 按挂起点把函数体切成若干段 C 代码。
 *
 * == 顶层语句：两段式切分 ==
 *
 * 每段末尾若含挂起调用，就紧跟一个"保存状态并返回 SUSPEND"的片段。
 * resume 跑到那里就把 Frame 写好并让出 Worker，恢复时从下一段继续。
 *
 * 必须逐语句生成而不是整段生成：整段生成的话，中途挂起会跳过写回，
 * 恢复后读到的是未初始化的 Frame 字段。
 *
 * 含挂起的那条语句要被生成两次：
 *   第一次 concIoSubmit —— 构造 IO 请求并提交，函数体到此为止
 *   第二次 concIoResult —— 恢复后从 TLS 取结果，函数体从这里继续
 * 这就是"把一次调用劈成两半"：不劈的话，要么 Worker 原地阻塞在 IO 上
 * （违反 I4），要么恢复后拿不到结果。
 *
 * == 复合语句：整条只生成一次 ==
 *
 * 体内部含挂起点的 while / if / block 不能拆成两段 —— 它们不是「执行两次
 * 同一段代码」，而是「恢复到循环体内部的某一处」。所以这类语句整条只生成
 * 一次，恢复位置交给 Frame.sub[d] 的守卫槽位（见 conc_guard.go）。
 *
 * 一旦某段里出现了这样的复合语句，**整段**都进守卫模式：排在它之前的语句
 * 在恢复时会被重新执行（`i64 i = 0` 再跑一遍就是循环从头数），必须同样被
 * 槽位挡住。所以守卫与否是**整段**的属性，生成之前先分组、判定完再动手。
 */
func (fg *FunctionGenerator) concBodySegments(stmt *ast.FunctionStatement, analysis *conc.BodyAnalysis, firstRun bool) []string {
	cg := fg.codegen
	sg := cg.statementGenerator

	if len(analysis.SuspendSites) == 0 {
		return []string{sg.generateStmtsWithLiveness(stmt.Body, nil)}
	}

	// 挂起返回需要知道此刻活跃的变量；内联挂起需要知道当前段号
	cg.concAnalysis = analysis
	cg.concParams = stmt.Params
	cg.concFirstRun = firstRun
	defer func() {
		cg.concAnalysis = nil
		cg.concParams = nil
		cg.concGuardDepth = -1
		cg.concGuardSeq = 0
		cg.concSegmentPC = 0
		cg.concSlotAwaits = 0
		cg.concFirstRun = false
	}()

	segments := make([]string, 0, len(analysis.SuspendSites)+1)
	cutoffs := make([]int, 0, len(analysis.SuspendSites)+1)
	var current strings.Builder

	// maxOrdinal：本段内最靠后的挂起点序号 —— Frame 变量恢复的界
	maxOrdinal := -1

	// finishSegment 收束一段（不动守卫状态：守卫是组的属性，不是段的属性）
	finishSegment := func() {
		var bound int
		if cg.concGuardActive() {
			/*
			 * 守卫模式：恢复副本覆盖全部跨 suspend 变量。
			 *
			 * 段体的声明被降级成「赋值」（对象由段首恢复副本提供），
			 * 所以一个都不能漏；漏一个就是「use of undeclared identifier」。
			 */
			bound = conc.RestoreAll
		} else {
			bound = -1
			if pc := len(segments); pc > 0 && pc-1 < len(analysis.SuspendSites) {
				bound = analysis.SuspendSites[pc-1].Ordinal
			}
			if maxOrdinal > bound {
				bound = maxOrdinal
			}
		}
		segments = append(segments, current.String())
		cutoffs = append(cutoffs, bound)
		current.Reset()
		maxOrdinal = -1
	}

	for _, grp := range concGroupBody(stmt.Body) {
		/*
		 * 整段决定要不要守卫。段体占第 0 层，复合语句体再往下开一层。
		 * 提前判定是必须的：段体里排在复合语句之前的语句此刻就要决定
		 * 带不带守卫，等看到复合语句再回头补已经来不及了。
		 */
		if grp.guarded {
			cg.concGuardDepth = 0
			cg.concGuardSeq = 0
		} else {
			cg.concGuardDepth = -1
			cg.concGuardSeq = 0
		}
		// 本组是否已经因为一条顶层挂起语句而切过段（切过就不能在组尾再收一次）
		splitInGroup := false

		for gi, s := range grp.stmts {
			cg.concSegmentPC = len(segments)

			if !statementHasSuspend(s) {
				// generateStmtsWithLiveness 在守卫模式下自带槽位包装
				current.WriteString(sg.generateStmtsWithLiveness([]ast.Statement{s}, nil))
				continue
			}

			cg.concCheckSupported(s, stmt.Name)

			if concIsResumableCompound(s) {
				/*
				 * 整条只生成一次：恢复位置由体内各层的守卫槽位表达。
				 *
				 * 必须走 concIoSubmit：可插桩调用要生成「构造请求 + 就地挂起
				 * 返回」这一整块。走 concIoNone 的话调用原样生成，Worker 就地
				 * 阻塞在 IO 上 —— 状态机形状还在，但 I4 已经没了。
				 */
				if l := concMaxSuspendOrdinal(s); l > maxOrdinal {
					maxOrdinal = l
				}
				current.WriteString(fg.concGenerateStmts(concIoSubmit, []ast.Statement{s}))
				continue
			}

			/*
			 * 这条语句是不是本组的最后一条？
			 *
			 * 组是「遇到顶层挂起语句就闭合」的（见 concGroupBody），所以
			 * 组尾恰好是顶层挂起时，组尾的 finishSegment 会另起一段。
			 * 而守卫模式下这条挂起若走下面的「两个相邻槽位」形态，
			 * pc 留在**本段**不推进 —— 新段就没有任何 pc 写入，
			 * 成了不可达段：它的语句永远不执行。
			 *
			 * 实测（test/concurrency/test_guard_slot_residue.kl 用例 B）：
			 *
			 *     while i < n { time_sleep(1); ... }   守卫组
			 *     time_sleep(1)                         组尾顶层挂起
			 *     if acc == 0 { acc = 42 }             下一组 → 不可达
			 *
			 * 表现为静默返回错值（acc 得 0，期望 42）：不崩、不报错。
			 *
			 * 所以组尾的顶层挂起必须走**切段**形态：pc 推进到新段，
			 * result 半生成进新段（与 !concGuardActive 分支同一套机制）。
			 */
			endsGroup := gi == len(grp.stmts)-1

			if !cg.concGuardActive() || endsGroup {
				/*
				 * 前半段：实参求值 + 请求构造 + 提交。
				 *
				 * 守卫组切段时必须**先关掉槽位包装**：submit 半生成的
				 * `KaulaIoRequest __kaula_io_N` 是这条语句的局部量，
				 * 而紧随其后的 concSuspendReturn 要引用它。包在
				 * `if (f->sub[d]==k) { ... }` 的花括号里，那个局部量
				 * 就跑出了作用域 —— clang: use of undeclared identifier。
				 *
				 * 这正是守卫模式内联挂起要"就地发挂起返回"的原因；
				 * 切段形态的挂起返回在外层，作用域天然够不到。
				 */
				if endsGroup && cg.concGuardActive() {
					cg.concGuardDepth = -1
				}
				current.WriteString(fg.concGenerateStmts(concIoSubmit, []ast.Statement{s}))
				if endsGroup && grp.guarded {
					cg.concGuardDepth = 0
					cg.concGuardSeq = 0
				}
				current.WriteString(fg.concSuspendReturn(stmt, len(segments)+1, analysis, 0,
					statementHasBlocking(s)))
				finishSegment()
				splitInGroup = true
				cg.concSegmentPC = len(segments)

				// 后半段：从 TLS 取结果，函数体从这里继续。
				// 守卫组被这里切成两段，后半段仍需保持守卫（同一组一个属性）。
				if grp.guarded {
					cg.concGuardDepth = 0
					cg.concGuardSeq = 0
				}
				current.WriteString(fg.concGenerateStmts(concIoResult, []ast.Statement{s}))
				continue
			}

		/*
			 * 守卫模式下的两段式切分：两个相邻槽位。
			 *
			 * 包装由 generateStmtsWithLiveness 自己做，这里不能再包一层 ——
			 * 多包一层就会出现「sub[d] 先推进一格、再进同一条语句」的嵌套守卫，
			 * 第二个守卫永远不成立。
			 *
			 * submit 槽里的挂起返回把 sub[d] 设成 concGuardSeq ——
			 * 那正是下一个（result）槽的序号，于是恢复后从 result 槽接着走。
			 */
			current.WriteString(fg.concGenerateStmts(concIoSubmit, []ast.Statement{s}))
			current.WriteString(fg.concGenerateStmts(concIoResult, []ast.Statement{s}))
			continue
		}

		/*
		 * 组尾收段。
		 *
		 * 组里若已经因为顶层挂起语句切过段（splitInGroup），这里就不能再收 ——
		 * 那会把「取回结果」那半段和本组余下的语句拆成两个 case，而前半段
		 * 以「段内无挂起」的兜底 return DONE 收尾，于是余下的语句永远执行不到。
		 */
		if !splitInGroup {
			finishSegment()
		}
	}

	cg.concGuardDepth = -1
	analysis.SegmentRestoreOrdinals = cutoffs
	return segments
}

/*
 * concBodyGroup 是一组「落在同一个 resume 段里」的顶层语句。
 */
type concBodyGroup struct {
	// stmts 是这一组的顶层语句（按源码顺序）
	stmts []ast.Statement
	// guarded：这一组里有没有「体内含挂起点」的复合语句。
	// 有的话整组都进守卫模式 —— 组内排在它之前的语句也必须被槽位挡住，
	// 否则恢复时会被重跑（`i64 i = 0` 再跑一遍就是循环从头数）。
	guarded bool
}

/*
 * concGroupBody 按「顶层语句两段式切分」的规则把函数体分组。
 *
 * 分组规则与老切分一致：一条「含挂起点但不是可恢复复合语句」的顶层语句
 * 结束当前组，它的 result 半另起一组。
 *
 * 分组只在「生成之前」做一次：守卫是组的属性，必须先知道整组里有没有
 * 可恢复复合语句，才知道前面的语句要不要带守卫。
 */
func concGroupBody(body []ast.Statement) []concBodyGroup {
	var groups []concBodyGroup
	cur := concBodyGroup{}

	for _, s := range body {
		if s == nil {
			continue
		}
		cur.stmts = append(cur.stmts, s)
		if concIsResumableCompound(s) {
			cur.guarded = true
		}
		if statementHasSuspend(s) && !concIsResumableCompound(s) {
			groups = append(groups, cur)
			cur = concBodyGroup{}
		}
	}
	if len(cur.stmts) > 0 {
		groups = append(groups, cur)
	}
	if len(groups) == 0 {
		// 空函数体：仍要产出一个（空）段，resume 的 switch 才合法
		groups = append(groups, concBodyGroup{})
	}
	return groups
}

// withConcIoRewrite 在指定改写模式下运行一次语句生成
func (fg *FunctionGenerator) withConcIoRewrite(mode concIoRewriteMode, gen func() string) string {
	prev := fg.codegen.concIoRewrite
	prevVar := fg.codegen.lastIoVar
	fg.codegen.concIoRewrite = mode
	if mode != concIoSubmit {
		// result 形态不应把上一次 submit 的变量名带进下一段
		fg.codegen.lastIoVar = ""
	}
	out := gen()
	fg.codegen.concIoRewrite = prev
	if mode == concIoSubmit {
		fg.lastIoVar = fg.codegen.lastIoVar
	}
	fg.codegen.lastIoVar = prevVar
	return out
}

// concGenerateStmts 在指定改写模式下生成一段语句
func (fg *FunctionGenerator) concGenerateStmts(mode concIoRewriteMode, stmts []ast.Statement) string {
	return fg.withConcIoRewrite(mode, func() string {
		return fg.codegen.statementGenerator.generateStmtsWithLiveness(stmts, nil)
	})
}
