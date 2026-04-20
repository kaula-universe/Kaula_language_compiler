#ifndef STD_WEB_WEB_H
#define STD_WEB_WEB_H

#include "../base/types.h"

// 跨平台socket支持
#if STD_PLATFORM_WINDOWS
    #include <winsock2.h>
    #include <ws2tcpip.h>
    #pragma comment(lib, "ws2_32.lib")
    typedef SOCKET WebSocket;
#else
    #include <sys/socket.h>
    #include <netinet/in.h>
    #include <arpa/inet.h>
    #include <netdb.h>
    #include <unistd.h>
    #include <fcntl.h>
    typedef int WebSocket;
    #define INVALID_SOCKET -1
    #define SOCKET_ERROR -1
#endif

// AsyncParam 结构体（用于 async 参数语法）
typedef struct {
    void* data;
} AsyncParam;

// HTTP方法
typedef enum {
    HTTP_GET,
    HTTP_POST,
    HTTP_PUT,
    HTTP_DELETE,
    HTTP_PATCH,
    HTTP_HEAD,
    HTTP_OPTIONS
} HttpMethod;

// HTTP状态码
typedef enum {
    HTTP_OK = 200,
    HTTP_CREATED = 201,
    HTTP_NO_CONTENT = 204,
    HTTP_BAD_REQUEST = 400,
    HTTP_UNAUTHORIZED = 401,
    HTTP_FORBIDDEN = 403,
    HTTP_NOT_FOUND = 404,
    HTTP_METHOD_NOT_ALLOWED = 405,
    HTTP_INTERNAL_SERVER_ERROR = 500,
    HTTP_BAD_GATEWAY = 502,
    HTTP_SERVICE_UNAVAILABLE = 503
} HttpStatusCode;

// MIME类型
#define MIME_TEXT_PLAIN "text/plain"
#define MIME_TEXT_HTML "text/html"
#define MIME_TEXT_CSS "text/css"
#define MIME_APP_JSON "application/json"
#define MIME_APP_XML "application/xml"
#define MIME_APP_FORM "application/x-www-form-urlencoded"
#define MIME_MULTIPART_FORM "multipart/form-data"
#define MIME_APP_OCTET "application/octet-stream"
#define MIME_IMAGE_PNG "image/png"
#define MIME_IMAGE_JPEG "image/jpeg"
#define MIME_IMAGE_GIF "image/gif"

// HTTP请求结构
typedef struct {
    HttpMethod method;
    char* path;
    char* query_string;
    char* headers;
    char* body;
    size_t body_length;
} HttpRequest;

// HTTP响应结构
typedef struct {
    HttpStatusCode status_code;
    char* status_message;
    char* headers;
    char* body;
    size_t body_length;
    char* content_type;
} HttpResponse;

// HTTP服务器结构
typedef struct {
    WebSocket socket;
    int port;
    bool running;
    char* document_root;
} HttpServer;

// HTTP客户端结构
typedef struct {
    WebSocket socket;
    char* base_url;
    i32 timeout_ms;
} HttpClient;

// 回调函数类型
typedef void (*HttpRequestHandler)(HttpRequest* req, HttpResponse* res);
typedef size_t (*HttpBodyCallback)(const char* data, size_t size, size_t nmemb, void* userdata);

// ==================== HTTP服务器函数 ====================

extern HttpServer* http_server_create(i32 port);
extern bool http_server_start(HttpServer* server, HttpRequestHandler handler);
extern bool http_server_stop(HttpServer* server);
extern void http_server_destroy(HttpServer* server);
extern bool http_server_set_document_root(HttpServer* server, const char* root);
extern void http_server_enable_cors(HttpServer* server, const char* allowed_origin);

// ==================== HTTP请求解析 ====================

extern HttpRequest* http_request_parse(const char* raw_request, size_t length);
extern void http_request_destroy(HttpRequest* req);
extern const char* http_request_get_header(HttpRequest* req, const char* name);
extern const char* http_request_get_query_param(HttpRequest* req, const char* name);

// ==================== HTTP响应构建 ====================

extern HttpResponse* http_response_create(void);
extern void http_response_destroy(HttpResponse* res);
extern void http_response_set_status(HttpResponse* res, HttpStatusCode code);
extern void http_response_set_body(HttpResponse* res, const char* body, size_t length);
extern void http_response_set_content_type(HttpResponse* res, const char* content_type);
extern void http_response_set_header(HttpResponse* res, const char* name, const char* value);
extern void http_response_set_json(HttpResponse* res, const char* json_string);
extern void http_response_set_html(HttpResponse* res, const char* html);
extern void http_response_set_redirect(HttpResponse* res, const char* location);
extern char* http_response_to_string(HttpResponse* res);

// ==================== HTTP客户端函数 ====================

extern HttpClient* http_client_create(void);
extern void http_client_destroy(HttpClient* client);
extern bool http_client_set_timeout(HttpClient* client, i32 timeout_ms);
extern HttpResponse* http_client_get(HttpClient* client, const char* url);
extern HttpResponse* http_client_post(HttpClient* client, const char* url, const char* body, const char* content_type);
extern HttpResponse* http_client_put(HttpClient* client, const char* url, const char* body, const char* content_type);
extern HttpResponse* http_client_delete(HttpClient* client, const char* url);
extern void http_response_print(HttpResponse* res);

// ==================== URL解析函数 ====================

typedef struct {
    char* scheme;
    char* host;
    i32 port;
    char* path;
    char* query;
} UrlParts;

extern UrlParts* url_parse(const char* url);
extern void url_destroy(UrlParts* parts);
extern char* url_encode(const char* str);
extern char* url_decode(const char* str);

// ==================== 工具函数 ====================

extern const char* http_method_to_string(HttpMethod method);
extern const char* http_status_to_string(HttpStatusCode code);
extern char* http_get_mime_type(const char* file_extension);

#endif // STD_WEB_WEB_H