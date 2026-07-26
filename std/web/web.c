#include "web.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <time.h>

#if STD_PLATFORM_WINDOWS
    #include <windows.h>
    #define CLOSE_SOCKET closesocket
#else
    #include <errno.h>
    #include <unistd.h>
    #include <fcntl.h>
    #include <sys/socket.h>
    #define CLOSE_SOCKET close
    #define SOCKET_LAST_ERROR errno
    #define SOCKET_EWOULDBLOCK EWOULDBLOCK
    typedef int SOCKET;
#endif

#define HTTP_BUFFER_SIZE 8192
#define HTTP_MAX_HEADERS 64
#define HTTP_HEADER_BUFFER_SIZE 4096

static bool ws_initialized = false;

static void ws_init(void) {
    if (ws_initialized) return;
#if STD_PLATFORM_WINDOWS
    WSADATA wsa_data;
    WSAStartup(MAKEWORD(2, 2), &wsa_data);
#endif
    ws_initialized = true;
}

static void ws_cleanup(void) {
    if (!ws_initialized) return;
#if STD_PLATFORM_WINDOWS
    WSACleanup();
#endif
    ws_initialized = false;
}

static void set_socket_nonblocking(SOCKET sock) {
#if STD_PLATFORM_WINDOWS
    u_long mode = 1;
    ioctlsocket(sock, FIONBIO, &mode);
#else
    int flags = fcntl(sock, F_GETFL, 0);
    fcntl(sock, F_SETFL, flags | O_NONBLOCK);
#endif
}

static void set_socket_blocking(SOCKET sock) {
#if STD_PLATFORM_WINDOWS
    u_long mode = 0;
    ioctlsocket(sock, FIONBIO, &mode);
#else
    int flags = fcntl(sock, F_GETFL, 0);
    fcntl(sock, F_SETFL, flags & ~O_NONBLOCK);
#endif
}

static bool socket_write(SOCKET sock, const char* data, size_t len) {
    size_t sent = 0;
    while (sent < len) {
#if STD_PLATFORM_WINDOWS
        int result = send(sock, data + sent, (int)(len - sent), 0);
#else
        int result = send(sock, data + sent, len - sent, 0);
#endif
        if (result < 0) {
#if STD_PLATFORM_WINDOWS
            if (WSAGetLastError() == WSAEWOULDBLOCK) {
#else
            if (errno == EWOULDBLOCK || errno == EAGAIN) {
#endif
                continue;
            }
            return false;
        }
        sent += result;
    }
    return true;
}

static char* socket_read(SOCKET sock, size_t* out_len) {
    char* buffer = (char*)kmm_v4_malloc(HTTP_BUFFER_SIZE);
    if (!buffer) return NULL;
    size_t total = 0;
    size_t capacity = HTTP_BUFFER_SIZE;

    while (1) {
        if (total >= capacity) {
            capacity *= 2;
            char* new_buffer = (char*)kmm_v4_realloc(buffer, capacity);
            if (!new_buffer) {
                return NULL;
            }
            buffer = new_buffer;
        }

#if STD_PLATFORM_WINDOWS
        int result = recv(sock, buffer + total, (int)(capacity - total - 1), 0);
#else
        int result = recv(sock, buffer + total, capacity - total - 1, 0);
#endif
        if (result == 0) break;
        if (result < 0) {
#if STD_PLATFORM_WINDOWS
            if (WSAGetLastError() == WSAEWOULDBLOCK) {
#else
            if (errno == EWOULDBLOCK || errno == EAGAIN) {
#endif
                continue;
            }
            return NULL;
        }
        total += result;
    }

    buffer[total] = '\0';
    if (out_len) *out_len = total;
    return buffer;
}

// ==================== HTTP服务器实现 ====================

HttpServer* http_server_create(i32 port) {
    ws_init();
    HttpServer* server = (HttpServer*)kmm_v4_calloc(1, sizeof(HttpServer));
    if (!server) return NULL;

    server->socket = socket(AF_INET, SOCK_STREAM, 0);
    if (server->socket == INVALID_SOCKET) {
        return NULL;
    }

    int opt = 1;
#if STD_PLATFORM_WINDOWS
    setsockopt(server->socket, SOL_SOCKET, SO_REUSEADDR, (const char*)&opt, sizeof(opt));
#else
    setsockopt(server->socket, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
#endif

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons((unsigned short)port);

    if (bind(server->socket, (struct sockaddr*)&addr, sizeof(addr)) == SOCKET_ERROR) {
        CLOSE_SOCKET(server->socket);
        return NULL;
    }

    if (listen(server->socket, 10) == SOCKET_ERROR) {
        CLOSE_SOCKET(server->socket);
        return NULL;
    }

    server->port = port;
    server->running = false;
    server->document_root = NULL;
    return server;
}

bool http_server_set_document_root(HttpServer* server, const char* root) {
    if (!server) return false;
    server->document_root = kmm_v4_strdup(root);
    return true;
}

void http_server_enable_cors(HttpServer* server, const char* allowed_origin) {
    // CORS会在响应构建时处理
}

static void build_error_response(HttpResponse* res, HttpStatusCode code, const char* message) {
    http_response_set_status(res, code);
    http_response_set_content_type(res, MIME_TEXT_HTML);

    char body[512];
    snprintf(body, sizeof(body),
        "<html><head><title>%d %s</title></head><body><h1>%d %s</h1></body></html>",
        code, message, code, message);
    http_response_set_body(res, body, strlen(body));
}

bool http_server_start(HttpServer* server, HttpRequestHandler handler) {
    if (!server || !handler) return false;
    server->running = true;

    while (server->running) {
        struct sockaddr_in client_addr;
        socklen_t client_len = sizeof(client_addr);
        WebSocket client_sock = accept(server->socket, (struct sockaddr*)&client_addr, &client_len);

        if (client_sock == INVALID_SOCKET) {
            if (!server->running) break;
            continue;
        }

        size_t req_len = 0;
        char* raw_request = socket_read(client_sock, &req_len);

        if (raw_request) {
            HttpRequest* req = http_request_parse(raw_request, req_len);
            HttpResponse* res = http_response_create();

            if (req && res) {
                // 默认CORS头
                http_response_set_header(res, "Access-Control-Allow-Origin", "*");
                http_response_set_header(res, "Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS");
                http_response_set_header(res, "Access-Control-Allow-Headers", "Content-Type");

                handler(req, res);

                char* response_str = http_response_to_string(res);
                if (response_str) {
                    socket_write(client_sock, response_str, strlen(response_str));
                }

                http_response_destroy(res);
                http_request_destroy(req);
            }
        }

        CLOSE_SOCKET(client_sock);
    }

    return true;
}

bool http_server_stop(HttpServer* server) {
    if (!server) return false;
    server->running = false;
    return true;
}

void http_server_destroy(HttpServer* server) {
    if (!server) return;
    if (server->socket != INVALID_SOCKET) {
        CLOSE_SOCKET(server->socket);
    }
}

// ==================== HTTP请求解析实现 ====================

HttpRequest* http_request_parse(const char* raw_request, size_t length) {
    if (!raw_request || length == 0) return NULL;

    HttpRequest* req = (HttpRequest*)kmm_v4_calloc(1, sizeof(HttpRequest));
    if (!req) return NULL;

    // 解析请求行
    const char* line_end = strstr(raw_request, "\r\n");
    if (!line_end) {
        return NULL;
    }

    size_t request_line_len = line_end - raw_request;
    char* request_line = (char*)kmm_v4_malloc(request_line_len + 1);
    if (!request_line) {
        return NULL;
    }
    memcpy(request_line, raw_request, request_line_len);
    request_line[request_line_len] = '\0';

    // 解析方法
    if (strncmp(request_line, "GET ", 4) == 0) {
        req->method = HTTP_GET;
    } else if (strncmp(request_line, "POST ", 5) == 0) {
        req->method = HTTP_POST;
    } else if (strncmp(request_line, "PUT ", 4) == 0) {
        req->method = HTTP_PUT;
    } else if (strncmp(request_line, "DELETE ", 7) == 0) {
        req->method = HTTP_DELETE;
    } else if (strncmp(request_line, "PATCH ", 6) == 0) {
        req->method = HTTP_PATCH;
    } else if (strncmp(request_line, "HEAD ", 5) == 0) {
        req->method = HTTP_HEAD;
    } else if (strncmp(request_line, "OPTIONS ", 8) == 0) {
        req->method = HTTP_OPTIONS;
    }

    // 解析路径
    char* path_start = strchr(request_line, ' ');
    if (path_start) {
        path_start++;
        char* path_end = strchr(path_start, ' ');
        if (path_end) {
            size_t path_len = path_end - path_start;
            req->path = (char*)kmm_v4_malloc(path_len + 1);
            if (req->path) {
                memcpy(req->path, path_start, path_len);
                req->path[path_len] = '\0';

                // 解析query string
                char* query = strchr(req->path, '?');
                if (query) {
                    *query = '\0';
                    req->query_string = kmm_v4_strdup(query + 1);
                }
            }
        }
    }

    // 查找body开始位置
    const char* header_end = strstr(raw_request, "\r\n\r\n");
    if (header_end) {
        const char* body_start = header_end + 4;
        req->body_length = length - (body_start - raw_request);
        if (req->body_length > 0) {
            req->body = (char*)kmm_v4_malloc(req->body_length + 1);
            if (req->body) {
                memcpy(req->body, body_start, req->body_length);
                req->body[req->body_length] = '\0';
            }
        }
    }

    return req;
}

void http_request_destroy(HttpRequest* req) {
    // KMM 管理内存，无需手动释放
}

const char* http_request_get_header(HttpRequest* req, const char* name) {
    if (!req || !name || !req->headers) return NULL;
    char* headers_copy = req->headers;
    char* line = headers_copy;
    while (line && *line) {
        char* colon = strchr(line, ':');
        if (colon) {
            size_t name_len = colon - line;
            if (name_len == strlen(name) && strncmp(line, name, name_len) == 0) {
                char* value = colon + 1;
                while (*value == ' ' || *value == '\t') value++;
                char* end = strchr(value, '\r');
                if (!end) end = strchr(value, '\n');
                if (end) {
                    static char header_value[HTTP_HEADER_BUFFER_SIZE];
                    size_t val_len = end - value;
                    if (val_len >= HTTP_HEADER_BUFFER_SIZE) val_len = HTTP_HEADER_BUFFER_SIZE - 1;
                    memcpy(header_value, value, val_len);
                    header_value[val_len] = '\0';
                    return header_value;
                }
            }
        }
        char* next = strchr(line, '\n');
        if (!next) break;
        line = next + 1;
    }
    return NULL;
}

const char* http_request_get_query_param(HttpRequest* req, const char* name) {
    if (!req || !name || !req->query_string) return NULL;
    char* query = req->query_string;
    while (query && *query) {
        char* eq = strchr(query, '=');
        if (eq) {
            size_t param_name_len = eq - query;
            if (param_name_len == strlen(name) && strncmp(query, name, param_name_len) == 0) {
                char* value = eq + 1;
                static char param_value[HTTP_HEADER_BUFFER_SIZE];
                char* amp = strchr(value, '&');
                size_t val_len = amp ? (size_t)(amp - value) : strlen(value);
                if (val_len >= HTTP_HEADER_BUFFER_SIZE) val_len = HTTP_HEADER_BUFFER_SIZE - 1;
                memcpy(param_value, value, val_len);
                param_value[val_len] = '\0';
                return param_value;
            }
        }
        char* amp = strchr(query, '&');
        if (!amp) break;
        query = amp + 1;
    }
    return NULL;
}

// ==================== HTTP响应构建实现 ====================

HttpResponse* http_response_create(void) {
    HttpResponse* res = (HttpResponse*)kmm_v4_calloc(1, sizeof(HttpResponse));
    if (!res) return NULL;
    res->status_code = HTTP_OK;
    res->status_message = "OK";
    return res;
}

void http_response_destroy(HttpResponse* res) {
    // KMM 管理内存，无需手动释放
}

void http_response_set_status(HttpResponse* res, HttpStatusCode code) {
    if (!res) return;
    res->status_code = code;
    switch (code) {
        case HTTP_OK: res->status_message = kmm_v4_strdup("OK"); break;
        case HTTP_CREATED: res->status_message = kmm_v4_strdup("Created"); break;
        case HTTP_NO_CONTENT: res->status_message = kmm_v4_strdup("No Content"); break;
        case HTTP_FOUND: res->status_message = kmm_v4_strdup("Found"); break;
        case HTTP_BAD_REQUEST: res->status_message = kmm_v4_strdup("Bad Request"); break;
        case HTTP_UNAUTHORIZED: res->status_message = kmm_v4_strdup("Unauthorized"); break;
        case HTTP_FORBIDDEN: res->status_message = kmm_v4_strdup("Forbidden"); break;
        case HTTP_NOT_FOUND: res->status_message = kmm_v4_strdup("Not Found"); break;
        case HTTP_METHOD_NOT_ALLOWED: res->status_message = kmm_v4_strdup("Method Not Allowed"); break;
        case HTTP_INTERNAL_SERVER_ERROR: res->status_message = kmm_v4_strdup("Internal Server Error"); break;
        case HTTP_BAD_GATEWAY: res->status_message = kmm_v4_strdup("Bad Gateway"); break;
        case HTTP_SERVICE_UNAVAILABLE: res->status_message = kmm_v4_strdup("Service Unavailable"); break;
        default: res->status_message = kmm_v4_strdup("Unknown"); break;
    }
}

void http_response_set_body(HttpResponse* res, const char* body, size_t length) {
    if (!res) return;
    res->body = (char*)kmm_v4_malloc(length + 1);
    if (res->body) {
        memcpy(res->body, body, length);
        res->body[length] = '\0';
        res->body_length = length;
    }
}

void http_response_set_content_type(HttpResponse* res, const char* content_type) {
    if (!res) return;
    res->content_type = kmm_v4_strdup(content_type);
}

void http_response_set_header(HttpResponse* res, const char* name, const char* value) {
    if (!res || !name || !value) return;

    size_t name_len = strlen(name);
    size_t value_len = strlen(value);
    size_t entry_len = name_len + 2 + value_len + 2;

    char* new_entry = (char*)kmm_v4_malloc(entry_len);
    if (!new_entry) return;

    snprintf(new_entry, entry_len, "%s: %s\r\n", name, value);

    if (res->headers) {
        size_t old_len = strlen(res->headers);
        size_t new_len = old_len + entry_len;
        char* new_headers = (char*)kmm_v4_realloc(res->headers, new_len + 1);
        if (new_headers) {
            strcat(new_headers, new_entry);
            res->headers = new_headers;
        }
    } else {
        res->headers = new_entry;
    }
}

void http_response_set_json(HttpResponse* res, const char* json_string) {
    if (!res || !json_string) return;
    http_response_set_content_type(res, MIME_APP_JSON);
    http_response_set_body(res, json_string, strlen(json_string));
}

void http_response_set_html(HttpResponse* res, const char* html) {
    if (!res || !html) return;
    http_response_set_content_type(res, MIME_TEXT_HTML);
    http_response_set_body(res, html, strlen(html));
}

void http_response_set_redirect(HttpResponse* res, const char* location) {
    if (!res || !location) return;
    http_response_set_status(res, HTTP_FOUND);
    http_response_set_header(res, "Location", location);
}

char* http_response_to_string(HttpResponse* res) {
    if (!res) return NULL;

    char* buffer = (char*)kmm_v4_malloc(16384);
    if (!buffer) return NULL;

    int offset = snprintf(buffer, 1024, "HTTP/1.1 %d %s\r\n",
        res->status_code, res->status_message ? res->status_message : "OK");

    if (res->headers && strlen(res->headers) > 0) {
        int headers_offset = snprintf(buffer + offset, 1024 - offset, "%s", res->headers);
        if (headers_offset > 0) {
            offset += headers_offset;
        }
    }

    if (res->content_type) {
        offset += snprintf(buffer + offset, 1024 - (offset > 0 ? offset : 0), "Content-Type: %s\r\n", res->content_type);
    }

    if (res->body && res->body_length > 0) {
        offset += snprintf(buffer + offset, 1024 - (offset > 0 ? offset : 0), "Content-Length: %zu\r\n", res->body_length);
    }

    offset += snprintf(buffer + offset, 1024 - (offset > 0 ? offset : 0), "\r\n");

    if (res->body && res->body_length > 0 && offset < 16384) {
        memcpy(buffer + offset, res->body, res->body_length);
        offset += res->body_length;
    }

    buffer[offset] = '\0';
    return buffer;
}

// ==================== HTTP客户端实现 ====================

HttpClient* http_client_create(void) {
    ws_init();
    HttpClient* client = (HttpClient*)kmm_v4_calloc(1, sizeof(HttpClient));
    if (!client) return NULL;
    client->socket = INVALID_SOCKET;
    client->timeout_ms = 30000;
    return client;
}

void http_client_destroy(HttpClient* client) {
    if (!client) return;
    if (client->socket != INVALID_SOCKET) {
        CLOSE_SOCKET(client->socket);
    }
}

bool http_client_set_timeout(HttpClient* client, i32 timeout_ms) {
    if (!client) return false;
    client->timeout_ms = timeout_ms;
    return true;
}

static bool client_connect(HttpClient* client, const char* host, i32 port) {
    if (!client || !host) return false;

    if (client->socket != INVALID_SOCKET) {
        CLOSE_SOCKET(client->socket);
    }

    client->socket = socket(AF_INET, SOCK_STREAM, 0);
    if (client->socket == INVALID_SOCKET) return false;

    struct hostent* server = gethostbyname(host);
    if (!server) return false;

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    memcpy(&addr.sin_addr.s_addr, server->h_addr_list[0], server->h_length);
    addr.sin_port = htons((unsigned short)port);

    if (connect(client->socket, (struct sockaddr*)&addr, sizeof(addr)) == SOCKET_ERROR) {
        CLOSE_SOCKET(client->socket);
        client->socket = INVALID_SOCKET;
        return false;
    }

    return true;
}

HttpResponse* http_client_get(HttpClient* client, const char* url) {
    if (!client || !url) return NULL;

    UrlParts* parts = url_parse(url);
    if (!parts) return NULL;

    if (!client_connect(client, parts->host, parts->port)) {
        url_destroy(parts);
        return NULL;
    }

    char request[1024];
    snprintf(request, sizeof(request), "GET /%s%s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
        parts->path ? parts->path : "",
        parts->query ? parts->query : "",
        parts->host);

    if (!socket_write(client->socket, request, strlen(request))) {
        CLOSE_SOCKET(client->socket);
        client->socket = INVALID_SOCKET;
        url_destroy(parts);
        return NULL;
    }

    size_t resp_len = 0;
    char* raw_response = socket_read(client->socket, &resp_len);
    CLOSE_SOCKET(client->socket);
    client->socket = INVALID_SOCKET;

    url_destroy(parts);

    if (!raw_response) return NULL;

    HttpResponse* res = http_response_create();
    if (strncmp(raw_response, "HTTP/1.1 200", 12) == 0) {
        res->status_code = HTTP_OK;
    } else {
        res->status_code = HTTP_BAD_REQUEST;
    }

    const char* body = strstr(raw_response, "\r\n\r\n");
    if (body) {
        body += 4;
        size_t body_len = resp_len - (body - raw_response);
        res->body = (char*)kmm_v4_malloc(body_len + 1);
        if (res->body) {
            memcpy(res->body, body, body_len);
            res->body[body_len] = '\0';
            res->body_length = body_len;
        }
    }

    return res;
}

HttpResponse* http_client_post(HttpClient* client, const char* url, const char* body, const char* content_type) {
    if (!client || !url) return NULL;

    UrlParts* parts = url_parse(url);
    if (!parts) return NULL;

    if (!client_connect(client, parts->host, parts->port)) {
        url_destroy(parts);
        return NULL;
    }

    size_t body_len = body ? strlen(body) : 0;

    char request[8192];
    snprintf(request, sizeof(request),
        "POST /%s%s HTTP/1.1\r\nHost: %s\r\nContent-Type: %s\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n",
        parts->path ? parts->path : "",
        parts->query ? parts->query : "",
        parts->host,
        content_type ? content_type : MIME_APP_FORM,
        body_len);

    size_t header_len = strlen(request);
    if (!socket_write(client->socket, request, header_len)) {
        CLOSE_SOCKET(client->socket);
        client->socket = INVALID_SOCKET;
        url_destroy(parts);
        return NULL;
    }

    if (body && body_len > 0) {
        if (!socket_write(client->socket, body, body_len)) {
            CLOSE_SOCKET(client->socket);
            client->socket = INVALID_SOCKET;
            url_destroy(parts);
            return NULL;
        }
    }

    size_t resp_len = 0;
    char* raw_response = socket_read(client->socket, &resp_len);
    CLOSE_SOCKET(client->socket);
    client->socket = INVALID_SOCKET;

    url_destroy(parts);

    if (!raw_response) return NULL;

    HttpResponse* res = http_response_create();
    res->status_code = HTTP_OK;

    const char* resp_body = strstr(raw_response, "\r\n\r\n");
    if (resp_body) {
        resp_body += 4;
        size_t resp_body_len = resp_len - (resp_body - raw_response);
        res->body = (char*)kmm_v4_malloc(resp_body_len + 1);
        if (res->body) {
            memcpy(res->body, resp_body, resp_body_len);
            res->body[resp_body_len] = '\0';
            res->body_length = resp_body_len;
        }
    }

    return res;
}

HttpResponse* http_client_put(HttpClient* client, const char* url, const char* body, const char* content_type) {
    if (!client || !url) return NULL;

    UrlParts* parts = url_parse(url);
    if (!parts) return NULL;

    if (!client_connect(client, parts->host, parts->port)) {
        url_destroy(parts);
        return NULL;
    }

    size_t body_len = body ? strlen(body) : 0;

    char request[8192];
    snprintf(request, sizeof(request),
        "PUT /%s%s HTTP/1.1\r\nHost: %s\r\nContent-Type: %s\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n",
        parts->path ? parts->path : "",
        parts->query ? parts->query : "",
        parts->host,
        content_type ? content_type : MIME_APP_FORM,
        body_len);

    size_t header_len = strlen(request);
    socket_write(client->socket, request, header_len);

    if (body && body_len > 0) {
        socket_write(client->socket, body, body_len);
    }

    size_t resp_len = 0;
    char* raw_response = socket_read(client->socket, &resp_len);
    CLOSE_SOCKET(client->socket);
    client->socket = INVALID_SOCKET;

    url_destroy(parts);

    if (!raw_response) return NULL;

    HttpResponse* res = http_response_create();
    res->status_code = HTTP_OK;

    const char* resp_body = strstr(raw_response, "\r\n\r\n");
    if (resp_body) {
        resp_body += 4;
        size_t resp_body_len = resp_len - (resp_body - raw_response);
        res->body = (char*)kmm_v4_malloc(resp_body_len + 1);
        if (res->body) {
            memcpy(res->body, resp_body, resp_body_len);
            res->body[resp_body_len] = '\0';
            res->body_length = resp_body_len;
        }
    }

    return res;
}

HttpResponse* http_client_delete(HttpClient* client, const char* url) {
    if (!client || !url) return NULL;

    UrlParts* parts = url_parse(url);
    if (!parts) return NULL;

    if (!client_connect(client, parts->host, parts->port)) {
        url_destroy(parts);
        return NULL;
    }

    char request[1024];
    snprintf(request, sizeof(request), "DELETE /%s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n",
        parts->path ? parts->path : "", parts->host);

    socket_write(client->socket, request, strlen(request));

    size_t resp_len = 0;
    char* raw_response = socket_read(client->socket, &resp_len);
    CLOSE_SOCKET(client->socket);
    client->socket = INVALID_SOCKET;

    url_destroy(parts);

    if (!raw_response) return NULL;

    HttpResponse* res = http_response_create();
    res->status_code = HTTP_OK;

    const char* resp_body = strstr(raw_response, "\r\n\r\n");
    if (resp_body) {
        resp_body += 4;
        size_t resp_body_len = resp_len - (resp_body - raw_response);
        res->body = (char*)kmm_v4_malloc(resp_body_len + 1);
        if (res->body) {
            memcpy(res->body, resp_body, resp_body_len);
            res->body[resp_body_len] = '\0';
            res->body_length = resp_body_len;
        }
    }

    return res;
}

void http_response_print(HttpResponse* res) {
    if (!res) return;
    printf("Status: %d %s\n", res->status_code, res->status_message ? res->status_message : "");
    if (res->body && res->body_length > 0) {
        printf("Body (%zu bytes):\n%s\n", res->body_length, res->body);
    }
}

// ==================== URL解析实现 ====================

UrlParts* url_parse(const char* url) {
    if (!url) return NULL;

    UrlParts* parts = (UrlParts*)kmm_v4_calloc(1, sizeof(UrlParts));
    if (!parts) return NULL;

    // 简单解析: http://host:port/path?query
    const char* p = url;

    // 跳过scheme
    const char* scheme_end = strstr(p, "://");
    if (scheme_end) {
        size_t scheme_len = scheme_end - p;
        parts->scheme = (char*)kmm_v4_malloc(scheme_len + 1);
        if (parts->scheme) {
            memcpy(parts->scheme, p, scheme_len);
            parts->scheme[scheme_len] = '\0';
        }
        p = scheme_end + 3;
    }

    // 解析host:port
    const char* path_start = strchr(p, '/');
    const char* query_start = strchr(p, '?');

    const char* host_end = path_start;
    if (query_start && (!path_start || query_start < path_start)) {
        host_end = query_start;
    }

    if (host_end) {
        size_t host_len = host_end - p;
        char* host_port = (char*)kmm_v4_malloc(host_len + 1);
        if (host_port) {
            memcpy(host_port, p, host_len);
            host_port[host_len] = '\0';

            // 分离host和port
            char* colon = strchr(host_port, ':');
            if (colon) {
                *colon = '\0';
                parts->host = kmm_v4_strdup(host_port);
                parts->port = atoi(colon + 1);
            } else {
                parts->host = kmm_v4_strdup(host_port);
                parts->port = 80;
            }
        }
        p = host_end;
    } else {
        parts->host = kmm_v4_strdup(p);
        parts->port = 80;
    }

    // 解析path
    if (path_start) {
        const char* q = query_start ? query_start : p + strlen(p);
        size_t path_len = q - path_start;
        parts->path = (char*)kmm_v4_malloc(path_len + 1);
        if (parts->path) {
            memcpy(parts->path, path_start, path_len);
            parts->path[path_len] = '\0';
        }
    } else {
        parts->path = kmm_v4_strdup("/");
    }

    // 解析query
    if (query_start) {
        parts->query = kmm_v4_strdup(query_start + 1);
    }

    return parts;
}

void url_destroy(UrlParts* parts) {
    // KMM 管理内存，无需手动释放
}

char* url_encode(const char* str) {
    if (!str) return NULL;
    char* result = (char*)kmm_v4_malloc(strlen(str) * 3 + 1);
    if (!result) return NULL;

    const char* p = str;
    char* q = result;
    while (*p) {
        if ((*p >= 'a' && *p <= 'z') || (*p >= 'A' && *p <= 'Z') || (*p >= '0' && *p <= '9') ||
            *p == '-' || *p == '_' || *p == '.' || *p == '~') {
            *q++ = *p;
        } else {
            snprintf(q, 4, "%%%02X", (unsigned char)*p);
            q += 3;
        }
        p++;
    }
    *q = '\0';
    return result;
}

char* url_decode(const char* str) {
    if (!str) return NULL;
    char* result = (char*)kmm_v4_malloc(strlen(str) + 1);
    if (!result) return NULL;

    const char* p = str;
    char* q = result;
    while (*p) {
        if (*p == '%' && p[1] && p[2]) {
            char hex[3] = {p[1], p[2], '\0'};
            *q++ = (char)strtol(hex, NULL, 16);
            p += 3;
        } else if (*p == '+') {
            *q++ = ' ';
            p++;
        } else {
            *q++ = *p++;
        }
    }
    *q = '\0';
    return result;
}

// ==================== 工具函数实现 ====================

const char* http_method_to_string(HttpMethod method) {
    switch (method) {
        case HTTP_GET: return "GET";
        case HTTP_POST: return "POST";
        case HTTP_PUT: return "PUT";
        case HTTP_DELETE: return "DELETE";
        case HTTP_PATCH: return "PATCH";
        case HTTP_HEAD: return "HEAD";
        case HTTP_OPTIONS: return "OPTIONS";
        default: return "UNKNOWN";
    }
}

const char* http_status_to_string(HttpStatusCode code) {
    switch (code) {
        case HTTP_OK: return "OK";
        case HTTP_CREATED: return "Created";
        case HTTP_NO_CONTENT: return "No Content";
        case HTTP_BAD_REQUEST: return "Bad Request";
        case HTTP_UNAUTHORIZED: return "Unauthorized";
        case HTTP_FORBIDDEN: return "Forbidden";
        case HTTP_NOT_FOUND: return "Not Found";
        case HTTP_METHOD_NOT_ALLOWED: return "Method Not Allowed";
        case HTTP_INTERNAL_SERVER_ERROR: return "Internal Server Error";
        case HTTP_BAD_GATEWAY: return "Bad Gateway";
        case HTTP_SERVICE_UNAVAILABLE: return "Service Unavailable";
        default: return "Unknown";
    }
}

char* http_get_mime_type(const char* file_extension) {
    if (!file_extension) return MIME_APP_OCTET;

    if (strcmp(file_extension, ".html") == 0 || strcmp(file_extension, ".htm") == 0) return MIME_TEXT_HTML;
    if (strcmp(file_extension, ".css") == 0) return MIME_TEXT_CSS;
    if (strcmp(file_extension, ".js") == 0) return "application/javascript";
    if (strcmp(file_extension, ".json") == 0) return MIME_APP_JSON;
    if (strcmp(file_extension, ".xml") == 0) return MIME_APP_XML;
    if (strcmp(file_extension, ".txt") == 0) return MIME_TEXT_PLAIN;
    if (strcmp(file_extension, ".png") == 0) return MIME_IMAGE_PNG;
    if (strcmp(file_extension, ".jpg") == 0 || strcmp(file_extension, ".jpeg") == 0) return MIME_IMAGE_JPEG;
    if (strcmp(file_extension, ".gif") == 0) return MIME_IMAGE_GIF;
    if (strcmp(file_extension, ".svg") == 0) return "image/svg+xml";
    if (strcmp(file_extension, ".ico") == 0) return "image/x-icon";
    if (strcmp(file_extension, ".pdf") == 0) return "application/pdf";
    if (strcmp(file_extension, ".zip") == 0) return "application/zip";

    return MIME_APP_OCTET;
}