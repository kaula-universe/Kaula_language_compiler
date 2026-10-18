package main

import (
	"errors"
	"fmt"
	"os"

	"kaula/internal/pkgpublish"
)

// runPkgPublish 处理 `kaulac pkg publish ...` 子命令。
//
// 在 main() 的预扫描之前拦截（与 test/unittest 同样的理由）：
// pkg 子命令收集参数时在遇到第一个 '-' 旗标即截断，`pkg publish --target github`
// 若走预扫描，`--target` 会被当成编译器旗标丢给 flag.Parse 而静默失效。
func runPkgPublish(args []string) int {
	opts, err := pkgpublish.ParsePublishArgs(args)
	if err != nil {
		if errors.Is(err, pkgpublish.ErrHelp) {
			fmt.Print(pkgpublish.UsageText)
			return 0
		}
		var ue *pkgpublish.UsageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "Error: %v\n\n%s", err, pkgpublish.UsageText)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	results, err := pkgpublish.Publish(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if len(results) > 0 {
		fmt.Println("[pkg publish] summary:")
		for _, r := range results {
			fmt.Printf("  %-8s %-12s %-10s %s\n", r.Target, r.Status, r.Tag, r.URL)
		}
	}
	return 0
}
