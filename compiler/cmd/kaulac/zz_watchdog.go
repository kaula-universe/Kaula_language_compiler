package main

// 临时诊断用：编译挂死时转储全部 goroutine 栈。
//
// 为什么要它：kaulac 在某些输入上会无限循环，而 Windows 上没法给 Go 进程
// 发 SIGQUIT，go test 的超时 panic 也够不到 main() 里内联的前端。
// 用一个独立文件 + init() 起看门狗，是唯一不碰 main.go 的办法
// （main.go 正在被另一处并行改动修改，不能冲突）。
//
// 用法：设 KAULA_WATCHDOG_SECONDS=45 再跑 kaulac。
// 不设就完全不启动，零开销。

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"
)

func init() {
	raw := os.Getenv("KAULA_WATCHDOG_SECONDS")
	if raw == "" {
		return
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		fmt.Fprintf(os.Stderr, "[watchdog] KAULA_WATCHDOG_SECONDS 非法: %q\n", raw)
		return
	}

	go func() {
		time.Sleep(time.Duration(secs) * time.Second)
		fmt.Fprintf(os.Stderr,
			"\n[watchdog] 编译超过 %d 秒仍未结束，转储 goroutine 栈：\n", secs)
		buf := make([]byte, 4<<20)
		n := runtime.Stack(buf, true)
		os.Stderr.Write(buf[:n])
		os.Stderr.Sync()
	}()
}
