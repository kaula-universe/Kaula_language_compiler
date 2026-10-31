# kaulafmt（已并入 `kaulac fmt`，本文仅作迁移说明）

> 自格式化入口统一为 `kaulac fmt` 后，**不再单独构建 `kaulafmt` 二进制**；
> `compiler/cmd/kaulafmt/` 目录仅保留历史源码壳，不作为交付产物。
> 完整用法、规则、限制与构建方式以 [formatter.md](formatter.md) 为准。

## 迁移对照

| 旧写法（已废弃） | 现行写法 |
|---|---|
| `kaulafmt main.kl`（格式化结果输出到 stdout 预览） | `kaulac fmt file main.kl` |
| `kaulafmt -w main.kl`（写回原文件） | `kaulac fmt file main.kl --write` |
| （无）检查文件是否已格式化 | `kaulac fmt check main.kl` |
| （无）从 stdin 读入 | `kaulac fmt stdin` |

格式化规则、失败即退出护栏（不输出、不落盘）、实现位置等，统一见
[formatter.md](formatter.md)。
