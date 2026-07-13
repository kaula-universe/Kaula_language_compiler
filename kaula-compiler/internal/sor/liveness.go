package sor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ============================================================================
// Pass 2.5d: 活跃性分析（Liveness Analysis）
// 精确确定每个变量的最后使用点，用于真正精确的释放点确定。
// ============================================================================

// LastUseInfo 记录变量的最后使用信息
type LastUseInfo struct {
	VarName      string   // 变量名
	ObjID        string   // 关联的 SOR 对象 ID（使用 VarName 代替，因为 Stmt 无 ObjID 字段）
	LastUseLine  int      // 最后一次使用的行号
	LastUseKind  string   // 使用类型: "read", "yield-src", "release-src", "extract-src", "call-arg"
	IsYieldSrc   bool     // 是否作为 yield 源（转移后失效，无需释放）
	IsExtractSrc bool     // 是否作为 extract 源（部分失效，hollow 清理）
	IsInLoop     bool     // 是否在循环体内声明（用于循环感知的池容量计算）
}

func (info *LastUseInfo) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s [obj=%s line=%d kind=%s",
		info.VarName, info.ObjID, info.LastUseLine, info.LastUseKind))
	if info.IsYieldSrc {
		b.WriteString(" yield-src")
	}
	if info.IsExtractSrc {
		b.WriteString(" extract-src")
	}
	if info.IsInLoop {
		b.WriteString(" in-loop")
	}
	b.WriteString("]")
	return b.String()
}

// LivenessResult 活跃性分析结果
type LivenessResult struct {
	lastUses     map[string]*LastUseInfo // key = varName, 每个变量的最后使用信息
	scopeLastUse map[int][]string        // key = scopeID, value = 该作用域中需要释放的变量列表
}

// GetLastUse 获取指定变量的最后使用信息
func (lr *LivenessResult) GetLastUse(varName string) *LastUseInfo {
	if lr == nil || lr.lastUses == nil {
		return nil
	}
	return lr.lastUses[varName]
}

// GetScopeDropVars 获取指定作用域中需要释放的变量列表
func (lr *LivenessResult) GetScopeDropVars(scopeID int) []string {
	if lr == nil || lr.scopeLastUse == nil {
		return nil
	}
	return lr.scopeLastUse[scopeID]
}

// GetAllLastUses 返回所有最后使用信息（按变量名排序）
func (lr *LivenessResult) GetAllLastUses() []*LastUseInfo {
	if lr == nil || lr.lastUses == nil {
		return nil
	}
	result := make([]*LastUseInfo, 0, len(lr.lastUses))
	names := make([]string, 0, len(lr.lastUses))
	for name := range lr.lastUses {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result = append(result, lr.lastUses[name])
	}
	return result
}

// FormatLivenessSummary 格式化活跃性分析摘要
func (lr *LivenessResult) FormatLivenessSummary() string {
	if lr == nil || len(lr.lastUses) == 0 {
		return "(no liveness info)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== Liveness Analysis (%d variables tracked) ===\n", len(lr.lastUses)))
	for _, info := range lr.GetAllLastUses() {
		b.WriteString(fmt.Sprintf("  %s\n", info.String()))
	}
	if len(lr.scopeLastUse) > 0 {
		b.WriteString(fmt.Sprintf("\n=== Scope Drop Points (%d scopes) ===\n", len(lr.scopeLastUse)))
		scopes := make([]int, 0, len(lr.scopeLastUse))
		for sid := range lr.scopeLastUse {
			scopes = append(scopes, sid)
		}
		sort.Ints(scopes)
		for _, sid := range scopes {
			vars := lr.scopeLastUse[sid]
			b.WriteString(fmt.Sprintf("  scope %d: drop [%s]\n", sid, strings.Join(vars, ", ")))
		}
	}
	return b.String()
}

// ============================================================================
// LivenessAnalyzer 活跃性分析器
// ============================================================================

// LivenessAnalyzer 活跃性分析器
// 遍历所有 Stmt，记录每个变量的最后使用位置。
type LivenessAnalyzer struct {
	lastUses     map[string]*LastUseInfo // varName -> 最后使用信息
	scopeObjects map[int][]string        // scopeID -> 在此作用域中创建的对象名
	scopeStack   []int                   // 当前作用域嵌套栈
	loopDepth    int                     // 当前循环嵌套深度
	loopVars     map[int][]string        // loopDepth -> 在该循环层级内声明的变量
}

// NewLivenessAnalyzer 创建活跃性分析器
func NewLivenessAnalyzer() *LivenessAnalyzer {
	return &LivenessAnalyzer{
		lastUses:     make(map[string]*LastUseInfo),
		scopeObjects: make(map[int][]string),
		scopeStack:   make([]int, 0, 8),
		loopDepth:    0,
		loopVars:     make(map[int][]string),
	}
}

// currentScope 返回当前所在的作用域 ID
func (la *LivenessAnalyzer) currentScope() int {
	if len(la.scopeStack) == 0 {
		return 0
	}
	return la.scopeStack[len(la.scopeStack)-1]
}

// recordUse 记录变量的一次使用，更新最后使用信息
func (la *LivenessAnalyzer) recordUse(varName, objID string, line int, kind string) {
	if varName == "" {
		return
	}
	info, exists := la.lastUses[varName]
	if !exists {
		info = &LastUseInfo{
			VarName: varName,
			ObjID:   objID,
		}
		la.lastUses[varName] = info
	}
	info.LastUseLine = line
	info.LastUseKind = kind
}

// recordYieldSrc 记录变量作为 yield 源的使用（转移后失效）
func (la *LivenessAnalyzer) recordYieldSrc(varName, objID string, line int) {
	la.recordUse(varName, objID, line, "yield-src")
	if info, ok := la.lastUses[varName]; ok {
		info.IsYieldSrc = true
	}
}

// recordExtractSrc 记录变量作为 extract 源的使用（部分失效）
func (la *LivenessAnalyzer) recordExtractSrc(varName, objID string, line int) {
	la.recordUse(varName, objID, line, "extract-src")
	if info, ok := la.lastUses[varName]; ok {
		info.IsExtractSrc = true
	}
}

// addScopeObject 将对象名注册到当前作用域
func (la *LivenessAnalyzer) addScopeObject(varName string) {
	scopeID := la.currentScope()
	la.scopeObjects[scopeID] = append(la.scopeObjects[scopeID], varName)
}

// pushScope 进入新作用域
func (la *LivenessAnalyzer) pushScope(scopeID int) {
	la.scopeStack = append(la.scopeStack, scopeID)
}

// popScope 退出当前作用域
func (la *LivenessAnalyzer) popScope() {
	if len(la.scopeStack) > 0 {
		la.scopeStack = la.scopeStack[:len(la.scopeStack)-1]
	}
}

// parseScopeNameToID 从 ScopeName 解析作用域 ID。
// ScopeName 通常为 "scope_N" 格式，解析失败返回 0。
func parseScopeNameToID(scopeName string) int {
	if scopeName == "" {
		return 0
	}
	// 尝试从 "scope_N" 格式解析
	if strings.HasPrefix(scopeName, "scope_") {
		idStr := scopeName[len("scope_"):]
		id, err := strconv.Atoi(idStr)
		if err == nil {
			return id
		}
	}
	// 尝试直接解析为整数
	id, err := strconv.Atoi(scopeName)
	if err == nil {
		return id
	}
	return 0
}

// AnalyzeLiveness 执行活跃性分析
// 遍历所有 Stmt，记录每个变量的最后使用位置。
// 分析完成后返回 LivenessResult，包含每个变量的最后使用信息
// 以及每个作用域中需要释放的变量列表。
func (la *LivenessAnalyzer) AnalyzeLiveness(stmts []Stmt) *LivenessResult {
	if len(stmts) == 0 {
		return &LivenessResult{
			lastUses:     la.lastUses,
			scopeLastUse: make(map[int][]string),
		}
	}

	// 正序遍历所有语句，后面的使用会覆盖前面的记录
	for _, stmt := range stmts {
		la.analyzeStmt(stmt)
	}

	// 构建结果
	result := &LivenessResult{
		lastUses:     la.lastUses,
		scopeLastUse: make(map[int][]string),
	}

	// 为每个作用域计算需要释放的变量
	for scopeID, vars := range la.scopeObjects {
		dropVars := make([]string, 0)
		for _, varName := range vars {
			info, ok := la.lastUses[varName]
			if !ok {
				continue
			}
			// yield 后失效的变量无需释放
			if info.IsYieldSrc {
				continue
			}
			// 状态为 Moved 的变量无需释放（所有权已转移）
			dropVars = append(dropVars, varName)
		}
		if len(dropVars) > 0 {
			sort.Strings(dropVars)
			result.scopeLastUse[scopeID] = dropVars
		}
	}

	return result
}

// analyzeStmt 分析单条语句
func (la *LivenessAnalyzer) analyzeStmt(stmt Stmt) {
	switch stmt.Kind {

	case StmtLoopEnter:
		// 进入循环，记录循环深度
		la.loopDepth++
		la.loopVars[la.loopDepth] = make([]string, 0)

	case StmtLoopExit:
		// 退出循环
		if la.loopDepth > 0 {
			delete(la.loopVars, la.loopDepth)
			la.loopDepth--
		}

	case StmtBranchEnter, StmtBranchElse, StmtBranchExit:
		// 分支标记，不影响活跃性分析

	case StmtLet:
		// 变量创建：注册到当前作用域
		la.addScopeObject(stmt.VarName)
		la.recordUse(stmt.VarName, stmt.VarName, stmt.Line, "let")
		// 如果在循环体内，记录为循环变量并标记 IsInLoop
		if la.loopDepth > 0 {
			la.loopVars[la.loopDepth] = append(la.loopVars[la.loopDepth], stmt.VarName)
			if info, ok := la.lastUses[stmt.VarName]; ok {
				info.IsInLoop = true
			}
		}

	case StmtYield:
		// yield: SrcName 转移所有权给 VarName（目标）
		// SrcName: 最后使用 = yield 行（转移后失效）
		if stmt.SrcName != "" {
			la.recordYieldSrc(stmt.SrcName, stmt.SrcName, stmt.Line)
		}
		// VarName: 在此获得所有权
		if stmt.VarName != "" {
			la.addScopeObject(stmt.VarName)
			la.recordUse(stmt.VarName, stmt.VarName, stmt.Line, "yield-dst")
		}

	case StmtRelease:
		// release: SrcName 分发只读引用给 HolderNames
		// SrcName: 最后使用 = release 行（分发后只读）
		if stmt.SrcName != "" {
			la.recordUse(stmt.SrcName, stmt.SrcName, stmt.Line, "release-src")
		}
		// holders: 在此获得只读引用
		for _, holder := range stmt.HolderNames {
			la.addScopeObject(holder)
			la.recordUse(holder, stmt.SrcName, stmt.Line, "release-holder")
		}

	case StmtExtract:
		// extract: SrcName 的部分内容被提取到 VarName（elem）
		// SrcName: 最后使用可能不是 extract（extract 后 SrcName 变为 hollow 但仍存在）
		//       所以这里不覆盖 SrcName 的最后使用
		if stmt.SrcName != "" {
			la.recordExtractSrc(stmt.SrcName, stmt.SrcName, stmt.Line)
		}
		// VarName: 在此获得提取的所有权
		if stmt.VarName != "" {
			la.addScopeObject(stmt.VarName)
			la.recordUse(stmt.VarName, stmt.VarName, stmt.Line, "extract-elem")
		}

	case StmtRead:
		// read: 变量被读取
		if stmt.VarName != "" {
			la.recordUse(stmt.VarName, stmt.VarName, stmt.Line, "read")
		}

	case StmtWrite:
		// write: 变量被写入
		if stmt.VarName != "" {
			la.recordUse(stmt.VarName, stmt.VarName, stmt.Line, "write")
		}

	case StmtCall:
		// call: 函数调用
		// owned 参数: 最后使用 = call 行（函数消费所有权）
		for i, arg := range stmt.ArgNames {
			ownership := "owned" // 保守默认
			if i < len(stmt.ArgOwnership) {
				ownership = stmt.ArgOwnership[i]
			}
			if ownership == "owned" {
				la.recordUse(arg, arg, stmt.Line, "call-arg")
			}
		}
		// borrow 参数不改变所有权，不记录为最后使用

	case StmtScopeEnter:
		// 进入新作用域
		scopeID := parseScopeNameToID(stmt.ScopeName)
		if scopeID > 0 {
			la.pushScope(scopeID)
		}

	case StmtScopeExit:
		// 退出当前作用域
		// 计算此作用域中每个 Owned 变量的释放点
		scopeID := parseScopeNameToID(stmt.ScopeName)
		if scopeID <= 0 {
			scopeID = la.currentScope()
		}
		la.popScope()

	default:
		// 其他语句类型，不处理活跃性
	}
}

// ============================================================================
// 释放点计算
// ============================================================================

// ComputeDropPoints 计算每个作用域中需要插入释放代码的位置。
// 对于 KMM bump pool 模型，释放点就是作用域退出。
// 但对于 hollow 清理和特殊释放，需要精确的插入点。
//
// 返回: map[int][]string — key = scopeID, value = 该作用域中需要在退出时处理的变量名
func (la *LivenessAnalyzer) ComputeDropPoints(result *LivenessResult) map[int][]string {
	if result == nil || result.scopeLastUse == nil {
		return nil
	}

	dropPoints := make(map[int][]string)
	for scopeID, vars := range result.scopeLastUse {
		if len(vars) == 0 {
			continue
		}
		// 按 LastUseLine 排序，方便代码生成时按顺序插入
		sorted := make([]string, len(vars))
		copy(sorted, vars)
		sort.Strings(sorted)
		dropPoints[scopeID] = sorted
	}
	return dropPoints
}

// ============================================================================
// 与 CodeGen 的接口
// ============================================================================

// GenerateScopeDrops 为指定作用域生成 drop 代码。
// 对于 bump pool: 无需逐变量释放（KMM_V4_SCOPE_END 批量回收）。
// 但需要为 hollow 变量生成标记清理。
// 对于 yield 后失效: 无需释放。
//
// 参数:
//   - liveness: 活跃性分析结果
//   - scopeID: 目标作用域 ID
//   - decisions: 内存管理决策列表（来自 MemoryAnalyzer）
//   - indent: 代码缩进
//
// 返回: 生成的 drop 代码字符串
func GenerateScopeDrops(liveness *LivenessResult, scopeID int, decisions []*MemoryDecision, indent string) string {
	if liveness == nil {
		return ""
	}

	var b strings.Builder
	dropVars := liveness.GetScopeDropVars(scopeID)
	if len(dropVars) == 0 {
		return ""
	}

	// 构建决策查找表
	decisionMap := make(map[string]*MemoryDecision)
	for _, d := range decisions {
		if d != nil {
			decisionMap[d.VarName] = d
		}
	}

	for _, varName := range dropVars {
		info := liveness.GetLastUse(varName)
		if info == nil {
			continue
		}

		d := decisionMap[varName]

		// yield 后失效的变量无需释放
		if info.IsYieldSrc {
			continue
		}

		// 根据 DropAction 决定生成何种代码
		if d != nil {
			switch d.DropAction {
			case DropHollow:
				// hollow 变量：释放残留外壳
				if d.IsComposite && len(d.ExtractedChildren) > 0 {
					b.WriteString(indent)
					b.WriteString(fmt.Sprintf("/* hollow cleanup: %s — extracted children: ", varName))
					children := make([]string, 0, len(d.ExtractedChildren))
					for path := range d.ExtractedChildren {
						children = append(children, path)
					}
					sort.Strings(children)
					b.WriteString(strings.Join(children, ", "))
					b.WriteString(" */\n")
				}

			case DropScopeEnd:
				// 作用域退出释放
				if d.FinalState == StateReleased {
					// 已 release 的变量：所有消费者退出，引用计数归零
					b.WriteString(indent)
					b.WriteString(fmt.Sprintf("/* scope-end release: %s — all consumers exited */\n", varName))
				} else if d.FinalState == StateOwned {
					// 仍持有的 Owned 变量：作用域退出时释放
					b.WriteString(indent)
					b.WriteString(fmt.Sprintf("/* scope-end drop: %s [obj=%s] — last use at line %d (%s) */\n",
						varName, info.ObjID, info.LastUseLine, info.LastUseKind))
				}

			case DropNone:
				// 无需释放（已 yield / 已 extract / 已 moved）
				continue
			}
		} else {
			// 无决策信息的变量：默认生成注释标记
			b.WriteString(indent)
			b.WriteString(fmt.Sprintf("/* drop: %s — last use at line %d (%s) */\n",
				varName, info.LastUseLine, info.LastUseKind))
		}
	}

	return b.String()
}

// ============================================================================
// SORAnalyzer 扩展
// ============================================================================

// GetAllStmtIDs 返回分析器中所有语句涉及的对象 ID 集合。
// 用于 LivenessAnalyzer 确定需要追踪的对象范围。
// 注意：Stmt 没有 ObjID 字段，此处收集 VarName 和 SrcName 作为标识。
func (a *SORAnalyzer) GetAllStmtIDs() []string {
	if a == nil || a.stmts == nil {
		return nil
	}
	idSet := make(map[string]struct{})
	for _, stmt := range a.stmts {
		if stmt.VarName != "" {
			idSet[stmt.VarName] = struct{}{}
		}
		if stmt.SrcName != "" {
			idSet[stmt.SrcName] = struct{}{}
		}
		for _, h := range stmt.HolderNames {
			if h != "" {
				idSet[h] = struct{}{}
			}
		}
		for _, arg := range stmt.ArgNames {
			if arg != "" {
				idSet[arg] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ============================================================================
// 扩展入口
// ============================================================================

// AnalyzeASTWithLiveness 扩展版分析，串联 SOR 安全分析 + 内存分析 + 活跃性分析。
// 返回 SOR 错误、内存决策、活跃性结果和执行日志。
func AnalyzeASTWithLiveness(program interface {
	GetStmts() []Stmt
}) ([]SORError, []*MemoryDecision, *LivenessResult, []string) {
	analyzer := NewSORAnalyzer()
	stmts := program.GetStmts()
	sorErrors := analyzer.Analyze(stmts)

	// 内存决策分析
	ma := NewMemoryAnalyzer()
	decisions := ma.AnalyzeMemory(analyzer.GetTracker())

	// 活跃性分析
	la := NewLivenessAnalyzer()
	liveness := la.AnalyzeLiveness(stmts)

	// 计算释放点
	la.ComputeDropPoints(liveness)

	return sorErrors, decisions, liveness, analyzer.GetExecLog()
}
