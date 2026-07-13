// kaula_runtime.h - Minimal stubs for Kaula runtime features
#ifndef KAULA_RUNTIME_H
#define KAULA_RUNTIME_H

#include <stdint.h>

// Task param struct (for task() function parameter)
typedef struct { int64_t priority; void* data; } TaskParam;

// Async param struct (for async() function parameter)
typedef struct { void* data; } AsyncParam;

#endif
