package symbol

import "sync"

// Symbol 表示符号表中的一个符号
type Symbol struct {
	Name        string
	Type        string
	Nullable    bool
	Scope       string
	Line        int
	Column      int
	IsGeneric   bool
	GenericInst *GenericInstanceInfo
}

// GenericInstanceInfo 存储泛型实例化信息
type GenericInstanceInfo struct {
	OriginalName  string
	TypeArguments []string
	Constraints   []string
}

// SymbolTable 表示符号表
type SymbolTable struct {
	symbols       map[string]*Symbol
	genericTypes  map[string][]string // 泛型类型参数映射
	parent        *SymbolTable
	scopeName     string
	scopeDepth    int
	mu            sync.RWMutex
	typeCache     map[string]*Symbol // 类型缓存
}

// NewSymbolTable 创建一个新的符号表
func NewSymbolTable(parent *SymbolTable, scopeName string) *SymbolTable {
	depth := 0
	if parent != nil {
		depth = parent.scopeDepth + 1
	}
	return &SymbolTable{
		symbols:      make(map[string]*Symbol),
		genericTypes: make(map[string][]string),
		typeCache:    make(map[string]*Symbol),
		parent:       parent,
		scopeName:    scopeName,
		scopeDepth:   depth,
	}
}

// AddSymbol 添加一个符号
func (st *SymbolTable) AddSymbol(name, symbolType string, nullable bool, scope string, line, column int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	
	// 清除缓存
	delete(st.typeCache, name)
	
	st.symbols[name] = &Symbol{
		Name:     name,
		Type:     symbolType,
		Nullable: nullable,
		Scope:    scope,
		Line:     line,
		Column:   column,
	}
}

// AddGenericSymbol 添加泛型符号
func (st *SymbolTable) AddGenericSymbol(name, symbolType string, typeParams []string, nullable bool, scope string, line, column int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	
	st.genericTypes[name] = typeParams
	delete(st.typeCache, name)
	
	st.symbols[name] = &Symbol{
		Name:      name,
		Type:      symbolType,
		Nullable:  nullable,
		Scope:     scope,
		Line:      line,
		Column:    column,
		IsGeneric: true,
		GenericInst: &GenericInstanceInfo{
			OriginalName:  name,
			TypeArguments: typeParams,
		},
	}
}

// InstantiateGeneric 实例化泛型类型
func (st *SymbolTable) InstantiateGeneric(name string, typeArgs []string) (*Symbol, error) {
	st.mu.RLock()
	symbol, exists := st.symbols[name]
	st.mu.RUnlock()
	
	if !exists || !symbol.IsGeneric {
		return nil, nil
	}
	
	// 生成实例化后的名称
	instName := name + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			instName += ","
		}
		instName += arg
	}
	instName += ">"
	
	// 检查缓存
	st.mu.RLock()
	if cached, ok := st.typeCache[instName]; ok {
		st.mu.RUnlock()
		return cached, nil
	}
	st.mu.RUnlock()
	
	// 创建实例化符号
	instSymbol := &Symbol{
		Name:      instName,
		Type:      symbol.Type,
		Nullable:  symbol.Nullable,
		Scope:     symbol.Scope,
		Line:      symbol.Line,
		Column:    symbol.Column,
		IsGeneric: false,
		GenericInst: &GenericInstanceInfo{
			OriginalName:  name,
			TypeArguments: typeArgs,
		},
	}
	
	// 添加到缓存
	st.mu.Lock()
	st.typeCache[instName] = instSymbol
	st.mu.Unlock()
	
	return instSymbol, nil
}

// GetSymbol 获取一个符号（线程安全）
func (st *SymbolTable) GetSymbol(name string) *Symbol {
	st.mu.RLock()
	defer st.mu.RUnlock()
	
	if symbol, exists := st.symbols[name]; exists {
		return symbol
	}
	if st.parent != nil {
		return st.parent.GetSymbol(name)
	}
	return nil
}

// GetLocalSymbol 获取当前作用域中的符号（线程安全）
func (st *SymbolTable) GetLocalSymbol(name string) *Symbol {
	st.mu.RLock()
	defer st.mu.RUnlock()
	
	if symbol, exists := st.symbols[name]; exists {
		return symbol
	}
	return nil
}

// HasSymbol 检查是否存在符号（线程安全）
func (st *SymbolTable) HasSymbol(name string) bool {
	return st.GetSymbol(name) != nil
}

// HasLocalSymbol 检查当前作用域是否存在符号（线程安全）
func (st *SymbolTable) HasLocalSymbol(name string) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	_, exists := st.symbols[name]
	return exists
}

// IsGenericType 检查是否是泛型类型
func (st *SymbolTable) IsGenericType(name string) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	
	symbol, exists := st.symbols[name]
	return exists && symbol.IsGeneric
}

// GetTypeParams 获取类型参数
func (st *SymbolTable) GetTypeParams(name string) []string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	
	return st.genericTypes[name]
}

// RemoveSymbol 移除符号
func (st *SymbolTable) RemoveSymbol(name string) {
	delete(st.symbols, name)
}

// GetScopeName 获取作用域名称
func (st *SymbolTable) GetScopeName() string {
	return st.scopeName
}

// GetScopeDepth 获取作用域深度
func (st *SymbolTable) GetScopeDepth() int {
	return st.scopeDepth
}

// GetParent 获取父符号表
func (st *SymbolTable) GetParent() *SymbolTable {
	return st.parent
}

// GetAllSymbols 获取所有符号
func (st *SymbolTable) GetAllSymbols() map[string]*Symbol {
	return st.symbols
}

// GetSymbolsInScope 获取指定作用域的符号
func (st *SymbolTable) GetSymbolsInScope(scope string) map[string]*Symbol {
	result := make(map[string]*Symbol)
	for name, symbol := range st.symbols {
		if symbol.Scope == scope {
			result[name] = symbol
		}
	}
	return result
}
