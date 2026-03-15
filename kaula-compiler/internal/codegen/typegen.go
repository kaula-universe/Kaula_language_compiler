package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
)

// TypeGenerator 负责类型相关的代码生成
type TypeGenerator struct {
	codegen *CodeGenerator
}

// NewTypeGenerator 创建一个新的类型生成器
func NewTypeGenerator(cg *CodeGenerator) *TypeGenerator {
	return &TypeGenerator{
		codegen: cg,
	}
}

// GenerateClassStatement 生成类定义代码
func (tg *TypeGenerator) GenerateClassStatement(stmt *ast.ClassStatement) string {
	code := fmt.Sprintf("// Class: %s\n", stmt.Name)
	
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
	
	// 生成结构体定义
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		fieldType := tg.convertType(field.Type, field.Nullable)
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
