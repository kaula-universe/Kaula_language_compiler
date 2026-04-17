package codegen

import (
	"testing"
	"kaula-compiler/internal/lexer"
	"kaula-compiler/internal/parser"
	"kaula-compiler/internal/sema"
	"kaula-compiler/internal/stdlib"
)

// BenchmarkGenerateSimpleFunction 基准测试：生成简单函数代码
func BenchmarkGenerateSimpleFunction(b *testing.B) {
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
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateVariableDeclarations 基准测试：生成变量声明代码
func BenchmarkGenerateVariableDeclarations(b *testing.B) {
	source := `fn main() {
    let int a = 10
    let float b = 3.14
    let string c = "hello"
    let bool d = true
    let int e = a + b
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateFunctionCalls 基准测试：生成函数调用代码
func BenchmarkGenerateFunctionCalls(b *testing.B) {
	source := `fn add(a: int, b: int): int {
    return a + b
}

fn sub(a: int, b: int): int {
    return a - b
}

fn mul(a: int, b: int): int {
    return a * b
}

fn main() {
    let x = add(10, 20)
    let y = sub(x, 5)
    let z = mul(y, 2)
    println(z)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateControlFlow 基准测试：生成控制流代码
func BenchmarkGenerateControlFlow(b *testing.B) {
	source := `fn main() {
    let x = 10
    
    if x > 5 {
        println("x is greater than 5")
    } else {
        println("x is less than or equal to 5")
    }
    
    while x > 0 {
        println(x)
        x = x - 1
    }
    
    for i = 0; i < 10; i = i + 1 {
        println(i)
    }
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateClass 基准测试：生成类代码
func BenchmarkGenerateClass(b *testing.B) {
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
    
    fn get_age(): int {
        return self.age
    }
}

fn main() {
    let p = Person("Alice", 30)
    println(p.greet())
    println(p.get_age())
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateLargeProgram 基准测试：生成大型程序代码
func BenchmarkGenerateLargeProgram(b *testing.B) {
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
		
		analyzer := sema.NewSemanticAnalyzer()
		config, _ := stdlib.LoadStdlibConfig("stdlib.json")
		analyzer.stdlibConfig = config
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.stdlibConfig = config
		cg.Generate(program)
	}
}

// BenchmarkGenerateExpressions 基准测试：生成表达式代码
func BenchmarkGenerateExpressions(b *testing.B) {
	source := `fn main() {
    let a = 10 + 20 * 30 / 40 - 50
    let b = (10 + 20) * (30 - 40)
    let c = a > b && b > 0 || a < 100
    let d = c ? 1 : 0
    let e = a == b ? b == c ? 1 : 2 : 3
    let f = a + b * c - d / e
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateMemberAccess 基准测试：生成成员访问代码
func BenchmarkGenerateMemberAccess(b *testing.B) {
	source := `class Outer {
    inner: Inner
    
    constructor() {
        self.inner = Inner()
    }
}

class Inner {
    value: int
    
    constructor() {
        self.value = 42
    }
}

fn main() {
    let obj = Outer()
    let val = obj.inner.value
    println(val)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateImportStatements 基准测试：生成导入语句代码
func BenchmarkGenerateImportStatements(b *testing.B) {
	source := `
import io
import string
import memory
import container

fn main() {
    println("All imports generated")
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		config, _ := stdlib.LoadStdlibConfig("stdlib.json")
		analyzer.stdlibConfig = config
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.stdlibConfig = config
		cg.Generate(program)
	}
}

// BenchmarkGenerateThirdPartyImports 基准测试：生成第三方库导入代码
func BenchmarkGenerateThirdPartyImports(b *testing.B) {
	source := `
import zlib
import stb_image

fn main() {
    let version = zlibVersion()
    let img = stbi_load("test.png", 0, 0, 0, 4)
    if img != null {
        stbi_image_free(img)
    }
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		config, _ := stdlib.LoadStdlibConfig("stdlib.json")
		analyzer.stdlibConfig = config
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.stdlibConfig = config
		cg.Generate(program)
	}
}

// BenchmarkGenerateSwitchStatement 基准测试：生成 switch 语句代码
func BenchmarkGenerateSwitchStatement(b *testing.B) {
	source := `fn main() {
    let x = 5
    switch x {
        case 1:
            println("One")
        case 2:
            println("Two")
        case 3:
            println("Three")
        case 4:
            println("Four")
        case 5:
            println("Five")
        default:
            println("Other")
    }
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}

// BenchmarkGenerateRecursiveFunctions 基准测试：生成递归函数代码
func BenchmarkGenerateRecursiveFunctions(b *testing.B) {
	source := `fn fibonacci(n: int): int {
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

fn gcd(a: int, b: int): int {
    if b == 0 {
        return a
    }
    return gcd(b, a % b)
}

fn main() {
    let fib = fibonacci(10)
    let fact = factorial(10)
    let g = gcd(48, 18)
    println(fib)
    println(fact)
    println(g)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lex := lexer.NewLexer(source)
		p := parser.NewParser(lex)
		program := p.Parse()
		
		analyzer := sema.NewSemanticAnalyzer()
		analyzer.Analyze(program)
		
		cg := NewCodeGenerator()
		cg.Generate(program)
	}
}
