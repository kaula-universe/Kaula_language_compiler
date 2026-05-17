package parser

import (
	"testing"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/ast"
)

func TestCStyleVariableDeclaration(t *testing.T) {
	testCases := []struct {
		Name         string
		Input        string
		ShouldError  bool
		ExpectedType string
		ExpectedName string
	}{
		// 基本类型
		{Name: "int 赋值", Input: "int x = 10", ShouldError: false, ExpectedType: "int", ExpectedName: "x"},
		{Name: "float 赋值", Input: "float f = 3.14", ShouldError: false, ExpectedType: "float", ExpectedName: "f"},
		{Name: "double 赋值", Input: "double d = 3.14159", ShouldError: false, ExpectedType: "double", ExpectedName: "d"},
		{Name: "bool 赋值", Input: "bool flag = true", ShouldError: false, ExpectedType: "bool", ExpectedName: "flag"},
		{Name: "char 赋值", Input: "char c = 'a'", ShouldError: false, ExpectedType: "char", ExpectedName: "c"},
		{Name: "string 赋值", Input: "string s = \"hello\"", ShouldError: false, ExpectedType: "string", ExpectedName: "s"},
		{Name: "void 变量", Input: "void v", ShouldError: false, ExpectedType: "void", ExpectedName: "v"},

		// 整数类型别名
		{Name: "i8 赋值", Input: "i8 a = 1", ShouldError: false, ExpectedType: "i8", ExpectedName: "a"},
		{Name: "i16 赋值", Input: "i16 b = 2", ShouldError: false, ExpectedType: "i16", ExpectedName: "b"},
		{Name: "i32 赋值", Input: "i32 c = 3", ShouldError: false, ExpectedType: "i32", ExpectedName: "c"},
		{Name: "i64 赋值", Input: "i64 d = 4", ShouldError: false, ExpectedType: "i64", ExpectedName: "d"},
		{Name: "u8 赋值", Input: "u8 e = 5", ShouldError: false, ExpectedType: "u8", ExpectedName: "e"},
		{Name: "u16 赋值", Input: "u16 f = 6", ShouldError: false, ExpectedType: "u16", ExpectedName: "f"},
		{Name: "u32 赋值", Input: "u32 g = 7", ShouldError: false, ExpectedType: "u32", ExpectedName: "g"},
		{Name: "u64 赋值", Input: "u64 h = 8", ShouldError: false, ExpectedType: "u64", ExpectedName: "h"},

		// 浮点类型别名
		{Name: "f32 赋值", Input: "f32 x = 1.0", ShouldError: false, ExpectedType: "f32", ExpectedName: "x"},
		{Name: "f64 赋值", Input: "f64 y = 2.0", ShouldError: false, ExpectedType: "f64", ExpectedName: "y"},

		// 类型别名
		{Name: "byte 赋值", Input: "byte b = 0", ShouldError: false, ExpectedType: "byte", ExpectedName: "b"},
		{Name: "sbyte 赋值", Input: "sbyte sb = -1", ShouldError: false, ExpectedType: "sbyte", ExpectedName: "sb"},
		{Name: "str 赋值", Input: "str s = \"hi\"", ShouldError: false, ExpectedType: "str", ExpectedName: "s"},
		{Name: "cstring 赋值", Input: "cstring cs = \"c\"", ShouldError: false, ExpectedType: "cstring", ExpectedName: "cs"},

		// 指针类型
		{Name: "指针 int*", Input: "int* p = &x", ShouldError: false, ExpectedType: "int*", ExpectedName: "p"},
		{Name: "指针 string*", Input: "string* sp = &s", ShouldError: false, ExpectedType: "string*", ExpectedName: "sp"},
		{Name: "前缀指针 *int", Input: "*int p = &x", ShouldError: false, ExpectedType: "int*", ExpectedName: "p"},

		// 可空类型
		{Name: "可空 string?", Input: "string? s = null", ShouldError: false, ExpectedType: "string", ExpectedName: "s"},
		{Name: "可空 int?", Input: "int? n = null", ShouldError: false, ExpectedType: "int", ExpectedName: "n"},

		// 无赋值
		{Name: "无赋值 int", Input: "int x", ShouldError: false, ExpectedType: "int", ExpectedName: "x"},
		{Name: "无赋值 string", Input: "string s", ShouldError: false, ExpectedType: "string", ExpectedName: "s"},

		// 自定义类型
		{Name: "自定义类型", Input: "MyType obj = MyType_new()", ShouldError: false, ExpectedType: "MyType", ExpectedName: "obj"},

		// 无效语法 - 应报错
		{Name: "错误 - 缺少变量名", Input: "int = 10", ShouldError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			lex := lexer.NewLexer(tc.Input)
			p := NewParser(lex)
			program := p.Parse()

			if !tc.ShouldError && program != nil && len(program.Statements) > 0 {
				if len(program.Statements) != 1 {
					t.Errorf("期望 1 条语句，但得到 %d 条", len(program.Statements))
					return
				}

				varDecl, ok := program.Statements[0].(*ast.VariableDeclaration)
				if !ok {
					t.Errorf("期望 VariableDeclaration 语句，但得到 %T", program.Statements[0])
					return
				}

				if varDecl.Name != tc.ExpectedName {
					t.Errorf("期望变量名为 '%s'，但得到 '%s'", tc.ExpectedName, varDecl.Name)
				}

				if varDecl.Type != tc.ExpectedType {
					t.Errorf("期望类型为 '%s'，但得到 '%s'", tc.ExpectedType, varDecl.Type)
				}

				if tc.ExpectedName != "" && varDecl.Name == "" {
					t.Errorf("变量名不应为空")
				}

				if tc.ExpectedType != "" && varDecl.Type == "" {
					t.Errorf("类型不应为空")
				}
			}

			if tc.ShouldError {
				// 对于错误用例，检查是否没有正确解析出变量声明
				if program != nil && len(program.Statements) > 0 {
					if varDecl, ok := program.Statements[0].(*ast.VariableDeclaration); ok {
						if varDecl != nil && varDecl.Name != "" && varDecl.Type != "" {
							t.Errorf("期望解析失败，但成功解析了变量声明: %s", tc.Input)
						}
					}
				}
			}
		})
	}
}
