package parser_test

import (
	"runtime"
	"testing"
	"time"

	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
)

func TestMatchHang(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	src := `import std.io

fn main() {
    int x = 3
    match x {
        1 => {
            println("one")
        },
        2 => println("two"),
        _ => println("many")
    }
}
`
	done := make(chan struct{})
	var prog interface{}
	var err error
	go func() {
		lx := lexer.NewLexer(src)
		p := parser.NewParser(lx)
		prog = p.Parse()
		close(done)
	}()
	select {
	case <-done:
		t.Logf("parse ok err=%v program=%v", err, prog != nil)
	case <-time.After(8 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("parser hung on match minimum\n%s", buf[:n])
	}
}
