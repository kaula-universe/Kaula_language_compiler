/* 探针：暴露 task 是否已完成，供 diag3 判定 await 快慢路径 */
#include "kaula/concurrency/kaula_conc_task.h"
int kaula_task_is_done_probe(void* task) {
    if (task == NULL) return 1;
    KaulaTask* t = (KaulaTask*)task;
    return atomic_load_explicit(&t->state, memory_order_acquire) == KAULA_TASK_DONE;
}
