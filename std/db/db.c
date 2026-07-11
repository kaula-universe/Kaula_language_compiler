#include "db.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdarg.h>

#ifdef KUALA_USE_SQLITE3
#include <sqlite3.h>
#endif

#define DB_MAX_ERRMSG 512
#define DB_INITIAL_CAPACITY 16
#define DB_MAX_COLUMNS 256
#define DB_MAX_BIND_PARAMS 256

#ifdef KUALA_USE_SQLITE3

struct DBConnection {
    sqlite3* handle;
    char errmsg[DB_MAX_ERRMSG];
    i64 last_insert_id;
    int changes;
};

struct DBStatement {
    DBConnection* db;
    sqlite3_stmt* stmt;
    char* sql;
};

struct DBResult {
    int rows;
    int cols;
    char** data;
    char** col_names;
};

struct DBRow {
    int cols;
    char** values;
    char** names;
};

static void db_set_error(DBConnection* db, const char* msg) {
    if (!db) return;
    if (msg) {
        strncpy(db->errmsg, msg, DB_MAX_ERRMSG - 1);
        db->errmsg[DB_MAX_ERRMSG - 1] = '\0';
    } else {
        db->errmsg[0] = '\0';
    }
}

DBConnection* db_open(const char* db_path) {
    DBConnection* db;
    int rc;
    if (!db_path) return NULL;
    db = (DBConnection*)kmm_v4_malloc(sizeof(DBConnection));
    if (!db) return NULL;
    memset(db, 0, sizeof(DBConnection));
    rc = sqlite3_open(db_path, &db->handle);
    if (rc != SQLITE_OK) {
        if (db->handle) {
            const char* err = sqlite3_errmsg(db->handle);
            db_set_error(db, err);
            sqlite3_close(db->handle);
        } else {
            db_set_error(db, "failed to open database");
        }
        kmm_v4_free(db);
        return NULL;
    }
    db->last_insert_id = 0;
    db->changes = 0;
    return db;
}

DBStatus db_close(DBConnection* db) {
    if (!db) return DB_ERROR;
    if (db->handle) {
        sqlite3_close(db->handle);
        db->handle = NULL;
    }
    kmm_v4_free(db);
    return DB_OK;
}

static int db_exec_callback(void* user_data, int num_cols, char** col_values, char** col_names) {
    DBRowCallback cb = (DBRowCallback)user_data;
    if (!cb) return 0;
    return cb(NULL, num_cols, col_values, col_names);
}

DBStatus db_exec(DBConnection* db, const char* sql, DBRowCallback callback, void* user_data, char** err_msg) {
    char* z_err_msg = NULL;
    int rc;
    if (!db || !sql) return DB_ERROR;
    rc = sqlite3_exec(db->handle, sql, callback ? db_exec_callback : NULL,
                      callback ? (void*)callback : user_data, &z_err_msg);
    if (rc != SQLITE_OK) {
        db_set_error(db, z_err_msg ? z_err_msg : "exec failed");
        if (err_msg && z_err_msg) {
            *err_msg = string_copy(z_err_msg);
        }
        if (z_err_msg) sqlite3_free(z_err_msg);
        return DB_ERROR;
    }
    if (z_err_msg) sqlite3_free(z_err_msg);
    db->last_insert_id = (i64)sqlite3_last_insert_rowid(db->handle);
    db->changes = sqlite3_changes(db->handle);
    return DB_OK;
}

DBStatement* db_prepare(DBConnection* db, const char* sql) {
    DBStatement* stmt;
    int rc;
    if (!db || !sql) return NULL;
    stmt = (DBStatement*)kmm_v4_malloc(sizeof(DBStatement));
    if (!stmt) return NULL;
    memset(stmt, 0, sizeof(DBStatement));
    stmt->db = db;
    stmt->sql = string_copy(sql);
    rc = sqlite3_prepare_v2(db->handle, sql, -1, &stmt->stmt, NULL);
    if (rc != SQLITE_OK) {
        db_set_error(db, sqlite3_errmsg(db->handle));
        if (stmt->sql) kmm_v4_free(stmt->sql);
        kmm_v4_free(stmt);
        return NULL;
    }
    return stmt;
}

DBStatus db_step(DBStatement* stmt) {
    int rc;
    if (!stmt || !stmt->stmt) return DB_ERROR;
    rc = sqlite3_step(stmt->stmt);
    switch (rc) {
        case SQLITE_ROW:
            return DB_ROW;
        case SQLITE_DONE:
            stmt->db->last_insert_id = (i64)sqlite3_last_insert_rowid(stmt->db->handle);
            stmt->db->changes = sqlite3_changes(stmt->db->handle);
            return DB_DONE;
        case SQLITE_BUSY:
            return DB_BUSY;
        default:
            db_set_error(stmt->db, sqlite3_errmsg(stmt->db->handle));
            return DB_ERROR;
    }
}

DBStatus db_reset(DBStatement* stmt) {
    int rc;
    if (!stmt || !stmt->stmt) return DB_ERROR;
    rc = sqlite3_reset(stmt->stmt);
    if (rc != SQLITE_OK) {
        db_set_error(stmt->db, sqlite3_errmsg(stmt->db->handle));
        return DB_ERROR;
    }
    return DB_OK;
}

DBStatus db_finalize(DBStatement* stmt) {
    if (!stmt) return DB_ERROR;
    if (stmt->stmt) {
        sqlite3_finalize(stmt->stmt);
        stmt->stmt = NULL;
    }
    if (stmt->sql) kmm_v4_free(stmt->sql);
    kmm_v4_free(stmt);
    return DB_OK;
}

int db_column_count(DBStatement* stmt) {
    if (!stmt || !stmt->stmt) return 0;
    return sqlite3_column_count(stmt->stmt);
}

const char* db_column_name(DBStatement* stmt, int col) {
    if (!stmt || !stmt->stmt || col < 0) return NULL;
    return sqlite3_column_name(stmt->stmt, col);
}

DBColumnType db_column_type(DBStatement* stmt, int col) {
    int t;
    if (!stmt || !stmt->stmt || col < 0) return DB_TYPE_NULL;
    t = sqlite3_column_type(stmt->stmt, col);
    switch (t) {
        case SQLITE_INTEGER: return DB_TYPE_INTEGER;
        case SQLITE_FLOAT:   return DB_TYPE_FLOAT;
        case SQLITE_TEXT:    return DB_TYPE_TEXT;
        case SQLITE_BLOB:    return DB_TYPE_BLOB;
        case SQLITE_NULL:
        default:             return DB_TYPE_NULL;
    }
}

i64 db_column_int(DBStatement* stmt, int col) {
    if (!stmt || !stmt->stmt || col < 0) return 0;
    return (i64)sqlite3_column_int64(stmt->stmt, col);
}

f64 db_column_double(DBStatement* stmt, int col) {
    if (!stmt || !stmt->stmt || col < 0) return 0.0;
    return (f64)sqlite3_column_double(stmt->stmt, col);
}

const char* db_column_text(DBStatement* stmt, int col) {
    if (!stmt || !stmt->stmt || col < 0) return NULL;
    return (const char*)sqlite3_column_text(stmt->stmt, col);
}

const void* db_column_blob(DBStatement* stmt, int col) {
    if (!stmt || !stmt->stmt || col < 0) return NULL;
    return sqlite3_column_blob(stmt->stmt, col);
}

int db_column_bytes(DBStatement* stmt, int col) {
    if (!stmt || !stmt->stmt || col < 0) return 0;
    return sqlite3_column_bytes(stmt->stmt, col);
}

DBStatus db_bind_int(DBStatement* stmt, int idx, i64 value) {
    int rc;
    if (!stmt || !stmt->stmt) return DB_ERROR;
    rc = sqlite3_bind_int64(stmt->stmt, idx, (sqlite3_int64)value);
    return (rc == SQLITE_OK) ? DB_OK : DB_ERROR;
}

DBStatus db_bind_double(DBStatement* stmt, int idx, f64 value) {
    int rc;
    if (!stmt || !stmt->stmt) return DB_ERROR;
    rc = sqlite3_bind_double(stmt->stmt, idx, (double)value);
    return (rc == SQLITE_OK) ? DB_OK : DB_ERROR;
}

DBStatus db_bind_text(DBStatement* stmt, int idx, const char* value) {
    int rc;
    if (!stmt || !stmt->stmt) return DB_ERROR;
    if (!value) {
        rc = sqlite3_bind_null(stmt->stmt, idx);
    } else {
        rc = sqlite3_bind_text(stmt->stmt, idx, value, -1, SQLITE_TRANSIENT);
    }
    return (rc == SQLITE_OK) ? DB_OK : DB_ERROR;
}

DBStatus db_bind_blob(DBStatement* stmt, int idx, const void* value, int size) {
    int rc;
    if (!stmt || !stmt->stmt) return DB_ERROR;
    if (!value || size <= 0) {
        rc = sqlite3_bind_null(stmt->stmt, idx);
    } else {
        rc = sqlite3_bind_blob(stmt->stmt, idx, value, size, SQLITE_TRANSIENT);
    }
    return (rc == SQLITE_OK) ? DB_OK : DB_ERROR;
}

DBStatus db_bind_null(DBStatement* stmt, int idx) {
    int rc;
    if (!stmt || !stmt->stmt) return DB_ERROR;
    rc = sqlite3_bind_null(stmt->stmt, idx);
    return (rc == SQLITE_OK) ? DB_OK : DB_ERROR;
}

i64 db_last_insert_id(DBConnection* db) {
    if (!db) return 0;
    return db->last_insert_id;
}

int db_changes(DBConnection* db) {
    if (!db) return 0;
    return db->changes;
}

const char* db_errmsg(DBConnection* db) {
    if (!db) return NULL;
    if (db->handle) {
        return sqlite3_errmsg(db->handle);
    }
    return db->errmsg;
}

int db_errcode(DBConnection* db) {
    if (!db) return 0;
    if (db->handle) {
        return sqlite3_errcode(db->handle);
    }
    return 0;
}

DBResult* db_query(DBConnection* db, const char* sql, ...) {
    DBResult* result;
    DBStatement* stmt;
    va_list ap;
    int col_count;
    int row_count = 0;
    int row_cap = DB_INITIAL_CAPACITY;
    char** data = NULL;
    char** col_names = NULL;
    int i, j;
    DBStatus rc;
    char* final_sql;
    if (!db || !sql) return NULL;

    final_sql = string_copy(sql);
    if (!final_sql) return NULL;

    stmt = db_prepare(db, final_sql);
    kmm_v4_free(final_sql);
    if (!stmt) return NULL;

    col_count = sqlite3_column_count(stmt->stmt);
    if (col_count <= 0) {
        db_finalize(stmt);
        result = (DBResult*)kmm_v4_malloc(sizeof(DBResult));
        if (!result) return NULL;
        memset(result, 0, sizeof(DBResult));
        return result;
    }

    col_names = (char**)kmm_v4_malloc(sizeof(char*) * col_count);
    if (!col_names) {
        db_finalize(stmt);
        return NULL;
    }
    for (i = 0; i < col_count; i++) {
        const char* n = sqlite3_column_name(stmt->stmt, i);
        col_names[i] = n ? string_copy(n) : NULL;
    }

    data = (char**)kmm_v4_malloc(sizeof(char*) * row_cap * col_count);
    if (!data) {
        for (i = 0; i < col_count; i++) {
            if (col_names[i]) kmm_v4_free(col_names[i]);
        }
        kmm_v4_free(col_names);
        db_finalize(stmt);
        return NULL;
    }

    while ((rc = db_step(stmt)) == DB_ROW) {
        if (row_count >= row_cap) {
            char** new_data;
            int new_cap = row_cap * 2;
            new_data = (char**)kmm_v4_malloc(sizeof(char*) * new_cap * col_count);
            if (!new_data) {
                for (i = 0; i < row_count * col_count; i++) {
                    if (data[i]) kmm_v4_free(data[i]);
                }
                kmm_v4_free(data);
                for (i = 0; i < col_count; i++) {
                    if (col_names[i]) kmm_v4_free(col_names[i]);
                }
                kmm_v4_free(col_names);
                db_finalize(stmt);
                return NULL;
            }
            memcpy(new_data, data, sizeof(char*) * row_count * col_count);
            kmm_v4_free(data);
            data = new_data;
            row_cap = new_cap;
        }
        for (j = 0; j < col_count; j++) {
            const char* v = (const char*)sqlite3_column_text(stmt->stmt, j);
            int idx = row_count * col_count + j;
            if (v) {
                data[idx] = string_copy(v);
            } else {
                data[idx] = NULL;
            }
        }
        row_count++;
    }

    db_finalize(stmt);

    result = (DBResult*)kmm_v4_malloc(sizeof(DBResult));
    if (!result) {
        for (i = 0; i < row_count * col_count; i++) {
            if (data[i]) kmm_v4_free(data[i]);
        }
        kmm_v4_free(data);
        for (i = 0; i < col_count; i++) {
            if (col_names[i]) kmm_v4_free(col_names[i]);
        }
        kmm_v4_free(col_names);
        return NULL;
    }
    result->rows = row_count;
    result->cols = col_count;
    result->data = data;
    result->col_names = col_names;
    return result;
}

void db_result_free(DBResult* result) {
    int i;
    if (!result) return;
    if (result->data) {
        for (i = 0; i < result->rows * result->cols; i++) {
            if (result->data[i]) kmm_v4_free(result->data[i]);
        }
        kmm_v4_free(result->data);
    }
    if (result->col_names) {
        for (i = 0; i < result->cols; i++) {
            if (result->col_names[i]) kmm_v4_free(result->col_names[i]);
        }
        kmm_v4_free(result->col_names);
    }
    kmm_v4_free(result);
}

int db_result_rows(DBResult* result) {
    if (!result) return 0;
    return result->rows;
}

int db_result_cols(DBResult* result) {
    if (!result) return 0;
    return result->cols;
}

const char* db_result_value(DBResult* result, int row, int col) {
    int idx;
    if (!result || row < 0 || row >= result->rows || col < 0 || col >= result->cols) {
        return NULL;
    }
    idx = row * result->cols + col;
    return result->data[idx];
}

const char* db_result_column_name(DBResult* result, int col) {
    if (!result || col < 0 || col >= result->cols) return NULL;
    return result->col_names[col];
}

bool_t db_table_exists(DBConnection* db, const char* table_name) {
    DBStatement* stmt;
    const char* sql = "SELECT name FROM sqlite_master WHERE type='table' AND name=?";
    DBStatus rc;
    bool_t exists = 0;
    if (!db || !table_name) return 0;
    stmt = db_prepare(db, sql);
    if (!stmt) return 0;
    db_bind_text(stmt, 1, table_name);
    rc = db_step(stmt);
    if (rc == DB_ROW) {
        exists = 1;
    }
    db_finalize(stmt);
    return exists;
}

DBStatus db_begin_transaction(DBConnection* db) {
    return db_exec(db, "BEGIN", NULL, NULL, NULL);
}

DBStatus db_commit(DBConnection* db) {
    return db_exec(db, "COMMIT", NULL, NULL, NULL);
}

DBStatus db_rollback(DBConnection* db) {
    return db_exec(db, "ROLLBACK", NULL, NULL, NULL);
}

const char* db_backend_name(void) {
    return "SQLite3";
}

int db_backend_version(void) {
    return SQLITE_VERSION_NUMBER;
}

#else

typedef struct MemColumn {
    char* name;
    DBColumnType type;
} MemColumn;

typedef struct MemRow {
    char** values;
    i64 rowid;
} MemRow;

typedef struct MemTable {
    char* name;
    MemColumn* columns;
    int col_count;
    int col_capacity;
    MemRow* rows;
    int row_count;
    int row_capacity;
    i64 next_rowid;
} MemTable;

struct DBConnection {
    MemTable* tables;
    int table_count;
    int table_capacity;
    char errmsg[DB_MAX_ERRMSG];
    i64 last_insert_id;
    int changes;
    int in_transaction;
};

typedef struct BindParam {
    DBColumnType type;
    i64 int_val;
    f64 float_val;
    char* text_val;
    void* blob_val;
    int blob_size;
    int is_null;
} BindParam;

struct DBStatement {
    DBConnection* db;
    char* sql;
    MemTable* current_table;
    int col_count;
    char** col_names;
    int* col_indices;
    int cursor;
    int row_count;
    int* result_rows;
    int result_capacity;
    BindParam* bind_params;
    int bind_count;
    int bind_capacity;
    int stmt_type;
    MemTable* insert_table;
    int* insert_cols;
    int insert_col_count;
};

struct DBResult {
    int rows;
    int cols;
    char** data;
    char** col_names;
};

struct DBRow {
    int cols;
    char** values;
    char** names;
};

#define STMT_SELECT 1
#define STMT_INSERT 2
#define STMT_UPDATE 3
#define STMT_DELETE 4
#define STMT_CREATE_TABLE 5
#define STMT_DROP_TABLE 6
#define STMT_BEGIN 7
#define STMT_COMMIT 8
#define STMT_ROLLBACK 9

static void db_set_error(DBConnection* db, const char* msg) {
    if (!db) return;
    if (msg) {
        strncpy(db->errmsg, msg, DB_MAX_ERRMSG - 1);
        db->errmsg[DB_MAX_ERRMSG - 1] = '\0';
    } else {
        db->errmsg[0] = '\0';
    }
}

static char* str_dup(const char* s) {
    size_t len;
    char* d;
    if (!s) return NULL;
    len = strlen(s);
    d = (char*)kmm_v4_malloc(len + 1);
    if (!d) return NULL;
    memcpy(d, s, len + 1);
    return d;
}

static char* str_trim(const char* s) {
    const char* start;
    const char* end;
    size_t len;
    char* result;
    if (!s) return NULL;
    start = s;
    while (*start && (*start == ' ' || *start == '\t' || *start == '\n' || *start == '\r')) {
        start++;
    }
    end = start + strlen(start);
    while (end > start && (end[-1] == ' ' || end[-1] == '\t' || end[-1] == '\n' || end[-1] == '\r')) {
        end--;
    }
    len = (size_t)(end - start);
    result = (char*)kmm_v4_malloc(len + 1);
    if (!result) return NULL;
    memcpy(result, start, len);
    result[len] = '\0';
    return result;
}

static char* str_tolower(const char* s) {
    char* r;
    size_t i, len;
    if (!s) return NULL;
    len = strlen(s);
    r = (char*)kmm_v4_malloc(len + 1);
    if (!r) return NULL;
    for (i = 0; i < len; i++) {
        char c = s[i];
        if (c >= 'A' && c <= 'Z') r[i] = c + 32;
        else r[i] = c;
    }
    r[len] = '\0';
    return r;
}

static int str_icmp(const char* a, const char* b) {
    if (!a && !b) return 0;
    if (!a) return -1;
    if (!b) return 1;
    while (*a && *b) {
        char ca = *a, cb = *b;
        if (ca >= 'A' && ca <= 'Z') ca += 32;
        if (cb >= 'A' && cb <= 'Z') cb += 32;
        if (ca != cb) return ca - cb;
        a++;
        b++;
    }
    return *a - *b;
}

static int str_startswith_icmp(const char* s, const char* prefix) {
    if (!s || !prefix) return 0;
    while (*prefix) {
        char cs = *s, cp = *prefix;
        if (!cs) return 0;
        if (cs >= 'A' && cs <= 'Z') cs += 32;
        if (cp >= 'A' && cp <= 'Z') cp += 32;
        if (cs != cp) return 0;
        s++;
        prefix++;
    }
    return 1;
}

static MemTable* find_table(DBConnection* db, const char* name) {
    int i;
    if (!db || !name) return NULL;
    for (i = 0; i < db->table_count; i++) {
        if (str_icmp(db->tables[i].name, name) == 0) {
            return &db->tables[i];
        }
    }
    return NULL;
}

static int find_column_index(MemTable* table, const char* name) {
    int i;
    if (!table || !name) return -1;
    for (i = 0; i < table->col_count; i++) {
        if (str_icmp(table->columns[i].name, name) == 0) {
            return i;
        }
    }
    return -1;
}

static MemTable* create_table(DBConnection* db, const char* name) {
    MemTable* t;
    if (!db || !name) return NULL;
    if (db->table_count >= db->table_capacity) {
        int new_cap = db->table_capacity == 0 ? DB_INITIAL_CAPACITY : db->table_capacity * 2;
        MemTable* nt = (MemTable*)kmm_v4_malloc(sizeof(MemTable) * new_cap);
        if (!nt) return NULL;
        memset(nt, 0, sizeof(MemTable) * new_cap);
        if (db->tables && db->table_count > 0) {
            memcpy(nt, db->tables, sizeof(MemTable) * db->table_count);
        }
        if (db->tables) kmm_v4_free(db->tables);
        db->tables = nt;
        db->table_capacity = new_cap;
    }
    t = &db->tables[db->table_count];
    t->name = str_dup(name);
    t->columns = NULL;
    t->col_count = 0;
    t->col_capacity = 0;
    t->rows = NULL;
    t->row_count = 0;
    t->row_capacity = 0;
    t->next_rowid = 1;
    db->table_count++;
    return t;
}

static int add_column(MemTable* table, const char* name, DBColumnType type) {
    if (!table || !name) return -1;
    if (table->col_count >= table->col_capacity) {
        int new_cap = table->col_capacity == 0 ? DB_INITIAL_CAPACITY : table->col_capacity * 2;
        MemColumn* nc = (MemColumn*)kmm_v4_malloc(sizeof(MemColumn) * new_cap);
        if (!nc) return -1;
        memset(nc, 0, sizeof(MemColumn) * new_cap);
        if (table->columns && table->col_count > 0) {
            memcpy(nc, table->columns, sizeof(MemColumn) * table->col_count);
        }
        if (table->columns) kmm_v4_free(table->columns);
        table->columns = nc;
        table->col_capacity = new_cap;
    }
    table->columns[table->col_count].name = str_dup(name);
    table->columns[table->col_count].type = type;
    table->col_count++;
    return table->col_count - 1;
}

static int add_row(MemTable* table, i64 rowid) {
    int i, idx;
    MemRow* r;
    if (!table) return -1;
    if (table->row_count >= table->row_capacity) {
        int new_cap = table->row_capacity == 0 ? DB_INITIAL_CAPACITY : table->row_capacity * 2;
        MemRow* nr = (MemRow*)kmm_v4_malloc(sizeof(MemRow) * new_cap);
        if (!nr) return -1;
        memset(nr, 0, sizeof(MemRow) * new_cap);
        if (table->rows && table->row_count > 0) {
            memcpy(nr, table->rows, sizeof(MemRow) * table->row_count);
        }
        if (table->rows) kmm_v4_free(table->rows);
        table->rows = nr;
        table->row_capacity = new_cap;
    }
    idx = table->row_count;
    r = &table->rows[idx];
    r->rowid = rowid;
    r->values = (char**)kmm_v4_malloc(sizeof(char*) * table->col_count);
    if (!r->values) return -1;
    for (i = 0; i < table->col_count; i++) {
        r->values[i] = NULL;
    }
    table->row_count++;
    return idx;
}

static void free_row(MemTable* table, int row_idx) {
    int j;
    MemRow* r;
    if (!table || row_idx < 0 || row_idx >= table->row_count) return;
    r = &table->rows[row_idx];
    if (r->values) {
        for (j = 0; j < table->col_count; j++) {
            if (r->values[j]) {
                kmm_v4_free(r->values[j]);
                r->values[j] = NULL;
            }
        }
        kmm_v4_free(r->values);
        r->values = NULL;
    }
}

static void free_table_contents(MemTable* t) {
    int i;
    if (!t) return;
    if (t->name) {
        kmm_v4_free(t->name);
        t->name = NULL;
    }
    if (t->columns) {
        for (i = 0; i < t->col_count; i++) {
            if (t->columns[i].name) kmm_v4_free(t->columns[i].name);
        }
        kmm_v4_free(t->columns);
        t->columns = NULL;
    }
    if (t->rows) {
        for (i = 0; i < t->row_count; i++) {
            free_row(t, i);
        }
        kmm_v4_free(t->rows);
        t->rows = NULL;
    }
    t->col_count = 0;
    t->row_count = 0;
}

static DBColumnType parse_column_type(const char* type_str) {
    char* lower;
    DBColumnType t;
    if (!type_str) return DB_TYPE_TEXT;
    lower = str_tolower(type_str);
    if (!lower) return DB_TYPE_TEXT;
    if (strstr(lower, "int")) t = DB_TYPE_INTEGER;
    else if (strstr(lower, "float") || strstr(lower, "double") || strstr(lower, "real")) t = DB_TYPE_FLOAT;
    else if (strstr(lower, "blob")) t = DB_TYPE_BLOB;
    else t = DB_TYPE_TEXT;
    kmm_v4_free(lower);
    return t;
}

static char* extract_identifier(const char** p) {
    const char* s = *p;
    const char* start;
    char* result;
    size_t len;
    while (*s && (*s == ' ' || *s == '\t')) s++;
    if (*s == '\"' || *s == '\'' || *s == '`') {
        char quote = *s;
        s++;
        start = s;
        while (*s && *s != quote) s++;
        len = (size_t)(s - start);
        if (*s == quote) s++;
        *p = s;
        result = (char*)kmm_v4_malloc(len + 1);
        if (!result) return NULL;
        memcpy(result, start, len);
        result[len] = '\0';
        return result;
    }
    start = s;
    while (*s && (*s >= 'A' && *s <= 'Z') || (*s >= 'a' && *s <= 'z') ||
           (*s >= '0' && *s <= '9') || *s == '_') {
        s++;
    }
    len = (size_t)(s - start);
    if (len == 0) {
        *p = s;
        return NULL;
    }
    *p = s;
    result = (char*)kmm_v4_malloc(len + 1);
    if (!result) return NULL;
    memcpy(result, start, len);
    result[len] = '\0';
    return result;
}

static void skip_whitespace(const char** p) {
    while (**p && (**p == ' ' || **p == '\t' || **p == '\n' || **p == '\r')) {
        (*p)++;
    }
}

static char* get_value_string(BindParam* p) {
    char buf[64];
    if (!p) return NULL;
    if (p->is_null) return NULL;
    switch (p->type) {
        case DB_TYPE_INTEGER:
            snprintf(buf, sizeof(buf), "%lld", (long long)p->int_val);
            return str_dup(buf);
        case DB_TYPE_FLOAT:
            snprintf(buf, sizeof(buf), "%g", p->float_val);
            return str_dup(buf);
        case DB_TYPE_TEXT:
            return str_dup(p->text_val);
        default:
            return NULL;
    }
}

DBConnection* db_open(const char* db_path) {
    DBConnection* db;
    (void)db_path;
    db = (DBConnection*)kmm_v4_malloc(sizeof(DBConnection));
    if (!db) return NULL;
    memset(db, 0, sizeof(DBConnection));
    db->tables = NULL;
    db->table_count = 0;
    db->table_capacity = 0;
    db->last_insert_id = 0;
    db->changes = 0;
    db->in_transaction = 0;
    db->errmsg[0] = '\0';
    return db;
}

DBStatus db_close(DBConnection* db) {
    int i;
    if (!db) return DB_ERROR;
    if (db->tables) {
        for (i = 0; i < db->table_count; i++) {
            free_table_contents(&db->tables[i]);
        }
        kmm_v4_free(db->tables);
    }
    kmm_v4_free(db);
    return DB_OK;
}

static int parse_create_table(DBConnection* db, const char* sql) {
    const char* p;
    char* table_name = NULL;
    char* col_name = NULL;
    char* col_type_str = NULL;
    MemTable* table = NULL;
    int paren_depth = 0;
    const char* col_start;
    const char* col_end;
    const char* type_start;
    const char* type_end;
    int in_paren = 0;
    p = sql;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "CREATE")) return 0;
    p += 6;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "TABLE")) return 0;
    p += 5;
    skip_whitespace(&p);
    if (*p == '\"' || *p == '\'' || *p == '`') {
        char quote = *p;
        p++;
        col_start = p;
        while (*p && *p != quote) p++;
        col_end = p;
        if (*p == quote) p++;
        table_name = (char*)kmm_v4_malloc((size_t)(col_end - col_start) + 1);
        if (!table_name) return -1;
        memcpy(table_name, col_start, (size_t)(col_end - col_start));
        table_name[col_end - col_start] = '\0';
    } else {
        col_start = p;
        while (*p && ((*p >= 'A' && *p <= 'Z') || (*p >= 'a' && *p <= 'z') ||
               (*p >= '0' && *p <= '9') || *p == '_')) p++;
        table_name = (char*)kmm_v4_malloc((size_t)(p - col_start) + 1);
        if (!table_name) return -1;
        memcpy(table_name, col_start, (size_t)(p - col_start));
        table_name[p - col_start] = '\0';
    }
    if (!table_name || strlen(table_name) == 0) {
        if (table_name) kmm_v4_free(table_name);
        return -1;
    }
    skip_whitespace(&p);
    if (*p != '(') {
        kmm_v4_free(table_name);
        return -1;
    }
    p++;
    in_paren = 1;
    table = create_table(db, table_name);
    kmm_v4_free(table_name);
    if (!table) return -1;
    while (*p && in_paren) {
        skip_whitespace(&p);
        if (*p == ')') {
            p++;
            in_paren = 0;
            break;
        }
        if (*p == ',') {
            p++;
            continue;
        }
        col_name = extract_identifier(&p);
        if (!col_name) {
            return -1;
        }
        skip_whitespace(&p);
        type_start = p;
        while (*p && *p != '(' && *p != ')' && *p != ',' && *p != ' ' && *p != '\t' && *p != '\n' && *p != '\r') {
            p++;
        }
        type_end = p;
        if (*p == '(') {
            paren_depth = 1;
            p++;
            while (*p && paren_depth > 0) {
                if (*p == '(') paren_depth++;
                else if (*p == ')') paren_depth--;
                p++;
            }
        }
        col_type_str = (char*)kmm_v4_malloc((size_t)(type_end - type_start) + 1);
        if (col_type_str) {
            memcpy(col_type_str, type_start, (size_t)(type_end - type_start));
            col_type_str[type_end - type_start] = '\0';
        }
        {
            DBColumnType ct = parse_column_type(col_type_str);
            add_column(table, col_name, ct);
        }
        if (col_type_str) {
            kmm_v4_free(col_type_str);
            col_type_str = NULL;
        }
        if (col_name) {
            kmm_v4_free(col_name);
            col_name = NULL;
        }
        while (*p && *p != ',' && *p != ')') p++;
    }
    return 1;
}

static int parse_drop_table(DBConnection* db, const char* sql) {
    const char* p = sql;
    char* table_name;
    const char* start;
    MemTable* t;
    int idx;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "DROP")) return 0;
    p += 4;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "TABLE")) return 0;
    p += 5;
    skip_whitespace(&p);
    if (*p == '\"' || *p == '\'' || *p == '`') {
        char quote = *p;
        p++;
        start = p;
        while (*p && *p != quote) p++;
        table_name = (char*)kmm_v4_malloc((size_t)(p - start) + 1);
        if (!table_name) return -1;
        memcpy(table_name, start, (size_t)(p - start));
        table_name[p - start] = '\0';
        if (*p == quote) p++;
    } else {
        start = p;
        while (*p && ((*p >= 'A' && *p <= 'Z') || (*p >= 'a' && *p <= 'z') ||
               (*p >= '0' && *p <= '9') || *p == '_')) p++;
        table_name = (char*)kmm_v4_malloc((size_t)(p - start) + 1);
        if (!table_name) return -1;
        memcpy(table_name, start, (size_t)(p - start));
        table_name[p - start] = '\0';
    }
    t = find_table(db, table_name);
    if (!t) {
        kmm_v4_free(table_name);
        return -1;
    }
    idx = (int)(t - db->tables);
    free_table_contents(t);
    if (idx < db->table_count - 1) {
        memmove(t, t + 1, sizeof(MemTable) * (db->table_count - idx - 1));
    }
    db->table_count--;
    kmm_v4_free(table_name);
    return 1;
}

static int parse_select_table(const char* sql, char** table_name) {
    const char* p = sql;
    const char* from_start;
    const char* start;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "SELECT")) return 0;
    p += 6;
    while (*p) {
        skip_whitespace(&p);
        if (str_startswith_icmp(p, "FROM")) {
            p += 4;
            skip_whitespace(&p);
            break;
        }
        p++;
    }
    if (*p == '\"' || *p == '\'' || *p == '`') {
        char quote = *p;
        p++;
        from_start = p;
        while (*p && *p != quote) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - from_start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, from_start, (size_t)(p - from_start));
        (*table_name)[p - from_start] = '\0';
    } else {
        start = p;
        while (*p && ((*p >= 'A' && *p <= 'Z') || (*p >= 'a' && *p <= 'z') ||
               (*p >= '0' && *p <= '9') || *p == '_')) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, start, (size_t)(p - start));
        (*table_name)[p - start] = '\0';
    }
    return 1;
}

static int parse_insert_table(const char* sql, char** table_name) {
    const char* p = sql;
    const char* start;
    const char* into_start;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "INSERT")) return 0;
    p += 6;
    skip_whitespace(&p);
    if (str_startswith_icmp(p, "INTO")) {
        p += 4;
        skip_whitespace(&p);
    }
    if (*p == '\"' || *p == '\'' || *p == '`') {
        char quote = *p;
        p++;
        into_start = p;
        while (*p && *p != quote) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - into_start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, into_start, (size_t)(p - into_start));
        (*table_name)[p - into_start] = '\0';
    } else {
        start = p;
        while (*p && ((*p >= 'A' && *p <= 'Z') || (*p >= 'a' && *p <= 'z') ||
               (*p >= '0' && *p <= '9') || *p == '_')) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, start, (size_t)(p - start));
        (*table_name)[p - start] = '\0';
    }
    return 1;
}

static int parse_update_table(const char* sql, char** table_name) {
    const char* p = sql;
    const char* start;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "UPDATE")) return 0;
    p += 6;
    skip_whitespace(&p);
    if (*p == '\"' || *p == '\'' || *p == '`') {
        char quote = *p;
        p++;
        start = p;
        while (*p && *p != quote) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, start, (size_t)(p - start));
        (*table_name)[p - start] = '\0';
    } else {
        start = p;
        while (*p && ((*p >= 'A' && *p <= 'Z') || (*p >= 'a' && *p <= 'z') ||
               (*p >= '0' && *p <= '9') || *p == '_')) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, start, (size_t)(p - start));
        (*table_name)[p - start] = '\0';
    }
    return 1;
}

static int parse_delete_table(const char* sql, char** table_name) {
    const char* p = sql;
    const char* start;
    const char* from_start;
    skip_whitespace(&p);
    if (!str_startswith_icmp(p, "DELETE")) return 0;
    p += 6;
    skip_whitespace(&p);
    if (str_startswith_icmp(p, "FROM")) {
        p += 4;
        skip_whitespace(&p);
    }
    if (*p == '\"' || *p == '\'' || *p == '`') {
        char quote = *p;
        p++;
        from_start = p;
        while (*p && *p != quote) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - from_start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, from_start, (size_t)(p - from_start));
        (*table_name)[p - from_start] = '\0';
    } else {
        start = p;
        while (*p && ((*p >= 'A' && *p <= 'Z') || (*p >= 'a' && *p <= 'z') ||
               (*p >= '0' && *p <= '9') || *p == '_')) p++;
        *table_name = (char*)kmm_v4_malloc((size_t)(p - start) + 1);
        if (!*table_name) return -1;
        memcpy(*table_name, start, (size_t)(p - start));
        (*table_name)[p - start] = '\0';
    }
    return 1;
}

static int detect_stmt_type(const char* sql) {
    const char* p = sql;
    skip_whitespace(&p);
    if (str_startswith_icmp(p, "SELECT")) return STMT_SELECT;
    if (str_startswith_icmp(p, "INSERT")) return STMT_INSERT;
    if (str_startswith_icmp(p, "UPDATE")) return STMT_UPDATE;
    if (str_startswith_icmp(p, "DELETE")) return STMT_DELETE;
    if (str_startswith_icmp(p, "CREATE")) return STMT_CREATE_TABLE;
    if (str_startswith_icmp(p, "DROP")) return STMT_DROP_TABLE;
    if (str_startswith_icmp(p, "BEGIN")) return STMT_BEGIN;
    if (str_startswith_icmp(p, "COMMIT")) return STMT_COMMIT;
    if (str_startswith_icmp(p, "ROLLBACK")) return STMT_ROLLBACK;
    return 0;
}

static int exec_create_table(DBConnection* db, const char* sql) {
    int r = parse_create_table(db, sql);
    if (r < 0) {
        db_set_error(db, "CREATE TABLE parse error");
        return DB_ERROR;
    }
    return DB_OK;
}

static int exec_drop_table(DBConnection* db, const char* sql) {
    int r = parse_drop_table(db, sql);
    if (r < 0) {
        db_set_error(db, "DROP TABLE error");
        return DB_ERROR;
    }
    return DB_OK;
}

static void split_csv_columns(const char* sql, char*** cols, int* count) {
    const char* p = sql;
    const char* start;
    int cap = DB_INITIAL_CAPACITY;
    int cnt = 0;
    char** result = (char**)kmm_v4_malloc(sizeof(char*) * cap);
    if (!result) { *cols = NULL; *count = 0; return; }
    while (*p) {
        skip_whitespace(&p);
        if (*p == '(' || *p == ')') { p++; continue; }
        if (*p == ',') { p++; continue; }
        if (!*p) break;
        start = p;
        while (*p && *p != ',' && *p != '(' && *p != ')' && *p != ' ' && *p != '\t') {
            p++;
        }
        if (p > start) {
            char* c;
            size_t len = (size_t)(p - start);
            if (cnt >= cap) {
                cap *= 2;
                result = (char**)kmm_v4_malloc(sizeof(char*) * cap);
            }
            c = (char*)kmm_v4_malloc(len + 1);
            if (c) {
                memcpy(c, start, len);
                c[len] = '\0';
                result[cnt++] = c;
            }
        }
        skip_whitespace(&p);
    }
    *cols = result;
    *count = cnt;
}

DBStatus db_exec(DBConnection* db, const char* sql, DBRowCallback callback, void* user_data, char** err_msg) {
    int type;
    char* table_name = NULL;
    MemTable* t;
    int i, j;
    char** col_names_arr = NULL;
    char** col_values_arr = NULL;
    if (!db || !sql) return DB_ERROR;
    (void)callback;
    (void)user_data;
    if (err_msg) *err_msg = NULL;

    type = detect_stmt_type(sql);
    switch (type) {
        case STMT_CREATE_TABLE:
            return exec_create_table(db, sql);
        case STMT_DROP_TABLE:
            return exec_drop_table(db, sql);
        case STMT_BEGIN:
            db->in_transaction = 1;
            return DB_OK;
        case STMT_COMMIT:
            db->in_transaction = 0;
            return DB_OK;
        case STMT_ROLLBACK:
            db->in_transaction = 0;
            return DB_OK;
        case STMT_SELECT: {
            DBStatement* stmt = db_prepare(db, sql);
            DBStatus rc;
            if (!stmt) return DB_ERROR;
            if (callback) {
                col_names_arr = (char**)kmm_v4_malloc(sizeof(char*) * stmt->col_count);
                col_values_arr = (char**)kmm_v4_malloc(sizeof(char*) * stmt->col_count);
                if (col_names_arr && col_values_arr) {
                    for (j = 0; j < stmt->col_count; j++) {
                        col_names_arr[j] = stmt->col_names[j];
                    }
                    while ((rc = db_step(stmt)) == DB_ROW) {
                        for (j = 0; j < stmt->col_count; j++) {
                            col_values_arr[j] = (char*)db_column_text(stmt, j);
                        }
                        callback(user_data, stmt->col_count, col_values_arr, col_names_arr);
                    }
                }
                if (col_names_arr) kmm_v4_free(col_names_arr);
                if (col_values_arr) kmm_v4_free(col_values_arr);
            } else {
                while (db_step(stmt) == DB_ROW) ;
            }
            rc = db_finalize(stmt);
            return rc;
        }
        case STMT_INSERT: {
            DBStatement* stmt = db_prepare(db, sql);
            DBStatus rc;
            if (!stmt) return DB_ERROR;
            rc = db_step(stmt);
            if (rc == DB_ERROR) {
                db_finalize(stmt);
                return DB_ERROR;
            }
            db_finalize(stmt);
            return DB_OK;
        }
        case STMT_UPDATE:
        case STMT_DELETE: {
            DBStatement* stmt = db_prepare(db, sql);
            DBStatus rc;
            if (!stmt) return DB_ERROR;
            rc = db_step(stmt);
            if (rc == DB_ERROR) {
                db_finalize(stmt);
                return DB_ERROR;
            }
            db_finalize(stmt);
            return DB_OK;
        }
        default:
            db_set_error(db, "unsupported SQL");
            return DB_ERROR;
    }
}

static void stmt_reset_result(DBStatement* stmt) {
    if (!stmt) return;
    if (stmt->col_names) {
        int i;
        for (i = 0; i < stmt->col_count; i++) {
            if (stmt->col_names[i]) kmm_v4_free(stmt->col_names[i]);
        }
        kmm_v4_free(stmt->col_names);
        stmt->col_names = NULL;
    }
    if (stmt->col_indices) {
        kmm_v4_free(stmt->col_indices);
        stmt->col_indices = NULL;
    }
    if (stmt->result_rows) {
        kmm_v4_free(stmt->result_rows);
        stmt->result_rows = NULL;
    }
    stmt->col_count = 0;
    stmt->cursor = 0;
    stmt->row_count = 0;
    stmt->result_capacity = 0;
}

static void stmt_free_binds(DBStatement* stmt) {
    int i;
    if (!stmt) return;
    if (stmt->bind_params) {
        for (i = 0; i < stmt->bind_count; i++) {
            if (stmt->bind_params[i].text_val) kmm_v4_free(stmt->bind_params[i].text_val);
            if (stmt->bind_params[i].blob_val) kmm_v4_free(stmt->bind_params[i].blob_val);
        }
        kmm_v4_free(stmt->bind_params);
        stmt->bind_params = NULL;
    }
    stmt->bind_count = 0;
    stmt->bind_capacity = 0;
}

static int prepare_select(DBStatement* stmt, const char* sql) {
    DBConnection* db = stmt->db;
    char* table_name = NULL;
    MemTable* t;
    const char* p;
    const char* sel_start;
    const char* sel_end;
    int i;
    if (parse_select_table(sql, &table_name) <= 0) {
        db_set_error(db, "SELECT parse error");
        return -1;
    }
    t = find_table(db, table_name);
    if (!t) {
        db_set_error(db, "table not found");
        kmm_v4_free(table_name);
        return -1;
    }
    kmm_v4_free(table_name);
    stmt->current_table = t;

    p = sql;
    skip_whitespace(&p);
    p += 6;
    skip_whitespace(&p);
    sel_start = p;
    while (*p) {
        if (str_startswith_icmp(p, "FROM")) break;
        p++;
    }
    sel_end = p;
    while (sel_end > sel_start && (sel_end[-1] == ' ' || sel_end[-1] == '\t')) sel_end--;

    if (sel_end - sel_start == 1 && sel_start[0] == '*') {
        stmt->col_count = t->col_count;
        stmt->col_names = (char**)kmm_v4_malloc(sizeof(char*) * t->col_count);
        stmt->col_indices = (int*)kmm_v4_malloc(sizeof(int) * t->col_count);
        if (!stmt->col_names || !stmt->col_indices) return -1;
        for (i = 0; i < t->col_count; i++) {
            stmt->col_names[i] = str_dup(t->columns[i].name);
            stmt->col_indices[i] = i;
        }
    } else {
        int cap = DB_INITIAL_CAPACITY;
        int cnt = 0;
        char** names = (char**)kmm_v4_malloc(sizeof(char*) * cap);
        int* indices = (int*)kmm_v4_malloc(sizeof(int) * cap);
        const char* cp = sel_start;
        if (!names || !indices) {
            if (names) kmm_v4_free(names);
            if (indices) kmm_v4_free(indices);
            return -1;
        }
        while (cp < sel_end) {
            char* col_name;
            int idx;
            skip_whitespace(&cp);
            if (cp >= sel_end) break;
            if (*cp == ',') { cp++; continue; }
            if (*cp == '*') {
                int j;
                for (j = 0; j < t->col_count; j++) {
                    if (cnt >= cap) {
                        cap *= 2;
                    }
                    names[cnt] = str_dup(t->columns[j].name);
                    indices[cnt] = j;
                    cnt++;
                }
                cp++;
                continue;
            }
            col_name = extract_identifier(&cp);
            if (!col_name) break;
            idx = find_column_index(t, col_name);
            if (idx >= 0) {
                if (cnt >= cap) {
                    cap *= 2;
                }
                names[cnt] = col_name;
                indices[cnt] = idx;
                cnt++;
            } else {
                kmm_v4_free(col_name);
            }
            skip_whitespace(&cp);
        }
        stmt->col_count = cnt;
        stmt->col_names = names;
        stmt->col_indices = indices;
    }

    stmt->result_capacity = t->row_count > 0 ? t->row_count : DB_INITIAL_CAPACITY;
    stmt->result_rows = (int*)kmm_v4_malloc(sizeof(int) * stmt->result_capacity);
    if (!stmt->result_rows) return -1;
    for (i = 0; i < t->row_count; i++) {
        stmt->result_rows[i] = i;
    }
    stmt->row_count = t->row_count;
    stmt->cursor = 0;

    return 0;
}

static int prepare_insert(DBStatement* stmt, const char* sql) {
    DBConnection* db = stmt->db;
    char* table_name = NULL;
    MemTable* t;
    const char* p;
    const char* start;
    int i;
    int* cols = NULL;
    int col_count = 0;
    int cap = DB_INITIAL_CAPACITY;

    if (parse_insert_table(sql, &table_name) <= 0) {
        db_set_error(db, "INSERT parse error");
        return -1;
    }
    t = find_table(db, table_name);
    if (!t) {
        db_set_error(db, "table not found");
        kmm_v4_free(table_name);
        return -1;
    }
    kmm_v4_free(table_name);
    stmt->insert_table = t;

    p = sql;
    skip_whitespace(&p);
    p += 6;
    skip_whitespace(&p);
    if (str_startswith_icmp(p, "INTO")) { p += 4; skip_whitespace(&p); }
    while (*p && *p != '(') p++;
    if (*p == '(') {
        p++;
        cols = (int*)kmm_v4_malloc(sizeof(int) * cap);
        if (!cols) return -1;
        while (*p && *p != ')') {
            char* cname;
            int idx;
            skip_whitespace(&p);
            if (*p == ',') { p++; continue; }
            if (*p == ')') break;
            cname = extract_identifier(&p);
            if (!cname) break;
            idx = find_column_index(t, cname);
            kmm_v4_free(cname);
            if (idx >= 0) {
                if (col_count >= cap) { cap *= 2; }
                cols[col_count++] = idx;
            }
            skip_whitespace(&p);
        }
        if (*p == ')') p++;
    }

    if (col_count == 0) {
        cols = (int*)kmm_v4_malloc(sizeof(int) * t->col_count);
        if (!cols) return -1;
        for (i = 0; i < t->col_count; i++) cols[i] = i;
        col_count = t->col_count;
    }

    stmt->insert_cols = cols;
    stmt->insert_col_count = col_count;
    return 0;
}

static int prepare_update(DBStatement* stmt, const char* sql) {
    DBConnection* db = stmt->db;
    char* table_name = NULL;
    MemTable* t;
    int i;
    (void)sql;
    if (parse_update_table(sql, &table_name) <= 0) {
        db_set_error(db, "UPDATE parse error");
        return -1;
    }
    t = find_table(db, table_name);
    if (!t) {
        db_set_error(db, "table not found");
        kmm_v4_free(table_name);
        return -1;
    }
    kmm_v4_free(table_name);
    stmt->current_table = t;

    stmt->result_capacity = t->row_count > 0 ? t->row_count : DB_INITIAL_CAPACITY;
    stmt->result_rows = (int*)kmm_v4_malloc(sizeof(int) * stmt->result_capacity);
    if (!stmt->result_rows) return -1;
    for (i = 0; i < t->row_count; i++) {
        stmt->result_rows[i] = i;
    }
    stmt->row_count = t->row_count;
    stmt->cursor = 0;
    return 0;
}

static int prepare_delete(DBStatement* stmt, const char* sql) {
    DBConnection* db = stmt->db;
    char* table_name = NULL;
    MemTable* t;
    int i;
    if (parse_delete_table(sql, &table_name) <= 0) {
        db_set_error(db, "DELETE parse error");
        return -1;
    }
    t = find_table(db, table_name);
    if (!t) {
        db_set_error(db, "table not found");
        kmm_v4_free(table_name);
        return -1;
    }
    kmm_v4_free(table_name);
    stmt->current_table = t;

    stmt->result_capacity = t->row_count > 0 ? t->row_count : DB_INITIAL_CAPACITY;
    stmt->result_rows = (int*)kmm_v4_malloc(sizeof(int) * stmt->result_capacity);
    if (!stmt->result_rows) return -1;
    for (i = 0; i < t->row_count; i++) {
        stmt->result_rows[i] = i;
    }
    stmt->row_count = t->row_count;
    stmt->cursor = 0;
    return 0;
}

DBStatement* db_prepare(DBConnection* db, const char* sql) {
    DBStatement* stmt;
    int type;
    if (!db || !sql) return NULL;
    stmt = (DBStatement*)kmm_v4_malloc(sizeof(DBStatement));
    if (!stmt) return NULL;
    memset(stmt, 0, sizeof(DBStatement));
    stmt->db = db;
    stmt->sql = str_dup(sql);
    stmt->current_table = NULL;
    stmt->col_count = 0;
    stmt->col_names = NULL;
    stmt->col_indices = NULL;
    stmt->cursor = 0;
    stmt->row_count = 0;
    stmt->result_rows = NULL;
    stmt->result_capacity = 0;
    stmt->bind_params = NULL;
    stmt->bind_count = 0;
    stmt->bind_capacity = 0;
    stmt->insert_table = NULL;
    stmt->insert_cols = NULL;
    stmt->insert_col_count = 0;

    type = detect_stmt_type(sql);
    stmt->stmt_type = type;

    switch (type) {
        case STMT_SELECT:
            if (prepare_select(stmt, sql) < 0) {
                db_finalize(stmt);
                return NULL;
            }
            break;
        case STMT_INSERT:
            if (prepare_insert(stmt, sql) < 0) {
                db_finalize(stmt);
                return NULL;
            }
            break;
        case STMT_UPDATE:
            if (prepare_update(stmt, sql) < 0) {
                db_finalize(stmt);
                return NULL;
            }
            break;
        case STMT_DELETE:
            if (prepare_delete(stmt, sql) < 0) {
                db_finalize(stmt);
                return NULL;
            }
            break;
        case STMT_CREATE_TABLE:
        case STMT_DROP_TABLE:
        case STMT_BEGIN:
        case STMT_COMMIT:
        case STMT_ROLLBACK:
            break;
        default:
            db_set_error(db, "unsupported statement");
            db_finalize(stmt);
            return NULL;
    }

    return stmt;
}

static int execute_insert_step(DBStatement* stmt) {
    MemTable* t = stmt->insert_table;
    int row_idx;
    int i;
    i64 rowid;
    if (!t) return DB_ERROR;
    rowid = t->next_rowid++;
    row_idx = add_row(t, rowid);
    if (row_idx < 0) return DB_ERROR;
    for (i = 0; i < stmt->insert_col_count && i < stmt->bind_count; i++) {
        int col_idx = stmt->insert_cols[i];
        char* v = get_value_string(&stmt->bind_params[i]);
        if (t->rows[row_idx].values[col_idx]) {
            kmm_v4_free(t->rows[row_idx].values[col_idx]);
        }
        t->rows[row_idx].values[col_idx] = v;
    }
    stmt->db->last_insert_id = rowid;
    stmt->db->changes = 1;
    return DB_DONE;
}

static int execute_delete_step(DBStatement* stmt) {
    MemTable* t = stmt->current_table;
    int changes = 0;
    int i, j;
    if (!t) return DB_ERROR;
    for (i = 0; i < stmt->row_count; i++) {
        int r = stmt->result_rows[i];
        free_row(t, r);
    }
    for (i = 0; i < t->row_count; i++) {
        int found = 0;
        for (j = 0; j < stmt->row_count; j++) {
            if (stmt->result_rows[j] == i) { found = 1; break; }
        }
        if (found) changes++;
    }
    {
        int write_idx = 0;
        for (i = 0; i < t->row_count; i++) {
            int is_deleted = 0;
            for (j = 0; j < stmt->row_count; j++) {
                if (stmt->result_rows[j] == i) { is_deleted = 1; break; }
            }
            if (!is_deleted) {
                if (write_idx != i) {
                    t->rows[write_idx] = t->rows[i];
                }
                write_idx++;
            }
        }
        t->row_count = write_idx;
    }
    stmt->db->changes = changes;
    return DB_DONE;
}

static int execute_update_step(DBStatement* stmt) {
    stmt->db->changes = stmt->row_count;
    return DB_DONE;
}

DBStatus db_step(DBStatement* stmt) {
    if (!stmt) return DB_ERROR;
    switch (stmt->stmt_type) {
        case STMT_SELECT:
            if (stmt->cursor < stmt->row_count) {
                stmt->cursor++;
                return DB_ROW;
            }
            return DB_DONE;
        case STMT_INSERT:
            return execute_insert_step(stmt);
        case STMT_UPDATE:
            return execute_update_step(stmt);
        case STMT_DELETE:
            return execute_delete_step(stmt);
        case STMT_CREATE_TABLE:
            exec_create_table(stmt->db, stmt->sql);
            return DB_DONE;
        case STMT_DROP_TABLE:
            exec_drop_table(stmt->db, stmt->sql);
            return DB_DONE;
        case STMT_BEGIN:
            stmt->db->in_transaction = 1;
            return DB_DONE;
        case STMT_COMMIT:
            stmt->db->in_transaction = 0;
            return DB_DONE;
        case STMT_ROLLBACK:
            stmt->db->in_transaction = 0;
            return DB_DONE;
        default:
            return DB_ERROR;
    }
}

DBStatus db_reset(DBStatement* stmt) {
    if (!stmt) return DB_ERROR;
    stmt->cursor = 0;
    return DB_OK;
}

DBStatus db_finalize(DBStatement* stmt) {
    if (!stmt) return DB_ERROR;
    stmt_reset_result(stmt);
    stmt_free_binds(stmt);
    if (stmt->sql) kmm_v4_free(stmt->sql);
    if (stmt->insert_cols) kmm_v4_free(stmt->insert_cols);
    kmm_v4_free(stmt);
    return DB_OK;
}

int db_column_count(DBStatement* stmt) {
    if (!stmt) return 0;
    return stmt->col_count;
}

const char* db_column_name(DBStatement* stmt, int col) {
    if (!stmt || col < 0 || col >= stmt->col_count) return NULL;
    return stmt->col_names[col];
}

DBColumnType db_column_type(DBStatement* stmt, int col) {
    MemTable* t;
    int idx;
    if (!stmt || !stmt->current_table || col < 0 || col >= stmt->col_count) {
        return DB_TYPE_NULL;
    }
    t = stmt->current_table;
    idx = stmt->col_indices[col];
    if (idx < 0 || idx >= t->col_count) return DB_TYPE_NULL;
    return t->columns[idx].type;
}

static char* get_current_value(DBStatement* stmt, int col) {
    MemTable* t;
    int row_idx;
    int col_idx;
    if (!stmt || !stmt->current_table) return NULL;
    if (stmt->cursor <= 0 || stmt->cursor > stmt->row_count) return NULL;
    if (col < 0 || col >= stmt->col_count) return NULL;
    t = stmt->current_table;
    row_idx = stmt->result_rows[stmt->cursor - 1];
    col_idx = stmt->col_indices[col];
    if (row_idx < 0 || row_idx >= t->row_count) return NULL;
    if (col_idx < 0 || col_idx >= t->col_count) return NULL;
    return t->rows[row_idx].values[col_idx];
}

i64 db_column_int(DBStatement* stmt, int col) {
    char* v = get_current_value(stmt, col);
    if (!v) return 0;
    return (i64)atoll(v);
}

f64 db_column_double(DBStatement* stmt, int col) {
    char* v = get_current_value(stmt, col);
    if (!v) return 0.0;
    return (f64)atof(v);
}

const char* db_column_text(DBStatement* stmt, int col) {
    return get_current_value(stmt, col);
}

const void* db_column_blob(DBStatement* stmt, int col) {
    return get_current_value(stmt, col);
}

int db_column_bytes(DBStatement* stmt, int col) {
    char* v = get_current_value(stmt, col);
    if (!v) return 0;
    return (int)strlen(v);
}

static int ensure_bind_capacity(DBStatement* stmt, int idx) {
    if (!stmt) return -1;
    if (idx >= stmt->bind_capacity) {
        int new_cap = stmt->bind_capacity == 0 ? DB_MAX_BIND_PARAMS : stmt->bind_capacity * 2;
        int i;
        BindParam* np;
        if (idx > DB_MAX_BIND_PARAMS) return -1;
        if (new_cap > DB_MAX_BIND_PARAMS) new_cap = DB_MAX_BIND_PARAMS;
        np = (BindParam*)kmm_v4_malloc(sizeof(BindParam) * new_cap);
        if (!np) return -1;
        memset(np, 0, sizeof(BindParam) * new_cap);
        if (stmt->bind_params && stmt->bind_count > 0) {
            for (i = 0; i < stmt->bind_count; i++) {
                np[i] = stmt->bind_params[i];
            }
        }
        if (stmt->bind_params) kmm_v4_free(stmt->bind_params);
        stmt->bind_params = np;
        stmt->bind_capacity = new_cap;
    }
    if (idx >= stmt->bind_count) {
        stmt->bind_count = idx + 1;
    }
    return 0;
}

DBStatus db_bind_int(DBStatement* stmt, int idx, i64 value) {
    if (!stmt || idx < 1) return DB_ERROR;
    idx--;
    if (ensure_bind_capacity(stmt, idx) < 0) return DB_ERROR;
    stmt->bind_params[idx].type = DB_TYPE_INTEGER;
    stmt->bind_params[idx].int_val = value;
    stmt->bind_params[idx].is_null = 0;
    return DB_OK;
}

DBStatus db_bind_double(DBStatement* stmt, int idx, f64 value) {
    if (!stmt || idx < 1) return DB_ERROR;
    idx--;
    if (ensure_bind_capacity(stmt, idx) < 0) return DB_ERROR;
    stmt->bind_params[idx].type = DB_TYPE_FLOAT;
    stmt->bind_params[idx].float_val = value;
    stmt->bind_params[idx].is_null = 0;
    return DB_OK;
}

DBStatus db_bind_text(DBStatement* stmt, int idx, const char* value) {
    if (!stmt || idx < 1) return DB_ERROR;
    idx--;
    if (ensure_bind_capacity(stmt, idx) < 0) return DB_ERROR;
    if (stmt->bind_params[idx].text_val) {
        kmm_v4_free(stmt->bind_params[idx].text_val);
        stmt->bind_params[idx].text_val = NULL;
    }
    if (!value) {
        stmt->bind_params[idx].is_null = 1;
        stmt->bind_params[idx].type = DB_TYPE_NULL;
    } else {
        stmt->bind_params[idx].text_val = str_dup(value);
        stmt->bind_params[idx].type = DB_TYPE_TEXT;
        stmt->bind_params[idx].is_null = 0;
    }
    return DB_OK;
}

DBStatus db_bind_blob(DBStatement* stmt, int idx, const void* value, int size) {
    if (!stmt || idx < 1) return DB_ERROR;
    idx--;
    if (ensure_bind_capacity(stmt, idx) < 0) return DB_ERROR;
    if (stmt->bind_params[idx].blob_val) {
        kmm_v4_free(stmt->bind_params[idx].blob_val);
        stmt->bind_params[idx].blob_val = NULL;
    }
    if (!value || size <= 0) {
        stmt->bind_params[idx].is_null = 1;
        stmt->bind_params[idx].type = DB_TYPE_NULL;
        stmt->bind_params[idx].blob_size = 0;
    } else {
        stmt->bind_params[idx].blob_val = kmm_v4_malloc(size);
        if (!stmt->bind_params[idx].blob_val) return DB_ERROR;
        memcpy(stmt->bind_params[idx].blob_val, value, size);
        stmt->bind_params[idx].blob_size = size;
        stmt->bind_params[idx].type = DB_TYPE_BLOB;
        stmt->bind_params[idx].is_null = 0;
    }
    return DB_OK;
}

DBStatus db_bind_null(DBStatement* stmt, int idx) {
    if (!stmt || idx < 1) return DB_ERROR;
    idx--;
    if (ensure_bind_capacity(stmt, idx) < 0) return DB_ERROR;
    if (stmt->bind_params[idx].text_val) {
        kmm_v4_free(stmt->bind_params[idx].text_val);
        stmt->bind_params[idx].text_val = NULL;
    }
    if (stmt->bind_params[idx].blob_val) {
        kmm_v4_free(stmt->bind_params[idx].blob_val);
        stmt->bind_params[idx].blob_val = NULL;
    }
    stmt->bind_params[idx].type = DB_TYPE_NULL;
    stmt->bind_params[idx].is_null = 1;
    return DB_OK;
}

i64 db_last_insert_id(DBConnection* db) {
    if (!db) return 0;
    return db->last_insert_id;
}

int db_changes(DBConnection* db) {
    if (!db) return 0;
    return db->changes;
}

const char* db_errmsg(DBConnection* db) {
    if (!db) return NULL;
    return db->errmsg;
}

int db_errcode(DBConnection* db) {
    if (!db) return 0;
    return 0;
}

DBResult* db_query(DBConnection* db, const char* sql, ...) {
    DBResult* result;
    DBStatement* stmt;
    int col_count;
    int row_count = 0;
    char** data = NULL;
    char** col_names = NULL;
    int i, j;
    DBStatus rc;
    if (!db || !sql) return NULL;

    stmt = db_prepare(db, sql);
    if (!stmt) return NULL;

    col_count = stmt->col_count;
    if (col_count <= 0) {
        db_finalize(stmt);
        result = (DBResult*)kmm_v4_malloc(sizeof(DBResult));
        if (!result) return NULL;
        memset(result, 0, sizeof(DBResult));
        return result;
    }

    col_names = (char**)kmm_v4_malloc(sizeof(char*) * col_count);
    if (!col_names) {
        db_finalize(stmt);
        return NULL;
    }
    for (i = 0; i < col_count; i++) {
        const char* n = db_column_name(stmt, i);
        col_names[i] = n ? str_dup(n) : NULL;
    }

    row_count = 0;
    data = NULL;

    while ((rc = db_step(stmt)) == DB_ROW) {
        char** new_data;
        new_data = (char**)kmm_v4_malloc(sizeof(char*) * (row_count + 1) * col_count);
        if (!new_data) {
            if (data) {
                for (i = 0; i < row_count * col_count; i++) {
                    if (data[i]) kmm_v4_free(data[i]);
                }
                kmm_v4_free(data);
            }
            for (i = 0; i < col_count; i++) {
                if (col_names[i]) kmm_v4_free(col_names[i]);
            }
            kmm_v4_free(col_names);
            db_finalize(stmt);
            return NULL;
        }
        if (data && row_count > 0) {
            memcpy(new_data, data, sizeof(char*) * row_count * col_count);
            kmm_v4_free(data);
        }
        data = new_data;
        for (j = 0; j < col_count; j++) {
            const char* v = db_column_text(stmt, j);
            int idx = row_count * col_count + j;
            if (v) {
                data[idx] = str_dup(v);
            } else {
                data[idx] = NULL;
            }
        }
        row_count++;
    }

    db_finalize(stmt);

    result = (DBResult*)kmm_v4_malloc(sizeof(DBResult));
    if (!result) {
        if (data) {
            for (i = 0; i < row_count * col_count; i++) {
                if (data[i]) kmm_v4_free(data[i]);
            }
            kmm_v4_free(data);
        }
        for (i = 0; i < col_count; i++) {
            if (col_names[i]) kmm_v4_free(col_names[i]);
        }
        kmm_v4_free(col_names);
        return NULL;
    }
    result->rows = row_count;
    result->cols = col_count;
    result->data = data;
    result->col_names = col_names;
    return result;
}

void db_result_free(DBResult* result) {
    int i;
    if (!result) return;
    if (result->data) {
        for (i = 0; i < result->rows * result->cols; i++) {
            if (result->data[i]) kmm_v4_free(result->data[i]);
        }
        kmm_v4_free(result->data);
    }
    if (result->col_names) {
        for (i = 0; i < result->cols; i++) {
            if (result->col_names[i]) kmm_v4_free(result->col_names[i]);
        }
        kmm_v4_free(result->col_names);
    }
    kmm_v4_free(result);
}

int db_result_rows(DBResult* result) {
    if (!result) return 0;
    return result->rows;
}

int db_result_cols(DBResult* result) {
    if (!result) return 0;
    return result->cols;
}

const char* db_result_value(DBResult* result, int row, int col) {
    int idx;
    if (!result || row < 0 || row >= result->rows || col < 0 || col >= result->cols) {
        return NULL;
    }
    idx = row * result->cols + col;
    return result->data[idx];
}

const char* db_result_column_name(DBResult* result, int col) {
    if (!result || col < 0 || col >= result->cols) return NULL;
    return result->col_names[col];
}

bool_t db_table_exists(DBConnection* db, const char* table_name) {
    if (!db || !table_name) return 0;
    return find_table(db, table_name) != NULL;
}

DBStatus db_begin_transaction(DBConnection* db) {
    if (!db) return DB_ERROR;
    db->in_transaction = 1;
    return DB_OK;
}

DBStatus db_commit(DBConnection* db) {
    if (!db) return DB_ERROR;
    db->in_transaction = 0;
    return DB_OK;
}

DBStatus db_rollback(DBConnection* db) {
    if (!db) return DB_ERROR;
    db->in_transaction = 0;
    return DB_OK;
}

const char* db_backend_name(void) {
    return "MemoryDB";
}

int db_backend_version(void) {
    return 10000;
}

#endif
