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
	FromPatch bool  // 是否来自本地路径覆盖（patches）
	Source   LockSource // 解析来源
}

// PatchConfig 本地路径覆盖配置（与 config.PatchConfig 对应）
type PatchConfig struct {
	Path string
}

// ResolveDependencies 解析全部依赖声明：
//  1. 项目锁 (kaula.lock) 中已锁定且缓存可用的包直接复用（不联网）
//  2. 其余包经 Fetch 联网解析（ls-remote -> semver 匹配 -> 浅克隆 -> 更新锁）
//
// patches 参数为本地路径覆盖（键：包名，值：本地目录路径）。
// offline 为 true 时禁止联网，缓存未命中则报错。
//
// 返回解析结果列表（按键排序）与更新后的项目锁。
// projectDir 用于读写项目 kaula.lock；锁文件不存在则新建。
func ResolveDependencies(registry string, deps map[string]string, patches map[string]PatchConfig, offline bool, projectDir string) ([]ResolveResult, *ProjectLock, error) {
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

		// 0. 本地路径覆盖（patches）：优先级最高
		if patch, ok := patches[name]; ok {
			if _, err := os.Stat(patch.Path); err == nil {
				// 验证本地路径是有效的包目录
				results = append(results, ResolveResult{
					Name:      name,
					Version:   "local",
					Dir:       patch.Path,
					FromPatch: true,
					Source:    LockSourceLocal,
				})
				// 更新锁记录为 local
				lock.SetPackage(name, "local:"+patch.Path, LockSourceLocal, "")
				continue
			}
			// 本地路径不存在，报错
			return nil, nil, fmt.Errorf("dependency %s: patch path %q does not exist", name, patch.Path)
		}

		// 1. 项目锁命中且缓存可用 -> 直接复用
		if ver := lock.GetVersion(name); ver != "" {
			// 检查是否是 local: 前缀的锁条目
			if len(ver) > 6 && ver[:6] == "local:" {
				localPath := ver[6:]
				if _, err := os.Stat(localPath); err == nil {
					results = append(results, ResolveResult{Name: name, Version: "local", Dir: localPath, FromLock: true, FromPatch: true, Source: LockSourceLocal})
					continue
				}
				// 本地路径不存在，删除锁条目
				delete(lock.Packages, name)
				delete(lock.Entries, name)
			} else {
				root, err := CacheRoot()
				if err == nil {
					d := pkgDir(root, name)
					if _, statErr := os.Stat(filepath.Join(d, ".git")); statErr == nil {
						results = append(results, ResolveResult{Name: name, Version: ver, Dir: d, FromLock: true, Source: LockSourceLock})
						continue
					}
				}
				// 缓存不可用（被清空）: 删除锁条目，走联网解析
				delete(lock.Packages, name)
				delete(lock.Entries, name)
			}
		}

		// 2. 联网解析
		if offline {
			return nil, nil, fmt.Errorf("dependency %s: not in lock/cache and --offline is set", name)
		}
		fr, err := Fetch(registry, name, constraint)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve dependency %s: %w", name, err)
		}
		lock.SetPackage(name, fr.Version, LockSourceFetch, registry)
		results = append(results, ResolveResult{
			Name:    fr.Name,
			Version: fr.Version,
			Dir:     fr.Dir,
			Source:  LockSourceFetch,
		})
	}
	return results, lock, nil
}

// FetchDependencies 强制联网解析所有依赖（忽略锁缓存）。
// 用于 `kaulac pkg fetch` 命令：显式联网拉取最新版本。
func FetchDependencies(registry string, deps map[string]string, projectDir string) ([]ResolveResult, *ProjectLock, error) {
	results := []ResolveResult{}
	lock, err := LoadProjectLock(projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load kaula.lock: %w", err)
	}
	if len(deps) == 0 {
		return results, lock, nil
	}

	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		constraint := deps[name]
		fr, err := Fetch(registry, name, constraint)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch dependency %s: %w", name, err)
		}
		lock.SetPackage(name, fr.Version, LockSourceFetch, registry)
		src := "fetched"
		if fr.Source == "cache" {
			src = "cached"
		}
		fmt.Printf("[pkg] %s %s (%s)\n", fr.Name, fr.Version, src)
		results = append(results, ResolveResult{
			Name:    fr.Name,
			Version: fr.Version,
			Dir:     fr.Dir,
			Source:  LockSourceFetch,
		})
	}
	return results, lock, nil
}

// UpdateDependencies 联网更新依赖到满足约束的最新版本。
// 与 FetchDependencies 的区别：Update 会删除旧版本缓存，强制拉取最新匹配版本。
func UpdateDependencies(registry string, deps map[string]string, projectDir string) ([]ResolveResult, *ProjectLock, error) {
	results := []ResolveResult{}
	lock, err := LoadProjectLock(projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load kaula.lock: %w", err)
	}
	if len(deps) == 0 {
		return results, lock, nil
	}

	root, cacheErr := CacheRoot()
	if cacheErr != nil {
		return nil, nil, fmt.Errorf("resolve cache root: %w", cacheErr)
	}

	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		constraint := deps[name]
		oldVer := lock.GetVersion(name)

		// 清除旧版本缓存，强制拉取最新
		dst := pkgDir(root, name)
		if locked, _ := LoadResolved(root, name); locked != nil {
			// 删除旧的 resolved 锁，让 Fetch 重新拉取
			_ = os.Remove(lockFile(root, name))
			// 如果有缓存目录，也删除以强制重新克隆
			_ = os.RemoveAll(dst)
		}

		fr, err := Fetch(registry, name, constraint)
		if err != nil {
			return nil, nil, fmt.Errorf("update dependency %s: %w", name, err)
		}

		source := LockSourceUpdate
		if oldVer == fr.Version {
			source = LockSourceFetch // 版本没变，不算真正更新
		}
		lock.SetPackage(name, fr.Version, source, registry)

		if oldVer != "" && oldVer != fr.Version {
			fmt.Printf("[pkg] %s: %s -> %s (updated)\n", name, oldVer, fr.Version)
		} else if oldVer == "" {
			fmt.Printf("[pkg] %s %s (new)\n", fr.Name, fr.Version)
		} else {
			fmt.Printf("[pkg] %s %s (up to date)\n", fr.Name, fr.Version)
		}

		results = append(results, ResolveResult{
			Name:    fr.Name,
			Version: fr.Version,
			Dir:     fr.Dir,
			Source:  source,
		})
	}
	return results, lock, nil
}
