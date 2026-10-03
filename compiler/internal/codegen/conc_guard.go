package codegen

import (
	"fmt"
	"sort"
	"strings"

	"kaula/internal/ast"
	"kaula/internal/codegen/conc"
	"kaula/internal/symbol"
)

/*
 * 复合语句内的恢复（守卫模式）
 *
 * == 问题 ==
 *
 * resume 的 pc 表达的是「函数体按顶层语句切成了几段」。挂起点落在
 * 循环体/分支体里时，顶层切分看不见它 —— lowering 只能把**整条复合语句**
 * 生成两遍：一遍「提交 IO」，一遍「取回结果」。
 *
 * 后果是恢复后整个循环体再跑一遍。它不崩，只是把副作用做了两遍；
 * 更早的版本甚至连 C 都编不过（挂起返回引用了块内的 IO 请求局部量）。
 *
 * == 办法 ==
 *
 * 对「体内部含挂起点」的复合语句，整条只生成一次，改成给每一层开一个
 * 槽位计数器（Frame 的 sub[d]）。语句逐条包进 `if (f->sub[d] == k)`：
 *
 *     for (;;) {
 *         if (f->sub[1] == 0) { f->sub[1] = 1; if (!(i < n)) break; }
 *         if (f->sub[1] == 1) { f->sub[1] = 2; <提交 time_sleep> 挂起返回 }
 *         if (f->sub[1] == 2) { f->sub[1] = 3; <取回结果> }
 *         if (f->sub[1] == 3) { f->sub[1] = 4; total = total + i; }
 *         f->sub[1] = 0;
 *     }
 *
 * 恢复后 sub[1]==2，槽位 1 被跳过，槽位 2 接着往下走。
 * 每轮末尾写回 0 —— 于是「进入任一层时读到的都是 0」这个不变量成立，
 * 循环能正常起转。
 *
 * 守卫槽位是嵌套作用域，所以段内的变量声明与恢复副本同名也不冲突：
 * 走正常路径时被遮蔽（用段内声明），走恢复路径时声明被跳过（用恢复副本）。
 *
 * == 不用 goto 的原因 ==
 *
 * 跳进循环体的标签方案更短，但它过不去：标签之后的代码按**词法作用域**
 * 解析标识符，源码变量名在标签之前声明过，跳过那条声明只会读到未初始化
 * 的副本。要修正就得把段内声明改名，那会牵动整个符号表。
 * 守卫方案不需要改名，也不需要 goto。
 */

// concGuardActive 报告当前是否在守卫模式
func (cg *CodeGenerator) concGuardActive() bool { return cg.concGuardDepth >= 0 }

// concGuardEnter 为「体内部含挂起点」的复合语句开一层守卫，返回恢复现场函数。
//
// 槽位号从 0 重新开始：外层的槽位由外层自己推进，兄弟体之间不能串号。
func (cg *CodeGenerator) concGuardEnter() func() {
	if !cg.concGuardActive() || cg.concGuardDepth >= conc.SubDepth {
		// 越界在 concCheckSupported 处已经报过诊断，这里只是别把 Frame 写坏。
		return func() {}
	}
	prevDepth, prevSeq := cg.concGuardDepth, cg.concGuardSeq
	cg.concGuardDepth++
	cg.concGuardSeq = 0
	return func() { cg.concGuardDepth, cg.concGuardSeq = prevDepth, prevSeq }
}

// concGuardSeqNext 分配下一个守卫槽位号并返回它。
func (cg *CodeGenerator) concGuardSeqNext() int {
	k := cg.concGuardSeq
	cg.concGuardSeq = k + 1
	return k
}

// concGuardSlot 把一段代码包进守卫槽位，进入即推进到下一格。
//
// 槽位序号在生成**之前**分配：体内部的可插桩调用要靠它算出「恢复到第几号」。
// 也就是生成的代码形如
//
//	if (f->sub[d] == k) {
//	    f->sub[d] = k + 1;      /* 走到这条语句之前先推进 */
//	    <inner>
//	}
//
// 推进放在前面而不是后面：若 inner 是挂起语句，推进的那一步照样执行，
// 挂起返回再把它设成恢复槽位即可。若放在后面，挂起语句永远走不到那里。
func (sg *StatementGenerator) concGuardSlot(inner func() string) string {
	return sg.concGuardSlotAdvance(inner, true)
}

// concGuardHold 把一段代码包进守卫槽位，但**进入时不推进**。
//
// 用在「体内含挂起点」的复合语句上：恢复点在那条语句的**内部**
// （由它自己更深一层的 sub[d+1] 表达），所以本层的槽位必须保持不变，
// 否则恢复时会跳过整条语句、连带跳过它里面的循环。
//
// 于是推进挪到它执行完之后 —— 循环真正结束才轮到下一格，
// 这正好也是 C 的语义：后面的语句只在循环跑完之后才该执行。
func (sg *StatementGenerator) concGuardHold(inner func() string) string {
	return sg.concGuardSlotAdvance(inner, false)
}

// concGuardSlotAdvance 是上面两种形态的共同实现。
// advanceFirst 为真表示「进入即推进」，为假表示「执行完才推进」。
func (sg *StatementGenerator) concGuardSlotAdvance(inner func() string, advanceFirst bool) string {
	cg := sg.codegen
	if !cg.concGuardActive() {
		return inner()
	}

	f := cg.concFrameVar
	d := cg.concGuardDepth
	k := cg.concGuardSeqNext()

	var b strings.Builder
	b.WriteString(cg.indentString())
	b.WriteString(fmt.Sprintf("if (%s->sub[%d] == %d) {\n", f, d, k))

	cg.indent++
	advance := func() {
		b.WriteString(cg.indentString())
		b.WriteString(fmt.Sprintf("%s->sub[%d] = %d;\n", f, d, k+1))
	}
	if advanceFirst {
		advance()
	}
	b.WriteString(inner())
	if !advanceFirst {
		advance()
	}
	cg.indent--

	b.WriteString(cg.indentString() + "}\n")
	return b.String()
}

// concGuardLoopHead 发出守卫模式下的循环头。
//
// 生成的是
//
//	if (f->sub[d] == 0) {
//	    f->sub[d] = 1;
//	    if (!(COND)) break;
//	}
//
// 条件为假时 break —— 所以循环体必须被包在 `for (;;)` 里，
// break 才绑定到这个新循环而不是外层的循环。
func (sg *StatementGenerator) concGuardLoopHead(condCode string) string {
	cg := sg.codegen
	f := cg.concFrameVar
	d := cg.concGuardDepth
	k := cg.concGuardSeqNext()

	var b strings.Builder
	b.WriteString(cg.indentString())
	b.WriteString(fmt.Sprintf("if (%s->sub[%d] == %d) {\n", f, d, k))
	cg.indent++
	b.WriteString(cg.indentString())
	b.WriteString(fmt.Sprintf("%s->sub[%d] = %d;\n", f, d, k+1))
	b.WriteString(cg.indentString())
	b.WriteString("if (!(" + condCode + ")) break;\n")
	cg.indent--
	b.WriteString(cg.indentString() + "}\n")
	return b.String()
}

// concGuardCondPrologue 发出「条件求值 + 记录分支」的守卫槽位，返回该槽位号。
//
// if 的条件必须先落定到 Frame（branch[d]）再进分支体：恢复时不能重算条件，
// 重算就可能因为 IO 副作用或变量已更新而走错分支。
func (sg *StatementGenerator) concGuardCondPrologue(condCode string) string {
	cg := sg.codegen
	f := cg.concFrameVar
	d := cg.concGuardDepth
	k := cg.concGuardSeqNext()

	var b strings.Builder
	b.WriteString(cg.indentString())
	b.WriteString(fmt.Sprintf("if (%s->sub[%d] == %d) {\n", f, d, k))
	cg.indent++
	b.WriteString(cg.indentString())
	b.WriteString(fmt.Sprintf("%s->sub[%d] = %d;\n", f, d, k+1))
	b.WriteString(cg.indentString())
	b.WriteString(fmt.Sprintf("%s->branch[%d] = (%s) ? 1 : 0;\n", f, d, condCode))
	cg.indent--
	b.WriteString(cg.indentString() + "}\n")
	return b.String()
}

/*
 * concGuardedDecl 生成「守卫模式下的一条变量声明」。
 *
 * 声明被拆成两半：
 *
 *	T v;            /* 声明本体，提到语句列表所在的作用域 *
 *	v = f->v;       /* 恢复副本 —— 恢复路径走这里 *
 *	if (f->sub[d] == k) { f->sub[d] = k+1; v = <init>; }
 *
 * 为什么不能把 `T v = <init>;` 整条留在槽位里：槽位是 `if (...) { ... }`，
 * 是嵌套作用域，于是初始化写进内层对象，而循环之后的代码看到的是段首
 * 恢复出来的外层对象 —— 两者不是同一个，累加结果会丢。
 *
 * 段体（第 0 层）不生成 `T v;`：那里的对象由 resume 的段首恢复副本提供，
 * 重复声明会编译不过。符号登记照做 —— 后面的语句要靠它解析类型。
 *
 * 已知限制：段体里**指针**类型的跨 suspend 局部量不分配对象（分配语句随声明
 * 一起被丢掉了），只从 Frame 恢复。指针跨挂起点的所有权语义是另一个话题，
 * 与这里的恢复位置正交。
 */
func (sg *StatementGenerator) concGuardedDecl(vd *ast.VariableDeclaration) string {
	cg := sg.codegen

	// 声明本身也是一个槽位：初始化式里若有两个挂起点，同样放不下
	cg.concCheckLeafStmt(vd)

	/*
	 * 先按「无初始化式」生成一次：符号登记、类型推导、属性前缀都靠它，
	 * 而它的类型文本在第 0 层会被丢弃、在更深层会被提升到列表作用域。
	 */
	saved := vd.Value
	vd.Value = nil
	plain := sg.generateVariableDeclaration(vd)
	vd.Value = saved

	// 初始化式单独生成，放进槽位：降级成赋值
	assign := ""
	if saved != nil {
		assign = fmt.Sprintf("%s = %s;\n",
			vd.Name, sg.codegen.expressionGenerator.GenerateExpression(saved))
	}

	// 第 0 层：对象由段首的恢复副本提供，这里只留赋值
	if cg.concGuardDepth == 0 {
		return sg.concGuardSlot(func() string { return assign })
	}

	var b strings.Builder
	// 声明本体提到槽位之外（类型文本来自上面那次生成）
	for _, line := range strings.Split(strings.TrimRight(plain, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(cg.indentString())
		b.WriteString(line)
		b.WriteString("\n")
	}
	// 恢复副本：无条件执行 —— 恢复路径跳过初始化槽，靠它拿到值
	b.WriteString(cg.indentString())
	b.WriteString(fmt.Sprintf("%s = %s->%s;\n", vd.Name, cg.concFrameVar, vd.Name))
	b.WriteString(sg.concGuardSlot(func() string { return assign }))
	return b.String()
}

/*
 * generateHandleRelease 发出「归还 Handle 持有的 epoch pin」。
 *
 * pin 是在 spawn 返回那一刻拿的（挡的是「任务已完成并 retire，而调用方还没
 * 来得及 await」那个窗口）。只由 await_into 归还是不够的：句柄从不 await 时
 * 那个 pin 永远不还，会一直挡住回收直到 shutdown，把内存峰值拖到进程结束。
 *
 * 归还点就是作用域出口 —— 句柄离开作用域即不再可能被读。
 * kaula_handle_release 幂等，所以 await_into 之后再发一次是安全的。
 *
 * 故意不做「最后使用点就释放」：那要精确的 Handle 活跃分析，而漏一处的
 * 代价是提前 unpin（悬垂）。作用域出口是保守方向 —— 晚还，不会早还。
 *
 * 只处理局部量：形参是借用（所有权在调用方），全局量活到进程结束。
 */
func (sg *StatementGenerator) generateHandleRelease(scope *symbol.SymbolTable) string {
	if scope == nil {
		return ""
	}
	symbols := scope.GetAllSymbols()
	if len(symbols) == 0 {
		return ""
	}
	names := make([]string, 0, len(symbols))
	for name := range symbols {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	indent := sg.codegen.indentString()
	for _, name := range names {
		sym := symbols[name]
		if sym == nil || sym.Scope == "parameter" || sym.Scope == "global" {
			continue
		}
		if !sg.isHandleVar(name) {
			continue
		}
		sg.codegen.noteConcIoUse()
		b.WriteString(indent)
		b.WriteString(fmt.Sprintf(
			"kaula_handle_release(%s); /* conc: 归还句柄持有的 epoch pin */\n", name))
	}
	return b.String()
}

// isHandleVar 判断一个局部量是不是 Kaula 层的 Handle。
// 只信事实登记：`Handle x = spawn f()` 的声明处（stmtgen.go）登记过结果类型。
// 不做类型名猜测——字符串匹配会撞上 std.concurrent 的 Task 结构体（"task"）
// 或任何与句柄同形的用户类型，导致作用域出口误发 kaula_handle_release。
// 代价：非 spawn 路径产生的 Handle（如 Handle 复制）不在此列，pin 会晚还——
// 这是泄漏方向，不是悬垂，与"晚还不会早还"的保守哲学一致。
func (sg *StatementGenerator) isHandleVar(name string) bool {
	return sg.codegen.handleResultTypes != nil && sg.codegen.handleResultTypes[name] != ""
}

// stmtsHaveSuspend 报告语句列表里是否含可插桩调用
func stmtsHaveSuspend(stmts []ast.Statement) bool {
	for _, s := range stmts {
		if s != nil && statementHasSuspend(s) {
			return true
		}
	}
	return false
}

/*
 * concIsResumableCompound 报告这条语句是否「体内含挂起点、因而需要守卫」。
 *
 * 只有 while / if / block 能进入守卫模式。for..in 不行：它的下标是编译器
 * 生成的局部量（`_i_`），不在 Frame 里，恢复后会从头再数一遍。
 * 条件里含挂起点的 while 也不行：条件在每轮头部求值，挂起在条件里就意味着
 * 「恢复时要不要重算条件」这个问题本身没有确定答案。
 *
 * 这两种情况一律报诊断而不是硬生成 —— 硬生成的旧行为是重复执行循环体，
 * 不报错、只是答案错。
 */
func concIsResumableCompound(s ast.Statement) bool {
	switch v := s.(type) {
	case *ast.IfStatement:
		return stmtsHaveSuspend(v.Body) || stmtsHaveSuspend(v.Else)
	case *ast.WhileStatement:
		return stmtsHaveSuspend(v.Body)
	case *ast.ForStatement:
		return stmtsHaveSuspend(v.Body)
	case *ast.BlockStatement:
		return stmtsHaveSuspend(v.Statements)
	}
	return false
}

// concCheckSupported 在把语句交给守卫模式之前先判定它能不能进。
// 不能进的报诊断 —— 编译失败比静默算错好。
func (cg *CodeGenerator) concCheckSupported(s ast.Statement, fnName string) {
	line := concStmtLine(s)
	report := func(msg string) {
		cg.error(fmt.Sprintf("%s:%d: %s", fnName, line, msg))
	}

	switch v := s.(type) {
	case *ast.WhileStatement:
		if conc.HasSuspendCall(v.Condition) {
			report("while 条件里的可插桩调用暂不支持：条件每轮求值，恢复时无法判定该不该重算")
		}
	case *ast.ForStatement:
		if conc.HasSuspendCall(v.Condition) {
			report("for 条件里的可插桩调用暂不支持：条件每轮求值，恢复时无法判定该不该重算")
		}
		if vd, ok := v.Init.(*ast.VariableDeclaration); ok && vd != nil && conc.HasSuspendCall(vd.Value) {
			report("for 初始化式里的可插桩调用暂不支持：初始化式只应执行一次")
		}
	case *ast.ForInStatement:
		if stmtsHaveSuspend(v.Body) {
			report("for..in 循环体内的可插桩调用暂不支持：迭代下标是编译器生成的局部量，" +
				"不在 Frame 里，恢复后会从头再数一遍")
		}
	}

	if concHasUsedAwaitResult(s) {
		report("复合语句内的可插桩调用暂不支持取用返回值：恢复落点是循环体里的语句，" +
			"而「提交」与「取回结果」两半分属同一条语句的前后，恢复到后半时前半已经过去了")
	}
	if cg.concGuardDepth >= conc.SubDepth {
		report(fmt.Sprintf("可恢复复合语句嵌套超过 %d 层", conc.SubDepth))
	}
}

/*
 * concCheckLeafStmt 校验一条「占一个守卫槽位」的叶子语句。
 *
 * 与 concCheckSupported 分开是因为粒度不同：后者看的是整条复合语句，
 * 而「一条语句里只能有一个挂起点」是**槽位**的性质 —— 复合语句的不同分支、
 * 不同轮次各自占槽，一个 if 的两个分支各有一次挂起是完全正常的，
 * 而同一条语句里的两次挂起会抢同一个恢复槽位，那才是真的放不下。
 */
func (cg *CodeGenerator) concCheckLeafStmt(s ast.Statement) {
	if s == nil || concIsResumableCompound(s) {
		return
	}
	if concStmtAwaitCount(s) <= 1 {
		return
	}
	line := concStmtLineOf(s)
	cg.error(fmt.Sprintf("%s:%d: 一条语句里含多个可插桩调用暂不支持："+
		"一次挂起要占掉两个相邻槽位（提交 / 取回结果），放不下第二对",
		cg.currentFunctionName, line))
}

// concStmtLineOf 取诊断用的行号；变量声明要单独认（它的 Pos 才有行号）
func concStmtLineOf(s ast.Statement) int {
	if vd, ok := s.(*ast.VariableDeclaration); ok {
		return vd.Pos.Line
	}
	return concStmtLine(s)
}

/*
 * concHasUsedAwaitResult 报告语句里是否有「取值被用到」的可插桩调用。
 *
 * 只有作为整条 ExpressionStatement 的调用算「取值未用」（void 调用），
 * 这种可以就地挂起、恢复后不需要再读一次结果。
 *
 * 取值被用到的情况没法在守卫模式下正确恢复：提交的半段与取回结果的半段
 * 在同一条语句里，恢复到后半段时前半段已经过去了，那个值取不到。
 * 宁可报诊断，也不要让它拿到一个过期的 TLS 结果 —— 那不报错，只是答案错。
 */
func concHasUsedAwaitResult(s ast.Statement) bool {
	used := false
	var walk func(stmts []ast.Statement)
	walk = func(stmts []ast.Statement) {
		for _, x := range stmts {
			if x == nil {
				continue
			}
			switch v := x.(type) {
			case *ast.ExpressionStatement:
				// 整条表达式就是这次调用 → 取值未用
				if call, ok := v.Expression.(*ast.CallExpression); ok {
					if name := conc.CalleeName(call); name != "" && conc.Lookup(name) != conc.SuspendNone {
						// 递归进实参：实参里的挂起仍然取值
						for _, a := range call.Args {
							walk([]ast.Statement{&ast.ExpressionStatement{Expression: a, Pos: call.Pos}})
						}
						continue
					}
				}
				if concCountSuspendExpr(v.Expression) > 0 {
					used = true
				}
			case *ast.VariableDeclaration:
				if concCountSuspendExpr(v.Value) > 0 {
					used = true
				}
			case *ast.ReturnStatement:
				if concCountSuspendExpr(v.Value) > 0 {
					used = true
				}
			case *ast.BlockStatement:
				walk(v.Statements)
			case *ast.IfStatement:
				if concCountSuspendExpr(v.Condition) > 0 {
					used = true
				}
				walk(v.Body)
				walk(v.Else)
			case *ast.WhileStatement:
				if concCountSuspendExpr(v.Condition) > 0 {
					used = true
				}
				walk(v.Body)
			case *ast.ForStatement:
				walk(v.Body)
			case *ast.ForInStatement:
				walk(v.Body)
			}
		}
	}
	walk([]ast.Statement{s})
	return used
}

// concStmtLine 取语句的源码行（诊断用）
func concStmtLine(s ast.Statement) int {
	switch v := s.(type) {
	case *ast.IfStatement:
		return v.Pos.Line
	case *ast.WhileStatement:
		return v.Pos.Line
	case *ast.ForStatement:
		return v.Pos.Line
	case *ast.ForInStatement:
		return v.Pos.Line
	case *ast.BlockStatement:
		return v.Pos.Line
	case *ast.VariableDeclaration:
		return v.Pos.Line
	case *ast.ExpressionStatement:
		return v.Pos.Line
	case *ast.ReturnStatement:
		return v.Pos.Line
	}
	return 0
}

// concStmtAwaitCount 数一条语句里可插桩调用的个数（保守下钻）
func concStmtAwaitCount(s ast.Statement) int {
	n := 0
	var walk func(stmts []ast.Statement)
	walk = func(stmts []ast.Statement) {
		for _, x := range stmts {
			if x == nil {
				continue
			}
			switch v := x.(type) {
			case *ast.ExpressionStatement:
				n += concCountSuspendExpr(v.Expression)
			case *ast.VariableDeclaration:
				n += concCountSuspendExpr(v.Value)
			case *ast.ReturnStatement:
				n += concCountSuspendExpr(v.Value)
			case *ast.BlockStatement:
				walk(v.Statements)
			case *ast.IfStatement:
				n += concCountSuspendExpr(v.Condition)
				walk(v.Body)
				walk(v.Else)
			case *ast.WhileStatement:
				n += concCountSuspendExpr(v.Condition)
				walk(v.Body)
			case *ast.ForStatement:
				walk(v.Body)
			case *ast.ForInStatement:
				walk(v.Body)
			}
		}
	}
	walk([]ast.Statement{s})
	return n
}

func concCountSuspendExpr(expr ast.Expression) int {
	if expr == nil {
		return 0
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		n := 0
		if name := conc.CalleeName(call); name != "" && conc.Lookup(name) != conc.SuspendNone {
			n++
		}
		for _, a := range call.Args {
			n += concCountSuspendExpr(a)
		}
		return n
	}
	n := 0
	for _, sub := range conc.ChildExprs(expr) {
		n += concCountSuspendExpr(sub)
	}
	return n
}

/*
 * concMaxSuspendOrdinal 报告一条语句内最靠后的挂起点序号。
 *
 * 用于 Frame 变量恢复的界：守卫模式下同一个段里有多个挂起点，
 * 最靠后的那个是最早的恢复位置，必须恢复的变量以它为准。
 *
 * 序号由一次深度优先遍历数出来。它必须与 conc.AnalyzeBody 的遍历**同序**，
 * 所以这里逐字复刻那边的语句分派 —— 两边一旦不同序，写回/恢复的变量集就错位，
 * 而症状只是恢复后读到旧值，不报任何错。
 */
func concMaxSuspendOrdinal(s ast.Statement) int {
	cur := -1
	var walk func(stmts []ast.Statement)
	walk = func(stmts []ast.Statement) {
		for _, x := range stmts {
			if x == nil {
				continue
			}
			switch v := x.(type) {
			case *ast.ExpressionStatement:
				cur = maxSuspendOrdinalExpr(cur, v.Expression)
			case *ast.VariableDeclaration:
				cur = maxSuspendOrdinalExpr(cur, v.Value)
			case *ast.ReturnStatement:
				cur = maxSuspendOrdinalExpr(cur, v.Value)
			case *ast.YieldStatement:
				cur = maxSuspendOrdinalExpr(cur, v.Source)
			case *ast.IfStatement:
				cur = maxSuspendOrdinalExpr(cur, v.Condition)
				walk(v.Body)
				walk(v.Else)
			case *ast.WhileStatement:
				cur = maxSuspendOrdinalExpr(cur, v.Condition)
				walk(v.Body)
			case *ast.ForStatement:
				cur = maxSuspendOrdinalExpr(cur, v.Condition)
				if vd, ok := v.Init.(*ast.VariableDeclaration); ok && vd != nil {
					cur = maxSuspendOrdinalExpr(cur, vd.Value)
				}
				walk(v.Body)
			case *ast.ForInStatement:
				walk(v.Body)
			case *ast.BlockStatement:
				walk(v.Statements)
			case *ast.CallStatement:
				walk(v.Body)
			}
		}
	}
	walk([]ast.Statement{s})
	return cur
}

func maxSuspendOrdinalExpr(cur int, expr ast.Expression) int {
	if expr == nil {
		return cur
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		name := conc.CalleeName(call)
		if name != "" && conc.Lookup(name) != conc.SuspendNone {
			cur++
		}
		for _, arg := range call.Args {
			cur = maxSuspendOrdinalExpr(cur, arg)
		}
		return cur
	}
	for _, sub := range conc.ChildExprs(expr) {
		cur = maxSuspendOrdinalExpr(cur, sub)
	}
	return cur
}

/*
 * concSaveLiveVars 发出「把此刻活跃的变量写回 Frame」。
 *
 * bound 是挂起点的遍历序号（见 conc.SuspendSite.Ordinal）：变量声明在第 k
 * 个挂起点之前，此刻就有值，必须写回；晚于它声明的（含这条语句自己声明的）
 * 此刻还没有值。漏写一个，恢复后那个局部就是未初始化的旧值 —— 不崩，
 * 只是静默算错。
 *
 * 这里曾经拿源码行号当界，而解析器从不填 CallExpression.Pos，
 * 于是每个挂起点的行号都是 0：跨挂起存活的局部一个都不会被写回，
 * 恢复后读到的全是上次 resume 留下的旧值。
 */
func (cg *CodeGenerator) concSaveLiveVars(bound int) string {
	f := cg.concFrameVar
	var b strings.Builder
	for _, p := range cg.concParams {
		b.WriteString(fmt.Sprintf("%s->%s = %s;\n", f, p, p))
	}
	if cg.concAnalysis == nil || bound < 0 {
		return b.String()
	}
	for _, v := range cg.concAnalysis.FrameVars {
		if cg.concAnalysis.LiveAt(v, bound) {
			b.WriteString(fmt.Sprintf("%s->%s = %s;\n", f, v.Name, v.Name))
		}
	}
	return b.String()
}
