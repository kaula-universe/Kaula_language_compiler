#include "subprocess.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include "../fs/fs.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

#ifdef _WIN32
#include <windows.h>
#include <process.h>
#include <fcntl.h>
#include <io.h>
#else
#include <unistd.h>
#include <sys/wait.h>
#include <signal.h>
#include <errno.h>
#endif

struct Process {
    char* command;
    char* cwd;
    FILE* stdin_file;
    FILE* stdout_file;
    FILE* stderr_file;
#ifdef _WIN32
    HANDLE hProcess;
    HANDLE hThread;
    DWORD pid;
#else
    pid_t pid;
#endif
    bool_t running;
    bool_t pipe_stdout;
    bool_t pipe_stderr;
    bool_t pipe_stdin;
    i32 exit_code;
};

Process* subprocess_create(const char* command) {
    Process* proc = (Process*)kmm_v4_malloc(sizeof(Process));
    if (!proc) return NULL;
    proc->command = kmm_v4_strdup(command);
    proc->cwd = NULL;
    proc->stdin_file = NULL;
    proc->stdout_file = NULL;
    proc->stderr_file = NULL;
    proc->running = false;
    proc->pipe_stdout = true;
    proc->pipe_stderr = true;
    proc->pipe_stdin = true;
    proc->exit_code = -1;
#ifdef _WIN32
    proc->hProcess = NULL;
    proc->hThread = NULL;
#endif
    return proc;
}

void subprocess_destroy(Process* proc) {
    if (!proc) return;
    if (proc->running) subprocess_kill(proc);
    kmm_v4_free(proc->command);
    kmm_v4_free(proc->cwd);
    if (proc->stdin_file) fclose(proc->stdin_file);
    if (proc->stdout_file) fclose(proc->stdout_file);
    if (proc->stderr_file) fclose(proc->stderr_file);
#ifdef _WIN32
    if (proc->hProcess) CloseHandle(proc->hProcess);
    if (proc->hThread) CloseHandle(proc->hThread);
#endif
    kmm_v4_free(proc);
}

bool_t subprocess_start(Process* proc) {
    if (!proc) return false;
    
#ifdef _WIN32
    SECURITY_ATTRIBUTES sa;
    sa.nLength = sizeof(sa);
    sa.lpSecurityDescriptor = NULL;
    sa.bInheritHandle = TRUE;
    
    HANDLE hStdinRead = NULL, hStdinWrite = NULL;
    HANDLE hStdoutRead = NULL, hStdoutWrite = NULL;
    HANDLE hStderrRead = NULL, hStderrWrite = NULL;
    
    if (proc->pipe_stdin) CreatePipe(&hStdinRead, &hStdinWrite, &sa, 0);
    if (proc->pipe_stdout) CreatePipe(&hStdoutRead, &hStdoutWrite, &sa, 0);
    if (proc->pipe_stderr) CreatePipe(&hStderrRead, &hStderrWrite, &sa, 0);
    
    STARTUPINFO si;
    PROCESS_INFORMATION pi;
    
    ZeroMemory(&si, sizeof(si));
    si.cb = sizeof(si);
    si.dwFlags = STARTF_USESTDHANDLES;
    si.hStdInput = hStdinRead;
    si.hStdOutput = hStdoutWrite;
    si.hStdError = hStderrWrite;
    
    char* cmd = kmm_v4_strdup(proc->command);
    
    BOOL result = CreateProcess(
        NULL, cmd, NULL, NULL, TRUE, 0, NULL,
        proc->cwd ? proc->cwd : NULL, &si, &pi
    );
    
    kmm_v4_free(cmd);
    
    if (!result) return false;
    
    proc->hProcess = pi.hProcess;
    proc->hThread = pi.hThread;
    proc->pid = pi.dwProcessId;
    proc->running = true;
    
    if (proc->pipe_stdin) {
        CloseHandle(hStdinRead);
        proc->stdin_file = _fdopen(_open_osfhandle((intptr_t)hStdinWrite, _O_TEXT), "w");
    }
    if (proc->pipe_stdout) {
        CloseHandle(hStdoutWrite);
        proc->stdout_file = _fdopen(_open_osfhandle((intptr_t)hStdoutRead, _O_TEXT), "r");
    }
    if (proc->pipe_stderr) {
        CloseHandle(hStderrWrite);
        proc->stderr_file = _fdopen(_open_osfhandle((intptr_t)hStderrRead, _O_TEXT), "r");
    }
    
    return true;
#else
    int stdin_pipe[2] = { -1, -1 };
    int stdout_pipe[2] = { -1, -1 };
    int stderr_pipe[2] = { -1, -1 };
    
    if (proc->pipe_stdin) pipe(stdin_pipe);
    if (proc->pipe_stdout) pipe(stdout_pipe);
    if (proc->pipe_stderr) pipe(stderr_pipe);
    
    pid_t pid = fork();
    
    if (pid == 0) {
        if (proc->pipe_stdin) {
            close(stdin_pipe[1]);
            dup2(stdin_pipe[0], STDIN_FILENO);
        }
        if (proc->pipe_stdout) {
            close(stdout_pipe[0]);
            dup2(stdout_pipe[1], STDOUT_FILENO);
        }
        if (proc->pipe_stderr) {
            close(stderr_pipe[0]);
            dup2(stderr_pipe[1], STDERR_FILENO);
        }
        
        if (proc->cwd) chdir(proc->cwd);
        
        execl("/bin/sh", "sh", "-c", proc->command, NULL);
        _exit(1);
    } else if (pid < 0) {
        return false;
    }
    
    proc->pid = pid;
    proc->running = true;
    
    if (proc->pipe_stdin) {
        close(stdin_pipe[0]);
        proc->stdin_file = fdopen(stdin_pipe[1], "w");
    }
    if (proc->pipe_stdout) {
        close(stdout_pipe[1]);
        proc->stdout_file = fdopen(stdout_pipe[0], "r");
    }
    if (proc->pipe_stderr) {
        close(stderr_pipe[1]);
        proc->stderr_file = fdopen(stderr_pipe[0], "r");
    }
    
    return true;
#endif
}

bool_t subprocess_wait(Process* proc, i64 timeout_ms) {
    if (!proc || !proc->running) return false;
    
#ifdef _WIN32
    DWORD result = WaitForSingleObject(proc->hProcess, (DWORD)timeout_ms);
    if (result == WAIT_OBJECT_0) {
        GetExitCodeProcess(proc->hProcess, (LPDWORD)&proc->exit_code);
        proc->running = false;
        return true;
    }
    return false;
#else
    int status;
    pid_t result = waitpid(proc->pid, &status, 0);
    if (result == proc->pid) {
        proc->exit_code = WEXITSTATUS(status);
        proc->running = false;
        return true;
    }
    return false;
#endif
}

i32 subprocess_exit_code(const Process* proc) {
    return proc ? proc->exit_code : -1;
}

bool_t subprocess_terminate(Process* proc) {
    if (!proc || !proc->running) return false;
    
#ifdef _WIN32
    return TerminateProcess(proc->hProcess, 1) != 0;
#else
    return kill(proc->pid, SIGTERM) == 0;
#endif
}

bool_t subprocess_kill(Process* proc) {
    if (!proc || !proc->running) return false;
    
#ifdef _WIN32
    return TerminateProcess(proc->hProcess, 1) != 0;
#else
    return kill(proc->pid, SIGKILL) == 0;
#endif
}

bool_t subprocess_running(const Process* proc) {
    return proc ? proc->running : false;
}

ssize_t subprocess_read_stdout(Process* proc, u8* buffer, size_t size) {
    if (!proc || !proc->stdout_file || !buffer) return -1;
    return (ssize_t)fread(buffer, 1, size, proc->stdout_file);
}

ssize_t subprocess_read_stderr(Process* proc, u8* buffer, size_t size) {
    if (!proc || !proc->stderr_file || !buffer) return -1;
    return (ssize_t)fread(buffer, 1, size, proc->stderr_file);
}

ssize_t subprocess_write_stdin(Process* proc, const u8* buffer, size_t size) {
    if (!proc || !proc->stdin_file || !buffer) return -1;
    return (ssize_t)fwrite(buffer, 1, size, proc->stdin_file);
}

bool_t subprocess_pipe_stdout(Process* proc, bool_t enable) {
    if (!proc) return false;
    proc->pipe_stdout = enable;
    return true;
}

bool_t subprocess_pipe_stderr(Process* proc, bool_t enable) {
    if (!proc) return false;
    proc->pipe_stderr = enable;
    return true;
}

bool_t subprocess_pipe_stdin(Process* proc, bool_t enable) {
    if (!proc) return false;
    proc->pipe_stdin = enable;
    return true;
}

bool_t subprocess_set_working_directory(Process* proc, const char* cwd) {
    if (!proc) return false;
    kmm_v4_free(proc->cwd);
    proc->cwd = kmm_v4_strdup(cwd);
    return proc->cwd != NULL;
}

bool_t subprocess_set_env(Process* proc, const char* name, const char* value) {
    if (!proc) return false;
#ifdef _WIN32
    return SetEnvironmentVariableA(name, value) != 0;
#else
    return setenv(name, value, 1) == 0;
#endif
}

i32 subprocess_call(const char* command) {
    Process* proc = subprocess_create(command);
    if (!proc) return -1;
    if (!subprocess_start(proc)) {
        subprocess_destroy(proc);
        return -1;
    }
    subprocess_wait(proc, -1);
    i32 exit_code = subprocess_exit_code(proc);
    subprocess_destroy(proc);
    return exit_code;
}

i32 subprocess_call_with_args(const char* program, const char** args) {
    if (!program || !args) return -1;
    
    size_t len = strlen(program) + 1;
    for (size_t i = 0; args[i]; i++) {
        len += strlen(args[i]) + 1;
    }
    
    char* command = (char*)kmm_v4_malloc(len);
    strcpy(command, program);
    
    for (size_t i = 0; args[i]; i++) {
        strcat(command, " ");
        strcat(command, args[i]);
    }
    
    i32 result = subprocess_call(command);
    kmm_v4_free(command);
    return result;
}

char* subprocess_output(const char* command) {
    Process* proc = subprocess_create(command);
    if (!proc) return NULL;
    
    if (!subprocess_start(proc)) {
        subprocess_destroy(proc);
        return NULL;
    }
    
    size_t capacity = 4096;
    char* output = (char*)kmm_v4_malloc(capacity);
    size_t pos = 0;
    
    char buffer[1024];
    ssize_t read;
    while ((read = subprocess_read_stdout(proc, (u8*)buffer, sizeof(buffer) - 1)) > 0) {
        buffer[read] = '\0';
        if (pos + read >= capacity) {
            capacity *= 2;
            output = (char*)kmm_v4_realloc(output, capacity);
        }
        strcpy(output + pos, buffer);
        pos += (size_t)read;
    }
    
    subprocess_wait(proc, -1);
    subprocess_destroy(proc);
    
    return output;
}
