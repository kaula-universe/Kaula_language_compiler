package main

import (
	"fmt"
	"os"

	"kaula/internal/errors"
	"kaula/internal/lexer"
	"kaula/internal/parser"
	"kaula/internal/sema"
	"kaula/internal/stdlib"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	cfg, err := stdlib.LoadStdlibConfig(os.Args[2])
	if err != nil {
		panic(err)
	}
	fmt.Printf("loaded %d modules\n", len(cfg.Modules))
	lx := lexer.NewLexer(string(data))
	p := parser.NewParser(lx)
	p.EnableLogging(false)
	prog := p.Parse()
	fmt.Printf("parse errors: %d, stmts: %d\n", len(p.GetErrorCollector().Errors()), len(prog.Statements))
	ec := errors.NewErrorCollector()
	sa := sema.NewSemanticAnalyzerWithConfig(os.Args[2], ec)
	sa.SetStdlibConfig(cfg)
	sa.Analyze(prog)
	for _, e := range ec.Errors() {
		fmt.Printf("SEM: %d:%d %s\n", e.Line, e.Column, e.Message)
	}
	if len(ec.Errors()) == 0 {
		fmt.Println("NO SEMANTIC ERRORS")
	}
}
