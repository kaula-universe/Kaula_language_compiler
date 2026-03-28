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

func LoadStdlibConfig(configPath string) (*StdlibConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdlib config: %w", err)
	}

	// stdlib.json 的顶层直接是模块，不是 {"modules": {...}} 格式
	// 所以我们需要手动解析
	var rawModules map[string]map[string]Function
	if err := json.Unmarshal(data, &rawModules); err != nil {
		return nil, fmt.Errorf("failed to parse stdlib config: %w", err)
	}

	config := &StdlibConfig{
		Modules: make(map[string]Module),
	}

	// 将原始数据转换为 Module 结构
	for moduleName, functions := range rawModules {
		config.Modules[moduleName] = Module{
			Functions: functions,
		}
	}

	// 尝试加载第三方库配置文件
	thirdPartyPath := filepath.Join(filepath.Dir(configPath), "thirdparty.json")
	if _, err := os.Stat(thirdPartyPath); err == nil {
		thirdPartyData, err := os.ReadFile(thirdPartyPath)
		if err == nil {
			var thirdPartyConfig struct {
				ThirdParty []ThirdPartyLibrary `json:"third_party"`
			}
			if err := json.Unmarshal(thirdPartyData, &thirdPartyConfig); err == nil {
				config.ThirdParty = thirdPartyConfig.ThirdParty
			}
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
