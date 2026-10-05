# C++ 并发运行时等价实现对比（kaula vs Boost.Asio vs cocoyaxi）

负载对齐 kl 语言层 `order_server_full.kl`（端口 8126）/ C 版 `kaula_order_server.c`：
每请求 checksum256(256轮) + parse seq/qty/price + 业务64轮校验
+ sleep 200us（可选）+ 响应 "R,<seq>,<hex8 ck>,<total>\n"

## 服务
- `order_server_boost.cpp` — Boost.Asio C++20 协程，4 线程 io_context（端口 8127）
  编译：g++ -O3 -std=c++20 -o order_server_boost order_server_boost.cpp -lpthread
  无 sleep 版：-DNO_SLEEP -DSVC_PORT=8130
- `order_server_co.cpp` — cocoyaxi 协程库（端口 8128）
  编译：g++ -O3 -std=c++17 -o order_server_co order_server_co.cpp -I<cocoyaxi>/include <cocoyaxi>/build/src/libco.a -lpthread -ldl -lrt
  注：co::sleep 仅 ms 粒度，200us 无法表示，本版不模拟 sleep
- `init_ctor.c` — kl 服务强制提前 init 的 constructor（kl 语言层惰性 init 默认 1 线程）
- `order_server_full.kl` — kl 语言层负载对齐版（含 sleep）

## 基准（100 连接，-O3，交错，3 轮中位）
### 口径 A（200us sleep）
| | TPS | p50 | p99 | p999 |
|---|---|---|---|---|
| kl | 105.7k | 878 | 2148 | 3064 |
| Boost.Asio (4线程) | 121.4k | 779 | 1674 | 3350 |

### 口径 B（无 sleep）
| | TPS | p50 | p99 | p999 |
|---|---|---|---|---|
| kl (强制8线程) | 71.1k | 1412 | 2161 | 2698 |
| Boost.Asio (4线程) | 137.9k | 730 | 1778 | 2946 |
| cocoyaxi (8线程) | 151.8k | 459 | 3344 | 6360 |

## 关键发现（2026-10-05，组织 agent 复核修正 2026-10-05）
1. **kl 语言层惰性 init**：无连接时服务 1 线程（main 阻塞 accept，首个 spawn 前惰性 init
   未触发——正确行为，非缺口）；首个连接 spawn 后自动建满核 worker（本机 4 worker +
   iodriver + evict + block_lane = 8 线程），与 C 版显式 kaula_runtime_init(NULL) 一致。
   `init_ctor.c` 为预热诊断工具（constructor 提前 init，幂等，非功能必需）。
2. **iodriver 单线程是真瓶颈**：多线程下无 sleep 吞吐仍 71k（Worker 20%×4 不饱和），
   所有 IO 完成事件走单线程 epoll（kaula_conc_iodriver.c iodriver_loop）串行分发。
   asio 4 线程 epoll / cocoyaxi 8 线程 epoll 分摊后 130-152k。
3. kl 尾延迟 p999 三服务最低（2698us vs asio 2946 vs co 6360）。
