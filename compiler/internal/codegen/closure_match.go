package codegen

import (
	"fmt"
	"kaula/internal/ast"
	"strings"
)

// One lowering for statement and assignment matches. Evaluate the scrutinee
// exactly once and give every arm its own compiler and C lexical scope.
func (eg *ExpressionGenerator) lowerMatch(e *ast.MatchExpression, result string) string {
	name := fmt.Sprintf("_match_%d", eg.codegen.lambdaCounter)
	eg.codegen.lambdaCounter++
	var b strings.Builder
	fmt.Fprintf(&b, "{ __auto_type %s = %s;\n", name, eg.GenerateExpression(e.Target))
	enumName := eg.inferEnumName(e.Target)
	cEnum := eg.inferEnumCName(e.Target)
	stringMatch := eg.inferType(e.Target) == "s" || eg.inferType(e.Target) == "cstr"
	for _, a := range e.Arms {
		if a.Pattern != nil && a.Pattern.Kind == ast.PatternString {
			stringMatch = true
		}
	}
	tagged := false
	if eg.codegen.program != nil {
		if en := eg.codegen.program.FindEnum(enumName); en != nil {
			for _, v := range en.Variants {
				if len(v.FieldTypes) > 0 {
					tagged = true
				}
			}
		}
	}
	if !stringMatch {
		target := name
		if tagged {
			target += ".kind"
		}
		fmt.Fprintf(&b, "switch (%s) {\n", target)
	}
	first := true
	for _, a := range e.Arms {
		if a == nil || a.Pattern == nil {
			continue
		}
		p := a.Pattern
		variant := p.VariantName
		if i := strings.LastIndex(variant, "."); i >= 0 {
			variant = variant[i+1:]
		}
		if stringMatch {
			cond := "1"
			if p.Kind == ast.PatternString {
				lit := "\"" + escapeCString(p.StrValue) + "\""
				if eg.inferType(e.Target) == "cstr" {
					cond = fmt.Sprintf("strcmp(%s, %s) == 0", name, lit)
				} else {
					cond = fmt.Sprintf("%s.len == sizeof(%s)-1 && memcmp(%s.ptr, %s, sizeof(%s)-1) == 0", name, lit, name, lit, lit)
				}
			}
			if !first {
				b.WriteString("else ")
			}
			fmt.Fprintf(&b, "if (%s) {\n", cond)
			first = false
		} else {
			switch p.Kind {
			case ast.PatternWildcard, ast.PatternVariable:
				b.WriteString("default: {\n")
			case ast.PatternVariant:
				fmt.Fprintf(&b, "case %s_Kind_%s: {\n", cEnum, variant)
			case ast.PatternInteger:
				fmt.Fprintf(&b, "case %d: {\n", p.IntValue)
			case ast.PatternBoolean:
				val := 0
				if variant == "true" {
					val = 1
				}
				fmt.Fprintf(&b, "case %d: {\n", val)
			default:
				continue
			}
		}
		eg.codegen.EnterScope("match_arm")
		if p.Kind == ast.PatternVariable {
			for _, n := range p.Bindings {
				if n != "_" {
					fmt.Fprintf(&b, "__auto_type %s = %s;\n", n, name)
					eg.codegen.AddSymbol(n, eg.closureExpressionType(e.Target), false, "local", 0, 0)
				}
			}
		}
		if p.Kind == ast.PatternVariant {
			v := eg.findEnumVariant(enumName, variant)
			if v != nil {
				for i, n := range p.Bindings {
					if n == "_" || i >= len(v.FieldTypes) {
						continue
					}
					field := variant + "_val"
					if len(v.FieldTypes) > 1 {
						field = fmt.Sprintf("%s_val%d", variant, i)
					}
					if i < len(v.FieldNames) && v.FieldNames[i] != "" {
						field = v.FieldNames[i]
					}
					fmt.Fprintf(&b, "%s %s = %s.data.%s;\n", eg.matchBindingCType(v, i, enumName, e.Target), n, name, field)
					eg.codegen.AddSymbol(n, v.FieldTypes[i], false, "local", 0, 0)
				}
			}
		}
		for i, s := range a.Body {
			if result != "" && i == len(a.Body)-1 {
				if ex, ok := s.(*ast.ExpressionStatement); ok {
					fmt.Fprintf(&b, "%s = %s;\n", result, eg.GenerateExpression(ex.Expression))
					continue
				}
			}
			b.WriteString(eg.codegen.generateStatement(s))
		}
		eg.codegen.ExitScope()
		if !stringMatch {
			b.WriteString("break;\n")
		}
		b.WriteString("}\n")
	}
	if !stringMatch {
		b.WriteString("}\n")
	}
	b.WriteString("}\n")
	return b.String()
}
