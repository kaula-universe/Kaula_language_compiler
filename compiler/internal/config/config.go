package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Config 表示编译器的完整配置
type Config struct {
	// ====== 基础路径 ======
	BasePath     string `json:"base_path"`
	TemplatePath string `json:"template_path"`
	IncludePath  string `json:"include_path"`
	StdlibPath   string `json:"stdlib_path,omitempty"`
	PkglibPath   string `json:"pkglib_path,omitempty"`
	SourceDir    string `json:"source_dir,omitempty"`
	OutputDir    string `json:"output_dir,omitempty"`

	// ====== 目标语言 ======
	TargetLanguage string `json:"target_language"`

	// ====== 目标平台（交叉编译/裸机） ======
	Freestanding bool   `json:"freestanding,omitempty"`  // 裸机模式（不依赖 libc/OS）
	TargetTriple string `json:"target_triple,omitempty"` // 目标三元组（如 x86_64-unknown-elf）
	LinkScript   string `json:"link_script,omitempty"`   // 链接脚本路径
	Entry        string `json:"entry,omitempty"`         // 入口函数名（默认 main，裸机可为 _start）
	OutputFormat string `json:"output_format,omitempty"` // 输出格式：elf/bin/obj（默认按平台自动选择）

	// ====== 裸机引导（boot） ======
	Boot        string `json:"boot,omitempty"`        // 引导方式：pvh/multiboot/custom/none（默认 none）
	BootFile    string `json:"boot_file,omitempty"`   // 自定义引导汇编文件路径（boot=custom 时使用）
	BootArch    string `json:"boot_arch,omitempty"`   // 引导架构：x86_64/i386/riscv64/aarch64（默认按 TargetTriple 推断）

	// ====== 优化选项 ======
	OptLevel string `json:"opt_level,omitempty"` // O0/O1/O2/O3, 覆盖所有默认值

	// ====== 编译模式 ======
	SOR     bool `json:"sor"`
	Release bool `json:"release,omitempty"`

	// ====== 缓存选项 ======
	NoCache bool `json:"no_cache,omitempty"`

	// ====== 队列 / 可花费组件 ======
	QueueSize     int `json:"queue_size"`
	SpendableSize int `json:"spendable_size"`

	// ====== 资源限制 ======
	MemoryLimitMB int `json:"memory_limit_mb,omitempty"` // 内存限制 (MB)
	TimeoutSec    int `json:"timeout_sec,omitempty"`     // 超时限制 (秒)

	// ====== 编译器选项 ======
	CCompiler string   `json:"c_compiler,omitempty"` // C 编译器路径（默认按名查找 clang；可指定 clang2gcc 翻译器或 gcc 等）
	CFlags     []string `json:"c_flags,omitempty"`   // 额外的 C 编译器参数
	CDefines   []string `json:"c_defines,omitempty"` // 额外的 C 宏定义 (#define)
	CLibs      []string `json:"c_libs,omitempty"`    // 额外的链接库

	// ====== 源码映射 ======
	SourceMap bool `json:"sourcemap,omitempty"`

	// ====== C 目标翻译单元（TU）拆分 ======
	//
	// 每个被本地 .kl import 的文件产出**独立**的 C 翻译单元，由 C 编译器
	// 并行编译成 .o，再与主 TU 一起链接，而不是把各文件的函数定义以文本方式
	// 注入同一个 .c。仅在 buildTarget/tuMode 非空时生效（由编译器内部
	// 自动推导，不对用户暴露配置项）。
	//
	// 拆分后 C 的链接语义（每个声明必须可见于使用它的 TU）必须与单 TU 的
	// 文本拼接结果保持一致，具体表现为三条约束：
	//   1. 跨文件类型：定义者 TU 的 type_code 同时写入一个 "prelude" 段，
	//      前置到所有 TU 的 includes 之后（名字带 include guard）；
	//   2. 跨文件常量：const 在编译期被折叠为字面量，单 TU 下不可见但能够
	//      工作；拆 TU 后若调用方 TU 未折叠（如顶层 auto 常量），
	//      必须由 prelude 提供 extern 声明 + 定义者 TU 提供真实定义；
	//   3. 泛型实例：实例函数体只能有一个定义（否则重复定义），而调用方
	//      又必须看到原型 —— 由归属规则决定唯一的定义者，其余 TU 只放原型。
	//
	// TUMode 取值：
	//   ""              单 TU（默认，兼容所有既有路径）
	//   "entry"         根 TU：生成 main 入口 + 顶层语句，不含任何本地 import 的定义
	//   "definition"    被 import 的文件：只生成自己那句，不含 main 入口
	TUMode string `json:"tu_mode,omitempty"`
	// TUPrelude 跨 TU 共享的类型/常量前置段，由入口在生成根 TU 前统一注入。
	TUPrelude string `json:"tu_prelude,omitempty"`
	// TUGenericOwner 判定某个泛型实例（MangleGenericName 产出的 C 名）
	// 是否由**本** TU 负责定义。nil 表示"凡实例化过的都由本 TU 定义"
	// （单 TU 语义）。非本 TU 负责的实例只输出原型，函数体由归属 TU 提供。
	TUGenericOwner func(instName string) bool `json:"-"`

	// ====== 输出模式 ======
	// verbose: 详细输出（ninja 风格默认安静：成功只显示状态与产物，出错才打详情）。
	Verbose bool `json:"verbose,omitempty"`

	// ====== 静态分析 ======
	AnalyzePkg    string `json:"analyze_pkg,omitempty"`
	AnalyzePkgAll bool   `json:"analyze_pkg_all,omitempty"`

	// ====== 项目版本 ======
	// 项目版本号（可选，写在 kaula.json 的 "version" 字段）。
	// 非空时嵌入生成 C 代码的头部注释，可用于产物溯源。
	Version string `json:"version,omitempty"`

	// ====== 包依赖 ======
	// 依赖声明（Cargo 风格）：包名 -> 版本约束或 git 仓库配置。
	// 编译时自动从注册源拉取缺失依赖到 ~/.kaula/pkglib/ 缓存。
	// 支持两种格式：
	// 1. 简单字符串：版本约束（如 "0.12" 或 "^1.0"）
	// 2. 对象：git 仓库配置（如 {"git": "https://github.com/user/repo.git", "tag": "v1.0.0"}）
	Dependencies map[string]Dependency `json:"dependencies,omitempty"`
	// 包注册源（默认 https://gitee.com/kaula-universe；可指向任意 git 仓库根/镜像）。
	Registry string `json:"registry,omitempty"`

	// ====== 本地路径覆盖（Patches） ======
	// 用本地目录覆盖远程依赖（类似 Cargo [patch]）。
	// 键为依赖名，path 指向本地包目录（含 kaula.json 或 pkglib config）。
	Patches map[string]PatchConfig `json:"patches,omitempty"`

	// ====== Workspace ======
	// Workspace 模式下管理多个成员包。
	Workspace *WorkspaceConfig `json:"workspace,omitempty"`

	// ====== 调试 ======
	// Debug 模式：生成 DWARF 调试符号（传 -g 给 Clang）。
	Debug bool `json:"debug,omitempty"`
	// DebugLevel 调试级别："line-tables"（默认，仅行号表）或 "full"（完整 DWARF 含类型信息）。
	DebugLevel string `json:"debug_level,omitempty"`

	// ====== 离线/在线模式 ======
	// Offline 强制离线模式：缓存未命中则报错，不联网。
	Offline bool `json:"offline,omitempty"`
	// Online 强制在线模式：忽略锁缓存，联网刷新依赖。
	Online bool `json:"online,omitempty"`

	// ====== 第三方库构建 ======
	BuildPkglib  string `json:"build_pkglib,omitempty"` // 构建指定 pkglib 库（或 "all"）后退出
	ForcePKG     bool   `json:"force_pkg,omitempty"`    // 强制重新构建/重新分析 pkglib 库
	SkipAutoPkg  bool   `json:"skip_auto_pkg,omitempty"` // 禁用使用库时的自动构建
	AutoAnalyzePkg bool `json:"auto_analyze_pkg,omitempty"` // 自动分析缺失/过期的库配置
}

// Dependency 依赖配置，支持版本约束或 git 仓库
type Dependency struct {
	// Version 版本约束（如 "0.12" 或 "^1.0"），与 Git 字段互斥
	Version string `json:"version,omitempty"`
	// Git git 仓库 URL（如 "https://github.com/user/repo.git"）
	Git string `json:"git,omitempty"`
	// Tag 指定 git 标签（可选，默认最新标签）
	Tag string `json:"tag,omitempty"`
	// Branch 指定 git 分支（可选，默认主分支）
	Branch string `json:"branch,omitempty"`
	// Commit 指定 git commit（可选）
	Commit string `json:"commit,omitempty"`
}

// UnmarshalJSON 实现自定义 JSON 反序列化，支持字符串和对象两种格式
func (d *Dependency) UnmarshalJSON(data []byte) error {
	// 尝试解析为字符串（旧格式）
	var version string
	if err := json.Unmarshal(data, &version); err == nil {
		d.Version = version
		return nil
	}

	// 尝试解析为对象（新格式）
	type Alias Dependency
	var alias Alias
	if err := json.Unmarshal(data, &alias); err == nil {
		*d = Dependency(alias)
		return nil
	}

	return fmt.Errorf("invalid dependency format: expected string or object")
}

// MarshalJSON 实现自定义 JSON 序列化
func (d Dependency) MarshalJSON() ([]byte, error) {
	// 如果是简单的版本依赖，序列化为字符串
	if !d.IsGitDependency() && d.Tag == "" && d.Branch == "" && d.Commit == "" {
		return json.Marshal(d.Version)
	}
	// 否则序列化为对象
	type Alias Dependency
	return json.Marshal(Alias(d))
}

// IsGitDependency 检查是否是 git 依赖
func (d Dependency) IsGitDependency() bool {
	return d.Git != ""
}

// GetVersion 获取版本约束（如果是版本依赖）
func (d Dependency) GetVersion() string {
	return d.Version
}

// GetGitURL 获取 git URL（如果是 git 依赖）
func (d Dependency) GetGitURL() string {
	return d.Git
}

// DependsOnEntry 表示包配置文件 (<name>.json) 中 depends_on 的一个条目。
// 支持两种格式：
//   1. 纯包名字符串: "webview"                           → {Name: "webview"}
//   2. 带 URL 对象:  {"name": "webview", "git": "https://..."} → 完整字段
type DependsOnEntry struct {
	Name   string `json:"name"`
	Git    string `json:"git,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// IsGit 检查此条目是否指定了自定义 Git 仓库地址
func (e DependsOnEntry) IsGit() bool {
	return e.Git != ""
}

// UnmarshalJSON 自定义反序列化: 兼容字符串和对象两种格式
func (e *DependsOnEntry) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		e.Name = name
		return nil
	}
	type Alias DependsOnEntry
	var alias Alias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("invalid depends_on entry: expected string or {name, git, ...}: %w", err)
	}
	*e = DependsOnEntry(alias)
	return nil
}

// MarshalJSON 自定义序列化: 纯名字条目输出为字符串，有 URL 的输出为对象
func (e DependsOnEntry) MarshalJSON() ([]byte, error) {
	if !e.IsGit() {
		return json.Marshal(e.Name)
	}
	type Alias DependsOnEntry
	return json.Marshal(Alias(e))
}

// PatchConfig 本地路径覆盖配置
type PatchConfig struct {
	Path string `json:"path"` // 本地包目录的绝对或相对路径
}

// WorkspaceConfig Workspace 配置
type WorkspaceConfig struct {
	// Members workspace 成员目录列表（每个成员含独立 kaula.json）
	Members []string `json:"members,omitempty"`
	// SharedDeps 所有成员共享的依赖声明
	SharedDeps map[string]string `json:"shared_deps,omitempty"`
	// Exclude 排除的目录模式
	Exclude []string `json:"exclude,omitempty"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	basePath, _ := os.Getwd()
	return &Config{
		BasePath:       basePath,
		TemplatePath:   "templates",
		IncludePath:    "../std",
		TargetLanguage: "c",
		QueueSize:      100,
		SpendableSize:  10,
		MemoryLimitMB:  4096,
		TimeoutSec:     120,
		DebugLevel:     "line-tables",
	}
}

// ResolveOptLevel 根据编译模式确定优化级别
// 优先级: --opt 手动指定 > --sor > --release > --debug(O1) > 默认 O2
func (cfg *Config) ResolveOptLevel(optOverride string) string {
	level := "-O2"

	// debug 模式默认 O1：减少 clang 优化时间，提升编译速度（TDD 红绿循环更流畅）
	// release/sor 模式仍使用 O3
	if cfg.Debug {
		level = "-O1"
	}
	if cfg.SOR {
		level = "-O3"
	}
	if cfg.Release {
		level = "-O3"
	}
	if optOverride != "" {
		// 约定形式：--opt O0（不带前缀），历史上也接受 --opt -O0。
		// 旧实现无条件拼 "-"：传 "-O0" 会得到 "--O0" → 不在白名单 →
		// 静默回落 -O2，用户以为开了 -O0，实际是 -O2（宏口径也跟着变）。
		level = "-" + strings.TrimPrefix(optOverride, "-")
	}

	// 验证优化级别
	valid := map[string]bool{"-O0": true, "-O1": true, "-O2": true, "-O3": true}
	if !valid[level] {
		level = "-O2"
	}
	return level
}

// OutputFile 返回可执行文件的输出路径
func (cfg *Config) OutputFile(inputFile string) string {
	inputDir := filepath.Dir(inputFile)
	inputBase := filepath.Base(inputFile)
	inputName := inputBase[:len(inputBase)-3]

	if cfg.OutputDir != "" {
		inputDir = cfg.OutputDir
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(inputDir, inputName+".exe")
	}
	return filepath.Join(inputDir, inputName)
}

// LoadConfig 从配置文件和命令行参数加载配置
func LoadConfig() (*Config, error) {
	// 1. 加载默认配置
	config := DefaultConfig()

	// 2. 从 kaula.json 加载项目配置
	var cfgErr error
	if err := loadProjectConfig(config); err != nil {
		// kaula.json 缺失或损坏（如空文件）时仍返回可用配置，
		// 保证调用方不会拿到 nil config（命令行 flag 依然生效）
		cfgErr = fmt.Errorf("failed to load kaula.json: %w", err)
	}

	// 3. 从命令行 flag 加载（覆盖配置文件）
	loadFlags(config)

	// 4. 规范化路径
	normalizePaths(config)

	return config, cfgErr
}

// LoadConfigAt 从指定目录加载 kaula.json 配置（不读取命令行 flag）
func LoadConfigAt(dir string) (*Config, error) {
	config := DefaultConfig()
	config.BasePath = dir

	configFile := filepath.Join(dir, "kaula.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configFile, err)
	}

	// 规范化路径
	normalizePaths(config)

	return config, nil
}

// loadProjectConfig 从 kaula.json 加载项目配置
func loadProjectConfig(config *Config) error {
	configFile := "kaula.json"
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 配置文件不存在，使用默认值
		}
		return err
	}
	if err := json.Unmarshal(data, config); err != nil {
		return fmt.Errorf("kaula.json parse error: %w", err)
	}
	return nil
}

// loadFlags 从命令行参数加载配置
func loadFlags(config *Config) {
	// 基础路径
	flag.StringVar(&config.TemplatePath, "template", config.TemplatePath, "模板路径")
	flag.StringVar(&config.IncludePath, "include", config.IncludePath, "包含路径")
	flag.StringVar(&config.StdlibPath, "stdlib", config.StdlibPath, "标准库路径")
	flag.StringVar(&config.PkglibPath, "pkglib", config.PkglibPath, "第三方库路径")
	flag.StringVar(&config.SourceDir, "source-dir", config.SourceDir, "源文件目录")
	flag.StringVar(&config.OutputDir, "output-dir", config.OutputDir, "输出目录")

	// 目标与优化
	flag.StringVar(&config.TargetLanguage, "target", config.TargetLanguage, "目标语言")
	flag.StringVar(&config.OptLevel, "opt", config.OptLevel, "优化级别 (O0/O1/O2/O3)")

	// 目标平台（交叉编译/裸机）
	flag.BoolVar(&config.Freestanding, "freestanding", config.Freestanding, "裸机模式（-ffreestanding -nostdlib -nostartfiles）")
	flag.StringVar(&config.TargetTriple, "target-triple", config.TargetTriple, "目标三元组（如 x86_64-unknown-elf, aarch64-none-elf）")
	flag.StringVar(&config.LinkScript, "link-script", config.LinkScript, "链接脚本路径（.lds）")
	flag.StringVar(&config.Entry, "entry", config.Entry, "入口函数名（默认 main，裸机可为 _start）")
	flag.StringVar(&config.OutputFormat, "output-format", config.OutputFormat, "输出格式：elf/bin/obj")

	// 裸机引导
	flag.StringVar(&config.Boot, "boot", config.Boot, "引导方式：pvh/multiboot/custom/none（none=不自动引导）")
	flag.StringVar(&config.BootFile, "boot-file", config.BootFile, "自定义引导汇编文件路径（boot=custom 时使用）")
	flag.StringVar(&config.BootArch, "boot-arch", config.BootArch, "引导架构：x86_64/i386/riscv64/aarch64")

	// 编译模式
	flag.BoolVar(&config.SOR, "sor", config.SOR, "启用 SOR 编译时所有权分析")
	flag.BoolVar(&config.Release, "release", config.Release, "Release 模式 (-O3)")

	// 调试
	flag.BoolVar(&config.Debug, "debug", config.Debug, "生成 DWARF 调试符号 (-g)")
	debugLevel := flag.String("debug-level", config.DebugLevel, "调试级别: line-tables / full")
	flag.BoolVar(&config.Offline, "offline", config.Offline, "强制离线模式")
	flag.BoolVar(&config.Online, "online", config.Online, "强制在线模式")

	// 缓存
	flag.BoolVar(&config.NoCache, "no-cache", config.NoCache, "禁用增量编译缓存")

	// 运行时配置
	flag.IntVar(&config.QueueSize, "queue", config.QueueSize, "队列大小")
	flag.IntVar(&config.SpendableSize, "spendable", config.SpendableSize, "可花费组件大小")

	// 资源限制
	flag.IntVar(&config.MemoryLimitMB, "memory-limit", config.MemoryLimitMB, "内存限制 (MB)")
	flag.IntVar(&config.TimeoutSec, "timeout", config.TimeoutSec, "超时限制 (秒)")

	// C 编译器选项
	flag.StringVar(&config.CCompiler, "cc", config.CCompiler, "C 编译器路径（默认按名查找 clang；可指定 clang2gcc 翻译器、gcc 等）")
	cFlags := flag.String("cflags", "", "额外的 C 编译器参数 (空格分隔)")
	cDefines := flag.String("defines", "", "额外的 C 宏定义 (逗号分隔)")
	cLibs := flag.String("libs", "", "额外的链接库 (逗号分隔)")

	// 源码映射
	flag.BoolVar(&config.SourceMap, "sourcemap", config.SourceMap, "生成源码映射文件")

	// 输出模式
	flag.BoolVar(&config.Verbose, "verbose", config.Verbose, "详细输出 (ninja 风格默认安静)")

	// pkglib 分析
	flag.StringVar(&config.AnalyzePkg, "analyze-pkg", config.AnalyzePkg, "分析指定包并生成配置文件")
	flag.BoolVar(&config.AnalyzePkgAll, "analyze-pkg-all", config.AnalyzePkgAll, "分析所有 pkglib 中的包")

	// 第三方库构建
	flag.StringVar(&config.BuildPkglib, "build-pkglib", config.BuildPkglib, "构建指定 pkglib 库（或 all）后退出")
	flag.BoolVar(&config.ForcePKG, "force-pkg", config.ForcePKG, "强制重新构建/重新分析 pkglib 库")
	flag.BoolVar(&config.SkipAutoPkg, "skip-auto-pkg", config.SkipAutoPkg, "禁用使用库时的自动构建")
	flag.BoolVar(&config.AutoAnalyzePkg, "auto-analyze-pkg", config.AutoAnalyzePkg, "(已废弃) 过期配置默认自动重新分析，无需此开关")

	flag.Parse()

	// 解析调试级别
	if *debugLevel != "" {
		config.DebugLevel = *debugLevel
	}

	// 解析逗号/空格分隔的列表
	if *cFlags != "" {
		config.CFlags = splitList(*cFlags)
	}
	if *cDefines != "" {
		config.CDefines = splitList(*cDefines)
	}
	if *cLibs != "" {
		config.CLibs = splitList(*cLibs)
	}
}

// normalizePaths 确保路径是绝对路径
func normalizePaths(config *Config) {
	paths := []*string{
		&config.BasePath,
		&config.TemplatePath,
		&config.IncludePath,
		&config.StdlibPath,
		&config.PkglibPath,
		&config.SourceDir,
		&config.OutputDir,
	}
	for _, p := range paths {
		if *p != "" && !filepath.IsAbs(*p) {
			if abs, err := filepath.Abs(*p); err == nil {
				*p = abs
			}
		}
	}
	// 规范化 patches 中的本地路径
	for name, patch := range config.Patches {
		if patch.Path != "" && !filepath.IsAbs(patch.Path) {
			if abs, err := filepath.Abs(patch.Path); err == nil {
				patch.Path = abs
				config.Patches[name] = patch
			}
		}
	}
}

// splitList 按逗号和空格分割字符串
func splitList(s string) []string {
	s = strings.ReplaceAll(s, ",", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	return parts
}

// SaveConfig 将配置保存到 kaula.json
func SaveConfig(config *Config, path string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// GenerateDefaultConfig 生成默认配置文件并写入磁盘
func GenerateDefaultConfig(path string) error {
	return SaveConfig(DefaultConfig(), path)
}