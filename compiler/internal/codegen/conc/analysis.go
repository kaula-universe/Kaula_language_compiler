// Kaula 并发内核 · conc fn lowering 的静态分析
//
// 在生成 C 代码之前必须先知道两件事：
//  1. 函数体里哪些调用是"可插桩 IO"（suspend 点）
//  2. 哪些局部变量跨 suspend 点存活（要提升到 Frame 字段）
//
// 这两个问题的答案决定 Frame 的形状与 resume 的状态划分。
// 分析错了的后果不是编译失败，而是运行期的数据竞争或状态机错乱，
// 所以这里一律保守：无法确定的调用当作可能 suspend。
package conc

import (
	"kaula/internal/ast"
)

// SuspendKind 是挂起的原因
type SuspendKind int

const (
	// SuspendNone 不是挂起点
	SuspendNone SuspendKind = iota
	// SuspendIO 是可插桩 IO：提交 IoDriver，Worker 立即转做别的事
	SuspendIO
	// SuspendAwait 是等待另一个任务完成
	SuspendAwait
	// SuspendSleep 是定时器
	SuspendSleep
)

func (k SuspendKind) String() string {
	switch k {
	case SuspendIO:
		return "IO"
	case SuspendAwait:
		return "await"
	case SuspendSleep:
		return "sleep"
	default:
		return "none"
	}
}

// SuspendSite 描述一个挂起位置
type SuspendSite struct {
	// Call 是触发挂起的调用表达式
	Call ast.Expression
	// Kind 是挂起类型
	Kind SuspendKind
	// Line 是源码行号，用于诊断
	Line int
}

// VarInfo 是一个局部变量的跨 suspend 存活信息
type VarInfo struct {
	Name string
	// DeclLine 是声明行
	DeclLine int
	// Initializer 是初始化表达式；挂起前求值
	Initializer ast.Expression
}

// BodyAnalysis 是函数体的分析结果
type BodyAnalysis struct {
	// SuspendSites 按源码顺序排列的挂起点
	SuspendSites []SuspendSite
	// FrameVars 必须进入 Frame 的变量
	FrameVars []VarInfo
	// NeedsSuspend 为假时，编译器可生成"单一形态"：
	// 既不需要 pc 字段，也不需要 resume 的 switch（文档 §2.7）
	NeedsSuspend bool
}

// ========== 可插桩函数注册表 ==========

// InstrumentationTable 是"哪些调用可插桩"的注册表。
//
// 运行时通过它把 Kaula 层的 IO 映射到 KaulaIoRequest；
// 编译器在 lowering 时查同一张表。两侧必须一致 —— 这是文档 §9.1 的 ABI 约定。
type InstrumentationTable struct {
	entries map[string]SuspendKind
}

var instrumentTable = &InstrumentationTable{
	entries: map[string]SuspendKind{
		// 网络
		"net_connect": SuspendIO,
		"net_accept":  SuspendIO,
		"net_recv":    SuspendIO,
		"net_send":    SuspendIO,
		"net_read":    SuspendIO,
		"net_write":   SuspendIO,
		"web_get":     SuspendIO,
		"web_post":    SuspendIO,
		// 文件
		"fs_read":  SuspendIO,
		"fs_write": SuspendIO,
		// 进程
		"subprocess_wait": SuspendIO,
		// 定时
		"time_sleep": SuspendSleep,
		"time_after": SuspendSleep,
		// 并发原语
		"handle_await": SuspendAwait,
		"future_await": SuspendAwait,
	},
}

// Register 登记一个可插桩函数（供 stdlib 扩展）
func Register(name string, kind SuspendKind) {
	instrumentTable.entries[name] = kind
}

// Lookup 查询一个调用是否可插桩
func Lookup(name string) SuspendKind {
	if k, ok := instrumentTable.entries[name]; ok {
		return k
	}
	return SuspendNone
}

// blockingHints 是 #[hint(blocking)] 标记的函数：不可插桩，走阻塞车道
var blockingHints = map[string]bool{}

// MarkBlocking 登记一个已知阻塞的函数
func MarkBlocking(name string) {
	blockingHints[name] = true
}

// IsBlocking 判断是否被标记为阻塞
func IsBlocking(name string) bool {
	return blockingHints[name]
}

// ========== 分析 ==========

// AnalyzeBody 分析函数体，找出挂起点与需要进入 Frame 的变量。
//
// 保守策略：一旦函数体含挂起点，就把所有已声明的局部变量提升到 Frame。
// 多几个字段只多几十字节；而漏掉一个就是跨 suspend 读到栈上的失效值（UAF）。
func AnalyzeBody(body []ast.Statement) *BodyAnalysis {
	res := &BodyAnalysis{}

	decls := collectDecls(body)
	findSuspends(body, res)

	res.NeedsSuspend = len(res.SuspendSites) > 0

	if res.NeedsSuspend {
		for _, d := range decls {
			res.FrameVars = append(res.FrameVars, d)
		}
	}
	return res
}

// declSet 是变量声明的有序集合
type declSet struct {
	list []VarInfo
	seen map[string]bool
}

func (d *declSet) add(v VarInfo) {
	if d.seen == nil {
		d.seen = make(map[string]bool)
	}
	if d.seen[v.Name] {
		return
	}
	d.seen[v.Name] = true
	d.list = append(d.list, v)
}

func collectDecls(body []ast.Statement) []VarInfo {
	var out []VarInfo
	seen := make(map[string]bool)

	add := func(name string, line int, init ast.Expression) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, VarInfo{Name: name, DeclLine: line, Initializer: init})
	}

	var walk func(stmts []ast.Statement)
	walk = func(stmts []ast.Statement) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.VariableDeclaration:
				add(s.Name, s.Pos.Line, s.Value)
			case *ast.BlockStatement:
				walk(s.Statements)
			case *ast.IfStatement:
				walk(s.Body)
				walk(s.Else)
			case *ast.WhileStatement:
				walk(s.Body)
			case *ast.ForStatement:
				if vd, ok := s.Init.(*ast.VariableDeclaration); ok {
					add(vd.Name, vd.Pos.Line, vd.Value)
				}
				walk(s.Body)
			case *ast.ForInStatement:
				if s.Variable != nil {
					add(s.Variable.Name, s.Pos.Line, nil)
				}
				walk(s.Body)
			case *ast.CallStatement:
				walk(s.Body)
			}
		}
	}
	walk(body)
	return out
}

// findSuspends 遍历语句，记录挂起点
func findSuspends(body []ast.Statement, res *BodyAnalysis) {
	var walk func(stmts []ast.Statement)
	walk = func(stmts []ast.Statement) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.VariableDeclaration:
				// 初始化表达式里可能有挂起
				recordSuspends(s.Value, res)
			case *ast.ExpressionStatement:
				recordSuspends(s.Expression, res)
			case *ast.ReturnStatement:
				recordSuspends(s.Value, res)
			case *ast.BlockStatement:
				walk(s.Statements)
			case *ast.IfStatement:
				recordSuspends(s.Condition, res)
				walk(s.Body)
				walk(s.Else)
			case *ast.WhileStatement:
				recordSuspends(s.Condition, res)
				walk(s.Body)
			case *ast.ForStatement:
				recordSuspends(s.Condition, res)
				walk(s.Body)
			case *ast.ForInStatement:
				walk(s.Body)
			case *ast.CallStatement:
				walk(s.Body)
			case *ast.YieldStatement:
				recordSuspends(s.Source, res)
			}
		}
	}
	walk(body)
}

// matchArmsWalkable 报告 MatchExpression 的分支体是否能作为语句序列遍历。
// MatchExpression 是表达式而非语句，只能出现在表达式位置，
// 因此语句遍历里走不到它；此函数为将来 match-as-statement 预留。
func matchArmsWalkable() bool { return false }

func recordSuspends(expr ast.Expression, res *BodyAnalysis) {
	if expr == nil {
		return
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		name := CalleeName(call)
		if name != "" {
			if k := Lookup(name); k != SuspendNone && res != nil {
				res.SuspendSites = append(res.SuspendSites, SuspendSite{
					Call: call,
					Kind: k,
					Line: call.Pos.Line,
				})
			}
		}
		// 递归检查实参：f(g()) 里 g 可能是 IO
		for _, a := range call.Args {
			recordSuspends(a, res)
		}
		return
	}

	for _, sub := range ChildExprs(expr) {
		recordSuspends(sub, res)
	}
}

// HasSuspendCall 判断表达式里是否含挂起调用
func HasSuspendCall(expr ast.Expression) bool {
	res := &BodyAnalysis{}
	recordSuspends(expr, res)
	return len(res.SuspendSites) > 0
}

// CalleeName 提取被调用者名字；无法确定时返回 ""
func CalleeName(call *ast.CallExpression) string {
	switch f := call.Function.(type) {
	case *ast.Identifier:
		return f.Name
	case *ast.MemberAccessExpression:
		// std.io.read 形式：取最后一段
		return f.Member
	case *ast.SpawnExpression:
		return CalleeName(&ast.CallExpression{Function: f.Function})
	}
	return ""
}

// ChildExprs 返回表达式的直接子表达式（保守遍历的入口）
func ChildExprs(expr ast.Expression) []ast.Expression {
	var out []ast.Expression
	switch e := expr.(type) {
	case *ast.BinaryExpression:
		out = append(out, e.Left, e.Right)
	case *ast.UnaryExpression:
		out = append(out, e.Right)
	case *ast.MemberAccessExpression:
		out = append(out, e.Object)
	case *ast.IndexExpression:
		out = append(out, e.Object, e.Index)
	case *ast.ParenExpression:
		out = append(out, e.Inner)
	case *ast.TypeCastExpression:
		out = append(out, e.Expression)
	case *ast.ConditionalExpression:
		out = append(out, e.Condition, e.TrueExpr, e.FalseExpr)
	case *ast.SpawnExpression:
		out = append(out, e.Function)
		out = append(out, e.Args...)
	case *ast.MatchExpression:
		// match 的分支体是语句序列，逐个递归
		out = append(out, e.Target)
		for _, arm := range e.Arms {
			for _, st := range arm.Body {
				if asExpr, ok := st.(ast.Expression); ok {
					out = append(out, asExpr)
				}
			}
		}
	}
	return out
}