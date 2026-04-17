package compiler

import (
	"testing"
)

// BenchmarkCompileSimpleProgram 基准测试：编译简单程序
func BenchmarkCompileSimpleProgram(b *testing.B) {
	source := `fn main() {
    let x = 10
    let y = 20
    let z = x + y
    println(z)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileFunctionCalls 基准测试：编译函数调用程序
func BenchmarkCompileFunctionCalls(b *testing.B) {
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
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileControlFlow 基准测试：编译控制流程序
func BenchmarkCompileControlFlow(b *testing.B) {
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
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileClassProgram 基准测试：编译类程序
func BenchmarkCompileClassProgram(b *testing.B) {
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
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileRecursiveProgram 基准测试：编译递归程序
func BenchmarkCompileRecursiveProgram(b *testing.B) {
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
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileLargeProgram 基准测试：编译大型程序
func BenchmarkCompileLargeProgram(b *testing.B) {
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
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileThirdPartyImports 基准测试：编译第三方库导入程序
func BenchmarkCompileThirdPartyImports(b *testing.B) {
	source := `
import zlib
import stb_image

fn main() {
    let version = zlibVersion()
    let img = stbi_load("test.png", 0, 0, 0, 4)
    if img != null {
        stbi_image_free(img)
    }
    println("Done")
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileExpressions 基准测试：编译复杂表达式
func BenchmarkCompileExpressions(b *testing.B) {
	source := `fn main() {
    let a = 10 + 20 * 30 / 40 - 50
    let b = (10 + 20) * (30 - 40)
    let c = a > b && b > 0 || a < 100
    let d = c ? 1 : 0
    let e = a == b ? b == c ? 1 : 2 : 3
    let f = a + b * c - d / e
    
    if a > b && b > c || c > d {
        println("Complex condition")
    }
    
    while a > 0 && b < 100 {
        a = a - 1
        b = b + 1
    }
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileNestedFunctions 基准测试：编译嵌套函数
func BenchmarkCompileNestedFunctions(b *testing.B) {
	source := `fn outer(): int {
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
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileSwitchStatement 基准测试：编译 switch 语句
func BenchmarkCompileSwitchStatement(b *testing.B) {
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
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileMultipleClasses 基准测试：编译多个类
func BenchmarkCompileMultipleClasses(b *testing.B) {
	source := `class Point {
    x: int
    y: int
    
    constructor(x: int, y: int) {
        self.x = x
        self.y = y
    }
    
    fn distance(): int {
        return self.x + self.y
    }
}

class Rectangle {
    top_left: Point
    width: int
    height: int
    
    constructor(x: int, y: int, w: int, h: int) {
        self.top_left = Point(x, y)
        self.width = w
        self.height = h
    }
    
    fn area(): int {
        return self.width * self.height
    }
}

fn main() {
    let rect = Rectangle(0, 0, 10, 20)
    let area = rect.area()
    println(area)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileArrayOperations 基准测试：编译数组操作
func BenchmarkCompileArrayOperations(b *testing.B) {
	source := `fn main() {
    let arr = [1, 2, 3, 4, 5]
    let sum = 0
    
    for i = 0; i < 5; i = i + 1 {
        sum = sum + arr[i]
    }
    
    println("Sum: ")
    println(sum)
    
    let max = arr[0]
    for i = 1; i < 5; i = i + 1 {
        if arr[i] > max {
            max = arr[i]
        }
    }
    
    println("Max: ")
    println(max)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileStringOperations 基准测试：编译字符串操作
func BenchmarkCompileStringOperations(b *testing.B) {
	source := `fn main() {
    let str1 = "Hello"
    let str2 = "World"
    let concatenated = str1 + " " + str2
    println(concatenated)
    
    let len = string_length(concatenated)
    println("Length: ")
    println(len)
    
    let upper = string_upper(concatenated)
    println(upper)
    
    let lower = string_lower(concatenated)
    println(lower)
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}

// BenchmarkCompileMemoryOperations 基准测试：编译内存操作
func BenchmarkCompileMemoryOperations(b *testing.B) {
	source := `import memory

fn main() {
    let ptr = fast_alloc(1024)
    if ptr != null {
        println("Allocated 1024 bytes")
        fast_free(ptr)
        println("Freed memory")
    }
    
    let arr = fast_calloc(10, 8)
    if arr != null {
        println("Allocated array of 10 elements")
        fast_free(arr)
    }
}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compile("test.kaula", source)
	}
}
