package codegen

import (
	"kaula/internal/ast"
	"strings"
	"testing"
)

// newAliasTypeGenerator 构造一个用于类型别名生成测试的 TypeGenerator，
// 可预注册 struct/class/enum 类型名。
func newAliasTypeGenerator(structs, classes, enums []string) *TypeGenerator {
	tg := &TypeGenerator{
		clibTypeMap:    make(map[string]string),
		structTypes:    make(map[string]bool),
		classTypes:     make(map[string]bool),
		enumTypes:      make(map[string]bool),
		typeAliasTypes: make(map[string]bool),
	}
	for _, s := range structs {
		tg.structTypes[s] = true
	}
	for _, c := range classes {
		tg.classTypes[c] = true
	}
	for _, e := range enums {
		tg.enumTypes[e] = true
	}
	return tg
}

// TestTypeAliasToStruct 验证 type P = Point（别名指向已定义 struct）
// 生成 typedef Point P;，而不是向 C 吐出不存在的裸名 Point。
func TestTypeAliasToStruct(t *testing.T) {
	tg := newAliasTypeGenerator([]string{"Point"}, nil, nil)
	stmt := &ast.TypeAliasStatement{Name: "P", UnderlyingType: "Point"}
	got := tg.GenerateTypeAliasStatement(stmt)
	t.Logf("生成:\n%s", got)
	if !strings.Contains(got, "typedef Point P;") {
		t.Errorf("struct 别名应生成 typedef Point P;，实际:\n%s", got)
	}
}

// TestTypeAliasToStructPointer 验证 type PNode = *Node 生成 Node* 指针别名。
func TestTypeAliasToStructPointer(t *testing.T) {
	tg := newAliasTypeGenerator([]string{"Node"}, nil, nil)
	stmt := &ast.TypeAliasStatement{Name: "PNode", UnderlyingType: "*Node"}
	got := tg.GenerateTypeAliasStatement(stmt)
	t.Logf("生成:\n%s", got)
	if !strings.Contains(got, "typedef Node* PNode;") {
		t.Errorf("struct 指针别名应生成 typedef Node* PNode;，实际:\n%s", got)
	}
}

// TestTypeAliasToEnum 验证 type C = Color（枚举值语义）生成 Color。
func TestTypeAliasToEnum(t *testing.T) {
	tg := newAliasTypeGenerator(nil, nil, []string{"Color"})
	// 枚举在 codegen 也登记进 structTypes（tagged union 形态），这里模拟登记
	tg.structTypes["Color"] = true
	stmt := &ast.TypeAliasStatement{Name: "C", UnderlyingType: "Color"}
	got := tg.GenerateTypeAliasStatement(stmt)
	if !strings.Contains(got, "typedef Color C;") {
		t.Errorf("enum 别名应生成 typedef Color C;，实际:\n%s", got)
	}
}

// TestTypeAliasBuiltinUnchanged 内置数值类型别名保持既有透传行为，不被自定义类型分支误伤。
func TestTypeAliasBuiltinUnchanged(t *testing.T) {
	tg := newAliasTypeGenerator(nil, nil, nil)
	stmt := &ast.TypeAliasStatement{Name: "MyInt", UnderlyingType: "i64"}
	got := tg.GenerateTypeAliasStatement(stmt)
	if !strings.Contains(got, "typedef i64 MyInt;") {
		t.Errorf("内置类型别名应保持既有 typedef i64 MyInt;，实际:\n%s", got)
	}
}
