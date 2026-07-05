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
	EscArg                          // 作为函数参数传递
	EscReturn                       // 作为函数返回值逃逸
	EscCrossScope                   // 跨作用域引用
	EscGlobal                       // 全局逃逸
	EscHeap                         // 堆逃逸
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
}

func NewEscapeAnalyzer() *EscapeAnalyzer {
	return &EscapeAnalyzer{
		results: make(map[string]EscapeLevel),
	}
}

// AnalyzeEscape 执行轻量逃逸分析
func (ea *EscapeAnalyzer) AnalyzeEscape(stmts []Stmt) map[string]EscapeLevel {
	// 第一遍：初始化
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StmtYeide:
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
		case StmtYeide:
			ea.analyzeYeide(stmt)
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

func (ea *EscapeAnalyzer) analyzeYeide(stmt Stmt) {
	ea.flowEdges = append(ea.flowEdges, FlowEdge{
		From: stmt.SrcName, To: stmt.VarName, Kind: "yeide", Line: stmt.Line,
	})
	dstLevel := ea.getLevel(stmt.VarName)
	srcLevel := ea.getLevel(stmt.SrcName)
	ea.setLevel(stmt.VarName, ea.promoteEscape(dstLevel, srcLevel))
}

func (ea *EscapeAnalyzer) analyzeRelease(stmt Stmt) {
	srcLevel := ea.getLevel(stmt.SrcName)
	for _, h := range stmt.HolderNames {
		ea.flowEdges = append(ea.flowEdges, FlowEdge{
			From: stmt.SrcName, To: h, Kind: "release", Line: stmt.Line,
		})
		ea.setLevel(h, ea.promoteEscape(ea.getLevel(h), srcLevel))
	}
}

func (ea *EscapeAnalyzer) analyzeExtract(stmt Stmt) {
	srcLevel := ea.getLevel(stmt.SrcName)
	ea.setLevel(stmt.VarName, ea.promoteEscape(ea.getLevel(stmt.VarName), srcLevel))
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
	// 不动点迭代：沿数据流边传播逃逸级别
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
		}
		if !changed {
			break
		}
	}
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
