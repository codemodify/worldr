#ifndef WORLDR_APPS_H
#define WORLDR_APPS_H
#include <stddef.h>
#include <stdint.h>
typedef struct worldr_apps worldr_apps;
typedef struct {
  uint64_t id, revision;
  int width, height, logical_width, logical_height;
  const char *title, *app_id;
  uint32_t pid;
  const unsigned char *pixels;
  int layered;
} worldr_app_surface;
typedef struct {
  uint64_t token;
  int width, height, gpu, root, opaque;
  float x, y, logical_width, logical_height;
  float u0, v0, u1, v1;
  const unsigned char *pixels;
} worldr_app_layer;
int worldr_apps_layers(worldr_apps *s, uint64_t id, worldr_app_layer *out,
                       int cap);
int worldr_apps_image_alive(worldr_apps *s, uint64_t token);
typedef struct {
  uint32_t fourcc;
  uint64_t modifier;
} worldr_app_dmabuf_format;
int worldr_apps_dmabuf(worldr_apps *s, uintptr_t handle,
                       const worldr_app_dmabuf_format *formats, int count,
                       uint32_t render_major, uint32_t render_minor,
                       int has_render_node);
typedef struct {
  uint64_t surface_id, revision, image_id, image_revision;
  int set, hidden, width, height, scale, hotspot_x, hotspot_y, logical_width,
      logical_height, drag_icon;
  const unsigned char *pixels;
} worldr_app_cursor;
void worldr_apps_cursor(worldr_apps *s, worldr_app_cursor *out);
int worldr_apps_open(const char *socket, int width, int height,
                     worldr_apps **out, char *err, size_t cap);
void worldr_apps_close(worldr_apps *s);
void worldr_apps_request_close(worldr_apps *s);
void worldr_apps_close_surface(worldr_apps *s, uint64_t id);
int worldr_apps_poll(worldr_apps *s, worldr_app_surface *out, int cap,
                     char *err, size_t errcap);
int worldr_apps_drag_active(worldr_apps *s);
void worldr_apps_cancel_drag(worldr_apps *s);
int worldr_apps_focus(worldr_apps *s, uint64_t id);
int worldr_apps_pointer(worldr_apps *s, uint64_t id, float x, float y);
void worldr_apps_button(worldr_apps *s, uint32_t code, int pressed,
                        uint32_t time);
void worldr_apps_axis(worldr_apps *s, float horizontal, float vertical,
                      uint32_t time);
void worldr_apps_key(worldr_apps *s, uint32_t code, int pressed, uint32_t time,
                     uint32_t depressed, uint32_t latched, uint32_t locked,
                     uint32_t group);
int worldr_apps_keymap(worldr_apps *s, const char *keymap);
void worldr_apps_modifiers(worldr_apps *s, uint32_t depressed, uint32_t latched,
                           uint32_t locked, uint32_t group);
void worldr_apps_repeat(worldr_apps *s, int32_t rate, int32_t delay);
int worldr_apps_resize(worldr_apps *s, uint64_t id, int width, int height);
typedef struct {
  uint64_t revision, id, external_id;
  int mime_count;
} worldr_app_clipboard_offer;
typedef struct {
  uint64_t external_id;
  char *mime;
  int fd;
} worldr_app_clipboard_request;
void worldr_apps_clipboard_offer(worldr_apps *s,
                                 worldr_app_clipboard_offer *out);
const char *worldr_apps_clipboard_mime(worldr_apps *s, int index);
int worldr_apps_offer_clipboard(worldr_apps *s, uint64_t external_id,
                                const char **mimes, int count);
int worldr_apps_receive_clipboard(worldr_apps *s, uint64_t offer_id,
                                  const char *mime, int fd);
int worldr_apps_clipboard_requests(worldr_apps *s,
                                   worldr_app_clipboard_request *out, int cap);
int worldr_apps_associate_x11(worldr_apps *s, uint32_t pid, uint32_t object,
                              uint32_t window, const char *title,
                              const char *app_id, uint64_t *id);
int worldr_apps_update_x11(worldr_apps *s, uint32_t window, const char *title,
                           const char *app_id);
void worldr_apps_withdraw_x11(worldr_apps *s, uint32_t window);
uint32_t worldr_apps_x11_window(worldr_apps *s, uint64_t id);
#endif
