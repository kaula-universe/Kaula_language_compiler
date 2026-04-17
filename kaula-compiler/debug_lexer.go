package main

import (
	"fmt"
	"kaula-compiler/internal/lexer"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run debug_lexer.go <file.kaula>")
		os.Exit(1)
	}

	filename := os.Args[1]
	source, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	lex := lexer.NewLexer(string(source))
	
	fmt.Printf("Token sequence for %s:\n\n", filename)
	i := 0
	for {
		tok := lex.Next()
		fmt.Printf("[%3d] %-20s %-10q (line %d, col %d)\n", 
			i, 
			lexer.TokenTypeToString(tok.Type), 
			tok.Value, 
			tok.Line, 
			tok.Column)
		i++
		if tok.Type == lexer.TOKEN_EOF {
			break
		}
	}
}
