package lexer_test

import (
	"strings"
	"testing"

	"kaula/internal/lexer"
)

// collect 反复取 token 直到 EOF，返回 token 序列。
// 同时充当死循环探针：若 lexer 不推进位置，上限会先触发。
func collect(t *testing.T, src string, limit int) []lexer.Token {
	t.Helper()
	l := lexer.NewLexer(src)
	var out []lexer.Token
	for i := 0; i < limit; i++ {
		tk := l.Next()
		out = append(out, tk)
		if tk.Type == lexer.TOKEN_EOF {
			return out
		}
	}
	t.Fatalf("取 token 超过 %d 个仍未见EOF —— lexer 很可能在原地打转", limit)
	return out
}

// 非法字节（孤立 UTF-8 续字节）不得让 Next() 死循环。
//
// 回归背景：Next() 的 default 分支报错后 continue 却不推进 l.pos，
// 导致同一字节被反复扫描、每次吐一个空 token，调用方拿到几万个垃圾 token，
// 表现为 kaulac fmt 把 1 行文件输出成 20000 行空白。
func TestLexerNoInfiniteLoopOnInvalidByte(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"孤立续字节 0x97", "\x97\n"},
		{"孤立续字节 0x9D", "\x9D\n"},
		{"截断的 UTF-8 序列", "\xe5\x9d\n"},
		{"BOM 后的孤立高位字节", "\xef\xbb\xbf\x97\n"},
		{"NUL 字节", "\x00\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks := collect(t, c.src, 100)
			// 关键：必须有 EOF，且 token 数是有限的
			if toks[len(toks)-1].Type != lexer.TOKEN_EOF {
				t.Fatal("未到达 EOF")
			}
		})
	}
}

// 非法字节必须被跳过、位置必须前进，而不是反复产出空 token。
func TestLexerInvalidByteAdvancesPosition(t *testing.T) {
	l := lexer.NewLexer("\x97abc\n")
	first := l.Next()
	if first.Type == lexer.TOKEN_EOF {
		t.Fatal("不应直接返回 EOF")
	}
	// 后续必须能读到 EOF，说明位置已越过非法字节
	sawEOF := false
	for i := 0; i < 50; i++ {
		if l.Next().Type == lexer.TOKEN_EOF {
			sawEOF = true
			break
		}
	}
	if !sawEOF {
		t.Fatal("扫到 EOF 仍未终止，位置大概率未推进")
	}
}

// UTF-8 多字节标识符必须被完整识别，不能从中间截断。
//
// 回归背景：scanIdentifier 对单个字节调 unicode.IsLetter(rune(c))，
// rune(0xE5) 恰是字母 'å' 而 rune(0x9D) 是控制字符，
// 于是「块」(E5 9D 97) 被吃掉首字节、续字节被留在缓冲区。
func TestLexerMultibyteIdentifierNotTruncated(t *testing.T) {
	toks := collect(t, "中文标识符 = 1\n", 50)

	var idents []string
	for _, tk := range toks {
		if tk.Type == lexer.TOKEN_IDENT {
			idents = append(idents, tk.Value)
		}
	}
	if len(idents) == 0 {
		t.Fatal("未识别出任何标识符")
	}
	// 第一个标识符应当是完整的中文串，而不是被截断的半个字符
	if idents[0] != "中文标识符" {
		t.Fatalf("标识符被截断：实得 %q，期望 %q", idents[0], "中文标识符")
	}
}

// 三种注释都必须被跳过，且**不产生任何 token**。
func TestLexerSkipsAllCommentForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // 期望唯一剩下的标识符
	}{
		{"# 行注释", "# 这是注释\nfoo\n", "foo"},
		{"// 行注释", "// 这是注释\nfoo\n", "foo"},
		{"/* 块注释 */", "/* 这是注释 */\nfoo\n", "foo"},
		{"/* 多行块注释 */", "/*\n * 多行\n * 说明\n */\nfoo\n", "foo"},
		{"含中文的块注释", "/* 中文块注释 */\nfoo\n", "foo"},
		{"块注释后紧跟代码", "/*x*/foo\n", "foo"},
		{"行注释在行尾", "foo # 尾注释\n", "foo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks := collect(t, c.src, 50)
			var idents []string
			for _, tk := range toks {
				if tk.Type == lexer.TOKEN_IDENT {
					idents = append(idents, tk.Value)
				}
			}
			if len(idents) != 1 || idents[0] != c.want {
				t.Fatalf("注释未被干净跳过：实得标识符 %v，期望 [%s]", idents, c.want)
			}
		})
	}
}

// 块注释内容里的引号/运算符不得泄漏成 token
//（此前 /* 被拆成除号+星号，注释体被当成表达式重新解析）。
func TestLexerBlockCommentContentIsInert(t *testing.T) {
	toks := collect(t, "/* a + b * c / d \"s\" */\nfoo\n", 50)
	for _, tk := range toks {
		if tk.Type != lexer.TOKEN_IDENT && tk.Type != lexer.TOKEN_EOF {
			t.Fatalf("块注释内容泄漏成 token：type=%d value=%q", int(tk.Type), tk.Value)
		}
	}
}

// 未闭合块注释必须报错但**不得死循环**。
func TestLexerUnterminatedBlockCommentTerminates(t *testing.T) {
	l := lexer.NewLexer("/* 没有闭合\nfoo\n")
	deadline := 200
	sawEOF := false
	for i := 0; i < deadline; i++ {
		if l.Next().Type == lexer.TOKEN_EOF {
			sawEOF = true
			break
		}
	}
	if !sawEOF {
		t.Fatalf("未闭合块注释导致 lexer 超过 %d 个 token 未终止", deadline)
	}
}

// 正常含中文源码的 token 流必须有界（防止任何新的死循环回归）。
func TestLexerChineseSourceTerminates(t *testing.T) {
	src := "# 文件头：中文注释\nimport std.io\n\n" +
		"// 函数说明\nfn 计算(甲: i64, 乙: i64) i64 {\n" +
		"    // 块注释：/* 这里也有 */\n" +
		"    return 甲 + 乙\n}\n"
	toks := collect(t, src, 500)
	if toks[len(toks)-1].Type != lexer.TOKEN_EOF {
		t.Fatal("未到达 EOF")
	}
	// 不应出现空值 IDENT（死循环的特征）
	for _, tk := range toks {
		if tk.Type == lexer.TOKEN_IDENT && strings.TrimSpace(tk.Value) == "" {
			t.Fatal("出现空值 IDENT，疑似死循环")
		}
	}
}
