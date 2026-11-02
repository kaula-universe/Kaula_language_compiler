package pkgcmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildTarGzBytes 构造 tar.gz 归档。
func buildTarGzBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0644, Size: int64(len(content)),
		}); err != nil {
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

func buildZipBytes(t *testing.T, entries map[string]string) []byte {
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

// archiveServer 起一个支持 Range 的归档下载服务器。
// path 为服务器上的归档路径（默认 /mylib-1.2.3.tar.gz）。
func archiveServer(t *testing.T, data []byte, path string) *httptest.Server {
	t.Helper()
	if path == "" {
		path = "/mylib-1.2.3.tar.gz"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(data))
	}))
}

func TestAddHTTPArchive_TarGzWrapped(t *testing.T) {
	data := buildTarGzBytes(t, map[string]string{
		"mylib/include/mylib.h": "#pragma once\n",
		"mylib/src/mylib.c":     "int mylib_add(int a,int b){return a+b;}\n",
		"mylib/kaula.json":      `{"name":"mylib","implement_macro":""}`,
	})
	srv := archiveServer(t, data, "/pkg/mylib-1.2.3.tar.gz")
	defer srv.Close()

	target := &PkgTarget{Root: t.TempDir()}
	// name 为空，应能从 URL 路径 /pkg/mylib-1.2.3.tar.gz 推断出 mylib
	info, err := target.AddHTTPArchive(srv.URL+"/pkg/mylib-1.2.3.tar.gz", "", 4)
	if err != nil {
		t.Fatalf("AddHTTPArchive: %v", err)
	}
	// 名称应推断为 mylib（去掉 1.2.3 版本后缀）
	if info.Name != "mylib" {
		t.Fatalf("name = %s, want mylib", info.Name)
	}
	// 验证目录上移：src/mylib.c 应在包根（而非 mylib/ 子目录）
	if _, err := os.Stat(filepath.Join(target.Root, "mylib", "src", "mylib.c")); err != nil {
		t.Fatalf("wrapped content not moved up: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target.Root, "mylib", "include", "mylib.h")); err != nil {
		t.Fatalf("include not moved up: %v", err)
	}
	// 归档临时文件已清理
	entries, _ := os.ReadDir(filepath.Join(target.Root, "mylib"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") || strings.HasSuffix(e.Name(), ".tgz") {
			t.Fatalf("archive temp file left: %s", e.Name())
		}
	}
	if !info.HasSources {
		t.Fatalf("HasSources should be true")
	}
}

func TestAddHTTPArchive_ZipExplicitName(t *testing.T) {
	data := buildZipBytes(t, map[string]string{
		"libfoo/include/foo.h": "int foo(void);\n",
		"libfoo/src/foo.c":     "int foo(void){return 42;}\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "libfoo-2.0.zip", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	target := &PkgTarget{Root: t.TempDir()}
	info, err := target.AddHTTPArchive(srv.URL, "renamed", 2)
	if err != nil {
		t.Fatalf("AddHTTPArchive: %v", err)
	}
	if info.Name != "renamed" {
		t.Fatalf("name = %s, want renamed", info.Name)
	}
	if _, err := os.Stat(filepath.Join(target.Root, "renamed", "src", "foo.c")); err != nil {
		t.Fatalf("extracted zip content missing: %v", err)
	}
}

func TestAddHTTPArchive_UnwrappedLayout(t *testing.T) {
	// 归档内的散落文件（无包裹目录），解压后直接到包根
	data := buildTarGzBytes(t, map[string]string{
		"kaula.json": `{"name":"flat"}`,
		"flat.kl":    "pub fn hi() {}\n",
	})
	srv := archiveServer(t, data, "/flatpkg-v2.0.tar.gz")
	defer srv.Close()

	target := &PkgTarget{Root: t.TempDir()}
	info, err := target.AddHTTPArchive(srv.URL, "flatpkg", 4)
	if err != nil {
		t.Fatalf("AddHTTPArchive: %v", err)
	}
	if info.Name != "flatpkg" {
		t.Fatalf("name = %s, want flatpkg", info.Name)
	}
	if _, err := os.Stat(filepath.Join(target.Root, "flatpkg", "flat.kl")); err != nil {
		t.Fatalf("flat layout file missing: %v", err)
	}
}

func TestAddHTTPArchive_PathTraversalRejected(t *testing.T) {
	data := buildTarGzBytes(t, map[string]string{
		"../evil.txt": "pwned",
	})
	srv := archiveServer(t, data, "/evil-1.0.tar.gz")
	defer srv.Close()

	target := &PkgTarget{Root: t.TempDir()}
	_, err := target.AddHTTPArchive(srv.URL+"/evil-1.0.tar.gz", "", 4)
	if err == nil {
		t.Fatalf("expected traversal rejection")
	}
	// 不应在 pkglib 上方创建 evil.txt
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(target.Root), "evil.txt")); statErr == nil {
		t.Fatalf("escape file created")
	}
}

func TestAddHTTPArchive_404Fails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	target := &PkgTarget{Root: t.TempDir()}
	_, err := target.AddHTTPArchive(srv.URL+"/nope-1.0.zip", "", 2)
	if err == nil {
		t.Fatalf("expected 404 error")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStripVersionSuffix(t *testing.T) {
	cases := map[string]string{
		"mylib-1.2.3":   "mylib",
		"mylib-v0.1.0":  "mylib",
		"mylib-1.0":     "mylib",
		"my-lib":        "my-lib", // 不以版本结尾，保留
		"my-lib-1.2.3":  "my-lib",
		"lib":           "lib",
		"mylib-1.2.3.4": "mylib",
		"mylib":         "mylib",
	}
	for input, want := range cases {
		if got := stripVersionSuffix(input); got != want {
			t.Fatalf("stripVersionSuffix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInferNameFromURLArchive(t *testing.T) {
	cases := map[string]string{
		"https://example.com/pkg/mylib-1.2.3.tar.gz": "mylib-1.2.3.tar.gz",
		"https://example.com/a/b.zip":                "b.zip",
	}
	for url, want := range cases {
		if got := inferNameFromURL(url); got != want {
			t.Fatalf("inferNameFromURL(%q) = %q, want %q", url, got, want)
		}
	}
}
