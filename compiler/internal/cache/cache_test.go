package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRejectsCorruptedC(t *testing.T) {
	dir := t.TempDir()
	m, err := NewCacheManager(dir, "0.1.0-alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	src := "fn main() {}"
	gen := "int main(){return 0;}"
	in := filepath.Join(dir, "a.kl")
	if err := m.Store(in, []byte(src), gen, nil, nil); err != nil {
		t.Fatal(err)
	}
	r := m.Check(in, []byte(src), nil)
	if !r.Hit {
		t.Fatalf("expected hit after clean store, got miss")
	}
	// 截断 .c
	c := m.cCodePath(m.GetCacheKey(in))
	os.WriteFile(c, []byte("int m"), 0644)
	r2 := m.Check(in, []byte(src), nil)
	if r2.Hit {
		t.Fatalf("corrupted .c must be a miss, got hit (len=%d)", len("int m"))
	}
	_ = strings.TrimSpace
}
