package conc

import (
	"testing"

	"kaula/internal/ast"
)

// ---------- 测试辅助 ----------

func pos(line int) ast.Position { return ast.Position{Line: line} }

func ident(name string) *ast.Identifier {
	return &ast.Identifier{Name: name}
}

func call(name string, line int, args ...ast.Expression) *ast.CallExpression {
	return &ast.CallExpression{
		Function: ident(name),
		Args:     args,
		Pos:      pos(line),
	}
}

func decl(name string, line int, val ast.Expression) *ast.VariableDeclaration {
	return &ast.VariableDeclaration{Name: name, Value: val, Pos: pos(line)}
}

func exprStmt(e ast.Expression, line int) *ast.ExpressionStatement {
	return &ast.ExpressionStatement{Expression: e, Pos: pos(line)}
}

func ret(v ast.Expression, line int) *ast.ReturnStatement {
	return &ast.ReturnStatement{Value: v, Pos: pos(line)}
}

// ---------- Lookup ----------

func TestLookup(t *testing.T) {
	if k := Lookup("net_recv"); k != SuspendIO {
		t.Errorf("net_recv 应为 SuspendIO，实际 %v", k)
	}
	if k := Lookup("time_sleep"); k != SuspendSleep {
		t.Errorf("time_sleep 应为 SuspendSleep，实际 %v", k)
	}
	/*
	 * connect / accept 必须不在表里。
	 *
	 * 内核的 IO 执行是 read/write，socket 专属的 connect/accept 表达不了。
	 * 若在这里登记，它们会被切成挂起点却在 lowering 阶段找不到操作码 ——
	 * 生成的 C 编译得过、跑得动，只是 Worker 原地阻塞，I4 静默失效。
	 */
	if k := Lookup("net_connect"); k != SuspendNone {
		t.Errorf("net_connect 不应是挂起点（内核无对应操作码），实际 %v", k)
	}
	if k := Lookup("net_accept"); k != SuspendNone {
		t.Errorf("net_accept 不应是挂起点（内核无对应操作码），实际 %v", k)
	}
	if k := Lookup("strlen"); k != SuspendNone {
		t.Errorf("普通函数应为 SuspendNone，实际 %v", k)
	}
	if k := Lookup("根本不存在"); k != SuspendNone {
		t.Errorf("未注册函数应为 SuspendNone，实际 %v", k)
	}
}

// TestSuspendPointTableNoSilentFailure 守住「挂起点表 ⊆ concIoTypeOf」铁律。
//
// 进 instrumentTable 却拿不到内核操作码的函数会被切成挂起点、却在 lowering
// 阶段查不到操作码 —— 生成的 C 编译得过、跑得动，只是 Worker 原地阻塞（I4
// 静默失效）。本测试锁死曾经漂移过的四个名字：它们要么根本不是真实 std 函数
// （net_read / web_get / web_post），要么不是单 fd read/write/sleep
// （subprocess_wait 是 waitpid）。前三者必须彻底退出挂起点表；subprocess_wait
// 改走阻塞车道（IsBlocking），而非静默失效。
func TestSuspendPointTableNoSilentFailure(t *testing.T) {
	for _, name := range []string{"net_read", "web_get", "web_post"} {
		if k := Lookup(name); k != SuspendNone {
			t.Errorf("%s 是幻影名且缺内核操作码，不应在挂起点表里，实际 %v", name, k)
		}
	}
	// subprocess_wait：退出 SuspendIO，但作为已知阻塞函数走阻塞车道。
	if k := Lookup("subprocess_wait"); k != SuspendNone {
		t.Errorf("subprocess_wait 不应再是 SuspendIO（内核无对应操作码），实际 %v", k)
	}
	if !IsBlocking("subprocess_wait") {
		t.Errorf("subprocess_wait 应登记为阻塞车道函数（MarkBlocking），实际未登记")
	}
}

func TestRegister(t *testing.T) {
	Register("my_custom_io", SuspendIO)
	if Lookup("my_custom_io") != SuspendIO {
		t.Error("Register 后应可查到")
	}
}

func TestSuspendKindString(t *testing.T) {
	cases := map[SuspendKind]string{
		SuspendNone:   "none",
		SuspendIO:     "IO",
		SuspendAwait:  "await",
		SuspendSleep:  "sleep",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("SuspendKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

// ---------- AnalyzeBody：无挂起点 ----------

func TestNoSuspend_NoFrameNeeded(t *testing.T) {
	body := []ast.Statement{
		decl("a", 1, ident("x")),
		exprStmt(call("strlen", 2, ident("a")), 2),
		ret(ident("a"), 3),
	}

	res := AnalyzeBody(body)

	if res.NeedsSuspend {
		t.Error("无挂起调用时 NeedsSuspend 应为 false")
	}
	if len(res.SuspendSites) != 0 {
		t.Errorf("不应有挂起点，实际 %d 个", len(res.SuspendSites))
	}
	if len(res.FrameVars) != 0 {
		t.Errorf("无挂起时不应提升变量，实际 %d 个", len(res.FrameVars))
	}
}

// ---------- AnalyzeBody：单个挂起点 ----------

func TestSingleSuspend_VarsPromoted(t *testing.T) {
	body := []ast.Statement{
		decl("url", 1, ident("u")),
		decl("resp", 2, call("net_recv", 2, ident("url"))),
		ret(ident("resp"), 3),
	}

	res := AnalyzeBody(body)

	if !res.NeedsSuspend {
		t.Error("含 net_recv 时 NeedsSuspend 应为 true")
	}
	if len(res.SuspendSites) != 1 {
		t.Fatalf("应有 1 个挂起点，实际 %d", len(res.SuspendSites))
	}
	if res.SuspendSites[0].Kind != SuspendIO {
		t.Errorf("挂起点类型应为 IO，实际 %v", res.SuspendSites[0].Kind)
	}
	if res.SuspendSites[0].Line != 2 {
		t.Errorf("挂起点行号应为 2，实际 %d", res.SuspendSites[0].Line)
	}
	// resp 跨 suspend 存活（挂起后被 return 读取），必须提升
	if len(res.FrameVars) == 0 {
		t.Fatal("有挂起点时应提升变量到 Frame")
	}
	found := false
	for _, v := range res.FrameVars {
		if v.Name == "resp" {
			found = true
		}
	}
	if !found {
		t.Error("resp 应被提升到 Frame")
	}
}

// ---------- 多挂起点按源码顺序 ----------

func TestMultipleSuspends_OrderPreserved(t *testing.T) {
	body := []ast.Statement{
		exprStmt(call("net_recv", 10), 10),
		exprStmt(call("net_send", 20), 20),
		exprStmt(call("time_sleep", 30), 30),
	}

	res := AnalyzeBody(body)

	if len(res.SuspendSites) != 3 {
		t.Fatalf("应有 3 个挂起点，实际 %d", len(res.SuspendSites))
	}
	wantLines := []int{10, 20, 30}
	wantKinds := []SuspendKind{SuspendIO, SuspendIO, SuspendSleep}
	for i, site := range res.SuspendSites {
		if site.Line != wantLines[i] {
			t.Errorf("第 %d 个挂起点行号 = %d, want %d", i, site.Line, wantLines[i])
		}
		if site.Kind != wantKinds[i] {
			t.Errorf("第 %d 个挂起点类型 = %v, want %v", i, site.Kind, wantKinds[i])
		}
	}
}

// ---------- 嵌套调用中的挂起 ----------

func TestNestedCallSuspend(t *testing.T) {
	// f(net_recv(...)) —— 挂起点在实参里
	inner := call("net_recv", 5)
	outer := call("process", 5, inner)

	body := []ast.Statement{exprStmt(outer, 5)}

	res := AnalyzeBody(body)
	if len(res.SuspendSites) != 1 {
		t.Fatalf("应识别出嵌套在实参里的挂起点，实际 %d", len(res.SuspendSites))
	}
}

// ---------- 控制流内 ----------

func TestSuspendInsideIfBody(t *testing.T) {
	body := []ast.Statement{
		&ast.IfStatement{
			Condition: ident("ready"),
			Body:      []ast.Statement{exprStmt(call("net_send", 7), 7)},
			Pos:       pos(6),
		},
	}

	res := AnalyzeBody(body)
	if len(res.SuspendSites) != 1 {
		t.Errorf("if 分支内的挂起应被识别，实际 %d 个", len(res.SuspendSites))
	}
}

func TestSuspendInsideWhileBody(t *testing.T) {
	body := []ast.Statement{
		&ast.WhileStatement{
			Condition: ident("more"),
			Body:      []ast.Statement{exprStmt(call("net_recv", 9), 9)},
			Pos:       pos(8),
		},
	}

	res := AnalyzeBody(body)
	if len(res.SuspendSites) != 1 {
		t.Errorf("while 分支内的挂起应被识别，实际 %d 个", len(res.SuspendSites))
	}
}

func TestSuspendInsideNestedBlocks(t *testing.T) {
	body := []ast.Statement{
		&ast.BlockStatement{
			Statements: []ast.Statement{
				&ast.BlockStatement{
					Statements: []ast.Statement{
						exprStmt(call("net_recv", 15), 15),
					},
					Pos: pos(14),
				},
			},
			Pos: pos(13),
		},
	}

	res := AnalyzeBody(body)
	if len(res.SuspendSites) != 1 {
		t.Errorf("深层嵌套块内的挂起应被识别，实际 %d 个", len(res.SuspendSites))
	}
}

// ---------- 变量声明覆盖各作用域 ----------

func TestVarsFromAllScopes(t *testing.T) {
	body := []ast.Statement{
		decl("a", 1, ident("x")),
		&ast.IfStatement{
			Condition: ident("c"),
			Body: []ast.Statement{
				decl("b", 3, ident("y")),
			},
			Else: []ast.Statement{
				decl("c2", 5, ident("z")),
			},
			Pos: pos(2),
		},
		&ast.ForStatement{
			Init:      decl("i", 7, ident("0")),
			Condition: ident("cond"),
			Body:      []ast.Statement{exprStmt(call("net_recv", 9), 9)},
			Pos:       pos(7),
		},
	}

	res := AnalyzeBody(body)

	names := map[string]bool{}
	for _, v := range res.FrameVars {
		names[v.Name] = true
	}
	for _, want := range []string{"a", "b", "c2", "i"} {
		if !names[want] {
			t.Errorf("变量 %s 应被提升到 Frame", want)
		}
	}
}

// ---------- 声明行号被记录 ----------

func TestDeclLineRecorded(t *testing.T) {
	body := []ast.Statement{
		decl("first", 42, ident("x")),
		exprStmt(call("net_recv", 50), 50),
	}

	res := AnalyzeBody(body)
	if len(res.FrameVars) == 0 {
		t.Fatal("应有 Frame 变量")
	}
	if res.FrameVars[0].Name != "first" {
		t.Fatalf("首个变量应为 first，实际 %s", res.FrameVars[0].Name)
	}
	if res.FrameVars[0].DeclLine != 42 {
		t.Errorf("声明行号应为 42，实际 %d", res.FrameVars[0].DeclLine)
	}
}

// ---------- CalleeName ----------

func TestCalleeName(t *testing.T) {
	if got := CalleeName(call("foo", 1)); got != "foo" {
		t.Errorf("标识符调用应得 foo，实际 %s", got)
	}

	member := &ast.CallExpression{
		Function: &ast.MemberAccessExpression{
			Object: ident("resp"),
			Member: "text",
		},
	}
	if got := CalleeName(member); got != "text" {
		t.Errorf("成员访问应取最后一段 text，实际 %s", got)
	}
}

// ---------- HasSuspendCall ----------

func TestHasSuspendCall(t *testing.T) {
	if HasSuspendCall(call("strlen", 1)) {
		t.Error("普通调用不应报告挂起")
	}
	if !HasSuspendCall(call("net_recv", 1)) {
		t.Error("IO 调用应报告挂起")
	}
}

// ---------- 保守性：未知函数不假设挂起 ----------

func TestUnknownCallNotAssumedSuspend(t *testing.T) {
	// 显式保证：未注册的函数不当成挂起点，
	// 否则每个 conc fn 都会被迫生成状态机。
	res := AnalyzeBody([]ast.Statement{
		exprStmt(call("some_unregistered_fn", 1), 1),
	})
	if res.NeedsSuspend {
		t.Error("未注册函数不应触发生成状态机")
	}
}

// ---------- blocking 标记 ----------

func TestBlockingHint(t *testing.T) {
	if IsBlocking("thread_join") {
		t.Error("初始不应有任何 blocking 标记")
	}
	MarkBlocking("thread_join")
	if !IsBlocking("thread_join") {
		t.Error("MarkBlocking 后应可查到")
	}
}