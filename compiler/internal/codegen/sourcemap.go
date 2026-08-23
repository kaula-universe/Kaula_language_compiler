package codegen

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type SourceMapEntry struct {
	GeneratedLine int    `json:"generated_line"`
	SourceFile    string `json:"source_file"`
	SourceLine    int    `json:"source_line"`
	SourceColumn  int    `json:"source_column"`
	Kind          string `json:"kind,omitempty"`
	SymbolName    string `json:"symbol_name,omitempty"`
	// 调试增强字段
	CVariableName string `json:"c_variable_name,omitempty"` // KL 变量对应的 C 变量名
	CTypeName     string `json:"c_type_name,omitempty"`     // KL 类型对应的 C 类型名
	KLType        string `json:"kl_type,omitempty"`          // KL 原始类型信息
	Scope         string `json:"scope,omitempty"`            // 作用域标识（如函数名、类名）
}

// TypeMapping Kaula 类型到 C 类型的映射表
var TypeMapping = map[string]string{
	"i8":    "int8_t",
	"i16":   "int16_t",
	"i32":   "int32_t",
	"i64":   "int64_t",
	"u8":    "uint8_t",
	"u16":   "uint16_t",
	"u32":   "uint32_t",
	"u64":   "uint64_t",
	"f32":   "float",
	"f64":   "double",
	"bool":  "int",
	"char":  "char",
	"byte":  "uint8_t",
	"usize": "size_t",
	"isize": "intptr_t",
	"void":  "void",
}

// MapKLType 将 Kaula 类型名映射为对应的 C 类型名
func MapKLType(klType string) string {
	// 处理指针类型
	if strings.HasSuffix(klType, "*") {
		inner := strings.TrimSuffix(klType, "*")
		return MapKLType(inner) + "*"
	}
	// 处理 Option<T>
	if strings.HasPrefix(klType, "Option<") && strings.HasSuffix(klType, ">") {
		inner := strings.TrimPrefix(klType, "Option<")
		inner = strings.TrimSuffix(inner, ">")
		cInner := MapKLType(inner)
		return "struct { int tag; union { " + cInner + " value; } ; }"
	}
	// 处理 Result<T,E>
	if strings.HasPrefix(klType, "Result<") && strings.HasSuffix(klType, ">") {
		inner := strings.TrimPrefix(klType, "Result<")
		inner = strings.TrimSuffix(inner, ">")
		parts := strings.SplitN(inner, ",", 2)
		cOk := MapKLType(strings.TrimSpace(parts[0]))
		cErr := "Error"
		if len(parts) > 1 {
			cErr = MapKLType(strings.TrimSpace(parts[1]))
		}
		return "struct { int tag; union { " + cOk + " ok; " + cErr + " err; } ; }"
	}
	// 处理数组类型 [N]T
	if strings.HasPrefix(klType, "[") {
		// 简单映射为指针
		idx := strings.LastIndex(klType, "]")
		if idx > 0 {
			inner := klType[idx+1:]
			return MapKLType(inner) + "*"
		}
	}
	// 直接查表
	if cType, ok := TypeMapping[klType]; ok {
		return cType
	}
	// String 特殊处理
	if klType == "String" {
		return "String"
	}
	// 未知类型原样返回
	return klType
}

type SourceMap struct {
	Version int              `json:"version"`
	Source  string           `json:"source"`
	Target  string           `json:"target"`
	Entries []SourceMapEntry `json:"entries"`
	// 调试增强：变量映射表（KL 变量名 -> C 变量名）
	VariableMap map[string]string `json:"variable_map,omitempty"`
	// 调试增强：类型映射表（KL 类型名 -> C 类型名）
	TypeMap map[string]string `json:"type_map,omitempty"`
}

func NewSourceMap(sourceFile, targetFile string) *SourceMap {
	return &SourceMap{
		Version:     2,
		Source:      sourceFile,
		Target:      targetFile,
		Entries:     []SourceMapEntry{},
		VariableMap: map[string]string{},
		TypeMap:     map[string]string{},
	}
}

func (sm *SourceMap) AddEntry(genLine int, srcFile string, srcLine, srcCol int, kind, symbol string) {
	sm.Entries = append(sm.Entries, SourceMapEntry{
		GeneratedLine: genLine,
		SourceFile:    srcFile,
		SourceLine:    srcLine,
		SourceColumn:  srcCol,
		Kind:          kind,
		SymbolName:    symbol,
	})
}

// AddEntryDebug 添加带调试信息的源码映射条目
func (sm *SourceMap) AddEntryDebug(genLine int, srcFile string, srcLine, srcCol int, kind, symbol, klType, cVarName, scope string) {
	entry := SourceMapEntry{
		GeneratedLine: genLine,
		SourceFile:    srcFile,
		SourceLine:    srcLine,
		SourceColumn:  srcCol,
		Kind:          kind,
		SymbolName:    symbol,
		Scope:         scope,
	}
	if klType != "" {
		entry.KLType = klType
		entry.CTypeName = MapKLType(klType)
	}
	if cVarName != "" {
		entry.CVariableName = cVarName
	}
	sm.Entries = append(sm.Entries, entry)
}

// AddVariableMapping 记录 KL 变量到 C 变量的映射
func (sm *SourceMap) AddVariableMapping(klName, cName string) {
	if sm.VariableMap == nil {
		sm.VariableMap = map[string]string{}
	}
	sm.VariableMap[klName] = cName
}

// AddTypeMapping 记录 KL 类型到 C 类型的映射
func (sm *SourceMap) AddTypeMapping(klType, cType string) {
	if sm.TypeMap == nil {
		sm.TypeMap = map[string]string{}
	}
	sm.TypeMap[klType] = cType
}

func (sm *SourceMap) ToJSON() (string, error) {
	data, err := json.MarshalIndent(sm, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func LoadSourceMap(path string) (*SourceMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sm := &SourceMap{}
	if err := json.Unmarshal(data, sm); err != nil {
		return nil, err
	}
	return sm, nil
}

func (sm *SourceMap) LookupGenerated(genLine int) (SourceMapEntry, bool) {
	var best SourceMapEntry
	found := false
	for _, e := range sm.Entries {
		if e.GeneratedLine <= genLine {
			if !found || e.GeneratedLine > best.GeneratedLine {
				best = e
				found = true
			}
		}
	}
	return best, found
}

func (sm *SourceMap) LookupSource(srcLine int) (SourceMapEntry, bool) {
	var best SourceMapEntry
	found := false
	for _, e := range sm.Entries {
		if e.SourceLine <= srcLine {
			if !found || e.SourceLine > best.SourceLine {
				best = e
				found = true
			}
		}
	}
	return best, found
}

// LookupBySymbol 按符号名查找映射条目
func (sm *SourceMap) LookupBySymbol(symbol string) ([]SourceMapEntry, bool) {
	var results []SourceMapEntry
	for _, e := range sm.Entries {
		if e.SymbolName == symbol {
			results = append(results, e)
		}
	}
	return results, len(results) > 0
}

// LookupByScope 按作用域查找所有映射条目
func (sm *SourceMap) LookupByScope(scope string) ([]SourceMapEntry, bool) {
	var results []SourceMapEntry
	for _, e := range sm.Entries {
		if e.Scope == scope {
			results = append(results, e)
		}
	}
	return results, len(results) > 0
}

// GetVariableCName 获取 KL 变量对应的 C 变量名
func (sm *SourceMap) GetVariableCName(klName string) (string, bool) {
	if sm.VariableMap == nil {
		return "", false
	}
	cName, ok := sm.VariableMap[klName]
	return cName, ok
}

// GetCType 获取 KL 类型对应的 C 类型
func (sm *SourceMap) GetCType(klType string) (string, bool) {
	if sm.TypeMap == nil {
		cType, ok := TypeMapping[klType]
		return cType, ok
	}
	cType, ok := sm.TypeMap[klType]
	if !ok {
		cType, ok = TypeMapping[klType]
	}
	return cType, ok
}

type MappedBuilder struct {
	builder strings.Builder
	srcMap  *SourceMap
	curLine int
	srcFile string
}

func NewMappedBuilder(sourceFile, targetFile string) *MappedBuilder {
	return &MappedBuilder{
		srcMap:  NewSourceMap(sourceFile, targetFile),
		curLine: 0,
		srcFile: sourceFile,
	}
}

func (mb *MappedBuilder) Write(s string) {
	mb.builder.WriteString(s)
}

func (mb *MappedBuilder) WriteLine(s string) {
	mb.builder.WriteString(s)
	mb.builder.WriteString("\n")
	mb.curLine++
}

func (mb *MappedBuilder) WriteLineMap(s string, srcLine, srcCol int, kind, symbol string) {
	mb.builder.WriteString(s)
	mb.builder.WriteString("\n")
	mb.curLine++
	if srcLine > 0 {
		mb.srcMap.AddEntry(mb.curLine, mb.srcFile, srcLine, srcCol, kind, symbol)
	}
}

func (mb *MappedBuilder) WriteLinesMap(s string, srcLine, srcCol int, kind, symbol string) {
	startLine := mb.curLine + 1
	mb.builder.WriteString(s)
	lines := strings.Count(s, "\n")
	if len(s) > 0 && s[len(s)-1] != '\n' {
		mb.builder.WriteString("\n")
		lines++
	}
	mb.curLine += lines
	if srcLine > 0 && lines > 0 {
		mb.srcMap.AddEntry(startLine, mb.srcFile, srcLine, srcCol, kind, symbol)
	}
}

// WriteLineMapDebug 写入一行并记录带调试信息的源码映射
func (mb *MappedBuilder) WriteLineMapDebug(s string, srcLine, srcCol int, kind, symbol, klType, cVarName, scope string) {
	mb.builder.WriteString(s)
	mb.builder.WriteString("\n")
	mb.curLine++
	if srcLine > 0 {
		mb.srcMap.AddEntryDebug(mb.curLine, mb.srcFile, srcLine, srcCol, kind, symbol, klType, cVarName, scope)
	}
}

func (mb *MappedBuilder) String() string {
	return mb.builder.String()
}

func (mb *MappedBuilder) SourceMap() *SourceMap {
	return mb.srcMap
}

func (mb *MappedBuilder) CurrentLine() int {
	return mb.curLine
}

func (mb *MappedBuilder) Grow(n int) {
	mb.builder.Grow(n)
}

var _ = fmt.Sprintf
