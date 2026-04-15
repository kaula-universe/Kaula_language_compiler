package sema

import (
	"testing"
	"kaula-compiler/internal/ast"
)

// BenchmarkGenericFunctionAnalysis 基准测试：泛型函数分析
func BenchmarkGenericFunctionAnalysis(b *testing.B) {
	analyzer := NewSemanticAnalyzer()
	
	// 创建一个泛型函数
	fnStmt := &ast.FunctionStatement{
		Name: "genericFunc",
		TypeParams: []*ast.TypeParameter{
			{Name: "T", Constraint: "any"},
			{Name: "U", Constraint: "comparable"},
		},
		Params: []string{"a", "b"},
		Body: []ast.Statement{
			&ast.ExpressionStatement{
				Expression: &ast.Identifier{Name: "a"},
			},
		},
		Generic: true,
	}
	
	program := &ast.Program{
		Statements: []ast.Statement{fnStmt},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer.Analyze(program)
	}
}

// BenchmarkGenericInstantiation 基准测试：泛型实例化
func BenchmarkGenericInstantiation(b *testing.B) {
	analyzer := NewSemanticAnalyzer()
	
	// 先添加泛型函数到符号表
	analyzer.symbolTable.AddGenericSymbol("Swap", "function", []string{"T", "U"}, false, "global", 1, 1)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analyzer.symbolTable.InstantiateGeneric("Swap", []string{"int", "string"})
	}
}

// BenchmarkTypeConstraintChecking 基准测试：类型约束检查
func BenchmarkTypeConstraintChecking(b *testing.B) {
	analyzer := NewSemanticAnalyzer()
	
	// 设置类型约束
	analyzer.typeConstraints["T"] = []string{"comparable"}
	analyzer.typeConstraints["U"] = []string{"number"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer.checkTypeConstraint("int", "comparable", 1, 1)
		analyzer.checkTypeConstraint("float", "number", 1, 1)
	}
}

// BenchmarkSymbolTableLookup 基准测试：符号表查找
func BenchmarkSymbolTableLookup(b *testing.B) {
	table := NewSymbolTable(nil, "global")
	
	// 添加一些符号
	for i := 0; i < 100; i++ {
		table.AddSymbol("var"+string(rune(i)), "int", false, "local", 1, 1)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table.GetSymbol("var50")
	}
}

// BenchmarkGenericSymbolTableLookup 基准测试：泛型符号表查找
func BenchmarkGenericSymbolTableLookup(b *testing.B) {
	table := NewSymbolTable(nil, "global")
	
	// 添加泛型符号
	table.AddGenericSymbol("GenericFunc", "function", []string{"T"}, false, "global", 1, 1)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table.IsGenericType("GenericFunc")
		table.GetTypeParams("GenericFunc")
	}
}
