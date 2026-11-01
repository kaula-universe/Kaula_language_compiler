package codegen

import (
	"crypto/sha256"
	"fmt"
	"kaula/internal/ast"
	"strings"
)

// Closures capture values. Environments are translation-unit owned and live until
// process exit: copying a closure never leaves a pointer into a returned C frame.
// This conservative lifetime also permits arbitrary aggregate/array storage.
const closureRuntime = `
#ifndef KAULA_CLOSURE_RUNTIME
#define KAULA_CLOSURE_RUNTIME
#include <stdlib.h>
#include <string.h>
typedef struct _kaula_closure_allocation { struct _kaula_closure_allocation *next; } _kaula_closure_allocation;
static _kaula_closure_allocation *_kaula_closure_allocations;
static void _kaula_closure_cleanup(void) {
    while (_kaula_closure_allocations) {
        _kaula_closure_allocation *p = _kaula_closure_allocations;
        _kaula_closure_allocations = p->next;
        free(p);
    }
}
static void *_kaula_closure_alloc(size_t n) {
    if (!_kaula_closure_allocations && atexit(_kaula_closure_cleanup)) abort();
    _kaula_closure_allocation *p = malloc(sizeof(*p) + n);
    if (!p) abort();
    p->next = _kaula_closure_allocations;
    _kaula_closure_allocations = p;
    return p + 1;
}
#endif
`

func closureParts(t string) ([]string, bool) {
	if !strings.HasPrefix(t, "closure<") || !strings.HasSuffix(t, ">") {
		return nil, false
	}
	return splitTopLevelCommas(t[8 : len(t)-1]), true
}
func lambdaType(e *ast.LambdaExpression) string {
	p := append([]string(nil), e.ParamTypes...)
	for i := range p {
		if p[i] == "" || p[i] == "auto" {
			p[i] = "i64"
		}
	}
	r := e.ReturnType
	if r == "" {
		r = "void"
	}
	return "closure<" + strings.Join(append(p, r), ",") + ">"
}
func (tg *TypeGenerator) closureType(t string) (string, bool) {
	p, ok := closureParts(t)
	if !ok {
		return "", false
	}
	name := fmt.Sprintf("_kaula_closure_%x", sha256.Sum256([]byte(t)))[:31]
	args := []string{"void*"}
	for _, a := range p[:len(p)-1] {
		args = append(args, tg.MapKaulaTypeToC(strings.TrimSpace(a)))
	}
	ret := tg.MapKaulaTypeToC(strings.TrimSpace(p[len(p)-1]))
	if tg.codegen != nil {
		marker := "#ifndef " + name + "_DEFINED"
		if !strings.Contains(tg.codegen.genericTypeCode.String(), marker) {
			tg.codegen.genericTypeCode.WriteString(fmt.Sprintf("%s\n#define %s_DEFINED\ntypedef struct { %s (*call)(%s); void *env; } %s;\n#endif\n", marker, name, ret, strings.Join(args, ", "), name))
		}
	}
	return name, true
}
func (eg *ExpressionGenerator) GenerateLambdaExpression(e *ast.LambdaExpression) string {
	cg := eg.codegen
	if cg.IsFreestanding() {
		cg.error("capturing closures require hosted allocation")
		return "0"
	}
	if !strings.Contains(cg.genericTypeCode.String(), "#define KAULA_CLOSURE_RUNTIME") {
		cg.genericTypeCode.WriteString(closureRuntime)
	}
	ct := cg.typeGenerator.MapKaulaTypeToC(lambdaType(e))
	name := fmt.Sprintf("_kaula_lambda_%d", cg.lambdaCounter)
	cg.lambdaCounter++
	fields, values := []string{}, []string{}
	for _, n := range e.Captures {
		s := cg.GetSymbol(n)
		if s == nil {
			cg.error("unknown closure capture: " + n)
			continue
		}
		t := cg.typeGenerator.convertType(s.Type, false)
		if t != "String" && !strings.HasPrefix(t, "_kaula_closure_") && !isClosureScalar(s.Type) {
			cg.error("closure capture requires a scalar, string, or closure value: " + n)
			continue
		}
		fields = append(fields, formatCVarDecl(t, n)+";")
		value := eg.GenerateExpression(&ast.Identifier{Name: n})
		values = append(values, fmt.Sprintf("%s_created->%s = %s;", name, n, value))
		if t == "String" {
			values = append(values, fmt.Sprintf("{ char *copy = _kaula_closure_alloc(%[1]s_created->%[2]s.len + 1); memcpy(copy, %[1]s_created->%[2]s.ptr, %[1]s_created->%[2]s.len); copy[%[1]s_created->%[2]s.len] = 0; %[1]s_created->%[2]s.ptr = copy; }", name, n))
		}
	}
	if len(fields) == 0 {
		fields = append(fields, "char unused;")
	}
	// Environment declarations precede nested lambda definitions.
	cg.lambdaDefinitions = append(cg.lambdaDefinitions, fmt.Sprintf("typedef struct { %s } %s_env;\n", strings.Join(fields, " "), name))
	// Lambda bodies are emitted as standalone C functions. Do not inherit the
	// enclosing function's runtime KMM scope bookkeeping: a return in this body
	// must only pop scopes opened by the lambda itself.
	savedKmmDepth := cg.kmmScopeDepth
	savedEmitsPromote := cg.funcEmitsPromote
	savedHasCheckpoint := cg.funcHasKMMCheckpoint
	cg.kmmScopeDepth = 0
	cg.funcEmitsPromote = false
	cg.funcHasKMMCheckpoint = false

	cg.EnterScope("lambda")
	previous := cg.currentCaptureSet
	cg.currentCaptureSet = nil
	oldAliases := eg.closureAliases
	eg.closureAliases = map[string]string{}
	for _, n := range e.Captures {
		eg.closureAliases[n] = "env->" + n
	}
	params := []string{"void *_environment"}
	for i, n := range e.Params {
		t := "i64"
		if i < len(e.ParamTypes) && e.ParamTypes[i] != "auto" && e.ParamTypes[i] != "" {
			t = e.ParamTypes[i]
		}
		cg.AddSymbol(n, t, false, "lambda_param", 0, 0)
		params = append(params, formatCVarDecl(cg.typeGenerator.convertType(t, false), n))
		delete(eg.closureAliases, n)
	}
	oldRet := cg.currentFunctionReturnType
	cg.currentFunctionReturnType = e.ReturnType
	var body strings.Builder
	for _, s := range e.Body {
		body.WriteString(cg.statementGenerator.GenerateStatement(s))
	}
	cg.currentFunctionReturnType = oldRet
	cg.currentCaptureSet = previous
	eg.closureAliases = oldAliases
	cg.ExitScope()
	cg.kmmScopeDepth = savedKmmDepth
	cg.funcEmitsPromote = savedEmitsPromote
	cg.funcHasKMMCheckpoint = savedHasCheckpoint
	ret := "void"
	if e.ReturnType != "" {
		ret = cg.typeGenerator.convertType(e.ReturnType, false)
	}
	cg.lambdaDefinitions = append(cg.lambdaDefinitions, fmt.Sprintf("static %s %s(%s) { %s_env *env = _environment;\n%s\n}\n", ret, name, strings.Join(params, ", "), name, body.String()))
	return fmt.Sprintf("({ %[1]s_env *%[1]s_created = _kaula_closure_alloc(sizeof(%[1]s_env)); %[2]s (%[3]s){%[1]s, %[1]s_created}; })", name, strings.Join(values, " "), ct)
}
func isClosureScalar(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "bool", "char":
		return true
	}
	return false
}
func (eg *ExpressionGenerator) closureExpressionType(e ast.Expression) string {
	switch x := e.(type) {
	case *ast.LambdaExpression:
		return lambdaType(x)
	case *ast.Identifier:
		if eg.codegen.currentScope == nil {
			return ""
		}
		if s := eg.codegen.GetSymbol(x.Name); s != nil {
			return s.Type
		}
	case *ast.ParenExpression:
		return eg.closureExpressionType(x.Inner)
	case *ast.CallExpression:
		if p, ok := closureParts(eg.closureExpressionType(x.Function)); ok {
			return strings.TrimSpace(p[len(p)-1])
		}
		if id, ok := x.Function.(*ast.Identifier); ok {
			if eg.codegen.program == nil {
				return ""
			}
			if f := eg.codegen.program.FindFunction(id.Name); f != nil {
				return f.ReturnType
			}
		}
	}
	return ""
}
func (eg *ExpressionGenerator) generateClosureCall(e *ast.CallExpression) (string, bool) {
	if _, ok := closureParts(eg.closureExpressionType(e.Function)); !ok {
		return "", false
	}
	name := fmt.Sprintf("_closure_call_%d", eg.codegen.lambdaCounter)
	eg.codegen.lambdaCounter++
	args := []string{name + ".env"}
	for _, a := range e.Args {
		args = append(args, eg.GenerateExpression(a))
	}
	return fmt.Sprintf("({ __auto_type %s = %s; %s.call(%s); })", name, eg.GenerateExpression(e.Function), name, strings.Join(args, ", ")), true
}
