#ifndef STD_GUI_GUI_H
#define STD_GUI_GUI_H

#include "../base/types.h"
#include <stdint.h>
#include <stdbool.h>

// Nuklear GUI 上下文
typedef struct nk_context GUIContext;
typedef struct nk_rect GUIRect;

// GUI 初始化函数
extern GUIContext* gui_create();
extern void gui_destroy(GUIContext* ctx);
extern void gui_begin(GUIContext* ctx, const char* title, float x, float y, float width, float height);
extern void gui_end(GUIContext* ctx);

// 基础控件
extern void gui_label(GUIContext* ctx, const char* text);
extern bool gui_button(GUIContext* ctx, const char* label);
extern void gui_slider_int(GUIContext* ctx, int min, int* value, int max, int step);
extern void gui_input_text(GUIContext* ctx, char* buffer, size_t max_length);

// 布局
extern void gui_layout_row_dynamic(GUIContext* ctx, float height, int columns);

#endif // STD_GUI_GUI_H
