package lexer

import (
	"strings"
	"testing"
)

// 回归：#[...] 属性内容、字符字面量、#[asm] 原文透传此前都用
// `result += string(input[pos])` 逐字节拼接。string(byte) 会把字节当作
// Unicode 码点再编码，导致任何 >= 0x80 的字符被二次编码成乱码。
func TestAttributeContentPreservesUTF8(t *testing.T) {
	l := NewLexer(`#[test("中文")]`)
	tok := l.Next()
	if tok.Type != TOKEN_ATTRIBUTE {
		t.Fatalf("expected TOKEN_ATTRIBUTE, got %v", tok.Type)
	}
	if !strings.Contains(tok.Value, "中文") {
		t.Errorf("attribute content must preserve UTF-8 bytes, got %q (% x)", tok.Value, []byte(tok.Value))
	}
	if strings.Contains(tok.Value, "ä¸") {
		t.Errorf("attribute content shows double-encoding mojibake: %q", tok.Value)
	}
}

// #[asm] 函数体原文透传走 ScanUntilRbrace，非 ASCII 注释同样不能被破坏
func TestScanUntilRbracePreservesUTF8(t *testing.T) {
	l := NewLexer("mov %rax, 1 // 中文注释\n}")
	got := l.ScanUntilRbrace()
	if !strings.Contains(got, "中文注释") {
		t.Errorf("asm body must preserve UTF-8, got %q (% x)", got, []byte(got))
	}
}

// 普通字符串字面量本就正确，这里作为对照锁定预期
func TestStringLiteralPreservesUTF8(t *testing.T) {
	l := NewLexer(`"中文"`)
	tok := l.Next()
	if tok.Value != "中文" {
		t.Errorf("string literal must preserve UTF-8, got %q", tok.Value)
	}
}
