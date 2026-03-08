package core

import (
	"sync"
)

// PrefixContext 表示前缀上下文
type PrefixContext struct {
	Name      string
	Variables map[string]interface{}
	Parent    *PrefixContext
}

// PrefixManager 表示前缀管理器
type PrefixManager struct {
	Contexts map[string]*PrefixContext
	Mutex    sync.RWMutex
}

// NewPrefixManager 创建一个新的前缀管理器
func NewPrefixManager() *PrefixManager {
	return &PrefixManager{
		Contexts: make(map[string]*PrefixContext),
	}
}

// CreatePrefix 创建一个新的前缀
func (pm *PrefixManager) CreatePrefix(name string) *PrefixContext {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	if _, exists := pm.Contexts[name]; !exists {
		pm.Contexts[name] = &PrefixContext{
			Name:      name,
			Variables: make(map[string]interface{}),
			Parent:    nil,
		}
	}
	return pm.Contexts[name]
}

// GetPrefix 获取前缀
func (pm *PrefixManager) GetPrefix(name string) *PrefixContext {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	return pm.Contexts[name]
}

// SetVariable 设置变量
func (pm *PrefixManager) SetVariable(prefix, name string, value interface{}) {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	if ctx, exists := pm.Contexts[prefix]; exists {
		ctx.Variables[name] = value
	}
}

// GetVariable 获取变量
func (pm *PrefixManager) GetVariable(prefix, name string) (interface{}, bool) {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	if ctx, exists := pm.Contexts[prefix]; exists {
		if value, ok := ctx.Variables[name]; ok {
			return value, true
		}
		// 查找父上下文
		if ctx.Parent != nil {
			return pm.getVariableFromContext(ctx.Parent, name)
		}
	}
	return nil, false
}

// getVariableFromContext 从上下文链中获取变量
func (pm *PrefixManager) getVariableFromContext(ctx *PrefixContext, name string) (interface{}, bool) {
	if value, ok := ctx.Variables[name]; ok {
		return value, true
	}
	if ctx.Parent != nil {
		return pm.getVariableFromContext(ctx.Parent, name)
	}
	return nil, false
}

// SetParent 设置父前缀
func (pm *PrefixManager) SetParent(prefix, parentPrefix string) bool {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	ctx, exists := pm.Contexts[prefix]
	if !exists {
		return false
	}
	parentCtx, parentExists := pm.Contexts[parentPrefix]
	if !parentExists {
		return false
	}
	ctx.Parent = parentCtx
	return true
}

// DeletePrefix 删除前缀
func (pm *PrefixManager) DeletePrefix(name string) bool {
	pm.Mutex.Lock()
	defer pm.Mutex.Unlock()
	if _, exists := pm.Contexts[name]; exists {
		delete(pm.Contexts, name)
		return true
	}
	return false
}

// ListPrefixes 列出所有前缀
func (pm *PrefixManager) ListPrefixes() []string {
	pm.Mutex.RLock()
	defer pm.Mutex.RUnlock()
	prefixes := make([]string, 0, len(pm.Contexts))
	for name := range pm.Contexts {
		prefixes = append(prefixes, name)
	}
	return prefixes
}
