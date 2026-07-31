/*
 * kimgui.h - Dear ImGui C wrapper (Win32 + D3D11) for Kaula
 *
 * All functions take an opaque context pointer returned by kimgui_init().
 * String parameters are UTF-8 C strings. Widget functions return 1 when the
 * widget value changed in the current frame (matching ImGui return semantics).
 */
#ifndef KIMGUI_H
#define KIMGUI_H

#ifdef __cplusplus
extern "C" {
#endif

/* ---- lifecycle ---- */
void* kimgui_init(const char* title, int width, int height);
void  kimgui_shutdown(void* ctx);
int   kimgui_poll_events(void* ctx);      /* 1 = keep running, 0 = window closed */
void  kimgui_new_frame(void* ctx);
void  kimgui_end_frame(void* ctx);
float kimgui_framerate(void* ctx);
void  kimgui_show_demo_window(void* ctx, int* open);

/* ---- theme ---- */
void kimgui_apply_dark_theme(void* ctx);
void kimgui_apply_light_theme(void* ctx);

/* ---- window / layout ---- */
void kimgui_begin_window(void* ctx, const char* title, int* open);
void kimgui_end_window(void* ctx);
void kimgui_set_next_window_pos(void* ctx, float x, float y);
void kimgui_set_next_window_size(void* ctx, float w, float h);
void kimgui_separator(void* ctx);
void kimgui_same_line(void* ctx);
void kimgui_spacing(void* ctx);
void kimgui_new_line(void* ctx);

/* ---- widgets ---- */
void kimgui_text(void* ctx, const char* text);
void kimgui_text_fmt(void* ctx, const char* fmt, ...);
void kimgui_text_colored(void* ctx, float r, float g, float b, float a, const char* text);
int  kimgui_button(void* ctx, const char* label);
int  kimgui_checkbox(void* ctx, const char* label, int* value);
int  kimgui_slider_int(void* ctx, const char* label, int* value, int vmin, int vmax);
int  kimgui_slider_float(void* ctx, const char* label, float* value, float vmin, float vmax);
int  kimgui_color_edit4(void* ctx, const char* label, float* rgba);
int  kimgui_plot_sine(void* ctx, const char* label, float phase);
int  kimgui_input_text(void* ctx, const char* label);
const char* kimgui_input_text_value(void* ctx);

/* ---- style ---- */
void kimgui_push_style_color(void* ctx, int col_idx, float r, float g, float b, float a);
void kimgui_pop_style_color(void* ctx, int count);

#ifdef __cplusplus
}
#endif

#endif /* KIMGUI_H */
