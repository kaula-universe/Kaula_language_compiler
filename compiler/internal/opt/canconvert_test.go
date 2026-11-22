package opt

import (
	"testing"

	"kaula/internal/ast"
)

// TestCanConvertIfChainMatchesRealRewrite CanConvertIfChain 是给前端诊断复用
// 的"只判定不改写"入口：它必须与真实改写（OptimizeProgram）的判定完全一致，
// 否则会出现"抑制了告警却没优化"或"优化了却还在告警"的错位。
func TestCanConvertIfChainMatchesRealRewrite(t *testing.T) {
	cases := []struct {
		name    string
		chain   *ast.IfStatement
		wantCan bool
	}{
		{
			name:    "3 个整型常量 + else：可被折叠",
			chain:   makeChain("x", []uint64{1, 2, 3}, [][]ast.Statement{{callStmt("a")}, {callStmt("b")}, {callStmt("c")}}, stmts(callStmt("d"))),
			wantCan: true,
		},
		{
			name:    "只有 2 个分支：达不到阈值",
			chain:   makeChain("x", []uint64{1, 2}, [][]ast.Statement{{callStmt("a")}, {callStmt("b")}}, stmts(callStmt("d"))),
			wantCan: false,
		},
		{
			name: "比较对象不是常量（变量）",
			chain: &ast.IfStatement{
				Condition: eqCond(idn("x"), idn("y")),
				Body:      stmts(callStmt("a")),
				Else: stmts(&ast.IfStatement{
					Condition: eqCond(idn("x"), idn("y")),
					Body:      stmts(callStmt("b")),
					Else: stmts(&ast.IfStatement{
						Condition: eqCond(idn("x"), idn("y")),
						Body:      stmts(callStmt("c")),
					}),
				}),
			},
			wantCan: false,
		},
		{
			name:  "分支体含绑定到外层循环的 break：语义陷阱，放弃改写",
			chain: makeChain("x", []uint64{1, 2, 3}, [][]ast.Statement{stmts(&ast.BreakStatement{}), {callStmt("b")}, {callStmt("c")}}, stmts(callStmt("d"))),
			// 注意：makeChain 的分支体直接含 break，hasUnboundLoopControl 会命中
			wantCan: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 判定入口
			gotCan := CanConvertIfChain(tc.chain, DefaultOptions())
			// 真实改写：把链放进函数跑一遍 pass，看是否被替换成 SwitchStatement
			p := fnWith(stmts(tc.chain))
			OptimizeProgram(p, DefaultOptions())
			_, isSwitch := firstFn(t, p).Body[0].(*ast.SwitchStatement)

			if gotCan != tc.wantCan {
				t.Errorf("CanConvertIfChain = %v, 期望 %v", gotCan, tc.wantCan)
			}
			if isSwitch != tc.wantCan {
				t.Errorf("真实改写结果 = %v, 期望 %v", isSwitch, tc.wantCan)
			}
			if gotCan != isSwitch {
				t.Errorf("判定与真实改写不一致：判定=%v 改写=%v（会导致告警与优化错位）", gotCan, isSwitch)
			}
		})
	}
}

// TestCanConvertIfChainDisabled 优化关闭时判定必须为 false——此时链不会被
// 改写，"建议改用 match" 告警也就仍然成立，不能抑制。
func TestCanConvertIfChainDisabled(t *testing.T) {
	chain := makeChain("x", []uint64{1, 2, 3},
		[][]ast.Statement{{callStmt("a")}, {callStmt("b")}, {callStmt("c")}},
		stmts(callStmt("d")))

	if CanConvertIfChain(chain, Options{EnableJumpTable: false, MinJumpCases: 3}) {
		t.Errorf("EnableJumpTable=false 时不应判定为可转换")
	}
	if CanConvertIfChain(nil, DefaultOptions()) {
		t.Errorf("nil 链不应判定为可转换")
	}
}
