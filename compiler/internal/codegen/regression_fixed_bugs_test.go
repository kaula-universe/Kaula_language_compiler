package codegen

import (
	"strings"
	"testing"

	"kaula/internal/config"
	"kaula/internal/sema"
)

// generateWithSourceMap 走完整 parse→sema→codegen 链路，返回最终 C 产物
// （含版本 banner，与写盘文件一致）与源码映射。
func generateWithSourceMap(t *testing.T, src string) (string, *SourceMap) {
	t.Helper()
	prog := parse(src)
	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)
	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)
	cCode := cg.Generate(prog)
	return cCode, cg.GetSourceMap()
}

// TestSourceMapGeneratedLinePointsAtRealLines 回归测试：
// map.json 的 generated_line 必须指向最终写盘 C 文件的真实行号。
//
// 历史缺陷（两个独立 bug 叠加，症状为所有条目系统性 -5 / global·func 条目 -4）：
//  1. 版本 banner 在映射条目换算之后才前置到产物头部，所有条目整体少 bannerLines 行；
//  2. 主分支 globalOffset 公式多算 1 行（forwardDecls 后的空行计数），经
//     funcOffset/mainFirstLine 传播到 global/func/main 条目。
func TestSourceMapGeneratedLinePointsAtRealLines(t *testing.T) {
	src := `import std.io

const capacity = 8

struct Student {
    name: string,
    score: i64
}

Student roster[capacity]
i64 count = 0

fn add_student(string name, i64 score) i64 {
    if (count >= capacity) {
        return 0
    }
    roster[count].name = name
    roster[count].score = score
    count = count + 1
    return 1
}

fn grade_of(i64 score) string {
    if (score >= 90) {
        return "A"
    }
    return "F"
}

fn main() {
    add_student("alice", 92)
    std.io.println(grade_of(92))
}
`
	cCode, sm := generateWithSourceMap(t, src)
	lines := strings.Split(cCode, "\n")
	if len(sm.Entries) == 0 {
		t.Fatal("源码映射没有任何条目")
	}
	for _, e := range sm.Entries {
		if e.GeneratedLine < 1 || e.GeneratedLine > len(lines) {
			t.Errorf("条目 %s(%s) generated_line=%d 超出产物行数 %d",
				e.SymbolName, e.Kind, e.GeneratedLine, len(lines))
			continue
		}
		content := lines[e.GeneratedLine-1]
		var hit bool
		switch e.Kind {
		case "struct", "class", "interface", "enum", "type":
			// 类型条目锚定在 typedef 行
			hit = strings.Contains(content, "typedef") && strings.Contains(content, e.SymbolName)
		case "function":
			hit = strings.Contains(content, e.SymbolName) && strings.Contains(content, "(")
		case "variable":
			hit = strings.Contains(content, e.SymbolName)
		default:
			hit = strings.Contains(content, e.SymbolName)
		}
		if !hit {
			t.Errorf("条目 %s(%s) generated_line=%d 未命中实际内容 %q",
				e.SymbolName, e.Kind, e.GeneratedLine, strings.TrimSpace(content))
		}
	}
}

// TestConstAsGlobalArraySize 回归测试：const 常量可用作全局数组尺寸。
// 此前 const 被折叠进常量表、不生成 C 定义，而数组尺寸原样透传到 C，
// clang 报 use of undeclared identifier 'capacity'。
func TestConstAsGlobalArraySize(t *testing.T) {
	src := `const capacity = 8
const double_cap = capacity * 2

struct Student {
    name: string,
    score: i64
}

Student roster[capacity]
i64 grid[double_cap]

fn main() {
    roster[0].score = 1
    grid[0] = 2
}
`
	cCode, _ := generateWithSourceMap(t, src)
	if strings.Contains(cCode, "[capacity]") {
		t.Errorf("const 数组尺寸未解析，产物仍含 [capacity]:\n%s", cCode)
	}
	if !strings.Contains(cCode, "Student roster[8]") {
		t.Errorf("期望生成 Student roster[8]，实际:\n%s", cCode)
	}
	if !strings.Contains(cCode, "int64_t grid[16]") {
		t.Errorf("期望 const 算术表达式尺寸解析为 grid[16]，实际:\n%s", cCode)
	}
}

// TestConstAsLocalArraySize 回归测试：const 常量可用作局部数组尺寸。
// 局部定长数组走栈声明路径（运行期尺寸才走堆分配），const 尺寸必须
// 在 convertType 之前解析，否则被误判为运行期数组生成 kmm_v4_alloc_global。
func TestConstAsLocalArraySize(t *testing.T) {
	src := `const capacity = 8

fn fill() {
    i64 local[capacity]
    local[0] = 42
}

fn main() {
    fill()
}
`
	cCode, _ := generateWithSourceMap(t, src)
	if strings.Contains(cCode, "[capacity]") {
		t.Errorf("const 局部数组尺寸未解析:\n%s", cCode)
	}
	if !strings.Contains(cCode, "int64_t local[8]") {
		t.Errorf("期望生成栈数组 int64_t local[8]，实际:\n%s", cCode)
	}
	if strings.Contains(cCode, "runtime-sized array") {
		t.Errorf("const 尺寸数组不应走运行期堆分配路径:\n%s", cCode)
	}
}

// TestArrayElementMemberAccessStringOps 回归测试：数组元素的字段访问
// （roster[i].name）必须能推断出字段类型。
//
// 此前 memberObjectClassName / indexElementKaulaType 均不处理
// IndexExpression 基座，类型推断回落为整数，导致两类症状：
//  1. println(roster[i].name) 生成 (int64_t)(String 结构体强转)，
//     clang 报 operand of type 'String' where arithmetic or pointer type is required；
//  2. roster[i].name == other 落不到 strcmp 分支，生成非法的
//     String == String 结构体比较。
func TestArrayElementMemberAccessStringOps(t *testing.T) {
	src := `import std.io

struct Student {
    name: string,
    score: i64
}

Student roster[8]

fn find(string name) i64 {
    for i in range(8) {
        if (roster[i].name == name) {
            return i
        }
    }
    return -1
}

fn main() {
    roster[0].name = "alice"
    roster[0].score = 92
    std.io.println(roster[0].name, roster[0].score)
    if (find("alice") >= 0) {
        std.io.println("found")
    }
}
`
	cCode, _ := generateWithSourceMap(t, src)
	// 1) 字段比较必须走 strcmp 分支（string_equals 内部同样是 strcmp）
	if !strings.Contains(cCode, "strcmp(string_cdata(roster[i].name), string_cdata(name))") {
		t.Errorf("roster[i].name == name 未生成 strcmp 比较:\n%s", cCode)
	}
	// 2) println 的 string 字段必须标为类型码 2（字符串），而非 0（整数）
	if !strings.Contains(cCode, "println_multi(2, 2, string_cdata(roster[0].name), 0, (int64_t)(roster[0].score))") {
		t.Errorf("println(roster[0].name, roster[0].score) 类型码推断错误:\n%s", cCode)
	}
}

// TestGlobalWithoutInitializerNoNilExpression 回归测试：无初始化器的
// 全局声明不得把 nil 表达式传入 generateExpression。
// 此前 `Student roster[8]`（无 = 初值）触发
// "[codegen] 警告: 未处理的表达式类型 <nil>，将当作 0 处理"。
// 产物行为不变：数组兜底 {0}，标量零初始化。
func TestGlobalWithoutInitializerNoNilExpression(t *testing.T) {
	src := `struct Student {
    name: string,
    score: i64
}

Student roster[8]
i64 counter

fn main() {
    roster[0].score = 1
    counter = 2
}
`
	cCode, _ := generateWithSourceMap(t, src)
	if !strings.Contains(cCode, "Student roster[8] = {0};") {
		t.Errorf("无初始化器数组应兜底 {0}:\n%s", cCode)
	}
	if !strings.Contains(cCode, "int64_t counter = 0;") {
		t.Errorf("无初始化器标量应零初始化:\n%s", cCode)
	}
}
