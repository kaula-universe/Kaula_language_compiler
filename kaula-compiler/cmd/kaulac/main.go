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
			// 解析器按 (CWD, 输入文件目录) 两种相对基准解析 LocalPath，
			// 这里先按输入目录尝试，失败则按 CWD 原样使用
			if candidate := filepath.Join(inputDir, absPath); fileExists(candidate) {
				absPath = candidate
			}
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
		errCountBefore := len(errorCollector.Errors())
		localProgram := localParser.Parse()

		if len(errorCollector.Errors()) > errCountBefore {
			fmt.Printf("[Multi-file] Parse errors in %s\n", localPath)
			for _, e := range errorCollector.Errors()[errCountBefore:] {
				if e != nil {
					fmt.Printf("[Multi-file]   %d:%d %s\n", e.Line, e.Column, e.Message)
				}
			}
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

		// 代码生成（本地文件使用 freestanding 模板，避免注入用户态/宿主入口）
		localCfg := *cfg
		localCfg.Boot = "none"
		localCG := codegen.NewCodeGenerator(&localCfg)
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
	braceDepth := 0     // 当前大括号深度（追踪函数体）
	inFunction := false // 是否在函数体内
	skipMain := false   // 是否在跳过 main 函数

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
		// 没有 main 函数：优先插入到注入锚点（用户态模板定义在 includes 之后），
		// 确保本地导入的函数原型/定义在类型定义之后、函数使用之前可见
		if anchorIdx := strings.Index(mainCode, "__KAULA_LOCAL_IMPORT_ANCHOR__"); anchorIdx >= 0 {
			endOfLine := strings.Index(mainCode[anchorIdx:], "\n")
			if endOfLine >= 0 {
				insertAt := anchorIdx + endOfLine + 1
				return mainCode[:insertAt] + "\n" + localCode + mainCode[insertAt:]
			}
		}
		if parts := strings.SplitN(mainCode, "\n\n", 2); len(parts) == 2 {
			return parts[0] + "\n\n" + localCode + "\n" + parts[1]
		}
		return mainCode + "\n" + localCode
	}

	// 在 main 函数之前插入本地代码
	before := mainCode[:mainIdx]
	after := mainCode[mainIdx:]
	return before + localCode + "\n" + after
}

func main() {
	totalStart := time.Now()

	// 自定义参数（非 flag 参数，需在 flag.Parse 前手动提取）
	inputFile := ""
	cleanCache := false
	purgeCache := false
	showCacheStats := false
	initConfig := false

	// 预扫描 os.Args，提取非 flag 参数和自定义 flag
	// 修复：-o/--output 的值不应被误认为输入文件；子命令（compile/run 等）也不是输入文件
	customArgs := []string{}
	args := os.Args[1:]
	knownSubcommands := map[string]bool{"compile": true, "run": true, "build": true, "check": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--clean-cache":
			cleanCache = true
		case arg == "--purge-cache":
			purgeCache = true
		case arg == "--cache-stats":
			showCacheStats = true
		case arg == "--init":
			initConfig = true
		case arg == "-o" || arg == "--output":
			// -o/--output 接受一个值（输出路径），需要跳过下一个参数避免误判为输入文件
			customArgs = append(customArgs, arg)
			if i+1 < len(args) {
				customArgs = append(customArgs, args[i+1])
				i++ // 跳过输出文件路径
			}
		default:
			customArgs = append(customArgs, arg)
			// 只将非 flag、非子命令的参数视为输入文件候选
			if len(arg) > 0 && arg[0] != '-' && !knownSubcommands[arg] {
				inputFile = arg
			}
		}
	}

	// 处理 --init 命令
	if initConfig {
		if _, err := os.Stat("kaula.json"); err == nil {
			fmt.Printf("kaula.json already exists. Use --init-force to overwrite.\n")
			os.Exit(1)
		}
		if err := config.GenerateDefaultConfig("kaula.json"); err != nil {
			fmt.Printf("Error: Failed to generate kaula.json: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Generated kaula.json with default configuration.")
		return
	}

	// 处理命令行参数（允许仅使用缓存管理命令而不需要输入文件）
	if len(os.Args) < 2 {
		printUsage(os.Args[0])
		os.Exit(1)
	}

	// 临时修改 os.Args 以供 flag.Parse() 使用
	os.Args = append([]string{os.Args[0]}, customArgs...)

	// 加载配置（kaula.json + 命令行 flag，会调用 flag.Parse()）
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v, using default\n", err)
	}

	// 处理 --analyze-pkg 命令
	if cfg.AnalyzePkg != "" {
		handleAnalyzePkg(cfg.AnalyzePkg)
		return
	}

	// 处理 --analyze-pkg-all 命令
	if cfg.AnalyzePkgAll {
		handleAnalyzePkgAll()
		return
	}

	// 初始化资源限制（从配置读取）
	timeout.Init()
	timeout.SetLimits(uint64(cfg.MemoryLimitMB), uint64(cfg.TimeoutSec))

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

	// 解析优化级别（优先级：--opt > --sor > --release > 默认 O2）
	optLevel := cfg.ResolveOptLevel(cfg.OptLevel)

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
	p.EnableLogging(true)
	if inputFile != "" {
		p.SetFile(inputFile)
	}
	if cfg.Freestanding {
		p.SetSkipMainCheck(true)
	}

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

	localPubFuncs := collectLocalPubFuncs(program, inputDir)
	concurrentSemanticAnalysisWithConfig(program, stdlibConfig, errorCollector, cfg.SOR, localPubFuncs)
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
	cg.SetSourceFile(inputFile)

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

	if cacheManager != nil && !cfg.NoCache {
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

	// 生成 source map
	if cfg.SourceMap {
		sm := cg.GetSourceMap()
		if sm != nil {
			sm.Target = cacheFile
			mapPath := filepath.Join(cacheDir, inputName+".map.json")
			mapJSON, err := sm.ToJSON()
			if err == nil {
				if err := os.WriteFile(mapPath, []byte(mapJSON), 0644); err != nil {
					fmt.Printf("Warning: Failed to write source map: %v\n", err)
				} else {
					fmt.Printf("[SourceMap] Generated: %s\n", mapPath)
				}
			}
		}
	}

	// Concurrent C compilation
	compileResult := concurrentCompile(cacheFile, output, inputDir, inputName, cwd, usedModules, cacheHit, stdlibConfig, optLevel, poolCapacity, cfg)
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

// isBootMode 判断是否启用编译器内建的裸机引导构建
// 需要: freestanding + boot != none
func isBootMode(cfg *config.Config) bool {
	return cfg != nil && cfg.Freestanding && cfg.Boot != "" && cfg.Boot != "none"
}

// resolveBootArch 推断引导架构（优先级: BootArch > TargetTriple > x86_64）
func resolveBootArch(cfg *config.Config) string {
	if cfg.BootArch != "" {
		return cfg.BootArch
	}
	t := strings.ToLower(cfg.TargetTriple)
	switch {
	case strings.HasPrefix(t, "x86_64"), strings.Contains(t, "amd64"):
		return "x86_64"
	case strings.HasPrefix(t, "i386"), strings.HasPrefix(t, "i686"):
		return "i386"
	case strings.HasPrefix(t, "aarch64"), strings.Contains(t, "arm64"):
		return "aarch64"
	case strings.HasPrefix(t, "riscv64"):
		return "riscv64"
	default:
		return "x86_64"
	}
}

// linkerEmulation 返回对应架构的 lld 仿真模式
func linkerEmulation(arch string) string {
	switch arch {
	case "i386":
		return "elf_i386"
	case "aarch64":
		return "aarch64elf"
	case "riscv64":
		return "elf64lriscv"
	default:
		return "elf_x86_64"
	}
}

// archCodeModel 返回各架构的 clang 代码模型参数
// x86_64/aarch64: large（内核可置于任意地址）
// riscv64: medany（0x80000000 起始，±2GB PC 相对寻址即可达）
// i386: 无代码模型（32 位固定寻址）
func archCodeModel(arch string) []string {
	switch arch {
	case "i386":
		return nil
	case "riscv64":
		return []string{"-mcmodel=medany"}
	default:
		return []string{"-mcmodel=large"}
	}
}

// resolveBootSource 定位引导汇编源文件
// 优先级: --boot-file > 内置模板 <templates>/boot/<arch>-<boot>.S
func resolveBootSource(cfg *config.Config) (string, error) {
	if cfg.BootFile != "" {
		if _, err := os.Stat(cfg.BootFile); err == nil {
			return cfg.BootFile, nil
		}
		return "", fmt.Errorf("boot file not found: %s", cfg.BootFile)
	}
	if cfg.Boot == "custom" {
		return "", fmt.Errorf("boot=custom requires --boot-file")
	}
	arch := resolveBootArch(cfg)
	candidates := []string{
		filepath.Join(cfg.TemplatePath, "boot", fmt.Sprintf("%s-%s.S", arch, cfg.Boot)),
		filepath.Join(cfg.TemplatePath, "boot", arch+".S"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no built-in boot stub for boot=%s arch=%s (looked in %s). Use --boot-file to provide a custom stub",
		cfg.Boot, arch, filepath.Join(cfg.TemplatePath, "boot"))
}

// resolveBootLinkScript 定位链接脚本
// 优先级: --link-script > 内置模板 <templates>/linker/<arch>.ld
func resolveBootLinkScript(cfg *config.Config) (string, error) {
	if cfg.LinkScript != "" {
		if _, err := os.Stat(cfg.LinkScript); err == nil {
			return cfg.LinkScript, nil
		}
		return "", fmt.Errorf("link script not found: %s", cfg.LinkScript)
	}
	arch := resolveBootArch(cfg)
	p := filepath.Join(cfg.TemplatePath, "linker", arch+".ld")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no built-in linker script for arch %s (looked in %s). Use --link-script to provide one",
		arch, filepath.Join(cfg.TemplatePath, "linker"))
}

// findBootKaulaSrcPath 查找 kaula 源目录（src/kaula.h 所在目录），
// 用于定位 freestanding runtime（kaula_freestanding_runtime.c）
func findBootKaulaSrcPath(workDir string) string {
	candidates := []string{}
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		candidates = append(candidates, filepath.Join(envHome, "src"))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates = append(candidates,
			filepath.Join(exeDir, "..", "src"),
			filepath.Join(exeDir, "..", "..", "src"),
			filepath.Join(exeDir, "src"),
		)
	}
	candidates = append(candidates,
		filepath.Join(workDir, "src"),
		filepath.Join(workDir, "..", "src"),
	)
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "kaula.h")); err == nil {
			return p
		}
	}
	return ""
}

// findBootKaulaStdPath 查找 stdlib 头目录（std/io/io.h 所在目录），
// 用于 boot 模式下解析 std.memory 等模块头（如 memory/memory.h）
func findBootKaulaStdPath(workDir string) string {
	candidates := []string{}
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		candidates = append(candidates, filepath.Join(envHome, "std"))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates = append(candidates,
			filepath.Join(exeDir, "..", "std"),
			filepath.Join(exeDir, "..", "..", "std"),
			filepath.Join(exeDir, "std"),
		)
	}
	candidates = append(candidates,
		filepath.Join(workDir, "std"),
		filepath.Join(workDir, "..", "std"),
		filepath.Join(workDir, "..", "..", "std"),
	)
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "io", "io.h")); err == nil {
			return p
		}
	}
	return ""
}

// findBootKaulaFreePath 查找 freestanding 库目录（freestanding/freestanding.h 所在目录），
// 用于解析 freestanding.memory 等模块头（如 memory/memory.h）与
// kaula_freestanding_runtime.c 的 unity include（memory/memory.c）
func findBootKaulaFreePath(workDir string) string {
	candidates := []string{}
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		candidates = append(candidates, filepath.Join(envHome, "freestanding"))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates = append(candidates,
			filepath.Join(exeDir, "..", "freestanding"),
			filepath.Join(exeDir, "..", "..", "freestanding"),
			filepath.Join(exeDir, "freestanding"),
		)
	}
	candidates = append(candidates,
		filepath.Join(workDir, "freestanding"),
		filepath.Join(workDir, "..", "freestanding"),
		filepath.Join(workDir, "..", "..", "freestanding"),
	)
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "freestanding.h")); err == nil {
			return p
		}
	}
	return ""
}

// compileBootKernel 裸机引导构建流水线：
//  1. clang -c 生成 C 代码 -> kernel.o
//  2. clang -c 编译 boot stub -> boot.o
//  3. clang -c 编译 freestanding runtime（如存在）-> runtime.o
//  4. ld.lld -T <linker script> boot.o kernel.o runtime.o -> 可引导 ELF/bin
func compileBootKernel(cacheFile, outputFile, workDir string, optLevel string, cfg *config.Config) error {
	clangPath, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang not found in PATH")
	}
	ldPath, err := exec.LookPath("ld.lld")
	if err != nil {
		return fmt.Errorf("ld.lld not found in PATH (required for bare-metal boot linking)")
	}

	bootSrc, err := resolveBootSource(cfg)
	if err != nil {
		return err
	}
	linkScript, err := resolveBootLinkScript(cfg)
	if err != nil {
		return err
	}

	arch := resolveBootArch(cfg)
	triple := cfg.TargetTriple
	if triple == "" {
		triple = arch + "-none-elf"
	}
	fmt.Printf("[Boot] arch=%s triple=%s boot=%s\n", arch, triple, cfg.Boot)
	fmt.Printf("[Boot] boot stub: %s\n", bootSrc)
	fmt.Printf("[Boot] linker script: %s\n", linkScript)

	workCache := filepath.Dir(cacheFile)
	os.MkdirAll(workCache, 0755)
	base := strings.TrimSuffix(filepath.Base(cacheFile), filepath.Ext(cacheFile))
	kernelObj := filepath.Join(workCache, base+".o")
	bootObj := filepath.Join(workCache, "boot.o")
	runtimeObj := filepath.Join(workCache, "kaula_freestanding_runtime.o")

	// 公共编译参数
	baseArgs := []string{"-target", triple, "-c", optLevel}

	kaulaSrcPath := findBootKaulaSrcPath(workDir)
	kaulaStdPath := findBootKaulaStdPath(workDir)
	kaulaFreePath := findBootKaulaFreePath(workDir)
	// 代码生成中的 freestanding 模块头保留完整前缀（freestanding/xxx/xxx.h），
	// 需要把 freestanding/ 的父目录也加入搜索路径，否则 #include "freestanding/base/types.h"
	// 无法被解析（-I freestanding 只能解析 base/types.h）
	kaulaFreeParentPath := ""
	if kaulaFreePath != "" {
		if parent := filepath.Dir(kaulaFreePath); parent != kaulaFreePath {
			kaulaFreeParentPath = parent
		}
	}

	// 1. 编译 C 代码
	kernelArgs := append(append([]string{}, baseArgs...),
		"-ffreestanding", "-nostdlib", "-fno-pic",
		"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL",
	)
	kernelArgs = append(kernelArgs, archCodeModel(arch)...)
	if kaulaSrcPath != "" {
		kernelArgs = append(kernelArgs, "-I", kaulaSrcPath)
	}
	if kaulaStdPath != "" {
		kernelArgs = append(kernelArgs, "-I", kaulaStdPath)
	}
	if kaulaFreeParentPath != "" {
		kernelArgs = append(kernelArgs, "-I", kaulaFreeParentPath)
	}
	if kaulaFreePath != "" {
		kernelArgs = append(kernelArgs, "-I", kaulaFreePath)
	}
	kernelArgs = append(kernelArgs, cacheFile, "-o", kernelObj)
	fmt.Printf("[Boot] Compiling kernel C -> %s\n", kernelObj)
	if out, err := exec.Command(clangPath, kernelArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("kernel C compilation failed: %v\n%s", err, string(out))
	}

	// 2. 编译 boot stub
	bootArgs := append(append([]string{}, baseArgs...), bootSrc, "-o", bootObj)
	fmt.Printf("[Boot] Compiling boot stub -> %s\n", bootObj)
	if out, err := exec.Command(clangPath, bootArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("boot stub compilation failed: %v\n%s", err, string(out))
	}

	linkObjs := []string{bootObj, kernelObj}

	// 3. 编译 freestanding runtime（unity 包含 freestanding 库：memset/memcpy 等，
	// 供 LLVM builtin lower 引用）
	// 注意：runtime 通过 #include "memory/memory.c" 等 unity-include freestanding 库，
	// 必须让 -I freestanding 优先于 -I std 解析，否则会错误地包含 std/memory/memory.c
	// （后者依赖 libc <string.h>，在 -nostdlib 下找不到）。
	// runtime 只复用 freestanding 库，不需要 std 头，故不传 -I std。
	if kaulaSrcPath != "" {
		runtimeSrc := filepath.Join(kaulaSrcPath, "kaula_freestanding_runtime.c")
		if _, err := os.Stat(runtimeSrc); err == nil {
			runtimeArgs := append(append([]string{}, baseArgs...),
				"-ffreestanding", "-nostdlib", "-fno-pic",
				"-DKAULA_FREESTANDING", "-I", kaulaSrcPath,
			)
			if kaulaFreePath != "" {
				runtimeArgs = append(runtimeArgs, "-I", kaulaFreePath)
			}
			runtimeArgs = append(runtimeArgs, archCodeModel(arch)...)
			runtimeArgs = append(runtimeArgs, runtimeSrc, "-o", runtimeObj)
			fmt.Printf("[Boot] Compiling freestanding runtime -> %s\n", runtimeObj)
			if out, err := exec.Command(clangPath, runtimeArgs...).CombinedOutput(); err != nil {
				fmt.Printf("[Boot] Warning: runtime compilation failed: %v\n%s\n", err, string(out))
			} else if _, err := os.Stat(runtimeObj); err == nil {
				linkObjs = append(linkObjs, runtimeObj)
			}
		}
	}

	// 3.5 编译 KMM V4 分配器（静态池模式，提供堆分配/作用域回收）
	allocObj := filepath.Join(workCache, "kmm_scoped_allocator_v4.o")
	if kaulaSrcPath != "" {
		allocSrc := filepath.Join(kaulaSrcPath, "kmm_scoped_allocator_v4.c")
		if _, err := os.Stat(allocSrc); err == nil {
			allocArgs := append(append([]string{}, baseArgs...),
				"-ffreestanding", "-nostdlib", "-fno-pic",
				"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL", "-I", kaulaSrcPath,
			)
			if kaulaFreePath != "" {
				allocArgs = append(allocArgs, "-I", kaulaFreePath)
			}
			allocArgs = append(allocArgs, archCodeModel(arch)...)
			allocArgs = append(allocArgs, allocSrc, "-o", allocObj)
			fmt.Printf("[Boot] Compiling KMM allocator -> %s\n", allocObj)
			if out, err := exec.Command(clangPath, allocArgs...).CombinedOutput(); err != nil {
				fmt.Printf("[Boot] Warning: allocator compilation failed: %v\n%s\n", err, string(out))
			} else if _, err := os.Stat(allocObj); err == nil {
				linkObjs = append(linkObjs, allocObj)
			}
		}
	}

	// 4. 链接
	ldArgs := []string{"-m", linkerEmulation(arch), "-T", linkScript}
	ldArgs = append(ldArgs, linkObjs...)
	ldArgs = append(ldArgs, "-o", outputFile)
	if cfg.OutputFormat == "bin" {
		ldArgs = append(ldArgs, "--oformat=binary")
	}
	fmt.Printf("[Boot] Linking -> %s\n", outputFile)
	if out, err := exec.Command(ldPath, ldArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("linking failed: %v\n%s", err, string(out))
	}
	return nil
}

// compileUserProgram 用户态程序构建流水线：
//  1. clang -c 生成 C 代码 -> prog.o（与 boot 内核同参，含 KMM 静态池）
//  2. ld.lld -T user.ld 链接（入口 user_start，加载地址 0x40000000）
//  输出可在内核 ELF 加载器下运行的用户程序 ELF
func compileUserProgram(cacheFile, outputFile, workDir string, optLevel string, cfg *config.Config) error {
	clangPath, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang not found in PATH")
	}
	ldPath, err := exec.LookPath("ld.lld")
	if err != nil {
		return fmt.Errorf("ld.lld not found in PATH (required for user program linking)")
	}

	arch := resolveBootArch(cfg)
	triple := cfg.TargetTriple
	if triple == "" {
		triple = arch + "-none-elf"
	}
	fmt.Printf("[User] arch=%s triple=%s\n", arch, triple)

	workCache := filepath.Dir(cacheFile)
	os.MkdirAll(workCache, 0755)
	base := strings.TrimSuffix(filepath.Base(cacheFile), filepath.Ext(cacheFile))
	progObj := filepath.Join(workCache, base+".o")
	runtimeObj := filepath.Join(workCache, "kaula_freestanding_runtime.o")
	allocObj := filepath.Join(workCache, "kmm_scoped_allocator_v4.o")

	baseArgs := []string{"-target", triple, "-c", optLevel}

	kaulaSrcPath := findBootKaulaSrcPath(workDir)
	kaulaStdPath := findBootKaulaStdPath(workDir)
	kaulaFreePath := findBootKaulaFreePath(workDir)
	// 见 compileBootKernel 中同样注释：freestanding/ 父目录需加入搜索路径
	kaulaFreeParentPath := ""
	if kaulaFreePath != "" {
		if parent := filepath.Dir(kaulaFreePath); parent != kaulaFreePath {
			kaulaFreeParentPath = parent
		}
	}

	progArgs := append(append([]string{}, baseArgs...),
		"-ffreestanding", "-nostdlib", "-fno-pic",
		"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL",
	)
	progArgs = append(progArgs, archCodeModel(arch)...)
	if kaulaSrcPath != "" {
		progArgs = append(progArgs, "-I", kaulaSrcPath)
	}
	if kaulaStdPath != "" {
		progArgs = append(progArgs, "-I", kaulaStdPath)
	}
	if kaulaFreeParentPath != "" {
		progArgs = append(progArgs, "-I", kaulaFreeParentPath)
	}
	if kaulaFreePath != "" {
		progArgs = append(progArgs, "-I", kaulaFreePath)
	}
	progArgs = append(progArgs, cacheFile, "-o", progObj)
	fmt.Printf("[User] Compiling program C -> %s\n", progObj)
	if out, err := exec.Command(clangPath, progArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("program C compilation failed: %v\n%s", err, string(out))
	}

	linkObjs := []string{progObj}

	if kaulaSrcPath != "" {
		runtimeSrc := filepath.Join(kaulaSrcPath, "kaula_freestanding_runtime.c")
		if _, err := os.Stat(runtimeSrc); err == nil {
			runtimeArgs := append(append([]string{}, baseArgs...),
				"-ffreestanding", "-nostdlib", "-fno-pic",
				"-DKAULA_FREESTANDING", "-I", kaulaSrcPath,
			)
			if kaulaFreePath != "" {
				runtimeArgs = append(runtimeArgs, "-I", kaulaFreePath)
			}
			runtimeArgs = append(runtimeArgs, archCodeModel(arch)...)
			runtimeArgs = append(runtimeArgs, runtimeSrc, "-o", runtimeObj)
			if out, err := exec.Command(clangPath, runtimeArgs...).CombinedOutput(); err != nil {
				fmt.Printf("[User] Warning: runtime compilation failed: %v\n%s\n", err, string(out))
			} else if _, err := os.Stat(runtimeObj); err == nil {
				linkObjs = append(linkObjs, runtimeObj)
			}
		}

		allocSrc := filepath.Join(kaulaSrcPath, "kmm_scoped_allocator_v4.c")
		if _, err := os.Stat(allocSrc); err == nil {
			allocArgs := append(append([]string{}, baseArgs...),
				"-ffreestanding", "-nostdlib", "-fno-pic",
				"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL", "-I", kaulaSrcPath,
			)
			if kaulaFreePath != "" {
				allocArgs = append(allocArgs, "-I", kaulaFreePath)
			}
			allocArgs = append(allocArgs, archCodeModel(arch)...)
			allocArgs = append(allocArgs, allocSrc, "-o", allocObj)
			if out, err := exec.Command(clangPath, allocArgs...).CombinedOutput(); err != nil {
				fmt.Printf("[User] Warning: allocator compilation failed: %v\n%s\n", err, string(out))
			} else if _, err := os.Stat(allocObj); err == nil {
				linkObjs = append(linkObjs, allocObj)
			}
		}
	}

	userLinkScript := filepath.Join(cfg.TemplatePath, "linker", "user.ld")
	if _, err := os.Stat(userLinkScript); err != nil {
		userLinkScript = filepath.Join(workDir, "user.ld")
	}
	ldArgs := []string{"-m", linkerEmulation(arch), "-T", userLinkScript}
	ldArgs = append(ldArgs, linkObjs...)
	ldArgs = append(ldArgs, "-o", outputFile)
	fmt.Printf("[User] Linking -> %s\n", outputFile)
	if out, err := exec.Command(ldPath, ldArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("user program linking failed: %v\n%s", err, string(out))
	}
	return nil
}

// concurrentCompile 并发保存缓存并编译 C 代码
func concurrentCompile(cacheFile, cCode, inputDir, inputName, workDir string, usedModules []string, cacheHit bool, stdlibConfig *stdlib.StdlibConfig, optLevel string, poolCapacity int, cfg *config.Config) *compileResult_t {
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
		if isBootMode(cfg) {
			// 裸机引导模式：输出可引导 ELF（或 raw bin）
			ext := ".elf"
			if cfg.OutputFormat == "bin" {
				ext = ".bin"
			}
			outputExe = filepath.Join(inputDir, inputName+ext)
		}

		var err error
		if cfg.Boot == "user" {
			// 用户态程序：clang -c + ld.lld 链接到用户区 0x40000000，无 boot stub
			err = compileUserProgram(cacheFile, outputExe, workDir, optLevel, cfg)
		} else if isBootMode(cfg) {
			err = compileBootKernel(cacheFile, outputExe, workDir, optLevel, cfg)
		} else {
			err = compileCCode(cacheFile, outputExe, workDir, usedModules, cCode, stdlibConfig, optLevel, poolCapacity, cfg)
		}
		if err != nil {
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
func concurrentSemanticAnalysisWithConfig(program *ast.Program, stdlibConfig *stdlib.StdlibConfig, errorCollector *errors.ErrorCollector, sorEnabled bool, localPubFuncs map[string]bool) *semaResult_t {
	result := &semaResult_t{ErrorCollector: errorCollector}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		sa := sema.NewSemanticAnalyzerWithConfig("kaula-compiler/stdlib.json", errorCollector)
		if stdlibConfig != nil {
			sa.SetStdlibConfig(stdlibConfig)
		}
		sa.SetLocalImportFuncs(localPubFuncs)
		sa.SetSOREnabled(sorEnabled)
		sa.Analyze(program)
	}()

	wg.Wait()
	return result
}

// collectLocalPubFuncs 扫描本地 import 的 .kl 文件，收集 pub 函数名
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// collectLocalPubFuncs 扫描本地 import 的 .kl 文件，收集 pub 函数名
// （在语义分析之前解析，使跨文件调用不被判为未定义）
func collectLocalPubFuncs(program *ast.Program, inputDir string) map[string]bool {
	pubFuncs := make(map[string]bool)

	var visit func(p *ast.Program, dir string)
	visit = func(p *ast.Program, dir string) {
		for _, stmt := range p.Statements {
			imp, ok := stmt.(*ast.ImportStatement)
			if !ok || !imp.IsLocal {
				continue
			}
			absPath := imp.LocalPath
			if !filepath.IsAbs(absPath) {
				if candidate := filepath.Join(dir, absPath); fileExists(candidate) {
					absPath = candidate
				}
			}
			data, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			localLex := lexer.NewLexer(string(data))
			localParser := parser.NewParser(localLex)
			localParser.SetSkipMainCheck(true)
			localParser.EnableLogging(false)
			localProgram := localParser.Parse()
			if localParser.HasErrors() {
				continue
			}
			for _, s := range localProgram.Statements {
				if fn, ok := s.(*ast.FunctionStatement); ok && fn.IsPublic {
					pubFuncs[fn.Name] = true
				}
			}
			visit(localProgram, filepath.Dir(absPath))
		}
	}

	visit(program, inputDir)
	return pubFuncs
}

type semaResult_t struct {
	*errors.ErrorCollector
}

func (s *semaResult_t) HasErrors() bool {
	return s.ErrorCollector.HasErrors()
}

func findStdlib() string {
	// 1. KAULA_HOME 环境变量
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		p := filepath.Join(envHome, "kaula-compiler", "stdlib.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 2. 可执行文件路径
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates := []string{
			filepath.Join(exeDir, "stdlib.json"),
			filepath.Join(exeDir, "..", "kaula-compiler", "stdlib.json"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	// 3. 工作目录相对路径
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
// 读取 std/dependencies.json 与 freestanding/dependencies.json，递归展开所有依赖模块，
// 返回去重后的完整模块列表（freestanding 模块保留 freestanding. 前缀）
func resolveModuleDependencies(usedModules []string, validStdPaths []string, validFreePaths []string) []string {
	// 查找 std 依赖声明
	var depsPath string
	for _, stdPath := range validStdPaths {
		candidate := filepath.Join(stdPath, "dependencies.json")
		if _, err := os.Stat(candidate); err == nil {
			depsPath = candidate
			break
		}
	}
	var depsMap map[string][]string
	if depsPath != "" {
		if data, err := os.ReadFile(depsPath); err == nil {
			_ = json.Unmarshal(data, &depsMap)
		}
	}

	// 查找 freestanding 依赖声明
	var freeDepsMap map[string][]string
	for _, freePath := range validFreePaths {
		candidate := filepath.Join(freePath, "dependencies.json")
		if _, err := os.Stat(candidate); err == nil {
			if data, err := os.ReadFile(candidate); err == nil {
				_ = json.Unmarshal(data, &freeDepsMap)
			}
			break
		}
	}
	if depsMap == nil && freeDepsMap == nil {
		return usedModules // 无依赖声明文件，原样返回
	}

	// BFS 递归展开所有依赖
	result := make(map[string]bool)
	queue := make([]string, len(usedModules))
	copy(queue, usedModules)

	for len(queue) > 0 {
		mod := queue[0]
		queue = queue[1:]

		if result[mod] {
			continue // 已处理
		}
		result[mod] = true

		// freestanding 模块：剥离前缀，依赖从 freestanding 声明中查找
		prefix := ""
		if len(mod) > 13 && mod[:13] == "freestanding." {
			prefix = "freestanding."
			mod = mod[13:]
		}

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

		// 查找该模块的依赖
		var depList []string
		if prefix == "freestanding." {
			if freeDepsMap != nil {
				depList = freeDepsMap[normalizedName]
			}
		} else if depsMap != nil {
			depList = depsMap[normalizedName]
		}
		for _, dep := range depList {
			key := dep
			if prefix == "freestanding." {
				key = "freestanding." + dep
			}
			if !result[key] {
				queue = append(queue, key)
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

func compileCCode(cFile, outputFile, workDir string, usedModules []string, cCodeInMemory string, stdlibConfig *stdlib.StdlibConfig, optLevel string, poolCapacity int, cfg *config.Config) error {
	clangPath, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang not found in PATH")
	}

	// 查找 kaula.h 所在的目录（优先级：KAULA_HOME > 可执行文件路径 > 相对路径）
	var kaulaRoot string

	// 1. 环境变量 KAULA_HOME
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		if _, err := os.Stat(filepath.Join(envHome, "src", "kaula.h")); err == nil {
			kaulaRoot = envHome
		} else if _, err := os.Stat(filepath.Join(envHome, "include", "kaula", "kaula.h")); err == nil {
			kaulaRoot = envHome
		}
	}

	// 2. 从可执行文件路径推断（kaulac 通常在 kaula-compiler/ 或 build/bin/ 下）
	if kaulaRoot == "" {
		if exePath, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(filepath.Clean(exePath))
			candidates := []string{
				filepath.Join(exeDir, ".."),       // kaula-compiler/ 的上级 = kaula 根
				filepath.Join(exeDir, "..", ".."), // build/bin/ 的上两级 = kaula 根
				exeDir,                            // 可执行文件所在目录就是 kaula 根
			}
			for _, c := range candidates {
				if _, err := os.Stat(filepath.Join(c, "src", "kaula.h")); err == nil {
					kaulaRoot = c
					break
				} else if _, err := os.Stat(filepath.Join(c, "include", "kaula", "kaula.h")); err == nil {
					kaulaRoot = c
					break
				}
			}
		}
	}

	// 3. 从工作目录相对路径回溯
	if kaulaRoot == "" {
		candidates := []string{
			workDir,
			filepath.Join(workDir, ".."),
			filepath.Join(workDir, "..", ".."),
			filepath.Join(workDir, "..", "..", ".."),
			filepath.Join(workDir, "..", "..", "..", "kaula"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(filepath.Join(c, "src", "kaula.h")); err == nil {
				kaulaRoot = c
				break
			} else if _, err := os.Stat(filepath.Join(c, "include", "kaula", "kaula.h")); err == nil {
				kaulaRoot = c
				break
			}
		}
	}

	stdLibraryName, runtimeLibraryName := installedLibraryNames()
	installedRoot := ""
	if cfg == nil || !cfg.Freestanding {
		installedCandidates := []string{kaulaRoot}
		if kaulaRoot != "" {
			installedCandidates = append(installedCandidates, filepath.Join(kaulaRoot, "build"))
		}
		for _, candidate := range installedCandidates {
			if candidate == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(candidate, "include", "kaula", "kaula.h")); err != nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(candidate, "lib", stdLibraryName)); err != nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(candidate, "lib", runtimeLibraryName)); err == nil {
				installedRoot = candidate
				break
			}
		}
	}
	useInstalledLibraries := installedRoot != ""

	var kaulaSrcPath string
	if useInstalledLibraries {
		kaulaSrcPath = filepath.Join(installedRoot, "include", "kaula")
	} else if kaulaRoot != "" {
		kaulaSrcPath = filepath.Join(kaulaRoot, "src")
	}

	srcPaths := []string{}
	if useInstalledLibraries {
		srcPaths = append(srcPaths,
			filepath.Join(installedRoot, "include", "kaula"),
			filepath.Join(installedRoot, "include", "runtime"),
		)
	} else {
		srcPaths = append(srcPaths, filepath.Join(workDir, "src"))
		if kaulaSrcPath != "" {
			srcPaths = append(srcPaths, kaulaSrcPath)
		}
		srcPaths = append(srcPaths, filepath.Join(workDir, "..", "src"))
	}

	stdPaths := []string{}
	if useInstalledLibraries {
		stdPaths = append(stdPaths, filepath.Join(installedRoot, "include", "std"))
	} else if kaulaRoot != "" {
		stdPaths = append(stdPaths, filepath.Join(kaulaRoot, "std"))
	}
	stdPaths = append(stdPaths,
		filepath.Join(workDir, "std"),
		filepath.Join(workDir, "..", "std"),
		filepath.Join(workDir, "..", "..", "std"),
	)

	// freestanding 库目录（freestanding.memory 等模块头/实现所在目录）
	freePaths := []string{}
	if useInstalledLibraries {
		freePaths = append(freePaths, filepath.Join(installedRoot, "include", "freestanding"))
	} else if kaulaRoot != "" {
		freePaths = append(freePaths, filepath.Join(kaulaRoot, "freestanding"))
	}
	freePaths = append(freePaths,
		filepath.Join(workDir, "freestanding"),
		filepath.Join(workDir, "..", "freestanding"),
		filepath.Join(workDir, "..", "..", "freestanding"),
	)
	// 代码生成中的 freestanding 模块头保留完整前缀（freestanding/xxx/xxx.h），
	// 需要把 freestanding/ 的父目录也加入搜索路径，否则 std 目录下的同名头
	// 文件（如 string/string.h）会被错误解析到 std 版本
	for _, p := range freePaths {
		parent := filepath.Dir(p)
		if parent != p {
			freePaths = append(freePaths, parent)
		}
	}

	var validSrcPaths, validStdPaths, validFreePaths []string
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
	for _, p := range freePaths {
		if _, err := os.Stat(p); err == nil {
			validFreePaths = append(validFreePaths, p)
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
		for _, p := range validFreePaths {
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
	clangArgs = append(clangArgs, "-fwrapv", "-fno-strict-aliasing")
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
	for _, p := range validFreePaths {
		clangArgs = append(clangArgs, "-I", p)
	}
	// 启用 PCH：让 clang 自动查找 kaula.h.gch 并使用
	// 暂时禁用 PCH 以调试崩溃问题
	// if kaulaSrcPath != "" {
	// 	clangArgs = append(clangArgs, "-I", objectCacheDir)
	// }

	// 自动解析模块传递依赖（读取 dependencies.json 递归展开）
	usedModules = resolveModuleDependencies(usedModules, validStdPaths, validFreePaths)

	// 预编译 std 模块为 .o 对象文件（增量编译缓存）
	// objectCacheDir 已在 PCH 阶段提前创建

	// 收集所有需要编译的 std / freestanding 模块 .c 文件
	type moduleSource struct {
		cPath        string
		objPath      string
		needsRebuild bool
	}
	var moduleSources []moduleSource

	for _, moduleName := range usedModules {
		moduleDirName := moduleName
		isFreeModule := false
		if len(moduleDirName) > 13 && moduleDirName[:13] == "freestanding." {
			isFreeModule = true
			moduleDirName = moduleDirName[13:]
		}
		if len(moduleDirName) > 4 && moduleDirName[:4] == "std/" {
			moduleDirName = moduleDirName[4:]
		}
		if len(moduleDirName) > 4 && moduleDirName[:4] == "std." {
			moduleDirName = moduleDirName[4:]
		}
		moduleDirName = strings.ReplaceAll(moduleDirName, ".", "/")

		// freestanding 模块在 freestanding 库目录中查找，std 模块在 stdlib 目录中查找
		searchPaths := validStdPaths
		if isFreeModule {
			searchPaths = validFreePaths
		}
		for _, stdPath := range searchPaths {
			moduleDir := filepath.Join(stdPath, moduleDirName)
			if _, err := os.Stat(moduleDir); err == nil {
				entries, _ := os.ReadDir(moduleDir)
				for _, entry := range entries {
					if !entry.IsDir() && filepath.Ext(entry.Name()) == ".c" {
						cFullPath := filepath.Join(moduleDir, entry.Name())
						objName := moduleDirName + "_" + strings.TrimSuffix(entry.Name(), ".c") + ".o"
						// freestanding 模块使用独立对象名，避免与同名 std 模块缓存冲突
						if isFreeModule {
							objName = "fs_" + objName
						}
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
							cPath:        cFullPath,
							objPath:      objFullPath,
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
				for _, p := range validFreePaths {
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

	// 非安装模式下需要链接的运行时对象（kmm_v4：KMM 分配器；spend_call：强制消费流；allocator：fast_alloc 系列符号）
	runtimeObjs := []string{"kmm_v4.o", "spend_call.o", "allocator.o"}

	// 编译 KMM V4 runtime（src/kmm_scoped_allocator_v4.c）— std 模块依赖其符号
	// 同时编译 spend_call.c（Spend/Call 强制消费流）与 allocator.c（fast_alloc 系列符号）
	if kaulaSrcPath != "" && !useInstalledLibraries {
		runtimeSources := []struct {
			cName string
			oName string
		}{
			{"kmm_scoped_allocator_v4.c", "kmm_v4.o"},
			{"spend_call.c", "spend_call.o"},
			{"allocator.c", "allocator.o"},
		}
		for _, rs := range runtimeSources {
			rsSrc := filepath.Join(kaulaSrcPath, rs.cName)
			rsObj := filepath.Join(objectCacheDir, rs.oName)
			needsRebuild := true
			if cInfo, cErr := os.Stat(rsSrc); cErr == nil {
				if oInfo, oErr := os.Stat(rsObj); oErr == nil {
					if oInfo.ModTime().After(cInfo.ModTime()) || oInfo.ModTime().Equal(cInfo.ModTime()) {
						needsRebuild = false
					}
				}
			}
			if needsRebuild {
				rsCmd := exec.Command(clangPath, "-c", optLevel, rsSrc, "-o", rsObj)
				for _, p := range validSrcPaths {
					rsCmd.Args = append(rsCmd.Args, "-I", p)
				}
				for _, p := range validStdPaths {
					rsCmd.Args = append(rsCmd.Args, "-I", p)
				}
				for _, p := range validFreePaths {
					rsCmd.Args = append(rsCmd.Args, "-I", p)
				}
				if poolCapacity > 0 {
					rsCmd.Args = append(rsCmd.Args, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
				}
				if output, err := rsCmd.CombinedOutput(); err != nil {
					fmt.Printf("[Compile] Warning: %s compilation failed: %s\n", rs.oName, string(output))
				} else {
					fmt.Printf("[Compile] %s compiled\n", rs.oName)
				}
			}
		}
	}

	// 使用预编译的 .o 文件链接，而不是重新编译 .c 文件
	// 注意：必须在 .o 文件前用 -x none 重置语言类型，否则前面的 -x c 会让 clang 把 .o 当作 C 源码
	// 安装模式下 std/freestanding 模块符号已在 kaula_std.lib / kaula_freestanding.lib 中，
	// 不再单独链接 .o，否则与静态库中的同名符号产生 LNK2005 重复定义。
	if len(moduleSources) > 0 && !useInstalledLibraries {
		clangArgs = append(clangArgs, "-x", "none")
		for _, ms := range moduleSources {
			clangArgs = append(clangArgs, ms.objPath)
		}
	}
	// 添加 kmm_v4.o/spend_call.o/allocator.o（如果存在）— 裸机模式下跳过（依赖 OS 调用）
	if !useInstalledLibraries && (cfg == nil || !cfg.Freestanding) {
		for _, objName := range runtimeObjs {
			runtimeObj := filepath.Join(objectCacheDir, objName)
			if _, err := os.Stat(runtimeObj); err == nil {
				clangArgs = append(clangArgs, runtimeObj)
			}
		}
	}

	// 合并所有 std .o 为单个 std.lib（减少链接器处理的文件数）
	// 裸机模式下跳过 std.lib（使用 -nostdlib，不需要标准库）
	if cfg == nil || !cfg.Freestanding {
		if useInstalledLibraries {
			clangArgs = append(clangArgs,
				"-x", "none",
				filepath.Join(installedRoot, "lib", stdLibraryName),
				filepath.Join(installedRoot, "lib", runtimeLibraryName),
			)
			fmt.Printf("[Compile] Using installed static libraries: %s, %s\n", stdLibraryName, runtimeLibraryName)
			// 使用 freestanding 模块时链接 libkaula_freestanding.a（安装模式下）
			for _, mod := range usedModules {
				if strings.HasPrefix(mod, "freestanding.") {
					freeLibPath := filepath.Join(installedRoot, "lib", "libkaula_freestanding.a")
					if runtime.GOOS == "windows" {
						freeLibPath = filepath.Join(installedRoot, "lib", "kaula_freestanding.lib")
					}
					if _, err := os.Stat(freeLibPath); err == nil {
						clangArgs = append(clangArgs, "-x", "none", freeLibPath)
						fmt.Printf("[Compile] Linked installed freestanding library: %s\n", freeLibPath)
					}
					break
				}
			}
		} else {
			stdLibPath := filepath.Join(objectCacheDir, "std.lib")
			// 计算当前模块集合的 hash，只有变化时才重新生成
			libModulesKey := strings.Join(usedModules, ",") + "|kmm_v4|spend_call|allocator"
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
				// Include runtime objects in the lib
				for _, objName := range runtimeObjs {
					runtimeObj := filepath.Join(objectCacheDir, objName)
					if _, err := os.Stat(runtimeObj); err == nil {
						objPaths = append(objPaths, runtimeObj)
					}
				}
				arCmd := exec.Command("llvm-lib", "/OUT:"+stdLibPath)
				arCmd.Args = append(arCmd.Args, objPaths...)
				if runtime.GOOS != "windows" {
					// 非 Windows 平台使用 ar 归档（llvm-lib 是 Windows 工具）
					arArgs := append([]string{"rcs", stdLibPath}, objPaths...)
					arCmd = exec.Command("ar", arArgs...)
				}
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
					for _, p := range validFreePaths {
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
					for _, objName := range runtimeObjs {
						runtimeObj := filepath.Join(objectCacheDir, objName)
						if _, err := os.Stat(runtimeObj); err == nil {
							objPaths = append(objPaths, runtimeObj)
						}
					}
					arCmd := exec.Command("llvm-lib", "/OUT:"+stdLibPath)
					arCmd.Args = append(arCmd.Args, objPaths...)
					if runtime.GOOS != "windows" {
						// 非 Windows 平台使用 ar 归档（llvm-lib 是 Windows 工具）
						arArgs := append([]string{"rcs", stdLibPath}, objPaths...)
						arCmd = exec.Command("ar", arArgs...)
					}
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
					for _, p := range validFreePaths {
						clangArgs = append(clangArgs, "-I", p)
					}
					clangArgs = append(clangArgs, "-x", "none", stdLibPath)
					fmt.Printf("[Compile] Using cached std.lib\n")
				}
			}
		}
	} // end if !cfg.Freestanding

	clangArgs = append(clangArgs, "-fwrapv", "-fno-strict-aliasing")

	// 添加 Windows 系统库链接（裸机模式跳过）
	if runtime.GOOS == "windows" && (cfg == nil || !cfg.Freestanding) {
		clangArgs = append(clangArgs, "-lws2_32")
		clangArgs = append(clangArgs, "-lwininet")
		clangArgs = append(clangArgs, "-lgdi32")
		clangArgs = append(clangArgs, "-luser32")
		clangArgs = append(clangArgs, "-ladvapi32")
	}

	// 非 Windows 平台链接 libm（pow/sqrt 等数学函数；Windows 在 CRT 中提供）
	if runtime.GOOS != "windows" && (cfg == nil || !cfg.Freestanding) {
		clangArgs = append(clangArgs, "-lm")
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
				incPath := lib.IncludePath
				// 跨平台兼容：json 中记录的绝对路径在当前机器不存在时，
				// 回退到工作目录下的 pkglib 目录
				if _, err := os.Stat(incPath); err != nil {
					fallbackInc := filepath.Join(workDir, "pkglib")
					if _, fbErr := os.Stat(fallbackInc); fbErr == nil {
						incPath = fallbackInc
					}
				}
				clangArgs = append(clangArgs, "-I", incPath)
			}
			// 添加库搜索路径
			if lib.LibraryPath != "" {
				libPath := lib.LibraryPath
				if _, err := os.Stat(libPath); err != nil {
					fallbackLib := filepath.Join(workDir, "pkglib", lib.Name)
					if _, fbErr := os.Stat(fallbackLib); fbErr == nil {
						libPath = fallbackLib
					}
				}
				clangArgs = append(clangArgs, "-L", libPath)
			}
			// 添加链接库
			for _, libName := range lib.Libraries {
				clangArgs = append(clangArgs, "-l"+libName)
			}
		}
	}

	// 添加用户自定义的 C 编译器参数
	if cfg != nil {
		for _, flag := range cfg.CFlags {
			clangArgs = append(clangArgs, flag)
		}
		for _, define := range cfg.CDefines {
			clangArgs = append(clangArgs, "-D"+define)
		}
		for _, lib := range cfg.CLibs {
			clangArgs = append(clangArgs, "-l"+lib)
		}

		// ====== 裸机/交叉编译模式 ======
		if cfg.Freestanding {
			clangArgs = append(clangArgs,
				"-ffreestanding",
				"-nostdlib",
				"-nostartfiles",
				"-DKMM_V4_STATIC_POOL",
				"-DKAULA_FREESTANDING",
			)
			// 链接 freestanding runtime：提供 memset/memcpy/memmove/memcmp/strlen
			// LLVM 在 size 未知时会将对 builtin 的调用 lower 为符号调用，裸机下必须自提供
			if kaulaSrcPath != "" {
				runtimeSrc := filepath.Join(kaulaSrcPath, "kaula_freestanding_runtime.c")
				if _, err := os.Stat(runtimeSrc); err == nil {
					clangArgs = append(clangArgs, "-x", "none", runtimeSrc)
					fmt.Printf("[Compile] Freestanding: linked runtime %s\n", runtimeSrc)
				} else {
					fmt.Printf("[Warning] Freestanding runtime not found: %s\n", runtimeSrc)
				}
			}
			// 裸机模式下禁用 Windows 系统库
			// （已添加的需要移除，但为简单起见，freestanding 优先跳过上面的 Windows 库添加）
		}
		if cfg.TargetTriple != "" {
			clangArgs = append(clangArgs, "-target", cfg.TargetTriple)
		}
		if cfg.LinkScript != "" {
			clangArgs = append(clangArgs, "-T", cfg.LinkScript)
		}
		if cfg.Entry != "" {
			clangArgs = append(clangArgs, "-e", cfg.Entry)
		}
		if cfg.OutputFormat != "" && cfg.OutputFormat != "elf" {
			switch cfg.OutputFormat {
			case "bin":
				// raw binary 输出：让链接器直接输出 binary 格式
				// 裸机内核、镜像文件常用格式
				clangArgs = append(clangArgs, "-Wl,--oformat=binary")
			case "obj":
				// 仅编译不链接，生成 .o 目标文件
				// 用于后续手工链接或集成到其他构建系统
				clangArgs = append(clangArgs, "-c")
				// obj 模式下不输出可执行文件，输出文件改为 .o 后缀
				// （由调用方通过 -o 控制，这里仅确保不链接）
			default:
				fmt.Printf("[Warning] Unknown output format: %s, using default (elf)\n", cfg.OutputFormat)
			}
		}
		// elf 格式：clang 默认行为，无需额外参数
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

func installedLibraryNames() (string, string) {
	if runtime.GOOS == "windows" {
		return "kaula_std.lib", "kaula_runtime.lib"
	}
	return "libkaula_std.a", "libkaula_runtime.a"
}

// findPkglibPath 查找 pkglib 目录路径
func findPkglibPath() string {
	// 1. KAULA_HOME 环境变量
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		p := filepath.Join(envHome, "pkglib")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			absPath, _ := filepath.Abs(p)
			return absPath
		}
	}

	// 2. 可执行文件路径
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates := []string{
			filepath.Join(exeDir, "pkglib"),
			filepath.Join(exeDir, "..", "pkglib"),
			filepath.Join(exeDir, "..", "..", "pkglib"),
		}
		for _, p := range candidates {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				absPath, _ := filepath.Abs(p)
				return absPath
			}
		}
	}

	// 3. 工作目录
	cwd, _ := os.Getwd()
	candidates := []string{
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

func printUsage(exe string) {
	fmt.Printf("Usage: %s [options] <input file>\n\n", exe)
	fmt.Printf("Options:\n")
	fmt.Printf("  --init                  生成默认 kaula.json 配置文件\n")
	fmt.Printf("  --clean-cache           清理过期缓存 (7 天以上)\n")
	fmt.Printf("  --purge-cache           清空所有缓存\n")
	fmt.Printf("  --cache-stats           显示缓存统计信息\n")
	fmt.Printf("  --no-cache              禁用增量编译缓存\n")
	fmt.Printf("  --sor                   启用 SOR 编译时所有权分析（默认 -O3）\n")
	fmt.Printf("  --release               Release 模式 (-O3)\n")
	fmt.Printf("  --opt <level>           优化级别 (O0/O1/O2/O3)，覆盖所有默认值\n")
	fmt.Printf("  --sourcemap             生成源码映射文件 (.kl.map.json)\n")
	fmt.Printf("  --memory-limit <MB>     内存限制 (默认 4096 MB)\n")
	fmt.Printf("  --timeout <sec>         编译超时限制 (默认 120 秒)\n")
	fmt.Printf("  --template <path>       代码生成模板路径\n")
	fmt.Printf("  --include <path>        C 头文件包含路径\n")
	fmt.Printf("  --stdlib <path>         标准库路径\n")
	fmt.Printf("  --pkglib <path>         第三方库路径\n")
	fmt.Printf("  --output-dir <dir>      输出目录\n")
	fmt.Printf("  --target <lang>         目标语言 (默认 c)\n")
	fmt.Printf("  --cflags <flags>        额外的 C 编译器参数\n")
	fmt.Printf("  --defines <macros>      额外的 C 宏定义 (逗号分隔)\n")
	fmt.Printf("  --libs <libs>           额外的链接库 (逗号分隔)\n")
	fmt.Printf("  --analyze-pkg <name>    分析指定包并生成配置文件\n")
	fmt.Printf("  --analyze-pkg-all       分析所有 pkglib 中的包\n")
	fmt.Printf("  --boot <mode>           裸机引导方式：pvh/multiboot/custom/none (默认 none，需配合 --freestanding)\n")
	fmt.Printf("  --boot-file <path>      自定义引导汇编文件 (boot=custom 时使用)\n")
	fmt.Printf("  --boot-arch <arch>      引导架构：x86_64/i386/riscv64/aarch64 (默认从 --target-triple 推断)\n")
	fmt.Printf("\nConfiguration File (kaula.json):\n")
	fmt.Printf("  在项目根目录创建 kaula.json 文件配置编译参数。\n")
	fmt.Printf("  命令行参数优先级高于配置文件。\n")
	fmt.Printf("  使用 --init 生成默认配置文件。\n")
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
