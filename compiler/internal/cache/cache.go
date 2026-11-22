// Package cache 提供增量编译缓存：基于源文件内容哈希 + 被 import 文件哈希
// 作为缓存键，命中时直接复用已生成的 .c 文件，跳过 codegen + C 编译。
//
// 缓存布局（cacheDir 下）：
//
//	<key>.c    生成的 C 源码
//	<key>.meta  JSON 清单（版本 / 源哈希 / 导入哈希 / 模块 / 时间戳）
//
// <key> = SHA256(输入文件绝对路径)，文件名安全。
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CacheManager 管理单个缓存目录。
// mode 为影响代码生成结果的编译模式串（如 "test"/"sor"/"debug"，
// 逗号连接，普通编译为空）。缓存键与条目清单都纳入 mode，
// 防止不同模式（生成的 C 不同）互相复用产物。
// keyExtra 为同样影响产物的编译参数（如 optLevel、KMM 池容量），
// 在编译参数确定后通过 SetKeyExtra 设置并混入缓存键，
// 防止 -O0/-O3 或不同池容量复用同一产物。
type CacheManager struct {
	dir      string
	version  string
	mode     string
	keyExtra string
}

// CacheResult 是 Check 的返回值。
type CacheResult struct {
	Hit       bool
	CCodePath string
}

// entryMeta 是每个缓存条目的清单。
type entryMeta struct {
	Version         string            `json:"version"`
	Mode            string            `json:"mode,omitempty"`
	SourceHash      string            `json:"source_hash"`
	CHash           string            `json:"c_hash,omitempty"` // .c 内容哈希，命中时校验防残缺缓存
	LinkFingerprint string            `json:"link_fingerprint,omitempty"` // 链接输入指纹，依赖库/标准库变更时失效
	ImportHashes    map[string]string `json:"import_hashes,omitempty"`
	UsedModules     []string          `json:"used_modules,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

// HashContent 返回 data 的 SHA-256 十六进制摘要。
func HashContent(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// NewCacheManager 初始化缓存管理器，确保 cacheDir 存在。
// mode 为空表示普通编译，缓存键与旧版本一致（不失效既有缓存）。
func NewCacheManager(cacheDir, version, mode string) (*CacheManager, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	return &CacheManager{dir: cacheDir, version: version, mode: mode}, nil
}

// SetKeyExtra 设置额外混入缓存键的编译参数（optLevel、池容量等）。
// 必须在首次 GetCacheKey/Check/Store 之前调用，之后不应再变更。
func (m *CacheManager) SetKeyExtra(extra string) {
	m.keyExtra = extra
}

// writeAtomic 以 tmp + rename 原子写入文件，避免进程中断或并发读取
// 看到截断/半写内容。
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// GetCacheKey 返回输入文件路径对应的稳定缓存键（文件名安全）。
// 非空 mode 与 keyExtra 混入哈希，使同一源码在不同编译模式/参数下
// 拥有独立缓存条目。
func (m *CacheManager) GetCacheKey(inputFile string) string {
	abs, err := filepath.Abs(inputFile)
	if err != nil {
		abs = inputFile
	}
	payload := abs
	if m.mode != "" {
		payload += "\x00" + m.mode
	}
	if m.keyExtra != "" {
		payload += "\x00" + m.keyExtra
	}
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func (m *CacheManager) cCodePath(key string) string { return filepath.Join(m.dir, key+".c") }
func (m *CacheManager) metaPath(key string) string  { return filepath.Join(m.dir, key+".meta") }

// ComputeLinkFingerprint 计算链接输入（标准库源、运行时源、第三方库）的指纹。
// 对每个根递归收集所有普通文件（不按扩展名过滤，最保守），
// 按绝对路径去重后以 (路径|大小|mtime_ns) 聚合为 SHA256。
// 任一链接输入文件变更（新增/删除/修改/大小变化）都会改变指纹，
// 从而让依赖 std/pkglib 源码的缓存条目失效。
func ComputeLinkFingerprint(roots []string) string {
	seen := make(map[string]struct{})
	entries := make([]string, 0, 64)
	var walk func(dir string)
	walk = func(dir string) {
		d, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, de := range d {
			p := filepath.Join(dir, de.Name())
			if de.IsDir() {
				walk(p)
				continue
			}
			info, err := de.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = p
			}
			if _, dup := seen[abs]; dup {
				continue
			}
			seen[abs] = struct{}{}
			entries = append(entries, fmt.Sprintf("%s|%d|%d", abs, info.Size(), info.ModTime().UnixNano()))
		}
	}
	for _, r := range roots {
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			walk(r)
		} else if err == nil && st.Mode().IsRegular() {
			// 根本身是文件（如 pkglib 归档）：直接纳入
			abs, aerr := filepath.Abs(r)
			if aerr != nil {
				abs = r
			}
			if _, dup := seen[abs]; !dup {
				seen[abs] = struct{}{}
				entries = append(entries, fmt.Sprintf("%s|%d|%d", abs, st.Size(), st.ModTime().UnixNano()))
			}
		}
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\x00")))
	return hex.EncodeToString(sum[:])
}

// MaxLinkInputMtime 返回链接输入（std .o、pkglib .a 等）的最大修改时间。
// 目录根递归收集所有普通文件；文件根直接 stat。
// 用于 exe 快路径校验：exe 必须比所有链接输入都新才可复用。
func MaxLinkInputMtime(roots []string) time.Time {
	var max time.Time
	var consider func(p string, info os.FileInfo)
	consider = func(p string, info os.FileInfo) {
		if info.ModTime().After(max) {
			max = info.ModTime()
		}
	}
	var walk func(dir string)
	walk = func(dir string) {
		d, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, de := range d {
			p := filepath.Join(dir, de.Name())
			if de.IsDir() {
				walk(p)
				continue
			}
			if info, err := de.Info(); err == nil && info.Mode().IsRegular() {
				consider(p, info)
			}
		}
	}
	for _, r := range roots {
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			walk(r)
		} else if err == nil {
			consider(r, st)
		}
	}
	return max
}

// Check 校验缓存是否命中：清单存在、版本/源哈希/导入哈希/链接输入指纹全部匹配、
// .c 文件存在且内容哈希一致（防止残缺/污染的缓存条目被复用）。
// 无论命中与否，CCodePath 都指向该条目的 .c 路径（未命中时为待写入路径）。
func (m *CacheManager) Check(inputFile string, data []byte, importFileHashes map[string]string, linkFingerprint string) CacheResult {
	key := m.GetCacheKey(inputFile)
	cpath := m.cCodePath(key)

	raw, err := os.ReadFile(m.metaPath(key))
	if err != nil {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	var meta entryMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if meta.Version != m.version {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if meta.Mode != m.mode {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if meta.SourceHash != HashContent(data) {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	if !hashMapsEqual(meta.ImportHashes, importFileHashes) {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	// 链接输入指纹：std/pkglib/runtime 任一变更都失效。
	// 旧条目无 LinkFingerprint 时，为兼容既有缓存不强制失效，
	// 但内容哈希校验（下）仍会兜住残缺条目。
	if meta.LinkFingerprint != "" && linkFingerprint != "" && meta.LinkFingerprint != linkFingerprint {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	cCode, err := os.ReadFile(cpath)
	if err != nil {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	// 内容指纹校验：meta 中记录 .c 内容哈希，读出的 .c 必须一致；
	// 旧条目（无 CHash）视为未命中，自然失效并重编译。
	if meta.CHash == "" || meta.CHash != HashContent(cCode) {
		return CacheResult{Hit: false, CCodePath: cpath}
	}
	return CacheResult{Hit: true, CCodePath: cpath}
}

// Store 写入生成的 C 代码与清单。Store 总是覆盖已有条目。
// .c 与 .meta 均以 tmp + rename 原子写入，杜绝"进程中断/并发读"产生
// 半写条目；meta 记录 .c 内容哈希与链接输入指纹供 Check 校验。
func (m *CacheManager) Store(inputFile string, data []byte, output string, usedModules []string, importFileHashes map[string]string, linkFingerprint string) error {
	key := m.GetCacheKey(inputFile)
	if err := writeAtomic(m.cCodePath(key), []byte(output), 0o644); err != nil {
		return err
	}
	meta := entryMeta{
		Version:         m.version,
		Mode:            m.mode,
		SourceHash:      HashContent(data),
		CHash:           HashContent([]byte(output)),
		LinkFingerprint: linkFingerprint,
		ImportHashes:    importFileHashes,
		UsedModules:     usedModules,
		CreatedAt:       time.Now().UTC(),
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return writeAtomic(m.metaPath(key), raw, 0o644)
}

// GetStats 返回 (条目数, .c 总字节, 最早/最近创建时间)。
func (m *CacheManager) GetStats() (int, int64, time.Time, time.Time) {
	entries := m.listEntries()
	if len(entries) == 0 {
		return 0, 0, time.Time{}, time.Time{}
	}
	var totalSize int64
	var oldest, newest time.Time
	for _, e := range entries {
		totalSize += e.cSize
		if oldest.IsZero() || e.createdAt.Before(oldest) {
			oldest = e.createdAt
		}
		if newest.IsZero() || e.createdAt.After(newest) {
			newest = e.createdAt
		}
	}
	return len(entries), totalSize, oldest, newest
}

// Clean 清理：先删除超过 maxAge 的条目，再若总大小超过 maxSize 则按最旧优先淘汰。
func (m *CacheManager) Clean(maxAge time.Duration, maxSize int64) error {
	entries := m.listEntries()
	now := time.Now().UTC()
	var survivors []cacheEntry
	var totalSize int64
	for _, e := range entries {
		if maxAge > 0 && now.Sub(e.createdAt) > maxAge {
			m.removeEntry(e.key)
			continue
		}
		survivors = append(survivors, e)
		totalSize += e.cSize
	}
	if maxSize <= 0 || totalSize <= maxSize {
		return nil
	}
	// 按时间从旧到新排序，优先淘汰最旧
	sort.Slice(survivors, func(i, j int) bool {
		return survivors[i].createdAt.Before(survivors[j].createdAt)
	})
	for _, e := range survivors {
		if totalSize <= maxSize {
			break
		}
		m.removeEntry(e.key)
		totalSize -= e.cSize
	}
	return nil
}

// Purge 清空整个缓存目录后重建。
func (m *CacheManager) Purge() error {
	if err := os.RemoveAll(m.dir); err != nil {
		return err
	}
	return os.MkdirAll(m.dir, 0o755)
}

// cacheEntry 是扫描目录得到的单条缓存信息。
type cacheEntry struct {
	key       string
	createdAt time.Time
	cSize     int64
}

// listEntries 扫描目录，返回所有合法缓存条目。
func (m *CacheManager) listEntries() []cacheEntry {
	dir, err := os.Open(m.dir)
	if err != nil {
		return nil
	}
	names, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		return nil
	}
	var out []cacheEntry
	for _, name := range names {
		key, ok := stripSuffix(name, ".meta")
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil {
			continue
		}
		var meta entryMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		var cSize int64
		if fi, err := os.Stat(m.cCodePath(key)); err == nil {
			cSize = fi.Size()
		}
		out = append(out, cacheEntry{key: key, createdAt: meta.CreatedAt, cSize: cSize})
	}
	return out
}

func (m *CacheManager) removeEntry(key string) {
	_ = os.Remove(m.cCodePath(key))
	_ = os.Remove(m.metaPath(key))
}

// stripSuffix 返回去后缀后的 name 及是否匹配。
func stripSuffix(name, suffix string) (string, bool) {
	if len(name) <= len(suffix) {
		return "", false
	}
	if name[len(name)-len(suffix):] != suffix {
		return "", false
	}
	return name[:len(name)-len(suffix)], true
}

// hashMapsEqual 比较 map[string]string，nil 与空 map 视作相等。
func hashMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
