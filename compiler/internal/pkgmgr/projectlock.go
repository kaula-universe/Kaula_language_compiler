package pkgmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LockSource 记录依赖的来源方式
type LockSource string

const (
	LockSourceLock   LockSource = "lock"   // 从 kaula.lock 复用（离线）
	LockSourceFetch  LockSource = "fetch"  // 联网拉取
	LockSourceLocal  LockSource = "local"  // 本地路径覆盖（patches）
	LockSourceUpdate LockSource = "update" // 联网更新到新版本
)

// PackageLockEntry 单个依赖的锁条目
type PackageLockEntry struct {
	Version      string     `json:"version"`                  // 精确版本（如 "0.12.0" 或 "local:../path"）
	ResolvedFrom LockSource `json:"resolved_from,omitempty"`  // 来源方式
	Registry     string     `json:"registry,omitempty"`       // 注册源 URL（联网解析时记录）
}

// ProjectLock 项目级依赖锁（kaula.lock，类似 Cargo.lock）。
// 记录每个依赖解析到的精确版本，保证构建可复现。
type ProjectLock struct {
	Packages map[string]string        `json:"packages,omitempty"` // 兼容旧格式：包名 -> 精确版本
	Entries  map[string]PackageLockEntry `json:"entries,omitempty"` // 新格式：包名 -> 锁条目（含来源信息）
}

// GetVersion 获取包的锁定版本（兼容新旧格式）
func (l *ProjectLock) GetVersion(name string) string {
	if l.Entries != nil {
		if e, ok := l.Entries[name]; ok {
			return e.Version
		}
	}
	if l.Packages != nil {
		return l.Packages[name]
	}
	return ""
}

// SetPackage 设置包的锁条目
func (l *ProjectLock) SetPackage(name, version string, source LockSource, registry string) {
	if l.Entries == nil {
		l.Entries = map[string]PackageLockEntry{}
	}
	l.Entries[name] = PackageLockEntry{
		Version:      version,
		ResolvedFrom: source,
		Registry:     registry,
	}
	// 同步到旧格式（向后兼容）
	if l.Packages == nil {
		l.Packages = map[string]string{}
	}
	l.Packages[name] = version
}

// LoadProjectLock 读取项目锁；不存在返回空锁（不报错）。
func LoadProjectLock(dir string) (*ProjectLock, error) {
	path := filepath.Join(dir, "kaula.lock")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectLock{Packages: map[string]string{}, Entries: map[string]PackageLockEntry{}}, nil
		}
		return nil, err
	}
	var l ProjectLock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse kaula.lock: %w", err)
	}
	if l.Packages == nil {
		l.Packages = map[string]string{}
	}
	if l.Entries == nil {
		l.Entries = map[string]PackageLockEntry{}
	}
	// 迁移旧格式到新格式
	for name, ver := range l.Packages {
		if _, ok := l.Entries[name]; !ok {
			l.Entries[name] = PackageLockEntry{Version: ver, ResolvedFrom: LockSourceLock}
		}
	}
	return &l, nil
}

// SaveProjectLock 写回项目锁（键排序保证输出稳定）。
func SaveProjectLock(dir string, l *ProjectLock) error {
	path := filepath.Join(dir, "kaula.lock")
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Merge 合并解析结果到锁（幂等：已存在且版本一致则不动）。
func (l *ProjectLock) Merge(results map[string]string) {
	if l.Packages == nil {
		l.Packages = map[string]string{}
	}
	for name, ver := range results {
		l.Packages[name] = ver
	}
}

// SortedNames 返回锁内包名排序列表（用于稳定输出/测试）。
func (l *ProjectLock) SortedNames() []string {
	names := make([]string, 0, len(l.Packages))
	for n := range l.Packages {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
