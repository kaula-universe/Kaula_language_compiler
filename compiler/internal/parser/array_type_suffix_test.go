package parser_test

import (
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// parseOne 解析 src 并返回解析器与程序；不检查错误，由调用方断言。
func parseOne(t *testing.T, src string) *parser.Parser {
	t.Helper()
	return parser.NewParser(lexer.NewLexer(src))
}

// parseErrs 返回所有解析错误消息（换行拼接）。
func parseErrs(t *testing.T, src string) string {
	t.Helper()
	p := parseOne(t, src)
	p.Parse()
	ec := p.GetErrorCollector()
	if ec == nil {
		return ""
	}
	var msgs []string
	for _, e := range ec.Errors() {
		msgs = append(msgs, e.Message)
	}
	return strings.Join(msgs, "\n")
}

// requireErr 断言 src 至少产生一条解析错误，且其中包含 want 子串。
func requireErr(t *testing.T, name, src, want string) {
	t.Helper()
	got := parseErrs(t, src)
	if got == "" {
		t.Fatalf("%s：期望报错 %q，实际 0 个解析错误", name, want)
	}
	if !strings.Contains(got, want) {
		t.Errorf("%s：错误消息 =\n%s\n期望包含 %q", name, got, want)
	}
}

// requireNoErr 断言 src 解析无错误。
func requireNoErr(t *testing.T, name, src string) {
	t.Helper()
	if got := parseErrs(t, src); got != "" {
		t.Errorf("%s：期望 0 个解析错误，实际：\n%s", name, got)
	}
}

const pointStruct = "struct Point {\n    x: f64\n    y: f64\n}\n"

// --- Type[N] 后缀数组类型必须报错（这是本次修复的核心）---

// TestArrayTypeSuffixInStructField 结构体字段写成 pts: Point[4] 必须报错，
// 修复前 [N] 被静默丢弃、直接生成非法 C。
func TestArrayTypeSuffixInStructField(t *testing.T) {
	requireErr(t, "字段后缀数组", pointStruct+"struct Box {\n    pts: Point[4]\n}\n",
		"write the length before the element type: '[4]Point'")
}

// TestArrayTypeSuffixInTypeAlias 类型别名 type Path = Point[4] 必须报错。
func TestArrayTypeSuffixInTypeAlias(t *testing.T) {
	requireErr(t, "别名后缀数组", pointStruct+"type Path = Point[4]\n",
		"'Point[4]' is not valid")
}

// TestArrayTypeSuffixInColonParam 参数写成 p: Point[4] 必须报错。
func TestArrayTypeSuffixInColonParam(t *testing.T) {
	requireErr(t, "冒号参数后缀数组",
		pointStruct+"fn take(p: Point[4]) -> f64 {\n    return p[0].x\n}\n",
		"'Point[4]' is not valid")
}

// TestArrayTypeSuffixInTypedParam 参数写成 Point[4] p 必须报错并归一。
func TestArrayTypeSuffixInTypedParam(t *testing.T) {
	requireErr(t, "类型名参数后缀数组",
		pointStruct+"fn take(Point[4] p) -> f64 {\n    return p[0].x\n}\n",
		"'Point[4] name' is not valid")
}

// TestArrayTypeSuffixInGenericArg 泛型实参写成 Box<Point[4]> 必须报错。
func TestArrayTypeSuffixInGenericArg(t *testing.T) {
	requireErr(t, "泛型实参后缀数组",
		pointStruct+"struct Box {\n    v: Point\n}\nfn main() {\n    Box<Point[4]> b\n}\n",
		"'Point[4]' is not valid")
}

// TestArrayTypeSuffixInClassMember class 体内的方法参数写成 Point[4] p 必须报错。
func TestArrayTypeSuffixInClassMember(t *testing.T) {
	requireErr(t, "class 方法参数后缀数组",
		pointStruct+"class Repo {\n    fn pick(Point[4] p) -> f64 {\n        return p[0].x\n    }\n}\n",
		"'Point[4] name' is not valid")
}

// TestArrayTypeSuffixInConstructor 构造函数参数写成 Point[4] p 必须报错。
func TestArrayTypeSuffixInConstructor(t *testing.T) {
	requireErr(t, "构造参数后缀数组",
		pointStruct+"class Repo {\n    constructor(Point[4] p) {\n        self.v = p[0].x\n    }\n}\n",
		"'Point[4] name' is not valid")
}

// TestArrayTypeSuffixInInterface 接口方法参数写成 Point[4] p 必须报错。
func TestArrayTypeSuffixInInterface(t *testing.T) {
	requireErr(t, "接口方法参数后缀数组",
		pointStruct+"interface Shape {\n    fn area(Point[4] p) f64\n}\n",
		"'Point[4] name' is not valid")
}

// --- 数组不能作返回类型 ---

// TestArrayReturnTypeSuffix 返回类型写成 -> Point[4] 必须报错。
func TestArrayReturnTypeSuffix(t *testing.T) {
	requireErr(t, "后缀数组返回类型",
		pointStruct+"fn make() -> Point[4] {\n    [4]Point r\n    return r\n}\n",
		"functions cannot return arrays")
}

// TestArrayReturnTypePrefix 返回类型写成 -> [4]Point 同样不能返回数组。
func TestArrayReturnTypePrefix(t *testing.T) {
	requireErr(t, "前缀数组返回类型",
		pointStruct+"fn make() -> [4]Point {\n    [4]Point r\n    return r\n}\n",
		"functions cannot return arrays")
}

// --- 合法写法必须照常通过 ---

// TestCanonicalFormsStillParse 规范写法（前缀数组类型、变量名后缀、
// 数组参数的三种形式）都不应产生解析错误。
func TestCanonicalFormsStillParse(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"前缀数组变量声明", pointStruct + "fn main() {\n    [4]Point a\n    a[0].x = 1.0\n}\n"},
		{"变量名后缀数组", pointStruct + "fn main() {\n    Point buf[8]\n    buf[0].x = 1.0\n}\n"},
		{"表达式语句 x[0] = 1", pointStruct + "fn main() {\n    [4]Point a\n    a[0].x = 1.0\n    a[1].x = 2.0\n}\n"},
		{"前缀数组参数", pointStruct + "fn take([4]Point p) -> f64 {\n    return p[0].x\n}\nfn main() {}\n"},
		{"冒号前缀数组参数", pointStruct + "fn take(p: [4]Point) -> f64 {\n    return p[0].x\n}\nfn main() {}\n"},
		{"参数名后缀数组", pointStruct + "fn take(Point p[4]) -> f64 {\n    return p[0].x\n}\nfn main() {}\n"},
		{"前缀数组类型别名", pointStruct + "type Path = [4]Point\nfn main() {}\n"},
		{"前缀数组泛型实参", pointStruct + "struct Box {\n    v: Point\n}\nfn main() {\n    Box<[4]Point> b\n}\n"},
		{"动态数组参数", "fn sum([]i64 arr) -> i64 {\n    return arr[0]\n}\nfn main() {}\n"},
		{"普通参数", "fn add(i64 a, i64 b) -> i64 {\n    return a + b\n}\nfn main() {}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireNoErr(t, tc.name, tc.src)
		})
	}
}

// TestArrayTypeSuffixNormalizes 验证报错同时把类型归一成前缀形式，
// 避免下游 sema 级联出无意义的 "类型不存在" 错误。
func TestArrayTypeSuffixNormalizes(t *testing.T) {
	src := pointStruct + "type Path = Point[4]\n"
	p := parseOne(t, src)
	prog := p.Parse()
	if p.GetErrorCollector().ErrorCount() == 0 {
		t.Fatal("期望报错，实际 0 个解析错误")
	}
	found := false
	for _, s := range prog.Statements {
		alias, ok := s.(*ast.TypeAliasStatement)
		if !ok || alias.Name != "Path" {
			continue
		}
		found = true
		if alias.UnderlyingType != "[4]Point" {
			t.Errorf("归一后类型 = %q，期望 [4]Point", alias.UnderlyingType)
		}
	}
	if !found {
		t.Error("未解析出 type Path 别名")
	}
}
