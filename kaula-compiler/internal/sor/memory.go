package sor

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"sort"
	"strings"
)

// ============================================================================
// Pass 2.5b + 2.5c: 内存分配决策与释放点确定
// ============================================================================

// AllocKind 内存分配位置
type AllocKind int

const (
	AllocStack      AllocKind = iota // 栈分配（编译器自动管理）
	AllocBumpPool                  // KMM bump pool（作用域批量回收）
	AllocArenaTiny                  // KMM tiny arena (≤64B)
	AllocArenaSmall                 // KMM small arena (≤256B)
	AllocArenaMedium                // KMM medium arena (≤2048B)
)

func (k AllocKind) String() string {
	switch k {
	case AllocStack:
		return "Stack"
	case AllocBumpPool:
		return "BumpPool"
	case AllocArenaTiny:
		return "ArenaTiny"
	case AllocArenaSmall:
		return "ArenaSmall"
	case AllocArenaMedium:
		return "ArenaMedium"
	default:
		return fmt.Sprintf("Unknown(%d)", int(k))
	}
}

// DropAction 释放动作
type DropAction int

const (
	DropNone     DropAction = iota // 无需释放（已 yeide / 已 extract）
	DropScopeEnd                   // 作用域退出时自动释放
	DropHollow                     // hollow 状态，释放残留外壳
)

func (a DropAction) String() string {
	switch a {
	case DropNone:
		return "None"
	case DropScopeEnd:
		return "ScopeEnd"
	case DropHollow:
		return "Hollow"
	default:
		return fmt.Sprintf("Unknown(%d)", int(a))
	}
}

// MemoryDecision 单个变量的内存管理决策
type MemoryDecision struct {
	VarName          string         // 变量名
	ObjID            string         // SOR 对象 ID
	FinalState       OwnershipState // 分析结束时的所有权状态
	AllocKind        AllocKind      // 分配位置建议
	DropAction       DropAction     // 释放动作
	ScopeID          int            // 所属作用域
	IsComposite      bool           // 是否复合类型
	ExtractedChildren map[string]bool // extract 后的 hollow 子元素
}

func (d *MemoryDecision) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s: alloc=%s drop=%s state=%s scope=%d",
		d.VarName, d.AllocKind, d.DropAction, d.FinalState, d.ScopeID))
	if d.IsComposite {
		b.WriteString(" composite")
	}
	if len(d.ExtractedChildren) > 0 {
		b.WriteString(fmt.Sprintf(" hollow_children=%d", len(d.ExtractedChildren)))
	}
	return b.String()
}

// GetAllObjects 返回所有受追踪的对象（按 ID 排序）。
// 添加此方法到 OwnershipTracker 以供 MemoryAnalyzer 使用。
func (t *OwnershipTracker) GetAllObjects() []*SORObject {
	result := make([]*SORObject, 0, len(t.objects))
	ids := make([]string, 0, len(t.objects))
	for id := range t.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		result = append(result, t.objects[id])
	}
	return result
}

// ============================================================================
// MemoryAnalyzer
// ============================================================================

// MemoryAnalyzer 基于 OwnershipTracker 的分析结果，为每个对象生成内存管理决策。
type MemoryAnalyzer struct {
	decisions map[string]*MemoryDecision
	tracker   *OwnershipTracker
}

func NewMemoryAnalyzer() *MemoryAnalyzer {
	return &MemoryAnalyzer{
		decisions: make(map[string]*MemoryDecision),
	}
}

// AnalyzeMemory 遍历 tracker 中所有对象，生成内存决策。
func (ma *MemoryAnalyzer) AnalyzeMemory(tracker *OwnershipTracker) []*MemoryDecision {
	if tracker == nil || tracker.GetObjectCount() == 0 {
		return nil
	}
	ma.tracker = tracker

	allObjs := tracker.GetAllObjects()
	for _, obj := range allObjs {
		d := ma.buildDecision(obj)
		ma.decisions[obj.Name] = d
	}

	results := make([]*MemoryDecision, 0, len(ma.decisions))
	for _, d := range ma.decisions {
		results = append(results, d)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].VarName < results[j].VarName
	})
	return results
}

func (ma *MemoryAnalyzer) buildDecision(obj *SORObject) *MemoryDecision {
	d := &MemoryDecision{
		VarName:    obj.Name,
		ObjID:      obj.ID,
		FinalState: obj.State,
		ScopeID:    obj.ScopeID,
		IsComposite: obj.IsComposite,
	}

	// 收集 hollow 子元素
	if obj.IsComposite {
		for path, childID := range obj.Children {
			if childObj := ma.tracker.GetObject(childID); childObj != nil {
				if childObj.State == StateHollow {
					if d.ExtractedChildren == nil {
						d.ExtractedChildren = make(map[string]bool)
					}
					d.ExtractedChildren[path] = true
				}
			}
		}
	}

	switch obj.State {
	case StateMoved:
		d.AllocKind = AllocStack
		d.DropAction = DropNone

	case StateOwned:
		d.AllocKind = ma.determineAllocKind(obj)
		if obj.IsComposite && len(d.ExtractedChildren) > 0 {
			d.DropAction = DropHollow
		} else {
			d.DropAction = DropScopeEnd
		}

	case StateReleased:
		d.AllocKind = ma.determineAllocKind(obj)
		d.DropAction = DropScopeEnd

	case StateHollow, StateExtracted:
		d.AllocKind = ma.determineAllocKind(obj)
		d.DropAction = DropNone

	default:
		d.AllocKind = AllocBumpPool
		d.DropAction = DropScopeEnd
	}

	return d
}

func (ma *MemoryAnalyzer) determineAllocKind(obj *SORObject) AllocKind {
	typeName := strings.ToLower(obj.TypeName)
	switch typeName {
	case "int64", "int32", "int16", "int8",
		"uint64", "uint32", "uint16", "uint8",
		"float64", "float32", "bool", "int", "uint",
		"double", "f64", "f32", "float",
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"void":
		return AllocStack
	}
	if strings.HasPrefix(typeName, "[]") {
		return AllocBumpPool
	}
	if typeName == "string" || typeName == "str" {
		return AllocBumpPool
	}
	return AllocBumpPool
}

// GetDecision 查找指定变量的内存决策
func (ma *MemoryAnalyzer) GetDecision(varName string) *MemoryDecision {
	return ma.decisions[varName]
}

// ============================================================================
// 代码生成辅助
// ============================================================================

// GenerateCleanupCode 为作用域退出生成清理代码。
// bump pool / arena 由 KMM_V4_SCOPE_END 自动回收，此处仅处理特殊情况。
func GenerateCleanupCode(decisions []*MemoryDecision, scopeID int, indent string) string {
	var b strings.Builder
	for _, d := range decisions {
		if d.ScopeID != scopeID {
			continue
		}
		switch d.DropAction {
		case DropHollow:
			if d.IsComposite && len(d.ExtractedChildren) > 0 {
				b.WriteString(indent)
				b.WriteString(fmt.Sprintf("/* hollow cleanup: %s */\n", d.VarName))
			}
		case DropScopeEnd:
			if d.FinalState == StateReleased {
				b.WriteString(indent)
				b.WriteString(fmt.Sprintf("/* scope-end release: %s — consumers exited */\n", d.VarName))
			}
		}
	}
	return b.String()
}

// GenerateScopeCleanupHeader 生成作用域 KMM 头
func GenerateScopeCleanupHeader(indent string, scopeID int) string {
	return fmt.Sprintf("%sKMM_V4_SCOPE_START { /* scope %d */\n", indent, scopeID)
}

// GenerateScopeCleanupFooter 生成作用域 KMM 脚
func GenerateScopeCleanupFooter(indent string, scopeID int, decisions []*MemoryDecision) string {
	var b strings.Builder
	cleanup := GenerateCleanupCode(decisions, scopeID, indent+"    ")
	if cleanup != "" {
		b.WriteString(cleanup)
	}
	b.WriteString(fmt.Sprintf("%s} KMM_V4_SCOPE_END; /* scope %d */\n", indent, scopeID))
	return b.String()
}

// ============================================================================
// 扩展入口
// ============================================================================

// AnalyzeASTWithMemory 扩展版分析，返回内存管理决策。
// 供 main.go 调用，串联 SOR 安全分析 + 内存分析。
func AnalyzeASTWithMemory(program interface {
	GetStmts() []Stmt
}) ([]SORError, []*MemoryDecision, []string) {
	analyzer := NewSORAnalyzer()
	stmts := program.GetStmts()
	sorErrors := analyzer.Analyze(stmts)

	ma := NewMemoryAnalyzer()
	decisions := ma.AnalyzeMemory(analyzer.GetTracker())

	return sorErrors, decisions, analyzer.GetExecLog()
}

// FullAnalysisResult 完整分析结果
type FullAnalysisResult struct {
	SORErrors    []SORError
	Decisions    []*MemoryDecision
	Escape       map[string]EscapeLevel
	Liveness     *LivenessResult
	InterProc    *InterProcResult
	Sizes        map[string]int
	ExecLog      []string
	// PoolCapacity 基于静态分析估算的 KMM V4 池容量（字节）
	// 0 表示使用默认值
	PoolCapacity int
}

// EstimatePoolCapacityFromAST 非 SOR 模式下仅基于 AST 扫描估算池容量
// 用于不启用 SOR 时也能根据动态分配调用调整池大小
func EstimatePoolCapacityFromAST(program *ast.Program) int {
	if program == nil {
		return 0
	}
	const (
		minCapacity  = 2 * 1024 * 1024   // 2 MB
		maxCapacity  = 256 * 1024 * 1024 // 256 MB
		safetyMargin = 3.0
	)
	dynTotal := scanDynamicAllocations(program)
	estimated := int(float64(dynTotal) * safetyMargin)
	if estimated < minCapacity {
		estimated = minCapacity
	}
	if estimated > maxCapacity {
		estimated = maxCapacity
	}
	return estimated
}

// AnalyzeFull 完整的 SOR 分析流水线：
// 安全分析 → 内存决策 → 逃逸分析 → 大小估算 → 活跃性分析 → 跨函数分析
func AnalyzeFull(program interface {
	GetStmts() []Stmt
}) *FullAnalysisResult {
	return AnalyzeFullWithAST(program, nil)
}

// AnalyzeFullWithAST 带AST的完整 SOR 分析流水线
// 当 astProgram 非 nil 时，会先从 AST 注册结构体字段类型到 TypeSizer，
// 使自定义结构体的大小估算和 arena 路由生效
func AnalyzeFullWithAST(program interface {
	GetStmts() []Stmt
}, astProgram *ast.Program) *FullAnalysisResult {
	result := &FullAnalysisResult{}

	// 第一阶段：标准 SOR 安全分析
	analyzer := NewSORAnalyzer()
	stmts := program.GetStmts()
	result.SORErrors = analyzer.Analyze(stmts)
	result.ExecLog = analyzer.GetExecLog()
	tracker := analyzer.GetTracker()

	// 第二阶段：内存决策分析
	ma := NewMemoryAnalyzer()
	result.Decisions = ma.AnalyzeMemory(tracker)

	// 第三阶段：轻量逃逸分析
	ea := NewEscapeAnalyzer()
	result.Escape = ea.AnalyzeEscape(stmts)
	// 将逃逸结果应用到内存决策
	// 只有高逃逸级别（EscReturn+）才强制分配策略，低级别逃逸暂设默认值，后续由大小路由优化
	for _, d := range result.Decisions {
		if level, ok := result.Escape[d.ObjID]; ok {
			if EscapeForcesAlloc(level) {
				d.AllocKind = EscapeToAlloc(level)
			}
		}
	}

	// 第四阶段：大小估算 + Arena 路由
	ts := NewTypeSizer()
	// 从 AST 注册结构体字段类型，使自定义结构体大小可估算
	if astProgram != nil {
		RegisterStructsFromAST(astProgram, ts)
	}
	result.Sizes = ts.AnalyzeSizes(tracker)
	for _, d := range result.Decisions {
		// 高逃逸级别对象已由 EscapeForcesAlloc 锁定，跳过大小路由
		if level, ok := result.Escape[d.ObjID]; ok && EscapeForcesAlloc(level) {
			continue
		}
		if size, ok := result.Sizes[d.ObjID]; ok && size > 0 {
			routed := ts.RouteArena(size)
			// 非强制逃逸的对象，按大小路由到合适的 arena/栈
			d.AllocKind = routed
		}
	}

	// 第五阶段：活跃性分析
	la := NewLivenessAnalyzer()
	result.Liveness = la.AnalyzeLiveness(stmts)
	la.ComputeDropPoints(result.Liveness)

	// 第六阶段：跨函数分析
	ipa := NewInterProcAnalyzer()
	result.InterProc = ipa.AnalyzeInterProc(stmts, tracker, astProgram)
	ipa.ApplyInterProcToDecisions(result.Decisions, result.InterProc)

	// 第七阶段：基于静态分析估算 KMM V4 池容量
	result.PoolCapacity = estimatePoolCapacityWithAST(result, astProgram)

	return result
}

// estimatePoolCapacityWithAST 结合 SOR 决策和 AST 动态分配扫描估算池容量
// 补充 SOR 不追踪的 kmm_v4_malloc/std_malloc 调用
func estimatePoolCapacityWithAST(result *FullAnalysisResult, astProgram *ast.Program) int {
	const (
		minCapacity   = 256 * 1024        // 256 KB
		maxCapacity   = 256 * 1024 * 1024 // 256 MB
		safetyMargin  = 1.5               // 50% 安全余量
		defaultDynSize = 1024             // 无法估算的动态分配默认 1KB
	)

	// 1. SOR 追踪对象的总大小
	sorTotal := 0
	for _, d := range result.Decisions {
		switch d.AllocKind {
		case AllocBumpPool, AllocArenaTiny, AllocArenaSmall, AllocArenaMedium:
			if size, ok := result.Sizes[d.ObjID]; ok && size > 0 {
				sorTotal += size
			} else {
				sorTotal += 256
			}
		}
	}

	// 2. AST 扫描动态分配调用（kmm_v4_malloc / std_malloc）
	dynTotal := 0
	if astProgram != nil {
		dynTotal = scanDynamicAllocations(astProgram)
	}

	// 3. 合计 + 安全余量
	total := sorTotal + dynTotal
	estimated := int(float64(total) * safetyMargin)

	// 4. 边界限制
	if estimated < minCapacity {
		estimated = minCapacity
	}
	if estimated > maxCapacity {
		estimated = maxCapacity
	}

	return estimated
}

// scanDynamicAllocations 扫描 AST 中的 kmm_v4_malloc/std_malloc 调用
// 估算动态分配的总内存需求
func scanDynamicAllocations(program *ast.Program) int {
	if program == nil {
		return 0
	}
	total := 0
	for _, stmt := range program.Statements {
		total += scanDynamicAllocInStmt(stmt)
	}
	return total
}

// scanDynamicAllocInStmt 递归扫描语句中的动态分配调用
func scanDynamicAllocInStmt(stmt ast.Statement) int {
	if isNilStmt(stmt) {
		return 0
	}
	total := 0
	switch s := stmt.(type) {
	case *ast.FunctionStatement:
		if s == nil { return 0 }
		for _, bodyStmt := range s.Body {
			total += scanDynamicAllocInStmt(bodyStmt)
		}
	case *ast.VariableDeclaration:
		if s == nil { return 0 }
		if s.Value != nil {
			total += scanDynamicAllocInExpr(s.Value)
		}
	case *ast.ExpressionStatement:
		if s == nil { return 0 }
		if s.Expression != nil {
			total += scanDynamicAllocInExpr(s.Expression)
		}
	case *ast.IfStatement:
		if s == nil { return 0 }
		if s.Condition != nil {
			total += scanDynamicAllocInExpr(s.Condition)
		}
		for _, bs := range s.Body {
			total += scanDynamicAllocInStmt(bs)
		}
		for _, bs := range s.Else {
			total += scanDynamicAllocInStmt(bs)
		}
	case *ast.WhileStatement:
		if s == nil { return 0 }
		if s.Condition != nil {
			total += scanDynamicAllocInExpr(s.Condition)
		}
		for _, bs := range s.Body {
			total += scanDynamicAllocInStmt(bs)
		}
	case *ast.ForStatement:
		if s == nil { return 0 }
		if s.Condition != nil {
			total += scanDynamicAllocInExpr(s.Condition)
		}
		for _, bs := range s.Body {
			total += scanDynamicAllocInStmt(bs)
		}
	case *ast.BlockStatement:
		if s == nil { return 0 }
		for _, bs := range s.Statements {
			total += scanDynamicAllocInStmt(bs)
		}
	case *ast.ReturnStatement:
		if s == nil { return 0 }
		if s.Value != nil {
			total += scanDynamicAllocInExpr(s.Value)
		}
	}
	return total
}

// isNilStmt 检测语句接口是否为 nil（包括非空接口包含 nil 指针的情况）
func isNilStmt(stmt ast.Statement) bool {
	if stmt == nil {
		return true
	}
	switch s := stmt.(type) {
	case *ast.FunctionStatement: return s == nil
	case *ast.VariableDeclaration: return s == nil
	case *ast.ExpressionStatement: return s == nil
	case *ast.IfStatement: return s == nil
	case *ast.WhileStatement: return s == nil
	case *ast.ForStatement: return s == nil
	case *ast.BlockStatement: return s == nil
	case *ast.ReturnStatement: return s == nil
	case *ast.ImportStatement: return s == nil
	case *ast.SwitchStatement: return s == nil
	case *ast.CaseStatement: return s == nil
	case *ast.StructStatement: return s == nil
	case *ast.ClassStatement: return s == nil
	case *ast.MethodStatement: return s == nil
	case *ast.ConstructorStatement: return s == nil
	case *ast.PrefixStatement: return s == nil
	case *ast.ObjectStatement: return s == nil
	case *ast.InterfaceStatement: return s == nil
	case *ast.YeideStatement: return s == nil
	case *ast.ReleaseStatement: return s == nil
	case *ast.ExtractStatement: return s == nil
	case *ast.CallStatement: return s == nil
	case *ast.SpendStatement: return s == nil
	case *ast.TaskStatement: return s == nil
	case *ast.NonLocalStatement: return s == nil
	case *ast.VOStatement: return s == nil
	case *ast.TreeStatement: return s == nil
	}
	return false
}

// scanDynamicAllocInExpr 扫描表达式中的 malloc 调用
func scanDynamicAllocInExpr(expr ast.Expression) int {
	if expr == nil {
		return 0
	}
	total := 0
	switch e := expr.(type) {
	case *ast.CallExpression:
		// 检测 kmm_v4_malloc / std_malloc 调用
		// 函数名可能是 Identifier（kmm_v4_malloc）或嵌套 MemberAccessExpression（std.memory.kmm_v4_malloc）
		name := extractFuncName(e.Function)
		if name == "kmm_v4_malloc" || name == "std_malloc" ||
			name == "kmm_v4_alloc_auto" || name == "malloc" {
			// 尝试估算参数（通常是 size 表达式）
			if len(e.Args) > 0 {
				if size := estimateExprSize(e.Args[0]); size > 0 {
					total += size
				} else {
					total += 1024 // 默认 1KB
				}
			}
		}
		// 递归扫描参数
		for _, arg := range e.Args {
			total += scanDynamicAllocInExpr(arg)
		}
	case *ast.MemberAccessExpression:
		// std.memory.kmm_v4_malloc(...) 本身不会出现在这里（CallExpression 已包装）
		// 但递归扫描 Object 以防链式调用
		total += scanDynamicAllocInExpr(e.Object)
	case *ast.BinaryExpression:
		total += scanDynamicAllocInExpr(e.Left)
		total += scanDynamicAllocInExpr(e.Right)
	case *ast.UnaryExpression:
		total += scanDynamicAllocInExpr(e.Right)
	}
	return total
}

// extractFuncName 从函数表达式提取最终的函数名
// 支持 Identifier（kmm_v4_malloc）和嵌套 MemberAccessExpression（std.memory.kmm_v4_malloc → kmm_v4_malloc）
func extractFuncName(fn ast.Expression) string {
	if fn == nil {
		return ""
	}
	switch e := fn.(type) {
	case *ast.Identifier:
		return e.Name
	case *ast.MemberAccessExpression:
		// std.memory.kmm_v4_malloc 是嵌套 MemberAccessExpression，
		// 递归取最右端的 Member 作为函数名（kmm_v4_malloc）
		return e.Member
	}
	return ""
}

func isKMMV4MallocCall(fn ast.Expression) bool {
	name := extractFuncName(fn)
	if name == "kmm_v4_malloc" || name == "std_malloc" {
		return true
	}
	return false
}

// estimateExprSize 尝试估算字面量表达式的大小
// 对 Identifier 返回保守的元素数量代表值，使 size*size*8 这类表达式可估算
func estimateExprSize(expr ast.Expression) int {
	const identDefault = 512 // Identifier 的保守元素数量代表值（用于 n/2 等除法估算）
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		if e.Value > 0 {
			return int(e.Value)
		}
	case *ast.Identifier:
		// 变量实际值未知，给合理的保守代表值
		// 1000 对应典型批量分配场景（如 n*8 → 8KB），
		// 选择 1000 而非 identDefault(512) 是因为乘法场景下
		// 1000 * small_const 能产生更合理的池大小估算
		return 1000
	case *ast.BinaryExpression:
		left := estimateExprSize(e.Left)
		right := estimateExprSize(e.Right)
		if left > 0 && right > 0 {
			switch e.Operator {
			case "*", "MULTIPLY":
				return left * right
			case "+", "PLUS":
				return left + right
			case "-", "MINUS":
				if left > right {
					return left - right
				}
				return 1 // 最小为 1，避免零或负值
			case "/", "DIVIDE":
				if right > 0 {
					result := left / right
					if result > 0 {
						return result
					}
				}
				return left // 无法整除时保守返回 left
			}
		}
	}
	return 0
}

// SerializeFullAnalysisResult 将完整分析结果序列化为 map[string]interface{}
// 供 CodeGen 通过 SORCodeGenAdapter 消费，避免 codegen 直接依赖 sor 包
func SerializeFullAnalysisResult(result *FullAnalysisResult) map[string]interface{} {
	out := make(map[string]interface{})

	// decisions: 变量决策数组
	decisionsArr := make([]interface{}, 0, len(result.Decisions))
	for _, d := range result.Decisions {
		dm := map[string]interface{}{
			"var_name":    d.VarName,
			"obj_id":      d.ObjID,
			"alloc_kind":  d.AllocKind.String(),
			"drop_action": d.DropAction.String(),
			"scope_id":    fmt.Sprintf("%d", d.ScopeID),
		}
		decisionsArr = append(decisionsArr, dm)
	}
	out["decisions"] = decisionsArr

	// escape: obj_id -> escape_level string
	escapeMap := make(map[string]interface{})
	for id, level := range result.Escape {
		escapeMap[id] = level.String()
	}
	out["escape"] = escapeMap

	// sizes: obj_id -> size int
	sizesMap := make(map[string]interface{})
	for id, size := range result.Sizes {
		sizesMap[id] = size
	}
	out["sizes"] = sizesMap

	// func_sigs: 跨函数分析的函数签名
	if result.InterProc != nil && len(result.InterProc.FuncSigs) > 0 {
		funcSigsMap := make(map[string]interface{})
		for name, sig := range result.InterProc.FuncSigs {
			// has_ptr_return: 返回值是否涉及指针/所有权
			hasPtrReturn := false
			for _, ret := range sig.Returns {
				if ret.Mode == ModeOwned || strings.Contains(ret.Type, "*") {
					hasPtrReturn = true
					break
				}
			}
			sigMap := map[string]interface{}{
				"name":           sig.Name,
				"has_ptr_return": hasPtrReturn,
				"param_modes":    []interface{}{},
			}
			modes := make([]interface{}, 0, len(sig.Params))
			for _, p := range sig.Params {
				modes = append(modes, p.Mode.String())
			}
			sigMap["param_modes"] = modes
			funcSigsMap[name] = sigMap
		}
		out["func_sigs"] = funcSigsMap
	}

	return out
}

// FormatDecisionsSummary 格式化内存决策摘要
func FormatDecisionsSummary(decisions []*MemoryDecision) string {
	if len(decisions) == 0 {
		return "(no decisions)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== Memory Decisions (%d objects) ===\n", len(decisions)))
	for _, d := range decisions {
		b.WriteString(fmt.Sprintf("  %s\n", d.String()))
	}
	return b.String()
}
