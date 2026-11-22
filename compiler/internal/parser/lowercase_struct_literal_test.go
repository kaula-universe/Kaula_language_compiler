package parser_test

import (
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// parseProgram 解析 src；有解析错误则 fail 并打印。
func parseProgramForTest(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.NewParser(lexer.NewLexer(src))
	p.SetSkipMainCheck(true)
	prog := p.Parse()
	if p.HasErrors() {
		ec := p.GetErrorCollector()
		var msgs []string
		for _, e := range ec.Errors() {
			msgs = append(msgs, e.Message)
		}
		t.Fatalf("解析失败: %s", strings.Join(msgs, "\n"))
	}
	return prog
}

// findStructLiteralInFns 在所有函数体里找第一个 StructLiteral。
func findStructLiteralInFns(prog *ast.Program) *ast.StructLiteral {
	for _, s := range prog.Statements {
		fn, ok := s.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		for _, b := range fn.Body {
			if vd, ok := b.(*ast.VariableDeclaration); ok {
				if lit, ok := vd.Value.(*ast.StructLiteral); ok {
					return lit
				}
			}
		}
	}
	return nil
}

// TestLowercaseStructLiteralMultiLine 回归：小写开头类型名的多行结构体
// 字面量此前被解析成裸 Identifier + 语句块，报出一串 unexpected COLON
// 级联（struct 声明本身合法，报错与用户写的类型毫无关系）。
// 修复：小写名只认无歧义形态（{ } / { .x = ... } / { x: ... }），命中即
// 走结构体字面量分支；大写名保持原判定（含 speculative field= 形态）。
func TestLowercaseStructLiteralMultiLine(t *testing.T) {
	src := `struct m000_rec {
    uid: i64,
    label: string
}

fn make(i64 uid) m000_rec {
    m000_rec r = m000_rec {
        uid: uid,
        label: "x"
    }
    return r
}
`
	prog := parseProgramForTest(t, src)
	lit := findStructLiteralInFns(prog)
	if lit == nil {
		t.Fatal("小写类型名的结构体字面量未解析为 StructLiteral")
	}
	if lit.TypeName != "m000_rec" {
		t.Fatalf("字面量类型名 = %q", lit.TypeName)
	}
	if len(lit.Fields) != 2 {
		t.Fatalf("期望 2 个字段，实际 %d", len(lit.Fields))
	}
}

// TestLowercaseStructLiteralColonField 单行冒号字段形态同样支持。
func TestLowercaseStructLiteralColonField(t *testing.T) {
	src := `struct cfg {
    retries: i64
}

fn make() cfg {
    cfg c = cfg { retries: 3 }
    return c
}
`
	prog := parseProgramForTest(t, src)
	lit := findStructLiteralInFns(prog)
	if lit == nil {
		t.Fatal("单行小写结构体字面量未解析为 StructLiteral")
	}
	if len(lit.Fields) != 1 {
		t.Fatalf("期望 1 个字段，实际 %d", len(lit.Fields))
	}
}

// TestLowercaseBlockAfterIdentStillBlock 保护：控制流块归属不变。
// `if x > limit {` 的 { 属于 if 块（blockDepth 防线），语句序列不被
// 结构体字面量判定吃掉。
func TestLowercaseBlockAfterIdentStillBlock(t *testing.T) {
	src := `fn f(i64 x) i64 {
    i64 limit = 3
    if x > limit {
        return 1
    }
    return 0
}
`
	prog := parseProgramForTest(t, src)
	for _, s := range prog.Statements {
		fn, ok := s.(*ast.FunctionStatement)
		if !ok || fn.Name != "f" {
			continue
		}
		if len(fn.Body) != 3 {
			t.Fatalf("期望 3 条语句（声明/ if / return），实际 %d", len(fn.Body))
		}
		if _, ok := fn.Body[1].(*ast.IfStatement); !ok {
			t.Fatalf("第 2 条语句应是 if，实际 %T", fn.Body[1])
		}
	}
}

// TestCapitalizedStructLiteralStillWorks 保护：大写名路径（含 speculative
// field= 形态）不受小写修复影响。
func TestCapitalizedStructLiteralStillWorks(t *testing.T) {
	src := `struct Point {
    x: i64,
    y: i64
}

fn make() Point {
    Point p = Point { x = 10, y = 20 }
    return p
}
`
	prog := parseProgramForTest(t, src)
	lit := findStructLiteralInFns(prog)
	if lit == nil {
		t.Fatal("大写结构体字面量未解析为 StructLiteral")
	}
	if len(lit.Fields) != 2 {
		t.Fatalf("期望 2 个字段，实际 %d", len(lit.Fields))
	}
}
