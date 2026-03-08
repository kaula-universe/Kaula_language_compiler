#ifndef STD_SYSTEM_SYSTEM_H
#define STD_SYSTEM_SYSTEM_H

#include "../base/types.h"

// 定义_FILE_OFFSET_BITS以确保off_t被正确定义
#define _FILE_OFFSET_BITS 64

// 包含必要的头文件以定义off_t和文件操作常量
#ifdef _WIN32
#include <io.h>
#include <fcntl.h>
#include <sys/types.h>
#else
#include <unistd.h>
#include <fcntl.h>
#include <sys/types.h>
#endif

// 系统信息函数
extern const char* system_get_os_name();
extern const char* system_get_os_version();
extern const char* system_get_cpu_architecture();
extern size_t system_get_cpu_count();
extern size_t system_get_total_memory();
extern size_t system_get_available_memory();

// 时间函数
typedef struct Time {
    int year;
    int month;
    int day;
    int hour;
    int minute;
    int second;
    int millisecond;
} Time;

typedef uint64_t Timestamp;

extern Timestamp system_get_timestamp();
extern void system_get_current_time(Time* time);
extern Timestamp system_time_to_timestamp(const Time* time);
extern void system_timestamp_to_time(Timestamp timestamp, Time* time);
extern double system_get_uptime();
extern void system_sleep_ms(uint32_t milliseconds);

// 环境变量函数
extern const char* system_get_env(const char* name);
extern bool system_set_env(const char* name, const char* value);
extern bool system_unset_env(const char* name);
extern char** system_get_env_list();

// 进程函数
typedef int ProcessId;
extern ProcessId system_get_current_process_id();
extern ProcessId system_get_parent_process_id();
extern int system_execute(const char* command, char* output, size_t output_size);
extern int system_execute_with_args(const char* command, char* const args[], char* output, size_t output_size);

// 文件系统函数
extern bool system_file_exists(const char* path);
extern bool system_file_is_regular(const char* path);
extern bool system_file_is_directory(const char* path);
extern size_t system_file_size(const char* path);
extern bool system_file_delete(const char* path);
extern bool system_file_copy(const char* src, const char* dst);
extern bool system_file_move(const char* src, const char* dst);
extern bool system_directory_create(const char* path);
extern bool system_directory_delete(const char* path);
extern bool system_directory_exists(const char* path);
extern char** system_directory_list(const char* path, size_t* count);

// 路径函数
extern char* system_get_current_directory();
extern bool system_change_directory(const char* path);
extern char* system_get_executable_path();
extern char* system_get_home_directory();

// 系统错误函数
extern int system_get_last_error();
extern const char* system_get_error_message(int error_code);
extern void system_clear_error();

// 网络函数
extern bool system_network_is_available();
extern const char* system_get_hostname();

// 电源管理函数
extern bool system_is_battery_powered();
extern int system_get_battery_percentage();

// Windows API 函数
extern bool system_windows_registry_set(const char* key, const char* value_name, const char* value);
extern const char* system_windows_registry_get(const char* key, const char* value_name);
extern bool system_windows_registry_delete(const char* key, const char* value_name);
extern bool system_windows_create_process(const char* command, bool show_window);
extern bool system_windows_get_process_info(ProcessId pid, char* name, size_t name_size, size_t* memory_usage);
extern bool system_windows_get_service_status(const char* service_name, char* status, size_t status_size);
extern bool system_windows_start_service(const char* service_name);
extern bool system_windows_stop_service(const char* service_name);
extern const char* system_windows_get_computer_name();
extern const char* system_windows_get_username();
extern bool system_windows_set_console_title(const char* title);

// 更多 Windows API 函数
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

// 系统 syscall 函数
extern int system_syscall_open(const char* path, int flags, int mode);
extern int system_syscall_close(int fd);
extern ssize_t system_syscall_read(int fd, void* buf, size_t count);
extern ssize_t system_syscall_write(int fd, const void* buf, size_t count);
extern off_t system_syscall_lseek(int fd, off_t offset, int whence);
extern int system_syscall_fork();
extern int system_syscall_execve(const char* path, char* const argv[], char* const envp[]);
extern int system_syscall_waitpid(int pid, int* status, int options);
extern int system_syscall_kill(int pid, int sig);
extern void* system_syscall_malloc(size_t size);
extern void system_syscall_free(void* ptr);
extern void* system_syscall_realloc(void* ptr, size_t size);
extern void* system_syscall_calloc(size_t count, size_t size);

#endif // STD_SYSTEM_SYSTEM_H