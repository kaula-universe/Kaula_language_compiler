/*
 * Kaula 并发内核 · MPSC 队列的尺寸契约
 *
 * 背景：kaula_mpsc_queue_t 内联展开 131072 个 cell，每个 cell 16 字节
 * （_Atomic(void*) value + _Atomic(uint64_t) seq），加上两个 cache line
 * 对齐的 head/tail，总计约 2 MB。
 *
 * 这个尺寸**不能用栈分配**：Windows 默认线程栈 1 MB，Linux 8 MB 但
 * 有 RLIMIT_STACK 限制。把它声明成局部变量会直接 STATUS_STACK_OVERFLOW
 * （0xC00000FD），且崩在进 main 之前 —— 没有任何输出，极难定位。
 *
 * test_mpsc_queue.c 曾就是这么写的（`kaula_mpsc_queue_t q;`），
 * 直接以 rc=3221225725 收场。
 *
 * 判据：
 *   1. sizeof 必须远大于默认栈，任何"局部变量"用法都是错的
 *   2. 内核自身必须把它放在全局/静态存储里（不是栈、不是被 free 的堆）
 *   3. 提供 kaula_mpsc_queue_create/destroy，让调用方不必自己算尺寸
 */

#include "kaula_conc_test.h"
#include "kaula_conc_mpsc_queue.h"

#include <stdlib.h>

static void test_size_exceeds_default_stack(void) {
    TEST_CASE("MPSC 队列尺寸远超默认栈 —— 只能用堆/全局");

    size_t sz = sizeof(kaula_mpsc_queue_t);

    /*
     * 1 MB 是 Windows 的默认线程栈；留一半余量，
     * 保证判据在栈被调小一点时依然成立。
     */
    const size_t kStackBudget = 512u * 1024u;

    if (sz <= kStackBudget) {
        TEST_FAIL("sizeof(kaula_mpsc_queue_t)=%zu 意外地小（<= %zu）；"
                  "若容量被调小，请同步复核本用例与"
                  "「栈上分配不可用」的结论", sz, kStackBudget);
        return;
    }
    /* 通过：不报错 */
}

static void test_heap_roundtrip(void) {
    TEST_CASE("堆上分配的 MPSC 队列可正常 push/pop");

    kaula_mpsc_queue_t* q = (kaula_mpsc_queue_t*)malloc(sizeof(*q));
    if (q == NULL) { TEST_FAIL("malloc 失败"); return; }
    kaula_mpsc_queue_init(q);

    for (int i = 0; i < 1000; i++) {
        if (!kaula_mpsc_queue_push(q, (void*)(intptr_t)(i + 1))) {
            TEST_FAIL("push #%d 意外失败", i);
            free(q);
            return;
        }
    }
    if (kaula_mpsc_queue_size(q) != 1000) {
        TEST_FAIL("size 应为 1000，实际 %llu",
                  (unsigned long long)kaula_mpsc_queue_size(q));
        free(q);
        return;
    }

    for (int i = 0; i < 1000; i++) {
        void* p = kaula_mpsc_queue_pop(q);
        if ((intptr_t)p != i + 1) {
            TEST_FAIL("第 %d 个 pop 应为 %d，实际 %lld",
                      i, i + 1, (long long)(intptr_t)p);
            free(q);
            return;
        }
    }
    ASSERT_NULL(kaula_mpsc_queue_pop(q), "排空后 pop 应为 NULL");
    free(q);
}

/* 容量必须仍是 2 的幂（I3：取模用掩码） */
static void test_capacity_is_power_of_two(void) {
    TEST_CASE("容量是 2 的幂");
    if ((KAULA_MPSC_CAPACITY & (KAULA_MPSC_CAPACITY - 1)) != 0) {
        TEST_FAIL("容量 %d 不是 2 的幂", (int)KAULA_MPSC_CAPACITY);
    }
}

int main(void) {
    TEST_SECTION("MPSC 尺寸契约");
    test_size_exceeds_default_stack();
    test_capacity_is_power_of_two();
    test_heap_roundtrip();
    return test_report("mpsc_contract");
}
