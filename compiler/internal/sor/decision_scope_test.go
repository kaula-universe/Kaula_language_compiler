package sor

import (
	"testing"

	"kaula/internal/lexer"
	"kaula/internal/parser"
)

// 决策必须携带声明所在函数：变量名只在词法作用域内唯一，跨函数同名是常态。
// CodeGen 据此按 (函数, 变量名) 建索引（根治跨函数同名决策互相污染）。
func TestDecisionsCarryFuncName(t *testing.T) {
	lx := lexer.NewLexer(`
fn worker(tmp: i64) {
    i64* inner = null
    inner[0] = 1
}

fn main() {
    i64* tmp = null
    tmp[0] = 2
    worker(1)
}
`)
	pr := parser.NewParser(lx)
	prog := pr.Parse()
	if pr.HasErrors() {
		t.Fatal("parse failed")
	}

	result := AnalyzeFullFromAST(prog)

	byFuncVar := map[string]string{}
	for _, d := range result.Decisions {
		if d.FuncName == "" {
			// 顶层声明：不属于任何函数，CodeGen 用 globalDecisions 索引
			byFuncVar["<top>|"+d.VarName] = d.ObjID
			continue
		}
		byFuncVar[d.FuncName+"|"+d.VarName] = d.ObjID
	}

	for _, want := range []string{"main|tmp", "worker|inner"} {
		if _, ok := byFuncVar[want]; !ok {
			t.Errorf("decision for %s missing; got keys %v", want, byFuncVar)
		}
	}
	// 同名不同函数：worker 的形参 tmp 不是对象，不应与 main|tmp 混淆
	if byFuncVar["main|tmp"] == byFuncVar["worker|tmp"] {
		t.Error("main|tmp and worker|tmp must not share the same object")
	}

	// 序列化必须带 func_name（CodeGen 依赖该键建索引）
	ser := SerializeFullAnalysisResult(result)
	arr, ok := ser["decisions"].([]interface{})
	if !ok || len(arr) == 0 {
		t.Fatalf("decisions not serialized: %#v", ser["decisions"])
	}
	foundMain := false
	for _, item := range arr {
		dm, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		fn, hasKey := dm["func_name"]
		if !hasKey {
			t.Fatalf("serialized decision missing func_name key: %#v", dm)
		}
		if dm["var_name"] == "tmp" && fn == "main" {
			foundMain = true
		}
	}
	if !foundMain {
		t.Error("serialized decisions should carry func_name=main for var tmp")
	}
}
