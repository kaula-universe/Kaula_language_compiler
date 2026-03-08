package test

import (
	"io/ioutil"
	"os"
	"testing"
)

// TestCase 表示一个测试用例
type TestCase struct {
	Name     string
	Input    string
	Expected string
}

// RunTest 运行通用测试
func RunTest(t *testing.T, testCases []TestCase, testFunc func(*testing.T, TestCase)) {
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			testFunc(t, tc)
		})
	}
}

// CreateTempFile 创建临时文件
func CreateTempFile(t *testing.T, content string) (string, func()) {
	inputFile, err := ioutil.TempFile("", "test_*.kaula")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	
	_, err = inputFile.WriteString(content)
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	inputFile.Close()
	
	cleanup := func() {
		os.Remove(inputFile.Name())
	}
	
	return inputFile.Name(), cleanup
}
