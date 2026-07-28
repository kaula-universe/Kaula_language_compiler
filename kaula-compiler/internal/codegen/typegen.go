package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

// TypeGenerator 负责类型相关的代码生成
type TypeGenerator struct {
	codegen     *CodeGenerator
	typeErasure map[string]string
	clibTypeMap map[string]string
	structTypes map[string]bool
}

func NewTypeGenerator(cg *CodeGenerator) *TypeGenerator {
	return &TypeGenerator{
		codegen:     cg,
		typeErasure: make(map[string]string),
		clibTypeMap: make(map[string]string),
		structTypes: make(map[string]bool),
	}
}

// kaulaStructTag 将 Kaula 类型名转换为 C struct tag 名称，添加 K_ 前缀
// 以避免与系统头文件中的宏/类型冲突（如 Windows wingdi.h 的 Rectangle 宏）
func kaulaStructTag(name string) string {
	return "K_" + name
}

var globalTypeMap = map[string]string{
	"i8":         "int8_t",
	"i16":        "int16_t",
	"i32":        "int32_t",
	"i64":        "int64_t",
	"int8":       "int8_t",
	"int16":      "int16_t",
	"int32":      "int32_t",
	"int64":      "int64_t",
	"int":        "int64_t",
	"integer":    "int64_t",
	"long":       "int64_t",
	"long_long":  "long long",
	"short":      "int16_t",
	"u8":         "uint8_t",
	"u16":        "uint16_t",
	"u32":        "uint32_t",
	"u64":        "uint64_t",
	"uint8":      "uint8_t",
	"uint16":     "uint16_t",
	"uint32":     "uint32_t",
	"uint64":     "uint64_t",
	"uint":       "uint64_t",
	"uchar":      "unsigned char",
	"ushort":     "uint16_t",
	"ulong":      "uint64_t",
	"ulong_long": "unsigned long long",
	"float":      "float",
	"f32":        "float",
	"single":     "float",
	"double":     "double",
	"f64":        "double",
	"real":       "double",
	"bool":       "int",
	"boolean":    "int",
	"char":       "char",
	"byte":       "uint8_t",
	"sbyte":      "int8_t",
	"void":       "void",
	"string":     "String",
	"cstring":    "const char*",
	"str":        "String",
	"intptr":     "intptr_t",
	"uintptr":    "uintptr_t",
	"size":       "size_t",
	"ssize":      "ssize_t",
}

func (tg *TypeGenerator) MapKaulaTypeToC(kaulaType string) string {
	typeLower := strings.ToLower(kaulaType)
	if cType, ok := globalTypeMap[typeLower]; ok {
		return cType
	}
	// 处理固定大小数组类型 [N]type → 转换为 C 的 "elemType[N]" 格式
	if len(typeLower) > 0 && typeLower[0] == '[' {
		closeBracket := strings.Index(typeLower, "]")
		if closeBracket > 0 {
			arraySize := typeLower[1:closeBracket]
			elemType := typeLower[closeBracket+1:]
			cElemType := tg.MapKaulaTypeToC(elemType)
			return cElemType + "[" + arraySize + "]"
		}
	}
	if strings.HasPrefix(typeLower, "[]") {
		innerType := typeLower[2:]
		if innerType == "cstring" {
			return "const char**"
		}
		if innerType == "string" || innerType == "str" {
			return "String*"
		}
		if cType, ok := globalTypeMap[innerType]; ok {
			return cType + "*"
		}
		return kaulaType[2:] + "*"
	}
	if strings.HasPrefix(typeLower, "*") {
		innerType := typeLower[1:]
		if innerType == "cstring" {
			return "const char**"
		}
		if innerType == "string" || innerType == "str" {
			return "String*"
		}
		if cType, ok := globalTypeMap[innerType]; ok {
			return cType + "*"
		}
		// 对于 struct 类型，内部递归 tg.MapKaulaTypeToC 会添加 struct 前缀
		return tg.MapKaulaTypeToC(kaulaType[1:]) + "*"
	}
	if strings.HasSuffix(typeLower, "*") {
		baseType := typeLower[:len(typeLower)-1]
		if baseType == "cstring" {
			return "const char**"
		}
		if baseType == "string" || baseType == "str" {
			return "String*"
		}
		if cType, ok := globalTypeMap[baseType]; ok {
			return cType + "*"
		}
		return kaulaType[:len(kaulaType)-1] + "*"
	}
	if strings.HasPrefix(typeLower, "const ") {
		innerType := typeLower[6:]
		if innerType == "string" || innerType == "str" {
			return "String"
		}
		if cType, ok := globalTypeMap[innerType]; ok {
			return "const " + cType
		}
		return "const " + kaulaType[6:]
	}
	if tg.structTypes[kaulaType] {
		return kaulaStructTag(kaulaType)
	}
	return kaulaType
}

func (tg *TypeGenerator) RegisterClibType(kaulaType string, cType string) {
	tg.clibTypeMap[kaulaType] = cType
}

func (tg *TypeGenerator) GenerateCLibHeaders(headers []string) string {
	var code strings.Builder
	for _, h := range headers {
		code.WriteString(fmt.Sprintf("#include %s\n", h))
	}
	return code.String()
}

// CLibFuncSignature C 库函数签名配置
type CLibFuncSignature struct {
	Args   []string `json:"args"`
	Return string   `json:"return"`
}

// CLibConfig C 库完整配置
type CLibConfig struct {
	Header    string                        `json:"header"`
	Headers   []string                      `json:"headers"`
	Functions map[string]*CLibFuncSignature `json:"functions"`
}

// GenerateClibWrappers 生成 C 库包装函数
func (tg *TypeGenerator) GenerateClibWrappers(config *CLibConfig) string {
	if config == nil || config.Functions == nil {
		return ""
	}

	var code strings.Builder
	code.WriteString("// ============================================\n")
	code.WriteString("// 自动生成的 C 库包装函数 (零成本适配层)\n")
	code.WriteString("// ============================================\n\n")

	for funcName, sig := range config.Functions {
		code.WriteString(fmt.Sprintf("static inline %s kaula_%s_wrapped(", sig.Return, funcName))

		for i, arg := range sig.Args {
			erasedType := tg.eraseGenericType(arg)
			if i > 0 {
				code.WriteString(", ")
			}
			code.WriteString(fmt.Sprintf("%s arg%d", erasedType, i))
		}
		code.WriteString(") {\n")

		code.WriteString(fmt.Sprintf("    return %s(", funcName))
		for i := range sig.Args {
			if i > 0 {
				code.WriteString(", ")
			}
			code.WriteString(fmt.Sprintf("arg%d", i))
		}
		code.WriteString(");\n}\n\n")
	}

	return code.String()
}

func (tg *TypeGenerator) eraseGenericType(typeName string) string {
	if len(typeName) == 1 && typeName[0] >= 'A' && typeName[0] <= 'Z' {
		return "void*"
	}

	if strings.Contains(typeName, "<") {
		return "void*"
	}

	if erased, ok := tg.typeErasure[typeName]; ok {
		return erased
	}

	switch typeName {
	case "int", "float", "double", "bool", "char", "string", "i32", "i64", "f32", "f64":
		tg.typeErasure[typeName] = typeName
		return typeName
	}

	erased := typeName + "*"
	tg.typeErasure[typeName] = erased
	return erased
}

func (tg *TypeGenerator) substituteType(typeName string, typeMap map[string]string) string {
	if typeMap == nil {
		return typeName
	}

	if substituted, ok := typeMap[typeName]; ok {
		return substituted
	}

	return typeName
}

func (tg *TypeGenerator) GenerateClassStatement(stmt *ast.ClassStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Class: %s (Implements: %v)\n", stmt.Name, stmt.Implements))

	if stmt.Generic {
		return tg.GenerateGenericClassStatement(stmt)
	}

	tg.structTypes[stmt.Name] = true
	code.WriteString(fmt.Sprintf("typedef struct %s {\n", kaulaStructTag(stmt.Name)))

	for _, ifaceName := range stmt.Implements {
		code.WriteString(fmt.Sprintf("    %s_MethodGroup %s;\n", ifaceName, ifaceName))
	}

	for _, field := range stmt.Fields {
		fieldType := tg.convertType(field.Type, field.Nullable)
		code.WriteString(fmt.Sprintf("    %s %s;\n", fieldType, field.Name))
	}
	code.WriteString(fmt.Sprintf("} %s;\n\n", kaulaStructTag(stmt.Name)))

	for _, method := range stmt.Methods {
		returnType := tg.convertType(method.ReturnType, false)
		code.WriteString(fmt.Sprintf("static inline %s %s_%s(%s* self", returnType, stmt.Name, method.Name, tg.convertType(stmt.Name, false)))
		for _, param := range method.Params {
			paramType := tg.convertType(param.Type, false)
			code.WriteString(fmt.Sprintf(", %s %s", paramType, param.Name))
		}
		code.WriteString(");\n")
	}
	code.WriteString("\n")

	for _, constructor := range stmt.Constructors {
		code.WriteString(tg.GenerateConstructorStatementWithInterfaceInit(stmt.Name, stmt.Implements, stmt.Methods, constructor))
	}

	for _, method := range stmt.Methods {
		code.WriteString(tg.GenerateMethodStatement(stmt.Name, method))
	}

	return code.String()
}

func (tg *TypeGenerator) GenerateConstructorStatementWithInterfaceInit(className string, interfaces []string, methods []*ast.MethodStatement, constructor *ast.ConstructorStatement) string {
	var code strings.Builder
	cName := kaulaStructTag(className)
	code.WriteString(fmt.Sprintf("%s* %s_new(", cName, className))
	for i, param := range constructor.Params {
		paramType := tg.convertType(param.Type, param.Nullable)
		if i > 0 {
			code.WriteString(", ")
		}
		code.WriteString(fmt.Sprintf("%s %s", paramType, param.Name))
	}
	code.WriteString(") {\n")

	code.WriteString(tg.codegen.indentString() + fmt.Sprintf("%s* self = KMM_V4_ALLOC_ZERO(%s);\n", cName, cName))
	code.WriteString(tg.codegen.indentString() + "if (self == NULL) { return NULL; }\n\n")

	if len(interfaces) > 0 && len(methods) > 0 {
		code.WriteString(tg.codegen.indentString() + "// Initialize interface method groups (composition grouping)\n")

		ifaceMethodsMap := make(map[string]map[string]bool)
		for _, ifaceName := range interfaces {
			ifaceMethodsMap[ifaceName] = make(map[string]bool)
			ifaceMethods := tg.getInterfaceMethods(ifaceName)
			if ifaceMethods != nil {
				for _, m := range ifaceMethods {
					ifaceMethodsMap[ifaceName][m.Name] = true
				}
			}
		}

		for _, classMethod := range methods {
			for _, ifaceName := range interfaces {
				if ifaceMethodsMap[ifaceName] != nil {
					if ifaceMethodsMap[ifaceName][classMethod.Name] {
						code.WriteString(tg.codegen.indentString() + fmt.Sprintf("self->%s.%s = (%s(*)(void*))%s_%s;\n", ifaceName, classMethod.Name, tg.convertType(classMethod.ReturnType, false), className, classMethod.Name))
					}
				}
			}
		}
		code.WriteString("\n")
	}

	for _, bodyStmt := range constructor.Body {
		code.WriteString(tg.codegen.indentString() + tg.generateStatementWithSelfPrefix(className, bodyStmt))
	}

	code.WriteString(tg.codegen.indentString() + "return self;\n")
	code.WriteString("}\n\n")

	return code.String()
}

func (tg *TypeGenerator) getInterfaceMethods(ifaceName string) []*ast.MethodStatement {
	if tg.codegen.program == nil {
		return nil
	}
	iface := tg.codegen.program.FindInterface(ifaceName)
	if iface == nil {
		return nil
	}
	return iface.Methods
}

func (tg *TypeGenerator) generateStatementWithSelfPrefix(className string, stmt ast.Statement) string {
	generated := tg.codegen.generateStatement(stmt)
	if generated == "" {
		return generated
	}

	// 预计算类字段集合，避免每行重复查询
	fieldSet := make(map[string]bool)
	for _, field := range tg.getClassFields(className) {
		fieldSet[field.Name] = true
	}

	lines := strings.Split(generated, "\n")
	var b strings.Builder
	b.Grow(len(generated) + 64)

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")

		if trimmed == "" || trimmed == ";" || trimmed == "}" || trimmed == "{" {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}

		// 查找赋值位置（跳过 ==）
		assignPos := -1
		for i := 0; i < len(trimmed); i++ {
			if i+1 < len(trimmed) && trimmed[i] == '=' && trimmed[i+1] == '=' {
				i++
				continue
			}
			if trimmed[i] == '=' {
				assignPos = i
				break
			}
		}

		if assignPos > 0 {
			lhsTrimmed := strings.TrimRight(trimmed[:assignPos], " ")
			if fieldSet[lhsTrimmed] {
				// 需要添加 self-> 前缀
				prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				b.WriteString(prefix)
				b.WriteString("self->")
				b.WriteString(lhsTrimmed)
				b.WriteString(trimmed[assignPos:])
				b.WriteByte('\n')
				continue
			}
		}

		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}

func (tg *TypeGenerator) getMethodBodyWithSelfPrefix(className string, method *ast.MethodStatement) string {
	// 预计算类字段集合
	fieldSet := make(map[string]bool)
	for _, field := range tg.getClassFields(className) {
		fieldSet[field.Name] = true
	}

	var b strings.Builder
	for _, bodyStmt := range method.Body {
		generated := tg.codegen.generateStatement(bodyStmt)
		if generated == "" {
			continue
		}

		lines := strings.Split(generated, "\n")
		for _, line := range lines {
			trimmed := strings.TrimLeft(line, " \t")

			if trimmed == "" || trimmed == ";" || trimmed == "}" || trimmed == "{" {
				b.WriteString(line)
				b.WriteByte('\n')
				continue
			}

			if strings.HasPrefix(trimmed, "return") {
				returnExpr := strings.TrimRight(trimmed[6:], "; ")
				returnExpr = strings.TrimSpace(returnExpr)

				prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				b.WriteString(prefix)
				b.WriteString("return ")
				if fieldSet[returnExpr] {
					b.WriteString("self->")
				}
				b.WriteString(returnExpr)
				b.WriteString(";\n")
			} else {
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func (tg *TypeGenerator) getClassFields(className string) []*ast.FieldDeclaration {
	if tg.codegen.program == nil {
		return nil
	}
	for _, stmt := range tg.codegen.program.Statements {
		if classStmt, ok := stmt.(*ast.ClassStatement); ok {
			if classStmt.Name == className {
				return classStmt.Fields
			}
		}
	}
	return nil
}

func (tg *TypeGenerator) GenerateGenericClassStatement(stmt *ast.ClassStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Generic Class: %s", stmt.Name))

	if len(stmt.TypeParams) > 0 {
		code.WriteString("<")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				code.WriteString(", ")
			}
			code.WriteString(tp.Name)
		}
		code.WriteString(">\n")
	} else {
		code.WriteString("\n")
	}

	code.WriteString(fmt.Sprintf("typedef struct %s {\n", kaulaStructTag(stmt.Name)))
	for _, field := range stmt.Fields {
		fieldType := tg.eraseGenericType(field.Type)
		if field.Nullable {
			fieldType += "*"
		}
		code.WriteString(fmt.Sprintf("    %s %s;\n", fieldType, field.Name))
	}
	code.WriteString(fmt.Sprintf("} %s;\n\n", kaulaStructTag(stmt.Name)))

	for _, constructor := range stmt.Constructors {
		code.WriteString(tg.GenerateGenericConstructorStatement(stmt.Name, constructor))
	}

	return code.String()
}

func (tg *TypeGenerator) GenerateInterfaceStatement(stmt *ast.InterfaceStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Interface: %s (Composition Grouping - Zero Overhead)\n", stmt.Name))

	methodGroupName := stmt.Name + "_MethodGroup"
	tg.structTypes[methodGroupName] = true
	code.WriteString(fmt.Sprintf("typedef struct %s {\n", kaulaStructTag(methodGroupName)))
	for _, method := range stmt.Methods {
		returnType := tg.convertType(method.ReturnType, false)
		code.WriteString(fmt.Sprintf("    %s (*%s)(void* self", returnType, method.Name))
		for _, param := range method.Params {
			paramType := tg.convertType(param.Type, false)
			code.WriteString(fmt.Sprintf(", %s %s", paramType, param.Name))
		}
		code.WriteString(");\n")
	}
	code.WriteString(fmt.Sprintf("} %s;\n\n", kaulaStructTag(methodGroupName)))

	return code.String()
}

func (tg *TypeGenerator) GenerateStructStatement(stmt *ast.StructStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Struct: %s (Generic=%v, TypeParams=%d)\n", stmt.Name, stmt.Generic, len(stmt.TypeParams)))

	if stmt.Generic {
		return tg.GenerateGenericStructStatement(stmt)
	}

	tg.structTypes[stmt.Name] = true
	code.WriteString(fmt.Sprintf("typedef struct %s {\n", kaulaStructTag(stmt.Name)))
	for _, field := range stmt.Fields {
		fieldType := tg.convertType(field.Type, field.Nullable)
		// 位域: fieldName: type : width → type fieldName : width;
		bitSuffix := ""
		if field.BitWidth > 0 {
			bitSuffix = fmt.Sprintf(" : %d", field.BitWidth)
		}
		// 处理数组字段：C 语法为 "elemType name[N]"，而非 "elemType[N] name"
		if strings.Contains(fieldType, "[") && !strings.HasSuffix(fieldType, "*") {
			openBracket := strings.Index(fieldType, "[")
			cElemType := fieldType[:openBracket]
			arrayPart := fieldType[openBracket:]
			code.WriteString(fmt.Sprintf("    %s %s%s%s;\n", cElemType, field.Name, arrayPart, bitSuffix))
		} else {
			code.WriteString(fmt.Sprintf("    %s %s%s;\n", fieldType, field.Name, bitSuffix))
		}
	}

	// 生成属性对应的 __attribute__
	attrStr := generateStructAttributes(stmt.Attributes)
	if attrStr != "" {
		code.WriteString(fmt.Sprintf("} %s %s;\n\n", attrStr, kaulaStructTag(stmt.Name)))
	} else {
		code.WriteString(fmt.Sprintf("} %s;\n\n", kaulaStructTag(stmt.Name)))
	}

	return code.String()
}

// generateStructAttributes 将 Kaula 属性转换为 C 的 __attribute__ 语法
// 放在 struct 定义的 } 和类型名之间
func generateStructAttributes(attrs []*ast.Attribute) string {
	if len(attrs) == 0 {
		return ""
	}
	var parts []string
	for _, attr := range attrs {
		switch attr.Name {
		case "packed":
			parts = append(parts, "__attribute__((packed))")
		case "aligned":
			if len(attr.Args) > 0 {
				parts = append(parts, fmt.Sprintf("__attribute__((aligned(%s)))", attr.Args[0]))
			} else {
				parts = append(parts, "__attribute__((aligned))")
			}
		}
	}
	return strings.Join(parts, " ")
}

// GenerateEnumStatement 生成枚举（ADT/tagged union）的 C 代码
// 编译为 C 的 tagged union:
//
//	enum Result<T, E> { Ok(T), Err(E) }
//
// 编译为:
//
//	typedef enum { Result_Kind_Ok, Result_Kind_Err } Result_Kind;
//	typedef struct Result { Result_Kind kind; union { struct { T Ok_val; }; struct { E Err_val; }; } data; } Result;
func (tg *TypeGenerator) GenerateEnumStatement(stmt *ast.EnumStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Enum: %s (ADT/Tagged Union, Generic=%v)\n", stmt.Name, stmt.Generic))

	if stmt.Generic {
		return tg.GenerateGenericEnumStatement(stmt)
	}

	// 检查是否有带数据的变体
	hasDataVariants := false
	for _, variant := range stmt.Variants {
		if len(variant.FieldTypes) > 0 {
			hasDataVariants = true
			break
		}
	}

	if !hasDataVariants {
		// 简单枚举（无数据），直接生成 C enum
		code.WriteString(fmt.Sprintf("typedef enum {\n"))
		for i, variant := range stmt.Variants {
			code.WriteString(fmt.Sprintf("    %s_Kind_%s", stmt.Name, variant.Name))
			if i < len(stmt.Variants)-1 {
				code.WriteString(",")
			}
			code.WriteString("\n")
		}
		code.WriteString(fmt.Sprintf("} %s;\n\n", stmt.Name))
		return code.String()
	}

	// 生成 kind 枚举
	code.WriteString(fmt.Sprintf("typedef enum {\n"))
	for i, variant := range stmt.Variants {
		code.WriteString(fmt.Sprintf("    %s_Kind_%s", stmt.Name, variant.Name))
		if i < len(stmt.Variants)-1 {
			code.WriteString(",")
		}
		code.WriteString("\n")
	}
	code.WriteString(fmt.Sprintf("} %s_Kind;\n\n", stmt.Name))

	// 生成 tagged union 结构体
	tg.structTypes[stmt.Name] = true
	code.WriteString(fmt.Sprintf("typedef struct %s {\n", kaulaStructTag(stmt.Name)))
	code.WriteString(fmt.Sprintf("    %s_Kind kind;\n", stmt.Name))
	code.WriteString("    union {\n")
	for _, variant := range stmt.Variants {
		if len(variant.FieldTypes) > 0 {
			code.WriteString("        struct { ")
			for j, fieldType := range variant.FieldTypes {
				cType := tg.MapKaulaTypeToC(fieldType)
				fieldName := variant.Name + "_val"
				if len(variant.FieldNames) > j && variant.FieldNames[j] != "" {
					fieldName = variant.FieldNames[j]
				} else if len(variant.FieldTypes) > 1 {
					fieldName = fmt.Sprintf("%s_val%d", variant.Name, j)
				}
				if j > 0 {
					code.WriteString("; ")
				}
				code.WriteString(fmt.Sprintf("%s %s", cType, fieldName))
			}
			code.WriteString("; };\n")
		}
	}
	code.WriteString("    } data;\n")
	code.WriteString(fmt.Sprintf("} %s;\n\n", kaulaStructTag(stmt.Name)))

	return code.String()
}

// GenerateGenericEnumStatement 生成泛型枚举的 C 代码（类型擦除为 void*）
func (tg *TypeGenerator) GenerateGenericEnumStatement(stmt *ast.EnumStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Generic Enum: %s", stmt.Name))

	if len(stmt.TypeParams) > 0 {
		code.WriteString("<")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				code.WriteString(", ")
			}
			code.WriteString(tp.Name)
		}
		code.WriteString(">")
	}
	code.WriteString("\n")

	// 生成 kind 枚举
	code.WriteString(fmt.Sprintf("typedef enum {\n"))
	for i, variant := range stmt.Variants {
		code.WriteString(fmt.Sprintf("    %s_Kind_%s", stmt.Name, variant.Name))
		if i < len(stmt.Variants)-1 {
			code.WriteString(",")
		}
		code.WriteString("\n")
	}
	code.WriteString(fmt.Sprintf("} %s_Kind;\n\n", stmt.Name))

	// 生成 tagged union（泛型类型擦除为 void*）
	tg.structTypes[stmt.Name] = true
	code.WriteString(fmt.Sprintf("typedef struct %s {\n", kaulaStructTag(stmt.Name)))
	code.WriteString(fmt.Sprintf("    %s_Kind kind;\n", stmt.Name))
	code.WriteString("    union {\n")
	for _, variant := range stmt.Variants {
		if len(variant.FieldTypes) > 0 {
			code.WriteString("        struct { ")
			for j, fieldType := range variant.FieldTypes {
				cType := tg.eraseGenericType(fieldType)
				fieldName := variant.Name + "_val"
				if len(variant.FieldNames) > j && variant.FieldNames[j] != "" {
					fieldName = variant.FieldNames[j]
				}
				if j > 0 {
					code.WriteString("; ")
				}
				code.WriteString(fmt.Sprintf("%s %s", cType, fieldName))
			}
			code.WriteString("; };\n")
		}
	}
	code.WriteString("    } data;\n")
	code.WriteString(fmt.Sprintf("} %s;\n\n", kaulaStructTag(stmt.Name)))

	return code.String()
}

func (tg *TypeGenerator) GenerateGenericStructStatement(stmt *ast.StructStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Generic Struct: %s", stmt.Name))

	if len(stmt.TypeParams) > 0 {
		code.WriteString("<")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				code.WriteString(", ")
			}
			code.WriteString(tp.Name)
		}
		code.WriteString(">\n")
	} else {
		code.WriteString("\n")
	}

	tg.structTypes[stmt.Name] = true
	code.WriteString(fmt.Sprintf("typedef struct %s {\n", kaulaStructTag(stmt.Name)))
	for _, field := range stmt.Fields {
		fieldType := tg.eraseGenericType(field.Type)
		if field.Nullable {
			fieldType += "*"
		}
		// 位域支持
		bitSuffix := ""
		if field.BitWidth > 0 {
			bitSuffix = fmt.Sprintf(" : %d", field.BitWidth)
		}
		code.WriteString(fmt.Sprintf("    %s %s%s;\n", fieldType, field.Name, bitSuffix))
	}
	code.WriteString(fmt.Sprintf("} %s;\n\n", kaulaStructTag(stmt.Name)))

	return code.String()
}

func (tg *TypeGenerator) GenerateConstructorStatement(className string, constructor *ast.ConstructorStatement) string {
	var code strings.Builder
	cName := kaulaStructTag(className)
	code.WriteString(fmt.Sprintf("%s* %s_new(", cName, className))
	for i, param := range constructor.Params {
		paramType := tg.convertType(param.Type, param.Nullable)
		if i > 0 {
			code.WriteString(", ")
		}
		code.WriteString(fmt.Sprintf("%s %s", paramType, param.Name))
	}
	code.WriteString(") {\n")

	code.WriteString(tg.codegen.indentString() + fmt.Sprintf("%s* self = KMM_V4_ALLOC_ZERO(%s);\n", cName, cName))
	code.WriteString(tg.codegen.indentString() + "if (self == NULL) { return NULL; }\n\n")

	code.WriteString(tg.codegen.indentString() + fmt.Sprintf("// Initialize interface method groups\n"))

	for _, bodyStmt := range constructor.Body {
		code.WriteString(tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt))
	}

	code.WriteString(tg.codegen.indentString() + "return self;\n")
	code.WriteString("}\n\n")

	return code.String()
}

func (tg *TypeGenerator) GenerateGenericConstructorStatement(className string, constructor *ast.ConstructorStatement) string {
	var code strings.Builder
	cName := kaulaStructTag(className)
	code.WriteString(fmt.Sprintf("%s* %s_new(", cName, className))
	for i, param := range constructor.Params {
		paramType := tg.eraseGenericType(param.Type)
		if param.Nullable {
			paramType += "*"
		}
		if i > 0 {
			code.WriteString(", ")
		}
		code.WriteString(fmt.Sprintf("%s %s", paramType, param.Name))
	}
	code.WriteString(") {\n")

	code.WriteString(tg.codegen.indentString() + fmt.Sprintf("%s* self = KMM_V4_ALLOC_ZERO(%s);\n", cName, cName))
	code.WriteString(tg.codegen.indentString() + "if (self == NULL) { return NULL; }\n\n")

	for _, bodyStmt := range constructor.Body {
		code.WriteString(tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt))
	}

	code.WriteString(tg.codegen.indentString() + "return self;\n")
	code.WriteString("}\n\n")

	return code.String()
}

func (tg *TypeGenerator) GenerateMethodStatement(className string, method *ast.MethodStatement) string {
	returnType := tg.convertType(method.ReturnType, false)

	var code strings.Builder
	code.WriteString(fmt.Sprintf("static inline %s %s_%s(%s* self", returnType, className, method.Name, className))
	for _, param := range method.Params {
		paramType := tg.convertType(param.Type, false)
		code.WriteString(fmt.Sprintf(", %s %s", paramType, param.Name))
	}
	code.WriteString(") {\n")

	bodyCode := tg.getMethodBodyWithSelfPrefix(className, method)
	code.WriteString(bodyCode)

	if returnType != "void" && !methodHasReturn(method.Body) {
		code.WriteString(tg.codegen.indentString() + "return NULL;\n")
	}
	code.WriteString("}\n\n")

	return code.String()
}

func (tg *TypeGenerator) GenerateGenericMethodStatement(className string, method *ast.MethodStatement) string {
	returnType := tg.eraseGenericType(method.ReturnType)

	var code strings.Builder
	code.WriteString(fmt.Sprintf("static inline %s %s_%s(%s* self", returnType, className, method.Name, className))
	for _, param := range method.Params {
		paramType := tg.eraseGenericType(param.Type)
		code.WriteString(fmt.Sprintf(", %s %s", paramType, param.Name))
	}
	code.WriteString(") {\n")

	for _, bodyStmt := range method.Body {
		code.WriteString(tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt))
	}

	if returnType != "void" && !methodHasReturn(method.Body) {
		code.WriteString(tg.codegen.indentString() + "return NULL;\n")
	}
	code.WriteString("}\n\n")

	return code.String()
}

func methodHasReturn(stmts []ast.Statement) bool {
	for _, s := range stmts {
		if _, ok := s.(*ast.ReturnStatement); ok {
			return true
		}
		if block, ok := s.(*ast.BlockStatement); ok {
			if methodHasReturn(block.Statements) {
				return true
			}
		}
		if ifStmt, ok := s.(*ast.IfStatement); ok {
			if methodHasReturn(ifStmt.Body) || methodHasReturn(ifStmt.Else) {
				return true
			}
		}
	}
	return false
}

func (tg *TypeGenerator) convertType(kaulaType string, nullable bool) string {
	if cType, ok := tg.clibTypeMap[kaulaType]; ok {
		if nullable && !strings.HasSuffix(cType, "*") {
			cType += "*"
		}
		return cType
	}

	cType := tg.MapKaulaTypeToC(kaulaType)

	if nullable && !strings.HasSuffix(cType, "*") {
		cType += "*"
	}

	return cType
}

// GetTypeSize 获取 Kaula 类型的大小（字节数）
// 用于编译期优化：当 sizeof 表达式用于 malloc 参数时，尝试在编译期确定大小
// 返回 (size, true) 如果能确定大小，否则返回 (0, false)
func (tg *TypeGenerator) GetTypeSize(kaulaType string) (int, bool) {
	if kaulaType == "" {
		return 0, false
	}

	// 基本类型大小映射
	basicTypeSizes := map[string]int{
		"i8": 1, "int8": 1, "sbyte": 1, "byte": 1, "uint8": 1, "u8": 1, "char": 1, "bool": 1, "boolean": 1,
		"i16": 2, "int16": 2, "short": 2, "ushort": 2, "uint16": 2, "u16": 2,
		"i32": 4, "int32": 4, "int": 4, "integer": 4, "float": 4, "f32": 4, "single": 4,
		"i64": 8, "int64": 8, "long": 8, "uint": 8, "uint64": 8, "u64": 8,
		"double": 8, "f64": 8, "real": 8,
	}

	typeLower := strings.ToLower(kaulaType)
	if size, ok := basicTypeSizes[typeLower]; ok {
		return size, true
	}

	// 指针类型：所有指针大小相同（8 字节，64 位系统）
	if strings.HasPrefix(typeLower, "*") || strings.HasSuffix(typeLower, "*") {
		return 8, true
	}

	// 字符串类型：String 结构体 {size_t len; char* ptr;} = 16 字节
	if typeLower == "string" || typeLower == "str" {
		return 16, true
	}
	// cstring：const char* = 8 字节
	if typeLower == "cstring" {
		return 8, true
	}

	// 数组类型：[N]type -> N * sizeof(type)
	if len(typeLower) > 0 && typeLower[0] == '[' {
		closeBracket := strings.Index(typeLower, "]")
		if closeBracket > 0 {
			arraySizeStr := typeLower[1:closeBracket]
			elemType := typeLower[closeBracket+1:]
			arraySize := 0
			for _, ch := range arraySizeStr {
				if ch >= '0' && ch <= '9' {
					arraySize = arraySize*10 + int(ch-'0')
				} else {
					return 0, false // 非数字数组大小，无法编译期确定
				}
			}
			elemSize, ok := tg.GetTypeSize(elemType)
			if ok {
				return arraySize * elemSize, true
			}
		}
	}

	// 动态数组 []type -> 指针大小（数组描述符）
	if strings.HasPrefix(typeLower, "[]") {
		return 8, true // 简化为指针大小
	}

	// 用户定义的类型（struct/class）：尝试从已生成的类型中查找
	// 这里简化处理，返回 false 让优化走 offset_save/restore 路径
	// 实际可以通过查询符号表或类型定义来获取大小
	return 0, false
}

func (tg *TypeGenerator) GenerateTypeAliasStatement(stmt *ast.TypeAliasStatement) string {
	if stmt == nil {
		return ""
	}

	if stmt.Generic {
		return tg.GenerateGenericTypeAliasStatement(stmt)
	}

	if stmt.IsFuncType {
		return tg.GenerateFuncTypeAliasStatement(stmt)
	}

	cType := tg.convertTypeAliasToCType(stmt.UnderlyingType)
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Type alias: %s = %s\n", stmt.Name, stmt.UnderlyingType))
	code.WriteString(fmt.Sprintf("typedef %s %s;\n\n", cType, stmt.Name))

	return code.String()
}

func (tg *TypeGenerator) GenerateFuncTypeAliasStatement(stmt *ast.TypeAliasStatement) string {
	returnType := tg.convertTypeAliasToCType(stmt.FuncReturnType)
	if returnType == "" {
		returnType = "void"
	}

	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Function type alias: %s func(", stmt.Name))

	var params []string
	for i, param := range stmt.FuncParams {
		paramType := tg.convertTypeAliasToCType(param.Type)
		if param.Nullable {
			paramType += "*"
		}
		params = append(params, fmt.Sprintf("%s arg%d", paramType, i))
	}
	code.WriteString(strings.Join(params, ", "))
	code.WriteString(fmt.Sprintf(") %s\n", returnType))

	code.WriteString(fmt.Sprintf("typedef %s (*%s)(", returnType, stmt.Name))

	var cParams []string
	for i, param := range stmt.FuncParams {
		paramType := tg.convertTypeAliasToCType(param.Type)
		if param.Nullable {
			paramType += "*"
		}
		cParams = append(cParams, fmt.Sprintf("%s arg%d", paramType, i))
	}
	if len(cParams) > 0 {
		code.WriteString(strings.Join(cParams, ", "))
	}
	code.WriteString(");\n\n")

	return code.String()
}

func (tg *TypeGenerator) convertTypeAliasToCType(kaulaType string) string {
	cType := kaulaType

	cType = strings.ReplaceAll(cType, "*void", "void*")
	cType = strings.ReplaceAll(cType, "*int", "int*")
	cType = strings.ReplaceAll(cType, "*float", "float*")
	cType = strings.ReplaceAll(cType, "*double", "double*")
	cType = strings.ReplaceAll(cType, "*bool", "bool*")
	cType = strings.ReplaceAll(cType, "*char", "char*")
	cType = strings.ReplaceAll(cType, "*string", "char**")

	if strings.HasPrefix(cType, "[]") {
		innerType := cType[2:]
		if innerType == "string" {
			cType = "char**"
		} else {
			cType = innerType + "*"
		}
	}

	if cType == "string" {
		cType = "char*"
	}

	return cType
}

func (tg *TypeGenerator) GenerateGenericTypeAliasStatement(stmt *ast.TypeAliasStatement) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("// Generic Type alias: %s", stmt.Name))

	if len(stmt.TypeParams) > 0 {
		code.WriteString("<")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				code.WriteString(", ")
			}
			code.WriteString(tp.Name)
		}
		code.WriteString(">\n")
	} else {
		code.WriteString("\n")
	}

	underlyingType := stmt.UnderlyingType
	for _, tp := range stmt.TypeParams {
		underlyingType = strings.ReplaceAll(underlyingType, tp.Name, "void")
	}

	cType := tg.convertTypeAliasToCType(underlyingType)

	code.WriteString(fmt.Sprintf("typedef %s %s;\n\n", cType, stmt.Name))

	return code.String()
}
