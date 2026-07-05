/**
 * sor_runtime.h - SOR (Sub-structural Ownership) Runtime
 *
 * Design Philosophy: Zero overhead in release mode.
 *   - All safety checks done at compile-time by SOR analyzer (Stage 2.5)
 *   - Runtime only executes necessary data operations (assignment + zeroing)
 *   - release DAG verified by compiler, zero runtime checks
 *   - Scope exit batch reclamation relies on KMM bump allocator
 *   - DEBUG mode provides optional safety assertion layer
 */

#ifndef SOR_RUNTIME_H
#define SOR_RUNTIME_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ============== Compile-time Config ============== */

#ifndef SOR_RUNTIME_DEBUG
#define SOR_RUNTIME_DEBUG 0
#endif

/* ============== Platform Detection ============== */

#if defined(__GNUC__) || defined(__clang__)
  #define SOR_LIKELY(x)   __builtin_expect(!!(x), 1)
  #define SOR_UNLIKELY(x) __builtin_expect(!!(x), 0)
  #define SOR_ALWAYS_INLINE __attribute__((always_inline)) inline
  #define SOR_WEAK __attribute__((weak))
#else
  #define SOR_LIKELY(x)   (x)
  #define SOR_UNLIKELY(x) (x)
  #define SOR_ALWAYS_INLINE inline
  #define SOR_WEAK
#endif

/* ============== DEBUG-only Types (completely removed in release) ============== */

#if SOR_RUNTIME_DEBUG

#ifndef SOR_MAX_HOLDERS
#define SOR_MAX_HOLDERS 64
#endif

#ifndef SOR_MAX_RELEASE_EDGES
#define SOR_MAX_RELEASE_EDGES 1024
#endif

#ifndef SOR_MAX_SCOPES
#define SOR_MAX_SCOPES 128
#endif

typedef enum {
    SOR_OWNED    = 0,
    SOR_RELEASED = 1,
    SOR_MOVED    = 2,
    SOR_HOLLOW   = 3
} sor_state_t;

typedef struct {
    uint32_t source_id;
    uint32_t holder_id;
    uint32_t scope_frame;
    uint8_t  active;
} sor_release_edge_t;

typedef struct {
    uint32_t frame_id;
    uint32_t parent_id;
    uint32_t owned_count;
    uint32_t edge_start;
    uint32_t edge_count;
    uint8_t  active;
} sor_scope_frame_t;

typedef struct {
    sor_release_edge_t edges[SOR_MAX_RELEASE_EDGES];
    uint32_t           edge_count;
    sor_scope_frame_t  scopes[SOR_MAX_SCOPES];
    uint32_t           scope_top;
    uint32_t           scope_depth;
    uint32_t           next_id;
    uint32_t           stat_yeide;
    uint32_t           stat_release;
    uint32_t           stat_extract;
    uint32_t           stat_scope_enter;
    uint32_t           stat_scope_exit;
    uint8_t            inited;
} sor_ctx_t;

/* ============== DEBUG-only Runtime API ============== */

SOR_WEAK void     sor_init(sor_ctx_t* ctx);
SOR_WEAK void     sor_destroy(sor_ctx_t* ctx);
SOR_WEAK uint32_t sor_alloc_id(sor_ctx_t* ctx);
SOR_WEAK void     sor_yeide_mark(sor_ctx_t* ctx);
SOR_WEAK void     sor_release_edge_add(sor_ctx_t* ctx, uint32_t source, uint32_t holder);
SOR_WEAK void     sor_scope_enter(sor_ctx_t* ctx);
SOR_WEAK void     sor_scope_exit(sor_ctx_t* ctx);
SOR_WEAK void     sor_extract_mark(sor_ctx_t* ctx);
SOR_WEAK void     sor_stats(const sor_ctx_t* ctx, uint32_t* yeide, uint32_t* release, uint32_t* extract, uint32_t* depth);

#else

/* ============== Release Mode: Empty type definitions (zero size) ============== */

typedef void sor_ctx_t;

#endif

/* ============== Core Macros (ZERO function call overhead, always present) ============== */

#define SOR_YEIDE(type, src, dst) \
    do { (dst) = (src); (src) = (type)0; } while(0)

#define SOR_YEIDE_PTR(src, dst) \
    do { (dst) = (src); (src) = NULL; } while(0)

#define SOR_EXTRACT(type, base, idx, target) \
    do { (target) = (base)[(idx)]; (base)[(idx)] = (type)0; } while(0)

#define SOR_EXTRACT_PTR(base, idx, target) \
    do { (target) = (base)[(idx)]; (base)[(idx)] = NULL; } while(0)

#define SOR_EXTRACT_FIELD(type, base, field, target) \
    do { (target) = (base).field; (base).field = (type)0; } while(0)

#define SOR_RELEASE_COPY(type, src, holder) \
    do { (holder) = (src); } while(0)

#define SOR_RELEASE_PTR(src, holder) \
    do { (holder) = (src); } while(0)

/* ============== Union Release Macros (编译期选举, 零运行时) ============== */

#define SOR_UNION_RELEASE_ELECTED(type, src, elected) \
    type* elected = &(src)

#define SOR_UNION_RELEASE_READER(type, src, reader) \
    const type* reader = &(src)

#define SOR_RELEASE_ZEROCOPY(type, src, holder) \
    const type* holder = &(src)

#define SOR_RELEASE_VALUECOPY(type, src, holder) \
    type holder = (src)

/* ============== Debug Checks (compile to nothing in release) ============== */

#if SOR_RUNTIME_DEBUG
#define SOR_NULL_CHECK(ptr, label)  if (SOR_UNLIKELY((ptr) == NULL)) { goto label; }
#define SOR_HOLLOW_CHECK(type, base, idx, label) if (SOR_UNLIKELY((base)[(idx)] == (type)0)) { goto label; }
#else
#define SOR_NULL_CHECK(ptr, label)       ((void)0)
#define SOR_HOLLOW_CHECK(type, base, idx, label) ((void)0)
#endif

#ifdef __cplusplus
}
#endif

#endif
