#ifndef STD_NET_NET_H
#define STD_NET_NET_H

#include "../base/types.h"
#include "../string/string.h"

// Socket 类型
typedef struct Socket {
#if STD_PLATFORM_WINDOWS
    u64 handle;
#else
    int fd;
#endif
    int family;
    int type;
    int protocol;
    bool_t is_blocking;
    bool_t is_valid;
} Socket;

typedef struct IPAddress {
    u8 ip[16];
    u16 port;
    int family;
} IPAddress;

// Socket 函数
extern Socket* socket_create(int family, int type, int protocol);
extern void socket_destroy(Socket* sock);
extern bool_t socket_bind(Socket* sock, const String host, u16 port);
extern bool_t socket_listen(Socket* sock, int backlog);
extern Socket* socket_accept(Socket* sock);
extern bool_t socket_connect(Socket* sock, const String host, u16 port);
extern size_t socket_send(Socket* sock, const u8* data, size_t len);
extern size_t socket_receive(Socket* sock, u8* buffer, size_t len);
extern bool_t socket_close(Socket* sock);
extern bool_t socket_set_blocking(Socket* sock, bool_t blocking);
extern bool_t socket_set_timeout(Socket* sock, uint32_t timeout_ms);
extern bool_t socket_is_valid(Socket* sock);

// TCP
extern Socket* tcp_server_create(const String host, u16 port);
extern Socket* tcp_client_create();
extern bool_t tcp_connect(Socket* sock, const String host, u16 port);
extern size_t tcp_send(Socket* sock, const u8* data, size_t len);
extern size_t tcp_receive(Socket* sock, u8* buffer, size_t len);

// UDP
extern Socket* udp_socket_create();
extern bool_t udp_bind(Socket* sock, const String host, u16 port);
extern size_t udp_send_to(Socket* sock, const String host, u16 port, const u8* data, size_t len);
extern size_t udp_receive_from(Socket* sock, String* out_host, u16* out_port, u8* buffer, size_t len);
extern bool_t udp_set_broadcast(Socket* sock, bool_t enable);

// DNS
extern bool_t dns_resolve(const String hostname, String* ip_out);
extern bool_t dns_reverse_resolve(const String ip, String* hostname_out);

// 网络工具
extern bool_t net_is_valid_ip(const String ip);
extern String net_get_local_hostname();
extern String net_get_local_ip();

#endif // STD_NET_NET_H
