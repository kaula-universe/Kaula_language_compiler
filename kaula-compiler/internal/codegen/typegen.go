package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

// TypeGenerator 负责类型相关的代码生成
type TypeGenerator struct {
	codegen *CodeGenerator
	typeErasure map[string]string // 类型擦除映射
	clibTypeMap map[string]string // C 库类型映射（通用）
}

// NewTypeGenerator 创建一个新的类型生成器
func NewTypeGenerator(cg *CodeGenerator) *TypeGenerator {
	return &TypeGenerator{
		codegen: cg,
		typeErasure: make(map[string]string),
		clibTypeMap: make(map[string]string),
	}
}

// RegisterClibType 注册 C 库类型映射（通用接口）
// 例如：nk_context -> struct nk_context*, nk_rect -> struct nk_rect
func (tg *TypeGenerator) RegisterClibType(kaulaType string, cType string) {
	tg.clibTypeMap[kaulaType] = cType
}

// GenerateCLibHeaders 生成 C 库头文件包含（通用）
func (tg *TypeGenerator) GenerateCLibHeaders(headers []string) string {
	var code string
	for _, h := range headers {
		code += fmt.Sprintf("#include %s\n", h)
	}
	return code
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

// GenerateClibWrappers 生成通用 C 库包装函数（零成本内联）
// 自动适配类型擦除，支持泛型参数
func (tg *TypeGenerator) GenerateClibWrappers(config *CLibConfig) string {
	if config == nil || config.Functions == nil {
		return ""
	}
	
	var code strings.Builder
	code.WriteString("// ============================================\n")
	code.WriteString("// 自动生成的 C 库包装函数 (零成本适配层)\n")
	code.WriteString("// ============================================\n\n")
	
	for funcName, sig := range config.Functions {
		// 生成内联包装函数
		code.WriteString(fmt.Sprintf("static inline %s kaula_%s_wrapped(", sig.Return, funcName))
		
		for i, arg := range sig.Args {
			erasedType := tg.eraseGenericType(arg)
			if i > 0 { code.WriteString(", ") }
			code.WriteString(fmt.Sprintf("%s arg%d", erasedType, i))
		}
		code.WriteString(") {\n")
		
		code.WriteString(fmt.Sprintf("    return %s(", funcName))
		for i := range sig.Args {
			if i > 0 { code.WriteString(", ") }
			code.WriteString(fmt.Sprintf("arg%d", i))
		}
		code.WriteString(");\n}\n\n")
	}
	
	return code.String()
}

// eraseGenericType 执行类型擦除，将泛型类型转换为 void*
func (tg *TypeGenerator) eraseGenericType(typeName string) string {
	// 检查是否是泛型类型参数（单个大写字母如 T, U, V 等）
	if len(typeName) == 1 && typeName[0] >= 'A' && typeName[0] <= 'Z' {
		return "void*"
	}
	
	// 检查是否是泛型类型（如 Box<T>）
	if strings.Contains(typeName, "<") {
		return "void*"
	}
	
	// 检查缓存
	if erased, ok := tg.typeErasure[typeName]; ok {
		return erased
	}
	
	// 基本类型保持不变
	switch typeName {
	case "int", "float", "double", "bool", "char", "string", "i32", "i64", "f32", "f64":
		tg.typeErasure[typeName] = typeName
		return typeName
	}
	
	// 其他类型视为指针
	erased := typeName + "*"
	tg.typeErasure[typeName] = erased
	return erased
}

// substituteType 替换泛型类型为具体类型
func (tg *TypeGenerator) substituteType(typeName string, typeMap map[string]string) string {
	if typeMap == nil {
		return typeName
	}
	
	// 检查是否有替换
	if substituted, ok := typeMap[typeName]; ok {
		return substituted
	}
	
	return typeName
}

// GenerateClassStatement 生成类定义代码
func (tg *TypeGenerator) GenerateClassStatement(stmt *ast.ClassStatement) string {
	code := fmt.Sprintf("// Class: %s\n", stmt.Name)
	
	// 处理泛型类
	if stmt.Generic {
		return tg.GenerateGenericClassStatement(stmt)
	}
	
	// 生成结构体定义
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		fieldType := tg.convertType(field.Type, field.Nullable)
		code += fmt.Sprintf("    %s %s;\n", fieldType, field.Name)
	}
	code += fmt.Sprintf("} %s;\n\n", stmt.Name)
	
	// 生成构造函数
	for _, constructor := range stmt.Constructors {
		code += tg.GenerateConstructorStatement(stmt.Name, constructor)
	}
	
	// 生成方法
	for _, method := range stmt.Methods {
		code += tg.GenerateMethodStatement(stmt.Name, method)
	}
	
	return code
}

// GenerateGenericClassStatement 生成泛型类定义代码（使用类型擦除）
func (tg *TypeGenerator) GenerateGenericClassStatement(stmt *ast.ClassStatement) string {
	code := fmt.Sprintf("// Generic Class: %s", stmt.Name)
	
	// 添加泛型参数注释
	if len(stmt.TypeParams) > 0 {
		code += fmt.Sprintf("<")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				code += ", "
			}
			code += tp.Name
		}
		code += fmt.Sprintf(">\n")
	} else {
		code += "\n"
	}
	
	// 使用类型擦除生成结构体定义
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		// 对泛型字段使用类型擦除
		fieldType := tg.eraseGenericType(field.Type)
		if field.Nullable {
			fieldType += "*"
		}
		code += fmt.Sprintf("    %s %s;\n", fieldType, field.Name)
	}
	code += fmt.Sprintf("} %s;\n\n", stmt.Name)
	
	// 生成泛型构造函数（使用类型擦除）
	for _, constructor := range stmt.Constructors {
		code += tg.GenerateGenericConstructorStatement(stmt.Name, constructor)
	}
	
	return code
}

// GenerateInterfaceStatement 生成接口定义代码
func (tg *TypeGenerator) GenerateInterfaceStatement(stmt *ast.InterfaceStatement) string {
	code := fmt.Sprintf("// Interface: %s\n", stmt.Name)
	
	// 生成函数指针结构体
	code += fmt.Sprintf("typedef struct %s_VTable {\n", stmt.Name)
	for _, method := range stmt.Methods {
		returnType := tg.convertType(method.ReturnType, false)
		code += fmt.Sprintf("    %s (*%s)(void* self", returnType, method.Name)
		for _, param := range method.Params {
			paramType := tg.convertType(param.Type, false)
			code += fmt.Sprintf(", %s %s", paramType, param.Name)
		}
		code += ");\n"
	}
	code += fmt.Sprintf("} %s_VTable;\n\n", stmt.Name)
	
	return code
}

// GenerateStructStatement 生成结构体定义代码
func (tg *TypeGenerator) GenerateStructStatement(stmt *ast.StructStatement) string {
	code := fmt.Sprintf("// Struct: %s (Generic=%v, TypeParams=%d)\n", stmt.Name, stmt.Generic, len(stmt.TypeParams))
	
	// 处理泛型结构体
	if stmt.Generic {
		return tg.GenerateGenericStructStatement(stmt)
	}
	
	// 生成结构体定义
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		fieldType := tg.convertType(field.Type, field.Nullable)
		code += fmt.Sprintf("    %s %s;\n", fieldType, field.Name)
	}
	code += fmt.Sprintf("} %s;\n\n", stmt.Name)
	
	return code
}

// GenerateGenericStructStatement 生成泛型结构体定义代码
func (tg *TypeGenerator) GenerateGenericStructStatement(stmt *ast.StructStatement) string {
	code := fmt.Sprintf("// Generic Struct: %s", stmt.Name)
	
	// 添加泛型参数注释
	if len(stmt.TypeParams) > 0 {
		code += fmt.Sprintf("<")
		for i, tp := range stmt.TypeParams {
			if i > 0 {
				code += ", "
			}
			code += tp.Name
		}
		code += fmt.Sprintf(">\n")
	} else {
		code += "\n"
	}
	
	// 使用类型擦除生成结构体定义
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		fieldType := tg.eraseGenericType(field.Type)
		if field.Nullable {
			fieldType += "*"
		}
		code += fmt.Sprintf("    %s %s;\n", fieldType, field.Name)
	}
	code += fmt.Sprintf("} %s;\n\n", stmt.Name)
	
	return code
}

// GenerateConstructorStatement 生成构造函数代码
func (tg *TypeGenerator) GenerateConstructorStatement(className string, constructor *ast.ConstructorStatement) string {
	// 构造函数不内联，保持为普通函数
	code := fmt.Sprintf("%s* %s_new(", className, className)
	for i, param := range constructor.Params {
		paramType := tg.convertType(param.Type, param.Nullable)
		if i > 0 {
			code += ", "
		}
		code += fmt.Sprintf("%s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	code += tg.codegen.indentString() + fmt.Sprintf("%s* self = KMM_V4_ALLOC_ZERO(%s);  // KMM Enhanced V4: 自动零初始化分配（类型安全）\n", className, className)
	code += tg.codegen.indentString() + "if (self == NULL) { return NULL; }\n\n"
	
	// 生成构造函数体
	for _, bodyStmt := range constructor.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	code += tg.codegen.indentString() + "return self;\n"
	code += "}\n\n"
	
	return code
}

// GenerateGenericConstructorStatement 生成泛型构造函数代码（使用类型擦除）
func (tg *TypeGenerator) GenerateGenericConstructorStatement(className string, constructor *ast.ConstructorStatement) string {
	// 泛型构造函数不内联，保持为普通函数
	code := fmt.Sprintf("%s* %s_new(", className, className)
	for i, param := range constructor.Params {
		// 对泛型参数使用类型擦除
		paramType := tg.eraseGenericType(param.Type)
		if param.Nullable {
			paramType += "*"
		}
		if i > 0 {
			code += ", "
		}
		code += fmt.Sprintf("%s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	code += tg.codegen.indentString() + fmt.Sprintf("%s* self = KMM_V4_ALLOC_ZERO(%s);  // KMM Enhanced V4: 自动零初始化分配（类型安全）\n", className, className)
	code += tg.codegen.indentString() + "if (self == NULL) { return NULL; }\n\n"
	
	// 生成构造函数体
	for _, bodyStmt := range constructor.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	code += tg.codegen.indentString() + "return self;\n"
	code += "}\n\n"
	
	return code
}

// GenerateMethodStatement 生成方法代码
func (tg *TypeGenerator) GenerateMethodStatement(className string, method *ast.MethodStatement) string {
	returnType := tg.convertType(method.ReturnType, false)
	
	// 使用 static inline 提示编译器进行内联优化
	code := fmt.Sprintf("static inline %s %s_%s(%s* self", returnType, className, method.Name, className)
	for _, param := range method.Params {
		paramType := tg.convertType(param.Type, false)
		code += fmt.Sprintf(", %s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	// 生成方法体
	for _, bodyStmt := range method.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	// 仅当方法体中没有 return 语句且返回值类型不是 void 时才添加默认返回
	if returnType != "void" && !methodHasReturn(method.Body) {
		code += tg.codegen.indentString() + "return NULL;\n"
	}
	code += "}\n\n"
	
	return code
}

// GenerateGenericMethodStatement 生成泛型方法代码（使用类型擦除）
func (tg *TypeGenerator) GenerateGenericMethodStatement(className string, method *ast.MethodStatement) string {
	returnType := tg.eraseGenericType(method.ReturnType)
	
	// 使用 static inline 提示编译器进行内联优化
	code := fmt.Sprintf("static inline %s %s_%s(%s* self", returnType, className, method.Name, className)
	for _, param := range method.Params {
		paramType := tg.eraseGenericType(param.Type)
		code += fmt.Sprintf(", %s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	// 生成方法体
	for _, bodyStmt := range method.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	if returnType != "void" && !methodHasReturn(method.Body) {
		code += tg.codegen.indentString() + "return NULL;\n"
	}
	code += "}\n\n"
	
	return code
}

// methodHasReturn 检查方法体是否已包含 return 语句
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

// convertType 转换 Kaula 类型到 C 类型（支持 C 库类型映射）
func (tg *TypeGenerator) convertType(kaulaType string, nullable bool) string {
	// 1. 检查 C 库类型映射（最高优先级）
	if cType, ok := tg.clibTypeMap[kaulaType]; ok {
		if nullable && !strings.HasSuffix(cType, "*") {
			cType += "*"
		}
		return cType
	}
	
	var cType string
	
	switch kaulaType {
	case "string":
		cType = "char*"
	default:
		cType = kaulaType
	}
	
	if nullable && cType != "char*" {
		cType += "*"
	}
	
	return cType
}
