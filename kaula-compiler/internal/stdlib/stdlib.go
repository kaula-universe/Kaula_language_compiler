package stdlib

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Function struct {
	Args    []string `json:"args"`
	VarArgs bool     `json:"varargs"`
	Return  string   `json:"return"`
}

type Module struct {
	Header    string                `json:"header"`
	Functions map[string]Function `json:"functions"`
}

// ThirdPartyLibrary 表示第三方库的配置
type ThirdPartyLibrary struct {
	// 库名称
	Name string `json:"name"`
	// 需要包含的头文件列表（支持 <> 和 "" 形式）
	Headers []string `json:"headers"`
	// 链接的库文件列表（Windows: .lib, Linux: .so）
	Libraries []string `json:"libraries"`
	// 库函数定义
	Functions map[string]Function `json:"functions"`
	// 库的搜索路径（可选）
	IncludePath string `json:"include_path,omitempty"`
	LibraryPath string `json:"library_path,omitempty"`
}

type StdlibConfig struct {
	Modules    map[string]Module
	ThirdParty []ThirdPartyLibrary
}

// LoadPkgLibraries 从 pkglib 目录自动加载第三方库
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
			// 没有配置文件，跳过这个库
			continue
		}
		
		// 读取并解析配置文件
		data, err := os.ReadFile(configFile)
		if err != nil {
			fmt.Printf("Warning: Failed to read %s: %v\n", configFile, err)
			continue
		}
		
		var libConfig ThirdPartyLibrary
		if err := json.Unmarshal(data, &libConfig); err != nil {
			fmt.Printf("Warning: Failed to parse %s: %v\n", configFile, err)
			continue
		}
		
		// 设置库名称（如果配置文件中没有指定）
		if libConfig.Name == "" {
			libConfig.Name = libName
		}
		
		libraries = append(libraries, libConfig)
		fmt.Printf("Loaded third-party library: %s from %s\n", libConfig.Name, configFile)
	}
	
	return libraries, nil
}

func LoadStdlibConfig(configPath string) (*StdlibConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdlib config: %w", err)
	}

	// 尝试使用新的 Module 结构解析（支持 header 字段）
	var rawModules map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawModules); err != nil {
		return nil, fmt.Errorf("failed to parse stdlib config structure: %w", err)
	}

	config := &StdlibConfig{
		Modules: make(map[string]Module),
	}

	// 将原始数据转换为 Module 结构
	for moduleName, rawData := range rawModules {
		// 尝试先解析为新的 Module 结构（带 header 字段）
		var module Module
		if err := json.Unmarshal(rawData, &module); err == nil && len(module.Functions) > 0 {
			// 新格式成功解析
			config.Modules[moduleName] = module
		} else {
			// 回退到旧格式（直接是 map[string]Function）
			var functions map[string]Function
			if err := json.Unmarshal(rawData, &functions); err != nil {
				return nil, fmt.Errorf("failed to parse module %s: %w", moduleName, err)
			}
			config.Modules[moduleName] = Module{
				Functions: functions,
			}
		}
	}

	// 不再从 thirdparty.json 加载，改为从 pkglib 目录自动加载
	// 保留向后兼容，如果 thirdparty.json 存在则也加载
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
	
	// 从 pkglib 目录加载所有第三方库
	// 使用可执行文件所在目录作为基准
	exePath, err := os.Executable()
	if err != nil {
		exePath = configPath
	}
	exeDir := filepath.Dir(exePath)
	// 尝试多个 pkglib 路径
	pkglibPaths := []string{
		filepath.Join(exeDir, "pkglib"),              // kaula-compiler/pkglib (kaula 目录内)
		filepath.Join(filepath.Dir(filepath.Dir(configPath)), "pkglib"), // kaula/pkglib
		filepath.Join(exeDir, "..", "pkglib"),        // kaula-compiler/../pkglib (旧位置)
		"pkglib",                                     // 当前目录
	}
	
	for _, pkglibPath := range pkglibPaths {
		fmt.Printf("Attempting to load pkglib from: %s\n", pkglibPath)
		if _, err := os.Stat(pkglibPath); err == nil {
			pkgLibraries, loadErr := LoadPkgLibraries(pkglibPath)
			if loadErr != nil {
				fmt.Printf("Warning: Failed to load pkglib libraries: %v\n", loadErr)
			} else {
				fmt.Printf("Successfully loaded %d libraries from pkglib\n", len(pkgLibraries))
				config.ThirdParty = append(config.ThirdParty, pkgLibraries...)
			}
			break // 找到并加载后退出
		}
	}

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

func (sc *StdlibConfig) IsStdlibFunction(funcName string) bool {
	for _, module := range sc.Modules {
		if _, ok := module.Functions[funcName]; ok {
			return true
		}
	}
	return false
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
