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
    STD_ERROR_UNKNOWN
} ErrorType;

// 错误结构
typedef struct {
    ErrorType type;
    int code;
    char message[256];
    char file[128];
    int line;
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

// 错误宏
#define ERROR_CREATE(type, code, message) error_create(type, code, message, __FILE__, __LINE__)
#define ERROR_PRINT(error) error_print(error)
#define ERROR_FREE(error) error_destroy(error)

#endif // STD_ERROR_ERROR_H