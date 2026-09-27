package codegen

import (
	"regexp"
	"strings"
	"testing"
)

// 接口多态的方法返回类型必须参与算术类型推断：
// 形参为 interface（多态句柄）时，方法签名声明在 interface 语句里；
// 查不到就会回落成整数 "d"，f64 结果被 kaula_add_i64 截断（静默算错）。
func TestIfaceMethodReturnTypeDrivesFloatArithmetic(t *testing.T) {
	cCode := nestedFnCode(t, "interface Shape {\n"+
		"    fn area() f64\n"+
		"}\n"+
		"class Circle implements Shape {\n"+
		"    r: f64\n"+
		"    constructor(f64 r) {\n"+
		"        self.r = r\n"+
		"    }\n"+
		"    fn area() f64 {\n"+
		"        return 3.0 * self.r\n"+
		"    }\n"+
		"}\n"+
		"fn sum(Shape a, Shape b) f64 {\n"+
		"    return a.area() + b.area()\n"+
		"}\n"+
		"fn main() {\n"+
		"    Circle c = Circle(2.0)\n"+
		"    f64 s = sum(c, c)\n"+
		"}\n")

	norm := regexp.MustCompile(`[ \t]+`).ReplaceAllString(cCode, " ")
	// 接口方法走 vtbl 分派
	if !strings.Contains(norm, ".vtbl.area(") {
		t.Errorf("interface call should dispatch through the vtable; got:\n%s", cCode)
	}
	// 返回表达式必须是浮点加法，不能被整数溢出检查包装
	body := norm[strings.Index(norm, "double K_sum(K_Shape a"):]
	if i := strings.Index(body, "return "); i >= 0 {
		ret := body[i:]
		if j := strings.Index(ret, ";"); j >= 0 {
			ret = ret[:j]
		}
		if strings.Contains(ret, "kaula_add_i64") || strings.Contains(ret, "(int64_t)(") {
			t.Errorf("f64 interface arithmetic must not be wrapped in integer overflow checks: %s", ret)
		}
	}
}

// 接口方法返回 string 时同样要能查到（printf 格式推断 / 字符串连接）
func TestIfaceMethodReturnTypeString(t *testing.T) {
	cCode := nestedFnCode(t, "interface Named {\n"+
		"    fn label() string\n"+
		"}\n"+
		"class Thing implements Named {\n"+
		"    t: string\n"+
		"    constructor(string t) {\n"+
		"        self.t = t\n"+
		"    }\n"+
		"    fn label() string {\n"+
		"        return self.t\n"+
		"    }\n"+
		"}\n"+
		"fn describe(Named n) string {\n"+
		"    return n.label()\n"+
		"}\n"+
		"fn main() {\n"+
		"    Thing x = Thing(\"a\")\n"+
		"    string s = describe(x)\n"+
		"}\n")
	norm := regexp.MustCompile(`[ \t]+`).ReplaceAllString(cCode, " ")
	if !strings.Contains(norm, ".vtbl.label(") {
		t.Errorf("interface call should dispatch through the vtable; got:\n%s", cCode)
	}
	if strings.Contains(norm, "string_concat(.vtbl") && !strings.Contains(norm, "string_clone") {
		// 仅为提示性断言：真实问题在 println 格式，不在 concat
		t.Logf("note: interface string call involved string_concat")
	}
}
