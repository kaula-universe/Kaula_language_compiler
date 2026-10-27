// btree_bench.cpp — 深度 16 完全二叉树：反复创建 + 遍历 100 遍基准
// 与 test/btree_bench.kl 逐阶段等价：同为 arena 连续分配 + 下标子节点、
// 同样的递归建树/递归中序求和、同样的阶段划分与计时口径。
#include <cstdint>
#include <cstdio>
#include <chrono>
#include <vector>

// 节点 16 字节：i32 left + i32 right + i64 value（无填充）
struct Node {
    int32_t left;
    int32_t right;
    int64_t value;
};

// 以 idx 为根、自 depth 层向下递归建树（完全二叉树，满 maxDepth 层）
static void build(std::vector<Node>& nodes, int32_t idx, int32_t depth, int32_t maxDepth) {
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

// 中序遍历求和
static int64_t inorder(const std::vector<Node>& nodes, int32_t idx) {
    if (idx < 0) {
        return 0;
    }
    int64_t leftSum = inorder(nodes, nodes[idx].left);
    int64_t mid = nodes[idx].value;
    int64_t rightSum = inorder(nodes, nodes[idx].right);
    return leftSum + mid + rightSum;
}

static int64_t now_ms() {
    using namespace std::chrono;
    return duration_cast<milliseconds>(steady_clock::now().time_since_epoch()).count();
}

int main() {
    const int32_t depth = 16;
    const int64_t nodesCount = 65535;   // 2^16 - 1
    const int64_t rounds = 100;
    const int64_t expect = 2147385345;

    std::printf("=== C++ btree_bench (depth=16, nodes=65535, rounds=100) ===\n");

    // 单块 arena，全部阶段复用（与 Kaula 版一致）
    std::vector<Node> nodes(static_cast<size_t>(nodesCount));

    // ---- 阶段 A：100 轮 重建 + 遍历 ----
    int64_t checksumA = 0;
    int64_t t0 = now_ms();
    for (int64_t r = 0; r < rounds; ++r) {
        build(nodes, 0, 1, depth);
        checksumA += inorder(nodes, 0);
    }
    int64_t t1 = now_ms();
    std::printf("[A] rebuild+traverse x100   %lld ms   checksum=%lld\n",
                static_cast<long long>(t1 - t0), static_cast<long long>(checksumA));

    // ---- 阶段 B：建树 1 次，遍历 100 遍 ----
    build(nodes, 0, 1, depth);
    int64_t checksumB = 0;
    int64_t t2 = now_ms();
    for (int64_t r = 0; r < rounds; ++r) {
        checksumB += inorder(nodes, 0);
    }
    int64_t t3 = now_ms();
    std::printf("[B] traverse x100           %lld ms   checksum=%lld\n",
                static_cast<long long>(t3 - t2), static_cast<long long>(checksumB));

    // ---- 阶段 C：单次建树 / 单次遍历 ----
    int64_t t4 = now_ms();
    build(nodes, 0, 1, depth);
    int64_t t5 = now_ms();
    int64_t one = inorder(nodes, 0);
    int64_t t6 = now_ms();
    std::printf("[C] build x1                %lld ms\n", static_cast<long long>(t5 - t4));
    std::printf("[C] traverse x1             %lld ms   checksum=%lld\n",
                static_cast<long long>(t6 - t5), static_cast<long long>(one));

    // ---- 阶段 D：阶段 A 重复 100 次（共 10000 轮），稳定计时 ----
    const int64_t repeats = 100;
    int64_t checksumD = 0;
    int64_t t7 = now_ms();
    for (int64_t rep = 0; rep < repeats; ++rep) {
        for (int64_t r = 0; r < rounds; ++r) {
            build(nodes, 0, 1, depth);
            checksumD += inorder(nodes, 0);
        }
    }
    int64_t t8 = now_ms();
    int64_t perRoundNs = (t8 - t7) * 100;  // (t8-t7) ms / 10000 轮 → ns/轮
    std::printf("[D] rebuild+traverse x10000 %lld ms   checksum=%lld\n",
                static_cast<long long>(t8 - t7), static_cast<long long>(checksumD));
    std::printf("[D] per-round               %lld ns (build+traverse, 65535 nodes)\n",
                static_cast<long long>(perRoundNs));

    // 一致性校验
    int64_t okA = checksumA - rounds * expect;
    int64_t okB = checksumB - rounds * expect;
    int64_t okD = checksumD - repeats * rounds * expect;
    std::printf("[V] verify A/B/D delta      %lld %lld %lld\n",
                static_cast<long long>(okA), static_cast<long long>(okB), static_cast<long long>(okD));

    return 0;
}
