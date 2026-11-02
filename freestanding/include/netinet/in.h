// freestanding/include/netinet/in.h — 裸机模式的 <netinet/in.h> 替身（仅原型）
//
// 裸机没有网络协议栈。std.web 的头会 include 它取 sockaddr_in 等类型；
// 这里只给结构与常量，保证头文件能解析，socket 调用保持未定义。
#ifndef KAULA_FS_COMPAT_NETINET_IN_H
#define KAULA_FS_COMPAT_NETINET_IN_H

#include <stdint.h>
#include <sys/socket.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef uint16_t in_port_t;
typedef uint32_t in_addr_t;

// 这些类型 sys/socket.h shim 已经定义过（真实系统头也是这么组织的），
// 重复定义会直接编译失败，故全部加守卫。
#ifndef __DEFINED_struct_in_addr
#define __DEFINED_struct_in_addr
struct in_addr { in_addr_t s_addr; };
#endif

#ifndef __DEFINED_struct_in6_addr
#define __DEFINED_struct_in6_addr
struct in6_addr { uint8_t s6_addr[16]; };
#endif

#ifndef __DEFINED_struct_sockaddr_in
#define __DEFINED_struct_sockaddr_in
struct sockaddr_in {
    sa_family_t sin_family;
    in_port_t sin_port;
    struct in_addr sin_addr;
    char sin_zero[8];
};
#endif

#ifndef __DEFINED_struct_sockaddr_in6
#define __DEFINED_struct_sockaddr_in6
struct sockaddr_in6 {
    sa_family_t sin6_family;
    in_port_t sin6_port;
    uint32_t sin6_flowinfo;
    struct in6_addr sin6_addr;
    uint32_t sin6_scope_id;
};
#endif

#define INADDR_ANY        ((in_addr_t)0x00000000)
#define INADDR_BROADCAST  ((in_addr_t)0xFFFFFFFF)
#define INADDR_LOOPBACK   ((in_addr_t)0x7F000001)
#define INET_ADDRSTRLEN   16
#define INET6_ADDRSTRLEN  46

uint32_t htonl(uint32_t hostlong);
uint16_t htons(uint16_t hostshort);
uint32_t ntohl(uint32_t netlong);
uint16_t ntohs(uint16_t netshort);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_NETINET_IN_H */
