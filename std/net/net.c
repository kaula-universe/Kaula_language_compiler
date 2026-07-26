/* _POSIX_C_SOURCE 必须在所有 #include 之前定义，以确保 struct timeval
   在 <sys/time.h> 中可见（Linux 严格模式要求） */
#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L
#endif

#include "net.h"
#include "../memory/memory.h"
#include <string.h>
#include <stdio.h>

#if STD_PLATFORM_WINDOWS
    #include <winsock2.h>
    #include <ws2tcpip.h>
    #pragma comment(lib, "ws2_32.lib")
    #define SOCKET_INIT do { WSADATA wsa; static bool_t init = false; if (!init) { WSAStartup(MAKEWORD(2,2), &wsa); init = true; } } while(0)
    #define SOCKET_HANDLE SOCKET
#else
    #include <sys/socket.h>
    #include <sys/time.h>
    #include <netinet/in.h>
    #include <arpa/inet.h>
    #include <netdb.h>
    #include <unistd.h>
    #include <fcntl.h>
    #define SOCKET_INIT ((void)0)
    #define SOCKET_HANDLE int
    #define INVALID_SOCKET -1
    #define SOCKET_ERROR -1
#endif

Socket* socket_create(int family, int type, int protocol) {
    SOCKET_INIT;
    Socket* sock = (Socket*)kmm_v4_calloc(1, sizeof(Socket));
    if (sock) {
#if STD_PLATFORM_WINDOWS
        sock->handle = (u64)socket(family, type, protocol);
        sock->is_valid = sock->handle != INVALID_SOCKET;
#else
        sock->fd = socket(family, type, protocol);
        sock->is_valid = sock->fd >= 0;
#endif
        sock->family = family;
        sock->type = type;
        sock->protocol = protocol;
        sock->is_blocking = true;
    }
    return sock;
}

void socket_destroy(Socket* sock) {
    if (sock) { socket_close(sock); }
    // KMM 管理内存，无需手动释放
}

bool_t socket_bind(Socket* sock, const String host, u16 port) {
    if (!sock || !sock->is_valid) return false;
    struct sockaddr_in addr; memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET; addr.sin_port = htons(port);
    if (host.ptr) inet_pton(AF_INET, host.ptr, &addr.sin_addr);
    else addr.sin_addr.s_addr = htonl(INADDR_ANY);
#if STD_PLATFORM_WINDOWS
    return bind((SOCKET)sock->handle, (struct sockaddr*)&addr, sizeof(addr)) != SOCKET_ERROR;
#else
    return bind(sock->fd, (struct sockaddr*)&addr, sizeof(addr)) != SOCKET_ERROR;
#endif
}

bool_t socket_listen(Socket* sock, int backlog) {
    if (!sock || !sock->is_valid) return false;
#if STD_PLATFORM_WINDOWS
    return listen((SOCKET)sock->handle, backlog) != SOCKET_ERROR;
#else
    return listen(sock->fd, backlog) != SOCKET_ERROR;
#endif
}

Socket* socket_accept(Socket* sock) {
    if (!sock || !sock->is_valid) return NULL;
#if STD_PLATFORM_WINDOWS
    SOCKET client = accept((SOCKET)sock->handle, NULL, NULL);
    if (client == INVALID_SOCKET) return NULL;
#else
    int client = accept(sock->fd, NULL, NULL);
    if (client < 0) return NULL;
#endif
    Socket* new_sock = (Socket*)kmm_v4_calloc(1, sizeof(Socket));
    new_sock->family = sock->family;
    new_sock->type = sock->type;
    new_sock->protocol = sock->protocol;
    new_sock->is_blocking = true;
    new_sock->is_valid = true;
#if STD_PLATFORM_WINDOWS
    new_sock->handle = (u64)client;
#else
    new_sock->fd = client;
#endif
    return new_sock;
}

bool_t socket_connect(Socket* sock, const String host, u16 port) {
    if (!sock || host.len == 0 || !sock->is_valid) return false;
    struct sockaddr_in addr; memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET; addr.sin_port = htons(port);
    inet_pton(AF_INET, host.ptr, &addr.sin_addr);
#if STD_PLATFORM_WINDOWS
    return connect((SOCKET)sock->handle, (struct sockaddr*)&addr, sizeof(addr)) != SOCKET_ERROR;
#else
    return connect(sock->fd, (struct sockaddr*)&addr, sizeof(addr)) != SOCKET_ERROR;
#endif
}

size_t socket_send(Socket* sock, const u8* data, size_t len) {
    if (!sock || !sock->is_valid || !data) return 0;
#if STD_PLATFORM_WINDOWS
    int ret = send((SOCKET)sock->handle, (const char*)data, (int)len, 0);
    return ret > 0 ? ret : 0;
#else
    ssize_t ret = send(sock->fd, data, len, 0);
    return ret > 0 ? ret : 0;
#endif
}

size_t socket_receive(Socket* sock, u8* buffer, size_t len) {
    if (!sock || !sock->is_valid || !buffer) return 0;
#if STD_PLATFORM_WINDOWS
    int ret = recv((SOCKET)sock->handle, (char*)buffer, (int)len, 0);
    return ret > 0 ? ret : 0;
#else
    ssize_t ret = recv(sock->fd, buffer, len, 0);
    return ret > 0 ? ret : 0;
#endif
}

bool_t socket_close(Socket* sock) {
    if (!sock || !sock->is_valid) return false;
#if STD_PLATFORM_WINDOWS
    closesocket((SOCKET)sock->handle);
#else
    close(sock->fd);
#endif
    sock->is_valid = false;
    return true;
}

bool_t socket_set_blocking(Socket* sock, bool_t blocking) {
    if (!sock || !sock->is_valid) return false;
#if STD_PLATFORM_WINDOWS
    u_long mode = blocking ? 0 : 1;
    return ioctlsocket((SOCKET)sock->handle, FIONBIO, &mode) == 0;
#else
    int flags = fcntl(sock->fd, F_GETFL, 0);
    if (blocking) flags &= ~O_NONBLOCK;
    else flags |= O_NONBLOCK;
    return fcntl(sock->fd, F_SETFL, flags) != -1;
#endif
}

bool_t socket_set_timeout(Socket* sock, uint32_t timeout_ms) {
    if (!sock || !sock->is_valid) return false;
    struct timeval tv;
    tv.tv_sec = timeout_ms / 1000;
    tv.tv_usec = (timeout_ms % 1000) * 1000;
#if STD_PLATFORM_WINDOWS
    return setsockopt((SOCKET)sock->handle, SOL_SOCKET, SO_RCVTIMEO, (const char*)&tv, sizeof(tv)) != SOCKET_ERROR
        && setsockopt((SOCKET)sock->handle, SOL_SOCKET, SO_SNDTIMEO, (const char*)&tv, sizeof(tv)) != SOCKET_ERROR;
#else
    return setsockopt(sock->fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv)) != -1
        && setsockopt(sock->fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv)) != -1;
#endif
}

bool_t socket_is_valid(Socket* sock) { return sock && sock->is_valid; }

Socket* tcp_server_create(const String host, u16 port) {
    Socket* sock = socket_create(AF_INET, SOCK_STREAM, 0);
    if (sock && socket_bind(sock, host, port) && socket_listen(sock, 5)) return sock;
    if (sock) socket_destroy(sock);
    return NULL;
}

Socket* tcp_client_create() { return socket_create(AF_INET, SOCK_STREAM, 0); }
bool_t tcp_connect(Socket* sock, const String host, u16 port) { return socket_connect(sock, host, port); }
size_t tcp_send(Socket* sock, const u8* data, size_t len) { return socket_send(sock, data, len); }
size_t tcp_receive(Socket* sock, u8* buffer, size_t len) { return socket_receive(sock, buffer, len); }

Socket* udp_socket_create() { return socket_create(AF_INET, SOCK_DGRAM, 0); }
bool_t udp_bind(Socket* sock, const String host, u16 port) { return socket_bind(sock, host, port); }

size_t udp_send_to(Socket* sock, const String host, u16 port, const u8* data, size_t len) {
    if (!sock || !sock->is_valid || host.len == 0 || !data) return 0;
    struct sockaddr_in addr; memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET; addr.sin_port = htons(port);
    inet_pton(AF_INET, host.ptr, &addr.sin_addr);
#if STD_PLATFORM_WINDOWS
    int ret = sendto((SOCKET)sock->handle, (const char*)data, (int)len, 0, (struct sockaddr*)&addr, sizeof(addr));
    return ret > 0 ? ret : 0;
#else
    ssize_t ret = sendto(sock->fd, data, len, 0, (struct sockaddr*)&addr, sizeof(addr));
    return ret > 0 ? ret : 0;
#endif
}

size_t udp_receive_from(Socket* sock, String* out_host, u16* out_port, u8* buffer, size_t len) {
    if (!sock || !sock->is_valid || !buffer) return 0;
    struct sockaddr_in addr; socklen_t addr_len = sizeof(addr);
#if STD_PLATFORM_WINDOWS
    int ret = recvfrom((SOCKET)sock->handle, (char*)buffer, (int)len, 0, (struct sockaddr*)&addr, &addr_len);
#else
    ssize_t ret = recvfrom(sock->fd, buffer, len, 0, (struct sockaddr*)&addr, &addr_len);
#endif
    if (ret > 0) {
        if (out_host) {
            char buf[64];
            inet_ntop(AF_INET, &addr.sin_addr, buf, sizeof(buf));
            *out_host = string_create(buf);
        }
        if (out_port) *out_port = ntohs(addr.sin_port);
        return ret;
    }
    return 0;
}

bool_t udp_set_broadcast(Socket* sock, bool_t enable) {
    if (!sock || !sock->is_valid) return false;
    int opt = enable ? 1 : 0;
#if STD_PLATFORM_WINDOWS
    return setsockopt((SOCKET)sock->handle, SOL_SOCKET, SO_BROADCAST, (const char*)&opt, sizeof(opt)) != SOCKET_ERROR;
#else
    return setsockopt(sock->fd, SOL_SOCKET, SO_BROADCAST, &opt, sizeof(opt)) != -1;
#endif
}

bool_t dns_resolve(const String hostname, String* ip_out) {
    if (hostname.len == 0) return false;
    struct addrinfo hints, *result;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_INET;
    if (getaddrinfo(hostname.ptr, NULL, &hints, &result) != 0) return false;
    char buf[64];
    struct sockaddr_in* ipv4 = (struct sockaddr_in*)result->ai_addr;
    inet_ntop(AF_INET, &ipv4->sin_addr, buf, sizeof(buf));
    if (ip_out) *ip_out = string_create(buf);
    freeaddrinfo(result);
    return true;
}

bool_t net_is_valid_ip(const String ip) {
    if (ip.len == 0) return false;
    struct sockaddr_in sa; return inet_pton(AF_INET, ip.ptr, &(sa.sin_addr)) == 1;
}

String net_get_local_hostname() {
    char buf[256];
    if (gethostname(buf, sizeof(buf)) == 0) return string_create(buf);
    return string_create("localhost");
}

String net_get_local_ip() {
    char buf[64];
#if STD_PLATFORM_WINDOWS
    SOCKET_INIT;
#endif
    char hostname[256];
    if (gethostname(hostname, sizeof(hostname)) != 0) return string_create("127.0.0.1");
    struct addrinfo hints, *result;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_INET;
    if (getaddrinfo(hostname, NULL, &hints, &result) == 0) {
        struct sockaddr_in* ipv4 = (struct sockaddr_in*)result->ai_addr;
        inet_ntop(AF_INET, &ipv4->sin_addr, buf, sizeof(buf));
        freeaddrinfo(result);
        return string_create(buf);
    }
    return string_create("127.0.0.1");
}
