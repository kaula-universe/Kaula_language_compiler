// runtimeGOMAXPROCS —— 单独一个文件，避免 import "runtime" 与上层的
// 命名冲突，同时让 GOMAXPROCS 的设置点显式可见（TRANSACTION.md §7）。
package main

import "runtime"

func runtimeGOMAXPROCS(n int) {
	runtime.GOMAXPROCS(n)
}
