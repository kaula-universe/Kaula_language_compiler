// SOR (Sub-structural Ownership) 编译时验证器 - 演示程序
//
// 这是一个纯编译时验证的安全范式原型，零运行时开销。
// 运行此程序将展示 12 个测试用例，覆盖 SOR 的各种验证场景。
package main

import (
	"fmt"
	"os"
	"strings"

	"kaula-sor/sor"
)

// ============================================================================
// 测试用例定义
// ============================================================================

// TestCase 表示一个测试用例。
type TestCase struct {
	// Name 是测试用例名称。
	Name string

	// Description 是测试用例的详细描述。
	Description string

	// Category 是测试分类。
	Category string

	// SourceCode 是模拟的源码（用于显示）。
	SourceCode string

	// ExpectedResult 是预期结果："PASS" 表示验证通过，"FAIL" 表示应报错。
	ExpectedResult string

	// ExpectedErrorKind 是预期的错误类型（如果 ExpectedResult 为 "FAIL"）。
	ExpectedErrorKind sor.ErrorKind

	// Stmts 是要分析的语句列表。
	Stmts []sor.Stmt
}

// ============================================================================
// 测试用例 1: 基本 yeide 转移成功
// ============================================================================

func testCase1() TestCase {
	return TestCase{
		Name:            "基本 yeide 转移",
		Description:     "演示最基本的所有权转移：yeide 后原变量失效，新变量获得所有权",
		Category:        "yeide 原语",
		ExpectedResult:  "PASS",
		ExpectedErrorKind: -1,
		SourceCode: `let x = Array([1, 2, 3])   // x: Owned
yeide x -> y                   // x 所有权转移给 y
// x 现在是 Moved 状态，不能使用
// y 现在是 Owned 状态，可以读写
let z = y[0]                   // 读取 y（合法）`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([1, 2, 3])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.YeideStmt(2, "yeide x -> y", "x", "y"),
			sor.CommentStmt("// x 现在是 Moved 状态，不能使用"),
			sor.CommentStmt("// y 现在是 Owned 状态，可以读写"),
			sor.ReadStmt(4, "let z = y[0]  // 读取 y", "y"),
		},
	}
}

// ============================================================================
// 测试用例 2: use-after-move 错误
// ============================================================================

func testCase2() TestCase {
	return TestCase{
		Name:            "use-after-move 错误",
		Description:     "yeide 后再使用原变量，应触发 use-after-move 错误",
		Category:        "安全检查",
		ExpectedResult:  "FAIL",
		ExpectedErrorKind: sor.ErrUseAfterMove,
		SourceCode: `let x = Array([1, 2, 3])   // x: Owned
yeide x -> y                   // x 所有权转移给 y
let z = x[0]                   // 错误：使用已转移的 x
// ^ use-after-move!`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([1, 2, 3])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.YeideStmt(2, "yeide x -> y", "x", "y"),
			sor.ReadStmt(3, "let z = x[0]  // 使用已转移的 x", "x"),
		},
	}
}

// ============================================================================
// 测试用例 3: release 分发成功（DAG）
// ============================================================================

func testCase3() TestCase {
	return TestCase{
		Name:            "release 分发（DAG）",
		Description:     "release 将所有权分发给多个持有者，共享只读访问，构成合法 DAG",
		Category:        "release 原语",
		ExpectedResult:  "PASS",
		ExpectedErrorKind: -1,
		SourceCode: `let x = Array([1, 2, 3])   // x: Owned
release x -> [a, b, c]         // a,b,c 共享 x 的只读访问
// x, a, b, c 都是 Released 状态
let val1 = a[0]                // 读 a（合法）
let val2 = b[1]                // 读 b（合法）
let val3 = c[2]                // 读 c（合法）`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([1, 2, 3])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.ReleaseStmt(2, "release x -> [a, b, c]", "x", "a", "b", "c"),
			sor.CommentStmt("// x, a, b, c 都是 Released 状态"),
			sor.ReadStmt(4, "let val1 = a[0]  // 读 a", "a"),
			sor.ReadStmt(5, "let val2 = b[1]  // 读 b", "b"),
			sor.ReadStmt(6, "let val3 = c[2]  // 读 c", "c"),
		},
	}
}

// ============================================================================
// 测试用例 4: release 环检测错误
// ============================================================================

func testCase4() TestCase {
	return TestCase{
		Name:            "release 环检测",
		Description:     "release 关系必须构成 DAG，循环 release 会被检测为错误",
		Category:        "DAG 检测",
		ExpectedResult:  "FAIL",
		ExpectedErrorKind: sor.ErrCycleDetected,
		SourceCode: `let x = Array([1, 2, 3])
let y = Array([4, 5, 6])
release x -> [a]                // x -> a
release a -> [y_ref]            // a -> y_ref  (形成间接环)
// 注：在原型中演示直接环
release x -> [y]                // 等等，换一种方式演示环
// 实际构造：x release 给 a，a release 给 b，b release 给 x -> 形成环`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([1, 2, 3])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
			),
			// 构造一个环：x -> a, a -> b, b -> x
			sor.CommentStmt("// 构造 release 环: x -> a -> b -> x"),
			sor.ReleaseStmt(3, "release x -> [a]", "x", "a"),
			sor.ReleaseStmt(4, "release a -> [b]", "a", "b"),
			sor.ReleaseStmt(5, "release b -> [x]", "b", "x"),
			// ^ 这条语句添加后形成环
		},
	}
}

// ============================================================================
// 测试用例 5: write-on-release 错误
// ============================================================================

func testCase5() TestCase {
	return TestCase{
		Name:            "write-on-release 错误",
		Description:     "release 状态的对象只能读不能写，修改会触发错误",
		Category:        "安全检查",
		ExpectedResult:  "FAIL",
		ExpectedErrorKind: sor.ErrWriteOnReleased,
		SourceCode: `let x = Array([1, 2, 3])   // x: Owned
release x -> [a]                // x: Released, a: Released
a[0] = 42                       // 错误：写已 release 的对象
// ^ write-on-released!`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([1, 2, 3])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.ReleaseStmt(2, "release x -> [a]", "x", "a"),
			sor.WriteStmt(3, "a[0] = 42  // 写已 release 的对象", "a"),
		},
	}
}

// ============================================================================
// 测试用例 6: extract 提取成功
// ============================================================================

func testCase6() TestCase {
	return TestCase{
		Name:            "extract 提取子元素",
		Description:     "从复合对象中提取子元素的独占所有权，原位置变为 null",
		Category:        "extract 原语",
		ExpectedResult:  "PASS",
		ExpectedErrorKind: -1,
		SourceCode: `let x = Array([10, 20, 30])  // x: Owned
extract x[2] -> elem           // 提取 x[2] 的所有权
// elem: Owned (int)
// x[2]: Hollow (null)
let val = elem                 // 读取 elem（合法，独占所有权）
x[0] = 100                     // 写 x[0]（合法，未被提取）`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([10, 20, 30])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.ExtractStmt(2, "extract x[2] -> elem", "x", "[2]", "elem"),
			sor.CommentStmt("// elem: Owned(int), x[2]: Hollow(null)"),
			sor.ReadStmt(4, "let val = elem  // 读 elem", "elem"),
			sor.ReadStmt(5, "x[0] = 100     // 写 x[0]（未提取，合法）", "x"),
		},
	}
}

// ============================================================================
// 测试用例 7: extract 后 null 安全错误
// ============================================================================

func testCase7() TestCase {
	return TestCase{
		Name:            "extract 后 null 安全",
		Description:     "extract 后原位置变为 null，再访问该位置会触发空解引用错误",
		Category:        "空安全检查",
		ExpectedResult:  "FAIL",
		ExpectedErrorKind: sor.ErrNullDereference,
		SourceCode: `let x = Array([10, 20, 30])
extract x[1] -> elem           // 提取 x[1]
let val = x[1]                 // 错误：访问已提取的位置（null）
// ^ null dereference!`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([10, 20, 30])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.ExtractStmt(2, "extract x[1] -> elem", "x", "[1]", "elem"),
			// 直接访问 x[1] 子元素（通过子元素 ID 模拟）
			// 这里我们用一个特殊的方式：检查 x 的子元素
			// 为了演示，我们用 ReadStmt 但指向子元素的路径
			// 在实际实现中，这需要更复杂的索引追踪
			// 简化版：直接对 hollow 对象进行读操作
			{
				Kind:    sor.StmtRead,
				Line:    3,
				Source:  "let val = x[1]  // 访问已提取的位置",
				VarName: "x[1]", // 特殊名称，会通过 tracker 查找到 hollow 对象
			},
		},
	}
}

// ============================================================================
// 测试用例 8: extract + yeide 组合
// ============================================================================

func testCase8() TestCase {
	return TestCase{
		Name:            "extract + yeide 组合",
		Description:     "extract 出来的对象可以继续 yeide 转移，展示原语的组合能力",
		Category:        "原语组合",
		ExpectedResult:  "PASS",
		ExpectedErrorKind: -1,
		SourceCode: `let x = Array([10, 20, 30])
extract x[0] -> elem           // 提取元素
yeide elem -> z                // 转移提取出的元素
// z: Owned, elem: Moved
let result = z                 // 读 z（合法）
// elem 不能再使用`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([10, 20, 30])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.ExtractStmt(2, "extract x[0] -> elem", "x", "[0]", "elem"),
			sor.YeideStmt(3, "yeide elem -> z", "elem", "z"),
			sor.CommentStmt("// z: Owned, elem: Moved"),
			sor.ReadStmt(5, "let result = z  // 读 z", "z"),
			sor.CommentStmt("// elem 不能再使用（已 yeide）"),
		},
	}
}

// ============================================================================
// 测试用例 9: 跨作用域所有权检查
// ============================================================================

func testCase9() TestCase {
	return TestCase{
		Name:            "跨作用域所有权检查",
		Description:     "作用域结束后，该作用域内创建的独占对象会失效，外部不能访问",
		Category:        "作用域安全",
		ExpectedResult:  "FAIL",
		ExpectedErrorKind: sor.ErrUseAfterMove,
		SourceCode: `let outer = 42                 // outer: Owned (int)
{
    let inner = Array([1, 2])  // inner: Owned
    let val = inner[0]         // 读 inner（合法）
}
// inner 作用域结束，对象已销毁
let x = inner                  // 错误：inner 已不存在
// ^ use-after-scope!`,
		Stmts: func() []sor.Stmt {
			innerStmts := []sor.Stmt{
				sor.LetStmt(3, "    let inner = Array([1, 2])", "inner", "[]int", true,
					sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
					sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				),
				sor.ReadStmt(4, "    let val = inner[0]", "inner"),
			}
			result := []sor.Stmt{
				sor.LetStmt(1, "let outer = 42", "outer", "int", false),
			}
			result = append(result, sor.ScopeStmt("inner block", innerStmts...)...)
			result = append(result, sor.ReadStmt(6, "let x = inner  // 错误：inner 已销毁", "inner"))
			return result
		}(),
	}
}

// ============================================================================
// 测试用例 10: 多线程编译时安全验证
// ============================================================================

func testCase10() TestCase {
	return TestCase{
		Name:            "多线程安全验证",
		Description:     "多线程中 release 对象只读，跨线程写操作会被检测为错误",
		Category:        "并发安全",
		ExpectedResult:  "FAIL",
		ExpectedErrorKind: sor.ErrWriteOnReleased,
		SourceCode: `let x = Array([1, 2, 3])     // x: Owned
release x -> [shared]           // shared: Released (只读)
spawn thread1 {
    let val = shared[0]         // 读 shared（合法，只读）
    shared[0] = 99              // 错误：线程中写 release 对象
    // ^ thread write-on-released!
}`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let x = Array([1, 2, 3])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.ReleaseStmt(2, "release x -> [shared]", "x", "shared"),
			sor.ThreadStmt("thread1",
				sor.ReadStmt(4, "    let val = shared[0]  // 读 shared", "shared"),
				sor.WriteStmt(5, "    shared[0] = 99     // 错误：写 release 对象", "shared"),
			),
		},
	}
}

// ============================================================================
// 测试用例 11: 函数参数所有权标注检查
// ============================================================================

func testCase11() TestCase {
	return TestCase{
		Name:            "函数参数所有权标注",
		Description:     "函数参数标注 owned/release，调用时检查所有权匹配",
		Category:        "函数接口",
		ExpectedResult:  "PASS",
		ExpectedErrorKind: -1,
		SourceCode: `// fn consume(arr owned []int)  // 消耗所有权
// fn read_only(arr release []int)  // 只读访问
let x = Array([1, 2, 3])
let y = Array([4, 5, 6])
release y -> [y_view]
consume(x)                       // x 是 Owned，匹配 owned 参数
read_only(y_view)                // y_view 是 Released，匹配 release 参数
// x 现在是 Moved（被函数消耗）
// y_view 仍然是 Released（只读借用）`,
		Stmts: []sor.Stmt{
			sor.CommentStmt("// fn consume(arr owned []int)"),
			sor.CommentStmt("// fn read_only(arr release []int)"),
			sor.LetStmt(3, "let x = Array([1, 2, 3])", "x", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.LetStmt(4, "let y = Array([4, 5, 6])", "y", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
			),
			sor.ReleaseStmt(5, "release y -> [y_view]", "y", "y_view"),
			sor.CallStmt(6, "consume(x)  // owned 参数", "consume",
				[]string{"x"}, []string{"owned"}),
			sor.CallStmt(7, "read_only(y_view)  // release 参数", "read_only",
				[]string{"y_view"}, []string{"release"}),
			sor.CommentStmt("// x 现在是 Moved（被函数消耗）"),
			sor.CommentStmt("// y_view 仍然是 Released（只读借用）"),
		},
	}
}

// ============================================================================
// 测试用例 12: 复杂场景 - 三大原语综合
// ============================================================================

func testCase12() TestCase {
	return TestCase{
		Name:            "三大原语综合场景",
		Description:     "综合使用 yeide、release、extract，展示 SOR 的完整表达能力",
		Category:        "综合场景",
		ExpectedResult:  "PASS",
		ExpectedErrorKind: -1,
		SourceCode: `let data = Array([10, 20, 30, 40, 50])

// 第一步：提取关键元素
extract data[2] -> key_elem

// 第二步：分发只读视图给多个消费者
release data -> [reader_a, reader_b]

// 第三步：转移关键元素的所有权
yeide key_elem -> processed_key

// 各角色操作：
reader_a[0]                      // 合法：读 release 对象
reader_b[4]                      // 合法：读 release 对象
// data[2] = 99                   // 非法：data[2] 已提取 (null)
// reader_a[0] = 100              // 非法：写 release 对象
// key_elem                       // 非法：use-after-move

let final = processed_key        // 合法：读独占所有权对象`,
		Stmts: []sor.Stmt{
			sor.LetStmt(1, "let data = Array([10, 20, 30, 40, 50])", "data", "[]int", true,
				sor.Stmt{Kind: sor.StmtLet, VarName: "0", TypeName: "int", ChildPath: "[0]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "1", TypeName: "int", ChildPath: "[1]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "2", TypeName: "int", ChildPath: "[2]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "3", TypeName: "int", ChildPath: "[3]"},
				sor.Stmt{Kind: sor.StmtLet, VarName: "4", TypeName: "int", ChildPath: "[4]"},
			),
			sor.CommentStmt("// 第一步：提取关键元素"),
			sor.ExtractStmt(3, "extract data[2] -> key_elem", "data", "[2]", "key_elem"),
			sor.CommentStmt("// 第二步：分发只读视图给多个消费者"),
			sor.ReleaseStmt(5, "release data -> [reader_a, reader_b]", "data", "reader_a", "reader_b"),
			sor.CommentStmt("// 第三步：转移关键元素的所有权"),
			sor.YeideStmt(7, "yeide key_elem -> processed_key", "key_elem", "processed_key"),
			sor.CommentStmt("// 各角色操作："),
			sor.ReadStmt(9, "reader_a[0]  // 读 release 对象", "reader_a"),
			sor.ReadStmt(10, "reader_b[4]  // 读 release 对象", "reader_b"),
			sor.CommentStmt("// data[2] = 99  // 非法：已提取 (null)"),
			sor.CommentStmt("// reader_a[0] = 100  // 非法：写 release 对象"),
			sor.CommentStmt("// key_elem  // 非法：use-after-move"),
			sor.ReadStmt(15, "let final = processed_key  // 读独占对象", "processed_key"),
		},
	}
}

// ============================================================================
// 主程序
// ============================================================================

func main() {
	// 所有测试用例
	testCases := []TestCase{
		testCase1(),
		testCase2(),
		testCase3(),
		testCase4(),
		testCase5(),
		testCase6(),
		testCase7(),
		testCase8(),
		testCase9(),
		testCase10(),
		testCase11(),
		testCase12(),
	}

	// 打印横幅
	printBanner()

	// 运行所有测试用例
	passed := 0
	failed := 0
	total := len(testCases)

	for i, tc := range testCases {
		result := runTestCase(i+1, tc)
		if result {
			passed++
		} else {
			failed++
		}
	}

	// 打印总结
	printSummary(total, passed, failed)
}

// ============================================================================
// 辅助函数
// ============================================================================

// printBanner 打印程序横幅。
func printBanner() {
	fmt.Println()
	fmt.Println(strings.Repeat("═", 72))
	fmt.Println("  SOR (Sub-structural Ownership) 编译时验证器  v0.1")
	fmt.Println("  纯编译时验证的安全范式  ·  零运行时开销")
	fmt.Println(strings.Repeat("═", 72))
	fmt.Println()
	fmt.Println("  三大核心原语:")
	fmt.Println("    yeide   - 显式所有权转移")
	fmt.Println("    release - 分发只读访问 (DAG)")
	fmt.Println("    extract - 提取子结构所有权")
	fmt.Println()
	fmt.Println(strings.Repeat("─", 72))
	fmt.Println()
}

// runTestCase 运行单个测试用例并返回是否通过。
func runTestCase(num int, tc TestCase) bool {
	fmt.Printf("  [%2d] %s\n", num, tc.Name)
	fmt.Printf("       分类: %s\n", tc.Category)
	fmt.Printf("       描述: %s\n", tc.Description)
	fmt.Println()
	fmt.Println("       ─── 源码 ───")
	for _, line := range strings.Split(tc.SourceCode, "\n") {
		fmt.Printf("         %s\n", line)
	}
	fmt.Println()

	// 运行分析
	analyzer := sor.NewSORAnalyzer()
	errors := analyzer.Analyze(tc.Stmts)

	// 判断结果
	actualResult := "PASS"
	if len(errors) > 0 {
		actualResult = "FAIL"
	}

	// 检查是否符合预期
	passed := actualResult == tc.ExpectedResult
	if tc.ExpectedResult == "FAIL" && len(errors) > 0 {
		// 检查错误类型是否匹配
		foundExpected := false
		for _, err := range errors {
			if err.Kind == tc.ExpectedErrorKind {
				foundExpected = true
				break
			}
		}
		passed = foundExpected
	}

	// 打印执行日志（摘要）
	fmt.Println("       ─── 验证过程 ───")
	execLog := analyzer.GetExecLog()
	for _, line := range execLog {
		// 缩进显示
		fmt.Printf("       %s\n", indentLines(line, "  "))
	}
	fmt.Println()

	// 打印结果
	if len(errors) > 0 {
		fmt.Println("       ─── 检测到的错误 ───")
		for j, err := range errors {
			fmt.Printf("       (%d) %s\n", j+1, err.Error())
		}
		fmt.Println()
	}

	// 打印结论
	status := "PASS ✓"
	statusColor := "\033[32m" // 绿色
	resetColor := "\033[0m"

	if !passed {
		status = "FAIL ✗"
		statusColor = "\033[31m" // 红色
	}

	fmt.Printf("       预期: %s  |  实际: %s  |  结果: %s%s%s\n",
		tc.ExpectedResult, actualResult, statusColor, status, resetColor)
	fmt.Println()
	fmt.Println(strings.Repeat("─", 72))
	fmt.Println()

	return passed
}

// printSummary 打印测试总结。
func printSummary(total, passed, failed int) {
	fmt.Println()
	fmt.Println(strings.Repeat("═", 72))
	fmt.Println("  测试总结")
	fmt.Println(strings.Repeat("═", 72))
	fmt.Printf("  总计:   %d 个用例\n", total)
	fmt.Printf("  通过:   \033[32m%d\033[0m 个用例\n", passed)
	fmt.Printf("  失败:   \033[31m%d\033[0m 个用例\n", failed)
	fmt.Printf("  通过率: %.1f%%\n", float64(passed)/float64(total)*100)
	fmt.Println()
	fmt.Println("  SOR 验证规则覆盖:")
	fmt.Println("    ✓ Use-after-move 检查          (测试用例 2)")
	fmt.Println("    ✓ DAG 环检测                    (测试用例 4)")
	fmt.Println("    ✓ Write-on-release 检查         (测试用例 5)")
	fmt.Println("    ✓ Null-safety 检查              (测试用例 7)")
	fmt.Println("    ✓ Extract 独占性                (测试用例 6, 8)")
	fmt.Println("    ✓ 跨作用域安全                  (测试用例 9)")
	fmt.Println("    ✓ 多线程安全                    (测试用例 10)")
	fmt.Println("    ✓ 函数参数标注检查              (测试用例 11)")
	fmt.Println()

	if failed > 0 {
		os.Exit(1)
	}
}

// indentLines 对多行字符串进行缩进。
func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}
