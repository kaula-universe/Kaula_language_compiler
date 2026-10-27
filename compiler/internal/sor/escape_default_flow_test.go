package sor

import (
	"testing"

	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// TestAnalyzeEscapeFromAST 轻量逃逸分析（默认编译流程入口）：
// 覆盖"只在本作用域内使用"(None) 与"作为实参传递"(Arg) 两类
func TestAnalyzeEscapeFromAST(t *testing.T) {
	lx := lexer.NewLexer(`
fn consume(i64* q) {
}

fn main() {
    i64* local
    i64* passed
    consume(passed)
    local[0] = passed[0]
}
`)
	pr := parser.NewParser(lx)
	prog := pr.Parse()
	if pr.HasErrors() {
		t.Fatalf("parse failed")
	}

	res := AnalyzeEscapeFromAST(prog)
	if res == nil {
		t.Fatal("AnalyzeEscapeFromAST returned nil")
	}
	if got := res.Escape["local"]; got != EscNone {
		t.Errorf("local escape = %v, want EscNone (仅本作用域内使用)", got)
	}
	if got := res.Escape["passed"]; got != EscArg {
		t.Errorf("passed escape = %v, want EscArg (作为实参传递)", got)
	}

	// 序列化形状必须与 SerializeFullAnalysisResult 的键位一致（适配器据此消费）
	ser := SerializeEscapeResult(res)
	escapeMap, ok := ser["escape"].(map[string]interface{})
	if !ok {
		t.Fatalf("SerializeEscapeResult: missing escape map, got %#v", ser)
	}
	if got := escapeMap["local"]; got != "None" {
		t.Errorf("serialized local = %v, want \"None\"", got)
	}
	if got := escapeMap["passed"]; got != "Arg" {
		t.Errorf("serialized passed = %v, want \"Arg\"", got)
	}
	if got := SerializeEscapeResult(nil); got != nil {
		t.Errorf("SerializeEscapeResult(nil) = %#v, want nil", got)
	}
	if got := AnalyzeEscapeFromAST(nil); got != nil {
		t.Errorf("AnalyzeEscapeFromAST(nil) = %#v, want nil", got)
	}
}

// TestSerializeEscapeResult_ExternOrigins extern 来源表也随逃逸分析一起下传，
// 使默认流程同样能识别"非 KMM 分配"的对象，避免对其发射 survivor_free
func TestSerializeEscapeResult_ExternOrigins(t *testing.T) {
	ser := SerializeEscapeResult(&EscapeResult{
		Escape:        map[string]EscapeLevel{"h": EscReturn},
		ExternOrigins: map[string]string{"h": "ext_open"},
	})
	origins, ok := ser["extern_origins"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing extern_origins, got %#v", ser)
	}
	if got := origins["h"]; got != "ext_open" {
		t.Errorf("extern origin = %v, want ext_open", got)
	}
}
