import ctypes, time, sys, os
MB = int(sys.argv[1]) if len(sys.argv)>1 else 1024
# balloon：分配并持续触碰，制造缺页 + 回收压力
bufs=[]
try:
    while True:
        b = bytearray(MB*1024*1024)
        # 每页写一次，强制 commit（不让它停在 lazy alloc）
        for off in range(0, len(b), 4096):
            b[off] = 1
        bufs.append(b)
        if len(bufs)*MB > MB*2:
            bufs.pop(0)   # 维持恒定驻留，避免把自己 OOM
        time.sleep(0.05)
except MemoryError:
    pass
