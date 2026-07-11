#ifndef STD_SUBPROCESS_SUBPROCESS_H
#define STD_SUBPROCESS_SUBPROCESS_H

#include "../base/types.h"

typedef struct Process Process;

Process* subprocess_create(const char* command);
void subprocess_destroy(Process* proc);

bool_t subprocess_start(Process* proc);
bool_t subprocess_wait(Process* proc, i64 timeout_ms);
i32 subprocess_exit_code(const Process* proc);

bool_t subprocess_terminate(Process* proc);
bool_t subprocess_kill(Process* proc);

bool_t subprocess_running(const Process* proc);

ssize_t subprocess_read_stdout(Process* proc, u8* buffer, size_t size);
ssize_t subprocess_read_stderr(Process* proc, u8* buffer, size_t size);
ssize_t subprocess_write_stdin(Process* proc, const u8* buffer, size_t size);

bool_t subprocess_pipe_stdout(Process* proc, bool_t enable);
bool_t subprocess_pipe_stderr(Process* proc, bool_t enable);
bool_t subprocess_pipe_stdin(Process* proc, bool_t enable);

bool_t subprocess_set_working_directory(Process* proc, const char* cwd);
bool_t subprocess_set_env(Process* proc, const char* name, const char* value);

i32 subprocess_call(const char* command);
i32 subprocess_call_with_args(const char* program, const char** args);

char* subprocess_output(const char* command);

#endif
