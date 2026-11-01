#ifndef STD_BITSET_BITSET_H
#define STD_BITSET_BITSET_H

#include "../base/types.h"

typedef struct Bitset Bitset;

Bitset* bitset_create(size_t size);
void bitset_destroy(Bitset* bs);

Bitset* bitset_copy(const Bitset* bs);

size_t bitset_size(const Bitset* bs);
size_t bitset_count(const Bitset* bs);

bool_t bitset_get(const Bitset* bs, size_t index);
void bitset_set(Bitset* bs, size_t index, bool_t value);
void bitset_set_all(Bitset* bs, bool_t value);

void bitset_set_bit(Bitset* bs, size_t index);
void bitset_clear_bit(Bitset* bs, size_t index);
bool_t bitset_toggle_bit(Bitset* bs, size_t index);

bool_t bitset_all(const Bitset* bs);
bool_t bitset_any(const Bitset* bs);
bool_t bitset_none(const Bitset* bs);

void bitset_and(Bitset* result, const Bitset* a, const Bitset* b);
void bitset_or(Bitset* result, const Bitset* a, const Bitset* b);
void bitset_xor(Bitset* result, const Bitset* a, const Bitset* b);
void bitset_not(Bitset* result, const Bitset* a);

void bitset_shift_left(Bitset* bs, size_t n);
void bitset_shift_right(Bitset* bs, size_t n);

size_t bitset_find_first_set(const Bitset* bs);
size_t bitset_find_last_set(const Bitset* bs);
size_t bitset_find_next_set(const Bitset* bs, size_t start);

void bitset_resize(Bitset* bs, size_t new_size);

char* bitset_to_string(const Bitset* bs);

#endif
