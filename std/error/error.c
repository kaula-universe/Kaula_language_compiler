#include "error.h"
#include <stdio.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>

// 错误处理函数
Error* error_create(ErrorType type, int code, const char* message, const char* file, int line) {
    Error* error = (Error*)malloc(sizeof(Error));
    if (error) {
        error->type = type;
        error->code = code;
        strncpy(error->message, message, sizeof(error->message) - 1);
        error->message[sizeof(error->message) - 1] = '\0';
        strncpy(error->file, file, sizeof(error->file) - 1);
        error->file[sizeof(error->file) - 1] = '\0';
        error->line = line;
    }
    return error;
}

void error_destroy(Error* error) {
    if (error) {
        free(error);
    }
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
    if (error) {
        return error->code;
    }
    return -1;
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
