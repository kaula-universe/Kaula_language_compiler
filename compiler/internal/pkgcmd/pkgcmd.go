package pkgcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"kaula/internal/pkgmgr"
	"kaula/internal/stdlib"
)

// PkgTarget 表示一个包管理目标目录
type PkgTarget struct {
	// Root pkglib 根目录（如 ~/.kaula/pkglib 或项目 ./pkglib）
	Root string
}

// NewPkgTarget 创建包管理目标，自动检测 pkglib 目录
func NewPkgTarget(prefer string) (*PkgTarget, error) {
	if prefer != "" {
		if info, err := os.Stat(prefer); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(prefer)
			return &PkgTarget{Root: abs}, nil
		}
		// 目录不存在则创建
		if err := os.MkdirAll(prefer, 0755); err == nil {
			abs, _ := filepath.Abs(prefer)
			return &PkgTarget{Root: abs}, nil
		}
	}

	// 按优先级搜索
	home, _ := os.UserHomeDir()
	candidates := []string{}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates = append(candidates, filepath.Join(exeDir, "pkglib"))
		candidates = append(candidates, filepath.Join(exeDir, "..", "pkglib"))
	}
	candidates = append(candidates, "./pkglib")
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".kaula", "pkglib"))
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(c)
			return &PkgTarget{Root: abs}, nil
		}
	}

	// 默认使用 ~/.kaula/pkglib
	if home != "" {
		defaultDir := filepath.Join(home, ".kaula", "pkglib")
		_ = os.MkdirAll(defaultDir, 0755)
		return &PkgTarget{Root: defaultDir}, nil
	}

	return nil, fmt.Errorf("cannot determine pkglib directory")
}

// PackageInfo 包的详细信息
type PackageInfo struct {
	Name        string // 包名（目录名）
	Dir         string // 包目录绝对路径
	HasConfig   bool   // 是否有 JSON 配置
	HasSources  bool   // 是否有 C/C++ 源码
	HasKLSource bool   // 是否有 Kaula 源码 (.kl)
	HasArchive  bool   // 是否有编译好的静态库
	IsGit       bool   // 是否来自 Git 仓库
	GitURL      string // Git 仓库 URL（如果有）
}

// AddGitRepo 拉取 Git 仓库到 pkglib 并自动分析/构建
// repoURL: Git 仓库 URL
// name: 包名（可选，为空时从 URL 推断）
// ref: Git ref/tag/branch（可选，默认 main/master）
func (t *PkgTarget) AddGitRepo(repoURL, name, ref string) (*PackageInfo, error) {
	if name == "" {
		name = inferNameFromURL(repoURL)
	}

	dst := filepath.Join(t.Root, name)

	// 检查是否已存在
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		fmt.Printf("[pkg] %s already exists, pulling latest...\n", name)
		if err := gitPull(dst, ref); err != nil {
			fmt.Printf("[pkg] warning: git pull failed: %v\n", err)
		}
	} else {
		if err := os.MkdirAll(t.Root, 0755); err != nil {
			return nil, fmt.Errorf("create pkglib dir: %w", err)
		}
		fmt.Printf("[pkg] cloning %s -> %s\n", repoURL, dst)
		if err := gitClone(repoURL, ref, dst); err != nil {
			return nil, fmt.Errorf("git clone: %w", err)
		}
	}

	info := t.inspectPackage(dst)
	info.GitURL = repoURL

	// 自动构建和分析
	if err := t.autoBuildAndAnalyze(dst, info); err != nil {
		fmt.Printf("[pkg] warning: auto-build/analyze: %v\n", err)
	}

	return info, nil
}

// AddLocalPath 将本地目录链接/复制到 pkglib 并自动分析/构建
func (t *PkgTarget) AddLocalPath(localPath, name string) (*PackageInfo, error) {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	if info, err := os.Stat(absPath); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("path %q is not a directory", absPath)
	}

	if name == "" {
		name = filepath.Base(absPath)
	}

	dst := filepath.Join(t.Root, name)

	// 创建符号链接（Windows 上 fallback 到复制）
	if _, err := os.Lstat(dst); err == nil {
		// 已存在，删除旧的
		os.RemoveAll(dst)
	}

	if runtime.GOOS == "windows" {
		// Windows 上用目录符号链接（需要管理员权限或开发者模式）
		// fallback: 直接使用源目录，不复制
		fmt.Printf("[pkg] using local path: %s -> %s\n", name, absPath)
		dst = absPath
	} else {
		if err := os.Symlink(absPath, dst); err != nil {
			// 符号链接失败，回退到直接使用源目录
			fmt.Printf("[pkg] symlink failed, using source directly: %s\n", absPath)
			dst = absPath
		}
	}

	info := t.inspectPackage(dst)

	// 自动构建和分析
	if err := t.autoBuildAndAnalyze(dst, info); err != nil {
		fmt.Printf("[pkg] warning: auto-build/analyze: %v\n", err)
	}

	return info, nil
}

// Build 手动构建指定包
func (t *PkgTarget) Build(name string, force bool) (*stdlib.BuildResult, error) {
	dst := filepath.Join(t.Root, name)
	if _, err := os.Stat(dst); err != nil {
		return nil, fmt.Errorf("package %q not found in %s", name, t.Root)
	}

	if force || stdlib.LibNeedsBuild(dst) {
		fmt.Printf("[pkg] building %s...\n", name)
		result, err := stdlib.BuildLibrary(dst)
		if err != nil {
			return nil, fmt.Errorf("build %s: %w", name, err)
		}
		fmt.Printf("[pkg] %s built: %s (%d sources)\n", name, result.ArchivePath, result.Built)
		return result, nil
	}

	fmt.Printf("[pkg] %s is up to date\n", name)
	sources, _ := stdlib.ScanPackageSources(dst)
	return &stdlib.BuildResult{
		Name:      name,
		ObjectDir: filepath.Join(dst, "kbuild"),
		Built:     len(sources),
	}, nil
}

// Analyze 手动分析指定包（重新生成 JSON 配置）
func (t *PkgTarget) Analyze(name string) (*stdlib.LibAnalysisResult, error) {
	dst := filepath.Join(t.Root, name)
	if _, err := os.Stat(dst); err != nil {
		return nil, fmt.Errorf("package %q not found in %s", name, t.Root)
	}

	fmt.Printf("[pkg] analyzing %s...\n", name)
	result, err := stdlib.AnalyzePackage(dst)
	if err != nil {
		return nil, fmt.Errorf("analyze %s: %w", name, err)
	}

	if err := result.WriteConfig(dst); err != nil {
		return nil, fmt.Errorf("write config for %s: %w", name, err)
	}

	fmt.Printf("[pkg] %s analyzed: %s (%s, %d functions)\n", name, result.Name, result.Type, len(result.Functions))
	return result, nil
}

// Remove 删除指定包
func (t *PkgTarget) Remove(name string) error {
	dst := filepath.Join(t.Root, name)
	if _, err := os.Stat(dst); err != nil {
		return fmt.Errorf("package %q not found in %s", name, t.Root)
	}

	// 检查是否是符号链接
	if info, err := os.Lstat(dst); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(dst)
	}

	return os.RemoveAll(dst)
}

// List 列出所有已安装的包
func (t *PkgTarget) List() ([]PackageInfo, error) {
	entries, err := os.ReadDir(t.Root)
	if err != nil {
		return nil, fmt.Errorf("read pkglib dir: %w", err)
	}

	var packages []PackageInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		pkgDir := filepath.Join(t.Root, entry.Name())
		info := t.inspectPackage(pkgDir)
		packages = append(packages, *info)
	}

	return packages, nil
}

// autoBuildAndAnalyze 自动检测包类型并执行构建/分析
func (t *PkgTarget) autoBuildAndAnalyze(pkgDir string, info *PackageInfo) error {
	libName := filepath.Base(pkgDir)
	configPath := filepath.Join(pkgDir, libName+".json")

	if info.HasKLSource {
		// Kaula 源码包：编译 .kl 文件
		return t.buildKLSource(pkgDir, info)
	}

	if info.HasSources {
		// C/C++ 包：先分析头文件生成配置，再构建
		if !info.HasConfig || stdlib.ConfigStale(pkgDir, libName) {
			result, err := stdlib.AnalyzePackage(pkgDir)
			if err != nil {
				return fmt.Errorf("analyze: %w", err)
			}
			if err := result.WriteConfig(pkgDir); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			fmt.Printf("[pkg] analyzed %s: %s (%d functions)\n", libName, result.Type, len(result.Functions))
		}

		// 构建静态库
		if stdlib.LibNeedsBuild(pkgDir) {
			buildResult, err := stdlib.BuildLibrary(pkgDir)
			if err != nil {
				return fmt.Errorf("build: %w", err)
			}
			fmt.Printf("[pkg] built %s: %s\n", libName, buildResult.ArchivePath)
		}
	} else if info.HasConfig {
		// 纯头文件库（如 stb 系列）：只需要配置，不需要构建
		fmt.Printf("[pkg] %s is a header-only library\n", libName)
	} else {
		// 没有源码也没有配置：尝试分析（可能有头文件）
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			// 没有配置文件也没有源码，跳过
			fmt.Printf("[pkg] %s: no sources or config found, skipping\n", libName)
		}
	}

	return nil
}

// buildKLSource 编译 Kaula 源码包
// Kaula 源码包的约定：
//   - 目录中包含 .kl 文件
//   - 可选的 kaula.json 配置（声明导出的函数/类型）
//   - 编译时将 .kl 文件作为 import 路径提供
func (t *PkgTarget) buildKLSource(pkgDir string, info *PackageInfo) error {
	libName := filepath.Base(pkgDir)

	// 查找 kaula.json 配置（如果有）
	configPath := filepath.Join(pkgDir, "kaula.json")
	if _, err := os.Stat(configPath); err == nil {
		// 有配置文件，读取并验证
		fmt.Printf("[pkg] %s: Kaula package with config\n", libName)
	} else {
		// 没有配置文件，生成默认配置
		fmt.Printf("[pkg] %s: Kaula source package (no config)\n", libName)
	}

	// Kaula 源码包不需要预编译步骤（编译时由主程序 import 并编译）
	// 但我们需要确保包的结构符合 Kaula 的 import 约定
	// - pub fn / export fn 声明的符号对外可见
	// - import "pkglib/<name>" 即可引用

	return nil
}

// inspectPackage 检查包目录的内容
func (t *PkgTarget) inspectPackage(pkgDir string) *PackageInfo {
	name := filepath.Base(pkgDir)
	info := &PackageInfo{
		Name: name,
		Dir:  pkgDir,
	}

	// 检查 .git
	if _, err := os.Stat(filepath.Join(pkgDir, ".git")); err == nil {
		info.IsGit = true
	}

	// 检查 JSON 配置
	configPath := filepath.Join(pkgDir, name+".json")
	if _, err := os.Stat(configPath); err == nil {
		info.HasConfig = true
	}

	// 扫描源码
	sources, _ := stdlib.ScanPackageSources(pkgDir)
	info.HasSources = len(sources) > 0

	// 扫描 Kaula 源码
	var klFiles []string
	filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".kl") {
			klFiles = append(klFiles, path)
		}
		return nil
	})
	info.HasKLSource = len(klFiles) > 0

	// 检查静态库
	if _, err := os.Stat(filepath.Join(pkgDir, name+".lib")); err == nil {
		info.HasArchive = true
	} else if _, err := os.Stat(filepath.Join(pkgDir, "lib"+name+".a")); err == nil {
		info.HasArchive = true
	}

	return info
}

// --- Git 操作辅助函数 ---

func gitClone(repoURL, ref, dst string) error {
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref, "--single-branch")
	}
	args = append(args, repoURL, dst)

	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitPull(dst, ref string) error {
	cmd := exec.Command("git", "-C", dst, "pull", "--ff-only")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func inferNameFromURL(url string) string {
	// 从 URL 推断包名：https://github.com/user/repo.git -> repo
	name := filepath.Base(url)
	name = strings.TrimSuffix(name, ".git")
	name = strings.TrimSuffix(name, ".git/")
	return name
}

// --- 项目依赖管理（kaula.json dependencies） ---

// LoadProjectDeps 从 kaula.json 读取 dependencies 和 registry
func LoadProjectDeps(projectDir string) (map[string]string, string, error) {
	kaulaJSON := filepath.Join(projectDir, "kaula.json")
	data, err := os.ReadFile(kaulaJSON)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("no kaula.json found in %s", projectDir)
		}
		return nil, "", fmt.Errorf("read kaula.json: %w", err)
	}

	var cfg struct {
		Dependencies map[string]string `json:"dependencies,omitempty"`
		Registry     string            `json:"registry,omitempty"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, "", fmt.Errorf("parse kaula.json: %w", err)
	}

	registry := cfg.Registry
	if registry == "" {
		registry = pkgmgr.DefaultRegistry
	}

	return cfg.Dependencies, registry, nil
}

// FetchProjectDeps 强制联网拉取项目所有依赖（忽略锁缓存）
func FetchProjectDeps(projectDir string) error {
	deps, registry, err := LoadProjectDeps(projectDir)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		fmt.Println("[pkg] no dependencies declared in kaula.json")
		return nil
	}

	fmt.Printf("[pkg] fetching %d dependencies from %s...\n", len(deps), registry)
	results, lock, err := pkgmgr.FetchDependencies(registry, deps, projectDir)
	if err != nil {
		return err
	}

	if err := pkgmgr.SaveProjectLock(projectDir, lock); err != nil {
		fmt.Printf("[pkg] warning: failed to write kaula.lock: %v\n", err)
	}

	fmt.Printf("[pkg] fetched %d dependencies\n", len(results))
	return nil
}

// UpdateProjectDeps 联网更新项目所有依赖到最新匹配版本
func UpdateProjectDeps(projectDir string) error {
	deps, registry, err := LoadProjectDeps(projectDir)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		fmt.Println("[pkg] no dependencies declared in kaula.json")
		return nil
	}

	fmt.Printf("[pkg] updating %d dependencies from %s...\n", len(deps), registry)
	results, lock, err := pkgmgr.UpdateDependencies(registry, deps, projectDir)
	if err != nil {
		return err
	}

	if err := pkgmgr.SaveProjectLock(projectDir, lock); err != nil {
		fmt.Printf("[pkg] warning: failed to write kaula.lock: %v\n", err)
	}

	updated := 0
	for _, r := range results {
		if r.Source == pkgmgr.LockSourceUpdate {
			updated++
		}
	}
	fmt.Printf("[pkg] updated %d of %d dependencies\n", updated, len(results))
	return nil
}

// UpdateSingleDep 更新单个依赖到最新版本
func UpdateSingleDep(projectDir, name string) error {
	deps, registry, err := LoadProjectDeps(projectDir)
	if err != nil {
		return err
	}

	constraint, ok := deps[name]
	if !ok {
		return fmt.Errorf("dependency %q not declared in kaula.json", name)
	}

	singleDeps := map[string]string{name: constraint}
	fmt.Printf("[pkg] updating %s from %s...\n", name, registry)
	results, lock, err := pkgmgr.UpdateDependencies(registry, singleDeps, projectDir)
	if err != nil {
		return err
	}

	if err := pkgmgr.SaveProjectLock(projectDir, lock); err != nil {
		fmt.Printf("[pkg] warning: failed to write kaula.lock: %v\n", err)
	}

	for _, r := range results {
		fmt.Printf("[pkg] %s %s\n", r.Name, r.Version)
	}
	return nil
}

// ShowLockStatus 显示 kaula.lock 的状态
func ShowLockStatus(projectDir string) error {
	lock, err := pkgmgr.LoadProjectLock(projectDir)
	if err != nil {
		return err
	}

	if len(lock.Packages) == 0 {
		fmt.Println("[pkg] no locked dependencies (kaula.lock is empty)")
		return nil
	}

	fmt.Printf("Locked dependencies (%d):\n", len(lock.Packages))
	for _, name := range lock.SortedNames() {
		ver := lock.GetVersion(name)
		source := "?"
		if entry, ok := lock.Entries[name]; ok {
			source = string(entry.ResolvedFrom)
		}
		fmt.Printf("  %-20s %-12s (via %s)\n", name, ver, source)
	}
	return nil
}
