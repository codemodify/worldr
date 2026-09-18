#include "text_input.h"
#include "text-input-v3-client-protocol.h"
#include <stdlib.h>
#include <string.h>

#define TEXT_LIMIT 4000
struct worldr_host_text_input {
  struct zwp_text_input_manager_v3 *manager;
  struct zwp_text_input_v3 *object;
  struct wl_seat *seat;
  struct wl_surface *surface;
  uint32_t manager_name, serial, context_serial;
  int entered, active, wanted, dirty, text_dirty, wait_matching;
  char context[513], surrounding[TEXT_LIMIT + 1];
  int cursor, anchor, rect[4], scale, input_method_cause;
  char *pending_commit, *pending_preedit;
  uint32_t before, after;
  int32_t begin, end;
  void (*emit)(void *, worldr_host_event);
  void *data;
};
static void pending_clear(worldr_host_text_input *t) {
  free(t->pending_commit);
  free(t->pending_preedit);
  t->pending_commit = NULL;
  t->pending_preedit = NULL;
  t->before = 0;
  t->after = 0;
  t->begin = 0;
  t->end = 0;
}
static void send_event(worldr_host_text_input *t, uint32_t kind,
                       const char *text, int32_t begin, int32_t end,
                       uint32_t before, uint32_t after) {
  worldr_host_event event = {.kind = kind,
                             .text = strdup(text ? text : ""),
                             .text_context = strdup(t->context),
                             .preedit_begin = begin,
                             .preedit_end = end,
                             .delete_before = before,
                             .delete_after = after};
  if (!event.text || !event.text_context) {
    free(event.text);
    free(event.text_context);
    return;
  }
  t->emit(t->data, event);
}
static void commit_state(worldr_host_text_input *t) {
  zwp_text_input_v3_commit(t->object);
  t->serial++;
}
static int floor_scale(int value, int scale) {
  return value >= 0 ? value / scale : -((-value + scale - 1) / scale);
}
static void publish(worldr_host_text_input *t) {
  if (!t->object || !t->entered)
    return;
  if (!t->wanted) {
    if (t->active) {
      zwp_text_input_v3_disable(t->object);
      commit_state(t);
    }
    t->active = 0;
    t->dirty = 0;
    t->wait_matching = 0;
    return;
  }
  if (t->wait_matching)
    return;
  if (!t->active) {
    zwp_text_input_v3_enable(t->object);
    t->active = 1;
    t->text_dirty = 1;
    t->context_serial = t->serial + 1;
    zwp_text_input_v3_set_content_type(
        t->object, 0, ZWP_TEXT_INPUT_V3_CONTENT_PURPOSE_NORMAL);
  }
  if (!t->dirty)
    return;
  if (t->text_dirty) {
    zwp_text_input_v3_set_surrounding_text(t->object, t->surrounding, t->cursor,
                                           t->anchor);
    zwp_text_input_v3_set_text_change_cause(
        t->object, t->input_method_cause
                       ? ZWP_TEXT_INPUT_V3_CHANGE_CAUSE_INPUT_METHOD
                       : ZWP_TEXT_INPUT_V3_CHANGE_CAUSE_OTHER);
  }
  int scale = t->scale ? t->scale : 1;
  zwp_text_input_v3_set_cursor_rectangle(
      t->object, floor_scale(t->rect[0], scale), floor_scale(t->rect[1], scale),
      (t->rect[2] + scale - 1) / scale, (t->rect[3] + scale - 1) / scale);
  commit_state(t);
  t->dirty = 0;
  t->text_dirty = 0;
}
static void entered(void *data, struct zwp_text_input_v3 *object,
                    struct wl_surface *surface) {
  (void)object;
  worldr_host_text_input *t = data;
  t->entered = surface == t->surface;
  t->active = 0;
  t->wait_matching = 0;
  t->dirty = 1;
  pending_clear(t);
  publish(t);
}
static void left(void *data, struct zwp_text_input_v3 *object,
                 struct wl_surface *surface) {
  (void)object;
  (void)surface;
  worldr_host_text_input *t = data;
  if (t->entered && t->active)
    send_event(t, HOST_TEXT_PREEDIT, "", 0, 0, 0, 0);
  t->entered = 0;
  t->active = 0;
  t->wait_matching = 0;
  pending_clear(t);
}
static char *bounded_copy(const char *text) {
  if (!text)
    return strdup("");
  size_t n = strnlen(text, TEXT_LIMIT + 1);
  if (n > TEXT_LIMIT)
    return NULL;
  return strdup(text);
}
static void preedit(void *data, struct zwp_text_input_v3 *object,
                    const char *text, int32_t begin, int32_t end) {
  (void)object;
  worldr_host_text_input *t = data;
  free(t->pending_preedit);
  t->pending_preedit = bounded_copy(text);
  t->begin = begin;
  t->end = end;
}
static void committed(void *data, struct zwp_text_input_v3 *object,
                      const char *text) {
  (void)object;
  worldr_host_text_input *t = data;
  free(t->pending_commit);
  t->pending_commit = bounded_copy(text);
}
static void deleted(void *data, struct zwp_text_input_v3 *object,
                    uint32_t before, uint32_t after) {
  (void)object;
  worldr_host_text_input *t = data;
  t->before = before;
  t->after = after;
}
static void done(void *data, struct zwp_text_input_v3 *object,
                 uint32_t serial) {
  (void)object;
  worldr_host_text_input *t = data;
  // Old serials within this field still apply, as required by text-input-v3.
  // A serial predating this field's enable belongs to another focus lifetime.
  if (t->entered && t->active && (int32_t)(serial - t->context_serial) >= 0 &&
      (int32_t)(t->serial - serial) >= 0) {
    if (t->pending_commit || t->before || t->after)
      send_event(t, HOST_TEXT_COMMIT, t->pending_commit, 0, 0, t->before,
                 t->after);
    send_event(t, HOST_TEXT_PREEDIT, t->pending_preedit, t->begin, t->end, 0,
               0);
    t->wait_matching = serial != t->serial;
  }
  pending_clear(t);
}
static const struct zwp_text_input_v3_listener listener = {
    .enter = entered,
    .leave = left,
    .preedit_string = preedit,
    .commit_string = committed,
    .delete_surrounding_text = deleted,
    .done = done};
static void attach(worldr_host_text_input *t) {
  if (t->manager && t->seat && !t->object) {
    t->object = zwp_text_input_manager_v3_get_text_input(t->manager, t->seat);
    t->serial = 0;
    t->context_serial = 0;
    zwp_text_input_v3_add_listener(t->object, &listener, t);
  }
}
worldr_host_text_input *
worldr_host_text_input_new(void (*emit)(void *, worldr_host_event),
                           void *data) {
  worldr_host_text_input *t = calloc(1, sizeof(*t));
  if (t) {
    t->emit = emit;
    t->data = data;
    t->scale = 1;
  }
  return t;
}
void worldr_host_text_input_cancel(worldr_host_text_input *t) {
  if (!t)
    return;
  if (t->active)
    send_event(t, HOST_TEXT_PREEDIT, "", 0, 0, 0, 0);
  pending_clear(t);
  t->wanted = 0;
  t->wait_matching = 0;
  publish(t);
}
void worldr_host_text_input_close(worldr_host_text_input *t) {
  if (!t)
    return;
  pending_clear(t);
  if (t->object)
    zwp_text_input_v3_destroy(t->object);
  if (t->manager)
    zwp_text_input_manager_v3_destroy(t->manager);
  free(t);
}
void worldr_host_text_input_global(worldr_host_text_input *t,
                                   struct wl_registry *r, uint32_t name,
                                   uint32_t version) {
  if (!t || t->manager || version < 1)
    return;
  t->manager =
      wl_registry_bind(r, name, &zwp_text_input_manager_v3_interface, 1);
  t->manager_name = name;
  attach(t);
}
void worldr_host_text_input_removed(worldr_host_text_input *t, uint32_t name) {
  if (!t || name != t->manager_name)
    return;
  worldr_host_text_input_cancel(t);
  if (t->object)
    zwp_text_input_v3_destroy(t->object);
  t->object = NULL;
  if (t->manager)
    zwp_text_input_manager_v3_destroy(t->manager);
  t->manager = NULL;
  t->manager_name = 0;
  t->entered = t->active = 0;
}
void worldr_host_text_input_seat(worldr_host_text_input *t,
                                 struct wl_seat *seat) {
  if (!t || t->seat == seat)
    return;
  worldr_host_text_input_cancel(t);
  if (t->object)
    zwp_text_input_v3_destroy(t->object);
  t->object = NULL;
  t->seat = seat;
  t->entered = t->active = 0;
  attach(t);
}
void worldr_host_text_input_surface(worldr_host_text_input *t,
                                    struct wl_surface *surface) {
  if (t)
    t->surface = surface;
}
int worldr_host_text_input_available(worldr_host_text_input *t) {
  return t && t->object;
}
void worldr_host_text_input_set(worldr_host_text_input *t, int enabled,
                                const char *context, const char *text,
                                int cursor, int anchor, int x, int y, int width,
                                int height, int scale, int input_method_cause) {
  if (!t)
    return;
  int changed = strcmp(t->context, context) != 0;
  t->input_method_cause = !changed && input_method_cause;
  if (changed) {
    if (t->active && t->entered) {
      zwp_text_input_v3_disable(t->object);
      commit_state(t);
    }
    t->active = 0;
    t->wait_matching = 0;
    pending_clear(t);
    strncpy(t->context, context, sizeof(t->context) - 1);
    t->dirty = 1;
    t->text_dirty = 1;
  }
  if (strcmp(t->surrounding, text) || t->cursor != cursor ||
      t->anchor != anchor)
    t->text_dirty = 1;
  if (t->wanted != enabled || strcmp(t->surrounding, text) ||
      t->cursor != cursor || t->anchor != anchor || t->rect[0] != x ||
      t->rect[1] != y || t->rect[2] != width || t->rect[3] != height ||
      t->scale != scale)
    t->dirty = 1;
  t->wanted = enabled;
  strncpy(t->surrounding, text, sizeof(t->surrounding) - 1);
  t->cursor = cursor;
  t->anchor = anchor;
  t->rect[0] = x;
  t->rect[1] = y;
  t->rect[2] = width;
  t->rect[3] = height;
  t->scale = scale;
  if (!enabled)
    pending_clear(t);
  publish(t);
}
