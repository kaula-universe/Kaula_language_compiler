//go:build windows

package version

import (
	"syscall"
	"unsafe"
)

// procGetConsoleMode 在 Windows 上动态加载 kernel32!GetConsoleMode
// 用于探测 stdout 是否连接 tty。
var procGetConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleMode")

// isTerminal 在 Windows 上调用 GetConsoleMode 探测 fd 是否连接 tty。
// 失败 (例如 stdout 重定向到文件/管道) 返回 false。
func isTerminal(fd uintptr) bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode)))
	return r != 0
}