package codegen

import (
	"strings"
	"testing"

	"kaula/internal/sema"
)

/*
 * 下列测试守护三个「泛型 / ADT」层面的缺陷修复。
 *
 * 与 literal_codegen_regression_test.go 守护的缺陷不同，这三个不是静默错数，
 * 而是让**官方 tutorial 里写着的写法**根本编不过：
 *   1. 泛型 ADT（`Option<i32>`）的 match 绑定变量拿不到具体类型
 *   2. 泛型 class 单态化无限递归 → 同一个 typedef 输出几十份
 *   3. 泛型 class 丢掉引用语义、方法名未改名、外层函数返回值被吞
 *
 * 影响面：docs/tutorials/generics.md 里的 `Option<T>` / `Result<T,E>` /
 * `class Box<T>` 三个例子都走这几条路径。
 */

// TestGenericResultTryAssign 守护缺陷 A 的另一处后果。
//
// `:=` 错误传播同样要按 Result enum 的变体字段类型给左侧变量定类型；
// 泛型 Result（`Res<i32,i32>`）原先会因为 FindEnum 查不到实例化名而直接
// 报错 `try_assign_non_enum`，把「泛型 Result + :=」这一组合彻底堵死。
func TestGenericResultTryAssign(t *testing.T) {
	src := "import std.io\n" +
		"enum Res<T,E> { Ok(T), Err(E) }\n" +
		"fn fallible(i32 n) -> Res<i32,i32> {\n" +
		"    if (n == 0) { return Res.Err(7) }\n" +
		"    return Res.Ok(n * 3)\n" +
		"}\n" +
		"fn chain(i32 n) -> Res<i32,i32> {\n" +
		"    v := fallible(n)\n" +
		"    return Res.Ok(v + 1)\n" +
		"}\n" +
		"fn main() -> i32 {\n" +
		"    Res<i32,i32> r = chain(4)\n" +
		"    i32 out = 0\n" +
		"    match r {\n" +
		"        Ok(v) => { out = v }\n" +
		"        Err(e) => { out = e }\n" +
		"        _ => { out = -1 }\n" +
		"    }\n" +
		"    return out\n" +
		"}\n"
	prog := parse(src)
	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	if errs := analyzer.ErrorCollector().Errors(); len(errs) > 0 {
		var sb strings.Builder
		for _, e := range errs {
			sb.WriteString("\n  - " + e.Message)
		}
		t.Fatalf("泛型 Result 的 := 错误传播应可用，但语义分析报错：%s", sb.String())
	}
}

// TestGenericEnumPayloadBindingGetsConcreteType 守护缺陷 A。
//
// match 的绑定变量类型取自枚举变体的 FieldTypes；泛型声明里那是类型参数名
// （`T`）。必须先按 `<...>` 实参做一次替换，否则 `T` 既不是数值也不是指针，
// 后面任何算术都会落到 type_mismatch。
func TestGenericEnumPayloadBindingGetsConcreteType(t *testing.T) {
	src := "import std.io\n" +
		"enum Option<T> { Some(T), None }\n" +
		"fn main() -> i32 {\n" +
		"    Option<i32> o = Option.Some(42)\n" +
		"    i32 out = 0\n" +
		"    match o {\n" +
		"        Some(v) => { out = v + 1 }\n" +
		"        None => { out = 111 }\n" +
		"    }\n" +
		"    return out\n" +
		"}\n"
	prog := parse(src)
	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	if errs := analyzer.ErrorCollector().Errors(); len(errs) > 0 {
		var sb strings.Builder
		for _, e := range errs {
			sb.WriteString("\n  - " + e.Message)
		}
		t.Fatalf("泛型 ADT 的负载绑定应得到具体类型 i32，但语义分析报错：%s", sb.String())
	}
}

// TestGenericClassMonomorphizedOnce 守护缺陷 B。
//
// 单态化缓存原先在生成**之后**才写回。处理字段/方法时若再次请求同一实例，
// 查不到缓存就会重新展开——同一个 `typedef struct K_Box_i32` 被输出几十次，
// clang 报 redefinition。
func TestGenericClassMonomorphizedOnce(t *testing.T) {
	src := "class Box<T> {\n" +
		"    value: T\n" +
		"    constructor(T val) { self.value = val }\n" +
		"    fn get() T { return self.value }\n" +
		"}\n" +
		"fn main() {\n" +
		"    Box<i32> b = Box<i32>(42)\n" +
		"}\n"
	cCode := generateForTest(t, src)

	if n := strings.Count(cCode, "typedef struct K_Box_i32 {"); n != 1 {
		t.Errorf("K_Box_i32 的 typedef 应恰好出现 1 次，实际 %d 次", n)
	}
	if n := strings.Count(cCode, "static inline int32_t Box_i32_get("); n > 2 {
		// 允许「前向声明 + 定义」两份，超过说明单态化重复了
		t.Errorf("Box_i32_get 重复生成（%d 处），单态化缓存失效", n)
	}
}

// TestGenericClassReferenceSemanticsAndMethodMangling 守护缺陷 C 的前两半。
//
// class 是引用语义，实例化名必须登记进 classTypes 才能让变量声明退化成指针；
// 方法调用前缀必须用单态化后的名字，否则产出生 `Box<i32>_get(...)` 这种
// 非法 C 标识符。
func TestGenericClassReferenceSemanticsAndMethodMangling(t *testing.T) {
	src := "class Box<T> {\n" +
		"    value: T\n" +
		"    constructor(T val) { self.value = val }\n" +
		"    fn get() T { return self.value }\n" +
		"}\n" +
		"fn main() {\n" +
		"    Box<i32> b = Box<i32>(42)\n" +
		"    i32 got = b.get()\n" +
		"}\n"
	cCode := generateForTest(t, src)

	if !strings.Contains(cCode, "K_Box_i32* b = ") {
		t.Errorf("泛型 class 变量应按引用语义声明为指针，生成:\n%s", cCode)
	}
	if !strings.Contains(cCode, "Box_i32_get(b)") {
		t.Errorf("方法调用应改写为单态化名 Box_i32_get(b)，生成:\n%s", cCode)
	}
	if strings.Contains(cCode, "Box<i32>_get") {
		t.Errorf("方法名残留泛型尖括号，生成:\n%s", cCode)
	}
}

// TestGenericInstantiationKeepsOuterReturnType 守护缺陷 C 的第三半。
//
// 实例化过程中生成的构造器会把 currentFunctionReturnType 复位成 ""，
// 于是外层函数在实例化之后遇到的 `return <expr>` 退化成裸 `return;`
// —— clang 报 non-void function 缺返回值。
func TestGenericInstantiationKeepsOuterReturnType(t *testing.T) {
	src := "class Box<T> {\n" +
		"    value: T\n" +
		"    constructor(T val) { self.value = val }\n" +
		"}\n" +
		"fn main() -> i32 {\n" +
		"    Box<i32> b = Box<i32>(42)\n" +
		"    return 0\n" +
		"}\n"
	cCode := generateForTest(t, src)

	if strings.Contains(cCode, "    return;\n") {
		t.Errorf("`return 0` 被吞成裸 `return`，外层函数返回值丢失，生成:\n%s", cCode)
	}
	if !strings.Contains(cCode, "return") {
		t.Errorf("main 完全没有 return 语句，生成:\n%s", cCode)
	}
}
