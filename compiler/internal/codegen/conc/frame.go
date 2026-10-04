// Kaula 并发内核 · conc fn 的 C 代码生成
//
// 产物（文档 §9.1）：
//  1. Frame 结构体 —— pc + 跨 suspend 存活的变量
//  2. resume 函数 —— KaulaResumeResult (*)(void* frame)
//  3. coercion shim —— 顺序调用 conc fn 时用（§9.4）
//
// 关键性质：无挂起点的 conc fn 走"单一形态"，
// 既不生成 pc 字段也不生成 switch，把 resume 的开销压到一次直接 return。
// 这是设计说明书 §2.7「顺序调用不比同名 fn 慢」的落地方式。
package conc

import (
	"fmt"
	"strings"

	"kaula/internal/ast"
)

// Emitter 汇总一次 lowering 需要的上下文
type Emitter struct {
	// FuncName 是 Kaula 层的函数名
	FuncName string
	// CName 是生成的 C 标识符前缀（通常是 K_<name>）
	CName string
	// ReturnType 是 Kaula 类型的返回值
	ReturnType string
	// CReturnType 是对应的 C 类型
	CReturnType string
	// FrameVarCTypes 是跨 suspend 存活的局部变量对应的 C 类型。
	//
	// 必须由调用方（funcgen，能访问 typeGenerator）填 —— conc 包不认识
	// Kaula 的类型名，自己猜会错（i64 vs i32 vs String 布局都不同，
	// 猜错就是静默的数据损坏，不是编译错误）。
	FrameVarCTypes map[string]string
	// Params 是参数名
	Params []string
	// ParamTypes 是参数类型（Kaula 侧）
	ParamTypes []string
	// ParamCTypes 是参数类型（C 侧）
	ParamCTypes []string
	// ParamNullables 标记可空参数
	ParamNullables []bool

	// stateful 由 Generate 设置：true 表示双形态（有挂起点）
	stateful bool

	// guardLayout 由 SetGuardLayout 固化（analysis.CompoundDepth / HasForIn）。
	// GenerateSpawn 读它决定免 memset 时要显式清零哪些守卫槽。
	compoundDepth int
	hasForIn      bool
}

// 生成的标识符
func (e *Emitter) FrameType() string { return e.CName + "_Frame" }
func (e *Emitter) ResumeFn() string  { return e.CName + "_resume" }
func (e *Emitter) ShimFn() string    { return e.CName + "_as_fn" }

// SimpleEntryFn 是单一形态下 spawn 用的 void*(void*) 入口
func (e *Emitter) SimpleEntryFn() string { return e.CName + "_simple" }

// Generate 为一个 conc fn 产出全部 C 代码。
//
// analysis 是函数体的分析结果（由 AnalyzeBody 得到）。
// FrameVarCType 返回跨 suspend 变量的 C 类型；未知时保守用 void*
func (e *Emitter) FrameVarCType(name string) string {
	if e.FrameVarCTypes != nil {
		if t, ok := e.FrameVarCTypes[name]; ok && t != "" {
			return t
		}
	}
	return "void*"
}

// Generate 为一个 conc fn 产出全部 C 定义。
//
// segments 是按挂起点切开的函数体分段；第 i 段对应 resume 的 case i。
// 分段而不是整段，是为了让每段末尾能插入"保存状态并挂起返回"。
func (e *Emitter) Generate(analysis *BodyAnalysis, segments []string, body []ast.Statement) string {
	var out strings.Builder
	e.stateful = analysis.NeedsSuspend

	joined := ""
	for _, s := range segments {
		joined += s
	}

	if !analysis.NeedsSuspend {
		// 单一形态：没有挂起点，resume 只是一次转发。
		// 编译器可以把 resume 与普通函数内联合并，spawn 也能退化为普通调用。
		out.WriteString(e.generateSimpleFrame())
		out.WriteString(e.generateSimpleResume(joined))
		out.WriteString(e.generateShim(joined, false))
		return out.String()
	}

	// 双形态：pc + Frame 字段
	out.WriteString(e.generateStatefulFrame(analysis))
	out.WriteString(e.generateStatefulResume(analysis, segments))
	out.WriteString(e.generateShim(joined, true))
	return out.String()
}

// ---------- Frame ----------

/*
 * 单一形态的 Frame：承载参数与结果，但没有 pc。
 *
 * 看起来"无挂起点就不需要 Frame"是显然的，但 spawn 仍需要它：
 * spawn 要把参数交给内核、在 Worker 上执行、再取回结果。
 * Frame 在这里只是参数包与结果槽 —— 没有状态机，但有数据传递。
 */
func (e *Emitter) generateSimpleFrame() string {
	var b strings.Builder
	b.WriteString("/* Frame for conc fn " + e.FuncName + " (无挂起点：单一形态) */\n")
	b.WriteString("typedef struct {\n")
	b.WriteString("    /* 无 suspend 点，不需要程序计数 */\n")
	if len(e.Params) > 0 {
		b.WriteString("    /* 参数 */\n")
		for i, p := range e.Params {
			b.WriteString(fmt.Sprintf("    %s %s;\n", e.CTypeOfParam(i), p))
		}
	}
	if e.HasResult() {
		b.WriteString("    /* 结果 */\n")
		b.WriteString(fmt.Sprintf("    %s result;\n", e.CReturnType))
	}
	b.WriteString("} " + e.FrameType() + ";\n\n")
	return b.String()
}

func (e *Emitter) generateStatefulFrame(analysis *BodyAnalysis) string {
	var b strings.Builder

	b.WriteString("/* Frame for conc fn " + e.FuncName + " */\n")
	b.WriteString("typedef struct {\n")

	// pc：程序计数，指向下次 resume 从哪个 case 继续
	b.WriteString("    /* 程序计数：0=入口，N=第 N 个 suspend 点之后，-1=已完成 */\n")
	b.WriteString("    int             pc;\n")

	/*
	 * 复合语句内的恢复槽位。
	 *
	 * pc 只能表达「顶层语句切到哪儿」。循环体/分支体里的挂起点落在
	 * 顶层切分之外 —— 没有 sub 就只能重新执行整个循环体，
	 * 而重新执行不会报错，只是把副作用做两遍。
	 *
	 * sub[d] 是第 d 层复合语句「本轮已执行到第几个语句」。
	 * 恢复时按它跳过已执行的语句，再继续往下走。
	 * branch[d] 记 if 选了哪个分支 —— 条件不能重算，重算就可能走错分支。
	 *
	 * 不变量：离开某个复合语句时 sub[d] 必须回到 0。
	 * 生成代码靠「每轮末尾写回 0」维持它，所以进入任一层时读到的都是 0。
	 */
	if analysis.HasCompound {
		// 数组按实际守卫深度（CompoundDepth+1，含 body 层 sub[0]），
		// 不再固定 KAULA_CONC_SUB_DEPTH=8：task Frame 的 sub/branch 从
		// 64B 降到 2×depth 个 int，KMM 分配档位随之下移。
		depth := analysis.CompoundDepth + 1
		if depth > 8 {
			depth = 8 // 防御：超深嵌套与固定上限持平，不引入新越界
		}
		b.WriteString("    /* 复合语句内的恢复槽位与 if 分支选择 */\n")
		b.WriteString(fmt.Sprintf("    int             sub[%d];\n", depth))
		b.WriteString(fmt.Sprintf("    int             branch[%d];\n", depth))
	}

	/*
	 * for..in(range) 循环守卫的迭代状态（按深度，同 sub/branch）。
	 *
	 * 迭代下标 _i_ 是编译器生成的局部量，原本不在 Frame 里 ——
	 * 守卫模式恢复时 _i_ 从头再数一遍，循环体被重复执行。
	 * 把 start/end/step 求值一次存入 Frame，恢复从 f->loop_i[d] 继续，
	 * 循环体只执行正确轮次。
	 */
	if analysis.HasForIn {
		depth := analysis.CompoundDepth + 1
		if depth > 8 {
			depth = 8
		}
		b.WriteString("    /* for..in 守卫：迭代下标 / 上界 / 步长 */\n")
		b.WriteString(fmt.Sprintf("    long long       loop_i[%d];\n", depth))
		b.WriteString(fmt.Sprintf("    long long       loop_end[%d];\n", depth))
		b.WriteString(fmt.Sprintf("    long long       loop_step[%d];\n", depth))
	}

	// 参数：函数入口时写入，跨所有挂起点存活
	if len(e.Params) > 0 {
		b.WriteString("    /* 参数 */\n")
		for i, p := range e.Params {
			ctype := e.CTypeOfParam(i)
			b.WriteString(fmt.Sprintf("    %s %s;\n", ctype, p))
		}
	}

	// 跨 suspend 存活的局部变量
	if len(analysis.FrameVars) > 0 {
		b.WriteString("    /* 跨 suspend 存活的局部变量 */\n")
		for _, v := range analysis.FrameVars {
			b.WriteString(fmt.Sprintf("    /* %s */\n", v.Name))
			/*
			 * 用真实 C 类型，不是 void*。
			 *
			 * 这些字段的读写发生在不同 resume case 里，类型必须与
			 * 函数体里的局部变量完全一致；void* 会让 resume 里的
			 * 恢复语句类型不匹配，或者更糟 —— void*↔整数的隐式转换
			 * 被接受，于是半个值被静默截断。
			 */
			b.WriteString(fmt.Sprintf("    %-14s %s;\n", e.FrameVarCType(v.Name), v.Name))
		}
	}

	// 结果
	if e.CReturnType != "" && e.CReturnType != "void" {
		b.WriteString("    /* 结果 */\n")
		b.WriteString(fmt.Sprintf("    %s result;\n", e.CReturnType))
	}

	// 标记：帧是否持有堆对象（决定回收时要不要 free）
	b.WriteString("    /* 帧是否需要显式回收 */\n")
	b.WriteString("    int             needs_free;\n")

	b.WriteString("} " + e.FrameType() + ";\n\n")
	return b.String()
}

// CTypeOfParam 返回参数 i 的 C 类型
func (e *Emitter) CTypeOfParam(i int) string {
	if i < len(e.ParamCTypes) && e.ParamCTypes[i] != "" {
		return e.ParamCTypes[i]
	}
	return "int64_t"
}

// CTypeOfParamByName 按形参名查 C 类型
func (e *Emitter) CTypeOfParamByName(name string) string {
	for i, p := range e.Params {
		if p == name {
			return e.CTypeOfParam(i)
		}
	}
	return "int64_t"
}

// HasResult 判断是否有返回值字段
func (e *Emitter) HasResult() bool {
	return e.CReturnType != "" && e.CReturnType != "void"
}

// IsStateful 报告该 conc fn 是否是双形态（有挂起点）
//
// 由 SetStateful / Generate 设置；GenerateSpawn 读它来决定写入哪些 Frame 字段。
func (e *Emitter) IsStateful() bool { return e.stateful }

// SetStateful 由 codegen 在 lowering 开始时设置形态判定
func (e *Emitter) SetStateful(v bool) { e.stateful = v }

// SetGuardLayout 固化守卫数组布局；GenerateSpawn 可能先于 Generate 被调用，
// 所以深度与 for-in 标记必须随 SetStateful 一起提前登记。
func (e *Emitter) SetGuardLayout(compoundDepth int, hasForIn bool) {
	e.compoundDepth = compoundDepth
	e.hasForIn = hasForIn
}

// GuardDepth 返回守卫数组深度（CompoundDepth+1，含 body 层 sub[0]）
func (e *Emitter) GuardDepth() int {
	d := e.compoundDepth + 1
	if d > 8 {
		d = 8
	}
	return d
}

// HasForIn 返回是否需要生成 for..in 迭代状态数组
func (e *Emitter) HasForIn() bool { return e.hasForIn }

// HasGuard 返回 Frame 是否含 sub/branch 守卫槽（compoundDepth > 0）
func (e *Emitter) HasGuard() bool { return e.compoundDepth > 0 }

// ---------- resume ----------

func (e *Emitter) generateSimpleResume(bodyCode string) string {
	var b strings.Builder

	b.WriteString("/* Resume for conc fn " + e.FuncName + "\n")
	b.WriteString(" * 无挂起点：一次转发，再返回 DONE。\n")
	b.WriteString(" * 这一形态让 conc fn 在不加 spawn 时等价于普通函数调用。 */\n")
	b.WriteString("KaulaResumeResult " + e.ResumeFn() + "(void* frame_ptr) {\n")
	b.WriteString("    (void)frame_ptr;\n")
	if e.HasResult() {
		// 结果已经由 shim 算好；这里没有 Frame 字段可指向，
		// 故只报告完成。结果由 spawn 路径通过 shim 之外的通道取，
		// 无挂起点时这不是问题 —— 没有跨挂起的中间状态需要保存。
		b.WriteString("    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
	} else {
		b.WriteString("    return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
	}
	b.WriteString("}\n\n")
	_ = bodyCode
	return b.String()
}

/*
 * generateStatefulResume 产出双形态的 resume：一个 switch，pc 是下标。
 *
 * 关键结构：第 i 个 case 承载第 i 段函数体。
 *
 * 早期版本把所有段拼起来塞进 case 0，把每个恢复 case 写成空壳 ——
 * 于是挂起后再 resume 时直接返回 DONE，而 f->result 从没被写过，
 * await 恒取到零值。这类错误不会崩，只会静默给错答案。
 *
 * 每段开头都要把跨挂起点存活的变量从 Frame 恢复成本地变量：上一段
 * 是在另一次 resume 调用（另一个栈帧）里跑的，那些局部早已随那次返回销毁。
 */
func (e *Emitter) generateStatefulResume(analysis *BodyAnalysis, segments []string) string {
	var b strings.Builder

	b.WriteString("/* Resume for conc fn " + e.FuncName + "\n")
	b.WriteString(fmt.Sprintf(" * %d 个挂起点 -> %d 段状态。pc 从入口顺序推进，\n",
		len(analysis.SuspendSites), len(segments)))
	b.WriteString(" * 每次 resume 只跑到下一个挂起点就返回，把 Worker 让出去。 */\n")
	b.WriteString("KaulaResumeResult " + e.ResumeFn() + "(void* frame_ptr) {\n")
	b.WriteString("    " + e.FrameType() + "* f = (" + e.FrameType() + "*)frame_ptr;\n")
	b.WriteString("    switch (f->pc) {\n")

	for pc, seg := range segments {
		b.WriteString(fmt.Sprintf("    case %d: {\n", pc))
		if pc == 0 {
			b.WriteString("        /* 入口：参数由 spawn 时写入 Frame */\n")
		} else {
			kind := SuspendNone
			if pc-1 < len(analysis.SuspendSites) {
				kind = analysis.SuspendSites[pc-1].Kind
			}
			// 行号只是注释：解析器不填表达式位置，它通常是 0。
			// 真正决定恢复哪一批变量的是序号，不是这里打印的行号。
			b.WriteString(fmt.Sprintf("        /* 从第 %d 个挂起点恢复（%s） */\n", pc, kind))
		}
		/*
		 * 活到本段的变量：参数 + 早于本段声明的局部。
		 *
		 * 参数每段都要重建 —— 挂起前把参数写回了 Frame，
		 * 而 resume 的栈帧是全新的，不重建就是读未初始化值。
		 *
		 * 局部变量同理，且**第 0 段也要**：守卫模式下第 0 段也可能被重入
		 * （复合语句体内部的挂起会把 pc 留在 0 段），此时恢复点之后的
		 * 循环变量只剩 Frame 里的那一份。
		 */
		for _, p := range e.Params {
			b.WriteString(fmt.Sprintf("        %s %s = f->%s;\n", e.CTypeOfParamByName(p), p, p))
		}
		for _, v := range e.frameVarsLiveAt(analysis, pc) {
			b.WriteString(fmt.Sprintf("        %s %s = f->%s;\n",
				e.FrameVarCType(v.Name), v.Name, v.Name))
		}

		b.WriteString(seg)

		// 段尾兜底：段内既没挂起也没 return 时落到这里
		b.WriteString("        f->pc = -1;\n")
		if e.HasResult() {
			b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = &f->result };\n")
		} else {
			b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
		}
		b.WriteString("    }\n")
	}

	b.WriteString("    default:\n")
	b.WriteString("        /* 已完成（pc = -1）：不应再次进入 */\n")
	b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
	b.WriteString("    }\n")
	b.WriteString("}\n\n")
	return b.String()
}

/*
 * frameVarsLiveAt 报告恢复到第 pc 段时，哪些跨 suspend 变量需要有恢复副本。
 *
 * 判据用遍历序号（见 SuspendSite.Ordinal）：变量声明在第 k 个挂起点之前，
 * 就必须在第 k 个挂起点处已存在。
 *
 * 界取「本段最靠后的那个挂起点」：那一处是最早的恢复位置，必须恢复的变量最多。
 * 守卫模式（RestoreAll）下界是 RestoreAll —— 段体的声明被降级成赋值，
 * 对象统一由这里的恢复副本提供，少一个就编译不过。
 */
func (e *Emitter) frameVarsLiveAt(analysis *BodyAnalysis, pc int) []VarInfo {
	if len(analysis.FrameVars) == 0 {
		return nil
	}
	bound := analysis.RestoreOrdinal(pc)
	if bound < 0 {
		return nil
	}
	var out []VarInfo
	for _, v := range analysis.FrameVars {
		if analysis.LiveAt(v, bound) {
			out = append(out, v)
		}
	}
	return out
}

// ---------- coercion shim ----------

/*
 * coercion shim（文档 §9.4）
 *
 * conc fn 被当作普通 fn 使用时（回调、C FFI thunk、泛型约束），
 * 编译器生成这个包装：栈上建 Frame，循环驱动 resume 直到完成。
 *
 * 关键：顺序路径下 BudgetExhausted 分支被消除 —— 那里根本没有预算概念。
 * SUSPEND 走 kaula_iodriver_blocking_wait 就地等待，不经过调度器。
 */
func (e *Emitter) generateShim(bodyCode string, stateful bool) string {
	var b strings.Builder

	b.WriteString("/* Coercion shim: conc fn -> fn（" + e.FuncName + "） */\n")

	if e.CReturnType == "" || e.CReturnType == "void" {
		b.WriteString("void " + e.ShimFn() + "(")
	} else {
		b.WriteString(e.CReturnType + " " + e.ShimFn() + "(")
	}

	for i, p := range e.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%s %s", e.CTypeOfParam(i), p))
	}
	if len(e.Params) == 0 {
		b.WriteString("void")
	}
	b.WriteString(") {\n")

	if !stateful {
		/*
		 * 单一形态：无挂起点。
		 *
		 * 顺序调用直接执行函数体 —— 不进 resume、不进调度器 ——
		 * 这就是设计说明书 §2.7「顺序调用不比同名 fn 慢」的兑现：
		 * 调度相关的代码整条消失，不是"开销很小"，而是不存在。
		 *
		 * 但 spawn 仍需要一个 void*(void*) 入口给内核的 trampoline 调，
		 * 故额外产出一个 simple 包装：从 Frame 取参、调函数体、存结果。
		 */
		b.WriteString("/* 无挂起点：顺序调用直接执行，无 resume / 无调度 */\n")
		b.WriteString(bodyCode)
		b.WriteString("}\n\n")

		/*
		 * spawn 入口：供内核 trampoline 调用的 void*(void*)。
		 *
		 * 它只是"从 Frame 取参数 → 调 shim → 把结果写回 Frame"的一层壳。
		 * 不重复函数体：函数体已经在上面的 shim 里，
		 * 复制一份既浪费代码体积，又让两条路径有漂移风险
		 * （顺序调用与 spawn 的行为不一致）。
		 */
		b.WriteString("/* spawn 入口：把 Frame 的参数喂给 shim，结果写回 */\n")
		b.WriteString("static void* " + e.SimpleEntryFn() + "(void* frame_ptr) {\n")
		b.WriteString("    " + e.FrameType() + "* f = (" + e.FrameType() + "*)frame_ptr;\n")

		var args []string
		for _, p := range e.Params {
			args = append(args, "f->"+p)
		}
		call := e.ShimFn() + "(" + strings.Join(args, ", ") + ")"
		if e.HasResult() {
			/*
			 * 返回**结果的地址**，不是结果本身。
			 *
			 * 必须与双形态 resume 的 `.value = &f->result` 严格一致 ——
			 * await 只认一种约定。两条路径约定不同的话，
			 * await 会在其中一条上静默解引用一个当量整数。
			 *
			 * 而且结果类型可能是结构体（如 String），位模式转 void* 编不过；
			 * 给地址是唯一能同时覆盖标量与结构体的形式。
			 *
			 * 代价：Frame 必须活到 await 读完为止。
			 * 由内核的 epoch 退休机制保证（见 kaula_task_complete）。
			 */
			b.WriteString("    f->result = " + call + ";\n")
			b.WriteString("    return (void*)&f->result;\n")
		} else {
			b.WriteString("    " + call + ";\n")
			b.WriteString("    return (void*)0;\n")
		}
		b.WriteString("}\n\n")
		return b.String()
	}

	/* 顺序调用路径：帧在调用者栈上，不堆分配、不进调度器。 */
	b.WriteString("    " + e.FrameType() + " frame;\n")
	b.WriteString("    memset(&frame, 0, sizeof(frame));\n")
	b.WriteString("    frame.pc = 0;\n")
	for _, p := range e.Params {
		b.WriteString(fmt.Sprintf("    frame.%s = %s;\n", p, p))
	}

	b.WriteString("    for (;;) {\n")
	b.WriteString("        KaulaResumeResult r = " + e.ResumeFn() + "(&frame);\n")
	b.WriteString("        switch (r.status) {\n")
	b.WriteString("        case KAULA_RESUME_DONE:\n")
	if e.CReturnType != "" && e.CReturnType != "void" {
		b.WriteString("            return frame.result;\n")
	} else {
		b.WriteString("            return;\n")
	}
	b.WriteString("        case KAULA_RESUME_SUSPEND:\n")
	b.WriteString("            /* 顺序模式：就地等 IO 完成，不经过调度器 */\n")
	b.WriteString("            kaula_conc_tls_io_result = kaula_iodriver_blocking_wait(&r.io_request);\n")
	b.WriteString("            break;\n")
	b.WriteString("        case KAULA_RESUME_BLOCKED:\n")
	b.WriteString("            /* 顺序模式：直接执行阻塞操作 */\n")
	b.WriteString("            break;\n")
	b.WriteString("        case KAULA_RESUME_BUDGET_EXHAUSTED:\n")
	b.WriteString("            /* 顺序路径下此分支编译期消除（这里不会到达） */\n")
	b.WriteString("            break;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n")
	b.WriteString("}\n\n")
	return b.String()
}

// ---------- spawn ----------

/*
 * GenerateSpawn 产出 spawn 表达式的 C 代码。
 *
 * 注意与 Frame 字段名的对应：Frame 里参数字段就是参数名，
 * 所以这里写 f-><参数名>，不是 f->arg_N。
 * （早期版本用 arg_N，与 Frame 定义不一致，编译必然失败。）
 */
func (e *Emitter) GenerateSpawn(argExprs []string, flags uint32) string {
	var b strings.Builder

	/*
	 * spawn 的值类型是 KaulaHandle（Kaula 层的 Handle）。
	 * 早期版本写成 `({ ... kaula_spawn(...); })` —— 语句表达式的值
	 * 是最后一条语句的值，而 kaula_spawn 的返回值被丢弃，
	 * 于是 spawn 表达式退化成 void*，赋给 Handle 就类型不符。
	 * 这里显式构造并返回句柄。
	 */
	b.WriteString("({\n")
	b.WriteString(fmt.Sprintf("    %s* __frame = (%s*)kmm_v4_alloc_global(sizeof(%s));\n",
		e.FrameType(), e.FrameType(), e.FrameType()))
	b.WriteString("    KaulaHandle __h = kaula_handle_null();\n")
	b.WriteString("    if (__frame != NULL) {\n")
	/*
	 * 免 memset：Frame 里所有字段都是先写后读 ——
	 *   pc/参数      spawn 时全量赋值
	 *   result       由 resume/simple 在 DONE 前写入
	 *   io_request   经 KaulaResumeResult 返回，不在 Frame 内
	 * 唯一需要显式归零的是守卫槽（恢复时首次进入读 sub[d]==0 判定
	 * 「从头开始」）与 needs_free 预留位。整帧 memset 在 Frame 160B
	 * 时代每任务白付一次，纯开销。
	 */
	if e.IsStateful() {
		b.WriteString("        __frame->pc = 0;\n")
		if e.HasGuard() {
			for d := 0; d < e.GuardDepth(); d++ {
				b.WriteString(fmt.Sprintf("        __frame->sub[%d] = 0;\n", d))
				b.WriteString(fmt.Sprintf("        __frame->branch[%d] = 0;\n", d))
			}
			if e.HasForIn() {
				for d := 0; d < e.GuardDepth(); d++ {
					b.WriteString(fmt.Sprintf("        __frame->loop_i[%d] = 0;\n", d))
					b.WriteString(fmt.Sprintf("        __frame->loop_end[%d] = 0;\n", d))
					b.WriteString(fmt.Sprintf("        __frame->loop_step[%d] = 0;\n", d))
				}
			}
		}
		b.WriteString("        __frame->needs_free = 0;\n")
		for i, arg := range argExprs {
			if i < len(e.Params) {
				b.WriteString(fmt.Sprintf("        __frame->%s = %s;\n", e.Params[i], arg))
			}
		}
		if flags != 0 {
			/*
			 * #[hint(blocking)]：用 kaula_spawn_flags 在任务对 Worker
			 * 可见之前设好标志 —— 事后写 flags 与 Worker 读 flags 是数据竞争。
			 */
			b.WriteString(fmt.Sprintf(
				"        __h = kaula_spawn_flags(%s, __frame, sizeof(*__frame), %du);\n",
				e.ResumeFn(), flags))
		} else {
			b.WriteString(fmt.Sprintf("        __h = kaula_spawn(%s, __frame, sizeof(*__frame));\n",
				e.ResumeFn()))
		}
	} else {
		/*
		 * 单一形态：没有 pc，也不走 resume。
		 * 但仍须交给内核执行 —— 否则 spawn 就失去了"并发提交"的语义。
		 * Frame 在这里只是参数包与结果槽。
		 *
		 * #[hint(blocking)] 标志在这里被忽略：单一形态没有状态机、
		 * 永远不会返回 KAULA_RESUME_BLOCKED，车道无从接管；
		 * 这类任务按普通 fn 提交，Worker 就地执行（I6 的"有栈逃逸"边界）。
		 */
		/*
		 * 参数必须搬进 Frame。
		 *
		 * 入口只拿到 void* frame_ptr，没有第二个参数通道，
		 * 所以参数只能随帧一起交过去。
		 * 漏掉这一步的症状很隐蔽：入口照常执行、照常返回，
		 * 但参数全是 memset 清出来的 0 —— 结果恒为"拿 0 当参数算一遍"。
		 */
		for i, arg := range argExprs {
			if i < len(e.Params) {
				b.WriteString(fmt.Sprintf("        __frame->%s = %s;\n", e.Params[i], arg))
			}
		}
		b.WriteString(fmt.Sprintf("        __h = kaula_spawn_simple(%s, __frame, sizeof(*__frame));\n",
			e.SimpleEntryFn()))
	}
	b.WriteString("    }\n")
	b.WriteString("    __h;\n")
	b.WriteString("})")
	return b.String()
}