package main

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
)

func main() {
	source := `interface Stringer {
    string toString();
}

class Person implements Stringer {
    name: string;
    age: int;
    
    constructor Person(string n) {
        name = n;
        age = 0;
    }
    
    string toString() {
        return name;
    }
}

fn main() {
    Person p = Person_new("Alice");
}`

	lex := lexer.NewLexer(source)
	p := parser.NewParser(lex)
	program := p.Parse()

	if p.HasErrors() {
		fmt.Println("Parser errors:")
		return
	}

	fmt.Printf("Program has %d statements\n", len(program.Statements))
	
	for i, stmt := range program.Statements {
		fmt.Printf("\nStatement %d: %T\n", i, stmt)
		
		switch s := stmt.(type) {
		case *ast.InterfaceStatement:
			fmt.Printf("  Interface: %s\n", s.Name)
			fmt.Printf("  Methods: %d\n", len(s.Methods))
			for _, m := range s.Methods {
				fmt.Printf("    Method: %s, ReturnType: %s, Params: %d\n", m.Name, m.ReturnType, len(m.Params))
			}
		case *ast.ClassStatement:
			fmt.Printf("  Class: %s\n", s.Name)
			fmt.Printf("  Implements: %v\n", s.Implements)
			fmt.Printf("  Fields: %d\n", len(s.Fields))
			for _, f := range s.Fields {
				fmt.Printf("    Field: %s: %s\n", f.Name, f.Type)
			}
			fmt.Printf("  Constructors: %d\n", len(s.Constructors))
			for _, c := range s.Constructors {
				fmt.Printf("    Constructor params: %d\n", len(c.Params))
				for _, param := range c.Params {
					fmt.Printf("      Param: %s %s\n", param.Type, param.Name)
				}
			}
			fmt.Printf("  Methods: %d\n", len(s.Methods))
			for _, m := range s.Methods {
				fmt.Printf("    Method: %s, ReturnType: %s, Params: %d\n", m.Name, m.ReturnType, len(m.Params))
			}
		}
	}
}
