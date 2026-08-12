package main

import (
	"fmt"
	"os"

	"compiler/internal/ast"
	"compiler/internal/lexer"
	"compiler/internal/parser"
)

func dump(stmts []ast.Statement, depth int) {
	for i, s := range stmts {
		if s == nil {
			fmt.Printf("%*s[%d] NIL STATEMENT\n", depth*2, "", i)
			continue
		}
		fmt.Printf("%*s[%d] %T\n", depth*2, "", i, s)
		switch v := s.(type) {
		case *ast.VariableDeclaration:
			fmt.Printf("%*s  Name=%q Type=%q IsAuto=%v IsConst=%v\n", depth*2, "", v.Name, v.Type, v.IsAuto, v.IsConst)
		case *ast.ExpressionStatement:
			if v.Expression == nil {
				fmt.Printf("%*s  Expression=NIL\n", depth*2, "")
			} else {
				fmt.Printf("%*s  Expression=%T\n", depth*2, "", v.Expression)
			}
		case *ast.SpendStatement:
			fmt.Printf("%*s  calls=%d\n", depth*2, "", len(v.Calls))
			for ci, c := range v.Calls {
				if c == nil {
					fmt.Printf("%*s  call[%d] = NIL\n", depth*2, "", ci)
					continue
				}
				fmt.Printf("%*s  call[%d] Index=%v IsDefault=%v\n", depth*2, "", ci, c.Index, c.IsDefault)
				dump(c.Body, depth+1)
			}
		case *ast.IfStatement:
			dump(v.Body, depth+1)
			if v.Else != nil {
				dump(v.Else, depth+1)
			}
		case *ast.WhileStatement:
			dump(v.Body, depth+1)
		case *ast.ForInStatement:
			dump(v.Body, depth+1)
		case *ast.FunctionStatement:
			dump(v.Body, depth+1)
		}
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("usage: dumper file.kl")
		return
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Println("read:", err)
		return
	}
	lx := lexer.NewLexer(string(src))
	p := parser.NewParser(lx)
	prog := p.Parse()
	fmt.Println("parse errors:", p.HasErrors())
	for i, s := range prog.Statements {
		if s == nil {
			fmt.Printf("[%d] NIL TOP STATEMENT\n", i)
			continue
		}
		fmt.Printf("[%d] %T\n", i, s)
		dump([]ast.Statement{s}, 1)
	}
}
