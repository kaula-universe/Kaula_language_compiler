package codegen

import (
	"fmt"
	"regexp"
	"strings"

	"kaula/internal/ast"
)

// mainShellRegex 匹配模板里包�?{{main_code}} �?main 壳，对空�?换行不敏�?
// （CRLF、缩进差异、int main(void) 都能命中）�?
var mainShellRegex = regexp.MustCompile(`int\s+main\s*\(\s*(?:void)?\s*\)\s*\{\s*\{\{main_code\}\}\s*return\s+0\s*;\s*\}`)

// replaceMainShellWithRunner 用测�?runner（自�?int main）整体替换模板的 main 壳�?
// 不能退化为只替�?{{main_code}}：那会把 runner �?#include、全局变量和函�?
// 定义嵌进模板�?int main() 函数体内，C 编译�?"function definition is not
// allowed here"。返�?false 表示模板里没有可识别�?main 壳�?
func replaceMainShellWithRunner(result, runner string) (string, bool) {
	loc := mainShellRegex.FindStringIndex(result)
	if loc == nil {
		return result, false
	}
	return result[:loc[0]] + runner + result[loc[1]:], true
}

// AttributeFeatures 处理 #[test]�?[derive]�?[command] 等高级属�?
type AttributeFeatures struct {
	cg *CodeGenerator
}

// NewAttributeFeatures 创建属性特性处理器
func NewAttributeFeatures(cg *CodeGenerator) *AttributeFeatures {
	return &AttributeFeatures{cg: cg}
}

// TestFunctionInfo 测试函数信息
type TestFunctionInfo struct {
	Name        string
	Description string
	Ignored     bool
	ShouldPanic bool
}

// CollectTestFunctions 收集所有带 #[test] 属性的函数
func (af *AttributeFeatures) CollectTestFunctions(program *ast.Program) []TestFunctionInfo {
	var tests []TestFunctionInfo
	program.Traverse(func(n ast.Node) {
		if fn, ok := n.(*ast.FunctionStatement); ok {
			if ast.HasAttribute(fn.Attributes, "test") {
				info := TestFunctionInfo{
					Name: fn.Name,
				}
				// 提取 #[test("description")] 的描�?
				for _, attr := range fn.Attributes {
					if attr != nil && attr.Name == "test" && len(attr.Args) > 0 {
						info.Description = strings.Trim(attr.Args[0], "\"'")
					}
				}
				info.Ignored = ast.HasAttribute(fn.Attributes, "ignore")
				info.ShouldPanic = ast.HasAttribute(fn.Attributes, "should_panic")
				tests = append(tests, info)
			}
		}
	})
	return tests
}

// GenerateTestRunner 生成测试运行�?main 函数
func (af *AttributeFeatures) GenerateTestRunner(tests []TestFunctionInfo) string {
	var sb strings.Builder

	sb.WriteString("\n\n/* ===== Auto-generated Test Runner ===== */\n")
	sb.WriteString("#include <stdio.h>\n")
	sb.WriteString("#include <stdlib.h>\n")
	sb.WriteString("#include <string.h>\n")
	sb.WriteString("#include <time.h>\n\n")

	// 测试状态全局变量
	sb.WriteString("static int __kaula_test_count = 0;\n")
	sb.WriteString("static int __kaula_test_passed = 0;\n")
	sb.WriteString("static int __kaula_test_failed = 0;\n")
	sb.WriteString("static int __kaula_test_ignored = 0;\n")
	sb.WriteString("static int __kaula_current_test_failed = 0;\n")
	sb.WriteString("static const char* __kaula_current_test_name = NULL;\n\n")

	// 测试断言函数
	sb.WriteString("void __kaula_test_assert(int cond, const char* expr, const char* file, int line) {\n")
	sb.WriteString("    if (!cond) {\n")
	sb.WriteString("        __kaula_current_test_failed = 1;\n")
	sb.WriteString("        fprintf(stderr, \"  assertion failed: %s (%s:%d)\\n\", expr, file, line);\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("void __kaula_test_assert_eq_i64(long long a, long long b, const char* file, int line) {\n")
	sb.WriteString("    if (a != b) {\n")
	sb.WriteString("        __kaula_current_test_failed = 1;\n")
	sb.WriteString("        fprintf(stderr, \"  assertion failed: %lld == %lld (%s:%d)\\n\", a, b, file, line);\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("void __kaula_test_assert_str_eq(const char* a, const char* b, const char* file, int line) {\n")
	sb.WriteString("    if (a == NULL || b == NULL || strcmp(a, b) != 0) {\n")
	sb.WriteString("        __kaula_current_test_failed = 1;\n")
	sb.WriteString("        fprintf(stderr, \"  assertion failed: \\\"%s\\\" == \\\"%s\\\" (%s:%d)\\n\", a?a:\"(null)\", b?b:\"(null)\", file, line);\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	// 单个测试运行函数
	// should_panic：语义为「断言失败即通过」。该测试若没有触发断言失败�?
	// 说明它本应失败却通过了，此时计为 FAILED�?
	sb.WriteString("static void __kaula_run_test(const char* name, const char* desc, void (*test_fn)(void), int ignored, int should_panic) {\n")
	sb.WriteString("    __kaula_test_count++;\n")
	sb.WriteString("    if (ignored) {\n")
	sb.WriteString("        __kaula_test_ignored++;\n")
	sb.WriteString("        printf(\"test %s ... IGNORED\\n\", name);\n")
	sb.WriteString("        return;\n")
	sb.WriteString("    }\n")
	sb.WriteString("    __kaula_current_test_name = name;\n")
	sb.WriteString("    __kaula_current_test_failed = 0;\n")
	sb.WriteString("    test_fn();\n")
	sb.WriteString("    if (should_panic) {\n")
	sb.WriteString("        if (__kaula_current_test_failed) {\n")
	sb.WriteString("            __kaula_test_passed++;\n")
	sb.WriteString("            printf(\"test %s ... ok\\n\", name);\n")
	sb.WriteString("        } else {\n")
	sb.WriteString("            __kaula_test_failed++;\n")
	sb.WriteString("            printf(\"test %s ... FAILED (expected #[should_panic] to fail, but it passed)\\n\", name);\n")
	sb.WriteString("        }\n")
	sb.WriteString("    } else if (__kaula_current_test_failed) {\n")
	sb.WriteString("        __kaula_test_failed++;\n")
	sb.WriteString("        printf(\"test %s ... FAILED\\n\", name);\n")
	sb.WriteString("    } else {\n")
	sb.WriteString("        __kaula_test_passed++;\n")
	sb.WriteString("        printf(\"test %s ... ok\\n\", name);\n")
	sb.WriteString("    }\n")
	sb.WriteString("    if (desc && desc[0] != '\\0') printf(\"  (%s)\\n\", desc);\n")
	sb.WriteString("}\n\n")

	// main 函数
	sb.WriteString("int main(int argc, char* argv[]) {\n")
	sb.WriteString("    printf(\"\\nRunning Kaula tests...\\n\\n\");\n")
	sb.WriteString("    clock_t start = clock();\n\n")

	// 生成测试调用
	for _, test := range tests {
		desc := cStringLiteral(test.Description)
		if test.Ignored {
			sb.WriteString(fmt.Sprintf("    __kaula_run_test(\"%s\", %s, NULL, 1, %d);\n", test.Name, desc, boolToCInt(test.ShouldPanic)))
		} else {
			sb.WriteString(fmt.Sprintf("    __kaula_run_test(\"%s\", %s, %s, 0, %d);\n", test.Name, desc, af.cg.UserFuncCName(test.Name), boolToCInt(test.ShouldPanic)))
		}
	}

	sb.WriteString("\n    clock_t end = clock();\n")
	sb.WriteString("    double elapsed = (double)(end - start) / CLOCKS_PER_SEC;\n\n")
	sb.WriteString("    printf(\"\\n===== Test Summary =====\\n\");\n")
	sb.WriteString("    printf(\"Total:   %d\\n\", __kaula_test_count);\n")
	sb.WriteString("    printf(\"Passed:  %d\\n\", __kaula_test_passed);\n")
	sb.WriteString("    printf(\"Failed:  %d\\n\", __kaula_test_failed);\n")
	sb.WriteString("    printf(\"Ignored: %d\\n\", __kaula_test_ignored);\n")
	sb.WriteString("    printf(\"Time:    %.3fs\\n\", elapsed);\n")
	sb.WriteString("    printf(\"========================\\n\");\n\n")
	sb.WriteString("    if (__kaula_test_failed > 0) {\n")
	sb.WriteString("        printf(\"\\nSome tests FAILED.\\n\");\n")
	sb.WriteString("        return 1;\n")
	sb.WriteString("    }\n")
	sb.WriteString("    printf(\"\\nAll tests passed.\\n\");\n")
	sb.WriteString("    return 0;\n")
	sb.WriteString("}\n")

	return sb.String()
}

// cStringLiteral 把任意字符串编码为安全的 C 字符串字面量�?
// #[test("描述")] 的描述会直接拼进生成�?C 源码，必须转义引�?反斜�?换行�?
// 否则描述里的一�?" 就能破坏生成代码�?
func cStringLiteral(s string) string {
	if s == "" {
		return "NULL"
	}
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			// 过滤不可打印控制字符，避免生成非�?C 源码
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&sb, `\x%02x`, c)
			} else {
				sb.WriteByte(c)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// boolToCInt �?Go bool 转为 C 整数字面�?
func boolToCInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeriveInfo 派生宏信�?
type DeriveInfo struct {
	TypeName string
	Traits   []string
}

// CollectDeriveTypes 收集所有带 #[derive(...)] 属性的类型
func (af *AttributeFeatures) CollectDeriveTypes(program *ast.Program) []DeriveInfo {
	var derives []DeriveInfo
	program.Traverse(func(n ast.Node) {
		if structStmt, ok := n.(*ast.StructStatement); ok {
			for _, attr := range structStmt.Attributes {
				if attr != nil && attr.Name == "derive" {
					info := DeriveInfo{
						TypeName: structStmt.Name,
						Traits:   attr.Args,
					}
					derives = append(derives, info)
				}
			}
		}
		if classStmt, ok := n.(*ast.ClassStatement); ok {
			for _, attr := range classStmt.Attributes {
				if attr != nil && attr.Name == "derive" {
					info := DeriveInfo{
						TypeName: classStmt.Name,
						Traits:   attr.Args,
					}
					derives = append(derives, info)
				}
			}
		}
	})
	return derives
}

// GenerateDeriveImpls �?#[derive] 类型生成方法实现
func (af *AttributeFeatures) GenerateDeriveImpls(derives []DeriveInfo, program *ast.Program) string {
	var sb strings.Builder

	for _, d := range derives {
		for _, trait := range d.Traits {
			trait = strings.TrimSpace(trait)
			switch strings.ToLower(trait) {
			case "debug":
				sb.WriteString(af.generateDebugImpl(d.TypeName, program))
			case "clone":
				sb.WriteString(af.generateCloneImpl(d.TypeName, program))
			case "eq", "equals":
				sb.WriteString(af.generateEqImpl(d.TypeName, program))
			}
		}
	}

	return sb.String()
}

// generateDebugImpl 生成 Debug trait 实现
func (af *AttributeFeatures) generateDebugImpl(typeName string, program *ast.Program) string {
	fields := af.getTypeFields(typeName, program)
	if len(fields) == 0 {
		return ""
	}

	var sb strings.Builder
	cName := typeName
	fnName := typeName + "_debug"

	// freestanding 模式下没有 libc 的 printf，只有 freestanding.io 的
	// print（变参、同 %d/%s/%f 语义）——#[derive(Debug)] 必须跟着切，
	// 否则裸机链接期出现未定义符号 printf。
	pf := "printf"
	if af.cg != nil && af.cg.config != nil && af.cg.config.Freestanding {
		pf = "print"
	}

	sb.WriteString(fmt.Sprintf("\n/* #[derive(Debug)] for %s */\n", typeName))
	sb.WriteString(fmt.Sprintf("void %s(%s* self) {\n", fnName, cName))
	sb.WriteString(fmt.Sprintf("    %s(\"%s { \");\n", pf, typeName))

	for i, field := range fields {
		if i > 0 {
			sb.WriteString(fmt.Sprintf("    %s(\", \");\n", pf))
		}
		sb.WriteString(fmt.Sprintf("    %s(\"%s: \", \"%s\");\n", pf, field.Name, field.Name))
		// 根据字段类型选择打印方式
		if af.isIntegerType(field.Type) {
			sb.WriteString(fmt.Sprintf("    %s(\"%%lld\", (long long)self->%s);\n", pf, field.Name))
		} else if af.isFloatType(field.Type) {
			sb.WriteString(fmt.Sprintf("    %s(\"%%f\", (double)self->%s);\n", pf, field.Name))
		} else if field.Type == "bool" || field.Type == "bool_t" {
			sb.WriteString(fmt.Sprintf("    %s(\"%%s\", self->%s ? \"true\" : \"false\");\n", pf, field.Name))
		} else if strings.Contains(field.Type, "char*") || field.Type == "string" || field.Type == "String" {
			sb.WriteString(fmt.Sprintf("    %s(\"\\\"%%s\\\"\", self->%s ? self->%s : \"(null)\");\n", pf, field.Name, field.Name))
		} else {
			sb.WriteString(fmt.Sprintf("    %s(\"<%%s>\", \"%s\");\n", pf, field.Type))
		}
	}

	sb.WriteString(fmt.Sprintf("    %s(\" }\\n\");\n", pf))
	sb.WriteString("}\n")

	return sb.String()
}

// generateCloneImpl 生成 Clone trait 实现
func (af *AttributeFeatures) generateCloneImpl(typeName string, program *ast.Program) string {
	fields := af.getTypeFields(typeName, program)
	if len(fields) == 0 {
		return ""
	}

	var sb strings.Builder
	cName := typeName
	fnName := typeName + "_clone"

	sb.WriteString(fmt.Sprintf("\n/* #[derive(Clone)] for %s */\n", typeName))
	sb.WriteString(fmt.Sprintf("%s* %s(%s* self) {\n", cName, fnName, cName))
	sb.WriteString(fmt.Sprintf("    %s* result = (%s*)malloc(sizeof(%s));\n", cName, cName, cName))
	sb.WriteString("    if (!result) return NULL;\n")
	sb.WriteString("    memcpy(result, self, sizeof(" + cName + "));\n")
	sb.WriteString("    return result;\n")
	sb.WriteString("}\n")

	return sb.String()
}

// generateEqImpl 生成 Eq trait 实现
func (af *AttributeFeatures) generateEqImpl(typeName string, program *ast.Program) string {
	fields := af.getTypeFields(typeName, program)
	if len(fields) == 0 {
		return ""
	}

	var sb strings.Builder
	cName := typeName
	fnName := typeName + "_equals"

	sb.WriteString(fmt.Sprintf("\n/* #[derive(Eq)] for %s */\n", typeName))
	sb.WriteString(fmt.Sprintf("int %s(%s* a, %s* b) {\n", fnName, cName, cName))
	sb.WriteString("    if (!a || !b) return 0;\n")

	for _, field := range fields {
		if af.isIntegerType(field.Type) || field.Type == "bool" || field.Type == "bool_t" {
			sb.WriteString(fmt.Sprintf("    if (a->%s != b->%s) return 0;\n", field.Name, field.Name))
		} else if af.isFloatType(field.Type) {
			sb.WriteString(fmt.Sprintf("    if (a->%s != b->%s) return 0;\n", field.Name, field.Name))
		} else if strings.Contains(field.Type, "char*") || field.Type == "string" || field.Type == "String" {
			sb.WriteString(fmt.Sprintf("    if (strcmp(a->%s, b->%s) != 0) return 0;\n", field.Name, field.Name))
		}
	}

	sb.WriteString("    return 1;\n")
	sb.WriteString("}\n")

	return sb.String()
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name string
	Type string
}

// getTypeFields 获取类型的字段列�?
func (af *AttributeFeatures) getTypeFields(typeName string, program *ast.Program) []FieldInfo {
	var fields []FieldInfo
	program.Traverse(func(n ast.Node) {
		if structStmt, ok := n.(*ast.StructStatement); ok && structStmt.Name == typeName {
			for _, f := range structStmt.Fields {
				fields = append(fields, FieldInfo{Name: f.Name, Type: f.Type})
			}
		}
		if classStmt, ok := n.(*ast.ClassStatement); ok && classStmt.Name == typeName {
			for _, f := range classStmt.Fields {
				fields = append(fields, FieldInfo{Name: f.Name, Type: f.Type})
			}
		}
	})
	return fields
}

// isIntegerType 判断是否为整数类�?
func (af *AttributeFeatures) isIntegerType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "size_t", "char":
		return true
	}
	return false
}

// isFloatType 判断是否为浮点类�?
func (af *AttributeFeatures) isFloatType(t string) bool {
	switch t {
	case "f32", "f64", "float", "double", "float32", "float64":
		return true
	}
	return false
}

// CommandInfo 命令行参数信�?
type CommandInfo struct {
	StructName string
	Fields     []CommandField
}

// CommandField 命令行参数字�?
type CommandField struct {
	Name    string
	Type    string
	Short   string
	Long    string
	Default string
	Help    string
}

// CollectCommands 收集所有带 #[command] 属性的结构�?
func (af *AttributeFeatures) CollectCommands(program *ast.Program) []CommandInfo {
	var commands []CommandInfo
	program.Traverse(func(n ast.Node) {
		if structStmt, ok := n.(*ast.StructStatement); ok {
			if ast.HasAttribute(structStmt.Attributes, "command") {
				cmd := CommandInfo{StructName: structStmt.Name}
				for _, f := range structStmt.Fields {
					field := CommandField{Name: f.Name, Type: f.Type}
					// 从字段属性中提取参数信息
					for _, attr := range f.Attributes {
						if attr != nil && attr.Name == "arg" {
							for _, arg := range attr.Args {
								if strings.HasPrefix(arg, "short=") {
									field.Short = strings.Trim(strings.TrimPrefix(arg, "short="), "\"'")
								} else if strings.HasPrefix(arg, "long=") {
									field.Long = strings.Trim(strings.TrimPrefix(arg, "long="), "\"'")
								} else if strings.HasPrefix(arg, "default=") {
									field.Default = strings.Trim(strings.TrimPrefix(arg, "default="), "\"'")
								} else if strings.HasPrefix(arg, "help=") {
									field.Help = strings.Trim(strings.TrimPrefix(arg, "help="), "\"'")
								}
							}
						}
					}
					if field.Long == "" {
						field.Long = field.Name
					}
					cmd.Fields = append(cmd.Fields, field)
				}
				commands = append(commands, cmd)
			}
		}
	})
	return commands
}

// GenerateCommandParser 生成命令行参数解析代�?
func (af *AttributeFeatures) GenerateCommandParser(commands []CommandInfo) string {
	var sb strings.Builder

	for _, cmd := range commands {
		cName := cmd.StructName
		parseFn := cmd.StructName + "_parse"

		sb.WriteString(fmt.Sprintf("\n/* #[command] parser for %s */\n", cmd.StructName))
		sb.WriteString(fmt.Sprintf("%s* %s(int argc, char* argv[]) {\n", cName, parseFn))
		sb.WriteString(fmt.Sprintf("    %s* args = (%s*)calloc(1, sizeof(%s));\n", cName, cName, cName))
		sb.WriteString("    if (!args) return NULL;\n\n")

		// 设置默认�?
		for _, field := range cmd.Fields {
			if field.Default != "" {
				if af.isIntegerType(field.Type) {
					sb.WriteString(fmt.Sprintf("    args->%s = %s;\n", field.Name, field.Default))
				} else if af.isFloatType(field.Type) {
					sb.WriteString(fmt.Sprintf("    args->%s = %s;\n", field.Name, field.Default))
				} else if field.Type == "bool" || field.Type == "bool_t" {
					sb.WriteString(fmt.Sprintf("    args->%s = %s;\n", field.Name, field.Default))
				} else if field.Type == "string" || field.Type == "String" {
					sb.WriteString(fmt.Sprintf("    args->%s = string_wrap(\"%s\");\n", field.Name, field.Default))
				} else {
					sb.WriteString(fmt.Sprintf("    args->%s = strdup(\"%s\");\n", field.Name, field.Default))
				}
			}
		}

		sb.WriteString("\n    for (int i = 1; i < argc; i++) {\n")
		sb.WriteString("        char* arg = argv[i];\n")

		for _, field := range cmd.Fields {
			longFlag := "--" + field.Long
			shortFlag := "-" + field.Short

			sb.WriteString(fmt.Sprintf("        if (strcmp(arg, \"%s\") == 0", longFlag))
			if field.Short != "" {
				sb.WriteString(fmt.Sprintf(" || strcmp(arg, \"%s\") == 0", shortFlag))
			}
			sb.WriteString(") {\n")

			if field.Type == "bool" || field.Type == "bool_t" {
				sb.WriteString(fmt.Sprintf("            args->%s = 1;\n", field.Name))
			} else {
				sb.WriteString("            if (i + 1 < argc) {\n")
				sb.WriteString("                i++;\n")
				if af.isIntegerType(field.Type) {
					sb.WriteString(fmt.Sprintf("                args->%s = (%s)atoll(argv[i]);\n", field.Name, field.Type))
				} else if af.isFloatType(field.Type) {
					sb.WriteString(fmt.Sprintf("                args->%s = (%s)atof(argv[i]);\n", field.Name, field.Type))
				} else if field.Type == "string" || field.Type == "String" {
					sb.WriteString(fmt.Sprintf("                args->%s = string_wrap(argv[i]);\n", field.Name))
				} else {
					sb.WriteString(fmt.Sprintf("                args->%s = strdup(argv[i]);\n", field.Name))
				}
				sb.WriteString("            }\n")
			}
			sb.WriteString("            continue;\n")
			sb.WriteString("        }\n")
		}

		sb.WriteString("    }\n")
		sb.WriteString("    return args;\n")
		sb.WriteString("}\n")
	}

	return sb.String()
}
