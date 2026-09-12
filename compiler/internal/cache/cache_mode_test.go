package cache

import "testing"

// F11 回归：test/sor/debug 等模式对同一源码生成不同的 C。
// 缓存键若不纳入模式，kaulac test 会命中普通编译的缓存、
// 直接运行不含测试 runner 的旧产物，一个测试都不执行。
func TestCacheKeyIsModeSensitive(t *testing.T) {
	dir := t.TempDir()
	normal, err := NewCacheManager(dir, "v1", "")
	if err != nil {
		t.Fatal(err)
	}
	test, err := NewCacheManager(dir, "v1", "test")
	if err != nil {
		t.Fatal(err)
	}
	k1 := normal.GetCacheKey("/proj/in.kl")
	k2 := test.GetCacheKey("/proj/in.kl")
	if k1 == k2 {
		t.Fatalf("cache key must differ by mode; both are %s", k1)
	}

	// 不同模式条目不得互相复用
	data := []byte("src")
	if err := test.Store("/proj/in.kl", data, "GEN_TEST_C", nil, nil); err != nil {
		t.Fatal(err)
	}
	if res := normal.Check("/proj/in.kl", data, nil); res.Hit {
		t.Errorf("normal compile must not hit test-mode cache entry")
	}
}
