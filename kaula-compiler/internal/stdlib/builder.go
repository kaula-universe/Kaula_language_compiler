package stdlib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// buildWorkDirName 存放桥接生成与编译产物的目录名（位于库目录内，已被 .gitignore 忽略）
const buildWorkDirName = "kbuild"

// BuildResult 表示一次静态库构建的结果
type BuildResult struct {
	Name         string   // 库名（与目录同名）
	ArchivePath  string   // lib<name>.a 的绝对路径
	ObjectDir    string   // 中间 .o 文件目录
	HasCpp       bool     // 是否包含 C++ 源码
	HasLibraries bool     // 是否产出了可链接的归档（纯头文件库为 false）
	Libraries    []string // 需要在链接阶段追加的库名（-l<name>）
	Built        int      // 实际编译的源文件数
	UpdatedAt    string
}

// BuildSource 表示一个待编译的源文件
type BuildSource struct {
	RelPath string // 相对库目录的路径（含子目录，如 backends/imgui_impl_dx11.cpp）
	AbsPath string
	Kind    string // "c" 或 "c++"
}

// IsCpp 判断源文件是否为 C++ 源码
func (s BuildSource) IsCpp() bool {
	return s.Kind == "c++"
}

// ScanPackageSources 扫描库目录内的可编译源文件（顶层 + 一级子目录）。
// 自动生成的桥接源 (*_kbridge.cpp) 同样会被纳入编译，从而链接 extern "C" 符号。
func ScanPackageSources(pkgDir string) ([]BuildSource, error) {
	var sources []BuildSource
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			src, ok := classifySource(e.Name())
			if ok {
				rel := filepath.ToSlash(e.Name())
				sources = append(sources, BuildSource{RelPath: rel, AbsPath: filepath.Join(pkgDir, e.Name()), Kind: src})
			}
			continue
		}
		// 递归一层子目录（C++ 库常用 backends/source 子目录结构）
		sub := filepath.Join(pkgDir, e.Name())
		subEntries, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if se.IsDir() {
				continue
			}
			src, ok := classifySource(se.Name())
			if ok {
				rel := filepath.ToSlash(filepath.Join(e.Name(), se.Name()))
				sources = append(sources, BuildSource{RelPath: rel, AbsPath: filepath.Join(sub, se.Name()), Kind: src})
			}
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].RelPath < sources[j].RelPath })
	return sources, nil
}

// classifySource 判断文件名是否是需编译的 C/C++ 源码
func classifySource(name string) (string, bool) {
	lower := strings.ToLower(name)
	// 跳过编译器自动输出的对象与 s 汇编
	if strings.HasSuffix(lower, ".o") || strings.HasSuffix(lower, ".obj") || strings.HasSuffix(lower, ".bc") {
		return "", false
	}
	if strings.HasSuffix(lower, ".c") {
		return "c", true
	}
	if strings.HasSuffix(lower, ".cpp") || strings.HasSuffix(lower, ".cc") ||
		strings.HasSuffix(lower, ".cxx") || strings.HasSuffix(lower, ".c++") {
		return "c++", true
	}
	return "", false
}

// findArchiverTool 定位静态库工具：优先 llvm-ar（clang 配套），回退 ar
func findArchiverTool() (string, error) {
	if p, err := exec.LookPath("llvm-ar"); err == nil {
		return p, nil
	}
	// 尝试与 clang 同目录的 llvm-ar
	if clangPath, err := findClangPath(); err == nil {
		dir := filepath.Dir(clangPath)
		cand := filepath.Join(dir, "llvm-ar.exe")
		if runtime.GOOS != "windows" {
			cand = filepath.Join(dir, "llvm-ar")
		}
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	if p, err := exec.LookPath("ar"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no archiver found (tried llvm-ar, ar)")
}

// findCxxFiltTool 定位 llvm-cxxfilt（C++ 符号反修饰），支持回退 c++filt
func findCxxFiltTool() (string, error) {
	for _, name := range []string{"llvm-cxxfilt", "c++filt"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	if clangPath, err := findClangPath(); err == nil {
		dir := filepath.Dir(clangPath)
		cand := filepath.Join(dir, "llvm-cxxfilt")
		if runtime.GOOS == "windows" {
			cand += ".exe"
		}
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no cxxfilt tool found (tried llvm-cxxfilt, c++filt)")
}

// Demangle 将 C++ 符号反修饰为可读名称（如 _ZN4ImGui4TextEPKcz -> ImGui::Text(char const*, ...)）
func Demangle(mangled string) string {
	if mangled == "" {
		return ""
	}
	tool, err := findCxxFiltTool()
	if err != nil {
		return ""
	}
	cmd := exec.Command(tool, "-n", mangled)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// LibNeedsBuild 判断库是否需要（重新）构建：
// 目录中存在 C/C++ 源码，且不存在对应归档、或存在比归档更新的源码
func LibNeedsBuild(pkgDir string) bool {
	archive := LibArchivePath(pkgDir)
	sources, err := ScanPackageSources(pkgDir)
	if err != nil || len(sources) == 0 {
		return false
	}
	arInfo, arErr := os.Stat(archive)
	if arErr != nil {
		return true
	}
	for _, s := range sources {
		if info, err := os.Stat(s.AbsPath); err == nil {
			if info.ModTime().After(arInfo.ModTime()) {
				return true
			}
		}
	}
	// 存在外部链接库可用的二进制与否都不影响，这里只看源码
	return false
}

// CppRuntimeLibraries 返回链接 C++ 库所需的运行时库：
// llvm-mingw 提供 libc++/libc++abi，其他平台用 libstdc++
func CppRuntimeLibraries(hasCpp bool) []string {
	if !hasCpp {
		return nil
	}
	if runtime.GOOS == "windows" {
		return []string{"c++", "c++abi"}
	}
	if runtime.GOOS == "darwin" {
		return nil // 系统自带
	}
	return []string{"stdc++"}
}

// LibArchivePath 返回库的静态归档文件路径（lib<name>.a）
func LibArchivePath(pkgDir string) string {
	return filepath.Join(pkgDir, "lib"+filepath.Base(pkgDir)+".a")
}

// ConfigStale 判断库的 JSON 配置是否已过期：配置缺失、或（且仅当配置由分析器自动生成时）
// 存在比配置文件更新的头文件/源码，返回 true。
// 手写/人工维护的配置（auto_generated=false）不做自动覆盖。
func ConfigStale(pkgDir, libName string) bool {
	cfgPath := filepath.Join(pkgDir, libName+".json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return true
	}
	var cfg struct {
		AutoGenerated bool `json:"auto_generated"`
	}
	cfgData := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if jerr := json.Unmarshal(cfgData, &cfg); jerr != nil {
		return true
	}
	if !cfg.AutoGenerated {
		return false // 手写配置不改动
	}
	cfgInfo, err := os.Stat(cfgPath)
	if err != nil {
		return true
	}
	// 配置里记录的头文件若已从磁盘消失（如被删除的手写桥接），同样视为过期
	var cfgFull struct {
		Headers []string `json:"headers"`
	}
	if data, rerr := os.ReadFile(cfgPath); rerr == nil {
		_ = json.Unmarshal(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}), &cfgFull)
	}
	for _, h := range cfgFull.Headers {
		h = strings.Trim(strings.TrimSpace(h), `"`)
		if h == "" || strings.HasPrefix(h, "<") || filepath.IsAbs(h) {
			continue
		}
		// 配置里的引用形如 "imgui/imgui.h"（相对 pkglib 根）或裸文件名（相对库目录）
		rel := h
		if strings.HasPrefix(h, filepath.Base(pkgDir)+"/") {
			rel = strings.TrimPrefix(h, filepath.Base(pkgDir)+"/")
		}
		if _, serr := os.Stat(filepath.Join(pkgDir, rel)); serr != nil {
			return true
		}
	}
	// 检查所有头文件与源码是否比配置新
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		isHeader := strings.HasSuffix(name, ".h") || strings.HasSuffix(name, ".hpp")
		isSource := strings.HasSuffix(name, ".c") || strings.HasSuffix(name, ".cpp") ||
			strings.HasSuffix(name, ".cc") || strings.HasSuffix(name, ".cxx")
		if !isHeader && !isSource {
			continue
		}
		info, eerr := e.Info()
		if eerr != nil {
			continue
		}
		if info.ModTime().After(cfgInfo.ModTime()) {
			return true
		}
	}
	return false
}

// MergeLibrariesInto 将旧配置中的【人工维护项】合并进重新分析的结果并写回：
// 重新分析只会产出本库自身的链接项（如 imgui），
// 人工补充的系统库（d3d11/dwmapi/d3dcompiler）会因此丢失，这里从旧配置找回。
// 仅合并 libraries/include_path/library_path，头文件与函数表以新分析为准。
// 返回合并后的库配置（供调用方直接使用），并写回 JSON。
func MergeLibrariesInto(pkgDir string, result *LibAnalysisResult) (*ThirdPartyLibrary, error) {
	libName := result.Name
	if libName == "" {
		libName = filepath.Base(pkgDir)
	}
	cfgPath := filepath.Join(pkgDir, libName+".json")

	var old ThirdPartyLibrary
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}), &old)
	}

	merged := &ThirdPartyLibrary{
		Name:        libName,
		Headers:     append([]string(nil), result.Headers...),
		Libraries:   append([]string(nil), result.Libraries...),
		Type:        result.Type,
		Functions:   result.Functions,
		IncludePath: result.IncludePath,
		LibraryPath: result.LibraryPath,
	}
	for _, oldLib := range old.Libraries {
		// 旧配置的 C++ 运行时名（stdc++）已被统一替换为 c++/c++abi，跳过
		if oldLib == "stdc++" || oldLib == "libstdc++" {
			continue
		}
		dup := false
		for _, m := range merged.Libraries {
			if m == oldLib {
				dup = true
				break
			}
		}
		if !dup {
			merged.Libraries = append(merged.Libraries, oldLib)
		}
	}
	if merged.IncludePath == "" {
		merged.IncludePath = old.IncludePath
	}
	if merged.LibraryPath == "" {
		merged.LibraryPath = old.LibraryPath
	}
	// 先合并再写回，保证磁盘上的 JSON 与返回的配置一致
	result.Libraries = merged.Libraries
	result.IncludePath = merged.IncludePath
	result.LibraryPath = merged.LibraryPath
	if err := result.WriteConfig(pkgDir); err != nil {
		return nil, fmt.Errorf("write merged config: %w", err)
	}
	return merged, nil
}

// BuildLibrary 编译库里所有 C/C++ 源码，打包为 lib<name>.a 静态库。
// 若是共享头库（无源码），返回 BuildResult{HasLibraries:false}。
func BuildLibrary(pkgDir string) (*BuildResult, error) {
	name := filepath.Base(pkgDir)
	clangPath, err := findClangPath()
	if err != nil {
		return nil, fmt.Errorf("library build needs clang: %w", err)
	}
	arPath, err := findArchiverTool()
	if err != nil {
		return nil, fmt.Errorf("library build needs archiver: %w", err)
	}

	sources, err := ScanPackageSources(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("scan sources in %s: %w", pkgDir, err)
	}
	if len(sources) == 0 {
		// 无编译源码（纯头文件库 at 头文件宏），无需构建
		return &BuildResult{Name: name, ObjectDir: filepath.Join(pkgDir, buildWorkDirName)}, nil
	}

	objDir := filepath.Join(pkgDir, buildWorkDirName)
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return nil, fmt.Errorf("create build dir %s: %w", objDir, err)
	}

	// include 路径：库里目录本身（函数式包含 "imgui/xxx.h" 需要父目录）与库目录
	includeDirs := []string{filepath.Dir(pkgDir), pkgDir}

	hasCpp := false
	for _, s := range sources {
		if s.Kind == "c++" {
			hasCpp = true
			break
		}
	}

	// 编译每个源文件
	type jobResult struct {
		rel string
		err error
	}
	jobs := make(chan BuildSource, len(sources))
	results := make(chan jobResult, len(sources))

	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				sem <- struct{}{}
				err := compileOneSource(clangPath, objDir, s, includeDirs)
				<-sem
				results <- jobResult{rel: s.RelPath, err: err}
			}
		}()
	}
	for _, s := range sources {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	close(results)

	var objPaths []string
	sourceByRel := make(map[string]BuildSource, len(sources))
	for _, s := range sources {
		sourceByRel[s.RelPath] = s
	}
	for r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("compile %s: %w", r.rel, r.err)
		}
		objPaths = append(objPaths, objFullPath(objDir, sourceByRel[r.rel]))
	}
	sort.Strings(objPaths)

	archivePath := LibArchivePath(pkgDir)
	// 删除旧的归档（放置到所有目标文件编译成功之后，避免编译失败时破坏可用的旧库）

	arCmd := exec.Command(arPath, "rcs", archivePath)
	if _, err := os.Stat(archivePath); err == nil {
		os.Remove(archivePath)
	}
	arCmd.Args = append(arCmd.Args, objPaths...)
	if out, err := arCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ar archive failed: %v\n%s", err, string(out))
	}

	libraries := []string{name}
	libraries = append(libraries, CppRuntimeLibraries(hasCpp)...)

	return &BuildResult{
		Name:         name,
		ArchivePath:  archivePath,
		ObjectDir:    objDir,
		HasCpp:       hasCpp,
		Libraries:    libraries,
		HasLibraries: true,
		Built:        len(sources),
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}, nil
}

// objFullPath 计算源文件对应的 .o 对象路径（保持目录结构，避免同名冲突）
func objFullPath(objDir string, src BuildSource) string {
	base := strings.TrimSuffix(filepath.Base(src.RelPath), filepath.Ext(src.RelPath))
	dir := filepath.Dir(src.RelPath)
	if dir == "." || dir == "" {
		return filepath.Join(objDir, base+".o")
	}
	sub := filepath.Join(objDir, dir)
	os.MkdirAll(sub, 0755)
	return filepath.Join(sub, base+".o")
}

// compileOneSource 用 clang 编译单个源文件为 .o
func compileOneSource(clangPath, objDir string, s BuildSource, includeDirs []string) error {
	args := []string{"-c", "-O2"}

	if s.Kind == "c++" {
		args = append(args, "-x", "c++", "-std=c++11")
	} else {
		args = append(args, "-x", "c", "-std=c11")
	}

	for _, inc := range includeDirs {
		args = append(args, "-I", inc)
	}

	objPath := objFullPath(objDir, s)
	args = append(args, s.AbsPath, "-o", objPath)

	cmd := exec.Command(clangPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %v\n%s", s.RelPath, err, strings.TrimRight(string(out), "\n"))
	}
	return nil
}