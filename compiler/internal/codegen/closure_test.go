package codegen

import (
	"kaula/internal/ast"
	"kaula/internal/config"
	"kaula/internal/sema"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runClosureC(t *testing.T, cg *CodeGenerator, body string) {
	t.Helper()
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang unavailable")
	}
	src := "#include <stdint.h>\n#include <stdbool.h>\n#include <stddef.h>\nstatic int64_t kaula_add_i64(int64_t a, int64_t b) { return a + b; }\n" + cg.genericTypeCode.String() + strings.Join(cg.lambdaDefinitions, "\n") + body
	dir := t.TempDir()
	c := filepath.Join(dir, "closure.c")
	exe := filepath.Join(dir, "closure.exe")
	if err := os.WriteFile(c, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=gnu11", c, "-o", exe).CombinedOutput(); err != nil {
		t.Fatalf("C compile: %v\n%s\n%s", err, out, src)
	}
	if out, err := exec.Command(exe).CombinedOutput(); err != nil {
		t.Fatalf("execution: %v %s\n%s", err, out, src)
	}
}
func TestClosureReturnedStoredAndNested(t *testing.T) {
	prog := parse(`fn main() { i64 seed = 40
 auto outer = fn(i64 x) -> i64 { auto inner = fn(i64 y) -> i64 { return seed + x + y }; return inner(1) }
 }`)
	sa := sema.NewSemanticAnalyzer()
	sa.Analyze(prog)
	if sa.ErrorCollector().HasErrors() {
		t.Fatalf("sema: %v", sa.ErrorCollector().Errors())
	}
	cg := NewCodeGenerator(config.DefaultConfig())
	cg.Generate(prog)
	if cg.HasErrors() {
		t.Fatal(cg.Errors())
	}
	// Exercise the exact lowering after the creating frame has returned, with two
	// independently captured values and copies stored in an array.
	cg2 := NewCodeGenerator(config.DefaultConfig())
	cg2.program = prog
	cg2.EnterScope("test")
	cg2.AddSymbol("seed", "i64", false, "local", 0, 0)
	fn := prog.FindFunction("main")
	outer := fn.Body[1].(*ast.VariableDeclaration).Value.(*ast.LambdaExpression)
	value := cg2.expressionGenerator.GenerateLambdaExpression(outer)
	typ := cg2.typeGenerator.MapKaulaTypeToC(lambdaType(outer))
	runClosureC(t, cg2, "static "+typ+" make(int64_t seed) { return "+value+"; }\nint main(void) { "+typ+" saved[2] = {make(40),make(80)}; if(saved[0].call(saved[0].env,1)!=42) return 1; if(saved[1].call(saved[1].env,1)!=82) return 2; return 0; }")
}
func TestMatchPayloadAssignmentAndScope(t *testing.T) {
	p := parse(`enum Result { Ok(i64), Err(i64) }
 fn main() { Result r
 i64 v = match(r) { Ok(x) => x, Err(x) => x }
 }`)
	sa := sema.NewSemanticAnalyzer()
	sa.Analyze(p)
	if sa.ErrorCollector().HasErrors() {
		t.Fatalf("sema: %v", sa.ErrorCollector().Errors())
	}
	cg := NewCodeGenerator(config.DefaultConfig())
	cg.Generate(p)
	if cg.HasErrors() {
		t.Fatal(cg.Errors())
	}
	cg.EnterScope("test")
	cg.AddSymbol("r", "Result", false, "local", 0, 0)
	m := p.FindFunction("main").Body[1].(*ast.VariableDeclaration).Value.(*ast.MatchExpression)
	code := cg.expressionGenerator.GenerateMatchAssign(m, "v")
	if cg.GetSymbol("x") != nil {
		t.Fatal("match payload escaped scope")
	}
	cg.genericTypeCode.Reset()
	cg.lambdaDefinitions = nil
	runClosureC(t, cg, "typedef struct { int kind; union { int64_t Ok_val; int64_t Err_val; } data; } K_Result;\nenum {Result_Kind_Ok,Result_Kind_Err};\nint main(void){ K_Result r={Result_Kind_Ok,{.Ok_val=42}}; int64_t v=0; "+code+" return v!=42; }")
}
func TestMatchStringsNULAndSingleEvaluation(t *testing.T) {
	cg := NewCodeGenerator(config.DefaultConfig())
	cg.program = &ast.Program{}
	cg.EnterScope("test")
	cg.AddSymbol("s", "string", false, "local", 0, 0)
	m := &ast.MatchExpression{Target: &ast.Identifier{Name: "s"}, Arms: []*ast.MatchArm{
		{Pattern: &ast.MatchPattern{Kind: ast.PatternString, StrValue: "a\\0b"}, Body: []ast.Statement{&ast.ExpressionStatement{Expression: &ast.IntegerLiteral{Value: 42}}}},
		{Pattern: &ast.MatchPattern{Kind: ast.PatternWildcard}, Body: []ast.Statement{&ast.ExpressionStatement{Expression: &ast.IntegerLiteral{Value: 1}}}},
	}}
	code := cg.expressionGenerator.GenerateMatchAssign(m, "v")
	if strings.Contains(code, "strcmp") {
		t.Fatal("String must compare length and bytes")
	}
	runClosureC(t, cg, "#include <string.h>\n#define STRING_SSO_CAPACITY 15\ntypedef struct { size_t len; union { char* ptr; char sso[16]; }; } String;\n#define string_cdata(s) ((s).len <= STRING_SSO_CAPACITY ? (const char*)(s).sso : (const char*)(s).ptr)\nint main(void){ String s={0}; memcpy(s.sso, \"a\\0b\", 3); s.len=3; int v=0;"+code+"return v!=42;}")
	if got := escapeCString("a\x007"); got != "a\\0007" {
		t.Fatalf("NUL octal bleed: %q", got)
	}
}
