# 三方真实混合服务基准：Kaula conc vs Go GMP vs Rust Tokio

> 回答三个问题：**同一份真实混合服务负载下，三方的吞吐与尾延迟（P50/P99/P999）如何？
> 受外部干扰（内存压力、CPU 核心竞争）时各自退化多少？**

> 📊 **实测结果见 [`REPORT.md`](REPORT.md)** —— 72 次运行（3 运行时 × 8 档 × 3 轮）
> 全部 `mismatch=0 errors=0`。本文件是**方法论文档**（怎么测、为什么这么测），
> `REPORT.md` 是**结果文档**（测出了什么）。

本目录是 `test/bench/` 下的**服务级**基准，与既有的**内核微基准**
（`run_bench.py` / `cbench/` / `go/bench/` / `tokio/bench/`）互补：

| | 内核微基准 | 本服务基准 |
|---|---|---|
| 入口 | 进程内 `spawn` 循环 | TCP 长连接请求-响应 |
| 延迟口径 | 内核打点 submit→complete | **客户端 RTT**（用户实际感知） |
| 并发 | 并发句柄窗口 | 并发连接数 |
| 干扰 | 无 | 内存 / CPU核竞争 / 网络，逐级加压 |

---

## 1. 三方实现

| 边 | 目录 | 实现 | 端口 |
|---|---|---|---|
| **Kaula** | `kaula_kl/` | `order_server.kl`，`conc fn conn_task`，**经 kaulac 编译** | 8123 |
| **Go** | `go/` | `go` + `bufio` per-connection goroutine | 8124 |
| **Tokio** | `tokio/` | `#[tokio::main]` + `tokio::spawn` per-connection | 8126 |

> **Kaula 边是纯语言层口径**（`conc fn` + `spawn` + 挂起点），不是手写 C 直调内核 API。
> 后者在 `test/bench/service/kaula_order_server.c`，代表**内核下限**，两者不可混用。

## 2. 负载：真实混合服务

行协议 `O,<seq>,<item>,<qty>,<price>` → `R,<seq>,<hex8>,<total>`。
每请求五段流水线，**计算 + 内存 + IO 三成分混合**：

| 段 | 内容 | 成分 |
|---|---|---|
| S1 | 读一行 | 网络 |
| S2 | `checksum256`（256 轮 FNV 变体，u32 回绕） | **CPU 主体** |
| S3 | 手写扫描解析 seq/qty/price | CPU |
| S4 | 堆分配 64B 业务对象 + 64 轮混合校验 + 释放 | **内存/分配器** |
| S5 | `sleep(200µs)` 模拟下游 DB/缓存 | **IO/定时器** |
| S6 | 编码响应（含 hex8） | CPU |

完整语义与公平性契约见 [`TRANSACTION.md`](TRANSACTION.md)。

### 正确性前置

`loadgen` 在 Go 里**独立复刻**了 S2–S4 的算法，对每条响应校验
格式 / seq 顺序 / hex8 / total 四项。任何一边算错 → `mismatch>0` → 该轮作废。
**「跑得飞快但算错了」拿不到数字。**

## 3. 抗干扰（逐级加压）

| 档位 | 干扰 | 实现 |
|---|---|---|
| `baseline` | 无 | — |
| `cpu25/50/75` | 占住 **8/16/24** 个核（32 核机器） | `disturb/cpuburn.c`，每线程绑核自旋 |
| `mem25/50/75` | balloon **256/512/768 MB** 常驻并持续触碰 | `disturb/mem.py` |
| `net` | loopback 大流量 ×2 | 抢带宽与软中断 |

干扰进程与被测服务**严格串行、独立进程组**，跑完即 `SIGKILL`。
退化倍率 = `干扰档指标 / 同运行时 baseline`。

## 4. 测量纪律

本仓 `FINDINGS.md` F10/F12 实测**噪声底 8%~49%**，单样本对比无效。因此：

- **交错执行**：同一档位下三方轮转（`kaula → go → tokio`），
  避免「按运行时分批」把时间序混进运行时差异。
- **重复采样**：每档 N 轮，报**跨轮中位数** + 轮内极差 `spread%`。
- **收敛判据**：每 5s 一窗，**丢弃前 50% 窗口**（预热+爬坡），
  对剩余窗口取中位数；同时报窗口间极差以证明已收敛。
- **三边同配置**：worker 数统一为 4（`SVC_WORKERS`）。

> 引用任何数字时必须同时给出 **轮数 / spread%**；差距小于噪声底的一律不下结论。

## 5. 复现

```bash
# 0) 构建 Kaula 编译器 + 运行时
python toolkit_build.py --release

# 1) 构建三方服务
cd test/bench/service3
kaulac kaula_kl/order_server.kl \
  -cflags "kaula_kl/svc_wrap.c -I<repo>/src/concurrency -I<repo>/build/include"
cp /path/to/libkaula_runtime.a .   # 或直接在仓库根跑上面那条

cd go && go build -o order_server_go .        && cd ..
cd tokio && cargo build --release             && cd ..

# 2) 构建压测器与干扰器
go build -o /tmp/svc3/loadgen ./loadgen.go
gcc -O2 -o /tmp/cpuburn disturb/cpuburn.c -lpthread

# 3) 跑矩阵
python3 run_service3.py --conns 100 --sec 40 --win 5 --rounds 3 \
    --levels baseline,cpu25,cpu50,cpu75,mem25,mem50,mem75,net \
    --out results.json
```

快速验证（推荐先跑这个）：
```bash
python3 run_service3.py --levels baseline --conns 50 --sec 10 --win 5 --rounds 1 --only kaula
```

## 6. 文件职责

| 文件 | 职责 |
|---|---|
| `TRANSACTION.md` | **负载语义与公平性契约**（三方必须逐位等价的定义） |
| `loadgen.go` | 压测器 + **独立正确性校验**（RTT 口径、逐窗口输出） |
| `run_service3.py` | 编排：交错执行、干扰注入、收敛判定、退化倍率报表 |
| `kaula_kl/` | Kaula 语言层服务（`order_server.kl` + `svc_wrap.c`） |
| `go/` | Go 服务 |
| `tokio/` | Tokio 服务 |
| `disturb/` | 干扰器（CPU 绑核自旋 / 内存 balloon） |
| `gen_report.py` | 从 `results.json` 生成 `REPORT.md` 的表格 |
| `KBUG_*.md` | 过程中发现的编译器缺陷记录 |
| `results.json` | 原始结果（含每窗口数据，缺陷修复**前**） |
| `results_after_fix.json` | 修复后重测结果（回归验证用） |
| `REPORT.md` | **实测报告与结论** |

## 7. 已知的可比性限制（务必随结论披露）

1. **`sleep(200µs)` 三边超额不同**（`FINDINGS.md` F2）：Tokio 的定时器轮盘
   粒度硬编码 1ms，短睡眠实际超额远大于 Go。这是**运行时属性**，
   但会让 S5 的绝对值被拉偏 —— **看相对形态，不要把绝对值归因于调度质量**。
2. **`for..in(range)` 循环体守卫槽缺陷**（见 `KBUG_RANGE_GUARD.md`）：
   **已修复**。Kaula 侧原先为绕开该缺陷把 S4 计算下沉到普通 `fn`，
   修复后已改回**用户直写形态**（`for kk in range(64)`），
   端到端 `loadgen` 验证 `mismatch=0 errors=0`。本报告数据为修复后重测。
3. 沙箱为共享实例，绝对 TPS 会随机器状态漂移，**比值比绝对值稳健**。
4. **CPU 干扰档是乐观口径**：干扰线程从高编号核占起（`base = NCPU - n`），
   与服务 worker 尽量错开；真实环境服务被直接抢核时退化会更重。
5. **内存干扰档未形成真实压力**：本机 123GB，balloon 最大 576MB（<0.5%），
   那一列只能说明「对轻量内存压力不敏感」，**不可外推到近 OOM 场景**。

