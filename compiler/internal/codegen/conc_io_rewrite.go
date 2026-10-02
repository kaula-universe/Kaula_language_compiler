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
	case "net_connect":
		return "KAULA_IO_CONNECT", true
	case "net_accept":
		return "KAULA_IO_ACCEPT", true
	case "time_sleep", "time_after":
		return "KAULA_IO_SLEEP", true
	}
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
		// 记下来，concSuspendReturn 要把它填进 KaulaResumeResult.io_request
		cg.lastIoVar = varName

		var b []byte
		b = append(b, fmt.Sprintf("KaulaIoRequest %s = {0};\n", varName)...)
		b = append(b, fmt.Sprintf("%s.type = %s;\n", varName, op)...)

		switch op {
		case "KAULA_IO_SLEEP":
			/*
			 * time_sleep 的实参是毫秒，内核按纳秒算。
			 * 乘法放在这里而不是内核里：Kaula 层的单位约定不该
			 * 渗进内核，内核只认 ns。
			 */
			if len(argCodes) > 0 {
				b = append(b, fmt.Sprintf("%s.timeout_ns = (uint64_t)(%s) * 1000000ull;\n",
					varName, argCodes[0])...)
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
				b = append(b, fmt.Sprintf("%s.fd = (int)(%s);\n", varName, argCodes[0])...)
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

		return string(b) + varName, true
	}

	return "", false
}

// noteConcIoUse 标记本 TU 用到了并发内核
func (cg *CodeGenerator) noteConcIoUse() {
	codeUsesConcurrency = true
}