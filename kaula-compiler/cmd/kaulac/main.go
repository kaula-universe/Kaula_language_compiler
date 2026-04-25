package main

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/codegen"
	"kaula-compiler/internal/config"
	errors "kaula-compiler/internal/errors"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/sema"
	"kaula-compiler/internal/stdlib"
	"kaula-compiler/internal/timeout"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

func main() {
	totalStart := time.Now()

	timeout.Init()
	timeout.SetLimits(4096, 120)

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for !timeout.IsTimedOut() {
			<-ticker.C
			if err := timeout.CheckMemory("global"); err != nil {
				fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
				os.Exit(1)
			}
			runtime.GC()
		}
	}()

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v, using default\n", err)
	}

	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <input file>\n", os.Args[0])
		os.Exit(1)
	}

	inputFile := os.Args[len(os.Args)-1]
	if len(inputFile) < 4 || inputFile[len(inputFile)-3:] != ".kl" {
		fmt.Printf("Error: Input file must have .kl extension\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}
	input := string(data)

	inputDir := filepath.Dir(inputFile)
	inputBase := filepath.Base(inputFile)
	inputName := inputBase[:len(inputBase)-3]

	cwd, _ := os.Getwd()
	cacheDir := filepath.Join(cwd, "cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		os.MkdirAll(cacheDir, 0755)
	}
	cacheFile := filepath.Join(cacheDir, inputName+".c")

	fmt.Println("=== Concurrent Compilation Pipeline ===")
	fmt.Printf("Starting at %v\n\n", totalStart.Format("15:04:05.000"))

	errorCollector := errors.NewErrorCollector()

	// Stage 1: Lex + Parse (with concurrent preparation)
	fmt.Println("[Stage 1] Lexing + Parsing...")
	stage1Start := time.Now()

	lex := lexer.NewLexer(input)
	lex.SetErrorCollector(errorCollector)

	p := parser.NewParser(lex)
	p.SetErrorCollector(errorCollector)
	p.EnableLogging(false)

	program := p.Parse()
	stage1Time := time.Since(stage1Start)
	fmt.Printf("[Stage 1] Lex + Parse completed in %v\n", stage1Time)

	// 保存词法分析和语法分析的错误数量
	stage1ErrorCount := len(errorCollector.Errors())

	// Find stdlib and load config once
	stdlibPath := findStdlib()
	stdlibConfig, _ := stdlib.LoadStdlibConfig(stdlibPath)

	// Stage 2: Semantic Analysis (concurrent)
	fmt.Println("[Stage 2] Semantic Analysis...")
	stage2Start := time.Now()

	concurrentSemanticAnalysisWithConfig(program, stdlibConfig, errorCollector)
	stage2Time := time.Since(stage2Start)
	fmt.Printf("[Stage 2] Semantic Analysis completed in %v\n", stage2Time)

	// 计算语义分析阶段新增的错误数量
	stage2ErrorCount := len(errorCollector.Errors()) - stage1ErrorCount

	// Stage 3: Code Gen + C Compile (concurrent)
	fmt.Println("[Stage 3] Code Generation + C Compilation...")
	stage3Start := time.Now()

	cg := codegen.NewCodeGenerator(cfg)
	if stdlibConfig != nil {
		cg.SetStdlibConfig(stdlibConfig)
	}
	output := cg.Generate(program)

	// 检查所有阶段的错误并统一输出
	totalErrors := stage1ErrorCount + stage2ErrorCount + len(cg.Errors())
	if totalErrors > 0 {
		fmt.Println("\n=== Compilation Errors ===")
		
		// 输出词法分析和语法分析错误（阶段 1 的错误）
		if stage1ErrorCount > 0 {
			fmt.Printf("\n[Lexing & Parsing Errors] (%d errors)\n", stage1ErrorCount)
			for i := 0; i < stage1ErrorCount; i++ {
				fmt.Printf("  %d. %s\n", i+1, errorCollector.Errors()[i].String())
			}
		}
		
		// 输出语义分析错误（阶段 2 新增的错误）
		if stage2ErrorCount > 0 {
			fmt.Printf("\n[Semantic Analysis Errors] (%d errors)\n", stage2ErrorCount)
			for i := 0; i < stage2ErrorCount; i++ {
				idx := stage1ErrorCount + i
				fmt.Printf("  %d. %s\n", i+1, errorCollector.Errors()[idx].String())
			}
		}
		
		// 输出代码生成错误
		if cg.HasErrors() {
			fmt.Printf("\n[Code Generation Errors] (%d errors)\n", len(cg.Errors()))
			for i, err := range cg.Errors() {
				fmt.Printf("  %d. %s\n", i+1, err)
			}
		}
		
		fmt.Printf("\nTotal: %d error(s)\n", totalErrors)
		os.Exit(1)
	}

	// Concurrent C compilation
	usedModules := cg.GetUsedModules()
	compileResult := concurrentCompile(cacheFile, output, inputDir, inputName, cwd, usedModules)
	stage3Time := time.Since(stage3Start)
	fmt.Printf("[Stage 3] Code Gen + Compilation completed in %v\n", stage3Time)

	totalTime := time.Since(totalStart)

	fmt.Println("\n=== Generated Code ===")
	fmt.Println(output)

	fmt.Printf("\n=== Compilation Results ===\n")
	if compileResult.Error != nil {
		fmt.Printf("Status: FAILED - %v\n", compileResult.Error)
		fmt.Printf("Cache:  %s (available for manual compilation)\n", cacheFile)
	} else {
		fmt.Printf("Status: SUCCESS\n")
		fmt.Printf("Output: %s\n", compileResult.OutputFile)
		fmt.Printf("Cache:  %s\n", cacheFile)
	}

	fmt.Printf("\n=== Timing Breakdown ===\n")
	fmt.Printf("Stage 1 (Lex + Parse):         %v\n", stage1Time)
	fmt.Printf("Stage 2 (Semantic):            %v\n", stage2Time)
	fmt.Printf("Stage 3 (Codegen+Compile):    %v\n", stage3Time)
	fmt.Printf("---------------------------------\n")
	fmt.Printf("Total End-to-End:              %v\n", totalTime)

	if compileResult.Error == nil {
		fmt.Printf("\n[Success] Compiled to: %s\n", compileResult.OutputFile)
	}
}

type compileResult_t struct {
	OutputFile string
	Error      error
}

// concurrentCompile 并发保存缓存并编译 C 代码
func concurrentCompile(cacheFile, cCode, inputDir, inputName, workDir string, usedModules []string) *compileResult_t {
	result := &compileResult_t{}
	var wg sync.WaitGroup
	wg.Add(2)

	startTime := time.Now()

	// 保存缓存
	go func() {
		defer wg.Done()
		os.WriteFile(cacheFile, []byte(cCode), 0644)
	}()

	// 编译
	go func() {
		defer wg.Done()

		outputExe := filepath.Join(inputDir, inputName+".exe")
		if runtime.GOOS != "windows" {
			outputExe = filepath.Join(inputDir, inputName)
		}

		if err := compileCCode(cacheFile, outputExe, workDir, usedModules); err != nil {
			result.Error = err
			return
		}

		result.OutputFile = outputExe
	}()

	wg.Wait()

	if result.Error == nil {
		fmt.Printf("[Compile] Completed in %v\n", time.Since(startTime))
	}

	return result
}

// concurrentSemanticAnalysis 并发执行语义分析
func concurrentSemanticAnalysis(program *ast.Program, stdlibPath string, errorCollector *errors.ErrorCollector) *semaResult_t {
	result := &semaResult_t{ErrorCollector: errorCollector}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		sa := sema.NewSemanticAnalyzerWithConfig(stdlibPath, result.ErrorCollector)
		sa.Analyze(program)
	}()

	wg.Wait()
	return result
}

// concurrentSemanticAnalysisWithConfig 并发执行语义分析（使用已加载的配置）
func concurrentSemanticAnalysisWithConfig(program *ast.Program, stdlibConfig *stdlib.StdlibConfig, errorCollector *errors.ErrorCollector) *semaResult_t {
	result := &semaResult_t{ErrorCollector: errorCollector}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		sa := sema.NewSemanticAnalyzer()
		if stdlibConfig != nil {
			sa.SetStdlibConfig(stdlibConfig)
		}
		sa.Analyze(program)
	}()

	wg.Wait()
	return result
}

type semaResult_t struct {
	*errors.ErrorCollector
}

func (s *semaResult_t) HasErrors() bool {
	return s.ErrorCollector.HasErrors()
}

func findStdlib() string {
	paths := []string{"stdlib.json", "kaula-compiler/stdlib.json", "../stdlib.json"}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "stdlib.json"
}

func printErrors(ec *errors.ErrorCollector, stage string) {
	fmt.Printf("Errors found during %s:\n", stage)
	for _, err := range ec.Errors() {
		fmt.Printf("  %s error: %s (line %d, column %d)\n",
			errors.ErrorTypeToString(err.Type), err.Message, err.Line, err.Column)
		if err.Suggestion != "" {
			fmt.Printf("  Suggestion: %s\n", err.Suggestion)
		}
	}
}

func compileCCode(cFile, outputFile, workDir string, usedModules []string) error {
	clangPath, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang not found in PATH")
	}

	srcPaths := []string{
		filepath.Join(workDir, "src"),
		filepath.Join(workDir, "..", "src"),
	}
	stdPaths := []string{
		filepath.Join(workDir, "std"),
		filepath.Join(workDir, "..", "std"),
	}

	var validSrcPaths, validStdPaths []string
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

	clangArgs := []string{cFile, "-o", outputFile, "-O3", "-I", workDir}
	for _, p := range validSrcPaths {
		clangArgs = append(clangArgs, "-I", p)
	}
	for _, p := range validStdPaths {
		clangArgs = append(clangArgs, "-I", p)
	}

	runtimeFile := filepath.Join(workDir, "src", "kmm_scoped_allocator.c")
	if _, err := os.Stat(runtimeFile); err == nil {
		clangArgs = append(clangArgs, runtimeFile)
	}
	// 也检查相对路径（相对于工作目录的父目录）
	relRuntimeFile := filepath.Join(workDir, "..", "src", "kmm_scoped_allocator.c")
	if _, err := os.Stat(relRuntimeFile); err == nil {
		// 检查是否已经添加了
		alreadyAdded := false
		for _, arg := range clangArgs {
			if arg == runtimeFile {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			clangArgs = append(clangArgs, relRuntimeFile)
		}
	}

	// 只编译使用过的 std 模块源文件（跳过 memory，因为 kmm_scoped_allocator.c 已包含）
	for _, moduleName := range usedModules {
		for _, stdPath := range validStdPaths {
			// 支持多种模块名格式：
			// "io" -> stdPath/io/
			// "std/io" -> stdPath/io/ (去掉 std/ 前缀)
			// "std.io" -> stdPath/io/ (替换 . 为 /)
			moduleDirName := moduleName
			if len(moduleDirName) > 4 && moduleDirName[:4] == "std/" {
				moduleDirName = moduleDirName[4:]
			}
			if len(moduleDirName) > 4 && moduleDirName[:4] == "std." {
				moduleDirName = moduleDirName[4:]
			}
			moduleDirName = strings.ReplaceAll(moduleDirName, ".", "/")
			
			moduleDir := filepath.Join(stdPath, moduleDirName)
			if _, err := os.Stat(moduleDir); err == nil {
				entries, _ := os.ReadDir(moduleDir)
				for _, entry := range entries {
					if !entry.IsDir() && filepath.Ext(entry.Name()) == ".c" {
						clangArgs = append(clangArgs, filepath.Join(moduleDir, entry.Name()))
					}
				}
			}
		}
	}

	cmd := exec.Command(clangPath, clangArgs...)
	fmt.Printf("[Compile] Clang command args:\n")
	for _, arg := range clangArgs {
		fmt.Printf("  %s\n", arg)
	}
	fmt.Printf("[Compile] Used modules: %v\n", usedModules)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clang compilation failed: %v, output: %s", err, string(output))
	}
	fmt.Printf("[Compile] Successfully compiled: %s\n", outputFile)
	return nil
}
