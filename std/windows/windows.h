#ifndef STD_WINDOWS_WINDOWS_H
#define STD_WINDOWS_WINDOWS_H

#include "../base/types.h"

// Windows 专用系统函数（实现在 std/system/system.c 中）
// 这些函数在 stdlib.json 中归属 std.windows 模块

extern bool system_windows_registry_set(const char* key, const char* value_name, const char* value);
extern const char* system_windows_registry_get(const char* key, const char* value_name);
extern bool system_windows_registry_delete(const char* key, const char* value_name);
extern bool system_windows_create_process(const char* command, bool show_window);
extern bool system_windows_get_process_info(int pid, char* name, size_t name_size, size_t* memory_usage);
extern bool system_windows_get_service_status(const char* service_name, char* status, size_t status_size);
extern bool system_windows_start_service(const char* service_name);
extern bool system_windows_stop_service(const char* service_name);
extern const char* system_windows_get_computer_name();
extern const char* system_windows_get_username();
extern bool system_windows_set_console_title(const char* title);

extern bool system_windows_show_message_box(const char* title, const char* message, int type);
extern bool system_windows_get_screen_size(int* width, int* height);
extern bool system_windows_set_cursor_position(int x, int y);
extern bool system_windows_get_cursor_position(int* x, int* y);
extern bool system_windows_get_desktop_background(char* path, size_t path_size);
extern bool system_windows_set_desktop_background(const char* path);
extern bool system_windows_get_system_directory(char* path, size_t path_size);
extern bool system_windows_get_windows_directory(char* path, size_t path_size);
extern bool system_windows_get_temp_directory(char* path, size_t path_size);
extern bool system_windows_get_environment_variable(const char* name, char* value, size_t value_size);
extern bool system_windows_set_environment_variable(const char* name, const char* value);

#endif // STD_WINDOWS_WINDOWS_H
