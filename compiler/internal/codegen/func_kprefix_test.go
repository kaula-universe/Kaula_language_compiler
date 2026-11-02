package codegen

import "testing"

// TestUserFuncCName 验证用户函数命名规则（K_ 前缀已取消，定义端与调用端统一用原名）：
//   - 用户自定义 fn（含嵌套、跨文件 import）→ 原名 <name>
//   - main（C 入口）保持原名
//   - extern/stdlib/第三方 C 函数（不在用户函数集合）保持原名
//   - 非用户定义的 max/min/abs 重定向 kaula_ 运行时实现以避开 C 宏
//   - 用户自己定义的 fn max 走原名，而非 kaula_max
func TestUserFuncCName(t *testing.T) {
	cg := &CodeGenerator{
		userFuncNames:    map[string]bool{"grow": true, "double": true, "max": true, "inner": true},
		localImportFuncs: map[string]bool{"helper": true},
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"普通用户函数", "grow", "grow"},
		{"C 保留字函数名", "double", "double"},
		{"嵌套用户函数", "inner", "inner"},
		{"跨文件用户函数", "helper", "helper"},
		{"用户自定义的 max 用原名", "max", "max"},
		{"main 保持原名", "main", "main"},
		{"空名保持", "", ""},
		{"extern C 函数保持原名", "strlen", "strlen"},
		{"stdlib 函数保持原名", "std_malloc", "std_malloc"},
		{"非用户定义的 min 重定向", "min", "kaula_min"},
		{"非用户定义的 abs 重定向", "abs", "kaula_abs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cg.UserFuncCName(tt.in); got != tt.want {
				t.Errorf("UserFuncCName(%q) = %q, 期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestUserFuncCNameEmpty 未初始化集合时不应 panic，且 extern/main 保持原名。
func TestUserFuncCNameEmpty(t *testing.T) {
	cg := &CodeGenerator{}
	if got := cg.UserFuncCName("printf"); got != "printf" {
		t.Errorf("extern 函数应保持原名, 得到 %q", got)
	}
	if got := cg.UserFuncCName("main"); got != "main" {
		t.Errorf("main 应保持原名, 得到 %q", got)
	}
}
