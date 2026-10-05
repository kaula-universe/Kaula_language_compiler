#include "kaula_conc.h"
__attribute__((constructor)) static void boot(void) {
    (void)kaula_runtime_init(NULL);
}
