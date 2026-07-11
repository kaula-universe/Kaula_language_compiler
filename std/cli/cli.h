#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef enum {
    CLI_FLAG_BOOL,
    CLI_FLAG_INT,
    CLI_FLAG_FLOAT,
    CLI_FLAG_STRING,
    CLI_FLAG_BOOL_LIST,
    CLI_FLAG_INT_LIST,
    CLI_FLAG_STRING_LIST
} CliFlagType;

typedef struct {
    const char* long_name;
    char        short_name;
    CliFlagType type;
    const char* description;
    const char* default_value;
    bool_t      required;
    bool_t      is_positional;
    int         position;
} CliFlag;

typedef struct {
    const char* name;
    const char* description;
    const char* version;
    CliFlag* flags;
    size_t  flag_count;
    size_t  flag_capacity;
    char**  args;
    size_t  arg_count;
    int     argc;
    char**  argv;
    bool_t  parsed;
    String  program_name;
} CliApp;

typedef struct CliSubcommand CliSubcommand;

struct CliSubcommand {
    const char* name;
    const char* description;
    CliFlag* flags;
    size_t  flag_count;
    size_t  flag_capacity;
    int  (*handler)(CliApp* app, void* user_data);
    CliSubcommand* subcommands;
    size_t  subcommand_count;
};

CliApp* cli_app_create(const char* name, const char* description, const char* version);
void    cli_app_destroy(CliApp* app);

void cli_add_flag(CliApp* app, const char* long_name, char short_name,
                  CliFlagType type, const char* description,
                  const char* default_value, bool_t required);
void cli_add_positional(CliApp* app, const char* name, CliFlagType type,
                        const char* description, int position, bool_t required);

bool_t cli_parse(CliApp* app, int argc, char** argv);

bool_t cli_get_bool(CliApp* app, const char* name);
i64    cli_get_int(CliApp* app, const char* name);
f64    cli_get_float(CliApp* app, const char* name);
String cli_get_string(CliApp* app, const char* name);

char** cli_get_string_list(CliApp* app, const char* name, size_t* out_count);

bool_t cli_is_set(CliApp* app, const char* name);

size_t cli_arg_count(CliApp* app);
String cli_arg(CliApp* app, size_t index);
String cli_arg_at(CliApp* app, size_t index);

void cli_print_help(CliApp* app);
void cli_print_version(CliApp* app);

bool_t cli_help_requested(CliApp* app);
bool_t cli_version_requested(CliApp* app);

const char* cli_program_name(CliApp* app);
