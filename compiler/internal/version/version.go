// Package version 提供 Kaula 编译器版本信息。
// 版本号来源：compiler/version.json（单一数据源，避免在 Go 代码里硬编码）。
//
// 版本格式:
//   - version:  v1.0.x (发布版本, x = 提交计数)
//   - snapshot: YY.M.DD-branch-hash (快照版本)
//   - codename: kaula (版本代号)
package version

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Info 是 version.json 的结构
type Info struct {
	Version  string `json:"version"`            // 发布版本 v1.0.x
	Snapshot string `json:"snapshot,omitempty"` // 快照版本 YY.M.DD-branch-hash
	Codename string `json:"codename,omitempty"` // 版本代号
	Build    string `json:"build,omitempty"`    // 兼容旧格式
}

var (
	once      sync.Once
	cached    Info
	loadError error
)

// LookupPaths 返回 version.json 的候选查找路径（按优先级）：
//  1. KAULA_HOME/compiler/version.json
//  2. 可执行文件所在目录/version.json（kaulac.exe 与 version.json 同目录部署）
//  3. 可执行文件上一级/compiler/version.json
//  4. 当前工作目录/compiler/version.json
//  5. 当前工作目录/version.json
func LookupPaths() []string {
	var candidates []string
	if envHome := os.Getenv("KAULA_HOME"); envHome != "" {
		candidates = append(candidates,
			filepath.Join(envHome, "compiler", "version.json"),
			filepath.Join(envHome, "version.json"))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(filepath.Clean(exePath))
		candidates = append(candidates,
			filepath.Join(exeDir, "version.json"),
			filepath.Join(exeDir, "compiler", "version.json"),
			filepath.Join(exeDir, "..", "compiler", "version.json"),
			// 开发布局: build/bin/kaulac.exe -> ../../compiler/version.json
			filepath.Join(exeDir, "..", "..", "compiler", "version.json"))
	}
	candidates = append(candidates,
		"kaula/version.json",
		"version.json")
	return candidates
}

// load 读取 version.json（带缓存）
func load() (Info, error) {
	once.Do(func() {
		for _, p := range LookupPaths() {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			var info Info
			if err := json.Unmarshal(data, &info); err != nil {
				loadError = fmt.Errorf("parse %s: %w", p, err)
				return
			}
			cached = info
			loadError = nil
			return
		}
		loadError = fmt.Errorf("version.json not found (looked in %d candidate paths)", len(LookupPaths()))
	})
	return cached, loadError
}

// Get 返回版本信息；读取失败时返回零值 Info 与错误。
// 调用方（kaulac 主流程）应容忍失败：版本缺失不阻断编译。
func Get() (Info, error) {
	return load()
}

// GetVersion 返回发布版本号（如 "v1.0.42"）；读取失败返回 "unknown"。
func GetVersion() string {
	info, err := load()
	if err != nil || info.Version == "" {
		return "unknown"
	}
	return info.Version
}

// GetSnapshot 返回快照版本号（如 "26.8.23-master-67ffac3"）；读取失败返回 "unknown"。
func GetSnapshot() string {
	info, err := load()
	if err != nil || info.Snapshot == "" {
		return "unknown"
	}
	return info.Snapshot
}

// LogoText 是 Kaula 的极简字符画 logo (6 行高, 取自 npm 风格 minimal block)
// 这是纯文本常量 (无 ANSI), 用于 fallback 或外部直接引用。
// 启用颜色时, 由 Logo() 函数返回带 24-bit 渐变 ANSI 的版本。
const LogoText = `
██╗  ██╗ █████╗ ██╗   ██╗██╗      █████╗
██║ ██╔╝██╔══██╗██║   ██║██║     ██╔══██╗
█████╔╝ ███████║██║   ██║██║     ███████║
██╔═██╗ ██╔══██║██║   ██║██║     ██╔══██║
██║ ╚██╗██║  ██║╚██████╔╝███████╗██║  ██║
╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝  ╚═╝
`

// colorEnabled 报告是否输出 24-bit 颜色 ANSI 转义。
//  1. NO_COLOR 环境变量存在且非空 → 关闭
//  2. stdout 不是 tty (例如管道/重定向) → 关闭 (避免污染日志/文件)
//  3. TERM=dumb → 关闭
// 其余场景开启 (现代 Windows Terminal / WezTerm / VSCode terminal / Linux 主流终端均支持 truecolor)
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	if !isTerminal(os.Stdout.Fd()) {
		return false
	}
	return true
}

// Logo 返回带 24-bit 渐变 ANSI 的 logo 字符串。
// 渐变方向: 水平 cyan (#00FFFF) → magenta (#FF00FF), 每字符一色。
// 当 colorEnabled() 为 false 时返回纯 LogoText 文本 (与管道/重定向/NO_COLOR 兼容)。
func Logo() string {
	if !colorEnabled() {
		return LogoText
	}
	const (
		cR0, cG0, cB0 = 0, 255, 255   // cyan
		cR1, cG1, cB1 = 255, 0, 255   // magenta
	)
	// 拆 LogoText 为行, 跳过首尾的空行
	raw := LogoText
	lines := []string{}
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			line := raw[start:i]
			if start == 0 && line == "" {
				start = i + 1
				continue
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(raw) {
		lines = append(lines, raw[start:])
	}
	// 计算最大可见宽度 (按 rune 数)
	width := 0
	for _, l := range lines {
		w := 0
		for range l {
			w++
		}
		if w > width {
			width = w
		}
	}
	if width == 0 {
		return LogoText
	}
	const reset = "\033[0m"
	b := make([]byte, 0, len(LogoText)*4)
	for li, line := range lines {
		col := 0
		for _, ch := range line {
			// 跳过普通空格不染色, 避免色块间隙突兀
			if ch == ' ' {
				b = append(b, ' ')
				col++
				continue
			}
			t := float64(col) / float64(width-1)
			r := int(float64(cR0) + float64(cR1-cR0)*t)
			g := int(float64(cG0) + float64(cG1-cG0)*t)
			bl := int(float64(cB0) + float64(cB1-cB0)*t)
			fmt.Fprintf(stringBuilder{&b}, "\033[38;2;%d;%d;%dm", r, g, bl)
			buf := []byte(string(ch))
			b = append(b, buf...)
			col++
		}
		b = append(b, reset...)
		if li < len(lines)-1 {
			b = append(b, '\n')
		}
	}
	return string(b)
}

// stringBuilder 适配 fmt.Fprintf 写到 []byte
type stringBuilder struct{ b *[]byte }

func (s stringBuilder) Write(p []byte) (int, error) {
	*s.b = append(*s.b, p...)
	return len(p), nil
}

// Banner 返回 logo + 版本信息的多行字符串。
// 格式:
//   <Logo>
//   kaulac v1.0.42 (26.8.23-master-67ffac3)
func Banner() string {
	return Logo() + "\n" + String()
}

// String 返回人类可读的版本描述。
// 格式: kaulac v1.0.42 (26.8.23-master-67ffac3)
func String() string {
	info, err := load()
	if err != nil {
		return fmt.Sprintf("kaulac %s", GetVersion())
	}

	s := "kaulac " + info.Version

	if info.Snapshot != "" {
		s += " (" + info.Snapshot + ")"
	}

	return s
}
