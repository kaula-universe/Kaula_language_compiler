package version

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLookupPathsWithKaulaHome 锁住 KAULA_HOME 分支的两条前缀路径。
// 安装介质的落盘约定（scripts/version_file.py）依赖这两条：
//
//	$KAULA_HOME/compiler/version.json
//	$KAULA_HOME/version.json
//
// 一旦有人调整顺序或改目录名，deb/rpm/dmg/NSIS 四类介质里已发布的
// version.json 就读不到了 —— `kaulac --version` 会退回 dev-local，
// 而这不会有任何报错（正是 v1.1.377 及之前发生的事）。
func TestLookupPathsWithKaulaHome(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "fake-kaula-home")
	t.Setenv("KAULA_HOME", home)

	paths := LookupPaths()
	if len(paths) < 2 {
		t.Fatalf("LookupPaths() 返回过短: %v", paths)
	}
	want := []string{
		filepath.Join(home, "compiler", "version.json"),
		filepath.Join(home, "version.json"),
	}
	for i, w := range want {
		if paths[i] != w {
			t.Errorf("候选路径[%d] = %q, 期望 %q（KAULA_HOME 分支必须保持在前两位）", i, paths[i], w)
		}
	}
}

// TestLookupPathsCoversInstallerLayouts 锁住"无 KAULA_HOME"时安装布局依赖的
// 两条 exeDir 相对路径：
//
//	exeDir/version.json             —— 版本文件与可执行文件同目录
//	exeDir/../compiler/version.json —— 安装根目录下的 compiler/
//
// 这是四类介质共用的命中方式（Windows $INSTDIR\bin、macOS .app/Contents/MacOS/bin、
// Linux /usr/local/kaula/bin 与 /usr/local/kaula/compiler）。
func TestLookupPathsCoversInstallerLayouts(t *testing.T) {
	t.Setenv("KAULA_HOME", "")

	exePath, err := os.Executable()
	if err != nil {
		t.Skipf("拿不到可执行文件路径，跳过: %v", err)
	}
	exeDir := filepath.Dir(filepath.Clean(exePath))

	want := map[string]bool{
		filepath.Join(exeDir, "version.json"):                        false,
		filepath.Join(exeDir, "..", "compiler", "version.json"):      false,
		filepath.Join(exeDir, "compiler", "version.json"):            false,
		filepath.Join(exeDir, "..", "..", "compiler", "version.json"): false,
	}

	for _, p := range LookupPaths() {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}

	var missing []string
	for p, found := range want {
		if !found {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Errorf("LookupPaths() 缺少安装介质依赖的候选路径 %v；"+
			"修改查找逻辑必须同步更新 scripts/version_file.py 与四类打包脚本", missing)
	}
}

// TestGetToleratesMissingVersionFile 保证版本文件缺失时不 panic、返回可用值。
// 降级值本身是 dev-local —— 但"缺失即降级"只允许发生在源码树开发环境，
// 安装介质由 scripts/version_file.py 硬失败兜住（见
// test/installer_version_file_test.py）。
func TestGetToleratesMissingVersionFile(t *testing.T) {
	info, err := Get()
	if err != nil {
		t.Fatalf("Get() 返回错误: %v", err)
	}
	if info.Version == "" {
		t.Error("Get() 返回空 version，--version 输出会缺版本号")
	}
	if GetVersion() == "" || GetVersion() == "unknown" {
		t.Errorf("GetVersion() = %q，不应为空或 unknown", GetVersion())
	}
	if GetSnapshot() == "" || GetSnapshot() == "unknown" {
		t.Errorf("GetSnapshot() = %q，不应为空或 unknown", GetSnapshot())
	}
}
