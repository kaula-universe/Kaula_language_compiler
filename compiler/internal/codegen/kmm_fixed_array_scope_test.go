package codegen

import (
	"strings"
	"testing"
)

// 本组用例锁定「定长值数组不触发 KMM 作用域」的插入策略改进。
//
// 背景：[16]i64 这类定长数组在 C 侧是纯栈对象（生成 int64_t buf[16] +
// 栈预算静态断言），整个函数不产生任何 KMM 分配。旧策略把所有 '[' 开头的
// 类型一律判为需要 KMM，于是给纯栈数组函数也套上
// kmm_v4_survivor_checkpoint + KMM_V4_SCOPE + survivor_rewind，
// 每次调用白付 TLS/作用域元数据开销 —— 内存是安全的，性能覆盖却很差。
//
// 改进：needsKMMForType 对定长数组剥离维度后按元素类型递归判定。
// 本组用例同时守住安全下界：含指针/字符串元素、切片、以及结构体元素
// 仍然必须保留 KMM 作用域，避免为了性能牺牲内存安全。

// kmmMarkers 统计生成 C 中 KMM 作用域/回收标记的数量，判断函数是否被套壳。
func countKMMMarkers(c string) (scope, checkpoint, rewind int) {
	scope = strings.Count(c, "KMM_V4_SCOPE_NAMED")
	checkpoint = strings.Count(c, "kmm_v4_survivor_checkpoint()")
	rewind = strings.Count(c, "kmm_v4_survivor_rewind(")
	return
}

// assertNoKMMWrapper 断言某段 C 完全没有被 KMM 作用域包裹。
func assertNoKMMWrapper(t *testing.T, name, c string) {
	t.Helper()
	scope, cp, rw := countKMMMarkers(c)
	if scope != 0 || cp != 0 || rw != 0 {
		t.Errorf("%s: 纯栈值数组不应插入 KMM 作用域，实际 scope=%d checkpoint=%d rewind=%d\n%s",
			name, scope, cp, rw, c)
	}
}

// ---- 1. 纯值定长数组：不插入 KMM ----

func TestFixedValueArrayNoKMMScope(t *testing.T) {
	src := `fn main() {
    [16]i64 buf
    buf[0] = 1
}`
	c := generateForTest(t, src)
	assertNoKMMWrapper(t, "[16]i64", c)
	if !strings.Contains(c, "buf[16]") {
		t.Errorf("应生成定长栈数组声明 buf[16]，实际：\n%s", c)
	}
}

func TestFixedMultiDimValueArrayNoKMMScope(t *testing.T) {
	src := `fn main() {
    [2][3]f64 grid
    grid[0][0] = 1.0
}`
	c := generateForTest(t, src)
	assertNoKMMWrapper(t, "[2][3]f64", c)
	if !strings.Contains(c, "grid[2][3]") {
		t.Errorf("应生成二维定长栈数组 grid[2][3]，实际：\n%s", c)
	}
}

func TestFixedConstSizedValueArrayNoKMMScope(t *testing.T) {
	src := `const N = 8
fn main() {
    [N]i64 buf
    buf[0] = 1
}`
	c := generateForTest(t, src)
	assertNoKMMWrapper(t, "[N]i64", c)
}

// ---- 2. 安全下界：这些类型仍然必须保留 KMM 作用域 ----

func TestFixedPointerArrayKeepsKMMScope(t *testing.T) {
	src := `fn main() {
    [4]i64* ps
    ps[0] = std_malloc(8)
}`
	c := generateForTest(t, src)
	scope, cp, rw := countKMMMarkers(c)
	if scope == 0 || cp == 0 || rw == 0 {
		t.Errorf("[4]i64* 元素含指针，必须保留 KMM 作用域，实际 scope=%d checkpoint=%d rewind=%d\n%s",
			scope, cp, rw, c)
	}
}

func TestFixedStringArrayKeepsKMMScope(t *testing.T) {
	src := `fn main() {
    [4]string names
    names[0] = "hi"
}`
	c := generateForTest(t, src)
	scope, cp, rw := countKMMMarkers(c)
	if scope == 0 || cp == 0 || rw == 0 {
		t.Errorf("[4]string 元素含字符串，必须保留 KMM 作用域，实际 scope=%d checkpoint=%d rewind=%d\n%s",
			scope, cp, rw, c)
	}
}

func TestSliceStillNeedsKMMScope(t *testing.T) {
	// 切片 []T 是堆分配（kmm_v4_alloc），不是定长栈数组，必须保留 KMM
	src := `fn main() {
    []i64 xs = [1, 2, 3]
    xs[0] = 4
}`
	c := generateForTest(t, src)
	scope, cp, rw := countKMMMarkers(c)
	if scope == 0 || cp == 0 || rw == 0 {
		t.Errorf("切片 []i64 是堆对象，必须保留 KMM 作用域，实际 scope=%d checkpoint=%d rewind=%d\n%s",
			scope, cp, rw, c)
	}
}

// 同一函数里既有纯值数组又有指针：指针触发 KMM，值数组不应抑制它
func TestMixedValueArrayAndPointerKeepsKMMScope(t *testing.T) {
	src := `fn main() {
    [16]i64 buf
    i64* p = std_malloc(8)
    buf[0] = 1
    p[0] = 2
}`
	c := generateForTest(t, src)
	scope, cp, rw := countKMMMarkers(c)
	if scope == 0 || cp == 0 || rw == 0 {
		t.Errorf("含指针分配的函数必须保留 KMM 作用域，实际 scope=%d checkpoint=%d rewind=%d\n%s",
			scope, cp, rw, c)
	}
}

// ---- 3. 循环内纯值数组：不插入 per-iteration survivor checkpoint ----

func TestFixedValueArrayInLoopNoSurvivorCheckpoint(t *testing.T) {
	src := `fn main() {
    i64 i = 0
    while (i < 10) {
        [16]i64 buf
        buf[0] = i
        i = i + 1
    }
}`
	c := generateForTest(t, src)
	if strings.Contains(c, "__while_surv_cp") || strings.Contains(c, "kmm_v4_survivor_checkpoint()") {
		t.Errorf("循环内纯值数组不应插入 survivor checkpoint，实际：\n%s", c)
	}
}

// ---- 4. 单元级：fixedArrayElementType / needsKMMForType 的判定表 ----

func TestFixedArrayElementType(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"[16]i64", "i64", true},
		{"[2][3]f64", "f64", true},
		{"[N]Point", "Point", true}, // 保留原大小写；needsKMMForType 调用前已 lower
		{"[]i64", "", false}, // 切片
		{"i64", "", false},
		{"[16", "", false}, // 畸形
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := fixedArrayElementType(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("fixedArrayElementType(%q) = (%q, %v), want (%q, %v)",
				tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNeedsKMMForTypeArrayRefinement(t *testing.T) {
	cases := []struct {
		typ  string
		want bool
	}{
		{"[16]i64", false},
		{"[2][3]f64", false},
		{"[100]int", false},
		{"[4]i64*", true},
		{"[4]string", true},
		{"[4]Point", true}, // 未知元素类型保守保留
		{"[]i64", true},    // 切片
		{"i64*", true},
		{"string", true},
		{"i64", false},
		{"f64", false},
		{"bool", false},
		{"void(i32)i32", false},
	}
	for _, tc := range cases {
		if got := needsKMMForType(tc.typ); got != tc.want {
			t.Errorf("needsKMMForType(%q) = %v, want %v", tc.typ, got, tc.want)
		}
	}
}
