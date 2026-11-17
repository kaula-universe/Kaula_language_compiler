package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// unittestBatch 汇总批量单元测试的结果
type unittestBatch struct {
	Passed int
	Failed int
}

// discoverTestFiles 找出目录下所有「含 #[test] 函数」的 .kl 文件。
//
// 判定条件与编译器单文件流程一致：测试模式下 main 是可选的，
// 真正决定能否作为测试输入的是文件里有没有 #[test]。
// 无 #[test] 的文件交给编译器会被 hasTestFunctions 守卫拒绝，
// 提前过滤可以避免整批跑出无意义的编译错误。
func discoverTestFiles(root string) ([]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}

	candidates := []string{}
	if !info.IsDir() {
		candidates = append(candidates, abs)
	} else {
		err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// 跳过构建产物与依赖目录，避免把生成物当测试
				switch d.Name() {
				case ".git", "build", "cache", "pkglib", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) == ".kl" {
				candidates = append(candidates, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	var files []string
	for _, path := range candidates {
		if hasTestAttribute(path) {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files, nil
}

// hasTestAttribute 报告文件里是否出现过 #[test] 属性。
// 只做文本匹配：与编译器「自动查找测试文件」的既有行为保持一致，
// 避免为一个发现逻辑引入第二套语义。
func hasTestAttribute(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "#[test]")
}

// runUnittestBatch 逐个以子进程方式调用自身（testMode）编译并运行测试文件，
// 最后汇总。
//
// 刻意不复用 main() 的编译流程：那条路径与 inputFile 单例强耦合，
// 拆成函数的重构面远大于收益。子进程方式直接复用「kaulac unittest <file>」
// 这条已验证可用的单文件路径，零行为分叉。
func runUnittestBatch(root string, extraArgs []string) int {
	files, err := discoverTestFiles(root)
	if err != nil {
		// 路径不存在时 os.Stat 的原始错误是平台相关的裸系统消息
		// （Windows 上是 GetFileAttributesEx ...），这里换成可读提示。
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "Error: path not found: %s\n", root)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		fmt.Fprintln(os.Stderr, "Usage: kaulac unittest [file.kl | directory]")
		return 1
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no #[test] files matched under %s\n", root)
		fmt.Fprintln(os.Stderr, "Usage: kaulac unittest [file.kl | directory]")
		return 1
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Printf("Running %d test file(s) under %s\n\n", len(files), root)

	var batch unittestBatch
	for _, file := range files {
		args := append([]string{"unittest"}, extraArgs...)
		args = append(args, file)
		cmd := exec.Command(self, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		runErr := cmd.Run()
		if runErr == nil {
			batch.Passed++
		} else {
			batch.Failed++
		}
	}

	fmt.Printf("\n===== Test Files Summary =====\n")
	fmt.Printf("Files:  %d\n", len(files))
	fmt.Printf("Passed: %d\n", batch.Passed)
	fmt.Printf("Failed: %d\n", batch.Failed)
	fmt.Printf("==========================\n")
	if batch.Failed > 0 {
		fmt.Printf("\n%d test file(s) FAILED.\n", batch.Failed)
		return 1
	}
	fmt.Printf("\nAll test files passed.\n")
	return 0
}

// splitUnittestArgs 把 unittest 的参数拆成位置参数（文件/目录）和编译旗标。
// 取值旗标（如 --stdlib <path>）连同其值一起归入 flags，避免值被误当成目标路径；
// `--flag=value` 形式本身以 '-' 开头，自然归入 flags。
func splitUnittestArgs(args []string) (positional, flags []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case compilerValueFlags[a]:
			flags = append(flags, a)
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
		default:
			positional = append(positional, a)
		}
	}
	return positional, flags
}

// isSingleFileTarget 判断 unittest 的目标是否可以直接走单文件编译流程。
// 只有「恰好一个已存在的 .kl 文件」才走单文件路径，其余（无参 / 目录 /
// 不存在的路径）都交给批量模式，以便批量复用单文件流程。
func isSingleFileTarget(args []string) bool {
	if len(args) != 1 {
		return false
	}
	if filepath.Ext(args[0]) != ".kl" {
		return false
	}
	info, err := os.Stat(args[0])
	return err == nil && !info.IsDir()
}
