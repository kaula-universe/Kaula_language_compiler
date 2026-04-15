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
}

// NewTypeGenerator 创建一个新的类型生成器
func NewTypeGenerator(cg *CodeGenerator) *TypeGenerator {
	return &TypeGenerator{
		codegen: cg,
		typeErasure: make(map[string]string),
	}
}

// eraseGenericType 执行类型擦除，将泛型类型转换为 void*
func (tg *TypeGenerator) eraseGenericType(typeName string) string {
	// 检查是否是泛型类型参数
	if strings.Contains(typeName, "<") {
		return "void*"
	}
	
	// 检查缓存
	if erased, ok := tg.typeErasure[typeName]; ok {
		return erased
	}
	
	// 基本类型保持不变
	switch typeName {
	case "int", "float", "double", "bool", "char", "string":
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
	code := fmt.Sprintf("// Struct: %s\n", stmt.Name)
	
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
	code := fmt.Sprintf("%s* %s_new(", className, className)
	for i, param := range constructor.Params {
		paramType := tg.convertType(param.Type, param.Nullable)
		if i > 0 {
			code += ", "
		}
		code += fmt.Sprintf("%s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	code += tg.codegen.indentString() + fmt.Sprintf("%s* self = malloc(sizeof(%s));\n", className, className)
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
	
	code += tg.codegen.indentString() + fmt.Sprintf("%s* self = malloc(sizeof(%s));\n", className, className)
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
	
	code := fmt.Sprintf("%s %s_%s(%s* self", returnType, className, method.Name, className)
	for _, param := range method.Params {
		paramType := tg.convertType(param.Type, false)
		code += fmt.Sprintf(", %s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	// 生成方法体
	for _, bodyStmt := range method.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	code += tg.codegen.indentString() + "return NULL;\n"
	code += "}\n\n"
	
	return code
}

// GenerateGenericMethodStatement 生成泛型方法代码（使用类型擦除）
func (tg *TypeGenerator) GenerateGenericMethodStatement(className string, method *ast.MethodStatement) string {
	// 对返回值使用类型擦除
	returnType := tg.eraseGenericType(method.ReturnType)
	
	code := fmt.Sprintf("%s %s_%s(%s* self", returnType, className, method.Name, className)
	for _, param := range method.Params {
		// 对泛型参数使用类型擦除
		paramType := tg.eraseGenericType(param.Type)
		code += fmt.Sprintf(", %s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	// 生成方法体
	for _, bodyStmt := range method.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	code += tg.codegen.indentString() + "return NULL;\n"
	code += "}\n\n"
	
	return code
}

// convertType 转换 Kaula 类型到 C 类型
func (tg *TypeGenerator) convertType(kaulaType string, nullable bool) string {
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
