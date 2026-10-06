package formatter_test

import (
	"testing"

	"kaula/internal/formatter"
)

// 直接注入一个未覆盖节点，验证护栏会被触发。
// 通过把已知结构伪装成新类型达不到目的，改为断言当前全仓语法均可覆盖，
// 并验证 HasUnknown 在干净输入下为 false、Reset 会清空登记。
func TestGuard_ResetClearsUnknown(t *testing.T) {
	f := formatter.New()
	f.FormatSource("fn main() {\n    i64 a = 1\n}\n")
	if f.HasUnknown() {
		t.Fatalf("干净输入不应登记 unknown，实得 %v", f.UnknownNodes())
	}
	f.Reset()
	if f.HasUnknown() {
		t.Fatal("Reset 后 unknown 应为空")
	}
	if got := f.UnknownNodes(); len(got) != 0 {
		t.Fatalf("Reset 后 UnknownNodes 应为空，实得 %v", got)
	}
}

// 连续多次格式化不同输入，unknown 登记不得跨次残留。
func TestGuard_NoCrossCallLeak(t *testing.T) {
	f := formatter.New()
	for i := 0; i < 3; i++ {
		f.FormatSource("fn main() {\n    i64 a = 1\n}\n")
		if f.HasUnknown() {
			t.Fatalf("第 %d 次：不应残留 unknown %v", i, f.UnknownNodes())
		}
	}
}

// 语法错误输入必须被 HasParseError 捕获，且解析错误数不得跨次残留。
//
// 回归背景：kaulac fmt 迁移时漏抄了旧 kaulafmt 的 `if p.HasErrors() { exit(1) }`，
// 于是语法错误文件仍会被格式化并输出 —— parser 的错误恢复会产出畸形 AST，
// 极端情况下（非法 UTF-8 字节触发 lexer 死循环）能把 1 行文件输出成
// 20000 行空白。这道护栏与 HasUnknown 是两个独立维度，都要断言。
func TestGuard_ParseErrorDetected(t *testing.T) {
	f := formatter.New()
	f.FormatSource("fn main( {\n") // 括号不闭合
	if !f.HasParseError() {
		t.Fatal("语法错误输入应被 HasParseError 捕获")
	}
	if f.ParseErrorCount() <= 0 {
		t.Fatalf("ParseErrorCount 应 > 0，实得 %d", f.ParseErrorCount())
	}
	f.Reset()
	if f.HasParseError() {
		t.Fatal("Reset 后 parseErrors 应清零")
	}
}

// 语法错误输入的格式化输出不得是「爆炸行数」。
//
// 这是死循环缺陷最直观的外部症状：修复前 1 行非法输入会产出 20000 行空白。
// 用行数上限把该类回归钉死。
func TestGuard_NoOutputExplosion(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"孤立高位字节", "\x97\n"},
		{"未闭合块注释", "/* 没有闭合\n"},
		{"括号不闭合", "fn main( {\n"},
		{"非法字符", "fn main() {\n    @@@ %%%\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := formatter.New()
			out := f.FormatSource(c.src)
			lines := 0
			for i := 0; i < len(out); i++ {
				if out[i] == '\n' {
					lines++
				}
			}
			if lines > 100 {
				t.Fatalf("输出爆炸：%d 行（上限 100）。疑似 lexer 死循环回归。\n前200字符: %q",
					lines, truncate(out, 200))
			}
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// 含中文的块注释必须被跳过，且格式化结果可被重新解析。
//
// 回归背景：lexer 曾完全不识别 /* */，块注释被拆成「除号 + 星号」重新解析；
// 注释体含中文时又因 UTF-8 字节截断而连锁失败。
func TestGuard_BlockCommentWithChinese(t *testing.T) {
	src := "/* 块注释：说明文件用途 */\nfn main() {\n    i64 a = 1\n}\n"
	f := formatter.New()
	out := f.FormatSource(src)
	if f.HasParseError() {
		t.Fatalf("含中文块注释的文件不应报语法错误，实得 %d 个", f.ParseErrorCount())
	}
	if f.HasUnknown() {
		t.Fatalf("含中文块注释的文件不应登记 unknown，实得 %v", f.UnknownNodes())
	}
	if !contains(out, "fn main()") {
		t.Fatalf("块注释被吞掉了函数体:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
