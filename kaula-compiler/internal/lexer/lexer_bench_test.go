package lexer

import (
	"testing"
)

// BenchmarkSimpleExpression 基准测试：简单表达式
func BenchmarkSimpleExpression(b *testing.B) {
	source := `fn main() {
    let x = 10
    let y = 20
    let z = x + y
    println(z)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkFunctionCall 基准测试：函数调用
func BenchmarkFunctionCall(b *testing.B) {
	source := `fn main() {
    println("Hello, World!")
    let x = abs(-42)
    let y = max(10, 20)
    let z = min(x, y)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkControlFlow 基准测试：控制流
func BenchmarkControlFlow(b *testing.B) {
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
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkLargeSource 基准测试：大型源代码
func BenchmarkLargeSource(b *testing.B) {
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
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkStringLiterals 基准测试：字符串字面量
func BenchmarkStringLiterals(b *testing.B) {
	source := `fn main() {
    let str1 = "Hello, World!"
    let str2 = "This is a longer string with many characters"
    let str3 = "Short"
    let str4 = "Another string with \"escaped\" quotes"
    println(str1)
    println(str2)
    println(str3)
    println(str4)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkNumberLiterals 基准测试：数字字面量
func BenchmarkNumberLiterals(b *testing.B) {
	source := `fn main() {
    let int1 = 42
    let int2 = 1000000
    let float1 = 3.14159
    let float2 = 2.7182818284
    let result = int1 + int2 + float1 + float2
    println(result)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkIdentifiers 基准测试：标识符
func BenchmarkIdentifiers(b *testing.B) {
	source := `fn main() {
    let variable_name = 10
    let anotherVariable = 20
    let yet_another_variable = 30
    let x1 = 40
    let x2 = 50
    let result = variable_name + anotherVariable + yet_another_variable + x1 + x2
    println(result)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkComments 基准测试：注释处理
func BenchmarkComments(b *testing.B) {
	source := `fn main() {
    // This is a single-line comment
    let x = 10  // inline comment
    /* This is a
       multi-line
       comment */
    let y = 20
    # This is also a comment
    let z = x + y
    println(z)
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}

// BenchmarkOperators 基准测试：运算符
func BenchmarkOperators(b *testing.B) {
	source := `fn main() {
    let a = 10
    let b = 3
    let add = a + b
    let sub = a - b
    let mul = a * b
    let div = a / b
    let mod = a % b
    let eq = a == b
    let ne = a != b
    let lt = a < b
    let gt = a > b
    let le = a <= b
    let ge = a >= b
    let and = true && false
    let or = true || false
}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(source)
		for {
			tok := lexer.Next()
			if tok.Type == TOKEN_EOF {
				break
			}
		}
	}
}
