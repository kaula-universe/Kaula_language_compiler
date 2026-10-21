package codegen

import (
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/core"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
)

// StatementGenerator 负责语句相关的代码生成
type StatementGenerator struct {
	codegen *CodeGenerator
}

// NewStatementGenerator 创建一个新的语句生成器
func NewStatementGenerator(cg *CodeGenerator) *StatementGenerator {
	return &StatementGenerator{
		codegen: cg,
	}
}

// GenerateStatement 生成语句代码
func (sg *StatementGenerator) GenerateStatement(stmt ast.Statement) string {
	if stmt == nil {
		return ""
	}
	// 首先尝试使用插件生成代码
	if code, ok := sg.codegen.pluginManager.GenerateStatement(stmt, sg.codegen); ok {
		return code
	}

	switch s := stmt.(type) {
	case *ast.SpendStatement:
		return sg.generateSpendStatement(s)
	case *ast.PrefixStatement:
		return sg.generatePrefixStatement(s)
	case *ast.TreeStatement:
		return sg.generateTreeStatement(s)
	case *ast.ObjectStatement:
		return sg.generateObjectStatement(s)
	case *ast.FunctionStatement:
		// 函数体内的嵌套函数必须提升为文件级定义（C 不允许函数内嵌定义），
		// 交由 CodeGenerator 统一排队（generateNestedFunctionStatement）
		return sg.codegen.generateNestedFunctionStatement(s)
	case *ast.ClassStatement:
		return sg.codegen.typeGenerator.GenerateClassStatement(s)
	case *ast.InterfaceStatement:
		return sg.codegen.typeGenerator.GenerateInterfaceStatement(s)
	case *ast.StructStatement:
		return sg.codegen.typeGenerator.GenerateStructStatement(s)
	case *ast.EnumStatement:
		return sg.codegen.typeGenerator.GenerateEnumStatement(s)
	case *ast.TypeAliasStatement:
		return sg.codegen.typeGenerator.GenerateTypeAliasStatement(s)
	case *ast.IfStatement:
		return sg.generateIfStatement(s)
	case *ast.SwitchStatement:
		// 跳转表语句：由 internal/opt 的「if 链转跳转表」优化产出
		return sg.generateSwitchStatement(s)
	case *ast.WhileStatement:
		return sg.generateWhileStatement(s)
	case *ast.ForInStatement:
		return sg.generateForInStatement(s)
	case *ast.ReturnStatement:
		return sg.generateReturnStatement(s)
	case *ast.BreakStatement:
		return sg.generateBreakStatement(s)
	case *ast.ContinueStatement:
		return sg.generateContinueStatement(s)
	case *ast.ImportStatement:
		return sg.generateImportStatement(s)
	case *ast.ExportStatement:
		return sg.generateExportStatement(s)
	case *ast.NonLocalStatement:
		return sg.generateNonLocalStatement(s)
	case *ast.VariableDeclaration:
		if s == nil {
			return ""
		}
		return sg.generateVariableDeclaration(s)
	case *ast.ExternStatement:
		if s == nil {
			return ""
		}
		return sg.generateExternStatement(s)
	case *ast.ExpressionStatement:
		if s == nil || s.Expression == nil {
			return ""
		}
		/*
		 * `hs[i] = spawn f(...)` / `obj.h = spawn f(...)` 也要登记结果类型。
		 *
		 * noteHandleResultType 原先只在**声明**处调用
		 * （`Handle x = spawn f()`），于是数组/字段形式的赋值登记不上，
		 * 后续 `hs[i].await` 推不出结果类型 → 退到解引用已 retire
		 * Frame 的老路上，安静返回垃圾值。
		 */
		sg.noteSpawnAssignedToTarget(s.Expression)
		// 检查是否是 PrefixCallExpression
		if prefixCall, ok := s.Expression.(*ast.PrefixCallExpression); ok {
			return sg.generatePrefixCallBody(prefixCall)
		}
		// void 尾调用改写：`f(...)` 语句形式（无返回值函数的递归自然写法）
		// → 回收本层 → 实参写回形参 → 跳回循环顶部
		if call, ok := s.Expression.(*ast.CallExpression); ok && sg.codegen.IsTcoTailSite(call) {
			if code := sg.generateVoidTcoTailCall(call); code != "" {
				return code
			}
		}
		// 安全地进行类型断言
		callExpr, isCall := interface{}(s.Expression).(*ast.CallExpression)
		if isCall && callExpr != nil && callExpr.Function != nil {
			if memberAccess, isMemberAccess := callExpr.Function.(*ast.MemberAccessExpression); isMemberAccess {
				// 跨线程逃逸：thread_create/thread_pool_add_task 的实参对象被
				// 另一线程异步使用，必须提升到幸存段并抑制提前回收（防 UAF）。
				if prefix, ok := sg.handleThreadSpawnEscape(callExpr); ok {
					return prefix + sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
				}
				// 模块限定调用（std.container.vector_push_back 等）：被调函数名
				// 在最内层 Member 上，同样要按"可能被长期持有"做实参提升——否则
				// 元素/对象存进全局容器后，调用方 scope_pop 一回卷就悬垂。
				if prefix := sg.emitCallArgPromotePrefix(memberAccess.Member, callExpr.Args); prefix != "" {
					return prefix + sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
				}
				// 这是模块函数调用，直接生成函数调用代码
				return sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
			}
			// 修复 #25：跨函数参数所有权提升（* 类型形参 = ModeOwned，消费调用方指针）
			if callee, ok := callExpr.Function.(*ast.Identifier); ok && callee.Name != "" {
				// SOR 模式传参 promote（精确，依赖 SOR 决策）
				promotes := sg.sorCallArgPromotes(callee.Name, callExpr.Args)
				// 基础层传参逃逸 promote（缺口3：extern/stdlib FFI 调用的指针实参，保守 promote）
				if len(promotes) == 0 {
					promotes = sg.structuralCallArgPromotes(callee.Name, callExpr.Args)
				}
				if prefix := sg.emitPromotePrefixFor(promotes, "promote arg into callee"); prefix != "" {
					return prefix + sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
				}
			}
		}
		// KMM 回卷悬垂修复：dst = src 标识符赋值的跨作用域逃逸检测。
		// 块级 KMM 作用域退出时 scope_pop/offset_restore 回卷 bump 指针，回收块内
		// 分配的对象；若块内通过普通赋值把对象引用传给外层局部变量或全局，
		// 会产生「声明还在、内容丢失」的悬垂。在赋值前把源对象提升到幸存段，
		// 并抑制源变量的块尾 per-object free。
		if assignCode := sg.generateAssignEscapeStatement(s); assignCode != "" {
			return assignCode
		}
		// 其他表达式语句
		return sg.codegen.expressionGenerator.GenerateExpression(s.Expression) + ";\n"
	case *ast.BlockStatement:
		return sg.generateBlockStatement(s)
	case *ast.YieldStatement:
		return sg.generateYieldStatement(s)
	case *ast.ReleaseStatement:
		return sg.generateReleaseStatement(s)
	case *ast.ExtractStatement:
		return sg.generateExtractStatement(s)
	default:
		return ""
	}
}

// initProvidesPointer 判断声明的初始值表达式本身是否已经提供指针对象。
//
// 用于消除"声明处先分配、再用初始值覆盖"的重复分配：
//   - 分配调用（kmm_v4_alloc / new / class 构造 / 返回指针的函数）→ true
//   - 指针别名（另一个指针变量）→ true
//   - 取址（&x）、成员访问、数组下标 → true（指针类型变量的这些形式产出指针）
//   - 整型/浮点/字符串/布尔字面量、二元表达式等 → false
//     （此时变量需要一个合法的初始指针，仍走分配路径）
//
// 未知形式一律返回 false：保守多分配一次也不会产生非法 C 代码。
func (sg *StatementGenerator) initProvidesPointer(expr ast.Expression) bool {
	switch e := expr.(type) {
	case nil:
		return false
	case *ast.CallExpression:
		return true
	case *ast.PrefixCallExpression:
		return true
	case *ast.TypeCastExpression:
		return sg.initProvidesPointer(e.Expression)
	case *ast.UnaryExpression:
		return sg.initProvidesPointer(e.Right)
	case *ast.ParenExpression:
		return sg.initProvidesPointer(e.Inner)
	case *ast.Identifier:
		return sg.isPointerTyped(e.Name) || sg.isStringTyped(e.Name)
	case *ast.MemberAccessExpression:
		return true
	case *ast.IndexExpression:
		return true
	default:
		return false
	}
}

/*
 * noteSpawnAssignedToTarget 处理 `target = spawn f(...)` 形态的结果类型登记。
 *
 * 声明形态（`Handle x = spawn f()`）由 noteHandleResultType 在声明处登记。
 * 赋值形态的目标不是新句柄，而是已存在的容器：
 *
 *   hs[i] = spawn f()  →  登记到 handleContainerResultTypes["hs"]
 *   obj.h = spawn f()  →  登记到 handleContainerResultTypes["h"]
 *   h = spawn f()      →  登记到 handleResultTypes["h"]（标量句柄）
 *
 * 容器**不能**登记进 handleResultTypes：isHandleVar 据那个表判断
 * "这个局部量本身是 Handle"并据此发 kaula_handle_release(name)，
 * 容器进去就会生成 kaula_handle_release(hs) —— hs 是数组，类型不符，
 * 编译直接失败。
 */
func (sg *StatementGenerator) noteSpawnAssignedToTarget(expr ast.Expression) {
	bin, ok := expr.(*ast.BinaryExpression)
	if !ok || bin.Operator != "=" {
		return
	}
	sp, ok := bin.Right.(*ast.SpawnExpression)
	if !ok {
		return
	}
	switch target := bin.Left.(type) {
	case *ast.IndexExpression:
		if id, ok := target.Object.(*ast.Identifier); ok {
			sg.codegen.noteHandleContainerResultType(id.Name, sp)
		}
	case *ast.MemberAccessExpression:
		if target.Member != "" {
			sg.codegen.noteHandleContainerResultType(target.Member, sp)
		}
	case *ast.Identifier:
		sg.codegen.noteHandleResultType(target.Name, sp)
	}
}

// generateVariableDeclaration 生成变量声明代码
func (sg *StatementGenerator) generateVariableDeclaration(stmt *ast.VariableDeclaration) string {
	if stmt.IsAuto {
		return sg.generateAutoDeclaration(stmt)
	}

	// 记录数组字面量长度，供 spend 强制消费流使用
	if arrLit, ok := stmt.Value.(*ast.ArrayLiteral); ok {
		sg.codegen.arrayLens[stmt.Name] = len(arrLit.Elements)
	}

	// const 变量：纯编译期常量，不参与运行时内存分配
	// 注：export 导出的常量需要生成真实的 C 常量定义（跨文件/外部链接可见）
	if stmt.IsConst && !stmt.IsExported {
		if evaluated := sg.codegen.tryEvalConstExpr(stmt.Value); evaluated != "" {
			sg.codegen.constTable[stmt.Name] = evaluated
			return ""
		}
		if stmt.Type == "" {
			// 无显式类型的 const name = expr：无法求值则表达式注册到常量表
			sg.codegen.constTable[stmt.Name] = sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
			return ""
		}
		// 有显式类型（C 风格 const type name = 运行时值，如 const char* s = fn()）：
		// 无法编译期求值，按普通变量生成并注册符号
	}

	sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local", stmt.Pos.Line, stmt.Pos.Column)

	// 线程逃逸修复：同名变量重新声明 = 新实例（循环体内每迭代声明一次），
	// 重置生命周期标志，避免上一实例的 promoted/assign-escaped 误跳过本实例
	// 的 thread_create 实参提升。
	sg.codegen.ResetLifetimeFlags(stmt.Name)

	// KMM 回卷悬垂修复：所有模式下登记 malloc 类初始化的常量分配大小，
	// 供赋值逃逸 promote 的大小解析使用（SOR 模式另有生成文本匹配路径）
	if stmt.Value != nil {
		sg.codegen.RecordAllocSizeFromAST(stmt.Name, stmt.Value)
		// 多分配器 origin 追踪：识别变量初始化的分配函数（kmm/libc/extern/unknown）
		sg.codegen.RecordVarOriginFromAST(stmt.Name, stmt.Value)
	}

	cType := sg.codegen.typeGenerator.convertType(stmt.Type, stmt.Nullable)
	if os.Getenv("KAULA_DEBUG_VARDECL") != "" {
		fmt.Printf("[DEBUG-VARDECL] name=%s type=%q nullable=%v cType=%q classTypes[Tensor]=%v\n",
			stmt.Name, stmt.Type, stmt.Nullable, cType, sg.codegen.typeGenerator.classTypes[stmt.Type])
	}

	/*
	 * 声明 Handle（含数组）就必须引入并发内核头。
	 *
	 * KaulaHandle 由 concurrency/kaula_conc.h 定义，而 codeUsesConcurrency
	 * 原本只在**用到** spawn / await / 可插桩调用时才置位。于是
	 * 「只声明 Handle 数组、本 TU 不 spawn」的生成物里 KaulaHandle
	 * 是个未声明标识符，clang 报 use of undeclared identifier ——
	 * 报错误导用户去查类型拼写，而真实原因是类型定义没被引入。
	 */
	if stmt.Type == "Handle" || strings.HasSuffix(strings.TrimSpace(stmt.Type), "]Handle") {
		sg.codegen.expressionGenerator.noteConcurrencyUse()
	}

	// class 类型变量: 引用语义, C 类型为指针 (K_A*)
	if sg.codegen.typeGenerator != nil && sg.codegen.typeGenerator.classTypes[stmt.Type] && !strings.HasSuffix(cType, "*") {
		cType += "*"
	}

	/*
	 * `Handle h = spawn f(...)`
	 *
	 * 登记 h 的结果 C 类型：表达式位置的 await（`h.await + ...`）
	 * 只能靠这张表才知道该解引用成什么类型。
	 */
	if sp, ok := stmt.Value.(*ast.SpawnExpression); ok {
		sg.codegen.noteHandleResultType(stmt.Name, sp)
	}

	/*
	 * `Handle y = x` 复制路径：y 没有 spawn 表达式可提取结果类型
	 * （复制的是句柄本身，不是新任务），从登记表传播源句柄 x 的结果类型。
	 *
	 * 不传播的话 y 不被识别为 Handle：
	 *   1) 作用域出口漏发 kaula_handle_release —— pin 晚还，内存峰值拖长；
	 *   2) 表达式位置 y.await 走 generateAwaitExpression 的错误分支，
	 *      生成 `(i64)kaula_handle_await(y)` —— 解引用已 retire 的 Frame
	 *      字段，是悬垂读：不崩，但安静地返回旧值。
	 * 链式复制（y=x, z=y）按语句顺序生成，天然逐级传播。
	 */
	if stmt.Type == "Handle" || stmt.Type == "handle" {
		if id, ok := stmt.Value.(*ast.Identifier); ok {
			if srcType := sg.codegen.HandleResultType(id.Name); srcType != "" {
				sg.codegen.RegisterHandleResultType(stmt.Name, srcType)
			}
		}
	}

	if aw, ok := stmt.Value.(*ast.AwaitExpression); ok && stmt.Name != "" {
		sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "local",
			stmt.Pos.Line, stmt.Pos.Column)
		handleCode := sg.codegen.expressionGenerator.GenerateExpression(aw.Handle)
		sg.codegen.expressionGenerator.noteConcurrencyUse()
		/*
		 * 走 await_into：结果直接写进这个变量。
		 *
		 * 不用 `*(T*)kaula_handle_await(h)` —— 那样是解引用一个
		 * 「Frame 里字段的地址」，而 Frame 在任务完成时就随任务 retire。
		 * 解引用发生在句柄 pin 释放之后，是个随时悬垂的读；
		 * 更糟的是它常常不崩，只是安静地返回上一次任务的旧值。
		 *
		 * 内核不知道 Kaula 的类型，所以大小由这里给 —— 调用点知道。
		 */
		return fmt.Sprintf("%s %s;\nkaula_handle_await_into(%s, &%s, sizeof(%s));\n",
			cType, stmt.Name, handleCode, stmt.Name, cType)
	}

	var builder strings.Builder
	builder.Grow(128)

	// 固定大小数组（cType 形如 "int32_t[10]"）不走 SOR 分配路由：
	// 1) 数组是栈/聚合声明，本来就不需要堆分配（SOR 侧最终也只是 stackVarDecl）；
	// 2) 数组字面量必须生成 **brace 初始化器**（C 不允许用复合字面量初始化
	//    固定数组），该处理在下面的基础层路径里，直接走 SOR 会生成
	//    "int arr[10] = ((int64_t[]){...})" → clang 报类型不匹配。
	isArray := strings.Contains(cType, "[") && strings.HasSuffix(cType, "]")

	/*
	 * 运行期长度的局部数组走**堆**，不是栈。
	 *
	 * `[n]T buf` 的 n 是运行期变量：编译期算不出字节数，而局部数组
	 * 默认是栈声明 → C 侧成了变长数组（VLA），占的是栈。栈预算不随
	 * n 增长，于是这是一条**编译期无法拦截**的越界。
	 *
	 * 实测（test/concurrency/test_vla_stack_probe.kl，Handle 16 字节）：
	 *
	 *     [100000]Handle hs + 只 spawn 不 await      通过
	 *     [100000]Handle hs + for..in + await        0xC00000FD
	 *     [100000]Handle hs + while  + await        0xC00000FD
	 *
	 * 同样的数组声明，一个通过一个崩，差别只是调用链深了几帧。
	 * 「这段代码会不会崩」取决于无关代码的改动 —— 最坏的一类缺陷。
	 *
	 * 定长（字面量长度）留在栈上（小数组没有分配开销），另由
	 * _Static_assert 兜住超预算的情形。
	 */
	if isArray && !stmt.IsStatic && !stmt.IsExported {
		if elem, count, ok := splitArrayCType(cType); ok && !isDecimalLiteral(count) {
			return sg.generateRuntimeSizedArrayAlloc(stmt, elem, count)
		}
	}

	/*
	 * 定长数组是**栈**分配，超大尺寸必须在编译期拦住。
	 *
	 * `[100000]Handle hs` → C 侧 `KaulaHandle hs[100000];`，Handle 16 字节
	 * → 1.6 MB。Windows 默认线程栈 1 MB，于是进 main 之前就
	 * STATUS_STACK_OVERFLOW (0xC00000FD / rc=3221225725)，全程零输出 ——
	 * 失败形态是"程序莫名崩了"，而不是"这行写错了"，极难定位。
	 *
	 * 用 _Static_assert 让 **clang** 判：字节数由 sizeof 算出，
	 * 不必在 Go 侧维护一张类型尺寸表，任何元素类型都覆盖。
	 */
	builder.WriteString(sg.stackArrayStaticAssert(stmt, cType, isArray))

	// KBUG_KMM_FREE_NOOP_SURVIVOR_LEAK：conc 守卫降级路径把声明拆成
	// 「声明 + 槽内赋值」，并以 Value=nil 复用本函数生成类型文本。
	// 那种情况下初始化式已单独生成，这里**不得**再发射缺省堆分配——
	// 否则该指针立刻被 `x = <恢复副本>/<槽内赋值>` 覆盖，分配结果永久丢失，
	// 每次 resume（= 每次请求）泄漏一个 survivor 槽位。
	// 必须置于 SOR 分支之前：SOR 的 GenerateSmartVarAlloc 同样会在
	// initValue="" 时发射 `x = kmm_v4_alloc_global(...)` 死分配。
	if sg.codegen.IsInitializerSupplied(stmt.Name) {
		return fmt.Sprintf("%s; /* structural: decl only (initializer generated separately) */\n",
			formatCVarDecl(cType, stmt.Name))
	}

	// SOR 智能分配路由
	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive && !isArray {
		if decision := adapter.GetVarDecision(stmt.Name); decision != nil {
			var initValue string
			if stmt.Value != nil {
				// 泛型 enum 构造（Result.Ok(42)）从声明类型取完整类型参数
				prevCtx := sg.codegen.pendingEnumCtxType
				sg.codegen.pendingEnumCtxType = stmt.Type
				initValue = sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
				sg.codegen.pendingEnumCtxType = prevCtx
				// 声明类型是 interface 而初始值是具体类对象 → 装箱成接口句柄。
				// SOR 路径绕过了基础层的 liftInterfaceValue 调用点（见下方 else 分支），
				// 不在这里装箱会生成 K_Shape sh = Square_new(...) → C 类型不匹配。
				initValue = sg.codegen.expressionGenerator.liftInterfaceValue(stmt.Type, initValue, stmt.Value)
			}
			// 修复 #25：登记编译期可知的堆分配常量大小（promote 回退用）
			sg.codegen.recordAllocSizeHint(stmt.Name, initValue)
			// KBUG_KMM_FREE_NOOP_SURVIVOR_LEAK：SOR 路由也必须登记分配来源。
			// 普通声明路径（下方 else 分支）早在 AddSymbol 后即调用
			// RecordVarOriginFromAST；本分支提前 return 会跳过它，导致
			// `u8* r = std.memory.kmm_v4_alloc(64)` 的 origin 恒为 unknown，
			// 进而使「用户显式 free」抑制分支误跳过 survivor 回收 → 槽位泄漏。
			sg.codegen.RecordVarOriginFromAST(stmt.Name, stmt.Value)
			// 初始值已是指针（分配调用/别名/取址/成员下标）→ 声明处不再重复分配
			return adapter.GenerateSmartVarAlloc(cType, stmt.Name, "", initValue,
				sg.initProvidesPointer(stmt.Value))
		}
	}

	// 基础层智能分配（从 SOR 下移的能力④+⑧）：
	// 指针类型变量用 kmm_v4_alloc_auto 分配（BumpPool），非指针用栈分配。
	// 数组类型（含指针数组）走栈声明，不做 alloc_auto。
	// 有初始值时直接用初始值初始化（尊重用户意图，避免先分配再覆盖导致泄漏）。
	isPointer := strings.HasSuffix(cType, "*")
	if isPointer && !isArray && !stmt.IsStatic && !stmt.IsExported {
		baseType := strings.TrimRight(cType, "*")
		var initValue string
		// 线程逃逸修复（声明路径）：Thread t = thread_create(worker, slot) 中
		// slot 被另一线程异步使用，实参提升到幸存段并抑制提前回收。
		var promotePrefix string
		if stmt.Value != nil {
			if callExpr, ok := stmt.Value.(*ast.CallExpression); ok {
				if prefix, okSpawn := sg.handleThreadSpawnEscape(callExpr); okSpawn {
					promotePrefix = prefix
				}
			}
			prevCtx := sg.codegen.pendingEnumCtxType
			sg.codegen.pendingEnumCtxType = stmt.Type

			/*
			 * `[]i32 arr = [1,3,1990]` 的类型是**指针** int32_t*，走本分支。
			 * 数组字面量的复合字面量元素类型必须与元素一致：不设置时恒生成
			 * `(int64_t[]){...}` → 绑到 int32_t* 造成指针步长错位，
			 * arr[1]/arr[2] 读到错误槽位，静默给错答案（1994 → 4）。
			 */
			prevElem := sg.codegen.pendingArrayElemType
			if kauraElem := inferElementType(stmt.Type); kauraElem != "" {
				sg.codegen.pendingArrayElemType = kauraElem
			} else {
				sg.codegen.pendingArrayElemType = strings.TrimSpace(baseType)
			}

			initValue = sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)

			sg.codegen.pendingEnumCtxType = prevCtx
			sg.codegen.pendingArrayElemType = prevElem
		}
		if initValue != "" {
			// 有初始值：直接用初始值初始化，不调用 alloc_auto
			sg.codegen.recordAllocSizeHint(stmt.Name, initValue)
			return promotePrefix + fmt.Sprintf("%s = %s;\n", formatCVarDecl(cType, stmt.Name), initValue)
		}
		// 无初始值：按逃逸分析结果选择分配路径
		//  - 逃逸级别 None（只在本作用域内使用）→ kmm_v4_alloc_auto 的 TLAB 快速路径，
		//    随作用域退出（scope_pop）整段回卷回收。
		//  - 逃逸（Arg/Return/CrossScope/Global/Heap）→ kmm_v4_alloc_global 的 survivor 段，
		//    scope_pop 不回卷，作用域末尾的 per-object kmm_v4_survivor_free 精确回收。
		//  - 逃逸分析未覆盖该变量 → 保持既有行为（alloc_auto）。
		sg.codegen.recordAllocSizeHint(stmt.Name, "sizeof("+baseType+")")
		allocFn := "kmm_v4_alloc_auto"
		allocTag := "structural: BumpPool alloc (TLAB fast path)"
		if scopeLocal, known := sg.codegen.IsVarScopeLocal(stmt.Name); known && !scopeLocal {
			allocFn = "kmm_v4_alloc_global"
			allocTag = "structural: BumpPool alloc (survivor, escaping scope)"
		}
		return fmt.Sprintf("%s = (%s)%s(sizeof(%s)); /* %s */\n",
			formatCVarDecl(cType, stmt.Name), cType, allocFn, baseType, allocTag)
	}

	// 生成属性前缀（#[volatile], #[section("...")], #[aligned(N)] 等）
	attrPrefix := generateVarAttributes(stmt.Attributes)

	builder.WriteString(attrPrefix)

	// export 修饰符：C 级导出声明
	if stmt.IsExported {
		builder.WriteString("KAULA_EXPORT ")
	}

	// static 存储修饰符
	if stmt.IsStatic {
		builder.WriteString("static ")
	}

	// const 常量修饰符
	if stmt.IsConst {
		builder.WriteString("const ")
		// 局部 const 也加入常量表，支持编译期常量求值
		if evaluated := sg.codegen.tryEvalConstExpr(stmt.Value); evaluated != "" {
			sg.codegen.constTable[stmt.Name] = evaluated
		}
	}

	builder.WriteString(sg.generateVarDeclString(cType, stmt.Name))

	if stmt.Value != nil {
		// 线程逃逸修复（声明路径，值类型变量如 Thread t = thread_create(...)）：
		// 实参对象被另一线程异步使用，提升到幸存段并抑制提前回收。
		var promotePrefix string
		if callExpr, ok := stmt.Value.(*ast.CallExpression); ok {
			if prefix, okSpawn := sg.handleThreadSpawnEscape(callExpr); okSpawn {
				promotePrefix = prefix
			}
		}
		// match 表达式作为变量初始化值：C 的 switch 不能作为表达式值，
		// 改为声明后接 switch 赋值
		if me, ok := stmt.Value.(*ast.MatchExpression); ok && cType != "auto" {
			var b strings.Builder
			b.WriteString(builder.String())
			b.WriteString(";\n")
			b.WriteString(sg.codegen.expressionGenerator.GenerateMatchAssign(me, stmt.Name))
			return b.String()
		}
		// 固定大小数组 + 数组字面量：C 要求 brace 初始化器
		// （复合字面量 ((T[]){...}) 不能初始化数组变量）。
		if arrLit, ok := stmt.Value.(*ast.ArrayLiteral); ok && isArray {
			builder.WriteString(" = {")
			for i, el := range arrLit.Elements {
				if i > 0 {
					builder.WriteString(", ")
				}
				builder.WriteString(sg.codegen.expressionGenerator.GenerateExpression(el))
			}
			builder.WriteString("}")
		} else if strLit, ok := stmt.Value.(*ast.StringLiteral); ok && isCharArrayDecl(cType) {
			// char s[40] = "abc";
			// GenerateExpression 对字符串字面量一律生成 String 结构体字面量
			// （那是 string 变量需要的形态），直接拿来初始化 char 数组会被 clang 拒：
			//     array initializer must be an initializer list or string literal
			// 定长 char 数组的初始化器必须是 C 字符串字面量。
			builder.WriteString(" = \"" + escapeCString(strLit.Value) + "\"")
		} else {
			builder.WriteString(" = ")
			// 泛型 enum 构造（Result.Ok(42)）：普通主路径（非 SOR/非指针）
			// 同样需要声明类型上下文，否则构造点只从字面量推断类型参数
			// （42→i32，E 缺失）→ 实例化名与声明类型不一致。
			prevCtx := sg.codegen.pendingEnumCtxType
			sg.codegen.pendingEnumCtxType = stmt.Type
			valueCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
			sg.codegen.pendingEnumCtxType = prevCtx
			// 声明类型是 interface 而初始值是具体类对象 → 装箱成接口句柄
			valueCode = sg.codegen.expressionGenerator.liftInterfaceValue(stmt.Type, valueCode, stmt.Value)
			builder.WriteString(valueCode)
		}
		return promotePrefix + builder.String() + ";\n"
	} else if stmt.Nullable {
		builder.WriteString(" = NULL")
	}
	builder.WriteString(";\n")
	return builder.String()
}

// isCharArrayDecl 判断一个 C 声明类型是不是「定长 char 数组」（char[N]）。
// 用于区分 char s[40] = "abc" 与 string s = "abc"：前者的初始化器必须是
// C 字符串字面量，后者是 String 结构体字面量。
func isCharArrayDecl(cType string) bool {
	t := strings.TrimSpace(cType)
	lb := strings.Index(t, "[")
	if lb < 0 || !strings.HasSuffix(t, "]") {
		return false
	}
	base := strings.TrimSpace(t[:lb])
	base = strings.TrimSpace(strings.TrimPrefix(base, "const "))
	switch base {
	case "char", "signed char", "unsigned char":
		return true
	}
	return false
}

/*
 * generateRuntimeSizedArrayAlloc 为「运行期长度」的局部数组生成堆分配。
 *
 * 生成物是一个元素指针，`buf[i]` 的写法完全不变 —— C 里数组与指针
 * 的下标语法同形，所以调用点无需改动。
 *
 * 恒用 kmm_v4_alloc_global（survivor），不用 kmm_v4_alloc_auto（TLAB bump）：
 *
 *   - TLAB 是**固定额度**的 bump 区，超出即行为未定义
 *   - survivor 段对大块有 malloc 兜底（见 kmm_scoped_allocator_v4.c 的
 *     paged 路径），而运行期长度的数组尺寸在编译期**无上界**
 *
 * 实测（test/concurrency/test_vla_stack_probe.kl，KaulaHandle 16 字节，
 * 池 2 MB）：TLAB 路径下 n=60000（960 KB）正确，n=100000（1.6 MB）
 * 起答案全错 —— 静默数据损坏，比崩更糟。
 */
func (sg *StatementGenerator) generateRuntimeSizedArrayAlloc(stmt *ast.VariableDeclaration, elem, count string) string {
	cg := sg.codegen

	// thread_create 的实参需要提升到 survivor 段（与指针变量同款处理）
	promotePrefix := ""
	if stmt.Value != nil {
		if callExpr, ok := stmt.Value.(*ast.CallExpression); ok {
			if prefix, okSpawn := sg.handleThreadSpawnEscape(callExpr); okSpawn {
				promotePrefix = prefix
			}
		}
	}

	/*
	 * 变量类型 = 元素类型 + 一层指针。
	 *
	 *   [n]i64    → 元素 int64_t     → 变量 int64_t*
	 *   [n]i64*   → 元素 int64_t*    → 变量 int64_t**
	 *
	 * C 里数组与指针的下标语法同形，所以 buf[i] 的写法不用改。
	 */
	elemType := strings.TrimSpace(elem)
	varType := elemType + "*"

	const allocFn = "kmm_v4_alloc_global"
	const allocTag = "structural: BumpPool alloc (survivor; runtime-sized array, unbounded)"

	cg.recordAllocSizeHint(stmt.Name, fmt.Sprintf("sizeof(%s) * (%s)", elemType, count))

	decl := fmt.Sprintf("%s %s = (%s)%s(sizeof(%s) * (%s)); /* %s */\n",
		varType, stmt.Name, varType, allocFn, elemType, count, allocTag)

	// 有初始值时用初始值覆盖（尊重用户意图），但仍需保留分配语句
	if stmt.Value != nil {
		prevCtx := cg.pendingEnumCtxType
		cg.pendingEnumCtxType = stmt.Type

		/*
		 * 数组字面量的复合字面量元素类型必须与 elem 一致。
		 *
		 * `[]i32 arr = [1,3,1990]`：元素是 i32，不设置上下文时字面量恒生成
		 * `(int64_t[]){...}` → 绑到 int32_t* 造成步长错位、静默错答案。
		 * 这里把**元素**Kaula 类型名传下去。
		 *
		 * 注：stmt.Type 是 Kaula 类型名。既有 `[]i32`（元素在前）也有
		 * `[3]i32`（元素在后）两种写法，两者都归约到元素类型。
		 */
		prevElem := cg.pendingArrayElemType
		if kauraElem := inferElementType(stmt.Type); kauraElem != "" {
			cg.pendingArrayElemType = kauraElem
		} else {
			// 兜底：用已解析出的 C 元素类型
			cg.pendingArrayElemType = elem
		}

		initValue := cg.expressionGenerator.GenerateExpression(stmt.Value)

		cg.pendingEnumCtxType = prevCtx
		cg.pendingArrayElemType = prevElem
		if initValue != "" {
			return promotePrefix + decl +
				fmt.Sprintf("%s = %s;\n", stmt.Name, initValue)
		}
	}
	return promotePrefix + decl
}

/*
 * stackArrayStaticAssert 为超预算的局部数组生成一条编译期断言。
 *
 * 返回 "" 表示不需要断言（非数组 / 静态 / 导出 / 长度非编译期常量）。
 *
 * 为什么用 _Static_assert 而不是 Go 侧查表报错：
 *   - 字节数由 C 的 sizeof 算出，不需要在 Go 侧维护类型尺寸表；
 *     Handle、String、自定义 struct 的 sizeof 都不会算错
 *   - 判据由 clang 给，措辞与实际 ABI 一致
 *   - 对 freestanding 目标同样有效（只需 C11）
 *
 * 长度必须是整数字面量：字节数要出现在常量表达式里。
 * `[n]T`（n 是运行期变量）算不出来就放过 —— 误报会拒掉正常代码。
 */
func (sg *StatementGenerator) stackArrayStaticAssert(stmt *ast.VariableDeclaration, cType string, isArray bool) string {
	if !isArray || stmt == nil || stmt.Name == "" {
		return ""
	}
	// static / exported 落在 .data/.bss，不是栈
	if stmt.IsStatic || stmt.IsExported {
		return ""
	}

	elem, count, ok := splitArrayCType(cType)
	if !ok {
		return ""
	}
	if !isDecimalLiteral(count) {
		return ""
	}

	n, okCount := new(big.Int).SetString(strings.ReplaceAll(count, "_", ""), 10)
	if okCount {
		if sz, known := cScalarSizeBytes(elem); known {
			total := new(big.Int).Mul(n, big.NewInt(int64(sz)))
			if total.IsInt64() && total.Int64() > kaulaStackArrayBudgetBytes {
				/*
				 * 尺寸算得出 → 直接给 kaulac 自己的诊断。
				 * 比让 clang 报错好：措辞可控，且能带上确切字节数。
				 */
				sg.codegen.error(fmt.Sprintf(
					"%s:%d: 局部数组 %s 占 %d 字节（%d 个 %s × %d），"+
						"超过栈预算 %d 字节。局部数组是栈分配，默认线程栈只有 1MB，"+
						"运行时会 STATUS_STACK_OVERFLOW 且没有任何输出。请改用堆分配。",
					sg.codegen.currentFunctionName, stmt.Pos.Line, stmt.Name,
					total.Int64(), n.Int64(), elem, sz,
					kaulaStackArrayBudgetBytes))
			}
		}
	}

	/*
	 * 无论 Go 侧算不算得出，都留一条 _Static_assert：
	 * 元素类型是 Handle / String / 自定义 struct 时 Go 侧没有尺寸表，
	 * 由 clang 用 sizeof 兜底，避免这类数组静默通过。
	 */
	msg := fmt.Sprintf(
		"局部数组 %s 超过栈预算 %d 字节：局部数组是栈分配，"+
			"而默认线程栈只有 1MB，运行时会 STATUS_STACK_OVERFLOW 且没有任何输出。"+
			"请改用堆分配",
		stmt.Name, kaulaStackArrayBudgetBytes)

	return fmt.Sprintf("_Static_assert(sizeof(%s) * (%s) <= %d, %q);\n",
		elem, count, kaulaStackArrayBudgetBytes, msg)
}

// cScalarSizeBytes 返回已知标量 C 类型的字节数。
//
// 只覆盖内建标量与指针 —— 这些尺寸由 C 标准固定，不需要看 kaula.h。
// struct / Handle / String 一律返回 false：那类尺寸取决于生成的
// typedef，由 clang 的 sizeof 判定更可靠。
func cScalarSizeBytes(cType string) (int, bool) {
	base := strings.TrimSpace(cType)
	// 指针数组的元素类型带 *
	if strings.HasSuffix(base, "*") {
		return 8, true
	}
	switch base {
	case "int8_t", "uint8_t", "signed char", "unsigned char", "char", "_Bool":
		return 1, true
	case "int16_t", "uint16_t", "short", "unsigned short":
		return 2, true
	case "int32_t", "uint32_t", "int", "unsigned int", "float":
		return 4, true
	case "int64_t", "uint64_t", "long long", "unsigned long long",
		"long", "unsigned long", "double":
		return 8, true
	}
	return 0, false
}

// kaulaStackArrayBudgetBytes 局部数组的栈预算。
//
// 取 256 KB：Windows 默认栈 1 MB，留足余量给递归与调用链；
// Linux 默认 8 MB 但受 RLIMIT_STACK 约束，线程里往往更小。
const kaulaStackArrayBudgetBytes = 256 * 1024

/*
 * splitArrayCType 把 C 数组类型拆成（元素类型, 长度表达式）。
 *
 * 支持 C 数组的两种拼法（Kaula 的 `[N]T` 会落到这两种）：
 *   T[N]   元素在前，长度在后 —— 如 "int64_t[128]"
 *   *[N]   指针数组           —— 如 "int64_t*[2]"
 *
 * 长度可以是字面量，也可以是运行期表达式（"int64_t[n]"）。
 * 调用方据 isDecimalLiteral 区分二者：字面量走栈 + _Static_assert 兜预算，
 * 运行期表达式走堆（栈预算不随 n 增长）。
 */
func splitArrayCType(cType string) (elem, count string, ok bool) {
	t := strings.TrimSpace(cType)
	if !strings.HasSuffix(t, "]") {
		return "", "", false
	}
	lb := strings.IndexByte(t, '[')
	if lb <= 0 {
		return "", "", false
	}
	count = strings.TrimSpace(t[lb+1 : len(t)-1])
	if count == "" {
		return "", "", false
	}
	elem = strings.TrimSpace(t[:lb])
	if elem == "" {
		return "", "", false
	}
	return elem, count, true
}

// isDecimalLiteral 判断是否纯十进制整数字面量（允许下划线分隔）。
//
// 刻意排除表达式：sizeof(T) * (2*3) 之类的虽然 clang 也算得出，
// 但那不是"用户写的长度"，按变量长度处理更稳妥。
func isDecimalLiteral(s string) bool {
	if s == "" {
		return false
	}
	digits := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '_':
			// 允许 1_000_000，但不能是首尾或连续
			if i == 0 || i == len(s)-1 || s[i-1] == '_' {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0
}

// formatCVarDecl 生成 C 变量声明字符串，处理固定大小数组类型和函数指针类型// 例如 [128]u8 → C 类型 "uint8_t[128]" → 声明 "uint8_t dst[128]"
// 指针数组 [2]int* → C 类型 "int64_t*[2]" → 声明 "int64_t* dst[2]"
// 函数指针 void(int,int)int → C 类型 "int64_t (*)(int64_t,int64_t)" → 声明 "int64_t (*f)(int64_t,int64_t)"
// 此函数为包级函数，供 SOR 和非 SOR 路径共用，确保数组后缀与函数指针声明符搬运一致。
func formatCVarDecl(cType, varName string) string {
	// 函数指针类型：R (*)(args) → R (*varName)(args)
	if idx := strings.Index(cType, "(*)"); idx >= 0 {
		return cType[:idx] + "(*" + varName + ")" + cType[idx+3:]
	}
	openBracket := strings.Index(cType, "[")
	if openBracket > 0 && strings.HasSuffix(cType, "]") {
		baseType := cType[:openBracket]
		arrayPart := cType[openBracket:]
		return baseType + " " + varName + arrayPart
	}
	return cType + " " + varName
}

// generateVarDeclString 生成 C 变量声明字符串（StatementGenerator 方法包装）
func (sg *StatementGenerator) generateVarDeclString(cType, varName string) string {
	return formatCVarDecl(cType, varName)
}

// generateAutoDeclaration 生成 auto 声明代码（类型推导）
func (sg *StatementGenerator) generateAutoDeclaration(stmt *ast.VariableDeclaration) string {
	if stmt.Type == "" {
		sg.codegen.error(fmt.Sprintf("编译错误：auto 变量 '%s' 类型推导失败，无法生成代码。请显式声明类型。", stmt.Name))
		return fmt.Sprintf("// ERROR: auto '%s' type not inferred - compilation should have stopped\n", stmt.Name)
	}

	// := 错误传播赋值：生成 if-else 传播逻辑
	if stmt.IsTryAssign {
		return sg.generateTryAssignDeclaration(stmt)
	}

	sg.codegen.AddSymbol(stmt.Name, stmt.Type, false, "local", stmt.Pos.Line, stmt.Pos.Column)
	sg.codegen.RecordDeclScopeDepth(stmt.Name)
	sg.codegen.RecordAllocSizeFromAST(stmt.Name, stmt.Value)

	// 记录数组字面量长度，供 spend 强制消费流使用
	if arrLit, ok := stmt.Value.(*ast.ArrayLiteral); ok {
		sg.codegen.arrayLens[stmt.Name] = len(arrLit.Elements)
	}

	cType := sg.codegen.typeGenerator.convertType(stmt.Type, false)
	sg.codegen.RecordDeclScopeDepth(stmt.Name)
	sg.codegen.RecordAllocSizeFromAST(stmt.Name, stmt.Value)

	var builder strings.Builder
	builder.Grow(64)

	builder.WriteString(sg.generateVarDeclString(cType, stmt.Name))

	if stmt.Value != nil {
		// match 表达式作为 auto 变量初始化值：C 的 switch 不能作为表达式值，
		// 改为声明后接 switch 赋值
		if me, ok := stmt.Value.(*ast.MatchExpression); ok && cType != "auto" {
			var b strings.Builder
			b.WriteString(builder.String())
			b.WriteString(";\n")
			b.WriteString(sg.codegen.expressionGenerator.GenerateMatchAssign(me, stmt.Name))
			return b.String()
		}
		builder.WriteString(" = ")
		initCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
		// 修复 #25：登记编译期可知的堆分配常量大小（promote 回退用）
		sg.codegen.recordAllocSizeHint(stmt.Name, initCode)
		builder.WriteString(initCode)
	}
	builder.WriteString(";\n")
	return builder.String()
}

// generateTryAssignDeclaration 生成 := 错误传播赋值
// 生成的 C 代码：
//
//	ResultType __kaula_try_N = <expr>;
//	if (__kaula_try_N.kind == 1) { return __kaula_try_N; }
//	T name = __kaula_try_N.data.Ok_val;
func (sg *StatementGenerator) generateTryAssignDeclaration(stmt *ast.VariableDeclaration) string {
	// 生成唯一临时变量名
	tryVar := fmt.Sprintf("__kaula_try_%d", sg.codegen.tryAssignCounter)
	sg.codegen.tryAssignCounter++

	// 推断右侧表达式的 Result enum 类型（sema 已填充）
	resultType := stmt.TryAssignResultType
	resultCType := sg.codegen.typeGenerator.convertType(resultType, false)
	if resultCType == "" {
		resultCType = "auto"
	}

	cType := sg.codegen.typeGenerator.convertType(stmt.Type, false)
	sg.codegen.AddSymbol(stmt.Name, stmt.Type, false, "local", stmt.Pos.Line, stmt.Pos.Column)

	var builder strings.Builder
	builder.Grow(256)

	// 临时变量保存 Result
	builder.WriteString(resultCType)
	builder.WriteByte(' ')
	builder.WriteString(tryVar)
	builder.WriteString(" = ")
	if stmt.Value != nil {
		builder.WriteString(sg.codegen.expressionGenerator.GenerateExpression(stmt.Value))
	}
	builder.WriteString(";\n")

	// if Err -> return
	builder.WriteString("if (")
	builder.WriteString(tryVar)
	builder.WriteString(".kind == 1) { return ")
	builder.WriteString(tryVar)
	builder.WriteString("; }\n")

	// 解包 Ok 赋值
	builder.WriteString(sg.generateVarDeclString(cType, stmt.Name))
	builder.WriteString(" = ")
	builder.WriteString(tryVar)
	builder.WriteString(".data.Ok_val;\n")

	return builder.String()
}

// generateExternStatement 生成 extern 外部符号/函数声明
// 变量: extern name: type → extern type name;
// 函数: extern fn name(params) -> ret → extern ret name(param_types);
func (sg *StatementGenerator) generateExternStatement(stmt *ast.ExternStatement) string {
	sg.codegen.AddSymbol(stmt.Name, stmt.Type, stmt.Nullable, "extern", stmt.Pos.Line, stmt.Pos.Column)

	var builder strings.Builder
	builder.Grow(256)

	if stmt.IsFunction {
		// extern 函数声明
		returnType := sg.codegen.typeGenerator.convertType(stmt.ReturnType, false)
		if returnType == "" {
			returnType = "void"
		}
		builder.WriteString("extern ")
		builder.WriteString(returnType)
		builder.WriteByte(' ')
		builder.WriteString(stmt.Name)
		builder.WriteByte('(')
		if len(stmt.ParamTypes) == 0 {
			builder.WriteString("void")
		} else {
			for i, pType := range stmt.ParamTypes {
				if i > 0 {
					builder.WriteString(", ")
				}
				cType := sg.codegen.typeGenerator.convertType(pType, false)
				if cType == "" {
					cType = "void*"
				}
				builder.WriteString(cType)
			}
		}
		builder.WriteString(");\n")
	} else {
		// extern 变量声明
		cType := sg.codegen.typeGenerator.convertType(stmt.Type, stmt.Nullable)
		builder.WriteString("extern ")
		builder.WriteString(cType)
		builder.WriteByte(' ')
		builder.WriteString(stmt.Name)
		builder.WriteString(";\n")
	}
	return builder.String()
}

// generateExpressionInMain 在 main 函数体中生成表达式语句
func (sg *StatementGenerator) generateExpressionInMain(expr ast.Expression) string {
	if callExpr, ok := expr.(*ast.CallExpression); ok {
		if ident, ok := callExpr.Function.(*ast.Identifier); ok && ident.Name == "println" {
			if len(callExpr.Args) > 1 {
				return sg.codegen.expressionGenerator.generatePrintlnMulti(callExpr.Args)
			}
		}
	}
	return sg.codegen.expressionGenerator.GenerateExpression(expr) + ";\n"
}

// threadSpawnFns 跨线程逃逸的标准库调用：实参对象被另一线程异步使用，
// 生命周期远超调用点。编译器活体分析会误判其在调用后死亡，提前回收
// （early free / 作用域回卷 / 函数出口 rewind），另一线程读取即 UAF。
// 修复：调用前把实参提升到幸存段（offset_restore/rewind 不回卷幸存段），
// 并登记为 assign-escaped 抑制全部后续回收路径。
var threadSpawnFns = map[string]bool{
	"thread_create":        true,
	"thread_pool_add_task": true,
}

// extractCallTailName 提取调用表达式目标链的最末端成员名：
// std.concurrent.thread_create -> "thread_create"；普通函数调用返回空。
func (sg *StatementGenerator) extractCallTailName(expr ast.Expression) string {
	ma, ok := expr.(*ast.MemberAccessExpression)
	if !ok {
		return ""
	}
	return ma.Member
}

// handleThreadSpawnEscape 处理线程启动调用的实参跨线程逃逸。
// 命中线程启动函数且存在指针/String 实参时：逐实参 promote + 登记
// assign-escaped，返回 promote 前缀（调用方自行拼接完整语句）与 true；
// 否则返回空串与 false。
func (sg *StatementGenerator) handleThreadSpawnEscape(callExpr *ast.CallExpression) (string, bool) {
	if callExpr == nil || callExpr.Function == nil {
		return "", false
	}
	fnName := sg.extractCallTailName(callExpr.Function)
	if !threadSpawnFns[fnName] {
		return "", false
	}
	var b strings.Builder
	promoted := false
	for _, arg := range callExpr.Args {
		ident, ok := arg.(*ast.Identifier)
		if !ok || ident.Name == "" {
			continue
		}
		if sg.codegen.IsAssignEscaped(ident.Name) || sg.codegen.IsPromoted(ident.Name) {
			continue
		}
		if !sg.isPointerTyped(ident.Name) && !sg.isStringTyped(ident.Name) {
			continue
		}
		sizeExpr := "sizeof(*" + ident.Name + ")"
		if size, ok := sg.sorPromoteSize(ident.Name, ident.Name); ok {
			sizeExpr = size
		}
		// promote 被跳过（拷贝大小不可信）时不能标记逃逸/已提升：
		// 那样会同时抑制回收又没有任何幸存副本。
		if line := sg.emitPromoteForType(ident.Name, sizeExpr, "promote thread arg into "+fnName, ident.Name); line != "" {
			b.WriteString(line)
			sg.codegen.MarkAssignEscaped(ident.Name) // 抑制 early free / 作用域回收 / return 回收
			sg.codegen.MarkPromoted(ident.Name)      // reaching definition：防止 return 时重复 promote
			promoted = true
		}
	}
	if !promoted {
		return "", false
	}
	sg.codegen.funcEmitsPromote = true // 方案 C：函数出口跳过 survivor_rewind
	return b.String(), true
}

// generateSpendStatement 生成 spend 强制消费流代码
// 数组模式：将目标包装为 Spendable → spend_lock → 逐子句 spend_call（标记消费并校验）
//
//	→ call(default) 时 spend_consume_all → spend_unlock（校验全消费）→ destroy
//
// 枚举模式：生成 switch 穷尽分发，default 分支为运行时安全网（编译期已证明不可达）
func (sg *StatementGenerator) generateSpendStatement(stmt *ast.SpendStatement) string {
	count, enumName, targetCode := sg.resolveSpendTarget(stmt.Target)
	if enumName != "" {
		return sg.generateSpendEnum(stmt, enumName, targetCode)
	}
	return sg.generateSpendArray(stmt, count, targetCode)
}

// resolveSpendTarget 解析 spend 目标
// 返回: (count, enumName, targetC)；enumName 非空表示枚举模式；count<=0 表示未知长度（带 default 兜底）
func (sg *StatementGenerator) resolveSpendTarget(target ast.Expression) (int, string, string) {
	switch t := target.(type) {
	case *ast.ArrayLiteral:
		return len(t.Elements), "", ""
	case *ast.Identifier:
		if sym := sg.codegen.GetSymbol(t.Name); sym != nil {
			if sg.codegen.IsEnumType(sym.Type) {
				return 0, sym.Type, t.Name
			}
		}
		if n, ok := sg.codegen.arrayLens[t.Name]; ok {
			return n, "", t.Name
		}
		return 0, "", ""
	default:
		return 0, "", ""
	}
}

// generateSpendArray 数组消费模式
func (sg *StatementGenerator) generateSpendArray(stmt *ast.SpendStatement, count int, targetCode string) string {
	sg.codegen.spendCounter++
	spdName := fmt.Sprintf("_spd_%d", sg.codegen.spendCounter)

	var builder strings.Builder
	builder.Grow(512)
	builder.WriteString("/* spend block: 强制消费流 */\n")

	// 数组字面量目标：提升为具名临时数组，避免子表达式重复求值
	arrTemp := ""
	if arrLit, ok := stmt.Target.(*ast.ArrayLiteral); ok {
		arrTemp = spdName + "_arr"
		elems := make([]string, len(arrLit.Elements))
		for i, elem := range arrLit.Elements {
			elems[i] = sg.codegen.expressionGenerator.GenerateExpression(elem)
		}
		builder.WriteString(fmt.Sprintf("int64_t* %s = ((int64_t[]){ %s });\n", arrTemp, strings.Join(elems, ", ")))
		targetCode = arrTemp
	}

	if targetCode == "" {
		// 未知长度：sema 已强制要求 call(default)；这里生成纯语义流（无元素可枚举）
		builder.WriteString("/* 未知长度目标：仅执行 default 兜底 */\n")
		for _, call := range stmt.Calls {
			if call.IsDefault {
				builder.WriteString("{\n")
				for _, bodyStmt := range call.Body {
					builder.WriteString(sg.codegen.generateStatement(bodyStmt))
				}
				builder.WriteString("}\n")
			}
		}
		builder.WriteString("/* end spend */\n")
		return builder.String()
	}

	// 包装元素指针到 Spendable
	builder.WriteString(fmt.Sprintf("Spendable* %s = spendable_create(%d);\n", spdName, count))
	for i := 0; i < count; i++ {
		builder.WriteString(fmt.Sprintf("spendable_add(%s, &(%s)[%d]);\n", spdName, targetCode, i))
	}
	builder.WriteString(fmt.Sprintf("spend_lock(%s);\n", spdName))

	// 逐子句消费
	for _, call := range stmt.Calls {
		if call.IsDefault {
			builder.WriteString(fmt.Sprintf("/* call(default): 消费全部剩余元素 */\n"))
			builder.WriteString(fmt.Sprintf("spend_consume_all(%s);\n", spdName))
		} else {
			index := 0
			if intLit, ok := call.Index.(*ast.IntegerLiteral); ok {
				index = int(intLit.Value)
			}
			builder.WriteString(fmt.Sprintf("/* call(%d) */\n", index))
			builder.WriteString(fmt.Sprintf("spend_call(%s, %d);\n", spdName, index))
		}
		builder.WriteString("{\n")
		for _, bodyStmt := range call.Body {
			builder.WriteString(sg.codegen.generateStatement(bodyStmt))
		}
		builder.WriteString("}\n")
	}

	builder.WriteString(fmt.Sprintf("spend_unlock(%s);\n", spdName))
	builder.WriteString(fmt.Sprintf("spendable_destroy(%s);\n", spdName))
	builder.WriteString("/* end spend */\n")
	return builder.String()
}

// generateSpendEnum 枚举穷尽消费模式：switch 分发 + 安全网
func (sg *StatementGenerator) generateSpendEnum(stmt *ast.SpendStatement, enumName, targetCode string) string {
	enumStmt := sg.codegen.program.FindEnum(enumName)
	if enumStmt == nil {
		return "/* spend: enum " + enumName + " not found */\n"
	}

	var builder strings.Builder
	builder.Grow(512)
	builder.WriteString("/* spend block: 枚举穷尽消费 */\n")
	builder.WriteString(fmt.Sprintf("switch ((int)(%s)) {\n", targetCode))

	variantLabel := map[string]string{}
	for _, v := range enumStmt.Variants {
		variantLabel[v.Name] = enumName + "_Kind_" + v.Name
	}

	for _, call := range stmt.Calls {
		if call.IsDefault || call.Index == nil {
			continue
		}
		var label string
		if id, ok := call.Index.(*ast.Identifier); ok {
			label = variantLabel[id.Name]
		}
		if label == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("case %s:\n", label))
		builder.WriteString("{\n")
		for _, bodyStmt := range call.Body {
			builder.WriteString(sg.codegen.generateStatement(bodyStmt))
		}
		builder.WriteString("}\n")
		builder.WriteString("break;\n")
	}

	// 编译期已穷尽证明；default 为运行时安全网
	builder.WriteString("default:\n")
	builder.WriteString("    /* unreachable: exhaustiveness proven at compile time */\n")
	builder.WriteString("    abort();\n")
	builder.WriteString("}\n")
	builder.WriteString("/* end spend */\n")
	return builder.String()
}

// generatePrefixStatement 生成 prefix 语句代码
// prefix 定义只注册前缀，不内联展开代码（由 @ 调用展开）
func (sg *StatementGenerator) generatePrefixStatement(stmt *ast.PrefixStatement) string {
	// 在 PrefixManager 中创建前缀上下文
	sg.codegen.prefixManager.CreatePrefix(stmt.Name, core.PrefixAnnotationPrefix)

	// 转义名称，防止 C 字符串注入
	safeName := escapeCString(stmt.Name)

	// 只生成注释标记，不展开代码（代码由 @ 调用时展开）
	code := fmt.Sprintf("/* prefix: %s (definition) */\n", safeName)
	return code
}

// findFunctionDeclaration 查找前缀函数的声明
func (sg *StatementGenerator) findFunctionDeclaration(name string) *ast.FunctionStatement {
	// 从当前程序中查找前缀函数定义
	// 这里需要访问 AST，可以通过 codegen 的 program 字段
	return sg.codegen.findFunctionByName(name)
}

// generateTreeStatement 生成 tree 语句代码
func (sg *StatementGenerator) generateTreeStatement(stmt *ast.TreeStatement) string {
	code := ""

	annotation := stmt.GetAnnotation()

	if annotation == ast.TreeAnnotationRoot || annotation == ast.TreeAnnotationRootTree {
		code += "// Root tree definition\n"
		code += sg.generateRootTreeDefinition(stmt)
	} else if annotation == ast.TreeAnnotationPrefix || annotation == ast.TreeAnnotationPrefixTree {
		code += "// Prefix tree definition\n"
		code += sg.generatePrefixTreeDefinition(stmt)
	} else {
		code += "// Tree structure implementation\n"
		code += sg.generateTreeImplementation(stmt)
	}

	return code
}

func (sg *StatementGenerator) generateRootTreeDefinition(stmt *ast.TreeStatement) string {
	code := "/* tree: root */\n"

	if stmt.Root != nil {
		code += "/* root: "
		code += sg.codegen.expressionGenerator.GenerateExpression(stmt.Root)
		code += " */\n"
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: root */\n"
	return code
}

func (sg *StatementGenerator) generatePrefixTreeDefinition(stmt *ast.TreeStatement) string {
	code := "/* tree: prefix */\n"

	if stmt.Root != nil {
		if ident, ok := stmt.Root.(*ast.Identifier); ok {
			code += fmt.Sprintf("/* prefix: %s */\n", ident.Name)
		}
	}

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: prefix */\n"
	return code
}

func (sg *StatementGenerator) generateValidatedTree(stmt *ast.TreeStatement, rootTree *core.Tree) string {
	code := "/* tree: validated */\n"

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: validated */\n"
	return code
}

func (sg *StatementGenerator) generateOrphanTreeError(stmt *ast.TreeStatement) string {
	code := fmt.Sprintf("// ERROR: Tree at line %d is orphan - no root tree defined\n", stmt.Pos.Line)
	code += "// Consider wrapping this tree in a prefix or class, or marking it with #[root,tree]\n"
	code += "// Example: #[prefix,tree] fn wrap() { ... tree code ... }\n"
	return code
}

func (sg *StatementGenerator) generateTreeImplementation(stmt *ast.TreeStatement) string {
	code := "/* tree: implementation */\n"

	for _, bodyStmt := range stmt.Body {
		code += sg.codegen.generateStatement(bodyStmt)
	}

	code += "/* end tree: implementation */\n"
	return code
}

// generateObjectStatement 生成 object 语句代码
func (sg *StatementGenerator) generateObjectStatement(stmt *ast.ObjectStatement) string {
	code := fmt.Sprintf("// Object: %s of type %s\n", stmt.Name, stmt.Type)
	sg.codegen.typeGenerator.structTypes[stmt.Name] = true
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for i := range stmt.Fields {
		code += fmt.Sprintf("    void* field%d;\n", i+1)
	}
	code += fmt.Sprintf("} %s;\n", stmt.Name)
	// 声明全局变量
	varName := stmt.Name + "_obj"
	code += fmt.Sprintf("%s* %s;\n", stmt.Name, varName)
	return code
}

// generateIfStatement 生成 if 语句代码
func (sg *StatementGenerator) generateIfStatement(stmt *ast.IfStatement) string {
	condCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Condition)

	/*
	 * 守卫模式：条件求值与分支体分开。
	 *
	 * 条件先在自己的槽位里算完并把结果写进 Frame 的 branch[d]，
	 * 分支体按 branch[d] 选。恢复时条件不会被重算 —— 重算就可能因为
	 * 变量已被更新而走到另一个分支，那正是「状态机丢失执行位置」。
	 */
	guardActive := sg.codegen.concGuardActive() &&
		(stmtsHaveSuspend(stmt.Body) || stmtsHaveSuspend(stmt.Else))
	guardDepth := -1
	seqAfterCond := 0
	restoreGuard := func() {}
	var code string
	if guardActive {
		restoreGuard = sg.codegen.concGuardEnter()
		guardDepth = sg.codegen.concGuardDepth
		code = sg.concGuardCondPrologue(condCode)
		seqAfterCond = sg.codegen.concGuardSeq
		code += sg.codegen.indentString() +
			fmt.Sprintf("if (%s->branch[%d]) {\n", sg.codegen.concFrameVar, guardDepth)
	} else {
		code = "if (" + condCode + ") {\n"
	}

	// 条件分支路径敏感（从 SOR 下移的能力⑤）：快照 promotedVars，
	// 生成 then 分支后记录新增 promote，恢复后生成 else 分支，最后取交集合并
	promotedSnapshot := sg.codegen.SnapshotPromotedVars()
	var thenPromoted map[string]bool

	// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
	sg.codegen.indent++
	sg.codegen.EnterScope("if_body")

	useKMM := sg.shouldUseKMMScopeForBody(stmt.Body)
	// 相邻 scope 合并优化：如果 body 包含 KMM 池分配调用，使用 offset_save/restore 代替 scope_push/pop
	useOffset := !useKMM && bodyContainsAllocation(stmt.Body, sg.codegen.IsFreestanding())

	if useKMM {
		sg.codegen.EnterKMMScope()
	}
	if useOffset {
		sg.codegen.EnterOffsetScope()
	}

	var bodyCode strings.Builder
	for _, bodyStmt := range stmt.Body {
		if guardActive {
			if concIsResumableCompound(bodyStmt) {
				bodyCode.WriteString(sg.concGuardHold(func() string {
					return sg.codegen.generateStatement(bodyStmt)
				}))
				continue
			}
			sg.codegen.concCheckLeafStmt(bodyStmt)
			bodyCode.WriteString(sg.concGuardSlot(func() string {
				return sg.codegen.generateStatement(bodyStmt)
			}))
			continue
		}
		bodyCode.WriteString(sg.codegen.indentString())
		bodyCode.WriteString(sg.codegen.generateStatement(bodyStmt))
	}
	// 方案 A：在作用域退出前发射 per-object survivor_free
	// 若末尾是 return，return 中已通过 generateSurvivorFreeForReturn 释放，此处为死代码，跳过
	if !lastStmtIsReturn(stmt.Body) {
		bodyCode.WriteString(sg.generateSurvivorFreeForScope())
	}

	if useOffset {
		sg.codegen.ExitOffsetScope()
	}
	if useKMM {
		sg.codegen.ExitKMMScope()
	}
	sg.codegen.ExitScope()
	sg.codegen.indent--

	// 记录 then 分支中新 promote 的变量（路径敏感分析）
	thenPromoted = sg.codegen.DiffPromotedVars(promotedSnapshot)

	if useOffset {
		code += sg.codegen.indentString() + "kmm_v4_mark_t _if_scope_start = kmm_v4_mark_save();\n"
		code += bodyCode.String()
		code += sg.codegen.indentString() + "kmm_v4_mark_restore(_if_scope_start);\n"
	} else if useKMM {
		// 缺口6优化：作用域级 survivor checkpoint/rewind
		// 块入口 checkpoint，块出口 rewind（如果块内没有 promote）
		// 提前回收块内大对象，减少内存峰值
		code += sg.codegen.indentString() + "kmm_survivor_checkpoint_t __if_surv_cp = kmm_v4_survivor_checkpoint();\n"
		code += sg.codegen.indentString() + sg.codegen.KMMStart("")
		code += bodyCode.String()
		code += sg.codegen.indentString() + sg.codegen.KMMEnd()
		// 只有块内没有新增 promote 时才 rewind（promote 的对象需要长期持有）
		if len(thenPromoted) == 0 {
			code += sg.codegen.indentString() + "kmm_v4_survivor_rewind(__if_surv_cp); /* survivor: scope reclaim */\n"
		}
	} else {
		code += bodyCode.String()
	}

	code += sg.codegen.indentString() + "}"

	if len(stmt.Else) > 0 {
		code += " else {\n"

		/*
		 * 两个分支**共用**同一段槽位号。
		 *
		 * 条件槽位已经把 sub[d] 推进到 seqAfterCond，而恢复时只会走其中一个
		 * 分支 —— 让 else 从 then 之后的号继续数的话，条件槽位留下的那个号
		 * 谁都不匹配，整条 else 分支永远不执行（症状是恒走 then 分支，
		 * 或者什么都不做）。
		 */
		if guardActive {
			sg.codegen.concGuardSeq = seqAfterCond
		}

		// 恢复 promotedVars 到快照状态，以便独立分析 else 分支
		sg.codegen.RestorePromotedVars(promotedSnapshot)

		sg.codegen.indent++
		sg.codegen.EnterScope("else_body")

		useKMMElse := sg.shouldUseKMMScopeForBody(stmt.Else)
		useOffsetElse := !useKMMElse && bodyContainsAllocation(stmt.Else, sg.codegen.IsFreestanding())

		if useKMMElse {
			sg.codegen.EnterKMMScope()
		}
		if useOffsetElse {
			sg.codegen.EnterOffsetScope()
		}

		var elseCode strings.Builder
		for _, elseStmt := range stmt.Else {
			if guardActive {
				if concIsResumableCompound(elseStmt) {
					elseCode.WriteString(sg.concGuardHold(func() string {
						return sg.codegen.generateStatement(elseStmt)
					}))
					continue
				}
				sg.codegen.concCheckLeafStmt(elseStmt)
				elseCode.WriteString(sg.concGuardSlot(func() string {
					return sg.codegen.generateStatement(elseStmt)
				}))
				continue
			}
			elseCode.WriteString(sg.codegen.indentString())
			elseCode.WriteString(sg.codegen.generateStatement(elseStmt))
		}
		// 方案 A：在作用域退出前发射 per-object survivor_free
		// 若末尾是 return，return 中已通过 generateSurvivorFreeForReturn 释放，此处为死代码，跳过
		if !lastStmtIsReturn(stmt.Else) {
			elseCode.WriteString(sg.generateSurvivorFreeForScope())
		}

		if useOffsetElse {
			sg.codegen.ExitOffsetScope()
		}
		if useKMMElse {
			sg.codegen.ExitKMMScope()
		}
		sg.codegen.ExitScope()
		sg.codegen.indent--

		// 路径敏感合并：只有两个分支都 promote 的变量才在合并点标记为已 promote
		elsePromoted := sg.codegen.DiffPromotedVars(promotedSnapshot)
		sg.codegen.MergePromotedFromBranches(thenPromoted, elsePromoted)

		if useOffsetElse {
			code += sg.codegen.indentString() + "kmm_v4_mark_t _else_scope_start = kmm_v4_mark_save();\n"
			code += elseCode.String()
			code += sg.codegen.indentString() + "kmm_v4_mark_restore(_else_scope_start);\n"
		} else if useKMMElse {
			// 缺口6优化：作用域级 survivor checkpoint/rewind
			code += sg.codegen.indentString() + "kmm_survivor_checkpoint_t __else_surv_cp = kmm_v4_survivor_checkpoint();\n"
			code += sg.codegen.indentString() + sg.codegen.KMMStart("")
			code += elseCode.String()
			code += sg.codegen.indentString() + sg.codegen.KMMEnd()
			if len(elsePromoted) == 0 {
				code += sg.codegen.indentString() + "kmm_v4_survivor_rewind(__else_surv_cp); /* survivor: scope reclaim */\n"
			}
		} else {
			code += elseCode.String()
		}

		code += sg.codegen.indentString() + "}"
	} else {
		// 无 else 分支：then 分支的 promote 不传播到合并点（保守，因为隐式 else 路径没 promote）
		sg.codegen.RestorePromotedVars(promotedSnapshot)
	}

	if guardActive {
		/*
		 * 分支体自带一层守卫深度，退出时还原回父层。
		 *
		 * 不需要额外推进 sub[d]：分支体不是循环，没有「下一轮」，
		 * 而 if 语句本身在父层已经占了一个槽位，父层负责往下推进。
		 * branch[d] 同理 —— 每次进入条件槽位都会被重写。
		 */
		restoreGuard()
	}

	code += "\n"
	return code
}

// generateWhileStatement 生成 while 语句代码
func (sg *StatementGenerator) generateWhileStatement(stmt *ast.WhileStatement) string {
	condCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Condition)

	/*
	 * 守卫模式：循环头重写成「无条件 for + 槽位 0 判条件」。
	 *
	 * 为什么不能继续用 while (cond)：sub[d]==0 的含义是「本轮从头开始」，
	 * 恢复时 sub[d] 是恢复槽位（非 0），while 会在条件为假时直接退出 ——
	 * 第一轮挂起，恢复后条件一变就再也走不进去。
	 *
	 * 每轮末尾把 sub[d] 写回 0，维持「进入任一层时读到的都是 0」这个不变量。
	 */
	var code string
	guardDepth := -1
	restoreGuard := func() {}
	if sg.codegen.concGuardActive() && stmtsHaveSuspend(stmt.Body) {
		restoreGuard = sg.codegen.concGuardEnter()
		guardDepth = sg.codegen.concGuardDepth
		code = "for (;;) {\n"
		code += sg.concGuardLoopHead(condCode)
	} else {
		code = "while (" + condCode + ") {\n"
	}

	// 作用域合并优化：先预判断，如果需要 KMM 则 EnterKMMScope 再生成 body
	sg.codegen.indent++
	sg.codegen.EnterScope("while_body")

	/*
	 * 外层守卫上下文 + 本循环体无挂起点（普通路径）时，本 while 是原子的：
	 * 恢复路径会整条跳过它，body 内部不需要任何恢复槽位。
	 * 若不加保护，body 语句会在守卫上下文里被 concGuardSlot 再包一层，
	 * 槽位号与外层 while 段的槽位号冲突（同号），循环体永不执行、
	 * 条件永真 → 死循环。普通路径下临时禁用守卫即可。
	 */
	guardSuspended := false
	guardSavedDepth, guardSavedSeq := sg.codegen.concGuardDepth, sg.codegen.concGuardSeq
	if guardDepth < 0 && sg.codegen.concGuardActive() {
		sg.codegen.concGuardDepth, sg.codegen.concGuardSeq = -1, 0
		guardSuspended = true
	}

	// 缺口6优化：快照 promotedVars，用于判断循环体内是否有 promote
	whilePromotedSnapshot := sg.codegen.SnapshotPromotedVars()

	// 方案 A'：本循环体的「写穿安全局部容器」打点从零开始，
	// 避免上一个循环的打点污染（whilePromotedSnapshot 只跟踪变量级 promote，
	// 看不到写穿目标，故用独立打点）。
	sg.codegen.ResetSuppressLoopRewind()

	// Phase 1+2 优化：区分 survivor 类型变量和 bump 分配
	// - survivor 类型变量（指针/string/slice/map）需要完整 survivor checkpoint/rewind
	// - bump 分配（kmm_v4_malloc等）只需 offset_save/restore 轻量回收
	// Phase 2：函数级已有 checkpoint 时，循环内 survivor 变量降级为 offset 回收
	//   （函数级 checkpoint 在函数出口统一回收所有 survivor 对象，
	//    循环内 offset_restore 只回收 bump 区域，不影响 survivor 区域）
	hasSurvivor := bodyHasSurvivorType(stmt.Body)
	hasBumpAlloc := bodyContainsAllocation(stmt.Body, sg.codegen.IsFreestanding())
	funcLevelCheckpoint := sg.codegen.funcHasKMMCheckpoint

	var useKMM, useOffset bool
	if funcLevelCheckpoint && hasSurvivor {
		// 函数级 checkpoint 已覆盖，循环内用 offset 轻量回收 bump 区域
		useKMM = false
		useOffset = true
	} else {
		useKMM = hasSurvivor
		useOffset = hasBumpAlloc
	}

	if useKMM {
		sg.codegen.EnterKMMScope()
	}
	if useOffset {
		sg.codegen.EnterOffsetScope()
	}

	var bodyCode strings.Builder
	// 循环生命周期（从 SOR 下移的能力⑥）：循环条件中使用的变量始终存活，
	// 不能在循环体内提前释放（下次迭代还会使用）
	loopAlwaysLive := make(map[string]bool)
	sg.collectVarsInExpr(stmt.Condition, loopAlwaysLive)
	bodyCode.WriteString(sg.generateStmtsWithLiveness(stmt.Body, loopAlwaysLive))
	// 方案 A：在作用域退出前发射 per-object survivor_free
	// 若末尾是 return，return 中已通过 generateSurvivorFreeForReturn 释放，此处为死代码，跳过
	if !lastStmtIsReturn(stmt.Body) {
		bodyCode.WriteString(sg.generateSurvivorFreeForScope())
	}

	if useOffset {
		sg.codegen.ExitOffsetScope()
	}
	if useKMM {
		sg.codegen.ExitKMMScope()
	}
	sg.codegen.ExitScope()
	sg.codegen.indent--

	// 缺口6优化：计算循环体内新增 promote 的变量
	whilePromoted := sg.codegen.DiffPromotedVars(whilePromotedSnapshot)

	// 方案 A'：循环体是否写穿了「安全局部容器」。
	//
	// 判定用 whilePromotedSnapshot 的时机不可靠（它只记录 promote 的**变量**，
	// 不记录写穿目标），这里直接让 body 生成期打点：generateAssignEscapeStatement
	// 在"因安全局部容器而放弃 promote"时调用 MarkSuppressLoopRewind()。
	// 该打点是 per-scope 的，进入循环体前后分别 Reset/Mark。
	suppressLoopRewind := sg.codegen.hasSuppressLoopRewind()

	if useKMM && useOffset {
		// survivor scope 外层包裹 + offset 内层回收：survivor 变量走 checkpoint/rewind，
		// bump 分配走 offset_save/restore 轻量回收，避免每次迭代完整 survivor 开销
		code += sg.codegen.indentString() + "kmm_survivor_checkpoint_t __while_surv_cp = kmm_v4_survivor_checkpoint();\n"
		code += sg.codegen.indentString() + sg.codegen.KMMStart("")
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "    kmm_v4_mark_t _loop_scope_start = kmm_v4_mark_save();\n"
		}
		code += bodyCode.String()
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "    kmm_v4_mark_restore(_loop_scope_start);\n"
		}
		code += sg.codegen.indentString() + sg.codegen.KMMEnd()
		if len(whilePromoted) == 0 {
			code += sg.codegen.indentString() + "kmm_v4_survivor_rewind(__while_surv_cp); /* survivor: scope reclaim */\n"
		}
	} else if useOffset {
		// 纯 bump 分配：offset_save/restore 轻量回收，无 survivor checkpoint 开销
		//
		// 方案 A'：若循环体写穿了安全局部容器，放弃循环级回卷 —— 对象必须留在
		// bump 区连续排布（否则回卷后的复用分配会覆盖刚链好的节点），
		// 由函数出口 scope_pop 统一回收。
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "kmm_v4_mark_t _loop_scope_start = kmm_v4_mark_save();\n"
		}
		code += bodyCode.String()
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "kmm_v4_mark_restore(_loop_scope_start);\n"
		}
	} else if useKMM {
		// 纯 survivor 类型：完整 survivor checkpoint/rewind
		code += sg.codegen.indentString() + "kmm_survivor_checkpoint_t __while_surv_cp = kmm_v4_survivor_checkpoint();\n"
		code += sg.codegen.indentString() + sg.codegen.KMMStart("")
		code += bodyCode.String()
		code += sg.codegen.indentString() + sg.codegen.KMMEnd()
		if len(whilePromoted) == 0 {
			code += sg.codegen.indentString() + "kmm_v4_survivor_rewind(__while_surv_cp); /* survivor: scope reclaim */\n"
		}
	} else {
		code += bodyCode.String()
	}

	/*
	 * 守卫模式：每轮末尾把槽位写回 0，下一轮才重新从循环头起跑。
	 *
	 * 放在循环体最后一步：`continue` 落到这里（语义与 C while 一致），
	 * `break` / `return` 提前跳出循环、走不到也不需要 ——
	 * 它们已经离开了这一层。
	 */
	if guardDepth >= 0 {
		code += sg.codegen.indentString() +
			fmt.Sprintf("%s->sub[%d] = 0;\n", sg.codegen.concFrameVar, guardDepth)
		restoreGuard()
	} else if guardSuspended {
		/* 普通路径：恢复被临时禁用的外层守卫 */
		sg.codegen.concGuardDepth, sg.codegen.concGuardSeq = guardSavedDepth, guardSavedSeq
	}

	code += sg.codegen.indentString() + "}\n"
	return code
}

// needsKMMScope 检查一段已生成的代码是否需要 KMM scope
// 优化策略：
// - 作用域合并：如果外层已有 KMM scope，内层不需要再插入
// - SOR 模式：使用精确的变量决策信息判断（从符号表获取当前 scope 的变量，排除函数参数）
// - 非 SOR 模式：基于符号表的类型分析 + 代码内容检测
// 纯 Stack 变量、系统 malloc（std_malloc，hosted 模式）分配的变量或
// 函数参数的 scope 可以省略，消除运行时开销
func (sg *StatementGenerator) needsKMMScope(bodyCode string) bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if sg.codegen.IsInKMMScope() {
		return false
	}

	// SOR 模式：使用精确的变量决策
	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		currentScope := sg.codegen.GetCurrentScope()
		var varNames []string
		if currentScope != nil {
			for name, sym := range currentScope.GetAllSymbols() {
				if sym.Scope != "parameter" && sym.Scope != "param" {
					varNames = append(varNames, name)
				}
			}
		}
		return adapter.NeedsKMMScopeByVars(varNames)
	}

	// 非 SOR 模式：智能分析
	return sg.needsKMMScopeNonSOR(bodyCode)
}

// shouldUseKMMScope 预判断：在生成 body 之前基于符号表判断是否需要 KMM scope
// 用于作用域合并优化：如果预判断需要 KMM，先 EnterKMMScope 再生成 body，
// 这样内层作用域的 needsKMMScope 会检测到外层已有 KMM 而跳过插入
func (sg *StatementGenerator) shouldUseKMMScope() bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if sg.codegen.IsInKMMScope() {
		return false
	}

	// SOR 模式：使用精确的变量决策（不依赖 body 代码）
	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		currentScope := sg.codegen.GetCurrentScope()
		var varNames []string
		if currentScope != nil {
			for name, sym := range currentScope.GetAllSymbols() {
				if sym.Scope != "parameter" && sym.Scope != "param" {
					varNames = append(varNames, name)
				}
			}
		}
		return adapter.NeedsKMMScopeByVars(varNames)
	}

	// 非 SOR 模式：基于符号表类型分析（不依赖 body 代码）
	currentScope := sg.codegen.GetCurrentScope()
	if currentScope != nil {
		for _, sym := range currentScope.GetAllSymbols() {
			if sym.Scope == "parameter" || sym.Scope == "param" {
				continue
			}
			if needsKMMForType(sym.Type) {
				return true
			}
		}
	}
	return false
}

// allocFuncNames 需要检测的 KMM 池分配函数名
// 注意：std_malloc 仅在 freestanding 模式下消耗 KMM 池（被重写为
// kmm_v4_alloc_auto）；hosted 模式下它是系统 malloc，不在此列
var allocFuncNames = map[string]bool{
	"kmm_v4_malloc":     true,
	"kmm_v4_alloc_auto": true,
	"kmm_v4_bump":       true,
	"malloc":            true,
}

// bodyContainsAllocation 递归检测语句列表中是否包含 KMM 池分配调用
// 用于在循环体等内存热点处插入 KMM scope，削平峰值
// isFreestanding: 用于判断 std_malloc 是否属于 KMM 池分配
func bodyContainsAllocation(stmts []ast.Statement, isFreestanding bool) bool {
	for _, stmt := range stmts {
		if stmtContainsAllocation(stmt, isFreestanding) {
			return true
		}
	}
	return false
}

// bodyHasSurvivorType 递归检测语句列表中是否声明了需要 survivor 管理的类型变量
// （指针、string、[]slice、map 等）。这些类型需要完整 survivor checkpoint/rewind，
// 不能仅用 offset_save/restore 回收。
func bodyHasSurvivorType(stmts []ast.Statement) bool {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.VariableDeclaration:
			if needsKMMForType(s.Type) {
				return true
			}
		case *ast.IfStatement:
			if bodyHasSurvivorType(s.Body) || bodyHasSurvivorType(s.Else) {
				return true
			}
		case *ast.WhileStatement:
			if bodyHasSurvivorType(s.Body) {
				return true
			}
		case *ast.ForStatement:
			if bodyHasSurvivorType(s.Body) {
				return true
			}
		case *ast.ForInStatement:
			if bodyHasSurvivorType(s.Body) {
				return true
			}
		case *ast.BlockStatement:
			if bodyHasSurvivorType(s.Statements) {
				return true
			}
		}
	}
	return false
}

// collectOuterVarNames 收集循环外（函数级）声明的变量名，用于逃逸分析
func collectOuterVarNames(funcBody []ast.Statement, loopBody []ast.Statement) map[string]bool {
	outer := make(map[string]bool)
	loopStmtSet := make(map[ast.Statement]bool)
	for _, s := range loopBody {
		loopStmtSet[s] = true
	}
	var collect func(stmts []ast.Statement, inLoop bool)
	collect = func(stmts []ast.Statement, inLoop bool) {
		for _, stmt := range stmts {
			if stmt == nil {
				continue
			}
			if !inLoop && loopStmtSet[stmt] {
				inLoop = true
			}
			switch s := stmt.(type) {
			case *ast.VariableDeclaration:
				if !inLoop {
					outer[s.Name] = true
				}
			case *ast.IfStatement:
				collect(s.Body, inLoop)
				collect(s.Else, inLoop)
			case *ast.WhileStatement:
				collect(s.Body, true)
			case *ast.ForStatement:
				collect(s.Body, true)
			case *ast.ForInStatement:
				collect(s.Body, true)
			case *ast.BlockStatement:
				collect(s.Statements, inLoop)
			}
		}
	}
	collect(funcBody, false)
	return outer
}

// allocationEscapes 检查循环体内的分配是否逃逸到循环外
// 逃逸条件（保守判断）：
//  1. 分配结果赋值给循环外声明的变量
//  2. 分配结果作为函数返回值
//  3. 分配结果作为参数传入函数调用（保守认为可能被保存）
//
// 返回 true 表示存在逃逸，不能用 offset_restore 回收（会导致悬垂指针）
func allocationEscapes(loopBody []ast.Statement, outerVars map[string]bool) bool {
	for _, stmt := range loopBody {
		if stmtEscapes(stmt, outerVars) {
			return true
		}
	}
	return false
}

func stmtEscapes(stmt ast.Statement, outerVars map[string]bool) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		// 赋值给循环外变量？变量声明的 Name 是新变量，不算逃逸
		// 但如果初始化表达式中分配的结果被外层引用，需要更复杂的分析
		return exprEscapes(s.Value, outerVars)
	case *ast.ExpressionStatement:
		// 检查赋值表达式：x = alloc(...) 其中 x 是循环外变量
		if bin, ok := s.Expression.(*ast.BinaryExpression); ok && bin.Operator == "=" {
			if ident, ok := bin.Left.(*ast.Identifier); ok && outerVars[ident.Name] {
				return true
			}
		}
		return exprEscapes(s.Expression, outerVars)
	case *ast.ReturnStatement:
		// 返回值中包含分配 → 逃逸到函数外
		return exprEscapes(s.Value, outerVars)
	case *ast.IfStatement:
		for _, bs := range s.Body {
			if stmtEscapes(bs, outerVars) {
				return true
			}
		}
		for _, bs := range s.Else {
			if stmtEscapes(bs, outerVars) {
				return true
			}
		}
	case *ast.WhileStatement:
		for _, bs := range s.Body {
			if stmtEscapes(bs, outerVars) {
				return true
			}
		}
	case *ast.ForStatement:
		for _, bs := range s.Body {
			if stmtEscapes(bs, outerVars) {
				return true
			}
		}
	case *ast.ForInStatement:
		for _, bs := range s.Body {
			if stmtEscapes(bs, outerVars) {
				return true
			}
		}
	case *ast.BlockStatement:
		for _, bs := range s.Statements {
			if stmtEscapes(bs, outerVars) {
				return true
			}
		}
	}
	return false
}

func exprEscapes(expr ast.Expression, outerVars map[string]bool) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.CallExpression:
		// 分配结果作为参数传入函数 → 保守认为逃逸
		if isAllocCall(e.Function, false) {
			return true
		}
		for _, arg := range e.Args {
			if exprEscapes(arg, outerVars) {
				return true
			}
		}
	case *ast.BinaryExpression:
		return exprEscapes(e.Left, outerVars) || exprEscapes(e.Right, outerVars)
	case *ast.UnaryExpression:
		return exprEscapes(e.Right, outerVars)
	case *ast.MemberAccessExpression:
		return exprEscapes(e.Object, outerVars)
	case *ast.ParenExpression:
		return exprEscapes(e.Inner, outerVars)
	case *ast.TypeCastExpression:
		return exprEscapes(e.Expression, outerVars)
	case *ast.ConditionalExpression:
		return exprEscapes(e.Condition, outerVars) ||
			exprEscapes(e.TrueExpr, outerVars) ||
			exprEscapes(e.FalseExpr, outerVars)
	}
	return false
}

func stmtContainsAllocation(stmt ast.Statement, isFreestanding bool) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		if s == nil {
			return false
		}
		return exprContainsAllocation(s.Value, isFreestanding)
	case *ast.ExpressionStatement:
		if s == nil {
			return false
		}
		return exprContainsAllocation(s.Expression, isFreestanding)
	case *ast.IfStatement:
		if s == nil {
			return false
		}
		if exprContainsAllocation(s.Condition, isFreestanding) {
			return true
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs, isFreestanding) {
				return true
			}
		}
		for _, bs := range s.Else {
			if stmtContainsAllocation(bs, isFreestanding) {
				return true
			}
		}
	case *ast.WhileStatement:
		if s == nil {
			return false
		}
		if exprContainsAllocation(s.Condition, isFreestanding) {
			return true
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs, isFreestanding) {
				return true
			}
		}
	case *ast.ForStatement:
		if s == nil {
			return false
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs, isFreestanding) {
				return true
			}
		}
	case *ast.ForInStatement:
		if s == nil {
			return false
		}
		for _, bs := range s.Body {
			if stmtContainsAllocation(bs, isFreestanding) {
				return true
			}
		}
	case *ast.BlockStatement:
		if s == nil {
			return false
		}
		for _, bs := range s.Statements {
			if stmtContainsAllocation(bs, isFreestanding) {
				return true
			}
		}
	case *ast.ReturnStatement:
		if s == nil {
			return false
		}
		return exprContainsAllocation(s.Value, isFreestanding)
	}
	return false
}

func exprContainsAllocation(expr ast.Expression, isFreestanding bool) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.CallExpression:
		if isAllocCall(e.Function, isFreestanding) {
			return true
		}
		for _, arg := range e.Args {
			if exprContainsAllocation(arg, isFreestanding) {
				return true
			}
		}
	case *ast.BinaryExpression:
		if exprContainsAllocation(e.Left, isFreestanding) {
			return true
		}
		if exprContainsAllocation(e.Right, isFreestanding) {
			return true
		}
	case *ast.UnaryExpression:
		return exprContainsAllocation(e.Right, isFreestanding)
	case *ast.MemberAccessExpression:
		return exprContainsAllocation(e.Object, isFreestanding)
	case *ast.ParenExpression:
		return exprContainsAllocation(e.Inner, isFreestanding)
	case *ast.TypeCastExpression:
		return exprContainsAllocation(e.Expression, isFreestanding)
	case *ast.ConditionalExpression:
		if exprContainsAllocation(e.Condition, isFreestanding) {
			return true
		}
		if exprContainsAllocation(e.TrueExpr, isFreestanding) {
			return true
		}
		if exprContainsAllocation(e.FalseExpr, isFreestanding) {
			return true
		}
	}
	return false
}

// isAllocCall 检查函数表达式是否为 KMM 池分配调用
// isFreestanding: std_malloc 仅在 freestanding 下（重写为 kmm_v4_alloc_auto）
// 消耗 KMM 池；hosted 模式下它是系统 malloc，不算 KMM 分配
func isAllocCall(fn ast.Expression, isFreestanding bool) bool {
	if fn == nil {
		return false
	}
	switch e := fn.(type) {
	case *ast.Identifier:
		if e.Name == "std_malloc" {
			return isFreestanding
		}
		return allocFuncNames[e.Name]
	case *ast.MemberAccessExpression:
		if e.Member == "std_malloc" {
			return isFreestanding
		}
		return allocFuncNames[e.Member]
	}
	return false
}

// shouldUseKMMScopeForBody 基于 AST body 语句列表预判断是否需要 KMM scope
// 解决符号表在 body 生成前未填充的问题：直接扫描 AST 变量声明
// 优先使用 SOR 作用域决策（如果已注册），否则回退到变量级别判断
// 额外检测动态分配调用（kmm_v4_malloc 等），在内存热点处插入 KMM scope 削平峰值
func (sg *StatementGenerator) shouldUseKMMScopeForBody(bodyStmts []ast.Statement) bool {
	// 作用域合并：外层已有 KMM scope 时，内层跳过
	if sg.codegen.IsInKMMScope() {
		return false
	}

	adapter := sg.codegen.GetSORAdapter()
	if adapter != nil && adapter.IsActive {
		// 优先使用作用域级别的 SOR 决策（如果当前作用域已注册）
		currentScope := sg.codegen.GetCurrentScope()
		if currentScope != nil {
			scopeName := currentScope.GetScopeName()
			if scopeName != "" {
				// 尝试作用域级别判断
				scopeID := adapter.GetScopeIDByName(scopeName)
				if scopeID > 0 {
					if sd := adapter.GetScopeDecision(scopeID); sd != nil {
						// 有作用域决策，直接使用
						// 修复 #15：arena 子系统已移除，只需检查 UsesBumpPool
						return sd.UsesBumpPool
					}
				}
			}
		}
		// 回退：变量级别判断
		var varNames []string
		for _, stmt := range bodyStmts {
			if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
				varNames = append(varNames, varDecl.Name)
			}
		}
		if adapter.NeedsKMMScopeByVars(varNames) {
			return true
		}
		// SOR 决策不需要 KMM 时，补充检查 SOR 未追踪的指针类型变量
		for _, stmt := range bodyStmts {
			if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
				if needsKMMForType(varDecl.Type) {
					return true
				}
			}
		}
		// 内存热点检测：body 中含 KMM 池分配调用时插入 KMM scope
		// 每次循环迭代结束时 scope_pop 回收 bump 指针，削平峰值
		if bodyContainsAllocation(bodyStmts, sg.codegen.IsFreestanding()) {
			return true
		}
		return false
	}

	// 非 SOR 模式：扫描 body 中的变量类型
	for _, stmt := range bodyStmts {
		if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
			if needsKMMForType(varDecl.Type) {
				return true
			}
		}
	}
	// 内存热点检测：body 中含 KMM 池分配调用时插入 KMM scope
	if bodyContainsAllocation(bodyStmts, sg.codegen.IsFreestanding()) {
		return true
	}
	return false
}

// needsKMMScopeNonSOR 非 SOR 模式下的智能 KMM 判断
// 基于符号表分析变量类型，判断是否需要 KMM scope
// 注意：hosted 模式下 std_malloc 是系统 malloc，不消耗 KMM 池；
// freestanding 下它被重写为 kmm_v4_alloc_auto，由下方 "kmm_v4" 内容检测覆盖
func (sg *StatementGenerator) needsKMMScopeNonSOR(bodyCode string) bool {
	// 快速检查：如果代码中没有 KMM 分配相关操作，直接返回 false
	if !strings.Contains(bodyCode, "kmm_v4") &&
		!strings.Contains(bodyCode, "string_concat") &&
		!strings.Contains(bodyCode, "string_dup") {
		return false
	}

	// 基于符号表分析
	currentScope := sg.codegen.GetCurrentScope()
	if currentScope != nil {
		for _, sym := range currentScope.GetAllSymbols() {
			// 排除函数参数
			if sym.Scope == "parameter" || sym.Scope == "param" {
				continue
			}
			// 检查变量类型是否需要 KMM
			if needsKMMForType(sym.Type) {
				return true
			}
		}
	}

	// 代码内容检测：如果有 kmm_v4_alloc_auto 调用，需要 KMM
	if strings.Contains(bodyCode, "kmm_v4_alloc_auto") {
		return true
	}

	return false
}

// needsKMMForType 判断类型是否需要 KMM 内存管理
// - 指针类型 (*T) 需要 KMM
// - 字符串类型 (string, str) 需要 KMM
// - 复合类型 ([]T, map[K]V) 需要 KMM
// - 值类型 (int, bool, float, double, etc.) 不需要 KMM
func needsKMMForType(typeName string) bool {
	if typeName == "" {
		return false
	}
	// void(T...)R 签名记法：运行时为裸 void*/函数指针，不由 KMM 托管
	if strings.HasPrefix(typeName, "void(") {
		return false
	}
	t := strings.ToLower(typeName)

	// 指针类型
	if strings.Contains(t, "*") {
		return true
	}

	// 字符串类型
	if t == "string" || t == "str" {
		return true
	}

	// 切片类型
	if strings.HasPrefix(t, "[]") {
		return true
	}

	// Map 类型
	if strings.HasPrefix(t, "map[") {
		return true
	}

	// 数组类型（大数组可能需要 KMM，但这里保守处理）
	if strings.HasPrefix(t, "[") {
		return true
	}

	// 值类型不需要 KMM
	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"float", "float32", "float64", "f32", "f64",
		"double", "bool", "char", "byte",
		"isize", "usize", "size", "void":
		return false
	}

	// 其他未知类型，保守处理为需要 KMM
	return true
}

// generateForInStatement 生成 range-based for 迭代代码
//
// 支持两种迭代形式：
//  1. range 迭代:  for x in range(N) | range(start, end[, step]) { body }
//     → for (T _i_ = start; _i_ < end; _i_ += step) { T x = _i_; body }
//  2. 数组/切片迭代: for x in arr { body }
//     → for (size_t _i_ = 0; _i_ < len; _i_++) { T x = arr[_i_]; body }
//
// 索引变量由编译器管理，用户代码不可见，消除越界可能
func (sg *StatementGenerator) generateForInStatement(stmt *ast.ForInStatement) string {
	// 检测 range(...) 调用
	if rng := asRangeCall(stmt.Iterable); rng != nil {
		return sg.generateForRangeStatement(stmt, rng)
	}

	iterCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Iterable)

	// 从符号表获取迭代对象的 Kaula 类型，决定迭代方式
	var iterName string
	if id, ok := stmt.Iterable.(*ast.Identifier); ok {
		iterName = id.Name
	}

	var elemCType string
	var isFixedArray bool
	var isSliceArray bool // []T 形态：C 侧是裸指针，运行期无长度
	var isStringIter bool
	var arraySize string

	if iterName != "" {
		if sym := sg.codegen.GetSymbol(iterName); sym != nil {
			kt := sym.Type
			isStringIter = kt == "string"
			elemKaula := inferElementType(kt)
			if elemKaula != "" {
				elemCType = sg.codegen.typeGenerator.convertType(elemKaula, false)
			}
			if len(kt) > 0 && kt[0] == '[' {
				closeBracket := strings.Index(kt, "]")
				if closeBracket > 0 {
					arraySize = kt[1:closeBracket]
				}
				/*
				 * 区分定长数组与切片（指针语义）：
				 *   `[5]i32` → arraySize="5"  → 定长数组，长度编译期已知
				 *   `[]i32`  → arraySize=""   → 指针，C 侧是 int32_t*，
				 *                               没有 .len / .ptr 字段
				 *
				 * 此前两者都置 isFixedArray=true，切片在长度表达式处走到
				 * `(arr).len` → 对裸指针取成员，clang 报
				 * "member reference base type 'int64_t *' is not a structure"。
				 * 切片的长度在运行期不可得，这里直接给编译期错误——比生成
				 * 坏 C 再让 clang 报栈底信息清晰得多。
				 */
				if arraySize == "" {
					isSliceArray = true
				} else {
					isFixedArray = true
				}
			}
		}
	}

	if isSliceArray {
		sg.codegen.error(fmt.Sprintf(
			"无法迭代切片 `[]T`（行 %d）：其 C 侧是裸指针，运行期不带长度。"+
				"请改用定长数组 `[N]T`，或用下标循环 `for i in range(n) { arr[i] }`。",
			stmt.Pos.Line))
	}

	code := "for (size_t _i_ = 0; _i_ < "
	if isFixedArray && arraySize != "" {
		code += arraySize
	} else {
		code += "(" + iterCode + ").len"
	}
	code += "; _i_++) {\n"

	sg.codegen.indent++
	sg.codegen.EnterScope("forin_body")
	code += sg.codegen.indentString()
	if elemCType != "" {
		code += elemCType + " " + stmt.Variable.Name + " = " + iterCode
	} else {
		code += "auto " + stmt.Variable.Name + " = " + iterCode
	}
	if isFixedArray {
		code += "[_i_]"
	} else if isStringIter {
		// 迭代 string：元素是第 i 个字符，必须经由 SSO 安全的取址宏。
		// 原先生成 `iterCode.ptr[_i_]`，而短串（len <= STRING_SSO_CAPACITY）的数据
		// 在 .sso 里、.ptr 未初始化，于是逐字符迭代读到随机内存。
		code += "string_data(" + iterCode + ")[_i_]"
	} else {
		code += ".ptr[_i_]"
	}
	code += ";\n"

	// 循环生命周期：循环变量和迭代对象始终存活
	loopAlwaysLive := make(map[string]bool)
	loopAlwaysLive[stmt.Variable.Name] = true
	if iterName != "" {
		loopAlwaysLive[iterName] = true
	}
	code += sg.generateStmtsWithLiveness(stmt.Body, loopAlwaysLive)
	// 循环体末尾释放循环内声明的变量
	if !lastStmtIsReturn(stmt.Body) {
		code += sg.generateSurvivorFreeForScope()
	}
	sg.codegen.ExitScope()
	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// rangeCallInfo 描述一个 range(...) 调用的参数（已求值为 C 表达式字符串）
type rangeCallInfo struct {
	startExpr string // 起始值（默认 "0"）
	endExpr   string // 结束值（不包含）
	stepExpr  string // 步长（默认 "1"）；可能为负
}

// asRangeCall 判断表达式是否为 range(...) 调用，是则返回参数信息，否则返回 nil
// 仅识别 CallExpression，且其 Function 为 Identifier "range"
func asRangeCall(expr ast.Expression) *rangeCallInfo {
	call, ok := expr.(*ast.CallExpression)
	if !ok || call == nil {
		return nil
	}
	id, ok := call.Function.(*ast.Identifier)
	if !ok || id.Name != "range" {
		return nil
	}
	// range 至少需要 1 个参数，最多 3 个
	if len(call.Args) < 1 || len(call.Args) > 3 {
		return nil
	}
	return &rangeCallInfo{}
}

// generateForRangeStatement 生成 for x in range(...) { body } 的 C 代码
func (sg *StatementGenerator) generateForRangeStatement(stmt *ast.ForInStatement, rng *rangeCallInfo) string {
	call, _ := stmt.Iterable.(*ast.CallExpression)

	// 求值参数
	argExprs := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		argExprs = append(argExprs, sg.codegen.expressionGenerator.GenerateExpression(arg))
	}

	startExpr := "0"
	endExpr := argExprs[0]
	stepExpr := "1"
	switch len(argExprs) {
	case 1:
		// range(N) → 0..N-1, step 1
	case 2:
		// range(start, end) → start..end-1, step 1
		startExpr = argExprs[0]
		endExpr = argExprs[1]
	case 3:
		// range(start, end, step)
		startExpr = argExprs[0]
		endExpr = argExprs[1]
		stepExpr = argExprs[2]
	}

	// 选择索引变量的 C 类型：i64 以匹配 Kaula int 语义
	idxType := "long long"

	// 步长符号决定循环条件：
	//   step >= 0 → _i_ < end
	//   step <  0 → _i_ > end
	// 编译期无法静态确定符号时，生成条件表达式
	var cond string
	if isNegativeLiteral(stepExpr) {
		cond = "_i_ > (" + endExpr + ")"
	} else if isPositiveLiteral(stepExpr) {
		cond = "_i_ < (" + endExpr + ")"
	} else {
		// 运行时判断步长符号
		cond = "((" + stepExpr + ") >= 0 ? _i_ < (" + endExpr + ") : _i_ > (" + endExpr + "))"
	}

	// 使用 do-while(0) 包裹，便于 body 中 break/continue 正常工作
	// 同时保持与 KMM scope 插入逻辑兼容
	//
	// 守卫模式（循环体含挂起点）：迭代下标/上下界提升到 Frame
	// （loop_i[d]/loop_end[d]/loop_step[d]），恢复从 Frame 继续，
	// 循环体只执行正确轮次 —— 不会"从头再数一遍"重复执行。
	var code string
	guardDepth := -1
	restoreGuard := func() {}
	if sg.codegen.concGuardActive() && stmtsHaveSuspend(stmt.Body) {
		restoreGuard = sg.codegen.concGuardEnter()
		guardDepth = sg.codegen.concGuardDepth
		f := sg.codegen.concFrameVar
		d := guardDepth
		// init 槽（循环外，只执行一次）：start/end/step 求值入 Frame。
		// 槽位分配：init=0、循环头=1、body 语句 2+；每轮末尾 sub[d]=1
		// 回到循环头（跳过 init），恢复点落在 body 槽 >= 2。
		code = sg.concGuardSlot(func() string {
			return fmt.Sprintf("%s->loop_i[%d] = (%s);\n"+
				"%s->loop_end[%d] = (%s);\n"+
				"%s->loop_step[%d] = (%s);\n",
				f, d, startExpr, f, d, endExpr, f, d, stepExpr)
		})
		code += "for (;;) {\n"
		// 循环头槽：判条件（引用 Frame 的迭代状态，不再每轮求值表达式）
		condFrame := fmt.Sprintf(
			"(%s->loop_step[%d] >= 0 ? %s->loop_i[%d] < %s->loop_end[%d] : %s->loop_i[%d] > %s->loop_end[%d])",
			f, d, f, d, f, d, f, d, f, d)
		code += sg.concGuardLoopHead(condFrame)
	} else {
		code = "for (" + idxType + " _i_ = (" + startExpr + "); " + cond + "; _i_ += (" + stepExpr + ")) {\n"
	}

	/*
	 * 原子循环体保护（与 generateWhileStatement 同源）。
	 *
	 * 走到这里且 guardDepth < 0，说明本 for..in 的**自身循环体不含挂起点**
	 * —— 它在恢复路径上是原子的（整条跳过即可），循环体不该带任何恢复槽位。
	 *
	 * 但它可能**嵌在外层守卫复合语句里**（例如 while 体内套一个无挂起的
	 * for..in）。此时 concGuardActive() 仍为真，循环体语句会被
	 * concGuardSlot 按**外层**的槽位号包进 `if (f->sub[d] == k)`。而
	 * 外层那个槽位是「一条语句只推进一次」的语义，在 C 的 for 循环体里
	 * 跨迭代**从不复位** —— 于是第 1 轮把它从 k 推到 k+1，第 2 轮起
	 * 整条守卫链失配，循环体只执行第 1 轮（静默算错）。
	 *
	 * 修法与 while 完全一致：生成循环体期间临时禁用守卫，让语句直接平铺。
	 */
	guardSuspended := false
	guardSavedDepth, guardSavedSeq := sg.codegen.concGuardDepth, sg.codegen.concGuardSeq
	if guardDepth < 0 && sg.codegen.concGuardActive() {
		sg.codegen.concGuardDepth, sg.codegen.concGuardSeq = -1, 0
		guardSuspended = true
	}

	sg.codegen.indent++
	sg.codegen.EnterScope("forrange_body")
	// 方案 A'：本循环体的「写穿安全局部容器」打点从零开始（同 while）。
	sg.codegen.ResetSuppressLoopRewind()
	if guardDepth >= 0 {
		// 守卫：迭代变量从 Frame 恢复
		code += sg.codegen.indentString()
		code += fmt.Sprintf("%s %s = %s->loop_i[%d];\n",
			idxType, stmt.Variable.Name, sg.codegen.concFrameVar, guardDepth)
	} else {
		code += sg.codegen.indentString()
		code += idxType + " " + stmt.Variable.Name + " = _i_;\n"
	}

	// KMM scope 处理：与 while 循环一致，循环体内分配的内存在每次迭代结束时回卷
	// Phase 1+2 优化：区分 survivor 类型变量和 bump 分配
	// Phase 2：函数级已有 checkpoint 时，循环内 survivor 变量降级为 offset 回收
	hasSurvivor := bodyHasSurvivorType(stmt.Body)
	hasBumpAlloc := bodyContainsAllocation(stmt.Body, sg.codegen.IsFreestanding())
	funcLevelCheckpoint := sg.codegen.funcHasKMMCheckpoint

	var useKMM, useOffset bool
	if funcLevelCheckpoint && hasSurvivor {
		useKMM = false
		useOffset = true
	} else {
		useKMM = hasSurvivor
		useOffset = hasBumpAlloc
	}

	if useKMM {
		sg.codegen.EnterKMMScope()
	}
	if useOffset {
		sg.codegen.EnterOffsetScope()
	}

	// 循环生命周期：循环变量始终存活
	loopAlwaysLive := make(map[string]bool)
	loopAlwaysLive[stmt.Variable.Name] = true
	var bodyCode strings.Builder
	bodyCode.WriteString(sg.generateStmtsWithLiveness(stmt.Body, loopAlwaysLive))
	// 循环体末尾释放循环内声明的变量
	if !lastStmtIsReturn(stmt.Body) {
		bodyCode.WriteString(sg.generateSurvivorFreeForScope())
	}

	if useOffset {
		sg.codegen.ExitOffsetScope()
	}
	if useKMM {
		sg.codegen.ExitKMMScope()
	}
	sg.codegen.ExitScope()

	// 方案 A'：循环体写穿了安全局部容器 → 放弃循环级回卷（同 while）。
	suppressLoopRewind := sg.codegen.hasSuppressLoopRewind()

	// 生成 scope 包裹代码
	if useKMM && useOffset {
		// survivor scope 外层 + offset 内层
		code += sg.codegen.indentString() + "kmm_survivor_checkpoint_t __for_surv_cp = kmm_v4_survivor_checkpoint();\n"
		code += sg.codegen.indentString() + sg.codegen.KMMStart("")
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "    kmm_v4_mark_t _for_scope_start = kmm_v4_mark_save();\n"
		}
		code += bodyCode.String()
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "    kmm_v4_mark_restore(_for_scope_start);\n"
		}
		code += sg.codegen.indentString() + sg.codegen.KMMEnd()
		code += sg.codegen.indentString() + "kmm_v4_survivor_rewind(__for_surv_cp); /* survivor: for-range scope reclaim */\n"
	} else if useOffset {
		// 纯 bump 分配：offset 轻量回收
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "kmm_v4_mark_t _for_scope_start = kmm_v4_mark_save();\n"
		}
		code += bodyCode.String()
		if !suppressLoopRewind {
			code += sg.codegen.indentString() + "kmm_v4_mark_restore(_for_scope_start);\n"
		}
	} else if useKMM {
		// 纯 survivor 类型：完整 survivor checkpoint/rewind
		code += sg.codegen.indentString() + "kmm_survivor_checkpoint_t __for_surv_cp = kmm_v4_survivor_checkpoint();\n"
		code += sg.codegen.indentString() + sg.codegen.KMMStart("")
		sg.codegen.indent++
		code += bodyCode.String()
		sg.codegen.indent--
		code += sg.codegen.indentString() + sg.codegen.KMMEnd()
		code += sg.codegen.indentString() + "kmm_v4_survivor_rewind(__for_surv_cp); /* survivor: for-range scope reclaim */\n"
	} else {
		code += bodyCode.String()
	}

	// 守卫模式：每轮末尾步进迭代下标并回到循环头槽（sub[d] = 1：
	// 跳过 init 槽 0、命中循环头槽 1）。continue 落到这里（与 C for
	// 的步进语义一致）；break/return 提前离开循环，无需归位。
	if guardDepth >= 0 {
		code += sg.codegen.indentString() +
			fmt.Sprintf("%s->loop_i[%d] += %s->loop_step[%d];\n",
				sg.codegen.concFrameVar, guardDepth,
				sg.codegen.concFrameVar, guardDepth)
		code += sg.codegen.indentString() +
			fmt.Sprintf("%s->sub[%d] = 1;\n", sg.codegen.concFrameVar, guardDepth)
		restoreGuard()
	} else if guardSuspended {
		/* 普通路径：恢复被临时禁用的外层守卫（见上面的原子循环体保护） */
		sg.codegen.concGuardDepth, sg.codegen.concGuardSeq = guardSavedDepth, guardSavedSeq
	}

	sg.codegen.indent--
	code += sg.codegen.indentString() + "}\n"
	return code
}

// isPositiveLiteral 判断字符串是否为正整数字面量
func isPositiveLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// 去除可选的 + 号
	if s[0] == '+' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	// "0" 视为非负但非正，这里返回 true（>= 0 分支）
	return true
}

// isNegativeLiteral 判断字符串是否以 '-' 开头的负数字面量
func isNegativeLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	for _, ch := range s[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// inferElementType 从数组/切片类型字符串中提取元素类型
func inferElementType(typeStr string) string {
	if len(typeStr) > 0 && typeStr[0] == '[' {
		closeBracket := strings.Index(typeStr, "]")
		if closeBracket > 0 {
			return typeStr[closeBracket+1:]
		}
	}
	return ""
}

// generateReturnStatement 生成 return 语句代码
// 修复 #22：在 return 前补 kmm_v4_scope_pop()，防止 do-while(0) 作用域泄漏
// KMM_V4_SCOPE_START/END 使用 do-while(0) 包裹，return 会跳过 KMM_V4_SCOPE_END
// 中的 scope_pop，导致作用域栈不弹出。此处按当前 kmmScopeDepth 补齐 pop。
func (sg *StatementGenerator) generateReturnStatement(stmt *ast.ReturnStatement) string {
	/*
	 * conc fn 的 resume：改写 return。
	 *
	 * resume 的 C 返回类型是 KaulaResumeResult，不是用户函数的返回类型。
	 * 裸生成 `return X;` 会让 clang 报类型不匹配。
	 */
	if sg.codegen.inConcResume {
		retType := sg.codegen.currentFunctionReturnType
		isVoid := (retType == "" || retType == "void" || retType == "Void")
		return sg.generateConcReturn(stmt, isVoid)
	}
	return sg.generateReturnStatementNormal(stmt)
}

/*
 * generateConcReturn 生成 conc fn resume 体内的 return。
 *
 * `return X`  →
 *      f->result = X;
 *      f->pc = -1;
 *      <KMM 作用域回卷：按当前活跃深度发射 scope_pop / mark_restore>
 *      return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = &f->result };
 *
 * void 函数则只标记完成。
 *
 * == 为什么必须在这里回卷 KMM 作用域 ==
 *
 * conc fn 的 body 会被块级 KMM 区间（while/for/if/block 各开一层
 * `KMM_V4_SCOPE_START`，见 stmtgen 各处 KMMStart("")）包裹。这些区间用
 * `do { ... } while (0); kmm_v4_scope_pop()` 收尾，**正常路径**经宏弹栈，
 * 而 `return` 直接跳出函数，绕过了 kmm_v4_scope_pop() —— 于是每执行一次
 * 提前 return，就在本线程 TLS 的定长作用域栈上永久泄漏一帧（16B）。
 * 单线程累计 KMM_V4_MAX_SCOPE_DEPTH（1024）次即 `KMM FATAL ... aborting`。
 *
 * 对「每连接一个 conc fn」的服务形态，连接 churn 就是 DoS：
 * 客户端反复建连/断连，约 `1024 × worker 数` 次后服务崩溃。
 * 本基准 order_server.kl 的 `got <= 0`（客户端 EOF）提前 return 正是此路径。
 *
 * 挂起 return（conc_io_rewrite.go / concSuspendReturn）早已做同样的配平，
 * 这里补齐提前 return，二者一致。
 *
 * == 深度口径 ==
 *
 * 直接用 `kmmScopeDepth`（与挂起 return 同一口径）：它记录「当前生成位置
 * 已 Enter 未 Exit 的 KMM 区间数」。conc fn 的 body 由 EmitResume 以
 * `cg.kmmScopeDepth = 0` 起步重新生成，此后各块级 KMMStart("") 与
 * EnterKMMScope() 成对维护该计数，所以提前 return 处读到的正是
 * 此刻真正打开的区间层数。
 *
 * conc guard 槽位（`if (f->sub[d] == k) { ... }`）是普通 C 花括号，
 * **不 push KMM 作用域**，因此不计入深度。
 *
 * mark 模式（递归环内函数改用 kmm_v4_mark_save/restore）下没有 scope 栈槽位，
 * 提前退出的净效果是一次回卷到函数入口 mark，走 KMMExitToFunctionMark。
 */
func (sg *StatementGenerator) generateConcReturn(stmt *ast.ReturnStatement, isVoid bool) string {
	f := sg.codegen.concFrameVar
	if f == "" {
		f = "f"
	}

	var b strings.Builder

	// KMM 作用域回卷：提前 return 必须与已发射的区间起始配平。
	// 放在 `f->pc = -1;` 之前——回卷与返回值写入互不影响，但保持
	// 「先清理再宣告完成」的次序更贴近普通 fn 的 return 路径。
	popCode := sg.concKMMExitPopLines()

	/*
	 * KBUG_CONC_RETURN_SURVIVOR_LEAK 修复②：per-object survivor 回收。
	 *
	 * 旧实现只发射 popCode（KMM 作用域回卷），**漏掉了 per-object 回收**。
	 * 普通 fn 的 return 路径（generateReturnStatementNormal → 
	 * generateSurvivorFreeForReturn）会对每个 KMM 来源的指针变量发射
	 * kmm_v4_survivor_free。而 conc fn 的 return 走了这条降级路径，
	 * 于是「跨挂起点存活、被提升到 survivor 段」的缓冲区永远不会归还。
	 *
	 * 典型受害形态（本仓 test/bench/service3 的 order_server.kl）：
	 *     conc fn conn_task(fd) {
	 *         u8* buf  = kmm_v4_alloc(8192)   // 跨挂起点 → survivor 段
	 *         u8* resp = kmm_v4_alloc(64)
	 *         ...
	 *         if (got <= 0) { kaula_close(fd); return 0 }   // ← 这里
	 *     }
	 * 每连接泄漏 8192+64 = 8256 B。100 连接闭环下每连接存活约 168 请求，
	 * 折合 8256/168 ≈ 49.1 B/req —— 与实测 49.2 B/req 精确吻合。
	 *
	 * 注意两点：
	 *  1) 回收必须发生在 **scope_pop 之前**：survivor_free 依赖变量当前值，
	 *     而 scope_pop 只回卷 TLS bump 区、不影响 survivor 段，但保持与普通
	 *     fn 相同的「先 free 再 pop」次序更安全（普通 fn 也是 freeCode 在
	 *     KMMExitPopLines 之前）。
	 *  2) conc fn 的「跨挂起点变量」是在 frame 里（`f->buf`），但它们同时也
	 *     是本 resume 体的局部量（`uint8_t* buf = f->buf;`），作用域符号表里
	 *     依然有它们，因此 generateSurvivorFreeForReturn 能正确遍历到。
	 */
	freeCode, _ := sg.generateSurvivorFreeForReturn("")
	if freeCode != "" {
		b.WriteString(freeCode)
	}

	if isVoid || stmt.Value == nil {
		b.WriteString(popCode)
		b.WriteString(f + "->pc = -1;\n")
		b.WriteString("return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
		return b.String()
	}

	/*
	 * 先求值返回值再回卷：返回值若是作用域内分配的对象，回卷后才拷贝
	 * 就晚了（悬垂）。return 提升路径（promote）由 SOR 负责，这里只保证
	 * 表达式求值发生在回卷之前。
	 */
	valueCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
	b.WriteString(f + "->result = " + valueCode + ";\n")
	b.WriteString(popCode)
	b.WriteString(f + "->pc = -1;\n")
	b.WriteString("return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = &" +
		f + "->result };\n")
	return b.String()
}

/*
 * concKMMExitPopLines 产出 conc fn 提前 return 所需的 KMM 作用域回卷代码。
 *
 * mark 模式：一次 kmm_v4_mark_restore(<函数入口 mark>)，等价于由内向外逐层回卷。
 * scope 模式：按词法深度（kmmScopeDepth）逐层发射 kmm_v4_scope_pop()。
 *
 * 深度口径必须与挂起 return 完全一致（conc_io_rewrite.go 用
 * `for i := 0; i < cg.kmmScopeDepth; i++`）—— 二者描述的是同一件事：
 * 「当前生成位置已 Enter 未 Exit 的 KMM 区间数」。conc guard 槽位
 * （`if (f->sub[d] == k) { ... }`）是普通 C 花括号，**不 push KMM 作用域**，
 * 不计入深度；把它算进去会导致多弹一层 → 作用域栈下溢 abort。
 *
 * 每行已带缩进（与 generateReturnStatementNormal 的 KMMExitPopLines 口径一致）。
 */
func (sg *StatementGenerator) concKMMExitPopLines() string {
	cg := sg.codegen
	indent := cg.indentString()

	if cg.kmmUseMark {
		// mark 模式：函数体（含提前退出路径）由 KMMExitToFunctionMark 一次性回卷。
		if cg.kmmFnMark == "" {
			return ""
		}
		return indent + "kmm_v4_mark_restore(" + cg.kmmFnMark + ");\n"
	}

	depth := cg.kmmScopeDepth
	if depth <= 0 {
		return ""
	}

	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString(indent)
		b.WriteString("kmm_v4_scope_pop();\n")
	}
	return b.String()
}

// generateReturnStatementNormal 生成普通 fn 的 return（原逻辑）
func (sg *StatementGenerator) generateReturnStatementNormal(stmt *ast.ReturnStatement) string {
	retType := sg.codegen.currentFunctionReturnType
	isVoid := (retType == "" || retType == "void" || retType == "Void")

	var b strings.Builder

	// 尾调用改写（TCO）判定：受 KMM 作用域托管的自递归函数，把 `return f(...)`
	// 换成"回收本层 → 实参写回形参 → 跳回循环顶部"，复用同一个 C 栈帧。
	// 必须在返回值表达式求值**之前**判定：尾调用路径根本不求值该表达式
	// （若求值就等于又做了一次真实递归调用，TCO 形同虚设）。
	tcoCall := (*ast.CallExpression)(nil)
	if call, ok := stmt.Value.(*ast.CallExpression); ok && sg.codegen.IsTcoTailSite(call) {
		if sg.tcoTailEligible(call) {
			tcoCall = call
		}
		// 实参引用了本帧会被回收的对象 → 退回真实尾调用（tcoCall 保持 nil）
	}

	// 修复 #25：return 前跨作用域提升返回值（必须在本函数作用域 pop 之前拷贝，
	// 否则返回值指针在 kmm_v4_scope_pop 回卷后悬垂）
	retPromoteLine := ""
	returnVarName := "" // 返回的对象及其别名不得在 return 路径被释放
	if !isVoid && stmt.Value != nil {
		if id, ok := stmt.Value.(*ast.Identifier); ok &&
			(sg.isPointerTyped(id.Name) || sg.isStringTyped(id.Name)) {
			returnVarName = id.Name
		}
		if id, ok := stmt.Value.(*ast.Identifier); ok && sg.needsReturnPromote(id.Name) {
			sizeExpr := ""
			if sg.isStringTyped(id.Name) {
				// 任务①：String 大小由 string_clone_global 内部决定，sizeExpr 可空
				sizeExpr = "sizeof(String)"
			} else if size, ok := sg.sorPromoteSize(id.Name, id.Name); ok {
				sizeExpr = size
			} else {
				sizeExpr = "sizeof(*" + id.Name + ")"
			}
			if line := sg.emitPromoteForType(id.Name, sizeExpr, "promote on return", id.Name); line != "" {
				retPromoteLine = sg.codegen.indentString() + line
				sg.codegen.funcEmitsPromote = true // 方案 C：标记函数有 promote
			} else {
				// 未知长度的缓冲区不能只拷贝一个元素；也不能在作用域
				// 回卷后把未提升的局部指针交给调用方。
				sg.codegen.error("[return escape] 无法安全提升返回指针 '" + id.Name +
					"'：分配大小未知；请使用 kmm_v4_global_alloc 或显式管理生命周期")
			}
		}
	}

	// 先回收其他对象，再提升返回值；始终排除返回指针及其别名，
	// 即使它已提升、来自形参或因未知大小而无法提升，也不能在返回前释放。
	freeCode, freedNames := sg.generateSurvivorFreeForReturn(returnVarName)

	// 直接返回分配表达式没有局部变量可供 promote。把作用域分配改为
	// survivor 分配（参数只求值一次，也支持运行期长度），避免 scope_pop 回卷产物。
	directReturnAlloc := ""
	if !isVoid && stmt.Value != nil {
		value := stmt.Value
		if cast, ok := value.(*ast.TypeCastExpression); ok {
			value = cast.Expression
		}
		if call, ok := value.(*ast.CallExpression); ok {
			switch calledFuncName(call.Function) {
			case "kmm_v4_alloc":
				directReturnAlloc = "kmm_v4_alloc("
			case "kmm_v4_alloc_auto":
				directReturnAlloc = "kmm_v4_alloc_auto("
			case "kmm_v4_bump":
				directReturnAlloc = "kmm_v4_bump("
			case "std_malloc":
				if sg.codegen.IsFreestanding() {
					directReturnAlloc = "kmm_v4_alloc_auto("
				}
			}
		}
	}
	if directReturnAlloc != "" {
		sg.codegen.funcEmitsPromote = true
	}

	// 方案 C：若函数不向外 promote，return 前回卷 survivor 段
	// （若 return 自身 promote 了返回值，funcEmitsPromote 已为 true，跳过）
	//
	// 追加保护：返回值本身若已是某次 promote 产出的幸存副本的持有者
	// （如 `keep = p` 之后 `return keep`），rewind 会把该副本连同 survivor
	// 段一起回卷，调用方拿到悬垂指针。这类副本必须活过函数返回，故与
	// "向外 promote" 同等对待——抑制 rewind。
	// 注意：被 promote 的源变量其 DropAction 已变为 DropNone，
	// sorNeedsReturnPromote 会返回 false（不再重复 promote），
	// 因此这里不能只依赖 funcEmitsPromote，必须显式查持有者身份。
	if returnVarName != "" && sg.codegen.IsPromoteHolder(returnVarName) {
		sg.codegen.funcEmitsPromote = true
	}
	// 只有函数级 useKMM 生成了 __surv_cp 声明时才发射 rewind(否则引用未声明变量)
	rewindOnReturn := sg.codegen.kmmScopeDepth > 0 && !sg.codegen.funcEmitsPromote && sg.codegen.funcHasKMMCheckpoint

	// F17：返回表达式的求值必须提升在回收动作之前。
	// 原实现先发射 free/rewind 再 `return <expr>`，表达式中凡访问被回收
	// 指针（如 return arr[0]+arr[1]）即 use-after-free、结果静默出错。
	// 统一先生成表达式代码；当本路径存在回收时，用 __typeof__ 把表达式
	// 求值提升到临时变量（标量/结构体拷贝语义皆可），再执行回收并返回临时。
	// 已知边界：表达式自身分配 >=2KB survivor 大对象且触发 rewind 时，
	// 提升会使回卷回收表达式产物——此类返回值应走 promote 路径。
	exprCode := ""
	if !isVoid && tcoCall == nil {
		if stmt.Value != nil {
			exprCode = sg.codegen.expressionGenerator.GenerateExpression(stmt.Value)
			if directReturnAlloc != "" {
				exprCode = strings.Replace(exprCode, directReturnAlloc, "kmm_v4_alloc_global(", 1)
			}
			// 返回类型是 interface 而返回值是具体类对象 → 装箱成接口句柄
			// （fn grow() Shape { return Circle(...) }）
			exprCode = sg.codegen.expressionGenerator.liftInterfaceValue(retType, exprCode, stmt.Value)
		} else {
			exprCode = "0"
		}
	}
	hoistTemp := ""
	// promote 会改写原变量；若先保存旧指针到临时变量，返回临时值仍会悬垂。
	// 标识符表达式无副作用，提升后直接返回改写后的变量即可。
	if !isVoid && tcoCall == nil && stmt.Value != nil && retPromoteLine == "" &&
		(len(freedNames) > 0 || rewindOnReturn) {
		sg.codegen.retHoistSeq++
		hoistTemp = fmt.Sprintf("__kaula_ret%d", sg.codegen.retHoistSeq)
		b.WriteString(sg.codegen.indentString())
		b.WriteString("__typeof__(")
		b.WriteString(exprCode)
		b.WriteString(") ")
		b.WriteString(hoistTemp)
		b.WriteString(" = ")
		b.WriteString(exprCode)
		b.WriteString(";\n")
	}

	b.WriteString(freeCode)

	if retPromoteLine != "" {
		b.WriteString(retPromoteLine)
	}

	if rewindOnReturn {
		b.WriteString(sg.codegen.indentString())
		b.WriteString("kmm_v4_survivor_rewind(__surv_cp); /* survivor: reclaim on return */\n")
	}

	// 在 return 前补区间回卷，防止作用域泄漏
	// 非 mark 模式：kmmScopeDepth 跟踪当前活跃的 do-while(0) KMM 作用域数量，
	// 逐个 pop（作用域合并优化确保内层不会重复创建 scope，depth 通常为 0 或 1）；
	// mark 模式：一次性回卷到函数入口 mark（等价于由内向外逐层回卷的净效果）。
	b.WriteString(sg.codegen.KMMExitPopLines(sg.codegen.indentString()))

	// 尾调用改写：到这一步 free / rewind / scope_pop 已按 return 路径发射完毕，
	// 正是尾跳转需要的前置动作（本层对象已释放、作用域已回收）。剩下的只是
	// 把实参写回形参并跳回循环顶部——不调用、不返回。
	if tcoCall != nil {
		sg.emitTcoRebindAndGoto(&b, tcoCall)
		sg.codegen.TcoState().MarkTcoConverted()
		return b.String()
	}

	if isVoid {
		b.WriteString(sg.codegen.indentString())
		b.WriteString("return;\n")
		return b.String()
	}

	b.WriteString(sg.codegen.indentString())
	b.WriteString("return ")
	if hoistTemp != "" {
		b.WriteString(hoistTemp)
	} else {
		b.WriteString(exprCode)
	}
	b.WriteString(";\n")
	return b.String()
}

// generateBreakStatement 生成 break 语句代码
func (sg *StatementGenerator) generateBreakStatement(stmt *ast.BreakStatement) string {
	return sg.codegen.indentString() + "break;\n"
}

// generateContinueStatement 生成 continue 语句代码
func (sg *StatementGenerator) generateContinueStatement(stmt *ast.ContinueStatement) string {
	return sg.codegen.indentString() + "continue;\n"
}

// generateImportStatement 生成 import 语句代码
func (sg *StatementGenerator) generateImportStatement(stmt *ast.ImportStatement) string {
	// import 语句在 C 中不需要特殊处理
	return ""
}

// generateExportStatement 生成 export 语句代码
func (sg *StatementGenerator) generateExportStatement(stmt *ast.ExportStatement) string {
	code := ""

	// 根据导出类型生成不同的代码
	switch stmt.Type {
	case "function":
		code += fmt.Sprintf("// Export function: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	case "class":
		code += fmt.Sprintf("// Export class: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	case "object":
		code += fmt.Sprintf("// Export object: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s_obj;\n", stmt.Name)
	case "variable":
		code += fmt.Sprintf("// Export variable: %s\n", stmt.Name)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	default:
		code += fmt.Sprintf("// Export: %s (%s)\n", stmt.Name, stmt.Type)
		code += fmt.Sprintf("KAULA_EXPORT %s;\n", stmt.Name)
	}

	return code
}

// generateNonLocalStatement 生成 nonlocal 语句代码
// nonlocal 是纯绑定声明：不产生新变量。若有初始值则对外层捕获变量赋值；
// 若目标在外层不存在（sema 已报错），生成空代码防御。
func (sg *StatementGenerator) generateNonLocalStatement(stmt *ast.NonLocalStatement) string {
	if stmt.Value != nil {
		if sg.codegen.currentCaptureSet != nil {
			if idx, ok := sg.codegen.currentCaptureSet[stmt.Name]; ok {
				return "(*_cap_" + strconv.Itoa(idx) + ") = " +
					sg.codegen.expressionGenerator.GenerateExpression(stmt.Value) + ";\n"
			}
		}
	}
	return ""
}

// generateBlockStatement 生成块语句代码
func (sg *StatementGenerator) generateBlockStatement(stmt *ast.BlockStatement) string {
	sg.codegen.EnterScope("block")
	indent := sg.codegen.indentString()

	adapter := sg.codegen.GetSORAdapter()
	needsKMM := false
	if adapter != nil && adapter.IsActive {
		needsKMM = sg.shouldUseKMMScopeForBody(stmt.Statements)
	}

	if needsKMM {
		// SOR 需要 KMM 作用域：offset save/restore
		if sg.codegen.IsInOffsetScope() {
			code := "{\n"
			sg.codegen.indent++
			code += sg.generateStmtsWithLiveness(stmt.Statements, nil)
			// 方案 A：per-object survivor 回收（末尾是 return 时跳过——return 中已释放）
			if !lastStmtIsReturn(stmt.Statements) {
				code += sg.generateSurvivorFreeForScope()
			}
			sg.codegen.indent--
			code += indent + "}\n"
			sg.codegen.ExitScope()
			return code
		}
		code := "{\n"
		sg.codegen.indent++
		sg.codegen.EnterOffsetScope()
		code += sg.codegen.indentString() + "kmm_v4_mark_t _scope_start = kmm_v4_mark_save();\n"
		code += sg.generateStmtsWithLiveness(stmt.Statements, nil)
		// 方案 A：per-object survivor 回收（在 offset_restore 前发射，末尾是 return 时跳过）
		if !lastStmtIsReturn(stmt.Statements) {
			code += sg.generateSurvivorFreeForScope()
		}
		code += sg.codegen.indentString() + "kmm_v4_mark_restore(_scope_start);\n"
		sg.codegen.ExitOffsetScope()
		sg.codegen.indent--
		code += indent + "}\n"
		sg.codegen.ExitScope()
		return code
	}

	// SOR 不活跃或不需要 KMM：退回到现有启发式
	// 修复 #20：删除批量 bump 优化路径（_batch_ptr 空转问题）
	// per-thread heap 已实现单次 CAS 批量获取的效果，无需额外优化
	_, _, mallocCount := sg.analyzeBlockMallocs(stmt.Statements)

	if mallocCount >= 1 {
		code := "{\n"
		sg.codegen.indent++
		sg.codegen.EnterOffsetScope()
		code += sg.codegen.indentString() + "kmm_v4_mark_t _scope_start = kmm_v4_mark_save();\n"
		code += sg.generateStmtsWithLiveness(stmt.Statements, nil)
		// 方案 A：per-object survivor 回收（在 offset_restore 前发射，末尾是 return 时跳过）
		if !lastStmtIsReturn(stmt.Statements) {
			code += sg.generateSurvivorFreeForScope()
		}
		code += sg.codegen.indentString() + "kmm_v4_mark_restore(_scope_start);\n"
		sg.codegen.ExitOffsetScope()
		sg.codegen.indent--
		code += indent + "}\n"
		sg.codegen.ExitScope()
		return code
	}

	if sg.codegen.IsInOffsetScope() {
		code := "{\n"
		sg.codegen.indent++
		code += sg.generateStmtsWithLiveness(stmt.Statements, nil)
		sg.codegen.indent--
		code += indent + "}\n"
		sg.codegen.ExitScope()
		return code
	}

	code := sg.codegen.KMMStart("")
	sg.codegen.indent++
	// 与实际发射的 scope_push 配对：块内 return 必须显式 pop 这一帧。
	sg.codegen.EnterKMMScope()
	code += sg.generateStmtsWithLiveness(stmt.Statements, nil)
	sg.codegen.ExitKMMScope()

	code += sg.codegen.indentString() + "// Free allocated memory\n"
	for name, symbol := range sg.codegen.currentScope.GetAllSymbols() {
		if symbol.Nullable {
			if symbol.Type == "string" || symbol.Type == "str" {
				code += sg.codegen.indentString()
				code += "if (" + name + ".ptr != NULL) { free(" + name + ".ptr); }\n"
			}
		}
	}
	// 方案 A：per-object survivor 回收（在 KMM_V4_SCOPE_END 前发射，末尾是 return 时跳过）
	if !lastStmtIsReturn(stmt.Statements) {
		code += sg.generateSurvivorFreeForScope()
	}

	sg.codegen.indent--
	code += indent + sg.codegen.KMMEnd()
	sg.codegen.ExitScope()
	return code
}

// === 结构化所有权基础层：完整 liveness 分析（从 SOR 下移的能力③）===

// collectVarsInExpr 递归遍历表达式，收集所有标识符（变量引用）到 out 集合。
// 完整覆盖所有 AST 表达式类型：Identifier、BinaryExpression、CallExpression、
// IndexExpression、MemberAccessExpression、UnaryExpression、PrefixCallExpression、
// TypeCastExpression、ParenExpression、ConditionalExpression、LambdaExpression、Literal。
func (sg *StatementGenerator) collectVarsInExpr(expr ast.Expression, out map[string]bool) {
	if expr == nil || out == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if e.Name != "" {
			out[e.Name] = true
		}
	case *ast.BinaryExpression:
		sg.collectVarsInExpr(e.Left, out)
		sg.collectVarsInExpr(e.Right, out)
	case *ast.CallExpression:
		sg.collectVarsInExpr(e.Function, out)
		for _, arg := range e.Args {
			sg.collectVarsInExpr(arg, out)
		}
	case *ast.IndexExpression:
		sg.collectVarsInExpr(e.Object, out)
		sg.collectVarsInExpr(e.Index, out)
	case *ast.MemberAccessExpression:
		sg.collectVarsInExpr(e.Object, out)
	case *ast.MemberExpression:
		sg.collectVarsInExpr(e.Object, out)
	case *ast.UnaryExpression:
		sg.collectVarsInExpr(e.Right, out)
	case *ast.PrefixCallExpression:
		for _, param := range e.Params {
			sg.collectVarsInExpr(param, out)
		}
		for _, bodyStmt := range e.Body {
			sg.collectVarsInStmt(bodyStmt, out)
		}
	case *ast.TypeCastExpression:
		sg.collectVarsInExpr(e.Expression, out)
	case *ast.ParenExpression:
		sg.collectVarsInExpr(e.Inner, out)
	case *ast.ConditionalExpression:
		sg.collectVarsInExpr(e.Condition, out)
		sg.collectVarsInExpr(e.TrueExpr, out)
		sg.collectVarsInExpr(e.FalseExpr, out)
	case *ast.LambdaExpression:
		for _, bodyStmt := range e.Body {
			sg.collectVarsInStmt(bodyStmt, out)
		}
	case *ast.LiteralExpression:
		// 字面量不引用变量
	case *ast.ArrayLiteral:
		for _, elem := range e.Elements {
			sg.collectVarsInExpr(elem, out)
		}
	}
}

// collectVarsInStmt 递归遍历语句，收集所有变量引用到 out 集合。
// 完整覆盖所有 AST 语句类型，包括嵌套块（if/else/while/for/block）。
func (sg *StatementGenerator) collectVarsInStmt(stmt ast.Statement, out map[string]bool) {
	if stmt == nil || out == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.VariableDeclaration:
		// 变量声明：记录 Name（定义点也算一次使用，这样声明后未使用的变量可在声明后立即释放）
		if s.Name != "" {
			out[s.Name] = true
		}
		if s.Value != nil {
			sg.collectVarsInExpr(s.Value, out)
		}
	case *ast.ExpressionStatement:
		sg.collectVarsInExpr(s.Expression, out)
	case *ast.ReturnStatement:
		if s.Value != nil {
			sg.collectVarsInExpr(s.Value, out)
		}
	case *ast.IfStatement:
		sg.collectVarsInExpr(s.Condition, out)
		for _, bodyStmt := range s.Body {
			sg.collectVarsInStmt(bodyStmt, out)
		}
		for _, elseStmt := range s.Else {
			sg.collectVarsInStmt(elseStmt, out)
		}
	case *ast.WhileStatement:
		sg.collectVarsInExpr(s.Condition, out)
		for _, bodyStmt := range s.Body {
			sg.collectVarsInStmt(bodyStmt, out)
		}
	case *ast.ForStatement:
		if s.Init != nil {
			sg.collectVarsInStmt(s.Init, out)
		}
		if s.Condition != nil {
			sg.collectVarsInExpr(s.Condition, out)
		}
		if s.Update != nil {
			sg.collectVarsInStmt(s.Update, out)
		}
		for _, bodyStmt := range s.Body {
			sg.collectVarsInStmt(bodyStmt, out)
		}
	case *ast.ForInStatement:
		sg.collectVarsInExpr(s.Iterable, out)
		for _, bodyStmt := range s.Body {
			sg.collectVarsInStmt(bodyStmt, out)
		}
	case *ast.BlockStatement:
		for _, bodyStmt := range s.Statements {
			sg.collectVarsInStmt(bodyStmt, out)
		}
	case *ast.CallStatement:
		sg.collectVarsInExpr(s.Target, out)
		for _, bodyStmt := range s.Body {
			sg.collectVarsInStmt(bodyStmt, out)
		}
	case *ast.YieldStatement:
		sg.collectVarsInExpr(s.Source, out)
		if s.Target != "" {
			out[s.Target] = true
		}
	case *ast.ReleaseStatement:
		sg.collectVarsInExpr(s.Source, out)
		for _, holder := range s.Holders {
			if holder != "" {
				out[holder] = true
			}
		}
	case *ast.ExtractStatement:
		sg.collectVarsInExpr(s.Source, out)
		sg.collectVarsInExpr(s.Index, out)
		if s.Target != "" {
			out[s.Target] = true
		}
	case *ast.SpendStatement:
		sg.collectVarsInExpr(s.Target, out)
		for _, call := range s.Calls {
			for _, bodyStmt := range call.Body {
				sg.collectVarsInStmt(bodyStmt, out)
			}
		}
	}
}

// computeLastUseIndex 计算块内每个指针变量最后一次被使用的语句索引。
// 正向扫描所有语句，对每个语句收集变量使用，更新最后使用索引。
// 返回 map[varName]lastStmtIndex，变量在块内从未使用时不包含在结果中。
func (sg *StatementGenerator) computeLastUseIndex(stmts []ast.Statement, pointerVars map[string]bool) map[string]int {
	lastUse := make(map[string]int)
	for i, stmt := range stmts {
		used := make(map[string]bool)
		sg.collectVarsInStmt(stmt, used)
		for name := range used {
			if pointerVars[name] {
				lastUse[name] = i
			}
		}
	}
	return lastUse
}

// collectBlockPointerVars 收集当前作用域内声明的所有指针类型变量名。
// 用于 liveness 分析确定哪些变量需要提前释放。
func (sg *StatementGenerator) collectBlockPointerVars() map[string]bool {
	pointerVars := make(map[string]bool)
	currentScope := sg.codegen.GetCurrentScope()
	if currentScope == nil {
		return pointerVars
	}
	for name := range currentScope.GetAllSymbols() {
		if sg.isPointerTyped(name) || sg.isStringTyped(name) {
			pointerVars[name] = true
		}
	}
	return pointerVars
}

// generateEarlyFreeForStmtIndex 生成在指定语句索引后应提前释放的变量的 survivor_free 代码。
// lastUse: varName -> 最后使用语句索引；stmtIndex: 当前语句索引；
// freed: 已释放变量集合（防止重复释放）；alwaysLive: 始终存活的变量集合（不提前释放）；
// excludeName: 排除的变量名（如 return 值）。
func (sg *StatementGenerator) generateEarlyFreeForStmtIndex(lastUse map[string]int, stmtIndex int, freed map[string]bool, alwaysLive map[string]bool, excludeName string) string {
	var b strings.Builder
	indent := sg.codegen.indentString()
	for name, idx := range lastUse {
		// 别名感知：同一分配的多个别名变量必须整体延迟到组内所有成员
		// 的最后使用点才能释放，否则先死的别名会触发对仍在使用的内存的
		// use-after-free；且整组只通过代表释放一次，防止 double-free。
		group := sg.codegen.GetVarAliasGroup(name)
		effIdx := idx
		if len(group) >= 2 {
			for _, member := range group {
				if mi, ok := lastUse[member]; ok && mi > effIdx {
					effIdx = mi
				}
			}
		}
		if effIdx != stmtIndex {
			continue
		}
		if freed[name] {
			continue
		}
		if name == excludeName {
			continue
		}
		// 线程逃逸修复：函数参数（借用，所有权属调用方）与全局（模块持有）
		// 绝不提前释放——参数可被跨线程异步读取，全局可被多线程共享。
		if sym := sg.codegen.GetSymbol(name); sym != nil &&
			(sym.Scope == "parameter" || sym.Scope == "global") {
			continue
		}
		// 始终存活的变量不提前释放（如循环条件中使用的变量）
		if alwaysLive != nil && alwaysLive[name] {
			continue
		}
		if len(group) >= 2 {
			// 线程逃逸修复：别名组内任一成员是参数/全局 → 整组不提前释放
			//（组指向调用方/模块所有权的内存，本函数无权回收）。
			skipGroup := false
			for _, m := range group {
				if sm := sg.codegen.GetSymbol(m); sm != nil &&
					(sm.Scope == "parameter" || sm.Scope == "global") {
					skipGroup = true
					break
				}
			}
			if skipGroup {
				continue
			}
			rep := sg.selectAliasGroupRepresentative(group)
			if rep != name {
				freed[name] = true
				// 代表已逃逸/promote/extern 来源时，整组所有权已移出或不透明，
				// 标记成员以抑制 return/scope 回收路径的重复释放
				if sg.codegen.IsAssignEscaped(rep) || sg.codegen.IsPromoted(rep) ||
					sg.codegen.GetVarOrigin(rep) == OriginExtern {
					sg.codegen.MarkEarlyFreed(name)
				}
				continue
			}
		}
		// 已逃逸或已 promote 的变量不提前释放（所有权已转移）
		if sg.codegen.IsAssignEscaped(name) || sg.codegen.IsPromoted(name) {
			freed[name] = true
			continue
		}
		// 方案 A'（链首保活）：函数级预扫描判定该变量是**写穿容器的链首别名**时
		// 不提前释放。
		//
		// 典型场景 `i64 head = as<i64>(cur); ...; return head`：head 是地址值、
		// 与 cur 之间没有指针别名对，逃逸传播不会把 cur 标为 assign-escaped；
		// 但 cur 此刻**正是链首所在节点**，liveness 在此处的最后使用点
		// （`cur = nd` 之前的写穿）发射 survivor_free 就会释放链首，
		// 下一轮分配回收该槽位 → 链首被覆写（n 大时才崩，n 小时 slab 未耗尽）。
		//
		// 注意：这里刻意只查 ChainEscaping（"链首被带出函数"这一精确判据），
		// **不能**放宽到整个 escapedVars。escapedVars 是词法粗判的逃逸集合，
		// 含大量"只是出现在 return/实参表达式里、但对象本身仍归本函数"的场景
		// （如 `nodes` 是循环内作用域缓冲、只在 build/inorder 里被借用），
		// 用它当闸门会误伤这些变量的正常提前释放 → 作用域缓冲不再回收，
		// RSS 随轮数暴涨（alloc_bench [sor] 实测 9.9MB → 7GB 后被 OOM kill）。
		if sg.codegen.IsChainEscaping(name) {
			freed[name] = true
			continue
		}
		// 方案 A'：已挂在链上的对象不提前释放。
		// 对象经写穿（cur.next = nd）由容器持有，游标变量随后不再引用它；
		// liveness 若按游标处的最后使用点判定就会把它当垃圾回收，
		// 而它其实还在链上被读 → use-after-free。豁免之。
		if sg.codegen.IsChainLinked(name) {
			freed[name] = true
			continue
		}
		// 多分配器 origin 追踪：extern 来源不释放，libc 用 free，kmm 用 survivor_free
		origin := sg.codegen.GetVarOrigin(name)
		if origin == OriginExtern {
			freed[name] = true
			continue
		}
		// String 类型：SSO 感知释放（只有 len>15 才释放堆指针）
		if sg.isStringTyped(name) {
			b.WriteString(indent)
			b.WriteString("string_survivor_free(")
			b.WriteString(name)
			b.WriteString("); /* structural: early free (liveness, String, SSO-aware) */\n")
			sg.codegen.MarkEarlyFreed(name)
		} else if origin == OriginLibcMalloc {
			b.WriteString(indent)
			b.WriteString("free(")
			b.WriteString(name)
			b.WriteString("); /* structural: early free (liveness, libc_malloc) */\n")
			sg.codegen.MarkEarlyFreed(name)
		} else {
			// kmm 或 unknown 来源：用 survivor_free（对非 KMM 指针是安全 no-op）
			b.WriteString(indent)
			b.WriteString("kmm_v4_survivor_free(")
			b.WriteString(name)
			b.WriteString("); /* structural: early free (liveness, kmm) */\n")
			sg.codegen.MarkEarlyFreed(name)
		}
		// 代表释放后标记全组成员，防止 return/scope 回收路径重复释放别名
		if len(group) >= 2 {
			for _, member := range group {
				if member != name {
					sg.codegen.MarkEarlyFreed(member)
					freed[member] = true
				}
			}
		}
		freed[name] = true
	}
	return b.String()
}

// selectAliasGroupRepresentative 为别名组选择释放代表：
// 优先选择具有分配来源（kmm/libc_malloc）的成员，保证释放函数与分配方式匹配；
// 无分配来源成员或多候选时按名称排序取第一个，确保生成代码确定性。
func (sg *StatementGenerator) selectAliasGroupRepresentative(group []string) string {
	members := make([]string, len(group))
	copy(members, group)
	sort.Strings(members)
	rep := members[0]
	for _, m := range members {
		origin := sg.codegen.GetVarOrigin(m)
		if origin == OriginKmm || origin == OriginLibcMalloc {
			rep = m
			break
		}
	}
	return rep
}

// isReleaseCallName 判断调用名是否为"释放类"函数（free / xxx_free）。
// Kaula 侧释放入口都遵循该命名（std.memory.kmm_v4_free、kmm_v4_global_free、
// string_free、fast_free、std_free、libc free 等）。
func isReleaseCallName(name string) bool {
	return name == "free" || strings.HasSuffix(name, "_free")
}

// calledFuncName 取调用表达式的最后一段函数名（std.memory.kmm_v4_free → kmm_v4_free）。
func calledFuncName(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.CallExpression:
		return calledFuncName(e.Function)
	case *ast.MemberAccessExpression:
		return e.Member
	case *ast.Identifier:
		return e.Name
	}
	return ""
}

// argPointerNames 收集调用实参中的裸指针变量名（跳过 as<T>(x) 强转的包装）。
func argPointerNames(args []ast.Expression, out map[string]bool) {
	for _, arg := range args {
		switch a := arg.(type) {
		case *ast.Identifier:
			out[a.Name] = true
		case *ast.TypeCastExpression:
			if id, ok := a.Expression.(*ast.Identifier); ok {
				out[id.Name] = true
			}
		}
	}
}

// collectUserFreedVars 预扫描块内语句，找出用户显式传给释放类函数的指针变量。
// 这些指针的所有权已交给用户代码（如 std.memory.kmm_v4_global_free(p)），
// 结构化提前释放必须跳过，否则同一指针被释放两次：
//   - slab 路径：同一槽位被重复推入垃圾箱，flush 时 allocated 被多减，
//     桶容量统计失真，逐步升级到更大档乃至页式分配；
//   - libc 路径：直接 double free。
//
// 只统计"必然执行一次"的调用位置（语句本身、if 条件）；循环体与 if 分支内的
// 释放是条件性的，跳过后会漏回收，故不计入（保持原有行为，不引入泄漏回归）。
func (sg *StatementGenerator) collectUserFreedVars(stmts []ast.Statement) map[string]bool {
	out := make(map[string]bool)
	for _, stmt := range stmts {
		sg.collectUserFreedVarsInStmt(stmt, out)
	}
	return out
}

func (sg *StatementGenerator) collectUserFreedVarsInStmt(stmt ast.Statement, out map[string]bool) {
	if stmt == nil || out == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		if call, ok := s.Expression.(*ast.CallExpression); ok &&
			isReleaseCallName(calledFuncName(call.Function)) {
			argPointerNames(call.Args, out)
		}
	case *ast.IfStatement:
		// if free(p) { ... }：条件必然求值一次 → 视为用户释放
		if call, ok := s.Condition.(*ast.CallExpression); ok &&
			isReleaseCallName(calledFuncName(call.Function)) {
			argPointerNames(call.Args, out)
		}
	case *ast.VariableDeclaration:
		// 不处理：声明处的初始化表达式不是释放点
	}
}

// CollectUserFreedVars 供函数体生成入口预扫描：返回块内被用户显式释放的指针变量名。
func (sg *StatementGenerator) CollectUserFreedVars(stmts []ast.Statement) map[string]bool {
	return sg.collectUserFreedVars(stmts)
}

// generateStmtsWithLiveness 生成语句序列，并在每个变量最后一次使用后插入提前释放。
// 这是结构化所有权基础层的 liveness 分析（从 SOR 下移的能力③）。
// alwaysLive: 始终存活的变量集合（如循环条件中使用的变量），这些变量不会被提前释放。
// 返回生成的代码字符串（不含外层缩进，调用方负责添加缩进）。
func (sg *StatementGenerator) generateStmtsWithLiveness(stmts []ast.Statement, alwaysLive map[string]bool) string {
	// 预扫描语句收集指针变量（变量符号在 generateStatement 时才加入符号表，所以需要预扫描）
	pointerVars := sg.collectPointerVarsFromStmts(stmts)
	// 合并当前作用域中已有的指针变量（如函数参数、外层作用域变量）
	for name := range sg.collectBlockPointerVars() {
		pointerVars[name] = true
	}
	lastUse := sg.computeLastUseIndex(stmts, pointerVars)
	freed := make(map[string]bool)
	// 用户已显式释放的指针：所有权已转移，结构化提前释放与作用域回收都要跳过，
	// 否则同一指针被释放两次（MarkEarlyFreed 同时抑制 return/scope 路径）。
	for name := range sg.collectUserFreedVars(stmts) {
		freed[name] = true
		sg.codegen.MarkEarlyFreed(name)
		sg.codegen.MarkUserFreed(name)
	}

	var b strings.Builder
	for i, bodyStmt := range stmts {
		/*
		 * 守卫模式：每条语句包进一个恢复槽位。
		 *
		 * 这里不做提前释放 —— 提前释放按「语句序号」决定，而守卫槽位会把
		 * 恢复路径截断在中途，提前释放的判定拿不到完整的后续语句序列。
		 * 少释放是可观测的保守退化（作用域出口照常回收），重复释放会
		 * double-free，所以宁可不做。
		 */
		if sg.codegen.concGuardActive() {
			if vd, ok := bodyStmt.(*ast.VariableDeclaration); ok && vd != nil {
				b.WriteString(sg.concGuardedDecl(vd))
				continue
			}
			/*
			 * 体内含挂起点的复合语句：恢复点在那条语句内部，所以本层的槽位
			 * 不能在进入时就推进 —— 推进了恢复就会整条跳过它。
			 */
			if concIsResumableCompound(bodyStmt) {
				b.WriteString(sg.concGuardHold(func() string {
					return sg.codegen.generateStatement(bodyStmt)
				}))
				continue
			}
			sg.codegen.concCheckLeafStmt(bodyStmt)
			b.WriteString(sg.concGuardSlot(func() string {
				return sg.codegen.generateStatement(bodyStmt)
			}))
			continue
		}
		b.WriteString(sg.codegen.indentString())
		b.WriteString(sg.codegen.generateStatement(bodyStmt))
		// F8 修复：return/break/continue 会跳过后续代码，不应在其后生成 early free（dead code）
		if _, isReturn := bodyStmt.(*ast.ReturnStatement); isReturn {
			continue
		}
		if _, isBreak := bodyStmt.(*ast.BreakStatement); isBreak {
			continue
		}
		if _, isContinue := bodyStmt.(*ast.ContinueStatement); isContinue {
			continue
		}
		// 在该语句后插入提前释放（变量在此处最后一次使用，且不在 alwaysLive 中）
		b.WriteString(sg.generateEarlyFreeForStmtIndex(lastUse, i, freed, alwaysLive, ""))
	}
	return b.String()
}

// collectPointerVarsFromStmts 预扫描语句列表，收集所有声明的指针类型变量名。
// 用于 liveness 分析（变量符号在 generateStatement 时才加入符号表，所以需要预扫描）。
func (sg *StatementGenerator) collectPointerVarsFromStmts(stmts []ast.Statement) map[string]bool {
	pointerVars := make(map[string]bool)
	for _, stmt := range stmts {
		if varDecl, ok := stmt.(*ast.VariableDeclaration); ok {
			if varDecl.Name != "" {
				kaulaType := varDecl.Type
				if kaulaType == "" && varDecl.Value != nil {
					// 类型推断：从初始化表达式推断（简化处理，只处理显式类型）
					continue
				}
				if kaulaType != "" {
					cType := sg.codegen.typeGenerator.convertType(kaulaType, false)
					if cType != "" && strings.HasSuffix(cType, "*") {
						pointerVars[varDecl.Name] = true
					}
					if kaulaType == "string" || kaulaType == "str" || kaulaType == "String" || cType == "String" {
						pointerVars[varDecl.Name] = true
					}
				}
			}
		}
	}
	return pointerVars
}

// mallocAnalysis 分析结果结构
type mallocAnalysis struct {
	count        int   // malloc 调用总数
	totalSize    int   // 所有 malloc 大小之和（仅计算已知大小）
	allKnown     bool  // 是否所有 malloc 大小都已知
	sizes        []int // 每个 malloc 的大小（0 表示未知）
	isContiguous bool  // malloc 是否连续出现（中间无其他语句）
}

// analyzeBlockMallocs 分析块语句中的 malloc 调用，判断是否可以批量分配
// 返回更详细的分配信息以支持激进的单对象优化
func (sg *StatementGenerator) analyzeBlockMallocs(stmts []ast.Statement) (canBatch bool, totalSize int, count int) {
	analysis := sg.analyzeBlockMallocsDetailed(stmts)
	return analysis.allKnown && analysis.count >= 1, analysis.totalSize, analysis.count
}

// analyzeBlockMallocsDetailed 返回详细的 malloc 分析结果
func (sg *StatementGenerator) analyzeBlockMallocsDetailed(stmts []ast.Statement) mallocAnalysis {
	result := mallocAnalysis{
		count:        0,
		totalSize:    0,
		allKnown:     true,
		sizes:        make([]int, 0),
		isContiguous: true,
	}

	lastWasMalloc := false
	hasOtherStmts := false

	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}

		isMalloc := false
		sizeBytes := 0

		switch s := stmt.(type) {
		case *ast.VariableDeclaration:
			if s.Value != nil {
				// 检查是否是 malloc 类调用（包括 std.memory.kmm_v4_malloc）
				if call, ok := s.Value.(*ast.CallExpression); ok {
					if sg.isMallocCallExpr(call) {
						isMalloc = true
						sizeBytes = sg.extractMallocSizeBytes(call)
					}
				}
			}
		case *ast.ExpressionStatement:
			if s.Expression != nil {
				// 检查是否是 malloc 类调用（包括 std.memory.kmm_v4_malloc）
				if call, ok := s.Expression.(*ast.CallExpression); ok {
					if sg.isMallocCallExpr(call) {
						isMalloc = true
						sizeBytes = sg.extractMallocSizeBytes(call)
					}
				}
			}
		}

		if isMalloc {
			result.count++
			result.sizes = append(result.sizes, sizeBytes)
			if sizeBytes > 0 {
				result.totalSize += sizeBytes
			} else {
				result.allKnown = false
			}

			// 检查连续性：如果之前有非 malloc 语句，则不连续
			if hasOtherStmts && !lastWasMalloc {
				result.isContiguous = false
			}
			lastWasMalloc = true
			hasOtherStmts = false
		} else {
			hasOtherStmts = true
			lastWasMalloc = false
		}
	}

	return result
}

// isMallocCallExpr 检查表达式是否是 KMM 分配调用
// std_malloc 仅在 freestanding 模式下属于 KMM 分配（被重写为 kmm_v4_alloc_auto）；
// hosted 模式下它是系统 malloc，与 KMM 池严格区分
func (sg *StatementGenerator) isMallocCallExpr(call *ast.CallExpression) bool {
	if call == nil || call.Function == nil {
		return false
	}
	isFree := sg.codegen.IsFreestanding()
	switch fn := call.Function.(type) {
	case *ast.Identifier:
		return (fn.Name == "std_malloc" && isFree) || fn.Name == "kmm_v4_malloc" || fn.Name == "kmm_v4_alloc_auto"
	case *ast.MemberAccessExpression:
		return (fn.Member == "std_malloc" && isFree) || fn.Member == "kmm_v4_malloc"
	}
	return false
}

// extractMallocSizeBytes 从 malloc 调用中提取编译期可确定的大小
func (sg *StatementGenerator) extractMallocSizeBytes(call *ast.CallExpression) int {
	if call == nil || len(call.Args) == 0 {
		return 0
	}
	arg := call.Args[0]

	// 整数字面量：直接返回
	if lit, ok := arg.(*ast.IntegerLiteral); ok {
		return int(lit.Value)
	}

	// sizeof 表达式：尝试从类型系统获取大小
	if sizeOf, ok := arg.(*ast.SizeOfExpression); ok {
		// 使用 TypeGenerator 获取类型大小
		if size, ok := sg.codegen.typeGenerator.GetTypeSize(sizeOf.TargetType); ok {
			return size
		}
		// 如果无法确定大小，返回 0（让优化走 offset_save/restore 路径）
		return 0
	}

	return 0
}

// generateYieldStatement 生成 yield 语句代码
// 语法: yield source -> target
// 生成: /* SOR: yield source -> target */ target = source; source = NULL;
// 优化: 当源表达式是字面量 0 时，yield 是纯死代码（target = 0, source = 0 → no-op），直接跳过
// 修复 #25: 跨作用域 yield 时在转移点前插入 kmm_v4_scope_promote_p（提升到全局幸存段），
// 否则源作用域 scope_pop 回卷后新持有者持有的指针悬垂。
func (sg *StatementGenerator) generateYieldStatement(stmt *ast.YieldStatement) string {
	srcCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Source)

	// 死代码消除：源为字面量 0 的 yield 是无操作，跳过生成
	if srcCode == "0" {
		return ""
	}

	var b strings.Builder
	b.WriteString("/* SOR: yield ")
	b.WriteString(srcCode)
	b.WriteString(" -> ")
	b.WriteString(stmt.Target)
	b.WriteString(" */\n")
	b.WriteString(stmt.Target)
	b.WriteString(" = ")
	b.WriteString(srcCode)
	b.WriteString(";\n")
	// 修复 #25：跨作用域所有权提升（必须在 source = 0 之前拷贝）
	if srcIdent, isSrcIdent := stmt.Source.(*ast.Identifier); isSrcIdent {
		// 源对象大小登记传播给目标（即使本次不提升，后续 return 提升也需要）
		sg.codegen.PropagateInitSize(stmt.Target, srcIdent.Name)
		if sg.sorNeedsCrossScopePromote(srcIdent.Name, stmt.Target) {
			// 任务①：String 走 string_clone_global（emitPromoteForType 内部分支），
			// 不需 sorPromoteSize；指针/聚合走大小解析。
			sizeExpr := ""
			if sg.isStringTyped(stmt.Target) {
				sizeExpr = "sizeof(String)"
			} else if size, ok := sg.sorPromoteSize(srcIdent.Name, srcCode); ok {
				sizeExpr = size
			} else {
				sizeExpr = "sizeof(*" + srcCode + ")"
			}
			// promote 被跳过时保持普通所有权转移（不标记 promote）
			if line := sg.emitPromoteForType(stmt.Target, sizeExpr, "promote cross-scope", srcIdent.Name); line != "" {
				b.WriteString(line)
				sg.markCrossScopePromote(stmt.Target)
			}
		}
	}
	b.WriteString(srcCode)
	b.WriteString(" = 0; /* SOR: ownership moved */\n")
	return b.String()
}

// generateAssignEscapeStatement 处理 dst = src 标识符赋值的跨作用域逃逸
// （KMM 作用域回卷导致声明丢失的修复）。
//
// 背景：块级 KMM 作用域（KMM_V4_SCOPE_START/END、offset_save/restore）退出时
// 会回卷 bump 指针，回收块内 kmm_v4 分配的对象。yield/return/调用实参都有
// promote 补救路径，但普通赋值 dst = src 此前没有任何处理——块内分配的对象
// 经赋值逃逸到外层局部变量或全局后，作用域回卷即产生悬垂：
// 声明还在、内容被后续分配覆写（静默数据损坏，无任何告警）。
//
// 修复：赋值前先把源对象提升到幸存段（kmm_v4_scope_promote_p 系列会改写 src
// 指针指向幸存副本），再执行普通赋值——两个持有者都指向幸存内存，回卷无害。
// 同时把 src 登记为 assign-escaped，抑制其块尾 per-object survivor_free
// （否则会释放幸存副本，新持有者照样悬垂）。
//
// 返回生成的语句代码；不构成逃逸时返回 ""（调用方走原路径）。
func (sg *StatementGenerator) generateAssignEscapeStatement(stmt *ast.ExpressionStatement) string {
	bin, ok := stmt.Expression.(*ast.BinaryExpression)
	if !ok || (bin.Operator != "=" && bin.Operator != "ASSIGN") {
		return ""
	}
	// 线程逃逸修复：dst = thread_create(...) / dst = thread_pool_add_task(...) 赋值路径。
	// 实参对象被另一线程异步使用（与语句调用路径相同的 UAF 风险）——
	// 提升到幸存段并抑制提前回收后再执行赋值。
	if callExpr, okCall := bin.Right.(*ast.CallExpression); okCall {
		if prefix, okSpawn := sg.handleThreadSpawnEscape(callExpr); okSpawn {
			return prefix + sg.codegen.expressionGenerator.GenerateExpression(stmt.Expression) + ";\n"
		}
	}
	srcIdent, okSrc := unwrapAssignSource(bin.Right)
	if !okSrc {
		return ""
	}
	// 源必须是指针/String 类型的局部变量
	if !sg.isPointerTyped(srcIdent.Name) && !sg.isStringTyped(srcIdent.Name) {
		return ""
	}
	if !sg.codegen.IsVarDeclaredInCurrentScope(srcIdent.Name) {
		return ""
	}
	// 已逃逸或已 promote 的变量不重复处理
	if sg.codegen.IsAssignEscaped(srcIdent.Name) || sg.codegen.IsPromoted(srcIdent.Name) {
		return ""
	}

	// 判断目标类型，决定是否需要 promote
	needsPromote := false
	dstName := ""
	// containerName：真正"持有"promote 副本的那个变量名。
	// 普通赋值就是目标变量本身；写穿（obj.field / arr[i]）则是容器变量。
	// 它决定 promote 属于"内部"还是"向外"——见下方 funcEmitsInternalPromote。
	containerName := ""

	switch dst := bin.Left.(type) {
	case *ast.Identifier:
		// 普通变量赋值：使用现有的跨作用域判断
		dstName = dst.Name
		containerName = dst.Name
		// 方案 A'：游标推进（cur = nd）——若目标是被写穿过的安全局部容器，
		// 则不 promote。理由见 docs/perf/P0-survivor-leak.md §6：
		// 写穿（cur.next = nd）已让整条链落在 bump 区，游标推进若单独 promote，
		// 就会把"链上每个节点"逐一搬到 survivor 段（泄漏），且链条存活方式不一致
		// （容器在 bump 区、节点在 survivor 段）导致回卷后链断裂。
		// 目标不在集合内（形参/全局/未写穿容器等）→ 保持旧行为。
		if sg.codegen.IsWrittenThroughContainer(dst.Name) {
			sg.codegen.MarkSuppressLoopRewind()
			return ""
		}
		needsPromote = sg.needsAssignEscapePromote(srcIdent.Name, dst.Name)
	case *ast.MemberAccessExpression:
		// 存入结构体字段：obj.field = ptr
		// 目标对象（obj）的生命周期通常比源更长（外层变量/全局/参数），保守 promote
		// 如果 obj 是当前作用域声明的局部变量且与源同层，可能不需要 promote，但保守处理
		dstName = "struct_field"
		containerName = "struct_field"
		needsPromote = true
		// 例外：如果 obj 是当前作用域声明的局部变量（与源同层或更内层），且不是指针类型，可能不需要 promote
		if objIdent, ok := dst.Object.(*ast.Identifier); ok {
			containerName = objIdent.Name
			// 方案 A'：写穿进「安全局部容器」时不 promote。
			//
			// 该容器从不逃逸，其对象在函数出口随 scope_pop 一锅端回收；
			// 留在 bump 区比 promote 到 survivor 段更优——既避免每轮新增一份
			// 幸存副本（survivor 段单调泄漏），也让整条链在同一 bump 段连续
			// 排布、不被循环回卷后的复用分配覆盖（链损坏）。
			// 对应的 mark_restore 抑制见 generateWhileStatement（A'3）。
			if sg.codegen.IsSafeLocalContainer(objIdent.Name) {
				sg.codegen.MarkSuppressLoopRewind()
				return ""
			}
			if sg.codegen.IsVarDeclaredInCurrentScope(objIdent.Name) {
				// obj 是当前作用域局部变量，检查其声明深度
				if srcDepth, srcOk := sg.codegen.declScopeDepth[srcIdent.Name]; srcOk {
					if dstDepth, dstOk := sg.codegen.declScopeDepth[objIdent.Name]; dstOk {
						if dstDepth >= srcDepth {
							// obj 与源同层或更内层，结构体生命周期不超过源，不需要 promote
							needsPromote = false
						}
					}
				}
			}
		}
	case *ast.IndexExpression:
		// 存入数组元素：arr[i] = ptr
		// 目标数组（arr）的生命周期通常比源更长，保守 promote
		dstName = "array_element"
		containerName = "array_element"
		needsPromote = true
		// 例外：如果 arr 是当前作用域声明的局部变量且与源同层或更内层，可能不需要 promote
		if arrIdent, ok := dst.Object.(*ast.Identifier); ok {
			containerName = arrIdent.Name
			// 方案 A'：同 MemberAccessExpression，写穿进安全局部容器不 promote。
			if sg.codegen.IsSafeLocalContainer(arrIdent.Name) {
				sg.codegen.MarkSuppressLoopRewind()
				return ""
			}
			if sg.codegen.IsVarDeclaredInCurrentScope(arrIdent.Name) {
				if srcDepth, srcOk := sg.codegen.declScopeDepth[srcIdent.Name]; srcOk {
					if dstDepth, dstOk := sg.codegen.declScopeDepth[arrIdent.Name]; dstOk {
						if dstDepth >= srcDepth {
							needsPromote = false
						}
					}
				}
			}
		}
	default:
		return ""
	}

	if !needsPromote {
		return ""
	}

	sizeExpr := ""
	if sg.isStringTyped(srcIdent.Name) {
		sizeExpr = "sizeof(String)"
	} else if size, ok := sg.sorPromoteSize(srcIdent.Name, srcIdent.Name); ok {
		sizeExpr = size
	} else {
		sizeExpr = "sizeof(*" + srcIdent.Name + ")"
	}
	var b strings.Builder
	// 目标变量已持有上一份幸存副本（第二轮及以后）：必须先释放旧副本再挂新副本。
	//
	// 这是 survivor 段单调增长的直接成因。动态池下 kmm_v4_survivor_rewind 只是
	// flush「已 free 的延迟槽位」构成的 TLS 垃圾箱，**不会**回收从未 free 的存活
	// 槽位；而 assign-escaped 的源变量被抑制了块尾 survivor_free，于是循环里
	// 每轮 promote 一份副本、旧副本无人接管 —— 副本数与迭代次数等比例增长。
	// 典型触发写法即 `keep = p` / `cur = nd` 这类游标推进。
	//
	// survivor_free 对 NULL 与非 survivor 段指针（作用域段对象）均返回 false 且
	// 无副作用，故首次赋值（目标还是 NULL 或指向作用域段）时调用也安全。
	if sg.canReleasePreviousSurvivorCopy(dstName, containerName) {
		b.WriteString(sg.codegen.indentString())
		b.WriteString("kmm_v4_survivor_free(" + containerName + "); /* release previous survivor copy */\n")
	}
	// promote 被跳过（拷贝大小不可信）时：不能抑制 src 的 free，也不能标记
	// funcEmitsPromote——否则既没有幸存副本，又关掉了函数出口的回收。
	// 直接返回 "" 让调用方回退到普通赋值路径。
	if line := sg.emitPromoteForType(srcIdent.Name, sizeExpr, "promote assign-escape into "+dstName, srcIdent.Name); line == "" {
		return ""
	} else {
		b.WriteString(line)
	}
	// 方案 C 分类：promote 副本留在函数内 vs 随目标逃出函数。
	//
	// 只有「向外」（副本的所有权随目标离开本函数）才置 funcEmitsPromote
	// 抑制函数出口的 survivor_rewind——否则 rewind 会回卷掉已交出去的副本。
	//
	// 若副本的持有者是本函数体内的局部变量/局部容器（containerName 是函数局部
	// 且非形参），则函数返回时该副本连同持有者一起成为垃圾，属于「内部」promote：
	// 记到 funcEmitsInternalPromote，**不**抑制 rewind。
	// 这里若一律按向外处理，循环里每轮 promote 一份、出口又不回卷，
	// survivor 段就单调增长（`keep = p` / `cur = nd` 这类游标写法即典型），
	// 等价于内存泄漏。
	sg.markCrossScopePromoteFor(containerName, dstName == "struct_field" || dstName == "array_element")
	sg.codegen.MarkAssignEscaped(srcIdent.Name) // 抑制 src 的块尾 per-object free
	sg.codegen.MarkPromoted(srcIdent.Name)      // reaching definition：防止 return 时重复 promote
	if dstName != "" && dstName != "struct_field" && dstName != "array_element" {
		sg.codegen.PropagateInitSize(dstName, srcIdent.Name)
	}
	b.WriteString(sg.codegen.expressionGenerator.GenerateExpression(stmt.Expression))
	b.WriteString(";\n")
	return b.String()
}

// unwrapAssignSource 从赋值右值里取出"被赋值的源标识符"，穿透纯地址转换包装。
//
// 修正如 `cur.next = as<i64>(nd)` / `g = as<void*>(p)` / `x = (p)` 这类右值：
// 它们本质上是"把 nd 这个对象挂出去"，但 AST 上是 TypeCastExpression /
// ParenExpression 而非 Identifier。此前直接用 `bin.Right.(*ast.Identifier)`
// 断言，这类写法一律断言失败 → 整个赋值逃逸路径提前 return →
// **promote 从不发射**（静默缺陷：跨作用域场景下产生悬垂）。
//
// 只穿透**不改变对象身份**的包装：
//   - as<T>(expr)：唯一允许的强转形式，运行时零开销（直接映射 C 的 ((T)(e))）；
//   - (expr)：ParenExpression，字段名是 Inner。
//
// 不穿透成员访问 / 索引 / 调用——那些返回的是别的对象（或新对象），
// 不是"本次赋值把源对象交出去"这一语义。
func unwrapAssignSource(expr ast.Expression) (*ast.Identifier, bool) {
	for expr != nil {
		switch e := expr.(type) {
		case *ast.Identifier:
			return e, true
		case *ast.ParenExpression:
			expr = e.Inner
		case *ast.TypeCastExpression:
			expr = e.Expression
		default:
			return nil, false
		}
	}
	return nil, false
}

// canReleasePreviousSurvivorCopy 判断能否在挂新副本前释放目标持有的旧副本。
//
// 注意这里判断的是**要不要发射这条 free 语句**，而不是"运行期第几轮"。
// 循环体在编译期只生成一份代码、运行期执行 N 次，因此 free 必须无条件发射，
// 让它在每一轮都释放上一轮的旧副本；首轮目标是 NULL 或指向作用域段，
// kmm_v4_survivor_free 对这两种情况都返回 false 且无副作用。
//
// 五个条件缺一不可：
//  1. 必须是"给变量重新赋值"（dstName 是普通变量名），不能是写穿
//     （struct_field / array_element）。写穿时 containerName 是容器变量、
//     但被改写的是容器的**某个字段/元素**而不是容器本身——释放容器会
//     连带释放容器指向的整个对象，而紧接着的 `cur->next = nd` 还要往里写，
//     直接变成 use-after-free（`cur->next = nd` 这类链表构建即典型）。
//  2. 目标是本函数局部变量——形参/全局指向的内存不归本函数回收；
//  3. 目标没有把副本分给别人——一旦发生过 `old = keep`（源被登记为
//     assign-escaped / promoted），旧副本就另有持有者，释放会让 old 悬垂。
//     此时宁可不回收（保守退化到修复前的行为），也不能引入 use-after-free；
//  4. 本次赋值确实是"跨作用域"（当前块深于目标声明块）——即典型的
//     循环内赋值 / 块内赋值给外层变量，这正是副本数随迭代次数累积的场景。
//     同层赋值一轮只 promote 一次，不存在累积，无需多发射一次 free。
func (sg *StatementGenerator) canReleasePreviousSurvivorCopy(dstName, holder string) bool {
	// 条件 1：写穿目标一律不放行（容器不是被替换的对象）
	if dstName == "" || dstName == "struct_field" || dstName == "array_element" {
		return false
	}
	if holder == "" || holder == "struct_field" || holder == "array_element" {
		return false
	}
	if !sg.codegen.IsFunctionLocalVar(holder) {
		return false
	}
	if sg.codegen.IsAssignEscaped(holder) || sg.codegen.IsPromoted(holder) {
		return false
	}
	holderDepth, recorded := sg.codegen.declScopeDepth[holder]
	if !recorded {
		return false
	}
	return sg.codegen.scopeDepth > holderDepth
}

// markCrossScopePromote 登记一次「跨作用域 promote」，并把它归类为内部或向外。
//
// 分类的判据是：promote 出来的幸存副本的持有者 holder 是否留在本函数内。
//   - holder 是本函数体内声明的局部变量（且非形参）→ 内部 promote：
//     函数返回时副本连同持有者一起成为垃圾，函数出口必须仍能 survivor_rewind
//     回收它，故只置 funcEmitsInternalPromote（不抑制 rewind）。
//   - 否则（全局 / 形参 / 未记录的外部内存）→ 向外 promote：
//     副本在本函数返回后仍被外部持有，置 funcEmitsPromote 抑制 rewind，
//     否则回卷会把外部持有的副本一并回收，留下悬垂指针。
//
// 内部 promote 若不区分出来、一律按向外处理，循环体内每轮 promote 一份而出口
// 又不回卷，survivor 段就会单调增长——`keep = p`、`cur = nd` 这类游标写法
// 会退化成事实上的内存泄漏。
//
// 写穿场景的补充判据（containerName 是容器变量、dstName 是 "struct_field"/
// "array_element"）：副本存活于**容器的字段/元素**里，其生命周期取决于
// **容器**是否逃逸，而不是容器是否为函数局部。`Node* cur = ...; Node* head =
// cur; ...; return as<i64>(head)` 里 cur 是函数局部、但整条链随 head 逃逸——
// 此时若按"局部容器 → 内部 promote"处理，函数出口的 survivor_rewind 会把
// return 出去的链一并回收，调用方拿到悬垂指针（读链即 SIGSEGV）。
// 故写穿时必须先查容器的逃逸性：容器逃逸 → 按向外 promote。
func (sg *StatementGenerator) markCrossScopePromote(holder string) {
	sg.markCrossScopePromoteFor(holder, false)
}

// markCrossScopePromoteFor 是 markCrossScopePromote 的完整形态。
// writeThrough=true 表示 holder 是**写穿容器**（副本存入其字段/元素），
// 此时额外查 holder 的逃逸性来决定外向/内部。
func (sg *StatementGenerator) markCrossScopePromoteFor(holder string, writeThrough bool) {
	// 写穿且容器逃逸 → 副本必须活过函数返回（向外 promote）。
	if writeThrough && sg.codegen.IsEscapedVar(holder) {
		sg.codegen.funcEmitsPromote = true
		return
	}
	if sg.codegen.IsFunctionLocalVar(holder) {
		sg.codegen.funcEmitsInternalPromote = true
		// 持有者是局部变量：它现在指向幸存段副本。若后续 return 该变量，
		// rewind 会回卷掉副本 → 返回的指针悬垂，故登记为持有者供 return 判定。
		sg.codegen.MarkPromoteHolder(holder)
		return
	}
	sg.codegen.funcEmitsPromote = true
}

// structuralNeedsAssignEscapePromote 结构化所有权基础层的跨域赋值 promote 判断（不依赖 SOR）。
// 规则：源是指针/String 类型的局部变量，且源的声明作用域深度严格深于目标（或目标未记录
// 声明深度，如全局/外部符号）。此时赋值会把可回卷对象传给更长生命周期的持有者，
// 必须先 promote 到 survivor 段再赋值。
func (sg *StatementGenerator) structuralNeedsAssignEscapePromote(srcName, dstName string) bool {
	if srcName == "" || dstName == "" || srcName == dstName {
		return false
	}
	// 源必须是指针类型或 String 类型
	if !sg.isPointerTyped(srcName) && !sg.isStringTyped(srcName) {
		return false
	}
	// 源必须有声明深度记录（是局部变量）
	srcDepth, srcRecorded := sg.codegen.declScopeDepth[srcName]
	if !srcRecorded {
		return false // 源不是局部变量（全局/参数），不需要 promote
	}
	// 目标有声明深度记录时：目标深度 >= 源深度 → 同层或更内层，无逃逸
	if dstDepth, dstRecorded := sg.codegen.declScopeDepth[dstName]; dstRecorded {
		if dstDepth >= srcDepth {
			return false
		}
	}
	// 目标未记录（全局/外部）或目标深度 < 源深度 → 跨域逃逸，需要 promote
	return true
}

// needsAssignEscapePromote 组合判断跨域赋值是否需要 promote（基础层 + SOR 优化层）。
func (sg *StatementGenerator) needsAssignEscapePromote(srcName, dstName string) bool {
	if !sg.structuralNeedsAssignEscapePromote(srcName, dstName) {
		return false
	}
	// SOR 优化层：如果 SOR 活跃且明确说不需要，则否决
	if sg.sorActive() && !sg.sorNeedsAssignEscapePromote(srcName, dstName) {
		return false
	}
	return true
}

// sorNeedsAssignEscapePromote 判断普通赋值 dst = src 是否把 KMM 可回卷对象
// 传给了更长生命周期的持有者（跨作用域逃逸），需要先提升再赋值。
//
// SOR 优化层判定（在 structuralNeedsAssignEscapePromote 基础上进一步精确）：
//  1. 源对象确实可能位于可回卷区域：SOR 决策为 BumpPool，或声明处登记过
//     kmm_v4/std_malloc 常量分配大小；
//  2. 所有权已转移（DropNone）的对象豁免，其回收责任已随 yield/extract 卸载。
func (sg *StatementGenerator) sorNeedsAssignEscapePromote(srcName, dstName string) bool {
	if srcName == "" || dstName == "" || srcName == dstName {
		return false
	}
	if !sg.isPointerTyped(srcName) && !sg.isStringTyped(srcName) {
		return false
	}
	if _, recorded := sg.codegen.declScopeDepth[dstName]; recorded {
		if sg.codegen.GetDeclScopeDepth(dstName) >= sg.codegen.GetDeclScopeDepth(srcName) {
			return false // 目标与源同层或更内层：目标先于源消亡，无逃逸
		}
	}
	d := sg.codegen.sorAdapter.GetVarDecision(srcName)
	if d != nil {
		if d.DropActionID == 0 {
			return false // 所有权已转移（yield/extract），无需重复提升
		}
		if d.AllocKindID == 1 {
			return true // BumpPool：位于可回卷的 per-thread heap 区域
		}
	}
	if n, ok := sg.codegen.sorInitSizes[srcName]; ok && n > 0 {
		return true // 声明处登记过 kmm_v4/std_malloc 常量分配大小
	}
	return false
}

// generateVarAttributes 将变量属性转换为 C 声明前缀
// #[volatile] i32 x → volatile int64_t x
// #[section(".bss")] → __attribute__((section(".bss")))
// #[aligned(16)] → __attribute__((aligned(16)))
func generateVarAttributes(attrs []*ast.Attribute) string {
	if len(attrs) == 0 {
		return ""
	}
	var prefix strings.Builder
	var suffixParts []string
	for _, attr := range attrs {
		switch attr.Name {
		case "volatile":
			prefix.WriteString("volatile ")
		case "section":
			if len(attr.Args) > 0 {
				suffixParts = append(suffixParts, fmt.Sprintf("__attribute__((section(%s)))", attr.Args[0]))
			}
		case "aligned":
			if len(attr.Args) > 0 {
				suffixParts = append(suffixParts, fmt.Sprintf("__attribute__((aligned(%s)))", attr.Args[0]))
			} else {
				suffixParts = append(suffixParts, "__attribute__((aligned))")
			}
		case "weak":
			suffixParts = append(suffixParts, "__attribute__((weak))")
		case "deprecated":
			if len(attr.Args) > 0 {
				suffixParts = append(suffixParts, fmt.Sprintf("__attribute__((deprecated(%s)))", attr.Args[0]))
			} else {
				suffixParts = append(suffixParts, "__attribute__((deprecated))")
			}
		}
	}
	// __attribute__ 放在类型前面，volatile 也放在类型前面
	if len(suffixParts) > 0 {
		prefix.WriteString(strings.Join(suffixParts, " "))
		prefix.WriteByte(' ')
	}
	return prefix.String()
}

// generateReleaseStatement 生成 release 语句代码
// 语法: release source -> [holder1, holder2, ...]
// 语义: 将 source 的只读访问权分发给各 holder
// 策略:
//   - holder 已预先声明: 直接赋值 (零开销, 只读语义由 SOR 分析器编译期保证)
//   - holder 未声明: 创建 const void* 指针别名 (利用 C const 系统保证只读)
func (sg *StatementGenerator) generateReleaseStatement(stmt *ast.ReleaseStatement) string {
	srcCode := sg.codegen.expressionGenerator.GenerateExpression(stmt.Source)
	var b strings.Builder
	b.WriteString("/* SOR: release ")
	b.WriteString(srcCode)
	b.WriteString(" -> [")
	b.WriteString(strings.Join(stmt.Holders, ", "))
	b.WriteString("] */\n")
	for _, holder := range stmt.Holders {
		if sg.codegen.HasSymbol(holder) {
			b.WriteString(holder)
			b.WriteString(" = ")
			b.WriteString(srcCode)
			b.WriteString("; /* SOR: release read-only */\n")
		} else {
			b.WriteString("const void* ")
			b.WriteString(holder)
			b.WriteString(" = &(/* release-read-only */ ")
			b.WriteString(srcCode)
			b.WriteString("); /* SOR: read-only alias */\n")
		}
	}
	return b.String()
}

// generateExtractStatement 生成 extract 语句代码
// 语法: extract source[index] -> target
// 生成: target = source[index]; source[index] = NULL; (真正留下 hollow 状态)
func (sg *StatementGenerator) generateExtractStatement(stmt *ast.ExtractStatement) string {
	// parsePrimaryExpressionIterative 会将 data[2] 解析为 IndexExpression
	// 所以 Source 实际是 IndexExpression{Object: data, Index: 2}
	var baseCode, idxCode string
	var baseIdent *ast.Identifier
	if idxExpr, ok := stmt.Source.(*ast.IndexExpression); ok {
		baseCode = sg.codegen.expressionGenerator.GenerateExpression(idxExpr.Object)
		idxCode = sg.codegen.expressionGenerator.GenerateExpression(idxExpr.Index)
		if objIdent, isObjIdent := idxExpr.Object.(*ast.Identifier); isObjIdent {
			baseIdent = objIdent
		}
	} else {
		baseCode = sg.codegen.expressionGenerator.GenerateExpression(stmt.Source)
		if stmt.Index != nil {
			idxCode = sg.codegen.expressionGenerator.GenerateExpression(stmt.Index)
		} else {
			idxCode = "0"
		}
	}
	var b strings.Builder
	b.WriteString("/* SOR: extract ")
	b.WriteString(baseCode)
	b.WriteString("[")
	b.WriteString(idxCode)
	b.WriteString("] -> ")
	b.WriteString(stmt.Target)
	b.WriteString(" */\n")
	b.WriteString(stmt.Target)
	b.WriteString(" = ")
	b.WriteString(baseCode)
	b.WriteString("[")
	b.WriteString(idxCode)
	b.WriteString("]; /* SOR: extracted value */\n")
	// 修复 #25：跨作用域提取提升（元素拷贝到幸存段，防止源作用域回收后悬垂）
	if baseIdent != nil {
		// 源数组对象大小登记传播给目标（即使本次不提升，后续 return 提升也需要）
		sg.codegen.PropagateInitSize(stmt.Target, baseIdent.Name)
		if sg.sorNeedsCrossScopePromote(baseIdent.Name, stmt.Target) {
			// 任务①：String 走 string_clone_global；指针/聚合走大小解析
			sizeExpr := ""
			if sg.isStringTyped(stmt.Target) {
				sizeExpr = "sizeof(String)"
			} else if size, ok := sg.sorPromoteSize(baseIdent.Name, baseCode); ok {
				sizeExpr = size
			} else {
				sizeExpr = "sizeof(*" + baseCode + ")"
			}
			// promote 被跳过时保持普通 extract 语义（不标记 promote）
			if line := sg.emitPromoteForType(stmt.Target, sizeExpr, "promote cross-scope", baseIdent.Name); line != "" {
				b.WriteString(line)
				sg.markCrossScopePromote(stmt.Target)
			}
		}
	}
	b.WriteString(baseCode)
	b.WriteString("[")
	b.WriteString(idxCode)
	b.WriteString("] = 0; /* SOR: hollow — source slot now null */\n")
	return b.String()
}

// ============================================================================
// 修复 #25：跨作用域所有权提升（promote）判定辅助
// kmm_v4_scope_promote_p 只对"堆分配对象 + 指针类型目标（含 void*）"有效，
// 且仅在所有权穿越到更外层作用域（或目标未被 SOR 追踪，如全局）时发射，
// 避免同层 yield/extract 的无意义拷贝。
// 注意：yield/extract 的源对象在 SOR 决策中最终状态为 Moved → 硬编码 AllocStack+
// DropNone（memory.go buildDecision），所以"堆分配"判定不能只看 AllocKind，
// 还需接受 DropActionID==0（所有权已转移）的组合。
// ============================================================================

func (sg *StatementGenerator) sorActive() bool {
	return sg.codegen.sorAdapter != nil && sg.codegen.sorAdapter.IsActive
}

// generateSurvivorFreeForScope 生成当前作用域中需要 per-object 回收的 survivor 对象的释放代码。
// 方案 A：在作用域退出点发射 kmm_v4_survivor_free，精确回收 survivor 段 slab 桶中的对象。
// 只对 SOR 决策为 DropAction=ScopeEnd && AllocKind=BumpPool 的指针变量发射。
// 对非 survivor 指针（per-thread heap）调用 survivor_free 是安全的 no-op（返回 false）。
// generateSurvivorFreeForScope 生成作用域退出时的 per-object survivor 回收代码。
//
// 结构化所有权基础层（不依赖 SOR）：遍历当前词法作用域的所有符号，对指针类型
// 变量统一发射 kmm_v4_survivor_free。survivor_free 对非 survivor 指针是安全
// no-op，因此不需要知道分配来源。
//
// SOR 优化层（启用时叠加）：用 ShouldSkipSurvivorFree / IsAssignEscaped 跳过
// 已逃逸或非堆变量，减少无意义调用。
func (sg *StatementGenerator) generateSurvivorFreeForScope() string {
	currentScope := sg.codegen.GetCurrentScope()
	if currentScope == nil {
		return ""
	}

	/*
	 * Handle 的 pin 归还排在 KMM 回收之前。
	 *
	 * 两者互不相干（一个还 epoch pin，一个还 KMM 堆对象），但顺序上
	 * 先还 pin 更好：pin 挡着的是帧的延迟回收，早一刻还就早一刻可回收。
	 */
	handleRelease := sg.generateHandleRelease(currentScope)

	symbols := currentScope.GetAllSymbols()
	if len(symbols) == 0 {
		return ""
	}
	// 按名称排序确保输出稳定
	names := make([]string, 0, len(symbols))
	for name := range symbols {
		names = append(names, name)
	}
	sort.Strings(names)

	adapter := sg.codegen.GetSORAdapter()
	sorActive := adapter != nil && adapter.IsActive

	var b strings.Builder
	indent := sg.codegen.indentString()
	b.WriteString(handleRelease)
	for _, name := range names {
		// F8 修复：函数参数是借用指针，所有权属于调用方，不应在函数内释放
		if sym := symbols[name]; sym != nil && sym.Scope == "parameter" {
			b.WriteString(indent)
			b.WriteString("/* structural: skip survivor_free for ")
			b.WriteString(name)
			b.WriteString(" (parameter, borrowed from caller) */\n")
			continue
		}
		// 线程逃逸修复：全局对象生命周期归模块（可被多个线程共享访问），
		// 任何函数（含线程函数）退出时都不得回收全局对象。
		if sym := symbols[name]; sym != nil && sym.Scope == "global" {
			b.WriteString(indent)
			b.WriteString("/* structural: skip survivor_free for ")
			b.WriteString(name)
			b.WriteString(" (global, module-owned) */\n")
			continue
		}
		// 基础层：必须是 C 指针类型或 String 类型（kmm_v4_alloc_auto 返回指针，String 含 ptr 字段）
		if !sg.isPointerTyped(name) && !sg.isStringTyped(name) {
			continue
		}
		// 已被 early free 的变量跳过（防止重复释放）
		if sg.codegen.IsEarlyFreed(name) {
			continue
		}
		// 用户已显式释放的指针 —— 仅当该释放**真的生效**时才跳过。
		//
		// KBUG_KMM_FREE_NOOP_SURVIVOR_LEAK：runtime 的 kmm_v4_free() 是彻底
		// no-op（src/kmm_scoped_allocator_v4.c：「修复 #6：free 为 no-op，
		// bump allocator 靠 scope 回收」）。因此对 KMM 来源（OriginKmm）的变量：
		//   - TLAB 分配（kmm_v4_alloc_auto）→ 由 scope_pop 回卷回收，free 是 no-op，
		//     这里发射 kmm_v4_survivor_free 会命中 TLAB 区间检查并安全返回 false；
		//   - survivor 分配（kmm_v4_alloc_global/promote）→ **只能**靠
		//     kmm_v4_survivor_free 回收，若因 IsUserFreed 跳过则槽位永久泄漏
		//     （实测 ~194 B/请求，slab 区耗尽后降级页式 malloc）。
		// 结论：OriginKmm 变量不得因 IsUserFreed 跳过回收；其余来源
		// （libc_malloc 的 free() 生效、extern 交给下游）维持原跳过语义，避免 double free。
		if os.Getenv("KAULA_DEBUG_ORIGIN") != "" {
			fmt.Fprintf(os.Stderr, "[ORIGIN] var=%s userFreed=%v origin=%q\n",
				name, sg.codegen.IsUserFreed(name), sg.codegen.GetVarOrigin(name))
		}
		if sg.codegen.IsUserFreed(name) && sg.codegen.GetVarOrigin(name) != OriginKmm {
			b.WriteString(indent)
			b.WriteString("/* structural: skip survivor_free for ")
			b.WriteString(name)
			b.WriteString(" (user-freed via *_free call) */\n")
			continue
		}
		// 别名组去重：同一分配的多个别名只通过代表释放一次，
		// 非代表成员跳过；代表被释放时已标记全组，防止跨路径重复释放
		if group := sg.codegen.GetVarAliasGroup(name); len(group) >= 2 {
			rep := sg.selectAliasGroupRepresentative(group)
			if rep != name {
				b.WriteString(indent)
				b.WriteString("/* structural: skip survivor_free for ")
				b.WriteString(name)
				b.WriteString(" (alias of ")
				b.WriteString(rep)
				b.WriteString(", freed once via representative) */\n")
				continue
			}
			for _, member := range group {
				if member != name {
					sg.codegen.MarkEarlyFreed(member)
				}
			}
		}
		// 基础层：extern 来源变量跳过（不是 KMM 分配的，survivor_free 会触发 unowned-pointer abort）
		if sg.codegen.IsExternOrigin(name) {
			b.WriteString(indent)
			b.WriteString("/* structural: skip survivor_free for ")
			b.WriteString(name)
			b.WriteString(" (extern-origin, not KMM-owned) */\n")
			continue
		}
		// 基础层：已通过赋值逃逸的变量跳过（所有权已移到外层）
		if sg.codegen.IsAssignEscaped(name) {
			b.WriteString(indent)
			b.WriteString("/* structural: skip survivor_free for ")
			b.WriteString(name)
			b.WriteString(" (assign-escaped, ownership moved out) */\n")
			continue
		}
		// SOR 优化层：统一通过 ShouldSkipSurvivorFree 判定
		if sorActive {
			if d := adapter.GetVarDecision(name); d != nil {
				if skip, reason := adapter.ShouldSkipSurvivorFree(name, d); skip {
					if reason != "" && reason != "no SOR decision" {
						b.WriteString(indent)
						b.WriteString("/* SOR: skip survivor_free for ")
						b.WriteString(name)
						b.WriteString(" (")
						b.WriteString(reason)
						b.WriteString(") */\n")
					}
					continue
				}
			}
		}
		// String 类型：SSO 感知释放（只有 len>15 才释放堆指针）
		if sg.isStringTyped(name) {
			b.WriteString(indent)
			b.WriteString("string_survivor_free(")
			b.WriteString(name)
			b.WriteString("); /* structural: String payload reclaim (SSO-aware) */\n")
		} else {
			// 多分配器 origin 追踪：按来源选择释放函数
			origin := sg.codegen.GetVarOrigin(name)
			switch origin {
			case OriginKmm:
				b.WriteString(indent)
				b.WriteString("kmm_v4_survivor_free(")
				b.WriteString(name)
				b.WriteString("); /* structural: per-object reclaim (kmm) */\n")
			case OriginLibcMalloc:
				b.WriteString(indent)
				b.WriteString("free(")
				b.WriteString(name)
				b.WriteString("); /* structural: per-object reclaim (libc_malloc) */\n")
			case OriginExtern:
				b.WriteString(indent)
				b.WriteString("/* structural: skip free for ")
				b.WriteString(name)
				b.WriteString(" (extern-origin, ownership opaque) */\n")
			default:
				// unknown 来源：保守用 survivor_free（对非 KMM 指针是安全 no-op）
				b.WriteString(indent)
				b.WriteString("kmm_v4_survivor_free(")
				b.WriteString(name)
				b.WriteString("); /* structural: per-object reclaim (unknown origin, safe no-op) */\n")
			}
		}
	}
	return b.String()
}

// generateSurvivorFreeForReturn 生成 return 前的 per-object survivor 回收代码。
//
// 结构化所有权基础层（不依赖 SOR）：return 会跳过所有外层作用域的末尾 free，
// 因此遍历当前作用域到根的整条作用域链，对每个指针类型变量发射
// kmm_v4_survivor_free。excludeName 指定不需要释放的变量名（通常是 return
// 值本身，已由 promote 处理）。对非 survivor 指针调用 survivor_free 是安全 no-op。
//
// SOR 优化层（启用时叠加）：ShouldSkipSurvivorFree / IsAssignEscaped 跳过。
// generateSurvivorFreeForReturn 发射 return 路径的 per-object 回收。
// 第二个返回值是实际发射了释放的变量名集合（F17：调用方据此决定
// 是否把返回表达式求值提升到回收之前）。
func (sg *StatementGenerator) generateSurvivorFreeForReturn(excludeName string) (string, []string) {
	var b strings.Builder
	indent := sg.codegen.indentString()
	seen := make(map[string]bool) // 去重：同名变量只在最内层作用域释放一次
	freed := make([]string, 0, 4)
	returnedAliases := make(map[string]bool)
	if excludeName != "" {
		returnedAliases[excludeName] = true
		for _, alias := range sg.codegen.GetVarAliasGroup(excludeName) {
			returnedAliases[alias] = true
		}
	}

	adapter := sg.codegen.GetSORAdapter()
	sorActive := adapter != nil && adapter.IsActive

	for scope := sg.codegen.GetCurrentScope(); scope != nil; scope = scope.GetParent() {
		// 函数边界：不跨入外层函数作用域回收（嵌套函数的 return 不得释放外层变量）
		if scope == sg.codegen.survivorStopScope {
			break
		}
		symbols := scope.GetAllSymbols()
		if len(symbols) == 0 {
			continue
		}

		// Handle 的 pin 归还（return 路径）：作用域出口那一发被 lastStmtIsReturn
		// 跳过了，这里必须补上，否则「最后一句是 return」的块会漏还。
		b.WriteString(sg.generateHandleRelease(scope))

		names := make([]string, 0, len(symbols))
		for name := range symbols {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			if returnedAliases[name] {
				continue
			}
			// F8 修复：函数参数是借用指针，所有权属于调用方，不应在 return 时释放
			if sym := symbols[name]; sym != nil && sym.Scope == "parameter" {
				b.WriteString(indent)
				b.WriteString("/* structural: skip survivor_free on return for ")
				b.WriteString(name)
				b.WriteString(" (parameter, borrowed from caller) */\n")
				continue
			}
			// 线程逃逸修复：全局对象生命周期归模块，函数（含线程函数）返回不得回收。
			if sym := symbols[name]; sym != nil && sym.Scope == "global" {
				b.WriteString(indent)
				b.WriteString("/* structural: skip survivor_free on return for ")
				b.WriteString(name)
				b.WriteString(" (global, module-owned) */\n")
				continue
			}
			// 已被 early free 的变量跳过（防止重复释放）
			if sg.codegen.IsEarlyFreed(name) {
				continue
			}
			// 别名组去重：同一分配的多个别名只通过代表释放一次，
			// 非代表成员跳过；代表被释放时已标记全组，防止跨路径重复释放
			if group := sg.codegen.GetVarAliasGroup(name); len(group) >= 2 {
				rep := sg.selectAliasGroupRepresentative(group)
				if rep != name {
					b.WriteString(indent)
					b.WriteString("/* structural: skip survivor_free on return for ")
					b.WriteString(name)
					b.WriteString(" (alias of ")
					b.WriteString(rep)
					b.WriteString(", freed once via representative) */\n")
					continue
				}
				for _, member := range group {
					if member != name {
						sg.codegen.MarkEarlyFreed(member)
					}
				}
			}
			// 基础层：extern 来源变量跳过（不是 KMM 分配的）
			if sg.codegen.IsExternOrigin(name) {
				b.WriteString(indent)
				b.WriteString("/* structural: skip survivor_free on return for ")
				b.WriteString(name)
				b.WriteString(" (extern-origin, not KMM-owned) */\n")
				continue
			}
			// 基础层：必须是指针类型
			if !sg.isPointerTyped(name) {
				continue
			}
			// 基础层：assign-escaped 跳过（所有权已移交）
			if sg.codegen.IsAssignEscaped(name) {
				b.WriteString(indent)
				b.WriteString("/* structural: skip survivor_free on return for ")
				b.WriteString(name)
				b.WriteString(" (assign-escaped, ownership moved out) */\n")
				continue
			}
			// 方案 A'：逃逸变量跳过（其对象被 return/实参/别名交给了函数外，
			// 或与这样的对象互为别名）。典型：`Node* head = cur; ...; return head`
			// 里 cur 本身没被 promote 标记，但它与 head 指向同一对象；
			// 在 return 处释放 cur 会把 return 出去的链尾一并释放 → 悬垂。
			if sg.codegen.IsEscapedVar(name) {
				b.WriteString(indent)
				b.WriteString("/* structural: skip survivor_free on return for ")
				b.WriteString(name)
				b.WriteString(" (escaped / aliased with an outgoing object) */\n")
				continue
			}
			// 已提升到幸存段的变量：所有权已移交给被调方/全局，出口不得回收
			if sg.codegen.IsPromoted(name) {
				b.WriteString(indent)
				b.WriteString("/* structural: skip survivor_free on return for ")
				b.WriteString(name)
				b.WriteString(" (promoted, ownership moved to survivor) */\n")
				continue
			}
			// SOR 优化层：ShouldSkipSurvivorFree 网关
			if sorActive {
				if d := adapter.GetVarDecision(name); d != nil {
					if skip, reason := adapter.ShouldSkipSurvivorFree(name, d); skip {
						if reason != "" && reason != "no SOR decision" {
							b.WriteString(indent)
							b.WriteString("/* SOR: skip survivor_free on return for ")
							b.WriteString(name)
							b.WriteString(" (")
							b.WriteString(reason)
							b.WriteString(") */\n")
						}
						continue
					}
				}
			}
			// 多分配器 origin 追踪：按来源选择释放函数
			origin := sg.codegen.GetVarOrigin(name)
			switch origin {
			case OriginKmm:
				b.WriteString(indent)
				b.WriteString("kmm_v4_survivor_free(")
				b.WriteString(name)
				b.WriteString("); /* structural: reclaim on return (kmm) */\n")
				freed = append(freed, name)
			case OriginLibcMalloc:
				b.WriteString(indent)
				b.WriteString("free(")
				b.WriteString(name)
				b.WriteString("); /* structural: reclaim on return (libc_malloc) */\n")
				freed = append(freed, name)
			case OriginExtern:
				b.WriteString(indent)
				b.WriteString("/* structural: skip free on return for ")
				b.WriteString(name)
				b.WriteString(" (extern-origin, ownership opaque) */\n")
			default:
				// unknown 来源：保守用 survivor_free（对非 KMM 指针是安全 no-op）
				b.WriteString(indent)
				b.WriteString("kmm_v4_survivor_free(")
				b.WriteString(name)
				b.WriteString("); /* structural: reclaim on return (unknown origin) */\n")
				freed = append(freed, name)
			}
		}
	}
	return b.String(), freed
}

// isPointerTyped 目标变量在 C 中必须是指针类型（含 void*），(void**)&var 才合法
func (sg *StatementGenerator) isPointerTyped(name string) bool {
	if name == "" {
		return false
	}
	kaulaType := sg.codegen.GetSymbolType(name)
	if kaulaType == "" {
		return false
	}
	cType := sg.codegen.typeGenerator.convertType(kaulaType, false)
	return cType != "" && strings.HasSuffix(cType, "*")
}

// isStringTyped 判断是否是 std String 类型（struct {len; ptr}），不是 C 指针，
// promote 不能走 (void**)&var 的 scope_promote_p，必须走 string_clone_global（任务①）。
// 匹配 kaula 类型 "string"/"str"/"String" 以及 C 类型名 "String"。
func (sg *StatementGenerator) isStringTyped(name string) bool {
	if name == "" {
		return false
	}
	kaulaType := sg.codegen.GetSymbolType(name)
	if kaulaType == "string" || kaulaType == "str" || kaulaType == "String" {
		return true
	}
	if kaulaType != "" {
		if cType := sg.codegen.typeGenerator.convertType(kaulaType, false); cType == "String" {
			return true
		}
	}
	return false
}

// tcoTailEligible 判断尾调用点能否改写：形参个数匹配 + 实参在本层回收之后
// 求值仍然安全（不引用本帧会被 survivor_free 的对象）。
func (sg *StatementGenerator) tcoTailEligible(call *ast.CallExpression) bool {
	tco := sg.codegen.TcoState()
	if tco == nil || call == nil || len(call.Args) != len(tco.params) {
		return false
	}
	return sg.tcoArgsSurviveFree(call.Args, tco.params)
}

// emitTcoRebindAndGoto 发射"实参写回形参 + 跳回循环顶部"。
// 前置的 free/rewind/scope_pop 由调用方按各自路径发射。
//
// 关键细节：多实参时必须**先全部求值到临时变量再统一写回**。顺序赋值会踩坑——
// 写回第 1 个形参后，后续实参表达式若引用该形参，读到的已经是新值
// （`n = n-1; acc = acc + n` 会少加一次 n）。这与 C 里函数实参先求值再传参的
// 语义必须一致。
func (sg *StatementGenerator) emitTcoRebindAndGoto(b *strings.Builder, call *ast.CallExpression) {
	tco := sg.codegen.TcoState()
	indent := sg.codegen.indentString()
	sg.codegen.tcoTempSeq++
	base := sg.codegen.tcoTempSeq
	if len(call.Args) > 1 {
		for i, arg := range call.Args {
			sg.codegen.tcoTempSeq++
			b.WriteString(indent)
			b.WriteString("__typeof__(")
			b.WriteString(tco.params[i])
			b.WriteString(") __kaula_tco")
			b.WriteString(strconv.Itoa(sg.codegen.tcoTempSeq))
			b.WriteString(" = ")
			b.WriteString(sg.codegen.expressionGenerator.GenerateExpression(arg))
			b.WriteString("; /* tco: capture arg */\n")
		}
		for i := range call.Args {
			b.WriteString(indent)
			b.WriteString(tco.params[i])
			b.WriteString(" = __kaula_tco")
			b.WriteString(strconv.Itoa(base + i + 1))
			b.WriteString("; /* tco: rebind */\n")
		}
	} else {
		for i, arg := range call.Args {
			b.WriteString(indent)
			b.WriteString(tco.params[i])
			b.WriteString(" = ")
			b.WriteString(sg.codegen.expressionGenerator.GenerateExpression(arg))
			b.WriteString("; /* tco: rebind */\n")
		}
	}
	b.WriteString(indent)
	b.WriteString("goto " + tco.label + "; /* tco: tail call, same frame */\n")
}

// generateVoidTcoTailCall 处理 void 函数的 `f(...)` 语句式尾调用：
// 发射与 return 路径完全相同的本层回收动作，再回写形参跳回循环顶部。
// 返回空串表示不适合改写，调用方退回真实调用。
func (sg *StatementGenerator) generateVoidTcoTailCall(call *ast.CallExpression) string {
	if !sg.tcoTailEligible(call) {
		return ""
	}
	var b strings.Builder
	freeCode, _ := sg.generateSurvivorFreeForReturn("")
	b.WriteString(freeCode)
	if sg.codegen.kmmScopeDepth > 0 && !sg.codegen.funcEmitsPromote && sg.codegen.funcHasKMMCheckpoint {
		b.WriteString(sg.codegen.indentString())
		b.WriteString("kmm_v4_survivor_rewind(__surv_cp); /* tco: reclaim this frame */\n")
	}
	b.WriteString(sg.codegen.KMMExitPopLines(sg.codegen.indentString()))
	sg.emitTcoRebindAndGoto(&b, call)
	sg.codegen.TcoState().MarkTcoConverted()
	return b.String()
}

// tcoArgsSurviveFree 判断尾调用实参在"本层回收之后"求值是否仍然安全。
//
// 尾调用改写把求值时机移到了本帧 per-object free 之后，因此实参不能引用
// 本帧会被 survivor_free / 回收的对象。形参是从调用方借用的（从不释放），
// 标量局部也不受影响；一旦实参引用了本帧的指针/String 局部变量就退回真实调用。
func (sg *StatementGenerator) tcoArgsSurviveFree(args []ast.Expression, params []string) bool {
	for _, name := range collectIdentifiers(args) {
		if containsStr(params, name) {
			continue // 形参：borrowed from caller，本帧不释放
		}
		if sg.isPointerTyped(name) || sg.isStringTyped(name) {
			return false
		}
	}
	return true
}

// collectIdentifiers 收集表达式树中出现的全部标识符名
func collectIdentifiers(exprs []ast.Expression) []string {
	var out []string
	var walk func(e ast.Expression)
	walk = func(e ast.Expression) {
		switch x := e.(type) {
		case *ast.Identifier:
			out = append(out, x.Name)
		case *ast.UnaryExpression:
			walk(x.Right)
		case *ast.ParenExpression:
			walk(x.Inner)
		case *ast.TypeCastExpression:
			walk(x.Expression)
		case *ast.BinaryExpression:
			walk(x.Left)
			walk(x.Right)
		case *ast.IndexExpression:
			walk(x.Object)
			walk(x.Index)
		case *ast.MemberAccessExpression:
			walk(x.Object)
		case *ast.CallExpression:
			for _, a := range x.Args {
				walk(a)
			}
		}
	}
	for _, e := range exprs {
		walk(e)
	}
	return out
}

// isPromotableAggregate 判断是否是"需要深提升回调的聚合类型"（任务②正解入口）。
// 目前仅识别 class 类型（声明里含指针字段）；返回 true 时 emitPromoteForType
// 会走 kmm_v4_scope_promote_p_deep 路径并合成类型化 cb（后续完善）。
func (sg *StatementGenerator) isPromotableAggregate(name string) bool {
	if name == "" {
		return false
	}
	kaulaType := sg.codegen.GetSymbolType(name)
	if kaulaType == "" {
		return false
	}
	if sg.codegen.typeGenerator == nil {
		return false
	}
	// classTypes[className]==true 标记 class/struct 类型，字段里可能有指针
	return sg.codegen.typeGenerator.classTypes[kaulaType]
}

// emitPromoteForType 根据被提升变量的类型分派 promote 代码（任务①②的统一入口）：
//   - String：target = string_clone_global(target)  （载荷从 survivor 段重分配，浅外壳+深载荷）
//   - 含指针字段的 class 聚合：预留 kmm_v4_scope_promote_p_deep + 合成类型化 cb（任务②正解，
//     当前先以带说明注释的 scope_promote_p 占位并在注释中标记 TODO，后续补回调合成）
//   - 普通 POD 指针：kmm_v4_scope_promote_p((void**)&target, sizeExpr)（原路径）
//
// 参数：
//
//	targetCVar：被 (void**)& 或 string_clone_global 作用的 C 变量（通常是目标名）
//	sizeExpr：提升大小表达式（用于非 String 的浅拷贝字节数）
//	commentTag：注释尾部 tag（"promote on return"/"promote cross-scope"/"promote arg into callee"）
//	promotedSrc：仅调试/注释用，源对象的变量名或代码
func (sg *StatementGenerator) emitPromoteForType(targetCVar, sizeExpr, commentTag, promotedSrc string) string {
	// 任务③：extern 返回值来源的对象强制拒绝跨作用域 promote。
	// extern 对 SOR 是 opaque：没有字段类型信息、没有分配器归属（可能是 libc/OS 内存），
	// 任何形式的自动 promote 都无法保证内部指针和释放语义正确。必须：
	//   a) 加入 codegen.errors（kaulac 报错）；
	//   b) 在生成的 C 代码中插入 #error 和 abort，避免静默生成悬空代码。
	if sg.sorActive() {
		if extFn, ok := sg.codegen.sorAdapter.IsExternOrigin(targetCVar); ok {
			msg := fmt.Sprintf("变量 '%s' 来自 extern 函数 '%s' 的返回值（对 SOR 不透明），"+
				"不能跨作用域提升（%s）：请改用 #[no_kmm] 跳过该函数，或手动用 std_malloc 显式管理",
				targetCVar, extFn, commentTag)
			sg.codegen.error("[SOR extern-escape] " + msg)
			// #error 触发 clang 编译中止；同时包装成 do{}while(0) 以便放语句位置
			return fmt.Sprintf("/* SOR ERROR: %s (extern-origin '%s' from %s) */\n"+
				"#error SOR_EXTERN_PROMOTE_FORBIDDEN: target=%s source_extern=%s\n"+
				"do { abort(); } while(0);\n",
				msg, targetCVar, extFn, targetCVar, extFn)
		}
		// 注：yield 路径下 targetCVar = yield 目标，而 extern 标记是沿别名传播的
		// （escape.go checkExternSink 已把 extern fn 继承给目标），所以仅查 targetCVar 即可。
	}
	if sg.isStringTyped(targetCVar) {
		// 任务①：String 特判走 string_clone_global
		// src 是结构体本身（目标已持有拷贝，string_clone_global 再克隆载荷）
		return fmt.Sprintf("%s = string_clone_global(%s); /* SOR: %s (String deep-clone payload via survivor) */\n",
			targetCVar, targetCVar, commentTag)
	}
	if sg.isPromotableAggregate(targetCVar) {
		// 任务② Bug 2 正解：含指针字段的聚合类型 → kmm_v4_scope_promote_p_deep + 合成类型化 cb。
		// cb 由 EnsureDeepPromoteCB 首次调用时合成，后续同类型复用（去重）。
		kaulaType := sg.codegen.GetSymbolType(targetCVar)
		cbName := sg.codegen.EnsureDeepPromoteCB(kaulaType)
		if cbName != "" {
			return fmt.Sprintf("kmm_v4_scope_promote_p_deep((void**)&%s, %s, %s); /* SOR: %s (aggregate deep-promote via synthesized cb) */\n",
				targetCVar, sizeExpr, cbName, commentTag)
		}
		// 安全性缺口修复（核查表 Bug 2 隐患①）：聚合声明通过却无法合成 cb，属于内部不一致。
		// 不允许"仅注释后静默浅提升"——Bug 2 悬垂指针就是这样漏过的。升级为编译期硬报错：
		//   a) Go 侧加入 codegen.errors（HasErrors 阻断后续流水线）；
		//   b) C 侧 #error + abort，避免任何静默生成 dangling 代码。
		msg := fmt.Sprintf("聚合类型 %q 的深提升回调合成失败（与 isPromotableAggregate 判定不一致，"+
			"可能导致 Bug 2：浅提升后内部指针仍指向已释放段）；拒绝浅提升降级", kaulaType)
		sg.codegen.error("[SOR deep-promote] " + msg)
		return fmt.Sprintf("/* SOR FATAL: %s (kaulaType=%q target=%s) */\n"+
			"#error SOR_DEEP_PROMOTE_CB_MISSING: type=%s var=%s  /* audit point Bug 2 */\n"+
			"do { abort(); } while(0);\n",
			msg, kaulaType, targetCVar, kaulaType, targetCVar)
	}
	// 根治病灶 B2：sizeExpr 退化为 sizeof(*p) 时，仅当指针恰好指向"单个对象"才安全。
	//
	// 变量声明为指针类型（`T*`，语义上是缓冲区/别名）时，sizeof(*p) 只是**一个元素**
	// 的字节数。典型受害场景：Node* nodes = kmm_v4_alloc(nodesCount * 16)——
	// 真实分配大小是运行期表达式，静态推不出来，tier3 兜底给出 sizeof(K_Node)=24，
	// 于是 1 MB 缓冲被"提升"成 24 字节副本，调用方仍指向原内存，跨作用域遍历立即越界
	// （btree_bench --sor 段错误）。
	//
	// 两种选择：拷贝错误长度（静默数据损坏）或不 promote（保持借用语义：调用方的缓冲
	// 在其作用域内存活）。必须选后者。真正需要转移所有权时，正确做法是让分配大小
	// 在编译期可见（常量表达式）或由运行期携带（分配大小元数据）。
	if !sg.promoteSizeIsSound(targetCVar, promotedSrc, sizeExpr) {
		sg.codegen.AddPromoteSkipNote(fmt.Sprintf(
			"%s: 跳过 promote '%s'（分配大小不可静态确定，%s 只覆盖一个元素；"+
				"保持借用语义）", commentTag, targetCVar, strings.TrimSpace(sizeExpr)))
		return ""
	}
	// 默认：普通 POD 指针的浅拷贝提升（scope_promote_p 原路径）
	return fmt.Sprintf("kmm_v4_scope_promote_p((void**)&%s, %s); /* SOR: %s */\n",
		targetCVar, sizeExpr, commentTag)
}

// promoteSizeIsSound 判断 promote 的拷贝大小表达式是否可信。
//
// 返回 false 的唯一情形：大小退化为 sizeof(*x)，且 x 是**指针类型变量**
// （`T*`）。此时指针指向的是缓冲区/别名，元素大小 ≠ 分配大小，按元素大小
// 拷贝必然截断。class/struct 实例（声明类型不带 `*`，C 侧存单对象指针）
// 的 sizeof(*x) 才是真实对象大小，允许 promote。
func (sg *StatementGenerator) promoteSizeIsSound(targetCVar, promotedSrc, sizeExpr string) bool {
	size := strings.TrimSpace(sizeExpr)
	if !strings.HasPrefix(size, "sizeof(*") {
		return true // 常量字节数 / SOR 估算 / sorInitSizes → 真实分配大小
	}
	// 源变量优先（yield/extract 的 sizeExpr 基于源对象计算）
	typeName := ""
	for _, candidate := range []string{promotedSrc, targetCVar} {
		if candidate == "" {
			continue
		}
		if t := sg.codegen.GetSymbolType(candidate); t != "" {
			typeName = strings.TrimSpace(t)
			break
		}
	}
	// 声明类型带 `*` → 指针/缓冲区语义，元素大小不可用于 promote
	return !strings.HasSuffix(typeName, "*")
}

// sorPromoteSize 返回提升所需的大小表达式（三级回退）：
//  1. decision.SizeBytes（SOR 大小估算）
//  2. 声明初始化处登记的常量大小（kmm_v4_alloc_auto(N)，覆盖 auto/void*/malloc 缓冲）
//  3. C 指针 deref：sizeof(*code)（要求非 void*）——仅当对象确为 pointee 大小值时适用
//
// 注意 kmm_v4_scope_promote_p 拷贝的是 *ptr 指向的整个对象，size 必须是对象的实际
// 分配大小；pointer-deref 的 sizeof(*var) 只等于对 malloc 缓冲的真实大小，可能偏小，
// 因此登记大小（tier 2）优先于 deref（tier 3）。
func (sg *StatementGenerator) sorPromoteSize(name, code string) (string, bool) {
	if name == "" {
		return "", false
	}
	if d := sg.codegen.sorAdapter.GetVarDecision(name); d != nil && d.SizeBytes > 0 {
		return fmt.Sprintf("%d", d.SizeBytes), true
	}
	if n, ok := sg.codegen.sorInitSizes[name]; ok && n > 0 {
		return fmt.Sprintf("%d", n), true
	}
	if kaulaType := sg.codegen.GetSymbolType(name); kaulaType != "" {
		cType := sg.codegen.typeGenerator.convertType(kaulaType, false)
		if cType != "" && cType != "void*" && strings.HasSuffix(cType, "*") {
			return "sizeof(*" + code + ")", true
		}
	}
	return "", false
}

// sorNeedsCrossScopePromote 判断 yield/extract 转移是否需要跨作用域提升
func (sg *StatementGenerator) sorNeedsCrossScopePromote(srcName, targetName string) bool {
	if !sg.sorActive() {
		return false
	}
	srcD := sg.codegen.sorAdapter.GetVarDecision(srcName)
	if srcD == nil {
		return false
	}
	// 源必须是堆分配对象：BumpPool(1)，或已转移所有权（DropNone=0）的堆对象
	if srcD.AllocKindID != 1 && srcD.DropActionID != 0 {
		return false
	}
	// 合法 promote 目标：C 指针（原口径）或 String struct（任务①）
	if !sg.isPointerTyped(targetName) && !sg.isStringTyped(targetName) {
		return false
	}
	// SOR 的 scopeID 不覆盖 if/while 块（恒为 0），必须叠加 codegen 块深度。
	// 目标声明块比源浅（或未被 SOR 追踪，如全局）→ 目标生存期更长 → 需要提升
	tgtD := sg.codegen.sorAdapter.GetVarDecision(targetName)
	if tgtD == nil {
		return true
	}
	if tgtD.ScopeID > srcD.ScopeID {
		return false // 目标在更内层作用域：目标先于源回收，无需提升
	}
	if tgtD.ScopeID < srcD.ScopeID {
		return true
	}
	// 同 SOR scope（含 if/while 块内声明）：看 codegen 作用域栈深度
	return sg.codegen.GetDeclScopeDepth(targetName) < sg.codegen.GetDeclScopeDepth(srcName)
}

// sorNeedsReturnPromote 判断 return 是否需要提升返回值
// BumpPool 分配 + DropScopeEnd（作用域结束回收）才需要；DropNone（已转移/已提升）不重复拷贝
// 任务①：额外允许 String（非指针 struct）返回提升
func (sg *StatementGenerator) sorNeedsReturnPromote(name string) bool {
	if !sg.sorActive() {
		return false
	}
	d := sg.codegen.sorAdapter.GetVarDecision(name)
	if d == nil || d.AllocKindID != 1 || d.DropActionID != 1 {
		return false
	}
	if sg.isStringTyped(name) {
		return true // 任务①：String 类型不需要 size 查询即可 promote（string_clone_global 内部自决）
	}
	_, ok := sg.sorPromoteSize(name, name)
	return ok
}

// structuralNeedsReturnPromote 结构化所有权基础层的 return promote 判断（不依赖 SOR）。
// 规则：返回值是当前函数内任一词法作用域声明的指针类型（或 String）变量，
// 且未通过赋值逃逸、未被 promote 过。内层块返回外层局部变量同样要提升。
// extern 来源的变量会加编译警告（promote 语义可能不正确，但基础层保守 promote）。
// SOR 优化层（启用时）可用 sorNeedsReturnPromote 做更精确的判断（如已提升的不重复拷贝）。
func (sg *StatementGenerator) structuralNeedsReturnPromote(name string) bool {
	if name == "" {
		return false
	}
	// 只查当前块会漏掉 if/循环内返回函数外层局部变量的情况。
	// 停在当前函数边界，不能提升形参、全局或嵌套函数捕获的外层变量。
	local := false
	for scope := sg.codegen.GetCurrentScope(); scope != nil && scope != sg.codegen.survivorStopScope; scope = scope.GetParent() {
		if sym := scope.GetLocalSymbol(name); sym != nil {
			local = sym.Scope != "parameter" && sym.Scope != "global"
			break
		}
	}
	if !local {
		return false
	}
	// 已通过赋值逃逸的变量不需要 promote（所有权已移到外层）
	if sg.codegen.IsAssignEscaped(name) {
		return false
	}
	// reaching definition：已被 promote 过的变量不重复 promote（从 SOR 下移的能力②）
	if sg.codegen.IsPromoted(name) {
		return false
	}
	// 指针类型或 String 类型需要 promote
	if sg.isPointerTyped(name) || sg.isStringTyped(name) {
		// extern 来源警告（从 SOR 下移的能力④）：基础层保守 promote，但提示语义可能不正确
		if sg.codegen.IsExternOrigin(name) {
			sg.codegen.error("[structural] 变量 '" + name + "' 来自 extern 函数返回值，" +
				"return promote 可能语义不正确（extern 指针对内存管理不透明）；" +
				"建议启用 -sor 获取精确的 extern 边界检查")
		}
		return true
	}
	return false
}

// needsReturnPromote 组合判断 return 是否需要 promote（基础层 + SOR 优化层）。
// 基础层 structuralNeedsReturnPromote 决定是否需要；SOR 活跃时可用 sorNeedsReturnPromote
// 做更精确的否决（如变量已被提升过、DropAction=None 等）。
func (sg *StatementGenerator) needsReturnPromote(name string) bool {
	if !sg.structuralNeedsReturnPromote(name) {
		return false
	}
	// SOR 优化层：如果 SOR 活跃且明确说不需要，则否决基础层判断
	if sg.sorActive() && !sg.sorNeedsReturnPromote(name) {
		return false
	}
	return true
}

// structuralCallArgPromotes 基础层传参逃逸分析（不依赖 SOR）。
// 缺口3：悬垂指针/use-after-free。
// 两类被调方按"可能长期持有指针"处理：
//   - extern 函数：所有权不透明，指针/String 实参一律保守 promote；
//   - stdlib FFI 函数：vector_push_back(G_POOL, as<void()>(e)) 之类会把元素
//     指针存进容器（容器通常是全局的），仅对本作用域新建的类实例 promote。
//
// 当前文件定义的 KL 函数暂不做跨函数所有权分析（保守不 promote）。
func (sg *StatementGenerator) structuralCallArgPromotes(funcName string, args []ast.Expression) []string {
	if funcName == "" {
		return nil
	}
	isExtern := sg.codegen.IsExternFunc(funcName)
	isFFI := !isExtern && sg.isStdlibFFIFunc(funcName)
	if !isExtern && !isFFI {
		return nil
	}
	var promotes []string
	for _, arg := range args {
		id, ok := arg.(*ast.Identifier)
		if !ok {
			// 显式转换实参（as<void()>(e) / as<Elem>(p)）与裸标识符实参等价：
			// 同样是把局部对象的指针交给被调方。此前只认裸标识符，导致
			// "存入全局容器"的常见写法漏提升，调用方 scope_pop 后对象内存
			// 被复用 → 悬垂（典型症状：元素池里的对象字段变成垃圾）。
			if cast, isCast := arg.(*ast.TypeCastExpression); isCast {
				id, ok = cast.Expression.(*ast.Identifier)
			}
		}
		if !ok {
			continue
		}
		// 必须是当前作用域声明的局部变量（参数和全局不需要 promote）
		if !sg.codegen.IsVarDeclaredInCurrentScope(id.Name) {
			continue
		}
		// 已通过赋值逃逸或已 promote 的变量不重复 promote
		if sg.codegen.IsAssignEscaped(id.Name) || sg.codegen.IsPromoted(id.Name) {
			continue
		}
		// 本作用域新建的类实例（`Elem e = Elem()`）：交给上面两类被调方时
		// 按"被调方会长期持有"处理——不提升则调用方 scope_pop 后对象内存
		// 被复用，容器里留下悬垂指针（渲染时表现为元素字段变成垃圾）。
		// 借用指针（elem_at()/vector_get()/file_open() 的返回值）不在此列：
		// 拷贝它们会让容器本体与副本失联。
		if sg.codegen.IsOwnedObj(id.Name) {
			promotes = append(promotes, id.Name)
			continue
		}
		if !isExtern {
			continue
		}
		// extern 调用的指针/String 实参：所有权不透明，保守 promote
		if sg.isPointerTyped(id.Name) || sg.isStringTyped(id.Name) {
			promotes = append(promotes, id.Name)
		}
	}
	return promotes
}

// isStdlibFFIFunc 判断函数名是否为 stdlib.json 登记的 FFI（C 实现）函数。
func (sg *StatementGenerator) isStdlibFFIFunc(name string) bool {
	if name == "" || sg.codegen == nil || sg.codegen.stdlibConfig == nil {
		return false
	}
	return sg.codegen.stdlibConfig.GetAnyFunctionSignature(name) != nil
}

// emitCallArgPromotePrefix 针对"按名字调用"（含 std.xxx.yyy 模块限定调用）
// 收集需要提升的实参并生成提升代码前缀；无需提升时返回 ""。
func (sg *StatementGenerator) emitCallArgPromotePrefix(funcName string, args []ast.Expression) string {
	if funcName == "" {
		return ""
	}
	// SOR 模式优先（更精确）；否则退回基础层的保守判定
	promotes := sg.sorCallArgPromotes(funcName, args)
	if len(promotes) == 0 {
		promotes = sg.structuralCallArgPromotes(funcName, args)
	}
	return sg.emitPromotePrefixFor(promotes, "promote arg into callee")
}

// emitPromotePrefixFor 把实参名列表渲染成 promote 语句前缀（空列表返回 ""）。
func (sg *StatementGenerator) emitPromotePrefixFor(promotes []string, commentTag string) string {
	if len(promotes) == 0 {
		return ""
	}
	var pb strings.Builder
	emitted := 0
	for _, argName := range promotes {
		sizeExpr := ""
		if size, ok := sg.sorPromoteSize(argName, argName); ok {
			sizeExpr = size
		} else {
			// 兜底：指针 deref 大小（String 不需要 size，会在 emitPromoteForType 内绕过）
			sizeExpr = "sizeof(*" + argName + ")"
		}
		// emitPromoteForType 在拷贝大小不可信时返回 ""（跳过 promote）：
		// 此时不能标记 funcEmitsPromote，否则函数出口会白白放弃
		// survivor_rewind，把本该回收的对象留在幸存段。
		if line := sg.emitPromoteForType(argName, sizeExpr, commentTag, argName); line != "" {
			pb.WriteString(line)
			// reaching definition：标记已提升，出口回收必须跳过该变量——
			// 否则会把刚提升到幸存段（所有权已移交被调方）的对象释放掉，
			// 被调方容器里留下悬垂指针。
			sg.codegen.MarkPromoted(argName)
			emitted++
		}
	}
	if emitted > 0 {
		sg.codegen.funcEmitsPromote = true // 方案 C：标记函数有 promote，出口跳过 survivor_rewind
	}
	return pb.String()
}

// sorCallArgPromotes 收集调用前需要提升的实参（修复 #25）
// 被调函数的 * 类型形参（ModeOwned=消费所有权）接收的指针若留在调用方作用域段，
// 调用方作用域 pop 后悬垂；调用点前提升到全局幸存段，保证被调方长期持有的安全。
// 仅处理语句级的直接函数调用（表达式上下文中的调用不做提升，见 generateCallExpression）。
// 任务①：额外接受 String 类型实参（struct，非指针）。
func (sg *StatementGenerator) sorCallArgPromotes(funcName string, args []ast.Expression) []string {
	if !sg.sorActive() || funcName == "" {
		return nil
	}
	var promotes []string
	for i, arg := range args {
		id, ok := arg.(*ast.Identifier)
		if !ok {
			continue
		}
		if sg.codegen.sorAdapter.GetFuncParamMode(funcName, i) != 0 {
			continue
		}
		d := sg.codegen.sorAdapter.GetVarDecision(id.Name)
		if d == nil || d.AllocKindID != 1 {
			continue
		}
		// 任务①：String 直接加入（无需 size，string_clone_global 自决）；
		// 指针类型需有可解析大小（scope_promote_p 需要字节数）
		if sg.isStringTyped(id.Name) {
			promotes = append(promotes, id.Name)
			continue
		}
		if _, ok := sg.sorPromoteSize(id.Name, id.Name); ok {
			promotes = append(promotes, id.Name)
		}
	}
	return promotes
}
