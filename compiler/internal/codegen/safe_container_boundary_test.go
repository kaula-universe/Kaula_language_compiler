package codegen

import (
	"strings"
	"testing"

	"kaula/internal/ast"
)

// parseFuncBody 解析 src 并取出指定函数的函数体与形参名。
func parseFuncBody(t *testing.T, src, fnName string) ([]ast.Statement, []string) {
	t.Helper()
	prog := parse(src)
	for _, st := range prog.Statements {
		f, ok := st.(*ast.FunctionStatement)
		if !ok || f.Name != fnName {
			continue
		}
		return f.Body, f.Params
	}
	t.Fatalf("函数 %s 未找到", fnName)
	return nil, nil
}

// ============================================================================
// 方案 A'（docs/perf/P0-survivor-leak.md §6-§7）的边界护栏
// ============================================================================
//
// A' 的核心前提：只有「安全局部容器」（本函数内声明、且从不逃逸）才允许
//   - 写穿点不 promote；
//   - 所在循环体放弃 mark_restore；
//   - 挂进去的对象豁免 early free。
//
// 一旦容器**逃逸**（return / 实参 / 赋值右值 / yield / 闭包捕获），
// 它就不再是安全局部容器 —— 对象留在 bump 区会在函数出口 scope_pop 时悬垂。
// 因此这些形态必须回退到旧路径（promote + 回卷，靠泄漏换正确性）。
//
// 本文件的每个用例都断言"必须仍然走旧路径"，即 A' 不得越界。

// TestSafeContainerScanBasic 直接验证预扫描的判定结果。
func TestSafeContainerScanBasic(t *testing.T) {
	cases := []struct {
		name          string
		src           string
		fn            string
		wantSafe      []string
		wantNotSafe   []string
		wantWriteThru []string
	}{
		{
			name: "write-through + cursor, container never escapes",
			src: `import std.memory
struct Node { v: i32, next: Node* }
fn build(i32 n) i32 {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.v = 0
    cur.next = null
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.v = i
        nd.next = null
        cur.next = nd
        cur = nd
    }
    return cur.v
}
fn main() i32 { return 0 }`,
			fn:            "build",
			wantSafe:      []string{"cur", "nd"},
			wantWriteThru: []string{"cur"},
		},
		{
			name: "aliased head returned -> alias class escapes, cur excluded",
			src: `import std.memory
struct Node { v: i32, next: Node* }
fn build(i32 n) Node* {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.v = 0
    cur.next = null
    Node* head = cur
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.next = null
        cur.next = nd
        cur = nd
    }
    return head
}
fn main() i32 { return 0 }`,
			fn:          "build",
			wantNotSafe: []string{"cur", "head"},
		},
		{
			name: "container returned as-is -> escapes",
			src: `import std.memory
struct Node { v: i32, next: Node* }
fn build(i32 n) Node* {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.v = 0
    cur.next = null
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.next = null
        cur.next = nd
        cur = nd
    }
    return cur
}
fn main() i32 { return 0 }`,
			fn:          "build",
			wantNotSafe: []string{"cur"},
		},
		{
			name: "container passed as call arg -> escapes",
			src: `import std.memory
struct Node { v: i32, next: Node* }
fn sink(Node* p) i32 { return 0 }
fn build(i32 n) i32 {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.next = null
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.next = null
        cur.next = nd
        cur = nd
    }
    return sink(cur)
}
fn main() i32 { return 0 }`,
			fn:          "build",
			wantNotSafe: []string{"cur"},
		},
		{
			name: "container assigned to outer var which is returned -> escapes",
			src: `import std.memory
struct Node { v: i32, next: Node* }
fn build(i32 n) Node* {
    Node* g = null
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.next = null
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.next = null
        cur.next = nd
        cur = nd
    }
    g = cur
    return g
}
fn main() i32 { return 0 }`,
			fn:          "build",
			wantNotSafe: []string{"cur", "g"},
		},
		{
			name: "container captured by lambda -> escapes",
			src: `import std.memory
struct Node { v: i32, next: Node* }
fn build(i32 n) i32 {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.next = null
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.next = null
        cur.next = nd
        cur = nd
    }
    fn() i32 { return cur.v }
    return 0
}
fn main() i32 { return 0 }`,
			fn:          "build",
			wantNotSafe: []string{"cur"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, params := parseFuncBody(t, tc.src, tc.fn)
			scan := ScanSafeLocalContainers(body, params)
			for _, n := range tc.wantSafe {
				if !scan.Safe[n] {
					t.Errorf("期望 %q 是安全局部容器，实际 Safe=%v", n, scan.Safe)
				}
			}
			for _, n := range tc.wantNotSafe {
				if scan.Safe[n] {
					t.Errorf("期望 %q NOT 是安全局部容器（已逃逸），实际 Safe=%v", n, scan.Safe)
				}
			}
			for _, n := range tc.wantWriteThru {
				if !scan.WrittenThrough[n] {
					t.Errorf("期望 %q 被识别为写穿容器，实际 WrittenThrough=%v", n, scan.WrittenThrough)
				}
			}
		})
	}
}

// TestEscapingContainerStillPromotes 逃逸容器必须仍走旧路径：写穿点仍 promote。
func TestEscapingContainerStillPromotes(t *testing.T) {
	src := `import std.memory
struct Node { v: i32, next: Node* }
fn build(i32 n) Node* {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.v = 0
    cur.next = null
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.v = i
        nd.next = null
        cur.next = nd
        cur = nd
    }
    return cur
}
fn main() i32 { return 0 }`
	cCode := generateForMarkTest(t, src)
	body := survivorFuncBody(t, cCode, "build")

	// 容器逃逸 → 不在安全集合 → 写穿/游标推进必须仍 promote 到幸存段
	if !strings.Contains(body, "kmm_v4_scope_promote_p") {
		t.Errorf("容器逃逸时必须仍 promote（否则对象留在 bump 区、函数出口悬垂），实际：\n%s", body)
	}
}

// TestWriteThroughContainerPromotesNothing 安全局部容器的写穿场景下，
// 循环体内不应再出现 promote（方案 A' 的直接效果）。
func TestWriteThroughContainerPromotesNothing(t *testing.T) {
	src := `import std.memory
struct Node { v: i32, next: Node* }
fn build(i32 n) i32 {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.v = 0
    cur.next = null
    i32 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.v = i
        nd.next = null
        cur.next = nd
        cur = nd
    }
    return cur.v
}
fn main() i32 { return 0 }`
	cCode := generateForMarkTest(t, src)
	body := survivorFuncBody(t, cCode, "build")

	if strings.Contains(body, "kmm_v4_scope_promote_p") {
		t.Errorf("安全局部容器的写穿 + 游标推进不应 promote（方案 A'），实际：\n%s", body)
	}
	if strings.Contains(body, "kmm_v4_mark_restore") {
		t.Errorf("安全局部容器的写穿场景应放弃循环级回卷（否则链被复用分配覆盖），实际：\n%s", body)
	}
}

// TestPureCursorAssignStillPromotes 纯游标推进（keep = p，无写穿）必须保持
// 既有行为（c983678 已修好的路径）：仍 promote，否则会回归。
func TestPureCursorAssignStillPromotes(t *testing.T) {
	src := `import std.memory
fn build(i32 n) i32 {
    i32 i = 0
    i32* keep = null
    while i < n {
        i = i + 1
        i32* p = as<i32*>(std.memory.kmm_v4_alloc(16))
        p[0] = i
        keep = p
    }
    if keep == null { return 0 }
    return keep[0]
}
fn main() i32 { return 0 }`
	cCode := generateForMarkTest(t, src)
	body := survivorFuncBody(t, cCode, "build")

	if !strings.Contains(body, "kmm_v4_scope_promote_p") {
		t.Errorf("纯游标推进（无写穿）必须保持既有 promote 行为，实际：\n%s", body)
	}
	if !strings.Contains(body, "kmm_v4_survivor_free(keep); /* release previous survivor copy */") {
		t.Errorf("纯游标推进仍需释放上一轮旧副本，实际：\n%s", body)
	}
}

// TestChainEscapingContainerKeepsHeadAlive 链首随返回值离开函数时，
// 容器（cur）不得被 early free —— 释放它就等于释放链首节点。
//
// 场景（P0 修 F 的第二个manifestation）：
//
//	i64 head = as<i64>(cur)
//	...
//	return head
//
// head 是地址值、与 cur 之间没有指针别名对（as<i64> 改变类型），
// 但 cur 指向的正是链首节点。此前 liveness 会在 `cur = nd` 前的
// 最后使用点发射 `kmm_v4_survivor_free(cur)`，把链首槽位释放；
// 下一轮分配复用该槽位 → 链首被覆写（n 小、slab 未耗尽时不暴露）。
func TestChainEscapingContainerKeepsHeadAlive(t *testing.T) {
	src := `import std.memory
struct Node { v: i64, next: i64 }
fn build(i64 n) i64 {
    Node* cur = as<Node*>(std.memory.kmm_v4_alloc(16))
    cur.v = -1
    cur.next = -1
    i64 head = as<i64>(cur)
    i64 i = 0
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.v = i
        nd.next = -1
        cur.next = as<i64>(nd)
        cur = nd
    }
    return head
}
fn main() i32 { return 0 }`

	// 扫描层：cur 必须被识别为 ChainEscaping，且退出 Safe 集合。
	body, params := parseFuncBody(t, src, "build")
	scan := ScanSafeLocalContainers(body, params)
	if !scan.ChainEscaping["cur"] {
		t.Errorf("期望 cur 被识别为 ChainEscaping（链首随 return 出去），实际=%v", scan.ChainEscaping)
	}
	if scan.Safe["cur"] {
		t.Errorf("链首逃逸后 cur 不应再是安全局部容器，实际 Safe=%v", scan.Safe)
	}
	if !scan.Escaped["cur"] {
		t.Errorf("链首逃逸后 cur 应并入逃逸集合，实际 Escaped=%v", scan.Escaped)
	}

	// 代码生成层：不得对 cur 发射 early free。
	cCode := generateForMarkTest(t, src)
	fnBody := survivorFuncBody(t, cCode, "build")
	if strings.Contains(fnBody, "kmm_v4_survivor_free(cur);") {
		t.Errorf("链首随 return 出去时不得 early free 容器 cur（会释放链首节点），实际：\n%s", fnBody)
	}
}
