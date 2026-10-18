#include "compress.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

#ifdef KUALA_USE_ZLIB
#include <zlib.h>
#endif

/* ============================================================
 * RLE (Run-Length Encoding)
 *   Format: repeating pairs [count][byte], count in 1..255
 * ============================================================ */

void* rle_compress(const void* data, size_t len, size_t* out_len) {
    unsigned char* out;
    const unsigned char* src;
    size_t i, op;

    if (out_len) *out_len = 0;
    if (!data || len == 0) {
        out = (unsigned char*)kmm_v4_malloc(1);
        return out;
    }

    /* worst case: no runs -> 2 bytes per input byte */
    out = (unsigned char*)kmm_v4_malloc(2 * len + 8);
    if (!out) return NULL;

    src = (const unsigned char*)data;
    i = 0;
    op = 0;
    while (i < len) {
        unsigned char cur = src[i];
        size_t run = 1;
        while (i + run < len && src[i + run] == cur && run < 255) run++;
        out[op++] = (unsigned char)run;
        out[op++] = cur;
        i += run;
    }

    if (out_len) *out_len = op;
    return out;
}

void* rle_decompress(const void* data, size_t len, size_t* out_len) {
    const unsigned char* src;
    unsigned char* out;
    size_t total, op, i, k;

    if (out_len) *out_len = 0;
    if (!data || len == 0) {
        out = (unsigned char*)kmm_v4_malloc(1);
        return out;
    }
    if (len % 2 != 0) return NULL; /* corrupt stream */

    src = (const unsigned char*)data;
    total = 0;
    for (i = 0; i < len; i += 2) total += src[i];

    out = (unsigned char*)kmm_v4_malloc(total + 1);
    if (!out) return NULL;

    op = 0;
    for (i = 0; i < len; i += 2) {
        unsigned char count = src[i];
        unsigned char byte = src[i + 1];
        for (k = 0; k < count; k++) out[op++] = byte;
    }

    if (out_len) *out_len = op;
    return out;
}

/* ============================================================
 * LZ77 (simplified)
 *   Sliding window: 32KB
 *   Match length: 3..258
 *   Token stream:
 *     - flag byte, 8 tokens per flag byte (LSB first)
 *     - bit 0 -> literal: 1 raw byte follows
 *     - bit 1 -> match:   3 bytes follow [off_lo][off_hi][len_code]
 *                         offset = off_lo | (off_hi << 8)  (1..32768)
 *                         length = len_code + MIN_MATCH     (3..258)
 * ============================================================ */

#define LZ77_WINDOW    32768
#define LZ77_MIN_MATCH 3
#define LZ77_MAX_MATCH 258
#define LZ77_HASH_BITS 15
#define LZ77_HASH_SIZE (1 << LZ77_HASH_BITS)
#define LZ77_HASH_MASK (LZ77_HASH_SIZE - 1)

static unsigned lz77_hash(const unsigned char* p) {
    unsigned h = ((unsigned)p[0] << 10) ^ ((unsigned)p[1] << 5) ^ (unsigned)p[2];
    return h & LZ77_HASH_MASK;
}

void* lz77_compress(const void* data, size_t len, size_t* out_len, int level) {
    const unsigned char* src;
    unsigned char* out;
    size_t* head;
    size_t* prev;
    size_t cap, pos, op, flag_pos, i;
    int flag_bit, max_chain;

    if (out_len) *out_len = 0;
    if (!data || len == 0) {
        out = (unsigned char*)kmm_v4_malloc(1);
        return out;
    }

    src = (const unsigned char*)data;

    /* worst case (all literals): 1 flag byte per 8 input bytes */
    cap = len + (len / 8) + 16;
    out = (unsigned char*)kmm_v4_malloc(cap);
    if (!out) return NULL;

    head = (size_t*)kmm_v4_malloc(LZ77_HASH_SIZE * sizeof(size_t));
    prev = (size_t*)kmm_v4_malloc(len * sizeof(size_t));
    if (!head || !prev) {
        if (head) kmm_v4_free(head);
        if (prev) kmm_v4_free(prev);
        kmm_v4_free(out);
        return NULL;
    }
    for (i = 0; i < LZ77_HASH_SIZE; i++) head[i] = (size_t)-1;
    for (i = 0; i < len; i++) prev[i] = (size_t)-1;

    if (level < 0) level = 6;
    if (level == 0) max_chain = 4;
    else if (level <= 3) max_chain = 32;
    else if (level <= 6) max_chain = 128;
    else max_chain = 1024;

    pos = 0;
    op = 0;
    flag_pos = 0;
    flag_bit = 0;
    {
        unsigned char cur_flag = 0;

        while (pos < len) {
            if (flag_bit == 0) {
                flag_pos = op++;
                cur_flag = 0;
            }

            if (pos + LZ77_MIN_MATCH <= len) {
                unsigned h = lz77_hash(src + pos);
                size_t cand = head[h];
                size_t limit = (pos > LZ77_WINDOW) ? (pos - LZ77_WINDOW) : 0;
                int chain = max_chain;
                size_t best_len = 0;
                size_t best_dist = 0;
                size_t maxl = len - pos;
                if (maxl > LZ77_MAX_MATCH) maxl = LZ77_MAX_MATCH;

                while (cand != (size_t)-1 && cand >= limit && chain-- > 0) {
                    size_t l = 0;
                    while (l < maxl && src[cand + l] == src[pos + l]) l++;
                    if (l > best_len) {
                        best_len = l;
                        best_dist = pos - cand;
                        if (l >= LZ77_MAX_MATCH) break;
                    }
                    cand = prev[cand];
                }

                if (best_len >= LZ77_MIN_MATCH) {
                    cur_flag |= (unsigned char)(1 << flag_bit);
                    out[op++] = (unsigned char)(best_dist & 0xff);
                    out[op++] = (unsigned char)((best_dist >> 8) & 0xff);
                    out[op++] = (unsigned char)(best_len - LZ77_MIN_MATCH);

                    {
                        size_t end = pos + best_len;
                        while (pos < end) {
                            if (pos + LZ77_MIN_MATCH <= len) {
                                unsigned hh = lz77_hash(src + pos);
                                prev[pos] = head[hh];
                                head[hh] = pos;
                            }
                            pos++;
                        }
                    }
                } else {
                    out[op++] = src[pos];
                    {
                        unsigned hh = lz77_hash(src + pos);
                        prev[pos] = head[hh];
                        head[hh] = pos;
                    }
                    pos++;
                }
            } else {
                /* not enough bytes left for a match -> literal */
                out[op++] = src[pos++];
            }

            flag_bit++;
            if (flag_bit == 8) {
                out[flag_pos] = cur_flag;
                flag_bit = 0;
            }
        }

        if (flag_bit != 0) {
            out[flag_pos] = cur_flag;
        }
    }

    kmm_v4_free(head);
    kmm_v4_free(prev);

    if (out_len) *out_len = op;
    return out;
}

void* lz77_decompress(const void* data, size_t len, size_t* out_len) {
    const unsigned char* src;
    unsigned char* out;
    size_t cap, op, ip;

    if (out_len) *out_len = 0;
    if (!data || len == 0) {
        out = (unsigned char*)kmm_v4_malloc(1);
        return out;
    }

    src = (const unsigned char*)data;
    cap = len * 2 + 64;
    out = (unsigned char*)kmm_v4_malloc(cap);
    if (!out) return NULL;

    op = 0;
    ip = 0;
    while (ip < len) {
        unsigned char flag = src[ip++];
        int b;
        for (b = 0; b < 8 && ip < len; b++) {
            if (flag & (1 << b)) {
                unsigned dist;
                size_t mlen, from, k;
                if (ip + 3 > len) { kmm_v4_free(out); return NULL; }
                dist = (unsigned)(src[ip] | (src[ip + 1] << 8));
                mlen = (size_t)src[ip + 2] + LZ77_MIN_MATCH;
                ip += 3;
                if (dist == 0 || (size_t)dist > op) { kmm_v4_free(out); return NULL; }

                if (op + mlen > cap) {
                    unsigned char* nb;
                    while (op + mlen > cap) cap *= 2;
                    nb = (unsigned char*)kmm_v4_malloc(cap);
                    if (!nb) { kmm_v4_free(out); return NULL; }
                    memcpy(nb, out, op);
                    kmm_v4_free(out);
                    out = nb;
                }
                from = op - dist;
                for (k = 0; k < mlen; k++) {
                    out[op] = out[from + k];
                    op++;
                }
            } else {
                if (ip >= len) { kmm_v4_free(out); return NULL; }
                if (op + 1 > cap) {
                    unsigned char* nb;
                    cap *= 2;
                    nb = (unsigned char*)kmm_v4_malloc(cap);
                    if (!nb) { kmm_v4_free(out); return NULL; }
                    memcpy(nb, out, op);
                    kmm_v4_free(out);
                    out = nb;
                }
                out[op++] = src[ip++];
            }
        }
    }

    if (out_len) *out_len = op;
    return out;
}

/* ============================================================
 * Generic dispatch
 * ============================================================ */

void* compress_data(const void* data, size_t len, size_t* out_len,
                    CompressAlgorithm algo, int level) {
    switch (algo) {
        case COMPRESS_RLE:
            return rle_compress(data, len, out_len);
        case COMPRESS_LZ77:
            return lz77_compress(data, len, out_len, level);
        case COMPRESS_DEFLATE:
#ifdef KUALA_USE_ZLIB
            return deflate_compress(data, len, out_len, level);
#else
            return NULL;
#endif
        case COMPRESS_GZIP:
#ifdef KUALA_USE_ZLIB
            return gzip_compress(data, len, out_len, level);
#else
            return NULL;
#endif
        case COMPRESS_ZSTD:
            return NULL;
        default:
            return NULL;
    }
}

void* decompress_data(const void* data, size_t len, size_t* out_len,
                      CompressAlgorithm algo) {
    switch (algo) {
        case COMPRESS_RLE:
            return rle_decompress(data, len, out_len);
        case COMPRESS_LZ77:
            return lz77_decompress(data, len, out_len);
        case COMPRESS_DEFLATE:
#ifdef KUALA_USE_ZLIB
            return deflate_decompress(data, len, out_len);
#else
            return NULL;
#endif
        case COMPRESS_GZIP:
#ifdef KUALA_USE_ZLIB
            return gzip_decompress(data, len, out_len);
#else
            return NULL;
#endif
        case COMPRESS_ZSTD:
            return NULL;
        default:
            return NULL;
    }
}

/* ============================================================
 * zlib wrappers (deflate / gzip)
 *   On-disk layout: [sizeof(size_t) bytes original length][zlib payload]
 *   The length prefix lets decompress allocate the exact output buffer.
 * ============================================================ */
#ifdef KUALA_USE_ZLIB

static void* zlib_deflate_generic(const void* data, size_t len,
                                  size_t* out_len, int level, int window_bits) {
    z_stream strm;
    unsigned char* out;
    size_t cap, orig;
    int rc;

    if (!data) return NULL;
    if (level < 0) level = Z_DEFAULT_COMPRESSION;
    if (level > 9) level = 9;

    memset(&strm, 0, sizeof(strm));
    rc = deflateInit2(&strm, level, Z_DEFLATED, window_bits, 8,
                      Z_DEFAULT_STRATEGY);
    if (rc != Z_OK) return NULL;

    cap = sizeof(size_t) + deflateBound(&strm, (uLong)len) + 64;
    out = (unsigned char*)kmm_v4_malloc(cap);
    if (!out) { deflateEnd(&strm); return NULL; }

    orig = len;
    memcpy(out, &orig, sizeof(size_t));

    strm.next_in = (Bytef*)data;
    strm.avail_in = (uInt)len;
    strm.next_out = (Bytef*)(out + sizeof(size_t));
    strm.avail_out = (uInt)(cap - sizeof(size_t));

    rc = deflate(&strm, Z_FINISH);
    if (rc != Z_STREAM_END) {
        deflateEnd(&strm);
        kmm_v4_free(out);
        return NULL;
    }

    {
        size_t produced = (size_t)(cap - sizeof(size_t) - strm.avail_out);
        deflateEnd(&strm);
        if (out_len) *out_len = sizeof(size_t) + produced;
        return out;
    }
}

static void* zlib_inflate_generic(const void* data, size_t len,
                                  size_t* out_len, int window_bits) {
    z_stream strm;
    unsigned char* out;
    size_t orig, produced;
    int rc;

    if (out_len) *out_len = 0;
    if (!data || len < sizeof(size_t)) return NULL;

    memcpy(&orig, data, sizeof(size_t));
    out = (unsigned char*)kmm_v4_malloc(orig + 1);
    if (!out) return NULL;

    memset(&strm, 0, sizeof(strm));
    rc = inflateInit2(&strm, window_bits);
    if (rc != Z_OK) { kmm_v4_free(out); return NULL; }

    strm.next_in = (Bytef*)((const unsigned char*)data + sizeof(size_t));
    strm.avail_in = (uInt)(len - sizeof(size_t));
    strm.next_out = (Bytef*)out;
    strm.avail_out = (uInt)orig;

    rc = inflate(&strm, Z_FINISH);
    produced = (size_t)(orig - strm.avail_out);
    inflateEnd(&strm);
    if (rc != Z_STREAM_END) {
        kmm_v4_free(out);
        return NULL;
    }

    if (out_len) *out_len = produced;
    return out;
}

void* deflate_compress(const void* data, size_t len, size_t* out_len, int level) {
    return zlib_deflate_generic(data, len, out_len, level, 15);
}

void* deflate_decompress(const void* data, size_t len, size_t* out_len) {
    return zlib_inflate_generic(data, len, out_len, 15 + 32);
}

void* gzip_compress(const void* data, size_t len, size_t* out_len, int level) {
    return zlib_deflate_generic(data, len, out_len, level, 15 + 16);
}

void* gzip_decompress(const void* data, size_t len, size_t* out_len) {
    return zlib_inflate_generic(data, len, out_len, 15 + 32);
}

#endif /* KUALA_USE_ZLIB */

/* ============================================================
 * Streaming compress
 *   Accumulates all written data in an internal buffer and
 *   performs a one-shot compress when read.
 * ============================================================ */

struct CompressStream {
    CompressAlgorithm algorithm;
    int level;
    unsigned char* buffer;
    size_t size;
    size_t capacity;
};

CompressStream* compress_stream_create(CompressAlgorithm algo, int level) {
    CompressStream* s = (CompressStream*)kmm_v4_malloc(sizeof(CompressStream));
    if (!s) return NULL;
    s->algorithm = algo;
    s->level = level;
    s->size = 0;
    s->capacity = 4096;
    s->buffer = (unsigned char*)kmm_v4_malloc(s->capacity);
    if (!s->buffer) { kmm_v4_free(s); return NULL; }
    return s;
}

bool_t compress_stream_write(CompressStream* stream, const void* data, size_t len) {
    if (!stream) return 0;
    if (!data && len) return 0;
    if (len == 0) return 1;

    if (stream->size + len > stream->capacity) {
        size_t newcap = stream->capacity;
        unsigned char* nb;
        while (newcap < stream->size + len) newcap *= 2;
        nb = (unsigned char*)kmm_v4_malloc(newcap);
        if (!nb) return 0;
        memcpy(nb, stream->buffer, stream->size);
        kmm_v4_free(stream->buffer);
        stream->buffer = nb;
        stream->capacity = newcap;
    }

    memcpy(stream->buffer + stream->size, data, len);
    stream->size += len;
    return 1;
}

void* compress_stream_read(CompressStream* stream, size_t* out_len) {
    if (!stream) {
        if (out_len) *out_len = 0;
        return NULL;
    }
    return compress_data(stream->buffer, stream->size, out_len,
                         stream->algorithm, stream->level);
}

void compress_stream_destroy(CompressStream* stream) {
    if (!stream) return;
    if (stream->buffer) kmm_v4_free(stream->buffer);
    kmm_v4_free(stream);
}

/* ============================================================
 * File helpers
 * ============================================================ */

static unsigned char* read_file_all(const char* path, size_t* out_size) {
    FILE* f;
    long sz;
    size_t rd;
    unsigned char* buf;

    if (!path || !out_size) return NULL;
    *out_size = 0;

    f = fopen(path, "rb");
    if (!f) return NULL;

    if (fseek(f, 0, SEEK_END) != 0) { fclose(f); return NULL; }
    sz = ftell(f);
    if (sz < 0) { fclose(f); return NULL; }
    if (fseek(f, 0, SEEK_SET) != 0) { fclose(f); return NULL; }

    buf = (unsigned char*)kmm_v4_malloc((size_t)sz + 1);
    if (!buf) { fclose(f); return NULL; }

    rd = fread(buf, 1, (size_t)sz, f);
    fclose(f);
    if (rd != (size_t)sz) { kmm_v4_free(buf); return NULL; }

    *out_size = (size_t)sz;
    return buf;
}

static bool_t write_file_all(const char* path, const void* data, size_t len) {
    FILE* f;
    size_t wr;
    int ok;

    if (!path) return 0;
    f = fopen(path, "wb");
    if (!f) return 0;

    wr = (len == 0) ? 0 : fwrite(data, 1, len, f);
    ok = (wr == len);
    if (fclose(f) != 0) ok = 0;
    return ok ? 1 : 0;
}

bool_t compress_file(const char* input, const char* output,
                     CompressAlgorithm algo, int level) {
    size_t in_size = 0;
    size_t out_size = 0;
    unsigned char* in_data;
    void* out_data;
    bool_t ok;

    if (!input || !output) return 0;

    in_data = read_file_all(input, &in_size);
    if (!in_data) return 0;

    out_data = compress_data(in_data, in_size, &out_size, algo, level);
    kmm_v4_free(in_data);
    if (!out_data) return 0;

    ok = write_file_all(output, out_data, out_size);
    kmm_v4_free(out_data);
    return ok;
}

bool_t decompress_file(const char* input, const char* output,
                       CompressAlgorithm algo) {
    size_t in_size = 0;
    size_t out_size = 0;
    unsigned char* in_data;
    void* out_data;
    bool_t ok;

    if (!input || !output) return 0;

    in_data = read_file_all(input, &in_size);
    if (!in_data) return 0;

    out_data = decompress_data(in_data, in_size, &out_size, algo);
    kmm_v4_free(in_data);
    if (!out_data) return 0;

    ok = write_file_all(output, out_data, out_size);
    kmm_v4_free(out_data);
    return ok;
}
