package sor

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"reflect"
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
	DropNone     DropAction = iota // 无需释放（已 yield / 已 extract）
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

// PoolCapacityBreakdown 池容量分步计算明细
// 用于诊断输出，展示每步优化的效果
type PoolCapacityBreakdown struct {
	RawSum            int // 原始求和（所有对象简单相加）
	SharingAdjusted   int // 指针共享调整后（排除 release holder 重复计算）
	ScopeTreeOptimized int // 作用域树优化后（分支互斥 + 循环复用）
	DynamicAlloc      int // 动态分配估算
	FinalWithMargin   int // 加安全余量后
	Clamped           int // 边界限制后（最终值）
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
	Stmts        []Stmt
	// PoolCapacity 基于静态分析估算的 KMM V4 池容量（字节）
	// 0 表示使用默认值
	PoolCapacity int
	// PoolBreakdown 池容量分步计算明细（用于诊断）
	PoolBreakdown *PoolCapacityBreakdown
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
	result.Stmts = stmts
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
// 采用分步优化策略：
//   rawSum → sharingAdjusted → scopeTreeOptimized → +dynamicAlloc → +safetyMargin → clamp
func estimatePoolCapacityWithAST(result *FullAnalysisResult, astProgram *ast.Program) int {
	const (
		minCapacity    = 256 * 1024        // 256 KB
		maxCapacity    = 256 * 1024 * 1024 // 256 MB
		safetyMargin   = 1.5               // 50% 安全余量
		defaultDynSize = 1024              // 无法估算的动态分配默认 1KB
	)

	breakdown := &PoolCapacityBreakdown{}

	// ===== 步骤 1: 原始求和（SOR 追踪对象） =====
	rawSORTotal := 0
	for _, d := range result.Decisions {
		switch d.AllocKind {
		case AllocBumpPool, AllocArenaTiny, AllocArenaSmall, AllocArenaMedium:
			if size, ok := result.Sizes[d.ObjID]; ok && size > 0 {
				rawSORTotal += size
			} else {
				rawSORTotal += defaultDynSize
			}
		}
	}
	breakdown.RawSum = rawSORTotal

	// ===== 步骤 2: 指针共享调整 =====
	// release holder 与 source 共享同一数据，不重复计算
	sharingAdjusted := rawSORTotal
	if tracker := findTrackerFromResult(result); tracker != nil {
		pts := BuildPointsToSet(tracker)
		adjustment := pts.EstimatePoolAdjustment(result.Sizes)
		sharingAdjusted = rawSORTotal + adjustment
		if sharingAdjusted < 0 {
			sharingAdjusted = 0
		}
	}
	breakdown.SharingAdjusted = sharingAdjusted

	// ===== 步骤 3: 作用域树优化（分支互斥 + 循环复用） =====
	scopeTreeOptimized := sharingAdjusted
	if stmts := findStmtsFromResult(result); len(stmts) > 0 {
		if tracker := findTrackerFromResult(result); tracker != nil {
			sta := NewScopeTreeAnalyzer()
			sta.BuildScopeTree(stmts)
			scopeTreeOptimized = sta.EstimatePoolCapacity(result.Sizes, tracker)
			if scopeTreeOptimized <= 0 {
				scopeTreeOptimized = sharingAdjusted
			}
		}
	}
	breakdown.ScopeTreeOptimized = scopeTreeOptimized

	// ===== 步骤 4: 动态分配估算 =====
	dynTotal := 0
	if astProgram != nil {
		dynTotal = scanDynamicAllocations(astProgram)
	}
	breakdown.DynamicAlloc = dynTotal

	// ===== 步骤 5: 合计 + 安全余量 =====
	total := scopeTreeOptimized + dynTotal
	withMargin := int(float64(total) * safetyMargin)
	breakdown.FinalWithMargin = withMargin

	// ===== 步骤 6: 边界限制 =====
	clamped := withMargin
	if clamped < minCapacity {
		clamped = minCapacity
	}
	if clamped > maxCapacity {
		clamped = maxCapacity
	}
	breakdown.Clamped = clamped

	result.PoolBreakdown = breakdown
	return clamped
}

// findTrackerFromResult 从 FullAnalysisResult 中重建 OwnershipTracker
// 由于 result 中没有直接存储 tracker，我们通过 decisions 反向构建一个简化版
func findTrackerFromResult(result *FullAnalysisResult) *OwnershipTracker {
	if result == nil || len(result.Decisions) == 0 {
		return nil
	}
	tracker := NewOwnershipTracker()
	// 收集所有作用域
	scopeSet := make(map[int]bool)
	for _, d := range result.Decisions {
		if d == nil {
			continue
		}
		scopeSet[d.ScopeID] = true
	}
	// 按作用域 ID 排序并初始化 scopeObjects 和 scopeStack
	scopeIDs := make([]int, 0, len(scopeSet))
	for sid := range scopeSet {
		scopeIDs = append(scopeIDs, sid)
	}
	// 简单起见，把所有对象放在 scope 0
	if len(scopeIDs) == 0 {
		scopeIDs = append(scopeIDs, 0)
	}
	tracker.scopeObjects = make(map[int][]string)
	for _, sid := range scopeIDs {
		tracker.scopeObjects[sid] = make([]string, 0)
	}
	tracker.scopeStack = []int{0}
	tracker.currentScope = 0
	// 添加所有对象
	for _, d := range result.Decisions {
		if d == nil {
			continue
		}
		obj := &SORObject{
			ID:          d.ObjID,
			Name:        d.VarName,
			TypeName:    "",
			State:       d.FinalState,
			ScopeID:     d.ScopeID,
			IsComposite: d.IsComposite,
		}
		tracker.objects[d.ObjID] = obj
		// 添加到对应的作用域
		if _, ok := tracker.scopeObjects[d.ScopeID]; !ok {
			tracker.scopeObjects[d.ScopeID] = make([]string, 0)
		}
		tracker.scopeObjects[d.ScopeID] = append(tracker.scopeObjects[d.ScopeID], d.ObjID)
	}
	return tracker
}

// findStmtsFromResult 从 FullAnalysisResult 中获取语句列表
func findStmtsFromResult(result *FullAnalysisResult) []Stmt {
	if result == nil {
		return nil
	}
	return result.Stmts
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

// isNilStmt 检测语句接口是否为 nil（包括接口内包裹的 nil 指针）
func isNilStmt(stmt ast.Statement) bool {
	if stmt == nil {
		return true
	}
	v := reflect.ValueOf(stmt)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// scanDynamicAllocInStmt 递归扫描语句中的动态分配调用
func scanDynamicAllocInStmt(stmt ast.Statement) int {
	if isNilStmt(stmt) {
		return 0
	}
	total := 0
	switch s := stmt.(type) {
	case *ast.FunctionStatement:
		for _, bodyStmt := range s.Body {
			total += scanDynamicAllocInStmt(bodyStmt)
		}
	case *ast.VariableDeclaration:
		if s.Value != nil {
			total += scanDynamicAllocInExpr(s.Value)
		}
	case *ast.ExpressionStatement:
		if s.Expression != nil {
			total += scanDynamicAllocInExpr(s.Expression)
		}
	case *ast.IfStatement:
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
		if s.Condition != nil {
			total += scanDynamicAllocInExpr(s.Condition)
		}
		for _, bs := range s.Body {
			total += scanDynamicAllocInStmt(bs)
		}
	case *ast.ForStatement:
		if s.Condition != nil {
			total += scanDynamicAllocInExpr(s.Condition)
		}
		for _, bs := range s.Body {
			total += scanDynamicAllocInStmt(bs)
		}
	case *ast.BlockStatement:
		for _, bs := range s.Statements {
			total += scanDynamicAllocInStmt(bs)
		}
	case *ast.ReturnStatement:
		if s.Value != nil {
			total += scanDynamicAllocInExpr(s.Value)
		}
	}
	return total
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
	const identDefault = 512 // Identifier 的保守元素数量代表值
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		if e.Value > 0 {
			return int(e.Value)
		}
	case *ast.Identifier:
		// 变量实际值未知，给合理的保守代表值
		// 512 是典型的批量分配元素数量
		return identDefault
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
	// 修复 #23：同时输出 int 值，消除 codegen 侧字符串匹配的脆弱性
	decisionsArr := make([]interface{}, 0, len(result.Decisions))
	for _, d := range result.Decisions {
		dm := map[string]interface{}{
			"var_name":        d.VarName,
			"obj_id":          d.ObjID,
			"alloc_kind":      d.AllocKind.String(),
			"alloc_kind_id":   int(d.AllocKind),
			"drop_action":     d.DropAction.String(),
			"drop_action_id":  int(d.DropAction),
			"scope_id":        fmt.Sprintf("%d", d.ScopeID),
			"scope_id_int":    d.ScopeID,
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

	// 修复 #16：序列化 liveness 数据，供 codegen 用于 scope 拆分优化
	if result.Liveness != nil {
		livenessArr := make([]interface{}, 0)
		for _, info := range result.Liveness.GetAllLastUses() {
			lm := map[string]interface{}{
				"var_name":      info.VarName,
				"obj_id":        info.ObjID,
				"last_use_line": info.LastUseLine,
				"last_use_kind": info.LastUseKind,
				"is_yield_src":  info.IsYieldSrc,
				"is_extract_src": info.IsExtractSrc,
				"is_in_loop":    info.IsInLoop,
			}
			livenessArr = append(livenessArr, lm)
		}
		out["liveness"] = livenessArr
	}

	// 修复 #17：funcSigs 未被 codegen 消费，移除序列化以减少死数据
	// 跨函数所有权传递特性上线时再恢复

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