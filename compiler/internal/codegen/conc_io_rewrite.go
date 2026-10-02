package codegen

import (
	"fmt"
	"strings"

	"kaula/internal/ast"
)

/*
 * concIoRewriteMode 决定可插桩调用在 resume 里的生成形态。
 *
 * 一次挂起把调用劈成两半（I4）：
 *
 *	submit 段  算出实参 → 填 KaulaIoRequest → 交 IoDriver → 归还 Worker
 *	result 段  恢复后从 kaula_conc_tls_io_result 取回结果
 *
 * 同一条源码语句因此要按两种模式各生成一次：
 * 挂起点之前的语句走 submit，挂起点之后的走 result。
 */
type concIoRewriteMode int

const (
	// concIoNone 不改写：普通调用语义（顺序路径、以及非 conc fn）
	concIoNone concIoRewriteMode = iota
	// concIoSubmit 生成 IO 请求构造 + 提交
	concIoSubmit
	// concIoResult 生成从 TLS 取回结果
	concIoResult
)

// concIoTypeOf 把可插桩函数映射到内核的 KaulaIoType。
//
// 编译器与内核必须对同一批函数给同一批 op 码 —— 这是文档 §9.1 的 ABI 约定。
// 映射不上就返回 ok=false，调用点退回普通语义（保守：退化成阻塞，
// 但不会生成错误的 op 码让内核去执行错的操作）。
func concIoTypeOf(name string) (op string, ok bool) {
	/*
	 * 只看最后一段。
	 *
	 * 可插桩函数在源码里几乎都是模块限定的（std.time.time_sleep），
	 * 而分析阶段用的是裸名（conc.CalleeName 取 Member）。
	 * 两边不一致的话，挂起点识别得出、lowering 却找不到 —— 症状是
	 * 挂起点被当成普通语句，Worker 原地阻塞（回到 I4 失效的老路）。
	 */
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}

	switch name {
	case "net_recv", "fs_read":
		return "KAULA_IO_READ", true
	case "net_send", "net_write", "fs_write":
		return "KAULA_IO_WRITE", true
	case "time_sleep", "time_sleep_us", "time_sleep_ns", "time_sleep_seconds", "time_after":
		return "KAULA_IO_SLEEP", true
	}
	/*
	 * net_connect / net_accept 不在此表。
	 *
	 * connect / accept 是 socket 专属操作，read/write 表达不了，
	 * 内核不实现。这里不登记，调用点就退回普通阻塞语义 —— 行为正确，
	 * 只是那个 Worker 会真的卡住。登记成操作码再让内核回 ENOSYS
	 * 才是错的：那会把"退化成阻塞"换成"静默给出操作失败"。
	 *
	 * net_recv / net_send 在表里是安全的：内核的 IO 执行用 read/write，
	 * 而 POSIX 上 socket 就是 fd，同一条路径通吃。
	 */
	return "", false
}

// concIoCallRewrite 在 conc fn 体内生成可插桩调用的替换形态。
//
// 返回 (code, true) 表示已改写；(code, false) 表示调用点自行按普通语义生成。
func (eg *ExpressionGenerator) concIoCallRewrite(
	call *ast.CallExpression, funcName string,
) (string, bool) {

	cg := eg.codegen
	if !cg.inConcResume || cg.concIoRewrite == concIoNone {
		return "", false
	}

	/*
	 * 归一化到裸名（std.time.time_sleep → time_sleep）。
	 *
	 * 分析阶段用的是裸名，这里若还用带前缀的名字做单位换算，
	 * switch 会全部落空、静默退回毫秒 —— 亚毫秒定时于是被放大 1000 倍，
	 * 而且不报错。
	 */
	if i := strings.LastIndexByte(funcName, '.'); i >= 0 {
		funcName = funcName[i+1:]
	}

	// 只有真正映射到内核 op 的函数才走这条路
	op, ok := concIoTypeOf(funcName)
	if !ok {
		return "", false
	}

	/*
	 * 实参按裸表达式生成。
	 *
	 * 可插桩那批函数的签名都很规整（标量 + 指针 + 长度），
	 * 走 stdlib 签名包装反而会把它们包成奇怪的形式。
	 * 代价是：若将来有需要完整签名包装的可插桩函数，要在这里补。
	 */
	var argCodes []string
	for _, a := range call.Args {
		argCodes = append(argCodes, eg.GenerateExpression(a))
	}

	switch cg.concIoRewrite {
	case concIoResult:
		/*
		 * 恢复后的结果。
		 *
		 * 内核在 resume 之前把 task->io_result 搬进 TLS
		 * （见 kaula_conc_worker.c），所以这里直接读。
		 */
		return "kaula_conc_tls_io_result", true

	case concIoSubmit:
		/*
		 * 提交形态：构造一个 KaulaIoRequest 局部量，返回它的名字。
		 *
		 * 返回标识符而不是复合字面量，是因为 concSuspendReturn 要在
		 * 更外层引用它 —— `return { .io_request = <这里> }` 需要一个
		 * 具名的、活到该 return 的对象。
		 */
		cg.concIoCounter++
		varName := fmt.Sprintf("__kaula_io_%d", cg.concIoCounter)
		// 记下来，挂起返回要把它填进 KaulaResumeResult.io_request
		cg.lastIoVar = varName
		cg.concSlotAwaits++

		var b []byte
		b = append(b, fmt.Sprintf("KaulaIoRequest %s = {0};\n", varName)...)
		b = append(b, fmt.Sprintf("%s.type = %s;\n", varName, op)...)

		switch op {
		case "KAULA_IO_SLEEP":
			/*
			 * 内核只认 ns。Kaula 层的单位约定（毫秒/微秒/纳秒/秒）
			 * 在这里换算掉，不该渗进内核。
			 *
			 * 四种粒度都要区分：只有毫秒版可插桩时，亚毫秒定时
			 * 无法成为挂起点，挂起路径的开销就测不出来
			 * （毫秒粒度把单任务吞吐钉死在 ~1000/s）。
			 */
			mult, unit := "1000000ull", "ms"
			switch funcName {
			case "time_sleep_us":
				mult, unit = "1000ull", "us"
			case "time_sleep_ns":
				mult, unit = "1ull", "ns"
			case "time_sleep_seconds":
				mult, unit = "1000000000ull", "s"
			}
			if len(argCodes) > 0 {
				b = append(b, fmt.Sprintf(
					"%s.timeout_ns = (uint64_t)(%s) * %s;  /* %s */\n",
					varName, argCodes[0], mult, unit)...)
			} else {
				b = append(b, fmt.Sprintf("%s.timeout_ns = 0;\n", varName)...)
			}

		default:
			/*
			 * fd 型 IO：(fd, buf, len) 或 (fd, buf, len, timeout)
			 *
			 * 只搬运前三个；其余实参无法从通用签名推出，
			 * 交给 Kaula 的 runtime 包装层解释。
			 */
			if len(argCodes) > 0 {
				/*
				 * 转成 KaulaIoFd，不写死 (int)。
				 *
				 * Windows 上 fd 装的是指针宽的 HANDLE，(int) 会把高位
				 * 截掉 —— 生成的 C 编译得过，运行时却拿着一个野句柄
				 * 去 _get_osfhandle，症状是"偶发失败"而不是报错。
				 * typedef 由内核头定义，两侧自动对齐。
				 */
				b = append(b, fmt.Sprintf("%s.fd = (KaulaIoFd)(%s);\n", varName, argCodes[0])...)
			}
			if len(argCodes) > 1 {
				b = append(b, fmt.Sprintf("%s.buf = (void*)(%s);\n", varName, argCodes[1])...)
			}
			if len(argCodes) > 2 {
				b = append(b, fmt.Sprintf("%s.buf_len = (size_t)(%s);\n", varName, argCodes[2])...)
			}
			if len(argCodes) > 3 {
				b = append(b, fmt.Sprintf("%s.timeout_ns = (uint64_t)(%s);\n",
					varName, argCodes[3])...)
			}
		}

		if !cg.concGuardActive() {
			/*
			 * 顶层两段式切分：挂起返回由 concSuspendReturn 在语句末尾统一发出，
			 * 那里的作用域能看到这条语句生成的 IO 请求局部量。
			 */
			return string(b) + varName, true
		}

		/*
		 * 守卫模式：挂起返回必须就地发出。
		 *
		 * 外层的挂起返回看不到这里的局部量 —— 它挂在复合语句的花括号之外，
		 * 而这条语句此刻正被包在循环体或分支体里。强行引用就是
		 * "use of undeclared identifier"。
		 *
		 * 恢复槽位取当前语句的下一个：concGuardSlot 在生成之前已经把
		 * concGuardSeq 推到 k+1，那正是「本语句提交完了」的落点。
		 * 一条语句里有多个可插桩调用时两个挂起会抢同一个槽位 ——
		 * 那种情况由 concCheckSupported 报诊断拦下。
		 */
		f := cg.concFrameVar
		ord := -1
		if cg.concAnalysis != nil {
			ord = cg.concAnalysis.SuspendOrdinalOf(call)
		}
		b = append(b, cg.concSaveLiveVars(ord)...)
		b = append(b, fmt.Sprintf("%s->pc = %d;\n", f, cg.concSegmentPC)...)
		b = append(b, fmt.Sprintf("%s->sub[%d] = %d;\n", f, cg.concGuardDepth, cg.concGuardSeq)...)
		b = append(b, fmt.Sprintf(
			"return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND, .io_request = %s, .value = NULL };\n",
			varName)...)

		return string(b) + varName, true
	}

	return "", false
}

// noteConcIoUse 标记本 TU 用到了并发内核
func (cg *CodeGenerator) noteConcIoUse() {
	codeUsesConcurrency = true
}