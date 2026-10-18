package pkgmgr

// 多线程并发 HTTP 下载。
//
// 背景：pkglib 包历史上只能经 git clone（需要 git 环境 + 完整仓库元数据），
// 或用单连接 io.Copy 下载归档。大包（如包含预编译产物的 C/C++ 库）单连接
// 在跨网环境下带宽利用率低。本文件实现 Range 分片并发下载：
//
//   - 先发 HEAD/GET 探测响应头，解析 Content-Length 与 Accept-Ranges；
//   - 服务器支持 Range 时，把文件切成 N 个分片并发下载，临时文件落盘后合并；
//   - 服务器不支持 Range（或未知长度）时，自动退化为单连接流式下载；
//   - 每个分片独立重试（指数退避），小分片错误整个重下，保证数据收敛；
//   - 下载完对整文件做内容校验：期望 sha256 与文件大小可同时校验；
//   - 临时文件写在同一目标目录（.download 前缀），结束时清理或改名，
//     失败不留半成品，避免污染缓存目录。
//
// 并发度由 MaxHTTPConcurrency 控制（默认 min(16, runtime.GOMAXPROCS))，
// 可通过环境变量 KAULAC_DL_CONCURRENCY 覆盖；分片最小 64KB，避免把小文件
// 切成过多碎片。

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// minSliceSize 单个分片的最小字节数：小文件不切片，直接单连接。
	minSliceSize = 64 * 1024
	// httpTimeout HTTP 请求总超时（单个请求，含读 body）。
	httpTimeout = 5 * time.Minute
	// httpHeaderTimeout 响应头超时（防半开连接挂死）。
	httpHeaderTimeout = 30 * time.Second
	// downloadChunkSize 分片/解压写盘缓冲。
	downloadChunkSize = 256 * 1024
)

// 可配置重试参数：包级变量便于单元测试覆盖（避免真实长退避）。
var (
	// dlMaxRetries 单分片/单连接最大重试次数（首次尝试后追加重试）。
	dlMaxRetries = 3
	// dlBackoffBase 重试退避基数（attempt n 退避 dlBackoffBase*2^(n-1)）。
	dlBackoffBase = time.Second
)

// MaxHTTPConcurrency 返回 HTTP 下载并发度上限。
// 默认 min(16, GOMAXPROCS)；可用环境变量 KAULAC_DL_CONCURRENCY 覆盖。
func MaxHTTPConcurrency() int {
	if v := os.Getenv("KAULAC_DL_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.GOMAXPROCS(0)
	if n <= 0 {
		n = 4
	}
	if n > 16 {
		n = 16
	}
	return n
}

// HTTPDownloadRequest 一次并发下载的描述。
type HTTPDownloadRequest struct {
	URL            string // 下载 URL（必填）
	Dest           string // 目标文件路径（必填，父目录不存在会自动创建）
	ExpectedSHA256 string // 期望 sha256（十六进制，可选）。非空则下载后校验
	ExpectedSize   int64  // 期望字节数（可选，>0 时校验）
	Concurrency    int    // 分片并发数（<=0 时用 MaxHTTPConcurrency）
}

// HTTPDownloadResult 并发下载结果。
type HTTPDownloadResult struct {
	URL          string
	Dest         string
	Bytes        int64
	Duration     time.Duration
	Concurrency  int
	TransferMode string // "multi" | "single"
	SHA256       string
}

// downloadOneRange 下载 [start,end] 区间到临时文件，支持重试。
func downloadOneRange(client *http.Client, url string, start, end int64, tmpPath string, retries int) error {
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0755); err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			// 指数退避：base / 2x / 4x
			time.Sleep(dlBackoffBase * time.Duration(1<<uint(attempt-1)))
		}
		if err := fetchRangeOnce(client, url, start, end, tmpPath); err != nil {
			lastErr = err
			continue
		}
		// 校验分片大小（Range 服务器必须返回精确区间）
		if fi, err := os.Stat(tmpPath); err == nil && fi.Size() != end-start+1 {
			lastErr = fmt.Errorf("range %d-%d: got %d bytes, want %d", start, end, fi.Size(), end-start+1)
			continue
		}
		return nil
	}
	// 返回前清掉残留半成品
	_ = os.Remove(tmpPath)
	return lastErr
}

// fetchRangeOnce 单次请求下载一个分片。
func fetchRangeOnce(client *http.Client, url string, start, end int64, tmpPath string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("User-Agent", "kaula-pkg/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		// 正常
	case http.StatusOK:
		// 服务器忽略 Range，整体返回。分片模式下属异常：若服务器不支持
		// Range，调用方应在探测阶段就退化，这里直接报错由上层处理。
		return fmt.Errorf("server ignores Range (would return full body)")
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	written, err := io.CopyBuffer(f, io.LimitReader(resp.Body, end-start+1), make([]byte, downloadChunkSize))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if written != end-start+1 {
		return fmt.Errorf("short read: got %d bytes, want %d", written, end-start+1)
	}
	return nil
}

// probeHTTPHead 发起 HEAD 请求探测服务器能力（大小、Range 支持）。
// HEAD 不可用或未声明 Accept-Ranges 时，用 GET + Range: bytes=0-0 探测。
func probeHTTPHead(client *http.Client, url string) (size int64, acceptsRange bool, err error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("User-Agent", "kaula-pkg/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		// 部分服务器（如某些对象存储）拒绝 HEAD，回退 GET Range 探测
		return probeHTTPRange(client, url)
	}

	size = parseContentLength(resp.Header)
	if size == 0 {
		// 空文件或未知长度：无法分片，走单连接
		return 0, false, nil
	}
	if isAcceptRanges(resp.Header) {
		return size, true, nil
	}
	// 未声明 Accept-Ranges，但可能实际支持（部分对象存储只在 GET 时响应）
	// 用一次性 0-0 GET 确认，宁可多一次探测也不损失并发能力
	if rsize, rok, rerr := probeHTTPRange(client, url); rerr == nil {
		return rsize, rok, nil
	}
	return size, false, nil
}

// probeHTTPRange 用 GET + Range: bytes=0-0 探测（HEAD 被拒时的回退）。
func probeHTTPRange(client *http.Client, url string) (int64, bool, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("User-Agent", "kaula-pkg/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusPartialContent {
		size := parseContentLength(resp.Header)
		if size == 0 {
			// Content-Range: bytes 0-0/1234 里有总长度
			if cr := resp.Header.Get("Content-Range"); cr != "" {
				if i := strings.LastIndex(cr, "/"); i >= 0 {
					if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil {
						size = n
					}
				}
			}
		}
		return size, true, nil
	}
	if resp.StatusCode == http.StatusOK {
		// 不支持 Range
		size := parseContentLength(resp.Header)
		return size, false, nil
	}
	return 0, false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

func parseContentLength(h http.Header) int64 {
	if v := h.Get("Content-Length"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

func isAcceptRanges(h http.Header) bool {
	for _, v := range h.Values("Accept-Ranges") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "bytes") {
				return true
			}
		}
	}
	return false
}

// DownloadHTTP 并发分片下载文件。
//
// 流程：
//  1. 探测（HEAD 优先，失败退化 GET Range 0-0）；未知大小或服务器不支持
//     Range → 单连接流式下载；
//  2. 支持 Range：按并发度切分，worker 池并发落盘到独立临时文件；
//  3. 全部成功 → 按序合并 → 校验期望大小/sha256 → 原子改名到目标；
//  4. 任一分片重试耗尽 → 清理全部临时文件，返回聚合错误（数据收敛，无半成品）。
func DownloadHTTP(req HTTPDownloadRequest) (*HTTPDownloadResult, error) {
	if req.URL == "" || req.Dest == "" {
		return nil, fmt.Errorf("url and dest are required")
	}
	conc := req.Concurrency
	if conc <= 0 {
		conc = MaxHTTPConcurrency()
	}

	client := &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   conc + 4,
			IdleConnTimeout:       60 * time.Second,
			ResponseHeaderTimeout: httpHeaderTimeout,
		},
	}

	// 可重试探测：网络抖动时 HEAD 失败会重试；HTTP 状态错误（4xx/5xx）
	// 不重试，直接失败（404 是确定的资源不存在）。
	size, acceptsRange, serr := probeHTTPHead(client, req.URL)
	if serr != nil && isNetworkError(serr) && !isHTTPStatusError(serr) {
		for i := 1; i <= 2; i++ {
			time.Sleep(dlBackoffBase / 2 * time.Duration(1<<uint(i-1)))
			size, acceptsRange, serr = probeHTTPHead(client, req.URL)
			if serr == nil {
				break
			}
		}
	}
	if serr != nil {
		return nil, fmt.Errorf("probe %s: %w", req.URL, serr)
	}

	start := time.Now()
	destDir := filepath.Dir(req.Dest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	// 期望大小校验先行：探测到的长度与期望不符时尽早失败
	if req.ExpectedSize > 0 && size > 0 && size != req.ExpectedSize {
		return nil, fmt.Errorf("size mismatch for %s: server %d, expect %d", req.URL, size, req.ExpectedSize)
	}

	var result *HTTPDownloadResult
	var err error
	if size > 0 && acceptsRange && size >= minSliceSize {
		result, err = downloadMulti(client, req, size, conc)
	} else {
		result, err = downloadSingle(client, req, size)
	}
	if err != nil {
		return nil, err
	}
	result.Duration = time.Since(start)
	return result, nil
}

// downloadMulti Range 分片并发下载。
func downloadMulti(client *http.Client, req HTTPDownloadRequest, size int64, conc int) (*HTTPDownloadResult, error) {
	if conc > int(size/minSliceSize) {
		conc = int(size / minSliceSize)
	}
	if conc < 1 {
		conc = 1
	}
	if conc > 64 {
		conc = 64
	}

	sliceLen := (size + int64(conc) - 1) / int64(conc)
	sliceLen = (sliceLen + minSliceSize - 1) / minSliceSize * minSliceSize // 对齐到 64KB
	if sliceLen <= 0 {
		sliceLen = size
	}
	nSlices := int((size + sliceLen - 1) / sliceLen)

	tmpDir := filepath.Dir(req.Dest)
	tmpPat := filepath.Join(tmpDir, ".dl-"+strconv.FormatInt(time.Now().UnixNano(), 36)+"-")
	var tmpFiles []string
	defer func() {
		// 失败或成功都必须清理临时分片
		for _, p := range tmpFiles {
			_ = os.Remove(p)
		}
	}()

	type sliceRange struct{ idx, start, end int64 }
	slices := make([]sliceRange, 0, nSlices)
	for i := int64(0); i < int64(nSlices); i++ {
		start := i * sliceLen
		end := start + sliceLen - 1
		if end > size-1 {
			end = size - 1
		}
		slices = append(slices, sliceRange{idx: i, start: start, end: end})
	}

	results := make([]string, nSlices)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, conc)
	var firstErr error
	var errMu sync.Mutex

	for _, s := range slices {
		wg.Add(1)
		go func(s sliceRange) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tmpPath := fmt.Sprintf("%s%d", tmpPat, s.idx)
			err := downloadOneRange(client, req.URL, s.start, s.end, tmpPath, dlMaxRetries)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("slice %d (%d-%d): %w", s.idx, s.start, s.end, err)
				}
				errMu.Unlock()
				return
			}
			mu.Lock()
			results[s.idx] = tmpPath
			tmpFiles = append(tmpFiles, tmpPath)
			mu.Unlock()
		}(s)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	for i := 0; i < nSlices; i++ {
		if results[i] == "" {
			return nil, fmt.Errorf("slice %d missing after download", i)
		}
	}

	// 合并分片到目标（原子：先写 .part，校验后改名）
	partPath := tmpPat + "merged"
	fout, err := os.Create(partPath)
	if err != nil {
		return nil, err
	}
	var total int64
	h := sha256.New()
	ok := true
	for i := 0; i < nSlices; i++ {
		fin, err := os.Open(results[i])
		if err != nil {
			ok = false
			break
		}
		n, err := io.Copy(io.MultiWriter(fout, h), fin)
		fin.Close()
		if err != nil {
			ok = false
			break
		}
		total += n
	}
	fout.Close()
	if !ok || total != size {
		_ = os.Remove(partPath)
		return nil, fmt.Errorf("merge failed: wrote %d of %d bytes", total, size)
	}

	// 校验期望
	shaHex := hex.EncodeToString(h.Sum(nil))
	if err := verifyDownload(req, total, shaHex); err != nil {
		_ = os.Remove(partPath)
		return nil, err
	}
	if err := os.Rename(partPath, req.Dest); err != nil {
		_ = os.Remove(partPath)
		return nil, err
	}

	return &HTTPDownloadResult{
		URL:          req.URL,
		Dest:         req.Dest,
		Bytes:        total,
		Concurrency:  nSlices,
		TransferMode: "multi",
		SHA256:       shaHex,
	}, nil
}

// downloadSingle 单连接流式下载（服务器不支持 Range 或长度未知）。
// 请求 Range: bytes=0- ：若服务器支持 Range 得到 206（即整个文件），
// 若服务器忽略返回 200（同样为完整内容），两种情况都无损拼接。
func downloadSingle(client *http.Client, req HTTPDownloadRequest, size int64) (*HTTPDownloadResult, error) {
	destDir := filepath.Dir(req.Dest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}
	partPath := filepath.Join(destDir, ".dl-single-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".part")
	defer os.Remove(partPath)

	var lastErr error
	for attempt := 0; attempt <= dlMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(dlBackoffBase * time.Duration(1<<uint(attempt-1)))
		}
		req2, err := http.NewRequest("GET", req.URL, nil)
		if err != nil {
			return nil, err
		}
		req2.Header.Set("Range", "bytes=0-")
		req2.Header.Set("User-Agent", "kaula-pkg/1.0")
		resp, err := client.Do(req2)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		f, err := os.Create(partPath)
		if err != nil {
			lastErr = err
			resp.Body.Close()
			return nil, err
		}
		h := sha256.New()
		total, err := io.CopyBuffer(io.MultiWriter(f, h), resp.Body, make([]byte, downloadChunkSize))
		resp.Body.Close()
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
		if err != nil {
			lastErr = err
			continue
		}
		shaHex := hex.EncodeToString(h.Sum(nil))
		if err := verifyDownload(req, total, shaHex); err != nil {
			lastErr = err
			continue
		}
		if err := os.Rename(partPath, req.Dest); err != nil {
			return nil, err
		}
		return &HTTPDownloadResult{
			URL:          req.URL,
			Dest:         req.Dest,
			Bytes:        total,
			Concurrency:  1,
			TransferMode: "single",
			SHA256:       shaHex,
		}, nil
	}
	return nil, lastErr
}

// verifyDownload 校验下载文件的期望大小与 sha256。
func verifyDownload(req HTTPDownloadRequest, size int64, shaHex string) error {
	if req.ExpectedSize > 0 && size != req.ExpectedSize {
		return fmt.Errorf("size mismatch: got %d, expect %d", size, req.ExpectedSize)
	}
	if req.ExpectedSHA256 != "" {
		if !strings.EqualFold(shaHex, req.ExpectedSHA256) {
			return fmt.Errorf("sha256 mismatch: got %s, expect %s", shaHex, req.ExpectedSHA256)
		}
	}
	return nil
}

// isNetworkError 判断错误是否属于网络/传输层错误（值得重试）。
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(interface{ Timeout() bool })
	if ok {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection") ||
		strings.Contains(msg, "reset by peer") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "network") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "eof")
}

// isHTTPStatusError 判断错误是否由确定性的 HTTP 状态码引起（不应重试）。
func isHTTPStatusError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 4") || strings.Contains(msg, "http 5") ||
		strings.Contains(msg, "http 3") || strings.Contains(msg, "http 2")
}

// --- 归档解压 ---

// DownloadArchive 并发下载归档并解压到 destDir。
// 支持 .tar.gz/.tgz/.tar/.zip（按扩展名识别，无法识别时按内容 magic 探测）。
// 解压做路径穿越防护：拒绝 ../、绝对路径与符号链接逃逸。
// 返回解压后的首个顶层路径（归档常被单一目录包裹，如 mylib/）。
// destDir 目录会由函数自动创建。
func DownloadArchive(req HTTPDownloadRequest, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}
	if _, err := DownloadHTTP(req); err != nil {
		return "", err
	}
	archivePath := req.Dest
	defer func() {
		// 归档下载解压后即弃，不留在缓存目录占空间。
		_ = os.Remove(archivePath)
	}()

	top, err := extractArchiveFile(archivePath, destDir)
	if err != nil {
		return "", err
	}
	return top, nil
}

// extractArchiveFile 解压归档文件。
// 优先级：先按内容 magic 识别（可靠）；无法识别时回退扩展名判断。
func extractArchiveFile(archivePath, destDir string) (string, error) {
	if f, err := os.Open(archivePath); err == nil {
		var magic [4]byte
		n, _ := io.ReadFull(f, magic[:])
		f.Close()
		if n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
			top, err := extractTarGz(archivePath, destDir)
			if err != nil {
				return "", err
			}
			return top, nil
		}
		if n >= 4 && magic[0] == 'P' && magic[1] == 'K' && (magic[2] == 3 || magic[2] == 5 || magic[2] == 7) {
			top, err := extractZip(archivePath, destDir)
			if err != nil {
				return "", err
			}
			return top, nil
		}
	}
	// magic 不可识别，回退扩展名
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(archivePath)), ".")
	switch ext {
	case "gz", "tgz", "tar":
		top, err := extractTarGz(archivePath, destDir)
		if err != nil {
			return "", err
		}
		return top, nil
	case "zip":
		top, err := extractZip(archivePath, destDir)
		if err != nil {
			return "", err
		}
		return top, nil
	default:
		return "", fmt.Errorf("unsupported archive format: %s (expected .tar.gz/.tgz/.tar/.zip)", filepath.Base(archivePath))
	}
}

// sanitizeArchivePath 把归档内路径规整为安全的相对路径。
// 拒绝绝对路径与 ../ 逃逸；返回 filepath.Clean 后的相对路径。
// 注意：先统一为正斜杠做语义检查（跨平台一致），再转回 OS 路径分隔符。
func sanitizeArchivePath(name string) (string, error) {
	cleaned := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	// Clean 会把 ./x 变 x、../x 保留 ../
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("archive entry with empty path")
	}
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("archive entry escapes destination: %q", name)
	}
	return filepath.FromSlash(cleaned), nil
}

// topLevelName 返回归档条目的顶层第一级路径分量。
// "mylib/include/a.h" -> "mylib"；"file.c" -> "file.c"。
// 用于判断归档是否被单一目录包裹。
func topLevelName(name string) string {
	cleaned := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if i := strings.IndexByte(cleaned, '/'); i >= 0 {
		return cleaned[:i]
	}
	return cleaned
}

func extractError(name string, err error) error {
	return fmt.Errorf("extract %s: %w", name, err)
}

func extractTarGz(archive, dest string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", extractError(archive, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", extractError(archive, err)
	}
	defer gz.Close()
	return extractTarReader(tar.NewReader(gz), dest)
}

func extractTar(archive, dest string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", extractError(archive, err)
	}
	defer f.Close()
	return extractTarReader(tar.NewReader(f), dest)
}

// extractTarReader 遍历 tar 条目安全解压。
// 返回解压后的首个顶层路径（用于探测归档是否被单一目录包裹）。
func extractTarReader(tr *tar.Reader, dest string) (string, error) {
	top := ""
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name, err := sanitizeArchivePath(hdr.Name)
		if err != nil {
			return "", err
		}
		if top == "" {
			top = topLevelName(hdr.Name)
		}
		target := filepath.Join(dest, name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0777)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return "", err
			}
			if err := f.Close(); err != nil {
				return "", err
			}
		case tar.TypeSymlink, tar.TypeLink:
			// 拒绝符号链接/硬链接：防止解压后链接逃逸目标目录
			return "", fmt.Errorf("archive entry with link type is not allowed: %s", hdr.Name)
		default:
			// 其他类型（设备、FIFO 等）跳过
			continue
		}
	}
	return top, nil
}

func extractZip(archive, dest string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", extractError(archive, err)
	}
	defer zr.Close()
	top := ""
	for _, f := range zr.File {
		name, err := sanitizeArchivePath(f.Name)
		if err != nil {
			return "", err
		}
		if top == "" {
			top = topLevelName(f.Name)
		}
		target := filepath.Join(dest, name)
		mode := f.Mode()
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
			continue
		}
		if mode&os.ModeSymlink != 0 {
			return "", fmt.Errorf("archive entry with link type is not allowed: %s", f.Name)
		}
		// zip 不会以 / 结尾但当目录含嵌套路径
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		of, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(of, rc)
		rc.Close()
		if cerr := of.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", err
		}
	}
	return top, nil
}
