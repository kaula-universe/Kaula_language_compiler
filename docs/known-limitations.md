# 已知限制与未完成能力清单（Known Limitations）

本页集中登记**当前版本仍然成立**的能力缺口、平台边界与刻意取舍。
教程与工具文档只在紧邻示例处保留必要提示，完整、可复核的清单以本页为准。

> 复核口径：
> - 标准库登记情况：`compiler/stdlib.json`（`python scripts/stdlib_stats.py` 复核）；
> - 语言行为：`compiler/internal/{parser,sema,codegen}` 对应诊断码与 `test/*.kl` 语料；
> - 并发内核缺口：另见 [core-runtime.md · 第 5 章 已知缺口](core-runtime.md)；
> - 历史缺陷的修复记录见 [CHANGELOG.md](../CHANGELOG.md)，本页只描述**当下**状态。
> 状态随版本演进：某条在此被移除 = 已实现并通过测试，不要再回退到旧说法。

---

## 1. 语言语法与语义

| 限制 | 现状 / 行为 | 推荐替代 | 来源 |
|---|---|---|---|
| **多返回值** | 不支持 `fn f() -> (A, B)` 元组返回 | 用结构体聚合返回，或指针/输出参数带回结果 | [functions.md](tutorials/functions.md) |
| **lambda 捕获外层变量** | lambda 不能捕获外层局部变量（编译报错） | 改用嵌套函数，嵌套函数按引用捕获外层变量 | [functions.md](tutorials/functions.md) |
| **闭包逃逸（带捕获的函数返回出去）** | 捕获指针仅在原作用域有效，带捕获的嵌套函数作为返回值传递不支持 | 需要跨作用域传值时封装成结构体持有 | [functions.md](tutorials/functions.md) |
| **extern C 变参 `(...)`** | `extern fn printf(fmt, ...)` 形式未实现，无法直接调用 C 变参函数 | 为固定签名的 C 函数逐个写 `extern fn` 声明 | [extern-and-ffi.md](tutorials/extern-and-ffi.md) |
| **`cstring("...")` 内置** | 字符串字面量到 C `const char*` 的内置转换未提供 | 用 `extern fn` + `const char*`/`void()` 记法直接对接 | [extern-and-ffi.md](tutorials/extern-and-ffi.md) |
| **`transmute<T>` / 无检查位重解释** | 编译器不存在该内置；类型转换以 `as<T>()` 为唯一显式通道 | 需要位重解释时经 `extern` 对接 C 辅助函数 | [extern-and-ffi.md](tutorials/extern-and-ffi.md) |
| **回调签名式记法 `void(T...)R` 作 extern 参数** | 作为 `extern fn` 参数尚无法解析；作为普通函数参数可解析但代码生成失败。透明指针 `void()` / `const void()` 记法可用 | 先以 `void*` 对接，再在内部 `as<T*>` 转型 | [extern-and-ffi.md](tutorials/extern-and-ffi.md) |
| **`spend` 语境下带数据的枚举变体** | 报 `spend_enum_with_data`；带数据枚举应在 `match` 表达式中解构 | 用 `match` 匹配 `Some(v) => ...` | [error-and-panic.md](tutorials/error-and-panic.md)、`compiler/internal/sema` |
| **裸 `try/catch` 语法** | 不提供语言级 `try/catch`；标准库以回调包裹提供 `panic_protect` 捕获恢复 | `std.panic.panic_try(&frame)` 回调模式 | [error-and-panic.md](tutorials/error-and-panic.md) |
| **隐式整数提升** | 刻意不做：`i64 + u8`、`i64 < usize` 等跨宽度/跨符号运算必须先 `as<T>()` 统一类型（类型安全设计，非缺陷） | 显式 `as<T>()` 转换 | [variables-and-types.md](tutorials/variables-and-types.md) |
| **prefix 宏：命名参数与位置实参混用** | `@F(1, tag = "x")` 报「不支持混用」 | 位置参数与 `@name=值` 分开使用 | [prefix-system.md](tutorials/prefix-system.md) |
| **prefix 宏：重复片段内嵌套 `$(...),*`** | 报「嵌套重复不支持」 | 拆成两层前缀或普通 `for` 展开 | [prefix-system.md](tutorials/prefix-system.md) |

## 2. 标准库登记与实现缺口

以 `compiler/stdlib.json` 为权威登记；源码存在 ≠ 已登记可调。

| 模块 | 现状 | 备注 / 去向 |
|---|---|---|
| `std.logging` | **仅头文件占位**：`std/logging/logging.c` 为空文件，`stdlib.json` 只登记 `LogLevel` 枚举、无函数签名 | 日志输出暂用 `std.io.print*`；待补实现与签名登记 |
| `std.serialize` | **源码已实现**（`std/serialize/serialize.c`），但 `stdlib.json` 仅登记 `header`、**无函数签名** | 编译器调用检查缺少签名信息；待补签名登记后可被类型检查 |
| `std.gui` | 有实现目录（`std/gui/gui.c` 116 行 + `gui.h`），**未登记进 `stdlib.json`** | 暂不能以 `std.gui.*` 调用；待登记与平台依赖补齐 |
| `std.i18n` | 有实现目录（`std/i18n/i18n.c` 951 行 + `i18n.h`），**未登记进 `stdlib.json`** | 同上，待登记 |
| 自动暴露签名范围 | 类成员方法、模板值返回类型暂不自动暴露到调用签名表 | 见 [stdlib-integration.md](stdlib-integration.md) 签名提取约束 |

## 3. CLI 与工具链

| 项 | 现状 | 替代 / 说明 | 来源 |
|---|---|---|---|
| 输出文件名 `-o` / `--output` | 用户编译命令**未注册**该 flag，传入报 `flag provided but not defined`；输出名随源码（`foo.kl` → `foo`） | 只能用 `--output-dir` 指定输出目录 | [tools/kaulac.md](tools/kaulac.md) |
| 格式化缩进风格 | 固定 4 空格，不支持自定义宽度/Tab | 全局统一风格 | [tools/formatter.md](tools/formatter.md) |
| 格式化注释保留 | AST 不保留注释，三种注释形式均在输出中丢失 | 注释需外置文档维护 | [tools/formatter.md](tools/formatter.md) |
| 格式化手动换行 / 注解自定义格式 | 不保留源码手动换行；注解不支持自定义排版 | 接受确定性重排 | [tools/formatter.md](tools/formatter.md) |
| 独立 `kaulafmt` 二进制 | **已废弃**，格式化统一为 `kaulac fmt`；`cmd/kaulafmt/` 仅留源码壳、不产出二进制 | 迁移对照见 [tools/kaulafmt.md](tools/kaulafmt.md) | [tools/formatter.md](tools/formatter.md) |

## 4. 运行时与平台边界

| 项 | 现状 | 影响 / 状态 | 来源 |
|---|---|---|---|
| CONC 挂起点表达力边界 | 表达式位置 `await`、复合语句内取 `await` 返回值、`for` 体内挂起、条件里挂起、一条语句多挂起点——一律**编译期报诊断**，不静默取过期结果 | 完整影响表与设计取舍见 [core-runtime.md 第 5 章](core-runtime.md) | core-runtime.md §5 |
| Windows socket 异步 IO | `SOCKET` 非 CRT fd，`_read` 返回 `ENOTSOCK`，内核 socket IO 在 Windows 不可用，需按句柄类型分派 | 已知平台边界 | core-runtime.md §1.4 / §5 |
| F13：双形态 spawn（Windows） | kl 双形态 spawn 在 Windows 上确定性堆损坏（`0xC0000379`） | 仅 Windows；Linux 路径不受影响，待专项修复 | CHANGELOG · Known issues |
| F11：高并发低频死锁 | `conc=10000` 下约 2.6% 死锁，成因未定，Linux 低概率可复现 | 建议挂死 watchdog dump 辅助定位 | `test/bench/FINDINGS.md` · CHANGELOG |
| KMM `realloc` 跨区域指针 | 不支持 KMM 指针与 libc 指针混入同一个 `realloc` | 分配与释放须在同一分配器域 | [core-runtime.md](core-runtime.md) KMM 章 |
| `fast_free`（Deprecated） | `fast_allocator` 为 bump 分配器不支持逐个释放，`fast_free` 当前为 no-op，仅保留符号兼容 | 批量回收经 `fast_allocator_reset()` | CHANGELOG · Deprecated |
| `struct KaulaTask` ABI | 完整定义仍暴露于安装头，标注为内部/非稳定 ABI；不透明化推迟到 v1.2 | 不要依赖其布局做跨版本 ABI | CHANGELOG · Known issues |
| LSan 测试残留报告 | 短生命周期 C 内核测试二进制退出时报 128–320B pthread 描述符残留；进程未调用 `kaula_runtime_shutdown`，属退出 teardown 良性残留（OS 回收），非运行期泄漏 | 非缺陷，无需修复 | CHANGELOG · Known issues |

## 5. 测试与质量工程

| 项 | 现状 | 说明 |
|---|---|---|
| 负向（错误代码应被拒绝）测试自动化不足 | `test/errors/` 用例不被 `run_tests.py` 收集（该脚本期望全部 PASS），尚未接入统一负向 gate | 见 [security-review-2026-10.md](security-review-2026-10.md) |
| 形式化验证未开展 | 并发内核仅靠 ASan 与 C 断言覆盖；无 TSan / loom / TLA+ 等形式化或强数据竞争验证 | core-runtime.md §5 |
| Freestanding QEMU 引导层 | CI（Linux + qemu）负责 L1–L6 全链路；缺少 qemu/llvm-nm 的开发机只能跑编译层 | 见 [freestanding.md](freestanding.md) |

---

## 维护约定

1. 每轮修复使某条限制成立的前提消失时，**直接从本页删除对应行**，不要保留"曾经不支持"的陈旧描述。
2. 新增已知限制先登记到本页对应分类，再在相关教程就近加一句提示；避免同一缺口散落在多处、口径漂移。
3. 每条尽量给出：现象、编译期/运行期行为、推荐替代、可复核的源码或测试位置。
