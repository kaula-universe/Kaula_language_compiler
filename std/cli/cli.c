#include "cli.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CLI_INITIAL_FLAG_CAPACITY 8
#define CLI_INITIAL_ARGS_CAPACITY 16

typedef struct {
    char** values;
    size_t count;
    size_t capacity;
    bool_t is_set;
} CliParsedValue;

typedef struct CliAppInternal CliAppInternal;
struct CliAppInternal {
    CliParsedValue* parsed_values;
    size_t parsed_count;
    bool_t help_requested;
    bool_t version_requested;
    char** positional_args;
    size_t positional_arg_count;
    size_t positional_arg_capacity;
};

static CliAppInternal* get_internal(CliApp* app) {
    return (CliAppInternal*)((unsigned char*)app + sizeof(CliApp));
}

static CliFlag* find_flag_by_long_name(CliApp* app, const char* name) {
    size_t i;
    if (!app || !name) return NULL;
    for (i = 0; i < app->flag_count; i++) {
        if (app->flags[i].long_name && strcmp(app->flags[i].long_name, name) == 0) {
            return &app->flags[i];
        }
    }
    return NULL;
}

static CliFlag* find_flag_by_short_name(CliApp* app, char short_name) {
    size_t i;
    if (!app || short_name == 0) return NULL;
    for (i = 0; i < app->flag_count; i++) {
        if (app->flags[i].short_name == short_name) {
            return &app->flags[i];
        }
    }
    return NULL;
}

static CliFlag* find_positional_by_position(CliApp* app, int position) {
    size_t i;
    if (!app) return NULL;
    for (i = 0; i < app->flag_count; i++) {
        if (app->flags[i].is_positional && app->flags[i].position == position) {
            return &app->flags[i];
        }
    }
    return NULL;
}

static size_t find_flag_index(CliApp* app, CliFlag* flag) {
    size_t i;
    if (!app || !flag) return (size_t)-1;
    for (i = 0; i < app->flag_count; i++) {
        if (&app->flags[i] == flag) {
            return i;
        }
    }
    return (size_t)-1;
}

static void parsed_value_add(CliParsedValue* pv, const char* value) {
    char* dup;
    size_t len;
    if (!pv || !value) return;
    if (pv->count >= pv->capacity) {
        size_t new_cap = pv->capacity == 0 ? 4 : pv->capacity * 2;
        char** new_values = (char**)kmm_v4_malloc(new_cap * sizeof(char*));
        if (!new_values) return;
        if (pv->values) {
            memcpy(new_values, pv->values, pv->count * sizeof(char*));
            kmm_v4_free(pv->values);
        }
        pv->values = new_values;
        pv->capacity = new_cap;
    }
    len = strlen(value);
    dup = (char*)kmm_v4_malloc(len + 1);
    if (!dup) return;
    memcpy(dup, value, len + 1);
    pv->values[pv->count++] = dup;
    pv->is_set = true;
}

static void parsed_value_free(CliParsedValue* pv) {
    size_t i;
    if (!pv) return;
    if (pv->values) {
        for (i = 0; i < pv->count; i++) {
            if (pv->values[i]) {
                kmm_v4_free(pv->values[i]);
            }
        }
        kmm_v4_free(pv->values);
    }
    pv->values = NULL;
    pv->count = 0;
    pv->capacity = 0;
    pv->is_set = false;
}

static const char* get_default_for_flag(CliFlag* flag) {
    if (!flag) return NULL;
    return flag->default_value;
}

static void add_positional_arg(CliAppInternal* internal, const char* arg) {
    char* dup;
    size_t len;
    if (!internal || !arg) return;
    if (internal->positional_arg_count >= internal->positional_arg_capacity) {
        size_t new_cap = internal->positional_arg_capacity == 0 ?
                         CLI_INITIAL_ARGS_CAPACITY : internal->positional_arg_capacity * 2;
        char** new_args = (char**)kmm_v4_malloc(new_cap * sizeof(char*));
        if (!new_args) return;
        if (internal->positional_args) {
            memcpy(new_args, internal->positional_args,
                   internal->positional_arg_count * sizeof(char*));
            kmm_v4_free(internal->positional_args);
        }
        internal->positional_args = new_args;
        internal->positional_arg_capacity = new_cap;
    }
    len = strlen(arg);
    dup = (char*)kmm_v4_malloc(len + 1);
    if (!dup) return;
    memcpy(dup, arg, len + 1);
    internal->positional_args[internal->positional_arg_count++] = dup;
}

static const char* extract_program_name(const char* path) {
    const char* name;
    if (!path) return "program";
    name = strrchr(path, '/');
    if (name) {
        name++;
    } else {
        name = strrchr(path, '\\');
        if (name) {
            name++;
        } else {
            name = path;
        }
    }
    return name;
}

CliApp* cli_app_create(const char* name, const char* description, const char* version) {
    CliApp* app;
    CliAppInternal* internal;
    size_t total_size;
    if (!name) return NULL;

    total_size = sizeof(CliApp) + sizeof(CliAppInternal);
    app = (CliApp*)kmm_v4_malloc(total_size);
    if (!app) return NULL;
    memset(app, 0, total_size);

    app->name = name;
    app->description = description ? description : "";
    app->version = version ? version : "0.0.0";
    app->flags = NULL;
    app->flag_count = 0;
    app->flag_capacity = 0;
    app->args = NULL;
    app->arg_count = 0;
    app->argc = 0;
    app->argv = NULL;
    app->parsed = false;
    app->program_name = (String){0, NULL};

    internal = get_internal(app);
    internal->parsed_values = NULL;
    internal->parsed_count = 0;
    internal->help_requested = false;
    internal->version_requested = false;
    internal->positional_args = NULL;
    internal->positional_arg_count = 0;
    internal->positional_arg_capacity = 0;

    cli_add_flag(app, "help", 'h', CLI_FLAG_BOOL, "Show help message", NULL, false);
    cli_add_flag(app, "version", 'V', CLI_FLAG_BOOL, "Show version information", NULL, false);

    return app;
}

void cli_app_destroy(CliApp* app) {
    CliAppInternal* internal;
    size_t i;
    if (!app) return;

    internal = get_internal(app);

    if (internal->parsed_values) {
        for (i = 0; i < internal->parsed_count; i++) {
            parsed_value_free(&internal->parsed_values[i]);
        }
        kmm_v4_free(internal->parsed_values);
    }

    if (internal->positional_args) {
        for (i = 0; i < internal->positional_arg_count; i++) {
            if (internal->positional_args[i]) {
                kmm_v4_free(internal->positional_args[i]);
            }
        }
        kmm_v4_free(internal->positional_args);
    }

    if (app->flags) {
        kmm_v4_free(app->flags);
    }

    if (app->program_name.len > 0) {
        string_free(app->program_name);
    }

    kmm_v4_free(app);
}

void cli_add_flag(CliApp* app, const char* long_name, char short_name,
                  CliFlagType type, const char* description,
                  const char* default_value, bool_t required) {
    CliFlag flag;
    CliAppInternal* internal;
    CliParsedValue* new_parsed;
    if (!app || !long_name) return;
    if (app->parsed) return;

    if (app->flag_count >= app->flag_capacity) {
        size_t new_cap = app->flag_capacity == 0 ?
                         CLI_INITIAL_FLAG_CAPACITY : app->flag_capacity * 2;
        CliFlag* new_flags = (CliFlag*)kmm_v4_malloc(new_cap * sizeof(CliFlag));
        if (!new_flags) return;
        if (app->flags) {
            memcpy(new_flags, app->flags, app->flag_count * sizeof(CliFlag));
            kmm_v4_free(app->flags);
        }
        app->flags = new_flags;
        app->flag_capacity = new_cap;

        internal = get_internal(app);
        new_parsed = (CliParsedValue*)kmm_v4_malloc(new_cap * sizeof(CliParsedValue));
        if (!new_parsed) return;
        memset(new_parsed, 0, new_cap * sizeof(CliParsedValue));
        if (internal->parsed_values) {
            memcpy(new_parsed, internal->parsed_values,
                   internal->parsed_count * sizeof(CliParsedValue));
            kmm_v4_free(internal->parsed_values);
        }
        internal->parsed_values = new_parsed;
        internal->parsed_count = app->flag_count;
    }

    memset(&flag, 0, sizeof(flag));
    flag.long_name = long_name;
    flag.short_name = short_name;
    flag.type = type;
    flag.description = description ? description : "";
    flag.default_value = default_value;
    flag.required = required;
    flag.is_positional = false;
    flag.position = -1;

    app->flags[app->flag_count++] = flag;

    internal = get_internal(app);
    internal->parsed_count = app->flag_count;
}

void cli_add_positional(CliApp* app, const char* name, CliFlagType type,
                        const char* description, int position, bool_t required) {
    CliFlag flag;
    CliAppInternal* internal;
    CliParsedValue* new_parsed;
    if (!app || !name) return;
    if (app->parsed) return;

    if (app->flag_count >= app->flag_capacity) {
        size_t new_cap = app->flag_capacity == 0 ?
                         CLI_INITIAL_FLAG_CAPACITY : app->flag_capacity * 2;
        CliFlag* new_flags = (CliFlag*)kmm_v4_malloc(new_cap * sizeof(CliFlag));
        if (!new_flags) return;
        if (app->flags) {
            memcpy(new_flags, app->flags, app->flag_count * sizeof(CliFlag));
            kmm_v4_free(app->flags);
        }
        app->flags = new_flags;
        app->flag_capacity = new_cap;

        internal = get_internal(app);
        new_parsed = (CliParsedValue*)kmm_v4_malloc(new_cap * sizeof(CliParsedValue));
        if (!new_parsed) return;
        memset(new_parsed, 0, new_cap * sizeof(CliParsedValue));
        if (internal->parsed_values) {
            memcpy(new_parsed, internal->parsed_values,
                   internal->parsed_count * sizeof(CliParsedValue));
            kmm_v4_free(internal->parsed_values);
        }
        internal->parsed_values = new_parsed;
        internal->parsed_count = app->flag_count;
    }

    memset(&flag, 0, sizeof(flag));
    flag.long_name = name;
    flag.short_name = 0;
    flag.type = type;
    flag.description = description ? description : "";
    flag.default_value = NULL;
    flag.required = required;
    flag.is_positional = true;
    flag.position = position;

    app->flags[app->flag_count++] = flag;

    internal = get_internal(app);
    internal->parsed_count = app->flag_count;
}

static bool_t parse_bool_value(const char* value) {
    if (!value) return false;
    if (strcmp(value, "true") == 0 || strcmp(value, "1") == 0 ||
        strcmp(value, "yes") == 0 || strcmp(value, "on") == 0 ||
        strcmp(value, "TRUE") == 0 || strcmp(value, "YES") == 0 ||
        strcmp(value, "ON") == 0) {
        return true;
    }
    return false;
}

static i64 parse_int_value(const char* value) {
    if (!value) return 0;
    return (i64)atoll(value);
}

static f64 parse_float_value(const char* value) {
    if (!value) return 0.0;
    return (f64)atof(value);
}

bool_t cli_parse(CliApp* app, int argc, char** argv) {
    CliAppInternal* internal;
    int i;
    int positional_idx = 0;
    if (!app || argc < 1 || !argv) return false;

    internal = get_internal(app);
    app->argc = argc;
    app->argv = argv;

    if (app->program_name.len > 0) {
        string_free(app->program_name);
    }
    app->program_name = string_create(extract_program_name(argv[0]));

    for (i = 0; i < app->flag_count; i++) {
        parsed_value_free(&internal->parsed_values[i]);
    }
    internal->help_requested = false;
    internal->version_requested = false;
    if (internal->positional_args) {
        for (i = 0; (size_t)i < internal->positional_arg_count; i++) {
            if (internal->positional_args[i]) {
                kmm_v4_free(internal->positional_args[i]);
            }
        }
        kmm_v4_free(internal->positional_args);
        internal->positional_args = NULL;
        internal->positional_arg_count = 0;
        internal->positional_arg_capacity = 0;
    }

    for (i = 1; i < argc; i++) {
        const char* arg = argv[i];
        size_t arg_len;

        if (arg[0] != '-') {
            CliFlag* pos_flag = find_positional_by_position(app, positional_idx);
            if (pos_flag) {
                size_t idx = find_flag_index(app, pos_flag);
                if (idx != (size_t)-1) {
                    parsed_value_add(&internal->parsed_values[idx], arg);
                }
                positional_idx++;
            } else {
                add_positional_arg(internal, arg);
            }
            continue;
        }

        arg_len = strlen(arg);

        if (arg_len == 2 && arg[1] == '-') {
            i++;
            while (i < argc) {
                CliFlag* pos_flag = find_positional_by_position(app, positional_idx);
                if (pos_flag) {
                    size_t idx = find_flag_index(app, pos_flag);
                    if (idx != (size_t)-1) {
                        parsed_value_add(&internal->parsed_values[idx], argv[i]);
                    }
                    positional_idx++;
                } else {
                    add_positional_arg(internal, argv[i]);
                }
                i++;
            }
            break;
        }

        if (arg[1] == '-') {
            const char* name_start = arg + 2;
            const char* eq_pos = strchr(name_start, '=');
            char* name_buf;
            size_t name_len;
            CliFlag* flag;
            size_t flag_idx;

            if (eq_pos) {
                name_len = (size_t)(eq_pos - name_start);
            } else {
                name_len = strlen(name_start);
            }

            name_buf = (char*)kmm_v4_malloc(name_len + 1);
            if (!name_buf) return false;
            memcpy(name_buf, name_start, name_len);
            name_buf[name_len] = '\0';

            flag = find_flag_by_long_name(app, name_buf);
            kmm_v4_free(name_buf);

            if (!flag) {
                fprintf(stderr, "Unknown option: --%s\n", name_start);
                return false;
            }

            flag_idx = find_flag_index(app, flag);

            if (flag->type == CLI_FLAG_BOOL) {
                if (eq_pos) {
                    parsed_value_add(&internal->parsed_values[flag_idx], eq_pos + 1);
                } else {
                    parsed_value_add(&internal->parsed_values[flag_idx], "true");
                }
            } else if (flag->type == CLI_FLAG_BOOL_LIST) {
                if (eq_pos) {
                    parsed_value_add(&internal->parsed_values[flag_idx], eq_pos + 1);
                } else {
                    if (i + 1 < argc) {
                        i++;
                        parsed_value_add(&internal->parsed_values[flag_idx], argv[i]);
                    } else {
                        fprintf(stderr, "Option --%s requires a value\n", flag->long_name);
                        return false;
                    }
                }
            } else {
                if (eq_pos) {
                    parsed_value_add(&internal->parsed_values[flag_idx], eq_pos + 1);
                } else {
                    if (i + 1 < argc) {
                        i++;
                        parsed_value_add(&internal->parsed_values[flag_idx], argv[i]);
                    } else {
                        fprintf(stderr, "Option --%s requires a value\n", flag->long_name);
                        return false;
                    }
                }
            }

            if (strcmp(flag->long_name, "help") == 0) {
                internal->help_requested = true;
            }
            if (strcmp(flag->long_name, "version") == 0) {
                internal->version_requested = true;
            }

            continue;
        }

        {
            int j;
            for (j = 1; arg[j] != '\0'; j++) {
                char c = arg[j];
                CliFlag* flag = find_flag_by_short_name(app, c);
                size_t flag_idx;

                if (!flag) {
                    fprintf(stderr, "Unknown option: -%c\n", c);
                    return false;
                }

                flag_idx = find_flag_index(app, flag);

                if (flag->type == CLI_FLAG_BOOL) {
                    parsed_value_add(&internal->parsed_values[flag_idx], "true");
                } else {
                    const char* rest = arg + j + 1;
                    if (rest[0] != '\0') {
                        parsed_value_add(&internal->parsed_values[flag_idx], rest);
                    } else {
                        if (i + 1 < argc) {
                            i++;
                            parsed_value_add(&internal->parsed_values[flag_idx], argv[i]);
                        } else {
                            fprintf(stderr, "Option -%c requires a value\n", c);
                            return false;
                        }
                    }
                    break;
                }

                if (flag->short_name == 'h' && strcmp(flag->long_name, "help") == 0) {
                    internal->help_requested = true;
                }
                if (flag->short_name == 'V' && strcmp(flag->long_name, "version") == 0) {
                    internal->version_requested = true;
                }
            }
        }
    }

    {
        size_t k;
        for (k = 0; k < app->flag_count; k++) {
            CliFlag* flag = &app->flags[k];
            if (flag->required && !internal->parsed_values[k].is_set) {
                if (flag->is_positional) {
                    fprintf(stderr, "Missing required positional argument: %s\n", flag->long_name);
                } else {
                    fprintf(stderr, "Missing required option: --%s\n", flag->long_name);
                }
                return false;
            }
        }
    }

    app->parsed = true;
    app->arg_count = internal->positional_arg_count;
    app->args = internal->positional_args;

    return true;
}

bool_t cli_get_bool(CliApp* app, const char* name) {
    CliFlag* flag;
    CliAppInternal* internal;
    size_t idx;
    CliParsedValue* pv;
    if (!app || !name) return false;

    flag = find_flag_by_long_name(app, name);
    if (!flag) return false;

    internal = get_internal(app);
    idx = find_flag_index(app, flag);
    if (idx == (size_t)-1) return false;

    pv = &internal->parsed_values[idx];
    if (pv->is_set && pv->count > 0) {
        return parse_bool_value(pv->values[pv->count - 1]);
    }
    if (flag->default_value) {
        return parse_bool_value(flag->default_value);
    }
    return false;
}

i64 cli_get_int(CliApp* app, const char* name) {
    CliFlag* flag;
    CliAppInternal* internal;
    size_t idx;
    CliParsedValue* pv;
    if (!app || !name) return 0;

    flag = find_flag_by_long_name(app, name);
    if (!flag) return 0;

    internal = get_internal(app);
    idx = find_flag_index(app, flag);
    if (idx == (size_t)-1) return 0;

    pv = &internal->parsed_values[idx];
    if (pv->is_set && pv->count > 0) {
        return parse_int_value(pv->values[pv->count - 1]);
    }
    if (flag->default_value) {
        return parse_int_value(flag->default_value);
    }
    return 0;
}

f64 cli_get_float(CliApp* app, const char* name) {
    CliFlag* flag;
    CliAppInternal* internal;
    size_t idx;
    CliParsedValue* pv;
    if (!app || !name) return 0.0;

    flag = find_flag_by_long_name(app, name);
    if (!flag) return 0.0;

    internal = get_internal(app);
    idx = find_flag_index(app, flag);
    if (idx == (size_t)-1) return 0.0;

    pv = &internal->parsed_values[idx];
    if (pv->is_set && pv->count > 0) {
        return parse_float_value(pv->values[pv->count - 1]);
    }
    if (flag->default_value) {
        return parse_float_value(flag->default_value);
    }
    return 0.0;
}

String cli_get_string(CliApp* app, const char* name) {
    CliFlag* flag;
    CliAppInternal* internal;
    size_t idx;
    CliParsedValue* pv;
    if (!app || !name) return STRING_EMPTY;

    flag = find_flag_by_long_name(app, name);
    if (!flag) return STRING_EMPTY;

    internal = get_internal(app);
    idx = find_flag_index(app, flag);
    if (idx == (size_t)-1) return STRING_EMPTY;

    pv = &internal->parsed_values[idx];
    if (pv->is_set && pv->count > 0) {
        return string_create(pv->values[pv->count - 1]);
    }
    if (flag->default_value) {
        return string_create(flag->default_value);
    }
    return STRING_EMPTY;
}

char** cli_get_string_list(CliApp* app, const char* name, size_t* out_count) {
    CliFlag* flag;
    CliAppInternal* internal;
    size_t idx;
    CliParsedValue* pv;
    char** result;
    size_t i;
    if (out_count) *out_count = 0;
    if (!app || !name) return NULL;

    flag = find_flag_by_long_name(app, name);
    if (!flag) return NULL;

    internal = get_internal(app);
    idx = find_flag_index(app, flag);
    if (idx == (size_t)-1) return NULL;

    pv = &internal->parsed_values[idx];
    if (!pv->is_set || pv->count == 0) {
        return NULL;
    }

    result = (char**)kmm_v4_malloc(pv->count * sizeof(char*));
    if (!result) return NULL;

    for (i = 0; i < pv->count; i++) {
        result[i] = string_create(pv->values[i]).ptr;
    }

    if (out_count) *out_count = pv->count;
    return result;
}

bool_t cli_is_set(CliApp* app, const char* name) {
    CliFlag* flag;
    CliAppInternal* internal;
    size_t idx;
    if (!app || !name) return false;

    flag = find_flag_by_long_name(app, name);
    if (!flag) return false;

    internal = get_internal(app);
    idx = find_flag_index(app, flag);
    if (idx == (size_t)-1) return false;

    return internal->parsed_values[idx].is_set;
}

size_t cli_arg_count(CliApp* app) {
    if (!app) return 0;
    return app->arg_count;
}

String cli_arg(CliApp* app, size_t index) {
    CliAppInternal* internal;
    if (!app || index >= app->arg_count) return STRING_EMPTY;
    internal = get_internal(app);
    if (!internal->positional_args) return STRING_EMPTY;
    return string_create(internal->positional_args[index]);
}

String cli_arg_at(CliApp* app, size_t index) {
    return cli_arg(app, index);
}

void cli_print_help(CliApp* app) {
    size_t i;
    size_t max_len = 0;
    if (!app) return;

    printf("Usage: %s [OPTIONS]", app->program_name.len > 0 ? app->program_name.ptr : app->name);

    for (i = 0; i < app->flag_count; i++) {
        if (app->flags[i].is_positional) {
            if (app->flags[i].required) {
                printf(" %s", app->flags[i].long_name);
            } else {
                printf(" [%s]", app->flags[i].long_name);
            }
        }
    }
    printf("\n\n");

    if (app->description && app->description[0] != '\0') {
        printf("%s\n\n", app->description);
    }

    printf("Options:\n");

    for (i = 0; i < app->flag_count; i++) {
        if (!app->flags[i].is_positional) {
            size_t len = strlen(app->flags[i].long_name);
            if (len > max_len) max_len = len;
        }
    }

    for (i = 0; i < app->flag_count; i++) {
        CliFlag* flag = &app->flags[i];
        if (flag->is_positional) continue;

        if (flag->short_name) {
            printf("  -%c, --%-*s  ", flag->short_name, (int)max_len, flag->long_name);
        } else {
            printf("      --%-*s  ", (int)max_len, flag->long_name);
        }
        printf("%s", flag->description);
        if (flag->default_value) {
            printf(" (default: %s)", flag->default_value);
        }
        if (flag->required) {
            printf(" [required]");
        }
        printf("\n");
    }

    {
        bool_t has_positional = false;
        for (i = 0; i < app->flag_count; i++) {
            if (app->flags[i].is_positional) {
                has_positional = true;
                break;
            }
        }
        if (has_positional) {
            printf("\nPositional arguments:\n");
            for (i = 0; i < app->flag_count; i++) {
                CliFlag* flag = &app->flags[i];
                if (!flag->is_positional) continue;
                printf("  %-*s  %s", (int)max_len, flag->long_name, flag->description);
                if (flag->required) {
                    printf(" [required]");
                }
                printf("\n");
            }
        }
    }
}

void cli_print_version(CliApp* app) {
    if (!app) return;
    printf("%s %s\n", app->name, app->version);
}

bool_t cli_help_requested(CliApp* app) {
    CliAppInternal* internal;
    if (!app) return false;
    internal = get_internal(app);
    return internal->help_requested;
}

bool_t cli_version_requested(CliApp* app) {
    CliAppInternal* internal;
    if (!app) return false;
    internal = get_internal(app);
    return internal->version_requested;
}

const char* cli_program_name(CliApp* app) {
    if (!app) return NULL;
    if (app->program_name.len > 0) {
        return app->program_name.ptr;
    }
    return app->name;
}
