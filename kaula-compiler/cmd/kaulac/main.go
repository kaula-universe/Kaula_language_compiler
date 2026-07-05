package main

import (
	"encoding/json"
	"fmt"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/cache"
	"kaula-compiler/internal/codegen"
	"kaula-compiler/internal/config"
	errors "kaula-compiler/internal/errors"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/sema"
	"kaula-compiler/internal/sor"
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

// precompileLocalImports 预解析本地 .kl 文件 import
// 返回: (pub 函数名集合, 合并后的 C 函数定义代码)
func precompileLocalImports(program *ast.Program, inputDir string, stdlibConfig *stdlib.StdlibConfig, cfg *config.Config, errorCollector *errors.ErrorCollector) (map[string]bool, string) {
	pubFuncs := make(map[string]bool)
	var allCode string
	compiled := make(map[string]bool)

	var localFiles []string
	for _, stmt := range program.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok && importStmt.IsLocal {
			localFiles = append(localFiles, importStmt.LocalPath)
		}
	}
	if len(localFiles) == 0 {
		return pubFuncs, ""
	}

	for _, localPath := range localFiles {
		if compiled[localPath] {
			continue
		}
		compiled[localPath] = true

		absPath := localPath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(inputDir, absPath)
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			fmt.Printf("[Multi-file] Warning: Failed to read %s: %v\n", localPath, err)
			continue
		}

		localSource := string(data)
		localLex := lexer.NewLexer(localSource)
		localLex.SetErrorCollector(errorCollector)
		localParser := parser.NewParser(localLex)
		localParser.SetErrorCollector(errorCollector)
		localParser.EnableLogging(false)
		localParser.SetSkipMainCheck(true)
		localProgram := localParser.Parse()

		if localParser.HasErrors() {
			fmt.Printf("[Multi-file] Parse errors in %s\n", localPath)
			continue
		}

		// 收集 pub 函数名
		for _, stmt := range localProgram.Statements {
			if fnStmt, ok := stmt.(*ast.FunctionStatement); ok && fnStmt.IsPublic {
				pubFuncs[fnStmt.Name] = true
			}
		}

		// 语义分析（跳过 main 检查）
		localAnalyzer := sema.NewSemanticAnalyzer()
		if stdlibConfig != nil {
			localAnalyzer.SetStdlibConfig(stdlibConfig)
		}
		localAnalyzer.SetSOREnabled(cfg.SOR)
		localAnalyzer.Analyze(localProgram)

		// 代码生成
		localCG := codegen.NewCodeGenerator(cfg)
		if stdlibConfig != nil {
			localCG.SetStdlibConfig(stdlibConfig)
		}
		localOutput := localCG.Generate(localProgram)

		// 提取函数定义（去掉 includes 和 main）
		funcCode := extractFunctionDefs(localOutput)
		if funcCode != "" {
			fmt.Printf("[Multi-file] Compiled local import: %s\n", localPath)
			allCode += funcCode + "\n"
		}

		// 递归处理嵌套本地 import
		nestedFuncs, nestedCode := precompileLocalImports(localProgram, filepath.Dir(absPath), stdlibConfig, cfg, errorCollector)
		if len(nestedFuncs) > 0 {
			for k, v := range nestedFuncs {
				pubFuncs[k] = v
			}
		}
		if nestedCode != "" {
			allCode += nestedCode
		}
	}

	return pubFuncs, allCode
}

// extractFunctionDefs 从 C 代码中提取函数定义（去掉 #include 和 main 函数）
func extractFunctionDefs(cCode string) string {
	lines := strings.Split(cCode, "\n")
	var result []string
	braceDepth := 0      // 当前大括号深度（追踪函数体）
	inFunction := false  // 是否在函数体内
	skipMain := false    // 是否在跳过 main 函数

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 跳过 #include 行
		if strings.HasPrefix(trimmed, "#include") {
			continue
		}

		// 如果正在跳过 main 函数体
		if skipMain {
			for _, c := range line {
				if c == '{' {
					braceDepth++
				} else if c == '}' {
					braceDepth--
				}
			}
			if braceDepth <= 0 {
				skipMain = false
				inFunction = false
				braceDepth = 0
			}
			continue
		}

		// 检测 main 函数开始（不在函数体内时）
		if !inFunction && strings.Contains(trimmed, "int main(") {
			skipMain = true
			inFunction = true
			braceDepth = 0
			for _, c := range trimmed {
				if c == '{' {
					braceDepth++
				} else if c == '}' {
					braceDepth--
				}
			}
			if braceDepth <= 0 && strings.Contains(trimmed, "{") {
				// 单行 main 函数（罕见）
				skipMain = false
				inFunction = false
				braceDepth = 0
			}
			continue
		}

		// 如果在函数体内（非 main），保留行并更新大括号深度
		if inFunction {
			result = append(result, line)
			for _, c := range line {
				if c == '{' {
					braceDepth++
				} else if c == '}' {
					braceDepth--
				}
			}
			if braceDepth <= 0 {
				inFunction = false
				braceDepth = 0
			}
			continue
		}

		// 不在函数体内：检测函数定义开始（包含 { 的非注释行）
		if strings.Contains(trimmed, "{") && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
			inFunction = true
			braceDepth = 0
			result = append(result, line)
			for _, c := range trimmed {
				if c == '{' {
					braceDepth++
				} else if c == '}' {
					braceDepth--
				}
			}
			if braceDepth <= 0 {
				inFunction = false
				braceDepth = 0
			}
			continue
		}

		// 其他行（前向声明、空行等）保留
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// injectLocalCode 将本地导入的函数定义注入到主 C 代码中
// 在 main 函数之前插入
func injectLocalCode(mainCode, localCode string) string {
	// 找到 "int main" 的位置
	mainIdx := strings.Index(mainCode, "int main(")
	if mainIdx == -1 {
		// 没有 main 函数，直接追加
		return mainCode + "\n" + localCode
	}

	// 在 main 函数之前插入本地代码
	before := mainCode[:mainIdx]
	after := mainCode[mainIdx:]
	return before + localCode + "\n" + after
}

func main() {
	totalStart := time.Now()

	timeout.Init()
	timeout.SetLimits(4096, 120)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for !timeout.IsTimedOut() {
			<-ticker.C
			if err := timeout.CheckMemory("global"); err != nil {
				fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
				os.Exit(1)
			}
		}
	}()

	// 在加载配置之前先解析我们自己的参数（避免 flag.Parse() 冲突）
	inputFile := ""
	cleanCache := false
	purgeCache := false
	showCacheStats := false
	noCache := false
	sorMode := false
	releaseMode := false
	optLevelOverride := "" // 用户手动指定的优化级别（O0/O1/O2/O3）
	analyzePkg := ""
	analyzePkgAll := false

	// 先处理我们的参数，过滤掉后再传递给 flag.Parse()
	customArgs := []string{}
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--clean-cache":
			cleanCache = true
		case arg == "--purge-cache":
			purgeCache = true
		case arg == "--cache-stats":
			showCacheStats = true
		case arg == "--no-cache":
			noCache = true
		case arg == "--sor":
			sorMode = true
		case arg == "--release":
			releaseMode = true
		case arg == "--opt":
			// 下一个参数是优化级别
			if i+1 < len(args) {
				optLevelOverride = args[i+1]
				i++ // 跳过级别参数
			} else {
				fmt.Printf("Error: --opt requires an optimization level (O0/O1/O2/O3)\n")
				os.Exit(1)
			}
		case arg == "--analyze-pkg-all":
			analyzePkgAll = true
		case arg == "--analyze-pkg":
			// 下一个参数是包名
			if i+1 < len(args) {
				analyzePkg = args[i+1]
				i++ // 跳过包名参数
			} else {
				fmt.Printf("Error: --analyze-pkg requires a package name\n")
				os.Exit(1)
			}
		default:
			// 非 flag 参数保留
			if len(arg) > 0 && arg[0] != '-' {
				inputFile = arg
			}
			customArgs = append(customArgs, arg)
		}
	}

	// 处理 --analyze-pkg 命令
	if analyzePkg != "" {
		handleAnalyzePkg(analyzePkg)
		return
	}

	// 处理 --analyze-pkg-all 命令
	if analyzePkgAll {
		handleAnalyzePkgAll()
		return
	}

	// 处理命令行参数（允许仅使用缓存管理命令而不需要输入文件）
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s [options] <input file>\n", os.Args[0])
		fmt.Printf("Options:\n")
		fmt.Printf("  --clean-cache       Clean cache directory\n")
		fmt.Printf("  --purge-cache       Purge all cache entries\n")
		fmt.Printf("  --cache-stats       Show cache statistics\n")
		fmt.Printf("  --no-cache          Disable incremental compilation\n")
		fmt.Printf("  --sor               启用 SOR (Sub-structural Ownership) 编译时所有权分析（默认 -O3）\n")
		fmt.Printf("  --release           使用 -O3 优化（普通模式默认 -O2，SOR/Release 默认 -O3）\n")
		fmt.Printf("  --opt <level>       手动指定优化级别（O0/O1/O2/O3），覆盖所有默认值\n")
		fmt.Printf("  --analyze-pkg <name>   手动分析指定包并生成/覆盖配置文件\n")
		fmt.Printf("  --analyze-pkg-all      手动分析所有 pkglib 中的包并生成/覆盖配置文件\n")
		os.Exit(1)
	}

	// 临时修改 os.Args 以避免 flag.Parse() 报错
	os.Args = append([]string{os.Args[0]}, customArgs...)

	// 加载配置（会调用 flag.Parse()）
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v, using default\n", err)
	}

	// 从自定义参数中同步 SOR 模式（因为 --sor 被提前消费了）
	cfg.SOR = sorMode

	// 如果没有输入文件但有缓存管理命令，也允许执行
	if inputFile == "" && !cleanCache && !purgeCache && !showCacheStats {
		fmt.Printf("Error: No input file specified\n")
		os.Exit(1)
	}

	if inputFile != "" && (len(inputFile) < 4 || inputFile[len(inputFile)-3:] != ".kl") {
		fmt.Printf("Error: Input file must have .kl extension\n")
		os.Exit(1)
	}

	// 初始化缓存管理器
	cwd, _ := os.Getwd()
	cacheDir := filepath.Join(cwd, "cache")
	cacheManager, err := cache.NewCacheManager(cacheDir, "0.1.0-alpha")
	if err != nil {
		fmt.Printf("Warning: Failed to initialize cache manager: %v\n", err)
	}

	// 处理缓存管理命令
	if cleanCache && cacheManager != nil {
		if err := cacheManager.Clean(7*24*time.Hour, 1024*1024*1024); err != nil {
			fmt.Printf("Error cleaning cache: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Cache cleaned successfully")
	}

	if purgeCache && cacheManager != nil {
		if err := cacheManager.Purge(); err != nil {
			fmt.Printf("Error purging cache: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Cache purged successfully")
	}

	if showCacheStats && cacheManager != nil {
		totalEntries, totalSize, oldest, newest := cacheManager.GetStats()
		fmt.Println("=== Cache Statistics ===")
		fmt.Printf("Total entries: %d\n", totalEntries)
		fmt.Printf("Total size: %d bytes (%.2f MB)\n", totalSize, float64(totalSize)/1024/1024)
		if !oldest.IsZero() {
			fmt.Printf("Oldest entry: %v\n", oldest.Format("2006-01-02 15:04:05"))
		}
		if !newest.IsZero() {
			fmt.Printf("Newest entry: %v\n", newest.Format("2006-01-02 15:04:05"))
		}
		if totalEntries == 0 && !cleanCache && !purgeCache && inputFile == "" {
			os.Exit(0)
		}
	}

	// 如果没有输入文件，退出
	if inputFile == "" {
		os.Exit(0)
	}

	// 读取源文件
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}
	input := string(data)

	inputDir := filepath.Dir(inputFile)
	inputBase := filepath.Base(inputFile)
	inputName := inputBase[:len(inputBase)-3]

	// 优化级别优先级：
	//   1. --opt 手动指定（最高优先级）
	//   2. --sor SOR 模式默认 -O3
	//   3. --release 模式 -O3
	//   4. 普通模式默认 -O2
	optLevel := "-O2"
	if sorMode {
		optLevel = "-O3"
	}
	if releaseMode {
		optLevel = "-O3"
	}
	if optLevelOverride != "" {
		// 验证优化级别合法性
		validOpts := map[string]bool{"O0": true, "O1": true, "O2": true, "O3": true}
		if !validOpts[optLevelOverride] {
			fmt.Printf("Error: Invalid optimization level '%s'. Must be O0, O1, O2, or O3\n", optLevelOverride)
			os.Exit(1)
		}
		optLevel = "-" + optLevelOverride
	}

	fmt.Println("=== Concurrent Compilation Pipeline ===")
	fmt.Printf("Starting at %v\n\n", totalStart.Format("15:04:05.000"))

	errorCollector := errors.NewErrorCollector()

	// 并行启动：stdlib 配置加载 + 路径搜索（不依赖 Go 前端）
	parallelStart := time.Now()
	var stdlibConfig *stdlib.StdlibConfig
	var stdlibPath string
	var parallelWg sync.WaitGroup
	parallelWg.Add(1)
	go func() {
		defer parallelWg.Done()
		stdlibPath = findStdlib()
		stdlibConfig, _ = stdlib.LoadStdlibConfig(stdlibPath)
	}()

	// Stage 1: Lex + Parse（与 stdlib 加载并行）
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

	// 等待 stdlib 配置加载完成
	parallelWg.Wait()
	parallelTime := time.Since(parallelStart)
	fmt.Printf("[Parallel] stdlib config loaded in %v\n", parallelTime)

	// Stage 2: Semantic Analysis (concurrent)
	fmt.Println("[Stage 2] Semantic Analysis...")
	stage2Start := time.Now()

	concurrentSemanticAnalysisWithConfig(program, stdlibConfig, errorCollector, cfg.SOR)
	stage2Time := time.Since(stage2Start)
	fmt.Printf("[Stage 2] Semantic Analysis completed in %v\n", stage2Time)

	// 计算语义分析阶段新增的错误数量
	stage2ErrorCount := len(errorCollector.Errors()) - stage1ErrorCount

	// Stage 2.5: SOR Ownership Analysis (--sor)
	var sorErrors []sor.SORError
	var sorResult map[string]interface{}
	poolCapacity := 0 // KMM V4 池容量（0=使用默认值）
	if cfg.SOR {
		fmt.Println("[Stage 2.5] SOR Ownership + Memory Analysis...")
		sorStart := time.Now()
		// 运行完整分析流水线：安全检查 + 内存决策 + 逃逸 + 大小估算 + 活跃性 + 跨函数
		fullResult := sor.AnalyzeFullFromAST(program)
		sorErrors = fullResult.SORErrors
		// 序列化结果供 CodeGen 使用
		sorResult = sor.SerializeFullAnalysisResult(fullResult)
		// 提取静态分析估算的池容量
		poolCapacity = fullResult.PoolCapacity
		sorTime := time.Since(sorStart)
		fmt.Printf("[Stage 2.5] SOR Analysis completed in %v\n", sorTime)
		if poolCapacity > 0 {
			fmt.Printf("         Pool Capacity: %d bytes (%.2f MB)\n", poolCapacity, float64(poolCapacity)/(1024.0*1024.0))
		}

		if len(sorErrors) > 0 {
			fmt.Printf("\n[SOR Ownership Errors] (%d errors)\n", len(sorErrors))
			for i, err := range sorErrors {
				fmt.Printf("  %d. [%s] line %d: %s\n", i+1, err.Kind.String(), err.SourceLine, err.Message)
				if err.Details != "" {
					fmt.Printf("      %s\n", err.Details)
				}
			}
		}
	} else {
		// 非 SOR 模式：基于 AST 扫描估算池容量
		poolCapacity = sor.EstimatePoolCapacityFromAST(program)
		if poolCapacity > 0 {
			fmt.Printf("[Stage 2.5] Pool Capacity Estimate (AST-only): %d bytes (%.2f MB)\n",
				poolCapacity, float64(poolCapacity)/(1024.0*1024.0))
		}
	}

	// Stage 3: Code Gen + C Compile (concurrent)
	fmt.Println("[Stage 3] Code Generation + C Compilation...")
	stage3Start := time.Now()

	codegenStart := time.Now()
	cg := codegen.NewCodeGenerator(cfg)
	if stdlibConfig != nil {
		cg.SetStdlibConfig(stdlibConfig)
	}
	if sorResult != nil {
		cg.SetSORResult(sorResult)
	}

	// 多文件编译：预解析本地 .kl 文件 import，收集 pub 函数名
	localImportFuncs, localImportCode := precompileLocalImports(program, inputDir, stdlibConfig, cfg, errorCollector)
	if len(localImportFuncs) > 0 {
		cg.SetLocalImportFuncs(localImportFuncs)
	}

	output := cg.Generate(program)
	usedModules := cg.GetUsedModules()

	// 将本地导入的函数定义注入到主输出中（在 main 函数之前）
	if localImportCode != "" {
		output = injectLocalCode(output, localImportCode)
	}

	codegenTime := time.Since(codegenStart)
	fmt.Printf("[Stage 3a] Code generation completed in %v\n", codegenTime)

	// 检查所有阶段的错误并统一输出
	totalErrors := stage1ErrorCount + stage2ErrorCount + len(cg.Errors()) + len(sorErrors)
	if totalErrors > 0 {
		fmt.Println("\n=== Compilation Errors ===")

		// 输出词法分析和语法分析错误（阶段 1 的错误）
		if stage1ErrorCount > 0 {
			fmt.Printf("\n[Lexing & Parsing Errors] (%d errors)\n", stage1ErrorCount)
			for i := 0; i < stage1ErrorCount; i++ {
				err := errorCollector.Errors()[i]
				fmt.Println(errors.FormatErrorWithContext(err))
			}
		}

		// 输出语义分析错误（阶段 2 新增的错误）
		if stage2ErrorCount > 0 {
			fmt.Printf("\n[Semantic Analysis Errors] (%d errors)\n", stage2ErrorCount)
			for i := 0; i < stage2ErrorCount; i++ {
				idx := stage1ErrorCount + i
				err := errorCollector.Errors()[idx]
				fmt.Println(errors.FormatErrorWithContext(err))
			}
		}

		// 输出代码生成错误
		if cg.HasErrors() {
			fmt.Printf("\n[Code Generation Errors] (%d errors)\n", len(cg.Errors()))
			for i, err := range cg.Errors() {
				fmt.Printf("  %d. %s\n", i+1, err)
			}
		}

		// 输出 SOR 错误
		if len(sorErrors) > 0 {
			fmt.Printf("\n[SOR Ownership Errors] (%d errors)\n", len(sorErrors))
			for i, err := range sorErrors {
				fmt.Printf("  %d. [%s] line %d: %s\n", i+1, err.Kind.String(), err.SourceLine, err.Message)
				if err.Details != "" {
					fmt.Printf("      %s\n", err.Details)
				}
			}
		}

		fmt.Printf("\nTotal: %d error(s)\n", totalErrors)
		os.Exit(1)
	}

	// 增量编译：检查缓存
	var cacheFile string
	var cacheHit bool
	
	if cacheManager != nil && !noCache {
		cacheKey := cacheManager.GetCacheKey(inputFile)
		cacheFile = filepath.Join(cacheDir, cacheKey+".c")
		
		// 检查缓存是否命中
		cacheResult := cacheManager.Check(inputFile, data)
		if cacheResult.Hit {
			cacheHit = true
			fmt.Printf("[Cache] Using cached C code: %s\n", cacheResult.CCodePath)
		} else {
			// 缓存未命中，存储新生成的代码
			if err := cacheManager.Store(inputFile, data, output, usedModules); err != nil {
				fmt.Printf("[Cache] Warning: Failed to store cache: %v\n", err)
			}
			cacheHit = false
			cacheFile = cacheResult.CCodePath
		}
	} else {
		// 无缓存模式，直接使用原来的路径
		cacheFile = filepath.Join(cacheDir, inputName+".c")
		cacheHit = false
		
		// 保存 C 代码到缓存文件
		if err := os.WriteFile(cacheFile, []byte(output), 0644); err != nil {
			fmt.Printf("Warning: Failed to save C code: %v\n", err)
		}
	}

	// Concurrent C compilation
	compileResult := concurrentCompile(cacheFile, output, inputDir, inputName, cwd, usedModules, cacheHit, stdlibConfig, optLevel, poolCapacity)
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
func concurrentCompile(cacheFile, cCode, inputDir, inputName, workDir string, usedModules []string, cacheHit bool, stdlibConfig *stdlib.StdlibConfig, optLevel string, poolCapacity int) *compileResult_t {
	result := &compileResult_t{}
	var wg sync.WaitGroup
	wg.Add(2)

	startTime := time.Now()

	// 如果是缓存命中，不需要保存 C 代码
	if !cacheHit {
		// 保存缓存
		go func() {
			defer wg.Done()
			os.WriteFile(cacheFile, []byte(cCode), 0644)
		}()
	} else {
		// 缓存命中，直接完成
		go func() {
			defer wg.Done()
		}()
	}

	// 编译
	go func() {
		defer wg.Done()

		outputExe := filepath.Join(inputDir, inputName+".exe")
		if runtime.GOOS != "windows" {
			outputExe = filepath.Join(inputDir, inputName)
		}

		if err := compileCCode(cacheFile, outputExe, workDir, usedModules, cCode, stdlibConfig, optLevel, poolCapacity); err != nil {
			result.Error = err
			return
		}

		result.OutputFile = outputExe
	}()

	wg.Wait()

	if result.Error == nil {
		if cacheHit {
			fmt.Printf("[Compile] Completed in %v (cache hit)\n", time.Since(startTime))
		} else {
			fmt.Printf("[Compile] Completed in %v\n", time.Since(startTime))
		}
	}

	return result
}

// concurrentSemanticAnalysis 并发执行语义分析
func concurrentSemanticAnalysis(program *ast.Program, stdlibPath string, errorCollector *errors.ErrorCollector, sorEnabled bool) *semaResult_t {
	result := &semaResult_t{ErrorCollector: errorCollector}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		sa := sema.NewSemanticAnalyzerWithConfig(stdlibPath, result.ErrorCollector)
		sa.SetSOREnabled(sorEnabled)
		sa.Analyze(program)
	}()

	wg.Wait()
	return result
}

// concurrentSemanticAnalysisWithConfig 并发执行语义分析（使用已加载的配置）
func concurrentSemanticAnalysisWithConfig(program *ast.Program, stdlibConfig *stdlib.StdlibConfig, errorCollector *errors.ErrorCollector, sorEnabled bool) *semaResult_t {
	result := &semaResult_t{ErrorCollector: errorCollector}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		sa := sema.NewSemanticAnalyzerWithConfig("kaula-compiler/stdlib.json", errorCollector)
		if stdlibConfig != nil {
			sa.SetStdlibConfig(stdlibConfig)
		}
		sa.SetSOREnabled(sorEnabled)
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
	// 首先尝试从可执行文件所在目录查找 stdlib.json
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		stdlibPath := filepath.Join(exeDir, "stdlib.json")
		if _, err := os.Stat(stdlibPath); err == nil {
			return stdlibPath
		}
	}
	
	// 尝试多个路径
	paths := []string{"stdlib.json", "kaula-compiler/stdlib.json", "../stdlib.json", "../../kaula-compiler/stdlib.json"}
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

// resolveModuleDependencies 自动解析模块传递依赖
// 读取 std/dependencies.json，递归展开所有依赖模块，返回去重后的完整模块列表
func resolveModuleDependencies(usedModules []string, validStdPaths []string) []string {
	// 查找 dependencies.json
	var depsPath string
	for _, stdPath := range validStdPaths {
		candidate := filepath.Join(stdPath, "dependencies.json")
		if _, err := os.Stat(candidate); err == nil {
			depsPath = candidate
			break
		}
	}
	if depsPath == "" {
		return usedModules // 无依赖声明文件，原样返回
	}

	// 读取依赖声明
	data, err := os.ReadFile(depsPath)
	if err != nil {
		return usedModules
	}

	var depsMap map[string][]string
	if err := json.Unmarshal(data, &depsMap); err != nil {
		return usedModules
	}

	// BFS 递归展开所有依赖
	result := make(map[string]bool)
	queue := make([]string, len(usedModules))
	copy(queue, usedModules)

	for len(queue) > 0 {
		mod := queue[0]
		queue = queue[1:]

		// 标准化模块名（去掉 std/ 或 std. 前缀）
		normalizedName := mod
		if len(normalizedName) > 4 && normalizedName[:4] == "std/" {
			normalizedName = normalizedName[4:]
		}
		if len(normalizedName) > 4 && normalizedName[:4] == "std." {
			normalizedName = normalizedName[4:]
		}
		normalizedName = strings.ReplaceAll(normalizedName, ".", "/")
		// 只取最后一段（如 "std.io" -> "io"）
		parts := strings.Split(normalizedName, "/")
		if len(parts) > 0 {
			normalizedName = parts[len(parts)-1]
		}

		if result[normalizedName] {
			continue // 已处理
		}
		result[normalizedName] = true

		// 查找该模块的依赖
		if depList, ok := depsMap[normalizedName]; ok {
			for _, dep := range depList {
				if !result[dep] {
					queue = append(queue, dep)
				}
			}
		}
	}

	// 构建有序结果列表
	var resolved []string
	for mod := range result {
		resolved = append(resolved, mod)
	}
	return resolved
}

func compileCCode(cFile, outputFile, workDir string, usedModules []string, cCodeInMemory string, stdlibConfig *stdlib.StdlibConfig, optLevel string, poolCapacity int) error {
	clangPath, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang not found in PATH")
	}

	// 查找 kaula.h 所在的目录
	kaulaSrcPaths := []string{
		filepath.Join(workDir, "..", "..", "..", "kaula", "src"),  // glm_cli/src -> 新建文件夹/kaula/src
		filepath.Join(workDir, "..", "..", "src"),                  // 上两级的 src
		filepath.Join(workDir, "..", "src"),                        // 上一级的 src
		filepath.Join(workDir, "src"),                              // 当前目录下的 src
	}
	
	var kaulaSrcPath string
	for _, p := range kaulaSrcPaths {
		if _, err := os.Stat(filepath.Join(p, "kaula.h")); err == nil {
			kaulaSrcPath = p
			break
		}
	}

	srcPaths := []string{
		filepath.Join(workDir, "src"),
	}
	if kaulaSrcPath != "" {
		srcPaths = append(srcPaths, kaulaSrcPath)
	}
	srcPaths = append(srcPaths, filepath.Join(workDir, "..", "src"))
	
	stdPaths := []string{
		filepath.Join(workDir, "..", "..", "..", "kaula", "std"),  // glm_cli/src -> 新建文件夹/kaula/std
		filepath.Join(workDir, "..", "..", "std"),                  // 上两级的 std
		filepath.Join(workDir, "..", "std"),                        // 上一级的 std
		filepath.Join(workDir, "std"),                              // 当前目录下的 std
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

	// 预编译对象缓存目录（PCH + std .o 共用）
	objectCacheDir := filepath.Join(workDir, "cache", "std-objects")
	os.MkdirAll(objectCacheDir, 0755)

	// 预编译头文件 (PCH)：将 kaula.h 预编译为 .gch，加速 clang 头文件解析
	// .gch 文件有缓存机制：仅在 kaula.h 变化时重新生成
	pchPath := filepath.Join(objectCacheDir, "kaula.h.gch")
	pchNeedsRebuild := true
	if kaulaSrcPath != "" {
		hInfo, hErr := os.Stat(filepath.Join(kaulaSrcPath, "kaula.h"))
		gchInfo, gchErr := os.Stat(pchPath)
		if hErr == nil && gchErr == nil {
			if gchInfo.ModTime().After(hInfo.ModTime()) || gchInfo.ModTime().Equal(hInfo.ModTime()) {
				pchNeedsRebuild = false
			}
		}
	}
	if pchNeedsRebuild && kaulaSrcPath != "" {
		pchCmd := exec.Command(clangPath, "-x", "c-header", "-c", filepath.Join(kaulaSrcPath, "kaula.h"), "-o", pchPath, optLevel)
		for _, p := range validSrcPaths {
			pchCmd.Args = append(pchCmd.Args, "-I", p)
		}
		for _, p := range validStdPaths {
			pchCmd.Args = append(pchCmd.Args, "-I", p)
		}
		pchCmd.Args = append(pchCmd.Args, "-DKMM_THREAD_SAFETY_LEVEL=1")
		if poolCapacity > 0 {
			pchCmd.Args = append(pchCmd.Args, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
		}
		if err := pchCmd.Run(); err == nil {
			fmt.Printf("[Compile] PCH generated: %s\n", pchPath)
		}
	} else if kaulaSrcPath != "" {
		fmt.Printf("[Compile] PCH cache hit: %s\n", pchPath)
	}

	clangArgs := []string{"-x", "c", "-", "-o", outputFile, optLevel, "-I", workDir}
	clangArgs = append(clangArgs, "-DKMM_THREAD_SAFETY_LEVEL=1")
	if poolCapacity > 0 {
		clangArgs = append(clangArgs, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
	}
	for _, p := range validSrcPaths {
		clangArgs = append(clangArgs, "-I", p)
	}
	for _, p := range validStdPaths {
		clangArgs = append(clangArgs, "-I", p)
	}
	// 启用 PCH：让 clang 自动查找 kaula.h.gch 并使用
	// 暂时禁用 PCH 以调试崩溃问题
	// if kaulaSrcPath != "" {
	// 	clangArgs = append(clangArgs, "-I", objectCacheDir)
	// }

	// 自动解析模块传递依赖（读取 dependencies.json 递归展开）
	usedModules = resolveModuleDependencies(usedModules, validStdPaths)

	// 预编译 std 模块为 .o 对象文件（增量编译缓存）
	// objectCacheDir 已在 PCH 阶段提前创建

	// 收集所有需要编译的 std .c 文件
	type moduleSource struct {
		cPath    string
		objPath  string
		needsRebuild bool
	}
	var moduleSources []moduleSource

	for _, moduleName := range usedModules {
		for _, stdPath := range validStdPaths {
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
						cFullPath := filepath.Join(moduleDir, entry.Name())
						objName := moduleDirName + "_" + strings.TrimSuffix(entry.Name(), ".c") + ".o"
						objFullPath := filepath.Join(objectCacheDir, objName)

						needsRebuild := true
						cInfo, cErr := os.Stat(cFullPath)
						oInfo, oErr := os.Stat(objFullPath)
						if oErr == nil && cErr == nil {
							if oInfo.ModTime().After(cInfo.ModTime()) || oInfo.ModTime().Equal(cInfo.ModTime()) {
								needsRebuild = false
							}
						}

						moduleSources = append(moduleSources, moduleSource{
							cPath:       cFullPath,
							objPath:     objFullPath,
							needsRebuild: needsRebuild,
						})
					}
				}
			}
		}
	}

	// 预编译需要更新的 std 模块
	rebuildCount := 0
	for _, ms := range moduleSources {
		if ms.needsRebuild {
			rebuildCount++
		}
	}

	if rebuildCount > 0 {
		fmt.Printf("[Compile] Pre-compiling %d std module(s)...\n", rebuildCount)
		// 并行预编译
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.NumCPU()) // 限制并发数
		var rebuildErrors []string
		var errMu sync.Mutex

		for _, ms := range moduleSources {
			if !ms.needsRebuild {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(cPath, objPath string) {
				defer wg.Done()
				defer func() { <-sem }()

				compileCmd := exec.Command(clangPath, "-c", optLevel, cPath, "-o", objPath)
			for _, p := range validSrcPaths {
				compileCmd.Args = append(compileCmd.Args, "-I", p)
			}
			for _, p := range validStdPaths {
				compileCmd.Args = append(compileCmd.Args, "-I", p)
			}
			if kaulaSrcPath != "" {
				compileCmd.Args = append(compileCmd.Args, "-I", objectCacheDir)
			}
			if poolCapacity > 0 {
				compileCmd.Args = append(compileCmd.Args, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
			}
				if output, err := compileCmd.CombinedOutput(); err != nil {
					errMu.Lock()
					rebuildErrors = append(rebuildErrors, fmt.Sprintf("  %s: %v", filepath.Base(cPath), string(output)))
					errMu.Unlock()
				}
			}(ms.cPath, ms.objPath)
		}
		wg.Wait()

		if len(rebuildErrors) > 0 {
			return fmt.Errorf("std module pre-compilation failed:\n%s", strings.Join(rebuildErrors, "\n"))
		}
		fmt.Printf("[Compile] Std modules pre-compiled (%d updated, %d cached)\n", rebuildCount, len(moduleSources)-rebuildCount)
	} else {
		fmt.Printf("[Compile] All %d std modules cached, skipping pre-compilation\n", len(moduleSources))
	}

	// 编译 KMM V4 runtime（src/kmm_scoped_allocator_v4.c）— std 模块依赖其符号
	if kaulaSrcPath != "" {
		kmmV4Src := filepath.Join(kaulaSrcPath, "kmm_scoped_allocator_v4.c")
		kmmV4Obj := filepath.Join(objectCacheDir, "kmm_v4.o")
		needsRebuild := true
		if cInfo, cErr := os.Stat(kmmV4Src); cErr == nil {
			if oInfo, oErr := os.Stat(kmmV4Obj); oErr == nil {
				if oInfo.ModTime().After(cInfo.ModTime()) || oInfo.ModTime().Equal(cInfo.ModTime()) {
					needsRebuild = false
				}
			}
		}
		if needsRebuild {
			kmmCmd := exec.Command(clangPath, "-c", optLevel, kmmV4Src, "-o", kmmV4Obj)
			for _, p := range validSrcPaths {
				kmmCmd.Args = append(kmmCmd.Args, "-I", p)
			}
			for _, p := range validStdPaths {
				kmmCmd.Args = append(kmmCmd.Args, "-I", p)
			}
			if poolCapacity > 0 {
				kmmCmd.Args = append(kmmCmd.Args, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
			}
			if output, err := kmmCmd.CombinedOutput(); err != nil {
				fmt.Printf("[Compile] Warning: kmm_v4.o compilation failed: %s\n", string(output))
			} else {
				fmt.Printf("[Compile] kmm_v4.o compiled\n")
			}
		}
	}

	// 使用预编译的 .o 文件链接，而不是重新编译 .c 文件
	// 注意：必须在 .o 文件前用 -x none 重置语言类型，否则前面的 -x c 会让 clang 把 .o 当作 C 源码
	clangArgs = append(clangArgs, "-x", "none")
	for _, ms := range moduleSources {
		clangArgs = append(clangArgs, ms.objPath)
	}
	// 添加 kmm_v4.o（如果存在）
	kmmV4Obj := filepath.Join(objectCacheDir, "kmm_v4.o")
	if _, err := os.Stat(kmmV4Obj); err == nil {
		clangArgs = append(clangArgs, kmmV4Obj)
	}

	// 合并所有 std .o 为单个 std.lib（减少链接器处理的文件数）
	stdLibPath := filepath.Join(objectCacheDir, "std.lib")
	// 计算当前模块集合的 hash，只有变化时才重新生成
	libModulesKey := strings.Join(usedModules, ",") + "|kmm_v4"
	libKeyFile := filepath.Join(objectCacheDir, "std.lib.key")
	rebuildLib := true
	if keyData, err := os.ReadFile(libKeyFile); err == nil && string(keyData) == libModulesKey {
		if _, err := os.Stat(stdLibPath); err == nil {
			rebuildLib = false
		}
	}
	if rebuildLib {
		var objPaths []string
		for _, ms := range moduleSources {
			objPaths = append(objPaths, ms.objPath)
		}
		// Include kmm_v4.o in the lib
		if _, err := os.Stat(kmmV4Obj); err == nil {
			objPaths = append(objPaths, kmmV4Obj)
		}
		arCmd := exec.Command("llvm-lib", "/OUT:"+stdLibPath)
		arCmd.Args = append(arCmd.Args, objPaths...)
		if _, err := arCmd.CombinedOutput(); err != nil {
			// llvm-lib 不可用时回退到直接链接 .o 文件
			fmt.Printf("[Compile] Warning: llvm-lib failed, using .o files directly\n")
			// 不写入 key 文件，下次继续尝试
		} else {
			os.WriteFile(libKeyFile, []byte(libModulesKey), 0644)
			// 用 std.lib 替换所有 .o 文件
			clangArgs = clangArgs[:0]
			clangArgs = append(clangArgs, "-x", "c", "-", "-o", outputFile, optLevel, "-I", workDir)
			clangArgs = append(clangArgs, "-DKMM_THREAD_SAFETY_LEVEL=1")
			if poolCapacity > 0 {
				clangArgs = append(clangArgs, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
			}
			for _, p := range validSrcPaths {
				clangArgs = append(clangArgs, "-I", p)
			}
			for _, p := range validStdPaths {
				clangArgs = append(clangArgs, "-I", p)
			}
			clangArgs = append(clangArgs, "-x", "none", stdLibPath)
			fmt.Printf("[Compile] Merged %d .o -> std.lib\n", len(objPaths))
		}
	} else {
		// std.lib 缓存命中，但需确认文件确实存在
		if _, err := os.Stat(stdLibPath); err != nil {
			// 文件不存在，清除 key 并重新构建
			os.Remove(libKeyFile)
			fmt.Printf("[Compile] Warning: std.lib key exists but file missing, rebuilding\n")
			// 重新走 rebuild 逻辑
			var objPaths []string
			for _, ms := range moduleSources {
				objPaths = append(objPaths, ms.objPath)
			}
			if _, err := os.Stat(kmmV4Obj); err == nil {
				objPaths = append(objPaths, kmmV4Obj)
			}
			arCmd := exec.Command("llvm-lib", "/OUT:"+stdLibPath)
			arCmd.Args = append(arCmd.Args, objPaths...)
			if _, err := arCmd.CombinedOutput(); err != nil {
				fmt.Printf("[Compile] Warning: llvm-lib failed, using .o files directly\n")
			} else {
				os.WriteFile(libKeyFile, []byte(libModulesKey), 0644)
				clangArgs = clangArgs[:0]
				clangArgs = append(clangArgs, "-x", "c", "-", "-o", outputFile, optLevel, "-I", workDir)
				clangArgs = append(clangArgs, "-DKMM_THREAD_SAFETY_LEVEL=1")
				if poolCapacity > 0 {
					clangArgs = append(clangArgs, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
				}
				for _, p := range validSrcPaths {
					clangArgs = append(clangArgs, "-I", p)
				}
				for _, p := range validStdPaths {
					clangArgs = append(clangArgs, "-I", p)
				}
				clangArgs = append(clangArgs, "-x", "none", stdLibPath)
				fmt.Printf("[Compile] Merged %d .o -> std.lib\n", len(objPaths))
			}
		} else {
			clangArgs = clangArgs[:0]
			clangArgs = append(clangArgs, "-x", "c", "-", "-o", outputFile, optLevel, "-I", workDir)
			clangArgs = append(clangArgs, "-DKMM_THREAD_SAFETY_LEVEL=1")
			if poolCapacity > 0 {
				clangArgs = append(clangArgs, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
			}
			for _, p := range validSrcPaths {
				clangArgs = append(clangArgs, "-I", p)
			}
			for _, p := range validStdPaths {
				clangArgs = append(clangArgs, "-I", p)
			}
			clangArgs = append(clangArgs, "-x", "none", stdLibPath)
			fmt.Printf("[Compile] Using cached std.lib\n")
		}
	}

	// 添加 Windows 系统库链接
	if runtime.GOOS == "windows" {
		clangArgs = append(clangArgs, "-lws2_32")
		clangArgs = append(clangArgs, "-lwininet")
		clangArgs = append(clangArgs, "-lgdi32")
		clangArgs = append(clangArgs, "-luser32")
		clangArgs = append(clangArgs, "-ladvapi32")
	}

	// 消费 pkglib 第三方库的 libraries/include_path/library_path 字段
	if stdlibConfig != nil {
		for _, lib := range stdlibConfig.ThirdParty {
			// 检查是否使用了此库（通过 usedModules）
			used := false
			for _, mod := range usedModules {
				if mod == lib.Name {
					used = true
					break
				}
			}
			if !used {
				continue
			}
			// 添加 include 路径
			if lib.IncludePath != "" {
				clangArgs = append(clangArgs, "-I", lib.IncludePath)
			}
			// 添加库搜索路径
			if lib.LibraryPath != "" {
				clangArgs = append(clangArgs, "-L", lib.LibraryPath)
			}
			// 添加链接库
			for _, libName := range lib.Libraries {
				clangArgs = append(clangArgs, "-l"+libName)
			}
		}
	}

	cmd := exec.Command(clangPath, clangArgs...)

	// 通过 stdin pipe 将 C 代码传递给 clang（内存编译，避免磁盘 I/O）
	cSource := cCodeInMemory
	if cSource == "" {
		data, err := os.ReadFile(cFile)
		if err != nil {
			return fmt.Errorf("failed to read C source file %s: %v", cFile, err)
		}
		cSource = string(data)
	}

	cmd.Stdin = strings.NewReader(cSource)
	fmt.Printf("[Compile] Clang: memory mode (stdin), %d bytes C code\n", len(cSource))
	fmt.Printf("[Compile] Used modules: %v\n", usedModules)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clang compilation failed: %v, output: %s", err, string(output))
	}
	fmt.Printf("[Compile] Successfully compiled: %s\n", outputFile)
	return nil
}

// findPkglibPath 查找 pkglib 目录路径
func findPkglibPath() string {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	cwd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(exeDir, "pkglib"),
		filepath.Join(exeDir, "..", "pkglib"),
		filepath.Join(cwd, "pkglib"),
		filepath.Join(cwd, "..", "pkglib"),
		"pkglib",
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			absPath, _ := filepath.Abs(p)
			return absPath
		}
	}
	return ""
}

// handleAnalyzePkg 处理 --analyze-pkg 命令，手动分析指定包
func handleAnalyzePkg(pkgName string) {
	pkglibPath := findPkglibPath()
	if pkglibPath == "" {
		fmt.Printf("Error: pkglib directory not found\n")
		os.Exit(1)
	}

	libDir := filepath.Join(pkglibPath, pkgName)
	if info, err := os.Stat(libDir); err != nil || !info.IsDir() {
		fmt.Printf("Error: package '%s' not found in %s\n", pkgName, pkglibPath)
		os.Exit(1)
	}

	fmt.Printf("Analyzing package: %s (%s)\n", pkgName, libDir)
	result, err := stdlib.AnalyzePackage(libDir)
	if err != nil {
		fmt.Printf("Error: failed to analyze %s: %v\n", pkgName, err)
		os.Exit(1)
	}

	if err := result.WriteConfig(libDir); err != nil {
		fmt.Printf("Error: failed to write config: %v\n", err)
		os.Exit(1)
	}

	configFile := filepath.Join(libDir, pkgName+".json")
	fmt.Printf("Config generated: %s\n", configFile)
	fmt.Printf("  Type: %s\n", result.Type)
	fmt.Printf("  Header: %s\n", result.Header)
	fmt.Printf("  Functions: %d\n", len(result.Functions))
	if result.ImplementMacro != "" {
		fmt.Printf("  Implement macro: %s\n", result.ImplementMacro)
	}
	if len(result.Libraries) > 0 {
		fmt.Printf("  Libraries: %v\n", result.Libraries)
	}
}

// handleAnalyzePkgAll 处理 --analyze-pkg-all 命令，手动分析所有包
func handleAnalyzePkgAll() {
	pkglibPath := findPkglibPath()
	if pkglibPath == "" {
		fmt.Printf("Error: pkglib directory not found\n")
		os.Exit(1)
	}

	entries, err := os.ReadDir(pkglibPath)
	if err != nil {
		fmt.Printf("Error: failed to read pkglib directory: %v\n", err)
		os.Exit(1)
	}

	successCount := 0
	failCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		libName := entry.Name()
		libDir := filepath.Join(pkglibPath, libName)

		fmt.Printf("\nAnalyzing: %s\n", libName)
		result, err := stdlib.AnalyzePackage(libDir)
		if err != nil {
			fmt.Printf("  FAILED: %v\n", err)
			failCount++
			continue
		}

		if err := result.WriteConfig(libDir); err != nil {
			fmt.Printf("  FAILED to write config: %v\n", err)
			failCount++
			continue
		}

		configFile := filepath.Join(libDir, libName+".json")
		fmt.Printf("  Generated: %s (type=%s, functions=%d)\n", configFile, result.Type, len(result.Functions))
		if result.ImplementMacro != "" {
			fmt.Printf("  Implement macro: %s\n", result.ImplementMacro)
		}
		successCount++
	}

	fmt.Printf("\nAnalysis complete: %d succeeded, %d failed\n", successCount, failCount)
}