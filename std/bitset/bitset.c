#include "bitset.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

struct Bitset {
    u64* words;
    size_t size;
    size_t word_count;
};

#define WORD_SIZE 64
#define WORD_INDEX(idx) ((idx) / WORD_SIZE)
#define BIT_INDEX(idx) ((idx) % WORD_SIZE)

Bitset* bitset_create(size_t size) {
    Bitset* bs = (Bitset*)kmm_v4_malloc(sizeof(Bitset));
    if (!bs) return NULL;
    
    bs->size = size;
    bs->word_count = (size + WORD_SIZE - 1) / WORD_SIZE;
    bs->words = (u64*)kmm_v4_calloc(bs->word_count, sizeof(u64));
    
    if (!bs->words) {
        kmm_v4_free(bs);
        return NULL;
    }
    
    return bs;
}

void bitset_destroy(Bitset* bs) {
    if (!bs) return;
    kmm_v4_free(bs->words);
    kmm_v4_free(bs);
}

Bitset* bitset_copy(const Bitset* bs) {
    if (!bs) return NULL;
    Bitset* copy = bitset_create(bs->size);
    if (!copy) return NULL;
    memcpy(copy->words, bs->words, bs->word_count * sizeof(u64));
    return copy;
}

size_t bitset_size(const Bitset* bs) {
    return bs ? bs->size : 0;
}

size_t bitset_count(const Bitset* bs) {
    if (!bs) return 0;
    size_t count = 0;
    for (size_t i = 0; i < bs->word_count; i++) {
        u64 w = bs->words[i];
        while (w) {
            count++;
            w &= w - 1;
        }
    }
    return count;
}

bool_t bitset_get(const Bitset* bs, size_t index) {
    if (!bs || index >= bs->size) return false;
    return (bs->words[WORD_INDEX(index)] & (1ULL << BIT_INDEX(index))) != 0;
}

void bitset_set(Bitset* bs, size_t index, bool_t value) {
    if (!bs || index >= bs->size) return;
    if (value) {
        bs->words[WORD_INDEX(index)] |= (1ULL << BIT_INDEX(index));
    } else {
        bs->words[WORD_INDEX(index)] &= ~(1ULL << BIT_INDEX(index));
    }
}

void bitset_set_all(Bitset* bs, bool_t value) {
    if (!bs) return;
    if (value) {
        memset(bs->words, 0xFF, bs->word_count * sizeof(u64));
    } else {
        memset(bs->words, 0, bs->word_count * sizeof(u64));
    }
}

void bitset_set_bit(Bitset* bs, size_t index) {
    bitset_set(bs, index, true);
}

void bitset_clear_bit(Bitset* bs, size_t index) {
    bitset_set(bs, index, false);
}

bool_t bitset_toggle_bit(Bitset* bs, size_t index) {
    if (!bs || index >= bs->size) return false;
    bs->words[WORD_INDEX(index)] ^= (1ULL << BIT_INDEX(index));
    return bitset_get(bs, index);
}

bool_t bitset_all(const Bitset* bs) {
    if (!bs) return false;
    for (size_t i = 0; i < bs->word_count - 1; i++) {
        if (bs->words[i] != ~0ULL) return false;
    }
    size_t last_bits = bs->size % WORD_SIZE;
    u64 mask = last_bits == 0 ? ~0ULL : ((1ULL << last_bits) - 1);
    return (bs->words[bs->word_count - 1] & mask) == mask;
}

bool_t bitset_any(const Bitset* bs) {
    if (!bs) return false;
    for (size_t i = 0; i < bs->word_count; i++) {
        if (bs->words[i] != 0) return true;
    }
    return false;
}

bool_t bitset_none(const Bitset* bs) {
    return !bitset_any(bs);
}

void bitset_and(Bitset* result, const Bitset* a, const Bitset* b) {
    if (!result || !a || !b) return;
    size_t min_words = a->word_count < b->word_count ? a->word_count : b->word_count;
    for (size_t i = 0; i < min_words; i++) {
        result->words[i] = a->words[i] & b->words[i];
    }
}

void bitset_or(Bitset* result, const Bitset* a, const Bitset* b) {
    if (!result || !a || !b) return;
    size_t min_words = a->word_count < b->word_count ? a->word_count : b->word_count;
    for (size_t i = 0; i < min_words; i++) {
        result->words[i] = a->words[i] | b->words[i];
    }
    if (a->word_count > b->word_count) {
        memcpy(result->words + min_words, a->words + min_words, (a->word_count - min_words) * sizeof(u64));
    } else if (b->word_count > a->word_count) {
        memcpy(result->words + min_words, b->words + min_words, (b->word_count - min_words) * sizeof(u64));
    }
}

void bitset_xor(Bitset* result, const Bitset* a, const Bitset* b) {
    if (!result || !a || !b) return;
    size_t min_words = a->word_count < b->word_count ? a->word_count : b->word_count;
    for (size_t i = 0; i < min_words; i++) {
        result->words[i] = a->words[i] ^ b->words[i];
    }
}

void bitset_not(Bitset* result, const Bitset* a) {
    if (!result || !a) return;
    for (size_t i = 0; i < a->word_count - 1; i++) {
        result->words[i] = ~a->words[i];
    }
    size_t last_bits = a->size % WORD_SIZE;
    u64 mask = last_bits == 0 ? ~0ULL : ((1ULL << last_bits) - 1);
    result->words[a->word_count - 1] = ~a->words[a->word_count - 1] & mask;
}

void bitset_shift_left(Bitset* bs, size_t n) {
    if (!bs || n == 0) return;
    size_t words_shift = n / WORD_SIZE;
    size_t bits_shift = n % WORD_SIZE;
    
    for (size_t i = bs->word_count - 1; i >= words_shift; i--) {
        bs->words[i] = bs->words[i - words_shift];
    }
    for (size_t i = 0; i < words_shift; i++) {
        bs->words[i] = 0;
    }
    
    if (bits_shift > 0) {
        for (size_t i = bs->word_count - 1; i > 0; i--) {
            bs->words[i] = (bs->words[i] << bits_shift) | (bs->words[i - 1] >> (WORD_SIZE - bits_shift));
        }
        bs->words[0] <<= bits_shift;
    }
}

void bitset_shift_right(Bitset* bs, size_t n) {
    if (!bs || n == 0) return;
    size_t words_shift = n / WORD_SIZE;
    size_t bits_shift = n % WORD_SIZE;
    
    for (size_t i = 0; i < bs->word_count - words_shift; i++) {
        bs->words[i] = bs->words[i + words_shift];
    }
    for (size_t i = bs->word_count - words_shift; i < bs->word_count; i++) {
        bs->words[i] = 0;
    }
    
    if (bits_shift > 0) {
        for (size_t i = 0; i < bs->word_count - 1; i++) {
            bs->words[i] = (bs->words[i] >> bits_shift) | (bs->words[i + 1] << (WORD_SIZE - bits_shift));
        }
        bs->words[bs->word_count - 1] >>= bits_shift;
    }
}

size_t bitset_find_first_set(const Bitset* bs) {
    if (!bs) return (size_t)-1;
    for (size_t i = 0; i < bs->word_count; i++) {
        if (bs->words[i] != 0) {
            for (size_t j = 0; j < WORD_SIZE; j++) {
                if (bs->words[i] & (1ULL << j)) {
                    return i * WORD_SIZE + j;
                }
            }
        }
    }
    return (size_t)-1;
}

size_t bitset_find_last_set(const Bitset* bs) {
    if (!bs) return (size_t)-1;
    for (size_t i = bs->word_count; i > 0; i--) {
        size_t idx = i - 1;
        if (bs->words[idx] != 0) {
            for (size_t j = WORD_SIZE; j > 0; j--) {
                if (bs->words[idx] & (1ULL << (j - 1))) {
                    return idx * WORD_SIZE + (j - 1);
                }
            }
        }
    }
    return (size_t)-1;
}

size_t bitset_find_next_set(const Bitset* bs, size_t start) {
    if (!bs || start >= bs->size) return (size_t)-1;
    for (size_t i = start; i < bs->size; i++) {
        if (bitset_get(bs, i)) return i;
    }
    return (size_t)-1;
}

void bitset_resize(Bitset* bs, size_t new_size) {
    if (!bs) return;
    size_t new_word_count = (new_size + WORD_SIZE - 1) / WORD_SIZE;
    if (new_word_count != bs->word_count) {
        bs->words = (u64*)kmm_v4_realloc(bs->words, new_word_count * sizeof(u64));
    }
    bs->size = new_size;
    bs->word_count = new_word_count;
}

char* bitset_to_string(const Bitset* bs) {
    if (!bs) return NULL;
    char* str = (char*)kmm_v4_malloc(bs->size + 1);
    if (!str) return NULL;
    for (size_t i = 0; i < bs->size; i++) {
        str[i] = bitset_get(bs, bs->size - 1 - i) ? '1' : '0';
    }
    str[bs->size] = '\0';
    return str;
}
