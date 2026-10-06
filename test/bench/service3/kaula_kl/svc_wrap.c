/*
 * kl 服务的 socket 侧 C wrapper（语言层不直接碰 syscall）。
 * 与 std.net 同属「C 互操作」层：kl 是最终调用方，这里只做
 * listen/accept/nonblock + TLS 结果读取 + 直写响应。
 *
 * 与 test/bench/service/kl_order_server/svc_wrap.c 的差异：
 *   端口改为 8123（本基准 Kaula 边固定端口），其余语义不变。
 */
#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>
#include <stdint.h>
#include "kaula_conc.h"

/* worker 数：kl 语言层默认惰性 init 会按核数建 worker。
   本基准要求三边同配置（TRANSACTION.md §7），用 constructor 提前
   固定 init，避免「按机器核数」变成隐性变量。
   SVC_WORKERS 环境变量可覆盖（默认 4）。 */
#include <stdlib.h>
__attribute__((constructor)) static void svc_fixed_init(void) {
    const char* w = getenv("SVC_WORKERS");
    int n = w ? atoi(w) : 4;
    if (n <= 0) n = 4;
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = (uint32_t)n;
    kaula_runtime_init(&cfg);
}

int kaula_svc_listen(int port) {
    int ls = socket(AF_INET, SOCK_STREAM, 0);
    if (ls < 0) return -1;
    int one = 1;
    setsockopt(ls, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    struct sockaddr_in a;
    memset(&a, 0, sizeof(a));
    a.sin_family = AF_INET;
    a.sin_addr.s_addr = htonl(INADDR_ANY);
    a.sin_port = htons((uint16_t)port);
    if (bind(ls, (struct sockaddr*)&a, sizeof(a)) != 0) { close(ls); return -1; }
    if (listen(ls, 8192) != 0) { close(ls); return -1; }
    return ls;
}

int kaula_svc_accept(int lfd) {
    int cfd = accept(lfd, NULL, NULL);
    if (cfd < 0) return -1;
    int fl = fcntl(cfd, F_GETFL, 0);
    if (fl >= 0) fcntl(cfd, F_SETFL, fl | O_NONBLOCK);
    return cfd;
}

void* kaula_svc_at(void* buf, int off) { return (char*)buf + off; }

int kaula_tls_io_result(void) { return kaula_conc_tls_io_result; }

int kaula_close(int fd) { return close(fd); }

/* 直写响应：ET 下 iodriver WRITE（EPOLLOUT 注册）在小响应/空写缓冲时
   永不触发（无「不可写→可写」跃迁），会死锁。服务响应短、客户端总在读，
   写缓冲几乎总是空 → 直写立即成功；EAGAIN（客户端慢）退避重试。 */
int kaula_svc_send(int fd, const void* buf, int len) {
    const char* p = (const char*)buf;
    int left = len;
    while (left > 0) {
        ssize_t n = write(fd, p, (size_t)left);
        if (n > 0) { p += n; left -= (int)n; continue; }
        if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
            struct timespec ts = {0, 100000L}; /* 100us 退避 */
            nanosleep(&ts, NULL);
            continue;
        }
        return -1;
    }
    return len;
}
