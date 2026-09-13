package stdlib

import (
	"encoding/json"
	"testing"
)

// ThirdPartyLibrary 必须能解析 pkglib 的 C 源码内联配置
// （source_dir / sources），feat/attribute-extensions 的 codegen
// 路径依赖这两个字段，但字段定义漏提交导致编译器无法构建。
func TestThirdPartyLibraryParsesCSourceConfig(t *testing.T) {
	data := []byte(`{
		"name": "zlib",
		"headers": ["zlib.h"],
		"source_dir": "/opt/pkglib/zlib",
		"sources": ["inflate.c", "deflate.c"]
	}`)
	var lib ThirdPartyLibrary
	if err := json.Unmarshal(data, &lib); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if lib.SourceDir != "/opt/pkglib/zlib" {
		t.Errorf("SourceDir = %q, want %q", lib.SourceDir, "/opt/pkglib/zlib")
	}
	if len(lib.Sources) != 2 || lib.Sources[0] != "inflate.c" || lib.Sources[1] != "deflate.c" {
		t.Errorf("Sources = %v, want [inflate.c deflate.c]", lib.Sources)
	}
}
