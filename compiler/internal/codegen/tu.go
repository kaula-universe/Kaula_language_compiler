package codegen

import (
	"strings"

	"kaula/internal/ast"
)

// genericPrototype 为"在别处定义"的泛型实例生成 C 原型。
//
// TU 拆分后，实例化调用点所在的 TU 必须看到原型（C99 起没有隐式声明），
// 而函数体只能由归属 TU 提供。原型必须与归属 TU 里的定义逐项一致，
// 否则 clang 会以 "conflicting types" 报错 —— 所以这里复用定义端同一套
// 类型映射（typeGenerator.convertType + formatCVarDecl），而不是另起一套规则。
//
// 注意：泛型实例化后的函数不再是泛型（instantiateGenericFunction 已把 T
// 替换为具体类型），因此无需考虑类型参数；嵌套函数捕获参数恒为空
// （泛型函数体内定义的嵌套 fn 属于另一层 lambda/嵌套函数生成）。
func genericPrototype(cName string, instFunc *ast.FunctionStatement, tg *TypeGenerator) string {
	if instFunc == nil || tg == nil {
		return ""
	}
	returnType := "void"
	if instFunc.ReturnType != "" && instFunc.ReturnType != "void" {
		// 定义端用的是 tgReturnTypeToC（不是 convertType）：class/struct 作为
		// 返回值时是值语义（K_X），参数才是指针。两者混用会让原型与定义
		// 出现 "conflicting types"。
		if mapped := tgReturnTypeToC(tg, instFunc.ReturnType); mapped != "" {
			returnType = mapped
		}
	}
	var b strings.Builder
	b.WriteString(returnType)
	b.WriteByte(' ')
	b.WriteString(cName)
	b.WriteByte('(')
	for i, param := range instFunc.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		paramType := "int64_t"
		if i < len(instFunc.ParamTypes) && instFunc.ParamTypes[i] != "" {
			isNullable := false
			if i < len(instFunc.ParamNullables) {
				isNullable = instFunc.ParamNullables[i]
			}
			if ctype := tg.convertType(instFunc.ParamTypes[i], isNullable); ctype != "" {
				paramType = ctype
			}
		}
		b.WriteString(formatCVarDecl(paramType, param))
	}
	if len(instFunc.Params) == 0 {
		b.WriteString("void")
	}
	b.WriteString(");\n")
	return b.String()
}
