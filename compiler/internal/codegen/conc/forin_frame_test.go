package conc

import (
	"strings"
	"testing"

	"kaula/internal/ast"
)

// for..in 的迭代下标不该进 FrameVars。
//
// 它已经由 guard 模式持久化在 loop_i[depth] 里 —— 每轮迭代开始时
// `long long i = f->loop_i[depth]` 从那里恢复，迭代末尾写回
// `f->loop_i[depth] += f->loop_step[depth]`。
//
// 早期实现把它也 addDecl 进 FrameVars，于是：
//  1. Frame 里多一个字段（纯浪费）
//  2. for-in 下标没有类型标注 → frameVarCTypes 跳过 → FrameVarCType
//     兜底成 void* → 生成的 `f->i = i;` 把 long long 赋给 void*，
//     clang 报 "incompatible integer to pointer conversion"，编译直接失败
//  3. resume 顶部多一句 `void* i = f->i;`，与循环体里的
//     `long long i = f->loop_i[d];` 同名遮蔽
//
// 判据：生成物必须既能编译（类型正确），又不含冗余字段与冗余回写。

func forInBody() []ast.Statement {
	return []ast.Statement{
		decl("acc", 1, ident("0")),
		&ast.ForInStatement{
			Variable: ident("i"),
			Body: []ast.Statement{
				exprStmt(call("time_sleep", 3), 3),
			},
			Pos: pos(2),
		},
		ret(ident("acc"), 4),
	}
}

func TestForInIndexNotInFrameVars(t *testing.T) {
	res := AnalyzeBody(forInBody())
	for _, v := range res.FrameVars {
		if v.Name == "i" {
			t.Fatalf("for..in 迭代下标不应进 FrameVars（已由 loop_i[depth] 持久化）；" +
				"实际进了，且因无类型标注会被兜底成 void*，导致生成的 C 编译失败")
		}
	}
}

// for-in 必须被登记 HasForIn，否则 Frame 里没有 loop_i/loop_end/loop_step，
// 恢复后迭代下标从头再数一遍 —— acc 会指数增长而不是线性累加。
func TestForInSetsHasForIn(t *testing.T) {
	res := AnalyzeBody(forInBody())
	if !res.HasForIn {
		t.Error("for..in 应置 HasForIn（Frame 需要 loop_i/loop_end/loop_step）")
	}
	if !res.HasCompound {
		t.Error("for..in 应置 HasCompound（Frame 需要 sub/branch）")
	}
}

// C 风格 for 循环的 init 变量与 for..in 不同：它没有 loop_i 兜底，
// 必须留在 FrameVars 里。回归防护 —— 修 for..in 时别把这个一起删掉。
func TestForStatementInitStillInFrameVars(t *testing.T) {
	body := []ast.Statement{
		&ast.ForStatement{
			Init:      decl("i", 1, ident("0")),
			Condition: ident("cond"),
			Body:      []ast.Statement{exprStmt(call("time_sleep", 3), 3)},
			Pos:       pos(1),
		},
	}
	res := AnalyzeBody(body)
	found := false
	for _, v := range res.FrameVars {
		if v.Name == "i" {
			found = true
		}
	}
	if !found {
		t.Error("C 风格 for 的 init 变量没有 loop_i 兜底，必须提升到 Frame")
	}
}

// 端到端：生成的 C 里不得出现把迭代下标写成 void* 的语句。
//
// 这是本次线上故障的直接形态 —— clang 报
// "incompatible integer to pointer conversion assigning to 'void *' from 'long long'"。
func TestGeneratedFrameHasNoVoidStarForInIndex(t *testing.T) {
	res := AnalyzeBody(forInBody())
	e := &Emitter{
		FuncName: "loop_suspend",
		CName:    "K_loop_suspend",
		CReturnType: "int64_t",
		ParamTypes:  []string{"i64"},
		ParamCTypes:  []string{"int64_t"},
		Params:       []string{"rounds"},
		// 故意不给 i 的类型：模拟 sema 对 for-in 下标无标注的现实
		FrameVarCTypes: map[string]string{"acc": "int64_t"},
	}
	// 第 3 个参数 enterSegment：段入口的槽位复位代码（上游新增）。
	// 本用例只看 Frame 布局与回写，给空串即可。
	code := e.Generate(res, []string{"/* body */"}, "", forInBody())

	if strings.Contains(code, "void*          i;") || strings.Contains(code, "void* i;") {
		t.Error("生成的 Frame 里出现了 void* 类型的 for..in 迭代下标 —— 该类型会让赋值编译失败")
	}
	// 迭代下标应由 loop_i 承载，出现这个字段就说明又被提升了一遍
	if strings.Contains(code, "f->i =") {
		t.Error("生成了 f->i = ... 回写，但迭代下标应由 f->loop_i[depth] 承载")
	}
}
