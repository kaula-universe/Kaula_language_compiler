package main

import (
	"fmt"
	"kaula-compiler/internal/codegen"
	"kaula-compiler/internal/config"
	errors "kaula-compiler/internal/errors"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/sema"
	"os"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v, using default\n", err)
	}

	// 获取输入文件
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <input file>\n", os.Args[0])
		os.Exit(1)
	}

	inputFile := os.Args[len(os.Args)-1] // 最后一个参数是输入文件
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	input := string(data)

	// 创建错误收集器
	errorCollector := errors.NewErrorCollector()

	// 词法分析
	lex := lexer.NewLexer(input)
	lex.SetErrorCollector(errorCollector)

	// 语法分析
	p := parser.NewParser(lex)
	p.SetErrorCollector(errorCollector)
	p.EnableLogging(true)
	program := p.Parse()

	// 检查词法和语法错误
	if errorCollector.HasErrors() {
		fmt.Println("Errors found during lexing and parsing:")
		for _, err := range errorCollector.Errors() {
			fmt.Printf("%s error: %s (line %d, column %d)\n", errors.ErrorTypeToString(err.Type), err.Message, err.Line, err.Column)
			if err.Suggestion != "" {
				fmt.Printf("Suggestion: %s\n", err.Suggestion)
			}
		}
		os.Exit(1)
	}

	// 语义分析
	// 尝试在当前目录和kaula-compiler目录中加载stdlib.json
	stdlibPath := "stdlib.json"
	if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
		stdlibPath = "kaula-compiler/stdlib.json"
		if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
			stdlibPath = "../stdlib.json"
		}
	}
	sa := sema.NewSemanticAnalyzerWithConfig(stdlibPath, errorCollector)
	sa.Run(program)

	// 检查语义错误
	if errorCollector.HasErrors() {
		fmt.Println("Errors found during semantic analysis:")
		for _, err := range errorCollector.Errors() {
			fmt.Printf("%s error: %s (line %d, column %d)\n", errors.ErrorTypeToString(err.Type), err.Message, err.Line, err.Column)
			if err.Suggestion != "" {
				fmt.Printf("Suggestion: %s\n", err.Suggestion)
			}
		}
		os.Exit(1)
	}

	// 代码生成
	cg := codegen.NewGenerator(cfg)
	output := cg.Generate(program)

	// 代码生成器接口没有错误检查方法，暂时注释掉
	// // 检查代码生成错误
	// if cg.HasErrors() {
	// 	fmt.Println("Errors found during code generation:")
	// 	for _, err := range cg.Errors() {
	// 		fmt.Printf("Code generation error: %s\n", err)
	// 	}
	// 	os.Exit(1)
	// }

	// 输出结果
	fmt.Println("Generated code:")
	fmt.Println(output)

	// 保存到文件
	outputFile := inputFile + ".c"
	err = os.WriteFile(outputFile, []byte(output), 0644)
	if err != nil {
		fmt.Printf("Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Code generated successfully and saved to %s\n", outputFile)
}
