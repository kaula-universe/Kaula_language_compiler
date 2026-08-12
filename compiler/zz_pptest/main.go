package main

import (
	"fmt"
	"os"
	"strings"

	"compiler/internal/ast"
	"compiler/internal/lexer"
	"compiler/internal/parser"
)

func dump(e ast.Expression, depth int) {
	if e == nil {
		fmt.Println(strings.Repeat("  ", depth) + "nil")
		return
	}
	switch n := e.(type) {
	case *ast.ObjectLiteral:
		fmt.Println(strings.Repeat("  ", depth) + "ObjectLiteral")
		for _, f := range n.Fields {
			fmt.Println(strings.Repeat("  ", depth+1) + f.Name + ":")
			dump(f.Value, depth+2)
		}
	case *ast.MemberAccessExpression:
		fmt.Println(strings.Repeat("  ", depth) + "MemberAccess ." + n.Member)
		dump(n.Object, depth+1)
	case *ast.BinaryExpression:
		fmt.Println(strings.Repeat("  ", depth) + "Binary " + n.Operator)
		dump(n.Left, depth+1)
		dump(n.Right, depth+1)
	default:
		fmt.Println(strings.Repeat("  ", depth) + fmt.Sprintf("%T", e))
	}
}

func main() {
	data, err := os.ReadFile(`E:\Users\tf\Desktop\新建文件夹\kaula\test\dynobj_test.kl`)
	if err != nil {
		fmt.Println("read err:", err)
		return
	}
	fmt.Println("=== full file")
	p := parser.NewParser(lexer.NewLexer(string(data)))
	prog := p.Parse()
	for _, stmt := range prog.Statements {
		fmt.Printf("stmt: %T\n", stmt)
		if es, ok := stmt.(*ast.ExpressionStatement); ok {
			dump(es.Expression, 1)
		}
	}
	if p.HasErrors() {
		fmt.Println("PARSE ERRORS")
		for _, err := range p.GetErrorCollector().Errors() {
			fmt.Println("  ", err)
		}
	}
}
