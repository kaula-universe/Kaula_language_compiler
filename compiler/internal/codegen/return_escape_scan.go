package codegen

import (
	"strings"

	"kaula/internal/ast"
)

// ============================================================================
// 返回链逃逸预扫描（「去逐对象提升」·链构造器识别）
// ============================================================================
//
// 背景：P-A（分配点提升）只为「单点 return」服务——每个 return 交出的裸指针
// 在分配点直接进 survivor 段。递归构造形态（binary_trees 的 bottomUpTree）
// 每层返回一个节点，P-A 仍要每层调用一次 kmm_v4_scope_promote_p 做浅拷贝，
// 且每个节点都要在块尾/返回路径上逐对象回收。depth=22 时峰值 RSS 678MB，
// 而 C-malloc 对拍 263MB；depth=25 直接 OOM。
//
// 本扫描识别「链构造器」这一类函数：函数体内所有堆对象都只通过
//   - kmm_v4_alloc / kmm_v4_alloc_auto / kmm_v4_bump（bump 区分配）
//   - 对链构造器的递归/互递归调用
// 产生，并且每个对象都归属于最终 return 出去的那条链（直接返回，或经字段
// 写穿挂到已归属节点上）。对这类函数，codegen 可以整条链留在**调用方**的
// bump 区：
//   - 不发射 return promote（浅拷贝到 survivor 段）；
//   - 不发射任何 per-object survivor_free / deep free；
//   - 不发射块级 mark_restore（否则分支退出就把刚建好的子树回收掉）；
//   - 不建立函数级 KMM 作用域（节点归调用方作用域所有，调用方的
//     scope_pop / mark_restore 一锅端回收）。
//
// 安全性：识别是**全有或全无**的保守判定。任何一个值不满足「归属返回链」
// 就整个函数退出链构造器集合；任何一个调用点可能让链指针逃出调用方
// bump 区，就把被调链构造器从集合里摘掉（并级联重验依赖它的函数）。
// 判错的最坏后果是退回原有 promote 路径（多一次拷贝、多一些回收），
// 不会把仍被外部持有的对象留在会被回卷的 bump 区。
//
// 本文件只做纯词法 AST 遍历，不依赖符号表 / SOR，可在函数体生成前运行。

// ChainFunctionInfo 描述一个已确认安全的链构造器。
type ChainFunctionInfo struct {
	// Name 函数名（Kaula 侧，不含 C 前缀）
	Name string
	// Values 函数体内所有「链值」变量：bump 分配结果 + 对链构造器的调用结果。
	// 每个 Values 成员都必须出现在 Owned 中（否则函数不成立）。
	Values map[string]bool
	// Owned 归属于最终返回链的值：ReturnVars 加上经字段写穿从它们可达的值。
	Owned map[string]bool
	// ReturnVars 被 return 直接交出的值（不含 null 返回）。
	ReturnVars map[string]bool
}

// ReturnEscapeScan 是全局预扫描结果。
type ReturnEscapeScan struct {
	// ChainFuncs 已确认的链构造器（函数名 → 信息）。
	ChainFuncs map[string]*ChainFunctionInfo
	// Receivers 非链构造函数中「安全接收链指针」的局部变量：
	// 函数名 → 变量名集合。这些变量只做只读/借用，不会把链带出当前
	// bump 区，因此 codegen 对它们同样跳过 promote / per-object free。
	Receivers map[string]map[string]bool
}

// 链构造器允许的 bump 分配入口。刻意排除 kmm_v4_alloc_global / promote：
// 它们把对象放进 survivor 段，不在调用方 bump 区的回收语义内。
var chainAllocFuncNames = map[string]bool{
	"kmm_v4_alloc":      true,
	"kmm_v4_alloc_auto": true,
	"kmm_v4_bump":       true,
	"std_malloc":        true,
	"malloc":            true,
	"calloc":            true,
}

func isChainAllocName(name string) bool {
	return chainAllocFuncNames[name]
}

// ScanReturnEscapeChains 扫描整个程序，返回链构造器与安全接收者集合。
//
// 算法分四步：
//  1. 计算「借用函数」最大不动点（只读指针形参、绝不把形参交出去）；
//  2. 以 bump 分配/链调用为种子，迭代扩张链构造器集合；
//  3. 全局校验调用点：任何可能让链指针逃出 bump 区的用法，摘掉对应
//     链构造器并级联重验（去固定点）；
//  4. 在非链构造函数中收集通过校验的接收者变量。
func ScanReturnEscapeChains(program *ast.Program) *ReturnEscapeScan {
	scan := &ReturnEscapeScan{
		ChainFuncs: make(map[string]*ChainFunctionInfo),
		Receivers:  make(map[string]map[string]bool),
	}
	if program == nil {
		return scan
	}

	fns := make(map[string]*ast.FunctionStatement)
	for _, stmt := range program.Statements {
		fn, ok := stmt.(*ast.FunctionStatement)
		if !ok || fn == nil || fn.Name == "" {
			continue
		}
		fns[fn.Name] = fn
	}
	if len(fns) == 0 {
		return scan
	}

	borrowOnly := computeBorrowOnlyFunctions(fns)

	// 步骤 2：链构造器最大不动点。每轮把新确认的函数加入集合，直到不再增长。
	for {
		changed := false
		for name, fn := range fns {
			if _, ok := scan.ChainFuncs[name]; ok {
				continue
			}
			if info, ok := analyzeChainFunction(fn, scan.ChainFuncs, borrowOnly); ok {
				scan.ChainFuncs[name] = info
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// 步骤 3：全局调用点校验 + 级联移除。
	for {
		bad := make(map[string]bool)
		for name, fn := range fns {
			if _, isChain := scan.ChainFuncs[name]; isChain {
				continue
			}
			assessChainReceivers(fn, scan.ChainFuncs, borrowOnly, nil, bad)
		}
		if len(bad) == 0 {
			break
		}
		for name := range bad {
			delete(scan.ChainFuncs, name)
		}
		// 被调链构造器被摘掉后，依赖它的调用者可能不再成立（其链调用结果
		// 不再是 Values），必须重验；反复直到集合稳定。
		for {
			removed := false
			for name, info := range scan.ChainFuncs {
				fresh, ok := analyzeChainFunction(fns[name], scan.ChainFuncs, borrowOnly)
				if !ok || !sameChainValueSet(info, fresh) {
					delete(scan.ChainFuncs, name)
					removed = true
				}
			}
			if !removed {
				break
			}
		}
	}

	// 步骤 4：收集安全的接收者变量（仅非链构造函数）。
	for name, fn := range fns {
		if _, isChain := scan.ChainFuncs[name]; isChain {
			continue
		}
		receivers := make(map[string]bool)
		assessChainReceivers(fn, scan.ChainFuncs, borrowOnly, receivers, nil)
		if len(receivers) > 0 {
			scan.Receivers[name] = receivers
		}
	}

	return scan
}

func sameChainValueSet(a, b *ChainFunctionInfo) bool {
	if a == nil || b == nil || len(a.Values) != len(b.Values) {
		return false
	}
	for name := range a.Values {
		if !b.Values[name] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// 链构造器判定
// ---------------------------------------------------------------------------

// chainInitKind 分类变量声明的初始化式（穿透 as<T>() / 括号）：
//   - "alloc"：bump 分配调用
//   - "call"：普通调用（callee 可能是链构造器）
//   - "null"：null 字面量
//   - "alias"：裸标识符
//   - ""：其它（字面量/成员访问/复合表达式）
type chainInitKind int

const (
	chainInitOther chainInitKind = iota
	chainInitAlloc
	chainInitCall
	chainInitNull
	chainInitAlias
)

type chainInitInfo struct {
	kind   chainInitKind
	callee string
	call   *ast.CallExpression
}

func classifyChainInit(expr ast.Expression) chainInitInfo {
	for expr != nil {
		switch e := expr.(type) {
		case *ast.ParenExpression:
			expr = e.Inner
		case *ast.TypeCastExpression:
			expr = e.Expression
		case *ast.CallExpression:
			name := calledFuncName(e.Function)
			if isChainAllocName(name) {
				return chainInitInfo{kind: chainInitAlloc, callee: name, call: e}
			}
			return chainInitInfo{kind: chainInitCall, callee: name, call: e}
		case *ast.Identifier:
			if isNullLikeName(e.Name) {
				return chainInitInfo{kind: chainInitNull}
			}
			return chainInitInfo{kind: chainInitAlias, callee: e.Name}
		default:
			return chainInitInfo{}
		}
	}
	return chainInitInfo{}
}

func isNullLikeName(name string) bool {
	return name == "null" || name == "NULL"
}

func isPointerDeclType(t string) bool {
	return strings.HasSuffix(strings.TrimSpace(t), "*")
}

// analyzeChainFunction 判定单个函数是否为链构造器。
//
// 形状约束（v1，刻意保守）：
//   - 顶层普通函数，非泛型/并发/asm/inline/no_kmm/注解/main，无闭包捕获；
//   - 返回单个指针；
//   - 函数体只含 VariableDeclaration / ExpressionStatement(赋值) /
//     IfStatement / ReturnStatement / BlockStatement；
//   - 无循环、无嵌套函数/lambda、无 switch/spend/prefix/tree、
//     无 yield/release/extract/call 语句。
//
// 数据流约束：
//   - 每个「链值」（bump 分配 / 链调用结果）都必须归属于返回链；
//   - 所有 return 只能返回 Owned 成员或 null；
//   - 所有字段写穿的目标必须是链值，源必须是链值或 null；
//   - 不允许任何普通变量赋值（游标推进/别名一律拒绝）。
func analyzeChainFunction(
	fn *ast.FunctionStatement,
	chainFuncs map[string]*ChainFunctionInfo,
	borrowOnly map[string]bool,
) (*ChainFunctionInfo, bool) {
	if fn == nil || fn.Name == "" {
		return nil, false
	}
	if fn.Name == "main" || fn.Generic || fn.IsConcurrent || fn.IsAsm ||
		fn.Inline || fn.NoKMM || fn.Annotation != ast.TreeAnnotationNone ||
		len(fn.Captures) > 0 {
		return nil, false
	}
	if !isPointerDeclType(fn.ReturnType) {
		return nil, false
	}

	a := &chainAnalysis{
		fn:         fn,
		chainFuncs: chainFuncs,
		borrowOnly: borrowOnly,
		values:     make(map[string]bool),
		decls:      make(map[string]bool),
	}
	if !a.walkStmts(fn.Body) {
		return nil, false
	}
	if a.unsafe || len(a.values) == 0 {
		return nil, false
	}

	// 归属闭包：从 return 值出发，沿「宿主字段 ← 源值」反向扩张。
	owned := make(map[string]bool, len(a.values))
	for _, name := range a.returns {
		if !a.values[name] {
			return nil, false
		}
		owned[name] = true
	}
	for {
		changed := false
		for _, fs := range a.fieldStores {
			if !a.values[fs.obj] {
				return nil, false
			}
			if !owned[fs.obj] {
				continue
			}
			if fs.src == "" || !a.values[fs.src] {
				continue
			}
			if !owned[fs.src] {
				owned[fs.src] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// 每个分配/链调用结果都必须挂在返回链上：局部临时对象若留在 bump 区
	// 会被调用方一起回收，语义上没错，但会让「链构造器」的边界变得模糊
	// （可能藏着被外部持有的对象）。全有或全无，宁可退回旧路径。
	for name := range a.values {
		if !owned[name] {
			return nil, false
		}
	}
	for _, name := range a.returns {
		if !owned[name] {
			return nil, false
		}
	}

	info := &ChainFunctionInfo{
		Name:       fn.Name,
		Values:     a.values,
		Owned:      owned,
		ReturnVars: make(map[string]bool, len(a.returns)),
	}
	for _, name := range a.returns {
		info.ReturnVars[name] = true
	}
	return info, true
}

type chainFieldStore struct {
	obj string
	src string // "" 表示 null
}

type chainAnalysis struct {
	fn         *ast.FunctionStatement
	chainFuncs map[string]*ChainFunctionInfo
	borrowOnly map[string]bool

	values      map[string]bool
	decls       map[string]bool
	returns     []string
	fieldStores []chainFieldStore
	unsafe      bool
}

func (a *chainAnalysis) markUnsafe() bool {
	a.unsafe = true
	return false
}

func (a *chainAnalysis) walkStmts(stmts []ast.Statement) bool {
	for _, stmt := range stmts {
		if !a.walkStmt(stmt) {
			return false
		}
	}
	return true
}

func (a *chainAnalysis) walkStmt(stmt ast.Statement) bool {
	if stmt == nil {
		return true
	}
	switch st := stmt.(type) {
	case *ast.VariableDeclaration:
		for _, d := range st.AllDeclarators() {
			if d == nil || d.Name == "" {
				continue
			}
			a.decls[d.Name] = true
			init := classifyChainInit(d.Value)
			isValue := false
			switch init.kind {
			case chainInitAlloc:
				isValue = true
			case chainInitCall:
				// 自递归调用在集合尚未包含本函数时也必须按链调用处理，
				// 否则 bottomUpTree 这类自递归构造器永远无法入选。
				if init.callee == a.fn.Name || a.chainFuncs[init.callee] != nil {
					isValue = true
				}
			}
			if isValue {
				if a.values[d.Name] {
					return a.markUnsafe() // 同名重声明/多初始化：边界不清
				}
				a.values[d.Name] = true
			} else if init.kind == chainInitAlias && a.values[init.callee] {
				// 链值别名（含隐式转成整数）等于复制地址：所有权/逃逸边界
				// 立刻变得不可判定，v1 一律拒绝。
				return a.markUnsafe()
			} else if isPointerDeclType(d.Type) {
				// 指针声明只允许三种来源：bump 分配、链构造器调用、null。
				// 别名（Node* q = n）会把归属关系复杂化，v1 直接拒绝。
				switch init.kind {
				case chainInitNull:
				case chainInitAlias:
					if a.values[init.callee] {
						return a.markUnsafe()
					}
					// 形参/全局别名：不参与链归属，但也不能进入返回值/字段。
					// 允许其存在（只读使用），后续 return/字段校验会拒绝。
				default:
					return a.markUnsafe()
				}
			}
			if d.Value != nil {
				if isValue {
					if !a.validateExpr(d.Value, init.call) {
						return false
					}
				} else {
					if !a.validateExpr(d.Value, nil) {
						return false
					}
				}
			}
		}
		return true

	case *ast.ExpressionStatement:
		bin, ok := st.Expression.(*ast.BinaryExpression)
		if !ok || (bin.Operator != "=" && bin.Operator != "ASSIGN") {
			// 表达式语句只允许赋值；裸调用可能隐藏一次分配/逃逸。
			return a.markUnsafe()
		}
		return a.walkChainAssign(bin)

	case *ast.IfStatement:
		if !a.validateExpr(st.Condition, nil) {
			return false
		}
		if !a.walkStmts(st.Body) {
			return false
		}
		return a.walkStmts(st.Else)

	case *ast.ReturnStatement:
		if st.Value == nil {
			return a.markUnsafe()
		}
		init := classifyChainInit(st.Value)
		switch init.kind {
		case chainInitNull:
			return true
		case chainInitAlias:
			a.returns = append(a.returns, init.callee)
			return true
		default:
			// return 成员访问/复合表达式：交出的是链中某节点而非本函数
			// 拥有的整条链，v1 无法保证归属，拒绝。
			return a.markUnsafe()
		}

	case *ast.BlockStatement:
		return a.walkStmts(st.Statements)

	default:
		return a.markUnsafe()
	}
}

// walkChainAssign 处理链函数内的赋值语句。
//
//	v = rhs          → 拒绝（游标推进/别名/重新绑定都会破坏全有或全无）
//	obj.field = src  → obj 必须是链值，src 必须是链值或 null
func (a *chainAnalysis) walkChainAssign(bin *ast.BinaryExpression) bool {
	if bin == nil {
		return a.markUnsafe()
	}
	member, ok := bin.Left.(*ast.MemberAccessExpression)
	if !ok {
		return a.markUnsafe()
	}
	obj, ok := member.Object.(*ast.Identifier)
	if !ok || !a.values[obj.Name] {
		return a.markUnsafe()
	}
	init := classifyChainInit(bin.Right)
	src := ""
	switch init.kind {
	case chainInitNull:
		// null 初始化字段：合法
	case chainInitAlias:
		if !a.values[init.callee] {
			return a.markUnsafe()
		}
		src = init.callee
	default:
		return a.markUnsafe()
	}
	a.fieldStores = append(a.fieldStores, chainFieldStore{obj: obj.Name, src: src})
	return true
}

// validateExpr 校验链函数内的表达式。allowCall 是声明初始化式根位置上的
// 分配/链调用（它是值的来源，不应按「普通调用」判定）。
func (a *chainAnalysis) validateExpr(expr ast.Expression, allowCall *ast.CallExpression) bool {
	if expr == nil {
		return true
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return true

	case *ast.ParenExpression:
		return a.validateExpr(e.Inner, allowCall)

	case *ast.TypeCastExpression:
		// 只允许把调用结果转成指针类型（as<Node*>(alloc(...))）。
		// 把链值转成整数/其它非指针类型等于泄漏地址，拒绝。
		if _, isCall := unwrapCallForCast(e.Expression); !isCall {
			if isPointerDeclType(e.TargetType) {
				// as<Node*>(n) 是别名，后面可能逃逸；v1 拒绝裸标识符强转。
				if root, ok := unwrapBareIdentifier(e.Expression); ok && a.values[root] {
					return a.markUnsafe()
				}
				return a.validateExpr(e.Expression, allowCall)
			}
			if a.exprUsesChainValue(e.Expression) {
				return a.markUnsafe()
			}
		}
		return a.validateExpr(e.Expression, allowCall)

	case *ast.UnaryExpression:
		if e.Operator != "!" && a.exprUsesBareChainValue(e.Right) {
			return a.markUnsafe()
		}
		if e.Operator == "&" && a.exprUsesChainValue(e.Right) {
			return a.markUnsafe()
		}
		return a.validateExpr(e.Right, allowCall)

	case *ast.BinaryExpression:
		if e.Operator == "=" || e.Operator == "ASSIGN" {
			return a.markUnsafe()
		}
		// 比较/逻辑运算只消费指针值，安全；其余算术（尤其指针相减）
		// 会把链地址变成整数，v1 拒绝。
		if !isComparisonOrLogicalOp(e.Operator) && a.exprUsesBareChainValue(e) {
			return a.markUnsafe()
		}
		return a.validateExpr(e.Left, allowCall) && a.validateExpr(e.Right, allowCall)

	case *ast.ConditionalExpression:
		return a.validateExpr(e.Condition, allowCall) &&
			a.validateExpr(e.TrueExpr, allowCall) &&
			a.validateExpr(e.FalseExpr, allowCall)

	case *ast.CallExpression:
		if allowCall != nil && e == allowCall {
			// 分配/链调用本身允许；实参不允许夹带链值（会把节点当地址传走）。
			for _, arg := range e.Args {
				if a.exprUsesChainValue(arg) {
					return a.markUnsafe()
				}
				if !a.validateExpr(arg, nil) {
					return false
				}
			}
			return true
		}
		callee := calledFuncName(e.Function)
		borrow := a.borrowOnly[callee]
		for _, arg := range e.Args {
			usesChain := a.exprUsesChainValue(arg)
			if usesChain && !borrow {
				return a.markUnsafe()
			}
			if !a.validateExpr(arg, nil) {
				return false
			}
		}
		return a.validateExpr(e.Function, nil)

	case *ast.MemberAccessExpression:
		return a.validateExpr(e.Object, allowCall)

	case *ast.MemberExpression:
		return a.validateExpr(e.Object, allowCall)

	case *ast.IndexExpression:
		return a.validateExpr(e.Object, allowCall) && a.validateExpr(e.Index, nil)

	case *ast.SpawnExpression:
		return a.markUnsafe()
	case *ast.AwaitExpression:
		return a.markUnsafe()
	case *ast.LambdaExpression:
		return a.markUnsafe()
	case *ast.MatchExpression:
		return a.markUnsafe()
	case *ast.StructLiteral:
		if a.exprUsesChainValue(expr) {
			return a.markUnsafe()
		}
		for _, f := range e.Fields {
			if !a.validateExpr(f.Value, nil) {
				return false
			}
		}
		return true
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			if a.exprUsesChainValue(el) {
				return a.markUnsafe()
			}
			if !a.validateExpr(el, nil) {
				return false
			}
		}
		return true
	case *ast.ComptimeExpression:
		if a.exprUsesChainValue(e.Inner) {
			return a.markUnsafe()
		}
		return a.validateExpr(e.Inner, nil)

	default:
		// 字面量 / sizeof 等：无子表达式。
		return true
	}
}

func (a *chainAnalysis) exprUsesChainValue(expr ast.Expression) bool {
	used := make(map[string]bool)
	collectUsedNamesExpr(expr, used)
	for name := range used {
		if a.values[name] {
			return true
		}
	}
	return false
}

// exprUsesBareChainValue 报告 expr 是否把链指针**本身**当值使用。
// 成员访问/下标是解引用读取（n.value / n[0]），不算裸指针；算术、强转、
// 普通调用实参等位置出现裸链值则视为地址泄漏（v1 保守拒绝）。
func (a *chainAnalysis) exprUsesBareChainValue(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return a.values[e.Name]
	case *ast.ParenExpression:
		return a.exprUsesBareChainValue(e.Inner)
	case *ast.MemberAccessExpression:
		return false
	case *ast.MemberExpression:
		return false
	case *ast.IndexExpression:
		return a.exprUsesBareChainValue(e.Index)
	case *ast.TypeCastExpression:
		return a.exprUsesChainValue(e.Expression)
	case *ast.UnaryExpression:
		return a.exprUsesBareChainValue(e.Right)
	case *ast.BinaryExpression:
		return a.exprUsesBareChainValue(e.Left) || a.exprUsesBareChainValue(e.Right)
	case *ast.ConditionalExpression:
		return a.exprUsesBareChainValue(e.Condition) ||
			a.exprUsesBareChainValue(e.TrueExpr) ||
			a.exprUsesBareChainValue(e.FalseExpr)
	case *ast.CallExpression:
		if a.exprUsesBareChainValue(e.Function) {
			return true
		}
		for _, arg := range e.Args {
			if a.exprUsesBareChainValue(arg) {
				return true
			}
		}
		return false
	default:
		return a.exprUsesChainValue(expr)
	}
}

func unwrapCallForCast(expr ast.Expression) (*ast.CallExpression, bool) {
	for expr != nil {
		switch e := expr.(type) {
		case *ast.ParenExpression:
			expr = e.Inner
		case *ast.CallExpression:
			return e, true
		default:
			return nil, false
		}
	}
	return nil, false
}

func unwrapBareIdentifier(expr ast.Expression) (string, bool) {
	for expr != nil {
		switch e := expr.(type) {
		case *ast.ParenExpression:
			expr = e.Inner
		case *ast.TypeCastExpression:
			expr = e.Expression
		case *ast.Identifier:
			return e.Name, true
		default:
			return "", false
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// 借用函数（borrow-only）不动点
// ---------------------------------------------------------------------------

// computeBorrowOnlyFunctions 计算「只借用指针形参」的函数集合。
//
// 判据（全部满足）：
//   - 顶层普通函数，非 main；
//   - 返回非指针（标量/void）——返回指针的函数可能把形参/新分配交出去；
//   - 函数体内无 spawn/yield/release/extract/lambda/嵌套函数；
//   - 指针形参只被读：成员访问、下标读、与 null 比较、传给另一个
//     borrow-only 函数；不得被赋值、写字段、强转、取地址、return、
//     传给非 borrow-only 函数或闭包捕获。
//
// 用途：链接收者把链指针传给 itemCheck 这类只读遍历函数是安全的；
// 没有这张表，任何实参传递都会被判成逃逸，链优化在真实程序里无法生效。
func computeBorrowOnlyFunctions(fns map[string]*ast.FunctionStatement) map[string]bool {
	borrowOnly := make(map[string]bool, len(fns))
	for name, fn := range fns {
		if fn == nil || name == "main" || fn.Generic || fn.IsConcurrent ||
			fn.IsAsm || fn.Inline || fn.NoKMM ||
			fn.Annotation != ast.TreeAnnotationNone || len(fn.Captures) > 0 {
			continue
		}
		// 返回类型必须显式且不是指针：空返回类型可能由实现推断/漏写，
		// 不能假设它不返回指针；任何含 '*' 的类型一律排除。
		if fn.ReturnType == "" || strings.Contains(fn.ReturnType, "*") {
			continue
		}
		borrowOnly[name] = true
	}

	for {
		changed := false
		for name := range borrowOnly {
			fn := fns[name]
			if fn == nil || !borrowParametersSafe(fn, borrowOnly) {
				delete(borrowOnly, name)
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return borrowOnly
}

func borrowParametersSafe(fn *ast.FunctionStatement, borrowOnly map[string]bool) bool {
	params := make(map[string]bool)
	for i, p := range fn.Params {
		if p == "" || i >= len(fn.ParamTypes) || !isPointerDeclType(fn.ParamTypes[i]) {
			continue
		}
		params[p] = true
	}
	if len(params) == 0 {
		return true
	}
	w := &borrowWalker{params: params, borrowOnly: borrowOnly}
	return w.walkStmts(fn.Body)
}

type borrowWalker struct {
	params     map[string]bool
	borrowOnly map[string]bool
	safe       bool
}

func (w *borrowWalker) walkStmts(stmts []ast.Statement) bool {
	for _, stmt := range stmts {
		if !w.walkStmt(stmt) {
			return false
		}
	}
	return true
}

func (w *borrowWalker) usesParam(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	used := make(map[string]bool)
	collectUsedNamesExpr(expr, used)
	for name := range used {
		if w.params[name] {
			return true
		}
	}
	return false
}

func (w *borrowWalker) walkStmt(stmt ast.Statement) bool {
	if stmt == nil {
		return true
	}
	switch st := stmt.(type) {
	case *ast.VariableDeclaration:
		for _, d := range st.AllDeclarators() {
			if d == nil || d.Value == nil {
				continue
			}
			// 形参赋值给任何变量都等于把指针交给另一个持有者。
			if w.usesParam(d.Value) {
				return false
			}
			if !w.walkExpr(d.Value, false) {
				return false
			}
		}
		return true
	case *ast.ExpressionStatement:
		bin, ok := st.Expression.(*ast.BinaryExpression)
		if ok && (bin.Operator == "=" || bin.Operator == "ASSIGN") {
			// 写形参本身 / 写形参字段 / 写数组元素：都视为把借用内存
			// 交给更长生命周期或改变其内容，保守拒绝。
			if w.usesParam(bin.Left) || w.usesParam(bin.Right) {
				return false
			}
			return w.walkExpr(bin.Left, false) && w.walkExpr(bin.Right, false)
		}
		return w.walkExpr(st.Expression, false)
	case *ast.IfStatement:
		return w.walkExpr(st.Condition, false) && w.walkStmts(st.Body) && w.walkStmts(st.Else)
	case *ast.WhileStatement:
		return w.walkExpr(st.Condition, false) && w.walkStmts(st.Body)
	case *ast.ForStatement:
		if st.Init != nil && !w.walkStmt(st.Init) {
			return false
		}
		if !w.walkExpr(st.Condition, false) {
			return false
		}
		if st.Update != nil && !w.walkStmt(st.Update) {
			return false
		}
		return w.walkStmts(st.Body)
	case *ast.ForInStatement:
		return w.walkExpr(st.Iterable, false) && w.walkStmts(st.Body)
	case *ast.ReturnStatement:
		// 返回非指针类型（已在入口约束）：允许返回由形参字段读取/借用调用
		// 计算出的标量（itemCheck 的 `1 + itemCheck(n.left)`）。裸形参、
		// 强转/取地址仍由 walkExpr 拒绝——那等于把指针交出去。
		if name, ok := unwrapBareIdentifier(st.Value); ok && w.params[name] {
			return false
		}
		return w.walkExpr(st.Value, false)
	case *ast.YieldStatement:
		return !w.usesParam(st.Source) && w.walkExpr(st.Source, false)
	case *ast.ReleaseStatement:
		return false
	case *ast.ExtractStatement:
		return false
	case *ast.BlockStatement:
		return w.walkStmts(st.Statements)
	case *ast.SwitchStatement:
		if !w.walkExpr(st.Scrutinee, false) {
			return false
		}
		for _, c := range st.Cases {
			for _, v := range c.Values {
				if w.usesParam(v) || !w.walkExpr(v, false) {
					return false
				}
			}
			if !w.walkStmts(c.Body) {
				return false
			}
		}
		return w.walkStmts(st.Default)
	case *ast.CallStatement:
		return false
	case *ast.SpendStatement:
		return false
	case *ast.PrefixStatement:
		return false
	case *ast.TreeStatement:
		return false
	case *ast.FunctionStatement:
		return false
	default:
		// 未知语句：若引用形参则保守拒绝。
		used := make(map[string]bool)
		collectUsedNamesStmt(stmt, used)
		for name := range used {
			if w.params[name] {
				return false
			}
		}
		return true
	}
}

func (w *borrowWalker) walkExpr(expr ast.Expression, borrowed bool) bool {
	if expr == nil {
		return true
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return true
	case *ast.ParenExpression:
		return w.walkExpr(e.Inner, borrowed)
	case *ast.TypeCastExpression:
		// 指针形参不得被强转（转整数=泄漏地址，转别的指针=别名）。
		if w.usesParam(e.Expression) {
			return false
		}
		return w.walkExpr(e.Expression, borrowed)
	case *ast.UnaryExpression:
		if e.Operator == "&" && w.usesParam(e.Right) {
			return false
		}
		return w.walkExpr(e.Right, borrowed)
	case *ast.BinaryExpression:
		if e.Operator == "=" || e.Operator == "ASSIGN" {
			return false
		}
		return w.walkExpr(e.Left, borrowed) && w.walkExpr(e.Right, borrowed)
	case *ast.ConditionalExpression:
		return w.walkExpr(e.Condition, borrowed) &&
			w.walkExpr(e.TrueExpr, borrowed) &&
			w.walkExpr(e.FalseExpr, borrowed)
	case *ast.CallExpression:
		callee := calledFuncName(e.Function)
		if w.usesParam(e.Function) {
			return false
		}
		borrowCallee := w.borrowOnly[callee]
		for _, arg := range e.Args {
			if w.usesParam(arg) && !borrowCallee {
				return false
			}
			if !w.walkExpr(arg, true) {
				return false
			}
		}
		return true
	case *ast.MemberAccessExpression:
		return w.walkExpr(e.Object, borrowed)
	case *ast.MemberExpression:
		return w.walkExpr(e.Object, borrowed)
	case *ast.IndexExpression:
		if w.usesParam(e.Index) {
			return false
		}
		return w.walkExpr(e.Object, borrowed) && w.walkExpr(e.Index, borrowed)
	case *ast.SpawnExpression:
		return false
	case *ast.AwaitExpression:
		return w.walkExpr(e.Handle, borrowed)
	case *ast.LambdaExpression:
		return false
	case *ast.MatchExpression:
		return false
	case *ast.StructLiteral:
		if w.usesParam(expr) {
			return false
		}
		for _, f := range e.Fields {
			if !w.walkExpr(f.Value, borrowed) {
				return false
			}
		}
		return true
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			if w.usesParam(el) {
				return false
			}
			if !w.walkExpr(el, borrowed) {
				return false
			}
		}
		return true
	default:
		return !w.usesParam(expr)
	}
}

// ---------------------------------------------------------------------------
// 调用方接收者校验
// ---------------------------------------------------------------------------

type receiverDecl struct {
	name        string
	path        []int
	pointer     bool
	chainCallee string
}

type receiverUse struct {
	name   string
	path   []int
	escape bool
	nested bool
}

type chainCallSite struct {
	callee    string
	declName  string
	declPath  []int
	path      []int
	nested    bool
	call      *ast.CallExpression
	rootInDec bool
}

type receiverWalker struct {
	decls  map[string][]receiverDecl
	uses   []receiverUse
	calls  []chainCallSite
	params map[string]bool
	pathID int
}

type receiverCtx struct {
	escape      bool
	nested      bool
	declName    string
	declPath    []int
	rootCall    *ast.CallExpression
	pointerDecl bool
}

func assessChainReceivers(
	fn *ast.FunctionStatement,
	chainFuncs map[string]*ChainFunctionInfo,
	borrowOnly map[string]bool,
	safeOut map[string]bool,
	badOut map[string]bool,
) {
	if fn == nil {
		return
	}
	w := &receiverWalker{
		decls:  make(map[string][]receiverDecl),
		params: make(map[string]bool),
	}
	for _, p := range fn.Params {
		w.params[p] = true
	}
	w.walkStmts(fn.Body, nil, false, chainFuncs, borrowOnly)

	// 按声明名归并：候选接收者必须能在同一词法块找到唯一初始化式。
	// （同名声明可能出现在多个兄弟块，用 declAt 按路径精确匹配。）

	// 收集每个名字的全部使用。
	usesByName := make(map[string][]receiverUse)
	for _, u := range w.uses {
		usesByName[u.name] = append(usesByName[u.name], u)
	}

	// 链调用点：只有「声明初始化式的根调用」且该声明通过校验才算安全；
	// 其余位置一律视为逃逸，摘掉被调链构造器。
	for _, site := range w.calls {
		callee := site.callee
		if site.nested || site.declName == "" || !site.rootInDec {
			if badOut != nil {
				badOut[callee] = true
			}
			continue
		}
		decl := w.declAt(site.declName, site.declPath)
		if decl == nil {
			if badOut != nil {
				badOut[callee] = true
			}
			continue
		}
		if decl.chainCallee != callee {
			if badOut != nil {
				badOut[callee] = true
			}
			continue
		}
		// 接收者是否安全：所有使用都是只读/借用，且词法上都落在声明块内。
		// 同名变量可能在不同兄弟块中重复声明（stress_ptr_tree 的两个 a），
		// 必须按「最内层可见声明」把使用归属到正确的实例，不能按名字合并。
		unsafe := false
		for _, u := range usesByName[site.declName] {
			owner := w.innermostDecl(site.declName, u.path)
			if owner != decl {
				continue // 属于另一个同名声明（或形参/外部符号）
			}
			if u.nested || u.escape || !pathHasPrefix(decl.path, u.path) {
				unsafe = true
				break
			}
		}
		if unsafe {
			if badOut != nil {
				badOut[callee] = true
			}
			continue
		}
		if safeOut != nil {
			safeOut[site.declName] = true
		}
	}

	// 声明了链指针但从未作为链调用初始化式出现（例如嵌套调用/别名）
	// 不影响这里：那些用法已由 calls 的 rootInDec 校验覆盖。
}

func pathHasPrefix(prefix, path []int) bool {
	if len(prefix) > len(path) {
		return false
	}
	for i := range prefix {
		if prefix[i] != path[i] {
			return false
		}
	}
	return true
}

func samePath(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// declAt 返回在 path 这一词法块中声明的 name（同名兄弟块互不干扰）。
func (w *receiverWalker) declAt(name string, path []int) *receiverDecl {
	var found *receiverDecl
	for i := range w.decls[name] {
		d := &w.decls[name][i]
		if !samePath(d.path, path) {
			continue
		}
		if found != nil {
			return nil // 同块重复声明：边界不清，拒绝
		}
		found = d
	}
	return found
}

// innermostDecl 返回使用点 path 处 name 的最内层可见局部声明。
// 若 name 是形参/外部符号（没有任何局部声明），返回 nil。
func (w *receiverWalker) innermostDecl(name string, path []int) *receiverDecl {
	var best *receiverDecl
	for i := range w.decls[name] {
		d := &w.decls[name][i]
		if !pathHasPrefix(d.path, path) {
			continue
		}
		if best == nil || len(d.path) > len(best.path) {
			best = d
		}
	}
	return best
}

func (w *receiverWalker) nextPath(parent []int) []int {
	w.pathID++
	out := make([]int, len(parent)+1)
	copy(out, parent)
	out[len(parent)] = w.pathID
	return out
}

func (w *receiverWalker) addUse(name string, path []int, ctx receiverCtx) {
	if name == "" {
		return
	}
	w.uses = append(w.uses, receiverUse{
		name:   name,
		path:   append([]int(nil), path...),
		escape: ctx.escape,
		nested: ctx.nested,
	})
}

func (w *receiverWalker) walkStmts(stmts []ast.Statement, path []int, nested bool, chainFuncs map[string]*ChainFunctionInfo, borrowOnly map[string]bool) bool {
	for _, stmt := range stmts {
		if !w.walkStmt(stmt, path, nested, chainFuncs, borrowOnly) {
			return false
		}
	}
	return true
}

func (w *receiverWalker) walkStmt(stmt ast.Statement, path []int, nested bool, chainFuncs map[string]*ChainFunctionInfo, borrowOnly map[string]bool) bool {
	if stmt == nil {
		return true
	}
	base := receiverCtx{nested: nested}
	switch st := stmt.(type) {
	case *ast.VariableDeclaration:
		for _, d := range st.AllDeclarators() {
			if d == nil || d.Name == "" {
				continue
			}
			init := classifyChainInit(d.Value)
			isChainCall := init.kind == chainInitCall && chainFuncs[init.callee] != nil
			pointer := isPointerDeclType(d.Type) || isChainCall || init.kind == chainInitAlloc
			decl := receiverDecl{
				name:    d.Name,
				path:    append([]int(nil), path...),
				pointer: pointer,
			}
			if isChainCall {
				decl.chainCallee = init.callee
				w.calls = append(w.calls, chainCallSite{
					callee:    init.callee,
					declName:  d.Name,
					declPath:  append([]int(nil), path...),
					path:      append([]int(nil), path...),
					nested:    nested,
					call:      init.call,
					rootInDec: true,
				})
			}
			w.decls[d.Name] = append(w.decls[d.Name], decl)

			ctx := base
			ctx.declName = d.Name
			ctx.declPath = path
			ctx.pointerDecl = pointer
			if isChainCall {
				ctx.rootCall = init.call
				ctx.escape = false
			} else if pointer {
				// 指针声明用非链调用/非 null 初始化 = 别名或外部来源，
				// 若引用接收者就必须按逃逸处理。
				ctx.escape = true
			}
			if d.Value != nil {
				w.walkExpr(d.Value, path, ctx, chainFuncs, borrowOnly)
			}
		}
		return true

	case *ast.ExpressionStatement:
		if bin, ok := st.Expression.(*ast.BinaryExpression); ok &&
			(bin.Operator == "=" || bin.Operator == "ASSIGN") {
			lhsCtx := base
			lhsCtx.escape = true
			w.walkExpr(bin.Left, path, lhsCtx, chainFuncs, borrowOnly)
			rhsCtx := base
			rhsCtx.escape = true
			w.walkExpr(bin.Right, path, rhsCtx, chainFuncs, borrowOnly)
			return true
		}
		w.walkExpr(st.Expression, path, base, chainFuncs, borrowOnly)
		return true

	case *ast.IfStatement:
		w.walkExpr(st.Condition, path, base, chainFuncs, borrowOnly)
		w.walkStmts(st.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		w.walkStmts(st.Else, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.WhileStatement:
		w.walkExpr(st.Condition, path, base, chainFuncs, borrowOnly)
		w.walkStmts(st.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.ForStatement:
		if st.Init != nil {
			w.walkStmt(st.Init, path, nested, chainFuncs, borrowOnly)
		}
		w.walkExpr(st.Condition, path, base, chainFuncs, borrowOnly)
		if st.Update != nil {
			w.walkStmt(st.Update, path, nested, chainFuncs, borrowOnly)
		}
		w.walkStmts(st.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.ForInStatement:
		w.walkExpr(st.Iterable, path, base, chainFuncs, borrowOnly)
		w.walkStmts(st.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.ReturnStatement:
		ctx := base
		ctx.escape = true
		w.walkExpr(st.Value, path, ctx, chainFuncs, borrowOnly)
		return true

	case *ast.YieldStatement:
		ctx := base
		ctx.escape = true
		w.walkExpr(st.Source, path, ctx, chainFuncs, borrowOnly)
		return true

	case *ast.ReleaseStatement:
		ctx := base
		ctx.escape = true
		w.walkExpr(st.Source, path, ctx, chainFuncs, borrowOnly)
		return true

	case *ast.ExtractStatement:
		ctx := base
		ctx.escape = true
		w.walkExpr(st.Source, path, ctx, chainFuncs, borrowOnly)
		w.walkExpr(st.Index, path, base, chainFuncs, borrowOnly)
		return true

	case *ast.BlockStatement:
		w.walkStmts(st.Statements, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.SwitchStatement:
		w.walkExpr(st.Scrutinee, path, base, chainFuncs, borrowOnly)
		for _, c := range st.Cases {
			for _, v := range c.Values {
				w.walkExpr(v, path, base, chainFuncs, borrowOnly)
			}
			w.walkStmts(c.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		}
		w.walkStmts(st.Default, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.FunctionStatement:
		// 嵌套函数：函数体内的链调用无法作为外层接收者声明处理；链指针
		// 被闭包捕获同样视为逃逸。保守按 nested 处理。
		w.walkStmts(st.Body, w.nextPath(path), true, chainFuncs, borrowOnly)
		return true

	case *ast.CallStatement:
		w.walkExpr(st.Target, path, base, chainFuncs, borrowOnly)
		w.walkStmts(st.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.SpendStatement:
		if st.Target != nil {
			w.walkExpr(st.Target, path, base, chainFuncs, borrowOnly)
		}
		for _, clause := range st.Calls {
			if clause == nil {
				continue
			}
			if clause.Index != nil {
				w.walkExpr(clause.Index, path, base, chainFuncs, borrowOnly)
			}
			w.walkStmts(clause.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		}
		return true

	case *ast.PrefixStatement:
		w.walkStmts(st.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	case *ast.TreeStatement:
		w.walkExpr(st.Root, path, base, chainFuncs, borrowOnly)
		w.walkStmts(st.Body, w.nextPath(path), nested, chainFuncs, borrowOnly)
		return true

	default:
		// 未知语句：保守地把其中出现的所有名字标为逃逸使用。
		used := make(map[string]bool)
		collectUsedNamesStmt(stmt, used)
		ctx := base
		ctx.escape = true
		for name := range used {
			w.addUse(name, path, ctx)
		}
		return true
	}
}

func (w *receiverWalker) walkExpr(expr ast.Expression, path []int, ctx receiverCtx, chainFuncs map[string]*ChainFunctionInfo, borrowOnly map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		w.addUse(e.Name, path, ctx)

	case *ast.ParenExpression:
		w.walkExpr(e.Inner, path, ctx, chainFuncs, borrowOnly)

	case *ast.TypeCastExpression:
		inner := ctx
		// 强转把值换成另一个类型：对指针接收者而言要么是别名，要么
		// 是把地址转成整数，均按逃逸处理。
		inner.escape = true
		inner.declName = ""
		inner.rootCall = nil
		w.walkExpr(e.Expression, path, inner, chainFuncs, borrowOnly)

	case *ast.UnaryExpression:
		inner := ctx
		if e.Operator == "&" {
			inner.escape = true
		}
		w.walkExpr(e.Right, path, inner, chainFuncs, borrowOnly)

	case *ast.BinaryExpression:
		if e.Operator == "=" || e.Operator == "ASSIGN" {
			inner := ctx
			inner.escape = true
			inner.declName = ""
			inner.rootCall = nil
			w.walkExpr(e.Left, path, inner, chainFuncs, borrowOnly)
			w.walkExpr(e.Right, path, inner, chainFuncs, borrowOnly)
			return
		}
		if isComparisonOrLogicalOp(e.Operator) {
			// 比较/逻辑运算只消费值，不会把指针带走。
			inner := ctx
			inner.escape = false
			inner.declName = ""
			inner.rootCall = nil
			w.walkExpr(e.Left, path, inner, chainFuncs, borrowOnly)
			w.walkExpr(e.Right, path, inner, chainFuncs, borrowOnly)
			return
		}
		w.walkExpr(e.Left, path, ctx, chainFuncs, borrowOnly)
		w.walkExpr(e.Right, path, ctx, chainFuncs, borrowOnly)

	case *ast.ConditionalExpression:
		read := ctx
		read.escape = false
		read.declName = ""
		read.rootCall = nil
		w.walkExpr(e.Condition, path, read, chainFuncs, borrowOnly)
		w.walkExpr(e.TrueExpr, path, ctx, chainFuncs, borrowOnly)
		w.walkExpr(e.FalseExpr, path, ctx, chainFuncs, borrowOnly)

	case *ast.CallExpression:
		callee := calledFuncName(e.Function)
		if chainFuncs[callee] != nil {
			isRoot := ctx.rootCall != nil && e == ctx.rootCall
			if !isRoot {
				w.calls = append(w.calls, chainCallSite{
					callee:    callee,
					path:      append([]int(nil), path...),
					nested:    ctx.nested,
					call:      e,
					rootInDec: false,
				})
			}
		}
		// 被调方表达式本身若是接收者（函数指针）→ 逃逸。
		calleeCtx := ctx
		calleeCtx.escape = true
		calleeCtx.declName = ""
		calleeCtx.rootCall = nil
		w.walkExpr(e.Function, path, calleeCtx, chainFuncs, borrowOnly)

		argEscape := !borrowOnly[callee]
		for _, arg := range e.Args {
			argCtx := ctx
			argCtx.escape = argEscape
			argCtx.declName = ""
			argCtx.rootCall = nil
			w.walkExpr(arg, path, argCtx, chainFuncs, borrowOnly)
		}

	case *ast.SpawnExpression:
		inner := ctx
		inner.escape = true
		inner.declName = ""
		inner.rootCall = nil
		w.walkExpr(e.Function, path, inner, chainFuncs, borrowOnly)
		for _, arg := range e.Args {
			w.walkExpr(arg, path, inner, chainFuncs, borrowOnly)
		}

	case *ast.AwaitExpression:
		w.walkExpr(e.Handle, path, ctx, chainFuncs, borrowOnly)

	case *ast.MemberAccessExpression:
		w.walkExpr(e.Object, path, ctx, chainFuncs, borrowOnly)

	case *ast.MemberExpression:
		w.walkExpr(e.Object, path, ctx, chainFuncs, borrowOnly)

	case *ast.IndexExpression:
		w.walkExpr(e.Object, path, ctx, chainFuncs, borrowOnly)
		read := ctx
		read.escape = false
		w.walkExpr(e.Index, path, read, chainFuncs, borrowOnly)

	case *ast.StructLiteral:
		inner := ctx
		inner.escape = true
		inner.declName = ""
		inner.rootCall = nil
		for _, f := range e.Fields {
			w.walkExpr(f.Value, path, inner, chainFuncs, borrowOnly)
		}

	case *ast.ArrayLiteral:
		inner := ctx
		inner.escape = true
		inner.declName = ""
		inner.rootCall = nil
		for _, el := range e.Elements {
			w.walkExpr(el, path, inner, chainFuncs, borrowOnly)
		}

	case *ast.LambdaExpression:
		inner := ctx
		inner.nested = true
		inner.escape = true
		inner.declName = ""
		inner.rootCall = nil
		w.walkStmts(e.Body, w.nextPath(path), true, chainFuncs, borrowOnly)

	case *ast.MatchExpression:
		w.walkExpr(e.Target, path, ctx, chainFuncs, borrowOnly)
		for _, arm := range e.Arms {
			if arm == nil {
				continue
			}
			w.walkStmts(arm.Body, w.nextPath(path), ctx.nested, chainFuncs, borrowOnly)
		}

	case *ast.ComptimeExpression:
		w.walkExpr(e.Inner, path, ctx, chainFuncs, borrowOnly)

	case *ast.PrefixCallExpression:
		for _, v := range e.Params {
			w.walkExpr(v, path, ctx, chainFuncs, borrowOnly)
		}
		w.walkStmts(e.Body, w.nextPath(path), ctx.nested, chainFuncs, borrowOnly)

	default:
		// 字面量/sizeof 等：无子表达式。
	}
}

func isComparisonOrLogicalOp(op string) bool {
	switch op {
	case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
		return true
	}
	return false
}
