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
	CFlags   []string `json:"c_flags,omitempty"`   // 额外的 C 编译器参数
	CDefines []string `json:"c_defines,omitempty"` // 额外的 C 宏定义 (#define)
	CLibs    []string `json:"c_libs,omitempty"`    // 额外的链接库

	// ====== 源码映射 ======
	SourceMap bool `json:"sourcemap,omitempty"`

	// ====== 静态分析 ======
	AnalyzePkg    string `json:"analyze_pkg,omitempty"`
	AnalyzePkgAll bool   `json:"analyze_pkg_all,omitempty"`
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
	}
}

// ResolveOptLevel 根据编译模式确定优化级别
// 优先级: --opt 手动指定 > --sor > --release > 默认 O2
func (cfg *Config) ResolveOptLevel(optOverride string) string {
	level := "-O2"

	if cfg.SOR {
		level = "-O3"
	}
	if cfg.Release {
		level = "-O3"
	}
	if optOverride != "" {
		level = "-" + optOverride
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
	if err := loadProjectConfig(config); err != nil {
		return nil, fmt.Errorf("failed to load kaula.json: %w", err)
	}

	// 3. 从命令行 flag 加载（覆盖配置文件）
	loadFlags(config)

	// 4. 规范化路径
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

	// 编译模式
	flag.BoolVar(&config.SOR, "sor", config.SOR, "启用 SOR 编译时所有权分析")
	flag.BoolVar(&config.Release, "release", config.Release, "Release 模式 (-O3)")

	// 缓存
	flag.BoolVar(&config.NoCache, "no-cache", config.NoCache, "禁用增量编译缓存")

	// 运行时配置
	flag.IntVar(&config.QueueSize, "queue", config.QueueSize, "队列大小")
	flag.IntVar(&config.SpendableSize, "spendable", config.SpendableSize, "可花费组件大小")

	// 资源限制
	flag.IntVar(&config.MemoryLimitMB, "memory-limit", config.MemoryLimitMB, "内存限制 (MB)")
	flag.IntVar(&config.TimeoutSec, "timeout", config.TimeoutSec, "超时限制 (秒)")

	// C 编译器选项
	cFlags := flag.String("cflags", "", "额外的 C 编译器参数 (空格分隔)")
	cDefines := flag.String("defines", "", "额外的 C 宏定义 (逗号分隔)")
	cLibs := flag.String("libs", "", "额外的链接库 (逗号分隔)")

	// 源码映射
	flag.BoolVar(&config.SourceMap, "sourcemap", config.SourceMap, "生成源码映射文件")

	// pkglib 分析
	flag.StringVar(&config.AnalyzePkg, "analyze-pkg", config.AnalyzePkg, "分析指定包并生成配置文件")
	flag.BoolVar(&config.AnalyzePkgAll, "analyze-pkg-all", config.AnalyzePkgAll, "分析所有 pkglib 中的包")

	flag.Parse()

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
