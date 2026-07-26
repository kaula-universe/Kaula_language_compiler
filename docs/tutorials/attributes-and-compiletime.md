# 属性注解与编译期反射

## 属性注解

使用 `#[name]` 语法为声明或表达式添加注解。

### 声明级属性

```kaula
#[packed]                         # 紧凑内存布局（无填充）
struct PacketHeader {
    u8 version,
    u8 flags,
    u16 length
}

#[aligned(16)]                    # 16 字节对齐
struct AlignedBuf {
    data: [64]u8
}

#[section(".isr_vector")]         # 链接到指定段
#[naked]                          # 裸函数（无 prologue/epilogue）
fn vector_handler() void {
    // raw asm body
}

#[inline]                         # 内联提示
fn fast_path() int { return 42 }

#[deprecated]                     # 标记弃用
fn old_api() { }

#[weak]                           # 弱符号
fn fallback() int { return -1 }

#[no_kmm]                         # 禁用 KMM 内存管理（回退到系统 malloc）

#[volatile]                       # volatile 语义
u32* status_reg = 0x40000000
```

### #[no_kmm] 详解

`#[no_kmm]` 属性在特定函数中禁用 KMM V4 内存管理：

- 函数内不插入 `kmm_v4_scope_push()`/`kmm_v4_scope_pop()`
- `std_malloc` 不会被重写为 `kmm_v4_alloc_auto`，保持系统 malloc 调用
- 适用于需要手动管理内存生命周期的场景（如 C 库互操作）

```kaula
#[no_kmm]
fn manual_memory() void* {
    // 使用系统 malloc，不受 KMM 作用域管理
    auto ptr = std.memory.std_malloc(1024)
    // ... 需要手动 std_free ...
    return ptr
}

fn kmm_managed() {
    // 默认 KMM 管理，函数退出自动回收
    auto ptr = std.memory.std_malloc(1024)
    // 无需 free
}
```

### 表达式级属性

```kaula
auto cr3 = #[asm("mov %cr3, %rax")]     # 内联汇编
#[fence()]                                # 内存屏障
```

## 编译期反射

使用 `comptime` 关键字在编译期获取类型信息：

```kaula
# 类型大小与对齐
auto sz = sizeof(PacketHeader)       # 类型大小
auto al = alignof(PacketHeader)      # 类型对齐
auto off = offsetof(Packet, length)  # 字段偏移

# 类型信息
comptime auto name = type_name(i32)        # "i32"
comptime auto kind = type_kind(Packet)     # "struct"
comptime auto count = field_count(Point)   # 字段数量
comptime auto fname = field_name(Point, 0) # 第 0 个字段名
comptime auto ftype = field_type(Point, 1) # 第 1 个字段类型

# 编译期表达式计算
comptime auto val = 1 + 2 * 3
```

`comptime` 确保表达式在编译期求值，可用于数组大小等场景。

## 完整示例

参见 [examples/attributes.kl](examples/attributes.kl)。
