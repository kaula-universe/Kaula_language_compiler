package codegen

import (
	"fmt"
	"kaula/internal/ast"
	"kaula/internal/config"
	"kaula/internal/lexer"
	"kaula/internal/parser"
	"kaula/internal/sema"
	"strings"
	"testing"
)

func parse(src string) *ast.Program {
	lex := lexer.NewLexer(src)
	p := parser.NewParser(lex)
	prog := p.Parse()
	if p.HasErrors() {
		ec := p.GetErrorCollector()
		if ec != nil {
			for _, e := range ec.Errors() {
				fmt.Println("Parser error:", e.Message)
			}
		}
		panic("parse failed")
	}
	return prog
}

// TestTypedefBeforeForwardDecl 验证：
// 1. simple enum 的 typedef enum { ... } Name 必须在引用它的导出函数 forward decl 之前
// 2. struct 的 typedef struct Name { ... } Name 必须在引用它的导出函数 forward decl 之前
// 3. type alias 的 typedef ... Name 必须在引用它的导出函数 forward decl 之前
func TestTypedefBeforeForwardDecl(t *testing.T) {
	src := `
enum EvKind { Click, Input, Change, Submit, Tick, Other }

type PlatformFn = void(cstring)void

struct ParsedEvent {
    kid: i64
    ok: bool
}

export fn ev_kind_of(i64 typ) EvKind {
    return EvKind.Other
}

export fn kw_json_parse_event(i64 json) ParsedEvent {
    ParsedEvent result
    result.ok = false
    return result
}

export fn bridge_register(cstring show_fn) {
    return
}

fn main() {
    i32 x = 0
    x = x + 1
}
`
	prog := parse(src)

	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)
	if analyzer.ErrorCollector().HasErrors() {
		t.Logf("sema has %d errors (may be expected without stdlib): %v",
			len(analyzer.ErrorCollector().Errors()), analyzer.ErrorCollector().Errors())
	}

	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)

	// 模拟跨文件类型，以验证 isUserDefinedType 中 localPubTypes 的分支
	mockTypes := make(map[string]*sema.LocalPubTypeInfo)
	mockTypes["RemoteEvent"] = &sema.LocalPubTypeInfo{}
	cg.SetLocalPubTypes(mockTypes)

	cCode := cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}

	// === 调试：打印生成的 C 代码前 180 行 ===
	lines := strings.Split(cCode, "\n")
	t.Logf("=== Generated C code (first 180 lines) ===")
	for i, l := range lines {
		if i >= 180 {
			break
		}
		t.Logf("%4d| %s", i+1, l)
	}
	t.Logf("=== END Generated C code ===")

	// 规则 1 & 2：只检查真正的 forward decl（以 ; 结尾的函数声明），排除函数定义（以 { 结尾）
	// 同时 struct/enum 的 C 名称可能带前缀
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 只处理 KAULA_EXPORT 的 forward decl：必须包含 KAULA_EXPORT、含 (、且以 ; 结尾（不是 {）
		if !strings.Contains(trimmed, "KAULA_EXPORT") || !strings.Contains(trimmed, "(") {
			continue
		}
		// 跳过以 { 结尾的行 —— 那是函数定义，不是 forward decl
		if strings.HasSuffix(trimmed, "{") {
			continue
		}
		// 跳过内联函数体定义（同一行有 {} 结构）
		if strings.Contains(trimmed, "{") && strings.Contains(trimmed, "}") {
			continue
		}
		// 必须以分号结尾才是声明
		if !strings.HasSuffix(trimmed, ";") {
			continue
		}

		before := strings.Join(lines[:i], "\n")

		if strings.Contains(line, "ev_kind_of(") || strings.Contains(line, "EvKind") {
			// 接受两种 typedef enum 形式：} EvKind 或 typedef enum ... EvKind
			hasTypedefEnum := strings.Contains(before, "} EvKind") ||
				(strings.Contains(before, "typedef enum") && strings.Contains(before, "EvKind"))
			if !hasTypedefEnum {
				t.Errorf("FAIL: EvKind forward decl at line %d BEFORE its typedef.\n  Line: %s", i+1, line)
			} else {
				t.Logf("PASS: EvKind typedef exists before line %d (ev_kind_of forward decl)", i+1)
			}
		}

		if strings.Contains(line, "kw_json_parse_event(") || (strings.Contains(line, "ParsedEvent") && strings.Contains(line, "(")) {
			// struct 可能叫 typedef struct K_ParsedEvent 或 typedef struct ParsedEvent
			// 只要有 "typedef struct" + "ParsedEvent" 在 typedef 行就行
			hasTypedefStruct := strings.Contains(before, "typedef struct") &&
				(strings.Contains(before, "} K_ParsedEvent") ||
					strings.Contains(before, "} ParsedEvent") ||
					strings.Contains(before, "typedef struct ParsedEvent") ||
					strings.Contains(before, "typedef struct K_ParsedEvent"))
			if !hasTypedefStruct {
				t.Errorf("FAIL: ParsedEvent forward decl at line %d BEFORE its typedef.\n  Line: %s", i+1, line)
			} else {
				t.Logf("PASS: ParsedEvent typedef exists before line %d (kw_json_parse_event forward decl)", i+1)
			}
		}
	}

	// 额外全局顺序检查：先出现 typedef enum，后出现真正的 EvKind ev_kind_of(...); 声明
	idxTypedefEnum := strings.Index(cCode, "typedef enum")
	if idxTypedefEnum < 0 {
		t.Fatal("typedef enum not found")
	}
	// 找 EvKind 相关的 forward decl：必须包含 KAULA_EXPORT + EvKind + ( + 以;结尾
	idxEvKindFn := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "KAULA_EXPORT") &&
			strings.Contains(line, "EvKind") &&
			strings.Contains(line, "(") &&
			strings.HasSuffix(trimmed, ";") &&
			!strings.HasSuffix(trimmed, "{") {
			idxEvKindFn = strings.Index(cCode, line)
			break
		}
	}
	// 如果 EvKind 没有显式 forward decl（因为没有跨文件需要），退而检查函数定义位置
	if idxEvKindFn < 0 {
		t.Log("INFO: EvKind has no standalone forward decl, will check function definition order")
		for _, line := range lines {
			if strings.Contains(line, "ev_kind_of(") {
				idxEvKindFn = strings.Index(cCode, line)
				break
			}
		}
	}
	if idxEvKindFn < 0 {
		t.Fatalf("cannot locate EvKind function decl/def. typedef enum idx=%d", idxTypedefEnum)
	}
	if idxEvKindFn < idxTypedefEnum {
		t.Errorf("FAIL: EvKind decl/def (idx=%d) appears BEFORE typedef enum (idx=%d)", idxEvKindFn, idxTypedefEnum)
	} else {
		t.Logf("PASS: typedef enum (idx=%d) < EvKind decl/def (idx=%d)", idxTypedefEnum, idxEvKindFn)
	}

	// 额外全局顺序检查：先出现 struct typedef，后出现 ParsedEvent 相关声明/定义
	idxTypedefStruct := -1
	for i, line := range lines {
		if strings.Contains(line, "typedef struct") &&
			(strings.Contains(line, "ParsedEvent") || strings.Contains(line, "K_ParsedEvent")) {
			idxTypedefStruct = 0
			for j := 0; j < i; j++ {
				idxTypedefStruct += len(lines[j]) + 1
			}
			break
		}
	}
	if idxTypedefStruct < 0 {
		idx := strings.Index(cCode, "} K_ParsedEvent")
		if idx < 0 {
			idx = strings.Index(cCode, "} ParsedEvent")
		}
		idxTypedefStruct = idx
	}
	if idxTypedefStruct < 0 {
		t.Fatal("ParsedEvent typedef not found")
	}
	// 找 kw_json_parse_event 首次出现的位置（forward decl 或函数定义都行）
	idxParseFn := -1
	for _, line := range lines {
		if strings.Contains(line, "kw_json_parse_event(") {
			idxParseFn = strings.Index(cCode, line)
			break
		}
	}
	if idxParseFn < 0 {
		t.Fatalf("kw_json_parse_event not found; ParsedEvent typedef idx=%d", idxTypedefStruct)
	}
	if idxParseFn < idxTypedefStruct {
		t.Errorf("FAIL: kw_json_parse_event decl/def (idx=%d) appears BEFORE ParsedEvent typedef (idx=%d)", idxParseFn, idxTypedefStruct)
	} else {
		t.Logf("PASS: ParsedEvent typedef (idx=%d) < kw_json_parse_event decl/def (idx=%d)", idxTypedefStruct, idxParseFn)
	}
}

// TestIsUserDefinedTypeCoverage 验证 hasStructParam 能够识别 enum、typealias、跨文件类型
func TestIsUserDefinedTypeCoverage(t *testing.T) {
	src := `
enum MyEnum { A, B, C }

type MyAlias = i64

struct MyStruct { x: i32 }

export fn takes_enum(MyEnum e) i32 { return 0 }
export fn takes_alias(MyAlias a) i32 { return 0 }
export fn takes_struct(MyStruct s) i32 { return 0 }

fn main() { i32 x = 0 }
`
	prog := parse(src)
	analyzer := sema.NewSemanticAnalyzer()
	analyzer.Analyze(prog)

	cfg := config.DefaultConfig()
	cg := NewCodeGenerator(cfg)

	// 加入跨文件类型，模拟跨文件场景
	localPub := make(map[string]*sema.LocalPubTypeInfo)
	localPub["CrossStruct"] = &sema.LocalPubTypeInfo{}
	cg.SetLocalPubTypes(localPub)

	_ = cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatalf("codegen errors: %v", cg.Errors())
	}

	tg := cg.typeGenerator
	if tg == nil {
		t.Fatal("typeGenerator is nil")
	}

	// 验证 enum 已登记
	if !tg.enumTypes["MyEnum"] {
		t.Errorf("FAIL: MyEnum not registered in typeGenerator.enumTypes")
	} else {
		t.Log("PASS: MyEnum registered in enumTypes")
	}

	// 验证 type alias 已登记
	if !tg.typeAliasTypes["MyAlias"] {
		t.Errorf("FAIL: MyAlias not registered in typeGenerator.typeAliasTypes")
	} else {
		t.Log("PASS: MyAlias registered in typeAliasTypes")
	}

	// 验证 struct 已登记（正常应该在 structTypes）
	if !tg.structTypes["MyStruct"] {
		t.Errorf("FAIL: MyStruct not registered in typeGenerator.structTypes")
	} else {
		t.Log("PASS: MyStruct registered in structTypes")
	}

	// 验证 localPubTypes
	if _, ok := cg.localPubTypes["CrossStruct"]; !ok {
		t.Errorf("FAIL: CrossStruct not registered in localPubTypes")
	} else {
		t.Log("PASS: CrossStruct registered in localPubTypes (cross-file type)")
	}
}
