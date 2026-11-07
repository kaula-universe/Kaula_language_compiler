#ifndef STD_ERROR_ERROR_H
#define STD_ERROR_ERROR_H

#include "../base/types.h"

// 错误类型
typedef enum {
    STD_ERROR_NONE,
    STD_ERROR_INVALID_ARGUMENT,
    STD_ERROR_OUT_OF_MEMORY,
    STD_ERROR_FILE_NOT_FOUND,
    STD_ERROR_PERMISSION_DENIED,
    STD_ERROR_IO_ERROR,
    STD_ERROR_NETWORK_ERROR,
    STD_ERROR_SYSTEM_ERROR,
    STD_ERROR_RUNTIME_ERROR,
    STD_ERROR_LOGIC_ERROR,
    STD_ERROR_NOT_FOUND,
    STD_ERROR_TIMEOUT,
    STD_ERROR_CANCELLED,
    STD_ERROR_EXISTS,
    STD_ERROR_UNKNOWN
} ErrorType;

// 错误结构
typedef struct kaula_Error {
    ErrorType type;
    int code;
    char message[256];
    char file[128];
    int line;
    struct kaula_Error* cause;   // 错误链：被包装的下层错误（KMM 生命周期内有效）
} Error;

// 错误函数
extern Error* error_create(ErrorType type, int code, const char* message, const char* file, int line);
extern void error_destroy(Error* error);
extern const char* error_get_message(Error* error);
extern ErrorType error_get_type(Error* error);
extern int error_get_code(Error* error);
extern void error_set_message(Error* error, const char* message);
extern void error_set_code(Error* error, int code);

// 错误工具函数
extern const char* error_type_to_string(ErrorType type);
extern void error_print(Error* error);
extern void error_printf(Error* error, const char* format, ...);

// 错误链（wrap/cause）：把底层错误包装成上层错误，形成因果链
extern Error* error_wrap(Error* cause, ErrorType type, const char* message);
extern Error* error_cause(Error* error);
extern int error_has_cause(Error* error);

// errno 桥接：把系统错误码包装为 Error（type 归类为 IO/SYSTEM 错误）
extern Error* error_from_errno(int err_code, const char* context);
extern const char* error_strerror(int err_code);

// 打印整条错误链（从最外层到最内层 cause）
extern void error_print_chain(Error* error);

// 错误宏
#define ERROR_CREATE(type, code, message) error_create(type, code, message, __FILE__, __LINE__)
#define ERROR_PRINT(error) error_print(error)
#define ERROR_FREE(error) error_destroy(error)

#endif // STD_ERROR_ERROR_H