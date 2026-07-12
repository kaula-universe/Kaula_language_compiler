# Kaula Programming Language

<div align="center">

<img src="logo.png" alt="Kaula Logo" width="200">

**High-performance, system-level compiled programming language**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21.0-00ADD8.svg?logo=go)](https://go.dev/)
[![Language](https://img.shields.io/badge/language-C-C.svg?logo=c)](https://en.wikipedia.org/wiki/C_(programming_language))
[![Version](https://img.shields.io/badge/version-0.1.0--alpha-orange.svg)](https://github.com/yourusername/kaula/releases)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)]()

</div>

---

## 📖 Project Overview

Kaula is a statically typed compiled programming language featuring a **Go-based compiler** and a **C-based runtime system**.

> ⚠️ **Note**: Current version is an **Alpha Preview**, primarily supporting Windows OS
> 
> 🎯 **Release Status**: v0.1.0-alpha - Core features implemented, under rapid iteration

---

## 🏗️ Project Architecture

```
kaula/
├── kaula-compiler/          # Compiler (implemented in Go)
│   ├── cmd/kaulac/          # Compiler CLI tool
│   │   └── main.go          # Main entry (concurrent compilation pipeline, cache management, timeout control)
│   ├── internal/
│   │   ├── ast/             # Abstract syntax tree definitions
│   │   │   └── ast.go
│   │   ├── cache/           # Incremental compilation cache system
│   │   │   └── cache.go     # Cache manager (SHA256 verification, atomic writes, manifest management)
│   │   ├── codegen/         # C code generator (modular architecture)
│   │   │   ├── codegen.go   # Core generator
│   │   │   ├── generator.go # Generator interface definitions
│   │   │   ├── typegen.go   # Type generation
│   │   │   ├── funcgen.go   # Function generation
│   │   │   ├── exprgen.go   # Expression generation
│   │   │   ├── stmtgen.go   # Statement generation
│   │   │   ├── template.go  # Template management
│   │   │   └── plugin.go    # Plugin system
│   │   ├── compiler/        # Compiler core logic
│   │   ├── config/          # Configuration management
│   │   ├── core/            # Core runtime features (Go layer)
│   │   │   ├── vo.go        # VO system
│   │   │   ├── spendcall.go # Spend/Call mechanism
│   │   │   ├── prefix.go    # Prefix system
│   │   │   ├── tree.go      # Tree system
│   │   │   └── task.go      # Task scheduling
│   │   ├── errors/          # Error handling
│   │   ├── lexer/           # Lexer
│   │   ├── parser/          # Parser (recursive descent parsing)
│   │   ├── sema/            # Semantic analyzer (two-pass analysis, generics support)
│   │   ├── semantic/        # Semantic analysis (extended)
│   │   ├── stdlib/          # Standard library configuration loader
│   │   ├── symbol/          # Symbol table
│   │   ├── test/            # Testing utilities
│   │   └── timeout/         # Timeout control (memory, time limits)
│   ├── templates/           # Code generation templates
│   │   └── main.c.tmpl
│   ├── stdlib.json          # Standard library function signature definitions (43 modules)
│   └── go.mod
├── pkglib/                  # Third-party library auto-loader
│   ├── stb_image/
│   ├── nuklear/
│   └── zlib/
├── src/                     # Runtime system (implemented in C)
│   ├── kaula.h              # Core header (cross-platform macros, type definitions)
│   ├── platform.h           # Platform detection
│   ├── kmm_scoped_allocator_v4.h # V4 memory management header
│   ├── kmm_scoped_allocator.c   # KMM V4 scoped allocator
│   ├── allocator.c          # Fast allocator
│   ├── vo.c                 # VO system
│   ├── spend_call.c         # Spend/Call mechanism
│   ├── queue.c              # Priority queue
│   ├── prefix_system.c      # Prefix system
│   └── tree_system.c        # Tree system
└── std/                     # Standard library (implemented in C, 62 modules)
    ├── algorithm/           # Algorithms (sorting, searching, traversal, comparison)
    ├── async/               # Async operations (event loop, coroutines, I/O)
    ├── base/                # Basic type conversion and comparison
    ├── cli/                 # Command line argument parsing
    ├── compress/            # Compression algorithms (Deflate, Gzip)
    ├── concurrent/          # Concurrency primitives (threads, locks, atomics, thread pool)
    ├── container/           # Containers (Vector, LinkedList, HashMap, Stack)
    ├── crypto/              # Cryptography (MD5, SHA256, Base64, CRC32, HMAC)
    ├── db/                  # Database interface
    ├── encoding/            # Encoding conversion (Base64, Hex, URL encoding)
    ├── error/               # Error handling
    ├── format/              # Formatting (printf, FormatBuilder)
    ├── fs/                  # File system operations (file tree traversal, path operations)
    ├── gui/                 # GUI support (Nuklear bindings)
    ├── i18n/                # Internationalization (multi-language, encoding conversion, UTF-8)
    ├── io/                  # I/O operations (console, files)
    ├── json/                # JSON parsing and serialization
    ├── logging/             # Logging system
    ├── math/                # Math functions (standard math library, random numbers)
    ├── memory/              # Memory management (KMM V4, aligned allocation, bulk allocation)
    ├── net/                 # Network programming (TCP/UDP, DNS resolution)
    ├── option/              # Option/Result types
    ├── path/                # Path handling (normalization, joining, extension, filename operations)
    ├── prefix/              # Prefix system interface
    ├── regex/               # Regular expressions (NFA implementation)
    ├── serialize/           # Serialization (binary, text)
    ├── string/              # String processing
    ├── system/              # System calls (processes, files, environment, network)
    ├── task/                # Task scheduling (priority queue)
    ├── testing/             # Unit testing framework
    ├── time/                # Time measurement
    ├── toml/                # TOML configuration parsing
    ├── traits/              # Type traits
    ├── unicode/             # Unicode support (UTF-8/UTF-16 conversion, character properties)
    ├── vo/                  # VO system interface
    ├── web/                 # HTTP server/client, URL processing
    ├── xml/                 # XML parsing
    ├── graph/               # Graph data structure (BFS, DFS, Dijkstra, Bellman-Ford, topological sort)
    ├── heap/                # Heap data structure (min-heap, heap sort, merge, K-way merge)
    ├── trie/                # Trie data structure (insert, search, prefix search)
    ├── datetime/            # Date/time handling (timestamp conversion, ISO 8601, timezone support)
    ├── calendar/            # Calendar operations (leap year detection, weekday calculation, date arithmetic)
    ├── protobuf/            # Protocol Buffers binary serialization (Varint encoding)
    ├── msgpack/             # MessagePack binary serialization (dynamic typing)
    ├── parallel/            # Parallel computing (parallel for, parallel reduce, parallel sort)
    ├── tls/                 # TLS encryption protocol (connection, handshake, encrypted I/O)
    ├── ssh/                 # SSH protocol (session management, channel operations, remote execution)
    ├── random/              # Random number generator (XorShift128+, range random, UUID)
    ├── cmath/               # Complex math operations (arithmetic, trigonometric, exponential, logarithmic)
    ├── decimal/             # High-precision decimal (arbitrary precision, arithmetic, comparison)
    ├── bitset/              # Bit set (bit operations, bitwise operations, bit search)
    ├── deque/               # Double-ended queue (push/pop from both ends, random access)
    ├── ipaddress/           # IP address handling (IPv4/IPv6, CIDR, network checking)
    ├── subprocess/          # Subprocess management (creation, pipes, environment variables)
    ├── archive/             # Archive handling (Tar/Zip, compression/decompression)
    └── template/            # Template engine (variable substitution, filters)
```

---

## ✨ Core Features

### 1. Compiler

The Kaula compiler is implemented in **Go 1.21+** and includes the following core components:

- **Lexer**: State machine implementation supporting keywords, identifiers, literals, strings, annotations (`#[...]`), prefix references (`$`), prefix calls (`@`), etc.
- **Parser**: Iterative recursive descent parsing to build the abstract syntax tree
- **Semantic Analyzer**: Two-pass analysis (symbol collection → function body analysis), symbol table management, type checking, scope validation, generic constraints
- **Code Generator**: Generates C code based on templates with modular design (separate type/function/expression/statement generators), supports generic instantiation caching
- **Incremental Compilation Cache System**: SHA256-based cache verification, atomic writes, manifest management, automatic cleanup mechanism
- **Generic System**: Supports generic function instantiation and caching, automatically avoids naming conflicts (`kaula_` prefix)

**Compilation Pipeline**:
```
Source Code → Lexing → Parsing → Semantic Analysis → Code Generation → C Code Cache → Clang Compilation → Executable
```

**Concurrent Compilation**: The compiler supports multi-stage parallel processing (lexing/parsing, semantic analysis, code generation, C compilation)

**Memory and Timeout Control**: Built-in memory monitoring and timeout protection to prevent compiler resource exhaustion

### 2. Runtime System

The runtime is implemented in **C** and provides the following core functionality:

#### KMM V4 ScopedAllocator (Scoped Memory Management)

Hierarchical memory management system based on V4 architecture:
- Supports Arena hierarchical allocation
- ThreadCache thread-local caching (atomic operations, lightweight real-time thread safety)
- SafeAlloc safe allocation
- Cleanup Stack automatic cleanup stack
- Union Domain management
- O(1) bulk release, automatic cleanup on scope exit

#### VO (Virtual On-site) System

Efficient data and code caching mechanism:

```kaula
vo create(100)              # Create VO module
vo_data_load(vo, 1, data)   # Load data
vo_code_load(vo, -1, fn)    # Load code
vo_associate(vo, 1, -1)     # Associate data and code
result = vo_access(vo, 1)   # Access (auto-execute code)
```

#### Spend/Call Mechanism

Dynamic component management:

```kaula
spend(component1, component2):
    call target1:
        # Processing logic
    call target2:
        # Processing logic
```

#### Three-Level Priority Queue

Task scheduling system:
- Priority 0 (HIGH) - High priority tasks
- Priority 1 (MEDIUM) - Normal tasks
- Priority 2 (LOW) - Low priority tasks

#### Cross-Platform Support

`kaula.h` provides cross-platform macro definitions:
- Windows / Linux / macOS platform detection
- GCC / Clang / MSVC compiler detection
- Thread Local Storage (TLS) support
- Atomic operations support (C11 or GCC builtins)

### 3. Standard Library

Provides over **700+** standard functions including:

| Module | Features |
|--------|----------|
| **base** | Type conversion, comparison, type checking |
| **memory** | KMM V4, fast allocator, memory pool, aligned allocation, bulk allocation |
| **string** | String creation, manipulation, search, replacement, regular expressions |
| **io** | Console I/O, file operations, path handling |
| **math** | Math functions, trigonometry, random numbers |
| **container** | Vector, LinkedList, HashMap, Stack |
| **concurrent** | Threads, mutexes, condition variables, semaphores, read-write locks, atomics, thread pool |
| **async** | Async tasks, event loops, coroutines, async I/O |
| **system** | System info, process management, environment variables, file system |
| **task** | Task creation, priority queue scheduling |
| **vo** | VO system interface |
| **prefix** | Prefix system interface |
| **error** | Error handling, error types, error printing |
| **format** | Formatted output, FormatBuilder |
| **time** | Time measurement, datetime conversion |
| **i18n** | Internationalization, multi-language support, encoding conversion |
| **gui** | GUI support (Nuklear bindings) |
| **web** | HTTP server/client, URL processing, MIME types |
| **json** | JSON parsing, serialization, deserialization |
| **crypto** | MD5, SHA256, Base64, CRC32, HMAC |
| **net** | TCP/UDP sockets, DNS resolution |
| **toml** | TOML configuration parsing |
| **xml** | XML parsing |
| **logging** | Logging system |
| **testing** | Unit testing framework |
| **windows** | Windows-specific features (registry, process info) |
| **syscall** | System call interface |
| **algorithm** | Sorting, searching, traversal, comparison algorithms |
| **cli** | Command line argument parsing |
| **compress** | Compression algorithms (Deflate, Gzip) |
| **db** | Database interface |
| **encoding** | Encoding conversion (Base64, Hex, URL encoding) |
| **fs** | File system operations (file tree traversal, path operations) |
| **option** | Option/Result types |
| **path** | Path handling (normalization, joining, extension, filename operations) |
| **regex** | Regular expressions (NFA implementation) |
| **serialize** | Serialization (binary, text) |
| **traits** | Type traits |
| **unicode** | Unicode support (UTF-8/UTF-16 conversion, character properties)

---

## 🛠️ Compiler Toolchain

### kaulac CLI Usage

```bash
# Basic usage
kaulac.exe [options] <source_file.kl>

# Compile a single file
kaulac.exe program.kl

# Compile with incremental compilation cache enabled
kaulac.exe program.kl

# Disable cache and force recompile
kaulac.exe --no-cache program.kl

# View cache statistics
kaulac.exe --cache-stats

# Clean expired cache entries (older than 7 days)
kaulac.exe --clean-cache

# Purge all cache
kaulac.exe --purge-cache
```

### Command Line Options

| Option | Description |
|--------|-------------|
| `--no-cache` | Disable incremental compilation, force recompile |
| `--cache-stats` | Display cache statistics (entries, size, time range) |
| `--clean-cache` | Clean expired cache entries (older than 7 days) |
| `--purge-cache` | Purge all cache |
| `-template <path>` | Specify code generation template path (default: templates) |
| `-include <path>` | Specify C header include path (default: ../std) |
| `-target <lang>` | Specify target language (default: c) |
| `-vo-cache <size>` | Set VO cache size (default: 2048) |
| `-queue <size>` | Set queue size (default: 100) |
| `-spendable <size>` | Set spendable component size (default: 10) |

### Compilation Pipeline

```
┌─────────────┐
│ Source.kl    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Lexing      │ 6ms
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Parsing     │ (concurrent)
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Semantic    │ 3ms
│ Analysis    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Code        │ Generate C code
│ Generation  │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Clang       │ 2.6s
│ Compilation │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Executable   │
└─────────────┘
```

**Typical Compilation Time**: ~2.9s (first compile) / ~2.6s (cache hit)

### Incremental Compilation

The compiler supports intelligent incremental compilation through a caching mechanism to accelerate recompilation:

```bash
# First compilation (full pipeline)
$ kaulac.exe main.kl
[Cache] Stored cache for main.kl (362 bytes)
[Compile] Completed in 2.85s

# Second compilation (using cache)
$ kaulac.exe main.kl
[Cache] Cache hit for main.kl
[Cache] Using cached C code: cache/main.c
[Compile] Completed in 2.64s (cache hit)

# View cache statistics
$ kaulac.exe --cache-stats
=== Cache Statistics ===
Total entries: 1
Total size: 479 bytes (0.00 MB)
Oldest entry: 2026-04-26 18:32:08
Newest entry: 2026-04-26 18:32:08
```

**Cache Verification Mechanism**:
- SHA256 source file hash comparison
- File size and modification time verification
- Compiler version tracking
- Automatic invalidation mechanism
- Atomic writes (write to temp file then rename, ensuring data integrity)
- Manifest file management (records all cache entry metadata)

### Standard Library Configuration

The compiler manages standard library function signatures through `stdlib.json`:

```json
{
  "std.io": {
    "header": "std/io/io.h",
    "println": {"args": ["const char*"], "varargs": true},
    "print_int": {"args": ["i64"]},
    "file_open": {"args": ["const char*", "const char*"], "return": "File"}
  },
  "std.math": {
    "header": "std/math/math.h",
    "math_sqrt": {"args": ["f64"], "return": "f64"},
    "math_pow": {"args": ["f64", "f64"], "return": "f64"}
  }
}
```

**Purpose**:
- Validate function calls during type checking
- Automatically generate correct C function declarations
- Track module dependencies

### Third-Party Library Integration

The compiler automatically loads third-party libraries from the `pkglib/` directory:

```bash
pkglib/
├── stb_image/
│   └── stb_image.json
├── nuklear/
│   └── nuklear.json
└── zlib/
    └── zlib.json
```

**Library Configuration File Format**:

```json
{
  "name": "stb_image",
  "headers": ["\"stb_image.h\""],
  "libraries": ["stb_image.lib"],
  "functions": {
    "stbi_load": {
      "args": ["const char*", "int*", "int*", "int*", "int"],
      "return": "void*"
    }
  }
}
```

**Usage**:

```kaula
import stb_image

fn main():
    # Use stb_image to load image
    void* img = stbi_load("texture.png", &width, &height, &channels, 4)
```

### Compilation Output

```bash
$ kaulac.exe program.kl
=== Concurrent Compilation Pipeline ===
Starting at 18:32:08.619

[Stage 1] Lexing + Parsing...
[Stage 1] Lex + Parse completed in 6.0025ms

[Stage 2] Semantic Analysis...
[Stage 2] Semantic Analysis completed in 0.9999ms

[Stage 3] Code Generation + C Compilation...
[Cache] Stored cache for program.kl (362 bytes)
[Compile] Clang command args:
  cache/program.c
  -o program.exe
  -O3
  -I E:\kaula
  -I E:\kaula\src
  -I E:\kaula\std
  E:\kaula\src\kmm_scoped_allocator.c
  E:\kaula\std\io\io.c
[Compile] Successfully compiled: program.exe
[Compile] Completed in 2.8505352s

=== Compilation Results ===
Status: SUCCESS
Output: program.exe
Cache: cache/program.c

=== Timing Breakdown ===
Stage 1 (Lex + Parse):         6.0025ms
Stage 2 (Semantic):            0.9999ms
Stage 3 (Codegen+Compile):    2.8505352s
---------------------------------
Total End-to-End:              2.9200441s
```

### Error Handling

The compiler provides detailed error reports and fix suggestions:

```bash
=== Compilation Errors ===

[Lexing & Parsing Errors] (2 errors)
  1. Syntax Error at line 7, column 34: unexpected token: PLUS
     Suggestion: Check for missing or extra punctuation
  2. Syntax Error at line 7, column 35: unexpected token: RPAREN
     Suggestion: Check for missing or extra punctuation

[Semantic Analysis Errors] (1 errors)
  1. Type Error at line 12, column 5: undefined variable 'x'
     Suggestion: Declare variable before use or check scope

Total: 3 error(s)
```

### Performance Optimization Options

**Compiler Optimization**:
- `-O3`: Uses Clang's highest optimization level by default
- Concurrent compilation: Multi-stage parallel processing
- Incremental cache: Skips unchanged code generation

**Runtime Optimization**:
- KMM V4 memory management: O(1) bulk release
- VO cache system: Hot data automatic caching
- Priority queue: Task scheduling optimization

### Debugging Tips

**1. View Generated C Code**:
```bash
# C code is saved in cache/ directory
cat cache/program.c
```

**2. Disable Optimization for Debugging**:
```bash
# Change -O3 to -O0 or -g in the compileCCode function
```

**3. View Compiler Logs**:
```bash
# Compiler output includes detailed stage information
# Check [Stage X] and [Cache] logs
```

**4. Memory Leak Detection**:
```bash
# Compiler has built-in memory and timeout monitoring
# Automatically terminates and reports when limits are exceeded
```

### Build System

**Building the Compiler**:
```bash
cd kaula-compiler
go build -o kaulac.exe cmd/kaulac/main.go
```

**Running Tests**:
```bash
# Run compiler test suite
go test ./internal/...

# Run benchmarks
go test -bench=. ./internal/lexer
go test -bench=. ./internal/parser
go test -bench=. ./internal/codegen
```

**Cross-Compilation**:
```bash
# Windows (current platform)
GOOS=windows GOARCH=amd64 go build -o kaulac.exe

# Linux
GOOS=linux GOARCH=amd64 go build -o kaulac

# macOS
GOOS=darwin GOARCH=amd64 go build -o kaulac
```

### Project Structure Best Practices

**Recommended Directory Organization**:
```
myproject/
├── src/
│   ├── main.kl          # Main entry
│   ├── utils.kl         # Utility functions
│   └── modules/         # Module directory
├── cache/               # Compilation cache (auto-created)
├── build.bat            # Build script
└── .gitignore
    cache/
    *.exe
```

**Build Script Example** (build.bat):
```batch
@echo off
echo Building Kaula project...

REM Clean old cache (optional)
kaulac.exe --clean-cache

REM Compile main program
kaulac.exe src/main.kl

REM Check compilation result
if exist src\main.exe (
    echo Build successful!
    src\main.exe
) else (
    echo Build failed!
    exit /b 1
)
```

### FAQ

**Q: Compiler can't find clang?**
```bash
# Ensure clang is in PATH
# Windows: Install LLVM and add to system PATH
# Linux: sudo apt install clang
```

**Q: Where is the cache directory?**
```bash
# Cache is located in the cache/ subdirectory of the working directory
# You can check usage with --cache-stats
```

**Q: How to disable incremental compilation?**
```bash
# Use --no-cache option
kaulac.exe --no-cache program.kl
```

**Q: How to update the standard library?**
```bash
# Recompile after modifying stdlib.json
# The compiler automatically loads the latest configuration
```

**Q: How to adapt third-party C libraries?**

**Step 1: Create library config directory in pkglib/**
```bash
pkglib/
└── mylib/
    └── mylib.json
```

**Step 2: Write library configuration file (mylib.json)**
```json
{
  "name": "mylib",
  "headers": ["\"mylib.h\""],
  "libraries": ["mylib.lib"],
  "functions": {
    "mylib_init": {
      "args": [],
      "return": "int"
    },
    "mylib_process": {
      "args": ["const char*", "int"],
      "return": "void*"
    },
    "mylib_cleanup": {
      "args": ["void*"],
      "return": "void"
    }
  }
}
```

**Field Descriptions**:
- `name`: Library name (used in import statement)
- `headers`: C header file path list (relative to project root)
- `libraries`: List of library files to link (.lib for Windows, .a/.so for Linux)
- `functions`: Function signature definitions
  - `args`: Parameter type list (using C types)
  - `return`: Return type (using C types)

**Step 3: Use in Kaula code**
```kaula
import mylib

fn main() {
    mylib_init()
    void* result = mylib_process("data", 42)
    mylib_cleanup(result)
}
```

**Step 4: Specify library path during compilation**
```bash
# Compiler will automatically load config from pkglib/
kaulac.exe program.kl

# If additional library file path is needed
# Modify compileCCode function to add -L parameter
```

**Q: How to update third-party libraries?**

**Method 1: Update header files**
```bash
# 1. Replace pkglib/mylib/mylib.h with new version
# 2. Check if function signatures have changed
# 3. Update function definitions in pkglib/mylib/mylib.json
# 4. Recompile
kaulac.exe --no-cache program.kl
```

**Method 2: Update library files**
```bash
# 1. Replace compiled library file (mylib.lib or libmylib.a)
# 2. Ensure new version ABI compatibility
# 3. Relink program
kaulac.exe --no-cache program.kl
```

**Method 3: Version upgrade considerations**
```bash
# If third-party library has breaking changes:
# 1. Check function signature changes in header files
# 2. Update function definitions in mylib.json
# 3. Modify calling convention in Kaula code
# 4. Clear cache and recompile
kaulac.exe --purge-cache
kaulac.exe --no-cache program.kl
```

**Example: stb_image library configuration**
```json
{
  "name": "stb_image",
  "headers": ["\"stb_image.h\""],
  "libraries": [],
  "functions": {
    "stbi_load": {
      "args": ["const char*", "int*", "int*", "int*", "int"],
      "return": "void*"
    },
    "stbi_image_free": {
      "args": ["void*"],
      "return": "void"
    },
    "stbi_write_png": {
      "args": ["const char*", "int", "int", "int", "const void*", "int"],
      "return": "int"
    }
  }
}
```

**Q: How to handle dependencies of third-party libraries?**

If a third-party library depends on other libraries (e.g., zlib depends on libpng):

```json
{
  "name": "zlib",
  "headers": ["\"zlib.h\""],
  "libraries": ["zlib.lib"],
  "dependencies": ["libpng"],  // Declare dependency
  "functions": {
    "compress": {
      "args": ["void*", "unsigned long*", "const void*", "unsigned long"],
      "return": "int"
    }
  }
}
```

The compiler will automatically link all required libraries in dependency order.

---

## 📄 License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details

---

<div align="center">

**Kaula - A More Modern and Usable C**

</div>
