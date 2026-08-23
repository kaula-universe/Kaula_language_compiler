// Package version 提供 Kaula 编译器版本信息。
// 版本号来源：compiler/version.json（单一数据源，避免在 Go 代码里硬编码）。
//
// 版本格式:
//   - version:  v1.0.x (发布版本, x = 提交计数)
//   - snapshot: YY.M.DD-branch-hash (快照版本)
//   - codename: sor-oxide (版本代号)
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
		"compiler/version.json",
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

// String 返回人类可读的版本描述。
// 格式: kaulac v1.0.42 (26.8.23-master-67ffac3, sor-oxide)
func String() string {
	info, err := load()
	if err != nil {
		return fmt.Sprintf("kaulac %s", GetVersion())
	}

	s := "kaulac " + info.Version

	if info.Snapshot != "" {
		s += " (" + info.Snapshot
		if info.Codename != "" {
			s += ", " + info.Codename
		}
		s += ")"
	} else if info.Codename != "" {
		s += " (" + info.Codename + ")"
	}

	return s
}
