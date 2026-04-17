package sema

import (
	"testing"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/stdlib"
	"kaula-compiler/internal/symbol"
)

// BenchmarkGenericFunctionAnalysis 基准测试：泛型函数分析
func BenchmarkGenericFunctionAnalysis(b *testing.B) {
	analyzer := NewSemanticAnalyzer()
	
	// 创建一个泛型函数
	fnStmt := &ast.FunctionStatement{
		Name: "genericFunc",
		TypeParams: []*ast.TypeParameter{
			{Name: "T", Constraint: "any"},
			{Name: "U", Constraint: "comparable"},
		},
		Params: []string{"a", "b"},
		Body: []ast.Statement{
			&ast.ExpressionStatement{
				Expression: &ast.Identifier{Name: "a"},
			},
		},
		Generic: true,
	}
	
	program := &ast.Program{
		Statements: []ast.Statement{fnStmt},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer.Analyze(program)
	}
}

// BenchmarkGenericInstantiation 基准测试：泛型实例化
func BenchmarkGenericInstantiation(b *testing.B) {
	analyzer := NewSemanticAnalyzer()
	
	// 先添加泛型函数到符号表
	analyzer.symbolTable.AddGenericSymbol("Swap", "function", []string{"T", "U"}, false, "global", 1, 1)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analyzer.symbolTable.InstantiateGeneric("Swap", []string{"int", "string"})
	}
}

// BenchmarkTypeConstraintChecking 基准测试：类型约束检查
func BenchmarkTypeConstraintChecking(b *testing.B) {
	analyzer := NewSemanticAnalyzer()
	
	// 设置类型约束
	analyzer.typeConstraints["T"] = []string{"comparable"}
	analyzer.typeConstraints["U"] = []string{"number"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer.checkTypeConstraint("int", "comparable", 1, 1)
		analyzer.checkTypeConstraint("float", "number", 1, 1)
	}
}

// BenchmarkSymbolTableLookup 基准测试：符号表查找
func BenchmarkSymbolTableLookup(b *testing.B) {
	table := symbol.NewSymbolTable(nil, "global")
	
	// 添加一些符号
	for i := 0; i < 100; i++ {
		table.AddSymbol("var"+string(rune(i)), "int", false, "local", 1, 1)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table.GetSymbol("var50")
	}
}

// BenchmarkGenericSymbolTableLookup 基准测试：泛型符号表查找
func BenchmarkGenericSymbolTableLookup(b *testing.B) {
	table := symbol.NewSymbolTable(nil, "global")
	
	// 添加泛型符号
	table.AddGenericSymbol("GenericFunc", "function", []string{"T"}, false, "global", 1, 1)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table.IsGenericType("GenericFunc")
		table.GetTypeParams("GenericFunc")
	}
}

// BenchmarkSimpleFunctionAnalysis 基准测试：简单函数分析
func BenchmarkSimpleFunctionAnalysis(b *testing.B) {
	source := `fn main() {
    let x = 10
    let y = 20
    let z = x + y
    println(z)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := NewSemanticAnalyzer()
		analyzer.Analyze(program)
	}
}

// BenchmarkVariableDeclarationAnalysis 基准测试：变量声明分析
func BenchmarkVariableDeclarationAnalysis(b *testing.B) {
	source := `fn main() {
    let int a = 10
    let float b = 3.14
    let string c = "hello"
    let bool d = true
    let int e = a + 10
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := NewSemanticAnalyzer()
		analyzer.Analyze(program)
	}
}

// BenchmarkFunctionCallAnalysis 基准测试：函数调用分析
func BenchmarkFunctionCallAnalysis(b *testing.B) {
	source := `fn add(a: int, b: int): int {
    return a + b
}

fn main() {
    let x = add(10, 20)
    let y = add(x, 30)
    println(y)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := NewSemanticAnalyzer()
		analyzer.Analyze(program)
	}
}

// BenchmarkClassAnalysis 基准测试：类分析
func BenchmarkClassAnalysis(b *testing.B) {
	source := `class Person {
    name: string
    age: int
    
    constructor(name: string, age: int) {
        self.name = name
        self.age = age
    }
    
    fn greet(): string {
        return "Hello, " + self.name
    }
}

fn main() {
    let p = Person("Alice", 30)
    println(p.greet())
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := NewSemanticAnalyzer()
		analyzer.Analyze(program)
	}
}

// BenchmarkLargeProgramAnalysis 基准测试：大型程序分析
func BenchmarkLargeProgramAnalysis(b *testing.B) {
	source := `
import io
import string

fn fibonacci(n: int): int {
    if n <= 1 {
        return n
    }
    return fibonacci(n - 1) + fibonacci(n - 2)
}

fn factorial(n: int): int {
    if n <= 1 {
        return 1
    }
    return n * factorial(n - 1)
}

class Calculator {
    value: int
    
    constructor() {
        self.value = 0
    }
    
    fn add(x: int) {
        self.value = self.value + x
    }
    
    fn get_value(): int {
        return self.value
    }
}

fn main() {
    let fib_result = fibonacci(10)
    let fact_result = factorial(10)
    
    println("Fibonacci: ")
    println(fib_result)
    
    println("Factorial: ")
    println(fact_result)
    
    let calc = Calculator()
    calc.add(fib_result)
    calc.add(fact_result)
    
    let total = calc.get_value()
    println("Total: ")
    println(total)
}
`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := NewSemanticAnalyzer()
		config, _ := stdlib.LoadStdlibConfig("stdlib.json")
		analyzer.stdlibConfig = config
		analyzer.Analyze(program)
	}
}

// BenchmarkImportAnalysis 基准测试：导入语句分析
func BenchmarkImportAnalysis(b *testing.B) {
	source := `
import io
import string
import memory

fn main() {
    println("All imports successful")
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := NewSemanticAnalyzer()
		config, _ := stdlib.LoadStdlibConfig("stdlib.json")
		analyzer.stdlibConfig = config
		analyzer.Analyze(program)
	}
}

// BenchmarkThirdPartyImportAnalysis 基准测试：第三方库导入分析
func BenchmarkThirdPartyImportAnalysis(b *testing.B) {
	source := `
import zlib
import stb_image

fn main() {
    let version = zlibVersion()
    let img = stbi_load("test.png", 0, 0, 0, 4)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := NewSemanticAnalyzer()
		config, _ := stdlib.LoadStdlibConfig("stdlib.json")
		analyzer.stdlibConfig = config
		analyzer.Analyze(program)
	}
}

