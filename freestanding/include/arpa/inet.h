// freestanding/include/arpa/inet.h — 裸机模式的 <arpa/inet.h> 替身（仅原型）
//
// 裸机没有网络。std.web 的头会 include 它取字节序转换与地址解析函数。
// 这里只声明，链接期未定义即"需要用户接协议栈"。
#ifndef KAULA_FS_COMPAT_ARPA_INET_H
#define KAULA_FS_COMPAT_ARPA_INET_H

#include <netinet/in.h>
#include <sys/socket.h>

#ifdef __cplusplus
extern "C" {
#endif

#ifndef INADDR_ANY
#define INADDR_ANY ((in_addr_t)0x00000000)
#endif
#ifndef INADDR_BROADCAST
#define INADDR_BROADCAST ((in_addr_t)0xFFFFFFFF)
#endif
#ifndef INADDR_LOOPBACK
#define INADDR_LOOPBACK ((in_addr_t)0x7F000001)
#endif
#ifndef INET_ADDRSTRLEN
#define INET_ADDRSTRLEN 16
#endif
#ifndef INET6_ADDRSTRLEN
#define INET6_ADDRSTRLEN 46
#endif

in_addr_t inet_addr(const char* cp);
char* inet_ntoa(struct in_addr addr);
int inet_aton(const char* cp, struct in_addr* inp);
in_addr_t inet_lpton(const char* cp);
const char* inet_ntop(int af, const void* src, char* dst, socklen_t size);
int inet_pton(int af, const char* src, void* dst);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_ARPA_INET_H */
