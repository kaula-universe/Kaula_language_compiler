package sema

import (
	"fmt"
	"kaula/internal/ast"
	"reflect"
	"regexp"
	"strings"
)

var numericTypeWord = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`)
var numericWidthWord = regexp.MustCompile(`^[iuf][0-9]+$`)
var numericAliases = map[string]string{
	"int": "i32", "uint": "u32", "int8": "i8", "int16": "i16", "int32": "i32", "int64": "i64",
	"uint8": "u8", "uint16": "u16", "uint32": "u32", "uint64": "u64", "byte": "u8", "sbyte": "i8",
	"float": "f32", "float32": "f32", "float64": "f64", "single": "f32", "double": "f64", "real": "f64",
}

// validateNumericSpellings checks explicit source annotations, not types imported
// from C metadata or inferred internally. Extern declarations retain their C ABI.
func (sa *SemanticAnalyzer) validateNumericSpellings(program *ast.Program) {
	var walk func(reflect.Value, ast.Position)
	check := func(text string, pos ast.Position) {
		for _, word := range numericTypeWord.FindAllString(text, -1) {
			if canonical, ok := numericAliases[word]; ok {
				sa.error(fmt.Sprintf("numeric type %s must be written as %s", word, canonical), pos.Line, pos.Column)
			} else if numericWidthWord.MatchString(word) {
				switch word {
				case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64":
				default:
					sa.error(fmt.Sprintf("unsupported numeric type %s; use i8/i16/i32/i64, u8/u16/u32/u64 or f32/f64", word), pos.Line, pos.Column)
				}
			}
		}
	}
	walk = func(v reflect.Value, pos ast.Position) {
		if !v.IsValid() {
			return
		}
		if v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return
			}
			if v.CanInterface() {
				if n, ok := v.Interface().(ast.Node); ok {
					pos = n.GetPosition()
				}
			}
			walk(v.Elem(), pos)
			return
		}
		switch v.Kind() {
		case reflect.Struct:
			if strings.HasPrefix(v.Type().Name(), "Extern") {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				f := v.Type().Field(i)
				if f.PkgPath != "" {
					continue
				}
				value := v.Field(i)
				switch f.Name {
				case "Type", "ReturnType", "TargetType", "UnderlyingType", "FuncReturnType":
					if value.Kind() == reflect.String {
						check(value.String(), pos)
					}
				case "ParamTypes", "FieldTypes", "FuncParamTypes":
					if value.Kind() == reflect.Slice {
						for j := 0; j < value.Len(); j++ {
							if value.Index(j).Kind() == reflect.String {
								check(value.Index(j).String(), pos)
							}
						}
					}
				}
				walk(value, pos)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), pos)
			}
		}
	}
	walk(reflect.ValueOf(program), ast.Position{Line: 1, Column: 1})
}
