package codegen

import (
	"kaula-compiler/internal/ast"
)

// Plugin 表示代码生成插件接口
type Plugin interface {
	// Name 返回插件名称
	Name() string
	
	// GenerateStatement 生成语句代码
	// 如果插件处理了该语句，返回生成的代码和true
	// 否则返回空字符串和false
	GenerateStatement(stmt ast.Statement, cg *CodeGenerator) (string, bool)
	
	// GenerateExpression 生成表达式代码
	// 如果插件处理了该表达式，返回生成的代码和true
	// 否则返回空字符串和false
	GenerateExpression(expr ast.Expression, cg *CodeGenerator) (string, bool)
}

// PluginManager 表示插件管理器
type PluginManager struct {
	plugins []Plugin
}

// NewPluginManager 创建一个新的插件管理器
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins: make([]Plugin, 0),
	}
}

// RegisterPlugin 注册插件
func (pm *PluginManager) RegisterPlugin(plugin Plugin) {
	pm.plugins = append(pm.plugins, plugin)
}

// GenerateStatement 尝试使用插件生成语句代码
func (pm *PluginManager) GenerateStatement(stmt ast.Statement, cg *CodeGenerator) (string, bool) {
	for _, plugin := range pm.plugins {
		if code, ok := plugin.GenerateStatement(stmt, cg); ok {
			return code, true
		}
	}
	return "", false
}

// GenerateExpression 尝试使用插件生成表达式代码
func (pm *PluginManager) GenerateExpression(expr ast.Expression, cg *CodeGenerator) (string, bool) {
	for _, plugin := range pm.plugins {
		if code, ok := plugin.GenerateExpression(expr, cg); ok {
			return code, true
		}
	}
	return "", false
}
