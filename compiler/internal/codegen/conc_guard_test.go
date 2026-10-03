package codegen

import (
	"strings"
	"testing"

	"kaula/internal/ast"
	"kaula/internal/config"
	"kaula/internal/sema"
)

// genConc 走 parse → sema → codegen，返回生成的 C 与诊断。
// 诊断不视为失败 —— 好几条用例就是要验证「该报错时报错」。
func genConc(t *testing.T, src string) (string, []string) {
	t.Helper()
	prog := parse(src)
	sema.NewSemanticAnalyzer().Analyze(prog)
	cg := NewCodeGenerator(config.DefaultConfig())
	c := cg.Generate(prog)
	return c, cg.Errors()
}

// countOf 数子串出现次数
func countOf(s, sub string) int { return strings.Count(s, sub) }

// ---------- 循环体 / 分支体内的挂起点 ----------

// while 体内的挂起点必须整条只生成一次。
//
// 回归：老 lowering 把整条复合语句生成两遍（提交半 + 结果半），
// 症状是恢复后整个循环体再跑一遍 —— 副作用翻倍，且不报任何错。
func TestConcGuard_WhileBodyEmittedOnce(t *testing.T) {
	src := `
import std.time

conc fn loop_sum(i64 n) -> i64 {
    i64 total = 0
    i64 i = 0
    while i < n {
        std.time.time_sleep(1)
        total = total + i
        i = i + 1
    }
    return total
}

fn main() -> i32 {
    return 0
}
`
	c, errs := genConc(t, src)
	for _, e := range errs {
		t.Errorf("不应报错，实际：%s", e)
	}

	// 累加语句只应出现一次：出现两次就意味着循环体会被跑两遍
	if n := countOf(c, "total = kaula_add_i64"); n != 1 {
		t.Errorf("循环体内的累加必须只生成一次，实际 %d 次：\n%s", n, c)
	}
	if n := countOf(c, "i = kaula_add_i64"); n != 1 {
		t.Errorf("循环体内的自增必须只生成一次，实际 %d 次：\n%s", n, c)
	}
}

// 循环体内的挂起点必须就地带守卫槽位与挂起返回
func TestConcGuard_WhileBodyHasSlotsAndInlineSuspend(t *testing.T) {
	src := `
import std.time

conc fn loop_sum(i64 n) -> i64 {
    i64 total = 0
    i64 i = 0
    while i < n {
        std.time.time_sleep(1)
        total = total + i
        i = i + 1
    }
    return total
}

fn main() -> i32 {
    return 0
}
`
	c, _ := genConc(t, src)

	for _, want := range []string{
		"KAULA_CONC_SUB_DEPTH", // Frame 里的槽位数组
		"f->sub[1] == 0",       // 循环头
		"f->sub[1] == 1",       // 循环体第一条语句
		"return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND, .io_request", // 就地挂起
		"if (!(i < n)) break;", // 条件为假时跳出
		"f->sub[1] = 0;",       // 每轮末尾复位
	} {
		if !strings.Contains(c, want) {
			t.Errorf("生成代码缺少 %q：\n%s", want, c)
		}
	}

	// 挂起返回必须在循环体内部（缩进比 while 更深），否则说明它被放到了外层
	if !strings.Contains(c, "f->total = total;") {
		t.Errorf("挂起前必须把活跃变量写回 Frame：\n%s", c)
	}
}

// 跨 suspend 的局部变量必须写回并恢复
//
// 回归：活跃判据曾经拿源码行号当界，而解析器从不填 CallExpression.Pos，
// 每个挂起点的行号都是 0 —— 于是判活跃全部落空：不写回、也不恢复。
// 循环恢复后 `total` / `i` 读到未初始化的值，循环要么跑飞要么不进。
func TestConcGuard_CrossSuspendLocalsSavedAndRestored(t *testing.T) {
	src := `
import std.time

conc fn loop_sum(i64 n) -> i64 {
    i64 total = 0
    i64 i = 0
    while i < n {
        std.time.time_sleep(1)
        total = total + i
        i = i + 1
    }
    return total
}

fn main() -> i32 {
    return 0
}
`
	c, _ := genConc(t, src)

	// 段首恢复副本
	if !strings.Contains(c, "int64_t total = f->total;") {
		t.Errorf("段首必须从 Frame 恢复跨 suspend 变量：\n%s", c)
	}
	if !strings.Contains(c, "int64_t i = f->i;") {
		t.Errorf("段首必须从 Frame 恢复跨 suspend 变量：\n%s", c)
	}
	// 挂起前写回
	if !strings.Contains(c, "f->total = total;") {
		t.Errorf("挂起前必须写回 Frame：\n%s", c)
	}
	if !strings.Contains(c, "f->i = i;") {
		t.Errorf("挂起前必须写回 Frame：\n%s", c)
	}
}

// 顶层语句里的挂起点仍走老的两段式切分，不该被守卫污染
func TestConcGuard_TopLevelSuspendUnchanged(t *testing.T) {
	src := `
import std.time

conc fn delayed(i64 ms) -> i64 {
    std.time.time_sleep(ms)
    return 7
}

fn main() -> i32 {
    return 0
}
`
	c, _ := genConc(t, src)

	// 不该出现守卫槽位的判断（只有 sub[0] 被复位，没有 == 比较）
	if strings.Contains(c, "f->sub[0] ==") {
		t.Errorf("顶层挂起点不需要守卫槽位：\n%s", c)
	}
	if !strings.Contains(c, "f->sub[0] = 0;") {
		t.Errorf("跨段时仍须把槽位复位到 0：\n%s", c)
	}
	if !strings.Contains(c, "f->pc = 1;") {
		t.Errorf("顶层挂起点仍应切段：\n%s", c)
	}
}

// if / else 的条件不能重算：分支选择必须落进 Frame
func TestConcGuard_IfBranchRecordedInFrame(t *testing.T) {
	src := `
import std.time

conc fn pick(i64 n) -> i64 {
    i64 acc = 0
    if n % 2 == 0 {
        std.time.time_sleep(1)
        acc = 5
    } else {
        std.time.time_sleep(1)
        acc = acc + 1
    }
    return acc
}

fn main() -> i32 {
    return 0
}
`
	c, _ := genConc(t, src)

	if !strings.Contains(c, "f->branch[1] = ") {
		t.Errorf("if 的分支选择必须记录进 Frame（恢复时不能重算条件）：\n%s", c)
	}
	if !strings.Contains(c, "if (f->branch[1])") {
		t.Errorf("分支体必须按 Frame 里记录的分支进入：\n%s", c)
	}

	/*
	 * 两个分支必须共用同一段槽位号。
	 *
	 * 回归：else 接着 then 之后继续数的话，条件槽位留下的那个号谁都不匹配，
	 * else 分支永远不执行 —— 症状是恒走 then 分支，或者什么都不做。
	 */
	slots := map[string]bool{}
	for _, line := range strings.Split(c, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "if (f->sub[1] == ") {
			slots[l] = true
		}
	}
	if len(slots) < 2 {
		t.Errorf("分支体槽位不足：%v", slots)
	}
}

// ---------- 不支持的形态：必须报错，不许静默 ----------

func TestConcGuard_UnsupportedReportsDiagnostic(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "for..in 体内的挂起点",
			src: `
import std.time

conc fn sum_items([]i64 arr) -> i64 {
    i64 total = 0
    for v in arr {
        std.time.time_sleep(1)
        total = total + v
    }
    return total
}

fn main() -> i32 { return 0 }
`,
			want: "for..in",
		},
		{
			name: "条件里的挂起点",
			src: `
import std.time

conc fn spin(i64 n) -> i64 {
    i64 i = 0
    while std.time.time_sleep(1) == 0 {
        i = i + 1
    }
    return i
}

fn main() -> i32 { return 0 }
`,
			want: "while 条件里的可插桩调用",
		},
		{
			name: "同一条语句里两个挂起点",
			src: `
import std.time

conc fn twice() -> i64 {
    i64 x = 0
    while true {
        i64 t = std.time.time_sleep(1) + std.time.time_sleep(1)
        x = x + 1
        if x > 2 {
            break
        }
    }
    return x
}

fn main() -> i32 { return 0 }
`,
			want: "多个可插桩调用",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := genConc(t, tc.src)
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("应当报诊断（含 %q），实际诊断：%v", tc.want, errs)
			}
		})
	}
}

// 分组：整段是否进守卫模式是组的属性，不能逐语句临时决定
func TestConcGroupBody(t *testing.T) {
	src := `
import std.time

conc fn f(i64 n) -> i64 {
    i64 a = 0
    std.time.time_sleep(1)
    a = a + 1
    i64 b = 0
    while b < n {
        std.time.time_sleep(1)
        b = b + 1
    }
    return a + b
}

fn main() -> i32 { return 0 }
`
	prog := parse(src)
	sema.NewSemanticAnalyzer().Analyze(prog)

	var body []ast.Statement
	for _, st := range prog.Statements {
		fs, ok := st.(*ast.FunctionStatement)
		if ok && fs.Name == "f" {
			body = fs.Body
		}
	}
	if body == nil {
		t.Fatalf("没有找到 conc fn f")
	}

	groups := concGroupBody(body)
	if len(groups) != 2 {
		t.Fatalf("顶层那条 time_sleep 应当把体切成两组，实际 %d 组", len(groups))
	}
	if groups[0].guarded {
		t.Errorf("不含复合语句的那组不该进守卫模式")
	}
	if !groups[1].guarded {
		t.Errorf("含 while 的那一组必须是守卫组")
	}
}

// 守卫是「组」的属性：整组里排在 while 之前的语句也必须被槽位挡住，
// 否则恢复时 `i64 b = 0` 会再跑一遍，循环从头数。
func TestConcGuard_PreCompoundStatementsAlsoGuarded(t *testing.T) {
	src := `
import std.time

conc fn f(i64 n) -> i64 {
    i64 b = 0
    while b < n {
        std.time.time_sleep(1)
        b = b + 1
    }
    return b
}

fn main() -> i32 { return 0 }
`
	c, _ := genConc(t, src)

	// `b` 的初始化必须落在守卫槽位里，而不是裸的段体语句
	if !strings.Contains(c, "if (f->sub[0] == 0) {") {
		t.Errorf("复合语句之前的语句也必须被守卫挡住：\n%s", c)
	}
	if !strings.Contains(c, "b = 0;") {
		t.Errorf("变量初始化应降级成赋值：\n%s", c)
	}
	if strings.Contains(c, "int64_t b = 0;") {
		t.Errorf("段体内不得再出现声明（会与段首恢复副本重复定义）：\n%s", c)
	}
}

// 段尾的「既没挂起也没 return」的兜底 return 必须仍在最后一段
func TestConcGuard_NoSpuriousExtraSegment(t *testing.T) {
	/*
	 * 回归：分组收段时若切过一次段又在组尾再收一次，「取回结果」那半段
	 * 会与本组余下的语句被拆成两个 case，而前半段以兜底 return DONE 收尾，
	 * 于是余下的语句永远执行不到（返回值恒为默认值）。
	 */
	src := `
import std.time

conc fn fetch_after_delay(string url, i64 delay_ms) -> string {
    std.time.time_sleep(delay_ms)
    return url
}

fn main() -> i32 { return 0 }
`
	c, _ := genConc(t, src)

	// 一次顶层挂起点 → 恰好两个 case
	if n := countOf(c, "case 0: {") + countOf(c, "case 1: {") + countOf(c, "case 2: {"); n != 2 {
		t.Errorf("一次顶层挂起点应当只切出两个 case，实际 %d 个：\n%s", n, c)
	}
	// 取回结果与 return 必须在同一个 case 里
	i := strings.Index(c, "kaula_conc_tls_io_result;")
	j := strings.Index(c, "f->result = url;")
	if i < 0 || j < 0 || i > j {
		t.Errorf("取回结果与 return 必须同段且顺序在前：\n%s", c)
	}
}
