package sema

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/errors"
)

// conc fn 恒定开启 SOR（不依赖 --sor，也不依赖 #[sor] 注解）。
//
// 动机：Frame 让局部变量跨 suspend 点存活，这属于所有权系统的管辖范围。
// 不开的话 conc fn 的跨挂起点存活变量没有任何校验。

// mkYieldPrimConcFn 造一个体内含 yield 的 conc fn
func mkYieldPrimConcFn(name string) *ast.FunctionStatement {
	fs := mkConcFn(name, "x")
	src := &ast.Identifier{Name: "x", Pos: ast.Position{Line: 2}}
	fs.Body = []ast.Statement{
		&ast.YieldStatement{
			Source: src,
			Target: "y",
			Pos:    ast.Position{Line: 2, Column: 1},
		},
		&ast.ReturnStatement{
			Value: &ast.Identifier{Name: "x", Pos: ast.Position{Line: 3}},
			Pos:   ast.Position{Line: 3},
		},
	}
	return fs
}

// mkYieldPrimPlainFn 造一个普通 fn，体内含 yield
func mkYieldPrimPlainFn(name string) *ast.FunctionStatement {
	fs := mkPlainFn(name)
	src := &ast.Identifier{Name: "z", Pos: ast.Position{Line: 2}}
	fs.Body = []ast.Statement{
		&ast.YieldStatement{
			Source: src,
			Target: "y",
			Pos:    ast.Position{Line: 2, Column: 1},
		},
		&ast.ReturnStatement{
			Value: &ast.IntegerLiteral{Value: 1, Pos: ast.Position{Line: 3}},
			Pos:   ast.Position{Line: 3},
		},
	}
	return fs
}

// 未开 --sor 时，conc fn 内的 yield 应被接受（不再报"未定义原语"）
func TestConcFn_SORPrimitivesAllowedWithoutFlag(t *testing.T) {
	sa := newSemaWithFns(mkYieldPrimConcFn("worker"))
	sa.currentFunction = sa.program.Statements[0].(*ast.FunctionStatement)

	sa.analyzeYieldStatement(&ast.YieldStatement{
		Source: &ast.Identifier{Name: "x", Pos: ast.Position{Line: 2}},
		Target: "y",
		Pos:    ast.Position{Line: 2, Column: 1},
	})

	for _, e := range sa.errorCollector.GetErrorsByType(errors.ErrorSemantic) {
		if containsStr(e.Message, "SOR 扩展的原语") {
			t.Errorf("conc fn 内的 yield 不应报「需要 --sor / #[sor]」: %s", e.Message)
		}
	}
}

// 普通 fn 内的 yield 仍应报原语未定义（不能被 conc fn 的改动牵连放开）
func TestPlainFn_SORPrimitivesStillRejected(t *testing.T) {
	sa := newSemaWithFns(mkYieldPrimPlainFn("plain"))
	sa.currentFunction = sa.program.Statements[0].(*ast.FunctionStatement)

	sa.analyzeYieldStatement(&ast.YieldStatement{
		Source: &ast.Identifier{Name: "z", Pos: ast.Position{Line: 2}},
		Target: "y",
		Pos:    ast.Position{Line: 2, Column: 1},
	})

	found := false
	for _, e := range sa.errorCollector.GetErrorsByType(errors.ErrorSemantic) {
		if containsStr(e.Message, "SOR 扩展的原语") {
			found = true
			break
		}
	}
	if !found {
		t.Error("普通 fn 内的 yield 应继续报「需要 --sor 或 #[sor] 注解」")
	}
}

// isFunctionSOR 的判定优先级：全局标志 / conc fn / #[sor] 注解
func TestIsFunctionSOR_Precedence(t *testing.T) {
	cases := []struct {
		name       string
		global     bool
		fnSOR      bool
		isConc     bool
		wantResult bool
	}{
		{"全关", false, false, false, false},
		{"仅全局", true, false, false, true},
		{"仅注解", false, true, false, true},
		{"仅 conc fn", false, false, true, true},
		{"全局+conc", true, false, true, true},
	}
	for _, c := range cases {
		sa := NewSemanticAnalyzer()
		sa.sorGlobalEnabled = c.global
		if c.fnSOR || c.isConc {
			sa.currentFunction = &ast.FunctionStatement{
				Name:         "f",
				SOREnabled:   c.fnSOR,
				IsConcurrent: c.isConc,
			}
		}
		if got := sa.isFunctionSOR(); got != c.wantResult {
			t.Errorf("%s: isFunctionSOR() = %v, 期望 %v", c.name, got, c.wantResult)
		}
	}
}

// conc fn 不应因为没写 #[sor] 而触发「标注了 #[sor] 却没用原语」的误报警告
func TestConcFn_NoSpuriousSorAnnotationWarning(t *testing.T) {
	sa := newSemaWithFns(mkConcFn("worker", "x"))
	fn := sa.program.Statements[0].(*ast.FunctionStatement)
	if fn.SOREnabled {
		t.Fatal("前提不成立：conc fn 不应带 #[sor] 注解标记")
	}

	analyzed := 0
	for _, stmt := range fn.Body {
		if !sa.hasSORPrimitives([]ast.Statement{stmt}) {
			continue
		}
		analyzed++
	}
	// 断言 isFunctionSOR 已对 conc fn 生效：这才是「不误报警告」的根据
	sa.currentFunction = fn
	if !sa.isFunctionSOR() {
		t.Error("conc fn 的 isFunctionSOR 应为 true")
	}
}

func containsStr(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}