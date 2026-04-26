package codegen

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"strings"
)

// TypeGenerator 负责类型相关的代码生成
type TypeGenerator struct {
	codegen *CodeGenerator
	typeErasure map[string]string
	clibTypeMap map[string]string
}

func NewTypeGenerator(cg *CodeGenerator) *TypeGenerator {
	return &TypeGenerator{
		codegen: cg,
		typeErasure: make(map[string]string),
		clibTypeMap: make(map[string]string),
	}
}

func (tg *TypeGenerator) RegisterClibType(kaulaType string, cType string) {
	tg.clibTypeMap[kaulaType] = cType
}

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
	code := fmt.Sprintf("// Class: %s\n", stmt.Name)
	
	if stmt.Generic {
		return tg.GenerateGenericClassStatement(stmt)
	}
	
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		fieldType := tg.convertType(field.Type, field.Nullable)
		code += fmt.Sprintf("    %s %s;\n", fieldType, field.Name)
	}
	code += fmt.Sprintf("} %s;\n\n", stmt.Name)
	
	for _, constructor := range stmt.Constructors {
		code += tg.GenerateConstructorStatement(stmt.Name, constructor)
	}
	
	for _, method := range stmt.Methods {
		code += tg.GenerateMethodStatement(stmt.Name, method)
	}
	
	return code
}

func (tg *TypeGenerator) GenerateGenericClassStatement(stmt *ast.ClassStatement) string {
	code := fmt.Sprintf("// Generic Class: %s", stmt.Name)
	
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
	
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		fieldType := tg.eraseGenericType(field.Type)
		if field.Nullable {
			fieldType += "*"
		}
		code += fmt.Sprintf("    %s %s;\n", fieldType, field.Name)
	}
	code += fmt.Sprintf("} %s;\n\n", stmt.Name)
	
	for _, constructor := range stmt.Constructors {
		code += tg.GenerateGenericConstructorStatement(stmt.Name, constructor)
	}
	
	return code
}

func (tg *TypeGenerator) GenerateInterfaceStatement(stmt *ast.InterfaceStatement) string {
	code := fmt.Sprintf("// Interface: %s\n", stmt.Name)
	
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

func (tg *TypeGenerator) GenerateStructStatement(stmt *ast.StructStatement) string {
	code := fmt.Sprintf("// Struct: %s (Generic=%v, TypeParams=%d)\n", stmt.Name, stmt.Generic, len(stmt.TypeParams))
	
	if stmt.Generic {
		return tg.GenerateGenericStructStatement(stmt)
	}
	
	code += fmt.Sprintf("typedef struct %s {\n", stmt.Name)
	for _, field := range stmt.Fields {
		fieldType := tg.convertType(field.Type, field.Nullable)
		code += fmt.Sprintf("    %s %s;\n", fieldType, field.Name)
	}
	code += fmt.Sprintf("} %s;\n\n", stmt.Name)
	
	return code
}

func (tg *TypeGenerator) GenerateGenericStructStatement(stmt *ast.StructStatement) string {
	code := fmt.Sprintf("// Generic Struct: %s", stmt.Name)
	
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
	
	code += tg.codegen.indentString() + fmt.Sprintf("%s* self = KMM_V4_ALLOC_ZERO(%s);\n", className, className)
	code += tg.codegen.indentString() + "if (self == NULL) { return NULL; }\n\n"
	
	for _, bodyStmt := range constructor.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	code += tg.codegen.indentString() + "return self;\n"
	code += "}\n\n"
	
	return code
}

func (tg *TypeGenerator) GenerateGenericConstructorStatement(className string, constructor *ast.ConstructorStatement) string {
	code := fmt.Sprintf("%s* %s_new(", className, className)
	for i, param := range constructor.Params {
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
	
	code += tg.codegen.indentString() + fmt.Sprintf("%s* self = KMM_V4_ALLOC_ZERO(%s);\n", className, className)
	code += tg.codegen.indentString() + "if (self == NULL) { return NULL; }\n\n"
	
	for _, bodyStmt := range constructor.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	code += tg.codegen.indentString() + "return self;\n"
	code += "}\n\n"
	
	return code
}

func (tg *TypeGenerator) GenerateMethodStatement(className string, method *ast.MethodStatement) string {
	returnType := tg.convertType(method.ReturnType, false)
	
	code := fmt.Sprintf("static inline %s %s_%s(%s* self", returnType, className, method.Name, className)
	for _, param := range method.Params {
		paramType := tg.convertType(param.Type, false)
		code += fmt.Sprintf(", %s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	for _, bodyStmt := range method.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	if returnType != "void" && !methodHasReturn(method.Body) {
		code += tg.codegen.indentString() + "return NULL;\n"
	}
	code += "}\n\n"
	
	return code
}

func (tg *TypeGenerator) GenerateGenericMethodStatement(className string, method *ast.MethodStatement) string {
	returnType := tg.eraseGenericType(method.ReturnType)
	
	code := fmt.Sprintf("static inline %s %s_%s(%s* self", returnType, className, method.Name, className)
	for _, param := range method.Params {
		paramType := tg.eraseGenericType(param.Type)
		code += fmt.Sprintf(", %s %s", paramType, param.Name)
	}
	code += ") {\n"
	
	for _, bodyStmt := range method.Body {
		code += tg.codegen.indentString() + tg.codegen.generateStatement(bodyStmt)
	}
	
	if returnType != "void" && !methodHasReturn(method.Body) {
		code += tg.codegen.indentString() + "return NULL;\n"
	}
	code += "}\n\n"
	
	return code
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
