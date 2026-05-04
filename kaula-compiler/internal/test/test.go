package test

import (
	"fmt"
	"kaula-compiler/internal/ast"
	"testing"
)

// TestCase 定义测试用例
type TestCase struct {
	Name     string
	Input    string
	Expected string
}

// TypeGeneratorTestCase 类型生成器测试用例
type TypeGeneratorTestCase struct {
	Name            string
	ClassStmt       *ast.ClassStatement
	InterfaceStmt   *ast.InterfaceStatement
	ExpectedInOutput string
}

// RunCodegenTest 运行代码生成器测试的辅助函数
func RunCodegenTest(t *testing.T, testCases []TestCase) {
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Skip("Codegen tests should be run from the codegen package directly")
		})
	}
}

// RunTypeGeneratorTest 运行类型生成器测试的辅助函数
func RunTypeGeneratorTest(t *testing.T, testCases []TypeGeneratorTestCase) {
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Skip("Type generator tests should be run from the codegen package directly")
		})
	}
}

// RunInterfaceCompositionTest 运行接口组合分组专项测试的辅助函数
func RunInterfaceCompositionTest(t *testing.T) {
	t.Skip("Interface composition tests should be run from the codegen package directly")
}

// RunIntegrationTest 运行端到端集成测试的辅助函数
func RunIntegrationTest(t *testing.T) {
	t.Skip("Integration tests should be run from the codegen package directly")
}

// RunAllTests 运行所有测试的辅助函数
func RunAllTests(t *testing.T) {
	t.Skip("Tests should be run from individual packages directly")
}

// BenchmarkTypeGenerator 性能基准测试的辅助函数
func BenchmarkTypeGenerator(b *testing.B) {
	b.Skip("Benchmark should be run from the codegen package directly")
}

// ContainsPattern Helper: 验证生成的 C 代码是否包含特定的模式
func ContainsPattern(output string, patterns ...string) (bool, string) {
	for _, pattern := range patterns {
		found := false
		for i := 0; i <= len(output)-len(pattern); i++ {
			if output[i:i+len(pattern)] == pattern {
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Sprintf("Pattern '%s' not found in output", pattern)
		}
	}
	return true, ""
}
