#pragma once
#include "../base/types.h"
#include "../string/string.h"

typedef struct DBConnection DBConnection;
typedef struct DBStatement DBStatement;
typedef struct DBResult DBResult;
typedef struct DBRow DBRow;

typedef enum {
    DB_TYPE_SQLITE3,
    DB_TYPE_MYSQL,
    DB_TYPE_POSTGRESQL
} DBBackendType;

typedef enum {
    DB_OK = 0,
    DB_ERROR = 1,
    DB_BUSY = 2,
    DB_ROW = 100,
    DB_DONE = 101
} DBStatus;

typedef enum {
    DB_TYPE_INTEGER,
    DB_TYPE_FLOAT,
    DB_TYPE_TEXT,
    DB_TYPE_BLOB,
    DB_TYPE_NULL
} DBColumnType;

typedef int (*DBRowCallback)(void* user_data, int num_cols, char** col_values, char** col_names);

DBConnection* db_open(const char* db_path);
DBStatus db_close(DBConnection* db);
DBStatus db_exec(DBConnection* db, const char* sql, DBRowCallback callback, void* user_data, char** err_msg);

DBStatement* db_prepare(DBConnection* db, const char* sql);
DBStatus db_step(DBStatement* stmt);
DBStatus db_reset(DBStatement* stmt);
DBStatus db_finalize(DBStatement* stmt);

int    db_column_count(DBStatement* stmt);
const char* db_column_name(DBStatement* stmt, int col);
DBColumnType db_column_type(DBStatement* stmt, int col);
i64    db_column_int(DBStatement* stmt, int col);
f64    db_column_double(DBStatement* stmt, int col);
const char* db_column_text(DBStatement* stmt, int col);
const void* db_column_blob(DBStatement* stmt, int col);
int    db_column_bytes(DBStatement* stmt, int col);

DBStatus db_bind_int(DBStatement* stmt, int idx, i64 value);
DBStatus db_bind_double(DBStatement* stmt, int idx, f64 value);
DBStatus db_bind_text(DBStatement* stmt, int idx, const char* value);
DBStatus db_bind_blob(DBStatement* stmt, int idx, const void* value, int size);
DBStatus db_bind_null(DBStatement* stmt, int idx);

i64 db_last_insert_id(DBConnection* db);
int db_changes(DBConnection* db);
const char* db_errmsg(DBConnection* db);
int db_errcode(DBConnection* db);

DBResult* db_query(DBConnection* db, const char* sql, ...);
void db_result_free(DBResult* result);
int db_result_rows(DBResult* result);
int db_result_cols(DBResult* result);
const char* db_result_value(DBResult* result, int row, int col);
const char* db_result_column_name(DBResult* result, int col);

bool_t db_table_exists(DBConnection* db, const char* table_name);
DBStatus db_begin_transaction(DBConnection* db);
DBStatus db_commit(DBConnection* db);
DBStatus db_rollback(DBConnection* db);

const char* db_backend_name(void);
int db_backend_version(void);
