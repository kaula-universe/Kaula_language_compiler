package sor

import "fmt"

// ComptimeValue 表示编译期计算出的常量值。
type ComptimeValue struct {
	// Kind 是值的类型："int", "float", "bool", "string"
	Kind string

	// IntVal 是整数值（Kind == "int" 时有效）
	IntVal int64

	// FloatVal 是浮点数值（Kind == "float" 时有效）
	FloatVal float64

	// BoolVal 是布尔值（Kind == "bool" 时有效）
	BoolVal bool

	// StringVal 是字符串值（Kind == "string" 时有效）
	StringVal string
}

// ComptimeFeedback 是编译期计算结果反馈给 SOR 的接口。
// 它收集编译期常量信息，用于优化 SOR 的内存分配决策。
type ComptimeFeedback struct {
	// arraySizes 记录编译期已知的数组大小。
	// key: 数组变量名，value: 数组长度
	arraySizes map[string]int64

	// structSizes 记录编译期已知的结构体大小（字节）。
	// key: 结构体类型名，value: 大小（字节）
	structSizes map[string]int64

	// loopCounts 记录编译期已知的循环次数。
	// key: 循环标识（如函数名+行号），value: 迭代次数
	loopCounts map[string]int64
}

// NewComptimeFeedback 创建一个新的编译期反馈对象。
func NewComptimeFeedback() *ComptimeFeedback {
	return &ComptimeFeedback{
		arraySizes:  make(map[string]int64),
		structSizes: make(map[string]int64),
		loopCounts:  make(map[string]int64),
	}
}

// RegisterArraySize 注册编译期已知的数组大小。
func (cf *ComptimeFeedback) RegisterArraySize(varName string, size int64) {
	cf.arraySizes[varName] = size
}

// RegisterStructSize 注册编译期已知的结构体大小。
func (cf *ComptimeFeedback) RegisterStructSize(typeName string, size int64) {
	cf.structSizes[typeName] = size
}

// RegisterLoopCount 注册编译期已知的循环次数。
func (cf *ComptimeFeedback) RegisterLoopCount(loopID string, count int64) {
	cf.loopCounts[loopID] = count
}

// GetArraySize 获取编译期已知的数组大小，如果未知返回 -1。
func (cf *ComptimeFeedback) GetArraySize(varName string) (int64, bool) {
	size, ok := cf.arraySizes[varName]
	return size, ok
}

// GetStructSize 获取编译期已知的结构体大小，如果未知返回 -1。
func (cf *ComptimeFeedback) GetStructSize(typeName string) (int64, bool) {
	size, ok := cf.structSizes[typeName]
	return size, ok
}

// GetLoopCount 获取编译期已知的循环次数，如果未知返回 -1。
func (cf *ComptimeFeedback) GetLoopCount(loopID string) (int64, bool) {
	count, ok := cf.loopCounts[loopID]
	return count, ok
}

// EstimateArrayMemory 估算数组的内存使用（字节）。
// 如果编译期已知数组大小，使用精确值；否则使用估算值。
func (cf *ComptimeFeedback) EstimateArrayMemory(varName string, elemSize int64, estimatedSize int64) int64 {
	if size, ok := cf.arraySizes[varName]; ok {
		return size * elemSize
	}
	return estimatedSize * elemSize
}

// Summary 返回编译期反馈的摘要信息。
func (cf *ComptimeFeedback) Summary() string {
	return fmt.Sprintf(
		"ComptimeFeedback: %d 数组大小, %d 结构体大小, %d 循环次数",
		len(cf.arraySizes),
		len(cf.structSizes),
		len(cf.loopCounts),
	)
}
