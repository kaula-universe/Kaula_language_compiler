package codegen

import (
	"strings"
	"testing"
)

// 本文件锁定「内部 promote vs 向外 promote」的分类语义，防止 survivor 段
// 单调增长（循环里每轮 promote 一份、函数出口又不回卷）的泄漏回归。
//
// 背景：动态池下 kmm_v4_survivor_rewind 只 flush「已 free 的延迟槽位」构成的
// TLS 垃圾箱，**不会**回收从未 free 的存活槽位。因此只要还有代码路径让副本
// 既不被 free、又活过函数出口，survivor 段就会随迭代/调用次数线性增长。

// survivorFuncBody 从生成的 C 中截取指定函数的函数体，便于对单个函数做断言。
func survivorFuncBody(t *testing.T, cCode, fnName string) string {
	t.Helper()
	idx := strings.Index(cCode, fnName+"(")
	if idx < 0 {
		t.Fatalf("生成的 C 中找不到函数 %s：\n%s", fnName, cCode)
	}
	// 从函数签名后的第一个 '{' 开始做括号配平，取到匹配的 '}'
	start := strings.Index(cCode[idx:], "{")
	if start < 0 {
		t.Fatalf("函数 %s 没有函数体：\n%s", fnName, cCode)
	}
	start += idx
	depth := 0
	for i := start; i < len(cCode); i++ {
		switch cCode[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return cCode[start : i+1]
			}
		}
	}
	t.Fatalf("函数 %s 函数体括号不配平：\n%s", fnName, cCode)
	return ""
}

// TestLoopCursorAssignReleasesPreviousSurvivorCopy 循环内把新分配的对象赋给
// 循环外声明的局部游标（keep = p）：每轮都会 promote 一份新副本，旧副本必须
// 在挂新副本前释放，否则副本数与迭代次数等比例增长。
func TestLoopCursorAssignReleasesPreviousSurvivorCopy(t *testing.T) {
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

fn main() i32 {
    return 0
}
`
	cCode := generateForMarkTest(t, src)
	body := survivorFuncBody(t, cCode, "build")

	if !strings.Contains(body, "kmm_v4_survivor_free(keep); /* release previous survivor copy */") {
		t.Errorf("期望在挂新副本前释放 keep 的旧副本（否则 survivor 段单调增长），实际：\n%s", body)
	}
	// 内部 promote 不抑制函数出口的回卷
	if !strings.Contains(body, "kmm_v4_survivor_rewind(__surv_cp)") {
		t.Errorf("目标是函数局部变量时属于内部 promote，函数出口必须仍能 survivor_rewind，实际：\n%s", body)
	}
}

// TestWriteThroughMustNotFreeContainer 写穿（`cur->next = nd`，dstName 为
// struct_field）时不能释放容器变量：被改写的是容器的某个字段而非容器本身，
// 紧接着的 `cur->next = nd` 还要往容器里写，释放容器即 use-after-free。
func TestWriteThroughMustNotFreeContainer(t *testing.T) {
	src := `import std.memory

struct Node {
    v: i32,
    next: Node*
}

fn buildList(i32 n) i32 {
    i32 i = 0
    Node* head = as<Node*>(std.memory.kmm_v4_alloc(16))
    head.v = 0
    head.next = null
    Node* cur = head
    while i < n {
        i = i + 1
        Node* nd = as<Node*>(std.memory.kmm_v4_alloc(16))
        nd.v = i
        nd.next = null
        cur.next = nd
        cur = nd
    }
    return head.v
}

fn main() i32 {
    return 0
}
`
	cCode := generateForMarkTest(t, src)
	body := survivorFuncBody(t, cCode, "buildList")

	if strings.Contains(body, "kmm_v4_survivor_free(cur); /* release previous survivor copy */") {
		t.Errorf("写穿目标不能释放容器变量 cur（会把还在用的对象释放掉），实际：\n%s", body)
	}
}

// TestReturnPromotedStillSuppressesRewind 返回值本身是幸存副本的持有者时，
// rewind 会回卷掉该副本、调用方拿到悬垂指针，故必须与向外 promote 同等对待。
func TestReturnPromotedStillSuppressesRewind(t *testing.T) {
	src := `import std.memory

fn make(i32 n) i32* {
    i32* out = null
    i32 i = 0
    while i < n {
        i = i + 1
        i32* p = as<i32*>(std.memory.kmm_v4_alloc(16))
        p[0] = i
        out = p
    }
    return out
}

fn main() i32 {
    return 0
}
`
	cCode := generateForMarkTest(t, src)
	body := survivorFuncBody(t, cCode, "make")

	// 返回的 out 持有幸存副本：任一 return 路径都不能回卷 survivor 段
	if strings.Contains(body, "kmm_v4_survivor_rewind(__surv_cp)") {
		t.Errorf("返回幸存副本的持有者时不能 survivor_rewind（会返回悬垂指针），实际：\n%s", body)
	}
}

// TestSameScopeAssignHasNoExtraFree 同层赋值（非跨作用域）不产生副本累积，
// 不应多发射一次 survivor_free——避免无谓调用与误伤。
func TestSameScopeAssignHasNoExtraFree(t *testing.T) {
	src := `import std.memory

fn flat(i32 n) i32 {
    i32 sum = 0
    i32 i = 0
    while i < n {
        i = i + 1
        i32* p = as<i32*>(std.memory.kmm_v4_alloc(16))
        p[0] = i
        sum = sum + p[0]
    }
    return sum
}

fn main() i32 {
    return 0
}
`
	cCode := generateForMarkTest(t, src)
	body := survivorFuncBody(t, cCode, "flat")

	if strings.Contains(body, "release previous survivor copy") {
		t.Errorf("同层赋值不应发射额外的副本释放，实际：\n%s", body)
	}
}
