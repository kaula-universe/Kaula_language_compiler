#!/usr/bin/env bash
# build_xapi.sh — 纯 Kaula 直调 XAPI（无 C shim）编译/链接/注入镜像
#
# 用法：
#   XJ380_ROOT=/workspace/OpenXJ380 ./build_xapi.sh
#
# 与 build.sh（shim 版）的区别：
#   - 输入是 hello_xapi.kl，不需要 xj380_shim.c，也不需要 -cflags；
#   - 链接时用 --defsym main=kaula_main 把 XJ380 的入口别名到 Kaula 入口。
#
# 前置条件：kaulac 已含 freestanding SSO String 修复（见 hello_xapi.kl 注释）。
#
# 产物：
#   $OUT/hello_xapi.elf   Kaula 编译器输出（ELF64 可重定位目标文件）
#   $OUT/shell_xapi.elf   最终 XJ380 用户态静态 ELF（入口 0x200000，零未定义符号）
#   $XJ380_ROOT/XJ380.img 已写入该程序的 UEFI 可启动镜像
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
KAULA_ROOT=${KAULA_ROOT:-$(cd "$HERE/../.." && pwd)}
XJ380_ROOT=${XJ380_ROOT:-/workspace/OpenXJ380}
OUT=${OUT:-/tmp/kaula_xj380}
KAULAC=${KAULAC:-$KAULA_ROOT/build/bin/kaulac}

mkdir -p "$OUT"
[ -x "$KAULAC" ] || { echo "找不到 kaulac：$KAULAC" >&2; exit 2; }
[ -f "$XJ380_ROOT/build.ninja" ] || { echo "XJ380 未生成构建图：$XJ380_ROOT" >&2; exit 2; }

# 1) Kaula -> C -> clang -c -> ELF64 可重定位目标文件
#    -boot none 不生成引导 stub；-output-format obj 只编译不链接。
#    修复后无需 -cflags：生成的 C 自带 SSO String 与 SSO 感知 string_cdata。
cd "$KAULA_ROOT"
"$KAULAC" --freestanding -boot none -output-format obj -output-dir "$OUT" \
    "$HERE/hello_xapi.kl"

# 2) 稳健定位 freestanding 运行库目标文件（缓存目录名带 hash，用 find 而非通配拼接）
FS_IO=$(find "$KAULA_ROOT/cache/std-objects" -name "fs_io_io.o" | head -1)
FS_BASE=$(find "$KAULA_ROOT/cache/std-objects" -name "fs_base_types.o" | head -1)
FS_RUNTIME=$(find "$KAULA_ROOT/cache/std-objects" -name "kaula_fs_runtime.o" | head -1)
FS_KMM=$(find "$KAULA_ROOT/cache/std-objects" -name "kmm_v4_fs.o" | head -1)
for f in "$FS_IO" "$FS_BASE" "$FS_RUNTIME" "$FS_KMM"; do
    [ -f "$f" ] || { echo "缺少 freestanding 运行库目标文件：$f" >&2; exit 2; }
done

# 3) 按 XJ380 用户态规则链接：复用 crt0 + XAPI 目标文件，加载地址 0x200000。
#    --defsym main=kaula_main：XJ380 的 xapi_start 调用 main，而 freestanding
#    模板把 fn main 生成为 kaula_main，用链接器符号别名对齐两者。
cd "$XJ380_ROOT"
ld -Ttext=0x200000 --defsym main=kaula_main \
    out/xapi/arch/x86_64/crt0.S.o \
    out/xapi/libsys.cpp.o \
    out/xapi/xgui_stubs.cpp.o \
    out/xapi/installer_native.cpp.o \
    out/xapi/constart.cpp.o \
    "$OUT/hello_xapi.elf" \
    "$FS_IO" "$FS_BASE" "$FS_RUNTIME" "$FS_KMM" \
    -o "$OUT/shell_xapi.elf"

# 4) 零未定义符号自检
UNDEF=$(nm -u "$OUT/shell_xapi.elf" | wc -l)
[ "$UNDEF" -eq 0 ] || { echo "链接产物仍有 $UNDEF 个未定义符号" >&2; nm -u "$OUT/shell_xapi.elf" >&2; exit 2; }

# 5) 写入镜像（XJ380_USER_APP 为 ninja_build.py 提供的可选覆盖点）
XJ380_USER_APP="$OUT/shell_xapi.elf" VDISK_SIZE_MIB=512 VDISK_ESP_MIB=64 \
    ninja -f build.ninja vdisk -j"$(nproc)"

echo "完成：$OUT/shell_xapi.elf 已写入 $XJ380_ROOT/XJ380.img 的 /apps/system/shell/shell.elf"
echo "启动验证：cd $XJ380_ROOT && DEBUG=0 KVM=0 SUDO=0 DISPLAY_BACKEND=none ninja -f build.ninja justrun"
echo "压测：MARKER=KAULA-XAPI-DIRECT ROUNDS=5 ./run_qemu_stress.sh"
