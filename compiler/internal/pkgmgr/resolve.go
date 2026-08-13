package pkgmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ResolveResult 单个依赖的解析结果
type ResolveResult struct {
	Name     string
	Version  string
	Dir      string // 缓存中的包目录（含 src/<version> 子结构，供 pkglib 加载）
	FromLock bool   // 是否直接命中项目锁（未联网）
}

// ResolveDependencies 解析全部依赖声明：
//  1. 项目锁 (kaula.lock) 中已锁定且缓存可用的包直接复用（不联网）
//  2. 其余包经 Fetch 联网解析（ls-remote -> semver 匹配 -> 浅克隆 -> 更新锁）
//
// 返回解析结果列表（按键排序）与更新后的项目锁。
// projectDir 用于读写项目 kaula.lock；锁文件不存在则新建。
func ResolveDependencies(registry string, deps map[string]string, projectDir string) ([]ResolveResult, *ProjectLock, error) {
	results := []ResolveResult{}
	lock, err := LoadProjectLock(projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load kaula.lock: %w", err)
	}
	if len(deps) == 0 {
		return results, lock, nil
	}

	// 按键排序保证确定性
	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		constraint := deps[name]

		// 1. 项目锁命中且缓存可用 -> 直接复用
		if ver, ok := lock.Packages[name]; ok {
			root, err := CacheRoot()
			if err == nil {
				d := pkgDir(root, name)
				if _, statErr := os.Stat(filepath.Join(d, ".git")); statErr == nil {
					results = append(results, ResolveResult{Name: name, Version: ver, Dir: d, FromLock: true})
					continue
				}
			}
			// 缓存不可用（被清空）: 删除锁条目，走联网解析
			delete(lock.Packages, name)
		}

		// 2. 联网解析
		fr, err := Fetch(registry, name, constraint)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve dependency %s: %w", name, err)
		}
		lock.Packages[name] = fr.Version
		results = append(results, ResolveResult{
			Name:    fr.Name,
			Version: fr.Version,
			Dir:     fr.Dir,
		})
	}
	return results, lock, nil
}
