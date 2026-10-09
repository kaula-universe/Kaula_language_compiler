// Kaula 并发内核（CONC）· conc fn lowering 的静态分析
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
	// SuspendBlocked 是 #[hint(blocking)] 调用：Worker 让出、Block Lane 接管
	SuspendBlocked
)

func (k SuspendKind) String() string {
	switch k {
	case SuspendIO:
		return "IO"
	case SuspendAwait:
		return "await"
	case SuspendSleep:
		return "sleep"
	case SuspendBlocked:
		return "blocked"
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
	// Line 是源码行号，仅用于诊断注释
	Line int
	// Ordinal 是本挂起点在遍历序里的序号（从 0 开始）。
	//
	// 为什么不用 Line 判活跃：解析器从不填 CallExpression.Pos，
	// 每个挂起点的 Line 都是 0 —— 拿它当界会让「跨挂起存活的变量」
	// 一个都判定不出来，于是挂起前不写回、恢复时不重建，那些局部
	// 全部读未初始化的旧值。序号是遍历出来的，一定存在。
	Ordinal int
}

// VarInfo 是一个局部变量的跨 suspend 存活信息
type VarInfo struct {
	Name string
	// DeclLine 是声明行（仅诊断用，见 SuspendSite.Ordinal 的说明）
	DeclLine int
	// DeclOrdinal 是「声明之前最后一个挂起点的序号」，没有则为 -1。
	//
	// 于是「v 在第 k 个挂起点存活」等价于 v.DeclOrdinal < k。
	DeclOrdinal int
	// Initializer 是初始化表达式；挂起前求值
	Initializer ast.Expression
	// KType 是显式声明的 Kaula 类型；auto 声明为空
	KType string
}

// SubDepth 是「复合语句恢复槽位」的最大嵌套层数。
//
// 生成代码里直接用内核头的 KAULA_CONC_SUB_DEPTH（单一事实来源），
// 这里这份只用于编译期越界检查。两者必须一致。
const SubDepth = 8

// RestoreAll 是 SegmentRestoreOrdinals 的哨兵：本段的恢复副本覆盖全部
// 跨 suspend 变量（而不是「到本段最靠后的那个挂起点为止」）。
//
// 守卫模式下必须如此：段体里每条变量声明都被包进 `if (f->sub[d] == k)`，
// 那个块是嵌套作用域，于是段内的初始化写进的是内层对象，而循环之后
// 的代码看到的是段首恢复出来的外层对象 —— 两者不是同一个。
// 所以段体的声明一律降级成「赋值」，对象统一由段首的恢复副本提供。
const RestoreAll = 1 << 30

// BodyAnalysis 是函数体的分析结果
type BodyAnalysis struct {
	// SuspendSites 按源码顺序排列的挂起点
	SuspendSites []SuspendSite
	// FrameVars 必须进入 Frame 的变量
	FrameVars []VarInfo
	// NeedsSuspend 为假时，编译器可生成"单一形态"：
	// 既不需要 pc 字段，也不需要 resume 的 switch（文档 §2.7）
	NeedsSuspend bool

	// HasCompound：函数体含 if/while/for/for-in 复合语句（需要 sub/branch
	// 守卫槽位）。无复合语句时省略，Frame 从 296B 瘦到 ~32B ——
	// KMM 分配从 512B 档（slots 少、桶满频繁）降到小档，spawn 热路径
	// 每任务省一次大分配 + 大 memset。
	HasCompound bool

	// CompoundDepth：复合语句最大守卫深度（1-based；0=无复合语句）。
	// Frame 里的 sub/branch 数组大小为 CompoundDepth+1（含 body 层 sub[0]），
	// 不再无条件生成 KAULA_CONC_SUB_DEPTH=8 的固定数组。
	CompoundDepth int

	// SuspendMaxDepth：挂起点所在的最大复合深度（1-based；0=挂起点在顶层）。
	//
	// P2.2：守卫深度只需覆盖挂起点所在路径，而非全函数体最大深度。
	// 深层复合语句（挂起点之后的 if/while）恢复时从头执行，不需要 sub/branch。
	// Frame 的 sub/branch 数组大小 = SuspendMaxDepth+1（含 body 层），
	// 可比 CompoundDepth+1 小数层——conn_task 从 sub[4] 降到 sub[2]，省 16B。
	SuspendMaxDepth int

	// HasForIn：函数体含 for..in 守卫（需要 loop_i/loop_end/loop_step）。
	// 无 for-in 时省略 192B 迭代状态数组。
	HasForIn bool

	/*
	 * SegmentRestoreOrdinals[i] 是第 i 段的 Frame 变量恢复界（挂起点序号）。
	 * 第 i 段要恢复的是 DeclOrdinal < 该值的变量；-1 = 本段不需要恢复副本。
	 *
	 * 为什么不能一律按「本段第一个挂起点」推：段里含复合语句时，一个段可能
	 * 有多个挂起点（分别在不同的循环层）。恢复点最靠后的那个决定活跃集 ——
	 * 取它，才不会在恢复后读到未初始化的循环变量。
	 *
	 * 为 nil 时按 SuspendSites 推导（第 i 段的界 = 第 i-1 个挂起点），
	 * 即「一条顶层语句一个挂起点」的老切分方式。
	 */
	SegmentRestoreOrdinals []int

	// ordinalByCall 把挂起点索引到它的序号；内联挂起按调用指针查自己是谁
	ordinalByCall map[*ast.CallExpression]int
}

// RestoreOrdinal 返回第 pc 段的 Frame 变量恢复界；-1 = 不需要恢复副本。
func (a *BodyAnalysis) RestoreOrdinal(pc int) int {
	if a.SegmentRestoreOrdinals != nil {
		if pc < 0 || pc >= len(a.SegmentRestoreOrdinals) {
			return -1
		}
		return a.SegmentRestoreOrdinals[pc]
	}
	// 老路径：pc 段之前最后一个挂起点
	if pc <= 0 || len(a.SuspendSites) == 0 {
		return -1
	}
	if pc-1 < len(a.SuspendSites) {
		return a.SuspendSites[pc-1].Ordinal
	}
	return a.SuspendSites[len(a.SuspendSites)-1].Ordinal
}

// SuspendOrdinalOf 返回某个调用对应的挂起点序号；不是挂起点返回 -1。
func (a *BodyAnalysis) SuspendOrdinalOf(call *ast.CallExpression) int {
	if a.ordinalByCall == nil || call == nil {
		return -1
	}
	return a.ordinalByCall[call]
}

// LiveAt 报告变量 v 在第 k 个挂起点是否已有值（必须被写回 / 被恢复）。
func (a *BodyAnalysis) LiveAt(v VarInfo, k int) bool { return v.DeclOrdinal < k }

// ========== 可插桩函数注册表 ==========

// InstrumentationTable 是"哪些调用可插桩"的注册表。
//
// 运行时通过它把 Kaula 层的 IO 映射到 KaulaIoRequest；
// 编译器在 lowering 时查同一张表。两侧必须一致 —— 这是文档 §9.1 的 ABI 约定。
type InstrumentationTable struct {
	entries map[string]SuspendKind
}

/*
 * 一致性铁律（文档 §9.1 / §1.4 ABI 约定）：
 * 本表里每一项 SuspendIO / SuspendSleep 都必须能在 concIoTypeOf
 * （codegen 侧操作码映射）里查到对应内核操作码（KAULA_IO_READ /
 * WRITE / SLEEP）。内核只实现这三种操作码，CONNECT / ACCEPT 是
 * 「保留：内核不实现」。
 *
 * 只在单侧登记的后果是"状态机切好了、调用却没改写"：分析阶段认它是
 * 挂起点，于是把函数体切成多段并插入 pc 赋值；lowering 阶段找不到对应
 * 操作码，调用原样生成。生成的 C 编译得过、跑得动，只是那个 Worker 原地
 * 阻塞在 IO 上 —— I4 静默失效，没有任何诊断。
 *
 * 因此本表是 concIoTypeOf 的「子集」：凡是进来的 IO / 定时函数，先确认
 * concIoTypeOf 里有它。下面 net_read / web_get / web_post / subprocess_wait
 * 曾经登记过，但都不满足这条铁律，已全部移出：
 *   - net_read：幻影名（std/net 真实 API 是 socket_*，无此函数），且缺操作码；
 *   - web_get / web_post：幻影名（std/web 只有 http server，无此函数），且缺操作码；
 *   - subprocess_wait：真实存在，但做的是 waitpid / WaitForSingleObject，
 *     不是单 fd read/write/sleep，内核无对应操作码；改走阻塞车道
 *     （见下方 MarkBlocking），而非静默失效。
 */
var instrumentTable = &InstrumentationTable{
	entries: map[string]SuspendKind{
		/*
		 * 网络收发登记为 SuspendIO，靠 KAULA_IO_READ / WRITE 走通 ——
		 * 内核的 IO 执行是 read/write，POSIX 上 socket 就是 fd。
		 *
		 * net_connect / net_accept 不登记：connect/accept 是 socket 专属，
		 * read/write 表达不了，内核不实现。调用点退回普通阻塞语义，
		 * 行为正确（只是那个 Worker 会真的卡住）。登记成挂起点却拿不到
		 * 操作码才是 I4 静默失效的那种错。
		 */
		"net_recv":  SuspendIO,
		"net_send":  SuspendIO,
		"net_write": SuspendIO,
		// 文件
		"fs_read":  SuspendIO,
		"fs_write": SuspendIO,
		// 定时
		// 四种粒度都要登记：只登记毫秒版的话，亚毫秒定时无法成为挂起点，
		// 挂起路径的开销就测不出来（毫秒粒度把吞吐钉死在 ~1000/s/任务）。
		"time_sleep":         SuspendSleep,
		"time_sleep_us":      SuspendSleep,
		"time_sleep_ns":      SuspendSleep,
		"time_sleep_seconds": SuspendSleep,
		"time_after":         SuspendSleep,
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

// 已知阻塞的内建 std 函数（无对应内核 IO 操作码）。
//
// 这些函数做的是 waitpid / WaitForSingleObject 之类「占住线程」的阻塞操作，
// 不是单 fd read/write/sleep，内核的 IoDriver 接管不了。把它们登记到阻塞车道
// 白名单，让并发内核把任务移交给 Block Lane 线程就地执行（I5），而不是在
// Worker 上原地阻塞、也不是当成可插桩 IO 去查一个不存在的操作码（那会 I4
// 静默失效）。
//
// 与用户在源码里写 #[hint(blocking)] 走的是同一张表（blockingHints），只是
// 这里提前把已知的内建阻塞 std 函数登记好——它们的实现在 C 里，挂不了 kl 属性。
func init() {
	MarkBlocking("subprocess_wait")
}

// ========== 分析 ==========

// AnalyzeBody 分析函数体，找出挂起点与需要进入 Frame 的变量。
//
// 保守策略：一旦函数体含挂起点，就把所有已声明的局部变量提升到 Frame。
// 多几个字段只多几十字节；而漏掉一个就是跨 suspend 读到栈上的失效值（UAF）。
//
// 挂起点与声明在**同一次遍历**里记录，这样「声明在第几个挂起点之前」
// 是确定的（VarInfo.DeclOrdinal）。分成两次遍历就只能拿源码行号去比，
// 而解析器不填表达式位置，拿到的是一片 0 —— 判活跃会全部落空。
func AnalyzeBody(body []ast.Statement) *BodyAnalysis {
	res := &BodyAnalysis{ordinalByCall: make(map[*ast.CallExpression]int)}

	// cur = 最近一次见到的挂起点序号；-1 表示还没见过挂起点
	cur := -1
	// curDepth = 当前 walk 深度，note 读取以记录挂起点所在深度
	curDepth := 0
	seen := make(map[string]bool)
	var decls []VarInfo

	addDecl := func(name string, line int, init ast.Expression, ktype string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		decls = append(decls, VarInfo{
			Name: name, DeclLine: line, DeclOrdinal: cur,
			Initializer: init, KType: ktype,
		})
	}

	// note 记录表达式里的挂起点并推进 cur；同时记录挂起点所在深度
	note := func(expr ast.Expression) {
		before := len(res.SuspendSites)
		res.recordSuspends(expr, &cur)
		if len(res.SuspendSites) > before && curDepth > res.SuspendMaxDepth {
			res.SuspendMaxDepth = curDepth
		}
	}

	var walk func(stmts []ast.Statement, depth int)
	walk = func(stmts []ast.Statement, depth int) {
		curDepth = depth
		for _, st := range stmts {
			if st == nil {
				continue
			}
			switch s := st.(type) {
			case *ast.VariableDeclaration:
				// 先记初始化式里的挂起：变量在自己的初始化式执行期间还没有值
				note(s.Value)
				addDecl(s.Name, s.Pos.Line, s.Value, s.Type)
			case *ast.ExpressionStatement:
				note(s.Expression)
			case *ast.ReturnStatement:
				note(s.Value)
			case *ast.YieldStatement:
				note(s.Source)
			case *ast.BlockStatement:
				walk(s.Statements, depth+1)
			case *ast.IfStatement:
				res.HasCompound = true
				if depth+1 > res.CompoundDepth {
					res.CompoundDepth = depth + 1
				}
				note(s.Condition)
				walk(s.Body, depth+1)
				walk(s.Else, depth+1)
			case *ast.WhileStatement:
				res.HasCompound = true
				if depth+1 > res.CompoundDepth {
					res.CompoundDepth = depth + 1
				}
				note(s.Condition)
				walk(s.Body, depth+1)
			case *ast.ForStatement:
				res.HasCompound = true
				if depth+1 > res.CompoundDepth {
					res.CompoundDepth = depth + 1
				}
				note(s.Condition)
				if vd, ok := s.Init.(*ast.VariableDeclaration); ok && vd != nil {
					note(vd.Value)
					addDecl(vd.Name, vd.Pos.Line, vd.Value, vd.Type)
				}
				walk(s.Body, depth+1)
			case *ast.ForInStatement:
				res.HasCompound = true
				res.HasForIn = true
				if depth+1 > res.CompoundDepth {
					res.CompoundDepth = depth + 1
				}
				/*
				 * 迭代下标**不**提升到 Frame（addDecl 已删除）。
				 *
				 * guard 模式已经把它持久化在 loop_i[depth] 里：
				 *   init   f->loop_i[d] = start
				 *   循环头 条件读 f->loop_i[d] / loop_end[d] / loop_step[d]
				 *   循环体 long long i = f->loop_i[d]   ← 从 Frame 恢复成本地
				 *   轮末   f->loop_i[d] += f->loop_step[d]
				 * 挂起发生在循环体内，此时 loop_i[d] 仍是本轮的值，
				 * 恢复后重新从它读出的 i 与挂起前一致。
				 *
				 * 早期版本在这里也 addDecl，于是：
				 *   1. Frame 多一个纯冗余字段
				 *   2. for-in 下标是 ast.Identifier，没有类型标注 →
				 *      frameVarCTypes 跳过 → FrameVarCType 兜底成 void*
				 *      → 生成的 `f->i = i;` 把 long long 赋给 void*，
				 *        clang: incompatible integer to pointer conversion，
				 *        整个 conc fn 编译失败
				 *   3. resume 顶部多一句 `void* i = f->i;`，与循环体里的
				 *      `long long i = f->loop_i[d];` 同名遮蔽
				 *
				 * 注意别把 C 风格 for（ForStatement）的 init 变量一起删掉：
				 * 那条路径没有 loop_i 兜底，必须留在 FrameVars 里。
				 */
				walk(s.Body, depth+1)
			case *ast.CallStatement:
				walk(s.Body, depth+1)
			}
		}
	}
	walk(body, 0)

	res.NeedsSuspend = len(res.SuspendSites) > 0

	if res.NeedsSuspend {
		res.FrameVars = decls
	}
	return res
}

// recordSuspends 记录表达式里的挂起点，并把 cur 推进到最后一个
func (a *BodyAnalysis) recordSuspends(expr ast.Expression, cur *int) {
	if expr == nil {
		return
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		name := CalleeName(call)
		if name != "" {
			k := Lookup(name)
			if k == SuspendNone && IsBlocking(name) {
				// #[hint(blocking)]：不可插桩，走阻塞车道（BLOCKED 形态）
				k = SuspendBlocked
			}
			if k != SuspendNone {
				ord := len(a.SuspendSites)
				a.SuspendSites = append(a.SuspendSites, SuspendSite{
					Call:    call,
					Kind:    k,
					Line:    call.Pos.Line,
					Ordinal: ord,
				})
				// 惰性建表：HasSuspendCall 走的是零值 BodyAnalysis
				if a.ordinalByCall == nil {
					a.ordinalByCall = make(map[*ast.CallExpression]int)
				}
				a.ordinalByCall[call] = ord
				*cur = ord
			}
		}
		// 递归检查实参：f(g()) 里 g 可能是 IO
		for _, arg := range call.Args {
			a.recordSuspends(arg, cur)
		}
		return
	}

	for _, sub := range ChildExprs(expr) {
		a.recordSuspends(sub, cur)
	}
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
	a := &BodyAnalysis{}
	cur := -1
	a.recordSuspends(expr, &cur)
	return len(a.SuspendSites) > 0
}

// hasBlockingCall 递归探测表达式内是否调用被 #[hint(blocking)] 标记的函数。
// 与 recordSuspends 同构，但只查 blockingHints —— 不登记 SuspendSite。
func hasBlockingCall(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		name := CalleeName(call)
		if name != "" && IsBlocking(name) {
			return true
		}
		for _, arg := range call.Args {
			if hasBlockingCall(arg) {
				return true
			}
		}
		return false
	}
	for _, sub := range ChildExprs(expr) {
		if hasBlockingCall(sub) {
			return true
		}
	}
	return false
}

// HasBlockingCall 判断表达式内是否含 #[hint(blocking)] 调用
func HasBlockingCall(expr ast.Expression) bool {
	return hasBlockingCall(expr)
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