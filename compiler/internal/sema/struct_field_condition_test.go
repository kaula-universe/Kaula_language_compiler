package sema

import (
	"strings"
	"testing"
)

// 回归：struct 字段类型此前在 sema 侧完全查不到（memberObjectClassName
// 只认 class），`if (r.field)`（bool 字段）因条件表达式类型为空被误报
// condition_type。修复：memberObjectClassName / classFieldType 补 struct
// 查询（structDecls），支持嵌套 struct 链。
const structFieldCondSrc = `import std.io

struct Rec {
    uid: i64,
    active: bool
}

fn make(i64 uid) Rec {
    Rec r = Rec {
        uid: uid,
        active: uid % 3 == 0
    }
    return r
}

fn main() {
    Rec r = make(4)
    if (r.active) {
        std.io.println("on")
    } else {
        std.io.println("off")
    }
}
`

func TestStructFieldBoolConditionAllowed(t *testing.T) {
	prog := parseForSema(t, structFieldCondSrc)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	summary := sa.errorCollector.GetErrorSummary()
	if strings.Contains(summary, "condition_type") {
		t.Fatalf("struct bool 字段作为 if 条件被误判 condition_type:\n%s", summary)
	}
	if sa.errorCollector.HasErrors() {
		t.Fatalf("struct 字段条件不应产生语义错误:\n%s", summary)
	}
}

// 嵌套 struct 链：o.inner.flag / o.ok 的类型解析同样要打通。
func TestNestedStructFieldConditionAllowed(t *testing.T) {
	src := `import std.io

struct Inner {
    flag: bool
}
struct Outer {
    inner: Inner,
    ok: bool
}

fn make(i64 uid) Outer {
    Outer o = Outer {
        inner: Inner { flag: uid % 2 == 0 },
        ok: uid > 1
    }
    return o
}

fn main() {
    Outer o = make(4)
    if (o.ok) {
        if (o.inner.flag) {
            std.io.println("both")
        }
    }
}
`
	prog := parseForSema(t, src)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	summary := sa.errorCollector.GetErrorSummary()
	if strings.Contains(summary, "condition_type") || strings.Contains(summary, "no_such_field") {
		t.Fatalf("嵌套 struct 字段条件解析失败:\n%s", summary)
	}
	if sa.errorCollector.HasErrors() {
		t.Fatalf("嵌套 struct 字段条件不应产生语义错误:\n%s", summary)
	}
}

// 保护：真正的非 bool 条件（i64 字段直接作条件）仍然报 condition_type。
func TestNonBoolConditionStillRejected(t *testing.T) {
	src := `struct Rec {
    uid: i64
}

fn make(i64 uid) Rec {
    Rec r = Rec { uid: uid }
    return r
}

fn main() {
    Rec r = make(4)
    if (r.uid) {
        std.io.println("x")
    }
}
`
	prog := parseForSema(t, src)
	sa := NewSemanticAnalyzer()
	sa.Analyze(prog)

	if !sa.errorCollector.HasErrors() {
		t.Fatal("i64 字段直接作 if 条件仍应报 condition_type")
	}
	summary := sa.errorCollector.GetErrorSummary()
	if !strings.Contains(summary, "条件必须是布尔值") {
		t.Fatalf("期望 condition_type 错误，实际:\n%s", summary)
	}
}
