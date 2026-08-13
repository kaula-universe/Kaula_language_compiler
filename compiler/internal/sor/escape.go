package sor

import (
	"fmt"
	"sort"
	"strings"
)

// ============================================================================
// Pass 2.5c: 轻量逃逸分析
// ============================================================================

// EscapeLevel 逃逸级别
type EscapeLevel int

const (
	EscNone       EscapeLevel = iota // 不逃逸：仅在本作用域内使用
	EscArg                           // 作为函数参数传递
	EscReturn                        // 作为函数返回值逃逸
	EscCrossScope                    // 跨作用域引用
	EscGlobal                        // 全局逃逸
	EscHeap                          // 堆逃逸
)

func (e EscapeLevel) String() string {
	switch e {
	case EscNone:
		return "None"
	case EscArg:
		return "Arg"
	case EscReturn:
		return "Return"
	case EscCrossScope:
		return "CrossScope"
	case EscGlobal:
		return "Global"
	case EscHeap:
		return "Heap"
	default:
		return fmt.Sprintf("Unknown(%d)", int(e))
	}
}

// FlowEdge 数据流边
type FlowEdge struct {
	From string
	To   string
	Kind string
	Line int
}

// EscapeAnalyzer 轻量逃逸分析器
type EscapeAnalyzer struct {
	results   map[string]EscapeLevel
	flowEdges []FlowEdge
	// externOrigins 记录外部逃逸来源：变量名 -> 来源 extern 函数名。
	// extern（opaque C 函数）返回值指向外部不可追踪内存，
	// 沿数据流边（yield/release/extract）传播到所有别名。
	externOrigins map[string]string
	// warnings 外部逃逸源被移入 SOR 管理域时的浅提升警告（非阻断）
	warnings []SORError
	// warnKeys 警告去重键集合
	warnKeys map[string]bool
}

func NewEscapeAnalyzer() *EscapeAnalyzer {
	return &EscapeAnalyzer{
		results:       make(map[string]EscapeLevel),
		externOrigins: make(map[string]string),
		warnKeys:      make(map[string]bool),
	}
}

// AnalyzeEscape 执行轻量逃逸分析
func (ea *EscapeAnalyzer) AnalyzeEscape(stmts []Stmt) map[string]EscapeLevel {
	// 第一遍：初始化
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StmtLet:
			ea.ensureResult(stmt.VarName)
		case StmtYield:
			ea.ensureResult(stmt.SrcName)
			ea.ensureResult(stmt.VarName)
		case StmtRelease:
			ea.ensureResult(stmt.SrcName)
			for _, h := range stmt.HolderNames {
				ea.ensureResult(h)
			}
		case StmtExtract:
			ea.ensureResult(stmt.SrcName)
			ea.ensureResult(stmt.VarName)
		case StmtCall:
			for _, arg := range stmt.ArgNames {
				ea.ensureResult(arg)
			}
		}
	}

	// 第二遍：分析
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StmtLet:
			ea.analyzeExternBind(stmt)
		case StmtWrite:
			ea.analyzeExternBind(stmt)
		case StmtYield:
			ea.analyzeYield(stmt)
		case StmtRelease:
			ea.analyzeRelease(stmt)
		case StmtExtract:
			ea.analyzeExtract(stmt)
		case StmtCall:
			ea.analyzeCall(stmt)
		}
	}

	// 第三遍：传播
	ea.propagateEscape()

	return ea.results
}

func (ea *EscapeAnalyzer) ensureResult(id string) {
	if _, ok := ea.results[id]; !ok && id != "" {
		ea.results[id] = EscNone
	}
}

func (ea *EscapeAnalyzer) getLevel(id string) EscapeLevel {
	if l, ok := ea.results[id]; ok {
		return l
	}
	return EscNone
}

func (ea *EscapeAnalyzer) setLevel(id string, level EscapeLevel) {
	if current := ea.getLevel(id); level > current {
		ea.results[id] = level
	}
}

func (ea *EscapeAnalyzer) promoteEscape(current, new EscapeLevel) EscapeLevel {
	if new > current {
		return new
	}
	return current
}

func (ea *EscapeAnalyzer) analyzeYield(stmt Stmt) {
	ea.flowEdges = append(ea.flowEdges, FlowEdge{
		From: stmt.SrcName, To: stmt.VarName, Kind: "yield", Line: stmt.Line,
	})
	dstLevel := ea.getLevel(stmt.VarName)
	srcLevel := ea.getLevel(stmt.SrcName)
	ea.setLevel(stmt.VarName, ea.promoteEscape(dstLevel, srcLevel))
	// 外部逃逸源经 yield 移入 SOR 管理域：警告 + 来源传播
	ea.checkExternSink(stmt.SrcName, stmt.VarName, "yield", stmt.Line)
}

func (ea *EscapeAnalyzer) analyzeRelease(stmt Stmt) {
	srcLevel := ea.getLevel(stmt.SrcName)
	for _, h := range stmt.HolderNames {
		ea.flowEdges = append(ea.flowEdges, FlowEdge{
			From: stmt.SrcName, To: h, Kind: "release", Line: stmt.Line,
		})
		ea.setLevel(h, ea.promoteEscape(ea.getLevel(h), srcLevel))
		// 外部逃逸源经 release 分发给多持有者：警告 + 来源传播
		ea.checkExternSink(stmt.SrcName, h, "release", stmt.Line)
	}
}

func (ea *EscapeAnalyzer) analyzeExtract(stmt Stmt) {
	srcLevel := ea.getLevel(stmt.SrcName)
	ea.setLevel(stmt.VarName, ea.promoteEscape(ea.getLevel(stmt.VarName), srcLevel))
	// 从外部逃逸源提取的字段同样指向外部内存：警告 + 来源传播
	ea.checkExternSink(stmt.SrcName, stmt.VarName, "extract", stmt.Line)
}

func (ea *EscapeAnalyzer) analyzeCall(stmt Stmt) {
	for _, arg := range stmt.ArgNames {
		ea.setLevel(arg, ea.promoteEscape(ea.getLevel(arg), EscArg))
		if stmt.FuncName != "" {
			// 已知函数，Arg 级别足够
		} else {
			// 未知函数，保守标记为堆逃逸
			ea.setLevel(arg, EscHeap)
		}
	}
}

func (ea *EscapeAnalyzer) propagateEscape() {
	// 不动点迭代：沿数据流边传播逃逸级别与外部逃逸来源
	for i := 0; i < 100; i++ {
		changed := false
		for _, edge := range ea.flowEdges {
			fromLevel := ea.getLevel(edge.From)
			toLevel := ea.getLevel(edge.To)
			newToLevel := ea.promoteEscape(toLevel, fromLevel)
			if newToLevel > toLevel {
				ea.setLevel(edge.To, newToLevel)
				changed = true
			}
			// 外部来源沿别名边传播（extract/yield/release 的字段继承外部性）
			if fn, ok := ea.externOrigins[edge.From]; ok {
				if _, exists := ea.externOrigins[edge.To]; !exists {
					ea.externOrigins[edge.To] = fn
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
}

// ============================================================================
// 外部逃逸来源（extern / opaque C 函数返回值）
// ============================================================================

// analyzeExternBind 处理来自 extern 函数调用的变量绑定（let/赋值）。
// 将其标记为外部逃逸来源，并提升到 EscReturn 级别：
// EscapeReturn 语义扩展到 opaque 外部函数——返回值视为“指向外部不可追踪内存”，
// 已超出 SOR 作用域段的生命周期管辖，任何跨作用域使用都必须走显式路径。
// EscReturn 同时会经 EscapeForcesAlloc 强制 bump pool 分配，避免外壳被作用域回卷销毁。
func (ea *EscapeAnalyzer) analyzeExternBind(stmt Stmt) {
	if !stmt.IsExternCall || stmt.VarName == "" {
		return
	}
	ea.ensureResult(stmt.VarName)
	ea.externOrigins[stmt.VarName] = stmt.FuncName
	ea.setLevel(stmt.VarName, EscReturn)
}

// checkExternSink 当外部逃逸源（或其别名）经 yield/release/extract
// 移入 SOR 管理域时，记录浅提升警告并将来源传播给目标。
// 背景（运行时代码 "Bug 2"）：kmm_v4_promote 仅浅拷贝外壳，
// 外壳内指向外部分配的指针不受 SOR 追踪，浅提升后行为不可验证，
// 必须显式告知用户而非静默产生悬垂。
func (ea *EscapeAnalyzer) checkExternSink(srcName, dstName, op string, line int) {
	fn, ok := ea.externOrigins[srcName]
	if !ok {
		return
	}
	// 来源传播：目标继承外部性（传播阶段也会沿流边补齐，此处直接覆盖 extract 等即时别名）
	if dstName != "" {
		if _, exists := ea.externOrigins[dstName]; !exists {
			ea.externOrigins[dstName] = fn
		}
	}
	key := fmt.Sprintf("%s|%s|%s|%d", srcName, dstName, op, line)
	if ea.warnKeys[key] {
		return
	}
	ea.warnKeys[key] = true
	ea.warnings = append(ea.warnings, SORError{
		Kind:       ErrExternShallowPromote,
		SourceLine: line,
		ObjectID:   srcName,
		Message: fmt.Sprintf("'%s' 来自 extern 函数 '%s' 的返回值（外部分配，对 SOR 不透明），"+
			"经 %s 移入 SOR 管理域：跨作用域提升（promote）仅浅拷贝外壳，"+
			"内部指向外部内存的指针不受 SOR 追踪，可能悬垂", srcName, fn, op),
		Details: "替代方案：为该函数所在调用链标记 #[no_kmm] 跳过作用域托管，" +
			"或改用显式全局分配 API（如 std_malloc）自行管理生命周期",
	})
}

// GetExternOrigins 返回外部逃逸来源表（变量名 -> extern 函数名）
func (ea *EscapeAnalyzer) GetExternOrigins() map[string]string {
	return ea.externOrigins
}

// GetWarnings 返回外部逃逸源浅提升警告列表
func (ea *EscapeAnalyzer) GetWarnings() []SORError {
	return ea.warnings
}

// FormatExternEscapeWarnings 格式化外部逃逸警告摘要（KAULA_SOR_DUMP 用）
func FormatExternEscapeWarnings(warnings []SORError) string {
	if len(warnings) == 0 {
		return "(no extern-escape warnings)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== Extern Escape Warnings (%d) ===\n", len(warnings)))
	for _, w := range warnings {
		b.WriteString(fmt.Sprintf("  line %d [%s] %s\n", w.SourceLine, w.Kind, w.Message))
		if w.Details != "" {
			b.WriteString(fmt.Sprintf("      %s\n", w.Details))
		}
	}
	return b.String()
}

// GetFlowEdges 返回所有数据流边
func (ea *EscapeAnalyzer) GetFlowEdges() []FlowEdge {
	return ea.flowEdges
}

// EscapeToAlloc 将逃逸级别转换为分配策略
// - EscNone: 不逃逸，优先栈分配（大小路由可进一步调整）
// - EscArg / EscCrossScope: 局部逃逸，可走 arena（由大小路由决定具体级别）
// - EscReturn / EscGlobal / EscHeap: 全局/堆逃逸，必须走 bump pool（生命周期跨作用域）
func EscapeToAlloc(level EscapeLevel) AllocKind {
	switch level {
	case EscNone:
		return AllocStack
	case EscArg, EscCrossScope:
		return AllocArenaSmall
	default:
		return AllocBumpPool
	}
}

// EscapeForcesAlloc 判断逃逸级别是否强制覆盖大小路由结果
// 低级别逃逸（EscArg, EscCrossScope）仍可由大小路由选择合适的 arena
// 高级别逃逸（EscReturn, EscGlobal, EscHeap）必须走 bump pool
func EscapeForcesAlloc(level EscapeLevel) bool {
	return level >= EscReturn
}

// FormatEscapeSummary 格式化逃逸分析结果
func FormatEscapeSummary(results map[string]EscapeLevel) string {
	if len(results) == 0 {
		return "(no escape results)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== Escape Analysis (%d objects) ===\n", len(results)))
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b.WriteString(fmt.Sprintf("  %s: %s\n", id, results[id]))
	}
	return b.String()
}
