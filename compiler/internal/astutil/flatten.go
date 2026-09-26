package astutil

import (
	"reflect"

	"kaula/internal/ast"
)

// statementElemType 反射判定 []ast.Statement 用
var statementElemType = reflect.TypeOf((*ast.Statement)(nil)).Elem()

// FlattenDeclarations 将单条语句内的多变量声明拆分为多条独立声明语句。
//
// 语法：`i64 a = 1, b = 2, c` —— 解析阶段由 VariableDeclaration.More 承载
// 后续声明符，语义分析/代码生成前调用本函数就地展平，使其与逐条书写的
// `i64 a = 1` / `i64 b = 2` / `i64 c` 完全等价，从而复用既有的单变量声明
// 处理链路（符号表、SOR、活跃期分析、C 代码生成等）。
//
// 遍历整棵语法树（含 match 分支体、lambda 体等嵌在表达式里的语句块），
// 就地改写所有语句切片；重复调用幂等（展平后 More 已清空）。
func FlattenDeclarations(program *ast.Program) {
	if program == nil {
		return
	}
	flattenValue(reflect.ValueOf(program), make(map[uintptr]bool))
}

// flattenValue 深入任意 AST 字段，遇到 []ast.Statement 时做声明拆分
func flattenValue(v reflect.Value, seen map[uintptr]bool) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		flattenValue(v.Elem(), seen)
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		// AST 无父指针，正常不会成环；seen 兜底防止别名/异常引用导致死循环
		key := v.Pointer()
		if seen[key] {
			return
		}
		seen[key] = true
		flattenValue(v.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" {
				// 未导出字段不可写，跳过（与 walkValue 一致）
				continue
			}
			flattenValue(v.Field(i), seen)
		}
	case reflect.Slice:
		if v.Type().Elem() == statementElemType {
			flattenStatementSlice(v, seen)
			return
		}
		for i := 0; i < v.Len(); i++ {
			flattenValue(v.Index(i), seen)
		}
	}
}

// flattenStatementSlice 展开语句切片里的多变量声明，并继续下钻子节点
func flattenStatementSlice(v reflect.Value, seen map[uintptr]bool) {
	if v.Len() == 0 {
		return
	}
	stmts, ok := v.Interface().([]ast.Statement)
	if !ok {
		return
	}
	out := make([]ast.Statement, 0, len(stmts))
	expanded := false
	for _, stmt := range stmts {
		vd, isVarDecl := stmt.(*ast.VariableDeclaration)
		if !isVarDecl || len(vd.More) == 0 {
			flattenValue(reflect.ValueOf(stmt), seen)
			out = append(out, stmt)
			continue
		}
		// 逗号分隔的多变量声明：拆成独立语句，逐个下钻
		for _, decl := range vd.AllDeclarators() {
			decl.More = nil
			flattenValue(reflect.ValueOf(decl), seen)
			out = append(out, decl)
		}
		expanded = true
	}
	if !expanded || !v.CanSet() {
		return
	}
	v.Set(reflect.ValueOf(out))
}
