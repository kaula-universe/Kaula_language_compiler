#ifndef STD_TEMPLATE_TEMPLATE_H
#define STD_TEMPLATE_TEMPLATE_H

#include "../base/types.h"

typedef struct Template Template;

Template* template_load(const char* path);
Template* template_create(const char* content);
void template_destroy(Template* tpl);

char* template_render(Template* tpl, const void* data);

bool_t template_set_variable(Template* tpl, const char* name, const char* value);
bool_t template_set_variable_int(Template* tpl, const char* name, i64 value);
bool_t template_set_variable_float(Template* tpl, const char* name, f64 value);
bool_t template_set_variable_bool(Template* tpl, const char* name, bool_t value);

void template_clear_variables(Template* tpl);

bool_t template_add_filter(Template* tpl, const char* name, char* (*filter)(const char*));

bool_t template_has_variable(Template* tpl, const char* name);

char* template_render_string(const char* content, const void* data);

#endif
