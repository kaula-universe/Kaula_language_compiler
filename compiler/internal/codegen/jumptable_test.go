package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/opt"
	"kaula/internal/sema"
)

// generateWithFrontendOptForTest 走完整链路并在 sema 之后插入前端优化：
// parse → sema → **前端 AST 优化** → codegen，返回生成的 C 代码。
func generateWithFrontendOptForTest(t *testing.T, src string) string {
	t.Helper()
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	opt.OptimizeProgram(prog, opt.DefaultOptions())

	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}
	return cCode
}

// TestIfChainBecomesJumpTable 验证等值 if 链被折叠成单一 switch 分派点。
func TestIfChainBecomesJumpTable(t *testing.T) {
	src := `fn dispatch(i32 code) i32 {
    i32 r = 0
    if (code == 1) {
        r = 10
    } else if (code == 2) {
        r = 20
    } else if (code == 3) {
        r = 30
    } else if (code == 4) {
        r = 40
    } else {
        r = -1
    }
    return r
}
` + "\nfn main() i32 {\n    return 0\n}\n"
	cCode := generateWithFrontendOptForTest(t, src)

	if !strings.Contains(cCode, "switch (code)") {
		t.Errorf("期望生成 switch 分派，实际生成的 C：\n%s", cCode)
	}
	for _, want := range []string{"case 1:", "case 2:", "case 3:", "case 4:", "default:"} {
		if !strings.Contains(cCode, want) {
			t.Errorf("生成的 C 缺少 %q：\n%s", want, cCode)
		}
	}
	// 原 if 链形态不应再出现（说明确实被折叠了，而不是两套并存）
	if strings.Contains(cCode, "if (code == 1)") {
		t.Errorf("if 链未被折叠，仍生成了顺序比较：\n%s", cCode)
	}
}

// TestIfChainWithBreakKeepsIfForm 验证分支内含 break 的链保持原样。
//
// 这是本优化最危险的静默错误来源：C 里 `break` 在 switch 内会变成
// "跳出 switch"，而原语义是跳出外层循环。必须放弃改写。
func TestIfChainWithBreakKeepsIfForm(t *testing.T) {
	src := `fn guard(i32 n) i32 {
    i32 acc = 0
    i32 i = 0
    while (i < 10) {
        if (n == 1) {
            acc = acc + 1
            break
        } else if (n == 2) {
            acc = acc + 2
            break
        } else if (n == 3) {
            acc = acc + 3
            break
        } else {
            acc = acc + 100
            break
        }
        i = i + 1
    }
    return acc
}
` + "\nfn main() i32 {\n    return 0\n}\n"
	cCode := generateWithFrontendOptForTest(t, src)

	if strings.Contains(cCode, "switch (n)") {
		t.Errorf("分支内含 break，不应改写成 switch（语义会被静默改变）：\n%s", cCode)
	}
	if !strings.Contains(cCode, "while (i < 10)") {
		t.Errorf("外层循环丢失：\n%s", cCode)
	}
}

// TestNegativeCaseLabels 验证负常量可以成为 case 标签。
func TestNegativeCaseLabels(t *testing.T) {
	src := `fn f(i32 code) i32 {
    i32 r = 0
    if (code == -1) {
        r = 100
    } else if (code == -2) {
        r = 200
    } else if (code == -3) {
        r = 300
    } else {
        r = -9
    }
    return r
}
` + "\nfn main() i32 {\n    return 0\n}\n"
	cCode := generateWithFrontendOptForTest(t, src)

	for _, want := range []string{"case -1:", "case -2:", "case -3:"} {
		if !strings.Contains(cCode, want) {
			t.Errorf("生成的 C 缺少 %q：\n%s", want, cCode)
		}
	}
}

// TestJumpTableNoDefaultKeepsFallthrough 验证无 else 的链不产出 default，
// 未命中时控制流继续向下（与原 if 链语义一致）。
func TestJumpTableNoDefaultKeepsFallthrough(t *testing.T) {
	src := `fn f(i32 code) i32 {
    i32 r = 7
    if (code == 1) {
        r = 11
    } else if (code == 2) {
        r = 22
    } else if (code == 3) {
        r = 33
    }
    return r
}
` + "\nfn main() i32 {\n    return 0\n}\n"
	cCode := generateWithFrontendOptForTest(t, src)

	if !strings.Contains(cCode, "switch (code)") {
		t.Errorf("期望生成 switch：\n%s", cCode)
	}
	if strings.Contains(cCode, "default:") {
		t.Errorf("无 else 的链不应产出 default 分支：\n%s", cCode)
	}
}

// TestShortChainNotConverted 验证短链不改写（收益不足）。
func TestShortChainNotConverted(t *testing.T) {
	src := `fn f(i32 code) i32 {
    i32 r = 0
    if (code == 1) {
        r = 11
    } else if (code == 2) {
        r = 22
    } else {
        r = -1
    }
    return r
}
` + "\nfn main() i32 {\n    return 0\n}\n"
	cCode := generateWithFrontendOptForTest(t, src)

	if strings.Contains(cCode, "switch (code)") {
		t.Errorf("仅 2 段等值比较不应改写成 switch：\n%s", cCode)
	}
}
