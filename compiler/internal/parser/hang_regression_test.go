package parser

import (
	"testing"
	"time"

	"kaula/internal/lexer"
)

// 回归：prefix-call 块内出现语法错误时，解析器曾无限循环。
//
// 触发形态：`IDENT {` 会被当作 prefix-call 块（parseStatementIterative 里
// `peekTok == LBRACE` 的分支），块内若有解析不了的语句，
// parseStatementIterative 返回 nil 且 token 不前进，
// 而外层 for 当时留了一个**空的 if**（注释写着「需要手动前进以避免死循环」
// 却什么都没做），于是整个 kaulac 挂死，一个字都不输出。
//
// 这不是理论问题：基准驱动里一个 while 体里同时有 `&&` 与 `spawn`，
// 错误恢复把 token 落到 `IDENT {` 上，就撞进了这条路。
//
// 用例本身是**故意写错**的 —— 它验的不是「能解析」，而是「能退出」。
// 所以断言是「限时内返回」而不是「无错误」。
func TestParsePrefixCallBlockWithSyntaxErrorTerminates(t *testing.T) {
	// `cfg { ... }` -> IDENT + LBRACE，进 prefix-call 分支
	// 块内的 `&&` 不被支持，制造一条解析不了的语句
	src := `import std.io

fn main() -> i32 {
    cfg {
        i64 i = 0;
        i = i + 1 && i > 0;
    }
    std.io.println("done");
    return 0;
}
`

	done := make(chan struct{})
	go func() {
		defer close(done)
		// 返回值丢弃：语法错误是预期的
		NewParser(lexer.NewLexer(src)).Parse()
	}()

	select {
	case <-done:
		// 正常退出即通过
	case <-time.After(5 * time.Second):
		// 不靠 go test 的全局超时：那样整个包的测试都要陪葬，
		// 而且报出来的是「test timed out」，看不出是哪条用例。
		t.Fatal("解析器在 prefix-call 块的语法错误上死循环：5 秒未返回")
	}
}
