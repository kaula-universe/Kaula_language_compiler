// cocoyaxi (co) C++ 协程版订单服务 —— 负载对齐 kl 对齐版（无 sleep 版）
// 注：co::sleep 仅 ms 粒度，200us 无法表示，本版不模拟下游 IO sleep
// 每请求：checksum256(256轮) + parse seq/qty/price + 业务64轮校验
//        + 响应 "R,<seq>,<hex8 ck>,<total>\n"
#include "co/co.h"
#include "co/flag.h"
#include "co/sock.h"
#include <thread>
#include <chrono>
#include <cstdint>
#include <cstring>
#include <cstdio>

static uint32_t checksum256(const char* s, size_t n) {
    uint32_t h = 0x811C9DC5u;
    for (int round = 0; round < 256; round++) {
        for (size_t i = 0; i < n; i++) {
            h = (h ^ (uint8_t)s[i]) * 16777619u + (uint32_t)round;
        }
    }
    return h;
}

static int parse_field(const char* line, size_t n, int k) {
    size_t i = 0; int f = 0;
    while (i < n && f < k) { if (line[i] == ',') f++; i++; }
    int v = 0;
    while (i < n && line[i] != ',') { v = v * 10 + (line[i] - '0'); i++; }
    return v;
}

static int itoa(int v, char* out, int base) {
    int t = v; int nd = 1;
    while (t >= 10) { t /= 10; nd++; }
    int p = base + nd - 1; t = v;
    while (t > 0) { out[p] = '0' + (t % 10); t /= 10; p--; }
    return nd;
}

static int itohex8(uint32_t v, char* out, int base) {
    uint64_t t = v; int p = base + 7;
    for (int k = 0; k < 8; k++) {
        uint64_t d = t % 16;
        out[p] = (d < 10) ? ('0' + (int)d) : ('a' + (int)(d - 10));
        t /= 16; p--;
    }
    return 8;
}

static void handle(sock_t fd) {
    char rbuf[1024];
    for (;;) {
        int got = co::recv(fd, rbuf, sizeof(rbuf));
        if (got <= 0) break;
        size_t rlen = (size_t)got;
        size_t off = 0;
        while (off < rlen) {
            size_t nl = off;
            while (nl < rlen && rbuf[nl] != '\n') nl++;
            if (nl >= rlen) break;
            size_t n = nl - off;
            int seq = parse_field(rbuf + off, n, 1);
            int qty = parse_field(rbuf + off, n, 3);
            int price = parse_field(rbuf + off, n, 4);
            uint32_t ck = checksum256(rbuf + off, n);
            int total = qty * price;
            unsigned char r[64]; std::memset(r, 0, 64);
            r[0] = (unsigned char)(seq & 0xff); r[1] = (unsigned char)(qty & 0xff); r[2] = (unsigned char)(price & 0xff);
            int h = 0;
            for (int k = 0; k < 64; k++) { h = (h ^ r[k]) + seq + qty + price; h = h * 31 + k; }
            ck ^= (uint32_t)h;
            char resp[64];
            resp[0] = 'R'; resp[1] = ',';
            int pos = itoa(seq, resp, 2); pos += 2;
            resp[pos] = ','; pos++;
            pos = itohex8(ck, resp, pos) + pos;
            resp[pos] = ','; pos++;
            pos = itoa(total, resp, pos) + pos;
            resp[pos] = '\n'; pos++;
            co::send(fd, resp, pos);
            off = nl + 1;
        }
    }
    co::close(fd);
}

int main(int argc, char** argv) {
    flag::parse(argc, argv); // 启动调度线程
    co::go([]() {
        sock_t lfd = co::tcp_server_socket("0.0.0.0", 8128);
        if (lfd < 0) { fprintf(stderr, "listen fail\n"); return; }
        for (;;) {
            sock_t cfd = co::accept(lfd);
            if (cfd >= 0) co::go([cfd]() { handle(cfd); });
        }
    });
    while (true) std::this_thread::sleep_for(std::chrono::seconds(1));
    return 0;
}
