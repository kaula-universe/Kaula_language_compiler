package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kaula/internal/config"
)

// 回归：freestanding 模板的 String 兜底定义原先是「仅 ptr」形态
//   typedef struct { size_t len; char* ptr; } String;
// 但代码生成器按 SSO 形态产出字符串字面量 (String){.len=..,.sso=".."}
// 并调用 string_cdata() 解包实参，导致 freestanding 下任何用到 Kaula
// string 的程序都编译失败：
//   error: field designator 'sso' does not refer to any field in type 'String'
//   error: call to undeclared function 'string_cdata'
//
// 本测试锁住三处修复：
//   1. 两个模板的 String 定义必须是 SSO union（含 sso[16]）
//   2. 两个模板必须自带 SSO 感知的 string_cdata 宏
//   3. codegen freestanding 分支的 includes 必须带 SSO 感知兜底宏

func locateTemplatesDir(t *testing.T) string {
	t.Helper()
	for _, cand := range []string{
		filepath.Join("..", ".."),
		".",
		filepath.Join("..", "..", "..", "compiler"),
	} {
		if _, err := os.Stat(filepath.Join(cand, "templates", "freestanding.c.tmpl")); err == nil {
			return filepath.Join(cand, "templates")
		}
	}
	abs, _ := os.Getwd()
	t.Fatalf("cannot locate templates/ dir from %s", abs)
	return ""
}

func TestTemplatesUseSSOStringLayout(t *testing.T) {
	dir := locateTemplatesDir(t)
	for _, name := range []string{"freestanding.c.tmpl", "user.c.tmpl"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		s := string(data)
		if !strings.Contains(s, "char sso[16]") {
			t.Errorf("%s 缺少 SSO 槽位 char sso[16]（会报 field designator 'sso'）", name)
		}
		if !strings.Contains(s, "union {") {
			t.Errorf("%s 的 String 必须是 SSO union 布局", name)
		}
		if !strings.Contains(s, "STRING_SSO_CAPACITY ? (const char*)(s).sso") {
			t.Errorf("%s 缺少 SSO 感知的 string_cdata 宏", name)
		}
		if strings.Contains(s, "#define string_cdata(s) ((s).ptr)") {
			t.Errorf("%s 仍带非 SSO 的 string_cdata 兜底（短串会读未初始化 ptr）", name)
		}
	}
}

func TestFreestandingIncludesHaveSSOAwareCDdata(t *testing.T) {
	dir := locateTemplatesDir(t)
	cfg := &config.Config{
		BasePath:     filepath.Dir(dir),
		TemplatePath: dir,
		Freestanding: true,
	}
	cg := NewCodeGenerator(cfg)
	prog := parse("fn main() -> i32 {\n    auto msg = \"hello\"\n    return 0\n}\n")
	cCode := cg.Generate(prog)

	// includes 段必须带 SSO 感知兜底宏（freestanding 下不链接 kaula.h）
	if !strings.Contains(cCode, "STRING_SSO_CAPACITY ? (const char*)(s).sso") {
		t.Errorf("freestanding includes 缺少 SSO 感知 string_cdata 兜底宏，生成 C 前 40 行:\n%s",
			strings.Join(strings.Split(cCode, "\n")[:min(40, len(strings.Split(cCode, "\n")))], "\n"))
	}
	if strings.Contains(cCode, "#define string_cdata(s) ((s).ptr)") {
		t.Error("freestanding includes 不应带非 SSO 的 string_cdata 兜底")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
