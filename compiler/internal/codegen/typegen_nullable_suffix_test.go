package codegen

import "testing"

// 回归：可空类型后缀 `?` 必须在 convertType 里统一擦除。
//
// parser 在函数形参的 ParamTypes 里保留 `?` 原文（ParamNullables 单独记录
// 可空标志），形参路径把 ParamTypes 原样交给 convertType。修复前
// convertType 不处理 `?`，`i64?` 形参被原样拼进 C：
//
//	int64_t f(i64?* p);   ->  clang: expected ')'
//
// 局部变量声明路径会先剥掉 `?` 所以一直正常，形参路径漏了。
// 修复：convertType 入口统一兜底——遇 `?` 后缀置 nullable=true 并剥掉。
func TestConvertTypeErasesNullableSuffix(t *testing.T) {
	tg := NewTypeGenerator(newCodeGeneratorForTest(t))

	cases := []struct {
		kaulaType string
		nullable  bool
		want      string
		why       string
	}{
		// —— 缺陷本体：形参路径带着 `?` 原文进来 ——
		{"i64?", false, "int64_t*", "形参 ParamTypes 保留 ? 原文且 nullable=false 的漏网路径"},
		{"i64?", true, "int64_t*", "? 后缀 + nullable=true 双重标记也只生成一个 *"},
		{"i32?", false, "int32_t*", "各宽度整型统一"},
		{"string?", false, "String*", "string 可空"},
		{"Task?", false, "Task*", "用户结构体可空"},
		// —— 对照：指针类型加 ? 不应再追加星号 ——
		{"i64*?", false, "int64_t*", "指针类型的 ? 不双写 *"},
		// —— 非回归对照：无可空后缀行为不变 ——
		{"i64", false, "int64_t", "普通标量"},
		{"i64", true, "int64_t*", "nullable=true 标量仍升为指针"},
		{"i64*", false, "int64_t*", "普通指针"},
	}

	for _, c := range cases {
		got := tg.convertType(c.kaulaType, c.nullable)
		if got != c.want {
			t.Errorf("convertType(%q, %v) = %q, 期望 %q —— %s",
				c.kaulaType, c.nullable, got, c.want, c.why)
		}
	}
}
