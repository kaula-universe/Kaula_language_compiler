package main

import (
 "flag"
	"crypto/sha256"
	"sync/atomic"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/cache"
	"kaula/internal/codegen"
	"kaula/internal/config"
	errors "kaula/internal/errors"
	"kaula/internal/formatter"
	"kaula/internal/i18n"
	"kaula/internal/lexer"
	"kaula/internal/parser"
	"kaula/internal/pkgcmd"
	"kaula/internal/pkgmgr"
	"kaula/internal/sema"
	"kaula/internal/sor"
	"kaula/internal/stdlib"
	"kaula/internal/timeout"
	"kaula/internal/version"
	"kaula/internal/testrunner"
 "kaula/internal/workspace"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// localImportFiles 返回单个 import 语句覆盖的全部本地 .kl 文件
// 库导入（import "lib"）展开为多个文件；文件导入（import "file"）为单文件
func localImportFiles(imp *ast.ImportStatement) []string {
	if len(imp.LocalPaths) > 0 {
		return imp.LocalPaths
	}
	if imp.IsLocal && imp.LocalPath != "" {
		return []string{imp.LocalPath}
	}
	return nil
}

// precompileLocalImports 预解析本地 .kl 文件 import
// 返回: (pub 函数名集合, export 函数名集合, 合并后的 C 函数定义代码, 被导入文件内容哈希表)
// 哈希表覆盖传递闭包内所有实际读取的 .kl 文件（绝对路径 -> SHA256），
// 供增量缓存键校验使用
func precompileLocalImports(program *ast.Program, inputDir string, stdlibConfig *stdlib.StdlibConfig, cfg *config.Config, errorCollector *errors.ErrorCollector) (map[string]bool, map[string]bool, string, []string, []string, map[string]string) {
	state := &localCompileState{
		pubFuncs:       make(map[string]bool),
		exportFuncs:    make(map[string]bool),
		compiled:       make(map[string]bool),
		fileHashes:     make(map[string]string),
		localHeaderSet: make(map[string]bool),
		usedModuleSet:  make(map[string]bool),
		stdlibConfig:   stdlibConfig,
		cfg:            cfg,
		errorCollector: errorCollector,
	}

	var localFiles []string
	for _, stmt := range program.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok && importStmt.IsLocal {
			localFiles = append(localFiles, localImportFiles(importStmt)...)
		}
	}
	for _, localPath := range localFiles {
		state.compileFile(localPath, inputDir)
	}

	return state.pubFuncs, state.exportFuncs, state.allCode, state.localHeaders, state.usedModules, state.fileHashes
}

// localCompileState 共享本地 import 编译状态（去重 + 依序输出）
type localCompileState struct {
	pubFuncs       map[string]bool
	exportFuncs    map[string]bool // 跨文件 export 函数名（C 侧保持原名，不加 K_ 前缀）
	allCode        string
	localHeaders   []string        // 本地文件依赖的 std 模块头文件（注入到主 TU）
	localHeaderSet map[string]bool // 去重
	usedModules    []string        // 本地文件依赖的 std 模块（链接时合并到主 usedModules）
	usedModuleSet  map[string]bool
	compiled       map[string]bool
	fileHashes     map[string]string // 已读 .kl 文件内容哈希（绝对路径 -> SHA256）
	stdlibConfig   *stdlib.StdlibConfig
	cfg            *config.Config
	errorCollector *errors.ErrorCollector
}

// compileFile 编译单个本地 .kl 文件
// 先编译该文件自身的导入依赖（保证被调用函数先定义），再编译文件本身；
// 这样跨文件调用（A 导入 B）时 B 的 C 定义始终先于 A 输出。
func (s *localCompileState) compileFile(localPath string, inputDir string) {
	if s.compiled[localPath] {
		return
	}
	s.compiled[localPath] = true

	absPath := localPath
	if !filepath.IsAbs(absPath) {
		// 路径导入在解析阶段已解析为相对文件/工作目录的路径，
		// 这里先按输入目录尝试，失败则按 CWD 原样使用
		if candidate := filepath.Join(inputDir, absPath); fileExists(candidate) {
			absPath = candidate
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		fmt.Printf("[Multi-file] Warning: Failed to read %s: %v\n", localPath, err)
		return
	}

	// 记录内容哈希，纳入增量缓存键校验（内容变化即缓存失效）
	if abs, errAbs := filepath.Abs(absPath); errAbs == nil {
		absPath = abs
	}
	s.fileHashes[absPath] = cache.HashContent(data)

	localSource := string(data)
	s.errorCollector.SetSource(localSource)
	localLex := lexer.NewLexer(localSource)
	localLex.SetErrorCollector(s.errorCollector)
	localParser := parser.NewParser(localLex)
	localParser.SetErrorCollector(s.errorCollector)
	localParser.SetFile(absPath)
	localParser.EnableLogging(false)
	localParser.SetSkipMainCheck(true)
	errCountBefore := len(s.errorCollector.Errors())
	localProgram := localParser.Parse()

	if len(s.errorCollector.Errors()) > errCountBefore {
		fmt.Printf("[Multi-file] Parse errors in %s\n", localPath)
		for _, e := range s.errorCollector.Errors()[errCountBefore:] {
			if e != nil {
				fmt.Printf("[Multi-file]   %d:%d %s\n", e.Line, e.Column, e.Message)
			}
		}
		return
	}

	// 先编译该文件自身的依赖（嵌套本地 import / 路径库导入）
	for _, stmt := range localProgram.Statements {
		if importStmt, ok := stmt.(*ast.ImportStatement); ok && importStmt.IsLocal {
			for _, nestedPath := range localImportFiles(importStmt) {
				s.compileFile(nestedPath, filepath.Dir(absPath))
			}
		}
	}

	// 收集 pub 函数名
	for _, stmt := range localProgram.Statements {
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok && fnStmt.IsPublic {
			s.pubFuncs[fnStmt.Name] = true
		}
		// 收集 export 函数名：export 含 pub 语义（跨文件可见 + C 级导出），
		// 调用端必须保持原名（不加 K_ 前缀），与定义端命名一致。
		if fnStmt, ok := stmt.(*ast.FunctionStatement); ok && fnStmt.IsExported {
			s.exportFuncs[fnStmt.Name] = true
		}
	}

	// 语义分析（跳过 main 检查）
	localAnalyzer := sema.NewSemanticAnalyzer()
	if s.stdlibConfig != nil {
		localAnalyzer.SetStdlibConfig(s.stdlibConfig)
	}
	// 本文件依赖的跨文件 pub/export 符号也注册给本地分析器
	depDir := filepath.Dir(absPath)
	localPubTypes := collectLocalPubTypes(localProgram, depDir)
	localAnalyzer.SetLocalImportFuncs(collectLocalPubFuncs(localProgram, depDir))
	localAnalyzer.SetLocalModuleFuncs(collectLocalAllFuncs(localProgram, depDir))
	localAnalyzer.SetLocalPubVars(collectLocalPubVars(localProgram, depDir))
	localAnalyzer.SetLocalPubTypes(localPubTypes)
	localAnalyzer.SetLibraryMode(true) // 库文件: 跳过 unused-function 检查(函数可能被外部调用)
	localAnalyzer.SetSOREnabled(s.cfg.SOR)
	localAnalyzer.Analyze(localProgram)

	// 代码生成（本地文件使用 freestanding 模板，避免注入用户态/宿主入口）
	localCfg := *s.cfg
	localCfg.Boot = "none"
	localCG := codegen.NewCodeGenerator(&localCfg)
	if s.stdlibConfig != nil {
		localCG.SetStdlibConfig(s.stdlibConfig)
	}
	// 跨文件公共类型同样必须注入本地 codegen：
	// 1) 前置声明阶段按 localPubTypes 跳过引用这些类型的函数原型
	//    （typedef 在被导入文件的段中，本文件内不可见）
	// 2) 变量声明/字面量的 convertType 需要映射 K_ 前缀 C 名
	localCG.SetLocalPubTypes(localPubTypes)
	localCG.SetLocalImportReturnTypes(collectLocalPubFuncReturns(localProgram, depDir))
	localOutput := localCG.Generate(localProgram)

	// 收集本地文件依赖的 std 模块头文件（如 math/math.h、memory/memory.h），
	// 注入到主 TU 中，使库内部 import std.xxx 的声明对调用方可见。
	for _, line := range strings.Split(localOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#include") {
			continue
		}
		if strings.Contains(trimmed, "<") {
			continue
		}
		if !s.localHeaderSet[trimmed] {
			s.localHeaderSet[trimmed] = true
			s.localHeaders = append(s.localHeaders, line)
		}
	}

	// 收集本地文件依赖的 std 模块（链接阶段需要对应 .c 实现）
	for _, m := range localCG.GetUsedModules() {
		if !s.usedModuleSet[m] {
			s.usedModuleSet[m] = true
			s.usedModules = append(s.usedModules, m)
		}
	}

	// 提取函数定义（去掉 includes 和 main）
	funcCode := extractFunctionDefs(localOutput)
	if funcCode != "" {
		fmt.Printf("[Multi-file] Compiled local import: %s\n", localPath)
		s.allCode += funcCode + "\n"
	}
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

		// 注意: #include 行不再跳过——库文件用到的 std 头(如 "memory/memory.h")
		// 必须保留, 否则 kmm_v4_alloc 等函数在合并后的 C 中未声明。
		// 所有头文件都有 include guard, 重复包含无害。

		// 跳过 Windows 浮点 stub(每个生成 TU 各有一份, 提取会与主文件重复定义)
		if strings.HasPrefix(trimmed, "int _fltused") {
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

		// 检测入口函数开始（不在函数体内时）
		//
		// 不只是 int main(：裸机模板 freestanding.c.tmpl 会额外生成 _kaula_entry，
		// 本地 import 文件用同一模板生成，把整段入口带进片段后注入主 TU，
		// 结果是每个本地 import 都多一份 _kaula_entry → "redefinition of '_kaula_entry'"。
		if !inFunction && (strings.Contains(trimmed, "int main(") ||
			strings.Contains(trimmed, "void _kaula_entry(")) {
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
// 优先插入到最后一个 #include 之后（早于主文件的函数体），
// 保证主文件中任意函数（不仅是 main）调用导入函数时声明可见。
func injectLocalCode(mainCode, localCode string) string {
	// 首选：最后一个 #include 行之后（主文件函数定义之前）
	if lastInc := strings.LastIndex(mainCode, "#include"); lastInc >= 0 {
		if endOfLine := strings.Index(mainCode[lastInc:], "\n"); endOfLine >= 0 {
			insertAt := lastInc + endOfLine + 1
			return mainCode[:insertAt] + "\n" + localCode + "\n" + mainCode[insertAt:]
		}
	}

	// 兜底 1：找到 "int main" 的位置
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

// verboseMode 包级开关: ninja 风格默认安静, --verbose 显示完整过程
var verboseMode = false

// kmmMaxScopeDepth 与 KMM 头文件中的 KMM_V4_MAX_SCOPE_DEPTH 保持一致，
// 仅用于编译期诊断文案（运行期上限由 KMM 自身强制）。
const kmmMaxScopeDepth = 1024

// compilerBuildStamp 返回编译器自身的构建标识，用于让代码生成缓存随
// kaulac 二进制变化而失效。
//
// 之前缓存键只含源文件路径/模式/优化等级/池容量，**不含编译器版本**：改了
// codegen 重新构建 kaulac 后，同一份源码仍会命中旧 .c 缓存（版本号是硬编码的
// "0.1.0-alpha"），生成结果静默停留在旧代码上——开发时表现为"改了没效果"。
// 取可执行文件的 size+mtime_ns 作为构建标识：无需改构建脚本重新打戳，
// 重新构建二进制必然改变它。
func compilerBuildStamp() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	st, err := os.Stat(exe)
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("%d-%d", st.Size(), st.ModTime().UnixNano())
}

// hasTestFunctions 报告程序中是否存在带 #[test] 属性的函数。
// 单元测试模式据此拦截「无测试函数」的文件，避免退化成普通 main 静默返回 0。
func hasTestFunctions(program *ast.Program) bool {
	found := false
	if program == nil {
		return false
	}
	program.Traverse(func(n ast.Node) {
		if fn, ok := n.(*ast.FunctionStatement); ok && ast.HasAttribute(fn.Attributes, "test") {
			found = true
		}
	})
	return found
}

func main() {
 // Test subcommands own their flags and must bypass compiler flag pre-scanning.
 if len(os.Args) > 1 && os.Args[1] == "test" {
  os.Exit(runTestsCLI(os.Args[2:], false))
 }
 if len(os.Args) > 2 && os.Args[1] == "workspace" && os.Args[2] == "test" {
  os.Exit(runTestsCLI(os.Args[3:], true))
 }
 // kaulac unittest：单元测试（#[test] runner）。
 // 目标是单个已存在的 .kl 文件时走常规编译流程（下方预扫描会置 testMode）；
 // 无参、传目录或路径不存在时进入批量模式，逐个文件以子进程复用该流程。
 if len(os.Args) > 1 && os.Args[1] == "unittest" {
  rest := os.Args[2:]
  if !isSingleFileTarget(rest) {
   root := "."
   var extra []string
   for _, a := range rest {
    if strings.HasPrefix(a, "-") {
     extra = append(extra, a)
    } else {
     root = a
    }
   }
   os.Exit(runUnittestBatch(root, extra))
  }
 }
 totalStart := time.Now()

	// 初始化本地化系统，检测系统语言
	i18n.Init()

	// ninja 风格: 默认安静, --verbose 显示完整过程
	verbose := false

	// 自定义参数（非 flag 参数，需在 flag.Parse 前手动提取）
	inputFile := ""
	cleanCache := false
	purgeCache := false
	showCacheStats := false
	initConfig := false
	showVersion := false
	workspaceCmd := ""  // workspace 子命令: init, build, list
	workspaceArgs := []string{} // workspace 子命令参数
	pkgCmd := ""        // pkg 子命令: add, build, list, remove, analyze
	pkgArgs := []string{} // pkg 子命令参数
	fmtCmd := ""        // fmt 子命令: file, stdin, check
	fmtArgs := []string{} // fmt 子命令参数
	testMode := false   // 测试模式：生成 #[test] 测试 runner

	// 预扫描 os.Args，提取非 flag 参数和自定义 flag
	// 修复：-o/--output 的值不应被误认为输入文件；子命令（compile/run 等）也不是输入文件
	customArgs := []string{}
	positionalArgs := []string{}
	args := os.Args[1:]
	knownSubcommands := map[string]bool{"compile": true, "run": true, "build": true, "check": true, "test": true, "unittest": true}
	// 需要吃掉「下一个参数当值」的旗标。漏掉任何一个都会把它的值当成输入文件
	// （`--boot pvh` 曾让 pvh 变成 inputFile，boot 模式被静默忽略）。
	// 名单必须与 config.loadFlags 里注册的非 bool 旗标保持一致。
	valueFlags := map[string]bool{
		"-o": true, "--output": true,
		"-output-dir": true, "--output-dir": true,
		"-template": true, "--template": true,
		"-include": true, "--include": true,
		"-stdlib": true, "--stdlib": true,
		"-pkglib": true, "--pkglib": true,
		"-source-dir": true, "--source-dir": true,
		"-target": true, "--target": true,
		"-opt": true, "--opt": true,
		"-target-triple": true, "--target-triple": true,
		"-link-script": true, "--link-script": true,
		"-entry": true, "--entry": true,
		"-output-format": true, "--output-format": true,
		"-boot": true, "--boot": true,
		"-boot-file": true, "--boot-file": true,
		"-boot-arch": true, "--boot-arch": true,
		"-cc": true, "--cc": true,
		"-cflags": true, "--cflags": true,
		"-defines": true, "--defines": true,
		"-libs": true, "--libs": true,
		"-queue": true, "--queue": true,
		"-spendable": true, "--spendable": true,
		"-memory-limit": true, "--memory-limit": true,
		"-timeout": true, "--timeout": true,
		"-debug-level": true, "--debug-level": true,
		"-analyze-pkg": true, "--analyze-pkg": true,
		"-build-pkglib": true, "--build-pkglib": true,
	}
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
		case arg == "--version" || arg == "-version" || arg == "-v":
			// 版本旗标：输出版本信息后直接退出（放在 flag.Parse 前处理，
			// 因为 flag 包未注册该旗标，交给 flag.Parse 会报 "flag provided but not defined"）
			showVersion = true
		case arg == "workspace":
			// workspace 子命令
			if i+1 < len(args) {
				workspaceCmd = args[i+1]
				i++
				// 收集 workspace 子命令的其余参数
				for i+1 < len(args) && len(args[i+1]) > 0 && args[i+1][0] != '-' {
					workspaceArgs = append(workspaceArgs, args[i+1])
					i++
				}
			}
		case arg == "pkg":
			// pkg 子命令
			if i+1 < len(args) {
				pkgCmd = args[i+1]
				i++
				// 收集 pkg 子命令的其余参数
				for i+1 < len(args) && len(args[i+1]) > 0 && args[i+1][0] != '-' {
					pkgArgs = append(pkgArgs, args[i+1])
					i++
				}
			}
		case arg == "fmt":
			// fmt 子命令
			if i+1 < len(args) {
				fmtCmd = args[i+1]
				i++
				// 收集 fmt 子命令的其余参数
				for i+1 < len(args) && len(args[i+1]) > 0 && args[i+1][0] != '-' {
					fmtArgs = append(fmtArgs, args[i+1])
					i++
				}
			}
		case valueFlags[arg]:
			// 取值旗标：跳过其值，避免被误判为输入文件
			customArgs = append(customArgs, arg)
			if i+1 < len(args) {
				customArgs = append(customArgs, args[i+1])
				i++
			}
		default:
			if len(arg) > 0 && arg[0] != '-' {
				// 位置参数（输入文件/子命令）单独收集，稍后拼接到 flag 之后：
				// Go 的 flag.Parse 遇到第一个位置参数即停止解析，
				// 若保持原顺序，排在输入文件后的旗标（如 -output-dir）会被静默忽略
				positionalArgs = append(positionalArgs, arg)
				// 处理 test 子命令：kaulac test [file.kl]
				// 注意 test 已在 main() 开头被 testrunner 拦截并 return，
				// 真正走「生成 #[test] runner」的是 unittest 子命令。
				if arg == "test" || arg == "unittest" {
					// 单元测试模式：编译后自动运行自动生成的 #[test] runner，
					// runner 以失败测试数决定进程退出码。
					testMode = true
				} else if !knownSubcommands[arg] {
					// 只将非 flag、非子命令的参数视为输入文件候选
					inputFile = arg
				}
			} else {
				customArgs = append(customArgs, arg)
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

	// 处理 workspace 子命令
	if workspaceCmd != "" {
		switch workspaceCmd {
		case "init":
			if len(workspaceArgs) == 0 {
				fmt.Println("Usage: kaulac workspace init <member1> [member2] ...")
				fmt.Println("  Initialize a workspace with the given member directories.")
				os.Exit(1)
			}
			wd, _ := os.Getwd()
			if err := workspace.InitWorkspace(wd, workspaceArgs); err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Workspace initialized with %d members.\n", len(workspaceArgs))
			return
		case "list":
			wd, _ := os.Getwd()
			ws, err := workspace.LoadWorkspace(wd)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			ws.ListMembers()
			return
		case "build":
			wd, _ := os.Getwd()
			ws, err := workspace.LoadWorkspace(wd)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			release := false
			debug := false
			for _, arg := range workspaceArgs {
				if arg == "--release" {
					release = true
				}
				if arg == "--debug" {
					debug = true
				}
			}
			if err := ws.BuildAll(release, debug); err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			return
		default:
			fmt.Printf("Unknown workspace command: %s\n", workspaceCmd)
			fmt.Println("Available commands: init, list, build, test")
			os.Exit(1)
		}
	}

	// 处理 pkg 子命令
	if pkgCmd != "" {
		switch pkgCmd {
		case "add":
			// kaulac pkg add <git-url> [--name <name>] [--ref <branch>]
			if len(pkgArgs) == 0 {
				fmt.Println("Usage: kaulac pkg add <git-url-or-path> [--name <name>] [--ref <branch>]")
				fmt.Println("  Add a package to pkglib from a Git repository or local path.")
				fmt.Println("  Examples:")
				fmt.Println("    kaulac pkg add https://github.com/user/lib.git")
				fmt.Println("    kaulac pkg add https://github.com/user/lib.git --name mylib --ref main")
				fmt.Println("    kaulac pkg add ../local-lib --name mylib")
				os.Exit(1)
			}
			target, err := pkgcmd.NewPkgTarget("")
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			source := pkgArgs[0]
			name := ""
			ref := ""
			for i := 1; i < len(pkgArgs); i++ {
				if pkgArgs[i] == "--name" && i+1 < len(pkgArgs) {
					name = pkgArgs[i+1]
					i++
				} else if pkgArgs[i] == "--ref" && i+1 < len(pkgArgs) {
					ref = pkgArgs[i+1]
					i++
				}
			}

			var info *pkgcmd.PackageInfo
			if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") ||
				strings.HasPrefix(source, "git://") || strings.HasSuffix(source, ".git") {
				info, err = target.AddGitRepo(source, name, ref)
			} else {
				info, err = target.AddLocalPath(source, name)
			}
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("[pkg] %s installed at %s\n", info.Name, info.Dir)
			if info.HasArchive {
				fmt.Printf("[pkg]   archive: ready\n")
			}
			if info.HasKLSource {
				fmt.Printf("[pkg]   type: kaula source\n")
			} else if info.HasSources {
				fmt.Printf("[pkg]   type: c/c++ library\n")
			} else if info.HasConfig {
				fmt.Printf("[pkg]   type: header-only\n")
			}
			return

		case "build":
			// kaulac pkg build <name> [--force]
			if len(pkgArgs) == 0 {
				fmt.Println("Usage: kaulac pkg build <name> [--force]")
				fmt.Println("  Build a package in pkglib.")
				os.Exit(1)
			}
			target, err := pkgcmd.NewPkgTarget("")
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			force := false
			for _, arg := range pkgArgs {
				if arg == "--force" {
					force = true
				}
			}
			name := pkgArgs[0]
			_, err = target.Build(name, force)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			return

		case "analyze":
			// kaulac pkg analyze <name>
			if len(pkgArgs) == 0 {
				fmt.Println("Usage: kaulac pkg analyze <name>")
				fmt.Println("  Re-analyze a package and regenerate its JSON config.")
				os.Exit(1)
			}
			target, err := pkgcmd.NewPkgTarget("")
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			_, err = target.Analyze(pkgArgs[0])
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			return

		case "remove", "rm":
			// kaulac pkg remove <name>
			if len(pkgArgs) == 0 {
				fmt.Println("Usage: kaulac pkg remove <name>")
				fmt.Println("  Remove a package from pkglib.")
				os.Exit(1)
			}
			target, err := pkgcmd.NewPkgTarget("")
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			if err := target.Remove(pkgArgs[0]); err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("[pkg] %s removed\n", pkgArgs[0])
			return

		case "list", "ls":
			// kaulac pkg list
			target, err := pkgcmd.NewPkgTarget("")
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			packages, err := target.List()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			if len(packages) == 0 {
				fmt.Println("No packages installed.")
			} else {
				fmt.Printf("Installed packages (%d):\n", len(packages))
				for _, pkg := range packages {
					types := []string{}
					if pkg.HasArchive {
						types = append(types, "built")
					}
					if pkg.HasSources {
						types = append(types, "c/c++")
					}
					if pkg.HasKLSource {
						types = append(types, "kaula")
					}
					if pkg.HasConfig {
						types = append(types, "configured")
					}
					if pkg.IsGit {
						types = append(types, "git")
					}
					typeStr := "empty"
					if len(types) > 0 {
						typeStr = strings.Join(types, ", ")
					}
					fmt.Printf("  %-20s %s\n", pkg.Name, typeStr)
				}
			}
			return

		case "fetch":
			// kaulac pkg fetch - 强制联网拉取所有项目依赖
			wd, _ := os.Getwd()
			if err := pkgcmd.FetchProjectDeps(wd); err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			return

		case "update":
			// kaulac pkg update [name] - 更新依赖（全部或指定包）
			wd, _ := os.Getwd()
			if len(pkgArgs) > 0 {
				// 更新单个包
				for _, name := range pkgArgs {
					if err := pkgcmd.UpdateSingleDep(wd, name); err != nil {
						fmt.Printf("Error: %v\n", err)
						os.Exit(1)
					}
				}
			} else {
				// 更新所有
				if err := pkgcmd.UpdateProjectDeps(wd); err != nil {
					fmt.Printf("Error: %v\n", err)
					os.Exit(1)
				}
			}
			return

		case "lock":
			// kaulac pkg lock - 显示锁状态
			wd, _ := os.Getwd()
			if err := pkgcmd.ShowLockStatus(wd); err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			return

		default:
			fmt.Printf("Unknown pkg command: %s\n", pkgCmd)
			fmt.Println("Available commands: add, build, analyze, remove, list, fetch, update, lock")
			os.Exit(1)
		}
	}

	// 处理 fmt 子命令
	if fmtCmd != "" {
		switch fmtCmd {
		case "file":
			// kaulac fmt file <file.kl> [--write]
			if len(fmtArgs) == 0 {
				fmt.Println("Usage: kaulac fmt file <file.kl> [--write]")
				fmt.Println("  Format a Kaula source file.")
				fmt.Println("  Without --write, prints formatted output to stdout.")
				fmt.Println("  With --write, overwrites the file in-place.")
				os.Exit(1)
			}
			writeBack := false
			var files []string
			for _, arg := range fmtArgs {
				if arg == "--write" || arg == "-w" {
					writeBack = true
				} else {
					files = append(files, arg)
				}
			}
			if len(files) == 0 {
				fmt.Println("Error: No input files specified")
				os.Exit(1)
			}
			formatter := formatter.New()
			for _, file := range files {
				if len(file) < 3 || file[len(file)-3:] != ".kl" {
					fmt.Printf("Error: %s is not a .kl file\n", file)
					os.Exit(1)
				}
				if writeBack {
					if err := formatter.FormatFileInPlace(file); err != nil {
						fmt.Printf("Error formatting %s: %v\n", file, err)
						os.Exit(1)
					}
					fmt.Printf("Formatted %s\n", file)
				} else {
					formatted, err := formatter.FormatFile(file)
					if err != nil {
						fmt.Printf("Error formatting %s: %v\n", file, err)
						os.Exit(1)
					}
					fmt.Println(formatted)
				}
			}
			return
		case "stdin":
			// kaulac fmt stdin - format from stdin
			data, err := os.Stdin.Stat()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			if (data.Mode() & os.ModeCharDevice) != 0 {
				fmt.Println("Error: No input piped to stdin")
				fmt.Println("Usage: echo 'code' | kaulac fmt stdin")
				os.Exit(1)
			}
			var buf []byte
			tmp := make([]byte, 1024)
			for {
				n, err := os.Stdin.Read(tmp)
				if n > 0 {
					buf = append(buf, tmp[:n]...)
				}
				if err != nil {
					break
				}
			}
			formatter := formatter.New()
			formatted := formatter.FormatSource(string(buf))
			fmt.Println(formatted)
			return
		case "check":
			// kaulac fmt check <file.kl> - check if file is formatted
			if len(fmtArgs) == 0 {
				fmt.Println("Usage: kaulac fmt check <file.kl>")
				fmt.Println("  Check if a file is properly formatted.")
				fmt.Println("  Exits with 0 if formatted, 1 if not.")
				os.Exit(1)
			}
			formatter := formatter.New()
			for _, file := range fmtArgs {
				if len(file) < 3 || file[len(file)-3:] != ".kl" {
					fmt.Printf("Error: %s is not a .kl file\n", file)
					os.Exit(1)
				}
				data, err := os.ReadFile(file)
				if err != nil {
					fmt.Printf("Error reading %s: %v\n", file, err)
					os.Exit(1)
				}
				formatter.Reset()
				formatted := formatter.FormatSource(string(data))
				if string(data) != formatted {
					fmt.Printf("%s is not formatted\n", file)
					os.Exit(1)
				}
			}
			fmt.Println("All files are formatted")
			return
		default:
			fmt.Printf("Unknown fmt command: %s\n", fmtCmd)
			fmt.Println("Available commands: file, stdin, check")
			os.Exit(1)
		}
	}

	// 处理 --version / -v 命令：输出编译器版本并退出
	// 注意：不能调用 config.LoadConfig() —— 它内部会 flag.Parse()，
	// 而 --version 未在 flag 包注册，会触发 "flag provided but not defined" 退出。
	// 项目版本直接从 kaula.json 读取 "version" 字段。
	if showVersion {
		fmt.Println(version.Banner())
		if data, rerr := os.ReadFile("kaula.json"); rerr == nil {
			var projCfg struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(data, &projCfg) == nil && projCfg.Version != "" {
				fmt.Printf("project version: %s\n", projCfg.Version)
			}
		}
		return
	}

	// 无参数运行时: 先展示 banner(logo + 版本), 再输出用法
	if len(os.Args) < 2 {
		fmt.Println(version.Banner())
		fmt.Println()
		printUsage(os.Args[0])
		os.Exit(1)
	}

	// 临时修改 os.Args 以供 flag.Parse() 使用（旗标在前，位置参数在后）
	os.Args = append([]string{os.Args[0]}, append(customArgs, positionalArgs...)...)

	// 加载配置（kaula.json + 命令行 flag，会调用 flag.Parse()）
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v, using default\n", err)
	}
	// templates/ 必须能定位到：找不到 main.c.tmpl 时 codegen 会静默降级为
	// "无模板"输出（裸机入口 _kaula_entry 不生成），报错点远离根因。
	cfg.TemplatePath = resolveTemplatePath(cfg.TemplatePath)

	// 让 stdlib 加载默认优先使用 --pkglib 指定的目录
	if cfg.PkglibPath != "" {
		stdlib.SetDefaultPkglibPrefer(cfg.PkglibPath)
	}
	// 配置自愈默认开启；--skip-auto-pkg 显式关闭
	stdlib.SetAutoHealEnabled(!cfg.SkipAutoPkg)
	// ninja 风格: 默认安静; --verbose 显示库加载/构建详情
	verbose = cfg.Verbose
	verboseMode = verbose
	stdlib.SetVerboseEnabled(verbose)
	parser.SetVerboseEnabled(verbose)

	// 处理 --analyze-pkg 命令
	if cfg.AnalyzePkg != "" {
		handleAnalyzePkg(cfg.AnalyzePkg, cfg.PkglibPath)
		return
	}

	// 处理 --analyze-pkg-all 命令
	if cfg.AnalyzePkgAll {
		handleAnalyzePkgAll(cfg.PkglibPath)
		return
	}

	// 处理 --build-pkglib 命令（构建指定库或全部库后退出）
	if cfg.BuildPkglib != "" {
		handleBuildPkg(cfg.BuildPkglib, cfg.ForcePKG, cfg.PkglibPath)
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
	// C 工具链子进程临时目录的最后回退位置（与中间产物同盘同权限）
	kaulaCacheDir = cacheDir
	// F11：将影响代码生成结果的编译模式折入缓存键，
	// 防止 kaulac test 等命中普通编译缓存、复用不含测试 runner 的旧产物
	var cacheModeParts []string
	if testMode {
		cacheModeParts = append(cacheModeParts, "test")
	}
	if cfg.SOR {
		cacheModeParts = append(cacheModeParts, "sor")
	}
	if cfg.Debug {
		cacheModeParts = append(cacheModeParts, "debug")
	}
	if cfg.Freestanding {
		cacheModeParts = append(cacheModeParts, "freestanding")
	}
	cacheManager, err := cache.NewCacheManager(cacheDir, "0.1.0-alpha", strings.Join(cacheModeParts, ","))
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

	// 单元测试模式未指定输入文件：走批量模式，逐个运行发现的测试文件。
	// （`kaulac unittest` 无参已在 main() 开头进入批量；这里兜住其他潜在入口。）
	if testMode && inputFile == "" {
		os.Exit(runUnittestBatch(".", nil))
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

	if verbose {
		fmt.Println("=== Concurrent Compilation Pipeline ===")
		fmt.Printf("Starting at %v\n\n", totalStart.Format("15:04:05.000"))
	}

	errorCollector := errors.NewErrorCollector()
	errorCollector.SetSource(input)
	errorCollector.SetFile(inputBase) // 错误首行显示文件名（如 student_manager.kl）

	// 解析包依赖（kaula.json "dependencies"）: 缺失的从注册源拉取到 ~/.kaula/pkglib
	// 解析结果写入项目 kaula.lock（记录精确版本, 保证可复现构建）
	if len(cfg.Dependencies) > 0 {
		registry := cfg.Registry
		if registry == "" {
			registry = pkgmgr.DefaultRegistry
		}
		projectDir, _ := filepath.Abs(cfg.BasePath)
		patches := make(map[string]pkgmgr.PatchConfig)
		for name, p := range cfg.Patches {
			patches[name] = pkgmgr.PatchConfig{Path: p.Path}
		}
		results, lock, depErr := pkgmgr.ResolveDependencies(registry, cfg.Dependencies, patches, cfg.Offline, projectDir)
		if depErr != nil {
			fmt.Printf("Error: %v\n", depErr)
			os.Exit(1)
		}
		if saveErr := pkgmgr.SaveProjectLock(projectDir, lock); saveErr != nil {
			fmt.Printf("[pkg] warning: failed to write kaula.lock: %v\n", saveErr)
		}
		for _, r := range results {
			src := "lock"
			if r.FromPatch {
				src = "local"
			} else if !r.FromLock {
				src = "fetched"
			}
			fmt.Printf("[pkg] %s %s (%s)\n", r.Name, r.Version, src)
		}
	}

	// 并行启动：stdlib 配置加载 + 路径搜索（不依赖 Go 前端）
	parallelStart := time.Now()
	var stdlibConfig *stdlib.StdlibConfig
	var stdlibPath string
	var parallelWg sync.WaitGroup
	parallelWg.Add(1)
	go func() {
		defer parallelWg.Done()
		stdlibPath = findStdlib(cfg.StdlibPath)
		// 把解析结果写回 cfg：codegen.NewCodeGenerator 内部会自己再查一次
		// stdlib.json，而它的候选列表不含 exeDir（只有 cwd 相对路径），
		// 不回传就会查不到并打印一条误导性的「标准库函数签名将不可用」警告。
		cfg.StdlibPath = stdlibPath
		stdlibConfig, _ = stdlib.LoadStdlibConfigWithPkglib(stdlibPath, cfg.PkglibPath)
	}()

	// Stage 1: Lex + Parse（与 stdlib 加载并行）
	if verbose {
		fmt.Println(i18n.T(i18n.MsgStage1))
	}
	stage1Start := time.Now()

	lex := lexer.NewLexer(input)
	lex.SetErrorCollector(errorCollector)

	p := parser.NewParser(lex)
	p.SetErrorCollector(errorCollector)
	p.EnableLogging(verbose)
	if inputFile != "" {
		p.SetFile(inputFile)
	}
	if cfg.Freestanding || testMode {
		p.SetSkipMainCheck(true)
	}

	program := p.Parse()
	stage1Time := time.Since(stage1Start)
	if verbose { fmt.Printf(i18n.T(i18n.MsgStage1Completed)+"\n", stage1Time) }

	// 单元测试模式必须真的存在 #[test] 函数：否则 runner 不会生成，
	// 程序会退化成普通 main 并以 0 退出，把「没跑任何测试」伪装成「全部通过」。
	if testMode && !hasTestFunctions(program) {
		fmt.Fprintf(os.Stderr, "Error: %s 中没有 #[test] 函数\n", inputFile)
		fmt.Fprintln(os.Stderr, "提示: 用 fn name() { ... } 声明无参无返回值的测试函数并加上 #[test]")
		os.Exit(1)
	}

	// 保存词法分析和语法分析的错误数量
	stage1ErrorCount := len(errorCollector.Errors())

	// 等待 stdlib 配置加载完成
	parallelWg.Wait()
	parallelTime := time.Since(parallelStart)
	if verboseMode { fmt.Printf("[Parallel] stdlib config loaded in %v\n", parallelTime) }

	// Stage 2: Semantic Analysis (concurrent)
	if verbose { fmt.Println(i18n.T(i18n.MsgStage2)) }
	stage2Start := time.Now()

	localPubFuncs := collectLocalPubFuncs(program, inputDir)
	localAllFuncs := collectLocalAllFuncs(program, inputDir)
	localPubVars := collectLocalPubVars(program, inputDir)
	localPubTypes := collectLocalPubTypes(program, inputDir)
	localImportReturns := collectLocalPubFuncReturns(program, inputDir)
	concurrentSemanticAnalysisWithConfig(program, stdlibConfig, errorCollector, cfg.SOR, localPubFuncs, localAllFuncs, localPubVars, localPubTypes)
	stage2Time := time.Since(stage2Start)
	if verbose { fmt.Printf(i18n.T(i18n.MsgStage2Completed)+"\n", stage2Time) }

	// Stage 2.5: SOR Ownership Analysis (--sor)
	var sorErrors []sor.SORError
	var sorResult map[string]interface{}
	var escapeResult map[string]interface{} // 默认流程的轻量逃逸分析（--sor 时包含在 sorResult 内）
	poolCapacity := 0                         // KMM V4 池容量（0=使用默认值）
	if cfg.SOR {
		fmt.Println(i18n.T(i18n.MsgStage25))
		sorStart := time.Now()
		// 运行完整分析流水线：安全检查 + 内存决策 + 逃逸 + 大小估算 + 活跃性 + 跨函数
		fullResult := sor.AnalyzeFullFromAST(program)
		sorErrors = fullResult.SORErrors
		// 序列化结果供 CodeGen 使用
		sorResult = sor.SerializeFullAnalysisResult(fullResult)
		// 提取静态分析估算的池容量
		poolCapacity = fullResult.PoolCapacity
		sorTime := time.Since(sorStart)
		if verbose { fmt.Printf(i18n.T(i18n.MsgStage25Completed)+"\n", sorTime) }
		if os.Getenv("KAULA_SOR_DUMP") != "" {
			fmt.Println(sor.FormatDecisionsSummary(fullResult.Decisions))
			fmt.Println(sor.FormatEscapeSummary(fullResult.Escape))
			fmt.Println(sor.FormatInterProcSummary(fullResult.InterProc))
			fmt.Println(sor.FormatExternEscapeWarnings(fullResult.Warnings))
		}
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

		// extern 逃逸源浅提升警告（非阻断）：extern 返回值移入 SOR 管理域时显式告知，
		// 避免静默产生悬垂指针
		if len(fullResult.Warnings) > 0 {
			fmt.Printf("\n[SOR Extern-Escape Warnings] (%d warnings)\n", len(fullResult.Warnings))
			for i, w := range fullResult.Warnings {
				fmt.Printf("  %d. [%s] line %d: %s\n", i+1, w.Kind.String(), w.SourceLine, w.Message)
				if w.Details != "" {
					fmt.Printf("      %s\n", w.Details)
				}
			}
		}
	} else {
		// 非 SOR 模式：基于 AST 扫描估算池容量
		poolCapacity = sor.EstimatePoolCapacityFromAST(program)
		if poolCapacity > 0 {
			if verbose {
				fmt.Printf("[Stage 2.5] Pool Capacity Estimate (AST-only): %d bytes (%.2f MB)\n",
					poolCapacity, float64(poolCapacity)/(1024.0*1024.0))
			}
		}
		// 非 SOR 模式同样执行轻量逃逸分析（SOR EscapeAnalyzer，Pass 2.5c）：
		// 逃逸信息是分配路径路由的前提（作用域内使用 → alloc_auto 快速路径，
		// 逃逸 → alloc_global survivor 段）。逃逸分析是单次全程序遍历、代价很低，
		// 不跑完整 SOR 流水线（所有权检查/内存决策/跨函数等仍仅在 --sor 下执行）。
		escapeStart := time.Now()
		escapeResult = sor.SerializeEscapeResult(sor.AnalyzeEscapeFromAST(program))
		if verbose {
			fmt.Printf("[Stage 2.5] Escape Analysis: %v\n", time.Since(escapeStart))
		}
	}

	// Stage 3: Code Gen + C Compile (concurrent)
	if verbose { fmt.Println(i18n.T(i18n.MsgStage3)) }
	stage3Start := time.Now()

	codegenStart := time.Now()
	cg := codegen.NewCodeGenerator(cfg)
	cg.TestMode = testMode
	if stdlibConfig != nil {
		cg.SetStdlibConfig(stdlibConfig)
	}
	if sorResult != nil {
		cg.SetSORResult(sorResult)
	}
	// 默认流程：注入轻量逃逸分析结果，供分配路径路由（TLAB 快路径 / survivor 慢路径）使用
	if escapeResult != nil {
		cg.SetEscapeAnalysis(escapeResult)
	}
	cg.SetSourceFile(inputFile)

	// 多文件编译：预解析本地 .kl 文件 import，收集 pub/export 函数名与被导入文件内容哈希
	localImportFuncs, localImportExportFuncs, localImportCode, localHeaders, localUsedModules, importFileHashes := precompileLocalImports(program, inputDir, stdlibConfig, cfg, errorCollector)
	if len(localImportFuncs) > 0 {
		cg.SetLocalImportFuncs(localImportFuncs)
	}
	if len(localImportExportFuncs) > 0 {
		cg.SetLocalImportExportFuncs(localImportExportFuncs)
	}
	if len(localPubTypes) > 0 {
		cg.SetLocalPubTypes(localPubTypes)
	}
	cg.SetLocalImportReturnTypes(localImportReturns)

	output := cg.Generate(program)
	usedModules := cg.GetUsedModules()

	// 将本地导入的函数定义注入到主输出中（在 main 函数之前）。
	// 本地文件依赖的 std 模块头文件（库内部 import std.xxx）随函数一起注入，
	// 保证声明先于函数体可见，且与主程序自身 include 去重（头文件均有守卫）。
	if len(localHeaders) > 0 {
		localImportCode = strings.Join(localHeaders, "\n") + "\n" + localImportCode
	}
	if localImportCode != "" {
		output = injectLocalCode(output, localImportCode)
	}
	if os.Getenv("KAULAC_DUMP") != "" {
		os.WriteFile(filepath.Join(os.TempDir(), "klc_final.c"), []byte(output), 0644)
	}
	// 本地文件依赖的 std 模块实现也需要参与链接（如库内部使用 std.math/memory）
	if len(localUsedModules) > 0 {
		seen := make(map[string]bool, len(usedModules)+len(localUsedModules))
		for _, m := range usedModules {
			seen[m] = true
		}
		for _, m := range localUsedModules {
			if !seen[m] {
				seen[m] = true
				usedModules = append(usedModules, m)
			}
		}
	}

	codegenTime := time.Since(codegenStart)
	if verbose { fmt.Printf(i18n.T(i18n.MsgStage3aCompleted)+"\n", codegenTime) }

	// 检查所有阶段的诊断信息（错误与警告分开统计）
	// 统计在此计算, 输出统一放到 Generated Code 之后、Compilation Results 之前
	stage1Errs, stage1Warns := splitErrorsWarnings(errorCollector.Errors()[:stage1ErrorCount])
	stage2Errs, stage2Warns := splitErrorsWarnings(errorCollector.Errors()[stage1ErrorCount:])
	frontendWarnings := append(stage1Warns, stage2Warns...)
	// codegen 诊断并入前端警告：递归 + 每层 KMM 作用域（嵌套深度受
	// KMM_V4_MAX_SCOPE_DEPTH 约束，超出即运行期 FATAL abort）
	for _, fn := range cg.RecursionScopeNotes() {
		frontendWarnings = append(frontendWarnings, &errors.Error{
			Type:     errors.ErrorWarning,
			Code:     "recursion_scope_depth",
			File:     inputFile,
			Message:  fmt.Sprintf("函数 '%s' 递归且每层使用 KMM 作用域：嵌套深度上限 %d 层，超出即运行期 FATAL abort", fn, kmmMaxScopeDepth),
			Suggestion: "改用循环/尾调用改写；或给该函数加 #[no_kmm]（放弃作用域托管，" +
				"改用显式释放）；或用 -DKMM_V4_MAX_SCOPE_DEPTH=N 放宽",
		})
	}
	errorCount := len(stage1Errs) + len(stage2Errs) + len(cg.Errors()) + len(sorErrors)
	warningCount := len(frontendWarnings)

	// 增量编译：检查缓存
	var cacheFile string
	var cacheHit bool

	if cacheManager != nil && !cfg.NoCache {
		// 编译参数混入缓存键：optLevel 与 KMM 池容量都会改变产物，
		// 不加进键会导致 -O0/-O3 或不同池容量复用同一产物（静默吞掉参数）。
		// 同时混入编译器自身构建标识：codegen 改动后旧的 .c 缓存必须失效，
		// 否则重装 kaulac（版本号不变）会静默复用旧代码生成结果。
		// freestanding/交叉编译的目标（triple/boot/arch/cflags）也必须入键：
		// 同一份 .kl 编 x86_64 与 riscv64 走的是不同的头文件集合与模板。
		cacheManager.SetKeyExtra(fmt.Sprintf("%s|%d|%s|%t|%s|%s|%s|%s",
			optLevel, poolCapacity, compilerBuildStamp(),
			cfg.Freestanding, cfg.TargetTriple, cfg.Boot, cfg.BootArch,
			strings.Join(append(append([]string{}, cfg.CFlags...), cfg.CDefines...), ",")))
		cacheKey := cacheManager.GetCacheKey(inputFile)
		cacheFile = filepath.Join(cacheDir, cacheKey+".c")

		// 链接输入指纹：标准库/运行时/第三方库任一源码变更，缓存即失效。
		// 覆盖 importFileHashes 的盲区（pkglib 依赖、std 库、runtime 源）。
		linkFpRoots := collectLinkFingerprintRoots(cwd, cfg)
		linkFp := cache.ComputeLinkFingerprint(linkFpRoots)

		// 检查缓存是否命中（校验含被 import 文件的内容哈希与链接输入指纹）
		cacheResult := cacheManager.Check(inputFile, data, importFileHashes, linkFp)
		if cacheResult.Hit {
			cacheHit = true
			// 缓存命中: 跳过编译, 不显示任何消息
		} else {
			// 缓存未命中, 需要编译
			if verbose { fmt.Printf("[Cache] 编译中: %s\n", inputBase) }
			// 存储新生成的代码
			if err := cacheManager.Store(inputFile, data, output, usedModules, importFileHashes, linkFp); err != nil {
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
	// 前端已有错误时跳过 clang 编译（C 代码可能不完整, 错误总结在 Generated Code 后统一输出）
	var compileResult *compileResult_t
	if errorCount > 0 {
		compileResult = &compileResult_t{
			Error: fmt.Errorf(i18n.T(i18n.MsgFrontendErrorsSkipped), errorCount),
		}
	} else {
		compileResult = concurrentCompile(cacheFile, output, inputDir, inputName, cwd, usedModules, cacheHit, stdlibConfig, optLevel, poolCapacity, cfg)
	}
	stage3Time := time.Since(stage3Start)
	if verbose { fmt.Printf(i18n.T(i18n.MsgStage3Completed)+"\n", stage3Time) }

	totalTime := time.Since(totalStart)

	if verbose {
		fmt.Println("\n" + i18n.T(i18n.MsgGeneratedCode))
		fmt.Print(strings.TrimRight(output, "\n"))
		fmt.Println()
	}

	// 输出所有阶段的诊断信息（错误与警告分开统计）— 放在 Generated Code 之后、Compilation Results 之前
	if errorCount > 0 || warningCount > 0 {
		fmt.Printf("\n%s%s%s\n", errors.ColorRed, i18n.T(i18n.MsgCompilationErrorsWarnings), errors.ColorReset)

		// 输出词法分析和语法分析错误（阶段 1 的错误）
		if len(stage1Errs) > 0 {
			fmt.Printf("\n%s%s%s\n", errors.ColorRed, i18n.T(i18n.MsgLexParseErrors, len(stage1Errs)), errors.ColorReset)
			for _, err := range stage1Errs {
				fmt.Print(errors.FormatErrorWithContext(err))
			}
		}

		// 输出语义分析错误（阶段 2 新增的错误）
		if len(stage2Errs) > 0 {
			fmt.Printf("\n%s%s%s\n", errors.ColorRed, i18n.T(i18n.MsgSemanticAnalysisErrors, len(stage2Errs)), errors.ColorReset)
			for _, err := range stage2Errs {
				fmt.Print(errors.FormatErrorWithContext(err))
			}
		}

		// 输出代码生成错误
		if cg.HasErrors() {
			fmt.Printf("\n%s%s%s\n", errors.ColorRed, i18n.T(i18n.MsgCodegenErrors, len(cg.Errors())), errors.ColorReset)
			for i, err := range cg.Errors() {
				fmt.Printf("  %d. %s\n", i+1, err)
			}
		}

		// 输出 promote 跳过诊断（拷贝大小不可静态确定 → 保持借用语义而非截断拷贝）
		if notes := cg.PromoteSkipNotes(); len(notes) > 0 {
			fmt.Printf("\n[SOR promote skipped] (%d)\n", len(notes))
			seenNote := make(map[string]bool, len(notes))
			for _, note := range notes {
				if seenNote[note] {
					continue
				}
				seenNote[note] = true
				fmt.Printf("  - %s\n", note)
			}
		}

		// 输出 SOR 错误
		if len(sorErrors) > 0 {
			fmt.Printf("\n%s%s%s\n", errors.ColorRed, i18n.T(i18n.MsgSorOwnershipErrors, len(sorErrors)), errors.ColorReset)
			for i, err := range sorErrors {
				fmt.Printf("  %d. [%s] line %d: %s\n", i+1, err.Kind.String(), err.SourceLine, err.Message)
				if err.Details != "" {
					fmt.Printf("      %s\n", err.Details)
				}
			}
		}

		// 输出警告（黄色高亮）
		if warningCount > 0 {
			fmt.Printf("\n%s%s%s\n", errors.ColorYellow, i18n.T(i18n.MsgWarningsHeader, warningCount), errors.ColorReset)
			for _, err := range frontendWarnings {
				fmt.Print(errors.FormatErrorWithContext(err))
			}
		}
	}

	// 标题颜色: 成功绿 / 有警告黄 / 失败红
	titleColor := errors.ColorGreen
	if compileResult.Error != nil {
		titleColor = errors.ColorRed
	} else if warningCount > 0 {
		titleColor = errors.ColorYellow
	}
	// ninja 风格: 成功时单行紧凑输出; 失败/警告/verbose 时显示完整汇总
	if verbose || compileResult.Error != nil || warningCount > 0 {
		fmt.Printf("\n%s%s%s\n", titleColor, i18n.T(i18n.MsgCompilationResults), errors.ColorReset)
	}
	if compileResult.Error != nil {
		printSummary(false, warningCount, errorCount)
		fmt.Printf("%s: %v\n", i18n.T(i18n.MsgError), compileResult.Error)
		os.Exit(1)
	}
	printSummary(true, warningCount, errorCount)
	if !verbose {
		// ninja 风格成功输出: 一行内显示产物与耗时
		fmt.Printf("  %s: %s (%v)\n", i18n.T(i18n.MsgOutput), compileResult.OutputFile, totalTime.Round(time.Millisecond))
	} else {
		fmt.Printf("%s: %s\n", i18n.T(i18n.MsgOutput), compileResult.OutputFile)
	}

	if verbose {
		fmt.Printf("\n%s\n", i18n.T(i18n.MsgTimingBreakdown))
		fmt.Printf("Stage 1 (Lex + Parse):         %v\n", stage1Time)
		fmt.Printf("Stage 2 (Semantic):            %v\n", stage2Time)
		fmt.Printf("Stage 3 (Codegen+Compile):    %v\n", stage3Time)
		fmt.Printf("---------------------------------\n")
		fmt.Printf(i18n.T(i18n.MsgTotalTime)+"\n", totalTime)
	}

	// 测试模式：编译成功后自动运行测试
	if testMode && compileResult.Error == nil {
		fmt.Printf("\n%s===== Running Tests =====%s\n", errors.ColorGreen, errors.ColorReset)
		// 仅在产物是裸文件名时补 "./" 让 shell 能找到它。
		// 用 filepath.IsAbs 而非前缀判断：Windows 绝对路径形如 "C:\..."，
		// 不以 "/" 开头，旧的 HasPrefix 检查会给它拼出非法的 "./C:\..."。
		testBin := compileResult.OutputFile
		if !filepath.IsAbs(testBin) && !strings.HasPrefix(testBin, "./") {
			testBin = "./" + testBin
		}
		cmd := exec.Command(testBin)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Printf("Test execution failed: %v\n", err)
			os.Exit(1)
		}
	}
}

func runTestsCLI(args []string, workspaceMode bool) int {
 opts, paths, err := testrunner.Parse(args, os.Stdout)
 if err == flag.ErrHelp { return 0 }
 if err == nil && workspaceMode {
  if len(paths) > 0 { err = fmt.Errorf("workspace test does not accept a path") } else {
   var cwd string
   cwd, err = os.Getwd()
   if err == nil {
    var ws *workspace.Workspace
    ws, err = workspace.LoadWorkspace(cwd)
    if err == nil { err = ws.TestAll(opts) }
   }
  }
 } else if err == nil {
  _, err = testrunner.Run(paths, opts)
 }
 if err != nil { fmt.Fprintf(os.Stderr, "Error: %v\n", err); return 1 }
 return 0
}

// printSummary 输出编译汇总行
func printSummary(success bool, warnCount, errCount int) {
	statusText := i18n.T(i18n.MsgSuccess)
	statusColor := errors.ColorGreen
	if !success {
		statusText = i18n.T(i18n.MsgFailed)
		statusColor = errors.ColorRed
	}
	warnColor := errors.ColorGreen
	if warnCount > 0 {
		warnColor = errors.ColorYellow
	}
	errColor := errors.ColorGreen
	if errCount > 0 {
		errColor = errors.ColorRed
	}
	fmt.Printf("%s: %s%s%s, %s%s%s, %s%s%s\n",
		i18n.T(i18n.MsgStatus),
		statusColor, statusText, errors.ColorReset,
		warnColor, i18n.T(i18n.MsgWarningsCount, warnCount), errors.ColorReset,
		errColor, i18n.T(i18n.MsgErrorsCount, errCount), errors.ColorReset)
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

// archDefaultCFlags 返回裸机目标缺失时必须补上的架构参数（仅当用户没显式给）。
//
// riscv64：clang 的 riscv64 默认是 rv64imac + 软浮点 ABI，f64 运算会被 lower 成
// __adddf3/__ledf2/__fixdfsi 等 libcall；裸机没有 compiler-rt，链接期必炸。
// 固定 rv64gc + lp64d（硬件 F/D，QEMU virt 与绝大多数开发板都支持）后零 libcall。
// 用户自己传了 -march/-mabi 就完全不干预（要软浮点内核自己加回去）。
func archDefaultCFlags(arch string, userFlags []string) []string {
	has := func(prefix string) bool {
		for _, f := range userFlags {
			if strings.HasPrefix(f, prefix) {
				return true
			}
		}
		return false
	}
	switch arch {
	case "riscv64":
		var out []string
		if !has("-march=") {
			out = append(out, "-march=rv64gc")
		}
		if !has("-mabi=") {
			out = append(out, "-mabi=lp64d")
		}
		return out
	}
	return nil
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

// pkglibCandidateRoots 返回所有可能承载第三方库的目录（存在才列出）。
// 与 stdlib.LoadStdlibConfigWithPkglib 的搜索路径保持一致：
// --pkglib 指定目录、可执行文件旁 pkglib/、工作区及上级 pkglib/、~/.kaula/pkglib。
func pkglibCandidateRoots(workDir string, cfg *config.Config) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			roots = append(roots, abs)
		}
	}
	if cfg != nil {
		add(cfg.PkglibPath)
	}
	if exePath, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(filepath.Clean(exePath)), "pkglib"))
	}
	add(filepath.Join(workDir, "pkglib"))
	add(filepath.Join(workDir, "..", "pkglib"))
	if home, herr := os.UserHomeDir(); herr == nil {
		add(filepath.Join(home, ".kaula", "pkglib"))
	}
	return roots
}

// collectLinkFingerprintRoots 返回链接输入指纹覆盖的根集合：
// std 源目录、kaula src、freestanding、runtime 源文件、pkglib 库目录。
// 任一文件变更都会使依赖它们的缓存条目失效。
func collectLinkFingerprintRoots(workDir string, cfg *config.Config) []string {
	var roots []string
	seen := map[string]bool{}
	addDir := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			roots = append(roots, abs)
		}
	}
	addFile := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		if st, err := os.Stat(abs); err == nil && st.Mode().IsRegular() {
			roots = append(roots, abs)
		}
	}

	// std / src / freestanding 源目录（workDir 及其上级，与 compileCCode 搜索路径一致）
	for _, d := range []string{
		filepath.Join(workDir, "std"),
		filepath.Join(workDir, "..", "std"),
		filepath.Join(workDir, "src"),
		filepath.Join(workDir, "..", "src"),
		filepath.Join(workDir, "freestanding"),
		filepath.Join(workDir, "..", "freestanding"),
	} {
		addDir(d)
	}

	// runtime 源文件（kaula.h、kmm 分配器、spend_call、allocator），
	// 位于 kaula 根目录；findBootKaulaSrcPath 返回 kaula根/src。
	if src := findBootKaulaSrcPath(workDir); src != "" {
		kaulaRoot := filepath.Dir(src)
		for _, f := range []string{
			"kaula.h",
			"kmm_scoped_allocator_v4.c",
			"spend_call.c",
			"allocator.c",
		} {
			addFile(filepath.Join(kaulaRoot, f))
		}
	}

	// pkglib 第三方库目录
	for _, p := range pkglibCandidateRoots(workDir, cfg) {
		addDir(p)
	}

	return roots
}

// linkInputMtimeRoots 返回 exe 快路径校验的链接输入根：
// std 模块对象缓存目录（std .o + runtime .o）与 pkglib 库目录（.a）。
func linkInputMtimeRoots(workDir string, cfg *config.Config) []string {
	roots := []string{filepath.Join(workDir, "cache", "std-objects")}
	roots = append(roots, pkglibCandidateRoots(workDir, cfg)...)
	return roots
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

// effectiveTargetTriple 返回本次构建实际使用的目标三元组。
// 托管模式留空 = 用 clang 宿主默认；裸机模式未显式指定时按 boot 架构推导
// <arch>-none-elf（这也是非 boot 裸机链路能直接产出裸机 ELF 的前提）。
func effectiveTargetTriple(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.TargetTriple != "" {
		return cfg.TargetTriple
	}
	if cfg.Freestanding {
		return resolveBootArch(cfg) + "-none-elf"
	}
	return ""
}

// cacheTargetKey 目标指纹：决定 .o 二进制内容的全部输入
// （目标三元组 / 托管或裸机 / 优化级别 / 池容量 / 用户 cflags+cdefines）。
// 指纹进缓存目录名，保证不同目标之间零复用。
func cacheTargetKey(cfg *config.Config, optLevel string, poolCapacity int) string {
	mode := "host"
	target := ""
	var extra []string
	if cfg != nil {
		if cfg.Freestanding {
			mode = "fs"
		}
		target = effectiveTargetTriple(cfg)
		extra = append(extra, cfg.CFlags...)
		extra = append(extra, cfg.CDefines...)
	}
	if target == "" {
		target = "host"
	}
	key := fmt.Sprintf("%s|%s|%s|%d|%s", mode, target, optLevel, poolCapacity, strings.Join(extra, ","))
	sum := sha256.Sum256([]byte(key))
	// 前缀保留可读性（ls 一眼能看出是哪个目标的缓存），后缀保证唯一
	return sanitizeCacheKey(mode+"-"+target+"-"+optLevel) + "-" + hex.EncodeToString(sum[:6])
}

// atomicObjectPath 为并行构建生成一个独占的目标文件路径。
//
// 缓存目录（cache/std-objects/<目标指纹>/）是跨进程共享的：并行构建（CI 的
// 多任务、测试 runner 的多线程、make -j）时，两个进程同时写同一个 .o 会让
// 对方链到一个写了一半的文件 —— 表现是随机的 undefined symbol / 坏 ELF，
// 单独重跑又好了，极难定位。写临时文件再 rename 可保证读者只看到完整产物。
func atomicObjectPath(obj string) string {
	return fmt.Sprintf("%s.tmp-%d-%d", obj, os.Getpid(), atomic.AddUint64(&objCounter, 1))
}

var objCounter uint64

// compileToObjectAtomic 编译到 objPath（先写临时文件再原子替换）
func compileToObjectAtomic(cmd *exec.Cmd, objPath string) (string, error) {
	tmp := atomicObjectPath(objPath)
	// 把 -o <objPath> 换成 -o <tmp>
	replaced := false
	for i := 0; i+1 < len(cmd.Args); i++ {
		if cmd.Args[i] == "-o" {
			cmd.Args[i+1] = tmp
			replaced = true
			break
		}
	}
	if !replaced {
		return "", fmt.Errorf("internal: compile command has no -o argument")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmp)
		return string(out), err
	}
	if err := renameWithRetry(tmp, objPath); err != nil {
		os.Remove(tmp)
		return string(out), err
	}
	return string(out), nil
}

// renameWithRetry 原子替换 .o。Windows 上目标文件可能正被另一个并行构建的
// 链接步骤打开，MoveFileEx 会返回 ERROR_SHARING_VIOLATION/ACCESS_DENIED；
// 退避重试几次即可（链接是毫秒级的）。
func renameWithRetry(from, to string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(time.Duration(20*(i+1)) * time.Millisecond)
	}
	return err
}

// sanitizeCacheKey 把 arch/triple 变成安全的目录名（去掉路径分隔符与冒号）
func sanitizeCacheKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
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
	// kernelObj 也必须按架构+引导方式分目录：cacheFile 只按 (源码, optLevel, 池容量,
	// 编译器构建标识) 命名，不含目标三元组，同一份 .c 编 x86_64 与 riscv64
	// 会得到同名 .o，链接期报 "xxx.o is incompatible with elf_x86_64"。
	// 引导方式也要进键：x86_64 的 pvh 与 multiboot 共用同一 arch+triple，但 boot stub
	// 不同（.note.Xen vs .multiboot 头）。共用 boot.o 时并行构建会互相覆盖，
	// 产物里可能带着另一种引导方式的头 —— QEMU 报 "Error loading uncompressed
	// kernel without PVH ELF Note"，或多字节引导头缺失直接不启动。
	archCache := filepath.Join(workCache, "boot-"+sanitizeCacheKey(arch+"-"+triple+"-"+cfg.Boot))
	os.MkdirAll(archCache, 0755)
	kernelObj := filepath.Join(archCache, base+".o")
	// boot.o / runtime.o / kmm.o 是与目标架构强相关（不同 arch 的 ELF 机器类型不同），
	// 必须按 arch+triple 分目录：多架构并行构建时共用固定文件名会互相覆盖，
	// 链接期报 "xxx.o is incompatible with elf_i386"。
	bootObj := filepath.Join(archCache, "boot.o")
	runtimeObj := filepath.Join(archCache, "kaula_freestanding_runtime.o")

	// 公共编译参数
	baseArgs := append([]string{"-target", triple, "-c", optLevel}, archDefaultCFlags(arch, cfg.CFlags)...)

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
	// 添加用户自定义的 C 编译器参数
	for _, flag := range cfg.CFlags {
		kernelArgs = append(kernelArgs, flag)
	}
	for _, define := range cfg.CDefines {
		kernelArgs = append(kernelArgs, "-D"+define)
	}
	kernelArgs = append(kernelArgs, cacheFile, "-o", kernelObj)
	fmt.Printf("[Boot] Compiling kernel C -> %s\n", kernelObj)
	clangCmd := compilerCommand(clangPath, kernelArgs...)
	if out, err := clangCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kernel C compilation failed: %v\n%s", err, string(out))
	}

	// 2. 编译 boot stub
	// 注意：用户自定义 C 参数必须一并传给 boot stub。此前只有 kernel/runtime/alloc
	// 三个 TU 带了 cfg.CFlags，stub 被漏掉：riscv64 上 kernel 用 -mabi=lp64d 而
	// stub 仍是软浮点，链接期报 "cannot link object files with different floating-point ABI"。
	bootArgs := append(append([]string{}, baseArgs...), bootSrc, "-o", bootObj)
	for _, flag := range cfg.CFlags {
		bootArgs = append(bootArgs, flag)
	}
	for _, define := range cfg.CDefines {
		bootArgs = append(bootArgs, "-D"+define)
	}
	fmt.Printf("[Boot] Compiling boot stub -> %s\n", bootObj)
	bootCmd := compilerCommand(clangPath, bootArgs...)
	if out, err := bootCmd.CombinedOutput(); err != nil {
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
				// -DKMM_V4_STATIC_POOL 必须与 KMM 分配器 TU（步骤 3.5）一致：
				// runtime 现在也 include kmm_scoped_allocator_v4.h（为 std.memory
				// 转发 kmm_v4_alloc 等符号），KMM 配置不一致时头里的内联路径会
				// 编出动态池专用符号（kmm_v4_survivor_paged_free / g_kmm_garbage），
				// 而静态池的 KMM .o 里根本没有它们 → 链接期 undefined symbol。
				"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL", "-I", kaulaSrcPath,
			)
			if kaulaFreePath != "" {
				runtimeArgs = append(runtimeArgs, "-I", kaulaFreePath)
			}
			runtimeArgs = append(runtimeArgs, archCodeModel(arch)...)
			// 添加用户自定义的 C 编译器参数
			for _, flag := range cfg.CFlags {
				runtimeArgs = append(runtimeArgs, flag)
			}
			for _, define := range cfg.CDefines {
				runtimeArgs = append(runtimeArgs, "-D"+define)
			}
			runtimeArgs = append(runtimeArgs, runtimeSrc, "-o", runtimeObj)
			fmt.Printf("[Boot] Compiling freestanding runtime -> %s\n", runtimeObj)
			rtCmd := compilerCommand(clangPath, runtimeArgs...)
	if out, err := rtCmd.CombinedOutput(); err != nil {
				fmt.Printf("[Boot] Warning: runtime compilation failed: %v\n%s\n", err, string(out))
			} else if _, err := os.Stat(runtimeObj); err == nil {
				linkObjs = append(linkObjs, runtimeObj)
			}
		}
	}

	// 3.5 编译 KMM V4 分配器（静态池模式，提供堆分配/作用域回收）
	allocObj := filepath.Join(archCache, "kmm_scoped_allocator_v4.o")
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
			// 添加用户自定义的 C 编译器参数
			for _, flag := range cfg.CFlags {
				allocArgs = append(allocArgs, flag)
			}
			for _, define := range cfg.CDefines {
				allocArgs = append(allocArgs, "-D"+define)
			}
			allocArgs = append(allocArgs, allocSrc, "-o", allocObj)
			fmt.Printf("[Boot] Compiling KMM allocator -> %s\n", allocObj)
			allocCmd := compilerCommand(clangPath, allocArgs...)
	if out, err := allocCmd.CombinedOutput(); err != nil {
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
	ldCmd := compilerCommand(ldPath, ldArgs...)
	if out, err := ldCmd.CombinedOutput(); err != nil {
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
	// 同 compileBootKernel：与架构强相关的中间产物按 arch+triple 分目录
	archCache := filepath.Join(workCache, "boot-"+sanitizeCacheKey(arch+"-"+triple+"-"+cfg.Boot))
	os.MkdirAll(archCache, 0755)
	progObj := filepath.Join(archCache, base+".o")
	runtimeObj := filepath.Join(archCache, "kaula_freestanding_runtime.o")
	allocObj := filepath.Join(archCache, "kmm_scoped_allocator_v4.o")

	baseArgs := append([]string{"-target", triple, "-c", optLevel}, archDefaultCFlags(arch, cfg.CFlags)...)

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
	// 添加用户自定义的 C 编译器参数
	for _, flag := range cfg.CFlags {
		progArgs = append(progArgs, flag)
	}
	for _, define := range cfg.CDefines {
		progArgs = append(progArgs, "-D"+define)
	}
	progArgs = append(progArgs, cacheFile, "-o", progObj)
	fmt.Printf("[User] Compiling program C -> %s\n", progObj)
	progCmd := compilerCommand(clangPath, progArgs...)
	if out, err := progCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("program C compilation failed: %v\n%s", err, string(out))
	}

	linkObjs := []string{progObj}

	if kaulaSrcPath != "" {
		runtimeSrc := filepath.Join(kaulaSrcPath, "kaula_freestanding_runtime.c")
		if _, err := os.Stat(runtimeSrc); err == nil {
			runtimeArgs := append(append([]string{}, baseArgs...),
				"-ffreestanding", "-nostdlib", "-fno-pic",
				// -DKMM_V4_STATIC_POOL 必须与 KMM 分配器 TU（步骤 3.5）一致：
				// runtime 现在也 include kmm_scoped_allocator_v4.h（为 std.memory
				// 转发 kmm_v4_alloc 等符号），KMM 配置不一致时头里的内联路径会
				// 编出动态池专用符号（kmm_v4_survivor_paged_free / g_kmm_garbage），
				// 而静态池的 KMM .o 里根本没有它们 → 链接期 undefined symbol。
				"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL", "-I", kaulaSrcPath,
			)
			if kaulaFreePath != "" {
				runtimeArgs = append(runtimeArgs, "-I", kaulaFreePath)
			}
			runtimeArgs = append(runtimeArgs, archCodeModel(arch)...)
			// 添加用户自定义的 C 编译器参数
			for _, flag := range cfg.CFlags {
				runtimeArgs = append(runtimeArgs, flag)
			}
			for _, define := range cfg.CDefines {
				runtimeArgs = append(runtimeArgs, "-D"+define)
			}
			runtimeArgs = append(runtimeArgs, runtimeSrc, "-o", runtimeObj)
			rtCmd := compilerCommand(clangPath, runtimeArgs...)
	if out, err := rtCmd.CombinedOutput(); err != nil {
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
			// 添加用户自定义的 C 编译器参数
			for _, flag := range cfg.CFlags {
				allocArgs = append(allocArgs, flag)
			}
			for _, define := range cfg.CDefines {
				allocArgs = append(allocArgs, "-D"+define)
			}
			allocArgs = append(allocArgs, allocSrc, "-o", allocObj)
			allocCmd := compilerCommand(clangPath, allocArgs...)
	if out, err := allocCmd.CombinedOutput(); err != nil {
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
	ldCmd := compilerCommand(ldPath, ldArgs...)
	if out, err := ldCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("user program linking failed: %v\n%s", err, string(out))
	}
	return nil
}

// concurrentCompile 并发保存缓存并编译 C 代码
func concurrentCompile(cacheFile, cCode, inputDir, inputName, workDir string, usedModules []string, cacheHit bool, stdlibConfig *stdlib.StdlibConfig, optLevel string, poolCapacity int, cfg *config.Config) *compileResult_t {
	result := &compileResult_t{}
	startTime := time.Now()

	// 缓存文件由调用方写入（cacheManager.Store 或无缓存模式的 os.WriteFile），
	// 这里不再重复写；cacheHit=false 时 cacheFile 一定是已就绪的完整内容，
	// clang（boot 模式）读取它不存在"写一半"竞态。

	// 输出目录：指定了 -output-dir 则输出到该目录（不存在则创建），
	// 否则保持原行为（落输入目录）
	outDir := inputDir
	if cfg.OutputDir != "" {
		outDir = cfg.OutputDir
		if err := os.MkdirAll(outDir, 0755); err != nil {
			result.Error = fmt.Errorf("failed to create output directory %s: %w", outDir, err)
			return result
		}
	}

	outputExe := filepath.Join(outDir, inputName+".exe")
	if runtime.GOOS != "windows" {
		outputExe = filepath.Join(outDir, inputName)
	}
	if isBootMode(cfg) {
		// 裸机引导模式：输出可引导 ELF（或 raw bin）
		ext := ".elf"
		if cfg.OutputFormat == "bin" {
			ext = ".bin"
		}
		outputExe = filepath.Join(outDir, inputName+ext)
	} else if cfg != nil && cfg.Freestanding {
		// 裸机（无 boot）：产物同样是裸机 ELF，用 ld.lld 直接链接（见 compileCCode
		// 的 freestanding 分支）—— 不能走 clang 默认链路：交叉到 *-none-elf 时
		// clang 会用 gcc 当链接驱动，宿主机上没有交叉 gcc 就直接失败。
		ext := ".elf"
		if cfg.OutputFormat == "bin" {
			ext = ".bin"
		}
		outputExe = filepath.Join(outDir, inputName+ext)
	}

	// 缓存命中且 exe 已存在: 跳过 clang 编译, 直接复用 (大幅提速)
	if cacheHit {
		if exeInfo, exeErr := os.Stat(outputExe); exeErr == nil {
			if cInfo, cErr := os.Stat(cacheFile); cErr == nil && !exeInfo.ModTime().Before(cInfo.ModTime()) {
				// 链接输入校验：exe 必须比所有 std .o / pkglib .a 都新。
				// 否则 std 库已重编（源变更、指纹失效但 exe 快照仍存在）
				// 或 pkglib 归档更新时，不能复用旧 exe。
				if !exeInfo.ModTime().Before(cache.MaxLinkInputMtime(linkInputMtimeRoots(workDir, cfg))) {
					result.OutputFile = outputExe
					return result
				}
			}
		}
		// exe 缺失或比缓存/链接输入旧: 回退到重新编译
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
		return result
	}

	result.OutputFile = outputExe

	if result.Error == nil {
		if cacheHit {
			// 缓存命中: 跳过编译, 静默 (不显示 cache 消息)
		} else {
			if verboseMode { fmt.Printf("[Compile] Completed in %v\n", time.Since(startTime)) }
		}
	}

	return result
}

// concurrentSemanticAnalysis 执行语义分析
// 注：原名带 "concurrent" 前缀，但实现是"单 goroutine + 立即 Wait"的假并发
// （并发度恒为 1），已在清理并发误导时改为直接同步调用。
func concurrentSemanticAnalysis(program *ast.Program, stdlibPath string, errorCollector *errors.ErrorCollector, sorEnabled bool) *semaResult_t {
	result := &semaResult_t{ErrorCollector: errorCollector}

	sa := sema.NewSemanticAnalyzerWithConfig(stdlibPath, result.ErrorCollector)
	sa.SetSOREnabled(sorEnabled)
	sa.Analyze(program)

	return result
}

// concurrentSemanticAnalysisWithConfig 执行语义分析（使用已加载的配置）
// 注：原实现为假并发（单 goroutine + 立即 Wait），已改为直接同步调用。
func concurrentSemanticAnalysisWithConfig(program *ast.Program, stdlibConfig *stdlib.StdlibConfig, errorCollector *errors.ErrorCollector, 	sorEnabled bool, localPubFuncs map[string]bool, localAllFuncs map[string]bool, localPubVars map[string]bool, localPubTypes map[string]*sema.LocalPubTypeInfo) *semaResult_t {
	result := &semaResult_t{ErrorCollector: errorCollector}

	sa := sema.NewSemanticAnalyzerWithConfig("kaula/stdlib.json", errorCollector)
	if stdlibConfig != nil {
		sa.SetStdlibConfig(stdlibConfig)
	}
	sa.SetLocalImportFuncs(localPubFuncs)
	sa.SetLocalModuleFuncs(localAllFuncs)
	sa.SetLocalPubVars(localPubVars)
	sa.SetLocalPubTypes(localPubTypes)
	sa.SetSOREnabled(sorEnabled)
	sa.Analyze(program)

	return result
}

// collectLocalPubFuncs 扫描本地 import 的 .kl 文件，收集 pub 函数名
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// collectLocalPubFuncs 扫描本地 import 的 .kl 文件，收集 pub/export 函数名
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
			for _, localPath := range localImportFiles(imp) {
				absPath := localPath
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
				localParser.SetFile(absPath)
				localParser.SetSkipMainCheck(true)
				localParser.EnableLogging(false)
				localProgram := localParser.Parse()
				if localParser.HasErrors() {
					continue
				}
				for _, s := range localProgram.Statements {
					if fn, ok := s.(*ast.FunctionStatement); ok && (fn.IsPublic || fn.IsExported) {
						pubFuncs[fn.Name] = true
					}
					// export <name> 裸名导出：转发导出本文件 import 的符号（重导出）
					if exp, ok := s.(*ast.ExportStatement); ok && exp.Name != "" {
						pubFuncs[exp.Name] = true
					}
				}
				visit(localProgram, filepath.Dir(absPath))
			}
		}
	}

	visit(program, inputDir)
	return pubFuncs
}

// collectLocalPubFuncReturns 扫描本地 import 的 .kl 文件，收集 pub/export
// 函数的返回类型（函数名 -> Kaula 返回类型）。主程序的 AST 不含被导入文件
// 的 FunctionStatement，codegen 侧 inferType 对此类调用查不到返回类型，
// 导致 `html + kw_shell_js()` 这类 string 拼接被误判为 int 走 concat_int。
func collectLocalPubFuncReturns(program *ast.Program, inputDir string) map[string]string {
	returns := make(map[string]string)

	var visit func(p *ast.Program, dir string)
	visit = func(p *ast.Program, dir string) {
		for _, stmt := range p.Statements {
			imp, ok := stmt.(*ast.ImportStatement)
			if !ok || !imp.IsLocal {
				continue
			}
			for _, localPath := range localImportFiles(imp) {
				absPath := localPath
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
				localParser.SetFile(absPath)
				localParser.SetSkipMainCheck(true)
				localParser.EnableLogging(false)
				localProgram := localParser.Parse()
				if localParser.HasErrors() {
					continue
				}
				for _, s := range localProgram.Statements {
					if fn, ok := s.(*ast.FunctionStatement); ok && (fn.IsPublic || fn.IsExported) && fn.ReturnType != "" {
						returns[fn.Name] = fn.ReturnType
					}
					if exp, ok := s.(*ast.ExportStatement); ok && exp.Name != "" {
						// export <name> 导出的函数：返回类型在相邻定义中，此处跳过，
						// 由同名 FunctionStatement 分支覆盖
						_ = exp
					}
				}
				visit(localProgram, filepath.Dir(absPath))
			}
		}
	}

	visit(program, inputDir)
	return returns
}

// collectLocalAllFuncs 扫描本地 import 的 .kl 文件, 收集全部函数名(含非 pub)
// 用于调用检查: 当代码调用了被 import 模块但非 pub/export 的函数时, 给"未导出"错误
func collectLocalAllFuncs(program *ast.Program, inputDir string) map[string]bool {
	allFuncs := make(map[string]bool)

	var visit func(p *ast.Program, dir string)
	visit = func(p *ast.Program, dir string) {
		for _, stmt := range p.Statements {
			imp, ok := stmt.(*ast.ImportStatement)
			if !ok || !imp.IsLocal {
				continue
			}
			for _, localPath := range localImportFiles(imp) {
				absPath := localPath
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
				localParser.SetFile(absPath)
				localParser.SetSkipMainCheck(true)
				localParser.EnableLogging(false)
				localProgram := localParser.Parse()
				if localParser.HasErrors() {
					continue
				}
				for _, s := range localProgram.Statements {
					if fn, ok := s.(*ast.FunctionStatement); ok {
						allFuncs[fn.Name] = true
					}
					if exp, ok := s.(*ast.ExportStatement); ok && exp.Name != "" {
						allFuncs[exp.Name] = true
					}
				}
				visit(localProgram, filepath.Dir(absPath))
			}
		}
	}

	visit(program, inputDir)
	return allFuncs
}



// collectLocalPubVars 扫描本地 import 的 .kl 文件，收集 pub/export 变量名
// （跨文件变量引用不被判为未定义）
func collectLocalPubVars(program *ast.Program, inputDir string) map[string]bool {
	pubVars := make(map[string]bool)

	var visit func(p *ast.Program, dir string)
	visit = func(p *ast.Program, dir string) {
		for _, stmt := range p.Statements {
			imp, ok := stmt.(*ast.ImportStatement)
			if !ok || !imp.IsLocal {
				continue
			}
			for _, localPath := range localImportFiles(imp) {
				absPath := localPath
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
				localParser.SetFile(absPath)
				localParser.SetSkipMainCheck(true)
				localParser.EnableLogging(false)
				localProgram := localParser.Parse()
				if localParser.HasErrors() {
					continue
				}
				for _, s := range localProgram.Statements {
					if vd, ok := s.(*ast.VariableDeclaration); ok && (vd.IsPublic || vd.IsExported) {
						// 单语句多变量声明（pub i64 a = 1, b = 2）的每个声明符都导出
						for _, d := range vd.AllDeclarators() {
							pubVars[d.Name] = true
						}
					}
					// export <name> 裸名导出也可能指向变量，一并注册
					if exp, ok := s.(*ast.ExportStatement); ok && exp.Name != "" {
						pubVars[exp.Name] = true
					}
				}
				visit(localProgram, filepath.Dir(absPath))
			}
		}
	}

	visit(program, inputDir)
	return pubVars
}

// collectLocalPubTypes 扫描本地 import 的 .kl 文件，收集导出的类型定义
// （class/struct/enum/interface/type 别名），供跨文件类型引用。
// class 含完整字段与方法签名（classFieldType/memberObjectClassName 等需要），
// struct/enum/interface/type 只需注册名称即可参与 isTypeValid / C 类型映射。
func collectLocalPubTypes(program *ast.Program, inputDir string) map[string]*sema.LocalPubTypeInfo {
	types := make(map[string]*sema.LocalPubTypeInfo)

	var visit func(p *ast.Program, dir string)
	visit = func(p *ast.Program, dir string) {
		for _, stmt := range p.Statements {
			imp, ok := stmt.(*ast.ImportStatement)
			if !ok || !imp.IsLocal {
				continue
			}
			for _, localPath := range localImportFiles(imp) {
				absPath := localPath
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
				localParser.SetFile(absPath)
				localParser.SetSkipMainCheck(true)
				localParser.EnableLogging(false)
				localProgram := localParser.Parse()
				if localParser.HasErrors() {
					continue
				}
				for _, s := range localProgram.Statements {
					switch t := s.(type) {
					case *ast.ClassStatement:
						types[t.Name] = &sema.LocalPubTypeInfo{Kind: "class", Class: t}
					case *ast.StructStatement:
						types[t.Name] = &sema.LocalPubTypeInfo{Kind: "struct"}
					case *ast.EnumStatement:
						types[t.Name] = &sema.LocalPubTypeInfo{Kind: "enum"}
					case *ast.InterfaceStatement:
						types[t.Name] = &sema.LocalPubTypeInfo{Kind: "interface"}
					case *ast.TypeAliasStatement:
						types[t.Name] = &sema.LocalPubTypeInfo{Kind: "type"}
					}
				}
				visit(localProgram, filepath.Dir(absPath))
			}
		}
	}

	visit(program, inputDir)
	return types
}

type semaResult_t struct {
	*errors.ErrorCollector
}

// splitErrorsWarnings 将诊断按严重级别拆分为错误与警告
func splitErrorsWarnings(list []*errors.Error) (errs, warns []*errors.Error) {
	for _, e := range list {
		if e == nil {
			continue
		}
		if e.Type == errors.ErrorWarning {
			warns = append(warns, e)
		} else {
			errs = append(errs, e)
		}
	}
	return errs, warns
}

func (s *semaResult_t) HasErrors() bool {
	return s.ErrorCollector.HasErrors()
}

func findStdlib(overridePath string) string {
	// 0. F3 修复：优先使用 --stdlib 命令行参数指定的路径
	if overridePath != "" {
		if _, err := os.Stat(overridePath); err == nil {
			return overridePath
		}
		// 如果指定的路径不存在，打印警告但继续 fallback
		fmt.Fprintf(os.Stderr, "[WARN] --stdlib 指定的路径不存在: %s，回退到自动查找\n", overridePath)
	}

	// 收集候选路径。按「谁最可能指对」排序：KAULA_HOME > exe 目录 > 工作目录。
	// 三处都要覆盖多种实际布局：
	//   仓库内开发：<root>/compiler/stdlib.json
	//   构建输出：  <root>/build/bin/{kaulac.exe,stdlib.json}（与 exe 同目录）
	//   安装布局：  <prefix>/bin/kaulac.exe + <prefix>/lib|share/kaula/stdlib.json
	//   就地运行：  cwd 下有 compiler/stdlib.json 或 stdlib.json
	//
	// 原先的列表漏掉了 KAULA_HOME 下的 bin/ 与 share/、exe 上溯两级、
	// 以及 cwd 下的 ./compiler/stdlib.json——把 kaulac.exe 单独拷到别处
	// （模拟安装、或 CI 里只缓存二进制）就完全找不到 stdlib.json。
	var candidates []string
	add := func(base string, subs ...string) {
		if base == "" {
			return
		}
		for _, s := range subs {
			candidates = append(candidates, filepath.Join(base, filepath.FromSlash(s)))
		}
	}

	// 1. KAULA_HOME 环境变量
	add(os.Getenv("KAULA_HOME"),
		"compiler/stdlib.json", // 仓库根
		"stdlib.json",          // KAULA_HOME 直接指向 stdlib.json 所在目录
		"bin/stdlib.json",
		"lib/stdlib.json",
		"share/kaula/stdlib.json",
		"../compiler/stdlib.json",
	)

	// 2. 可执行文件目录（及其上溯两级）
	exeDir := ""
	if exePath, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(filepath.Clean(exePath))
	}
	add(exeDir, "stdlib.json", "compiler/stdlib.json")
	add(filepath.Dir(exeDir), // <root>/build
		"stdlib.json", "compiler/stdlib.json", "bin/stdlib.json", "lib/stdlib.json")
	add(filepath.Join(filepath.Dir(exeDir), ".."), // <root>
		"compiler/stdlib.json", "stdlib.json", "bin/stdlib.json",
		"lib/stdlib.json", "share/kaula/stdlib.json")

	// 3. 工作目录相对路径
	add(".",
		"stdlib.json", "compiler/stdlib.json", "kaula/stdlib.json",
		"kaula/compiler/stdlib.json", "build/bin/stdlib.json")
	add("..", "stdlib.json", "compiler/stdlib.json")
	add(filepath.Join("..", ".."), "compiler/stdlib.json", "stdlib.json")

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 一个都没找到：明确说出来。找不到时 LoadStdlibConfigWithPkglib 静默返回
	// 空配置，用户看到的报错是 "cannot deduce type for variable 'x'"，
	// 与真实原因（标准库签名表没加载）毫无关系。
	// 诊断里用绝对路径并去重，否则同一路径会重复出现好几次。
	seen := make(map[string]bool, len(candidates))
	var tried []string
	for _, p := range candidates {
		abs := p
		if a, err := filepath.Abs(p); err == nil {
			abs = a
		}
		if !seen[abs] {
			seen[abs] = true
			tried = append(tried, abs)
		}
	}
	cwd := "(未知)"
	if d, err := os.Getwd(); err == nil {
		cwd = d
	}
	if exeDir == "" {
		exeDir = "(未知)"
	}
	fmt.Fprintf(os.Stderr, "[ERROR] 找不到 stdlib.json（标准库函数签名表），标准库函数将不可用。\n")
	fmt.Fprintf(os.Stderr, "        已尝试以下 %d 个路径：\n", len(tried))
	for _, p := range tried {
		fmt.Fprintf(os.Stderr, "          %s\n", p)
	}
	fmt.Fprintf(os.Stderr,
		"        KAULA_HOME=%q\n        可执行文件目录=%s\n        工作目录=%s\n"+
			"        请用 --stdlib <路径> 显式指定，或把 KAULA_HOME 设为包含 stdlib.json 的目录。\n",
		os.Getenv("KAULA_HOME"), exeDir, cwd)

	return "stdlib.json"
}

// pickKaulaRoot 在候选目录里挑 kaula 根目录，两轮匹配：
//
//	第 1 轮: <c>/src/kaula.h            —— 源码布局（.c 源码齐全）
//	第 2 轮: <c>/include/kaula/kaula.h  —— 安装布局（只有头文件）
//
// 必须源码布局优先。仓库里 build/ 目录含 include/kaula/kaula.h，而
// build/bin/kaulac.exe 的 exeDir/.. 正好是 build/：逐候选「先 src 再 include」
// 的单轮匹配会选中 build/，随后 kaulaSrcPath = build/src（空目录）——
// 裸机模式需要的 kaula_freestanding_runtime.c / kmm_scoped_allocator_v4.c
// 全部找不到，链接期报 calloc/memcpy 一堆 undefined symbol，根因却完全
// 不在错误信息里。
func pickKaulaRoot(candidates []string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "src", "kaula.h")); err == nil {
			return c
		}
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "include", "kaula", "kaula.h")); err == nil {
			return c
		}
	}
	return ""
}

// resolveTemplatePath 定位 templates/ 目录（main.c.tmpl / freestanding.c.tmpl /
// boot/*.S / linker/*.ld）。
//
// config 的默认值是相对路径 "templates"，只有当 cwd 恰好是 compiler/ 时才存在。
// 从仓库根（templates 在 compiler/templates）或从 build/bin（templates 在
// ../../compiler/templates）调用时会静默找不到模板：codegen 的 GetTemplate 失败
// 后走"无模板"分支，裸机入口 _kaula_entry 根本不会生成，链接期才报
// undefined symbol: _kaula_entry —— 报错点离根因极远。
// 这里按 KAULA_HOME → 可执行文件 → cwd 的顺序补齐查找，与 findStdlib 同构。
func resolveTemplatePath(configured string) string {
	hasTemplate := func(dir string) bool {
		if dir == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(dir, "main.c.tmpl"))
		return err == nil
	}

	if hasTemplate(configured) {
		return configured
	}
	if configured != "" {
		fmt.Fprintf(os.Stderr, "[WARN] --template 指定的目录没有 main.c.tmpl: %s，回退到自动查找\n", configured)
	}

	var candidates []string
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		candidates = append(candidates,
			filepath.Join(envHome, "compiler", "templates"),
			filepath.Join(envHome, "templates"))
	}
	workDir, _ := os.Getwd()
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates = append(candidates,
			filepath.Join(exeDir, "templates"),                        // compiler/ 布局
			filepath.Join(exeDir, "compiler", "templates"),            // kaula 根布局
			filepath.Join(exeDir, "..", "compiler", "templates"),      // build/bin 布局
			filepath.Join(exeDir, "..", "templates"))
	}
	candidates = append(candidates,
		filepath.Join(workDir, "compiler", "templates"),
		filepath.Join(workDir, "templates"),
		filepath.Join(workDir, "..", "compiler", "templates"),
		filepath.Join(workDir, "..", "templates"))
	for _, c := range candidates {
		if hasTemplate(c) {
			return c
		}
	}
	return configured
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
	// 选择 C 编译器：优先使用 -cc 显式指定的编译器（可为 clang2gcc 翻译器、gcc 等），
	// 否则按名查找 clang（原有行为）。
	var clangPath string
	if cfg != nil && cfg.CCompiler != "" {
		if strings.ContainsAny(cfg.CCompiler, `/\`) {
			// 含路径分隔符：视为可执行文件路径直接使用
			if _, err := os.Stat(cfg.CCompiler); err != nil {
				return fmt.Errorf("specified C compiler not found: %s", cfg.CCompiler)
			}
			clangPath = cfg.CCompiler
		} else {
			p, err := exec.LookPath(cfg.CCompiler)
			if err != nil {
				return fmt.Errorf("C compiler %q not found in PATH", cfg.CCompiler)
			}
			clangPath = p
		}
	} else {
		p, err := exec.LookPath("clang")
		if err != nil {
			return fmt.Errorf("clang not found in PATH (use -cc to specify a C compiler, e.g. the clang2gcc translator or gcc)")
		}
		clangPath = p
	}

	// ccache 集成：如果 ccache 可用，用它包装 clang 以缓存编译结果（大幅提升重复编译速度）
	compilerCmd := clangPath
	compilerPrefix := []string{}
	if ccachePath, err := exec.LookPath("ccache"); err == nil {
		compilerCmd = ccachePath
		compilerPrefix = []string{clangPath}
	}

	// 查找 kaula.h 所在的目录（优先级：KAULA_HOME > 可执行文件路径 > 相对路径）
	var kaulaRoot string

	// 1. 环境变量 KAULA_HOME
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		kaulaRoot = pickKaulaRoot([]string{envHome})
	}

	// 2. 从可执行文件路径推断（kaulac 通常在 compiler/ 或 build/bin/ 下）
	if kaulaRoot == "" {
		if exePath, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(filepath.Clean(exePath))
			kaulaRoot = pickKaulaRoot([]string{
				filepath.Join(exeDir, ".."),       // compiler/ 的上级 = kaula 根
				filepath.Join(exeDir, "..", ".."), // build/bin/ 的上两级 = kaula 根
				exeDir,                            // 可执行文件所在目录就是 kaula 根
			})
		}
	}

	// 3. 从工作目录相对路径回溯
	if kaulaRoot == "" {
		kaulaRoot = pickKaulaRoot([]string{
			workDir,
			filepath.Join(workDir, ".."),
			filepath.Join(workDir, "..", ".."),
			filepath.Join(workDir, "..", "..", ".."),
			filepath.Join(workDir, "..", "..", "..", "kaula"),
		})
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

	// 裸机模式下 freestanding runtime 的 unity 编译产物（5 模块合一的 .o）
	// 与 fsRuntimeCoversLib：该 .o 已包含全部 freestanding 模块符号，
	// 再链接 fs_*.o 会得到 duplicate symbol。
	var fsRuntimeObj string
	fsRuntimeCoversLib := false
	// 裸机链接要带上的预编译对象（runtime + KMM 分配器）
	var fsLinkObjs []string

	// 裸机标志：模块 .o / runtime .o / KMM .o 都要按裸机目标编译并与最终链接一致
	isFreestanding := cfg != nil && cfg.Freestanding

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

	// 裸机 libc 头替身目录（freestanding/include/）：只放 freestanding 模式下
	// 用来顶替宿主 SDK 头的最小声明集（stdio.h / sys/types.h ...）。
	// 放在 -I 里即可压过系统头，且只在本函数内拼进 clangArgs —— 托管模式不碰。
	var validCompatPaths []string
	if isFreestanding {
		compatRoots := []string{
			filepath.Join(kaulaRoot, "freestanding", "include"),
			filepath.Join(workDir, "freestanding", "include"),
			filepath.Join(workDir, "..", "freestanding", "include"),
		}
		for _, p := range compatRoots {
			if _, err := os.Stat(p); err == nil {
				validCompatPaths = append(validCompatPaths, p)
				break
			}
		}
	}

	// 预编译对象缓存目录（PCH + std .o 共用）
	//
	// 必须按"目标指纹"分子目录。.o 的机器类型 / 浮点 ABI / 宏集全由目标决定：
	// 同一份 .c 编 x86_64 与 riscv64 产物完全不同，托管版与裸机版更是连
	// 预处理路径都不同（-DKAULA_FREESTANDING / -DKMM_V4_STATIC_POOL）。
	// 共用一个目录时，交叉编译会静默复用上一个目标的 .o，报错要等到链接期
	// 才以 "xxx.o is incompatible with elf_i386" 的形式出现。
	objectCacheDir := filepath.Join(workDir, "cache", "std-objects", cacheTargetKey(cfg, optLevel, poolCapacity))
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
		pchCmd := compilerCommand(clangPath, "-x", "c-header", "-c", filepath.Join(kaulaSrcPath, "kaula.h"), "-o", pchPath, optLevel)
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
			if verboseMode { fmt.Printf("[Compile] PCH generated: %s\n", pchPath) }
		}
	} else if kaulaSrcPath != "" {
		if verboseMode { fmt.Printf("[Compile] PCH cache hit: %s\n", pchPath) }
	}

	clangArgs := []string{"-x", "c", "-", "-o", outputFile, optLevel}
	// 编译速度优化：使用管道而非临时文件，减少 I/O 开销
	clangArgs = append(clangArgs, "-pipe")
	// 调试符号：--debug 启用 DWARF 生成
	if cfg != nil && cfg.Debug {
		clangArgs = append(clangArgs, "-g")
		if cfg.DebugLevel == "full" {
			clangArgs = append(clangArgs, "-gdwarf-4")
		}
	}
	clangArgs = append(clangArgs, "-I", workDir)
	clangArgs = append(clangArgs, "-fwrapv", "-fno-strict-aliasing")
	clangArgs = append(clangArgs, "-DKMM_THREAD_SAFETY_LEVEL=1")
	// 链接器优化：Windows 强制 lld（MSVC ABI 兼容性）；Linux/macOS 优先 lld（更快），不可用时回退默认
	if runtime.GOOS == "windows" {
		clangArgs = append(clangArgs, "-fuse-ld=lld")
		// Windows 目标下 clang 对栈帧 >4KB 的函数默认生成 __chkstk 调用，
		// 而本编译器不链接 CRT，导致 ld.lld 报 undefined symbol: __chkstk。
		// 把探测阈值设到极大值即可避免（程序为单线程、栈充足，安全）。
		clangArgs = append(clangArgs, "-mstack-probe-size=2147483647")
	} else if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// 检测 lld 是否可用，可用则使用（比默认 ld 快 2-5 倍）
		if _, err := exec.LookPath("ld.lld"); err == nil {
			clangArgs = append(clangArgs, "-fuse-ld=lld")
		}
	}
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

		// 裸机模式只用 freestanding 模块，std.* 一律不编译不链接。
		//
		// std 模块是托管代码：std/concurrent 要 <sys/types.h>，std/io 要 <stdio.h>，
		// std/web 要 socket 头 —— 交叉到 *-none-elf 后宿主 SDK 根本不在搜索路径里，
		// 编译期就报 "'sys/types.h' file not found"。就算能编过，产物也是调用 libc
		// 的托管代码，链进裸机 ELF 没有意义（链接期必然缺一堆 libc 符号）。
		// freestanding 库本身由 runtime unity TU 一次编译全部 5 个模块（见下），
		// 这里跳过 std 模块不会削弱对 freestanding 库的编译校验。
		if isFreestanding && !isFreeModule {
			if verboseMode { fmt.Printf("[Compile] freestanding: skipping hosted std module %s\n", moduleName) }
			continue
		}

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

	// 去重：searchPaths 可能包含同一实际目录的多个挂载路径，
	// 导致同一模块 .c 被重复加入。按 objPath 去重（objPath 基于模块名，与搜索路径无关）。
	seenObj := make(map[string]bool)
	var uniqueSources []moduleSource
	for _, ms := range moduleSources {
		if !seenObj[ms.objPath] {
			seenObj[ms.objPath] = true
			uniqueSources = append(uniqueSources, ms)
		}
	}
	moduleSources = uniqueSources

	// 预编译需要更新的 std 模块
	rebuildCount := 0
	for _, ms := range moduleSources {
		if ms.needsRebuild {
			rebuildCount++
		}
	}

	if rebuildCount > 0 {
		if verboseMode { fmt.Printf("[Compile] Pre-compiling %d std module(s)...\n", rebuildCount) }
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

				compileCmd := compilerCommand(compilerCmd, append(compilerPrefix, "-c", optLevel, cPath, "-o", objPath)...)
				if runtime.GOOS == "windows" {
					compileCmd.Args = append(compileCmd.Args, "-mstack-probe-size=2147483647")
				}
				// 裸机：模块 .o 必须与最终链接同目标（机器类型 / 浮点 ABI /
				// 静态池宏）。少了 -target 就会编出 COFF 目标文件，链接 ELF 时报
				// "xxx.o is incompatible with elf_x86_64"。
				if isFreestanding {
					compileCmd.Args = append(compileCmd.Args,
						"-target", effectiveTargetTriple(cfg),
						"-ffreestanding", "-fno-pic",
						"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL")
					compileCmd.Args = append(compileCmd.Args, archCodeModel(resolveBootArch(cfg))...)
					compileCmd.Args = append(compileCmd.Args, archDefaultCFlags(resolveBootArch(cfg), cfg.CFlags)...)
					for _, flag := range cfg.CFlags {
						compileCmd.Args = append(compileCmd.Args, flag)
					}
					for _, define := range cfg.CDefines {
						compileCmd.Args = append(compileCmd.Args, "-D"+define)
					}
				}
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
				if output, err := compileToObjectAtomic(compileCmd, objPath); err != nil {
					errMu.Lock()
					// 含完整命令行 / cwd / 临时目录：std 模块是并发编译的，
					// 缺少上下文时"unable to make temporary file"这类报错无法定位
					rebuildErrors = append(rebuildErrors, formatCommandFailure(
						"  "+filepath.Base(cPath), compileCmd, err, []byte(output)))
					errMu.Unlock()
				}
			}(ms.cPath, ms.objPath)
		}
		wg.Wait()

		if len(rebuildErrors) > 0 {
			return fmt.Errorf("std module pre-compilation failed:\n%s", strings.Join(rebuildErrors, "\n"))
		}
		if verboseMode { fmt.Printf("[Compile] Std modules pre-compiled (%d updated, %d cached)\n", rebuildCount, len(moduleSources)-rebuildCount) }
	} else {
		if verboseMode { fmt.Printf("[Compile] All %d std modules cached, skipping pre-compilation\n", len(moduleSources)) }
	}

	// 裸机模式：预先单独编译 freestanding runtime（5 模块 unity build）成 .o。
	//
	// 必须单独编译、且 freestanding 的 -I 排在 std 之前：
	// runtime 通过 #include "memory/memory.c" 等 unity-include freestanding 库，
	// 而生成代码那次调用的 -I 顺序是 src → std → freestanding，short 形式的 include
	// 会先命中 std/memory/memory.c（依赖 libc <string.h>，-nostdlib 下直接失败），
	// std/io/io.c 还会把 <stdio.h> 拉进来（hosted 语义，裸机不存在）。
	// boot 路径（compileBootKernel）本来就单独编译，顺序正确。
	//
	// 成功后 fsRuntimeCoversLib=true：下面的 moduleSources（fs_*.o）不再链接，
	// 否则同一批 fs_* 符号会被定义两次。
	if cfg != nil && cfg.Freestanding {
		// 裸机默认参数（如 riscv64 的 rv64gc/lp64d，见 archDefaultCFlags）
		clangArgs = append(clangArgs, archDefaultCFlags(resolveBootArch(cfg), cfg.CFlags)...)
		clangArgs = append(clangArgs, archCodeModel(resolveBootArch(cfg))...)
		runtimeSrc := filepath.Join(kaulaSrcPath, "kaula_freestanding_runtime.c")
		if _, err := os.Stat(runtimeSrc); err == nil {
			rtObj := filepath.Join(objectCacheDir, "kaula_fs_runtime.o")
			rtArgs := append([]string{}, compilerPrefix...)
			rtArgs = append(rtArgs, "-c", optLevel, "-ffreestanding", "-nostdlib",
				"-fno-pic", "-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL")
			// 与最终链接同目标：机器类型 / 浮点 ABI 必须一致
			rtArgs = append(rtArgs, "-target", effectiveTargetTriple(cfg))
			rtArgs = append(rtArgs, archCodeModel(resolveBootArch(cfg))...)
			rtArgs = append(rtArgs, archDefaultCFlags(resolveBootArch(cfg), cfg.CFlags)...)
			for _, p := range validSrcPaths {
				rtArgs = append(rtArgs, "-I", p)
			}
			// 关键：freestanding 目录必须排在 std 之前
			for _, p := range validFreePaths {
				rtArgs = append(rtArgs, "-I", p)
			}
			for _, p := range validStdPaths {
				rtArgs = append(rtArgs, "-I", p)
			}
			if poolCapacity > 0 {
				rtArgs = append(rtArgs, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
			}
			for _, flag := range cfg.CFlags {
				rtArgs = append(rtArgs, flag)
			}
			for _, define := range cfg.CDefines {
				rtArgs = append(rtArgs, "-D"+define)
			}
			rtArgs = append(rtArgs, runtimeSrc, "-o", rtObj)
			if runtime.GOOS == "windows" {
				rtArgs = append(rtArgs, "-mstack-probe-size=2147483647")
			}
			if out, err := compileToObjectAtomic(compilerCommand(compilerCmd, rtArgs...), rtObj); err != nil {
				return fmt.Errorf("freestanding runtime compilation failed: %v\n%s", err, out)
			}
			fsRuntimeObj = rtObj
			fsRuntimeCoversLib = true
			if verboseMode { fmt.Printf("[Compile] Freestanding runtime -> %s\n", rtObj) }
		} else {
			return fmt.Errorf("freestanding runtime not found: %s\n"+
				"裸机模式需要 Kaula 源码布局（含 src/ 的完整安装），当前只找到头文件布局。\n"+
				"请设置 KAULA_HOME 指向仓库根目录，或在源码树内直接运行 kaulac。", runtimeSrc)
		}
	}

	// 非安装模式下需要链接的运行时对象（kmm_v4：KMM 分配器；spend_call：强制消费流；allocator：fast_alloc 系列符号）
	// 裸机模式用 _fs 后缀的独立产物：KMM 的静态池/无 libc 变体与托管版不是同一个 .o，
	// 共用缓存路径会互相覆盖（先编裸机再跑普通程序，就会链接到没有 calloc 的版本）。
	runtimeObjs := []string{"kmm_v4.o", "spend_call.o", "allocator.o"}
	if isFreestanding {
		// 裸机只需要 KMM（见下方循环的跳过逻辑）
		runtimeObjs = []string{"kmm_v4_fs.o"}
	}

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
			// 裸机只需要 KMM 分配器：spend_call.c / allocator.c 依赖 libc
			//（fwrite/abort），在 -nostdlib 下必然编译失败 —— boot 路径同样不编它们。
			if isFreestanding && rs.cName != "kmm_scoped_allocator_v4.c" {
				continue
			}
			oName := rs.oName
			if isFreestanding {
				oName = strings.TrimSuffix(rs.oName, ".o") + "_fs.o"
			}
			rsSrc := filepath.Join(kaulaSrcPath, rs.cName)
			rsObj := filepath.Join(objectCacheDir, oName)
			needsRebuild := true
			if cInfo, cErr := os.Stat(rsSrc); cErr == nil {
				if oInfo, oErr := os.Stat(rsObj); oErr == nil {
					if oInfo.ModTime().After(cInfo.ModTime()) || oInfo.ModTime().Equal(cInfo.ModTime()) {
						needsRebuild = false
					}
				}
			}
			if needsRebuild {
				rsCmd := compilerCommand(compilerCmd, append(compilerPrefix, "-c", optLevel, rsSrc, "-o", rsObj)...)
				if runtime.GOOS == "windows" {
					rsCmd.Args = append(rsCmd.Args, "-mstack-probe-size=2147483647")
				}
				for _, p := range validSrcPaths {
					rsCmd.Args = append(rsCmd.Args, "-I", p)
				}
				for _, p := range validStdPaths {
					rsCmd.Args = append(rsCmd.Args, "-I", p)
				}
				for _, p := range validFreePaths {
					rsCmd.Args = append(rsCmd.Args, "-I", p)
				}
				if isFreestanding {
					// 静态池 + 无 libc + 同目标：与 boot 路径 (compileBootKernel 3.5) 保持一致
					rsCmd.Args = append(rsCmd.Args, "-ffreestanding", "-nostdlib", "-fno-pic",
						"-DKAULA_FREESTANDING", "-DKMM_V4_STATIC_POOL",
						"-target", effectiveTargetTriple(cfg))
					rsCmd.Args = append(rsCmd.Args, archCodeModel(resolveBootArch(cfg))...)
					rsCmd.Args = append(rsCmd.Args, archDefaultCFlags(resolveBootArch(cfg), cfg.CFlags)...)
					for _, flag := range cfg.CFlags {
						rsCmd.Args = append(rsCmd.Args, flag)
					}
					for _, define := range cfg.CDefines {
						rsCmd.Args = append(rsCmd.Args, "-D"+define)
					}
				}
				if poolCapacity > 0 {
					rsCmd.Args = append(rsCmd.Args, fmt.Sprintf("-DKMM_V4_POOL_SIZE=%d", poolCapacity))
				}
				if output, err := compileToObjectAtomic(rsCmd, rsObj); err != nil {
					fmt.Printf("[Compile] Warning: %s compilation failed: %v\n%s\n", oName, err, output)
				} else {
					fmt.Printf("[Compile] %s compiled\n", oName)
				}
			}
		}
	}

	// 使用预编译的 .o 文件链接，而不是重新编译 .c 文件
	// 注意：必须在 .o 文件前用 -x none 重置语言类型，否则前面的 -x c 会让 clang 把 .o 当作 C 源码
	// 安装模式下 std/freestanding 模块符号已在 kaula_std.lib / kaula_freestanding.lib 中，
	// 不再单独链接 .o，否则与静态库中的同名符号产生 LNK2005 重复定义。
	// 裸机模式下在此链接 moduleSources（下方 else 分支仅处理非裸机）
	directLink := isFreestanding && cfg.Boot == "" // 裸机无 boot：走 ld.lld 直连，.o 不进 clangArgs
	if !directLink {
		if fsRuntimeCoversLib && fsRuntimeObj != "" {
			clangArgs = append(clangArgs, "-x", "none", fsRuntimeObj)
		} else if len(moduleSources) > 0 && !useInstalledLibraries && cfg != nil && cfg.Freestanding {
			clangArgs = append(clangArgs, "-x", "none")
			for _, ms := range moduleSources {
				clangArgs = append(clangArgs, ms.objPath)
			}
		}
		// KMM 分配器等运行时对象：托管模式在下方 std.lib 分支里链接，裸机模式必须在这里补上
		// （否则 g_kmm_v4_survivor / g_kmm_v4_scope_stack / g_kmm_v4_thread_heap 无定义）。
		if isFreestanding && !useInstalledLibraries {
			clangArgs = append(clangArgs, "-x", "none")
			for _, objName := range runtimeObjs {
				runtimeObj := filepath.Join(objectCacheDir, objName)
				if _, err := os.Stat(runtimeObj); err == nil {
					clangArgs = append(clangArgs, runtimeObj)
				}
			}
		}
	} else {
		// ld.lld 直连：收集要链的预编译对象
		if fsRuntimeCoversLib && fsRuntimeObj != "" {
			fsLinkObjs = append(fsLinkObjs, fsRuntimeObj)
		} else {
			for _, ms := range moduleSources {
				fsLinkObjs = append(fsLinkObjs, ms.objPath)
			}
		}
		for _, objName := range runtimeObjs {
			runtimeObj := filepath.Join(objectCacheDir, objName)
			if _, err := os.Stat(runtimeObj); err == nil {
				fsLinkObjs = append(fsLinkObjs, runtimeObj)
			}
		}
	}

	// 合并所有 std .o 为单个 std.lib（减少链接器处理的文件数）
	// 裸机模式下跳过 std.lib（使用 -nostdlib，不需要标准库）
	// Windows 下直接链接 .o 文件，避免 llvm-lib 产生的 COFF 库损坏
	if cfg == nil || !cfg.Freestanding {
		if useInstalledLibraries {
			clangArgs = append(clangArgs,
				"-x", "none",
				filepath.Join(installedRoot, "lib", stdLibraryName),
				filepath.Join(installedRoot, "lib", runtimeLibraryName),
			)
			if verboseMode { fmt.Printf("[Compile] Using installed static libraries: %s, %s\n", stdLibraryName, runtimeLibraryName) }
			// 使用 freestanding 模块时链接 libkaula_freestanding.a（安装模式下）
			for _, mod := range usedModules {
				if strings.HasPrefix(mod, "freestanding.") {
					freeLibPath := filepath.Join(installedRoot, "lib", "libkaula_freestanding.a")
					if runtime.GOOS == "windows" {
						freeLibPath = filepath.Join(installedRoot, "lib", "kaula_freestanding.lib")
					}
					if _, err := os.Stat(freeLibPath); err == nil {
						clangArgs = append(clangArgs, "-x", "none", freeLibPath)
						if verboseMode { fmt.Printf("[Compile] Linked installed freestanding library: %s\n", freeLibPath) }
					}
					break
				}
			}
		} else {
			// Windows 下直接链接所有 .o 文件，避免 llvm-lib 问题
			// 同样需要 -x none 重置语言：本分支可能在无 std 模块源（如仅用
			// 第三方库）时执行，此时前面的 -x c 会把 .o 当作 C 源码编译失败
			if verboseMode { fmt.Printf("[Compile] Linking %d .o files directly\n", len(moduleSources)+len(runtimeObjs)) }
			clangArgs = append(clangArgs, "-x", "none")
			for _, ms := range moduleSources {
				clangArgs = append(clangArgs, ms.objPath)
			}
			for _, objName := range runtimeObjs {
				runtimeObj := filepath.Join(objectCacheDir, objName)
				if _, err := os.Stat(runtimeObj); err == nil {
					clangArgs = append(clangArgs, runtimeObj)
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
		var extraLibs []string // 自动构建/桥接产生的额外链接库（如 stdc++）
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
			// 编译时显示第三方库版本（pkglib 配置的 "version" 字段，可为空）
			if lib.Version != "" {
				fmt.Printf("[Pkg] %s %s\n", lib.Name, lib.Version)
			}
			// 解析库目录（含回退），并支持按需自动构建（增强：从源码构建缺失/过期的静态库）
			libForce := cfg != nil && cfg.ForcePKG
			resolvedLibDir := lib.LibraryPath
			if resolvedLibDir == "" || !dirExists(resolvedLibDir) {
				fallbackDir := filepath.Join(workDir, "pkglib", lib.Name)
				if dirExists(fallbackDir) {
					resolvedLibDir = fallbackDir
				}
			}
			if resolvedLibDir != "" && (cfg == nil || !cfg.SkipAutoPkg) {
				// 先完整构建：库编译产物（lib<name>.a）优先就绪。
				// 构建只依赖源码扫描，无需函数签名；解析（提取调用签名、
				// 生成桥接）在后执行，避免"解析先行"在签名提取失败时
				// 连构建都被跳过。
				result, bErr := ensureLibrary(resolvedLibDir, libForce)
				if bErr != nil {
					fmt.Printf("[Warn] Auto-build failed for %s: %v (continuing)\n", lib.Name, bErr)
				} else if result != nil && result.HasLibraries {
					// 构建产生的额外链接库（如 C++ 运行时 stdc++）稍后与 -L 一起追加，
					// 保证 -L 出现在 -l 之前（ld 按顺序解析搜索路径）
					for _, extra := range result.Libraries {
						already := false
						for _, l := range lib.Libraries {
							if l == extra {
								already = true
								break
							}
						}
						if !already {
							extraLibs = append(extraLibs, extra)
						}
					}
				}
				// 再解析：配置过期时重新分析（生成桥接/新签名），
				// 并合并旧配置里的人工库列表，
				// 避免重新分析丢掉人工链接项；缺失配置的库由导入阶段的按需分析自动处理。
				if stdlib.ConfigStale(resolvedLibDir, lib.Name) {
					fmt.Printf("[Info] Config for %s is stale, re-analyzing...\n", lib.Name)
					if aRes, aErr := stdlib.AnalyzePackage(resolvedLibDir); aErr == nil {
						// 合并旧配置的人工链接库/额外目录，防止自动分析丢项
						if merged, mErr := stdlib.MergeLibrariesInto(resolvedLibDir, aRes); mErr == nil {
							lib = *merged
						} else {
							fmt.Printf("[Warn] Merge config for %s failed: %v\n", lib.Name, mErr)
						}
					} else {
						fmt.Printf("[Warn] Re-analyze %s failed: %v\n", lib.Name, aErr)
					}
				}
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
			if resolvedLibDir != "" {
				libPath := resolvedLibDir
				if _, err := os.Stat(libPath); err != nil {
					fallbackLib := filepath.Join(workDir, "pkglib", lib.Name)
					if _, fbErr := os.Stat(fallbackLib); fbErr == nil {
						libPath = fallbackLib
					}
				}
				clangArgs = append(clangArgs, "-L", libPath)
			}
			// 添加链接库（先追加构建产物产生的额外库，再追加配置声明的库）
			for _, extra := range extraLibs {
				clangArgs = append(clangArgs, "-l"+extra)
			}
			extraLibs = extraLibs[:0]
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
			// 目标三元组：未显式指定时按 boot 架构推导 <arch>-none-elf。
			// 不推导就会退回 clang 的宿主默认（Windows 上 = x86_64-pc-windows-msvc），
			// 产物是 PE、模块 .o 也是 COFF：既不能引导，也没法和交叉目标共用缓存。
			// 推导后 L2 那种"四架构各自零依赖 ELF"对非 boot 链路同样成立。
			clangArgs = append(clangArgs, "-target", effectiveTargetTriple(cfg))
			// 裸机 libc 头替身（freestanding/include）：std.* 的头文件仍会 include
			// <stdio.h> / <sys/types.h>，而交叉目标没有宿主 SDK。加在 -I 里即可
			// 压过系统头（-I 永远先于系统目录），托管模式不受影响。
			for _, p := range validCompatPaths {
				clangArgs = append(clangArgs, "-I", p)
			}
			// 裸机入口是 _kaula_entry（freestanding.c.tmpl），裸机 ELF 链接器不会
			// 自动找它，必须显式 -e。
			if cfg.Entry == "" {
				clangArgs = append(clangArgs, "-Wl,-e,_kaula_entry")
			}
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

	// ccache 集成：使用 ccache 包装 clang（如果可用）
	mainArgs := append(compilerPrefix, clangArgs...)
	cmd := compilerCommand(compilerCmd, mainArgs...)

	// 通过 stdin pipe 将 C 代码传递给 clang（内存编译，避免磁盘 I/O）
	cSource := cCodeInMemory
	if cSource == "" {
		data, err := os.ReadFile(cFile)
		if err != nil {
			return fmt.Errorf("failed to read C source file %s: %v", cFile, err)
		}
		cSource = string(data)
	}

	// 调试辅助：KAULA_DUMP_C=<path> 时把生成的 C 落盘。
	// 生成的 C 默认只经 stdin 传给 clang，崩溃类问题（access violation）没有任何
	// 可读现场，定位只能靠反复二分 .kl 源码。
	if dump := os.Getenv("KAULA_DUMP_C"); dump != "" {
		if err := os.WriteFile(dump, []byte(cSource), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] KAULA_DUMP_C 写入失败: %v\n", err)
		} else if verboseMode {
			fmt.Printf("[Compile] C source dumped: %s\n", dump)
		}
	}

	// 裸机（无 boot）：clang 只负责编译成 .o，链接交给 ld.lld 直连。
	//
	// 不能走 clang 默认链接：交叉到 *-none-elf 时 clang 会挑 gcc 当链接驱动
	//（-### 里能看到 `"gcc" "-nostdlib" ... -fuse-ld=lld`），宿主机上没有交叉
	// gcc 就直接 "unable to execute command: program not executable"。
	// 直连 ld.lld 顺带保证产物零外部工具链依赖（与 boot 路径、L2 独立链接一致）。
	if isFreestanding && cfg.Boot == "" {
		return linkFreestandingDirectly(cfg, clangArgs, outputFile, cSource, usedModules, fsLinkObjs, compilerPrefix, compilerCmd)
	}

	cmd.Stdin = strings.NewReader(cSource)
	if verboseMode { fmt.Printf("[Compile] Clang: memory mode (stdin), %d bytes C code\n", len(cSource)) }
	if verboseMode { fmt.Printf("[Compile] Used modules: %v\n", usedModules) }
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 失败 C 源码落到已解析的可写目录（%TEMP% 不可写时 os.TempDir() 也会失败）
		failDir := compilerTempDir()
		if failDir == "" {
			failDir = os.TempDir()
		}
		failPath := filepath.Join(failDir, "kaula_gen_fail.c")
		os.WriteFile(failPath, []byte(cSource), 0644)
		// 诊断信息含完整命令行 / cwd / 临时目录，避免"unable to make temporary file"
		// 这类无定位信息的报错再次出现
		return fmt.Errorf("%s\n  C source: %s", formatCommandFailure("clang compilation failed", cmd, err, output), failPath)
	}
	if verboseMode { fmt.Printf("[Compile] Successfully compiled: %s\n", outputFile) }
	return nil
}

// linkFreestandingDirectly 裸机（无 boot）链接：clang -c → ld.lld。
// 见调用处注释：clang 交叉链接需要 gcc 驱动，宿主机上没有。
func linkFreestandingDirectly(cfg *config.Config, clangArgs []string, outputFile, cSource string,
	usedModules []string, fsObjs []string, compilerPrefix []string, compilerCmd string) error {

	ldPath, err := exec.LookPath("ld.lld")
	if err != nil {
		return fmt.Errorf("ld.lld not found in PATH (required for freestanding linking)")
	}

	// 1) clang -c：把 clangArgs 里的链接相关参数剥掉，只留编译参数
	objFile := strings.TrimSuffix(outputFile, filepath.Ext(outputFile)) + ".o"
	compileArgs := append([]string{}, clangArgs...)
	filtered := make([]string, 0, len(compileArgs))
	for i := 0; i < len(compileArgs); i++ {
		a := compileArgs[i]
		switch {
		case a == "-o":
			i++ // 丢掉 "-o <file>"，下面自己给
			continue
		case strings.HasPrefix(a, "-Wl,"):
			continue
		case a == "-x" && i+1 < len(compileArgs) && compileArgs[i+1] == "none":
			i++ // 丢掉 "-x none <obj...>"：那些 .o 由下面的链接步骤处理
			continue
		case strings.HasSuffix(a, ".o") && filepath.IsAbs(a):
			continue
		}
		filtered = append(filtered, a)
	}
	// clangArgs 以 "-x c -" 开头（源码走 stdin）：filtered 里已保留那个 "-"，
	// 这里只补 -c 与输出，不要再加第二个输入（会变成"multiple output files"）
	ccArgs := append(compilerPrefix, filtered...)
	ccArgs = append(ccArgs, "-c", "-o", objFile)
	ccCmd := compilerCommand(compilerCmd, ccArgs...)
	ccCmd.Stdin = strings.NewReader(cSource)
	if verboseMode {
		fmt.Printf("[Compile] Clang: freestanding -c, %d bytes C code\n", len(cSource))
		fmt.Printf("[Compile] Used modules: %v\n", usedModules)
	}
	if out, err := ccCmd.CombinedOutput(); err != nil {
		failDir := compilerTempDir()
		if failDir == "" {
			failDir = os.TempDir()
		}
		failPath := filepath.Join(failDir, "kaula_gen_fail.c")
		os.WriteFile(failPath, []byte(cSource), 0644)
		return fmt.Errorf("%s\n  C source: %s", formatCommandFailure("freestanding C compilation failed", ccCmd, err, out), failPath)
	}

	// 2) ld.lld 链接
	linkArgs := []string{"-e", "_kaula_entry", "--gc-sections", "-o", outputFile}
	if cfg.Entry != "" {
		linkArgs = []string{"-e", cfg.Entry, "--gc-sections", "-o", outputFile}
	}
	if cfg.LinkScript != "" {
		linkArgs = append(linkArgs, "-T", cfg.LinkScript)
	}
	if cfg.OutputFormat == "bin" {
		linkArgs = append(linkArgs, "--oformat=binary")
	}
	linkArgs = append(linkArgs, objFile)
	// 预编译好的裸机对象：freestanding runtime（5 模块 unity build）+ KMM 分配器
	linkArgs = append(linkArgs, fsObjs...)
	for _, extra := range cfg.CLibs {
		linkArgs = append(linkArgs, "-l"+extra)
	}
	ldCmd := compilerCommand(ldPath, linkArgs...)
	if verboseMode { fmt.Printf("[Compile] ld.lld: %s\n", outputFile) }
	if out, err := ldCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s", formatCommandFailure("freestanding linking failed", ldCmd, err, out))
	}
	if verboseMode { fmt.Printf("[Compile] Successfully linked: %s\n", outputFile) }
	return nil
}

func installedLibraryNames() (string, string) {
	if runtime.GOOS == "windows" {
		return "kaula_std.lib", "kaula_runtime.lib"
	}
	return "libkaula_std.a", "libkaula_runtime.a"
}

// findPkglibPath 查找 pkglib 目录路径（prefer 优先：--pkglib 命令行指定的目录）
func findPkglibPath(prefer string) string {
	if prefer != "" {
		if info, err := os.Stat(prefer); err == nil && info.IsDir() {
			absPath, _ := filepath.Abs(prefer)
			return absPath
		}
	}
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
	fmt.Printf("  --version               显示编译器版本并退出 (版本号来源 version.json)\n")
	fmt.Printf("  --init                  生成默认 kaula.json 配置文件\n")
	fmt.Printf("  --clean-cache           清理过期缓存 (7 天以上)\n")
	fmt.Printf("  --purge-cache           清空所有缓存\n")
	fmt.Printf("  --cache-stats           显示缓存统计信息\n")
	fmt.Printf("  --no-cache              禁用增量编译缓存\n")
	fmt.Printf("  --sor                   启用 SOR 编译时所有权分析（默认 -O3）\n")
	fmt.Printf("  --release               Release 模式 (-O3)\n")
	fmt.Printf("  --opt <level>           优化级别 (O0/O1/O2/O3)，覆盖所有默认值\n")
	fmt.Printf("  --debug                 生成 DWARF 调试符号 (-g)\n")
	fmt.Printf("  --debug-level <level>   调试级别: line-tables (默认) / full (完整 DWARF)\n")
	fmt.Printf("  --sourcemap             生成源码映射文件 (.kl.map.json)\n")
	fmt.Printf("  --offline               强制离线模式（缓存未命中则报错）\n")
	fmt.Printf("  --online                强制在线模式（忽略锁缓存）\n")
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
	fmt.Printf("  --build-pkglib <name>   构建指定 pkglib 库（all=全部），含缺失配置自动分析\n")
	fmt.Printf("  --force-pkg             强制重新构建/重新分析 pkglib 库\n")
	fmt.Printf("  --skip-auto-pkg         禁止编译按需的自动构建\n")
	fmt.Printf("  --boot <mode>           裸机引导方式：pvh/multiboot/custom/none (默认 none，需配合 --freestanding)\n")
	fmt.Printf("  --boot-file <path>      自定义引导汇编文件 (boot=custom 时使用)\n")
	fmt.Printf("  --boot-arch <arch>      引导架构：x86_64/i386/riscv64/aarch64 (默认从 --target-triple 推断)\n")
	fmt.Printf("\nConfiguration File (kaula.json):\n")
	fmt.Printf("  在项目根目录创建 kaula.json 文件配置编译参数。\n")
	fmt.Printf("  命令行参数优先级高于配置文件。\n")
	fmt.Printf("  使用 --init 生成默认配置文件。\n")
	fmt.Printf("\nPatches (Local Path Override):\n")
	fmt.Printf("  在 kaula.json 中用 patches 字段覆盖远程依赖：\n")
	fmt.Printf("  \"patches\": { \"mylib\": { \"path\": \"../mylib-dev\" } }\n")
	fmt.Printf("\nWorkspace:\n")
	fmt.Printf("  kaulac workspace init <members...>  初始化多包工作空间\n")
	fmt.Printf("  kaulac workspace list              列出工作空间成员\n")
	fmt.Printf("  kaulac workspace build [--release] [--debug]  构建所有成员\n")
	fmt.Printf("  kaulac workspace test              运行所有成员的测试\n")
	fmt.Printf("\nPackage Management (pkglib):\n")
	fmt.Printf("  kaulac pkg add <url-or-path> [--name <name>] [--ref <branch>]\n")
	fmt.Printf("                                    添加包到 pkglib（Git 仓库或本地路径）\n")
	fmt.Printf("  kaulac pkg build <name> [--force]  构建指定包（C/C++ 库）\n")
	fmt.Printf("  kaulac pkg analyze <name>          重新分析包并生成 JSON 配置\n")
	fmt.Printf("  kaulac pkg remove <name>           从 pkglib 移除包\n")
	fmt.Printf("  kaulac pkg list                    列出已安装的包\n")
	fmt.Printf("  kaulac pkg fetch                   强制联网拉取所有项目依赖\n")
	fmt.Printf("  kaulac pkg update [name]           更新依赖到最新版本（全部或指定包）\n")
	fmt.Printf("  kaulac pkg lock                    显示 kaula.lock 锁状态\n")

	// F2 修复：补充语言语法文档，特别是 pub/export 的区别
	fmt.Printf("\n语言语法 (Language Syntax):\n")
	fmt.Printf("  变量声明: <type> <name> = <value>          例: i64 x = 42\n")
	fmt.Printf("  函数声明: fn <name>(<params>) <ret> { }    例: fn add(a: i64, b: i64) i64\n")
	fmt.Printf("  结构体:   struct <name> { <field>: <type> } 例: struct Point { x: f64, y: f64 }\n")
	fmt.Printf("  类:       class <name> { ... }\n")
	fmt.Printf("  导入:     import <module>                  例: import std.io\n")
	fmt.Printf("\n可见性修饰符 (Visibility):\n")
	fmt.Printf("  pub     模块级可见性：跨文件/跨模块可调用（Kaula 内部可见性）\n")
	fmt.Printf("          用法: pub fn name() { ... } / pub struct Name { ... }\n")
	fmt.Printf("  export  C 级 FFI 导出：生成可被 C 代码直接调用的符号\n")
	fmt.Printf("          用法: export fn name() { ... }（自动包含 pub 语义）\n")
	fmt.Printf("          也支持裸名转发: export <name>（导出已声明或导入的符号）\n")
	fmt.Printf("  区别:   pub 仅控制 Kaula 模块间可见性；export 额外生成 C ABI 导出符号\n")
	fmt.Printf("          跨文件调用 Kaula 函数用 pub；需要 C 互操作时用 export\n")
}

// ensureLibrary 按需确保库：force 为 true 或归档缺失/源码更新时重建，
// 否则直接返回现有状态（含 C++ 运行时库项，链接时依然需要）
func ensureLibrary(libDir string, force bool) (*stdlib.BuildResult, error) {
	name := filepath.Base(libDir)
	if !force && !stdlib.LibNeedsBuild(libDir) {
		sources, err := stdlib.ScanPackageSources(libDir)
		if err != nil {
			return nil, err
		}
		if len(sources) == 0 {
			// 纯头文件库（无源码，实现宏内联进用户代码）：无可链接归档
			return &stdlib.BuildResult{Name: name}, nil
		}
		hasCpp := false
		for _, s := range sources {
			if s.IsCpp() {
				hasCpp = true
				break
			}
		}
		libraries := []string{name}
		libraries = append(libraries, stdlib.CppRuntimeLibraries(hasCpp)...)
		return &stdlib.BuildResult{
			Name:         name,
			ArchivePath:  stdlib.LibArchivePath(libDir),
			HasCpp:       hasCpp,
			Libraries:    libraries,
			HasLibraries: true,
		}, nil
	}
	return stdlib.BuildLibrary(libDir)
}

// dirExists 判断路径是否存在且为目录
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// handleAnalyzePkg 处理 --analyze-pkg 命令，手动分析指定包
func handleAnalyzePkg(pkgName, pkglibPrefer string) {
	pkglibPath := findPkglibPath(pkglibPrefer)
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

// handleBuildPkg 处理 --build-pkglib 命令，构建指定库或全部库
func handleBuildPkg(target string, force bool, pkglibPrefer string) {
	pkglibPath := findPkglibPath(pkglibPrefer)
	if pkglibPath == "" {
		fmt.Printf("Error: pkglib directory not found\n")
		os.Exit(1)
	}

	// 若库无配置或配置过期（auto_generated 且落后于头/源码），先重新分析：
	// 统一的自愈入口：放库 -> 本条命令自动 分析/桥接 -> 编译归档
	ensureFresh := func(libDir, libName string) {
		cfgPath := filepath.Join(libDir, libName+".json")
		if _, err := os.Stat(cfgPath); err != nil {
			fmt.Printf("  [auto-analyze] %s has no config, analyzing...\n", libName)
			if result, aErr := stdlib.AnalyzePackage(libDir); aErr == nil {
				_ = result.WriteConfig(libDir)
			} else {
				fmt.Printf("  [warn] auto-analyze %s failed: %v\n", libName, aErr)
			}
			return
		}
		if stdlib.ConfigStale(libDir, libName) {
			fmt.Printf("  [re-analyze] %s config is stale, re-analyzing...\n", libName)
			if aRes, aErr := stdlib.AnalyzePackage(libDir); aErr == nil {
				// 合并旧配置的人工链接库/额外目录，防止自动分析丢项
				if _, mErr := stdlib.MergeLibrariesInto(libDir, aRes); mErr != nil {
					fmt.Printf("  [warn] merge config for %s failed: %v\n", libName, mErr)
				}
			} else {
				fmt.Printf("  [warn] re-analyze %s failed: %v\n", libName, aErr)
			}
		}
	}

	if target == "all" {
		entries, rErr := os.ReadDir(pkglibPath)
		if rErr != nil {
			fmt.Printf("Error: failed to read pkglib directory: %v\n", rErr)
			os.Exit(1)
		}
		ok, failed := 0, 0
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			libName := e.Name()
			libDir := filepath.Join(pkglibPath, libName)
			ensureFresh(libDir, libName)
			if !force && !stdlib.LibNeedsBuild(libDir) {
				continue
			}
			fmt.Printf("\nBuilding: %s\n", libName)
			if _, err := buildOnePackage(libDir, libName, force); err != nil {
				fmt.Printf("  失败: %v\n", err)
				failed++
				continue
			}
			ok++
		}
		fmt.Printf("\n构建完成: %d 个重建, %d 个失败\n", ok, failed)
		return
	}

	libDir := filepath.Join(pkglibPath, target)
	if info, err := os.Stat(libDir); err != nil || !info.IsDir() {
		fmt.Printf("Error: package '%s' not found in %s\n", target, pkglibPath)
		os.Exit(1)
	}
	ensureFresh(libDir, target)
	fmt.Printf("Build package: %s\n", target)
	if res, err := buildOnePackage(libDir, target, force); err != nil {
		fmt.Printf("Error: failed to build %s: %v\n", target, err)
		os.Exit(1)
	} else {
		fmt.Printf("  Archive: %s\n", res.ArchivePath)
		fmt.Printf("  Sources compiled: %d\n", res.Built)
		fmt.Printf("  Link libraries: %v\n", res.Libraries)
	}
}

// buildOnePackage 构建单个包；up-to-date 时提示并返回空结果
func buildOnePackage(libDir, libName string, force bool) (*stdlib.BuildResult, error) {
	if !force && !stdlib.LibNeedsBuild(libDir) {
		fmt.Printf("  Up to date (no rebuild needed)\n")
		return &stdlib.BuildResult{Name: libName, ArchivePath: stdlib.LibArchivePath(libDir)}, nil
	}
	return stdlib.BuildLibrary(libDir)
}

// handleAnalyzePkgAll 处理 --analyze-pkg-all 命令，手动分析所有包
func handleAnalyzePkgAll(pkglibPrefer string) {
	pkglibPath := findPkglibPath(pkglibPrefer)
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
			fmt.Printf("  失败: %v\n", err)
			failCount++
			continue
		}

		if err := result.WriteConfig(libDir); err != nil {
			fmt.Printf("  写入配置失败: %v\n", err)
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
