#pragma once
#include "clipboard.h"
#include <stdint.h>

typedef struct worldr_host worldr_host;
enum {
  HOST_MOVE = 1,
  HOST_DOWN,
  HOST_UP,
  HOST_CANCEL,
  HOST_KEY,
  HOST_CLOSE,
  HOST_SCROLL,
  HOST_KEYBOARD_CANCEL,
  HOST_KEYMAP,
  HOST_MODIFIERS,
  HOST_REPEAT_INFO,
  HOST_TEXT_COMMIT,
  HOST_TEXT_PREEDIT
};
// A keymap string is owned by its queued event. Poll transfers ownership to the
// caller, which must free it after copying; no Go pointer is stored by C.
typedef struct worldr_host_event {
  uint32_t kind, code, mods, keycode, button, time;
  uint32_t depressed, latched, locked, group;
  float x, y, scroll_x, scroll_y;
  int pressed;
  int32_t repeat_rate, repeat_delay;
  char *keymap;
  char *text, *text_context;
  int32_t preedit_begin, preedit_end;
  uint32_t delete_before, delete_after;
} worldr_host_event;
typedef struct worldr_host_text_input worldr_host_text_input;
int worldr_host_open(const char *title, int w, int h, int fullscreen,
                     int timeout_ms, worldr_host **out, char *err, int errlen);
void worldr_host_close(worldr_host *h);
void *worldr_host_display(worldr_host *h);
void *worldr_host_surface(worldr_host *h);
void worldr_host_size(worldr_host *h, int *w, int *height);
int worldr_host_poll(worldr_host *h, worldr_host_event *events, int capacity,
                     char *err, int errlen);
worldr_host_clipboard *worldr_host_clipboard_state(worldr_host *h);
worldr_host_text_input *worldr_host_text_input_state(worldr_host *h);
int worldr_host_scale120(worldr_host *h);
void worldr_host_logical_size(worldr_host *h, int *w, int *height);
void worldr_host_rect_to_surface(worldr_host *h, int *x, int *y, int *w,
                                 int *height);
