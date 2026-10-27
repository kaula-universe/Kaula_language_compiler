// freestanding/include/sys/socket.h — 裸机模式的 <sys/socket.h> 替身（仅原型）
//
// 语义边界：裸机没有网络协议栈。std.web 的头会 include 它来取 socket/sockaddr
// 类型。这里只给类型与常量，保证头文件能解析；所有 socket 调用保持未定义 ——
// 内核要用网络就得自己接协议栈。
#ifndef KAULA_FS_COMPAT_SYS_SOCKET_H
#define KAULA_FS_COMPAT_SYS_SOCKET_H

#include <stddef.h>
#include <stdint.h>
#include <sys/types.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef uint32_t socklen_t;
typedef uint16_t sa_family_t;
typedef uint16_t in_port_t;
typedef uint32_t in_addr_t;

struct sockaddr {
    sa_family_t sa_family;
    char sa_data[14];
};

struct sockaddr_storage {
    sa_family_t ss_family;
    char __ss_pad[126];
};

// in_addr / sockaddr_in 等在 netinet/in.h shim 里定义（与真实系统一致的分层），
// 加守卫避免重复定义。

struct addrinfo {
    int ai_flags;
    int ai_family;
    int ai_socktype;
    int ai_protocol;
    socklen_t ai_addrlen;
    struct sockaddr* ai_addr;
    char* ai_canonname;
    struct addrinfo* ai_next;
};

// 地址族
#define AF_UNSPEC   0
#define AF_UNIX     1
#define AF_INET     2
#define AF_INET6   10
#define AF_PACKET  17

// socket 类型与协议
#define SOCK_STREAM 1
#define SOCK_DGRAM  2
#define SOCK_RAW    3
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

// socket 选项
#define SOL_SOCKET  1
#define SO_REUSEADDR 2
#define SO_ERROR     4
#define SO_RCVTIMEO  20
#define SO_SNDTIMEO  21
#define SO_BROADCAST 6
#define SO_KEEPALIVE 9
#define SO_LINGER    13
#define SO_RCVBUF    8
#define SO_SNDBUF    7

// 关闭与错误
#define SHUT_RD   0
#define SHUT_WR   1
#define SHUT_RDWR 2

int socket(int domain, int type, int protocol);
int bind(int sockfd, const struct sockaddr* addr, socklen_t addrlen);
int listen(int sockfd, int backlog);
int accept(int sockfd, struct sockaddr* addr, socklen_t* addrlen);
int connect(int sockfd, const struct sockaddr* addr, socklen_t addrlen);
ssize_t send(int sockfd, const void* buf, size_t len, int flags);
ssize_t recv(int sockfd, void* buf, size_t len, int flags);
ssize_t sendto(int sockfd, const void* buf, size_t len, int flags,
               const struct sockaddr* dest, socklen_t addrlen);
ssize_t recvfrom(int sockfd, void* buf, size_t len, int flags,
                 struct sockaddr* src, socklen_t* addrlen);
int shutdown(int sockfd, int how);
int setsockopt(int sockfd, int level, int optname, const void* optval, socklen_t optlen);
int getsockopt(int sockfd, int level, int optname, void* optval, socklen_t* optlen);
int getaddrinfo(const char* node, const char* service,
                const struct addrinfo* hints, struct addrinfo** res);
void freeaddrinfo(struct addrinfo* res);
const char* gai_strerror(int errcode);

#ifdef __cplusplus
}
#endif

#endif /* KAULA_FS_COMPAT_SYS_SOCKET_H */
