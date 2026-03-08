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
	Functions map[string]Function `json:""`
}

type StdlibConfig struct {
	Modules map[string]Module `json:""`
}

func LoadStdlibConfig(configPath string) (*StdlibConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdlib config: %w", err)
	}

	var modules map[string]map[string]Function
	if err := json.Unmarshal(data, &modules); err != nil {
		return nil, fmt.Errorf("failed to parse stdlib config: %w", err)
	}

	config := &StdlibConfig{
		Modules: make(map[string]Module),
	}

	for moduleName, functions := range modules {
		config.Modules[moduleName] = Module{
			Functions: functions,
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
