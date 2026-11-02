#!/usr/bin/env python3
"""安装包必须自带真实版本号 / Installers must ship compiler/version.json.

回归背景
--------
v1.1.377 及之前，四类安装介质（NSIS .exe / .deb / .rpm / .dmg）都没有把构建
生成的 `compiler/version.json` 打进包里。kaulac 运行期按候选路径读不到版本
文件时静默降级，于是安装后：

    $ kaulac --version
    kaulac dev-local (dev-local)

语言功能正常，但版本自述失真，用户无法确认自己装的是哪个版本，也没人会发现
产物与源码版本不一致。修复见 `scripts/version_file.py`。

本测试分三层
------------
1. 单元层：`stage_into` / `verify_installed` 的正常路径与四种必须硬失败的场景
   （缺文件、JSON 损坏、version 字段为空、版本不匹配）。
2. 守门层：静态断言每个打包入口都真的接了版本文件落盘 —— 防止有人后续重构
   时把这一步删掉，让缺陷原地复活。
3. 症状层：如果本仓库已构建出 `build/bin/kaulac`，直接跑 `--version`，断言
   输出里没有 `dev-local` 且版本号与 `compiler/version.json` 一致。这是唯一
   能证明"用户看到的东西是对的"的检查，CI 构建后必然执行。

运行
----
    python3 test/installer_version_file_test.py           # 全部
    python3 test/installer_version_file_test.py --strict   # 缺 build/bin/kaulac 即失败

退出码 0 = 通过；1 = 失败。--strict 用于 CI 构建之后，确保症状层没有被跳过。
"""

import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT / "scripts"))

from version_file import (  # noqa: E402
    VersionFileError,
    load_version_json,
    stage_into,
    verify_installed,
)

PACKAGING_ENTRIES = [
    "scripts/installers/build_deb.py",
    "scripts/installers/build_rpm.py",
    "scripts/installers/build_dmg.py",
    "scripts/package_release.py",
]

STRICT = "--strict" in sys.argv


def strip_ansi(text: str) -> str:
    """剥离 ANSI CSI 序列（kaulac --version 会打印彩色 banner）。"""
    out = text
    while "\x1b[" in out:
        head, rest = out.split("\x1b[", 1)
        parts = rest.split("m", 1)
        if len(parts) < 2:          # 尾部残缺，停止
            break
        out = head + parts[1]
    return out


def make_fake_project(root: Path, payload=None) -> Path:
    """造一个带 compiler/version.json 的假项目根。"""
    project = root / "project"
    (project / "compiler").mkdir(parents=True, exist_ok=True)
    data = {"version": "v9.9.9", "snapshot": "26.11.2-master-abc1234", "codename": "kaula"}
    if payload is not None:
        data = payload
    (project / "compiler" / "version.json").write_text(
        json.dumps(data, ensure_ascii=False), encoding="utf-8"
    )
    return project


class StageVersionFileTest(unittest.TestCase):
    """单元层：落盘行为与必须硬失败的场景。"""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def test_stage_writes_both_locations(self):
        project = make_fake_project(self.tmp)
        install = self.tmp / "install"
        (install / "bin").mkdir(parents=True)

        written = stage_into(project, install)

        self.assertEqual(
            sorted(p.name for p in written), ["version.json", "version.json"])
        self.assertTrue((install / "bin" / "version.json").exists(),
                        "可执行文件同目录必须有 version.json（命中 exeDir/version.json）")
        self.assertTrue((install / "compiler" / "version.json").exists(),
                        "安装根目录下必须有 compiler/version.json（命中 exeDir/../compiler）")
        for path in written:
            data = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["version"], "v9.9.9")
            self.assertEqual(data["snapshot"], "26.11.2-master-abc1234")

    def test_stage_creates_missing_dirs(self):
        project = make_fake_project(self.tmp)
        install = self.tmp / "fresh"          # bin/ 与 compiler/ 都不存在
        stage_into(project, install)
        self.assertTrue((install / "bin" / "version.json").exists())
        self.assertTrue((install / "compiler" / "version.json").exists())

    def test_stage_custom_bin_dir(self):
        """dmg 之类把可执行文件放在非 bin/ 的场景要能显式指定。"""
        project = make_fake_project(self.tmp)
        install = self.tmp / "app"
        stage_into(project, install, bin_dir=install / "MacOS" / "bin")
        self.assertTrue((install / "MacOS" / "bin" / "version.json").exists())

    def test_missing_source_is_hard_failure(self):
        project = self.tmp / "empty-project"
        (project / "compiler").mkdir(parents=True)
        install = self.tmp / "install"
        with self.assertRaises(VersionFileError):
            stage_into(project, install)
        self.assertFalse((install / "bin" / "version.json").exists(),
                         "硬失败时不允许留下半截介质")

    def test_broken_json_is_hard_failure(self):
        project = make_fake_project(self.tmp, payload=None)
        bad = project / "compiler" / "version.json"
        bad.write_text("{ not json", encoding="utf-8")
        with self.assertRaises(VersionFileError):
            load_version_json(project)
        with self.assertRaises(VersionFileError):
            stage_into(project, self.tmp / "install")

    def test_empty_version_field_is_hard_failure(self):
        project = make_fake_project(
            self.tmp, payload={"version": "", "snapshot": "26.11.2-master-x"})
        with self.assertRaises(VersionFileError):
            stage_into(project, self.tmp / "install")

    def test_empty_snapshot_field_is_hard_failure(self):
        project = make_fake_project(
            self.tmp, payload={"version": "v1.0.1", "snapshot": "  "})
        with self.assertRaises(VersionFileError):
            stage_into(project, self.tmp / "install")

    def test_verify_installed_checks_version_match(self):
        project = make_fake_project(self.tmp)
        install = self.tmp / "install"
        (install / "bin").mkdir(parents=True)
        stage_into(project, install)

        verify_installed(install, "v9.9.9")      # 不抛异常
        verify_installed(install, "9.9.9")       # 允许无 v 前缀
        with self.assertRaises(VersionFileError):
            verify_installed(install, "v9.9.8")  # 版本不一致必须拒绝出包

    def test_verify_installed_fails_when_missing(self):
        empty = self.tmp / "empty"
        (empty / "bin").mkdir(parents=True)
        with self.assertRaises(VersionFileError):
            verify_installed(empty, "v9.9.9")


class PackagingWiringTest(unittest.TestCase):
    """守门层：每个出包入口都必须真的接上版本文件落盘。"""

    def test_packagers_stage_version_file(self):
        for rel in PACKAGING_ENTRIES:
            path = PROJECT_ROOT / rel
            self.assertTrue(path.exists(), f"打包脚本不存在: {rel}")
            text = path.read_text(encoding="utf-8")
            self.assertIn("stage_into(", text,
                          f"{rel} 没有调用 stage_into()，安装介质会缺 version.json")
            self.assertIn("verify_installed(", text,
                          f"{rel} 没有调用 verify_installed()，出包前缺版本校验")

    def test_toolkit_builds_installs_version_json(self):
        text = (PROJECT_ROOT / "toolkit_build.py").read_text(encoding="utf-8")
        self.assertIn("def install_version_json(self):", text,
                      "toolkit_build.py 缺少 install_version_json()")
        # build_all / --target compiler / --target package 三条路径都要调用
        self.assertEqual(text.count("install_version_json()"), 3,
                         "install_version_json() 必须在 build_all 与两个 target 分支都调用")

    def test_nsis_ships_version_file(self):
        text = (PROJECT_ROOT / "scripts" / "installers" / "kaula.nsi").read_text(
            encoding="utf-8")
        self.assertIn('File "..\\..\\compiler\\version.json"', text,
                      "NSIS 安装包必须安装 compiler/version.json（源文件缺失时 "
                      "makensis 会直接报错，这正是我们要的硬失败）")
        self.assertIn('RMDir /r "$INSTDIR\\compiler"', text,
                      "卸载段要清理 $INSTDIR\\compiler，否则会留残")

    def test_workflows_gate_on_version_string(self):
        """发布流水线必须有 --version 冒烟门禁，否则同类缺陷再次静默发布。"""
        text = (PROJECT_ROOT / ".github" / "workflows" / "release.yml").read_text(
            encoding="utf-8")
        self.assertIn("installer_version_file_test.py", text,
                      "release.yml 必须跑 test/installer_version_file_test.py 作为出包门禁")


class InstalledBinarySmokeTest(unittest.TestCase):
    """症状层：构建产物里的 kaulac 必须自述真实版本号。"""

    def _find_binary(self):
        for name in ("kaulac.exe", "kaulac"):
            for base in (PROJECT_ROOT / "build" / "bin", PROJECT_ROOT / "bin"):
                cand = base / name
                if cand.exists():
                    return cand
        return None

    def test_installed_kaulac_reports_real_version(self):
        binary = self._find_binary()
        if binary is None:
            if STRICT:
                self.fail("--strict 下必须已构建 build/bin/kaulac 才能验证版本自述")
            self.skipTest("未构建 build/bin/kaulac，跳过症状层检查")

        # 模拟"安装后"环境：把 bin/ 目录整体拷到临时目录，脱离源码树，
        # 用户机器上的 kaulac 就是这个形态。
        with tempfile.TemporaryDirectory() as tmp:
            install_root = Path(tmp) / "kaula"
            shutil.copytree(binary.parent, install_root / "bin")
            compiler_dir = PROJECT_ROOT / "compiler"
            if compiler_dir.exists():
                (install_root / "compiler").mkdir(parents=True, exist_ok=True)
                shutil.copy2(compiler_dir / "version.json",
                             install_root / "compiler" / "version.json")
            exe = install_root / "bin" / binary.name

            try:
                # 故意不用 text=True：banner 含终端图形字符，其 UTF-8 字节序列在
                # Windows 默认编码 (cp1252) 下不可解码，text=True 会让 subprocess 的
                # readerthread 抛 UnicodeDecodeError 并把 stdout 读成空串，症状层
                # 于是误报"版本号为空"。这里按字节捕获、自行容错解码。
                proc = subprocess.run(
                    [str(exe), "--version"], capture_output=True,
                    timeout=120, cwd=str(tmp))
            except (OSError, subprocess.TimeoutExpired) as exc:
                self.fail(f"运行 {exe} --version 失败: {exc}")

            out = ((proc.stdout or b"") + (proc.stderr or b"")).decode(
                "utf-8", errors="replace")
            self.assertEqual(proc.returncode, 0, f"--version 退出码非 0:\n{out}")
            self.assertTrue(out.strip(), "--version 无任何输出（捕获/解码失败）")

            plain = strip_ansi(out)
            self.assertNotIn("dev-local", plain,
                             f"kaulac 仍自述 dev-local —— 安装介质缺 version.json:\n{out[:600]}")

            source = PROJECT_ROOT / "compiler" / "version.json"
            if source.exists():
                expected = json.loads(source.read_text(encoding="utf-8"))["version"]
                self.assertIn(expected.lstrip("v"), plain,
                              f"--version 未包含期望版本号 {expected}:\n{plain[:400]}")


if __name__ == "__main__":
    unittest.main(verbosity=2, argv=[sys.argv[0]])
