#ifndef STB_IMAGE_H
#define STB_IMAGE_H

#ifndef STBIDEF
#define STBIDEF extern
#endif

#ifdef __cplusplus
extern "C" {
#endif

typedef unsigned char stbi_uc;

STBIDEF stbi_uc *stbi_load(char const *filename, int *x, int *y, int *channels_in_file, int desired_channels);
STBIDEF void stbi_image_free(void *retval_from_stbi_load);
STBIDEF const char *stbi_failure_reason(void);
STBIDEF void stbi_set_flip_vertically_on_load(int flag_true_if_should_flip);
STBIDEF stbi_uc *stbi_load_from_memory(stbi_uc const *buffer, int len, int *x, int *y, int *channels_in_file, int desired_channels);

#ifdef STB_IMAGE_IMPLEMENTATION
#include <stdlib.h>
#include <string.h>

static const char *stbi__failure_reason = "no failure";
stbi_uc *stbi_load(char const *filename, int *x, int *y, int *channels_in_file, int desired_channels) {
    (void)filename; (void)x; (void)y; (void)channels_in_file; (void)desired_channels;
    return 0;
}
void stbi_image_free(void *retval_from_stbi_load) {
    (void)retval_from_stbi_load;
}
const char *stbi_failure_reason(void) {
    return stbi__failure_reason;
}
void stbi_set_flip_vertically_on_load(int flag_true_if_should_flip) {
    (void)flag_true_if_should_flip;
}
stbi_uc *stbi_load_from_memory(stbi_uc const *buffer, int len, int *x, int *y, int *channels_in_file, int desired_channels) {
    (void)buffer; (void)len; (void)x; (void)y; (void)channels_in_file; (void)desired_channels;
    return 0;
}
#endif

#ifdef __cplusplus
}
#endif

#endif
