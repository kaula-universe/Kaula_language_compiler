#include "error.h"
#include "../memory/memory.h"
#include <stdio.h>
#include <stdarg.h>
#include <string.h>
#include <errno.h>

#if defined(_WIN32)
#define ERROR_THREAD_LOCAL __declspec(thread)
#else
#define ERROR_THREAD_LOCAL _Thread_local
#endif

#define ERROR_STRERROR_CAP 256

// strerror 的线程安全副本缓冲（strerror 返回静态缓冲，不可重入）
static ERROR_THREAD_LOCAL char g_strerror_buf[ERROR_STRERROR_CAP];

// 错误处理函数
Error* error_create(ErrorType type, int code, const char* message, const char* file, int line) {
    Error* error = (Error*)kmm_v4_calloc(1, sizeof(Error));
    if (error) {
        error->type = type;
        error->code = code;
        strncpy(error->message, message, sizeof(error->message) - 1);
        error->message[sizeof(error->message) - 1] = '\0';
        strncpy(error->file, file, sizeof(error->file) - 1);
        error->file[sizeof(error->file) - 1] = '\0';
        error->line = line;
        error->cause = NULL;
    }
    return error;
}

void error_destroy(Error* error) {
    // KMM 管理内存，无需手动释放：Error 与 error_wrap 链上的全部节点
    // 都分配自 KMM bump 池，作用域退出时批量回收。
    // 因此 error_destroy 保持空操作；不要手动 free。
    (void)error;
}

const char* error_get_message(Error* error) {
    if (error) {
        return error->message;
    }
    return "Unknown error";
}

ErrorType error_get_type(Error* error) {
    if (error) {
        return error->type;
    }
    return STD_ERROR_UNKNOWN;
}

int error_get_code(Error* error) {
    // 检查错误对象是否有效
    if (error) {
        return error->code;  // 返回错误码
    }
    return -1;  // 无效对象返回默认值
}

void error_set_message(Error* error, const char* message) {
    if (error) {
        strncpy(error->message, message, sizeof(error->message) - 1);
        error->message[sizeof(error->message) - 1] = '\0';
    }
}

void error_set_code(Error* error, int code) {
    if (error) {
        error->code = code;
    }
}

// 错误工具函数
const char* error_type_to_string(ErrorType type) {
    switch (type) {
        case STD_ERROR_NONE:
            return "No error";
        case STD_ERROR_INVALID_ARGUMENT:
            return "Invalid argument";
        case STD_ERROR_OUT_OF_MEMORY:
            return "Out of memory";
        case STD_ERROR_FILE_NOT_FOUND:
            return "File not found";
        case STD_ERROR_PERMISSION_DENIED:
            return "Permission denied";
        case STD_ERROR_IO_ERROR:
            return "I/O error";
        case STD_ERROR_NETWORK_ERROR:
            return "Network error";
        case STD_ERROR_SYSTEM_ERROR:
            return "System error";
        case STD_ERROR_RUNTIME_ERROR:
            return "Runtime error";
        case STD_ERROR_LOGIC_ERROR:
            return "Logic error";
        case STD_ERROR_NOT_FOUND:
            return "Not found";
        case STD_ERROR_TIMEOUT:
            return "Timeout";
        case STD_ERROR_CANCELLED:
            return "Cancelled";
        case STD_ERROR_EXISTS:
            return "Already exists";
        case STD_ERROR_UNKNOWN:
        default:
            return "Unknown error";
    }
}

void error_print(Error* error) {
    if (error) {
        fprintf(stderr, "Error: %s (code: %d)\n", error_type_to_string(error->type), error->code);
        fprintf(stderr, "Message: %s\n", error->message);
        fprintf(stderr, "File: %s:%d\n", error->file, error->line);
    } else {
        fprintf(stderr, "Error: NULL error pointer\n");
    }
}

void error_printf(Error* error, const char* format, ...) {
    if (error) {
        va_list args;
        va_start(args, format);
        vsnprintf(error->message, sizeof(error->message) - 1, format, args);
        error->message[sizeof(error->message) - 1] = '\0';
        va_end(args);
    }
}

// 错误链：包装底层错误，保留 cause 指针
Error* error_wrap(Error* cause, ErrorType type, const char* message) {
    Error* error = (Error*)kmm_v4_calloc(1, sizeof(Error));
    if (error) {
        error->type = type;
        error->code = 0;
        strncpy(error->message, message, sizeof(error->message) - 1);
        error->message[sizeof(error->message) - 1] = '\0';
        error->file[0] = '\0';
        error->line = 0;
        error->cause = cause;   // 允许 NULL：无底层错误时等价于 error_create
    }
    return error;
}

// 返回被包装的下层错误；无 cause 时返回 NULL
Error* error_cause(Error* error) {
    if (!error) {
        return NULL;
    }
    return error->cause;
}

// 是否带有 cause 链
int error_has_cause(Error* error) {
    if (!error) {
        return 0;
    }
    return error->cause != NULL;
}

// errno 桥接：把系统错误码包装为 Error。
// 消息为 strerror(err_code) 的线程安全副本；code 保留原始 errno。
Error* error_from_errno(int err_code, const char* context) {
    ErrorType type = STD_ERROR_SYSTEM_ERROR;
    if (err_code == ENOENT) {
        type = STD_ERROR_FILE_NOT_FOUND;
    } else if (err_code == EACCES || err_code == EPERM) {
        type = STD_ERROR_PERMISSION_DENIED;
    } else if (err_code == ETIMEDOUT) {
        type = STD_ERROR_TIMEOUT;
    } else if (err_code == ENOMEM) {
        type = STD_ERROR_OUT_OF_MEMORY;
    }
    const char* desc = error_strerror(err_code);
    Error* error = (Error*)kmm_v4_calloc(1, sizeof(Error));
    if (error) {
        error->type = type;
        error->code = err_code;
        if (context && context[0] != '\0') {
            snprintf(error->message, sizeof(error->message), "%s: %s", context, desc);
        } else {
            strncpy(error->message, desc, sizeof(error->message) - 1);
            error->message[sizeof(error->message) - 1] = '\0';
        }
        error->file[0] = '\0';
        error->line = 0;
        error->cause = NULL;
    }
    return error;
}

// strerror 的线程安全版本：返回内部线程本地缓冲，下次调用可能被覆盖
const char* error_strerror(int err_code) {
    const char* msg = strerror(err_code);
    if (!msg) {
        msg = "Unknown error";
    }
    strncpy(g_strerror_buf, msg, sizeof(g_strerror_buf) - 1);
    g_strerror_buf[sizeof(g_strerror_buf) - 1] = '\0';
    return g_strerror_buf;
}

// 打印整条错误链（从最外层到最内层 cause）
void error_print_chain(Error* error) {
    int depth = 0;
    for (Error* cur = error; cur != NULL; cur = cur->cause) {
        if (depth > 0) {
            fprintf(stderr, "  caused by:\n");
        }
        fprintf(stderr, "Error: %s (code: %d)\n", error_type_to_string(cur->type), cur->code);
        fprintf(stderr, "Message: %s\n", cur->message);
        if (cur->file[0] != '\0') {
            fprintf(stderr, "File: %s:%d\n", cur->file, cur->line);
        }
        depth++;
        if (depth > 64) {
            fprintf(stderr, "Error chain too deep, stopping\n");
            break;
        }
    }
}
