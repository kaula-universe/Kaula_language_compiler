#!/usr/bin/env bash
# run_qemu_stress.sh — XJ380 上 Kaula 程序的多轮引导压测 / 数据收敛验证
#
# 用法：
#   ROUNDS=5 XJ380_ROOT=/workspace/OpenXJ380 ./run_qemu_stress.sh
#
# 判定标准（全部满足才算通过）：
#   1. 每一轮都能在串口日志中看到 Kaula 标记行；
#   2. 每一轮用户进程都以 exit code 0 正常退出；
#   3. 所有轮次的提取结果字节级完全一致（sha256 相同，数据收敛）。
#
# 依赖：已构建的 XJ380 镜像（镜像内 /apps/system/shell/shell.elf 为 Kaula 程序）、
#       QEMU + OVMF、ninja_build.py 的 XJ380_USER_APP 覆盖点。
#
# MARKER：串口标记行，默认 "KAULA-XJ380-HELLO"（shim 版 hello.kl）。
#         跑无 shim 版 hello_xapi.kl 时设 MARKER=KAULA-XAPI-DIRECT。
set -euo pipefail

XJ380_ROOT=${XJ380_ROOT:-/workspace/OpenXJ380}
ROUNDS=${ROUNDS:-5}
QEMU_SECONDS=${QEMU_SECONDS:-18}
OUT=${OUT:-/tmp/kaula_xj380_stress}
MARKER=${MARKER:-KAULA-XJ380-HELLO}

mkdir -p "$OUT"
rm -f "$OUT"/round_*.txt "$OUT"/summary.txt
cd "$XJ380_ROOT"

pass=0
fail=0
for i in $(seq 1 "$ROUNDS"); do
    rm -f serial.log
    DEBUG=0 KVM=0 SUDO=0 SMP=2 DISPLAY_BACKEND=none AUDIO_BACKEND=none \
        sh -c "sleep $((QEMU_SECONDS + 8)) | timeout --kill-after=2 ${QEMU_SECONDS} ninja -f build.ninja justrun" \
        > "$OUT/qemu_${i}.log" 2>&1 || true

    grep -E "${MARKER}|sys_exit: Thread shell exit with code" serial.log \
        > "$OUT/round_${i}.txt" 2>/dev/null || true

    if grep -q "$MARKER" "$OUT/round_${i}.txt" &&
       grep -q "sys_exit: Thread shell exit with code 0" "$OUT/round_${i}.txt"; then
        pass=$((pass + 1))
        echo "round $i: PASS"
    else
        fail=$((fail + 1))
        echo "round $i: FAIL"
        cat "$OUT/round_${i}.txt" || true
    fi
done

echo "=== 各轮结果 sha256 ===" | tee -a "$OUT/summary.txt"
sha256sum "$OUT"/round_*.txt | tee -a "$OUT/summary.txt"
distinct=$(sha256sum "$OUT"/round_*.txt | awk '{print $1}' | sort -u | wc -l)
echo "distinct_outputs=$distinct" | tee -a "$OUT/summary.txt"
echo "pass=$pass fail=$fail rounds=$ROUNDS" | tee -a "$OUT/summary.txt"

if [ "$fail" -eq 0 ] && [ "$distinct" -eq 1 ]; then
    echo "STRESS RESULT: PASS (数据收敛，所有轮次输出一致且退出码为 0)" | tee -a "$OUT/summary.txt"
    exit 0
fi
echo "STRESS RESULT: FAIL" | tee -a "$OUT/summary.txt"
exit 1
