package sema

import (
	"kaula/internal/ast"
	"testing"
)

func TestNumericSpellingValidation(t *testing.T) {
	for _, typ := range []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "void(i32)f64", "*u8", "[8]i16"} {
		sa := NewSemanticAnalyzer()
		sa.validateNumericSpellings(&ast.Program{Statements: []ast.Statement{&ast.VariableDeclaration{Name: "value", Type: typ}}})
		if sa.ErrorCollector().HasErrors() {
			t.Errorf("canonical type rejected: %s", typ)
		}
	}
	for _, typ := range []string{"int", "uint8", "float", "double", "byte", "f8", "*int32", "[8]float64", "void(int)double"} {
		sa := NewSemanticAnalyzer()
		sa.validateNumericSpellings(&ast.Program{Statements: []ast.Statement{&ast.VariableDeclaration{Name: "value", Type: typ}}})
		if !sa.ErrorCollector().HasErrors() {
			t.Errorf("noncanonical numeric spelling accepted: %s", typ)
		}
	}
}

// TestNumericSpellingSkipsAdoptUnderlying 守护 adopt 底层的豁免。
//
// `type X = adopt <C 类型>` 的底层是**原始 C 类型**——adopt 的全部意义就是
// 不经过 Kaula 类型映射。`adopt int` 表达的是 C 的 int（32 位），
// 若被「int 必须写成 i32」这条规范化规则拦下，官方文档与 std/ctypes.kl 里的
// `type CInt = adopt int` 就彻底写不出来。
func TestNumericSpellingSkipsAdoptUnderlying(t *testing.T) {
	// adopt 的底层：常见 C 标量拼写一律放行
	for _, underlying := range []string{"int", "uint", "float", "double", "byte", "size_t", "long", "char"} {
		sa := NewSemanticAnalyzer()
		sa.validateNumericSpellings(&ast.Program{
			Statements: []ast.Statement{
				&ast.TypeAliasStatement{
					Name:            "T",
					IsAdopt:         true,
					AdoptUnderlying: underlying,
					UnderlyingType:  underlying,
				},
			},
		})
		if sa.ErrorCollector().HasErrors() {
			t.Errorf("adopt 底层 %q 不该被拼写规范化拦截，实际报错：%v",
				underlying, sa.ErrorCollector().Errors())
		}
	}

	// 非 adopt 的类型别名仍须规范化：type Bad = int 要报错
	sa := NewSemanticAnalyzer()
	sa.validateNumericSpellings(&ast.Program{
		Statements: []ast.Statement{
			&ast.TypeAliasStatement{Name: "Bad", IsAdopt: false, UnderlyingType: "int"},
		},
	})
	if !sa.ErrorCollector().HasErrors() {
		t.Error("非 adopt 的 `type Bad = int` 仍应报错（不应被豁免放宽）")
	}
}
