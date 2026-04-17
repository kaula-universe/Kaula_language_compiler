package parser

import (
	"testing"
	"kaula-compiler/internal/lexer"
)

// BenchmarkParseSimpleFunction 基准测试：解析简单函数
func BenchmarkParseSimpleFunction(b *testing.B) {
	source := `fn main() {
    let x = 10
    let y = 20
    let z = x + y
    println(z)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseFunctionCall 基准测试：解析函数调用
func BenchmarkParseFunctionCall(b *testing.B) {
	source := `fn main() {
    println("Hello, World!")
    let x = abs(-42)
    let y = max(10, 20)
    let z = min(x, y)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseControlFlow 基准测试：解析控制流
func BenchmarkParseControlFlow(b *testing.B) {
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
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseLargeSource 基准测试：解析大型源代码
func BenchmarkParseLargeSource(b *testing.B) {
	source := `
import io
import string
import memory

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

fn main() {
    let fib_result = fibonacci(10)
    let fact_result = factorial(10)
    
    println("Fibonacci(10): ")
    println(fib_result)
    
    println("Factorial(10): ")
    println(fact_result)
    
    let str1 = "Hello"
    let str2 = "World"
    let concatenated = str1 + " " + str2
    println(concatenated)
    
    let arr = [1, 2, 3, 4, 5]
    let sum = 0
    for i = 0; i < 5; i = i + 1 {
        sum = sum + arr[i]
    }
    println("Sum: ")
    println(sum)
    
    if sum > 10 {
        println("Sum is greater than 10")
    } else {
        println("Sum is less than or equal to 10")
    }
    
    switch sum {
        case 15:
            println("Sum is 15")
        case 20:
            println("Sum is 20")
        default:
            println("Sum is something else")
    }
}
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseClass 基准测试：解析类定义
func BenchmarkParseClass(b *testing.B) {
	source := `class Person {
    name: string
    age: int
    
    constructor(name: string, age: int) {
        self.name = name
        self.age = age
    }
    
    fn greet(): string {
        return "Hello, my name is " + self.name
    }
    
    fn get_age(): int {
        return self.age
    }
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseExpressions 基准测试：解析表达式
func BenchmarkParseExpressions(b *testing.B) {
	source := `fn main() {
    let a = 10 + 20 * 30 / 40 - 50
    let b = (10 + 20) * (30 - 40)
    let c = a > b && b > 0 || a < 100
    let d = c ? 1 : 0
    let e = a == b ? b == c ? 1 : 2 : 3
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseMemberAccess 基准测试：解析成员访问
func BenchmarkParseMemberAccess(b *testing.B) {
	source := `fn main() {
    let obj = create_object()
    let val = obj.field
    let result = obj.method()
    let nested = obj.subobj.field
    let call = obj.subobj.method()
    let deep = obj.subobj.subsubobj.field
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseImportStatements 基准测试：解析导入语句
func BenchmarkParseImportStatements(b *testing.B) {
	source := `
import io
import string
import memory
import container
import math
import system

fn main() {
    println("All imports successful")
}
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseNestedFunctions 基准测试：解析嵌套函数
func BenchmarkParseNestedFunctions(b *testing.B) {
	source := `
fn outer(): int {
    fn middle(): int {
        fn inner(): int {
            return 42
        }
        return inner()
    }
    return middle()
}

fn main() {
    let result = outer()
    println(result)
}
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}

// BenchmarkParseSwitchStatement 基准测试：解析 switch 语句
func BenchmarkParseSwitchStatement(b *testing.B) {
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
		lexer := lexer.NewLexer(source)
		parser := NewParser(lexer)
		parser.Parse()
	}
}
