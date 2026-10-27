package codegen

import (
	"regexp"
	"strings"
	"testing"
)

// countReleases 统计对变量 name 的释放调用次数。
// 用标识符边界匹配，避免 kmm_v4_free(p) 被裸 free(p) 模式重复计数。
func countReleases(cCode, name string) int {
	n := 0
	for _, fn := range []string{"kmm_v4_free", "kmm_v4_global_free", "kmm_v4_survivor_free",
		"string_survivor_free", "free"} {
		re := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(fn) +
			`\(` + regexp.QuoteMeta(name) + `\)`)
		n += len(re.FindAllString(cCode, -1))
	}
	return n
}

// 用户显式调用 *_free 释放的指针，所有权已转移：代码生成不得再补一次结构性释放。
// 回归背景：此前 main 与普通函数体都会在作用域末尾补 kmm_v4_survivor_free，
// 与用户的显式释放叠加成 double free（slab 路径重复入垃圾箱导致 allocated
// 计数失真、桶容量逐步失效；libc 路径则是直接 double free）。
func TestUserFree_NoStructuralDoubleFree(t *testing.T) {
	cases := []struct {
		name string
		src  string
		varName string
	}{
		{
			name: "语句式显式释放(main)",
			src: "fn main() {\n" +
				"    i32* p = as<i32*>(std.memory.kmm_v4_global_alloc(16))\n" +
				"    p[0] = 1\n" +
				"    std.memory.kmm_v4_free(p)\n" +
				"}\n",
			varName: "p",
		},
		{
			name: "if 条件式显式释放(main)",
			src: "fn main() {\n" +
				"    i32* p = as<i32*>(std.memory.kmm_v4_global_alloc(16))\n" +
				"    p[0] = 1\n" +
				"    if std.memory.kmm_v4_global_free(p) {\n    }\n" +
				"}\n",
			varName: "p",
		},
		{
			name: "普通函数体内显式释放",
			src: "fn f() {\n" +
				"    i32* q = as<i32*>(std.memory.kmm_v4_global_alloc(16))\n" +
				"    q[0] = 2\n" +
				"    std.memory.kmm_v4_free(q)\n" +
				"}\n" +
				"fn main() {\n    f()\n}\n",
			varName: "q",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cCode := generateWithEscape(t, tc.src, map[string]interface{}{tc.varName: "Heap"})
			if n := countReleases(cCode, tc.varName); n != 1 {
				t.Errorf("指针 %s 应只被释放 1 次，实际 %d 次；生成代码：\n%s",
					tc.varName, n, cCode)
			}
		})
	}
}

// 未显式释放的指针仍要保留结构性回收（不能被"用户已释放"过滤误伤）。
func TestNoUserFree_KeepsStructuralReclaim(t *testing.T) {
	src := "fn main() {\n" +
		"    i32* p = as<i32*>(std.memory.kmm_v4_global_alloc(16))\n" +
		"    p[0] = 1\n" +
		"}\n"
	cCode := generateWithEscape(t, src, map[string]interface{}{"p": "Heap"})
	if !strings.Contains(cCode, "kmm_v4_survivor_free(p)") {
		t.Errorf("未显式释放的指针仍应保留结构性回收；生成代码：\n%s", cCode)
	}
}

// 释放类函数名识别：free / *_free 命中，其他不命中。
func TestIsReleaseCallName(t *testing.T) {
	yes := []string{"free", "kmm_v4_free", "kmm_v4_global_free", "string_free",
		"fast_free", "std_free", "db_result_free"}
	no := []string{"kmm_v4_alloc", "kmm_v4_global_alloc", "println", "freed",
		"fs_free_space", "kmm_v4_usage"}
	for _, n := range yes {
		if !isReleaseCallName(n) {
			t.Errorf("%s 应识别为释放类函数", n)
		}
	}
	for _, n := range no {
		if isReleaseCallName(n) {
			t.Errorf("%s 不应识别为释放类函数", n)
		}
	}
}
