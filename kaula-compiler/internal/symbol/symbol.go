package symbol

// Symbol 表示符号表中的一个符号
type Symbol struct {
	Name        string
	Type        string
	Nullable    bool
	NullChecked bool // 是否已通过 if x != null 检查（空指针安全）
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
	symbols      map[string]*Symbol
	genericTypes map[string][]string
	parent       *SymbolTable
	scopeName    string
	scopeDepth   int
	typeCache    map[string]*Symbol
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
	symbol, exists := st.symbols[name]

	if !exists || !symbol.IsGeneric {
		return nil, nil
	}

	instName := name + "<"
	for i, arg := range typeArgs {
		if i > 0 {
			instName += ","
		}
		instName += arg
	}
	instName += ">"

	if cached, ok := st.typeCache[instName]; ok {
		return cached, nil
	}

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

	st.typeCache[instName] = instSymbol

	return instSymbol, nil
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
	_, exists := st.symbols[name]
	return exists
}

// IsGenericType 检查是否是泛型类型
func (st *SymbolTable) IsGenericType(name string) bool {
	symbol, exists := st.symbols[name]
	return exists && symbol.IsGeneric
}

// GetTypeParams 获取类型参数
func (st *SymbolTable) GetTypeParams(name string) []string {
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
