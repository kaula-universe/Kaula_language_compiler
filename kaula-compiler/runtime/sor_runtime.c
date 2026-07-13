/**
 * sor_runtime.c - SOR Runtime Implementation
 *
 * All functions here are for DEBUG mode validation only.
 * In release mode, this file compiles to zero bytes.
 *
 * SOR guarantees compile-time safety:
 *   - yield: ownership transfer (assignment + zeroing)
 *   - release: read-only distribution (DAG verified at compile-time)
 *   - extract: sub-structure ownership (hollow state at compile-time)
 *
 * Release mode: All runtime functions are completely stripped by linker.
 * Debug mode: Optional safety assertion layer for development.
 */

#include "sor_runtime.h"

#if SOR_RUNTIME_DEBUG

#include <string.h>

void sor_init(sor_ctx_t* ctx) {
    if (!ctx) return;
    memset(ctx, 0, sizeof(sor_ctx_t));
    ctx->inited = 1;
}

void sor_destroy(sor_ctx_t* ctx) {
    if (!ctx || !ctx->inited) return;
    while (ctx->scope_top > 0) {
        sor_scope_exit(ctx);
    }
    memset(ctx, 0, sizeof(sor_ctx_t));
}

uint32_t sor_alloc_id(sor_ctx_t* ctx) {
    if (!ctx || !ctx->inited) return 0;
    return ++ctx->next_id;
}

void sor_yield_mark(sor_ctx_t* ctx) {
    if (!ctx || !ctx->inited) return;
    ctx->stat_yield++;
}

void sor_release_edge_add(sor_ctx_t* ctx, uint32_t source, uint32_t holder) {
    if (!ctx || !ctx->inited) return;
    if (ctx->edge_count >= SOR_MAX_RELEASE_EDGES) return;

    sor_release_edge_t* e = &ctx->edges[ctx->edge_count];
    e->source_id   = source;
    e->holder_id   = holder;
    e->scope_frame = ctx->scope_top;
    e->active      = 1;
    ctx->edge_count++;
    ctx->stat_release++;
}

void sor_scope_enter(sor_ctx_t* ctx) {
    if (!ctx || !ctx->inited) return;
    if (ctx->scope_top >= SOR_MAX_SCOPES) return;

    sor_scope_frame_t* f = &ctx->scopes[ctx->scope_top];
    f->frame_id     = ctx->scope_depth;
    f->parent_id    = (ctx->scope_top > 0) ? ctx->scope_top - 1 : 0;
    f->owned_count  = 0;
    f->edge_start   = ctx->edge_count;
    f->edge_count   = 0;
    f->active       = 1;

    ctx->scope_top++;
    ctx->scope_depth++;
    ctx->stat_scope_enter++;
}

void sor_scope_exit(sor_ctx_t* ctx) {
    if (!ctx || !ctx->inited) return;
    if (ctx->scope_top == 0) return;

    ctx->scope_top--;
    ctx->scope_depth--;

    sor_scope_frame_t* f = &ctx->scopes[ctx->scope_top];
    f->active = 0;

    for (uint32_t i = f->edge_start; i < f->edge_start + f->edge_count; i++) {
        ctx->edges[i].active = 0;
    }

    ctx->stat_scope_exit++;
}

void sor_extract_mark(sor_ctx_t* ctx) {
    if (!ctx || !ctx->inited) return;
    ctx->stat_extract++;
}

void sor_stats(const sor_ctx_t* ctx, uint32_t* yield, uint32_t* release,
                uint32_t* extract, uint32_t* depth) {
    if (!ctx) return;
    if (yield)   *yield   = ctx->stat_yield;
    if (release) *release = ctx->stat_release;
    if (extract) *extract = ctx->stat_extract;
    if (depth)   *depth   = ctx->scope_depth;
}

#endif
