package pkgmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ProjectLock 项目级依赖锁（kaula.lock，类似 Cargo.lock）。
// 记录每个依赖解析到的精确版本，保证构建可复现。
type ProjectLock struct {
	Packages map[string]string `json:"packages"` // 包名 -> 精确版本（如 "0.12.0"）
}

// LoadProjectLock 读取项目锁；不存在返回空锁（不报错）。
func LoadProjectLock(dir string) (*ProjectLock, error) {
	path := filepath.Join(dir, "kaula.lock")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectLock{Packages: map[string]string{}}, nil
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
