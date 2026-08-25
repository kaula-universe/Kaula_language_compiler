//go:build windows

package version

import (
	"syscall"
	"unsafe"
)

var (
	procGetConsoleMode    = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleMode")
	procSetConsoleMode    = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	procGetConsoleOutputCP = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleOutputCP")
)

const ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004

// isTerminal 检测 fd 是否连接终端且支持 ANSI 颜色。
// 1. GetConsoleMode 失败 → 不是终端
// 2. Windows 10+ 需要 ENABLE_VIRTUAL_TERMINAL_PROCESSING → 尝试开启
// 3. 开启失败 → 不支持颜色 (如旧版 PowerShell / cmd)
func isTerminal(fd uintptr) bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return false
	}
	// 尝试开启 VT 处理
	if mode&ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	newMode := mode | ENABLE_VIRTUAL_TERMINAL_PROCESSING
	r2, _, _ := procSetConsoleMode.Call(fd, uintptr(newMode))
	return r2 != 0
}