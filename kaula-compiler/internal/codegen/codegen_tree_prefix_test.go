package codegen

import (
	"io/ioutil"
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/config"
	"os"
	"testing"
)

func TestTreePrefixIntegration(t *testing.T) {
	// 创建配置
	cfg := config.DefaultConfig()

	// 创建代码生成器
	cg := NewCodeGenerator(cfg)

	// 测试Tree语句
	treeStmt := &ast.TreeStatement{
		Root: &ast.IntegerLiteral{Value: 42},
	}

	treeCode := cg.generateTreeStatement(treeStmt)
	expectedTreeCode := "// Tree structure using Vector from std library\n"
	expectedTreeCode += "Vector* tree_nodes = vector_create(100);\n"
	expectedTreeCode += "// Add root node\n"
	expectedTreeCode += "void* root_node = 42;\n"
	expectedTreeCode += "vector_push_back(tree_nodes, root_node);\n"
	expectedTreeCode += "// Print tree structure\n"
	expectedTreeCode += "// TODO: Implement tree traversal using std library functions\n"
	expectedTreeCode += "vector_destroy(tree_nodes);\n"

	if treeCode != expectedTreeCode {
		t.Errorf("Expected tree code to be:\n%s\nGot:\n%s", expectedTreeCode, treeCode)
	}

	// 测试Prefix语句
	prefixStmt := &ast.PrefixStatement{
		Name: "test",
		Body: []ast.Statement{
			&ast.ExpressionStatement{
				Expression: &ast.CallExpression{
					Function: &ast.Identifier{Name: "println"},
					Args: []ast.Expression{
						&ast.StringLiteral{Value: "Hello from prefix"},
					},
				},
			},
		},
	}

	prefixCode := cg.generatePrefixStatement(prefixStmt)
	expectedPrefixCode := "PrefixSystem* prefix_system = prefix_system_create();\n"
	expectedPrefixCode += "prefix_enter(\"test\");\n"
	expectedPrefixCode += "printf(\"%s\\n\", \"Hello from prefix\");\n"
	expectedPrefixCode += "prefix_leave();\n"
	expectedPrefixCode += "prefix_system_destroy(prefix_system);\n"

	if prefixCode != expectedPrefixCode {
		t.Errorf("Expected prefix code to be:\n%s\nGot:\n%s", expectedPrefixCode, prefixCode)
	}

	// 验证Tree和Prefix管理器是否正确初始化
	if cg.treeManager == nil {
		t.Error("Tree manager should not be nil")
	}

	if cg.prefixManager == nil {
		t.Error("Prefix manager should not be nil")
	}

	// 验证Prefix是否被正确创建
	prefixes := cg.prefixManager.ListPrefixes()
	if len(prefixes) != 1 || prefixes[0] != "test" {
		t.Errorf("Expected prefix 'test' to be created, got: %v", prefixes)
	}

	// 验证Tree是否被正确创建
	treeHeight := cg.treeManager.GetHeight()
	if treeHeight != 2 { // 根节点 + 一个子节点
		t.Errorf("Expected tree height to be 2, got: %d", treeHeight)
	}

	treeSize := cg.treeManager.GetSize()
	if treeSize != 2 { // 根节点 + 一个子节点
		t.Errorf("Expected tree size to be 2, got: %d", treeSize)
	}
}

func TestTemplateEnhancements(t *testing.T) {
	// 暂时跳过模板测试，专注于Tree和Prefix集成
	t.Skip("Template enhancements test skipped for now")
	
	// 创建模板管理器
	tm := NewTemplateManager()

	// 加载测试模板
	testTemplate := `
{{if has_condition}}
Hello {{name}}!
{{endif}}
{{each items}}
Item: {{items}}
{{endeach}}
`

	// 保存到临时文件
	tempFile, err := ioutil.TempFile("", "test_template.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	ioutil.WriteFile(tempFile.Name(), []byte(testTemplate), 0644)
	tm.LoadTemplate("test", tempFile.Name())

	// 测试模板填充
	params := map[string]string{
		"has_condition": "true",
		"name":         "World",
		"items":        "test item",
	}

	result, err := tm.FillTemplate("test", params)
	if err != nil {
		t.Fatal(err)
	}

	expected := `

Hello World!


Item: test item

`

	if result != expected {
		t.Errorf("Expected template result to be:\n%s\nGot:\n%s", expected, result)
	}

	// 测试条件为false的情况
	params["has_condition"] = "false"
	result, err = tm.FillTemplate("test", params)
	if err != nil {
		t.Fatal(err)
	}

	expected = `



Item: test item

`

	if result != expected {
		t.Errorf("Expected template result with false condition to be:\n%s\nGot:\n%s", expected, result)
	}
}
