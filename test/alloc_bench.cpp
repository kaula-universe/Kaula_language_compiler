// alloc_bench.cpp — 与 test/alloc_bench.kl 逐阶段等价的 malloc/free 对照基准
//
// 阶段划分与计时口径同 Kaula 版：
//   A 空循环基线 / B 大块 1MB 分配+释放循环 / C 批量小对象分配再批量释放 /
//   D 小对象 alloc+free 成对 / E 分配后释放（Kaula 侧为 alloc+reset）/
//   F 真实负载：分配 + 建树 + 中序遍历 ×10000 / V 校验
#include <cstdint>
#include <cstdio>
#include <chrono>
#include <cstdlib>
#include <vector>

// 节点 16 字节：i32 left + i32 right + i64 value（无填充）
struct Node {
    int32_t left;
    int32_t right;
    int64_t value;
};

// 防止 LLVM 把「malloc 后只写 1 字节就 free」当成死分配整段删除：
// 让分配出的地址本身可观察（volatile 累加），否则 B/D/E 会被优化成 0 ns。
// Kaula 侧 kmm_v4_alloc 是不透明调用，不存在该问题。
static volatile int64_t g_ptr_sink = 0;

static void build(Node* nodes, int32_t idx, int32_t depth, int32_t maxDepth) {
    nodes[idx].left = -1;
    nodes[idx].right = -1;
    nodes[idx].value = static_cast<int64_t>(idx);
    if (depth < maxDepth) {
        int32_t li = idx * 2 + 1;
        int32_t ri = idx * 2 + 2;
        nodes[idx].left = li;
        nodes[idx].right = ri;
        build(nodes, li, depth + 1, maxDepth);
        build(nodes, ri, depth + 1, maxDepth);
    }
}

static int64_t inorder(const Node* nodes, int32_t idx) {
    if (idx < 0) {
        return 0;
    }
    int64_t leftSum = inorder(nodes, nodes[idx].left);
    int64_t mid = nodes[idx].value;
    int64_t rightSum = inorder(nodes, nodes[idx].right);
    return leftSum + mid + rightSum;
}

static int64_t now_ns() {
    using namespace std::chrono;
    return duration_cast<nanoseconds>(steady_clock::now().time_since_epoch()).count();
}

int main() {
    const int64_t n = 200000;
    const size_t block = 1048576;   // 1MB
    const int64_t resetRounds = 50000;
    const int64_t fRounds = 10000;
    const int64_t expect = 2147385345;
    int64_t sink = 0;

    std::printf("=== C++ alloc_bench (N=200000, block=1MB, small=16B) ===\n");

    // ---- 预热：页池/自由链懒初始化 + 分支预测 ----
    for (int w = 0; w < 1000; ++w) {
        void* p = std::malloc(16);
        static_cast<char*>(p)[0] = 1;
        sink += static_cast<unsigned char>(static_cast<char*>(p)[0]);
        g_ptr_sink += static_cast<int64_t>(reinterpret_cast<uintptr_t>(p));
        std::free(p);
        void* q = std::malloc(16);
        g_ptr_sink += static_cast<int64_t>(reinterpret_cast<uintptr_t>(q));
        std::free(q);
    }

    // ---- 阶段 A：空循环基线 ----
    int64_t t0 = now_ns();
    int64_t i = 0;
    while (i < n) {
        sink = sink + 1;
        i = i + 1;
    }
    int64_t t1 = now_ns();
    std::printf("[A] loop baseline            %lld ns/op\n",
                static_cast<long long>((t1 - t0) / n));

    // ---- 阶段 B：大块 1MB 分配 + 释放 ----
    int64_t t2 = now_ns();
    for (i = 0; i < n; ++i) {
        void* p = std::malloc(block);
        static_cast<char*>(p)[0] = 1;
        sink += static_cast<unsigned char>(static_cast<char*>(p)[0]);
        g_ptr_sink += static_cast<int64_t>(reinterpret_cast<uintptr_t>(p));
        std::free(p);
    }
    int64_t t3 = now_ns();
    std::printf("[B] malloc(1MB)+free x200000 %lld ms   %lld ns/round\n",
                static_cast<long long>((t3 - t2) / 1000000),
                static_cast<long long>((t3 - t2) / n));

    // ---- 阶段 C：批量分配 N 个 16B，再批量释放 ----
    void** ptrs = static_cast<void**>(std::malloc(sizeof(void*) * static_cast<size_t>(n)));
    int64_t t4 = now_ns();
    for (i = 0; i < n; ++i) {
        void* p = std::malloc(16);
        static_cast<char*>(p)[0] = 1;
        sink += static_cast<unsigned char>(static_cast<char*>(p)[0]);
        ptrs[i] = p;
    }
    int64_t t5 = now_ns();
    for (i = 0; i < n; ++i) {
        std::free(ptrs[i]);
    }
    int64_t t6 = now_ns();
    std::printf("[C] malloc 16B x200000       %lld ms   %lld ns/alloc\n",
                static_cast<long long>((t5 - t4) / 1000000),
                static_cast<long long>((t5 - t4) / n));
    std::printf("[C] free x200000 (per-slot)  %lld ns   %lld ns/slot\n",
                static_cast<long long>(t6 - t5),
                static_cast<long long>((t6 - t5) / n));
    std::free(ptrs);

    // ---- 阶段 D：小对象 alloc+free 成对 ----
    int64_t freed = 0;
    int64_t t7 = now_ns();
    for (i = 0; i < n; ++i) {
        void* p = std::malloc(16);
        static_cast<char*>(p)[0] = 1;
        sink += static_cast<unsigned char>(static_cast<char*>(p)[0]);
        g_ptr_sink += static_cast<int64_t>(reinterpret_cast<uintptr_t>(p));
        if (p != nullptr) {
            std::free(p);
            freed = freed + 1;
        }
    }
    int64_t t8 = now_ns();
    std::printf("[D] malloc+free 16B x200000  %lld ms   %lld ns/pair\n",
                static_cast<long long>((t8 - t7) / 1000000),
                static_cast<long long>((t8 - t7) / n));
    std::printf("[D] freed count              %lld / %lld\n",
                static_cast<long long>(freed), static_cast<long long>(n));

    // ---- 阶段 E：分配 + 释放（Kaula 侧为 alloc + kmm_v4_reset）----
    int64_t t9 = now_ns();
    for (i = 0; i < resetRounds; ++i) {
        void* p = std::malloc(block);
        static_cast<char*>(p)[0] = 1;
        sink += static_cast<unsigned char>(static_cast<char*>(p)[0]);
        g_ptr_sink += static_cast<int64_t>(reinterpret_cast<uintptr_t>(p));
        std::free(p);
    }
    int64_t t10 = now_ns();
    std::printf("[E] malloc(1MB)+free x50000  %lld ms   %lld ns/round\n",
                static_cast<long long>((t10 - t9) / 1000000),
                static_cast<long long>((t10 - t9) / resetRounds));

    // ---- 阶段 F：真实负载（分配 + 建树 + 遍历）----
    int64_t checksumF = 0;
    int64_t t11 = now_ns();
    for (int64_t rep = 0; rep < fRounds; ++rep) {
        // malloc 1MB + build + free：对齐 Kaula 侧「作用域分配(不清零) + 建树 + scope 回收」
        Node* nodes = static_cast<Node*>(std::malloc(65535 * sizeof(Node)));
        g_ptr_sink += static_cast<int64_t>(reinterpret_cast<uintptr_t>(nodes));
        build(nodes, 0, 1, 16);
        checksumF += inorder(nodes, 0);
        std::free(nodes);
    }
    int64_t t12 = now_ns();
    std::printf("[F] malloc+build+traverse x10000 %lld ms   %lld ns/round\n",
                static_cast<long long>((t12 - t11) / 1000000),
                static_cast<long long>((t12 - t11) / fRounds));
    std::printf("[F] checksum                 %lld\n", static_cast<long long>(checksumF));

    // ---- 校验 ----
    int64_t okF = checksumF - fRounds * expect;
    std::printf("[V] checksum delta          %lld\n", static_cast<long long>(okF));
    std::printf("[V] sink                    %lld\n", static_cast<long long>(sink));
    std::printf("[V] ptr_sink                %lld\n", static_cast<long long>(g_ptr_sink));
    return 0;
}
