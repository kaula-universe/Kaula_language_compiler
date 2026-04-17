package compiler

import (
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/codegen"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/sema"
	"kaula-compiler/internal/stdlib"
)

// CompileResult 编译结果
type CompileResult struct {
	CCode string
	AST   *ast.Program
}

// Compile 编译 Kaula 源代码到 C 代码
func Compile(filename string, source string) (*CompileResult, error) {
	// 1. 词法分析
	lex := lexer.NewLexer(source)
	
	// 2. 语法分析
	p := parser.NewParser(lex)
	program := p.Parse()
	
	// 检查语法错误
	if p.HasErrors() {
		return nil, p.GetErrors()
	}
	
	// 3. 语义分析
	analyzer := sema.NewSemanticAnalyzer()
	config, _ := stdlib.LoadStdlibConfig("stdlib.json")
	analyzer.stdlibConfig = config
	
	analyzer.Analyze(program)
	
	// 检查语义错误
	if analyzer.HasErrors() {
		return nil, analyzer.GetErrors()
	}
	
	// 4. 代码生成
	cg := codegen.NewCodeGenerator()
	cg.stdlibConfig = config
	cCode := cg.Generate(program)
	
	return &CompileResult{
		CCode: cCode,
		AST:   program,
	}, nil
}
