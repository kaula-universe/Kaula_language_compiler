#ifndef STD_LOGGING_LOGGING_H
#define STD_LOGGING_LOGGING_H

#include "../base/types.h"
#include "../string/string.h"
#include "../time/time.h"

typedef enum {
    LOG_LEVEL_DEBUG,
    LOG_LEVEL_INFO,
    LOG_LEVEL_WARN,
    LOG_LEVEL_ERROR,
    LOG_LEVEL_FATAL
} LogLevel;

typedef struct {
    LogLevel level;
    String module;
    String file;
    int line;
    String message;
    i64 timestamp;
} LogEntry;

typedef struct {
    LogLevel min_level;
    String log_file;
    FILE* file_handle;
    bool_t console_output;
    bool_t file_output;
    bool_t color_output;
} Logger;

extern Logger* logger_create();
extern void logger_destroy(Logger* logger);

extern void logger_set_level(Logger* logger, LogLevel level);
extern void logger_set_file(Logger* logger, const String path);
extern void logger_enable_console(Logger* logger, bool_t enable);
extern void logger_enable_file(Logger* logger, bool_t enable);
extern void logger_enable_color(Logger* logger, bool_t enable);

extern void logger_log(Logger* logger, LogLevel level, const String module, const String file, int line, const String format, ...);
extern void logger_debug(Logger* logger, const String module, const String message, ...);
extern void logger_info(Logger* logger, const String module, const String message, ...);
extern void logger_warn(Logger* logger, const String module, const String message, ...);
extern void logger_error(Logger* logger, const String module, const String message, ...);
extern void logger_fatal(Logger* logger, const String module, const String message, ...);

extern const String log_level_to_string(LogLevel level);
extern LogLevel log_level_from_string(const String str);

// Global logger
extern Logger* log_global_logger();
extern void log_init();
extern void log_shutdown();

#endif // STD_LOGGING_LOGGING_H
