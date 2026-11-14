#!/usr/bin/env bash
# build.sh — 把最简 Kaula 程序编译并链接成 XJ380 用户态程序，再写入磁盘镜像
#
# 用法：
#   XJ380_ROOT=/workspace/OpenXJ380 ./build.sh
#
# 产物：
#   /tmp/kaula_xj380/hello.elf      Kaula 编译器输出（ELF64 可重定位目标文件）
#   /tmp/kaula_xj380/xj380_shim.o   XJ380 适配层目标文件
#   /tmp/kaula_xj380/shell.elf      最终 XJ380 用户态静态 ELF（入口 0x200000）
#   $XJ380_ROOT/XJ380.img          已写入该程序的 UEFI 可启动镜像
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
#    -boot none 表示不生成引导 stub；-output-format obj 表示只编译不链接。
#    产物文件名仍为 hello.elf，但内容是 REL 目标文件（用 file/nm 可确认）。
cd "$KAULA_ROOT"
"$KAULAC" --freestanding -boot none -output-format obj -output-dir "$OUT" \
    "$HERE/hello.kl"

# 2) 适配层：覆写 fs_output_putchar + 提供 main
clang -O2 -ffreestanding -fno-builtin -fno-stack-protector -mno-red-zone \
    -m64 -Wall -Wextra -c "$HERE/xj380_shim.c" -o "$OUT/xj380_shim.o"

# 3) freestanding 运行库目标文件（kaulac 编译时生成在 cache/std-objects/）
#    缓存目录名带编译器/配置 hash，可能存在多个；用 find 逐个定位，
#    不要 echo 通配拼接（多目录时会拼成空格串，使 -f 判定必然失败）。
FS_RUNTIME=$(find "$KAULA_ROOT/cache/std-objects" -name "kaula_fs_runtime.o" | head -1)
FS_KMM=$(find "$KAULA_ROOT/cache/std-objects" -name "kmm_v4_fs.o" | head -1)
for f in "$FS_RUNTIME" "$FS_KMM"; do
    [ -f "$f" ] || { echo "缺少 freestanding 运行库目标文件：$f" >&2; exit 2; }
done

# 4) 按 XJ380 用户态规则链接：复用其 crt0 + XAPI 目标文件，加载地址 0x200000
cd "$XJ380_ROOT"
ld -Ttext=0x200000 \
    out/xapi/arch/x86_64/crt0.S.o \
    out/xapi/libsys.cpp.o \
    out/xapi/xgui_stubs.cpp.o \
    out/xapi/installer_native.cpp.o \
    out/xapi/constart.cpp.o \
    "$OUT/xj380_shim.o" \
    "$OUT/hello.elf" \
    "$FS_RUNTIME" \
    "$FS_KMM" \
    -o "$OUT/shell.elf"

# 5) 写入镜像（XJ380_USER_APP 为 ninja_build.py 提供的可选覆盖点）
XJ380_USER_APP="$OUT/shell.elf" VDISK_SIZE_MIB=512 VDISK_ESP_MIB=64 \
    ninja -f build.ninja vdisk -j"$(nproc)"

echo "完成：$OUT/shell.elf 已写入 $XJ380_ROOT/XJ380.img 的 /apps/system/shell/shell.elf"
echo "启动验证：cd $XJ380_ROOT && DEBUG=0 KVM=0 SUDO=0 DISPLAY_BACKEND=none ninja -f build.ninja justrun"
