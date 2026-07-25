# SOR 子结构所有权

SOR（Sub-structural Ownership Release）是 Kaula 独特的编译期内存安全机制，
追踪每个资源的**唯一所有权**，在编译期消除数据竞争和悬垂指针，零运行时开销。

> 启用方式：全局 `--sor` 参数，或单函数 `#[sor]` 注解。

## 三大原语

### yield — 所有权转移

将资源的所有权从一个变量转移到另一个，原变量不再可用（move 语义）：

```kaula
#[sor]
fn transfer() {
    auto buf = alloc(1024)

    yield buf -> owner       # 所有权转移到 owner
    # buf 不再可用
    use(owner)               # ✅ 正确
}
```

### extract — 子结构提取

从复合类型中提取出某个子元素的所有权，原位置留下空位：

```kaula
#[sor]
fn split() {
    auto arr = alloc_array(10)

    extract arr[0] -> first   # 取出第 0 个元素
    extract arr[1] -> second  # 取出第 1 个元素

    use(first)
    use(second)
}
```

### release — 所有权分发

将一个资源的只读访问权分发给多个持有者（必须形成 DAG，无环）：

```kaula
#[sor]
fn share() {
    auto data = compute()

    release data -> [reader1, reader2]   # 两个只读持有者
    # 所有持有者都可读
    reader1.process()
    reader2.process()
}
```

## 运行时支持

SOR 在 debug 模式下会插入安全断言检查；release 模式下完全消除。

```kaula
#[sor]
fn demo() {
    auto resource = Resource()
    yield resource -> r1
    # yield resource -> r2   # 编译错误：resource 已 move
}
```

## 完整示例

参见 [examples/sor_demo.kl](examples/sor_demo.kl)。
