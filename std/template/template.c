#include "template.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include "../fs/fs.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdarg.h>

struct TemplateVariable {
    char* name;
    char* value;
};

struct TemplateFilter {
    char* name;
    char* (*filter)(const char*);
};

struct Template {
    char* content;
    struct TemplateVariable* variables;
    size_t variable_count;
    size_t variable_capacity;
    struct TemplateFilter* filters;
    size_t filter_count;
    size_t filter_capacity;
};

Template* template_load(const char* path) {
    size_t size;
    char* content = fs_read_file(path, &size);
    if (!content) return NULL;
    
    Template* tpl = template_create(content);
    kmm_v4_free(content);
    
    return tpl;
}

Template* template_create(const char* content) {
    Template* tpl = (Template*)kmm_v4_malloc(sizeof(Template));
    if (!tpl) return NULL;
    
    tpl->content = kmm_v4_strdup(content);
    tpl->variables = NULL;
    tpl->variable_count = 0;
    tpl->variable_capacity = 0;
    tpl->filters = NULL;
    tpl->filter_count = 0;
    tpl->filter_capacity = 0;
    
    return tpl;
}

void template_destroy(Template* tpl) {
    if (!tpl) return;
    
    kmm_v4_free(tpl->content);
    
    for (size_t i = 0; i < tpl->variable_count; i++) {
        kmm_v4_free(tpl->variables[i].name);
        kmm_v4_free(tpl->variables[i].value);
    }
    kmm_v4_free(tpl->variables);
    
    for (size_t i = 0; i < tpl->filter_count; i++) {
        kmm_v4_free(tpl->filters[i].name);
    }
    kmm_v4_free(tpl->filters);
    
    kmm_v4_free(tpl);
}

static const char* template_find_variable(const Template* tpl, const char* name) {
    for (size_t i = 0; i < tpl->variable_count; i++) {
        if (strcmp(tpl->variables[i].name, name) == 0) {
            return tpl->variables[i].value;
        }
    }
    return NULL;
}

char* template_render(Template* tpl, const void* data) {
    if (!tpl || !tpl->content) return NULL;
    
    size_t capacity = strlen(tpl->content) * 2;
    char* result = (char*)kmm_v4_malloc(capacity);
    size_t pos = 0;
    
    const char* p = tpl->content;
    while (*p) {
        if (*p == '{' && *(p + 1) == '{') {
            p += 2;
            const char* var_start = p;
            // 提取变量名：读到 '}' 为止
            while (*p && *p != '}') p++;
            if (!*p || *(p + 1) != '}') {
                // 非法格式：{{... 缺少闭合 }}，当作普通文本处理
                p = var_start - 2;
            } else {
                size_t var_len = p - var_start;
                char* var_name = (char*)kmm_v4_malloc(var_len + 1);
                strncpy(var_name, var_start, var_len);
                var_name[var_len] = '\0';
                
                const char* value = template_find_variable(tpl, var_name);
                if (value) {
                    size_t value_len = strlen(value);
                    if (pos + value_len >= capacity) {
                        capacity *= 2;
                        result = (char*)kmm_v4_realloc(result, capacity);
                    }
                    strcpy(result + pos, value);
                    pos += value_len;
                }
                
                kmm_v4_free(var_name);
                p += 2; // 跳过闭合 }}
                continue;
            }
        }
        // 普通字符
        if (pos + 1 >= capacity) {
            capacity *= 2;
            result = (char*)kmm_v4_realloc(result, capacity);
        }
        result[pos++] = *p++;
    }
    
    result[pos] = '\0';
    return result;
}

bool_t template_set_variable(Template* tpl, const char* name, const char* value) {
    if (!tpl || !name || !value) return false;
    
    for (size_t i = 0; i < tpl->variable_count; i++) {
        if (strcmp(tpl->variables[i].name, name) == 0) {
            kmm_v4_free(tpl->variables[i].value);
            tpl->variables[i].value = kmm_v4_strdup(value);
            return true;
        }
    }
    
    if (tpl->variable_count >= tpl->variable_capacity) {
        tpl->variable_capacity = tpl->variable_capacity == 0 ? 8 : tpl->variable_capacity * 2;
        tpl->variables = (struct TemplateVariable*)kmm_v4_realloc(
            tpl->variables, tpl->variable_capacity * sizeof(struct TemplateVariable)
        );
        if (!tpl->variables) return false;
    }
    
    tpl->variables[tpl->variable_count].name = kmm_v4_strdup(name);
    tpl->variables[tpl->variable_count].value = kmm_v4_strdup(value);
    tpl->variable_count++;
    
    return true;
}

bool_t template_set_variable_int(Template* tpl, const char* name, i64 value) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%lld", (long long)value);
    return template_set_variable(tpl, name, buf);
}

bool_t template_set_variable_float(Template* tpl, const char* name, f64 value) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%f", value);
    return template_set_variable(tpl, name, buf);
}

bool_t template_set_variable_bool(Template* tpl, const char* name, bool_t value) {
    return template_set_variable(tpl, name, value ? "true" : "false");
}

void template_clear_variables(Template* tpl) {
    if (!tpl) return;
    
    for (size_t i = 0; i < tpl->variable_count; i++) {
        kmm_v4_free(tpl->variables[i].name);
        kmm_v4_free(tpl->variables[i].value);
    }
    tpl->variable_count = 0;
}

bool_t template_add_filter(Template* tpl, const char* name, char* (*filter)(const char*)) {
    if (!tpl || !name || !filter) return false;
    
    if (tpl->filter_count >= tpl->filter_capacity) {
        tpl->filter_capacity = tpl->filter_capacity == 0 ? 8 : tpl->filter_capacity * 2;
        tpl->filters = (struct TemplateFilter*)kmm_v4_realloc(
            tpl->filters, tpl->filter_capacity * sizeof(struct TemplateFilter)
        );
        if (!tpl->filters) return false;
    }
    
    tpl->filters[tpl->filter_count].name = kmm_v4_strdup(name);
    tpl->filters[tpl->filter_count].filter = filter;
    tpl->filter_count++;
    
    return true;
}

bool_t template_has_variable(Template* tpl, const char* name) {
    return template_find_variable(tpl, name) != NULL;
}

char* template_render_string(const char* content, const void* data) {
    Template* tpl = template_create(content);
    if (!tpl) return NULL;
    
    char* result = template_render(tpl, data);
    template_destroy(tpl);
    
    return result;
}
