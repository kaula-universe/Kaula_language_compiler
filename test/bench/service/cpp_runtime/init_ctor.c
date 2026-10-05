#include "kaula_conc.h"
 * kl 服务预热 init 的诊断工具：constructor 提前触发 kaula_runtime_init(NULL)。
 * 注意：kl 惰性 init 在首个 spawn 时也会自动建满核 worker（与显式 init 一致），
 * 本文件仅用于"启动即预热"的实证/预热，非功能必需。
 */
__attribute__((constructor)) static void boot(void) {
    (void)kaula_runtime_init(NULL);
}
