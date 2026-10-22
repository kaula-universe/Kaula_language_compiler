package codegen

// 本文件是「结构宏」的展开器：前缀模板在语义上是一次 AST 层面的替换，
// 而不是文本拼接。
//
// 展开一次 @Name(参数…) 的完整流程如下（顺序有语义，不可调换）：
//
//  1. 找出模板体（#[prefix] 函数体或 prefix 语句体），跳过参数声明语句，
//     把调用体拼到占位符处 → 得到"展开骨架"。
//  2. 克隆骨架：模板 AST 是所有调用点共享的只读对象，任何改写都必须作用在
//     副本上（后续语义分析阶段也会对展开后的 AST 做事跨越引用解析）。
//  3. 自动 alpha 重命名：骨架里**本层声明**的局部变量改为唯一名
//     `__kaula_m<序号>_<原名>`，消除变量捕获（宏的临时变量不再踩调用方的同名变量）。
//  4. `$参数` 替换：参数绑定载荷是实参 AST（PrefixParamBinding.Expr），
//     `$p` 处直接换成实参节点的克隆；默认值同理（AST 绑定在模板上下文中求值）。
//       - 整条语句就是一个 `$p` → 参数展开：表达式参数按语句生成，
//         语句级参数（调用体内联的 $body）整段替换；
//       - 表达式位置上的 `$p` → 实参表达式内联。
//  5. 直引重写：既非本地声明、也非 `$参数` 的名字，若调用点不可见而在外层
//     可见，则改写为外层变量的唯一名（跨层传递 `$x` 的场景）。
//  6. 结构约束校验：按 tree 注解 / 形参类型声明逐参数验证（见 macro_tree.go），
//     不满足时**在生成任何代码之前**报错。
//  7. 按重写表改写 AST：`$参数` 直接换成实参节点的克隆。
//
// 展开结果以语句块交给 generateBlockStatement 生成，这样作用域登记、
// liveness 与 survivor 回收都发生在块内（手写 "{ }" 文本会让块内变量登记
// 到外层作用域，回收代码落到块外）。

import (
	"fmt"
	"sort"
	"strings"

	"kaula/internal/ast"
	"kaula/internal/astutil"
	"kaula/internal/comptime"
)

// macroExpansion 一次结构宏展开的产物
type macroExpansion struct {
	Name  string // 前缀名（用于诊断信息）
	Stmts []ast.Statement

	// Renames 模板本地变量的重命名表（原名 → 唯一名），诊断与测试用
	Renames map[string]string
	// Decls 重命名后的局部变量声明（新名 → 声明节点），tree 结构校验用
	Decls map[string]*ast.VariableDeclaration
	// Params 参数名 → 实参 AST（已绑定，含默认值）
	Params map[string]ast.Expression
	// Frees 直引名 → 展开时采用的调用点实际名字（仅记录发生了重写的）
	Frees map[string]string
	// ParamTypeDecls 形参声明（#[prefix] fn / macro 的形参），供约束校验
	ParamTypeDecls map[string]string
	// Sig 前缀的结构签名（tree 注解 / 结构类型形参），供结构约束校验
	Sig *MacroSignature
	// BodyStmts 调用体语句（展开前），$body 结构校验用
	BodyStmts []ast.Statement
	// Bodies 展开结果里内联进来的调用体（调用体作用域内，$参数 可直接用）
	Bodies []*macroBodyInfo
	// AllParams 模板声明的全部 $参数名
	AllParams []string
	// Errors 展开期发现的调用点/结构错误（由调用方上报，避免重复报错）
	Errors []string
	// Lets 「实参只求值一次」的绑定计划：$参数名 → 临时变量名。
	// 模板骨架里右值引用 ≥2 次、且从未作为赋值左值的参数，实参求值一次
	// 绑到 __auto_type 临时变量，骨架里的引用改指临时变量（见第 5.5 步）。
	Lets map[string]string
}

// macroBodyInfo 描述一段内联进展开结果的调用体
type macroBodyInfo struct {
	Stmts     []ast.Statement
	AllParams []string
}

func (e *macroExpansion) addError(msg string) {
	e.Errors = append(e.Errors, msg)
}

// expandStructureMacro 按结构宏语义展开一次前缀调用（只做 AST 改写，不生成代码）。
//
// 返回 ok=false 表示前缀不是模板（普通函数调用形式），调用方应按语句处理。
func (sg *StatementGenerator) expandStructureMacro(e *ast.PrefixCallExpression) (*macroExpansion, bool) {
	exp, ok := sg.expandStructureMacroIn(e, true)
	return exp, ok
}

// expandStructureMacroIn 与 expandStructureMacro 相同，spliceBody 指定是否把调用
// 点提供的 `{ ... }` 调用体拼接进展开结果。
//
// 为什么需要区分：`$body` 拼接是针对**语句位置** `@Name(...) { 调用体 }` 的
// 语法糖；表达式位置的 `@Name(...)` 没有调用体，它的值由模板体自己给出。
// 统一拼进去会让 `i64 v = @NoValue(x = 1)` 静默取到调用体的最后一条语句。
func (sg *StatementGenerator) expandStructureMacroIn(e *ast.PrefixCallExpression, spliceBody bool) (*macroExpansion, bool) {
	if e == nil {
		return nil, false
	}
	bodyStmts := e.Body
	if !spliceBody {
		bodyStmts = nil
	}
	funcDecl := sg.findFunctionDeclaration(e.Name)
	// Task #1：多规则按结构分派。一个前缀名可以定义多条 #[prefix] fn /
	// #[prefix,tree] fn / #[tree] fn，每条用不同的 tree(...) 签名声明它接受的
	// 实参结构。展开时按声明顺序选择第一条「所有约束都命中」的规则；
	// 无 tree 约束的规则作为兜底，最后才选。
	if rules := sg.codegen.findPrefixRules(e.Name); len(rules) > 1 {
		funcDecl = sg.selectPrefixRule(e, rules)
	}
	var tplBody []ast.Statement
	var sig *MacroSignature
	paramTypeDecls := map[string]string{}
	found := false
	if funcDecl != nil {
		annotation := funcDecl.GetAnnotation()
		if annotation != ast.TreeAnnotationPrefix &&
			annotation != ast.TreeAnnotationPrefixTree &&
			annotation != ast.TreeAnnotationTree {
			return nil, false
		}
		tplBody = funcDecl.Body
		for i, name := range funcDecl.Params {
			if i < len(funcDecl.ParamTypes) && funcDecl.ParamTypes[i] != "" {
				paramTypeDecls[name] = funcDecl.ParamTypes[i]
			}
		}
		found = true
	} else if prefixStmt := sg.codegen.findPrefixStatement(e.Name); prefixStmt != nil {
		tplBody = prefixStmt.Body
		found = true
	}
	if !found {
		return nil, false
	}
	if err := sg.enterMacroExpansion(e.Name); err != nil {
		exp := &macroExpansion{Name: e.Name, Renames: map[string]string{}, Params: map[string]ast.Expression{}}
		exp.addError(err.Error())
		return exp, true
	}
	defer sg.leaveMacroExpansion()

	// 结构签名：`tree(...)` / `struct(...)` 注解参数或同名 `tree Name { ... }`
	sig = sg.macroSignatureFor(e.Name, funcDecl, nil)

	// 已声明参数与调用体参数分区（sema 已做过一次，此处幂等，兜底 codegen 单独使用的场景）
	declared := ast.DeclaredPrefixParams(tplBody)
	for name := range paramTypeDecls {
		declared[name] = true
	}
	e.CollectBodyParams(declared)

	exp := &macroExpansion{
		Name:           e.Name,
		BodyStmts:      bodyStmts,
		Renames:        map[string]string{},
		Decls:          map[string]*ast.VariableDeclaration{},
		Params:         map[string]ast.Expression{},
		Frees:          map[string]string{},
		ParamTypeDecls: paramTypeDecls,
		Sig:            sig,
	}
	for name := range declared {
		exp.AllParams = append(exp.AllParams, name)
	}
	sort.Strings(exp.AllParams)

	bindings := map[string]ast.Expression{}
	for name, value := range e.Params {
		if value != nil {
			bindings[name] = value
		}
	}
	for _, decl := range ast.DefaultPrefixParams(tplBody) {
		if _, ok := bindings[decl.Name]; !ok && decl.Value != nil {
			bindings[decl.Name] = decl.Value
		}
	}

	/* 第 1 步：骨架。
	 *
	 * 所有裸 `$p` 语句都是**参数声明**（模板体内不声明就等于由调用点提供），
	 * 因此一律从这里剔除，真正的值由第 4/7 步按绑定表替换。
	 *
	 * `$body` 特殊：它是调用体拼接点。为了兼容老写法（`$body` 出现在模板
	 * 中间、显式标出拼接位置），第一个裸 `$body` 处就地拼接调用体；若模板
	 * 里没有 `$body`（`#[prefix] fn` 最常见的形态），调用体追加到末尾。
	 * 没有调用体时（`@Name(x = 1)`，不写 `{ }`）什么都不拼接。
	 *
	 * `$p = expr` 有两种完全不同的含义，而且语法上无法区分：
	 *
	 *	$limit = 10        // 参数声明，10 是默认值，这一行不生成代码
	 *	$target = $x + 1   // 普通赋值，改的是调用点传进来的变量
	 *
	 * 判据只有一条：**调用点有没有为这个名字传实参**。传了实参就说明 `$p`
	 * 是参数，这一行只能是普通赋值（实参才是权威绑定），必须生成；
	 * 没传实参时，若它是模板里第一次出现就紧跟 `=`，按默认值声明丢弃，
	 * 否则按普通赋值保留。这样 `$target = $target + $value` 永远不会被误吞。 */
	calledParams := map[string]bool{}
	for name := range e.Params {
		calledParams[name] = true
	}
	/* leadingDecl：名字在模板体里第一次出现就是 `$p = …`。
	 * CollectPrefixParamRefs 的「首次出现才算声明」判据只管得到字面顺序，
	 * 对 `$target = $target + $value` 这种"先赋值后引用"的写法恰好会误判，
	 * 所以这里再补一条：调用点传了实参的参数，其 `$p = …` 一律不是声明。 */
	leadingDecl := map[string]bool{}
	for _, ref := range ast.DefaultPrefixParams(tplBody) {
		if !calledParams[ref.Name] {
			leadingDecl[ref.Name] = true
		}
	}
	var skeleton []ast.Statement
	var varargParam string
	bodySpliced := false
	for _, bodyStmt := range tplBody {
		if vd, ok := bodyStmt.(*ast.PrefixVarArgDecl); ok {
			// 变参匹配声明 `$( $x ),*`：记录参数名，本身不生成代码
			varargParam = vd.Param
			continue
		}
		if name, ok := ast.BarePrefixParamName(bodyStmt); ok {
			if name == "body" && !bodySpliced && len(bodyStmts) > 0 {
				skeleton = append(skeleton, bodyStmts...)
				bodySpliced = true
			}
			continue
		}
		if name, _, ok := ast.PrefixParamDefaultDecl(bodyStmt); ok && leadingDecl[name] {
			continue
		}
		skeleton = append(skeleton, bodyStmt)
	}
	if !bodySpliced {
		skeleton = append(skeleton, bodyStmts...)
	}
	// 变参合法性校验：
	//  - 模板没有 `$( $x ),*` 匹配声明却收到位置实参 → 报错
	//  - 模板声明了变参却又收到命名参数 → 报错（不支持混用）
	if len(e.VarArgs) > 0 && varargParam == "" {
		exp.addError(fmt.Sprintf("前缀 '%s' 不接受位置实参：模板缺少 `$( $x ),*` 匹配声明（收到的位置实参个数 %d）",
			e.Name, len(e.VarArgs)))
	}
	if varargParam != "" && len(e.Params) > 0 {
		exp.addError(fmt.Sprintf("前缀 '%s' 不支持命名参数与位置实参混用：模板声明了变参 `$( $%s ),*`，位置实参应全部写在 @%s(…) 内且不带名字",
			e.Name, varargParam, e.Name))
	}

	// 第 2 步：克隆骨架（模板 AST 共享只读）
	cloned := astutil.CloneStatements(skeleton)

	// 第 3 步：自动 alpha 重命名
	sg.codegen.macroRenameSeq++
	renamed, renames, decls := astutil.AlphaRenameTemplate(cloned, sg.codegen.macroRenameSeq)
	exp.Renames = renames
	exp.Decls = decls

	/* 第 3.5 步：变参绑定 + 重复展开。
	 *
	 * 模板含 `$( $x ),*` 匹配声明时，调用点的位置实参在展开块开头各求值
	 * 一次，绑定到 `__auto_type` 临时变量（__kaula_b{seq}_va{i}，与第 5.5 步
	 * 的 let 绑定共用同一序号计数器，名字不会碰撞），重复占位按实参个数
	 * 展开并把 `$x` 依次替换为临时变量：
	 *   - `$( <语句> ),*`（PrefixRepeatStmt）→ 重复整条语句；
	 *   - `$( <表达式> ),*`（PrefixRepeatExpr）→ 只允许出现在调用实参、
	 *     数组元素或前缀位置实参中，重复并以逗号连接。
	 *
	 * 语义保证：位置实参只求值一次（副作用不翻倍）；零个位置实参时重复
	 * 占位展开为空。模板若没有引用变参的重复占位，位置实参不求值、不绑定。 */
	if varargParam != "" {
		if sg.containsVarArgRepeat(renamed) {
			temps := make([]ast.Expression, 0, len(e.VarArgs))
			prelude := make([]ast.Statement, 0, len(e.VarArgs))
			for i, arg := range e.VarArgs {
				sg.codegen.prefixLetSeq++
				temp := fmt.Sprintf("%s%d_va%d", prefixLetTempPrefix, sg.codegen.prefixLetSeq, i)
				prelude = append(prelude, &ast.VariableDeclaration{
					Name:   temp,
					Type:   "__auto_type",
					IsAuto: true,
					Value:  astutil.CloneExpression(arg),
					Pos:    e.Pos,
				})
				temps = append(temps, &ast.Identifier{Name: temp})
			}
			renamed = append(prelude, renamed...)
			if errs := sg.expandVarArgRepeatsInPlace(&renamed, varargParam, temps); len(errs) > 0 {
				for _, err := range errs {
					exp.addError(err)
				}
			}
		}
		// 重复展开后残留的裸 `$x` 引用：变参只能通过重复占位消费
		if hasPrefixVarRef(renamed, varargParam) {
			exp.addError(fmt.Sprintf("前缀 '%s' 的变参 '$%s' 只能通过 `$( $%s ),*` 重复占位引用",
				e.Name, varargParam, varargParam))
		}
	}

	// 第 4 步：参数替换（AST 层面）。实参本身也可能引用 $参数（转发），
	// 因此替换表按"模板绑定 + 外层已绑定的 $参数"合并，自内向外。
	sg.codegen.pushMacroFreeBindings(nil)
	paramTable := make(map[string]ast.Expression, len(bindings))
	for k, v := range bindings {
		paramTable[k] = v
	}
	// 外层宏的 $参数（嵌套展开时可用）：只补本层没有的，本层优先
	for i := len(sg.codegen.prefixBindings) - 1; i >= 0; i-- {
		for k, b := range sg.codegen.prefixBindings[i] {
			if _, exists := paramTable[k]; exists {
				continue
			}
			if b.Expr != nil {
				paramTable[k] = b.Expr
			}
		}
	}
	exp.Params = paramTable

	/* 第 5 步：直引重写。
	 * 只对"既不是本层本地变量、也不是 $参数"的名字解析，因为这两类的语义
	 * 已经由第 3/4 步确定；解析结果为空表示保持原名（调用点可见）。 */
	frees := map[string]string{}
	for _, name := range astutil.ScanFreeReferences(renamed) {
		if _, isLocal := renames[name]; isLocal {
			continue
		}
		resolved := sg.codegen.resolveMacroFreeName(name)
		if resolved != "" && resolved != name {
			frees[name] = resolved
		}
	}
	exp.Frees = frees
	sg.codegen.pushMacroFreeBindings(frees)

	/* 第 5.5 步：实参只求值一次（let 绑定）。
	 *
	 * 模板骨架里**右值表达式引用 ≥2 次**、且**从未作为赋值左值**出现的
	 * $参数，先在展开块开头把实参求值一次绑到 `__auto_type` 临时变量，
	 * 再把骨架里的引用改写为临时变量名。语义：实参表达式只执行一次，
	 * 副作用（IO、计数器、写变量）不再因为内联两次而翻倍。
	 *
	 * 绑定声明必须插在直引重写（第 5 步）**之后**：早于第 5 步，绑定声明
	 * 里调用点的实参标识符会被直引重写误当成模板直引，改成外层变量的
	 * 唯一名。绑定临时变量以 __auto_type 登记符号：isStringTyped /
	 * isPointerTyped 均判 false，作用域出口不生成任何 free，天然规避
	 * String 浅拷贝的双释放；其真实类型由 prefixLetKinds 提示表补上
	 * （第 8 步登记）。 */
	exp.Lets = sg.planPrefixLetBindings(renamed, paramTable)
	if len(exp.Lets) > 0 {
		prelude := make([]ast.Statement, 0, len(exp.Lets))
		for _, param := range sortedPrefixLetParams(exp.Lets) {
			temp := exp.Lets[param]
			prelude = append(prelude, &ast.VariableDeclaration{
				Name:   temp,
				Type:   "__auto_type",
				IsAuto: true,
				Value:  astutil.CloneExpression(paramTable[param]),
				Pos:    e.Pos,
			})
		}
		// 绑定声明插在展开块开头：实参在宏体任何语句执行之前求值一次
		renamed = append(prelude, renamed...)
		// 骨架与绑定声明里的 $参数 引用统一改指临时变量（转发引用
		// `$a = $b` 的绑定值里若有其他绑定参数，同样指向其临时变量）
		for _, param := range sortedPrefixLetParams(exp.Lets) {
			renamed = bindPrefixParamRefs(renamed, param, exp.Lets[param])
		}
	}

	// 第 6 步：结构约束校验（tree 注解 / tree 模板），在生成代码之前发现问题
	if errs := sg.validateMacroArguments(exp, len(e.Body)); len(errs) > 0 {
		sg.codegen.popMacroFreeBindings()
		sg.codegen.popMacroFreeBindings()
		for _, msg := range errs {
			exp.addError(msg)
		}
		/*
		 * 结构不匹配时不继续展开：继续展开只会把错误推迟到 C 编译阶段，
		 * 报出与源码无关的行号。返回空展开，主流程以"已报错"终止。
		 */
		exp.Stmts = nil
		return exp, true
	}

	// 第 7 步：按重写表改写（$参数 克隆、本地变量与直引改名）
	bind := astutil.NewBindings()
	bind.Params = paramTable
	bind.Locals = renames
	bind.Frees = frees
	rewriter := &macroRewriter{sg: sg, exp: exp}

	expanded := rewriter.rewriteStmts(renamed)

	// 第 8 步：登记「实参只求值一次」绑定临时变量的类型提示。
	// 绑定声明与骨架同批改写，Value 此时已完全物化（转发 $q 也已替换），
	// 用它推断的 kind 最准确。不登记的话 __auto_type 符号在 inferType 的
	// Identifier 分支兜底成 "d"：`$s` 绑定后 `__kaula_b0_s + "!"` 会被判成
	// 数字加法，println 按 %d 打印 String 结构体垃圾。
	if len(exp.Lets) > 0 {
		for _, s := range expanded {
			if vd, ok := s.(*ast.VariableDeclaration); ok && vd.IsAuto && strings.HasPrefix(vd.Name, prefixLetTempPrefix) {
				if vd.Value != nil {
					sg.codegen.prefixLetKinds[vd.Name] = sg.codegen.expressionGenerator.inferType(vd.Value)
				}
			}
		}
	}

	sg.codegen.popMacroFreeBindings()
	sg.codegen.popMacroFreeBindings()

	exp.Stmts = expanded
	return exp, true
}

// selectPrefixRule 在候选规则里选择第一条「实参结构命中」的规则。
//
// 多规则分派的语义（Task #1）：按声明顺序遍历，用调用点实参的 AST 结构去
// 匹配每条规则的 tree 签名；第一条全部命中者胜出。若没有带约束的规则命中，
// 回退到第一条无约束规则（兜底，兼容"前缀也能当普通模板"的老写法）。
// 若所有规则都带约束且都不命中，返回第一条，让后续结构校验报出最有用的
// 错误（指出期望结构 vs 实际结构），而不是静默选错。
func (sg *StatementGenerator) selectPrefixRule(e *ast.PrefixCallExpression, rules []*ast.FunctionStatement) *ast.FunctionStatement {
	if len(rules) == 0 {
		return nil
	}
	if len(rules) == 1 {
		return rules[0]
	}
	// 第一遍：带约束的规则按声明顺序匹配
	for _, fn := range rules {
		sig := sg.macroSignatureFor(e.Name, fn, nil)
		if sig.IsEmpty() {
			continue // 无约束规则留到第二遍作兜底
		}
		if sg.ruleMatches(e, sig) {
			return fn
		}
	}
	// 第二遍：无约束规则作兜底
	for _, fn := range rules {
		if sg.macroSignatureFor(e.Name, fn, nil).IsEmpty() {
			return fn
		}
	}
	// 全部带约束且都不命中：返回第一条，让结构校验报错
	return rules[0]
}

// ruleMatches 判断调用点实参是否满足某条规则的结构签名。
//
// 只用调用点**显式传入**的实参（e.Params）做匹配：未传的参数会走模板里的
// 默认值，其结构由展开阶段校验，分派时不据此判否——否则「某参数有默认值、
// 但调用点没传」会让规则被错误地排除。约束来自 tree(...) 注解与形参类型
// 声明（见 macroSignatureFor）。
func (sg *StatementGenerator) ruleMatches(e *ast.PrefixCallExpression, sig *MacroSignature) bool {
	if sig == nil || len(sig.Params) == 0 {
		return true
	}
	for name, constraint := range sig.Params {
		if constraint == nil {
			continue
		}
		value, ok := e.Params[name]
		if !ok || value == nil {
			continue // 未传实参：默认值兜底，分派不判否
		}
		if err := checkMacroArgument(e.Name, name, value, constraint); err != "" {
			return false
		}
		// 值约束 tree(mode = "int")：用 comptime 求值实参并比较字符串结果。
		// 这是值域反哺宏分派的反向跨——comptime(shape_of(v)) 算出的字符串
		// 能直接决定选中哪条规则。
		if constraint.Value != "" {
			ev := comptime.NewEvaluator()
			val, err := ev.Eval(value)
				if err != nil || val == nil || val.String() != constraint.Value {
				return false
			}
		}
	}
	return true
}

// generatePrefixCallBody 生成前缀调用的展开代码（编译期结构宏展开）。
//
// 展开规则（语义见 docs/tutorials/prefix-system.md）：
//   - 裸 `$param` 声明与 `$param = 默认值` 声明不生成代码；
//   - `$param` 经前缀绑定栈替换为实参（含默认值）的 AST；
//   - 模板本地局部变量展开时自动唯一化（alpha 重命名），不再依赖用户手工避让；
//   - 调用体在首个未绑定的裸 `$param` 处拼接（$body 占位），否则追加在模板体之后；
//   - 整体包裹块作用域，避免多次展开时变量重定义。
func (sg *StatementGenerator) generatePrefixCallBody(e *ast.PrefixCallExpression) string {
	// 非模板（普通函数调用形式的前缀函数）：只处理调用体内的语句
	if !sg.isPrefixTemplate(e.Name) {
		code := ""
		sg.codegen.indent++
		for _, bodyStmt := range e.Body {
			code += sg.codegen.indentString() + sg.codegen.generateStatement(bodyStmt)
		}
		sg.codegen.indent--
		return code
	}

	/* 生成期递归闸门。
	 *
	 * 展开器本身只展开一层，返回的语句里可能仍含宏调用（模板体里的
	 * `@Loop(...)`）。这些语句随后由 generateBlockStatement 生成，又回到
	 * 本函数——于是形成 generatePrefixCallBody → generateBlockStatement →
	 * generatePrefixCallBody 的递归，且每一层的 exp.Stmts 都正常非空。
	 * expandStructureMacro 内部的计数器在这种"每层返回后复位"的链路上
	 * 看不到嵌套，只能在这里按实际生成嵌套计。 */
	if sg.codegen.macroGenerateDepth >= macroMaxDepth {
		chain := strings.Join(sg.codegen.macroExpansionStack, " → ")
		if chain == "" {
			chain = e.Name
		}
		sg.codegen.error(fmt.Sprintf(
			"前缀 '%s' 展开层数超过上限 %d（展开链：%s）；前缀模板里是否出现了自我调用或互相调用？",
			e.Name, macroMaxDepth, chain))
		return ""
	}
	sg.codegen.macroGenerateDepth++
	defer func() { sg.codegen.macroGenerateDepth-- }()

	exp, ok := sg.expandStructureMacro(e)
	if !ok {
		/*
		 * 找不到模板：静默跳过。
		 *
		 * 这里**不能**只靠 codegen 报错——语义阶段已经对未知前缀名报过
		 * undefined_prefix，codegen 再报一次就是双报。真正危险的是
		 * "语义阶段没报、codegen 也查不到"的组合（跨文件前缀曾经就是如此：
		 * 编译 Success，@trace(...) 却被整个丢弃）。那种缺口现在由
		 * sema/codegen 两侧的本地 import 前缀注入（SetLocalExternalPrefixes）
		 * 堵住，见 cmd/kaulac 的 collectLocalPrefixDefs。
		 */
		return ""
	}
	for _, msg := range exp.Errors {
		sg.codegen.error(msg)
	}

	// 按普通块生成：作用域登记、liveness 与 survivor 回收都在块内完成
	return sg.generateBlockStatement(&ast.BlockStatement{Statements: exp.Stmts, Pos: e.Pos})
}

// isPrefixTemplate 判断前缀名是否指向一个模板（#[prefix] 函数或 prefix 语句）
func (sg *StatementGenerator) isPrefixTemplate(name string) bool {
	if fn := sg.findFunctionDeclaration(name); fn != nil {
		ann := fn.GetAnnotation()
		return ann == ast.TreeAnnotationPrefix ||
			ann == ast.TreeAnnotationPrefixTree ||
			ann == ast.TreeAnnotationTree
	}
	return sg.codegen.findPrefixStatement(name) != nil
}

// expandMacroExpression 在表达式位置展开结构宏（P1：表达式级宏返回）。
//
// 语义：`@Name(...)` 出现在表达式位置时，展开结果里**最后一个表达式语句**
// 的值就是宏的值；其余语句被包进 GNU 语句表达式 `({ ... })`，因此宏体里的
// 局部变量不会泄漏到外层作用域，且整体是一个表达式（可参与算术、可作实参）。
//
// 返回值 ok=false 表示没有可用的返回值（展开结果为空、或最后一条语句不是
// 表达式），调用方会给出明确错误，而不是静默生成 0。
func (sg *StatementGenerator) expandMacroExpression(e *ast.PrefixCallExpression) (string, bool) {
	/* 带调用体的 `@Name(...) { ... }` 是**语句**语法糖，不是表达式。
	 * 若这里把它当表达式处理，脚本会因为"最后一条语句恰好是调用"而静默
	 * 取到那个值，C 端则是一段不合法的初始值。直接给出错误更诚实。 */
	if len(e.Body) > 0 {
		sg.codegen.error(fmt.Sprintf("前缀 '%s' 在表达式位置使用，但携带了 `{ ... }` 调用体；调用体只能用于语句位置，表达式位置请去掉调用体并让模板体末尾给出要返回的表达式", e.Name))
		return "", true
	}
	if !sg.isPrefixTemplate(e.Name) {
		return "", false
	}
	exp, ok := sg.expandStructureMacroIn(e, false)
	if !ok {
		return "", false
	}
	for _, msg := range exp.Errors {
		sg.codegen.error(msg)
	}
	if len(exp.Stmts) == 0 {
		sg.codegen.error(fmt.Sprintf("前缀 '%s' 在表达式位置展开后没有任何语句，无法提供返回值", e.Name))
		return "", true
	}
	/* 值从哪来：展开结果的最后一条语句，且它必须是**一条语句里带值**的
	 * 形态（表达式语句 / return / yield）。
	 *
	 * 注意这里不能只看"最后一条语句是 ExpressionStatement"：解析器把每个
	 * 调用都包成 ExpressionStatement，于是 void 调用（println）也满足该条件，
	 * 宏会在毫无提示的情况下被当成"返回 println 的返回值"——C 端把
	 * `printf(...)` 的 int 塞进 i64 变量，语义完全不是用户写的那个。
	 * 所以还要确认语句形态本身允许出现在语句表达式末尾。 */
	last := exp.Stmts[len(exp.Stmts)-1]
	lastExpr, err := macroValueExpression(last)
	if err != nil {
		sg.codegen.error(fmt.Sprintf("前缀 '%s' 在表达式位置使用，但模板体没给出返回值：%s", e.Name, err.Error()))
		return "", true
	}

	// 前缀是模板才能走到这里：确有返回值即成功
	if len(exp.Stmts) == 1 {
		return sg.codegen.expressionGenerator.GenerateExpression(lastExpr), true
	}

	// 多条语句：语句表达式里逐条生成，保证局部变量作用域封闭
	prevInFunction := sg.codegen.inFunctionBody
	sg.codegen.inFunctionBody = false
	code := "({ "
	prevIndent := sg.codegen.indent
	sg.codegen.indent = 0
	for _, s := range exp.Stmts {
		gen := sg.codegen.generateStatement(s)
		code += strings.TrimSpace(gen) + " "
	}
	sg.codegen.indent = prevIndent
	sg.codegen.inFunctionBody = prevInFunction
	code += "})"
	return code, true
}

// macroRewriter 在克隆后的骨架上做参数替换、参数展开与调用体作用域处理
type macroRewriter struct {
	sg  *StatementGenerator
	exp *macroExpansion
}

// rewriteStmts 逐条改写；整条语句是单个 `$p` 时按参数展开处理
func (r *macroRewriter) rewriteStmts(stmts []ast.Statement) []ast.Statement {
	out := make([]ast.Statement, 0, len(stmts))
	for _, s := range stmts {
		if s == nil {
			continue
		}
		if id, ok := prefixVarStatement(s); ok {
			out = append(out, r.expandParamStatement(id, s.GetPosition())...)
			continue
		}
		out = append(out, r.rewriteStmt(s))
	}
	return out
}

// prefixVarStatement 识别「整条语句就是单个 $p」的形态（裸 `$p` 语句）
func prefixVarStatement(s ast.Statement) (*ast.Identifier, bool) {
	es, ok := s.(*ast.ExpressionStatement)
	if !ok {
		return nil, false
	}
	id, ok := es.Expression.(*ast.Identifier)
	if !ok || !id.IsPrefixVar {
		return nil, false
	}
	return id, true
}

// expandParamStatement 参数展开：把 `$p` 语句替换为实参对应的语句序列。
//
//   - 表达式参数 → 一条表达式语句（`$a + 1` 这样的参数是真实存在的用法）；
//   - 语句级参数（调用体、语句字面量）→ 整段语句替换。
func (r *macroRewriter) expandParamStatement(id *ast.Identifier, pos ast.Position) []ast.Statement {
	bound, ok := r.exp.Params[id.Name]
	if !ok || bound == nil {
		return nil
	}
	if call, isPrefix := bound.(*ast.PrefixCallExpression); isPrefix {
		/* 参数本身是宏调用：递归展开为语句。
		 *
		 * 这里的递归同样需要闸门。expandStructureMacro 内部的深度计数在它
		 * 返回时就复位了，而展开结果里的宏调用是在**本函数**里、且是在
		 * 外层返回之后才被处理的——即 `prefix Loop { $v  @Loop($v) }` 会
		 * 形成"展开→返回→再展开"的循环，深度始终是 0，永远不会触发上限。
		 * 用 r.depth 记录改写阶段自身的嵌套层数才行。 */
		if r.sg.codegen.macroRewriteDepth >= macroMaxDepth {
			return []ast.Statement{&ast.ExpressionStatement{
				Expression: call,
				Pos:        pos,
			}}
		}
		r.sg.codegen.macroRewriteDepth++
		exp, ok := r.sg.expandStructureMacro(call)
		r.sg.codegen.macroRewriteDepth--
		if ok {
			return exp.Stmts
		}
	}
	if isStatementLike(bound) {
		stmts := astutil.CloneStatements([]ast.Statement{boundAsStatement(bound)})
		// 语句级实参内部同样可能引用 $参数（`$target = $value` 里的 `$target` 是
		// 等号左侧的左值位置，不会被当作独立语句展开），必须一并改写，否则
		// 未绑定的 `$p` 会漏到 C 代码里。
		return r.rewriteStmts(stmts)
	}
	e := astutil.CloneExpression(bound)
	e.SetPosition(pos)
	return []ast.Statement{&ast.ExpressionStatement{Expression: r.rewriteExpr(e), Pos: pos}}
}

// isStatementLike 判断绑定值是否应按语句序列处理（语句级参数）
func isStatementLike(e ast.Expression) bool {
	_, ok := e.(ast.Statement)
	return ok
}

// boundAsStatement 把语句级绑定还原为语句节点
func boundAsStatement(e ast.Expression) ast.Statement {
	if s, ok := e.(ast.Statement); ok {
		return s
	}
	return &ast.ExpressionStatement{Expression: e, Pos: e.GetPosition()}
}

func (r *macroRewriter) rewriteStmt(s ast.Statement) ast.Statement {
	return astutil.RewriteStatementChildren(s, func(node ast.Node) ast.Node {
		switch v := node.(type) {
		case ast.Expression:
			return r.rewriteExpr(v)
		case ast.Statement:
			return r.rewriteStmt(v)
		}
		return nil
	})
}

// rewriteExpr 改写表达式：`$p` 内联实参 AST，其余下钻
func (r *macroRewriter) rewriteExpr(e ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	if id, ok := e.(*ast.Identifier); ok && id.IsPrefixVar {
		if bound, ok := r.exp.Params[id.Name]; ok && bound != nil {
			clone := astutil.CloneExpression(bound)
			clone.SetPosition(id.Pos)
			return clone
		}
		return e
	}
	return astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
		switch v := node.(type) {
		case ast.Expression:
			return r.rewriteExpr(v)
		case ast.Statement:
			return r.rewriteStmt(v)
		}
		return nil
	})
}

/* ── 「实参只求值一次」（let 绑定）── */

// prefixLetTempPrefix 「实参只求值一次」绑定临时变量的名字前缀。
// 独立分族（__kaula_b*），不与 alpha 重命名的 __kaula_m* 碰撞。
const prefixLetTempPrefix = "__kaula_b"

// prefixAssignOps 赋值类二元运算符（= 及复合赋值）。这些运算符的 Left 子树
// 是左值位置：`$target = $target + $value` 中 $target 既是左值又被引用，
// 若绑定，赋值目标会悄悄改到临时变量上，语义完全错误——左值出现即不绑定。
var prefixAssignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true,
}

// sortedPrefixLetParams 返回绑定计划 map 的 key 排序列表
// （确定性遍历：临时变量编号稳定）
func sortedPrefixLetParams(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// prefixLetPlanner 「实参只求值一次」的分析器：按模板骨架统计每个 $参数 的
// 右值引用次数与左值出现。
type prefixLetPlanner struct {
	counts map[string]int  // 右值引用次数
	lhs    map[string]bool // 是否在赋值左值位置出现
	order  []string        // 首次出现顺序（绑定顺序与编号稳定）
}

func newPrefixLetPlanner() *prefixLetPlanner {
	return &prefixLetPlanner{counts: map[string]int{}, lhs: map[string]bool{}}
}

// analyze 遍历语句序列，按左值/右值位置统计 $参数 引用。
//
// 判据刻意**不用** CollectPrefixParamRefs 的「首次出现=声明」：那套判据
// 会把 `$target = $target + $value` 的左值误判成 Default 声明。这里自建
// 遍历器（与 macroRewriter.rewriteExpr 的 RewriteExpressionChildren 递归
// 同构），赋值二元表达式的左子树按左值模式走（子树里的 $参数 记为左值
// 出现，不计数右值）；其余槽位一律右值计数。
func (p *prefixLetPlanner) analyze(stmts []ast.Statement) {
	var visitStmt func(s ast.Statement)
	var visitExpr func(e ast.Expression, lhsMode bool)
	visitExpr = func(e ast.Expression, lhsMode bool) {
		if e == nil {
			return
		}
		if id, ok := e.(*ast.Identifier); ok {
			if id.IsPrefixVar {
				if lhsMode {
					p.lhs[id.Name] = true
				} else if _, seen := p.counts[id.Name]; !seen {
					p.order = append(p.order, id.Name)
					p.counts[id.Name] = 1
				} else {
					p.counts[id.Name]++
				}
			}
			return
		}
		if be, ok := e.(*ast.BinaryExpression); ok && prefixAssignOps[be.Operator] {
			visitExpr(be.Left, true)
			visitExpr(be.Right, false)
			return
		}
		astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				visitExpr(v, lhsMode)
			case ast.Statement:
				visitStmt(v)
			}
			return nil
		})
	}
	visitStmt = func(s ast.Statement) {
		if s == nil {
			return
		}
		astutil.RewriteStatementChildren(s, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				visitExpr(v, false)
			case ast.Statement:
				visitStmt(v)
			}
			return nil
		})
	}
	for _, s := range stmts {
		visitStmt(s)
	}
}

// planPrefixLetBindings 决定哪些 $参数 需要「实参只求值一次」绑定。
//
// 触发条件（缺一不可）：
//   - 模板骨架里右值表达式引用 ≥2 次；
//   - 从未作为赋值左值出现（左值参数绑定会把赋值目标改到临时变量上）；
//   - 绑定载荷可绑定：语句级实参、宏调用实参、字面量、无返回值的调用
//     一律跳过（前者无值可绑，字面量无副作用无需绑定）。
//
// 返回 map：$参数名 → 临时变量名 __kaula_b<seq>_<参数>。
func (sg *StatementGenerator) planPrefixLetBindings(renamed []ast.Statement, paramTable map[string]ast.Expression) map[string]string {
	planner := newPrefixLetPlanner()
	planner.analyze(renamed)
	out := map[string]string{}
	for _, name := range planner.order {
		if planner.counts[name] < 2 || planner.lhs[name] {
			continue
		}
		if !sg.prefixLetBindable(paramTable[name]) {
			continue
		}
		sg.codegen.prefixLetSeq++
		out[name] = fmt.Sprintf("%s%d_%s", prefixLetTempPrefix, sg.codegen.prefixLetSeq, name)
	}
	return out
}

// prefixLetBindable 判断绑定载荷能否绑到 __auto_type 临时变量。
func (sg *StatementGenerator) prefixLetBindable(bound ast.Expression) bool {
	if bound == nil || isStatementLike(bound) {
		return false
	}
	switch bound.(type) {
	case *ast.PrefixCallExpression:
		// 宏调用实参：展开结果是语句序列/语句表达式，不绑
		return false
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.CharLiteral, *ast.BooleanLiteral, *ast.ArrayLiteral, *ast.ObjectLiteral:
		// 字面量无副作用：内联多少次都一样，无需绑定
		return false
	}
	if sg.prefixLetExprHasNoValue(bound) {
		// void 调用：`__auto_type x = println(...);` 是非法 C
		return false
	}
	return true
}

// prefixLetExprHasNoValue 判断绑定载荷是否确定不产生值（void 内建 /
// 用户 void 函数）。
func (sg *StatementGenerator) prefixLetExprHasNoValue(e ast.Expression) bool {
	call, ok := e.(*ast.CallExpression)
	if !ok || call == nil {
		return false
	}
	name := ""
	if id, ok := call.Function.(*ast.Identifier); ok {
		name = id.Name
	}
	if name == "" {
		return false
	}
	if macroVoidBuiltins[name] {
		return true
	}
	if prog := sg.codegen.getProgram(); prog != nil {
		if fn := prog.FindFunction(name); fn != nil {
			return voidFuncFor(fn)
		}
	}
	// 跨文件 import 的函数：返回类型表里查得到才下结论；查不到无法确认
	// 无值，保守按"有值"处理（不绑定也不出错，只是退化为内联）。
	if rt, ok := sg.codegen.localImportReturns[name]; ok {
		return rt == "" || isVoidType(rt)
	}
	return false
}

// bindPrefixParamRefs 把语句序列里 $param 的全部引用改写为临时变量名。
// 只对已确认「从未作为赋值左值」的参数生效——绑定计划保证了这类参数
// 不存在左值引用，全量替换即等价于只替换右值引用。
//
// 递归必须由 fn 自己完成（RewriteStatementChildren/RewriteExpressionChildren
// 只改写**直接子节点**）：遍历形态与 macroRewriter.rewriteStmt/rewriteExpr
// 同构，否则嵌套在调用实参、二元操作数等槽位里的引用会漏改。
func bindPrefixParamRefs(stmts []ast.Statement, param, temp string) []ast.Statement {
	var walkStmt func(s ast.Statement) ast.Statement
	var walkExpr func(e ast.Expression) ast.Expression
	walkExpr = func(e ast.Expression) ast.Expression {
		if e == nil {
			return nil
		}
		if id, ok := e.(*ast.Identifier); ok && id.IsPrefixVar && id.Name == param {
			return &ast.Identifier{Name: temp, Pos: id.Pos}
		}
		return astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				return walkExpr(v)
			case ast.Statement:
				return walkStmt(v)
			}
			return nil
		})
	}
	walkStmt = func(s ast.Statement) ast.Statement {
		if s == nil {
			return nil
		}
		return astutil.RewriteStatementChildren(s, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				return walkExpr(v)
			case ast.Statement:
				return walkStmt(v)
			}
			return nil
		})
	}
	out := make([]ast.Statement, 0, len(stmts))
	for _, s := range stmts {
		if s == nil {
			continue
		}
		out = append(out, walkStmt(s))
	}
	return out
}

/* ── 变参：$( ... ),* 匹配与重复展开（Task #3）── */

// containsVarArgRepeat 判断语句序列中是否存在变参重复占位
// （PrefixRepeatStmt / PrefixRepeatExpr）。存在意味着模板消费了变参，
// 位置实参需要绑定；不存在则位置实参不求值、不绑定。
func (sg *StatementGenerator) containsVarArgRepeat(stmts []ast.Statement) bool {
	found := false
	var walkStmt func(s ast.Statement)
	var walkExpr func(e ast.Expression)
	walkExpr = func(e ast.Expression) {
		if found || e == nil {
			return
		}
		if _, ok := e.(*ast.PrefixRepeatExpr); ok {
			found = true
			return
		}
		astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				walkExpr(v)
			case ast.Statement:
				walkStmt(v)
			}
			return nil
		})
	}
	walkStmt = func(s ast.Statement) {
		if found || s == nil {
			return
		}
		if _, ok := s.(*ast.PrefixRepeatStmt); ok {
			found = true
			return
		}
		astutil.RewriteStatementChildren(s, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				walkExpr(v)
			case ast.Statement:
				walkStmt(v)
			}
			return nil
		})
	}
	for _, s := range stmts {
		walkStmt(s)
		if found {
			break
		}
	}
	return found
}

// hasPrefixVarRef 判断语句序列中是否残留裸 `$param` 引用
// （重复展开后变参引用应已全部替换为临时变量；残留说明用错了位置）
func hasPrefixVarRef(stmts []ast.Statement, param string) bool {
	found := false
	var walkStmt func(s ast.Statement)
	var walkExpr func(e ast.Expression)
	walkExpr = func(e ast.Expression) {
		if found || e == nil {
			return
		}
		if id, ok := e.(*ast.Identifier); ok && id.IsPrefixVar && id.Name == param {
			found = true
			return
		}
		astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				walkExpr(v)
			case ast.Statement:
				walkStmt(v)
			}
			return nil
		})
	}
	walkStmt = func(s ast.Statement) {
		if found || s == nil {
			return
		}
		astutil.RewriteStatementChildren(s, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				walkExpr(v)
			case ast.Statement:
				walkStmt(v)
			}
			return nil
		})
	}
	for _, s := range stmts {
		walkStmt(s)
		if found {
			break
		}
	}
	return found
}

// expandVarArgRepeatsInPlace 展开模板（已 alpha 重命名）中的变参重复占位：
//   - PrefixRepeatStmt（语句级）→ 按位置实参个数重复片段，$param 依次绑定；
//   - PrefixRepeatExpr（表达式级）→ 仅允许出现在调用实参列表、数组元素或
//     前缀调用位置实参中，按实参个数重复片段、逗号连接；
//   - 出现在其他位置的表达式重复占位、重复片段内的嵌套重复 → 报错。
//
// temps 为空表示零个位置实参：所有重复占位展开为空。
func (sg *StatementGenerator) expandVarArgRepeatsInPlace(stmts *[]ast.Statement, param string, temps []ast.Expression) []string {
	var errs []string
	*stmts = sg.expandVarArgStmtList(*stmts, param, temps, &errs)
	return errs
}

// expandVarArgStmtList 处理语句列表：语句级重复占位在此拼接（可返回多条语句）
func (sg *StatementGenerator) expandVarArgStmtList(stmts []ast.Statement, param string, temps []ast.Expression, errs *[]string) []ast.Statement {
	var out []ast.Statement
	for _, s := range stmts {
		if s == nil {
			continue
		}
		if rep, ok := s.(*ast.PrefixRepeatStmt); ok {
			if len(temps) == 0 {
				continue // 零个位置实参：重复占位展开为空
			}
			for _, temp := range temps {
				frag := astutil.CloneStatements(rep.Body)
				frag = sg.replaceVarArgRefsStmts(frag, param, temp)
				if sg.containsVarArgRepeat(frag) {
					*errs = append(*errs, "嵌套的 $( ... ),* 重复占位不支持：把内层重复移到外层或用普通循环")
					continue
				}
				out = append(out, frag...)
			}
			continue
		}
		out = append(out, sg.expandVarArgNestedStmt(s, param, temps, errs))
	}
	return out
}

// expandVarArgNestedStmt 处理非重复语句：表达式与嵌套语句下钻。
// 与 astutil.RewriteStatementChildren 同构，但嵌套语句列表交给
// expandVarArgStmtList（语句级重复可在任意嵌套层拼接）。
func (sg *StatementGenerator) expandVarArgNestedStmt(s ast.Statement, param string, temps []ast.Expression, errs *[]string) ast.Statement {
	if s == nil {
		return nil
	}
	ex := func(x ast.Expression) ast.Expression {
		if x == nil {
			return nil
		}
		return sg.expandVarArgExpr(x, param, temps, errs)
	}
	sub := func(in []ast.Statement) []ast.Statement {
		return sg.expandVarArgStmtList(in, param, temps, errs)
	}
	switch n := s.(type) {
	case *ast.ExpressionStatement:
		c := *n
		c.Expression = ex(n.Expression)
		return &c
	case *ast.VariableDeclaration:
		var copyDecl func(d *ast.VariableDeclaration) *ast.VariableDeclaration
		copyDecl = func(d *ast.VariableDeclaration) *ast.VariableDeclaration {
			nd := *d
			nd.Value = ex(d.Value)
			nd.More = nil
			for _, m := range d.More {
				if m == nil {
					continue
				}
				nd.More = append(nd.More, copyDecl(m))
			}
			return &nd
		}
		return copyDecl(n)
	case *ast.ReturnStatement:
		c := *n
		c.Value = ex(n.Value)
		return &c
	case *ast.YieldStatement:
		c := *n
		c.Source = ex(n.Source)
		return &c
	case *ast.ReleaseStatement:
		c := *n
		c.Source = ex(n.Source)
		return &c
	case *ast.ExtractStatement:
		c := *n
		c.Source = ex(n.Source)
		c.Index = ex(n.Index)
		return &c
	case *ast.IfStatement:
		c := *n
		c.Condition = ex(n.Condition)
		c.Body = sub(n.Body)
		c.Else = sub(n.Else)
		return &c
	case *ast.WhileStatement:
		c := *n
		c.Condition = ex(n.Condition)
		c.Body = sub(n.Body)
		return &c
	case *ast.ForStatement:
		c := *n
		if n.Init != nil {
			c.Init = sg.expandVarArgNestedStmt(n.Init, param, temps, errs)
		}
		c.Condition = ex(n.Condition)
		if n.Update != nil {
			c.Update = sg.expandVarArgNestedStmt(n.Update, param, temps, errs)
		}
		c.Body = sub(n.Body)
		return &c
	case *ast.ForInStatement:
		c := *n
		c.Iterable = ex(n.Iterable)
		c.Body = sub(n.Body)
		return &c
	case *ast.BlockStatement:
		c := *n
		c.Statements = sub(n.Statements)
		return &c
	case *ast.CallStatement:
		c := *n
		c.Target = ex(n.Target)
		c.Body = sub(n.Body)
		return &c
	case *ast.SpendStatement:
		c := *n
		c.Target = ex(n.Target)
		if n.Calls != nil {
			c.Calls = make([]*ast.CallClause, len(n.Calls))
			for i, cl := range n.Calls {
				if cl == nil {
					continue
				}
				nc := *cl
				nc.Index = ex(cl.Index)
				nc.Body = sub(cl.Body)
				c.Calls[i] = &nc
			}
		}
		return &c
	case *ast.NonLocalStatement:
		c := *n
		c.Value = ex(n.Value)
		return &c
	case *ast.ObjectStatement:
		c := *n
		c.Fields = sg.expandVarArgExprSlice(n.Fields, param, temps, errs)
		c.Value = ex(n.Value)
		return &c
	case *ast.SwitchStatement:
		c := *n
		c.Scrutinee = ex(n.Scrutinee)
		c.Cases = make([]*ast.SwitchCase, len(n.Cases))
		for i, cs := range n.Cases {
			if cs == nil {
				continue
			}
			nc := *cs
			nc.Values = sg.expandVarArgExprSlice(cs.Values, param, temps, errs)
			nc.Body = sub(cs.Body)
			c.Cases[i] = &nc
		}
		c.Default = sub(n.Default)
		return &c
	case *ast.PrefixStatement:
		c := *n
		c.Body = sub(n.Body)
		return &c
	case *ast.TreeStatement:
		c := *n
		c.Root = ex(n.Root)
		c.Body = sub(n.Body)
		return &c
	case *ast.FunctionStatement:
		c := *n
		c.Body = sub(n.Body)
		return &c
	case *ast.ClassStatement:
		c := *n
		c.Methods = nil
		for _, m := range n.Methods {
			if m == nil {
				continue
			}
			c.Methods = append(c.Methods, sg.expandVarArgNestedStmt(m, param, temps, errs).(*ast.MethodStatement))
		}
		c.Constructors = nil
		for _, ct := range n.Constructors {
			if ct == nil {
				continue
			}
			c.Constructors = append(c.Constructors, sg.expandVarArgNestedStmt(ct, param, temps, errs).(*ast.ConstructorStatement))
		}
		return &c
	case *ast.MethodStatement:
		c := *n
		c.Body = sub(n.Body)
		return &c
	case *ast.ConstructorStatement:
		c := *n
		c.Body = sub(n.Body)
		return &c
	case *ast.PrefixVarArgDecl:
		// 匹配声明在骨架阶段已剔除，不应残留
		return s
	default:
		return s
	}
}

// expandVarArgExprSlice 处理表达式列表：表达式重复占位在此拼接（可返回多条表达式）
func (sg *StatementGenerator) expandVarArgExprSlice(exprs []ast.Expression, param string, temps []ast.Expression, errs *[]string) []ast.Expression {
	var out []ast.Expression
	for _, e := range exprs {
		if e == nil {
			continue
		}
		if rep, ok := e.(*ast.PrefixRepeatExpr); ok {
			if len(temps) == 0 {
				continue // 零个位置实参：展开为空
			}
			for _, temp := range temps {
				frag := astutil.CloneExpression(rep.Fragment)
				frag = sg.replaceVarArgRefsExpr(frag, param, temp)
				if sg.containsVarArgRepeatExpr(frag) {
					*errs = append(*errs, "嵌套的 $( ... ),* 重复占位不支持：把内层重复移到外层或用普通循环")
					continue
				}
				out = append(out, frag)
			}
			continue
		}
		out = append(out, sg.expandVarArgExpr(e, param, temps, errs))
	}
	return out
}

// expandVarArgExpr 处理表达式：只下钻（重复占位只能在列表槽位拼接）
func (sg *StatementGenerator) expandVarArgExpr(e ast.Expression, param string, temps []ast.Expression, errs *[]string) ast.Expression {
	if e == nil {
		return nil
	}
	if _, ok := e.(*ast.PrefixRepeatExpr); ok {
		*errs = append(*errs, "表达式重复占位 $( ... ),* 只能出现在调用实参、数组元素或前缀位置实参中")
		return e
	}
	switch n := e.(type) {
	case *ast.CallExpression:
		c := *n
		c.Function = sg.expandVarArgExpr(n.Function, param, temps, errs)
		c.Args = sg.expandVarArgExprSlice(n.Args, param, temps, errs)
		return &c
	case *ast.SpawnExpression:
		c := *n
		c.Function = sg.expandVarArgExpr(n.Function, param, temps, errs)
		c.Args = sg.expandVarArgExprSlice(n.Args, param, temps, errs)
		return &c
	case *ast.ArrayLiteral:
		c := *n
		c.Elements = sg.expandVarArgExprSlice(n.Elements, param, temps, errs)
		return &c
	case *ast.PrefixCallExpression:
		c := *n
		c.Params = make(map[string]ast.Expression, len(n.Params))
		for k, v := range n.Params {
			c.Params[k] = sg.expandVarArgExpr(v, param, temps, errs)
		}
		c.VarArgs = sg.expandVarArgExprSlice(n.VarArgs, param, temps, errs)
		c.Body = sg.expandVarArgStmtList(n.Body, param, temps, errs)
		return &c
	default:
		return astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				return sg.expandVarArgExpr(v, param, temps, errs)
			case ast.Statement:
				return sg.expandVarArgNestedStmt(v, param, temps, errs)
			}
			return nil
		})
	}
}

// containsVarArgRepeatExpr 判断表达式内是否含嵌套的表达式重复占位
func (sg *StatementGenerator) containsVarArgRepeatExpr(e ast.Expression) bool {
	found := false
	var walk func(x ast.Expression)
	walk = func(x ast.Expression) {
		if found || x == nil {
			return
		}
		if _, ok := x.(*ast.PrefixRepeatExpr); ok {
			found = true
			return
		}
		astutil.RewriteExpressionChildren(x, func(node ast.Node) ast.Node {
			switch v := node.(type) {
			case ast.Expression:
				walk(v)
			}
			return nil
		})
	}
	walk(e)
	return found
}

// replaceVarArgRefsStmts 把片段内的 `$param` 引用全部替换为临时变量引用（1:1 替换）
func (sg *StatementGenerator) replaceVarArgRefsStmts(stmts []ast.Statement, param string, temp ast.Expression) []ast.Statement {
	out := make([]ast.Statement, 0, len(stmts))
	for _, s := range stmts {
		if s == nil {
			continue
		}
		out = append(out, sg.replaceVarArgRefsStmt(s, param, temp))
	}
	return out
}

func (sg *StatementGenerator) replaceVarArgRefsStmt(s ast.Statement, param string, temp ast.Expression) ast.Statement {
	if s == nil {
		return nil
	}
	return astutil.RewriteStatementChildren(s, func(node ast.Node) ast.Node {
		switch v := node.(type) {
		case ast.Expression:
			return sg.replaceVarArgRefsExpr(v, param, temp)
		case ast.Statement:
			return sg.replaceVarArgRefsStmt(v, param, temp)
		}
		return nil
	})
}

func (sg *StatementGenerator) replaceVarArgRefsExpr(e ast.Expression, param string, temp ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	if id, ok := e.(*ast.Identifier); ok && id.IsPrefixVar && id.Name == param {
		ref := astutil.CloneExpression(temp)
		ref.SetPosition(id.Pos)
		return ref
	}
	return astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
		switch v := node.(type) {
		case ast.Expression:
			return sg.replaceVarArgRefsExpr(v, param, temp)
		case ast.Statement:
			return sg.replaceVarArgRefsStmt(v, param, temp)
		}
		return nil
	})
}

// macroValueExpression 取「这条语句作为宏返回值时的表达式」，并校验该形态
// 确实可以出现在 GNU 语句表达式末尾（也即 C 里能给出值）。
//
// 允许：
//   - 表达式语句，且表达式本身是**有值表达式**（不是 void 调用）；
//   - `return expr`（宏体内的 return 表示"宏的值"）；
//   - `yield expr`。
//
// 其余（声明、赋值、println、if/while/块…）一律报错：静默取一个无关的值
// 比报错危险得多。
func macroValueExpression(stmt ast.Statement) (ast.Expression, error) {
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		if s.Expression == nil {
			return nil, fmt.Errorf("最后一条语句是空表达式语句")
		}
		if exprHasNoValue(s.Expression) {
			return nil, fmt.Errorf("最后一个语句不是有值的表达式（%s 不产生值）；请把要返回的表达式写在模板体末尾", describeMacroExpr(s.Expression))
		}
		return s.Expression, nil
	case *ast.ReturnStatement:
		if s.Value == nil {
			return nil, fmt.Errorf("最后一条语句是 `return`（无值）")
		}
		return s.Value, nil
	case *ast.YieldStatement:
		if s.Source == nil {
			return nil, fmt.Errorf("最后一条语句是 `yield`（无源对象）")
		}
		return s.Source, nil
	default:
		return nil, fmt.Errorf("最后一个语句是 %s，它不产生值；请把要返回的表达式写在模板体末尾", describeMacroStmt(stmt))
	}
}

// exprHasNoValue 判断表达式是否**确定不产生值**。
//
// 只对能静态判定的一类下手：直接调用一个「无返回值」的已知 void 函数
// （println/print/puts 等内建输出，或返回类型为空的用户函数）。
// 其余表达式一律当作有值——宏的参数可能是任意类型，过度判定会误报。
func exprHasNoValue(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpression)
	if !ok || call == nil {
		return false
	}
	name := ""
	if id, ok := call.Function.(*ast.Identifier); ok {
		name = id.Name
	}
	if name == "" {
		return false
	}
	if macroVoidBuiltins[name] {
		return true
	}
	return false
}

// macroVoidBuiltins 无返回值的语言/标准库内建调用（名字层面即可判定）
var macroVoidBuiltins = map[string]bool{
	"println":  true,
	"print":    true,
	"printf":   true,
	"puts":     true,
	"eprintln": true,
	"eprint":   true,
	"panic":    true,
	"assert":   true,
}

// describeMacroExpr / describeMacroStmt 给出简短可读的节点描述（诊断用）
func describeMacroExpr(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.CallExpression:
		if id, ok := e.Function.(*ast.Identifier); ok {
			return "调用 " + id.Name + "(...)"
		}
		return "函数调用"
	case *ast.Identifier:
		return "标识符 " + e.Name
	}
	return fmt.Sprintf("%T", expr)
}

func describeMacroStmt(stmt ast.Statement) string {
	switch stmt.(type) {
	case *ast.VariableDeclaration:
		return "变量声明"
	case *ast.BlockStatement:
		return "块语句"
	case *ast.IfStatement:
		return "if 语句"
	case *ast.WhileStatement:
		return "while 语句"
	case *ast.ForStatement, *ast.ForInStatement:
		return "for 语句"
	case nil:
		return "空语句"
	}
	return fmt.Sprintf("%T", stmt)
}

// prefixBodyParamNames 收集前缀函数体"可以直接直译成 C 函数体"的替换表：
// 形参名 → 同名的 C 形参标识符。
//
// 返回 nil 表示这个函数体不该被直译。判据是"体内只用到了形参"：
// 一旦模板体自己声明了 `$p`（如 `$limit = 10`），这个 `$p` 在函数体里没有
// 任何 C 变量可以指代——它只存在于宏展开的世界里。此时生成一个空函数体，
// 而不是编造一个未声明的标识符（那会让整个程序在 clang 阶段失败，用户看到
// 的错误与真实原因毫无关系）。
//
// 空函数体是安全的：注解前缀函数只会在"当成普通函数调用"时被链接到，
// 而 `$参数` 形态的模板本来就不该被当成普通函数调用。
func (cg *CodeGenerator) prefixBodyParamNames(fn *ast.FunctionStatement) map[string]bool {
	if fn == nil {
		return nil
	}
	params := map[string]bool{}
	for _, name := range fn.Params {
		if name != "" {
			params[name] = true
		}
	}
	for name := range ast.DeclaredPrefixParams(fn.Body) {
		if !params[name] {
			return nil // 模板体自声明了 $参数：无法直译
		}
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

// rewritePrefixBodyStatement 把语句里的 `$参数` 改写成同名形参标识符。
//
// 只用于 generatePrefixFunction 生成的那个"兼容用 C 函数体"：宏语义在调用点
// 展开时处理，这里只需要保证代码能在 C 里编译通过、行为等价于直译。
func (cg *CodeGenerator) rewritePrefixBodyStatement(stmt ast.Statement, params map[string]bool) ast.Statement {
	if stmt == nil || len(params) == 0 {
		return stmt
	}
	renamed := astutil.RewriteStatementChildren(stmt, func(node ast.Node) ast.Node {
		switch v := node.(type) {
		case ast.Expression:
			return cg.rewritePrefixBodyExpression(v, params)
		case ast.Statement:
			return cg.rewritePrefixBodyStatement(v, params)
		}
		return nil
	})
	return renamed
}

func (cg *CodeGenerator) rewritePrefixBodyExpression(e ast.Expression, params map[string]bool) ast.Expression {
	if e == nil {
		return nil
	}
	if id, ok := e.(*ast.Identifier); ok && id.IsPrefixVar && params[id.Name] {
		return &ast.Identifier{Name: id.Name, Pos: id.Pos}
	}
	return astutil.RewriteExpressionChildren(e, func(node ast.Node) ast.Node {
		switch v := node.(type) {
		case ast.Expression:
			return cg.rewritePrefixBodyExpression(v, params)
		case ast.Statement:
			return cg.rewritePrefixBodyStatement(v, params)
		}
		return nil
	})
}

// macroMaxDepth 宏展开的嵌套深度上限。
//
// 展开是递归的：模板体里可以再调用另一个前缀，而被调用的前缀又可以反过来
// 调用外层的那个。合法的嵌套很少超过 3~4 层；给到 32 是为了不误伤"宏生成
// 宏"的写法，同时远小于 Go 栈的承受上限。
const macroMaxDepth = 32

// enterMacroExpansion 进入一层展开，超深或直接自引用时报错。
//
// 不做这道闸门的话，`prefix Loop { $v  @Loop(v = $v) }` 会在展开阶段无限
// 递归，用户看到的是 `fatal error: stack overflow` 加一段 Go runtime 转储，
// 既看不出是哪个宏，也不知道该怎么改。
func (sg *StatementGenerator) enterMacroExpansion(name string) error {
	if sg.codegen.macroDepth >= macroMaxDepth {
		return fmt.Errorf("宏展开层数超过上限 %d（展开链：%s）；前缀模板里是否出现了自我调用？",
			macroMaxDepth, strings.Join(append(append([]string{}, sg.codegen.macroExpansionStack...), name), " → "))
	}
	sg.codegen.macroDepth++
	sg.codegen.macroExpansionStack = append(sg.codegen.macroExpansionStack, name)
	return nil
}

func (sg *StatementGenerator) leaveMacroExpansion() {
	if sg.codegen.macroDepth > 0 {
		sg.codegen.macroDepth--
	}
	if n := len(sg.codegen.macroExpansionStack); n > 0 {
		sg.codegen.macroExpansionStack = sg.codegen.macroExpansionStack[:n-1]
	}
}
