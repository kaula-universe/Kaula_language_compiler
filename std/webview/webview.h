#ifndef STD_WEBVIEW_WEBVIEW_H
#define STD_WEBVIEW_WEBVIEW_H

#include "../base/types.h"

// ============================================================
// std.webview — 跨平台 WebView 封装
// 目前实现平台：Windows (WebView2 via COM)
// 未来扩展：macOS (WKWebView), Linux (WebKitGTK)
//
// 注意：
//   Windows 版需要 WebView2 SDK 头文件（WebView2.h / WebView2WebView.h）
//   运行时依赖 Edge WebView2 Runtime（系统已自带 EmbeddedBrowserWebView.dll）
//
// 使用方法（两步）：
//   1. 先运行 install_webview2_sdk.ps1 下载 SDK（首次）
//   2. 用 build_webview2.bat 编译 Kaula 代码 + WebView2 实现
// ============================================================

#ifdef __cplusplus
extern "C" {
#endif

// WebView 实例（不透明指针）
typedef struct KaulaWebView KaulaWebView;

// JS -> Native 回调（WebView 内调用 chrome.webview.postMessage(json) 触发）
typedef void (*KaulaWebViewMessageCallback)(const char* json, void* userdata);

// 初始化配置
typedef struct {
    const char* title;
    int width;
    int height;
    bool resizable;
    bool debug_port;            // 启用 Edge DevTools（右键->检查）
    const char* url;            // 初始 URL（file:// 或 http://）
    KaulaWebViewMessageCallback on_message;
    void* userdata;
} KaulaWebViewConfig;

// 创建窗口（返回 NULL 表示失败）
extern KaulaWebView* kaula_webview_create(const KaulaWebViewConfig* config);

// 进入消息循环（阻塞；窗口关闭时返回）
extern void kaula_webview_run(KaulaWebView* wv);

// 主动终止窗口（可在回调线程调用）
extern void kaula_webview_terminate(KaulaWebView* wv);

// 销毁资源（run 返回后自动调用一次）
extern void kaula_webview_destroy(KaulaWebView* wv);

// 运行时修改窗口标题
extern void kaula_webview_set_title(KaulaWebView* wv, const char* title);

// 运行时跳转到 URL
extern void kaula_webview_navigate(KaulaWebView* wv, const char* url);

// 在页面上下文中执行 JavaScript
extern void kaula_webview_eval(KaulaWebView* wv, const char* js);

// ============================================================
// 便捷 API（Kaula 层常用）
// ============================================================

// 简化：启动窗口 + 进入循环
//   url: 本地文件路径或 http URL
//   title: 窗口标题
//   width/height: 初始大小
extern bool webview_launch(const char* url, const char* title, int width, int height);

#ifdef __cplusplus
}
#endif

#endif // STD_WEBVIEW_WEBVIEW_H
