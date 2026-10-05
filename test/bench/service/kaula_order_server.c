/*
 * Kaula 并发内核（CONC）· 真实服务演示：TCP 订单处理服务
 *
 * 语义：行协议请求-响应服务。客户端每行一个订单请求：
 *     O,<seq>,<item>,<qty>,<price>
 * 服务端流水线：解析 → checksum 校验（256 轮混合）→ 业务计算（qty*price）
 *              → 模拟下游 IO（200us sleep）→ 编码响应
 *     R,<seq>,<checksum>,<total>
 *
 * 并发模型：每连接一个 conc 任务（KaulaResumeFn 状态机）。IO 走
 * IoDriver 的 READ/WRITE 挂起（fd 级 epoll），短 sleep 走 Worker 本地
 * 定时器（P-D/P-D3，不跨线程）。
 *
 * 构建：
 *   clang -O2 -o kaula_order_server kaula_order_server.c \
 *     -I <kaula>/src -I <kaula>/src/concurrency \
 *     <kaula>/build/lib/libkaula_runtime.a -lpthread -lm
 */

#include "kaula_conc.h"

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#define PORT        8123
#define READ_CAP    1024
#define WRITE_CAP   512
#define DB_SLEEP_NS 200000ULL   /* 200us：模拟下游 DB/缓存 IO */

/* ---------- 业务流水线（与 Go 版逐字节同语义） ---------- */

static uint32_t checksum256(const char* s, size_t n) {
    uint32_t h = 0x811C9DC5u;
    for (int round = 0; round < 256; round++) {
        for (size_t i = 0; i < n; i++) {
            h = (h ^ (uint8_t)s[i]) * 16777619u + (uint32_t)round;
        }
    }
    return h;
}

/* 解析 "O,<seq>,<item>,<qty>,<price>"，返回 0 成功 */
static int parse_order(const char* line, size_t n, uint32_t* seq,
                       int* qty, int* price) {
    /* O,<seq>,<item>,<qty>,<price>：item 为字母串（跳过不校验内容） */
    if (n < 5 || line[0] != 'O' || line[1] != ',') return -1;
    const char* p = line + 2;
    char* end = NULL;
    *seq = (uint32_t)strtoul(p, &end, 10);
    if (end == p || *end != ',') return -1;
    p = end + 1;
    while (p < line + n && *p != ',') p++;          /* item */
    if (p >= line + n) return -1;
    p++;
    *qty = (int)strtol(p, &end, 10);
    if (end == p || *end != ',') return -1;
    p = end + 1;
    *price = (int)strtol(p, &end, 10);
    if (end == p || end > line + n) return -1;
    return 0;
}

/* ---------- 连接任务状态机 ---------- */

typedef struct {
    int         pc;
    KaulaIoFd   fd;
    char        rbuf[READ_CAP];
    size_t      rlen;
    char        wbuf[WRITE_CAP];
    size_t      wlen;
    size_t      woff;
} ConnFrame;

static KaulaResumeResult conn_resume(void* fp) {
    ConnFrame* f = (ConnFrame*)fp;

    switch (f->pc) {
    case 0: { /* 挂起：读进缓冲 */
        f->pc = 1;
        KaulaIoRequest req = {0};
        req.type = KAULA_IO_READ;
        req.fd = f->fd;
        req.buf = f->rbuf + f->rlen;
        req.buf_len = READ_CAP - f->rlen;
        return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                    .io_request = req };
    }
    case 1: { /* 从 READ 恢复：并入缓冲后走统一检查 */
        int got = kaula_conc_tls_io_result;
        if (got < 0) { /* 真错误 / 断连 */
            close(f->fd);
            return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                        .value = NULL };
        }
        if (got > 0) f->rlen += (size_t)got;
        /*
         * got == 0 不能当 EOF！ET 竞态：数据可能已被本连接的
         * 非阻塞直读循环拿走（内核已生成 EPOLLIN 事件但 fd 已空），
         * iodriver 此时 read 返回 0 —— 误判 EOF 会杀掉活连接。
         * 回 case 2 由非阻塞直读循环判定真实状态：
         * read 返回 0 才是真 EOF，EAGAIN 才是真的读净。
         */
        /* fallthrough 到 case 2：检查缓冲是否有完整行 */
    }
    case 2: { /* 统一检查缓冲（无 IO 路径）：ET 下缓冲可能有数据却无新事件 */
        char* nl = (char*)memchr(f->rbuf, '\n', f->rlen);
        if (nl != NULL) {
            size_t line_len = (size_t)(nl - f->rbuf);
            uint32_t seq = 0;
            int qty = 0, price = 0;
            uint32_t ck = checksum256(f->rbuf, line_len);
            if (parse_order(f->rbuf, line_len, &seq, &qty, &price) != 0) {
                ck = 0xDEADBEEFu; /* 坏行：响应标记 */
            }
            int total = qty * price;
            size_t remain = f->rlen - (line_len + 1);
            if (remain > 0) memmove(f->rbuf, nl + 1, remain);
            f->rlen = remain;
            int wn = snprintf(f->wbuf, WRITE_CAP, "R,%u,%08x,%d\n",
                              seq, ck, total);
            f->wlen = (wn > 0 && (size_t)wn < WRITE_CAP) ? (size_t)wn : 0;
            f->woff = 0;
            f->pc = 3;
            KaulaIoRequest req = {0};
            req.type = KAULA_IO_SLEEP;
            req.timeout_ns = DB_SLEEP_NS;
            return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                        .io_request = req };
        }
        if (f->rlen >= READ_CAP) { /* 无换行的超长行：协议错误 */
            close(f->fd);
            return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                        .value = NULL };
        }
        /*
         * 行不完整：ET 下内核缓冲可能还有数据（单次 READ 不保证读干净），
         * 必须先把缓冲读空（直到 EAGAIN）才能挂起等事件，否则漏数据。
         * 非阻塞直读：有数据立即并入缓冲再回 case 2 重扫。
         */
        for (;;) {
            ssize_t n = read(f->fd, f->rbuf + f->rlen, READ_CAP - f->rlen);
            if (n > 0) {
                f->rlen += (size_t)n;
                return conn_resume(f); /* pc==2：重扫缓冲 */
            }
            if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
                break;
            }
            if (n == 0) { /* EOF */
                close(f->fd);
                return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                            .value = NULL };
            }
            if (errno == EAGAIN || errno == EWOULDBLOCK) break;
            close(f->fd); /* 其他错误 */
            return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                        .value = NULL };
        }
        /*
         * 缓冲读净且无完整行：挂起等新数据。
         * 关键：pc 必须设为 1（READ 恢复进 case 1 处理 got）——
         * 设为 0 会进 case 0 重新挂起，do_io 读到的数据被整体吞掉
         * （实测：do_io r=17 → 恢复 pc=0 → 17 字节丢失 → 请求永久缺失）。
         */
        f->pc = 1;
        KaulaIoRequest req = {0};
        req.type = KAULA_IO_READ;
        req.fd = f->fd;
        req.buf = f->rbuf + f->rlen;
        req.buf_len = READ_CAP - f->rlen;
        return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                    .io_request = req };
    }
    case 3: { /* sleep 完成：写响应 */
        /*
         * 非阻塞直写，绝不挂起 EPOLLOUT：ET 下 EPOLLOUT 只在
         * 「不可写→可写」跃迁触发，响应短、写缓冲常空时 fd 一直
         * 可写，没有跃迁就没有事件 → 挂起必死锁（实测连接卡死）。
         * 写完为止；EAGAIN（客户端慢）退避 100us 重试，持续慢则
         * 关连接放弃。
         */
        for (;;) {
            ssize_t n = write(f->fd, f->wbuf + f->woff, f->wlen - f->woff);
            if (n > 0) {
                f->woff += (size_t)n;
                if (f->woff >= f->wlen) {
                    f->pc = 2; /* 写完：直接检查缓冲（ET：下一条可能已到） */
                    return conn_resume(f);
                }
                continue; /* 部分写：继续写 */
            }
            if (n == 0) {
                close(f->fd);
                return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                            .value = NULL };
            }
            if (errno == EAGAIN || errno == EWOULDBLOCK) {
                f->pc = 5;
                KaulaIoRequest req = {0};
                req.type = KAULA_IO_SLEEP;
                req.timeout_ns = 100000ULL; /* 100us 退避（走本地定时器） */
                return (KaulaResumeResult){ .status = KAULA_RESUME_SUSPEND,
                                            .io_request = req };
            }
            close(f->fd);
            return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                        .value = NULL };
        }
    }
    case 5: { /* EAGAIN 退避醒来：重试写 */
        f->pc = 3;
        return conn_resume(f);
    }
    default:
        close(f->fd);
        return (KaulaResumeResult){ .status = KAULA_RESUME_DONE,
                                    .value = NULL };
    }
}

/* ---------- 服务端 ---------- */

static int set_nonblock(int fd) {
    int fl = fcntl(fd, F_GETFL, 0);
    if (fl < 0) return -1;
    return fcntl(fd, F_SETFL, fl | O_NONBLOCK);
}

int main(void) {
    KaulaRuntimeConfig cfg = {0};
    cfg.worker_count = 4;
    if (kaula_runtime_init(&cfg) != 0) {
        fprintf(stderr, "[kaula] runtime init failed\n");
        return 1;
    }

    int ls = socket(AF_INET, SOCK_STREAM, 0);
    if (ls < 0) { perror("socket"); return 1; }
    int one = 1;
    setsockopt(ls, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port = htons(PORT);
    if (bind(ls, (struct sockaddr*)&addr, sizeof(addr)) != 0) {
        perror("bind"); return 1;
    }
    if (listen(ls, 512) != 0) { perror("listen"); return 1; }

    fprintf(stderr, "[kaula] order server on port %d, workers=%u\n",
            PORT, kaula_conc_worker_count());
    fflush(stderr);

    for (;;) {
        int cfd = accept(ls, NULL, NULL);
        if (cfd < 0) {
            if (errno == EINTR || errno == EAGAIN) continue;
            perror("accept");
            break;
        }
        if (set_nonblock(cfd) != 0) { close(cfd); continue; }
        ConnFrame* f = (ConnFrame*)kmm_v4_alloc_global(sizeof(ConnFrame));
        if (f == NULL) { close(cfd); continue; }
        memset(f, 0, sizeof(*f));
        f->fd = cfd;
        KaulaHandle h = kaula_spawn_flags(conn_resume, f, sizeof(ConnFrame),
                                          KAULA_TASK_FLAG_FRAME_ON_HEAP);
        (void)h; /* 无需 await：连接任务生命周期自持 */
    }
    return 0;
}
