package symbol

// Symbol 表示符号表中的一个符号
type Symbol struct {
	Name     string
	Type     string
	Nullable bool
	Scope    string
	Line     int
	Column   int
}

// SymbolTable 表示符号表
type SymbolTable struct {
	symbols     map[string]*Symbol
	parent      *SymbolTable
	scopeName   string
	scopeDepth  int
}

// NewSymbolTable 创建一个新的符号表
func NewSymbolTable(parent *SymbolTable, scopeName string) *SymbolTable {
	depth := 0
	if parent != nil {
		depth = parent.scopeDepth + 1
	}
	return &SymbolTable{
		symbols:     make(map[string]*Symbol),
		parent:      parent,
		scopeName:   scopeName,
		scopeDepth:  depth,
	}
}

// AddSymbol 添加一个符号
func (st *SymbolTable) AddSymbol(name, symbolType string, nullable bool, scope string, line, column int) {
	st.symbols[name] = &Symbol{
		Name:     name,
		Type:     symbolType,
		Nullable: nullable,
		Scope:    scope,
		Line:     line,
		Column:   column,
	}
}

// GetSymbol 获取一个符号
func (st *SymbolTable) GetSymbol(name string) *Symbol {
	if symbol, exists := st.symbols[name]; exists {
		return symbol
	}
	if st.parent != nil {
		return st.parent.GetSymbol(name)
	}
	return nil
}

// GetLocalSymbol 获取当前作用域中的符号
func (st *SymbolTable) GetLocalSymbol(name string) *Symbol {
	if symbol, exists := st.symbols[name]; exists {
		return symbol
	}
	return nil
}

// HasSymbol 检查是否存在符号
func (st *SymbolTable) HasSymbol(name string) bool {
	return st.GetSymbol(name) != nil
}

// HasLocalSymbol 检查当前作用域是否存在符号
func (st *SymbolTable) HasLocalSymbol(name string) bool {
	return st.GetLocalSymbol(name) != nil
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
