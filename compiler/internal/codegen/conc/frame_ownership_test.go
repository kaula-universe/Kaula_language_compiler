package conc

import (
	"regexp"
	"strings"
	"testing"

	"kaula/internal/ast"
)

// Frame 内存归属契约（docs/core-runtime.md「内存归属」一节）：
//
// 跨挂起点存活的变量**按值内嵌**进 Frame 字段，不接入 KMM V4 堆分配。
// Frame 随任务 retire，内嵌值随 Frame 一起释放，回收时机由 epoch 判定统一裁决。
//
// 这组测试守住该契约：若将来有人把 Frame 字段改成 T* + KMM 分配（即模型 A → B），
// 这里会失败，提示必须先解决 epoch pin 与 retire 的时序问题。

// Frame 字段必须是内嵌实值，不能是指针
func TestFrameContract_VarsEmbeddedNotPointers(t *testing.T) {
	e := newEmitter("n")
	e.FrameVarCTypes = map[string]string{"p": "K_Point"}
	analysis := &BodyAnalysis{
		NeedsSuspend: true,
		FrameVars:    []VarInfo{{Name: "p", KType: "Point", DeclLine: 2}},
	}
	res := e.generateStatefulFrame(analysis)

	// 内嵌形式：类型在前、变量名在后，且不是 K_Point*
	if !strings.Contains(res, "K_Point") {
		t.Errorf("Frame 应含 K_Point 字段，实际:\n%s", res)
	}
	// 用正则而非字面量：字段是 `%-14s` 对齐输出的，指针星号与类型之间
	// 可能有任意多个空格（`K_Point        *p;`），字面量匹配会漏掉。
	ptrRe := regexp.MustCompile(`K_Point\s+\*`)
	if ptrRe.MatchString(res) {
		t.Errorf("Frame 字段不应是指针（内嵌模型），实际:\n%s", res)
	}
	// 正例：应出现内嵌形态 `K_Point<空白>p;`
	if !regexp.MustCompile(`K_Point\s+p;`).MatchString(res) {
		t.Errorf("Frame 应按值内嵌 K_Point p，实际:\n%s", res)
	}
}

// Frame 里不应出现任何 KMM 分配调用 —— 内嵌模型下没有逐字段 free
func TestFrameContract_NoKMMAllocationInFrame(t *testing.T) {
	e := newEmitter("n")
	e.FrameVarCTypes = map[string]string{"p": "K_Point"}
	analysis := &BodyAnalysis{
		NeedsSuspend: true,
		FrameVars:    []VarInfo{{Name: "p", KType: "Point", DeclLine: 2}},
	}
	res := e.generateStatefulFrame(analysis)

	for _, forbidden := range []string{
		"kmm_v4_alloc", "kmm_v4_alloc_auto", "kmm_v4_alloc_global",
		"kmm_v4_scope_push", "survivor_free", "kmm_v4_survivor_rewind",
		"free(",
	} {
		if strings.Contains(res, forbidden) {
			t.Errorf("Frame 定义不应含 %q（内嵌模型下不参与堆分配），实际:\n%s", forbidden, res)
		}
	}
}

// 生成的 C 代码里，跨挂起点变量的恢复应是按值拷贝，不是取指针解引用
func TestFrameContract_ResumeRestoresByValue(t *testing.T) {
	// 用真实的 AnalyzeBody 产出 liveness 信息：RestoreOrdinal/LiveAt 依赖
	// 挂起点的遍历序号，手工拼 VarInfo 会让变量被判成「非存活」而测不到东西。
	body := []ast.Statement{
		&ast.VariableDeclaration{Name: "p", Type: "Point", Pos: ast.Position{Line: 2}},
		&ast.ExpressionStatement{
			Expression: &ast.CallExpression{
				Function: &ast.Identifier{Name: "time_sleep"},
				Args:     []ast.Expression{&ast.IntegerLiteral{Value: 1}},
				Pos:      ast.Position{Line: 3},
			},
			Pos: ast.Position{Line: 3},
		},
		&ast.ReturnStatement{
			Value: &ast.Identifier{Name: "p"},
			Pos:   ast.Position{Line: 4},
		},
	}
	analysis := AnalyzeBody(body)
	if !analysis.NeedsSuspend {
		t.Fatal("前提不成立：time_sleep 应构成挂起点")
	}

	e := newEmitter("n")
	e.FrameVarCTypes = map[string]string{"p": "K_Point"}
	res := e.Generate(analysis, []string{"", ""}, "", body)

	// 恢复形态：`<ctype> <name> = f-><name>;` —— 按值
	if !strings.Contains(res, "K_Point p = f->p;") {
		t.Errorf("应按值把 Frame 字段恢复到同名局部变量，实际:\n%s", res)
	}
	// 不应是 `*(&f->p)` 这类指针化写法（对齐空格可能夹在中间，用正则）
	if regexp.MustCompile(`K_Point\s+\*\s*`).MatchString(res) {
		t.Errorf("恢复不应把变量指针化，实际:\n%s", res)
	}
}

// needs_free 在内嵌模型下恒为 0 —— 它是模型切换的预留位，不是活跃路径
func TestFrameContract_NeedsFreeStaysZero(t *testing.T) {
	e := newEmitter("n")
	e.FrameVarCTypes = map[string]string{"p": "K_Point"}
	analysis := &BodyAnalysis{
		NeedsSuspend: true,
		FrameVars:    []VarInfo{{Name: "p", KType: "Point", DeclLine: 2}},
	}
	res := e.generateStatefulFrame(analysis)

	if !strings.Contains(res, "int             needs_free;") {
		t.Errorf("Frame 应保留 needs_free 字段（模型切换预留位），实际:\n%s", res)
	}
	// 结构体字段声明处不应被初始化为非 0
	if strings.Contains(res, "needs_free = 1") {
		t.Errorf("内嵌模型下 needs_free 不应被置 1，实际:\n%s", res)
	}
}