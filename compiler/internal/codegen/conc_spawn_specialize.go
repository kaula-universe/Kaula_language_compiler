// Kaula 并发内核（CONC）· spawn 实参常量特化（A 方案）
//
// spawn f(c1, c2, ...) 的实参若全部是编译期字面量，就克隆一份特化版
// conc fn：形参直接代入常量、重命名、插到生成列表头部，spawn 调用改指
// 特化版。收益：
//
//   1. 去参数化：特化版 Frame 没有被常量代入的参数字段（如 task(i, 0)
//      的 pct 字段消失），spawn 少写参数、resume/enter 少恢复参数；
//   2. 重分析：特化版重新 AnalyzeBody —— 若代入后挂起点落在恒判分支，
//      守卫消失甚至降成单形态（当前只做字面量代入，不做运行时常数折叠
//      与范围推理，task(i,0) 的 `i%100<0` 仍不可静态判定）。
//
// 保守边界：只认字面量实参（整数/浮点/字符串/布尔），只处理形参名
// 的直接标识符替换（函数体内与形参同名的局部变量也会被替换 ——
// 遮蔽同名在 kl 里罕见，且错误方向是"多特化"，语义由重分析兜底）。

package codegen

import (
	"fmt"

	"kaula/internal/ast"
	"kaula/internal/codegen/conc"
)

// isConstLiteralExpr 报告表达式是否为编译期字面量（可安全代入特化）
func isConstLiteralExpr(e ast.Expression) bool {
	if e == nil {
		return false
	}
	switch e.(type) {
	case *ast.IntegerLiteral, *ast.StringLiteral, *ast.BooleanLiteral, *ast.FloatLiteral:
		return true
	}
	return false
}

// findSpawnExprs 递归遍历语句与表达式，收集所有 SpawnExpression
func findSpawnExprs(stmts []ast.Statement, out *[]*ast.SpawnExpression) {
	for _, st := range stmts {
		if st == nil {
			continue
		}
		switch s := st.(type) {
		case *ast.ExpressionStatement:
			findSpawnExpr(s.Expression, out)
		case *ast.VariableDeclaration:
			findSpawnExpr(s.Value, out)
		case *ast.ReturnStatement:
			findSpawnExpr(s.Value, out)
		case *ast.IfStatement:
			findSpawnExpr(s.Condition, out)
			findSpawnExprs(s.Body, out)
			findSpawnExprs(s.Else, out)
		case *ast.WhileStatement:
			findSpawnExpr(s.Condition, out)
			findSpawnExprs(s.Body, out)
		case *ast.ForStatement:
			findSpawnExpr(s.Condition, out)
			if s.Init != nil {
				if vd, ok := s.Init.(*ast.VariableDeclaration); ok {
					findSpawnExpr(vd.Value, out)
				}
			}
			findSpawnExprs(s.Body, out)
		case *ast.ForInStatement:
			findSpawnExpr(s.Iterable, out)
			findSpawnExprs(s.Body, out)
		case *ast.BlockStatement:
			findSpawnExprs(s.Statements, out)
		case *ast.CallStatement:
			findSpawnExprs(s.Body, out)
		case *ast.FunctionStatement:
			findSpawnExprs(s.Body, out)
		}
	}
}

// findSpawnExpr 递归遍历表达式树，收集 SpawnExpression
func findSpawnExpr(e ast.Expression, out *[]*ast.SpawnExpression) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *ast.SpawnExpression:
		*out = append(*out, x)
	case *ast.BinaryExpression:
		findSpawnExpr(x.Left, out)
		findSpawnExpr(x.Right, out)
	case *ast.CallExpression:
		findSpawnExpr(x.Function, out)
		for _, a := range x.Args {
			findSpawnExpr(a, out)
		}
	case *ast.MemberAccessExpression:
		findSpawnExpr(x.Object, out)
	case *ast.IndexExpression:
		findSpawnExpr(x.Object, out)
		findSpawnExpr(x.Index, out)
	case *ast.UnaryExpression:
		findSpawnExpr(x.Right, out)
	case *ast.TypeCastExpression:
		findSpawnExpr(x.Expression, out)
	}
}

// replaceIdentInExpr 递归替换指定名称的 Identifier 为 repl（不可变风格：
// 返回替换后的表达式；节点内部字段原地改）。
func replaceIdentInExpr(e ast.Expression, name string, repl ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	switch x := e.(type) {
	case *ast.Identifier:
		if x.Name == name {
			return repl
		}
		return x
	case *ast.SpawnExpression:
		// 函数名不替换（被 spawn 的是另一个 conc fn，不可能与形参同名）
		for i := range x.Args {
			x.Args[i] = replaceIdentInExpr(x.Args[i], name, repl)
		}
		return x
	case *ast.BinaryExpression:
		x.Left = replaceIdentInExpr(x.Left, name, repl)
		x.Right = replaceIdentInExpr(x.Right, name, repl)
		return x
	case *ast.CallExpression:
		x.Function = replaceIdentInExpr(x.Function, name, repl)
		for i := range x.Args {
			x.Args[i] = replaceIdentInExpr(x.Args[i], name, repl)
		}
		return x
	case *ast.MemberAccessExpression:
		x.Object = replaceIdentInExpr(x.Object, name, repl)
		return x
	case *ast.IndexExpression:
		x.Object = replaceIdentInExpr(x.Object, name, repl)
		x.Index = replaceIdentInExpr(x.Index, name, repl)
		return x
	case *ast.UnaryExpression:
		x.Right = replaceIdentInExpr(x.Right, name, repl)
		return x
	case *ast.TypeCastExpression:
		x.Expression = replaceIdentInExpr(x.Expression, name, repl)
		return x
	}
	return e
}

// replaceIdentInStmts 递归替换语句树里的 Identifier
func replaceIdentInStmts(stmts []ast.Statement, name string, repl ast.Expression) {
	for _, st := range stmts {
		if st == nil {
			continue
		}
		switch s := st.(type) {
		case *ast.ExpressionStatement:
			s.Expression = replaceIdentInExpr(s.Expression, name, repl)
		case *ast.VariableDeclaration:
			s.Value = replaceIdentInExpr(s.Value, name, repl)
		case *ast.ReturnStatement:
			s.Value = replaceIdentInExpr(s.Value, name, repl)
		case *ast.IfStatement:
			s.Condition = replaceIdentInExpr(s.Condition, name, repl)
			replaceIdentInStmts(s.Body, name, repl)
			replaceIdentInStmts(s.Else, name, repl)
		case *ast.WhileStatement:
			s.Condition = replaceIdentInExpr(s.Condition, name, repl)
			replaceIdentInStmts(s.Body, name, repl)
		case *ast.ForStatement:
			s.Condition = replaceIdentInExpr(s.Condition, name, repl)
			if vd, ok := s.Init.(*ast.VariableDeclaration); ok {
				vd.Value = replaceIdentInExpr(vd.Value, name, repl)
			}
			replaceIdentInStmts(s.Body, name, repl)
		case *ast.ForInStatement:
			s.Iterable = replaceIdentInExpr(s.Iterable, name, repl)
			replaceIdentInStmts(s.Body, name, repl)
		case *ast.BlockStatement:
			replaceIdentInStmts(s.Statements, name, repl)
		case *ast.CallStatement:
			replaceIdentInStmts(s.Body, name, repl)
		}
	}
}

// specializeSpawnConsts：spawn 实参全字面量 → 克隆特化 conc fn，
// 形参代入常量、重命名、插入生成列表头部，spawn 调用改指特化版。
func (cg *CodeGenerator) specializeSpawnConsts(program *ast.Program) {
	var spawns []*ast.SpawnExpression
	findSpawnExprs(program.Statements, &spawns)
	if len(spawns) == 0 {
		return
	}

	// 顶层 conc fn 索引（特化只针对顶层定义的 conc fn）
	fns := make(map[string]*ast.FunctionStatement)
	for _, st := range program.Statements {
		if fn, ok := st.(*ast.FunctionStatement); ok && fn.IsConcurrent {
			fns[fn.Name] = fn
		}
	}

	seq := 0
	for _, sp := range spawns {
		ident, ok := sp.Function.(*ast.Identifier)
		if !ok {
			continue
		}
		fn := fns[ident.Name]
		if fn == nil || len(sp.Args) == 0 {
			continue
		}
		allConst := true
		for _, a := range sp.Args {
			if !isConstLiteralExpr(a) {
				allConst = false
				break
			}
		}
		if !allConst {
			continue
		}

		// 克隆 inline 后的定义（inlineConcFns 已先行，嵌套调用已展开）
		clone, ok := cloneNode(fn).(*ast.FunctionStatement)
		if !ok {
			continue
		}
		clone.Name = fmt.Sprintf("%s_spc_%d", fn.Name, seq)
		seq++

		// 形参 → 常量代入（只改**函数体**，形参列表原样保留）
		for i, p := range clone.Params {
			if i < len(sp.Args) {
				replaceIdentInStmts(clone.Body, p, sp.Args[i])
			}
		}
		/*
		 * 形参列表**必须原样保留** —— 曾经在这里 `clone.Params = nil`，
		 * 理由是「参数已全部代入常量，Frame 不该再有参数字段」。
		 *
		 * 那个理由站不住：spawn 的调用点仍然按原签名传实参
		 * （GenerateSpawn 走非内联路径时写 `__frame->rounds = 5`，
		 *  走内联路径时写 `K_x_spc_N_as_fn(5)`），
		 * 而 emitter 是**从 AST 的 Params 建的**（generateConcurrentFunction）。
		 * 清空 Params 于是产生两处错位：
		 *
		 *   1. Frame 里没有 rounds 字段，但 GenerateSpawn 照写不误
		 *      → C 报 no member named 'rounds'
		 *   2. shim 定义按 Params 空生成 `(void)`，而调用点传 1 个实参
		 *      → C 报 too many arguments to function call,
		 *        expected 0, have 1
		 *
		 * 实测 test_for_range_suspend.kl 的 `spawn loop_nested(5)` 必现
		 * （loop_nested 的所有实参都是字面量 → 命中特化路径）。
		 *
		 * 保留 Params 的代价是 Frame 多几个常量字段、spawn 多写几个常量 ——
		 * 几字节，换来的是生成代码自洽。真正省下的收益（体内引用已被
		 * 常量折叠掉、不再读形参）一分不少。
		 *
		 * 注意 ParamCTypes/ParamNullables 与 Params 必须同长：
		 * generateConcurrentFunction 会按 Params 逐个建 C 类型，
		 * 三张表错位就是又一次静默的签名错乱。
		 */
		if len(clone.ParamTypes) < len(clone.Params) {
			// 防御：ParamTypes 短于 Params 时补齐，避免下游按下标越界
			for len(clone.ParamTypes) < len(clone.Params) {
				clone.ParamTypes = append(clone.ParamTypes, "")
			}
		}
		if len(clone.ParamNullables) < len(clone.Params) {
			for len(clone.ParamNullables) < len(clone.Params) {
				clone.ParamNullables = append(clone.ParamNullables, false)
			}
		}

		// 插入头部：先生成 → 先注册 emitter → 后到的 spawn 引用可查到
		program.Statements = append([]ast.Statement{clone}, program.Statements...)
		// 重写 spawn 调用指向特化版
		ident.Name = clone.Name
	}
}

// preRegisterConcFns：主生成前预注册所有 conc fn 的完整 emitter。
//
// 生成顺序问题：fn A 顺序调用 conc fn B（B 定义在 A 之后）时，A 生成时
// B 尚未经 EmitResume 注册 → LookupConcFunc 查不到 → 顺序调用按普通 C
// 函数生成（K_compute(7)），而 conc fn 没有同名普通 C 定义 → 链接失败。
// spawn 常量特化把特化版插到头部，使该问题从「可能」变成「必现」。
//
// 预注册做完整形态判定（AnalyzeBody + SetStateful/SetGuardLayout），
// 与生成时 EmitResume 的判定同源；生成时再注册覆盖，判定一致。
func (cg *CodeGenerator) preRegisterConcFns(program *ast.Program) {
	for _, st := range program.Statements {
		fn, ok := st.(*ast.FunctionStatement)
		if !ok || !fn.IsConcurrent {
			continue
		}
		// 注意：预注册跑在 isUserKaulaFunc 等表填充之前，UserFuncCName
		// 此时对用户函数返回的是原名（compute 而非 K_compute），与生成时
		// （EmitCall Lookup 用 K_compute）不一致 → 查不到。这里按与
		// funcgen 构造 emitter 相同的规则手动定名：用户 conc fn 非 export
		// 一律 K_ 前缀；export 保持原名（FFI 边界）。
		cName := fn.Name
		if !fn.IsExported {
			cName = "K_" + fn.Name
		}
		if cg.LookupConcFunc(cName) != nil {
			continue
		}
		emitter := &conc.Emitter{
			FuncName:   fn.Name,
			CName:      cName,
			ReturnType: fn.ReturnType,
			CReturnType: tgReturnTypeToC(cg.typeGenerator, fn.ReturnType) + " ",
			Params:     fn.Params,
			ParamTypes: fn.ParamTypes,
		}
		for i := range fn.Params {
			ctype := "int64_t"
			isNullable := false
			if i < len(fn.ParamNullables) {
				isNullable = fn.ParamNullables[i]
			}
			if i < len(fn.ParamTypes) && fn.ParamTypes[i] != "" {
				if t := cg.typeGenerator.convertType(fn.ParamTypes[i], isNullable); t != "" {
					ctype = t
				}
			}
			emitter.ParamCTypes = append(emitter.ParamCTypes, ctype)
			emitter.ParamNullables = append(emitter.ParamNullables, isNullable)
		}
		analysis := conc.AnalyzeBody(fn.Body)
		emitter.SetStateful(analysis.NeedsSuspend)
		emitter.SetGuardLayout(analysis.CompoundDepth, analysis.SuspendMaxDepth, analysis.HasForIn)
		cg.RegisterConcFunc(emitter)
	}
}
