#pragma once
#include <stdint.h>
#include <stddef.h>
typedef struct worldr_term worldr_term;
typedef struct {
    uint32_t chars[6];
    int width;
    uint8_t fg[3], bg[3];
    uint8_t bold, italic, underline, reverse, strike;
} worldr_term_cell;
typedef struct {
    uint64_t revision;
    uint64_t first_line;
    int rows, cols, cursor_row, cursor_col, cursor_visible, cursor_shape, cursor_blink;
    int scroll_offset, scrollback_len, mouse, alternate;
} worldr_term_info;
typedef struct { uint32_t chars[6]; int width; } worldr_term_text_cell;
typedef struct {
    uint64_t id, command_line, output_line, end_line;
    int command_col, output_col, end_col, started, finished, status;
} worldr_term_command;
worldr_term *worldr_term_new(int rows,int cols,int scrollback);
void worldr_term_free(worldr_term *t);
int worldr_term_feed(worldr_term *t,const char *data,size_t len);
void worldr_term_resize(worldr_term *t,int rows,int cols);
worldr_term_info worldr_term_snapshot(worldr_term *t,worldr_term_cell *cells);
const char *worldr_term_title(worldr_term *t);
size_t worldr_term_output(worldr_term *t,char *data,size_t capacity);
int worldr_term_overflow(worldr_term *t);
int worldr_term_keymap(worldr_term *t,const char *map);
void worldr_term_modifiers(worldr_term *t,uint32_t depressed,uint32_t latched,uint32_t locked,uint32_t group);
void worldr_term_key(worldr_term *t,uint32_t code);
int worldr_term_repeats(worldr_term *t,uint32_t code);
void worldr_term_focus(worldr_term *t,int focused);
void worldr_term_paste(worldr_term *t,const char *data,size_t len);
void worldr_term_scroll(worldr_term *t,int lines);
void worldr_term_mouse(worldr_term *t,int row,int col,int button,int pressed,int mods);
void worldr_term_mouse_cancel(worldr_term *t);
int worldr_term_text_line(worldr_term *t,int index,worldr_term_text_cell *cells,int capacity);
int worldr_term_commands(worldr_term *t,worldr_term_command *commands,int capacity);
