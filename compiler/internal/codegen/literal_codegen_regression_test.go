package codegen

import (
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/config"
	"kaula/internal/sema"
)

/*
 * 下列测试守护四个「静默错误」缺陷的修复。
 *
 * 之所以叫静默：它们全部**编译通过、零警告**，只是安静地给出错答案，
 * 或让 clang 报一句和根因无关的错误。这类缺陷比编译失败危险得多。
 *
 * 共同根因都在 AST/parser 层——声明的字面量形状没被识别，值被丢失：
 *   1. `[]i32 arr = [1,3,1990]` —— 复合字面量硬编码 int64_t，步长错位
 *   2. `i32 arr[5] = {10,...}`  —— `{}` 被当成 struct literal，裸值丢弃
 *   3. `Point p = Point{ x = 10 }` —— 无点字段赋值被丢弃 → 全零对象
 *   4. `#[derive(Debug)]`        —— 依赖 3，字段读不到值
 */

// TestArrayLiteralElementTypeMatchesDeclaration 守护缺陷 1。
// `[]i32` 的复合字面量必须是 int32_t[]，不能是 int64_t[]。
func TestArrayLiteralElementTypeMatchesDeclaration(t *testing.T) {
	cCases := []struct {
		decl    string
		wantLit string
	}{
		{"[]i32 arr = [1, 3, 1990]", "((int32_t[]){ 1, 3, 1990 })"},
		{"[]i64 arr = [1, 3, 1990]", "((int64_t[]){ 1, 3, 1990 })"},
		{"[]u8 arr = [65, 66]", "((uint8_t[]){ 65, 66 })"},
	}
	for _, tc := range cCases {
		src := "fn main() {\n    " + tc.decl + "\n}\n"
		cCode := generateForTest(t, src)
		if !strings.Contains(cCode, tc.wantLit) {
			t.Errorf("%q: 期望字面量 %q，实际生成:\n%s", tc.decl, tc.wantLit, cCode)
		}
	}
}

// TestBraceArrayLiteralParsedAsArray 守护缺陷 2 的 parser 侧。
// 定长数组初始化器 `{10, 20}` 必须被解析成 ArrayLiteral，
// 而不是 StructLiteral（后者会丢弃裸值）。
func TestBraceArrayLiteralParsedAsArray(t *testing.T) {
	src := "fn main() {\n    i32 arr[3] = {10, 20, 30}\n}\n"
	prog := parse(src)
	var found bool
	for _, st := range prog.Statements {
		fn, ok := st.(*ast.FunctionStatement)
		if !ok || fn.Name != "main" {
			continue
		}
		for _, bst := range fn.Body {
			v, ok := bst.(*ast.VariableDeclaration)
			if !ok || v.Name != "arr" {
				continue
			}
			if _, isArr := v.Value.(*ast.ArrayLiteral); !isArr {
				t.Fatalf("`{10,20,30}` 应解析为 *ast.ArrayLiteral，实际 %T", v.Value)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("未找到 arr 声明")
	}

	// 端到端：初始化值必须真的写进 C 的 brace 初始化器
	cCode := generateForTest(t, src)
	if !strings.Contains(cCode, "= {10, 20, 30}") {
		t.Errorf("定长数组初始化值丢失，生成:\n%s", cCode)
	}
}

// TestStructLiteralDotlessFieldAssign 守护缺陷 3。
// 文档写法 `Point { x = 10, y = 20 }`（无点）必须生成字段指派。
func TestStructLiteralDotlessFieldAssign(t *testing.T) {
	src := "struct Point { x: i64, y: i64 }\n" +
		"fn main() {\n    Point p = Point { x = 10, y = 20 }\n}\n"
	cCode := generateForTest(t, src)
	if !strings.Contains(cCode, ".x=10") || !strings.Contains(cCode, ".y=20") {
		t.Errorf("无点字段赋值丢失（应为 .x=10,.y=20），生成:\n%s", cCode)
	}
}

// TestSliceIterationRejectedClearly 守护缺陷 2 的另一半。
// `[]T` 迭代必须给出清晰的编译期错误，而不是生成对裸指针取 .len 的坏 C。
func TestSliceIterationRejectedClearly(t *testing.T) {
	src := "fn main() {\n" +
		"    []i64 arr = [1, 3, 1990]\n" +
		"    i64 s = 0\n" +
		"    for v in arr { s = s + v }\n" +
		"}\n"
	prog := parse(src)
	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cg := NewCodeGenerator(config.DefaultConfig())
	cg.Generate(prog)
	if !cg.HasErrors() {
		t.Fatalf("切片迭代应产生编译期错误，但没有报错")
	}
	joined := strings.Join(cg.Errors(), "\n")
	if !strings.Contains(joined, "切片") {
		t.Errorf("错误信息应说明切片无法迭代，实际:\n%s", joined)
	}
}
