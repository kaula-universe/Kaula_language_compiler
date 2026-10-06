/*
 * Kaula 并发内核 · 完成队列
 *
 * 文档 §8：存放已完成 Task 的结果，供 await 消费。
 *
 * 设计要点：
 * - 分片（默认 16 片），按 task_id 哈希落片，摊平竞争。
 * - 片内是有界环形缓冲区，满了不覆盖 —— 满了说明消费端跟不上，
 *   此时 Task 完成路径直接走事件唤醒（见 kaula_task_complete），
 *   完成队列只承担"结果暂存 + 批量观察"的角色。
 * - 完成事件本身用 kaula_p_event（信号量/条件变量）实现，
 *   await 是 park/unpark，不是轮询（对比原实现的 kaula_sleep_us(10) 轮询）。
 */

#ifndef KAULA_CONC_COMPLETION_H
#define KAULA_CONC_COMPLETION_H

#include <stdint.h>
#include <stdatomic.h>

#include "kaula_conc_task.h"

#ifdef __cplusplus
extern "C" {
#endif

#define KAULA_COMPLETION_CAPACITY_PER_SHARD 1024

typedef struct {
    _Atomic(uint64_t) head;   /* 生产者 */
    _Atomic(uint64_t) tail;   /* 消费者 */
    _Atomic(KaulaTask*) slots[KAULA_COMPLETION_CAPACITY_PER_SHARD];
} KaulaCompletionShard;

typedef struct {
    KaulaCompletionShard shards[KAULA_COMPLETION_SHARDS];
    _Atomic(uint64_t) total_completions;
    _Atomic(uint64_t) total_drops;   /* 队列满被拒的次数（应为 0） */
} KaulaCompletionQueue;

extern KaulaCompletionQueue g_conc_completion;

void     kaula_completion_init(void);
void     kaula_completion_shutdown(void);

/*
 * 推入一个已完成的 Task。
 * 返回 1 = 已入队（或无消费者时的计数路径），0 = 该片已满。
 *
 * 无消费者时（见 kaula_completion_consumer_begin 说明）：只递增
 * total_completions，不写环形缓冲，恒返回 1，total_drops 保持 0。
 */
int kaula_completion_push(KaulaTask* task);

/* 取出一个（按 task_id 查最近的完成项）；找不到返回 NULL */
KaulaTask* kaula_completion_take(uint64_t task_id);

/*
 * 消费期开关。
 *
 * 完成队列的唯一消费者是 kaula_completion_take()；热路径 await
 * （kaula_task_await）不读此队列。因此仅在"确定会调 take"的场景
 * （单元测试、按 id 查完成状态的诊断）开启消费期，稳态热路径保持
 * 关闭以省掉全部环形缓冲写入。
 *
 * kaula_task_complete 在 enable_metrics != 0 时会自动开启/维持消费期
 * （metrics 打开意味着完成队列需要完整可观测）。
 */
void kaula_completion_consumer_begin(void);
void kaula_completion_consumer_end(void);

/* 队列统计 */
uint64_t kaula_completion_count(void);
uint64_t kaula_completion_drops(void);


#ifdef __cplusplus
}
#endif
#endif /* KAULA_CONC_COMPLETION_H */