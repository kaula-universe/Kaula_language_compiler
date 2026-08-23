package parser_test

import (
	"runtime"
	"testing"
	"time"

	"kaula/internal/lexer"
	"kaula/internal/parser"
)

func tryParse(t *testing.T, name, src string) (ok bool, seconds float64) {
	done := make(chan struct{})
	go func() {
		lx := lexer.NewLexer(src)
		p := parser.NewParser(lx)
		p.Parse()
		close(done)
	}()
	select {
	case <-done:
		return true, 0
	case <-time.After(5 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Logf("HANG %s\n%s", name, buf[:n])
		return false, 0
	}
}

func TestMatchVariants(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"match_ident_arms_block", `import std.io
fn main() {
    int x = 3
    match x {
        1 => { println("one") },
        2 => { println("two") },
        _ => { println("many") }
    }
}`},
		{"match_ident_arms_expr", `import std.io
fn main() {
    int x = 3
    match x {
        1 => println("one"),
        2 => println("two"),
        _ => println("many")
    }
}`},
		{"match_literal_target", `import std.io
fn main() {
    match(3) {
        1 => println("one"),
        2 => println("two"),
        _ => println("many")
    }
}`},
		{"match_enum", `import std.io
enum Color {
    Red, Green, Blue
}
fn main() {
    Color c = Color.Red
    match c {
        Color.Red => println("red"),
        Color.Green => println("green"),
        _ => println("other")
    }
}`},
		{"no_match_bare", `import std.io
fn main() {
    int x = 3
    println(x)
}`},
		{"ident_lbrace_block", `import std.io
fn main() {
    x {
        println("hi")
    }
}`},
	}
	for _, c := range cases {
		ok, _ := tryParse(t, c.name, c.src)
		if !ok {
			t.Errorf("%s: hangs", c.name)
		}
	}
}