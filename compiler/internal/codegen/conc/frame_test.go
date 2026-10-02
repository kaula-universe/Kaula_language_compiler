package conc

import (
	"strings"
	"testing"

	"kaula/internal/ast"
)

func newEmitter(params ...string) *Emitter {
	e := &Emitter{
		FuncName:    "fetch",
		CName:       "K_fetch",
		ReturnType:  "i64",
		CReturnType: "int64_t",
		Params:      params,
	}
	for range params {
		e.ParamTypes = append(e.ParamTypes, "i64")
		e.ParamCTypes = append(e.ParamCTypes, "int64_t")
		e.ParamNullables = append(e.ParamNullables, false)
	}
	return e
}

// ---------- 单一形态：无挂起点 ----------

func TestGenerate_Simple_FrameHasNoPC(t *testing.T) {
	e := newEmitter("url")
	res := e.Generate(&BodyAnalysis{NeedsSuspend: false}, []string{""}, nil)

	if !strings.Contains(res, "K_fetch_Frame") {
		t.Error("应生成 Frame 类型")
	}
	if strings.Contains(res, "int             pc;") {
		t.Error("无挂起点时 Frame 不应有 pc 字段（单一形态）")
	}
	if !strings.Contains(res, "K_fetch_resume") {
		t.Error("应生成 resume 函数")
	}
	if !strings.Contains(res, "K_fetch_as_fn") {
		t.Error("应生成 coercion shim")
	}
}

/*
 * Frame 虽无 pc，但仍要承载参数与结果。
 *
 * 早期实现让单一形态的 Frame 是空结构体，理由是"无挂起点就不需要保存状态"。
 * 但 spawn 仍要把参数交给内核、在 Worker 上执行、再取回结果 ——
 * Frame 在这里是参数包与结果槽，不是状态机。
 */
func TestGenerate_Simple_FrameCarriesParamsAndResult(t *testing.T) {
	e := newEmitter("url")
	res := e.Generate(&BodyAnalysis{NeedsSuspend: false}, []string{""}, nil)

	if !strings.Contains(res, "int64_t url;") {
		t.Error("单一形态的 Frame 也必须承载参数（spawn 要传递实参）")
	}
	if !strings.Contains(res, "int64_t result;") {
		t.Error("单一形态的 Frame 必须有结果槽（内核要取回返回值）")
	}
}

func TestGenerate_Simple_ResumeHasNoSwitch(t *testing.T) {
	e := newEmitter()
	res := e.Generate(&BodyAnalysis{NeedsSuspend: false}, []string{"    /* body */\n"}, nil)

	if strings.Contains(res, "switch (f->pc)") {
		t.Error("无挂起点时 resume 不应有 switch（会引入分支开销）")
	}
	if !strings.Contains(res, "KAULA_RESUME_DONE") {
		t.Error("resume 应返回 DONE")
	}
}

/*
 * spawn 单一形态的 conc fn 时，内核 trampoline 需要一个 void*(void*) 入口 ——
 * 因为 Task 的 resume_fn 是固定签名的 KaulaResumeFn。
 * 而 shim 的签名是用户的真实签名 (i64, i64) -> i64，两者不兼容。
 */
func TestGenerate_Simple_HasSimpleEntryForKernel(t *testing.T) {
	e := newEmitter("url")
	res := e.Generate(&BodyAnalysis{NeedsSuspend: false}, []string{""}, nil)

	if !strings.Contains(res, "K_fetch_simple") {
		t.Error("单一形态需产出 void*(void*) 入口供内核 trampoline 调用")
	}
	if !strings.Contains(res, "static void* K_fetch_simple(void* frame_ptr)") {
		t.Error("simple 入口签名应为 void*(void*)")
	}
}

// ---------- 双形态：有挂起点 ----------

func TestGenerate_Stateful_FrameHasPCAndVars(t *testing.T) {
	e := newEmitter("url")
	analysis := &BodyAnalysis{
		NeedsSuspend: true,
		SuspendSites: []SuspendSite{
			{Call: call("net_recv", 10), Kind: SuspendIO, Line: 10},
		},
		FrameVars: []VarInfo{{Name: "resp", DeclLine: 11}},
	}

	res := e.Generate(analysis, []string{""}, nil)

	if !strings.Contains(res, "int             pc;") {
		t.Error("有挂起点时 Frame 应含 pc 字段")
	}
	if !strings.Contains(res, "int64_t url;") {
		t.Error("参数应进入 Frame 字段")
	}
	/*
	 * 跨 suspend 变量按真实 C 类型入 Frame，不用 void* 改名后缀。
	 * void* 能与任意整型互转，编译器不报错但值已经被截断 ——
	 * 宁可这里编译失败，也不要静默给错答案。
	 */
	if !strings.Contains(res, "resp;") {
		t.Error("跨 suspend 变量应进入 Frame")
	}
	if strings.Contains(res, "resp__v") {
		t.Error("Frame 字段不应使用 void* 占位：会静默截断整数值")
	}
	if !strings.Contains(res, "int64_t result;") {
		t.Error("返回值应进入 Frame 字段")
	}
}

func TestGenerate_Stateful_ResumeHasCasePerSuspend(t *testing.T) {
	e := newEmitter()
	analysis := &BodyAnalysis{
		NeedsSuspend: true,
		SuspendSites: []SuspendSite{
			{Call: call("net_connect", 5), Kind: SuspendIO, Line: 5},
			{Call: call("net_send", 6), Kind: SuspendIO, Line: 6},
			{Call: call("time_sleep", 7), Kind: SuspendSleep, Line: 7},
		},
	}

	// 3 个挂起点 → 4 段（第 3 段之后的收尾段）
	res := e.Generate(analysis, []string{"", "", "", ""}, nil)

	if !strings.Contains(res, "case 0:") {
		t.Error("应有入口 case 0")
	}
	for i := 1; i <= 3; i++ {
		marker := "case " + itoa(i) + ":"
		if !strings.Contains(res, marker) {
			t.Errorf("缺少 %s 分支（每个挂起点一个）", marker)
		}
	}
	if !strings.Contains(res, "switch (f->pc)") {
		t.Error("双形态 resume 应用 pc 做 switch 分派")
	}
}

// 回归：早期把所有段拼进 case 0、恢复 case 写成空壳，
// 挂起后 resume 直接返回 DONE，f->result 从没被写过 —— await 恒取零值。
func TestGenerate_Stateful_ResumeCarriesSegmentBody(t *testing.T) {
	e := newEmitter()
	analysis := &BodyAnalysis{
		NeedsSuspend: true,
		SuspendSites: []SuspendSite{
			{Call: call("net_recv", 5), Kind: SuspendIO, Line: 5},
		},
	}

	res := e.Generate(analysis, []string{"    /* 段0 */\n", "    /* 段1 */\n"}, nil)

	seg0 := strings.Index(res, "case 0:")
	seg1 := strings.Index(res, "case 1:")
	if seg0 < 0 || seg1 < 0 || seg1 < seg0 {
		t.Fatal("两个 case 应按源码顺序出现")
	}
	mid := res[seg0:seg1]
	if !strings.Contains(mid, "/* 段0 */") {
		t.Error("case 0 应承载第 0 段函数体")
	}
	if strings.Contains(mid, "/* 段1 */") {
		t.Error("case 0 不应包含第 1 段（否则挂起后走不到）")
	}
	after := res[seg1:]
	if !strings.Contains(after, "/* 段1 */") {
		t.Error("case 1 应承载第 1 段函数体")
	}
}

// ---------- coercion shim ----------

func TestShim_StackFrameNoAllocation(t *testing.T) {
	e := newEmitter("url")
	res := e.Generate(&BodyAnalysis{NeedsSuspend: true}, []string{""}, nil)

	if !strings.Contains(res, "K_fetch_Frame frame;") {
		t.Error("双形态 shim 应在栈上建 Frame")
	}
	if !strings.Contains(res, "frame.url = url;") {
		t.Error("shim 应把参数写入 Frame")
	}
}

func TestShim_SuspendUsesBlockingWait(t *testing.T) {
	e := newEmitter()
	res := e.Generate(&BodyAnalysis{NeedsSuspend: true}, []string{""}, nil)

	if !strings.Contains(res, "kaula_iodriver_blocking_wait") {
		t.Error("shim 的 SUSPEND 分支应就地等 IO，不经过调度器")
	}
	if !strings.Contains(res, "kaula_conc_tls_io_result") {
		t.Error("shim 应把 IO 结果写回 TLS 供 resume 读取")
	}
}

func TestShim_HandlesAllResumeStatuses(t *testing.T) {
	e := newEmitter()
	res := e.Generate(&BodyAnalysis{NeedsSuspend: true}, []string{""}, nil)

	for _, st := range []string{
		"KAULA_RESUME_DONE",
		"KAULA_RESUME_SUSPEND",
		"KAULA_RESUME_BLOCKED",
		"KAULA_RESUME_BUDGET_EXHAUSTED",
	} {
		if !strings.Contains(res, st) {
			t.Errorf("shim 未处理 %s", st)
		}
	}
}

/*
 * 生成的 C 注释必须闭合。
 *
 * 实际踩过的坑：shim 的头注释少了结尾的斜杠星号，
 * 后面的函数签名全被吞进注释里，clang 报的是莫名其妙的
 * "expected identifier or '('"，排查成本极高。
 */
func TestGeneratedCommentsAreClosed(t *testing.T) {
	for _, stateful := range []bool{false, true} {
		e := newEmitter("url")
		analysis := &BodyAnalysis{NeedsSuspend: stateful}
		if stateful {
			analysis.SuspendSites = []SuspendSite{
				{Call: call("net_recv", 1), Kind: SuspendIO, Line: 1},
			}
		}
		res := e.Generate(analysis, []string{"    /* body */\n"}, nil)

		open := strings.Count(res, "/*")
		close := strings.Count(res, "*/")
		if open != close {
			t.Errorf("stateful=%v: C 注释未闭合（/*=%d, */=%d）\n%s",
				stateful, open, close, res)
		}
	}
}

// ---------- spawn ----------

/*
 * spawn 必须按形参名写 Frame 字段。
 *
 * 早期实现写的是 __frame->arg_0，而 Frame 字段名是形参名 url ——
 * 两者对不上，编译必然失败。这是回归测试。
 */
func TestGenerateSpawn_UsesParameterNamesNotArgN(t *testing.T) {
	e := newEmitter("url", "count")
	e.SetStateful(true) // 双形态：Frame 有字段
	code := e.GenerateSpawn([]string{"\"a\"", "42"})

	if !strings.Contains(code, `__frame->url = "a";`) {
		t.Error("spawn 应按参数名写 Frame 字段")
	}
	if !strings.Contains(code, "__frame->count = 42;") {
		t.Error("spawn 应按参数名写 Frame 字段")
	}
	if strings.Contains(code, "arg_0") || strings.Contains(code, "arg_1") {
		t.Error("spawn 不应使用 arg_N 命名（与 Frame 定义不一致）")
	}
}

func TestGenerateSpawn_CallsResumeAndKernel(t *testing.T) {
	e := newEmitter("url")
	e.SetStateful(true)
	code := e.GenerateSpawn([]string{"x"})

	if !strings.Contains(code, "kaula_spawn(K_fetch_resume, __frame, sizeof(*__frame))") {
		t.Error("双形态 spawn 应调用 resume 函数与内核")
	}
	if !strings.Contains(code, "__frame->pc = 0;") {
		t.Error("双形态 spawn 应初始化 pc")
	}
}

/*
 * spawn 的值类型是 KaulaHandle。
 *
 * 早期实现写成 `({ … kaula_spawn(...); })` —— 语句表达式的值是最后一条
 * 语句的值，而 kaula_spawn 的返回值被丢弃，于是 spawn 表达式退化成 void*，
 * 赋给 Handle 时类型不符。必须显式构造并返回句柄。
 */
func TestGenerateSpawn_ReturnsHandle(t *testing.T) {
	e := newEmitter("url")
	e.SetStateful(true)
	code := e.GenerateSpawn([]string{"x"})

	if !strings.Contains(code, "KaulaHandle __h") {
		t.Error("spawn 必须构造并返回 KaulaHandle")
	}
	if !strings.Contains(code, "__h = kaula_spawn(") {
		t.Error("spawn 的返回值应赋给句柄变量")
	}
}

// 单一形态没有 pc 字段，写 __frame->pc 就是编译错误
func TestGenerateSpawn_SimpleFormNoPC(t *testing.T) {
	e := newEmitter("url")
	e.SetStateful(false)
	code := e.GenerateSpawn([]string{"x"})

	if strings.Contains(code, "__frame->pc") {
		t.Error("单一形态的 Frame 无 pc 字段，不应写 __frame->pc")
	}
	if !strings.Contains(code, "kaula_spawn_simple(K_fetch_simple") {
		t.Error("单一形态 spawn 应走 simple 入口（void*(void*)）")
	}
}

// 分配失败必须可观测（I3：不崩溃）
func TestGenerateSpawn_HandlesOOM(t *testing.T) {
	e := newEmitter()
	e.SetStateful(true)
	code := e.GenerateSpawn([]string{"x"})

	if !strings.Contains(code, "if (__frame != NULL)") {
		t.Error("spawn 应检查分配失败（I3：不崩溃）")
	}
	if !strings.Contains(code, "kaula_handle_null()") {
		t.Error("分配失败应返回空句柄")
	}
}

// ---------- 标识符 ----------

func TestGeneratedNames(t *testing.T) {
	e := newEmitter()
	if e.FrameType() != "K_fetch_Frame" {
		t.Errorf("FrameType = %s", e.FrameType())
	}
	if e.ResumeFn() != "K_fetch_resume" {
		t.Errorf("ResumeFn = %s", e.ResumeFn())
	}
	if e.ShimFn() != "K_fetch_as_fn" {
		t.Errorf("ShimFn = %s", e.ShimFn())
	}
	if e.SimpleEntryFn() != "K_fetch_simple" {
		t.Errorf("SimpleEntryFn = %s", e.SimpleEntryFn())
	}
}

// ---------- void 返回的 conc fn ----------

func TestGenerate_VoidReturn(t *testing.T) {
	e := &Emitter{
		FuncName:    "notify",
		CName:       "K_notify",
		CReturnType: "void",
	}
	res := e.Generate(&BodyAnalysis{NeedsSuspend: true}, []string{""}, nil)

	if !strings.Contains(res, "void K_notify_as_fn(void)") {
		t.Error("void 返回的 shim 签名应为 void")
	}
	if strings.Contains(res, "void* result;") {
		t.Error("void 返回不应有 result 字段")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// ---------- 分析 + 生成串起来 ----------

func TestEndToEnd_NoSuspendPath(t *testing.T) {
	body := []ast.Statement{
		decl("a", 1, ident("x")),
		exprStmt(call("strlen", 2, ident("a")), 2),
		ret(ident("a"), 3),
	}
	analysis := AnalyzeBody(body)
	e := newEmitter("x")
	res := e.Generate(analysis, []string{"    /* generated body */\n"}, body)

	if !strings.Contains(res, "generated body") {
		t.Error("函数体应被包含在产物中")
	}
	if strings.Contains(res, "switch (f->pc)") {
		t.Error("无挂起的函数应走单一形态")
	}
}

func TestEndToEnd_SuspendPath(t *testing.T) {
	body := []ast.Statement{
		decl("url", 1, ident("u")),
		decl("resp", 2, call("net_recv", 2, ident("url"))),
		ret(ident("resp"), 3),
	}
	analysis := AnalyzeBody(body)
	e := newEmitter("url")
	res := e.Generate(analysis, []string{"    /* generated body */\n"}, body)

	if !strings.Contains(res, "switch (f->pc)") {
		t.Error("有挂起的函数应走双形态")
	}
}