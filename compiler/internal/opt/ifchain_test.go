package opt

import (
	"testing"

	"kaula/internal/ast"
)

// ---------- AST 构造辅助 ----------

func idn(name string) *ast.Identifier {
	return &ast.Identifier{Name: name}
}

func lit(v uint64) *ast.IntegerLiteral {
	return &ast.IntegerLiteral{Value: v}
}

func eqCond(l, r ast.Expression) *ast.BinaryExpression {
	return &ast.BinaryExpression{Left: l, Operator: "==", Right: r}
}

func stmts(s ...ast.Statement) []ast.Statement { return s }

// callStmt 造一条 `f()` 表达式语句，当作分支体内容
func callStmt(name string) *ast.ExpressionStatement {
	return &ast.ExpressionStatement{
		Expression: &ast.CallExpression{Function: idn(name)},
	}
}

// makeChain 构造 if/else-if 链：
//
//	if (scrut == vals[0]) { bodies[0] }
//	else if (scrut == vals[1]) { bodies[1] }
//	...
//	else { elseBody }
//
// elseBody 为 nil 表示链尾没有 else。
func makeChain(scrut string, vals []uint64, bodies [][]ast.Statement, elseBody []ast.Statement) *ast.IfStatement {
	if len(vals) == 0 || len(bodies) != len(vals) {
		panic("makeChain: vals/bodies 数量必须一致且非空")
	}
	var build func(i int) *ast.IfStatement
	build = func(i int) *ast.IfStatement {
		var els []ast.Statement
		if i+1 < len(vals) {
			// 链的下一节：嵌套 if 作为 Else 的唯一元素
			els = []ast.Statement{build(i + 1)}
		} else {
			els = elseBody
		}
		return &ast.IfStatement{
			Condition: eqCond(idn(scrut), lit(vals[i])),
			Body:      bodies[i],
			Else:      els,
		}
	}
	return build(0)
}

// fnWith 把语句包进一个函数，返回 Program
func fnWith(body []ast.Statement) *ast.Program {
	return &ast.Program{
		Statements: []ast.Statement{
			&ast.FunctionStatement{Name: "f", Body: body},
		},
	}
}

// firstFn 取程序里第一个函数
func firstFn(t *testing.T, p *ast.Program) *ast.FunctionStatement {
	t.Helper()
	fn := p.FindFunction("f")
	if fn == nil {
		t.Fatalf("找不到函数 f")
	}
	return fn
}

func runPass(p *ast.Program) Result {
	return OptimizeProgram(p, DefaultOptions())
}

// ---------- 正向改写 ----------

func TestIfChainConvertedToSwitch(t *testing.T) {
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	p := fnWith(stmts(chain))

	res := runPass(p)
	if res.JumpTables != 1 {
		t.Fatalf("期望改写 1 处跳转表，实际 %d", res.JumpTables)
	}

	fn := firstFn(t, p)
	sw, ok := fn.Body[0].(*ast.SwitchStatement)
	if !ok {
		t.Fatalf("期望函数体首语句为 *ast.SwitchStatement，实际 %T", fn.Body[0])
	}
	if id, ok := sw.Scrutinee.(*ast.Identifier); !ok || id.Name != "x" {
		t.Fatalf("期望 switch 分派变量为 x，实际 %#v", sw.Scrutinee)
	}
	if len(sw.Cases) != 3 {
		t.Fatalf("期望 3 个 case，实际 %d", len(sw.Cases))
	}
	for i, want := range []uint64{1, 2, 3} {
		v, ok := sw.Cases[i].Values[0].(*ast.IntegerLiteral)
		if !ok || v.Value != want {
			t.Fatalf("case %d 期望常量 %d，实际 %#v", i, want, sw.Cases[i].Values[0])
		}
	}
	if !sw.HasDefault() {
		t.Fatalf("期望存在 default 分支")
	}
	if len(sw.Default) != 1 {
		t.Fatalf("default 分支应有 1 条语句，实际 %d", len(sw.Default))
	}
}

func TestIfChainWithoutElseHasNoDefault(t *testing.T) {
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		nil, // 无 else
	)
	p := fnWith(stmts(chain))
	runPass(p)

	fn := firstFn(t, p)
	sw, ok := fn.Body[0].(*ast.SwitchStatement)
	if !ok {
		t.Fatalf("期望改写为 switch，实际 %T", fn.Body[0])
	}
	if sw.HasDefault() {
		t.Fatalf("无 else 的链不应产出 default 分支")
	}
	if len(sw.Cases) != 3 {
		t.Fatalf("期望 3 个 case，实际 %d", len(sw.Cases))
	}
}

func TestNegativeConstantsConverted(t *testing.T) {
	neg := func(v uint64) ast.Expression {
		return &ast.UnaryExpression{Operator: "-", Right: lit(v)}
	}
	// if (x == -1) ... else if (x == -2) ... else if (x == -3) ... else ...
	var build func(i uint64) *ast.IfStatement
	build = func(i uint64) *ast.IfStatement {
		var els []ast.Statement
		if i < 3 {
			els = []ast.Statement{build(i + 1)}
		} else {
			els = stmts(callStmt("d"))
		}
		return &ast.IfStatement{
			Condition: eqCond(idn("x"), neg(i)),
			Body:      stmts(callStmt("a")),
			Else:      els,
		}
	}
	p := fnWith(stmts(build(1)))
	res := runPass(p)
	if res.JumpTables != 1 {
		t.Fatalf("负常量链应被改写，实际 %d", res.JumpTables)
	}
}

func TestConstantOnLeftIsConverted(t *testing.T) {
	// 1 == x 形式
	var build func(i uint64) *ast.IfStatement
	build = func(i uint64) *ast.IfStatement {
		var els []ast.Statement
		if i < 3 {
			els = []ast.Statement{build(i + 1)}
		} else {
			els = stmts(callStmt("d"))
		}
		return &ast.IfStatement{
			Condition: eqCond(lit(i), idn("x")), // 常量在左
			Body:      stmts(callStmt("a")),
			Else:      els,
		}
	}
	p := fnWith(stmts(build(1)))
	if runPass(p).JumpTables != 1 {
		t.Fatalf("常量在左的等值链也应被改写")
	}
}

func TestIfChainInsideWhileIsConverted(t *testing.T) {
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	loop := &ast.WhileStatement{
		Condition: &ast.BooleanLiteral{Value: true},
		Body:      stmts(chain),
	}
	p := fnWith(stmts(loop))
	if runPass(p).JumpTables != 1 {
		t.Fatalf("循环内的 if 链也应被改写")
	}
	fn := firstFn(t, p)
	w := fn.Body[0].(*ast.WhileStatement)
	if _, ok := w.Body[0].(*ast.SwitchStatement); !ok {
		t.Fatalf("循环体内应为 switch，实际 %T", w.Body[0])
	}
}

func TestNestedChainFoldedFromOutermost(t *testing.T) {
	// 5 段链：必须从最外层一次折叠成 5 个 case，
	// 而不是先折叠内层形成嵌套 switch
	chain := makeChain("x",
		[]uint64{1, 2, 3, 4, 5},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
			stmts(callStmt("d")),
			stmts(callStmt("e")),
		},
		stmts(callStmt("z")),
	)
	p := fnWith(stmts(chain))
	runPass(p)
	fn := firstFn(t, p)
	sw, ok := fn.Body[0].(*ast.SwitchStatement)
	if !ok {
		t.Fatalf("期望整体折叠为 switch，实际 %T", fn.Body[0])
	}
	if len(sw.Cases) != 5 {
		t.Fatalf("期望一次折叠出 5 个 case，实际 %d", len(sw.Cases))
	}
	// 分支体内不应残留被折叠成 switch 的子链
	for _, c := range sw.Cases {
		for _, s := range c.Body {
			if _, ok := s.(*ast.SwitchStatement); ok {
				t.Fatalf("分支体内出现了嵌套 switch，说明折叠顺序有误")
			}
		}
	}
}

// ---------- 安全守卫：必须放弃改写 ----------

func TestBreakInBranchBlocksConversion(t *testing.T) {
	// 分支体含 break：改成 switch 后 break 会穿透为"跳出 switch"，语义变了
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a"), &ast.BreakStatement{}),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	loop := &ast.WhileStatement{Condition: &ast.BooleanLiteral{Value: true}, Body: stmts(chain)}
	p := fnWith(stmts(loop))
	if runPass(p).JumpTables != 0 {
		t.Fatalf("分支体含 break 时必须放弃改写（否则语义被静默改变）")
	}
}

func TestContinueInBranchBlocksConversion(t *testing.T) {
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(&ast.ContinueStatement{}),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	loop := &ast.WhileStatement{Condition: &ast.BooleanLiteral{Value: true}, Body: stmts(chain)}
	p := fnWith(stmts(loop))
	if runPass(p).JumpTables != 0 {
		t.Fatalf("分支体含 continue 时必须放弃改写")
	}
}

func TestBreakNestedInInnerLoopAllowsConversion(t *testing.T) {
	// break 位于分支体内的内层 while 中 → 绑定到内层循环，改写是安全的
	inner := &ast.WhileStatement{
		Condition: &ast.BooleanLiteral{Value: true},
		Body:      stmts(&ast.BreakStatement{}),
	}
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(inner),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	p := fnWith(stmts(chain))
	if runPass(p).JumpTables != 1 {
		t.Fatalf("内层循环里的 break 应被判定为安全，仍应改写")
	}
}

func TestDifferentScrutineeBlocksConversion(t *testing.T) {
	// 第二段换成 y：不是单一分派点
	inner := &ast.IfStatement{
		Condition: eqCond(idn("y"), lit(2)),
		Body:      stmts(callStmt("b")),
		Else: []ast.Statement{
			&ast.IfStatement{
				Condition: eqCond(idn("x"), lit(3)),
				Body:      stmts(callStmt("c")),
				Else:      stmts(callStmt("d")),
			},
		},
	}
	chain := &ast.IfStatement{
		Condition: eqCond(idn("x"), lit(1)),
		Body:      stmts(callStmt("a")),
		Else:      []ast.Statement{inner},
	}
	p := fnWith(stmts(chain))
	if runPass(p).JumpTables != 0 {
		t.Fatalf("比较变量不一致时必须放弃改写")
	}
}

func TestNonEqualityConditionBlocksConversion(t *testing.T) {
	// 第二段是 x < 2：不是等值比较，不能进跳转表
	chain := &ast.IfStatement{
		Condition: eqCond(idn("x"), lit(1)),
		Body:      stmts(callStmt("a")),
		Else: []ast.Statement{
			&ast.IfStatement{
				Condition: &ast.BinaryExpression{Left: idn("x"), Operator: "<", Right: lit(2)},
				Body:      stmts(callStmt("b")),
				Else: []ast.Statement{
					&ast.IfStatement{
						Condition: eqCond(idn("x"), lit(3)),
						Body:      stmts(callStmt("c")),
						Else:      stmts(callStmt("d")),
					},
				},
			},
		},
	}
	p := fnWith(stmts(chain))
	if runPass(p).JumpTables != 0 {
		t.Fatalf("含非等值条件时必须放弃改写")
	}
}

func TestFloatConstantBlocksConversion(t *testing.T) {
	// C 的 case 标签不允许浮点，必须放弃
	var build func(i int) *ast.IfStatement
	build = func(i int) *ast.IfStatement {
		var els []ast.Statement
		if i < 3 {
			els = []ast.Statement{build(i + 1)}
		} else {
			els = stmts(callStmt("d"))
		}
		return &ast.IfStatement{
			Condition: eqCond(idn("x"), &ast.FloatLiteral{Value: float64(i)}),
			Body:      stmts(callStmt("a")),
			Else:      els,
		}
	}
	p := fnWith(stmts(build(1)))
	if runPass(p).JumpTables != 0 {
		t.Fatalf("浮点常量链必须放弃改写（C case 标签不接受浮点）")
	}
}

func TestShortChainNotConverted(t *testing.T) {
	// 只有 2 段等值比较，未达阈值（默认 3）
	chain := makeChain("x",
		[]uint64{1, 2},
		[][]ast.Statement{stmts(callStmt("a")), stmts(callStmt("b"))},
		stmts(callStmt("c")),
	)
	p := fnWith(stmts(chain))
	if runPass(p).JumpTables != 0 {
		t.Fatalf("短链不应改写（收益不足以抵销可读性损失）")
	}
}

func TestDuplicateConstantsDeduped(t *testing.T) {
	// 重复常量：C 不允许重复 case 标签，须去重后仍满足阈值
	chain := makeChain("x",
		[]uint64{1, 1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(callStmt("dup")),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	p := fnWith(stmts(chain))
	if runPass(p).JumpTables != 1 {
		t.Fatalf("重复常量链应去重后改写")
	}
	fn := firstFn(t, p)
	sw := fn.Body[0].(*ast.SwitchStatement)
	if len(sw.Cases) != 3 {
		t.Fatalf("期望去重后 3 个 case，实际 %d", len(sw.Cases))
	}
	// 保留的是首个命中分支（后续重复分支在原链里本就不可达）
	if es, ok := sw.Cases[0].Body[0].(*ast.ExpressionStatement); !ok {
		t.Fatalf("case 1 分支体类型异常")
	} else if c, ok := es.Expression.(*ast.CallExpression); !ok ||
		c.Function.(*ast.Identifier).Name != "a" {
		t.Fatalf("去重应保留首个分支，实际保留的不是 a")
	}
}

func TestConcurrentFunctionSkipped(t *testing.T) {
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	p := &ast.Program{
		Statements: []ast.Statement{
			&ast.FunctionStatement{Name: "f", IsConcurrent: true, Body: stmts(chain)},
		},
	}
	if runPass(p).JumpTables != 0 {
		t.Fatalf("并发函数应整体跳过（守卫状态机不受前端改写影响）")
	}
}

func TestMethodBodiesAreOptimized(t *testing.T) {
	chain := makeChain("x",
		[]uint64{1, 2, 3},
		[][]ast.Statement{
			stmts(callStmt("a")),
			stmts(callStmt("b")),
			stmts(callStmt("c")),
		},
		stmts(callStmt("d")),
	)
	p := &ast.Program{
		Statements: []ast.Statement{
			&ast.ClassStatement{
				Name:    "C",
				Methods: []*ast.MethodStatement{{Name: "m", Body: stmts(chain)}},
			},
		},
	}
	if runPass(p).JumpTables != 1 {
		t.Fatalf("类方法体内的 if 链也应被改写")
	}
}

func TestDisabledByEnv(t *testing.T) {
	t.Setenv("KAULA_OPT_DISABLE", "1")
	if Enabled("-O2") {
		t.Fatalf("KAULA_OPT_DISABLE 应关闭前端优化")
	}
	if Enabled("-O0") {
		t.Fatalf("-O0 应关闭前端优化")
	}
	if !Enabled("-O2") && Enabled("-O2") {
		t.Fatalf("状态不一致")
	}
}

func TestEnabledLevels(t *testing.T) {
	t.Setenv("KAULA_OPT_DISABLE", "")
	if !Enabled("-O2") || !Enabled("-O3") || !Enabled("-O1") {
		t.Fatalf("O1/O2/O3 应启用前端优化")
	}
	if Enabled("-O0") {
		t.Fatalf("-O0 不应启用前端优化")
	}
}
