package sor

import "testing"

func TestComptimeFeedback_Basic(t *testing.T) {
	cf := NewComptimeFeedback()

	cf.RegisterArraySize("arr", 100)
	cf.RegisterStructSize("Point", 16)
	cf.RegisterLoopCount("loop_1", 50)

	if size, ok := cf.GetArraySize("arr"); !ok || size != 100 {
		t.Errorf("数组大小错误，期望 100，实际 %d (ok=%v)", size, ok)
	}

	if size, ok := cf.GetStructSize("Point"); !ok || size != 16 {
		t.Errorf("结构体大小错误，期望 16，实际 %d (ok=%v)", size, ok)
	}

	if count, ok := cf.GetLoopCount("loop_1"); !ok || count != 50 {
		t.Errorf("循环次数错误，期望 50，实际 %d (ok=%v)", count, ok)
	}

	// 测试未知值
	if _, ok := cf.GetArraySize("unknown"); ok {
		t.Error("未知数组不应该有大小")
	}

	summary := cf.Summary()
	if summary == "" {
		t.Error("摘要不应该为空")
	}
	t.Logf("摘要: %s", summary)
}

func TestComptimeFeedback_EstimateArrayMemory(t *testing.T) {
	cf := NewComptimeFeedback()

	// 编译期已知大小的数组
	cf.RegisterArraySize("known_arr", 100)
	mem := cf.EstimateArrayMemory("known_arr", 4, 10)
	if mem != 400 { // 100 * 4
		t.Errorf("已知数组内存估算错误，期望 400，实际 %d", mem)
	}

	// 编译期未知大小的数组，使用估算值
	mem = cf.EstimateArrayMemory("unknown_arr", 4, 10)
	if mem != 40 { // 10 * 4
		t.Errorf("未知数组内存估算错误，期望 40，实际 %d", mem)
	}
}

func TestSORAnalyzer_ComptimeIntegration(t *testing.T) {
	analyzer := NewSORAnalyzer()

	// 设置编译期反馈
	cf := NewComptimeFeedback()
	cf.RegisterArraySize("buffer", 1024)
	cf.RegisterStructSize("Header", 64)
	analyzer.SetComptimeFeedback(cf)

	// 验证反馈已设置
	if analyzer.GetComptimeFeedback() == nil {
		t.Fatal("编译期反馈不应该为 nil")
	}

	// 执行一个简单的分析
	stmts := []Stmt{
		{Kind: StmtLet, Line: 1, VarName: "x", TypeName: "int", IsComposite: false},
	}

	errors := analyzer.Analyze(stmts)
	if len(errors) != 0 {
		t.Errorf("不应该有错误，实际有 %d 个", len(errors))
	}

	// 验证执行日志中包含编译期反馈信息
	log := analyzer.GetExecLog()
	foundComptime := false
	for _, line := range log {
		if contains(line, "编译期反馈") {
			foundComptime = true
			break
		}
	}
	if !foundComptime {
		t.Error("执行日志中应该包含编译期反馈信息")
		t.Log("执行日志:")
		for _, line := range log {
			t.Log("  ", line)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
