//go:build !windows

package version

import "os"

// isTerminal 在非 Windows 平台上保守返回 true。
// NO_COLOR / TERM=dumb 等环境变量已经在 colorEnabled() 里检查过。
// 如需更精确的探测，可改用 golang.org/x/term.IsTerminal，但会引入额外依赖。
func isTerminal(fd uintptr) bool {
	_ = os.Stdout
	return true
}