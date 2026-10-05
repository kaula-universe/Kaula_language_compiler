// Boost.Asio C++20 协程版订单服务 —— 负载对齐 kl 对齐版 / C 版
// 每请求：checksum256(256轮) + parse seq/qty/price + 业务64轮校验
//        + sleep 200us + 响应 "R,<seq>,<hex8 ck>,<total>\n"
#include <boost/asio.hpp>
#include <boost/asio/co_spawn.hpp>
#include <boost/asio/use_awaitable.hpp>
#include <boost/asio/steady_timer.hpp>
#include <cstdint>
#include <cstring>
#include <cstdio>
#include <string>
#include <memory>
#include <chrono>
#include <thread>
#include <vector>
#ifndef SVC_PORT
#define SVC_PORT 8127
#endif

namespace asio = boost::asio;
using asio::ip::tcp;

static uint32_t checksum256(const char* s, size_t n) {
    uint32_t h = 0x811C9DC5u;
    for (int round = 0; round < 256; round++) {
        for (size_t i = 0; i < n; i++) {
            h = (h ^ (uint8_t)s[i]) * 16777619u + (uint32_t)round;
        }
    }
    return h;
}

// 解析第 k 个逗号分隔字段（0 起始）——手写（对齐 kl 生成代码，避免 strtol）
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

static asio::awaitable<void> session(tcp::socket sock) {
    try {
        char rbuf[1024]; size_t rlen = 0;
        for (;;) {
            size_t got = co_await sock.async_read_some(asio::buffer(rbuf + rlen, sizeof(rbuf) - rlen), asio::use_awaitable);
            if (got == 0) break;
            rlen += got;
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
                // 业务：栈上 64B 记录 + 64 轮混合校验（对齐 C 版 BizRecord/mixed_verify）
                unsigned char r[64]; std::memset(r, 0, 64);
                r[0] = (unsigned char)(seq & 0xff); r[1] = (unsigned char)(qty & 0xff); r[2] = (unsigned char)(price & 0xff);
                int h = 0;
                for (int k = 0; k < 64; k++) { h = (h ^ r[k]) + seq + qty + price; h = h * 31 + k; }
                ck ^= (uint32_t)h;
                // sleep 200us（对应 kl iodriver 定时器挂起；NO_SLEEP 时跳过）
#ifndef NO_SLEEP
                asio::steady_timer tm(co_await asio::this_coro::executor, std::chrono::microseconds(200));
                co_await tm.async_wait(asio::use_awaitable);
#endif
                // 响应 R,<seq>,<hex8>,<total>\n
                char resp[64];
                resp[0] = 'R'; resp[1] = ',';
                int pos = itoa(seq, resp, 2); pos += 2;
                resp[pos] = ','; pos++;
                pos = itohex8(ck, resp, pos) + pos;
                resp[pos] = ','; pos++;
                pos = itoa(total, resp, pos) + pos;
                resp[pos] = '\n'; pos++;
                co_await asio::async_write(sock, asio::buffer(resp, pos), asio::use_awaitable);
                off = nl + 1;
            }
            if (off > 0) { size_t rem = rlen - off; std::memmove(rbuf, rbuf + off, rem); rlen = rem; }
        }
    } catch (...) {}
}

static asio::awaitable<void> acceptor(tcp::acceptor& acc) {
    for (;;) {
        tcp::socket sock = co_await acc.async_accept(asio::use_awaitable);
        asio::co_spawn(acc.get_executor(), session(std::move(sock)), asio::detached);
    }
}

int main() {
    asio::io_context io(4);
    tcp::acceptor acc(io, tcp::endpoint(tcp::v4(), SVC_PORT));
    asio::co_spawn(io, acceptor(acc), asio::detached);
    std::vector<std::thread> ts;
    for (int i = 0; i < 4; i++) ts.emplace_back([&] { io.run(); });
    for (auto& t : ts) t.join();
    return 0;
}
