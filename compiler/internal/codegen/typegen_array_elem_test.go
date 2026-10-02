package codegen

import "testing"

// 回归：数组与指针的元素类型映射绕过了 clibTypeMap。
//
// MapKaulaTypeToC 的四个分支（[N]T / []T / *T / T*）都直接递归调用自己，
// 而只在 clibTypeMap 里登记的类型（如 Handle -> KaulaHandle）不在 globalTypeMap。
// 于是这些类型在标量位置正常、在数组或指针位置漏成原始 Kaula 名：
//
//	[4]Handle hs;   ->  Handle hs[4];   ->  clang: use of undeclared identifier 'Handle'
//	Handle h;       ->  KaulaHandle h;  ->  正常
//
// 对一门以并发为卖点的语言，这是实质限制：写 worker pool 时没法把句柄存进数组。
func TestArrayElementTypeMapsClibTypes(t *testing.T) {
	tg := NewTypeGenerator(newCodeGeneratorForTest(t))

	cases := []struct {
		kaulaType string
		want      string
		why       string
	}{
		{"[4]Handle", "KaulaHandle[4]", "定长数组元素是只在 clibTypeMap 登记的类型"},
		{"[8]Handle", "KaulaHandle[8]", "同上，元素个数不影响"},
		{"[]Handle", "KaulaHandle*", "动态数组（切片）"},
		{"*Handle", "KaulaHandle*", "指针前缀记法"},
		{"Handle*", "KaulaHandle*", "指针后缀记法"},
		// 非回归对照：已在 globalTypeMap 的类型行为不变
		{"[4]i64", "int64_t[4]", "已在 globalTypeMap 的类型不应受影响"},
	}

	for _, c := range cases {
		got := tg.MapKaulaTypeToC(c.kaulaType)
		if got != c.want {
			t.Errorf("MapKaulaTypeToC(%q) = %q, 期望 %q —— %s",
				c.kaulaType, got, c.want, c.why)
		}
	}

	/*
	 * 标量位置对照：走 convertType（它才查 clibTypeMap）。
	 *
	 * 这正是原 bug 的不对称之处 —— 标量正确、数组错误，
	 * 差别就在于标量先查了 clibTypeMap 而数组分支没查。
	 */
	if got := tg.convertType("Handle", false); got != "KaulaHandle" {
		t.Errorf("convertType(\"Handle\") = %q, 期望 KaulaHandle", got)
	}
}
