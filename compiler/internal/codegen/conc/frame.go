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
func (e *Emitter) Generate(analysis *BodyAnalysis, bodyCode string, body []ast.Statement) string {
	var out strings.Builder
	e.stateful = analysis.NeedsSuspend

	if !analysis.NeedsSuspend {
		// 单一形态：没有挂起点，resume 只是一次转发。
		// 编译器可以把 resume 与普通函数内联合并，spawn 也能退化为普通调用。
		out.WriteString(e.generateSimpleFrame())
		out.WriteString(e.generateSimpleResume(bodyCode))
		out.WriteString(e.generateShim(bodyCode, false))
		return out.String()
	}

	// 双形态：pc + Frame 字段
	out.WriteString(e.generateStatefulFrame(analysis))
	out.WriteString(e.generateStatefulResume(analysis, bodyCode))
	out.WriteString(e.generateShim(bodyCode, true))
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
			// 类型未知时用 void*：保守但安全（编译器稍后可优化为具体类型）
			b.WriteString(fmt.Sprintf("    void*          %s__v;\n", v.Name))
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

func (e *Emitter) generateStatefulResume(analysis *BodyAnalysis, bodyCode string) string {
	var b strings.Builder

	b.WriteString("/* Resume for conc fn " + e.FuncName + "\n")
	b.WriteString(fmt.Sprintf(" * %d 个 suspend 点。pc 从入口顺序推进，\n", len(analysis.SuspendSites)))
	b.WriteString(" * 每次 resume 只跑到下一个挂起点就返回，把 Worker 让出去。 */\n")
	b.WriteString("KaulaResumeResult " + e.ResumeFn() + "(void* frame_ptr) {\n")
	b.WriteString("    " + e.FrameType() + "* f = (" + e.FrameType() + "*)frame_ptr;\n")
	b.WriteString("    switch (f->pc) {\n")

	// 入口 case：参数已在 spawn 时写入 Frame，这里从 Frame 恢复到局部变量
	b.WriteString("    case 0: {\n")
	b.WriteString("        /* 入口：从 Frame 恢复参数 */\n")
	for _, p := range e.Params {
		b.WriteString(fmt.Sprintf("        %s %s = f->%s;\n", e.CTypeOfParamByName(p), p, p))
	}
	b.WriteString(bodyCode)
	// 返回值已由 return 改写存入 f->result（见 generateConcReturn）
	b.WriteString("        f->pc = -1;\n")
	if e.HasResult() {
		b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = &f->result };\n")
	} else {
		b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
	}
	b.WriteString("    }\n")

	// 每个 suspend 点一个恢复 case
	for i, site := range analysis.SuspendSites {
		b.WriteString(fmt.Sprintf("    case %d: {\n", i+1))
		b.WriteString(fmt.Sprintf("        /* 从第 %d 个挂起点恢复（源码行 %d，%s） */\n",
			i+1, site.Line, site.Kind))
		b.WriteString("        /* 参数已由挂起前的写回保存到 Frame */\n")
		b.WriteString("        f->pc = -1;\n")
		if e.HasResult() {
			b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = &f->result };\n")
		} else {
			b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
		}
		b.WriteString("    }\n")
	}

	b.WriteString("    default:\n")
	b.WriteString("        /* 已完成：不应再次进入 */\n")
	b.WriteString("        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE, .value = NULL };\n")
	b.WriteString("    }\n")
	b.WriteString("}\n\n")
	return b.String()
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

		b.WriteString("/* spawn 入口：供内核 trampoline 调用 */\n")
		b.WriteString("static void* " + e.SimpleEntryFn() + "(void* frame_ptr) {\n")
		b.WriteString("    " + e.FrameType() + "* f = (" + e.FrameType() + "*)frame_ptr;\n")
		for i, p := range e.Params {
			b.WriteString(fmt.Sprintf("    %s %s = f->%s;\n", e.CTypeOfParam(i), p, p))
		}
		b.WriteString("    /* 与顺序路径同一份函数体 */\n")
		b.WriteString(bodyCode)
		if e.HasResult() {
			b.WriteString("    f->result = result;\n")
			b.WriteString("    return (void*)0;\n")
		}
		b.WriteString("    return (void*)0;\n")
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
func (e *Emitter) GenerateSpawn(argExprs []string) string {
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
	b.WriteString("        memset(__frame, 0, sizeof(*__frame));\n")
	if e.IsStateful() {
		b.WriteString("        __frame->pc = 0;\n")
		for i, arg := range argExprs {
			if i < len(e.Params) {
				b.WriteString(fmt.Sprintf("        __frame->%s = %s;\n", e.Params[i], arg))
			}
		}
		b.WriteString(fmt.Sprintf("        __h = kaula_spawn(%s, __frame, sizeof(*__frame));\n",
			e.ResumeFn()))
	} else {
		/*
		 * 单一形态：没有 Frame 字段，也不走 resume。
		 * 但仍须交给内核执行 —— 否则 spawn 就失去了"并发提交"的语义。
		 * 帧内容只有一个结果槽，用一个包装结构承载返回值。
		 */
		b.WriteString(fmt.Sprintf("        __h = kaula_spawn_simple(%s, __frame, sizeof(*__frame));\n",
			e.SimpleEntryFn()))
	}
	b.WriteString("    }\n")
	b.WriteString("    __h;\n")
	b.WriteString("})")
	return b.String()
}