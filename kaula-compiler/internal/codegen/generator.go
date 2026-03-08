package codegen

import (
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/config"
)

// Generator 表示代码生成器接口
type Generator interface {
	// Generate 生成代码
	Generate(program *ast.Program) string
	
	// RegisterPlugin 注册插件
	RegisterPlugin(plugin Plugin)
}

// NewGenerator 创建一个新的代码生成器
func NewGenerator(cfg *config.Config) Generator {
	// 根据配置的目标语言创建相应的代码生成器
	switch cfg.TargetLanguage {
	case "c":
		return NewCodeGenerator(cfg)
	// 可以添加其他语言的代码生成器
	default:
		return NewCodeGenerator(cfg)
	}
}
