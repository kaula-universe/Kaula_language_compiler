#include "system.h"
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <stdio.h>
#include <stdint.h>

// 路径安全检查
static bool is_path_safe(const char* path) {
    if (!path) return false;
    
    if (strstr(path, "..") != NULL) {
        return false;
    }
    
    if (path[0] == '\\' && path[1] == '\\') {
        return false;
    }
    
    if (strstr(path, "\\\\") ) {
        return false;
    }
    
    return true;
}

#ifdef _WIN32
#include <windows.h>
#include <process.h>
#include <direct.h>
#include <io.h>
#include <psapi.h>
#include <wininet.h>
#pragma comment(lib, "wininet.lib")
// 定义UNLEN宏，如果未定义
#ifndef UNLEN
#define UNLEN 256
#endif
#elif defined(__APPLE__)
#include <unistd.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/syscall.h>
#include <sys/wait.h>
#include <sys/utsname.h>
#include <sys/sysinfo.h>
#include <dirent.h>
#include <pwd.h>
#include <errno.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <fcntl.h>
#include <mach-o/dyld.h>
#elif defined(__FreeBSD__)
#include <unistd.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/sysctl.h>
#include <sys/wait.h>
#include <sys/utsname.h>
#include <dirent.h>
#include <pwd.h>
#include <errno.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <fcntl.h>
#else
// Linux 和其他 Unix 系统
#include <unistd.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/utsname.h>
#include <sys/sysinfo.h>
#include <dirent.h>
#include <pwd.h>
#include <errno.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <fcntl.h>
#endif

// 系统信息函数
const char* system_get_os_name() {
#ifdef _WIN32
    return "Windows";
#else
    struct utsname info;
    if (uname(&info) == 0) {
        return info.sysname;
    }
    return "Unknown";
#endif
}

const char* system_get_os_version() {
#ifdef _WIN32
    static char version[256];
    // 使用 RtlGetVersion 替代已废弃的 GetVersionEx，支持 Windows 8.1+
    typedef LONG (NTAPI *RtlGetVersionFunc)(OSVERSIONINFOEXW*);
    HMODULE hMod = GetModuleHandleW(L"ntdll.dll");
    if (hMod) {
        RtlGetVersionFunc pRtlGetVersion = (RtlGetVersionFunc)GetProcAddress(hMod, "RtlGetVersion");
        if (pRtlGetVersion) {
            OSVERSIONINFOEXW info;
            ZeroMemory(&info, sizeof(OSVERSIONINFOEXW));
            info.dwOSVersionInfoSize = sizeof(OSVERSIONINFOEXW);
            if (pRtlGetVersion(&info) == 0) {
                sprintf(version, "%lu.%lu.%lu", info.dwMajorVersion, info.dwMinorVersion, info.dwBuildNumber);
                return version;
            }
        }
    }
    // 回退方案：返回通用 Windows 版本
    sprintf(version, "Windows NT 10.0");
    return version;
#else
    struct utsname info;
    if (uname(&info) == 0) {
        return info.release;
    }
    return "Unknown";
#endif
}

const char* system_get_cpu_architecture() {
#ifdef _WIN32
    SYSTEM_INFO info;
    GetSystemInfo(&info);
    switch (info.wProcessorArchitecture) {
        case PROCESSOR_ARCHITECTURE_AMD64:
            return "x86_64";
        case PROCESSOR_ARCHITECTURE_INTEL:
            return "x86";
        case PROCESSOR_ARCHITECTURE_ARM:
            return "ARM";
        case PROCESSOR_ARCHITECTURE_ARM64:
            return "ARM64";
        default:
            return "Unknown";
    }
#else
    #ifdef __x86_64__
    return "x86_64";
    #elif __i386__
    return "x86";
    #elif __arm__
    return "ARM";
    #elif __aarch64__
    return "ARM64";
    #else
    return "Unknown";
    #endif
#endif
}

size_t system_get_cpu_count() {
#ifdef _WIN32
    SYSTEM_INFO info;
    GetSystemInfo(&info);
    return info.dwNumberOfProcessors;
#else
    return sysconf(_SC_NPROCESSORS_ONLN);
#endif
}

size_t system_get_total_memory() {
#ifdef _WIN32
    MEMORYSTATUSEX info;
    info.dwLength = sizeof(MEMORYSTATUSEX);
    if (GlobalMemoryStatusEx(&info)) {
        return info.ullTotalPhys;
    }
    return 0;
#else
    struct sysinfo info;
    if (sysinfo(&info) == 0) {
        return (size_t)info.totalram * info.mem_unit;
    }
    return 0;
#endif
}

size_t system_get_available_memory() {
#ifdef _WIN32
    MEMORYSTATUSEX info;
    info.dwLength = sizeof(MEMORYSTATUSEX);
    if (GlobalMemoryStatusEx(&info)) {
        return info.ullAvailPhys;
    }
    return 0;
#else
    struct sysinfo info;
    if (sysinfo(&info) == 0) {
        return (size_t)info.freeram * info.mem_unit;
    }
    return 0;
#endif
}

// 时间函数
Timestamp system_get_timestamp() {
#ifdef _WIN32
    return GetTickCount64();
#else
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (Timestamp)(ts.tv_sec * 1000 + ts.tv_nsec / 1000000);
#endif
}

void system_get_current_time(Time* time) {
    if (!time) return;
    time_t now = _time64(NULL);
    struct tm* tm_info = localtime(&now);
    time->year = tm_info->tm_year + 1900;
    time->month = tm_info->tm_mon + 1;
    time->day = tm_info->tm_mday;
    time->hour = tm_info->tm_hour;
    time->minute = tm_info->tm_min;
    time->second = tm_info->tm_sec;
    time->millisecond = 0;
}

Timestamp system_time_to_timestamp(const Time* time) {
    if (!time) return 0;
    struct tm tm_info = {
        .tm_year = time->year - 1900,
        .tm_mon = time->month - 1,
        .tm_mday = time->day,
        .tm_hour = time->hour,
        .tm_min = time->minute,
        .tm_sec = time->second
    };
    time_t t = mktime(&tm_info);
    return (Timestamp)(t * 1000 + time->millisecond);
}

void system_timestamp_to_time(Timestamp timestamp, Time* time) {
    if (!time) return;
    time_t t = timestamp / 1000;
    struct tm* tm_info = localtime(&t);
    time->year = tm_info->tm_year + 1900;
    time->month = tm_info->tm_mon + 1;
    time->day = tm_info->tm_mday;
    time->hour = tm_info->tm_hour;
    time->minute = tm_info->tm_min;
    time->second = tm_info->tm_sec;
    time->millisecond = timestamp % 1000;
}

double system_get_uptime() {
#ifdef _WIN32
    return (double)GetTickCount64() / 1000.0;
#else
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec + ts.tv_nsec / 1e9;
#endif
}

void system_sleep_ms(uint32_t milliseconds) {
#ifdef _WIN32
    Sleep(milliseconds);
#else
    usleep(milliseconds * 1000);
#endif
}

// 环境变量函数
const char* system_get_env(const char* name) {
    return getenv(name);
}

bool system_set_env(const char* name, const char* value) {
#ifdef _WIN32
    return SetEnvironmentVariable(name, value) != 0;
#else
    return setenv(name, value, 1) == 0;
#endif
}

bool system_unset_env(const char* name) {
#ifdef _WIN32
    return SetEnvironmentVariable(name, NULL) != 0;
#else
    return unsetenv(name) == 0;
#endif
}

char** system_get_env_list() {
#ifdef _WIN32
    // Windows 使用 GetEnvironmentStrings 获取所有环境变量
    static char* env_buffer = NULL;
    static char** env_array = NULL;
    static size_t env_count = 0;
    
    if (env_array) {
        return env_array;
    }
    
    LPWSTR env_block = GetEnvironmentStringsW();
    if (!env_block) {
        return NULL;
    }
    
    // 计算环境变量数量
    LPWSTR p = env_block;
    while (*p != L'\0') {
        env_count++;
        while (*p != L'\0') p++;
        p++;
    }
    
    // 分配数组
    env_array = (char**)malloc((env_count + 1) * sizeof(char*));
    if (!env_array) {
        FreeEnvironmentStringsW(env_block);
        return NULL;
    }
    
    // 转换并存储
    env_buffer = (char*)malloc(32768);
    if (!env_buffer) {
        free(env_array);
        env_array = NULL;
        FreeEnvironmentStringsW(env_block);
        return NULL;
    }
    
    p = env_block;
    for (size_t i = 0; i < env_count; i++) {
        env_array[i] = NULL;
        while (*p != L'\0') {
            p++;
        }
        p++;
    }
    
    env_array[env_count] = NULL;
    FreeEnvironmentStringsW(env_block);
    return env_array;
#else
    return environ;
#endif
}

// 进程函数
ProcessId system_get_current_process_id() {
#ifdef _WIN32
    return GetCurrentProcessId();
#else
    return getpid();
#endif
}

ProcessId system_get_parent_process_id() {
#ifdef _WIN32
    return GetCurrentProcessId(); // Windows 没有直接获取父进程ID的API
#else
    return getppid();
#endif
}

static bool is_safe_command(const char* cmd) {
    if (!cmd) return false;
    
    const char* dangerous_chars[] = {"&", "|", ";", "`", "$", "(", ")", "<", ">", "\\", "\n", "\r", NULL};
    for (int i = 0; dangerous_chars[i] != NULL; i++) {
        if (strstr(cmd, dangerous_chars[i]) != NULL) {
            return false;
        }
    }
    
    const char* dangerous_cmds[] = {"del", "rm", "format", "fdisk", "shutdown", "reboot", "curl", "wget", "powershell", "cmd", "bash", NULL};
    for (int i = 0; dangerous_cmds[i] != NULL; i++) {
        if (strstr(cmd, dangerous_cmds[i]) != NULL) {
            return false;
        }
    }
    
    return true;
}

int system_execute(const char* command, char* output, size_t output_size) {
    if (!command) {
        return -1;
    }
    
    if (!is_safe_command(command)) {
        fprintf(stderr, "Error: Command contains unsafe characters or commands\n");
        return -1;
    }
    
#ifdef _WIN32
    FILE* pipe = _popen(command, "r");
    if (!pipe) return -1;
    if (output && output_size > 0) {
        size_t total_read = 0;
        size_t bytes_read;
        while ((bytes_read = fread(output + total_read, 1, 1, pipe)) > 0) {
            total_read += bytes_read;
            if (total_read >= output_size - 1) {
                break;
            }
        }
        output[total_read] = '\0';
    }
    int result = _pclose(pipe);
    return result;
#else
    FILE* pipe = popen(command, "r");
    if (!pipe) return -1;
    if (output && output_size > 0) {
        size_t total_read = 0;
        size_t bytes_read;
        while ((bytes_read = fread(output + total_read, 1, 1, pipe)) > 0) {
            total_read += bytes_read;
            if (total_read >= output_size - 1) {
                break;
            }
        }
        output[total_read] = '\0';
    }
    int result = pclose(pipe);
    return result;
#endif
}

int system_execute_with_args(const char* command, char* const args[], char* output, size_t output_size) {
    if (!command || !args) {
        return -1;
    }
    
    size_t cmd_len = strlen(command);
    size_t args_len = 0;
    for (int i = 0; args[i]; i++) {
        if (!is_safe_command(args[i])) {
            fprintf(stderr, "Error: Argument %d contains unsafe characters\n", i);
            return -1;
        }
        args_len += strlen(args[i]) + 1;
    }
    
    size_t full_len = cmd_len + args_len + 1;
    char* full_cmd = (char*)malloc(full_len);
    if (!full_cmd) return -1;
    
    strcpy(full_cmd, command);
    for (int i = 0; args[i]; i++) {
        strcat(full_cmd, " ");
        strcat(full_cmd, args[i]);
    }
    
    int result = system_execute(full_cmd, output, output_size);
    free(full_cmd);
    return result;
}

// 文件系统函数
bool system_file_exists(const char* path) {
    if (!path) return false;
#ifdef _WIN32
    return GetFileAttributes(path) != INVALID_FILE_ATTRIBUTES;
#else
    struct stat st;
    return stat(path, &st) == 0;
#endif
}

bool system_file_is_regular(const char* path) {
    if (!path) return false;
#ifdef _WIN32
    DWORD attr = GetFileAttributes(path);
    return attr != INVALID_FILE_ATTRIBUTES && !(attr & FILE_ATTRIBUTE_DIRECTORY);
#else
    struct stat st;
    if (stat(path, &st) == 0) {
        return S_ISREG(st.st_mode);
    }
    return false;
#endif
}

bool system_file_is_directory(const char* path) {
    if (!path) return false;
#ifdef _WIN32
    DWORD attr = GetFileAttributes(path);
    return attr != INVALID_FILE_ATTRIBUTES && (attr & FILE_ATTRIBUTE_DIRECTORY);
#else
    struct stat st;
    if (stat(path, &st) == 0) {
        return S_ISDIR(st.st_mode);
    }
    return false;
#endif
}

size_t system_file_size(const char* path) {
    if (!path) return 0;
#ifdef _WIN32
    WIN32_FIND_DATA data;
    HANDLE hFind = FindFirstFile(path, &data);
    if (hFind == INVALID_HANDLE_VALUE) return 0;
    FindClose(hFind);
    return (size_t)data.nFileSizeLow;
#else
    struct stat st;
    if (stat(path, &st) == 0) {
        return (size_t)st.st_size;
    }
    return 0;
#endif
}

bool system_file_delete(const char* path) {
    if (!path) return false;
#ifdef _WIN32
    return DeleteFile(path) != 0;
#else
    return unlink(path) == 0;
#endif
}

bool system_file_copy(const char* src, const char* dst) {
    if (!src || !dst) return false;
#ifdef _WIN32
    return CopyFile(src, dst, FALSE) != 0;
#else
    FILE* src_file = fopen(src, "rb");
    if (!src_file) return false;
    FILE* dst_file = fopen(dst, "wb");
    if (!dst_file) {
        fclose(src_file);
        return false;
    }
    char buffer[4096];
    size_t read;
    while ((read = fread(buffer, 1, sizeof(buffer), src_file)) > 0) {
        if (fwrite(buffer, 1, read, dst_file) != read) {
            fclose(src_file);
            fclose(dst_file);
            return false;
        }
    }
    fclose(src_file);
    fclose(dst_file);
    return true;
#endif
}

bool system_file_move(const char* src, const char* dst) {
    if (!src || !dst) return false;
#ifdef _WIN32
    return MoveFile(src, dst) != 0;
#else
    return rename(src, dst) == 0;
#endif
}

bool system_directory_create(const char* path) {
    if (!path) return false;
#ifdef _WIN32
    return CreateDirectory(path, NULL) != 0;
#else
    return mkdir(path, 0755) == 0;
#endif
}

bool system_directory_delete(const char* path) {
    if (!path) return false;
#ifdef _WIN32
    return RemoveDirectory(path) != 0;
#else
    return rmdir(path) == 0;
#endif
}

bool system_directory_exists(const char* path) {
    return system_file_is_directory(path);
}

char** system_directory_list(const char* path, size_t* count) {
    if (count) *count = 0;
    
    if (!path) {
        return NULL;
    }
    
    if (!is_path_safe(path)) {
        fprintf(stderr, "Error: Unsafe path in directory_list\n");
        return NULL;
    }
    
#ifdef _WIN32
    WIN32_FIND_DATA data;
    char search_path[1024];
    sprintf(search_path, "%s\\*", path);
    HANDLE hFind = FindFirstFile(search_path, &data);
    if (hFind == INVALID_HANDLE_VALUE) return NULL;
    size_t file_count = 0;
    while (FindNextFile(hFind, &data)) {
        if (strcmp(data.cFileName, ".") != 0 && strcmp(data.cFileName, "..") != 0) {
            file_count++;
        }
    }
    FindClose(hFind);
    if (file_count == 0) return NULL;
    char** result = (char**)malloc((file_count + 1) * sizeof(char*));
    if (!result) return NULL;
    hFind = FindFirstFile(search_path, &data);
    size_t index = 0;
    while (FindNextFile(hFind, &data)) {
        if (strcmp(data.cFileName, ".") != 0 && strcmp(data.cFileName, "..") != 0) {
            result[index] = strdup(data.cFileName);
            index++;
        }
    }
    FindClose(hFind);
    result[index] = NULL;
    if (count) *count = file_count;
    return result;
#else
    DIR* dir = opendir(path);
    if (!dir) return NULL;
    size_t file_count = 0;
    struct dirent* entry;
    while ((entry = readdir(dir)) != NULL) {
        if (strcmp(entry->d_name, ".") != 0 && strcmp(entry->d_name, "..") != 0) {
            file_count++;
        }
    }
    rewinddir(dir);
    if (file_count == 0) {
        closedir(dir);
        return NULL;
    }
    char** result = (char**)malloc((file_count + 1) * sizeof(char*));
    if (!result) {
        closedir(dir);
        return NULL;
    }
    size_t index = 0;
    while ((entry = readdir(dir)) != NULL) {
        if (strcmp(entry->d_name, ".") != 0 && strcmp(entry->d_name, "..") != 0) {
            result[index] = strdup(entry->d_name);
            index++;
        }
    }
    closedir(dir);
    result[index] = NULL;
    if (count) *count = file_count;
    return result;
#endif
}

// 路径函数
char* system_get_current_directory() {
#ifdef _WIN32
    char* buffer = (char*)malloc(MAX_PATH);
    if (buffer && GetCurrentDirectory(MAX_PATH, buffer)) {
        return buffer;
    }
    free(buffer);
    return NULL;
#else
    char* buffer = getcwd(NULL, 0);
    return buffer;
#endif
}

bool system_change_directory(const char* path) {
#ifdef _WIN32
    return SetCurrentDirectory(path) != 0;
#else
    return chdir(path) == 0;
#endif
}

char* system_get_executable_path() {
#ifdef _WIN32
    char* buffer = (char*)malloc(MAX_PATH);
    if (buffer && GetModuleFileName(NULL, buffer, MAX_PATH)) {
        return buffer;
    }
    free(buffer);
    return NULL;
#elif defined(__APPLE__)
    // macOS 使用 _NSGetExecutablePath
    uint32_t bufsize = 1024;
    char* buffer = (char*)malloc(bufsize);
    if (buffer) {
        if (_NSGetExecutablePath(buffer, &bufsize) == 0) {
            return buffer;
        }
        free(buffer);
        // 重试更大的缓冲区
        buffer = (char*)malloc(bufsize);
        if (buffer && _NSGetExecutablePath(buffer, &bufsize) == 0) {
            return buffer;
        }
        free(buffer);
    }
    return NULL;
#elif defined(__linux__)
    char* buffer = (char*)malloc(1024);
    if (buffer && readlink("/proc/self/exe", buffer, 1024) != -1) {
        return buffer;
    }
    free(buffer);
    return NULL;
#elif defined(__FreeBSD__)
    // FreeBSD 使用 KERN_PROC_PATHNAME
    int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_PATHNAME, -1};
    char buffer[1024];
    size_t cb = sizeof(buffer);
    if (sysctl(mib, 4, buffer, &cb, NULL, 0) == 0) {
        return strdup(buffer);
    }
    return NULL;
#else
    // 其他 Unix 系统回退方案
    char* buffer = (char*)malloc(1024);
    if (buffer) {
        ssize_t len = readlink("/proc/curproc/file", buffer, 1023);
        if (len != -1) {
            buffer[len] = '\0';
            return buffer;
        }
    }
    free(buffer);
    return NULL;
#endif
}

char* system_get_home_directory() {
#ifdef _WIN32
    char* buffer = (char*)malloc(MAX_PATH);
    if (buffer && GetEnvironmentVariable("USERPROFILE", buffer, MAX_PATH)) {
        return buffer;
    }
    free(buffer);
    return NULL;
#elif defined(__APPLE__)
    // macOS 优先使用 HOME 环境变量
    char* home = getenv("HOME");
    if (home) {
        return strdup(home);
    }
    // 回退到 getpwuid
    struct passwd* pw = getpwuid(getuid());
    if (pw) {
        return strdup(pw->pw_dir);
    }
    return NULL;
#else
    // Linux 和其他 Unix 系统
    char* home = getenv("HOME");
    if (home) {
        return strdup(home);
    }
    struct passwd* pw = getpwuid(getuid());
    if (pw) {
        return strdup(pw->pw_dir);
    }
    return NULL;
#endif
}

// 系统错误函数
int system_get_last_error() {
#ifdef _WIN32
    return GetLastError();
#else
    return errno;
#endif
}

const char* system_get_error_message(int error_code) {
#ifdef _WIN32
    static char buffer[256];
    FormatMessage(FORMAT_MESSAGE_FROM_SYSTEM, NULL, error_code, 
                  MAKELANGID(LANG_NEUTRAL, SUBLANG_DEFAULT), 
                  buffer, sizeof(buffer), NULL);
    return buffer;
#else
    return strerror(error_code);
#endif
}

void system_clear_error() {
#ifdef _WIN32
    SetLastError(0);
#else
    errno = 0;
#endif
}

// 网络函数
bool system_network_is_available() {
#ifdef _WIN32
    DWORD flags;
    return InternetGetConnectedState(&flags, 0) != 0;
#else
    // 简单实现，尝试打开一个常用端口
    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) return false;
    struct sockaddr_in addr;
    addr.sin_family = AF_INET;
    addr.sin_port = htons(80);
    addr.sin_addr.s_addr = inet_addr("8.8.8.8");
    int result = connect(sock, (struct sockaddr*)&addr, sizeof(addr));
    close(sock);
    return result == 0;
#endif
}

const char* system_get_hostname() {
#ifdef _WIN32
    static char buffer[256];
    if (GetComputerName(buffer, &(DWORD){sizeof(buffer)})) {
        return buffer;
    }
    return "Unknown";
#else
    static char buffer[256];
    if (gethostname(buffer, sizeof(buffer)) == 0) {
        return buffer;
    }
    return "Unknown";
#endif
}

// 电源管理函数
bool system_is_battery_powered() {
#ifdef _WIN32
    SYSTEM_POWER_STATUS status;
    if (GetSystemPowerStatus(&status)) {
        return status.BatteryFlag != 128; // 128 means no battery
    }
    return false;
#else
    // 检查多个可能的电池路径
    const char* battery_paths[] = {
        "/sys/class/power_supply/BAT0",
        "/sys/class/power_supply/battery",
        "/proc/acpi/battery/BAT0",
        "/sys/class/power_supply/BAT1",
        NULL
    };
    for (int i = 0; battery_paths[i] != NULL; i++) {
        if (access(battery_paths[i], F_OK) == 0) {
            return true;
        }
    }
    return false;
#endif
}

int system_get_battery_percentage() {
#ifdef _WIN32
    SYSTEM_POWER_STATUS status;
    if (GetSystemPowerStatus(&status)) {
        return status.BatteryLifePercent;
    }
    return -1;
#else
    // 尝试多个可能的电池百分比路径
    const char* battery_paths[] = {
        "/sys/class/power_supply/BAT0/capacity",
        "/sys/class/power_supply/battery/capacity",
        "/proc/acpi/battery/BAT0/state",
        "/sys/class/power_supply/BAT1/capacity",
        NULL
    };
    
    for (int i = 0; battery_paths[i] != NULL; i++) {
        FILE* file = fopen(battery_paths[i], "r");
        if (file) {
            int percentage;
            if (fscanf(file, "%d", &percentage) == 1) {
                fclose(file);
                return percentage;
            }
            fclose(file);
        }
    }
    return -1;
#endif
}

// Windows API 函数实现
#ifdef _WIN32

bool system_windows_registry_set(const char* key, const char* value_name, const char* value) {
    HKEY hKey;
    LONG result = RegOpenKeyEx(HKEY_CURRENT_USER, key, 0, KEY_SET_VALUE, &hKey);
    if (result != ERROR_SUCCESS) {
        return false;
    }
    result = RegSetValueEx(hKey, value_name, 0, REG_SZ, (const BYTE*)value, (DWORD)strlen(value) + 1);
    RegCloseKey(hKey);
    return result == ERROR_SUCCESS;
}

const char* system_windows_registry_get(const char* key, const char* value_name) {
    static char buffer[1024];
    HKEY hKey;
    DWORD size = sizeof(buffer);
    LONG result = RegOpenKeyEx(HKEY_CURRENT_USER, key, 0, KEY_QUERY_VALUE, &hKey);
    if (result != ERROR_SUCCESS) {
        return NULL;
    }
    result = RegQueryValueEx(hKey, value_name, NULL, NULL, (LPBYTE)buffer, &size);
    RegCloseKey(hKey);
    if (result != ERROR_SUCCESS) {
        return NULL;
    }
    return buffer;
}

bool system_windows_registry_delete(const char* key, const char* value_name) {
    HKEY hKey;
    LONG result = RegOpenKeyEx(HKEY_CURRENT_USER, key, 0, KEY_SET_VALUE, &hKey);
    if (result != ERROR_SUCCESS) {
        return false;
    }
    result = RegDeleteValue(hKey, value_name);
    RegCloseKey(hKey);
    return result == ERROR_SUCCESS;
}

bool system_windows_create_process(const char* command, bool show_window) {
    STARTUPINFO si;
    PROCESS_INFORMATION pi;
    ZeroMemory(&si, sizeof(si));
    si.cb = sizeof(si);
    if (!show_window) {
        si.dwFlags = STARTF_USESHOWWINDOW;
        si.wShowWindow = SW_HIDE;
    }
    ZeroMemory(&pi, sizeof(pi));
    bool success = CreateProcess(NULL, (LPSTR)command, NULL, NULL, FALSE, 0, NULL, NULL, &si, &pi);
    if (success) {
        CloseHandle(pi.hProcess);
        CloseHandle(pi.hThread);
    }
    return success;
}

bool system_windows_get_process_info(ProcessId pid, char* name, size_t name_size, size_t* memory_usage) {
    HANDLE hProcess = OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, FALSE, pid);
    if (!hProcess) {
        return false;
    }
    bool success = false;
    if (name && name_size > 0) {
        DWORD size = name_size;
        if (QueryFullProcessImageName(hProcess, 0, name, &size)) {
            success = true;
        }
    }
    if (memory_usage) {
        PROCESS_MEMORY_COUNTERS pmc;
        if (GetProcessMemoryInfo(hProcess, &pmc, sizeof(pmc))) {
            *memory_usage = pmc.WorkingSetSize;
            success = true;
        }
    }
    CloseHandle(hProcess);
    return success;
}

bool system_windows_get_service_status(const char* service_name, char* status, size_t status_size) {
    SC_HANDLE hSCManager = OpenSCManager(NULL, NULL, SC_MANAGER_CONNECT);
    if (!hSCManager) {
        return false;
    }
    SC_HANDLE hService = OpenService(hSCManager, service_name, SERVICE_QUERY_STATUS);
    if (!hService) {
        CloseServiceHandle(hSCManager);
        return false;
    }
    SERVICE_STATUS_PROCESS ssStatus;
    DWORD dwBytesNeeded;
    bool success = QueryServiceStatusEx(hService, SC_STATUS_PROCESS_INFO, (LPBYTE)&ssStatus, sizeof(SERVICE_STATUS_PROCESS), &dwBytesNeeded);
    if (success && status && status_size > 0) {
        switch (ssStatus.dwCurrentState) {
            case SERVICE_STOPPED:
                strncpy(status, "STOPPED", status_size);
                break;
            case SERVICE_START_PENDING:
                strncpy(status, "START_PENDING", status_size);
                break;
            case SERVICE_STOP_PENDING:
                strncpy(status, "STOP_PENDING", status_size);
                break;
            case SERVICE_RUNNING:
                strncpy(status, "RUNNING", status_size);
                break;
            case SERVICE_CONTINUE_PENDING:
                strncpy(status, "CONTINUE_PENDING", status_size);
                break;
            case SERVICE_PAUSE_PENDING:
                strncpy(status, "PAUSE_PENDING", status_size);
                break;
            case SERVICE_PAUSED:
                strncpy(status, "PAUSED", status_size);
                break;
            default:
                strncpy(status, "UNKNOWN", status_size);
                break;
        }
        status[status_size - 1] = '\0';
    }
    CloseServiceHandle(hService);
    CloseServiceHandle(hSCManager);
    return success;
}

bool system_windows_start_service(const char* service_name) {
    SC_HANDLE hSCManager = OpenSCManager(NULL, NULL, SC_MANAGER_CONNECT);
    if (!hSCManager) {
        return false;
    }
    SC_HANDLE hService = OpenService(hSCManager, service_name, SERVICE_START);
    if (!hService) {
        CloseServiceHandle(hSCManager);
        return false;
    }
    bool success = StartService(hService, 0, NULL) != 0;
    CloseServiceHandle(hService);
    CloseServiceHandle(hSCManager);
    return success;
}

bool system_windows_stop_service(const char* service_name) {
    SC_HANDLE hSCManager = OpenSCManager(NULL, NULL, SC_MANAGER_CONNECT);
    if (!hSCManager) {
        return false;
    }
    SC_HANDLE hService = OpenService(hSCManager, service_name, SERVICE_STOP);
    if (!hService) {
        CloseServiceHandle(hSCManager);
        return false;
    }
    SERVICE_STATUS ssStatus;
    bool success = ControlService(hService, SERVICE_CONTROL_STOP, &ssStatus) != 0;
    CloseServiceHandle(hService);
    CloseServiceHandle(hSCManager);
    return success;
}

const char* system_windows_get_computer_name() {
    static char buffer[MAX_COMPUTERNAME_LENGTH + 1];
    DWORD size = sizeof(buffer);
    if (GetComputerName(buffer, &size)) {
        return buffer;
    }
    return "Unknown";
}

const char* system_windows_get_username() {
    static char buffer[UNLEN + 1];
    DWORD size = sizeof(buffer);
    if (GetUserName(buffer, &size)) {
        return buffer;
    }
    return "Unknown";
}

bool system_windows_set_console_title(const char* title) {
    return SetConsoleTitle(title) != 0;
}

// 更多 Windows API 函数实现
bool system_windows_show_message_box(const char* title, const char* message, int type) {
    UINT uType = MB_OK;
    switch (type) {
        case 1:
            uType = MB_OKCANCEL;
            break;
        case 2:
            uType = MB_YESNO;
            break;
        case 3:
            uType = MB_ICONERROR;
            break;
        case 4:
            uType = MB_ICONINFORMATION;
            break;
        case 5:
            uType = MB_ICONWARNING;
            break;
    }
    int result = MessageBox(NULL, message, title, uType);
    return result != 0;
}

bool system_windows_get_screen_size(int* width, int* height) {
    if (!width || !height) {
        return false;
    }
    HDC hdc = GetDC(NULL);
    if (!hdc) {
        return false;
    }
    *width = GetDeviceCaps(hdc, HORZRES);
    *height = GetDeviceCaps(hdc, VERTRES);
    ReleaseDC(NULL, hdc);
    return true;
}

bool system_windows_set_cursor_position(int x, int y) {
    return SetCursorPos(x, y) != 0;
}

bool system_windows_get_cursor_position(int* x, int* y) {
    if (!x || !y) {
        return false;
    }
    POINT point;
    if (GetCursorPos(&point)) {
        *x = point.x;
        *y = point.y;
        return true;
    }
    return false;
}

bool system_windows_get_desktop_background(char* path, size_t path_size) {
    if (!path || path_size == 0) {
        return false;
    }
    HKEY hKey;
    LONG result = RegOpenKeyEx(HKEY_CURRENT_USER, "Control Panel\\Desktop", 0, KEY_QUERY_VALUE, &hKey);
    if (result != ERROR_SUCCESS) {
        return false;
    }
    DWORD size = path_size;
    result = RegQueryValueEx(hKey, "Wallpaper", NULL, NULL, (LPBYTE)path, &size);
    RegCloseKey(hKey);
    return result == ERROR_SUCCESS;
}

bool system_windows_set_desktop_background(const char* path) {
    SystemParametersInfo(SPI_SETDESKWALLPAPER, 0, (void*)path, SPIF_UPDATEINIFILE | SPIF_SENDCHANGE);
    return true;
}

bool system_windows_get_system_directory(char* path, size_t path_size) {
    if (!path || path_size == 0) {
        return false;
    }
    UINT size = GetSystemDirectory(path, (UINT)path_size);
    return size > 0 && size < path_size;
}

bool system_windows_get_windows_directory(char* path, size_t path_size) {
    if (!path || path_size == 0) {
        return false;
    }
    UINT size = GetWindowsDirectory(path, (UINT)path_size);
    return size > 0 && size < path_size;
}

bool system_windows_get_temp_directory(char* path, size_t path_size) {
    if (!path || path_size == 0) {
        return false;
    }
    DWORD size = GetTempPath((DWORD)path_size, path);
    return size > 0 && size < path_size;
}

bool system_windows_get_environment_variable(const char* name, char* value, size_t value_size) {
    if (!name || !value || value_size == 0) {
        return false;
    }
    DWORD size = GetEnvironmentVariable(name, value, (DWORD)value_size);
    return size > 0 && size < value_size;
}

bool system_windows_set_environment_variable(const char* name, const char* value) {
    if (!name) {
        return false;
    }
    return SetEnvironmentVariable(name, value) != 0;
}

#endif

// 系统 syscall 函数实现
int system_syscall_open(const char* path, int flags, int mode) {
#ifdef _WIN32
    return _open(path, flags, mode);
#else
    return open(path, flags, mode);
#endif
}

int system_syscall_close(int fd) {
#ifdef _WIN32
    return _close(fd);
#else
    return close(fd);
#endif
}

ssize_t system_syscall_read(int fd, void* buf, size_t count) {
#ifdef _WIN32
    return _read(fd, buf, count);
#else
    return read(fd, buf, count);
#endif
}

ssize_t system_syscall_write(int fd, const void* buf, size_t count) {
#ifdef _WIN32
    return _write(fd, buf, count);
#else
    return write(fd, buf, count);
#endif
}

off_t system_syscall_lseek(int fd, off_t offset, int whence) {
#ifdef _WIN32
    return _lseek(fd, offset, whence);
#else
    return lseek(fd, offset, whence);
#endif
}

int system_syscall_fork() {
#ifdef _WIN32
    // Windows 不支持 fork，返回 -1 表示不支持
    return -1;
#else
    return fork();
#endif
}

int system_syscall_execve(const char* path, char* const argv[], char* const envp[]) {
#ifdef _WIN32
    // Windows 不直接支持 execve，使用 spawn 替代
    return _spawnve(_P_OVERLAY, path, argv, envp);
#else
    return execve(path, argv, envp);
#endif
}

int system_syscall_waitpid(int pid, int* status, int options) {
#ifdef _WIN32
    return _cwait(status, pid, options);
#else
    return waitpid(pid, status, options);
#endif
}

int system_syscall_kill(int pid, int sig) {
#ifdef _WIN32
    return TerminateProcess((HANDLE)pid, sig);
#else
    return kill(pid, sig);
#endif
}

void* system_syscall_malloc(size_t size) {
    return malloc(size);
}

void system_syscall_free(void* ptr) {
    free(ptr);
}

void* system_syscall_realloc(void* ptr, size_t size) {
    return realloc(ptr, size);
}

void* system_syscall_calloc(size_t count, size_t size) {
    return calloc(count, size);
}
