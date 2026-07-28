package sor

import (
	"testing"
)

func TestBuiltinSizes(t *testing.T) {
	ts := NewTypeSizer()

	tests := []struct {
		typeName string
		expected int
	}{
		{"int8", 1}, {"uint8", 1}, {"byte", 1}, {"bool", 1},
		{"int16", 2}, {"uint16", 2},
		{"int32", 4}, {"uint32", 4}, {"float32", 4}, {"f32", 4}, {"float", 4},
		{"int64", 8}, {"uint64", 8}, {"float64", 8},
		{"int", 8}, {"uint", 8},
		{"double", 8}, {"f64", 8},
		{"i64", 8}, {"u64", 8},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			size := ts.SizeOf(tt.typeName)
			if size != tt.expected {
				t.Errorf("SizeOf(%q) = %d, want %d", tt.typeName, size, tt.expected)
			}
		})
	}
}

func TestPointerType(t *testing.T) {
	ts := NewTypeSizer()

	tests := []struct {
		typeName string
		expected int
	}{
		{"*int", 8},
		{"ptr", 8},
		{"int64ptr", 8},
		{"*string", 8},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			size := ts.SizeOf(tt.typeName)
			if size != tt.expected {
				t.Errorf("SizeOf(%q) = %d, want %d", tt.typeName, size, tt.expected)
			}
		})
	}
}

func TestStringType(t *testing.T) {
	ts := NewTypeSizer()
	size := ts.SizeOf("string")
	if size != 24 {
		t.Errorf("SizeOf(string) = %d, want 24", size)
	}
}

func TestSliceType(t *testing.T) {
	ts := NewTypeSizer()

	tests := []struct {
		typeName string
		expected int
	}{
		{"[]int", 24},
		{"[]string", 24},
		{"[]*int", 24},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			size := ts.SizeOf(tt.typeName)
			if size != tt.expected {
				t.Errorf("SizeOf(%q) = %d, want %d", tt.typeName, size, tt.expected)
			}
		})
	}
}

func TestArrayType(t *testing.T) {
	ts := NewTypeSizer()

	tests := []struct {
		typeName string
		expected int
	}{
		{"[10]int", 80},  // 10 * 8
		{"[5]int64", 40}, // 5 * 8
		{"[4]byte", 4},   // 4 * 1
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			size := ts.SizeOf(tt.typeName)
			if size != tt.expected {
				t.Errorf("SizeOf(%q) = %d, want %d", tt.typeName, size, tt.expected)
			}
		})
	}
}

func TestStructType(t *testing.T) {
	ts := NewTypeSizer()

	// 注册一个简单结构体
	ts.RegisterStructFields("point", []string{"int64", "int64"})
	size := ts.SizeOf("point")
	if size != 16 {
		t.Errorf("SizeOf(point) = %d, want 16", size)
	}

	// 测试带对齐的结构体
	ts.RegisterStructFields("aligned", []string{"int8", "int64"})
	size = ts.SizeOf("aligned")
	// int8(1) + padding(7) + int64(8) = 16
	if size != 16 {
		t.Errorf("SizeOf(aligned) = %d, want 16", size)
	}
}

func TestMapType(t *testing.T) {
	ts := NewTypeSizer()

	// map[string]int: 48 + 24(string) + 8(int) = 80
	size := ts.SizeOf("map[string]int")
	if size != 80 {
		t.Errorf("SizeOf(map[string]int) = %d, want 80", size)
	}

	// map[int64]*int: 48 + 8 + 8 = 64
	size = ts.SizeOf("map[int64]*int")
	if size != 64 {
		t.Errorf("SizeOf(map[int64]*int) = %d, want 64", size)
	}
}

func TestRecursiveType(t *testing.T) {
	ts := NewTypeSizer()

	// 递归类型：node 包含 *node
	// *node 是指针类型，返回 8 字节，不会触发递归检测
	// node = int64(8) + *node(8) = 16
	ts.RegisterStructFields("node", []string{"int64", "*node"})
	size := ts.SizeOf("node")
	if size != 16 {
		t.Errorf("SizeOf(node) = %d, want 16", size)
	}

	// 测试真正的递归类型（自引用结构体，非指针）
	// 这种情况在实际中很少见，但应该被检测到
	ts2 := NewTypeSizer()
	ts2.RegisterStructFields("linked", []string{"int64", "linked"})
	size2 := ts2.SizeOf("linked")
	// 递归类型返回保守估算值（应大于0，避免无限递归）
	if size2 <= 0 {
		t.Errorf("SizeOf(linked) = %d, should be > 0 for recursive type", size2)
	}
}

func TestRouteArena(t *testing.T) {
	ts := NewTypeSizer()

	tests := []struct {
		size     int
		expected AllocKind
	}{
		{0, AllocBumpPool},
		{4, AllocStack},
		{8, AllocStack},
		{32, AllocArenaTiny},
		{64, AllocArenaTiny},
		{128, AllocArenaSmall},
		{256, AllocArenaSmall},
		{512, AllocArenaMedium},
		{2048, AllocArenaMedium},
		{4096, AllocBumpPool},
	}

	for _, tt := range tests {
		t.Run(tt.expected.String(), func(t *testing.T) {
			result := ts.RouteArena(tt.size)
			if result != tt.expected {
				t.Errorf("RouteArena(%d) = %s, want %s", tt.size, result, tt.expected)
			}
		})
	}
}

func TestAlignOf(t *testing.T) {
	ts := NewTypeSizer()

	tests := []struct {
		typeName string
		expected int
	}{
		{"int8", 1},
		{"int16", 2},
		{"int32", 4},
		{"int64", 8},
		{"string", 8}, // 24 bytes, align = 8
		{"*int", 8},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			align := ts.AlignOf(tt.typeName)
			if align != tt.expected {
				t.Errorf("AlignOf(%q) = %d, want %d", tt.typeName, align, tt.expected)
			}
		})
	}
}

func TestCalculatePoolLayout(t *testing.T) {
	ts := NewTypeSizer()

	// 测试简单的对齐优化
	sizes := []int{8, 1, 4}
	aligns := []int{8, 1, 4}

	layout := ts.CalculatePoolLayout(sizes, aligns)
	// 按对齐降序排列: 8(align=8), 4(align=4), 1(align=1)
	// offset 0: 8 bytes
	// offset 8: 4 bytes (align 4, already aligned)
	// offset 12: 1 byte (align 1, already aligned)
	// total: 13
	if layout.TotalSize != 13 {
		t.Errorf("TotalSize = %d, want 13", layout.TotalSize)
	}
}

func TestParseStructFields(t *testing.T) {
	ts := NewTypeSizer()

	tests := []struct {
		name     string
		def      string
		expected int // expected number of fields
	}{
		{"simple", "struct{x:int,y:int}", 2},
		{"with spaces", "struct { x: int, y: int, z: string }", 3},
		{"empty", "struct{}", 0},
		{"no fields", "struct{x:int}", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := ts.ParseStructFields(tt.def)
			if len(fields) != tt.expected {
				t.Errorf("ParseStructFields(%q) returned %d fields, want %d", tt.def, len(fields), tt.expected)
			}
		})
	}
}
