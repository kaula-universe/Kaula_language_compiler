# Kaula Programming Language

<div align="center">

<img src="logo.png" alt="Kaula Logo" width="200">

**A more modern, more usable C**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/runtime-C-A8B9CC.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))

</div>

---

## What is Kaula

Kaula is a **statically typed systems programming language** that compiles to C, producing native binaries via Clang.

Core design goal: **Rust-level safety with simpler syntax** — through SOR (Sub-structural Ownership), Kaula eliminates data races and dangling pointers at compile time, with zero runtime overhead.

---

## Quick Examples

### Hello World

```kaula
import std.io

fn main() {
    println("Hello, Kaula!")
}
```

### Variables and Types

```kaula
i32 x = 42               # explicit type
auto y = 3.14             # type inference
string name = "Kaula"
bool flag = true
i32* ptr = &x             # pointer
i32? maybe = null         # nullable type
const MAX = 1024          # compile-time constant
```

### Functions and Generics

```kaula
fn add(i32 a, i32 b) i32 {
    return a + b
}

# generic function
fn first<T>(T a, T b) T {
    return a
}

# inline assembly
#[asm]
fn read_cr0() u64 {
    mov %cr0, %rax
}
```

### Control Flow

```kaula
# if/else
if x > 0 {
    println("positive")
} else if x == 0 {
    println("zero")
} else {
    println("negative")
}

# while
while x > 0 {
    x = x - 1
}

# for
for (i32 i = 0; i < 10; i = i + 1) {
    println(i)
}

# switch
switch x {
    case 1:
        println("one")
    case 2:
        println("two")
    default:
        println("other")
}
```

### struct and class

```kaula
# struct - value type
#[packed]
struct PacketHeader {
    u8 version
    u8 flags
    u16 length
}

# class - reference semantics, supports methods and constructors
class Vec2 {
    f64 x
    f64 y

    constructor(f64 x, f64 y) {
        self.x = x
        self.y = y
    }

    fn length() f64 {
        return math_sqrt(self.x * self.x + self.y * self.y)
    }
}

# interface
interface Printable {
    fn to_string() string
}
```

### enum and match

```kaula
# algebraic data types
enum Option<T> {
    Some(T)
    None
}

enum Result<T, E> {
    Ok(T)
    Err(E)
}

# pattern matching
auto result = fetch_data()
match result {
    Ok(data) => {
        println(data)
    }
    Err(e) => {
        println("error: " + e)
    }
    _ => {
        println("unknown")
    }
}
```

### SOR (Sub-structural Ownership)

Kaula's core safety mechanism — SOR tracks resource ownership at compile time with no GC or reference counting overhead:

```kaula
#[sor]
fn process() {
    auto buf = std.memory.kmm_v4_alloc(1024)

    # yield: transfer ownership, original variable becomes inaccessible
    yield buf -> owner

    # extract: extract sub-structural ownership from a collection
    extract owner[0] -> first_byte

    # release: distribute ownership to multiple holders
    release owner -> [holder_a, holder_b]
}
```

Three primitives:
- **yield** — ownership transfer (move semantics)
- **extract** — sub-structure extraction (partial ownership from composite types)
- **release** — ownership distribution (split one resource among multiple holders)

Enable with: `--sor` for global, or `#[sor]` per-function.

### Prefix System

Kaula's unique declarative code reuse mechanism:

```kaula
# define a prefix template
prefix Logger {
    $message
    $level
    auto log = $level + ": " + $message
    println(log)
}

# invoke prefix with @
@Logger(message = "server started", level = "INFO")

# $ references parameters inside prefix
prefix Validate {
    $value
    $min
    if $value < $min {
        println("validation failed")
    }
}

@Validate(value = age, min = 0)
```

### Spend/Call Consuming Traversal

Structured per-element processing of collections:

```kaula
auto items = [10, 20, 30]
spend(items) {
    call(1) {
        println("first")
    }
    call(2) {
        println("second")
    }
    call(3) {
        println("third")
    }
}
```

### Attribute Annotations

```kaula
#[packed]                         # compact memory layout
#[aligned(16)]                    # alignment requirement
#[section(".isr_vector")]         # link to specific section
#[volatile]                       # volatile semantics
#[naked]                          # naked function (no prologue/epilogue)
#[inline]                         # inline hint
#[no_kmm]                         # disable KMM memory management
#[deprecated]                     # mark as deprecated
#[weak]                           # weak symbol
```

### Type Reflection and Comptime

```kaula
auto sz = sizeof(PacketHeader)        # type size
auto al = alignof(PacketHeader)       # type alignment
auto off = offsetof(Packet, length)   # field offset

# compile-time expressions
comptime auto count = #field_count(MyStruct)
comptime auto name = #type_name(i32)
comptime auto kind = #type_kind(MyEnum)
```

### extern and FFI

```kaula
# declare external symbols
extern bss_start: u8

# declare external functions
extern fn boot_main() -> void

# import standard library
import std.io
import std.math
import std.string
```

### Bare-Metal Development

```kaula
#[naked]
#[section(".init")]
fn _start() {
    # direct hardware register access
    #[volatile] u32* reg = 0x40000000
    *reg = 0x01
}

# --freestanding compilation mode
# automatically links kaula_freestanding_runtime.c
# provides memset/memcpy/memmove and minimal runtime
```

---

## Standard Library

62 modules, 800+ functions, covering systems programming to web development:

| Category | Modules |
|----------|---------|
| **Memory** | memory (KMM V4 scoped allocator), bitset |
| **Containers** | container (Vector/LinkedList/HashMap/Stack), deque, heap, trie, graph |
| **Strings** | string, regex, unicode, template |
| **I/O** | io, fs, path |
| **Concurrency** | concurrent (threads/locks/atomics/thread pool/Channel/Future), async, parallel |
| **Networking** | net, web, ssh, tls |
| **Serialization** | json, toml, xml, protobuf, msgpack, serialize |
| **Math** | math, cmath, decimal, random |
| **Encoding** | encoding, crypto, compress, archive |
| **System** | system, subprocess, cli, time, datetime, calendar |
| **Types** | option, traits, base, error |
| **Runtime** | vo, prefix, task, format, logging, testing, i18n |
| **GUI** | gui (Nuklear bindings) |

---

## Compiler

### Installation

```bash
cd kaula-compiler
go build -o kaulac.exe cmd/kaulac/main.go
```

Dependencies: Go 1.21+, Clang

### Compilation Pipeline

```
Source .kl → Lexing → Parsing → Semantic Analysis → C Code Generation → Clang → Executable
```

Supports multi-stage concurrent compilation and SHA256-based incremental caching.

### Usage

```bash
kaulac [options] <source.kl>

kaulac program.kl              # compile
kaulac --sor program.kl        # enable SOR ownership analysis
kaulac --release program.kl    # release mode (-O3)
kaulac --freestanding program.kl  # bare-metal mode
kaulac --sourcemap program.kl  # generate source map
kaulac --no-cache program.kl   # disable cache
```

Common options:

| Option | Description |
|--------|-------------|
| `--sor` | Enable SOR compile-time ownership analysis |
| `--release` | Release mode (-O3) |
| `--freestanding` | Bare-metal mode (no OS dependencies) |
| `--opt <level>` | Optimization level O0/O1/O2/O3 |
| `--sourcemap` | Generate source map |
| `--no-cache` | Disable incremental compilation |
| `--output-format <fmt>` | Output format elf/bin/obj |

Full option list and kaula.json configuration in [docs/](docs/).

---

## Project Structure

```
kaula/
├── kaula-compiler/          # Compiler (Go)
│   ├── cmd/kaulac/          # Compiler CLI
│   ├── internal/
│   │   ├── lexer/           # Lexer
│   │   ├── parser/          # Parser (recursive descent)
│   │   ├── sema/            # Semantic analysis (two-pass, generics, SOR)
│   │   ├── codegen/         # C code generation
│   │   ├── sor/             # SOR ownership analysis engine
│   │   ├── symbol/          # Symbol table
│   │   ├── stdlib/          # Standard library config
│   │   └── ...
│   └── stdlib.json
├── src/                     # Runtime (C)
│   ├── kaula.h              # Cross-platform header
│   ├── kmm_scoped_allocator_v4.h  # KMM V4 memory management
│   └── ...
├── std/                     # Standard library (C, 62 modules)
├── pkglib/                  # Third-party libs (stb_image, nuklear, zlib)
└── docs/                    # Compiler documentation
```

---

## License

[Apache License 2.0](LICENSE)
