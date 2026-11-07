#include "gui.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef _WIN32
#include <windows.h>
#endif

#ifdef KUALA_USE_NUKLEAR
#include "../../pkglib/nuklear/nuklear.h"

// 全局窗口句柄
static HWND g_window = NULL;
static HDC g_dc = NULL;
static struct nk_gdi g_gdi;

static LRESULT CALLBACK WindowProc(HWND hwnd, UINT msg, WPARAM wParam, LPARAM lParam) {
    return DefWindowProcW(hwnd, msg, wParam, lParam);
}

GUIContext* gui_create() {
    GdiplusStartupInput gdiplusStartupInput;
    ULONG_PTR gdiplusToken;
    GdiplusStartup(&gdiplusToken, &gdiplusStartupInput, NULL);
    
    WNDCLASSEXW wc;
    RECT rect = {0, 0, 800, 600};
    wc.style = CS_VREDRAW | CS_HREDRAW;
    wc.cbSize = sizeof(wc);
    wc.hInstance = NULL;
    wc.lpfnWndProc = WindowProc;
    wc.cbClsExtra = 0;
    wc.cbWndExtra = 0;
    wc.hCursor = LoadCursor(NULL, IDC_ARROW);
    wc.hbrBackground = (HBRUSH)GetStockObject(WHITE_BRUSH);
    wc.lpszClassName = L"NuklearWindow";
    wc.hIcon = NULL;
    wc.hIconSm = NULL;
    RegisterClassExW(&wc);
    AdjustWindowRect(&rect, WS_OVERLAPPEDWINDOW, FALSE);
    g_window = CreateWindowExW(0, L"NuklearWindow", L"Kaula GUI", WS_OVERLAPPEDWINDOW,
        CW_USEDEFAULT, CW_USEDEFAULT, rect.right - rect.left, rect.bottom - rect.top,
        NULL, NULL, NULL, NULL);
    ShowWindow(g_window, SW_SHOW);
    struct nk_context *ctx = nk_create();
    g_dc = GetDC(g_window);
    nk_gdi_init(&g_gdi, g_dc, 800, 600);
    return ctx;
}

void gui_destroy(GUIContext* ctx) {
    nk_gdi_shutdown();
    nk_free(ctx);
    DestroyWindow(g_window);
}

void gui_begin(GUIContext* ctx, const char* title, float x, float y, float width, float height) {
    struct nk_rect rect = {x, y, width, height};
    nk_begin(ctx, title, rect, NK_WINDOW_BORDER | NK_WINDOW_MOVABLE | NK_WINDOW_SCALABLE | NK_WINDOW_MINIMIZABLE | NK_WINDOW_TITLE);
}

void gui_end(GUIContext* ctx) {
    nk_end(ctx);
    if (!nk_window_is_hidden(ctx, "")) {
        nk_gdi_render(nk_rgb(30, 30, 30));
        Sleep(16);
    }
}

void gui_label(GUIContext* ctx, const char* text) {
    nk_label(ctx, text, NK_TEXT_LEFT);
}

bool gui_button(GUIContext* ctx, const char* label) {
    return nk_button_label(ctx, label) != 0;
}

void gui_slider_int(GUIContext* ctx, int min, int* value, int max, int step) {
    nk_slider_int(ctx, min, value, max, step);
}

void gui_input_text(GUIContext* ctx, char* buffer, size_t max_length) {
    nk_edit_string(ctx, NK_EDIT_FIELD, buffer, &max_length, max_length, nk_filter_default);
}

void gui_layout_row_dynamic(GUIContext* ctx, float height, int columns) {
    nk_layout_row_dynamic(ctx, height, columns);
}

bool gui_process_messages() {
    MSG msg;
    if (PeekMessageW(&msg, NULL, 0, 0, PM_REMOVE)) {
        if (msg.message == WM_QUIT) return false;
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
    return true;
}

#else

// Stub implementations when Nuklear is not available

GUIContext* gui_create() { return NULL; }
void gui_destroy(GUIContext* ctx) { (void)ctx; }
void gui_begin(GUIContext* ctx, const char* title, float x, float y, float width, float height) { (void)ctx; (void)title; (void)x; (void)y; (void)width; (void)height; }
void gui_end(GUIContext* ctx) { (void)ctx; }
void gui_label(GUIContext* ctx, const char* text) { (void)ctx; (void)text; }
bool gui_button(GUIContext* ctx, const char* label) { (void)ctx; (void)label; return false; }
void gui_slider_int(GUIContext* ctx, int min, int* value, int max, int step) { (void)ctx; (void)min; (void)value; (void)max; (void)step; }
void gui_input_text(GUIContext* ctx, char* buffer, size_t max_length) { (void)ctx; (void)buffer; (void)max_length; }
void gui_layout_row_dynamic(GUIContext* ctx, float height, int columns) { (void)ctx; (void)height; (void)columns; }
bool gui_process_messages() { return true; }

#endif
