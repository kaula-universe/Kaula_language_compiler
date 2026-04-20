package compiler

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/codegen"
	"kaula-compiler/internal/config"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/sema"
	"kaula-compiler/internal/stdlib"
	"path/filepath"
)

type CompileResult struct {
	CCode string
	AST   *ast.Program
	Errors []string
}

func Compile(filename string, source string) (*CompileResult, error) {
	lex := lexer.NewLexer(source)

	p := parser.NewParser(lex)
	program := p.Parse()

	if p.HasErrors() {
		return &CompileResult{
			Errors: []string{fmt.Sprintf("parser errors in %s", filename)},
		}, fmt.Errorf("parser errors")
	}

	// 确定 stdlib.json 路径
	dir, _ := filepath.Abs(filepath.Dir(filename))
	for i := 0; i < 5; i++ {
		dir = filepath.Dir(dir)
	}
	stdlibPath := filepath.Join(dir, "kaula-compiler", "stdlib.json")
	fmt.Printf("DEBUG compiler: filename=%s, stdlibPath=%s\n", filename, stdlibPath)

	// 加载 stdlib 配置
	stdlibConfig, err := stdlib.LoadStdlibConfig(stdlibPath)
	if err != nil {
		fmt.Printf("Warning: Failed to load stdlib.json from %s: %v\n", stdlibPath, err)
	} else {
		fmt.Printf("DEBUG compiler: Loaded stdlib.json, modules: %d\n", len(stdlibConfig.Modules))
	}

	analyzer := sema.NewSemanticAnalyzer()
	if stdlibConfig != nil {
		analyzer.SetStdlibConfig(stdlibConfig)
	}

	analyzer.Analyze(program)

	var errors []string
	if analyzer.ErrorCollector().HasErrors() {
		errors = append(errors, fmt.Sprintf("semantic errors in %s", filename))
	}

	cfg := config.DefaultConfig()
	cg := codegen.NewCodeGenerator(cfg)
	if stdlibConfig != nil {
		cg.SetStdlibConfig(stdlibConfig)
	}

	cCode := cg.Generate(program)

	if cg.HasErrors() {
		errors = append(errors, cg.Errors()...)
	}

	return &CompileResult{
		CCode: cCode,
		AST:   program,
		Errors: errors,
	}, nil
}
