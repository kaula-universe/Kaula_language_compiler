// Package astutil 提供跨解析器/语义/代码生成复用的 AST 工具。
package astutil

import (
	"reflect"

	"kaula-compiler/internal/ast"
)

// CollectedRefs 收集结果：
//
//	Refs:    所有被引用的标识符名（含嵌套函数/闭包体内的引用——调用方自行排除）
//	Locals:  各作用域声明的局部变量名（VariableDeclaration/循环变量/嵌套函数参数），
//	         用于遮蔽判定（同名引用不回再次捕获）
type CollectedRefs struct {
	Refs   map[string]bool
	Locals map[string]bool
}

// CollectIdentifiers 遍历语句集合，收集标识符引用与局部声明名。
// 嵌套函数（FunctionStatement）与 lambda（LambdaExpression）的参数并入 Locals，
// 其函数体不再深入（由调用方在各自上下文中独立收集）。
func CollectIdentifiers(stmts []ast.Statement) *CollectedRefs {
	c := &CollectedRefs{
		Refs:   make(map[string]bool),
		Locals: make(map[string]bool),
	}
	for _, s := range stmts {
		walkStatement(s, c)
	}
	return c
}

// CollectIdentifiersInExpr 遍历表达式（如 nonlocal 的初值）收集引用。
func CollectIdentifiersInExpr(e ast.Expression) map[string]bool {
	c := &CollectedRefs{
		Refs:   make(map[string]bool),
		Locals: make(map[string]bool),
	}
	walkValue(reflect.ValueOf(e), c)
	return c.Refs
}

func walkStatement(s ast.Statement, c *CollectedRefs) {
	if s == nil {
		return
	}
	switch fn := s.(type) {
	case *ast.FunctionStatement:
		for _, p := range fn.Params {
			c.Locals[p] = true
		}
		return
	case *ast.NonLocalStatement:
		// nonlocal 是绑定声明：名字本身即引用（强制捕获）
		c.Refs[fn.Name] = true
		if fn.Value != nil {
			walkValue(reflect.ValueOf(fn.Value), c)
		}
		return
	}
	walkValue(reflect.ValueOf(s), c)
}

func walkValue(v reflect.Value, c *CollectedRefs) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		switch fn := v.Interface().(type) {
		case *ast.FunctionStatement:
			for _, p := range fn.Params {
				c.Locals[p] = true
			}
			return
		case *ast.LambdaExpression:
			for _, p := range fn.Params {
				c.Locals[p] = true
			}
			return
		case *ast.VariableDeclaration:
			// 局部声明名进入遮蔽集；其初值中的引用继续收集
			c.Locals[fn.Name] = true
		}
		walkValue(v.Elem(), c)
		return
	}

	switch v.Kind() {
	case reflect.Struct:
		if id, ok := v.Interface().(ast.Identifier); ok {
			c.Refs[id.Name] = true
		}
		for i := 0; i < v.NumField(); i++ {
			t := v.Type()
			if t.Field(i).PkgPath != "" {
				continue
			}
			field := v.Field(i)
			// VariableDeclaration 的 Name/Type 是字符串字段，值表达式仍是 Expression 引用
			walkValue(field, c)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), c)
		}
	}
}