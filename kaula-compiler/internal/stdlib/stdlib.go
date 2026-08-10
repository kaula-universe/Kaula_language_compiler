package stdlib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Function struct {
	Args         []string `json:"args"`
	VarArgs      bool     `json:"varargs"`
	Return       string   `json:"return"`
	MangledName  string   `json:"-"`
	Name         string   `json:"name,omitempty"`
	IsFromBridge bool     `json:"-"`
}

type Module struct {
	Header    string              `json:"header"`
	Prefix    string              `json:"prefix,omitempty"`
	Types     map[string]struct{} `json:"types,omitempty"`
	Functions map[string]Function `json:"functions"`
}

type ThirdPartyLibrary struct {
	Name           string              `json:"name"`
	Headers        []string            `json:"headers"`
	Libraries      []string            `json:"libraries"`
	Type           string              `json:"type,omitempty"`
	ImplementMacro string              `json:"implement_macro,omitempty"`
	Functions      map[string]Function `json:"functions"`
	IncludePath    string              `json:"include_path,omitempty"`
	LibraryPath    string              `json:"library_path,omitempty"`
}

type StdlibConfig struct {
	Modules    map[string]Module
	ThirdParty []ThirdPartyLibrary
}

var (
	configCache   map[string]*StdlibConfig
	configCacheMu sync.RWMutex
)

func init() {
	configCache = make(map[string]*StdlibConfig)
}

func LoadPkgLibraries(pkglibPath string) ([]ThirdPartyLibrary, error) {
	libraries := []ThirdPartyLibrary{}

	// 检查 pkglib 目录是否存在
	if _, err := os.Stat(pkglibPath); os.IsNotExist(err) {
		return libraries, nil // pkglib 不存在时返回空列表
	}

	// 遍历 pkglib 目录中的所有子目录
	entries, err := os.ReadDir(pkglibPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read pkglib directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		libName := entry.Name()
		libDir := filepath.Join(pkglibPath, libName)

		// 查找与目录同名的 .json 配置文件
		configFile := filepath.Join(libDir, libName+".json")
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			// 没有配置文件：先完整构建库（编译产物就绪），再解析处理调用签名
			fmt.Printf("No config for %s, building before analyze...\n", libName)
			if bRes, bErr := BuildLibrary(libDir); bErr != nil {
				fmt.Printf("Warning: Build %s before analyze failed: %v\n", libName, bErr)
			} else if bRes.HasLibraries {
				fmt.Printf("Built %s before analyze (%d sources)\n", libName, bRes.Built)
			}
			result, analyzeErr := AnalyzePackage(libDir)
			if analyzeErr != nil {
				fmt.Printf("Warning: Failed to auto-analyze %s: %v\n", libName, analyzeErr)
				continue
			}
			if writeErr := result.WriteConfig(libDir); writeErr != nil {
				fmt.Printf("Warning: Failed to write config for %s: %v\n", libName, writeErr)
				continue
			}
			fmt.Printf("Auto-generated config: %s (%d functions)\n", configFile, len(result.Functions))
		}

		// 读取并解析配置文件（新生成的或已存在的）
		data, err := os.ReadFile(configFile)
		if err != nil {
			fmt.Printf("Warning: Failed to read %s: %v\n", configFile, err)
			continue
		}
		// 剥离 UTF-8 BOM（部分编辑器/工具写入 json 时带 BOM，json.Unmarshal 会拒绝）
		data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

		var libConfig ThirdPartyLibrary
		if err := json.Unmarshal(data, &libConfig); err != nil {
			fmt.Printf("Warning: Failed to parse %s: %v\n", configFile, err)
			continue
		}

		// 设置库名称（如果配置文件中没有指定）
		if libConfig.Name == "" {
			libConfig.Name = libName
		}

		// 注意：不在此处执行 auto-heal（ConfigStale 触发 AnalyzePackage），
		// 因为加载阶段不知道哪些库会被使用。auto-heal 应只在 compileCCode 中
		// 针对 usedModules 中的库执行，避免对未使用的 C++ 库生成桥接代码并触发编译。
		libraries = append(libraries, libConfig)
		fmt.Printf("Loaded third-party library: %s from %s\n", libConfig.Name, configFile)
	}

	return libraries, nil
}

var autoHealEnabled = true

// SetAutoHealEnabled 开关配置自愈（--skip-auto-pkg 关闭）
func SetAutoHealEnabled(on bool) {
	autoHealEnabled = on
}

func LoadStdlibConfig(configPath string) (*StdlibConfig, error) {
	return LoadStdlibConfigWithPkglib(configPath, defaultPkglibPrefer)
}

var defaultPkglibPrefer string

// SetDefaultPkglibPrefer 设置所有 LoadStdlibConfig 调用默认优先使用的 pkglib 目录
func SetDefaultPkglibPrefer(path string) {
	defaultPkglibPrefer = path
}

// LoadStdlibConfigWithPkglib 加载 stdlib 配置；pkglibPrefer 非空时优先从该目录加载第三方库
func LoadStdlibConfigWithPkglib(configPath, pkglibPrefer string) (*StdlibConfig, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		absPath = configPath
	}

	cacheKey := absPath
	if pkglibPrefer != "" {
		if p, err := filepath.Abs(pkglibPrefer); err == nil {
			cacheKey += "|" + p
		}
	}

	configCacheMu.RLock()
	if cached, ok := configCache[cacheKey]; ok {
		configCacheMu.RUnlock()
		return cached, nil
	}
	configCacheMu.RUnlock()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdlib config: %w", err)
	}

	var rawModules map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawModules); err != nil {
		return nil, fmt.Errorf("failed to parse stdlib config structure: %w", err)
	}

	config := &StdlibConfig{
		Modules: make(map[string]Module),
	}

	for moduleName, rawData := range rawModules {
		var moduleWithHeader struct {
			Header    string              `json:"header"`
			Types     map[string]struct{} `json:"types,omitempty"`
			Functions map[string]Function `json:"functions"`
		}
		if err := json.Unmarshal(rawData, &moduleWithHeader); err == nil && len(moduleWithHeader.Functions) > 0 {
			config.Modules[moduleName] = Module{
				Header:    moduleWithHeader.Header,
				Types:     moduleWithHeader.Types,
				Functions: moduleWithHeader.Functions,
			}
			continue
		}

		var header string
		var rawMap map[string]interface{}
		if err := json.Unmarshal(rawData, &rawMap); err == nil {
			if h, ok := rawMap["header"]; ok {
				if hstr, ok := h.(string); ok {
					header = hstr
				}
			}
		}

		// Parse types if present
		var types map[string]struct{}
		if rawMap != nil {
			if typesRaw, ok := rawMap["types"]; ok {
				if typesMap, ok := typesRaw.(map[string]interface{}); ok {
					types = make(map[string]struct{})
					for k := range typesMap {
						types[k] = struct{}{}
					}
				}
			}
		}

		flatFunctions := make(map[string]Function)
		if rawMap != nil {
			for key, value := range rawMap {
				if key == "header" || key == "types" || key == "prefix" {
					continue
				}
				valueJSON, err := json.Marshal(value)
				if err != nil {
					continue
				}
				var fn Function
				if err := json.Unmarshal(valueJSON, &fn); err != nil {
					flatFunctions[key] = Function{}
				} else {
					flatFunctions[key] = fn
				}
			}
		}

		config.Modules[moduleName] = Module{
			Header:    header,
			Types:     types,
			Functions: flatFunctions,
		}
	}

	thirdPartyPath := filepath.Join(filepath.Dir(configPath), "thirdparty.json")
	if _, err := os.Stat(thirdPartyPath); err == nil {
		thirdPartyData, err := os.ReadFile(thirdPartyPath)
		if err == nil {
			var thirdPartyConfig struct {
				ThirdParty []ThirdPartyLibrary `json:"third_party"`
			}
			if err := json.Unmarshal(thirdPartyData, &thirdPartyConfig); err == nil {
				config.ThirdParty = append(config.ThirdParty, thirdPartyConfig.ThirdParty...)
			}
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		exePath = configPath
	}
	exeDir := filepath.Dir(exePath)
	pkglibPaths := []string{}
	if pkglibPrefer != "" {
		if _, err := os.Stat(pkglibPrefer); err == nil {
			pkglibPaths = append(pkglibPaths, pkglibPrefer)
		}
	}
	pkglibPaths = append(pkglibPaths,
		filepath.Join(exeDir, "pkglib"),
		filepath.Join(filepath.Dir(filepath.Dir(configPath)), "pkglib"),
		filepath.Join(exeDir, "..", "pkglib"),
		"pkglib",
	)

	for _, pkglibPath := range pkglibPaths {
		if _, err := os.Stat(pkglibPath); err == nil {
			pkgLibraries, loadErr := LoadPkgLibraries(pkglibPath)
			if loadErr != nil {
				fmt.Printf("Warning: Failed to load pkglib libraries: %v\n", loadErr)
			} else {
				fmt.Printf("Successfully loaded %d libraries from pkglib\n", len(pkgLibraries))
				config.ThirdParty = append(config.ThirdParty, pkgLibraries...)
			}
			break
		}
	}

	configCacheMu.Lock()
	configCache[cacheKey] = config
	configCacheMu.Unlock()

	return config, nil
}

func LoadStdlibConfigFromPath(relativePath string) (*StdlibConfig, error) {
	absPath, err := filepath.Abs(relativePath)
	if err != nil {
		return nil, err
	}
	return LoadStdlibConfig(absPath)
}

func (sc *StdlibConfig) GetFunction(moduleName, funcName string) *Function {
	if module, ok := sc.Modules[moduleName]; ok {
		if fn, ok := module.Functions[funcName]; ok {
			return &fn
		}
	}
	return nil
}

// GetCFunctionName 获取 C 函数名（自动映射 Kaula 函数名到 C 函数名）
// 策略：
// 1. 如果模块有 Prefix 字段，直接拼接
// 2. 否则通过后缀匹配：funcName="sleep" -> 查找模块中 "_sleep" 结尾的函数
func (sc *StdlibConfig) GetCFunctionName(moduleName, funcName string) string {
	if module, ok := sc.Modules[moduleName]; ok {
		// 优先使用 Prefix 字段
		if module.Prefix != "" {
			return module.Prefix + funcName
		}

		// 精确匹配优先：Kaula 名即 C 名（如 read_line 对应 io.h 中的 read_line，
		// 而非后缀相似的 file_read_line）
		if _, exists := module.Functions[funcName]; exists {
			return funcName
		}

		// 通过后缀匹配查找（如 Kaula 名 printf 对应 C 名 file_printf）
		suffix := "_" + funcName
		for cFuncName := range module.Functions {
			if strings.HasSuffix(cFuncName, suffix) {
				return cFuncName
			}
		}
	}
	return funcName
}

func (sc *StdlibConfig) IsStdlibFunction(funcName string) bool {
	for _, module := range sc.Modules {
		if _, ok := module.Functions[funcName]; ok {
			return true
		}
	}
	return false
}

// GetFunctionByName 按函数名在所有模块中查找签名
func (sc *StdlibConfig) GetFunctionByName(funcName string) *Function {
	for _, module := range sc.Modules {
		if fn, ok := module.Functions[funcName]; ok {
			return &fn
		}
	}
	return nil
}

// GetAnyFunctionSignature 在标准库模块和第三方库（pkglib）中查找函数签名
func (sc *StdlibConfig) GetAnyFunctionSignature(funcName string) *Function {
	if fn := sc.GetFunctionByName(funcName); fn != nil {
		return fn
	}
	for _, lib := range sc.ThirdParty {
		if fn, ok := lib.Functions[funcName]; ok {
			return &fn
		}
	}
	return nil
}

func (sc *StdlibConfig) GetAllFunctions() []string {
	functions := make([]string, 0)
	for _, module := range sc.Modules {
		for name := range module.Functions {
			functions = append(functions, name)
		}
	}
	return functions
}

// GetThirdPartyLibrary 获取指定的第三方库配置
func (sc *StdlibConfig) GetThirdPartyLibrary(name string) *ThirdPartyLibrary {
	for _, lib := range sc.ThirdParty {
		if lib.Name == name {
			return &lib
		}
	}
	return nil
}

// IsThirdPartyFunction 检查是否是第三方库函数
func (sc *StdlibConfig) IsThirdPartyFunction(funcName string) (bool, *ThirdPartyLibrary) {
	for _, lib := range sc.ThirdParty {
		if _, ok := lib.Functions[funcName]; ok {
			return true, &lib
		}
	}
	return false, nil
}

// GetAllHeaders 获取所有需要包含的头文件（标准库 + 第三方库）
func (sc *StdlibConfig) GetAllHeaders() []string {
	headers := []string{}
	for _, lib := range sc.ThirdParty {
		headers = append(headers, lib.Headers...)
	}
	return headers
}

// GetAllLibraries 获取所有需要链接的库文件
func (sc *StdlibConfig) GetAllLibraries() []string {
	libraries := []string{}
	for _, lib := range sc.ThirdParty {
		libraries = append(libraries, lib.Libraries...)
	}
	return libraries
}
