package parser_test

import (
	"testing"

	"kaula/internal/ast"
	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// TestCapitalizedTailOperandIsNotStructLiteral 守护：控制流块左花括号
// 紧跟在大写开头的标识符之后时，不能把这个标识符吃成结构体字面量。
//
// 曾经的判定只看"首字母大写 + 后面是 {", 于是
//
//	if Count > Limit { ... }
//
// 里 `Limit { ... }` 被解析成结构体字面量，条件退化为
// `Count > Limit{...}`，语义层报 "i64 与 struct 不可比较"，
// 生成层则输出 `Count > (Limit){0}` 这种非法 C。
//
// 触发条件与宏无关：任何大写开头的变量/常量出现在控制流条件末尾即可。
func TestCapitalizedTailOperandIsNotStructLiteral(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "if 条件尾操作数",
			src:  "fn main() {\n    i64 Limit = 3\n    i64 Count = 9\n    if Count > Limit { Count = 0 }\n}\n",
		},
		{
			name: "while 条件尾操作数",
			src:  "fn main() {\n    i64 Max = 3\n    i64 i = 0\n    while i < Max { i = i + 1 }\n}\n",
		},
		{
			name: "全局常量作尾操作数",
			src:  "const LIMIT = 200\nfn main() {\n    i64 a = 300\n    if a > LIMIT { a = 0 }\n}\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.NewParser(lexer.NewLexer(tc.src))
			prog := p.Parse()
			if p.HasErrors() {
				t.Fatalf("解析出错: %v", p.GetErrorCollector().Errors())
			}

			fn := prog.FindFunction("main")
			if fn == nil {
				t.Fatal("找不到 main")
			}

			// 找到 if / while 语句，取它的条件表达式
			var cond ast.Expression
			for _, s := range fn.Body {
				switch n := s.(type) {
				case *ast.IfStatement:
					cond = n.Condition
				case *ast.WhileStatement:
					cond = n.Condition
				}
				if cond != nil {
					break
				}
			}
			if cond == nil {
				t.Fatal("没有找到 if/while 条件")
			}

			bin, ok := cond.(*ast.BinaryExpression)
			if !ok {
				t.Fatalf("条件应为二元表达式, 实际 %T", cond)
			}
			right, ok := bin.Right.(*ast.Identifier)
			if !ok {
				t.Fatalf("条件右侧应为标识符, 实际 %T（说明被误判成结构体字面量）", bin.Right)
			}
			if _, bad := bin.Right.(*ast.StructLiteral); bad {
				t.Fatalf("条件右侧被误判成结构体字面量: %s", right.Name)
			}
		})
	}
}

// TestStructLiteralFormsStillParsed 守护：三种字段写法仍必须是结构体字面量。
// 修上一个缺陷时把判据从"首字母大写"改成"看花括号里的内容"，
// 这三种写法是必须同时成立的底线。
func TestStructLiteralFormsStillParsed(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "点号字段",
			src:  "struct Point { x: i64, y: i64 }\nfn main() {\n    Point p = Point { .x = 10, .y = 20 }\n}\n",
		},
		{
			name: "无点字段",
			src:  "struct Point { x: i64, y: i64 }\nfn main() {\n    Point p = Point { x = 10, y = 20 }\n}\n",
		},
		{
			name: "冒号字段",
			src:  "struct Point { x: i64, y: i64 }\nfn main() {\n    Point p = Point { x: 10, y: 20 }\n}\n",
		},
		{
			name: "空字面量",
			src:  "struct Point { x: i64, y: i64 }\nfn main() {\n    Point p = Point {}\n}\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.NewParser(lexer.NewLexer(tc.src))
			prog := p.Parse()
			if p.HasErrors() {
				t.Fatalf("解析出错: %v", p.GetErrorCollector().Errors())
			}

			fn := prog.FindFunction("main")
			if fn == nil {
				t.Fatal("找不到 main")
			}
			decl, ok := fn.Body[0].(*ast.VariableDeclaration)
			if !ok {
				t.Fatalf("第一条语句应为变量声明, 实际 %T", fn.Body[0])
			}
			if _, ok := decl.Value.(*ast.StructLiteral); !ok {
				t.Fatalf("右侧应为结构体字面量, 实际 %T", decl.Value)
			}
		})
	}
}
