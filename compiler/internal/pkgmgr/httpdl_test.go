package pkgmgr

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- 测试辅助：支持 Range 的静态文件服务器 ---

// rangeServer 起一个支持/不支持 Range 的静态文件服务器。
// allowRange=false 时忽略 Range 头始终返回 200 全量（模拟不支持 Range 的 CDN）。
func rangeServer(t *testing.T, data []byte, allowRange bool) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowRange {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}
		http.ServeContent(w, r, "blob.bin", time.Time{}, bytes.NewReader(data))
	})
	return httptest.NewServer(handler)
}

// shaHex 计算字节切片 sha256 十六进制。
func shaHex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// makeData 生成确定性的伪随机数据（不依赖 math/rand 全局状态）。
func makeData(n int64) []byte {
	b := make([]byte, n)
	var x uint32 = 0x9e3779b9
	for i := int64(0); i < n; i++ {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x >> 24)
	}
	return b
}

func TestDownloadHTTP_MultiSliceMatchesSource(t *testing.T) {
	data := makeData(1 << 20) // 1MiB，足够切分
	srv := rangeServer(t, data, true)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	res, err := DownloadHTTP(HTTPDownloadRequest{
		URL:            srv.URL,
		Dest:           dest,
		Concurrency:    4,
		ExpectedSHA256: shaHex(data),
		ExpectedSize:   int64(len(data)),
	})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if res.TransferMode != "multi" {
		t.Fatalf("expected multi slice, got %s", res.TransferMode)
	}
	if res.Bytes != int64(len(data)) {
		t.Fatalf("bytes = %d, want %d", res.Bytes, len(data))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: sha %s != %s", shaHex(got), shaHex(data))
	}
}

func TestDownloadHTTP_NoRangeDegradesToSingle(t *testing.T) {
	data := makeData(3 << 20)
	srv := rangeServer(t, data, false) // 不支持 Range
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	res, err := DownloadHTTP(HTTPDownloadRequest{URL: srv.URL, Dest: dest, Concurrency: 4})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if res.TransferMode != "single" {
		t.Fatalf("expected single fallback, got %s", res.TransferMode)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch")
	}
}

func TestDownloadHTTP_SmallFileSingleSlice(t *testing.T) {
	// 小于 minSliceSize 的文件不应走多分片（避免碎片开销）
	data := makeData(1024)
	srv := rangeServer(t, data, true)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	res, err := DownloadHTTP(HTTPDownloadRequest{URL: srv.URL, Dest: dest, Concurrency: 4})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if res.TransferMode != "single" {
		t.Fatalf("small file should be single, got %s", res.TransferMode)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch")
	}
}

// failServer 在响应 Range 分片时对指定分片注入 N 次失败，随后恢复。
// 用于验证失败重试后的数据收敛。
func failServer(t *testing.T, data []byte, failSlice int, failCount int32) *httptest.Server {
	t.Helper()
	var fails atomic.Int32
	fails.Store(0)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.ServeContent(w, r, "blob.bin", time.Time{}, bytes.NewReader(data))
			return
		}
		// 解析出第一段（bytes=start-）
		var start, end int64
		fmt.Sscanf(strings.TrimPrefix(rangeHeader, "bytes="), "%d-%d", &start, &end)
		sliceIdx := start / (64 * 1024)
		if sliceIdx == int64(failSlice) && fails.Load() < failCount {
			fails.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, "injected failure")
			return
		}
		http.ServeContent(w, r, "blob.bin", time.Time{}, bytes.NewReader(data))
	})
	return httptest.NewServer(handler)
}

func TestDownloadHTTP_RetryConverges(t *testing.T) {
	data := makeData(1 << 20)
	srv := failServer(t, data, 2, 2) // 第 2 分片失败 2 次后才成功
	defer srv.Close()

	// 快速重试（覆盖包级退避）
	old := dlBackoffBase
	dlBackoffBase = time.Millisecond
	defer func() { dlBackoffBase = old }()

	dest := filepath.Join(t.TempDir(), "out.bin")
	res, err := DownloadHTTP(HTTPDownloadRequest{
		URL:            srv.URL,
		Dest:           dest,
		Concurrency:    4,
		ExpectedSHA256: shaHex(data),
	})
	if err != nil {
		t.Fatalf("download failed despite retry: %v", err)
	}
	if res.TransferMode != "multi" {
		t.Fatalf("expected multi, got %s", res.TransferMode)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch after retry")
	}
}

func TestDownloadHTTP_SHA256MismatchFails(t *testing.T) {
	data := makeData(200 * 1024)
	srv := rangeServer(t, data, true)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	_, err := DownloadHTTP(HTTPDownloadRequest{
		URL:            srv.URL,
		Dest:           dest,
		ExpectedSHA256: strings.Repeat("0", 64),
	})
	if err == nil {
		t.Fatalf("expected sha256 mismatch error")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
	// 失败不应留下目标文件
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatalf("fail should not leave dest file")
	}
}

func TestDownloadHTTP_SizeMismatchFails(t *testing.T) {
	data := makeData(100 * 1024)
	srv := rangeServer(t, data, true)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	_, err := DownloadHTTP(HTTPDownloadRequest{
		URL:          srv.URL,
		Dest:         dest,
		ExpectedSize: int64(50 * 1024),
	})
	if err == nil {
		t.Fatalf("expected size mismatch error")
	}
}

func TestDownloadHTTP_NotFoundFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	_, err := DownloadHTTP(HTTPDownloadRequest{URL: srv.URL, Dest: dest})
	if err == nil {
		t.Fatalf("expected error for 404")
	}
}

func TestDownloadHTTP_EmptyBodyDegrades(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	res, err := DownloadHTTP(HTTPDownloadRequest{URL: srv.URL, Dest: dest})
	if err != nil {
		t.Fatalf("empty body should succeed: %v", err)
	}
	if res.Bytes != 0 {
		t.Fatalf("bytes = %d, want 0", res.Bytes)
	}
}

// --- 归档解压测试 ---

func buildTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractArchive_TarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.tar.gz")
	entries := map[string]string{
		"mylib/include/mylib.h": "#pragma once\n",
		"mylib/src/a.c":         "int main(void){return 0;}\n",
	}
	if err := os.WriteFile(archive, buildTarGz(t, entries), 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	dest := filepath.Join(dir, "dest")
	top, err := extractArchiveFile(archive, dest)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if top == "" {
		t.Fatalf("expected top-level path")
	}
	if _, err := os.Stat(filepath.Join(dest, top, "include", "mylib.h")); err != nil {
		t.Fatalf("header not extracted: %v", err)
	}
}

func TestExtractArchive_Zip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.zip")
	entries := map[string]string{
		"foo/bar.txt": "hello\n",
	}
	if err := os.WriteFile(archive, buildZip(t, entries), 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	dest := filepath.Join(dir, "dest")
	top, err := extractArchiveFile(archive, dest)
	if err != nil {
		t.Fatalf("extract zip: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, top, "bar.txt"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("content = %q", string(got))
	}
}

func TestExtractArchive_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.tar.gz")
	// "../escape.txt" 会逃逸目标目录
	entries := map[string]string{
		"../escape.txt": "pwned",
	}
	if err := os.WriteFile(archive, buildTarGz(t, entries), 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	_, err := extractArchiveFile(archive, filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatalf("expected traversal rejection")
	}
	// 不应在 dest 上层生成 escape.txt
	if _, statErr := os.Stat(filepath.Join(dir, "escape.txt")); statErr == nil {
		t.Fatalf("escape file should not exist")
	}
}

func TestExtractArchive_RejectsAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "abs.zip")
	entries := map[string]string{
		"/tmp/evil.txt": "pwned",
	}
	if err := os.WriteFile(archive, buildZip(t, entries), 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	_, err := extractArchiveFile(archive, filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatalf("expected absolute path rejection")
	}
}

func TestExtractArchive_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "link.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "mylib/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	tw.Close()
	gz.Close()
	if err := os.WriteFile(archive, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	_, err := extractArchiveFile(archive, filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatalf("expected symlink rejection")
	}
}

func TestExtractArchive_MagicSniff(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pkg.bin") // 无扩展名，靠 magic 识别
	entries := map[string]string{
		"mylib/a.txt": "hi\n",
	}
	if err := os.WriteFile(archive, buildTarGz(t, entries), 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	dest := filepath.Join(dir, "dest")
	top, err := extractArchiveFile(archive, dest)
	if err != nil {
		t.Fatalf("magic sniff: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, top, "a.txt")); err != nil {
		t.Fatalf("extracted file missing: %v", err)
	}
}

func TestDownloadArchive_TarGzEndToEnd(t *testing.T) {
	data := buildTarGz(t, map[string]string{
		"mylib/kaula.json": `{"name":"mylib"}`,
		"mylib/lib.kl":     `pub fn hi() {}`,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "mylib.tar.gz", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	destDir := filepath.Join(t.TempDir(), "pkglib")
	_ = os.MkdirAll(destDir, 0755)
	// 下载到 destDir/mylib.tar.gz，解压回 destDir
	_, err := DownloadArchive(
		HTTPDownloadRequest{URL: srv.URL, Dest: filepath.Join(destDir, "mylib.tar.gz")},
		destDir,
	)
	if err != nil {
		t.Fatalf("e2e extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "mylib", "kaula.json"))
	if err != nil {
		t.Fatalf("extracted config missing: %v", err)
	}
	if string(got) != `{"name":"mylib"}` {
		t.Fatalf("content = %q", string(got))
	}
}

func TestSanitizeArchivePath(t *testing.T) {
	valid := []string{"a/b/c.txt", "dir/", "file.txt", "./x"}
	for _, v := range valid {
		if _, err := sanitizeArchivePath(v); err != nil {
			t.Fatalf("valid path %q rejected: %v", v, err)
		}
	}
	invalid := []string{"../x", "..", "/abs", "a/../../b", ""}
	for _, v := range invalid {
		if _, err := sanitizeArchivePath(v); err == nil {
			t.Fatalf("invalid path %q accepted", v)
		}
	}
}

func TestMaxHTTPConcurrencyEnv(t *testing.T) {
	old := os.Getenv("KAULAC_DL_CONCURRENCY")
	os.Setenv("KAULAC_DL_CONCURRENCY", "7")
	defer func() { os.Setenv("KAULAC_DL_CONCURRENCY", old) }()
	if got := MaxHTTPConcurrency(); got != 7 {
		t.Fatalf("concurrency = %d, want 7", got)
	}
}

// BenchmarkDownloadHTTP 大文件并发下载吞吐压测。
// 仅做本机回环吞吐基准，验证分片合并路径在高并发下不退化。
func BenchmarkDownloadHTTP_1MB_Concurrency8(b *testing.B) {
	data := makeData(1 << 20)
	srv := rangeServer(&testing.T{}, data, true)
	defer srv.Close()
	dest := filepath.Join(os.TempDir(), "dl-bench.bin")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DownloadHTTP(HTTPDownloadRequest{URL: srv.URL, Dest: dest, Concurrency: 8}); err != nil {
			b.Fatalf("bench download: %v", err)
		}
	}
	os.Remove(dest)
}
