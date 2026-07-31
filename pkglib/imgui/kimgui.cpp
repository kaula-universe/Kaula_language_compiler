/*
 * kimgui.cpp - Dear ImGui C wrapper implementation (Win32 + D3D11)
 *
 * Based on the official Dear ImGui example (example_win32_directx11),
 * adapted to a minimal C API for the Kaula programming language.
 */
#include "kimgui.h"

#include "imgui.h"
#include "backends/imgui_impl_win32.h"
#include "backends/imgui_impl_dx11.h"

#include <windows.h>
#include <d3d11.h>
#include <math.h>
#include <stdarg.h>
#include <stdio.h>
#include <string.h>

extern IMGUI_IMPL_API LRESULT ImGui_ImplWin32_WndProcHandler(HWND hWnd, UINT msg, WPARAM wParam, LPARAM lParam);

struct KImguiCtx {
    HWND                    hwnd;
    ID3D11Device*           device;
    ID3D11DeviceContext*    device_context;
    IDXGISwapChain*         swap_chain;
    ID3D11RenderTargetView* rtv;
    bool                    running;
    char                    input_buf[256];
};

static LRESULT CALLBACK KImguiWndProc(HWND hwnd, UINT msg, WPARAM wParam, LPARAM lParam) {
    if (ImGui_ImplWin32_WndProcHandler(hwnd, msg, wParam, lParam))
        return true;

    switch (msg) {
    case WM_DESTROY:
        ::PostQuitMessage(0);
        return 0;
    case WM_ERASEBKGND:
        return 1; /* avoid flicker */
    }
    return ::DefWindowProcW(hwnd, msg, wParam, lParam);
}

void* kimgui_init(const char* title, int width, int height) {
    KImguiCtx* ctx = new KImguiCtx();
    memset(ctx, 0, sizeof(KImguiCtx));
    ctx->running = true;

    WNDCLASSEXW wc = {};
    wc.cbSize = sizeof(wc);
    wc.style = CS_CLASSDC;
    wc.lpfnWndProc = KImguiWndProc;
    wc.hInstance = GetModuleHandleW(NULL);
    wc.lpszClassName = L"KaulaImGuiWindow";
    wc.hCursor = LoadCursorW(NULL, MAKEINTRESOURCEW(32512)); /* IDC_ARROW */
    RegisterClassExW(&wc);

    int wlen = MultiByteToWideChar(CP_UTF8, 0, title, -1, NULL, 0);
    wchar_t* wtitle = new wchar_t[wlen];
    MultiByteToWideChar(CP_UTF8, 0, title, -1, wtitle, wlen);

    RECT rect = {0, 0, width, height};
    AdjustWindowRect(&rect, WS_OVERLAPPEDWINDOW, FALSE);
    ctx->hwnd = CreateWindowExW(0, wc.lpszClassName, wtitle, WS_OVERLAPPEDWINDOW,
        CW_USEDEFAULT, CW_USEDEFAULT, rect.right - rect.left, rect.bottom - rect.top,
        NULL, NULL, wc.hInstance, NULL);
    delete[] wtitle;
    if (!ctx->hwnd) {
        delete ctx;
        return NULL;
    }

    DXGI_SWAP_CHAIN_DESC sd = {};
    sd.BufferCount = 2;
    sd.BufferDesc.Width = (UINT)width;
    sd.BufferDesc.Height = (UINT)height;
    sd.BufferDesc.Format = DXGI_FORMAT_R8G8B8A8_UNORM;
    sd.BufferDesc.RefreshRate.Numerator = 60;
    sd.BufferDesc.RefreshRate.Denominator = 1;
    sd.Flags = DXGI_SWAP_CHAIN_FLAG_ALLOW_MODE_SWITCH;
    sd.BufferUsage = DXGI_USAGE_RENDER_TARGET_OUTPUT;
    sd.OutputWindow = ctx->hwnd;
    sd.SampleDesc.Count = 1;
    sd.SampleDesc.Quality = 0;
    sd.Windowed = TRUE;
    sd.SwapEffect = DXGI_SWAP_EFFECT_DISCARD;

    const D3D_FEATURE_LEVEL featureLevels[2] = { D3D_FEATURE_LEVEL_11_0, D3D_FEATURE_LEVEL_10_0 };
    HRESULT hr = D3D11CreateDeviceAndSwapChain(NULL, D3D_DRIVER_TYPE_HARDWARE, NULL, 0,
        featureLevels, 2, D3D11_SDK_VERSION, &sd, &ctx->swap_chain, &ctx->device,
        NULL, &ctx->device_context);
    if (hr == DXGI_ERROR_UNSUPPORTED) {
        hr = D3D11CreateDeviceAndSwapChain(NULL, D3D_DRIVER_TYPE_WARP, NULL, 0,
            featureLevels, 2, D3D11_SDK_VERSION, &sd, &ctx->swap_chain, &ctx->device,
            NULL, &ctx->device_context);
    }
    if (hr != S_OK) {
        DestroyWindow(ctx->hwnd);
        delete ctx;
        return NULL;
    }

    ID3D11Texture2D* backBuffer = NULL;
    ctx->swap_chain->GetBuffer(0, IID_PPV_ARGS(&backBuffer));
    ctx->device->CreateRenderTargetView(backBuffer, NULL, &ctx->rtv);
    backBuffer->Release();

    IMGUI_CHECKVERSION();
    ImGui::CreateContext();
    ImGui::StyleColorsDark();

    ImGui_ImplWin32_Init(ctx->hwnd);
    ImGui_ImplDX11_Init(ctx->device, ctx->device_context);

    ShowWindow(ctx->hwnd, SW_SHOWDEFAULT);
    UpdateWindow(ctx->hwnd);
    return ctx;
}

void kimgui_shutdown(void* vctx) {
    KImguiCtx* ctx = (KImguiCtx*)vctx;
    if (!ctx)
        return;

    ImGui_ImplDX11_Shutdown();
    ImGui_ImplWin32_Shutdown();
    ImGui::DestroyContext();

    if (ctx->rtv)          { ctx->rtv->Release();          ctx->rtv = NULL; }
    if (ctx->swap_chain)   { ctx->swap_chain->Release();   ctx->swap_chain = NULL; }
    if (ctx->device_context){ ctx->device_context->Release(); ctx->device_context = NULL; }
    if (ctx->device)       { ctx->device->Release();       ctx->device = NULL; }
    if (ctx->hwnd)         { DestroyWindow(ctx->hwnd);     ctx->hwnd = NULL; }
    UnregisterClassW(L"KaulaImGuiWindow", GetModuleHandleW(NULL));
    delete ctx;
}

int kimgui_poll_events(void* vctx) {
    KImguiCtx* ctx = (KImguiCtx*)vctx;
    MSG msg;
    while (PeekMessageW(&msg, NULL, 0U, 0U, PM_REMOVE)) {
        if (msg.message == WM_QUIT) {
            ctx->running = false;
            break;
        }
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
    return ctx->running ? 1 : 0;
}

void kimgui_new_frame(void* vctx) {
    KImguiCtx* ctx = (KImguiCtx*)vctx;

    RECT rc;
    GetClientRect(ctx->hwnd, &rc);
    int w = rc.right - rc.left;
    int h = rc.bottom - rc.top;

    DXGI_SWAP_CHAIN_DESC sd;
    ctx->swap_chain->GetDesc(&sd);
    if (w > 0 && h > 0 && (w != (int)sd.BufferDesc.Width || h != (int)sd.BufferDesc.Height)) {
        ctx->device_context->OMSetRenderTargets(0, NULL, NULL);
        if (ctx->rtv) { ctx->rtv->Release(); ctx->rtv = NULL; }
        ctx->swap_chain->ResizeBuffers(0, (UINT)w, (UINT)h, DXGI_FORMAT_UNKNOWN, 0);
        ID3D11Texture2D* backBuffer = NULL;
        ctx->swap_chain->GetBuffer(0, IID_PPV_ARGS(&backBuffer));
        if (backBuffer) {
            ctx->device->CreateRenderTargetView(backBuffer, NULL, &ctx->rtv);
            backBuffer->Release();
        }
    }

    ImGui_ImplDX11_NewFrame();
    ImGui_ImplWin32_NewFrame();
    ImGui::NewFrame();
}

void kimgui_end_frame(void* vctx) {
    KImguiCtx* ctx = (KImguiCtx*)vctx;

    ImGui::Render();
    const float clear_color[4] = { 0.09f, 0.09f, 0.11f, 1.0f };
    if (ctx->rtv) {
        ctx->device_context->OMSetRenderTargets(1, &ctx->rtv, NULL);
        ctx->device_context->ClearRenderTargetView(ctx->rtv, clear_color);
    }
    ImGui_ImplDX11_RenderDrawData(ImGui::GetDrawData());
    ctx->swap_chain->Present(1, 0); /* vsync */
}

float kimgui_framerate(void* vctx) {
    (void)vctx;
    return ImGui::GetIO().Framerate;
}

void kimgui_show_demo_window(void* vctx, int* open) {
    (void)vctx;
    ImGui::ShowDemoWindow(open ? (bool*)open : NULL);
}

void kimgui_apply_dark_theme(void* vctx) {
    (void)vctx;
    ImGui::StyleColorsDark();
}

void kimgui_apply_light_theme(void* vctx) {
    (void)vctx;
    ImGui::StyleColorsLight();
}

void kimgui_begin_window(void* vctx, const char* title, int* open) {
    (void)vctx;
    ImGui::Begin(title, open ? (bool*)open : NULL);
}

void kimgui_end_window(void* vctx) {
    (void)vctx;
    ImGui::End();
}

void kimgui_set_next_window_pos(void* vctx, float x, float y) {
    (void)vctx;
    ImGui::SetNextWindowPos(ImVec2(x, y));
}

void kimgui_set_next_window_size(void* vctx, float w, float h) {
    (void)vctx;
    ImGui::SetNextWindowSize(ImVec2(w, h));
}

void kimgui_separator(void* vctx) { (void)vctx; ImGui::Separator(); }
void kimgui_same_line(void* vctx) { (void)vctx; ImGui::SameLine(); }
void kimgui_spacing(void* vctx)   { (void)vctx; ImGui::Spacing(); }
void kimgui_new_line(void* vctx)  { (void)vctx; ImGui::NewLine(); }

void kimgui_text(void* vctx, const char* text) {
    (void)vctx;
    ImGui::TextUnformatted(text);
}

void kimgui_text_fmt(void* vctx, const char* fmt, ...) {
    (void)vctx;
    char buf[1024];
    va_list args;
    va_start(args, fmt);
    vsnprintf(buf, sizeof(buf), fmt, args);
    va_end(args);
    ImGui::TextUnformatted(buf);
}

void kimgui_text_colored(void* vctx, float r, float g, float b, float a, const char* text) {
    (void)vctx;
    ImGui::TextColored(ImVec4(r, g, b, a), "%s", text);
}

int kimgui_button(void* vctx, const char* label) {
    (void)vctx;
    return ImGui::Button(label) ? 1 : 0;
}

int kimgui_checkbox(void* vctx, const char* label, int* value) {
    (void)vctx;
    return ImGui::Checkbox(label, (bool*)value) ? 1 : 0;
}

int kimgui_slider_int(void* vctx, const char* label, int* value, int vmin, int vmax) {
    (void)vctx;
    return ImGui::SliderInt(label, value, vmin, vmax) ? 1 : 0;
}

int kimgui_slider_float(void* vctx, const char* label, float* value, float vmin, float vmax) {
    (void)vctx;
    return ImGui::SliderFloat(label, value, vmin, vmax) ? 1 : 0;
}

int kimgui_color_edit4(void* vctx, const char* label, float* rgba) {
    (void)vctx;
    return ImGui::ColorEdit4(label, rgba) ? 1 : 0;
}

int kimgui_plot_sine(void* vctx, const char* label, float phase) {
    (void)vctx;
    static float values[128];
    for (int i = 0; i < 128; ++i)
        values[i] = sinf((float)i * 0.08f + phase) * 0.5f + 0.5f;
    ImGui::PlotLines(label, values, 128, 0, NULL, 0.0f, 1.0f, ImVec2(0, 80));
    return 0;
}

int kimgui_input_text(void* vctx, const char* label) {
    KImguiCtx* ctx = (KImguiCtx*)vctx;
    return ImGui::InputText(label, ctx->input_buf, sizeof(ctx->input_buf), ImGuiInputTextFlags_EnterReturnsTrue) ? 1 : 0;
}

const char* kimgui_input_text_value(void* vctx) {
    KImguiCtx* ctx = (KImguiCtx*)vctx;
    return ctx->input_buf;
}

void kimgui_push_style_color(void* vctx, int col_idx, float r, float g, float b, float a) {
    (void)vctx;
    ImGui::PushStyleColor((ImGuiCol)col_idx, ImVec4(r, g, b, a));
}

void kimgui_pop_style_color(void* vctx, int count) {
    (void)vctx;
    ImGui::PopStyleColor(count);
}
