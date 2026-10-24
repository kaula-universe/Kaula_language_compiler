package codegen

import (
	"testing"
)

const chainScanTreeSrc = `
struct Node {
    left: Node*
    right: Node*
}

fn bottomUpTree(i64 depth) Node* {
    if depth <= 0 {
        Node* leaf = as<Node*>(kmm_v4_alloc(16))
        leaf.left = null
        leaf.right = null
        return leaf
    }
    Node* l = bottomUpTree(depth - 1)
    Node* r = bottomUpTree(depth - 1)
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = l
    n.right = r
    return n
}

fn itemCheck(Node* n) i64 {
    if n.left == null {
        return 1
    }
    return 1 + itemCheck(n.left) + itemCheck(n.right)
}

fn main() {
    Node* stretch = bottomUpTree(13)
    i64 s = itemCheck(stretch)
    Node* longLived = bottomUpTree(12)
    i64 depth = 4
    while depth <= 12 {
        i64 checksum = 0
        i64 i = 0
        while i < 4 {
            Node* a = bottomUpTree(depth)
            checksum = checksum + itemCheck(a)
            i = i + 1
        }
        depth = depth + 2
    }
    i64 l = itemCheck(longLived)
    println(s, l)
}
`

// TestReturnEscapeScanDetectsTreeBuilder 验证 binary_trees 形态：
// bottomUpTree 被识别为链构造器，全部分配/链调用值都归返回链所有；
// main 中的 stretch/longLived/a 是通过校验的接收者，itemCheck 是借用函数。
func TestReturnEscapeScanDetectsTreeBuilder(t *testing.T) {
	prog := parse(chainScanTreeSrc)
	scan := ScanReturnEscapeChains(prog)

	info := scan.ChainFuncs["bottomUpTree"]
	if info == nil {
		t.Fatalf("bottomUpTree 未被识别为链构造器: %+v", scan.ChainFuncs)
	}
	for _, name := range []string{"leaf", "l", "r", "n"} {
		if !info.Values[name] {
			t.Errorf("Values 缺少 %q: %v", name, info.Values)
		}
		if !info.Owned[name] {
			t.Errorf("Owned 缺少 %q: %v", name, info.Owned)
		}
	}
	if !info.ReturnVars["leaf"] || !info.ReturnVars["n"] {
		t.Errorf("ReturnVars 不正确: %v", info.ReturnVars)
	}
	if scan.ChainFuncs["itemCheck"] != nil {
		t.Error("itemCheck 返回标量，不应成为链构造器")
	}

	receivers := scan.Receivers["main"]
	for _, name := range []string{"stretch", "longLived", "a"} {
		if !receivers[name] {
			t.Errorf("main 的接收者缺少 %q: %v", name, receivers)
		}
	}
}

// TestReturnEscapeScanRejectsLoops 链构造器不允许循环（游标推进/重新绑定
// 会破坏「全有或全无」的归属判定）。
func TestReturnEscapeScanRejectsLoops(t *testing.T) {
	src := `
struct Node { next: Node* }
fn build(i64 n) Node* {
    Node* head = as<Node*>(kmm_v4_alloc(16))
    Node* cur = head
    i64 i = 0
    while i < n {
        Node* nd = as<Node*>(kmm_v4_alloc(16))
        cur.next = nd
        cur = nd
        i = i + 1
    }
    return head
}
fn main() { Node* p = build(4) }
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["build"] != nil {
		t.Error("含循环与游标推进的函数不应被识别为链构造器")
	}
}

// TestReturnEscapeScanRejectsUnownedAllocation 局部临时分配没有挂到返回链上
// → 整个函数退出集合（全有或全无）。
func TestReturnEscapeScanRejectsUnownedAllocation(t *testing.T) {
	src := `
struct Node { left: Node* }
fn build() Node* {
    Node* scratch = as<Node*>(kmm_v4_alloc(16))
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = null
    return n
}
fn main() { Node* p = build() }
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["build"] != nil {
		t.Error("存在不属于返回链的分配时不应识别为链构造器")
	}
}

// TestReturnEscapeScanRejectsForeignFieldStore 把形参/外部指针挂进链字段
// 无法证明其生命周期覆盖调用方 bump 区，必须拒绝。
func TestReturnEscapeScanRejectsForeignFieldStore(t *testing.T) {
	src := `
struct Node { left: Node* }
fn build(Node* p) Node* {
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = p
    return n
}
fn main() { Node* p = build(null) }
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["build"] != nil {
		t.Error("把外部指针写进链字段时不应识别为链构造器")
	}
}

// TestReturnEscapeScanRejectsEscapedReceiver 调用方把链接收者强转成整数
// → 链指针逃出当前 bump 区 → 撤销被调链构造器（全局去固定点）。
func TestReturnEscapeScanRejectsEscapedReceiver(t *testing.T) {
	src := `
struct Node { left: Node* right: Node* }
fn bottomUpTree(i64 depth) Node* {
    Node* leaf = as<Node*>(kmm_v4_alloc(16))
    leaf.left = null
    leaf.right = null
    return leaf
}
fn main() {
    Node* n = bottomUpTree(3)
    i64 h = as<i64>(n)
    println(h)
}
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["bottomUpTree"] != nil {
		t.Error("接收者被强转成整数后，链构造器必须被撤销")
	}
}

// TestReturnEscapeScanRejectsNonBorrowCall 非借用函数接收链指针 → 可能长期
// 持有 → 链会随调用方 bump 区回卷而悬垂 → 撤销链构造器。
func TestReturnEscapeScanRejectsNonBorrowCall(t *testing.T) {
	src := `
struct Node { left: Node* }
Node* g = null
fn sink(Node* p) {
    g = p
}
fn bottomUpTree(i64 depth) Node* {
    Node* leaf = as<Node*>(kmm_v4_alloc(16))
    leaf.left = null
    return leaf
}
fn main() {
    Node* n = bottomUpTree(3)
    sink(n)
}
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["bottomUpTree"] != nil {
		t.Error("链指针传给会长期持有它的函数后，链构造器必须被撤销")
	}
}

// TestReturnEscapeScanRejectsAlias 接收者被赋给另一个指针变量 → 别名可能
// 逃逸，v1 无法跟踪 → 撤销链构造器。
func TestReturnEscapeScanRejectsAlias(t *testing.T) {
	src := `
struct Node { left: Node* }
fn bottomUpTree(i64 depth) Node* {
    Node* leaf = as<Node*>(kmm_v4_alloc(16))
    leaf.left = null
    return leaf
}
fn main() {
    Node* n = bottomUpTree(3)
    Node* q = n
    println(as<i64>(q))
}
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["bottomUpTree"] != nil {
		t.Error("接收者存在指针别名时，链构造器必须被撤销")
	}
}

// TestReturnEscapeScanBorrowOnlyTraversal 只读遍历函数可作为借用函数接收
// 链指针；其内部递归调用也必须保持在借用集合内。
func TestReturnEscapeScanBorrowOnlyTraversal(t *testing.T) {
	prog := parse(chainScanTreeSrc)
	scan := ScanReturnEscapeChains(prog)
	if scan.ChainFuncs["bottomUpTree"] == nil {
		t.Fatal("bottomUpTree 应被识别")
	}
	if !scan.Receivers["main"]["stretch"] {
		t.Error("stretch 应能通过 itemCheck 借用校验")
	}
}

// TestReturnEscapeScanAllowsSiblingRedeclaration 同名接收者在不同兄弟块中
// 重复声明时，必须按「最内层可见声明」归属使用，不能按名字合并（否则
// stress_ptr_tree 的两个 a 会把链构造器整体误撤销）。
func TestReturnEscapeScanAllowsSiblingRedeclaration(t *testing.T) {
	src := `
struct Node { left: Node* }
fn leaf() Node* {
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = null
    return n
}
fn itemCheck(Node* n) i64 {
    if n.left == null { return 1 }
    return 1
}
fn main() {
    i64 x = 0
    if x == 0 {
        Node* a = leaf()
        println(itemCheck(a))
    }
    if x == 1 {
        Node* a = leaf()
        println(itemCheck(a))
    }
}
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["leaf"] == nil {
		t.Errorf("兄弟块同名接收者不应撤销链构造器: %v", scan.ChainFuncs)
	}
	if !scan.Receivers["main"]["a"] {
		t.Errorf("两个同名 a 都应通过接收者校验: %v", scan.Receivers["main"])
	}
}

// TestReturnEscapeScanRejectsChainAliasToScalar 把链指针赋给非指针变量
// （显式或隐式）等于复制地址，所有权边界不可判定，必须拒绝。
func TestReturnEscapeScanRejectsChainAliasToScalar(t *testing.T) {
	src := `
struct Node { left: Node* }
fn build() Node* {
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = null
    i64 addr = n
    println(addr)
    return n
}
fn main() { Node* p = build() }
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["build"] != nil {
		t.Error("链指针被赋给整数变量后不应识别为链构造器")
	}
}

// TestReturnEscapeScanRejectsChainPointerArithmetic 裸链指针参与算术
// （尤其指针相减）会泄漏地址，v1 保守拒绝。
func TestReturnEscapeScanRejectsChainPointerArithmetic(t *testing.T) {
	src := `
struct Node { left: Node* }
fn build() Node* {
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = null
    i64 d = n - n
    println(d)
    return n
}
fn main() { Node* p = build() }
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["build"] != nil {
		t.Error("裸链指针参与算术时不应识别为链构造器")
	}
}

// TestReturnEscapeScanRejectsBorrowOnlyPointerReturn 省略返回类型的函数
// 不能被当作借用函数：它可能把形参指针直接返回出去。
func TestReturnEscapeScanRejectsBorrowOnlyPointerReturn(t *testing.T) {
	src := `
struct Node { left: Node* }
fn sneaky(Node* n) {
    return n
}
fn bottomUpTree(i64 depth) Node* {
    Node* leaf = as<Node*>(kmm_v4_alloc(16))
    leaf.left = null
    return leaf
}
fn main() {
    Node* p = bottomUpTree(3)
    sneaky(p)
}
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["bottomUpTree"] != nil {
		t.Error("链指针传给可能返回它的非借用函数后，链构造器必须被撤销")
	}
}

// TestReturnEscapeScanChainForwarder 转发链指针的函数（有链调用、无分配、
// 直接 return 调用结果）也属于链构造器；其调用者同样要过接收者校验。
func TestReturnEscapeScanChainForwarder(t *testing.T) {
	src := `
struct Node { left: Node* }
fn leafNode() Node* {
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = null
    return n
}
fn forward() Node* {
    Node* n = leafNode()
    return n
}
fn main() {
    Node* p = forward()
    if p == null {
        println(0)
    }
}
`
	scan := ScanReturnEscapeChains(parse(src))
	if scan.ChainFuncs["leafNode"] == nil {
		t.Error("leafNode 应被识别为链构造器")
	}
	if scan.ChainFuncs["forward"] == nil {
		t.Error("forward 应被识别为链构造器（转发链指针）")
	}
	if !scan.Receivers["main"]["p"] {
		t.Errorf("main 的接收者 p 应通过校验: %v", scan.Receivers["main"])
	}
}

// TestReturnEscapeScanForwarderCascade 接收者逃逸时，转发链必须整体撤销：
// 先摘掉 forward，随后 leafNode 因其结果被 forward 直接 return 也失效。
func TestReturnEscapeScanForwarderCascade(t *testing.T) {
	src := `
struct Node { left: Node* }
fn leafNode() Node* {
    Node* n = as<Node*>(kmm_v4_alloc(16))
    n.left = null
    return n
}
fn forward() Node* {
    Node* n = leafNode()
    return n
}
fn main() {
    Node* p = forward()
    println(as<i64>(p))
}
`
	scan := ScanReturnEscapeChains(parse(src))
	if len(scan.ChainFuncs) != 0 {
		t.Errorf("接收者逃逸后链构造器集合应为空: %v", scan.ChainFuncs)
	}
}
