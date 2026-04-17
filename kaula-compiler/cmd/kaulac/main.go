package main

import (
	"fmt"
	"kaula-compiler/internal/codegen"
	"kaula-compiler/internal/config"
	errors "kaula-compiler/internal/errors"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/sema"
	"kaula-compiler/internal/timeout"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	// 初始化超时控制
	timeout.Init()
	timeout.SetLimits(4096, 120) // 4GB 内存限制，120 秒时间限制
	
	// 启动内存监控和 GC 协程
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		
		for !timeout.IsTimedOut() {
			<-ticker.C
			if err := timeout.CheckMemory("global"); err != nil {
				fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
				os.Exit(1)
			}
			
			// 强制 GC 以减少内存使用
			runtime.GC()
		}
	}()
	
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
	
	// 检查文件扩展名是否为 .kl
	if len(inputFile) < 4 || inputFile[len(inputFile)-3:] != ".kl" {
		fmt.Printf("Error: Input file must have .kl extension (got: %s)\n", inputFile)
		fmt.Println("Usage: kaulac <input.kl>")
		os.Exit(1)
	}
	
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	input := string(data)

	// 创建错误收集器
	errorCollector := errors.NewErrorCollector()

	// 词法分析
	fmt.Println("Starting lexing...")
	timeout.StartStage("lex")
	if err := timeout.CheckTimeout("lex"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	
	lex := lexer.NewLexer(input)
	lex.SetErrorCollector(errorCollector)
	
	// 检查词法分析后
	if err := timeout.CheckTimeout("lex"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	timeout.EndStage("lex")

	// 语法分析
	fmt.Println("Starting parsing...")
	timeout.StartStage("parse")
	if err := timeout.CheckTimeout("parse"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	
	p := parser.NewParser(lex)
	p.SetErrorCollector(errorCollector)
	p.EnableLogging(false) // 禁用日志以提高性能
	program := p.Parse()
	
	// 检查语法分析后
	if err := timeout.CheckTimeout("parse"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	timeout.EndStage("parse")

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
	fmt.Println("Starting semantic analysis...")
	timeout.StartStage("sema")
	if err := timeout.CheckTimeout("sema"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	
	// 尝试在当前目录和 kaula-compiler 目录中加载 stdlib.json
	stdlibPath := "stdlib.json"
	if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
		stdlibPath = "kaula-compiler/stdlib.json"
		if _, err := os.Stat(stdlibPath); os.IsNotExist(err) {
			stdlibPath = "../stdlib.json"
		}
	}
	sa := sema.NewSemanticAnalyzerWithConfig(stdlibPath, errorCollector)
	sa.Analyze(program)
	
	// 检查语义分析后
	if err := timeout.CheckTimeout("sema"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	timeout.EndStage("sema")

	fmt.Println("Semantic analysis completed, starting code generation...")

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
	fmt.Println("Starting code generation...")
	timeout.StartStage("codegen")
	if err := timeout.CheckTimeout("codegen"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	
	cg := codegen.NewGenerator(cfg)
	output := cg.Generate(program)
	
	// 检查代码生成后
	if err := timeout.CheckTimeout("codegen"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	timeout.EndStage("codegen")

	// 检查代码生成错误
	if cg.HasErrors() {
		fmt.Println("Errors found during code generation:")
		for _, err := range cg.Errors() {
			fmt.Printf("Code generation error: %s\n", err)
		}
		os.Exit(1)
	}

	// 输出结果
	fmt.Println("Generated code:")
	fmt.Println(output)

	// 获取输入文件的目录和文件名
	inputDir := filepath.Dir(inputFile)
	inputBase := filepath.Base(inputFile)
	inputName := inputBase[:len(inputBase)-3] // 去掉 .kl 后缀
	
	// 在父目录创建 cache 目录
	cwd, _ := os.Getwd()
	cacheDir := filepath.Join(cwd, "cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			fmt.Printf("Error creating cache directory: %v\n", err)
			os.Exit(1)
		}
	}
	
	// 保存到 cache 目录
	cacheFile := filepath.Join(cacheDir, inputName+".c")
	err = os.WriteFile(cacheFile, []byte(output), 0644)
	if err != nil {
		fmt.Printf("Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Code generated successfully and saved to %s\n", cacheFile)
	fmt.Printf("Total time: %v, Memory: %dMB\n", 
		timeout.GetElapsed(), 
		getMemoryUsage())
	
	// 打印各阶段详细统计
	printStageStats()
	
	// 自动编译生成的 C 代码
	fmt.Println("\n=== Compiling C code ===")
	outputExe := filepath.Join(inputDir, inputName+".exe")
	if runtime.GOOS != "windows" {
		outputExe = filepath.Join(inputDir, inputName)
	}
	if err := compileCCode(cacheFile, outputExe, inputDir); err != nil {
		fmt.Printf("Warning: C compilation failed: %v\n", err)
		fmt.Println("You can compile manually with: clang <file>.c -o <file>.exe")
	}
}

func getMemoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024
}

func printStageStats() {
	fmt.Println("\n=== Compilation Stage Statistics ===")
	fmt.Println("Stage           | Duration  | Memory Delta     | Peak    | Allocs")
	fmt.Println("----------------|-----------|------------------|---------|--------")
	
	// 这里需要 timeout 包提供获取阶段统计的接口
	// 暂时简化处理
	fmt.Printf("Total compilation completed in %v\n", timeout.GetElapsed())
	fmt.Println("================================")
}

func compileCCode(cFile string, outputFile string, workDir string) error {
	// 尝试查找 clang 编译器
	clangPath, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang not found in PATH")
	}
	
	// 获取项目根目录（当前工作目录）
	cwd, _ := os.Getwd()
	
	// 尝试多个路径：当前目录、父目录
	srcPaths := []string{
		filepath.Join(cwd, "src"),
		filepath.Join(cwd, "..", "src"),
	}
	stdPaths := []string{
		filepath.Join(cwd, "std"),
		filepath.Join(cwd, "..", "std"),
	}
	
	// 检查哪个路径存在
	var validSrcPaths []string
	var validStdPaths []string
	for _, p := range srcPaths {
		if _, err := os.Stat(p); err == nil {
			validSrcPaths = append(validSrcPaths, p)
		}
	}
	for _, p := range stdPaths {
		if _, err := os.Stat(p); err == nil {
			validStdPaths = append(validStdPaths, p)
		}
	}
	
	// 构建 clang 参数，使用 -O3 优化级别
	clangArgs := []string{cFile, "-o", outputFile, "-O3"}
	
	// 添加项目根目录到包含路径，使 "src/kaula.h" 能正确找到
	clangArgs = append(clangArgs, "-I", cwd)
	
	for _, p := range validSrcPaths {
		clangArgs = append(clangArgs, "-I", p)
	}
	for _, p := range validStdPaths {
		clangArgs = append(clangArgs, "-I", p)
	}
	
	// 检查是否存在运行时文件
	runtimeFile := filepath.Join(cwd, "src", "kmm_scoped_allocator.c")
	if _, err := os.Stat(runtimeFile); err == nil {
		// 存在运行时文件，一起编译
		clangArgs = append(clangArgs, runtimeFile)
	}
	
	cmd := exec.Command(clangPath, clangArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clang compilation failed: %v, output: %s", err, string(output))
	}
	fmt.Printf("C code compiled successfully: %s\n", outputFile)
	return nil
}
