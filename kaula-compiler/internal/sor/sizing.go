package sor

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ============================================================================
// 类型大小估算器 — 基于类型名称的编译期大小估算，用于精确路由到 KMM arena
// ============================================================================

// 内置基本类型大小（字节）
// 覆盖 Kaula 语言中所有标量类型及其别名
var builtinSizes = map[string]int{
	// 1 字节
	"int8":  1, "uint8": 1, "byte": 1, "bool": 1,
	// 2 字节
	"int16": 2, "uint16": 2,
	// 4 字节
	"int32":  4, "uint32": 4, "float32": 4, "f32": 4, "float": 4,
	// 8 字节
	"int64":   8, "uint64": 8, "float64": 8,
	"int":     8, "uint":     8,
	"size":    8, "isize": 8, "usize": 8,
	"double":  8, "f64":      8,
	"i64":     8, "u64":      8,
}

// TypeSizer 类型大小估算器
// 维护一个 typeName -> 字节大小的缓存，避免重复计算。
// pointerSize 为平台指针大小，默认 8（64 位平台）。
type TypeSizer struct {
	cache        map[string]int // typeName -> 字节大小
	pointerSize  int            // 平台指针大小（默认 8）
	structFields map[string][]string // 自定义结构体的字段类型注册表
}

// NewTypeSizer 创建默认的类型大小估算器（64 位平台）
func NewTypeSizer() *TypeSizer {
	return &TypeSizer{
		cache:        make(map[string]int),
		pointerSize:  8,
		structFields: make(map[string][]string),
	}
}

// NewTypeSizerWithPtrSize 创建指定指针大小的类型大小估算器
func NewTypeSizerWithPtrSize(ptrSize int) *TypeSizer {
	return &TypeSizer{
		cache:        make(map[string]int),
		pointerSize:  ptrSize,
		structFields: make(map[string][]string),
	}
}

// RegisterStructFields 注册自定义结构体的字段类型列表
// 结构体名使用小写形式作为 key，例如 "mypoint" 对应 "MyPoint"
func (ts *TypeSizer) RegisterStructFields(structName string, fieldTypes []string) {
	key := strings.ToLower(structName)
	ts.structFields[key] = fieldTypes
	// 清除缓存中可能存在的旧估算值
	delete(ts.cache, key)
}

// SizeOf 估算类型大小（字节）
// 返回 0 表示无法估算（保守策略，将走 bump pool）。
// 递归类型会被检测并返回 256（保守最大估算值）以避免无限递归。
func (ts *TypeSizer) SizeOf(typeName string) int {
	if typeName == "" {
		return 0
	}

	// 统一转小写进行比较
	key := strings.ToLower(typeName)

	// 查缓存
	if size, ok := ts.cache[key]; ok {
		return size
	}

	size := ts.sizeOfImpl(key, make(map[string]bool))
	ts.cache[key] = size
	return size
}

// sizeOfImpl 递归估算实现，seen 集合用于检测递归类型
func (ts *TypeSizer) sizeOfImpl(typeName string, seen map[string]bool) int {
	// 递归检测：同一个类型在当前路径中出现两次，说明是递归类型
	// 返回保守的最大估算值（256 字节），避免无限递归且不走 bump pool
	if seen[typeName] {
		return 256
	}
	seen[typeName] = true
	defer func() { delete(seen, typeName) }()

	// ---- 1. 基本类型：直接查表 ----
	if size, ok := builtinSizes[typeName]; ok {
		return size
	}

	// ---- 2. 指针类型 (*T 或 "ptr") ----
	if typeName == "ptr" || strings.HasPrefix(typeName, "*") {
		return ts.pointerSize
	}
	if strings.HasSuffix(typeName, "ptr") && len(typeName) > 3 {
		// 例如 "int64ptr" 视为指针
		return ts.pointerSize
	}

	// ---- 3. 字符串 (string, str) ----
	if typeName == "string" || typeName == "str" {
		// Go 风格：ptr(8) + len(8) + cap(8) = 24
		return 24
	}

	// ---- 4. 切片 ([]T) ----
	if strings.HasPrefix(typeName, "[]") {
		// Go 风格切片头：ptr(8) + len(8) + cap(8) = 24
		// 注意：切片头本身固定 24 字节，底层数组走 arena 分配
		return 24
	}

	// ---- 5. 数组 [N]T ----
	if arrSize, elemType, ok := ts.parseArrayType(typeName); ok {
		if arrSize <= 0 {
			return 0
		}
		elemSize := ts.sizeOfImpl(strings.ToLower(elemType), seen)
		if elemSize == 0 {
			return 0 // 元素大小未知，无法估算数组
		}
		return arrSize * elemSize
	}

	// ---- 6. 结构体（自定义） ----
	if fields, ok := ts.structFields[typeName]; ok {
		return ts.estimateStructSize(fields, seen)
	}

	// ---- 7. map[K]V ----
	if strings.HasPrefix(typeName, "map[") {
		// Go 风格 map 头：ptr(8) + count(8) + buckets(8) + ... 约 48 字节
		// 加上 key 和 value 类型的大小
		inner := typeName[4:] // 去掉 "map["
		closeBracket := strings.Index(inner, "]")
		if closeBracket > 0 {
			keyType := strings.ToLower(inner[:closeBracket])
			valueType := strings.ToLower(inner[closeBracket+1:])
			keySize := ts.sizeOfImpl(keyType, seen)
			valueSize := ts.sizeOfImpl(valueType, seen)
			if keySize > 0 && valueSize > 0 {
				return 48 + keySize + valueSize
			}
		}
		return 48 // 回退到仅头部大小
	}

	// ---- 8. 函数类型 (fn, fn(...)) ----
	if typeName == "fn" || strings.HasPrefix(typeName, "fn(") || strings.HasPrefix(typeName, "func(") {
		// 函数值 = 函数指针
		return ts.pointerSize
	}

	// ---- 9. 未知类型 ----
	return 0
}

// parseArrayType 从类型名称中解析数组大小和元素类型
// 支持格式："[N]T" 或 "array_N_T"
// 返回：数组长度, 元素类型, 是否解析成功
func (ts *TypeSizer) parseArrayType(typeName string) (int, string, bool) {
	// 格式 1: [N]T
	if strings.HasPrefix(typeName, "[") {
		closeBracket := strings.Index(typeName, "]")
		if closeBracket < 1 {
			return 0, "", false
		}
		nStr := typeName[1:closeBracket]
		n, err := strconv.Atoi(nStr)
		if err != nil || n <= 0 {
			return 0, "", false
		}
		elemType := typeName[closeBracket+1:]
		return n, elemType, true
	}

	// 格式 2: array_N_T（简化表示）
	if strings.HasPrefix(typeName, "array_") {
		parts := strings.SplitN(typeName, "_", 3)
		if len(parts) < 3 {
			return 0, "", false
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil || n <= 0 {
			return 0, "", false
		}
		return n, parts[2], true
	}

	return 0, "", false
}

// estimateStructSize 估算结构体大小：递归计算字段总和并加对齐填充
// 对齐规则：每个字段按其自然对齐边界对齐，结构体总大小按最大对齐边界对齐
func (ts *TypeSizer) estimateStructSize(fieldTypes []string, seen map[string]bool) int {
	if len(fieldTypes) == 0 {
		return 0
	}

	totalSize := 0
	maxAlign := 1 // 结构体整体最大对齐

	for _, ft := range fieldTypes {
		fieldSize := ts.sizeOfImpl(strings.ToLower(ft), seen)
		if fieldSize == 0 {
			// 字段大小未知，保守返回 0
			return 0
		}

		// 计算字段对齐值（取不超过字段大小的最大 2 的幂）
		align := naturalAlign(fieldSize)
		if align > maxAlign {
			maxAlign = align
		}

		// 对齐填充：当前偏移向上对齐到 align 的倍数
		offset := alignUp(totalSize, align)
		totalSize = offset + fieldSize
	}

	// 结构体总大小对齐到最大对齐边界
	totalSize = alignUp(totalSize, maxAlign)

	return totalSize
}

// naturalAlign 返回不超过 n 的最大 2 的幂
// 例如：1->1, 2->2, 3->2, 4->4, 5->4, 7->4, 8->8, 24->8
func naturalAlign(n int) int {
	if n <= 1 {
		return 1
	}
	align := 1
	for align*2 <= n {
		align *= 2
	}
	return align
}

// alignUp 将 offset 向上对齐到 align 的倍数
func alignUp(offset, align int) int {
	return (offset + align - 1) / align * align
}

// AlignOf 返回类型的自然对齐边界（字节）
// 对齐值取不超过类型大小的最大 2 的幂，最小为 1，最大为 8
func (ts *TypeSizer) AlignOf(typeName string) int {
	size := ts.SizeOf(typeName)
	if size <= 0 {
		return ts.pointerSize // 未知类型保守按指针对齐
	}
	align := naturalAlign(size)
	if align > 8 {
		return 8 // 最大对齐 8 字节
	}
	return align
}

// PoolLayout 池布局计算结果
type PoolLayout struct {
	TotalSize    int // 含对齐填充的总大小
	AlignmentPad int // 总对齐填充字节
}

// CalculatePoolLayout 模拟 bump pool 的实际布局
// 按对齐值降序排列对象（大对齐优先），最小化填充
// objSizes: 各对象的字节大小
// objAligns: 各对象的对齐要求
func (ts *TypeSizer) CalculatePoolLayout(objSizes []int, objAligns []int) *PoolLayout {
	if len(objSizes) == 0 {
		return &PoolLayout{}
	}

	type entry struct {
		size  int
		align int
	}
	entries := make([]entry, len(objSizes))
	for i := range objSizes {
		align := objAligns[i]
		if align <= 0 {
			align = naturalAlign(objSizes[i])
			if align > 8 {
				align = 8
			}
		}
		entries[i] = entry{objSizes[i], align}
	}

	// 按对齐值降序排序（大对齐优先，类似 struct packing 最优策略）
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].align > entries[j].align
	})

	offset := 0
	totalPad := 0
	for _, e := range entries {
		if e.size <= 0 {
			continue
		}
		aligned := alignUp(offset, e.align)
		totalPad += aligned - offset
		offset = aligned + e.size
	}

	return &PoolLayout{TotalSize: offset, AlignmentPad: totalPad}
}

// ============================================================================
// Arena 路由
// ============================================================================

// RouteArena 根据对象字节大小路由到合适的 KMM 分配位置
// 路由规则：
//
//	size <= 8     -> AllocStack      （寄存器 / 栈传递）
//	size <= 64    -> AllocArenaTiny   （tiny arena，适用于小标量和短结构体）
//	size <= 256   -> AllocArenaSmall  （small arena）
//	size <= 2048  -> AllocArenaMedium （medium arena）
//	size > 2048   -> AllocBumpPool    （大对象走 bump pool，批量回收）
func (ts *TypeSizer) RouteArena(sizeBytes int) AllocKind {
	switch {
	case sizeBytes <= 0:
		// 大小未知或为零，保守走 bump pool
		return AllocBumpPool
	case sizeBytes <= 8:
		return AllocStack
	case sizeBytes <= 64:
		return AllocArenaTiny
	case sizeBytes <= 256:
		return AllocArenaSmall
	case sizeBytes <= 2048:
		return AllocArenaMedium
	default:
		return AllocBumpPool
	}
}

// ============================================================================
// 结构体字段解析
// ============================================================================

// ParseStructFields 从结构体定义字符串中提取字段类型列表
// 支持输入格式：
//   - "struct{field1:Type1,field2:Type2,...}"
//   - "struct { field1: Type1, field2: Type2, ... }"
//
// 返回字段类型列表（仅类型部分），解析失败返回 nil。
func (ts *TypeSizer) ParseStructFields(structDef string) []string {
	// 去除所有空白
	def := strings.TrimSpace(structDef)

	// 提取花括号内的内容
	start := strings.Index(def, "{")
	end := strings.LastIndex(def, "}")
	if start < 0 || end < 0 || end <= start {
		return nil
	}

	body := def[start+1 : end]
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	// 按逗号分割字段
	parts := strings.Split(body, ",")
	fieldTypes := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// 分离字段名和类型：冒号分隔
		colonIdx := strings.Index(part, ":")
		if colonIdx < 0 {
			continue
		}

		typeStr := strings.TrimSpace(part[colonIdx+1:])
		if typeStr != "" {
			fieldTypes = append(fieldTypes, typeStr)
		}
	}

	return fieldTypes
}

// RegisterStructFromDef 从结构体定义字符串解析并注册字段类型
// 是 ParseStructFields + RegisterStructFields 的便捷组合。
// 返回解析到的字段类型列表，解析失败返回 nil。
func (ts *TypeSizer) RegisterStructFromDef(structName, structDef string) []string {
	fields := ts.ParseStructFields(structDef)
	if fields == nil {
		return nil
	}
	ts.RegisterStructFields(structName, fields)
	return fields
}

// ============================================================================
// 批量分析
// ============================================================================

// AnalyzeSizes 为 OwnershipTracker 中的所有对象计算估算大小
// 返回：objectID -> 字节大小 的映射。
// 大小为 0 的对象表示无法估算，应保守走 bump pool。
func (ts *TypeSizer) AnalyzeSizes(tracker *OwnershipTracker) map[string]int {
	if tracker == nil || tracker.GetObjectCount() == 0 {
		return nil
	}

	allObjs := tracker.GetAllObjects()
	result := make(map[string]int, len(allObjs))

	for _, obj := range allObjs {
		if obj == nil {
			continue
		}
		size := ts.SizeOf(obj.TypeName)
		result[obj.ID] = size
	}

	return result
}

// AnalyzeAndRoute 为 OwnershipTracker 中的所有对象计算大小并路由到 arena
// 返回：objectID -> AllocKind 的映射。
func (ts *TypeSizer) AnalyzeAndRoute(tracker *OwnershipTracker) map[string]AllocKind {
	if tracker == nil || tracker.GetObjectCount() == 0 {
		return nil
	}

	allObjs := tracker.GetAllObjects()
	result := make(map[string]AllocKind, len(allObjs))

	for _, obj := range allObjs {
		if obj == nil {
			continue
		}
		size := ts.SizeOf(obj.TypeName)
		result[obj.ID] = ts.RouteArena(size)
	}

	return result
}

// ============================================================================
// 工具方法
// ============================================================================

// PointerSize 返回当前平台指针大小
func (ts *TypeSizer) PointerSize() int {
	return ts.pointerSize
}

// CacheSize 返回缓存条目数
func (ts *TypeSizer) CacheSize() int {
	return len(ts.cache)
}

// ClearCache 清除大小缓存
func (ts *TypeSizer) ClearCache() {
	ts.cache = make(map[string]int)
}

// Summary 返回缓存中所有已计算类型的摘要
func (ts *TypeSizer) Summary() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("TypeSizer: %d cached types, ptr=%d\n", len(ts.cache), ts.pointerSize))

	// 按大小分组统计
	stackCount := 0
	tinyCount := 0
	smallCount := 0
	mediumCount := 0
	bumpCount := 0
	unknownCount := 0

	for _, size := range ts.cache {
		switch ts.RouteArena(size) {
		case AllocStack:
			stackCount++
		case AllocArenaTiny:
			tinyCount++
		case AllocArenaSmall:
			smallCount++
		case AllocArenaMedium:
			mediumCount++
		case AllocBumpPool:
			if size == 0 {
				unknownCount++
			} else {
				bumpCount++
			}
		}
	}

	b.WriteString(fmt.Sprintf("  Stack: %d, Tiny: %d, Small: %d, Medium: %d, Bump: %d, Unknown: %d\n",
		stackCount, tinyCount, smallCount, mediumCount, bumpCount, unknownCount))
	return b.String()
}

// ============================================================================
// 结构体名称提取辅助
// ============================================================================

// structNamePattern 用于识别 "struct{...}" 类型名的前缀
var structNamePattern = regexp.MustCompile(`^struct\s*\{.*\}$`)

// IsStructType 判断类型名称是否为内联结构体定义
// 例如：struct{x:int,y:int} 或 struct { x: int, y: int }
func (ts *TypeSizer) IsStructType(typeName string) bool {
	return structNamePattern.MatchString(typeName)
}
