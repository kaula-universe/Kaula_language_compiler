package codegen

// kmm_precision.go —— 分配点驱动的 KMM 插入精度优化（Phase 3）。
//
// 37debaa（定长值数组免 KMM）把「类型启发式」推进了一步；本文件继续同一方向，
// 补齐剩余三类误判与两类冗余发射的判定基础：
//
//  1. POD struct 判定表：纯值字段（含传递闭包）的 struct 不再被
//     needsKMMForType 的"未知类型保守 true"误判（基线缺陷：Stats/Point 这类
//     纯 POD 局部变量把整个函数套进 KMM 作用域）。
//  2. exprMayAllocate：分配点检测从「kmm_v4 命名的调用」扩展到「stdlib 中
//     返回堆类型（String/指针/切片/map）的函数」。此前 std.string.create 这类
//     真实堆分配在 AST 层不可见，全靠"变量类型是 string"兜底；变量重赋值
//     （形参 s = string_create(...)）会漏判。
//  3. SSO-only String 跟踪：全程只被字符串字面量赋值的局部 String 变量，
//     载荷内联在结构体里，不进堆——不需要 survivor 管理，返回时也不需要
//     string_clone_global（结构体按值拷贝即 move）。
//
// 安全下界（不变式）：任何无法证明的点一律回退保守路径——未知 struct、泛型、
// adopt、含指针/string 字段、非字面量 String 赋值、KL 函数调用（无跨函数
// 所有权分析）都维持原判定。所有优化只做"证明不需要"，不做"猜测不需要"。

import (
	"fmt"
	"strings"

	"kaula/internal/ast"
)

// ---------------------------------------------------------------------------
// POD struct 判定表（#4 的地基）
// ---------------------------------------------------------------------------

// podStructReady 标记 POD 表是否已构建（惰性，首次查询时构建一次）。
func (cg *CodeGenerator) buildPODStructTableIfNeeded() {
	if cg.podStructBuilt {
		return
	}
	cg.podStructBuilt = true
	cg.podStructTable = map[string]bool{}

	prog := cg.getProgram()
	if prog == nil {
		return
	}
	for _, stmt := range prog.Statements {
		if s, ok := stmt.(*ast.StructStatement); ok && s != nil && !s.Generic {
			cg.isPODStructDef(s.Name, map[string]bool{})
		}
	}
}

// isPODStructDef 判定具名 struct 是否传递纯值。visiting 防循环引用死递归。
// 结果缓存进 podStructTable（含失败结果，避免重复下探）。
func (cg *CodeGenerator) isPODStructDef(name string, visiting map[string]bool) bool {
	if v, ok := cg.podStructTable[name]; ok {
		return v
	}
	if visiting[name] {
		return false // 循环引用：C 中无指针不可能成立，保守非 POD
	}
	visiting[name] = true
	defer delete(visiting, name)

	prog := cg.getProgram()
	if prog == nil {
		return false
	}
	structStmt := prog.FindStruct(name)
	if structStmt == nil || structStmt.Generic {
		cg.podStructTable[name] = false
		return false
	}
	pod := true
	for _, f := range structStmt.Fields {
		if f == nil {
			pod = false
			break
		}
		if !cg.isPODFieldType(f.Type, visiting) {
			pod = false
			break
		}
	}
	cg.podStructTable[name] = pod
	return pod
}

// isPODFieldType 判定 struct 字段类型是否纯值。
// 纯值 = 标量 / 无载荷枚举 / 定长数组(元素纯值) / 已证明 POD 的 struct。
func (cg *CodeGenerator) isPODFieldType(fieldType string, visiting map[string]bool) bool {
	t := strings.TrimSpace(fieldType)
	if t == "" {
		return false
	}
	// 指针 / 切片 / map / string / 透明指针 / 可空：一律非 POD
	if strings.ContainsAny(t, "*?") ||
		strings.HasPrefix(t, "[]") ||
		strings.HasPrefix(t, "map[") ||
		strings.HasPrefix(t, "void(") {
		return false
	}
	// 定长数组：剥维度后按元素判定（与 37debaa 同一语义）
	if strings.HasPrefix(t, "[") {
		if elem, ok := fixedArrayElementType(t); ok {
			return cg.isPODFieldType(elem, visiting)
		}
		return false
	}
	base := strings.ToLower(t)
	if isScalarKaulaType(base) {
		return true
	}
	// 无载荷枚举：C enum，纯整数标签
	if enumStmt := cg.program.FindEnum(t); enumStmt != nil && !enumStmt.Generic {
		for _, v := range enumStmt.Variants {
			if v != nil && len(v.FieldTypes) > 0 {
				return false // 带载荷变体 = tagged union，含堆指针可能
			}
		}
		return true
	}
	// 嵌套 POD struct（传递闭包）
	return cg.isPODStructDef(t, visiting)
}

// IsPODStruct 查询类型名是否为已证明的纯值 struct。
// 未登记 / 泛型实例 / 未知类型 → false（保守回退）。
func (cg *CodeGenerator) IsPODStruct(typeName string) bool {
	cg.buildPODStructTableIfNeeded()
	return cg.podStructTable[typeName]
}

// isScalarKaulaType needsKMMForType 的值类型白名单（不查表，纯词法）。
func isScalarKaulaType(t string) bool {
	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"float", "float32", "float64", "f32", "f64",
		"double", "bool", "char", "byte",
		"isize", "usize", "size", "void":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// 类型级判定（needsKMMForType 的程序感知升级版）
// ---------------------------------------------------------------------------

// typeMayNeedKMM 在 needsKMMForType 基础上利用程序信息收紧判定：
//   - POD struct（含传递闭包）→ false
//   - 无载荷枚举 → false（needsKMMForType 对未知类型名保守 true）
//   - 定长数组沿用 37debaa 的元素递归判定
//   - 其余全部回退 needsKMMForType 的保守结果
func (cg *CodeGenerator) typeMayNeedKMM(typeName string) bool {
	if typeName == "" {
		return false
	}
	t := strings.TrimSpace(typeName)
	// 定长数组：剥维度递归（37debaa 语义）
	if strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[]") {
		if elem, ok := fixedArrayElementType(t); ok && elem != "" {
			return cg.typeMayNeedKMM(elem)
		}
		return needsKMMForType(t)
	}
	// POD struct 查表
	if cg.IsPODStruct(t) {
		return false
	}
	// 无载荷枚举查表（needsKMMForType 对枚举名是未知类型 → 保守 true）
	if enumStmt := cg.program.FindEnum(t); enumStmt != nil && !enumStmt.Generic {
		for _, v := range enumStmt.Variants {
			if v != nil && len(v.FieldTypes) > 0 {
				return true
			}
		}
		return false
	}
	return needsKMMForType(t)
}

// ---------------------------------------------------------------------------
// 分配点检测（#2 的地基）：从「调用名启发」升级到「返回堆类型即分配」
// ---------------------------------------------------------------------------

// klPureBuiltins 纯编译期内建/运行期不分配的调用名白名单。
// 这些调用既不在 stdlib.json、也不在 KL 函数表里，不加白名单会被
// "未知调用保守 true" 误伤（range/assert/sizeof 出现在几乎所有循环里）。
var klPureBuiltins = map[string]bool{
	"range": true, "assert": true,
	"sizeof": true, "alignof": true, "offsetof": true,
	"type_name": true, "type_kind": true,
	"field_count": true, "field_name": true, "field_type": true,
	"shape_of": true, "panic": true,
}

// buildKLAllocTableIfNeeded 构建 KL 函数「是否可能分配」的跨函数闭包表。
//
// 两阶段不动点：
//   A. 每个函数计算直接分配点（KL 调用忽略）→ 初值
//   B. 不动点迭代：调用方继承被调方的 mayAlloc（被调方自身的分配在它
//      自己的作用域内回收；其返回值若逃逸会走 promote-on-return 进
//      全局段，调用方不需要为其包壳）——因此"调用不分配函数"的调用方
//      仍可不包壳。
//
// 未知函数（extern / 非本程序定义）→ 保守 true。
func (cg *CodeGenerator) buildKLAllocTableIfNeeded() {
	if cg.klAllocBuilt {
		return
	}
	cg.klAllocBuilt = true
	cg.klAllocTable = map[string]bool{}
	prog := cg.getProgram()
	if prog == nil {
		return
	}

	// 收集全部函数（含嵌套定义）与调用边
	direct := map[string]bool{}
	callees := map[string][]string{}
	known := map[string]bool{}

	var walkStmts func(stmts []ast.Statement, owner string)
	walkExpr := func(owner string, expr ast.Expression) {
		// 表达式级：收集 KL 调用名 + 直接分配点
		var rec func(e ast.Expression)
		rec = func(e ast.Expression) {
			if e == nil {
				return
			}
			switch x := e.(type) {
			case *ast.CallExpression:
				if n := calledFuncName(x.Function); n != "" && !cg.isStdlibOrBuiltinName(n) {
					callees[owner] = append(callees[owner], n)
				} else if isAllocCall(x.Function, cg.IsFreestanding()) ||
					cg.isHeapReturnStdlibCall(x.Function) {
					direct[owner] = true
				}
				for _, a := range x.Args {
					rec(a)
				}
			case *ast.BinaryExpression:
				rec(x.Left)
				rec(x.Right)
			case *ast.UnaryExpression:
				rec(x.Right)
			case *ast.MemberAccessExpression:
				rec(x.Object)
			case *ast.ParenExpression:
				rec(x.Inner)
			case *ast.TypeCastExpression:
				rec(x.Expression)
			case *ast.ConditionalExpression:
				rec(x.Condition)
				rec(x.TrueExpr)
				rec(x.FalseExpr)
			}
		}
		rec(expr)
	}
	walkStmts = func(stmts []ast.Statement, owner string) {
		for _, stmt := range stmts {
			if stmt == nil {
				continue
			}
			switch s := stmt.(type) {
			case *ast.ExpressionStatement:
				if s != nil {
					walkExpr(owner, s.Expression)
				}
			case *ast.VariableDeclaration:
				if s != nil {
					walkExpr(owner, s.Value)
				}
			case *ast.ReturnStatement:
				if s != nil {
					walkExpr(owner, s.Value)
				}
			case *ast.IfStatement:
				if s != nil {
					walkExpr(owner, s.Condition)
					walkStmts(s.Body, owner)
					walkStmts(s.Else, owner)
				}
			case *ast.WhileStatement:
				if s != nil {
					walkExpr(owner, s.Condition)
					walkStmts(s.Body, owner)
				}
			case *ast.ForStatement:
				if s != nil {
					walkStmts(s.Body, owner)
				}
			case *ast.ForInStatement:
				if s != nil {
					walkExpr(owner, s.Iterable)
					walkStmts(s.Body, owner)
				}
			case *ast.BlockStatement:
				if s != nil {
					walkStmts(s.Statements, owner)
				}
			case *ast.FunctionStatement:
				if s != nil && s.Name != owner {
					// 嵌套函数：独立 owner 重新入表
					known[s.Name] = true
					if _, seen := direct[s.Name]; !seen {
						direct[s.Name] = false
					}
					walkStmts(s.Body, s.Name)
				}
			}
		}
	}

	for _, stmt := range prog.Statements {
		if fn, ok := stmt.(*ast.FunctionStatement); ok && fn != nil {
			known[fn.Name] = true
			direct[fn.Name] = false
			walkStmts(fn.Body, fn.Name)
		}
	}

	// 不动点：调用方继承被调方
	may := make(map[string]bool, len(direct))
	for k, v := range direct {
		may[k] = v
	}
	for changed := true; changed; {
		changed = false
		for owner, cs := range callees {
			if may[owner] {
				continue
			}
			for _, c := range cs {
				if may[c] {
					may[owner] = true
					changed = true
					break
				}
			}
		}
	}
	cg.klAllocTable = may
	cg.klKnownFuncs = known
}

// isStdlibOrBuiltinName 名字是否属于 stdlib 或纯内建（不算 KL 调用边）。
func (cg *CodeGenerator) isStdlibOrBuiltinName(name string) bool {
	if klPureBuiltins[name] {
		return true
	}
	if cfg := cg.GetStdlibConfig(); cfg != nil {
		return cfg.IsStdlibFunction(name)
	}
	return false
}

// klFuncMayAllocate 查询 KL 函数是否可能分配。
// 未登记（extern / 外部符号）→ 保守 true。
func (cg *CodeGenerator) klFuncMayAllocate(name string) bool {
	cg.buildKLAllocTableIfNeeded()
	if _, ok := cg.klKnownFuncs[name]; ok {
		return cg.klAllocTable[name]
	}
	return true
}

// isHeapReturnStdlibCall 判断 stdlib 函数调用是否返回堆托管类型。
// std.json 的 return 字段是 C 侧类型（String / T* / void() 等），
// 其中 String 按值返回但载荷可能走 kmm_v4_malloc —— 必须视为分配点。
func (cg *CodeGenerator) isHeapReturnStdlibCall(fn ast.Expression) bool {
	cfg := cg.GetStdlibConfig()
	if cfg == nil {
		return false
	}
	var mod, name string
	switch f := fn.(type) {
	case *ast.MemberAccessExpression:
		// std.string.create → mod=std.string, name=create
		if id, ok := f.Object.(*ast.Identifier); ok && id != nil {
			mod, name = id.Name, f.Member
		}
	case *ast.Identifier:
		// 简名：省略 std. 前缀的导入形式
		if fnAny := cfg.GetFunctionByName(f.Name); fnAny != nil {
			return heapReturnCType(fnAny.Return)
		}
		return false
	default:
		return false
	}
	if mod == "" || name == "" {
		return false
	}
	if fc := cfg.GetFunction(mod, name); fc != nil {
		return heapReturnCType(fc.Return)
	}
	return false
}

// heapReturnCType C 返回类型是否堆托管。
func heapReturnCType(ret string) bool {
	r := strings.TrimSpace(ret)
	if r == "" || r == "void" {
		return false
	}
	lower := strings.ToLower(r)
	if isScalarKaulaType(strings.TrimSuffix(lower, "*")) && !strings.Contains(r, "*") {
		// 标量按值返回（含 bool/char）
		return false
	}
	// String / 指针 / void() / 其他结构体按值返回：保守视为可能堆分配
	return true
}

// isScalarReturnStdlibSimple 简名/链式 stdlib 调用且返回标量 → 确定不分配。
func (cg *CodeGenerator) isScalarReturnStdlibSimple(fn ast.Expression) bool {
	cfg := cg.GetStdlibConfig()
	if cfg == nil {
		return false
	}
	switch f := fn.(type) {
	case *ast.Identifier:
		if fc := cfg.GetFunctionByName(f.Name); fc != nil {
			return !heapReturnCType(fc.Return)
		}
	case *ast.MemberAccessExpression:
		if id, ok := f.Object.(*ast.Identifier); ok && id != nil {
			if fc := cfg.GetFunction(id.Name, f.Member); fc != nil {
				return !heapReturnCType(fc.Return)
			}
		}
	}
	return false
}

// exprMayAllocate AST 表达式级分配点检测。
// 覆盖：KMM 命名调用（原有）+ stdlib 堆返回调用（新增）+ KL 跨函数闭包
// （新增，未知/extern 保守 true）。
func (cg *CodeGenerator) exprMayAllocate(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.CallExpression:
		if isAllocCall(e.Function, cg.IsFreestanding()) {
			return true
		}
		if id, ok := e.Function.(*ast.Identifier); ok && id != nil {
			if cg.isStdlibOrBuiltinName(id.Name) {
				// stdlib 标量返回 / 纯内建：自身不分配，但实参可能分配
				for _, arg := range e.Args {
					if cg.exprMayAllocate(arg) {
						return true
					}
				}
				return false
			}
			// KL（查闭包表）/ extern / 未知：保守
			return cg.klFuncMayAllocate(id.Name)
		}
		if _, isMember := e.Function.(*ast.MemberAccessExpression); isMember {
			if cg.isHeapReturnStdlibCall(e.Function) {
				return true
			}
			if cg.isScalarReturnStdlibSimple(e.Function) {
				for _, arg := range e.Args {
					if cg.exprMayAllocate(arg) {
						return true
					}
				}
				return false
			}
			// 链式调用无法解析返回类型：保守 true
			return true
		}
		return true
	case *ast.BinaryExpression:
		return cg.exprMayAllocate(e.Left) || cg.exprMayAllocate(e.Right)
	case *ast.UnaryExpression:
		return cg.exprMayAllocate(e.Right)
	case *ast.MemberAccessExpression:
		return cg.exprMayAllocate(e.Object)
	case *ast.ParenExpression:
		return cg.exprMayAllocate(e.Inner)
	case *ast.TypeCastExpression:
		return cg.exprMayAllocate(e.Expression)
	case *ast.ConditionalExpression:
		return cg.exprMayAllocate(e.Condition) ||
			cg.exprMayAllocate(e.TrueExpr) || cg.exprMayAllocate(e.FalseExpr)
	case *ast.MatchExpression:
		// match 各分支体语句保守扫描
		for _, arm := range e.Arms {
			for _, s := range arm.Body {
				if cg.stmtMayAllocate(s) {
					return true
				}
			}
		}
		return false
	}
	return false
}

// stmtMayAllocate 语句级分配点检测（递归进控制流块）。
func (cg *CodeGenerator) stmtMayAllocate(stmt ast.Statement) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		return cg.exprMayAllocate(s.Expression)
	case *ast.VariableDeclaration:
		return cg.exprMayAllocate(s.Value)
	case *ast.ReturnStatement:
		return cg.exprMayAllocate(s.Value)
	case *ast.IfStatement:
		if cg.exprMayAllocate(s.Condition) {
			return true
		}
		return cg.stmtsMayAllocate(s.Body) || cg.stmtsMayAllocate(s.Else)
	case *ast.WhileStatement:
		return cg.exprMayAllocate(s.Condition) || cg.stmtsMayAllocate(s.Body)
	case *ast.ForStatement:
		return cg.stmtsMayAllocate(s.Body)
	case *ast.ForInStatement:
		return cg.exprMayAllocate(s.Iterable) || cg.stmtsMayAllocate(s.Body)
	case *ast.BlockStatement:
		return cg.stmtsMayAllocate(s.Statements)
	}
	return false
}

// stmtsMayAllocate 语句列表分配点检测。
func (cg *CodeGenerator) stmtsMayAllocate(stmts []ast.Statement) bool {
	for _, stmt := range stmts {
		if cg.stmtMayAllocate(stmt) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// SSO-only String 跟踪（#2/#5 的地基）
// ---------------------------------------------------------------------------

// ScanStringVarOrigins 扫描函数体，登记「全程只被字符串字面量赋值」的
// 局部 String 变量（ssoStringScanned）。任何非字面量来源（函数调用、
// 表达式、嵌套函数内赋值、for-in 元素）都会把该名字拉黑。
//
// SSO 变量间的赋值（s = t）不直接拉黑：污点沿赋值边做不动点传播，
// 只有源头被拉黑才传染（t 干净则 s 保持 SSO-only）。
//
// 安全不变式：字面量赋值生成 SSO 结构体字面量（载荷内联，零堆），
// 结构体按值拷贝即可安全移交所有权；非字面量来源一律拉黑。
// map 按变量名全局记录（与 assignEscapedVars 同一先例）：跨函数同名
// 冲突只会导致过度拉黑（保守），不会误标。
func (cg *CodeGenerator) ScanStringVarOrigins(body []ast.Statement) {
	cg.ssoStringScanned = map[string]bool{}
	cg.ssoStringTainted = map[string]bool{}
	cg.ssoStringEdges = map[string][]string{}
	cg.scanStringVarOriginsStmts(body, map[string]bool{})

	// 污点沿赋值边传播至不动点：s ← t 且 t 被拉黑 ⇒ s 被拉黑
	for changed := true; changed; {
		changed = false
		for name, sources := range cg.ssoStringEdges {
			if cg.ssoStringTainted[name] {
				continue
			}
			for _, src := range sources {
				if cg.ssoStringTainted[src] {
					cg.ssoStringTainted[name] = true
					changed = true
					break
				}
			}
		}
	}
}

func (cg *CodeGenerator) scanStringVarOriginsStmts(stmts []ast.Statement, paramNames map[string]bool) {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.VariableDeclaration:
			if s == nil {
				continue
			}
			base := strings.ToLower(strings.TrimSuffix(s.Type, "?"))
			isStringType := base == "string" || base == "str"
			if isStringType && !s.Nullable && !paramNames[s.Name] && s.Value != nil {
				if lit, ok := s.Value.(*ast.StringLiteral); ok && lit != nil {
					cg.ssoStringScanned[s.Name] = true
				} else if id, ok := s.Value.(*ast.Identifier); ok && id != nil {
					// SSO 变量间初始化：记边，污点传播阶段判定
					cg.ssoStringScanned[s.Name] = true
					cg.ssoStringEdges[s.Name] = append(cg.ssoStringEdges[s.Name], id.Name)
				} else {
					cg.ssoStringTainted[s.Name] = true
				}
			} else if isStringType {
				// 可空 string / 形参 / 无初值：保守拉黑
				cg.ssoStringTainted[s.Name] = true
			}
			// 嵌套函数体内的声明同样扫描
			if s.Value != nil {
				cg.scanStringVarOriginsExpr(s.Value)
			}
		case *ast.ExpressionStatement:
			if s != nil {
				cg.scanStringVarOriginsExpr(s.Expression)
			}
		case *ast.ReturnStatement:
			if s != nil {
				cg.scanStringVarOriginsExpr(s.Value)
			}
		case *ast.IfStatement:
			if s != nil {
				cg.scanStringVarOriginsExpr(s.Condition)
				cg.scanStringVarOriginsStmts(s.Body, paramNames)
				cg.scanStringVarOriginsStmts(s.Else, paramNames)
			}
		case *ast.WhileStatement:
			if s != nil {
				cg.scanStringVarOriginsExpr(s.Condition)
				cg.scanStringVarOriginsStmts(s.Body, paramNames)
			}
		case *ast.ForStatement:
			if s != nil {
				cg.scanStringVarOriginsStmts(s.Body, paramNames)
			}
		case *ast.ForInStatement:
			if s != nil {
				// for-in 元素变量若是 string 类型：载荷来自容器，保守拉黑
				if s.Variable != nil && s.Variable.Name != "" {
					cg.ssoStringTainted[s.Variable.Name] = true
				}
				cg.scanStringVarOriginsExpr(s.Iterable)
				cg.scanStringVarOriginsStmts(s.Body, paramNames)
			}
		case *ast.BlockStatement:
			if s != nil {
				cg.scanStringVarOriginsStmts(s.Statements, paramNames)
			}
		case *ast.FunctionStatement:
			if s != nil {
				// 嵌套函数：形参名拉黑 + 递归扫描其体
				pn := map[string]bool{}
				for _, p := range s.Params {
					pn[p] = true
				}
				cg.scanStringVarOriginsStmts(s.Body, pn)
			}
		}
	}
}

// scanStringVarOriginsExpr 表达式级扫描：识别 `x = expr` 赋值对 String
// 变量的来源登记/拉黑。非赋值表达式只下探。
func (cg *CodeGenerator) scanStringVarOriginsExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.BinaryExpression:
		if e.Operator == "=" {
			if id, ok := e.Left.(*ast.Identifier); ok && id != nil {
				if lit, isLit := e.Right.(*ast.StringLiteral); isLit && lit != nil {
					// 字面量赋值：登记为 SSO-only 候选（已登记则保持）
					cg.ssoStringScanned[id.Name] = true
				} else if rid, isIdent := e.Right.(*ast.Identifier); isIdent && rid != nil {
					// SSO 变量间赋值：记边，污点传播阶段判定
					cg.ssoStringEdges[id.Name] = append(cg.ssoStringEdges[id.Name], rid.Name)
				} else {
					// 非字面量非变量赋值：拉黑（无论该名当前是否登记）
					cg.ssoStringTainted[id.Name] = true
				}
			}
			cg.scanStringVarOriginsExpr(e.Right)
			return
		}
		cg.scanStringVarOriginsExpr(e.Left)
		cg.scanStringVarOriginsExpr(e.Right)
	case *ast.UnaryExpression:
		cg.scanStringVarOriginsExpr(e.Right)
	case *ast.ParenExpression:
		cg.scanStringVarOriginsExpr(e.Inner)
	case *ast.TypeCastExpression:
		cg.scanStringVarOriginsExpr(e.Expression)
	case *ast.ConditionalExpression:
		cg.scanStringVarOriginsExpr(e.Condition)
		cg.scanStringVarOriginsExpr(e.TrueExpr)
		cg.scanStringVarOriginsExpr(e.FalseExpr)
	case *ast.CallExpression:
		for _, arg := range e.Args {
			cg.scanStringVarOriginsExpr(arg)
		}
	case *ast.MatchExpression:
		for _, arm := range e.Arms {
			cg.scanStringVarOriginsStmts(arm.Body, map[string]bool{})
		}
	case *ast.MemberAccessExpression:
		cg.scanStringVarOriginsExpr(e.Object)
	}
}

// IsSSOOnlyStringVar 查询变量是否为「全程仅字面量赋值的局部 String」。
func (cg *CodeGenerator) IsSSOOnlyStringVar(name string) bool {
	return name != "" && cg.ssoStringScanned[name] && !cg.ssoStringTainted[name]
}

// suppressSSOOnly 编译期诊断辅助：打印 SSO-only 集合（verbose 模式用）。
func (cg *CodeGenerator) debugSSOOnly() string {
	var b strings.Builder
	for name := range cg.ssoStringScanned {
		if cg.IsSSOOnlyStringVar(name) {
			fmt.Fprintf(&b, "%s ", name)
		}
	}
	return b.String()
}
