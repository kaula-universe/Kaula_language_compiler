#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef enum {
    COMPRESS_RLE,
    COMPRESS_LZ77,
    COMPRESS_DEFLATE,
    COMPRESS_GZIP,
    COMPRESS_ZSTD
} CompressAlgorithm;

typedef struct {
    CompressAlgorithm algorithm;
    int level;
} CompressOptions;

// One-shot compress/decompress
void*  compress_data(const void* data, size_t len, size_t* out_len,
                     CompressAlgorithm algo, int level);
void*  decompress_data(const void* data, size_t len, size_t* out_len,
                       CompressAlgorithm algo);

// RLE (Run-Length Encoding) - built-in
void*  rle_compress(const void* data, size_t len, size_t* out_len);
void*  rle_decompress(const void* data, size_t len, size_t* out_len);

// LZ77 - built-in
void*  lz77_compress(const void* data, size_t len, size_t* out_len, int level);
void*  lz77_decompress(const void* data, size_t len, size_t* out_len);

// DEFLATE (requires zlib)
#ifdef KUALA_USE_ZLIB
void*  deflate_compress(const void* data, size_t len, size_t* out_len, int level);
void*  deflate_decompress(const void* data, size_t len, size_t* out_len);
void*  gzip_compress(const void* data, size_t len, size_t* out_len, int level);
void*  gzip_decompress(const void* data, size_t len, size_t* out_len);
#endif

// Streaming compress
typedef struct CompressStream CompressStream;

CompressStream* compress_stream_create(CompressAlgorithm algo, int level);
bool_t  compress_stream_write(CompressStream* stream, const void* data, size_t len);
void*   compress_stream_read(CompressStream* stream, size_t* out_len);
void    compress_stream_destroy(CompressStream* stream);

// File helpers
bool_t  compress_file(const char* input, const char* output,
                      CompressAlgorithm algo, int level);
bool_t  decompress_file(const char* input, const char* output,
                        CompressAlgorithm algo);
