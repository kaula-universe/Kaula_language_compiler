package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

// TypeGenerator 负责类型相关的代码生成
type TypeGenerator struct {
	codegen     *CodeGenerator
	clibTypeMap map[string]string
	structTypes map[string]bool

	// activeTypeMap 当前活跃的类型参数映射（泛型单态化期间设置）。
	// T -> 具体类型（Kaula 类型名，如 "int"）
	// 在 MapKaulaTypeToC 中查表替换，使函数体内的类型参数引用被替换为具体类型。
	activeTypeMap map[string]string
}

func NewTypeGenerator(cg *CodeGenerator) *TypeGenerator {
	return &TypeGenerator{
		codegen: cg,
		clibTypeMap: map[string]string{
			"File": "FILE*",
		},
		structTypes:   make(map[string]bool),
		activeTypeMap: nil,
	}
}

// PushActiveTypeMap 设置当前活跃的类型参数映射，返回之前的映射（用于恢复）。
// 用于泛型单态化：在生成实例化函数体前设置，生成后恢复。
func (tg *TypeGenerator) PushActiveTypeMap(m map[string]string) map[string]string {
	old := tg.activeTypeMap
	tg.activeTypeMap = m
	return old
}

// PopActiveTypeMap 恢复之前的类型参数映射。
func (tg *TypeGenerator) PopActiveTypeMap(old map[string]string) {
	tg.activeTypeMap = old
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
	"bool":       "bool",
	"boolean":    "bool",
	"char":       "char",
	"byte":       "uint8_t",
	"sbyte":      "int8_t",
	"void":       "void",
	"string":     "String",
	"cstring":    "const char*",
	"cint":       "int",
	"str":        "String",
	"intptr":     "intptr_t",
	"uintptr":    "uintptr_t",
	"size":       "size_t",
	"ssize":      "ssize_t",
	"object":     "Object*",
}

// splitTopLevelCommas 在顶层（不计嵌套括号/尖括号/方括号）按逗号分割字符串。
// 用于解析 void(T1,T2,...)R 中的参数列表，避免误分割 void(void(i32)i32, i32) 这类嵌套签名。
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '<', '[':
			depth++
		case ')', '>', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseVoidSignatureType 解析 void(T...)R 类型记法并映射到 C 类型。
//
// 记法规则（无歧义）：
//
//	void()        → void*                  完全不透明数据指针
//	void(T)       → void*                  幻影类型化数据指针（T 仅类型系统跟踪，运行时即 void*）
//	void(T1,T2)R  → R (*)(T1, T2)          带签名函数指针（R 必须显式）
//	void(...)void → void (*)(...)          无返回值函数指针（显式写 void 返回类型）
//
// 判定：')' 后有返回类型 → 函数指针；无 → 数据指针。
// 运行时零开销：数据指针即 void*，函数指针即 C 函数指针，与 C ABI 一一对应。
func (tg *TypeGenerator) parseVoidSignatureType(kaulaType string) (string, bool) {
	if !strings.HasPrefix(kaulaType, "void(") {
		return "", false
	}
	// 定位匹配 void( 的右括号（处理嵌套 void(...) 与泛型 <...> 中的括号）
	depth := 1
	i := 5 // 跳过 "void("
	for i < len(kaulaType) {
		switch kaulaType[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				goto foundClose
			}
		}
		i++
	}
	return "", false // 括号不匹配，交回主路径处理

foundClose:
	argsStr := kaulaType[5:i]
	retStr := strings.TrimSpace(kaulaType[i+1:])

	// 数据指针：')' 后无返回类型 → void*
	if retStr == "" {
		return "void*", true
	}

	// 函数指针：')' 后有返回类型 R → R (*)(args...)
	var cArgs []string
	if argsStr != "" {
		for _, part := range splitTopLevelCommas(argsStr) {
			cArgs = append(cArgs, tg.MapKaulaTypeToC(strings.TrimSpace(part)))
		}
	}
	if len(cArgs) == 0 {
		cArgs = []string{"void"}
	}
	retC := tg.MapKaulaTypeToC(retStr)
	return fmt.Sprintf("%s (*)(%s)", retC, strings.Join(cArgs, ", ")), true
}

// tryInstantiateGenericType 解析泛型类型引用并触发实例化。
// baseName 是类型名（如 Box），argsStr 是尖括号内的参数列表（如 "int" 或 "int, string"）。
// args 中的类型参数先经 activeTypeMap 替换为具体类型（如 Box<T> 中 T→int），
// 再调用 InstantiateGenericType 生成实例化 C 类型定义，返回 C 类型名（如 K_Box_int）。
func (tg *TypeGenerator) tryInstantiateGenericType(baseName, argsStr string) (string, bool) {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		return "", false
	}

	var typeArgs []string
	for _, part := range splitTopLevelCommas(argsStr) {
		arg := strings.TrimSpace(part)
		if arg == "" {
			continue
		}
		// 应用 activeTypeMap 替换类型参数（如 T → int），
		// 使 Box<T> 在 activeTypeMap{T:int} 下实例化为 Box<int>。
		if tg.activeTypeMap != nil {
			if substituted, ok := tg.activeTypeMap[arg]; ok && substituted != "" {
				arg = substituted
			}
		}
		typeArgs = append(typeArgs, arg)
	}

	if len(typeArgs) == 0 {
		return "", false
	}

	cName, err := tg.codegen.InstantiateGenericType(baseName, typeArgs, 0)
	if err != nil {
		return "", false
	}
	return cName, true
}

func (tg *TypeGenerator) MapKaulaTypeToC(kaulaType string) string {
	// void(T...)R 签名记法优先处理（必须保留原始大小写，参数/返回类型可能含用户定义类型）
	if cType, ok := tg.parseVoidSignatureType(kaulaType); ok {
		return cType
	}

	// 泛型类型实例化：Name<args> → 实例化为具体 C 类型（如 Box<int> → K_Box_int）。
	// 必须在 activeTypeMap 之前处理，因为 args 中的类型参数（如 Box<T>）需在此处替换。
	// 仅匹配 "Name<...>" 形态（以 > 结尾），避免误匹配比较运算符残留。
	if lt := strings.Index(kaulaType, "<"); lt > 0 && strings.HasSuffix(kaulaType, ">") {
		if cType, ok := tg.tryInstantiateGenericType(kaulaType[:lt], kaulaType[lt+1:len(kaulaType)-1]); ok {
			return cType
		}
	}

	// 泛型单态化：若当前活跃类型映射中存在该类型参数，替换为具体类型后递归映射。
	// 这样函数体内的 T、[]T、*T 等引用都能被替换为具体 C 类型。
	if tg.activeTypeMap != nil {
		if substituted, ok := tg.activeTypeMap[kaulaType]; ok && substituted != "" && substituted != kaulaType {
			return tg.MapKaulaTypeToC(substituted)
		}
	}

	typeLower := strings.ToLower(kaulaType)
	if cType, ok := globalTypeMap[typeLower]; ok {
		return cType
	}
	// 处理固定大小数组类型 [N]type → 转换为 C 的 "elemType[N]" 格式
	// 注意：[N] 中 N 必须非空，否则是动态数组 []type → 指针（见下方 [] 分支）
	if len(typeLower) > 0 && typeLower[0] == '[' {
		closeBracket := strings.Index(typeLower, "]")
		if closeBracket > 1 {
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
		// 对于 struct 类型，内部递归 tg.MapKaulaTypeToC 会添加 struct 前缀
		return tg.MapKaulaTypeToC(kaulaType[:len(kaulaType)-1]) + "*"
	}
	if strings.HasPrefix(typeLower, "const ") {
		innerType := kaulaType[6:] // 保留原始大小写，用户定义类型可能含大写
		if innerType == "string" || innerType == "str" {
			return "String"
		}
		if cType, ok := globalTypeMap[strings.ToLower(innerType)]; ok {
			return "const " + cType
		}
		// 递归映射内部类型，支持 const void() → const void* 等复合类型
		return "const " + tg.MapKaulaTypeToC(innerType)
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

// GenerateGenericClassStatement 泛型类定义：仅生成注释占位。
// 实际 C 类型定义在实例化点生成（如 Box<int> 使用时触发 InstantiateGenericType），
// 生成 K_Box_int typedef 及对应的构造函数/方法。避免类型擦除为 void*。
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
		code.WriteString(">")
	}
	code.WriteString(" (instantiated on use)\n")
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
		if len(stmt.Variants) == 0 {
			// C 标准不允许空 enum, 生成占位成员
			code.WriteString(fmt.Sprintf("    %s_Kind__empty\n", stmt.Name))
		}
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
	if len(stmt.Variants) == 0 {
		// C 标准不允许空 enum, 生成占位成员
		code.WriteString(fmt.Sprintf("    %s_Kind__empty\n", stmt.Name))
	}
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

// GenerateGenericEnumStatement 泛型枚举定义：仅生成注释占位。
// 实际 C 类型定义在实例化点生成（如 Result<int, string> 使用时触发 InstantiateGenericType），
// 生成 K_Result_int_string tagged union，变体字段类型按类型参数替换。
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
	code.WriteString(" (instantiated on use)\n")
	return code.String()
}

// GenerateGenericStructStatement 泛型结构体定义：仅生成注释占位。
// 实际 C 类型定义在实例化点生成（如 Pair<int, string> 使用时触发 InstantiateGenericType），
// 生成 K_Pair_int_string typedef，字段类型按类型参数替换。
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
		code.WriteString(">")
	}
	code.WriteString(" (instantiated on use)\n")
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

// tgReturnTypeToC 函数返回类型 → C 类型（与函数定义生成保持一致）
func tgReturnTypeToC(tg *TypeGenerator, kaulaType string) string {
	switch kaulaType {
	case "":
		return "void"
	case "int":
		return "int"
	case "i64":
		return "int64_t"
	case "u64":
		return "uint64_t"
	case "i32":
		return "int32_t"
	case "u32":
		return "uint32_t"
	case "i16":
		return "int16_t"
	case "u16":
		return "uint16_t"
	case "i8":
		return "int8_t"
	case "u8":
		return "uint8_t"
	case "float":
		return "float"
	case "f32":
		return "float"
	case "double":
		return "double"
	case "f64":
		return "double"
	case "bool":
		return "bool"
	case "char":
		return "char"
	case "void":
		return "void"
	case "string":
		return "String"
	default:
		return tg.convertType(kaulaType, false)
	}
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

	// void(T...)R 签名记法：无论数据指针还是函数指针，均为指针大小
	if strings.HasPrefix(kaulaType, "void(") {
		return 8, true
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

	// void(...)R 函数指针 typedef 需 C 特殊语法：R (*Name)(args)
	// parseVoidSignatureType 产出形如 "R (*)(args)"，须把 Name 插入 (*Name) 位置
	if marker := strings.Index(cType, " (*)("); marker > 0 {
		retType := cType[:marker]
		argsPart := cType[marker+len(" (*)("):] // "args)"
		args := strings.TrimSuffix(argsPart, ")")
		code.WriteString(fmt.Sprintf("typedef %s (*%s)(%s);\n\n", retType, stmt.Name, args))
	} else {
		code.WriteString(fmt.Sprintf("typedef %s %s;\n\n", cType, stmt.Name))
	}

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

	// void(T...)R 签名记法委托给主映射器
	if strings.HasPrefix(cType, "void(") {
		return tg.MapKaulaTypeToC(cType)
	}

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
