// freestanding/include/netdb.h — 裸机模式的 <netdb.h> 替身（仅原型）
//
// 裸机没有 DNS/协议栈。std.web 的头会 include 它取 addrinfo 与 getaddrinfo。
// 这里只给类型与常量（addrinfo 类型在 sys/socket.h shim 里），函数保持未定义。
#ifndef KAULA_FS_COMPAT_NETDB_H
#define KAULA_FS_COMPAT_NETDB_H

#include <netinet/in.h>
#include <sys/socket.h>

#ifdef __cplusplus
extern "C" {
#endif

// getaddrinfo 的 flags / 返回码（与 POSIX 一致）
#define AI_PASSIVE     0x00000001
#define AI_CANONNAME   0x00000002
#define AI_NUMERICHOST 0x00000004
#define AI_NUMERICSERV 0x00000008
#define AI_V4MAPPED    0x00000010
#define AI_ALL         0x00000020
#define AI_ADDRCONFIG   0x00000040
#define AI_IDN          0x00000080

#define EAI_BADFLAGS   -1
#define EAI_NONAME     -2
#define EAI_AGAIN      -3
#define EAI_FAIL       -4
#define EAI_FAMILY     -6
#define EAI_SERVICE    -8
#define EAI_MEMORY     -10
#define EAI_SYSTEM     -11

// 常见服务端口（网络字节序）
#define htons(x) ((uint16_t)((((uint16_t)(x) & 0xff) << 8) | (((uint16_t)(x) >> 8) & 0xff)))
#define ntohs(x) htons(x)

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_NETDB_H */
