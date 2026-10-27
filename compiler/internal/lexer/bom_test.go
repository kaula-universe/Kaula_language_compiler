package lexer

import "testing"

// UTF-8 BOM（EF BB BF）开头的源文件必须能正常词法分析。
// Windows 编辑器与脚本（PowerShell Set-Content -Encoding utf8 等）常写 BOM，
// 不跳过会导致 "Syntax Error at 1:1"。
func TestLexerSkipsUTF8BOM(t *testing.T) {
	src := "\xEF\xBB\xBFfn main() {\n    i64 x = 1\n}\n"
	lx := NewLexer(src)
	if got := lx.pos; got != 3 {
		t.Errorf("lexer should start after the 3-byte BOM, pos = %d", got)
	}
	tok := lx.Next()
	// 第一个有效 token 应该是 fn，而不是被 BOM 污染的错误
	if len(tok.Value) == 3 && tok.Value[0] == 0xEF {
		t.Errorf("BOM leaked into the first token: %+v", tok)
	}
	if lx.GetErrorCollector().HasErrors() {
		t.Errorf("BOM-prefixed source produced lexer errors: %v", lx.GetErrorCollector().Errors())
	}
}

// 无 BOM 时行为不变（pos 从 0 开始）
func TestLexerWithoutBOMStartsAtZero(t *testing.T) {
	lx := NewLexer("fn main() {}\n")
	if got := lx.pos; got != 0 {
		t.Errorf("pos = %d, want 0 for BOM-less source", got)
	}
}
