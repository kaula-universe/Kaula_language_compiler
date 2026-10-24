package codegen

import (
	"kaula/internal/ast"
)

// ============================================================================
// 方案 A'：函数级「安全局部容器」预扫描
// ============================================================================
//
// 背景（见 docs/perf/P0-survivor-leak.md §6）：
//
//	`cur.next = as<i64>(nd); cur = nd` 这类「写穿 + 游标推进」写法，在基线
//	（c983678）下同时产生两个恶果：
//	  1. survivor 段单调泄漏：游标推进被判为跨作用域逃逸 → 每轮 promote 一份
//	     幸存副本；而该 promote 归为「内部 promote」不抑制函数出口 rewind，
//	     动态池下 rewind 又不回收存活槽位 → 副本数与迭代次数等比增长。
//	  2. 链损坏（静默数据错误）：容器 cur 自身留在 bump 区，循环体末尾
//	     mark_restore 回卷 bump 指针后，下一轮分配复用同一地址、把刚链好的
//	     节点覆盖掉。
//
// 方案 A' 的机制：当写穿的目标容器是**安全局部容器**时——
//   - 该写穿不 promote（对象留在 bump 区）；
//   - 该写穿所在的循环体不做 mark_restore（放弃循环级回卷）；
//   - 整条链在同一 bump 段里连续排布、互不覆盖，函数出口 scope_pop 一锅端回收。
//
// 安全性（P2 的实质）：只有「从不逃逸」的容器才进集合。集合外的容器一律走
// 旧路径（promote + 回卷，用泄漏换正确性），因此最坏后果只是"该回收的没回收"，
// 绝不会把一个仍被外部持有的对象留在会被 scope_pop 回收的 bump 区里。
//
// 本文件只做纯词法 AST 遍历，不依赖符号表 / SOR，可在函数体生成前运行。

// SafeContainerScan 是预扫描的结果。
type SafeContainerScan struct {
	// Safe: 「安全局部容器」名集合——既在本函数内声明、又从不逃逸。
	Safe map[string]bool
	// WrittenThrough: Safe 的子集中，被写穿过的容器名
	// （出现过 `obj.field = p` / `arr[i] = p` 且 obj 是安全局部容器）。
	// 游标推进（cur = nd）据此与链一起放弃 promote（方案 A'）。
	WrittenThrough map[string]bool
	// ChainLinked: 被**写穿进任何字段/元素**的源对象名集合
	// （`cur.next = nd` 里的 nd、`n.left = l` 里的 l）。
	//
	// 用途：字段存储产生一个随宿主存活的字段引用，对象的真实死亡点是
	// 宿主死亡点而非局部变量的词法最后使用点。liveness 若按后者判定就会
	// 在写穿点后把仍被宿主字段引用的对象回收 → 悬垂。ChainLinked 成员
	// 必须豁免 early free。
	//
	// 与宿主是否逃逸无关：`n.left = l; return n` 里 n 逃逸，l 的字段引用
	// 最长命，豁免反而最必要。宿主是否逃逸只影响宿主自身的 promote/回卷
	// 待遇（ChainEscaping），不波及挂进去的源对象。
	ChainLinked map[string]bool
	// Escaped: 逃逸过的名字集合（含别名闭包传播后的结果）。
	//
	// 用途：写穿 promote 的分类。`obj.field = p` 的 promote 副本存活于
	// obj 的字段里，是否"向外"取决于 **obj** 是否逃逸——而 obj 极可能是
	// 函数局部变量（IsFunctionLocalVar 为真），仅凭它一律判"内部 promote"
	// 会漏掉 `Node* head = cur; return head` 这种"局部别名逃逸"的情形，
	// 导致函数出口 survivor_rewind 回收掉 return 出去的链。
	Escaped map[string]bool
	// ChainEscaping: 写穿容器的名字集合，且该容器的**链首**随返回值离开函数。
	//
	// 场景（`i64 head = as<i64>(cur); ...; return head`）：
	//   head 是 i64（地址值），与 cur 之间**没有**可识别的别名对（as<i64>(cur)
	//   在词法上被当成"把 cur 转成整数"，不是 Node* 别名），于是 cur 看起来
	//   从不逃逸、进入 Safe 集合；但 head 确实把整条链带出了函数。
	//
	// 后果：cur 被当成安全局部容器后，liveness 认为 cur 是最后一次使用、
	// 发射 `kmm_v4_survivor_free(cur)`——而 cur 此刻**正是链首所在节点**
	//   （value=-1、next=第 0 号节点）。第一个节点的槽位被释放后，下一轮
	//   build_chain 的分配器把它回收另作他用，链首被覆写 → 读链时踩空
	//   （n 小时 slab 未耗尽所以看不出来，n 大时必崩）。
	//
	// 判据：函数体内出现过 `return X` 且 X 与某个写穿容器存在"地址值别名"
	//   （X 由 as<T>(容器) 或直接取容器地址得到）。此时该容器的链首已交出去，
	//   容器不得再被当作安全局部容器（退回旧路径：promote + 回卷）。
	ChainEscaping map[string]bool
	// ReturnEscaped: 函数体内 `return <裸标识符>` 出现过的局部变量名集合
	// （穿透 as<T>()/括号；排除形参与全局——它们的所有权不在本函数）。
	//
	// 用途（P-A 修复·分配点提升）：这些名字的 kmm_v4_alloc 分配点直接把
	// 对象分配到 survivor 段（kmm_v4_alloc_global），而不是"bump 区分配 +
	// return 时浅拷贝一份到 survivor 段"。后者即 promote 复制链——递归
	// 构造形态（bottomUpTree）下每返回帧复制一层，depth=22 峰值 RSS
	// 678MB vs C-malloc 对拍 263MB，depth=25 直接 3-4GB OOM。
	// 分配点提升后对象天然活在 survivor 段，return 的 scope_promote_p 被
	// pool_owned 护栏 no-op 化（survivor 段不属于主池），funcEmitsPromote
	// 仍为 true（抑制 rewind），语义不变而双份拷贝消失。
	//
	// 与 Escaped 的区别：Escaped 含实参/赋值右值/yield 等一切逃逸通道；
	// ReturnEscaped 只取 return 这一条精确通道——只有它能证明"本函数
	// 分配的该对象必将跨函数边界存活"，才是分配点提升的充分条件。
	ReturnEscaped map[string]bool
}

// ScanSafeLocalContainers 扫描函数体，返回「安全局部容器」名集合（方案 A'）。
//
// 判据（全部满足才算安全）：
//  1. 名字在函数体内被声明为局部变量（含 `var x T`、`x := ...`、`x T = ...`、
//     多声明符 `T a, b`、for/for-in 循环变量）；
//  2. 该名字**从不逃逸**：
//     - 不作为 return 值（直接或经 as<T>() / (expr) / 一元 +,-,& 包装）；
//     - 不作为调用/spawn/await 实参；
//     - 不作为赋值右值（g = cur / other.field = cur / arr[i] = cur）；
//     - 不作为 yield / release 的源；
//     - 不作为 extract 的源；
//     - 不被闭包（LambdaExpression）捕获；
//  3. 不是形参（paramSet）。
//
// lambda 体内的语句**不参与**外层函数的逃逸判定（闭包对局部变量按引用捕获，
// 其生命周期由外层函数决定），但 lambda 自身若是外层语句的右值，那属于外层
// 语句的表达式，照常遍历。
func ScanSafeLocalContainers(body []ast.Statement, params []string) SafeContainerScan {
	paramSet := make(map[string]bool, len(params))
	for _, p := range params {
		paramSet[p] = true
	}

	declared := make(map[string]bool)       // 函数体内声明过的局部变量
	escaped := make(map[string]bool)        // 逃逸过的名字
	writtenThrough := make(map[string]bool) // 被写穿过的容器名
	lambdaCaptures := make(map[string]bool)

	s := &containerScan{
		declared:       declared,
		escaped:        escaped,
		writtenThrough: writtenThrough,
	}
	s.scanStmts(body)

	// lambda 捕获集单独收集后并入逃逸（闭包按引用捕获 → 生命周期超出语句）。
	for _, stmts := range s.lambdas {
		collectLambdaCaptureNames(stmts, lambdaCaptures)
	}

	// 合并 lambda 体内出现的、且在函数体声明过的自由名字（保守：全部计入逃逸）。
	for name := range lambdaCaptures {
		escaped[name] = true
	}

	// 别名传播与逃逸判定（关键的正确性一环）：
	//
	//  1. `head = cur` 让 head 与 cur 指向同一对象。若 head 随后逃逸
	//     （return head / 作为实参），则 cur 所指对象同样已逃逸 —— 即使 cur
	//     自身没在任何逃逸上下文出现。此时若把 cur 当安全局部容器、让对象
	//     留在 bump 区,return 出去的 head 就会在函数出口悬垂。
	//
	//  2. `g = cur`（g 是全局/形参/外部符号）→ 对象被交给函数外的持有者，
	//     cur 逃逸。遍历期无法判定 lhs 是否函数局部（declared 未完备），
	//     故此处用已收集的 declared 统一结算。
	escaped = resolveAssignEscape(escaped, declared, paramSet, s.assignTargets)
	escaped = closureAliasEscape(escaped, s.aliasPairs, declared)

	safe := make(map[string]bool)
	for name := range declared {
		if paramSet[name] || escaped[name] {
			continue
		}
		safe[name] = true
	}

	// WrittenThrough 必须是 Safe 的子集：写在非安全容器上的写穿走旧路径。
	wt := make(map[string]bool)
	for name := range writtenThrough {
		if safe[name] {
			wt[name] = true
		}
	}

	// ChainLinked：**所有**写穿目标（字段/元素）里存入的源对象，一律豁免
	// early free——不论宿主是否逃逸。
	//
	// 语义说明（与 yield 的区别，勿混）：
	//   `n.left = l` 不是 yield，l 没有交出所有权；它只是往 n 的字段里
	//   存了一个指针值。但这个存储动作产生了一个**随宿主存活的字段引用**：
	//   宿主 n 活多久，n.left 里的地址就可读多久。词法 liveness 只看到
	//   "l 此后不再出现在源码里"，看不到这个字段引用，于是在写穿点后
	//   回收 l 指向的对象 → 宿主字段悬垂。
	//
	//   正确的死亡判据是「所有引用（含字段引用）都消亡」，不是「局部
	//   变量的词法最后使用点」。本豁免即该判据的落地：对象的死亡点
	//   推迟到宿主死亡点。
	//
	// 宿主逃逸（`n.left = l; ...; return n`）时字段引用最长命，恰恰是
	// 最需要保活的场景。旧判据要求"宿主是安全局部容器"（即不逃逸），
	// 正好在这种场景下失灵：递归建树 depth>=8 时回收掉子树节点，
	// 下一轮分配覆写该槽位 → 读树踩空（depth<=7 slab 未耗尽不暴露）。
	//
	// 代价：宿主字段引用的对象不再有机会提前回收，只能随宿主/作用域尾
	// 统一回收。这是"用回收粒度换正确性"的保守取舍——与 ChainLinked
	// 机制既有的安全姿态一致（最坏只是该回收的没回收，绝不会悬垂）。
	cl := make(map[string]bool)
	for _, pair := range s.writeThroughPairs {
		cl[pair.source] = true
	}

	// ChainEscaping 结算：写穿容器的**链首**随返回值离开函数时，
	// 该容器必须退出 Safe 集合（见 ChainEscaping 字段注释）。
	//
	// 判据：容器 c 被写穿过，且存在地址值别名 v（`i64 v = as<i64>(c)`），
	// 而 v 出现在某个 return 的返回值里。
	//
	// 为什么必须这样处理：v 与 c 之间没有可识别的**指针别名对**
	// （as<i64> 改变了类型），逃逸传播不会把 c 标为 escaped；但 c 指向的
	// 首节点确实被 return 出去，此时若 c 仍算安全局部容器，liveness 会在
	// 块尾发射 `kmm_v4_survivor_free(c)`——释放的正是链首节点，
	// 下一轮分配复用该槽位 → 链首被覆写（n 大时才暴露）。
	chainEscaping := make(map[string]bool)
	writtenThroughNow := writtenThrough // 所有被写穿过的容器（未与 Safe 求交）
	if len(s.returnedNames) > 0 {
		returned := make(map[string]bool, len(s.returnedNames))
		for _, n := range s.returnedNames {
			returned[n] = true
		}
		for _, p := range s.addressAliases {
			value, container := p[0], p[1]
			if writtenThroughNow[container] && returned[value] {
				chainEscaping[container] = true
				// 该容器不再安全：退出 Safe（写穿不再享受"不 promote"待遇，
				// 退回 promote + 回卷旧路径）。
				delete(safe, container)
				delete(wt, container)
				// 关键：把容器并入逃逸集合。这一处改动同时修好两个下游消费者：
				//   - generateEarlyFreeForStmtIndex 见到 Name 落在 escapedVars，
				//     不再对 cur 发射 `kmm_v4_survivor_free(cur)`（链首保活）；
				//   - markCrossScopePromoteFor 的写穿分支据 IsEscapedVar 判为
				//     「向外 promote」，抑制函数出口 rewind（链随返回值存活）。
				escaped[container] = true
			}
		}
	}
	// 注意：ChainLinked 不再随 safe 收敛——字段引用延命与宿主是否逃逸无关，
	// 容器退出 safe 只影响它自身的 promote/回卷待遇，不影响挂进去的源对象
	// 的豁免（否则宿主逃逸时源对象反被暴露给 early free，正是本判据要修的 bug）。

	// ReturnEscaped（P-A）：return 语句交出的裸标识符（排除形参/全局）。
	// 分配点提升的充分条件集——见字段注释。
	returnEscaped := make(map[string]bool)
	for _, n := range s.returnedNames {
		if n == "" || paramSet[n] {
			continue
		}
		returnEscaped[n] = true
	}

	return SafeContainerScan{
		Safe:           safe,
		WrittenThrough: wt,
		ChainLinked:    cl,
		Escaped:        escaped,
		ChainEscaping:  chainEscaping,
		ReturnEscaped:  returnEscaped,
	}
}

// writeThroughPair 记录一次写穿：(容器名, 被挂进去的源对象名)。
type writeThroughPair struct {
	container string
	source    string
}

// containerScan 持有遍历期间的累计状态。
type containerScan struct {
	declared          map[string]bool
	escaped           map[string]bool
	writtenThrough    map[string]bool
	writeThroughPairs []writeThroughPair
	aliasPairs        [][2]string       // 形如 (lhs, rhs)：lhs = rhs 的普通赋值（两侧都是标识符）
	assignTargets     [][2]string       // 同上，但用于判逃逸（lhs 非函数局部 → rhs 逃逸）
	lambdas           [][]ast.Statement // 收集到的 lambda 体，稍后单独分析捕获名
	// addressAliases: 形如 (valueVar, containerVar)：`i64 v = as<i64>(c)` /
	// `i64 v = <地址值表达式>`——v 是容器 c 的**地址值别名**（不是指针别名）。
	// 用于识别 `return head` 把链首带出函数的情形（见 ChainEscaping 注释）。
	addressAliases [][2]string
	// returnedNames: 各 return 语句返回值里出现的裸标识符（穿透 as<T>()/括号）。
	returnedNames []string
	// returnedNamesNested: 嵌套函数体内的 return 标识符（不进 ReturnEscaped——
	// 嵌套函数是独立生成单元，有自己的 ScanSafeLocalContainers 调用，
	// 其 return 逃逸通道属于它自己；混入外层会把外层同名变量误标为
	// return-escaped，触发分配点提升的多余 survivor 分配）。
	returnedNamesNested []string
	// nestedDepth: 当前遍历深度是否位于嵌套函数体内（>0 = 是）。
	nestedDepth int
}

// ---------------------------------------------------------------------------
// 语句遍历
// ---------------------------------------------------------------------------

func (s *containerScan) scanStmts(stmts []ast.Statement) {
	for _, stmt := range stmts {
		s.scanStmt(stmt)
	}
}

func (s *containerScan) scanStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	switch st := stmt.(type) {
	case *ast.VariableDeclaration:
		s.scanVarDecl(st)
	case *ast.ExpressionStatement:
		s.scanExprStmt(st)
	case *ast.IfStatement:
		s.scanExpr(st.Condition)
		s.scanStmts(st.Body)
		s.scanStmts(st.Else)
	case *ast.WhileStatement:
		s.scanExpr(st.Condition)
		s.scanStmts(st.Body)
	case *ast.ForStatement:
		// Init 里声明的循环变量也是函数局部变量
		if st.Init != nil {
			s.scanStmt(st.Init)
		}
		s.scanExpr(st.Condition)
		if st.Update != nil {
			s.scanStmt(st.Update)
		}
		s.scanStmts(st.Body)
	case *ast.ForInStatement:
		if st.Variable != nil {
			s.declared[st.Variable.Name] = true
		}
		s.scanExpr(st.Iterable)
		s.scanStmts(st.Body)
	case *ast.ReturnStatement:
		s.markEscapedExpr(st.Value)
		// 记录返回值里出现的**裸标识符**，供 ChainEscaping 结算：
		// `return head` 且 head 是某写穿容器的地址值别名 → 该容器的链首已交出去。
		// 嵌套函数体内的 return 分流到 returnedNamesNested（P-A：ReturnEscaped
		// 只认本函数自己的 return 通道；ChainEscaping 维持既有口径仍用外层集）。
		if s.nestedDepth == 0 {
			s.returnedNames = append(s.returnedNames, collectRootIdentifiers(st.Value)...)
		} else {
			s.returnedNamesNested = append(s.returnedNamesNested, collectRootIdentifiers(st.Value)...)
		}
	case *ast.YieldStatement:
		s.markEscapedExpr(st.Source)
	case *ast.ReleaseStatement:
		s.markEscapedExpr(st.Source)
	case *ast.ExtractStatement:
		s.markEscapedExpr(st.Source)
		s.scanExpr(st.Index)
	case *ast.BlockStatement:
		s.scanStmts(st.Statements)
	case *ast.CallStatement:
		s.scanExpr(st.Target)
		s.scanStmts(st.Body)
	case *ast.SwitchStatement:
		s.scanExpr(st.Scrutinee)
		for _, c := range st.Cases {
			for _, v := range c.Values {
				s.scanExpr(v)
			}
			s.scanStmts(c.Body)
		}
		s.scanStmts(st.Default)
	case *ast.SpendStatement:
		s.scanExpr(st.Target)
		for _, clause := range st.Calls {
			if clause == nil {
				continue
			}
			if clause.Index != nil {
				s.scanExpr(clause.Index)
			}
			s.scanStmts(clause.Body)
		}
	case *ast.PrefixStatement:
		s.scanStmts(st.Body)
	case *ast.TreeStatement:
		s.scanExpr(st.Root)
		s.scanStmts(st.Body)
	case *ast.ObjectStatement:
		for _, f := range st.Fields {
			s.scanExpr(f)
		}
		s.scanExpr(st.Value)
	case *ast.FunctionStatement:
		// 嵌套函数：其形参与本函数局部集合互不影响；仅登记其名（不是容器）。
		// return 标识符分流（nestedDepth>0 时不进本层 returnedNames），
		// 但 declared/escaped/writtenThrough 维持既有口径继续合并扫描。
		s.nestedDepth++
		s.scanStmts(st.Body)
		s.nestedDepth--
	default:
		// 其他语句类型（break/continue/import/struct/...）不影响容器逃逸判定
	}
}

func (s *containerScan) scanVarDecl(st *ast.VariableDeclaration) {
	if st == nil {
		return
	}
	s.declared[st.Name] = true
	for _, extra := range st.More {
		if extra != nil {
			s.declared[extra.Name] = true
		}
	}
	// 声明初始化 `T x = y` 与赋值 `x = y` 语义一致：登记别名对，
	// 并让 y 的逃逸性通过别名闭包传播到 x（反之亦然）。
	// 例：`Node* head = cur` 之后 `return head` → head 逃逸 → cur 必须一并逃逸。
	if rhs, okR := unwrapAssignSource(st.Value); okR {
		s.aliasPairs = append(s.aliasPairs, [2]string{st.Name, rhs.Name})
		s.assignTargets = append(s.assignTargets, [2]string{st.Name, rhs.Name})
	}
	// 地址值别名：`i64 head = as<i64>(cur)`。
	//
	// as<i64>(cur) 在词法上不是"指针别名"（类型不同），但它把 cur 指向的
	// **地址**存进了 head。若随后 `return head`，整条链的首节点就随返回值
	// 离开函数——cur 必须按"已逃出"处理，不能当安全局部容器。
	// 单独记一张表：(head, cur)。
	if addr, okA := unwrapAddressSource(st.Value); okA {
		s.addressAliases = append(s.addressAliases, [2]string{st.Name, addr.Name})
	}
	// 右值表达式整体扫描；其内出现的名字是"使用"而非"声明"
	s.scanExpr(st.Value)
	for _, extra := range st.More {
		if extra != nil {
			if rhs, okR := unwrapAssignSource(extra.Value); okR {
				s.aliasPairs = append(s.aliasPairs, [2]string{extra.Name, rhs.Name})
				s.assignTargets = append(s.assignTargets, [2]string{extra.Name, rhs.Name})
			}
			s.scanExpr(extra.Value)
		}
	}
}

// scanExprStmt 处理表达式语句。除常规扫描外，额外识别**写穿**：
//
//	obj.field = rhs    → obj 是写穿容器
//	arr[i]   = rhs     → arr 是写穿容器
//
// 写穿容器名进入 writtenThrough 集合（稍后与 Safe 求交）。这用于区分
// 「写穿 + 游标推进」与「纯游标推进」——只有前者才让游标也放弃 promote。
func (s *containerScan) scanExprStmt(st *ast.ExpressionStatement) {
	if st == nil {
		return
	}
	bin, ok := st.Expression.(*ast.BinaryExpression)
	if ok && (bin.Operator == "=" || bin.Operator == "ASSIGN") {
		containerName := ""
		switch lhs := bin.Left.(type) {
		case *ast.MemberAccessExpression:
			if obj, okObj := lhs.Object.(*ast.Identifier); okObj {
				containerName = obj.Name
			}
		case *ast.IndexExpression:
			if obj, okObj := lhs.Object.(*ast.Identifier); okObj {
				containerName = obj.Name
			}
		}
		if containerName != "" {
			s.writtenThrough[containerName] = true
			// 记录 (容器, 源对象)：稍后若容器落入 Safe 集合，则源对象即为
			// ChainLinked，必须豁免 early free（对象已挂在链上）。
			if src, okSrc := unwrapAssignSource(bin.Right); okSrc {
				s.writeThroughPairs = append(s.writeThroughPairs, writeThroughPair{
					container: containerName,
					source:    src.Name,
				})
			}
			s.scanExpr(bin.Left)
			s.scanExpr(bin.Right)
			return
		}
		// 普通赋值 `lhs = rhs`（lhs 不是写穿目标）。
		//
		// 关键区分：rhs 是否"逃逸"取决于 lhs 的生命周期是否更长。
		//   - lhs 是函数局部变量 → 只是把对象换个局部名字持有（别名），
		//     不是逃逸（`cur = nd`、`head = cur` 都是这种）；
		//   - lhs 是全局/形参/未声明的外部符号 → 对象被交给函数外的持有者，
		//     rhs 逃逸（`global_p = p`、`param.field = p` 由写穿分支处理）。
		//
		// 这里先统一登记别名对 (lhs, rhs)，再按"lhs 是否为函数局部"决定
		// 是否把 rhs 记为逃逸。注意 declared 在本函数体遍历完之前不完整，
		// 故逃逸判定推迟到 ScanSafeLocalContainers 末尾统一做。
		s.scanExpr(bin.Left)
		if lhs, okL := bin.Left.(*ast.Identifier); okL {
			if rhs, okR := unwrapAssignSource(bin.Right); okR {
				s.aliasPairs = append(s.aliasPairs, [2]string{lhs.Name, rhs.Name})
				s.assignTargets = append(s.assignTargets, [2]string{lhs.Name, rhs.Name})
				s.scanExpr(bin.Right)
				return
			}
		}
		// lhs 非简单标识符（成员访问等）或 rhs 不是标识符：rhs 整体按逃逸处理。
		s.markEscapedExpr(bin.Right)
		return
	}
	s.scanExpr(st.Expression)
}

// resolveAssignEscape 结算普通赋值的逃逸性：`lhs = rhs` 中若 lhs 不是本函数
// 声明的局部变量（即全局 / 形参 / 其它外部符号），则 rhs 被交给函数外的持有者，
// rhs 逃逸。
//
// 例：
//
//	Node* p = alloc(16)
//	global_head = p      # global_head 是全局 → p 逃逸
//	cur = p              # cur 是局部 → 只是别名，p/cur 都不因此逃逸
func resolveAssignEscape(escaped, declared, paramSet map[string]bool, pairs [][2]string) map[string]bool {
	for _, p := range pairs {
		lhs, rhs := p[0], p[1]
		if declared[lhs] && !paramSet[lhs] {
			continue // lhs 是函数局部 → 不逃逸
		}
		// lhs 非函数局部（全局/形参/外部符号/未声明）→ rhs 逃逸
		escaped[rhs] = true
	}
	return escaped
}

// closureAliasEscape 对别名对做闭包传播：同一等价类中任一名字逃逸 → 整类逃逸。
// 用并查集合并 (lhs, rhs)，再把每个类的逃逸性扩散给全部成员。
// 只把逃逸性扩散给 declared（本函数声明的局部变量），避免把外部符号卷进来。
func closureAliasEscape(escaped map[string]bool, pairs [][2]string, declared map[string]bool) map[string]bool {
	if len(pairs) == 0 {
		return escaped
	}
	parent := make(map[string]string)
	var find func(string) string
	find = func(x string) string {
		if _, ok := parent[x]; !ok {
			parent[x] = x
			return x
		}
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for _, p := range pairs {
		union(p[0], p[1])
	}
	// 每个等价类的逃逸性
	classEscaped := make(map[string]bool)
	for name := range escaped {
		if escaped[name] {
			classEscaped[find(name)] = true
		}
	}
	for name := range parent {
		if classEscaped[find(name)] && declared[name] {
			escaped[name] = true
		}
	}
	return escaped
}

// ---------------------------------------------------------------------------
// 表达式遍历（普通上下文：只登记声明与逃逸，不做"逃逸右值"标记）
// ---------------------------------------------------------------------------

func (s *containerScan) scanExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		// 普通读取，不构成逃逸
	case *ast.ParenExpression:
		s.scanExpr(e.Inner)
	case *ast.TypeCastExpression:
		s.scanExpr(e.Expression)
	case *ast.UnaryExpression:
		s.scanExpr(e.Right)
	case *ast.BinaryExpression:
		s.scanExpr(e.Left)
		s.scanExpr(e.Right)
	case *ast.ConditionalExpression:
		s.scanExpr(e.Condition)
		s.scanExpr(e.TrueExpr)
		s.scanExpr(e.FalseExpr)
	case *ast.CallExpression:
		// 实参一律视为逃逸（对象被交给被调方）
		s.markEscapedExpr(e.Function)
		for _, a := range e.Args {
			s.markEscapedExpr(a)
		}
	case *ast.SpawnExpression:
		s.markEscapedExpr(e.Function)
		for _, a := range e.Args {
			s.markEscapedExpr(a)
		}
	case *ast.AwaitExpression:
		s.scanExpr(e.Handle)
	case *ast.IndexExpression:
		s.scanExpr(e.Object)
		s.scanExpr(e.Index)
	case *ast.MemberAccessExpression:
		s.scanExpr(e.Object)
	case *ast.MemberExpression:
		s.scanExpr(e.Object)
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			s.scanExpr(f.Value)
		}
	case *ast.MatchExpression:
		s.scanExpr(e.Target)
		for _, arm := range e.Arms {
			if arm != nil {
				s.scanStmts(arm.Body)
			}
		}
	case *ast.LambdaExpression:
		// 收集 lambda 体，稍后单独分析捕获名；lambda 自身作为外层表达式
		// 的一部分已由外层语句处理（如 `f = fn(){...}` → f 被标右值逃逸）。
		s.lambdas = append(s.lambdas, e.Body)
		s.scanStmts(e.Body) // 同时扫描体内声明（计入 declared，稍后由 capture 覆盖）
	case *ast.ComptimeExpression:
		s.scanExpr(e.Inner)
	case *ast.PrefixCallExpression:
		for _, v := range e.Params {
			s.scanExpr(v)
		}
		s.scanStmts(e.Body)
	case *ast.FieldNameExpression:
		s.scanExpr(e.Index)
	case *ast.FieldTypeExpression:
		s.scanExpr(e.Index)
	default:
		// 字面量 / sizeof / 类型名等：无子表达式需要处理
	}
}

// ---------------------------------------------------------------------------
// 逃逸上下文：把表达式里出现的**直接标识符**标记为逃逸
// ---------------------------------------------------------------------------

// markEscapedExpr 在"逃逸上下文"（return 值 / 实参 / yield 源 / release 源 /
// 赋值右值）里遍历表达式，把其中出现的标识符标记为逃逸。
// 穿透 as<T>() / (expr) / 一元 +/-/& 等纯包装，但**不**穿透成员访问与索引——
// `return cur.next` 返回的是 cur 所在对象的**字段值**，cur 本身并未逃逸
// （这正是 leak_writeonly 的形态，必须保持可回收）。
func (s *containerScan) markEscapedExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		s.escaped[e.Name] = true
	case *ast.ParenExpression:
		s.markEscapedExpr(e.Inner)
	case *ast.TypeCastExpression:
		s.markEscapedExpr(e.Expression)
	case *ast.UnaryExpression:
		s.markEscapedExpr(e.Right)
	case *ast.ConditionalExpression:
		s.markEscapedExpr(e.TrueExpr)
		s.markEscapedExpr(e.FalseExpr)
		s.scanExpr(e.Condition)
	default:
		// 成员访问 / 索引 / 复合表达式：只按普通上下文扫描（不标记逃逸）。
		// 保守起见，其子表达式仍走普通扫描以登记声明。
		s.scanExpr(expr)
	}
}

// collectLambdaCaptureNames 从 lambda 体里收集"自由变量"名（保守近似：
// 凡是 lambda 体内出现、且不是在其内部声明的名字，都视为捕获）。
//
// 这里不做精确的作用域解析——保守地把 lambda 体内所有出现的标识符名并入
// 外层逃逸集合。代价是：若某个容器恰好在闭包体内被提及，就不再被视为
// 安全局部容器（退化到旧路径，多一份泄漏），换来绝不会因误判而悬垂。
func collectLambdaCaptureNames(body []ast.Statement, out map[string]bool) {
	inner := &containerScan{
		declared: make(map[string]bool),
		escaped:  make(map[string]bool),
	}
	inner.scanStmts(body)
	for name := range inner.escaped {
		out[name] = true
	}
	// 对 lambda 体做一次"使用名"收集：任何被读取的名字都可能是外层变量的捕获。
	used := make(map[string]bool)
	collectUsedNames(body, used)
	for name := range used {
		out[name] = true
	}
}

// collectUsedNames 收集语句序列里所有被引用的标识符名（保守、忽略作用域）。
func collectUsedNames(stmts []ast.Statement, out map[string]bool) {
	for _, stmt := range stmts {
		collectUsedNamesStmt(stmt, out)
	}
}

func collectUsedNamesStmt(stmt ast.Statement, out map[string]bool) {
	if stmt == nil {
		return
	}
	switch st := stmt.(type) {
	case *ast.VariableDeclaration:
		collectUsedNamesExpr(st.Value, out)
		for _, extra := range st.More {
			if extra != nil {
				collectUsedNamesExpr(extra.Value, out)
			}
		}
	case *ast.ExpressionStatement:
		collectUsedNamesExpr(st.Expression, out)
	case *ast.IfStatement:
		collectUsedNamesExpr(st.Condition, out)
		collectUsedNames(st.Body, out)
		collectUsedNames(st.Else, out)
	case *ast.WhileStatement:
		collectUsedNamesExpr(st.Condition, out)
		collectUsedNames(st.Body, out)
	case *ast.ForStatement:
		collectUsedNamesStmt(st.Init, out)
		collectUsedNamesExpr(st.Condition, out)
		collectUsedNamesStmt(st.Update, out)
		collectUsedNames(st.Body, out)
	case *ast.ForInStatement:
		collectUsedNamesExpr(st.Iterable, out)
		collectUsedNames(st.Body, out)
	case *ast.ReturnStatement:
		collectUsedNamesExpr(st.Value, out)
	case *ast.YieldStatement:
		collectUsedNamesExpr(st.Source, out)
	case *ast.ReleaseStatement:
		collectUsedNamesExpr(st.Source, out)
	case *ast.ExtractStatement:
		collectUsedNamesExpr(st.Source, out)
		collectUsedNamesExpr(st.Index, out)
	case *ast.BlockStatement:
		collectUsedNames(st.Statements, out)
	case *ast.CallStatement:
		collectUsedNamesExpr(st.Target, out)
		collectUsedNames(st.Body, out)
	case *ast.SwitchStatement:
		collectUsedNamesExpr(st.Scrutinee, out)
		for _, c := range st.Cases {
			for _, v := range c.Values {
				collectUsedNamesExpr(v, out)
			}
			collectUsedNames(c.Body, out)
		}
		collectUsedNames(st.Default, out)
	case *ast.PrefixStatement:
		collectUsedNames(st.Body, out)
	case *ast.TreeStatement:
		collectUsedNamesExpr(st.Root, out)
		collectUsedNames(st.Body, out)
	}
}

func collectUsedNamesExpr(expr ast.Expression, out map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		out[e.Name] = true
	case *ast.ParenExpression:
		collectUsedNamesExpr(e.Inner, out)
	case *ast.TypeCastExpression:
		collectUsedNamesExpr(e.Expression, out)
	case *ast.UnaryExpression:
		collectUsedNamesExpr(e.Right, out)
	case *ast.BinaryExpression:
		collectUsedNamesExpr(e.Left, out)
		collectUsedNamesExpr(e.Right, out)
	case *ast.ConditionalExpression:
		collectUsedNamesExpr(e.Condition, out)
		collectUsedNamesExpr(e.TrueExpr, out)
		collectUsedNamesExpr(e.FalseExpr, out)
	case *ast.CallExpression:
		collectUsedNamesExpr(e.Function, out)
		for _, a := range e.Args {
			collectUsedNamesExpr(a, out)
		}
	case *ast.SpawnExpression:
		collectUsedNamesExpr(e.Function, out)
		for _, a := range e.Args {
			collectUsedNamesExpr(a, out)
		}
	case *ast.AwaitExpression:
		collectUsedNamesExpr(e.Handle, out)
	case *ast.IndexExpression:
		collectUsedNamesExpr(e.Object, out)
		collectUsedNamesExpr(e.Index, out)
	case *ast.MemberAccessExpression:
		collectUsedNamesExpr(e.Object, out)
	case *ast.MemberExpression:
		collectUsedNamesExpr(e.Object, out)
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			collectUsedNamesExpr(f.Value, out)
		}
	case *ast.MatchExpression:
		collectUsedNamesExpr(e.Target, out)
		for _, arm := range e.Arms {
			if arm != nil {
				collectUsedNames(arm.Body, out)
			}
		}
	case *ast.LambdaExpression:
		collectUsedNames(e.Body, out)
	case *ast.ComptimeExpression:
		collectUsedNamesExpr(e.Inner, out)
	}
}

// unwrapAddressSource 从表达式中取出"地址值别名"的源容器标识符。
//
// 识别 `as<i64>(cur)` / `as<u64>(ptr)` / `(cur)` 这类形式——它们把容器的
// **地址**（而非对象本身）存进目标变量，词法上不是指针别名，但同样会把
// 链首带出函数（`i64 head = as<i64>(cur); ...; return head`）。
//
// 只接受**纯标识符**作为源（穿透 as<T>() 与括号）：`as<i64>(cur.next)` 这种
// 成员访问的地址属于链中某个节点，不属于容器本身，不登记。
func unwrapAddressSource(expr ast.Expression) (*ast.Identifier, bool) {
	cast, ok := expr.(*ast.TypeCastExpression)
	if !ok {
		return nil, false
	}
	inner := cast.Expression
	for inner != nil {
		switch e := inner.(type) {
		case *ast.Identifier:
			return e, true
		case *ast.ParenExpression:
			inner = e.Inner
		default:
			return nil, false
		}
	}
	return nil, false
}

// collectRootIdentifiers 收集返回值表达式里出现的**裸标识符**（穿透 as<T>()/括号）。
//
// 用途限定：判断 `return head` 是否把某个写穿容器的链首带出去。
// 只在表达式本身（或其 as<T>()/括号包装）是裸标识符时收集——成员访问/索引
// 等复合表达式不算（`return cur.next` 返回的是链中节点，不是容器链首）。
func collectRootIdentifiers(expr ast.Expression) []string {
	var out []string
	for expr != nil {
		switch e := expr.(type) {
		case *ast.Identifier:
			out = append(out, e.Name)
			return out
		case *ast.ParenExpression:
			expr = e.Inner
		case *ast.TypeCastExpression:
			expr = e.Expression
		default:
			return out
		}
	}
	return out
}
