// go:build linux && cgo

#define _GNU_SOURCE
#include "apps.h"
#include "linux-dmabuf-server-protocol.h"
#include "xdg-decoration-server-protocol.h"
#include "xdg-shell-server-protocol.h"
#include <errno.h>
#include <fcntl.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/sysmacros.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>
#include <wayland-server.h>
#include <xkbcommon/xkbcommon.h>

// The adapter deliberately advertises only protocols it implements. SHM
// buffers are copied under libwayland's SIGBUS guard and released immediately.
#define MAX_SURFACES 128
#define MAX_PIXELS_BYTES (128u * 1024u * 1024u)
#define MAX_CURSOR_SIZE 512
#define MAX_DMABUF_FORMATS 256
enum surface_role {
  ROLE_NONE,
  ROLE_XDG,
  ROLE_SUBSURFACE,
  ROLE_CURSOR,
  ROLE_DRAG_ICON,
  ROLE_X11
};
struct app_surface;
struct app_positioner {
  struct wl_resource *wm;
  int width, height, anchor_x, anchor_y, anchor_width, anchor_height, offset_x,
      offset_y;
  uint32_t anchor, gravity, constraints;
  int reactive, parent_width, parent_height, parent_size_set;
  uint32_t parent_configure;
  int parent_configure_set;
};
struct app_source {
  struct worldr_apps *server;
  struct wl_resource *resource;
  char *mimes[64];
  int mime_count, used;
  uint32_t actions;
  int actions_set;
  uint64_t id, external_id;
};
struct clipboard_request {
  struct wl_list link;
  worldr_app_clipboard_request request;
};
struct app_offer {
  struct wl_list link;
  struct wl_resource *resource;
  struct app_source *source;
  struct worldr_apps *server;
  uint32_t actions, preferred, action, enter_serial;
  int drag, accepted, dropped, finished;
};
struct input_resource {
  struct wl_list link;
  struct wl_resource *resource;
  struct worldr_apps *server;
};
struct frame_callback {
  struct wl_list link;
  struct wl_resource *resource;
  uint64_t generation;
};
#include "subsurface_state.h"
struct worldr_apps {
  struct wl_display *display;
  struct wl_event_loop *loop;
  struct wl_list surfaces, keyboards, pointers, data_devices, offers,
      clipboard_requests, images;
  struct app_source *selection;
  struct app_source *drag_source;
  struct app_surface *drag_origin, *drag_target, *drag_icon;
  struct wl_resource *drag_device;
  struct app_offer *drag_offer;
  uint32_t drag_button;
  int drag_icon_x, drag_icon_y;
  int drag_active;
  struct app_surface *keyboard_focus, *pointer_focus;
  struct app_surface *popup_grab;
  struct app_surface *cursor_surface;
  uint64_t cursor_revision;
  uint32_t pointer_enter_serial;
  int cursor_set, cursor_x, cursor_y;
  uint64_t next_id;
  uint64_t next_clipboard_id, clipboard_revision;
  struct {
    uint32_t serial;
    uint64_t root_id;
  } input_serials[64];
  unsigned input_serial_cursor;
  int width, height, keymap_fd, repeat_rate, repeat_delay, surface_count,
      source_count;
  size_t keymap_size, pixels_bytes;
  uint32_t depressed, latched, locked, group;
  float pointer_x, pointer_y;
  uint32_t keys[256];
  size_t key_count;
  uint32_t buttons[32];
  uint32_t button_serials[32];
  size_t button_count;
  int pointer_cancelling;
  struct wl_global *dmabuf_global;
  uintptr_t importer_handle;
  worldr_app_dmabuf_format dmabuf_formats[MAX_DMABUF_FORMATS];
  int dmabuf_format_count, dmabuf_params_count, dmabuf_buffer_count;
  dev_t dmabuf_device;
  int dmabuf_has_device;
  uint64_t next_image;
  uint64_t next_update, next_association;
  int update_count;
  int region_count;
  uint32_t preferred_scale120;
};
struct app_surface {
  struct wl_list link;
  struct worldr_apps *server;
  struct wl_resource *resource, *xdg, *toplevel, *subsurface, *popup;
  struct wl_resource *viewport, *fractional;
  int viewport_source, viewport_destination, pending_viewport_source,
      pending_viewport_destination;
  int32_t viewport_src[4], viewport_dst[2], pending_viewport_src[4],
      pending_viewport_dst[2];
  struct app_surface *parent;
  uint64_t id, revision;
  uint64_t image_token;
  struct app_image *image;
  struct app_update *latest;
  struct app_input_region input, pending_input;
  uint64_t applied_generation, association;
  int placed, stack_order, pending_below_parent;
  int gpu, opaque;
  char *title, *app_id;
  uint32_t pid;
  uint32_t x11_window;
  int x11_mapped;
  struct wl_resource *pending_buffer;
  struct wl_listener buffer_destroy;
  int pending_attach, scale, pending_scale, transform;
  struct {
    int set;
    int64_t x1, y1, x2, y2;
  } pending_surface_damage, pending_buffer_damage;
  int role, attach_x, attach_y;
  int width, height, x, y, pending_x, pending_y, synchronized, below_parent;
  int configured, acked, requested_width, requested_height;
  uint32_t configure_serial;
  int geometry_x, geometry_y, geometry_width, geometry_height;
  int pending_geometry_x, pending_geometry_y, pending_geometry_width,
      pending_geometry_height;
  int popup_x, popup_y, popup_width, popup_height, popup_grabbed,
      popup_dismissed;
  struct app_positioner popup_positioner;
  int popup_positioner_set;
  int pending_popup_x, pending_popup_y, pending_popup_width,
      pending_popup_height, pending_popup_position;
  uint32_t pending_popup_serial;
  unsigned char *pixels, *snapshot;
  size_t pixels_size, snapshot_size;
  int snapshot_width, snapshot_height;
  int snapshot_dirty;
  struct wl_list frames;
};
static int has_buffer(struct app_surface *v) {
  return v && (v->pixels || v->gpu);
}
static uint32_t now_ms(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return (uint32_t)(t.tv_sec * 1000 + t.tv_nsec / 1000000);
}
static void focus_surface(struct worldr_apps *s, struct app_surface *v);
static void dismiss_popup(struct app_surface *v);
static void dismiss_popups(struct worldr_apps *s);
static void dismiss_surface_tree(struct app_surface *v);
static void drag_cancel(struct worldr_apps *s, int notify);
static void drag_motion(struct worldr_apps *s, struct app_surface *v, float x,
                        float y);
static void drag_action(struct app_offer *offer);
static void drag_drop(struct worldr_apps *s);
static int tree_gpu(struct app_surface *root);
static int flatten_surface(struct app_surface *root, int opaque);
static void reactive_popups_changed(struct app_surface *ancestor);
static void clear_cursor(struct worldr_apps *s) {
  s->cursor_surface = NULL;
  s->cursor_set = s->cursor_x = s->cursor_y = 0;
  s->cursor_revision++;
}
static int descendant(struct app_surface *v, struct app_surface *ancestor) {
  while (v) {
    if (v == ancestor)
      return 1;
    v = v->parent;
  }
  return 0;
}
static void destroy_request(struct wl_client *c, struct wl_resource *r) {
  (void)c;
  wl_resource_destroy(r);
}
static void post_oom(struct wl_resource *r) {
  wl_client_post_no_memory(wl_resource_get_client(r));
}
#include "image_state.inc"
// DMA-BUF commits share the retained image ownership helpers above.
#include "dmabuf_server.inc"
static struct app_surface *find_surface(struct worldr_apps *s, uint64_t id) {
  struct app_surface *v;
  wl_list_for_each(v, &s->surfaces, link) if (v->id == id &&
                                              (v->toplevel || v->x11_mapped) &&
                                              has_buffer(v)) return v;
  return NULL;
}
static struct app_surface *root_surface(struct app_surface *v) {
  int n = 0;
  while (v && v->parent && n++ < MAX_SURFACES)
    v = v->parent;
  return v;
}
static uint32_t input_serial(struct worldr_apps *s, struct app_surface *v,
                             int pressed) {
  uint32_t serial = wl_display_next_serial(s->display);
  if (pressed && v) {
    unsigned index = s->input_serial_cursor++ % 64;
    s->input_serials[index].serial = serial;
    s->input_serials[index].root_id = root_surface(v)->id;
  }
  return serial;
}
static int valid_grab_serial(struct worldr_apps *s, struct app_surface *v,
                             uint32_t serial) {
  uint64_t root = root_surface(v)->id;
  for (unsigned i = 0; i < 64; i++)
    if (s->input_serials[i].serial == serial &&
        s->input_serials[i].root_id == root)
      return 1;
  return 0;
}
static void changed(struct app_surface *v) {
  v = root_surface(v);
  if (v) {
    v->snapshot_dirty = 1;
    v->revision++;
    if (v->server->cursor_surface == v || v->server->drag_icon == v)
      v->server->cursor_revision++;
  }
}
#include "scaling_server.inc"
static void pointer_frame(struct wl_resource *r) {
  if (wl_resource_get_version(r) >= 5)
    wl_pointer_send_frame(r);
}
static int belongs(struct wl_resource *r, struct app_surface *v) {
  return v && wl_resource_get_client(r) == wl_resource_get_client(v->resource);
}
static void send_modifiers(struct worldr_apps *s, struct wl_resource *r) {
  wl_keyboard_send_modifiers(r, wl_display_next_serial(s->display),
                             s->depressed, s->latched, s->locked, s->group);
}
static void send_selection(struct worldr_apps *s, struct wl_resource *r);
static void keyboard_enter(struct worldr_apps *s, struct wl_resource *r) {
  struct wl_array keys;
  wl_array_init(&keys);
  if (s->key_count) {
    void *p = wl_array_add(&keys, s->key_count * sizeof(uint32_t));
    if (p)
      memcpy(p, s->keys, s->key_count * sizeof(uint32_t));
  }
  wl_keyboard_send_enter(r, wl_display_next_serial(s->display),
                         s->keyboard_focus->resource, &keys);
  send_modifiers(s, r);
  wl_array_release(&keys);
}
static void configure(struct app_surface *v) {
  if (!v->xdg || (!v->toplevel && !v->popup))
    return;
  if (v->popup) {
    if (v->popup_dismissed)
      return;
    xdg_popup_send_configure(v->popup, v->popup_x, v->popup_y, v->popup_width,
                             v->popup_height);
    v->configure_serial = wl_display_next_serial(v->server->display);
    xdg_surface_send_configure(v->xdg, v->configure_serial);
    v->configured = 1;
    return;
  }
  struct wl_array states;
  wl_array_init(&states);
  if (root_surface(v->server->keyboard_focus) == v) {
    uint32_t *p = wl_array_add(&states, sizeof(*p));
    if (p)
      *p = XDG_TOPLEVEL_STATE_ACTIVATED;
  }
  xdg_toplevel_send_configure(v->toplevel, v->requested_width,
                              v->requested_height, &states);
  wl_array_release(&states);
  v->configure_serial = wl_display_next_serial(v->server->display);
  xdg_surface_send_configure(v->xdg, v->configure_serial);
  v->configured = 1;
}

static void pending_buffer_gone(struct wl_listener *l, void *data) {
  (void)data;
  struct app_surface *v = wl_container_of(l, v, buffer_destroy);
  wl_list_remove(&v->buffer_destroy.link);
  wl_list_init(&v->buffer_destroy.link);
  v->pending_buffer = NULL;
}
static void clear_pending_buffer(struct app_surface *v) {
  if (v->pending_buffer) {
    wl_list_remove(&v->buffer_destroy.link);
    wl_list_init(&v->buffer_destroy.link);
    v->pending_buffer = NULL;
  }
}
static void frame_destroy(struct wl_resource *r) {
  struct frame_callback *f = wl_resource_get_user_data(r);
  wl_list_remove(&f->link);
  free(f);
}
static void finish_frames(struct app_surface *v, uint64_t generation) {
  struct frame_callback *f, *tmp;
  wl_list_for_each_safe(f, tmp, &v->frames, link) {
    if (!f->generation || f->generation > generation)
      continue;
    wl_callback_send_done(f->resource, now_ms());
    wl_resource_destroy(f->resource);
  }
}
static void surface_destroy(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  struct worldr_apps *s = v->server;
  scale_destroy_surface(v);
  if (s->drag_origin && descendant(s->drag_origin, v))
    drag_cancel(s, 1);
  else if (s->drag_target && descendant(s->drag_target, v)) {
    if (s->drag_active)
      drag_motion(s, NULL, 0, 0);
    else
      drag_cancel(s, 1);
  }
  if (s->drag_icon == v) {
    s->drag_icon = NULL;
    s->cursor_revision++;
  }
  if (s->cursor_surface == v)
    clear_cursor(s);
  dismiss_surface_tree(v);
  if (s->popup_grab && descendant(s->popup_grab, v))
    dismiss_popups(s);
  if (s->keyboard_focus == v)
    worldr_apps_focus(s, 0);
  if (s->pointer_focus == v)
    worldr_apps_pointer(s, 0, 0, 0);
  clear_pending_buffer(v);
  struct frame_callback *f, *ft;
  wl_list_for_each_safe(f, ft, &v->frames, link)
      wl_resource_destroy(f->resource);
  struct app_surface *child;
  wl_list_for_each(child, &s->surfaces, link) if (child->parent == v) {
    child->parent = NULL;
    child->placed = 0;
  }
  if (v->parent)
    changed(v->parent);
  if (v->xdg)
    wl_resource_set_user_data(v->xdg, NULL);
  if (v->toplevel)
    wl_resource_set_user_data(v->toplevel, NULL);
  if (v->subsurface)
    wl_resource_set_user_data(v->subsurface, NULL);
  if (v->popup)
    wl_resource_set_user_data(v->popup, NULL);
  wl_list_remove(&v->link);
  s->surface_count--;
  s->pixels_bytes -= v->snapshot_size;
  image_release(v->image);
  update_release(v->latest);
  free(v->snapshot);
  free(v->title);
  free(v->app_id);
  free(v);
}
static void surface_attach(struct wl_client *c, struct wl_resource *r,
                           struct wl_resource *b, int32_t x, int32_t y) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  clear_pending_buffer(v);
  v->pending_attach = 1;
  v->pending_buffer = b;
  v->attach_x = x;
  v->attach_y = y;
  if (b) {
    v->buffer_destroy.notify = pending_buffer_gone;
    wl_resource_add_destroy_listener(b, &v->buffer_destroy);
  }
}
static void surface_damage(struct wl_client *c, struct wl_resource *r,
                           int32_t x, int32_t y, int32_t w, int32_t h) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (w <= 0 || h <= 0)
    return;
  int64_t right = (int64_t)x + w, bottom = (int64_t)y + h;
  if (!v->pending_surface_damage.set) {
    v->pending_surface_damage.set = 1;
    v->pending_surface_damage.x1 = x;
    v->pending_surface_damage.y1 = y;
    v->pending_surface_damage.x2 = right;
    v->pending_surface_damage.y2 = bottom;
  } else {
    if (x < v->pending_surface_damage.x1)
      v->pending_surface_damage.x1 = x;
    if (y < v->pending_surface_damage.y1)
      v->pending_surface_damage.y1 = y;
    if (right > v->pending_surface_damage.x2)
      v->pending_surface_damage.x2 = right;
    if (bottom > v->pending_surface_damage.y2)
      v->pending_surface_damage.y2 = bottom;
  }
}
static void surface_damage_buffer(struct wl_client *c, struct wl_resource *r,
                                  int32_t x, int32_t y, int32_t w,
                                  int32_t h) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (w <= 0 || h <= 0)
    return;
  int64_t right = (int64_t)x + w, bottom = (int64_t)y + h;
  if (!v->pending_buffer_damage.set) {
    v->pending_buffer_damage.set = 1;
    v->pending_buffer_damage.x1 = x;
    v->pending_buffer_damage.y1 = y;
    v->pending_buffer_damage.x2 = right;
    v->pending_buffer_damage.y2 = bottom;
  } else {
    if (x < v->pending_buffer_damage.x1)
      v->pending_buffer_damage.x1 = x;
    if (y < v->pending_buffer_damage.y1)
      v->pending_buffer_damage.y1 = y;
    if (right > v->pending_buffer_damage.x2)
      v->pending_buffer_damage.x2 = right;
    if (bottom > v->pending_buffer_damage.y2)
      v->pending_buffer_damage.y2 = bottom;
  }
}
static void surface_region(struct wl_client *c, struct wl_resource *r,
                           struct wl_resource *region) {
  (void)c;
  (void)r;
  (void)region;
}
static void surface_input_region(struct wl_client *c, struct wl_resource *r,
                                 struct wl_resource *region) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (region)
    v->pending_input =
        ((struct app_region *)wl_resource_get_user_data(region))->shape;
  else
    v->pending_input = (struct app_input_region){.infinite = 1};
}
static void surface_frame(struct wl_client *c, struct wl_resource *r,
                          uint32_t id) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (wl_list_length(&v->frames) >= 128) {
    post_oom(r);
    return;
  }
  struct frame_callback *f = calloc(1, sizeof(*f));
  if (!f) {
    post_oom(r);
    return;
  }
  f->resource = wl_resource_create(c, &wl_callback_interface, 1, id);
  if (!f->resource) {
    free(f);
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(f->resource, NULL, f, frame_destroy);
  wl_list_insert(v->frames.prev, &f->link);
}
static void surface_scale(struct wl_client *c, struct wl_resource *r,
                          int32_t scale) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (scale < 1 || scale > 8) {
    wl_resource_post_error(r, WL_SURFACE_ERROR_INVALID_SCALE,
                           "buffer scale must be between 1 and 8");
    return;
  }
  v->pending_scale = scale;
}
static void surface_transform(struct wl_client *c, struct wl_resource *r,
                              int32_t transform) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (transform != WL_OUTPUT_TRANSFORM_NORMAL) {
    wl_resource_post_error(
        r, WL_SURFACE_ERROR_INVALID_TRANSFORM,
        "application adapter supports normal buffer transform only");
    return;
  }
  v->transform = transform;
}
static int copy_buffer(struct app_surface *v, struct wl_resource *b) {
  struct wl_shm_buffer *shm = wl_shm_buffer_get(b);
  if (!shm) {
    return copy_dmabuf(v, b);
  }
  int w = wl_shm_buffer_get_width(shm), h = wl_shm_buffer_get_height(shm),
      stride = wl_shm_buffer_get_stride(shm);
  uint32_t format = wl_shm_buffer_get_format(shm);
  int limit = v->role == ROLE_CURSOR ? MAX_CURSOR_SIZE : 4096;
  if (w < 1 || h < 1 || w > limit || h > limit || stride < w * 4 ||
      stride > 65536 ||
      (format != WL_SHM_FORMAT_ARGB8888 && format != WL_SHM_FORMAT_XRGB8888) ||
      w % v->pending_scale || h % v->pending_scale) {
    wl_resource_post_error(v->resource, WL_SURFACE_ERROR_INVALID_SIZE,
                           "unsupported SHM size, stride, format, or scale");
    return -1;
  }
  size_t size = (size_t)w * h * 4;
  if (v->server->pixels_bytes + size > MAX_PIXELS_BYTES) {
    post_oom(v->resource);
    return -1;
  }
  unsigned char *p = malloc(size);
  if (!p) {
    post_oom(v->resource);
    return -1;
  }
  int incremental = v->pixels && !v->gpu && v->width == w && v->height == h &&
                    v->pixels_size == size && v->opaque ==
                        (format == WL_SHM_FORMAT_XRGB8888) &&
                    v->attach_x == 0 && v->attach_y == 0 &&
                    (v->pending_buffer_damage.set ||
                     v->pending_surface_damage.set);
  // Surface-space damage with a viewport needs the full surface-to-buffer
  // transform. Keep that uncommon case conservative while buffer-space damage
  // remains incremental under viewports.
  if (v->pending_surface_damage.set &&
      (v->viewport_source || v->viewport_destination ||
       v->pending_viewport_source || v->pending_viewport_destination))
    incremental = 0;
  int64_t left = 0, top = 0, right = w, bottom = h;
  if (incremental) {
    memcpy(p, v->pixels, size);
    left = w;
    top = h;
    right = bottom = 0;
#define INCLUDE_DAMAGE(box, factor)                                             \
  do {                                                                           \
    if ((box).set) {                                                             \
      int64_t bx1 = (box).x1 * (factor), by1 = (box).y1 * (factor);              \
      int64_t bx2 = (box).x2 * (factor), by2 = (box).y2 * (factor);              \
      if (bx1 < left)                                                            \
        left = bx1;                                                              \
      if (by1 < top)                                                             \
        top = by1;                                                               \
      if (bx2 > right)                                                           \
        right = bx2;                                                             \
      if (by2 > bottom)                                                          \
        bottom = by2;                                                            \
    }                                                                            \
  } while (0)
    INCLUDE_DAMAGE(v->pending_buffer_damage, 1);
    INCLUDE_DAMAGE(v->pending_surface_damage, v->pending_scale);
#undef INCLUDE_DAMAGE
    if (left < 0)
      left = 0;
    if (top < 0)
      top = 0;
    if (right > w)
      right = w;
    if (bottom > h)
      bottom = h;
  }
  wl_shm_buffer_begin_access(shm);
  const unsigned char *src = wl_shm_buffer_get_data(shm);
  for (int y = (int)top; y < (int)bottom; y++) {
    const unsigned char *row = src + (size_t)y * stride;
    for (int x = (int)left; x < (int)right; x++) {
      uint32_t q;
      memcpy(&q, row + (size_t)x * 4, sizeof(q));
      size_t off = ((size_t)y * w + x) * 4;
      p[off] = (q >> 16) & 255;
      p[off + 1] = (q >> 8) & 255;
      p[off + 2] = q & 255;
      p[off + 3] = format == WL_SHM_FORMAT_ARGB8888 ? (q >> 24) : 255;
    }
  }
  wl_shm_buffer_end_access(shm);
  struct app_image *image =
      image_create(v->server, p, size, w, h, 0,
                   format == WL_SHM_FORMAT_XRGB8888, ++v->server->next_image);
  if (!image) {
    free(p);
    post_oom(v->resource);
    return -1;
  }
  image_assign(v, image);
  return 0;
}
#include "subsurface_state.inc"
static void surface_commit(struct wl_client *c, struct wl_resource *r) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if ((v->toplevel || v->popup) && !v->configured) {
    if (v->pending_buffer) {
      wl_resource_post_error(
          v->xdg, XDG_SURFACE_ERROR_UNCONFIGURED_BUFFER,
          "initial configure must be acknowledged before attaching a buffer");
      return;
    }
    configure(v);
  }
  if (v->pending_buffer && (v->toplevel || v->popup) && !v->acked) {
    wl_resource_post_error(v->xdg, XDG_SURFACE_ERROR_UNCONFIGURED_BUFFER,
                           "configure must be acknowledged before mapping");
    return;
  }
  struct app_update *update = create_update(v);
  memset(&v->pending_surface_damage, 0, sizeof(v->pending_surface_damage));
  memset(&v->pending_buffer_damage, 0, sizeof(v->pending_buffer_damage));
  if (!update)
    return;
  update_release(v->latest);
  v->latest = update;
  if (v->pending_attach) {
    if (v->server->cursor_surface == v) {
      int64_t x = (int64_t)v->server->cursor_x - v->attach_x,
              y = (int64_t)v->server->cursor_y - v->attach_y;
      v->server->cursor_x = x < INT32_MIN   ? INT32_MIN
                            : x > INT32_MAX ? INT32_MAX
                                            : (int)x;
      v->server->cursor_y = y < INT32_MIN   ? INT32_MIN
                            : y > INT32_MAX ? INT32_MAX
                                            : (int)y;
    }
    if (v->server->drag_icon == v) {
      int64_t x = (int64_t)v->server->drag_icon_x + v->attach_x,
              y = (int64_t)v->server->drag_icon_y + v->attach_y;
      v->server->drag_icon_x = x < INT32_MIN   ? INT32_MIN
                               : x > INT32_MAX ? INT32_MAX
                                               : (int)x;
      v->server->drag_icon_y = y < INT32_MIN   ? INT32_MIN
                               : y > INT32_MAX ? INT32_MAX
                                               : (int)y;
    }
    v->pending_attach = 0;
  }
  if (!effectively_synchronized(v)) {
    apply_update(v, update);
    changed(v);
  }
}
static const struct wl_surface_interface surface_impl = {
    .destroy = destroy_request,
    .attach = surface_attach,
    .damage = surface_damage,
    .frame = surface_frame,
    .set_opaque_region = surface_region,
    .set_input_region = surface_input_region,
    .commit = surface_commit,
    .set_buffer_transform = surface_transform,
    .set_buffer_scale = surface_scale,
    .damage_buffer = surface_damage_buffer};
static void region_add(struct wl_client *c, struct wl_resource *r, int32_t x,
                       int32_t y, int32_t w, int32_t h) {
  (void)c;
  if (w <= 0 || h <= 0)
    return;
  struct app_region *region = wl_resource_get_user_data(r);
  if (region->shape.count == 64) {
    post_oom(r);
    return;
  }
  int n = region->shape.count++;
  region->shape.operations[n].x = x;
  region->shape.operations[n].y = y;
  region->shape.operations[n].width = w;
  region->shape.operations[n].height = h;
  region->shape.operations[n].add = 1;
}
static void region_subtract(struct wl_client *c, struct wl_resource *r,
                            int32_t x, int32_t y, int32_t w, int32_t h) {
  struct app_region *region = wl_resource_get_user_data(r);
  int before = region->shape.count;
  region_add(c, r, x, y, w, h);
  if (region->shape.count > before)
    region->shape.operations[before].add = 0;
}
static void region_destroy(struct wl_resource *r) {
  struct app_region *region = wl_resource_get_user_data(r);
  region->server->region_count--;
  free(region);
}
static const struct wl_region_interface region_impl = {
    .destroy = destroy_request, .add = region_add, .subtract = region_subtract};
static void create_surface(struct wl_client *c, struct wl_resource *r,
                           uint32_t id) {
  struct worldr_apps *s = wl_resource_get_user_data(r);
  if (s->surface_count >= MAX_SURFACES) {
    post_oom(r);
    return;
  }
  struct app_surface *v = calloc(1, sizeof(*v));
  if (!v) {
    post_oom(r);
    return;
  }
  v->resource = wl_resource_create(c, &wl_surface_interface,
                                   wl_resource_get_version(r), id);
  if (!v->resource) {
    free(v);
    post_oom(r);
    return;
  }
  v->server = s;
  v->id = ++s->next_id;
  pid_t pid = 0;
  wl_client_get_credentials(c, &pid, NULL, NULL);
  v->pid = pid > 0 ? (uint32_t)pid : 0;
  v->scale = v->pending_scale = 1;
  v->input.infinite = v->pending_input.infinite = 1;
  v->requested_width = s->width;
  v->requested_height = s->height;
  v->title = strdup("");
  wl_list_init(&v->buffer_destroy.link);
  wl_list_init(&v->frames);
  wl_list_insert(s->surfaces.prev, &v->link);
  s->surface_count++;
  wl_resource_set_implementation(v->resource, &surface_impl, v,
                                 surface_destroy);
}
static void create_region(struct wl_client *c, struct wl_resource *r,
                          uint32_t id) {
  struct worldr_apps *s = wl_resource_get_user_data(r);
  if (s->region_count >= 256) {
    post_oom(r);
    return;
  }
  struct app_region *region = calloc(1, sizeof(*region));
  if (!region) {
    post_oom(r);
    return;
  }
  struct wl_resource *q = wl_resource_create(c, &wl_region_interface, 1, id);
  if (!q) {
    free(region);
    post_oom(r);
    return;
  }
  region->server = s;
  s->region_count++;
  wl_resource_set_implementation(q, &region_impl, region, region_destroy);
}
static const struct wl_compositor_interface compositor_impl = {
    .create_surface = create_surface, .create_region = create_region};
static void bind_compositor(struct wl_client *c, void *data, uint32_t ver,
                            uint32_t id) {
  struct wl_resource *r =
      wl_resource_create(c, &wl_compositor_interface, ver < 4 ? ver : 4, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &compositor_impl, data, NULL);
}

static void subsurface_destroy(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) {
    changed(v);
    apply_unmap(v);
    v->subsurface = NULL;
    v->parent = NULL;
    v->placed = 0;
  }
}
static void sub_position(struct wl_client *c, struct wl_resource *r, int32_t x,
                         int32_t y) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) {
    v->pending_x = x;
    v->pending_y = y;
  }
}
static void sub_place(struct wl_client *c, struct wl_resource *r,
                      struct wl_resource *sibling, int above) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  struct app_surface *b = wl_resource_get_user_data(sibling);
  if (v && (b == v || (b != v->parent && (!b || b->parent != v->parent)))) {
    wl_resource_post_error(r, WL_SUBSURFACE_ERROR_BAD_SURFACE,
                           "sibling is not in the same surface tree");
    return;
  }
  if (!v || !b)
    return;
  if (b == v->parent) {
    v->pending_below_parent = !above;
    struct app_surface *other, *anchor = NULL;
    wl_list_for_each(other, &v->server->surfaces, link) {
      if (other == v || other->parent != v->parent || !other->subsurface ||
          other->pending_below_parent != !above)
        continue;
      anchor = other;
      if (above)
        break;
    }
    if (anchor) {
      wl_list_remove(&v->link);
      wl_list_insert(above ? anchor->link.prev : &anchor->link, &v->link);
    }
  } else {
    v->pending_below_parent = b->pending_below_parent;
    wl_list_remove(&v->link);
    wl_list_insert(above ? &b->link : b->link.prev, &v->link);
  }
}
static void sub_above(struct wl_client *c, struct wl_resource *r,
                      struct wl_resource *sibling) {
  sub_place(c, r, sibling, 1);
}
static void sub_below(struct wl_client *c, struct wl_resource *r,
                      struct wl_resource *sibling) {
  sub_place(c, r, sibling, 0);
}
static void sub_sync(struct wl_client *c, struct wl_resource *r) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v)
    v->synchronized = 1;
}
static void sub_desync(struct wl_client *c, struct wl_resource *r) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) {
    v->synchronized = 0;
    struct app_surface *child;
    wl_list_for_each(child, &v->server->surfaces,
                     link) if (descendant(child, v) &&
                               !effectively_synchronized(child))
        apply_update(child, child->latest);
    changed(v);
  }
}
static const struct wl_subsurface_interface subsurface_impl = {
    .destroy = destroy_request,
    .set_position = sub_position,
    .place_above = sub_above,
    .place_below = sub_below,
    .set_sync = sub_sync,
    .set_desync = sub_desync};
static void get_subsurface(struct wl_client *c, struct wl_resource *r,
                           uint32_t id, struct wl_resource *surface,
                           struct wl_resource *parent) {
  struct app_surface *v = wl_resource_get_user_data(surface),
                     *p = wl_resource_get_user_data(parent);
  if (!v || !p || v == p || v->xdg || v->subsurface ||
      (v->role && v->role != ROLE_SUBSURFACE) || root_surface(p) == v) {
    wl_resource_post_error(r, WL_SUBCOMPOSITOR_ERROR_BAD_SURFACE,
                           "invalid subsurface parent or existing role");
    return;
  }
  v->subsurface = wl_resource_create(c, &wl_subsurface_interface, 1, id);
  if (!v->subsurface) {
    post_oom(r);
    return;
  }
  v->parent = p;
  v->placed = 0;
  v->association = ++v->server->next_association;
  v->role = ROLE_SUBSURFACE;
  v->synchronized = 1;
  wl_resource_set_implementation(v->subsurface, &subsurface_impl, v,
                                 subsurface_destroy);
}
static const struct wl_subcompositor_interface subcompositor_impl = {
    .destroy = destroy_request, .get_subsurface = get_subsurface};
static void bind_subcompositor(struct wl_client *c, void *data, uint32_t ver,
                               uint32_t id) {
  (void)ver;
  struct wl_resource *r =
      wl_resource_create(c, &wl_subcompositor_interface, 1, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &subcompositor_impl, data, NULL);
}

static void top_destroy(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) {
    dismiss_surface_tree(v);
    if (v->server->keyboard_focus == v)
      worldr_apps_focus(v->server, 0);
    if (v->server->pointer_focus == v)
      worldr_apps_pointer(v->server, 0, 0, 0);
    v->toplevel = NULL;
    v->configured = v->acked = 0;
  }
}
static void top_parent(struct wl_client *c, struct wl_resource *r,
                       struct wl_resource *p) {
  (void)c;
  (void)r;
  (void)p;
}
static void top_title(struct wl_client *c, struct wl_resource *r,
                      const char *title) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) {
    char *p = strndup(title, 4096);
    if (!p) {
      post_oom(r);
      return;
    }
    free(v->title);
    v->title = p;
  }
}
static void top_appid(struct wl_client *c, struct wl_resource *r,
                      const char *id) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) {
    char *p = strndup(id, 4096);
    if (!p) {
      post_oom(r);
      return;
    }
    free(v->app_id);
    v->app_id = p;
  }
}
static void top_menu(struct wl_client *c, struct wl_resource *r,
                     struct wl_resource *seat, uint32_t serial, int32_t x,
                     int32_t y) {
  (void)c;
  (void)r;
  (void)seat;
  (void)serial;
  (void)x;
  (void)y;
}
static void top_move(struct wl_client *c, struct wl_resource *r,
                     struct wl_resource *seat, uint32_t serial) {
  (void)c;
  (void)r;
  (void)seat;
  (void)serial;
}
static void top_resize(struct wl_client *c, struct wl_resource *r,
                       struct wl_resource *seat, uint32_t serial,
                       uint32_t edges) {
  (void)c;
  (void)r;
  (void)seat;
  (void)serial;
  (void)edges;
}
static void top_size(struct wl_client *c, struct wl_resource *r, int32_t w,
                     int32_t h) {
  (void)c;
  if (w < 0 || h < 0)
    wl_resource_post_error(r, XDG_TOPLEVEL_ERROR_INVALID_SIZE, "negative size");
}
static void top_state(struct wl_client *c, struct wl_resource *r) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v)
    configure(v);
}
static void top_fullscreen(struct wl_client *c, struct wl_resource *r,
                           struct wl_resource *output) {
  (void)output;
  top_state(c, r);
}
static const struct xdg_toplevel_interface top_impl = {
    .destroy = destroy_request,
    .set_parent = top_parent,
    .set_title = top_title,
    .set_app_id = top_appid,
    .show_window_menu = top_menu,
    .move = top_move,
    .resize = top_resize,
    .set_max_size = top_size,
    .set_min_size = top_size,
    .set_maximized = top_state,
    .unset_maximized = top_state,
    .set_fullscreen = top_fullscreen,
    .unset_fullscreen = top_state,
    .set_minimized = top_state};
static void xdg_destroy(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v)
    v->xdg = NULL;
}
static void get_toplevel(struct wl_client *c, struct wl_resource *r,
                         uint32_t id) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (!v)
    return;
  if (v->toplevel || v->subsurface || v->popup) {
    wl_resource_post_error(r, XDG_SURFACE_ERROR_ALREADY_CONSTRUCTED,
                           "surface already has a role");
    return;
  }
  int tops = 0;
  struct app_surface *q;
  wl_list_for_each(q, &v->server->surfaces,
                   link) if (q->toplevel || q->x11_mapped) tops++;
  if (tops >= 32) {
    post_oom(r);
    return;
  }
  v->toplevel = wl_resource_create(c, &xdg_toplevel_interface,
                                   wl_resource_get_version(r), id);
  if (!v->toplevel) {
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(v->toplevel, &top_impl, v, top_destroy);
}
static void popup_destroy(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (!v)
    return;
  v->popup = NULL;
  dismiss_popup(v);
}
static void dismiss_popup(struct app_surface *v) {
  if (!v || v->popup_dismissed)
    return;
  struct worldr_apps *s = v->server;
  struct app_surface *child;
  wl_list_for_each_reverse(child, &s->surfaces,
                           link) if (child->parent == v && child->popup)
      dismiss_popup(child);
  v->popup_dismissed = 1;
  if (v->popup)
    xdg_popup_send_popup_done(v->popup);
  if (s->popup_grab == v) {
    s->popup_grab = NULL;
    for (struct app_surface *p = v->parent; p; p = p->parent)
      if (p->popup && p->popup_grabbed && !p->popup_dismissed) {
        s->popup_grab = p;
        break;
      }
  }
  if (descendant(s->pointer_focus, v))
    worldr_apps_pointer(s, 0, 0, 0);
  if (descendant(s->keyboard_focus, v)) {
    struct app_surface *next = s->popup_grab ? s->popup_grab : root_surface(v);
    if (next == v || !has_buffer(next) || next->popup_dismissed)
      next = NULL;
    focus_surface(s, next);
  }
  changed(v);
}
static void dismiss_popups(struct worldr_apps *s) {
  while (s->popup_grab)
    dismiss_popup(s->popup_grab);
}
static void dismiss_surface_tree(struct app_surface *v) {
  struct worldr_apps *s = v->server;
  struct app_surface *child;
  wl_list_for_each_reverse(child, &s->surfaces,
                           link) if (child->popup && descendant(child, v))
      dismiss_popup(child);
  if (descendant(s->pointer_focus, v))
    worldr_apps_pointer(s, 0, 0, 0);
  if (descendant(s->keyboard_focus, v))
    focus_surface(s, NULL);
}
static void popup_grab(struct wl_client *c, struct wl_resource *r,
                       struct wl_resource *seat, uint32_t serial) {
  (void)seat;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (!v)
    return;
  struct worldr_apps *s = v->server;
  if (has_buffer(v) || !v->parent ||
      (v->parent->popup && !v->parent->popup_grabbed)) {
    wl_resource_post_error(
        r, XDG_POPUP_ERROR_INVALID_GRAB,
        "popup grab requires an unmapped popup and grabbing parent");
    return;
  }
  if (!serial || !valid_grab_serial(s, v, serial) || !s->keyboard_focus ||
      wl_resource_get_client(s->keyboard_focus->resource) != c ||
      (s->popup_grab && v->parent != s->popup_grab) ||
      v->parent->popup_dismissed) {
    dismiss_popup(v);
    return;
  }
  v->popup_grabbed = 1;
  s->popup_grab = v;
}
static int direction_x(uint32_t value) {
  switch (value) {
  case 3:
  case 5:
  case 6:
    return -1;
  case 4:
  case 7:
  case 8:
    return 1;
  default:
    return 0;
  }
}
static int direction_y(uint32_t value) {
  switch (value) {
  case 1:
  case 5:
  case 7:
    return -1;
  case 2:
  case 6:
  case 8:
    return 1;
  default:
    return 0;
  }
}
static int position_axis(int start, int extent, int anchor, int gravity,
                         int size, int offset) {
  return start +
         (anchor < 0   ? 0
          : anchor > 0 ? extent
                       : extent / 2) +
         (gravity < 0   ? -size
          : gravity > 0 ? 0
                        : -size / 2) +
         offset;
}
static int constrain_axis(int value, int flipped, int *size, int low, int high,
                          uint32_t constraints, int flip, int slide,
                          int resize) {
  if (value >= low && value + *size <= high)
    return value;
  if ((constraints & flip) && flipped >= low && flipped + *size <= high)
    value = flipped;
  if (constraints & slide) {
    if (value + *size > high)
      value = high - *size;
    if (value < low)
      value = low;
  }
  if (constraints & resize) {
    int end = value + *size;
    if (end > high)
      end = high;
    if (value < low)
      value = low;
    if (end > value)
      *size = end - value;
  }
  return value;
}
static void popup_geometry(struct app_surface *v, struct app_positioner *p,
                           int *out_x, int *out_y, int *out_w, int *out_h) {
  int ax = direction_x(p->anchor), ay = direction_y(p->anchor),
      gx = direction_x(p->gravity), gy = direction_y(p->gravity);
  int x = position_axis(p->anchor_x, p->anchor_width, ax, gx, p->width,
                        p->offset_x);
  int y = position_axis(p->anchor_y, p->anchor_height, ay, gy, p->height,
                        p->offset_y);
  int fx = position_axis(p->anchor_x, p->anchor_width, -ax, -gx, p->width,
                         p->offset_x);
  int fy = position_axis(p->anchor_y, p->anchor_height, -ay, -gy, p->height,
                         p->offset_y);
  int ox = v->parent->geometry_x, oy = v->parent->geometry_y;
  for (struct app_surface *q = v->parent; q->parent; q = q->parent) {
    ox += q->x;
    oy += q->y;
  }
  struct app_surface *root = root_surface(v);
  int w = p->width, h = p->height;
  int root_width = scale_logical_width(root);
  int root_height = scale_logical_height(root);
  if (p->parent_size_set && v->parent == root) {
    root_width = p->parent_width;
    root_height = p->parent_height;
  }
  x = constrain_axis(x, fx, &w, -ox, root_width - ox,
                     p->constraints, 4, 1, 16);
  y = constrain_axis(y, fy, &h, -oy, root_height - oy,
                     p->constraints, 8, 2, 32);
  *out_x = x;
  *out_y = y;
  *out_w = w;
  *out_h = h;
}
static void position_popup(struct app_surface *v, struct app_positioner *p) {
  popup_geometry(v, p, &v->popup_x, &v->popup_y, &v->popup_width,
                 &v->popup_height);
  v->x = v->pending_x = v->parent->geometry_x + v->popup_x;
  v->y = v->pending_y = v->parent->geometry_y + v->popup_y;
  v->popup_positioner = *p;
  // Parent hints describe the state this placement targets. Reactive updates
  // after that placement use the parent's actual committed dimensions.
  v->popup_positioner.parent_size_set = 0;
  v->popup_positioner.parent_configure_set = 0;
  v->popup_positioner_set = 1;
}
static int complete_positioner(struct app_positioner *p) {
  return p && p->width > 0 && p->height > 0 && p->anchor_width > 0 &&
         p->anchor_height > 0;
}
static int positioner_fits_parent(struct app_surface *parent,
                                  struct app_positioner *p) {
  if (!complete_positioner(p) || !parent)
    return 0;
  int width = parent->geometry_width > 0 ? parent->geometry_width
                                         : scale_logical_width(parent);
  int height = parent->geometry_height > 0 ? parent->geometry_height
                                            : scale_logical_height(parent);
  if (p->parent_size_set) {
    width = p->parent_width;
    height = p->parent_height;
  }
  return p->anchor_x >= 0 && p->anchor_y >= 0 &&
         (int64_t)p->anchor_x + p->anchor_width <= width &&
         (int64_t)p->anchor_y + p->anchor_height <= height;
}
static void configure_popup_position(struct app_surface *v, int x, int y,
                                     int width, int height, uint32_t token,
                                     int repositioned) {
  if (repositioned)
    xdg_popup_send_repositioned(v->popup, token);
  xdg_popup_send_configure(v->popup, x, y, width, height);
  v->pending_popup_serial = wl_display_next_serial(v->server->display);
  xdg_surface_send_configure(v->xdg, v->pending_popup_serial);
  v->configure_serial = v->pending_popup_serial;
  v->pending_popup_x = x;
  v->pending_popup_y = y;
  v->pending_popup_width = width;
  v->pending_popup_height = height;
  v->pending_popup_position = 1;
  v->configured = 1;
}
static void popup_reposition(struct wl_client *c, struct wl_resource *r,
                             struct wl_resource *positioner, uint32_t token) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  struct app_positioner *p = wl_resource_get_user_data(positioner);
  if (!v || v->popup_dismissed)
    return;
  if (!positioner_fits_parent(v->parent, p)) {
    wl_resource_post_error(p->wm, XDG_WM_BASE_ERROR_INVALID_POSITIONER,
                           "popup reposition requires a complete positioner");
    return;
  }
  int x, y, width, height;
  popup_geometry(v, p, &x, &y, &width, &height);
  v->popup_positioner = *p;
  v->popup_positioner.parent_size_set = 0;
  v->popup_positioner.parent_configure_set = 0;
  v->popup_positioner_set = 1;
  configure_popup_position(v, x, y, width, height, token, 1);
}
static const struct xdg_popup_interface popup_impl = {
    .destroy = destroy_request,
    .grab = popup_grab,
    .reposition = popup_reposition};
static void reactive_popups_changed(struct app_surface *ancestor) {
  if (!ancestor)
    return;
  struct app_surface *v;
  wl_list_for_each(v, &ancestor->server->surfaces, link) {
    if (!v->popup || v->popup_dismissed || !v->configured ||
        !v->popup_positioner_set || !v->popup_positioner.reactive ||
        !descendant(v->parent, ancestor))
      continue;
    int x, y, width, height;
    popup_geometry(v, &v->popup_positioner, &x, &y, &width, &height);
    int old_x = v->pending_popup_position ? v->pending_popup_x : v->popup_x;
    int old_y = v->pending_popup_position ? v->pending_popup_y : v->popup_y;
    int old_width =
        v->pending_popup_position ? v->pending_popup_width : v->popup_width;
    int old_height =
        v->pending_popup_position ? v->pending_popup_height : v->popup_height;
    if (x == old_x && y == old_y && width == old_width &&
        height == old_height)
      continue;
    configure_popup_position(v, x, y, width, height, 0, 0);
  }
}
static void get_popup(struct wl_client *c, struct wl_resource *r, uint32_t id,
                      struct wl_resource *parent,
                      struct wl_resource *positioner) {
  struct app_surface *v = wl_resource_get_user_data(r);
  struct app_surface *p = parent ? wl_resource_get_user_data(parent) : NULL;
  struct app_positioner *pos = wl_resource_get_user_data(positioner);
  if (!v)
    return;
  if (v->toplevel || v->subsurface || v->popup) {
    wl_resource_post_error(r, XDG_SURFACE_ERROR_ALREADY_CONSTRUCTED,
                           "surface already has a role");
    return;
  }
  if (!pos) {
    wl_resource_post_error(r, XDG_SURFACE_ERROR_NOT_CONSTRUCTED,
                           "popup requires a live positioner");
    return;
  }
  if (!p || (!p->toplevel && !p->popup) || p == v) {
    wl_resource_post_error(pos->wm, XDG_WM_BASE_ERROR_INVALID_POPUP_PARENT,
                           "popup requires a live xdg parent");
    return;
  }
  if (!positioner_fits_parent(p, pos)) {
    wl_resource_post_error(pos->wm, XDG_WM_BASE_ERROR_INVALID_POSITIONER,
                           "popup requires a complete positioner");
    return;
  }
  v->popup = wl_resource_create(c, &xdg_popup_interface,
                                wl_resource_get_version(r), id);
  if (!v->popup) {
    post_oom(r);
    return;
  }
  v->parent = p;
  position_popup(v, pos);
  wl_resource_set_implementation(v->popup, &popup_impl, v, popup_destroy);
}
static void geometry(struct wl_client *c, struct wl_resource *r, int32_t x,
                     int32_t y, int32_t w, int32_t h) {
  (void)c;
  if (w <= 0 || h <= 0 || w > 4096 || h > 4096 || x < -4096 || x > 4096 ||
      y < -4096 || y > 4096) {
    wl_resource_post_error(r, XDG_SURFACE_ERROR_INVALID_SIZE,
                           "invalid window geometry");
    return;
  }
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) {
    v->pending_geometry_x = x;
    v->pending_geometry_y = y;
    v->pending_geometry_width = w;
    v->pending_geometry_height = h;
  }
}
static void ack_configure(struct wl_client *c, struct wl_resource *r,
                          uint32_t serial) {
  (void)c;
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v && v->configured && serial <= v->configure_serial)
    v->acked = 1;
  if (v && v->pending_popup_position && serial == v->pending_popup_serial) {
    v->popup_x = v->pending_popup_x;
    v->popup_y = v->pending_popup_y;
    v->popup_width = v->pending_popup_width;
    v->popup_height = v->pending_popup_height;
    v->pending_popup_position = 0;
    if (v->parent) {
      v->x = v->parent->geometry_x + v->popup_x - v->geometry_x;
      v->y = v->parent->geometry_y + v->popup_y - v->geometry_y;
    }
    changed(v);
    reactive_popups_changed(v);
  }
}
static const struct xdg_surface_interface xdg_impl = {
    .destroy = destroy_request,
    .get_toplevel = get_toplevel,
    .get_popup = get_popup,
    .set_window_geometry = geometry,
    .ack_configure = ack_configure};
static void positioner_destroy(struct wl_resource *r) {
  free(wl_resource_get_user_data(r));
}
static void positioner_size(struct wl_client *c, struct wl_resource *r,
                            int32_t w, int32_t h) {
  (void)c;
  struct app_positioner *p = wl_resource_get_user_data(r);
  if (w <= 0 || h <= 0 || w > 4096 || h > 4096) {
    wl_resource_post_error(r, XDG_POSITIONER_ERROR_INVALID_INPUT,
                           "invalid size");
    return;
  }
  p->width = w;
  p->height = h;
}
static void positioner_rect(struct wl_client *c, struct wl_resource *r,
                            int32_t x, int32_t y, int32_t w, int32_t h) {
  (void)c;
  struct app_positioner *p = wl_resource_get_user_data(r);
  if (w <= 0 || h <= 0 || w > 4096 || h > 4096 || x < -32768 || x > 32768 ||
      y < -32768 || y > 32768) {
    wl_resource_post_error(r, XDG_POSITIONER_ERROR_INVALID_INPUT,
                           "invalid anchor rectangle");
    return;
  }
  p->anchor_x = x;
  p->anchor_y = y;
  p->anchor_width = w;
  p->anchor_height = h;
}
static void positioner_anchor(struct wl_client *c, struct wl_resource *r,
                              uint32_t value) {
  (void)c;
  if (value > 8) {
    wl_resource_post_error(r, XDG_POSITIONER_ERROR_INVALID_INPUT,
                           "invalid anchor");
    return;
  }
  struct app_positioner *p = wl_resource_get_user_data(r);
  p->anchor = value;
}
static void positioner_gravity(struct wl_client *c, struct wl_resource *r,
                               uint32_t value) {
  (void)c;
  if (value > 8) {
    wl_resource_post_error(r, XDG_POSITIONER_ERROR_INVALID_INPUT,
                           "invalid gravity");
    return;
  }
  struct app_positioner *p = wl_resource_get_user_data(r);
  p->gravity = value;
}
static void positioner_constraints(struct wl_client *c, struct wl_resource *r,
                                   uint32_t value) {
  (void)c;
  if (value & ~63u) {
    wl_resource_post_error(r, XDG_POSITIONER_ERROR_INVALID_INPUT,
                           "invalid constraints");
    return;
  }
  struct app_positioner *p = wl_resource_get_user_data(r);
  p->constraints = value;
}
static void positioner_offset(struct wl_client *c, struct wl_resource *r,
                              int32_t x, int32_t y) {
  (void)c;
  if (x < -32768 || x > 32768 || y < -32768 || y > 32768) {
    wl_resource_post_error(r, XDG_POSITIONER_ERROR_INVALID_INPUT,
                           "invalid offset");
    return;
  }
  struct app_positioner *p = wl_resource_get_user_data(r);
  p->offset_x = x;
  p->offset_y = y;
}
static void positioner_reactive(struct wl_client *c, struct wl_resource *r) {
  (void)c;
  struct app_positioner *p = wl_resource_get_user_data(r);
  p->reactive = 1;
}
static void positioner_parent_size(struct wl_client *c, struct wl_resource *r,
                                   int32_t width, int32_t height) {
  (void)c;
  if (width <= 0 || height <= 0 || width > 4096 || height > 4096) {
    wl_resource_post_error(r, XDG_POSITIONER_ERROR_INVALID_INPUT,
                           "invalid parent size");
    return;
  }
  struct app_positioner *p = wl_resource_get_user_data(r);
  p->parent_width = width;
  p->parent_height = height;
  p->parent_size_set = 1;
}
static void positioner_parent_configure(struct wl_client *c,
                                        struct wl_resource *r,
                                        uint32_t serial) {
  (void)c;
  struct app_positioner *p = wl_resource_get_user_data(r);
  p->parent_configure = serial;
  p->parent_configure_set = 1;
}
static const struct xdg_positioner_interface positioner_impl = {
    .destroy = destroy_request,
    .set_size = positioner_size,
    .set_anchor_rect = positioner_rect,
    .set_anchor = positioner_anchor,
    .set_gravity = positioner_gravity,
    .set_constraint_adjustment = positioner_constraints,
    .set_offset = positioner_offset,
    .set_reactive = positioner_reactive,
    .set_parent_size = positioner_parent_size,
    .set_parent_configure = positioner_parent_configure};
static void create_positioner(struct wl_client *c, struct wl_resource *r,
                              uint32_t id) {
  struct app_positioner *p = calloc(1, sizeof(*p));
  if (!p) {
    post_oom(r);
    return;
  }
  p->wm = r;
  struct wl_resource *q = wl_resource_create(
      c, &xdg_positioner_interface, wl_resource_get_version(r), id);
  if (!q) {
    free(p);
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(q, &positioner_impl, p, positioner_destroy);
}
static void get_xdg_surface(struct wl_client *c, struct wl_resource *r,
                            uint32_t id, struct wl_resource *surface) {
  struct app_surface *v = wl_resource_get_user_data(surface);
  if (v->xdg || v->subsurface || (v->role && v->role != ROLE_XDG)) {
    wl_resource_post_error(r, XDG_WM_BASE_ERROR_ROLE,
                           "surface already has a role");
    return;
  }
  v->xdg = wl_resource_create(c, &xdg_surface_interface,
                              wl_resource_get_version(r), id);
  if (!v->xdg) {
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(v->xdg, &xdg_impl, v, xdg_destroy);
  v->role = ROLE_XDG;
}
static void pong(struct wl_client *c, struct wl_resource *r, uint32_t serial) {
  (void)c;
  (void)r;
  (void)serial;
}
static const struct xdg_wm_base_interface wm_impl = {
    .destroy = destroy_request,
    .create_positioner = create_positioner,
    .get_xdg_surface = get_xdg_surface,
    .pong = pong};
static void bind_wm(struct wl_client *c, void *data, uint32_t ver,
                    uint32_t id) {
  struct wl_resource *r = wl_resource_create(
      c, &xdg_wm_base_interface, ver < 3 ? ver : 3, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &wm_impl, data, NULL);
}

static void offer_destroy(struct wl_resource *r) {
  struct app_offer *v = wl_resource_get_user_data(r);
  if (v->server && v->server->drag_offer == v) {
    v->server->drag_offer = NULL;
    if (v->dropped && !v->finished) {
      int legacy = wl_resource_get_version(r) < 3;
      if (legacy && v->source && v->source->resource &&
          wl_resource_get_version(v->source->resource) >= 3)
        wl_data_source_send_dnd_finished(v->source->resource);
      drag_cancel(v->server, !legacy);
    }
  }
  wl_list_remove(&v->link);
  free(v);
}
static int source_has_mime(struct app_source *s, const char *mime) {
  if (!s || !mime)
    return 0;
  for (int i = 0; i < s->mime_count; i++)
    if (!strcmp(s->mimes[i], mime))
      return 1;
  return 0;
}
static void offer_accept(struct wl_client *c, struct wl_resource *r,
                         uint32_t serial, const char *mime) {
  (void)c;
  struct app_offer *v = wl_resource_get_user_data(r);
  if (v->drag) {
    if (!v->server || v->server->drag_offer != v || v->finished ||
        (!v->dropped && serial != v->enter_serial))
      return;
    v->accepted = source_has_mime(v->source, mime);
  }
  if (v->source && v->source->resource)
    wl_data_source_send_target(v->source->resource,
                               source_has_mime(v->source, mime) ? mime : NULL);
}
static void offer_receive(struct wl_client *c, struct wl_resource *r,
                          const char *mime, int32_t fd) {
  (void)c;
  struct app_offer *v = wl_resource_get_user_data(r);
  if (v->drag && (v->finished || !v->server || v->server->drag_offer != v)) {
    close(fd);
    return;
  }
  if (source_has_mime(v->source, mime)) {
    if (v->source->resource) {
      wl_data_source_send_send(v->source->resource, mime, fd);
    } else {
      struct worldr_apps *s = v->source->server;
      if (wl_list_length(&s->clipboard_requests) < 64) {
        struct clipboard_request *q = calloc(1, sizeof(*q));
        if (q) {
          q->request.mime = strdup(mime);
          if (q->request.mime) {
            q->request.fd = fd;
            q->request.external_id = v->source->external_id;
            fcntl(fd, F_SETFD, FD_CLOEXEC);
            wl_list_insert(s->clipboard_requests.prev, &q->link);
            return;
          }
          free(q);
        }
      }
    }
  }
  close(fd);
}
static void offer_finish(struct wl_client *c, struct wl_resource *r) {
  (void)c;
  struct app_offer *v = wl_resource_get_user_data(r);
  if (!v->drag || !v->dropped || !v->accepted || !v->action || v->finished ||
      !v->source || !v->server || v->server->drag_offer != v) {
    wl_resource_post_error(r, WL_DATA_OFFER_ERROR_INVALID_FINISH,
                           "only an accepted dropped drag offer can finish");
    return;
  }
  v->finished = 1;
  if (v->source->resource && wl_resource_get_version(v->source->resource) >= 3)
    wl_data_source_send_dnd_finished(v->source->resource);
  drag_cancel(v->server, 0);
}
static void offer_actions(struct wl_client *c, struct wl_resource *r,
                          uint32_t actions, uint32_t preferred) {
  (void)c;
  struct app_offer *v = wl_resource_get_user_data(r);
  if (!v->drag) {
    wl_resource_post_error(r, WL_DATA_OFFER_ERROR_INVALID_OFFER,
                           "clipboard offers have no drag actions");
    return;
  }
  if (actions & ~7u) {
    wl_resource_post_error(r, WL_DATA_OFFER_ERROR_INVALID_ACTION_MASK,
                           "invalid drag actions");
    return;
  }
  if (preferred && ((preferred & (preferred - 1)) || !(preferred & actions))) {
    wl_resource_post_error(r, WL_DATA_OFFER_ERROR_INVALID_ACTION,
                           "invalid preferred drag action");
    return;
  }
  if (v->finished || !v->server || v->server->drag_offer != v)
    return;
  v->actions = actions;
  v->preferred = preferred;
  drag_action(v);
}
static const struct wl_data_offer_interface offer_impl = {
    .accept = offer_accept,
    .receive = offer_receive,
    .destroy = destroy_request,
    .finish = offer_finish,
    .set_actions = offer_actions};
static void send_selection(struct worldr_apps *s, struct wl_resource *r) {
  if (wl_list_length(&s->offers) >= 512) {
    post_oom(r);
    return;
  }
  if (!s->selection) {
    wl_data_device_send_selection(r, NULL);
    return;
  }
  struct app_offer *v = calloc(1, sizeof(*v));
  if (!v) {
    post_oom(r);
    return;
  }
  v->resource =
      wl_resource_create(wl_resource_get_client(r), &wl_data_offer_interface,
                         wl_resource_get_version(r), 0);
  if (!v->resource) {
    free(v);
    post_oom(r);
    return;
  }
  v->source = s->selection;
  v->server = s;
  wl_list_insert(s->offers.prev, &v->link);
  wl_resource_set_implementation(v->resource, &offer_impl, v, offer_destroy);
  wl_data_device_send_data_offer(r, v->resource);
  for (int i = 0; i < v->source->mime_count; i++)
    wl_data_offer_send_offer(v->resource, v->source->mimes[i]);
  wl_data_device_send_selection(r, v->resource);
}
static void selection_changed(struct worldr_apps *s) {
  struct input_resource *v;
  wl_list_for_each(v, &s->data_devices,
                   link) if (belongs(v->resource, s->keyboard_focus))
      send_selection(s, v->resource);
}
static void source_release(struct app_source *v) {
  if (v->server->drag_source == v)
    drag_cancel(v->server, 0);
  v->server->source_count--;
  struct app_offer *o;
  wl_list_for_each(o, &v->server->offers, link) if (o->source == v) o->source =
      NULL;
  if (v->server->selection == v) {
    v->server->selection = NULL;
    v->server->clipboard_revision++;
    selection_changed(v->server);
  }
  for (int i = 0; i < v->mime_count; i++)
    free(v->mimes[i]);
  free(v);
}
static void source_destroy(struct wl_resource *r) {
  source_release(wl_resource_get_user_data(r));
}
static void replace_selection(struct worldr_apps *s, struct app_source *v) {
  struct app_source *old = s->selection;
  s->selection = NULL;
  if (old && old != v) {
    if (old->resource)
      wl_data_source_send_cancelled(old->resource);
    else
      source_release(old);
  }
  s->selection = v;
  s->clipboard_revision++;
  selection_changed(s);
}
static void source_offer(struct wl_client *c, struct wl_resource *r,
                         const char *mime) {
  (void)c;
  struct app_source *v = wl_resource_get_user_data(r);
  if (v->mime_count >= 64 || strlen(mime) > 4096) {
    post_oom(r);
    return;
  }
  char *m = strdup(mime);
  if (!m) {
    post_oom(r);
    return;
  }
  v->mimes[v->mime_count++] = m;
}
static void source_actions(struct wl_client *c, struct wl_resource *r,
                           uint32_t actions) {
  (void)c;
  struct app_source *v = wl_resource_get_user_data(r);
  if (actions & ~7u) {
    wl_resource_post_error(r, WL_DATA_SOURCE_ERROR_INVALID_ACTION_MASK,
                           "invalid drag actions");
    return;
  }
  if (v->used || v->actions_set) {
    wl_resource_post_error(r, WL_DATA_SOURCE_ERROR_INVALID_SOURCE,
                           "drag actions already set or source used");
    return;
  }
  v->actions = actions;
  v->actions_set = 1;
}
static const struct wl_data_source_interface source_impl = {
    .offer = source_offer,
    .destroy = destroy_request,
    .set_actions = source_actions};
static void create_source(struct wl_client *c, struct wl_resource *r,
                          uint32_t id) {
  struct worldr_apps *s = wl_resource_get_user_data(r);
  if (s->source_count >= 128) {
    post_oom(r);
    return;
  }
  struct app_source *v = calloc(1, sizeof(*v));
  if (!v) {
    post_oom(r);
    return;
  }
  v->server = wl_resource_get_user_data(r);
  v->resource = wl_resource_create(c, &wl_data_source_interface,
                                   wl_resource_get_version(r), id);
  if (!v->resource) {
    free(v);
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(v->resource, &source_impl, v, source_destroy);
  v->id = ++s->next_clipboard_id;
  s->source_count++;
}
static void drag_leave(struct worldr_apps *s) {
  if (s->drag_device)
    wl_data_device_send_leave(s->drag_device);
  if (s->drag_offer && !s->drag_offer->dropped) {
    s->drag_offer->source = NULL;
    s->drag_offer = NULL;
  }
  s->drag_device = NULL;
  s->drag_target = NULL;
}
static void drag_cancel(struct worldr_apps *s, int notify) {
  struct app_source *source = s->drag_source;
  drag_leave(s);
  if (s->drag_offer) {
    s->drag_offer->source = NULL;
    s->drag_offer = NULL;
  }
  s->drag_source = NULL;
  s->drag_origin = s->drag_icon = NULL;
  s->drag_active = 0;
  s->cursor_revision++;
  if (notify && source && source->resource)
    wl_data_source_send_cancelled(source->resource);
}
static void drag_action(struct app_offer *offer) {
  struct app_source *source = offer->source;
  if (!source)
    return;
  uint32_t supported =
      (source->actions_set ? source->actions : 1u) & offer->actions;
  // COPY and MOVE are fully negotiated. ASK requires an action-picker UI,
  // which this adapter does not claim; a source offering only ASK is rejected.
  supported &= 3u;
  uint32_t action = supported & offer->preferred;
  if (!action)
    action = supported & 1u ? 1u : supported & 2u;
  if (action == offer->action)
    return;
  offer->action = action;
  if (wl_resource_get_version(offer->resource) >= 3)
    wl_data_offer_send_action(offer->resource, action);
  if (source->resource && wl_resource_get_version(source->resource) >= 3)
    wl_data_source_send_action(source->resource, action);
}
static void drag_motion(struct worldr_apps *s, struct app_surface *v, float x,
                        float y) {
  if (!s->drag_active)
    return;
  if (v && !s->drag_source &&
      wl_resource_get_client(v->resource) !=
          wl_resource_get_client(s->drag_origin->resource))
    v = NULL;
  if (v != s->drag_target) {
    drag_leave(s);
    struct input_resource *device;
    wl_list_for_each(device, &s->data_devices, link) {
      if (!belongs(device->resource, v))
        continue;
      s->drag_device = device->resource;
      break;
    }
    if (!s->drag_device)
      return;
    s->drag_target = v;
    uint32_t serial = wl_display_next_serial(s->display);
    if (s->drag_source) {
      if (wl_list_length(&s->offers) >= 512) {
        post_oom(s->drag_device);
        drag_cancel(s, 1);
        return;
      }
      struct app_offer *offer = calloc(1, sizeof(*offer));
      if (!offer) {
        post_oom(s->drag_device);
        drag_cancel(s, 1);
        return;
      }
      offer->resource = wl_resource_create(
          wl_resource_get_client(s->drag_device), &wl_data_offer_interface,
          wl_resource_get_version(s->drag_device), 0);
      if (!offer->resource) {
        free(offer);
        post_oom(s->drag_device);
        drag_cancel(s, 1);
        return;
      }
      offer->server = s;
      offer->source = s->drag_source;
      offer->drag = 1;
      offer->enter_serial = serial;
      offer->actions = wl_resource_get_version(offer->resource) < 3 ? 1u : 0u;
      s->drag_offer = offer;
      wl_list_insert(s->offers.prev, &offer->link);
      wl_resource_set_implementation(offer->resource, &offer_impl, offer,
                                     offer_destroy);
      wl_data_device_send_data_offer(s->drag_device, offer->resource);
      for (int i = 0; i < offer->source->mime_count; i++)
        wl_data_offer_send_offer(offer->resource, offer->source->mimes[i]);
      if (wl_resource_get_version(offer->resource) >= 3)
        wl_data_offer_send_source_actions(
            offer->resource,
            offer->source->actions_set ? offer->source->actions : 1u);
      drag_action(offer);
    }
    wl_data_device_send_enter(s->drag_device, serial, v->resource,
                              wl_fixed_from_double(x), wl_fixed_from_double(y),
                              s->drag_offer ? s->drag_offer->resource : NULL);
  }
  if (s->drag_device)
    wl_data_device_send_motion(s->drag_device, now_ms(),
                               wl_fixed_from_double(x),
                               wl_fixed_from_double(y));
}
static void drag_drop(struct worldr_apps *s) {
  struct app_offer *offer = s->drag_offer;
  if (s->drag_device &&
      ((!s->drag_source) || (offer && offer->accepted && offer->action))) {
    wl_data_device_send_drop(s->drag_device);
    if (offer) {
      offer->dropped = 1;
      if (offer->source->resource &&
          wl_resource_get_version(offer->source->resource) >= 3)
        wl_data_source_send_dnd_drop_performed(offer->source->resource);
    }
    s->drag_active = 0;
    s->drag_icon = NULL;
    s->cursor_revision++;
    if (!offer)
      drag_cancel(s, 0);
  } else {
    drag_cancel(s, 1);
  }
}
static void device_drag(struct wl_client *c, struct wl_resource *r,
                        struct wl_resource *source, struct wl_resource *origin,
                        struct wl_resource *icon, uint32_t serial) {
  struct input_resource *device = wl_resource_get_user_data(r);
  struct worldr_apps *s = device->server;
  struct app_surface *v = wl_resource_get_user_data(origin);
  struct app_source *data = source ? wl_resource_get_user_data(source) : NULL;
  if (data && data->used) {
    wl_resource_post_error(source, WL_DATA_SOURCE_ERROR_INVALID_SOURCE,
                           "drag source already used");
    return;
  }
  size_t button = 0;
  while (button < s->button_count && s->button_serials[button] != serial)
    button++;
  if (!v || v != s->pointer_focus || wl_resource_get_client(origin) != c ||
      button == s->button_count) {
    if (source)
      wl_data_source_send_cancelled(source);
    return;
  }
  struct app_surface *picture = icon ? wl_resource_get_user_data(icon) : NULL;
  if (icon && (!picture || (picture->role != ROLE_NONE &&
                            picture->role != ROLE_DRAG_ICON))) {
    wl_resource_post_error(r, WL_DATA_DEVICE_ERROR_ROLE,
                           "drag icon already has another role");
    return;
  }
  if (picture && tree_gpu(picture)) {
    wl_resource_post_error(icon, WL_SURFACE_ERROR_INVALID_SIZE,
                           "GPU drag icons are unsupported");
    return;
  }
  drag_cancel(s, 1);
  s->drag_active = 1;
  s->drag_source = data;
  s->drag_origin = v;
  s->drag_icon_x = picture ? picture->attach_x : 0;
  s->drag_icon_y = picture ? picture->attach_y : 0;
  s->drag_icon = picture;
  s->drag_button = s->buttons[button];
  if (data)
    data->used = 2;
  if (picture)
    picture->role = ROLE_DRAG_ICON;
  s->cursor_revision++;
  drag_motion(s, v, s->pointer_x, s->pointer_y);
}
static void device_selection(struct wl_client *c, struct wl_resource *r,
                             struct wl_resource *source, uint32_t serial) {
  (void)c;
  (void)serial;
  struct input_resource *d = wl_resource_get_user_data(r);
  struct worldr_apps *s = d->server;
  if (!belongs(r, s->keyboard_focus))
    return;
  struct app_source *v = source ? wl_resource_get_user_data(source) : NULL;
  if (v && v->used) {
    wl_resource_post_error(source, WL_DATA_SOURCE_ERROR_INVALID_SOURCE,
                           "selection source already used");
    return;
  }
  if (v)
    v->used = 1;
  replace_selection(s, v);
}
static void device_destroy(struct wl_resource *r) {
  struct input_resource *v = wl_resource_get_user_data(r);
  if (v->server->drag_device == r)
    drag_cancel(v->server, 1);
  wl_list_remove(&v->link);
  free(v);
}
static const struct wl_data_device_interface device_impl = {
    .start_drag = device_drag,
    .set_selection = device_selection,
    .release = destroy_request};
static void get_data_device(struct wl_client *c, struct wl_resource *r,
                            uint32_t id, struct wl_resource *seat) {
  (void)seat;
  struct worldr_apps *s = wl_resource_get_user_data(r);
  if (wl_list_length(&s->data_devices) >= 64) {
    post_oom(r);
    return;
  }
  struct input_resource *v = calloc(1, sizeof(*v));
  if (!v) {
    post_oom(r);
    return;
  }
  v->server = s;
  v->resource = wl_resource_create(c, &wl_data_device_interface,
                                   wl_resource_get_version(r), id);
  if (!v->resource) {
    free(v);
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(v->resource, &device_impl, v, device_destroy);
  wl_list_insert(s->data_devices.prev, &v->link);
  if (belongs(v->resource, s->keyboard_focus))
    send_selection(s, v->resource);
}
static const struct wl_data_device_manager_interface data_manager_impl = {
    .create_data_source = create_source, .get_data_device = get_data_device};
static void bind_data_manager(struct wl_client *c, void *data, uint32_t ver,
                              uint32_t id) {
  struct wl_resource *r = wl_resource_create(
      c, &wl_data_device_manager_interface, ver < 3 ? ver : 3, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &data_manager_impl, data, NULL);
}
static void decoration_mode(struct wl_client *c, struct wl_resource *r,
                            uint32_t mode) {
  (void)c;
  (void)mode;
  zxdg_toplevel_decoration_v1_send_configure(
      r, ZXDG_TOPLEVEL_DECORATION_V1_MODE_SERVER_SIDE);
}
static void decoration_unset(struct wl_client *c, struct wl_resource *r) {
  decoration_mode(c, r, 0);
}
static const struct zxdg_toplevel_decoration_v1_interface decoration_impl = {
    .destroy = destroy_request,
    .set_mode = decoration_mode,
    .unset_mode = decoration_unset};
static void get_decoration(struct wl_client *c, struct wl_resource *r,
                           uint32_t id, struct wl_resource *toplevel) {
  (void)toplevel;
  struct wl_resource *v =
      wl_resource_create(c, &zxdg_toplevel_decoration_v1_interface, 1, id);
  if (!v) {
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(v, &decoration_impl, NULL, NULL);
  zxdg_toplevel_decoration_v1_send_configure(
      v, ZXDG_TOPLEVEL_DECORATION_V1_MODE_SERVER_SIDE);
}
static const struct zxdg_decoration_manager_v1_interface
    decoration_manager_impl = {.destroy = destroy_request,
                               .get_toplevel_decoration = get_decoration};
static void bind_decorations(struct wl_client *c, void *data, uint32_t ver,
                             uint32_t id) {
  (void)ver;
  struct wl_resource *r =
      wl_resource_create(c, &zxdg_decoration_manager_v1_interface, 1, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &decoration_manager_impl, data, NULL);
}
static const struct wl_output_interface output_impl = {.release =
                                                           destroy_request};
static void bind_output(struct wl_client *c, void *data, uint32_t ver,
                        uint32_t id) {
  struct worldr_apps *s = data;
  struct wl_resource *r =
      wl_resource_create(c, &wl_output_interface, ver < 2 ? ver : 2, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &output_impl, s, NULL);
  wl_output_send_geometry(r, 0, 0, 340, 210, WL_OUTPUT_SUBPIXEL_UNKNOWN,
                          "worldr", "application surface",
                          WL_OUTPUT_TRANSFORM_NORMAL);
  wl_output_send_mode(r, WL_OUTPUT_MODE_CURRENT | WL_OUTPUT_MODE_PREFERRED,
                      s->width, s->height, 60000);
  if (wl_resource_get_version(r) >= 2) {
    wl_output_send_scale(r, 1);
    wl_output_send_done(r);
  }
}
static void input_destroy(struct wl_resource *r) {
  struct input_resource *v = wl_resource_get_user_data(r);
  wl_list_remove(&v->link);
  free(v);
}
static void set_cursor(struct wl_client *c, struct wl_resource *r,
                       uint32_t serial, struct wl_resource *surface, int32_t x,
                       int32_t y) {
  struct input_resource *pointer = wl_resource_get_user_data(r);
  struct worldr_apps *s = pointer->server;
  if (!belongs(r, s->pointer_focus) || serial != s->pointer_enter_serial)
    return;
  struct app_surface *v = surface ? wl_resource_get_user_data(surface) : NULL;
  if (surface && (!v || wl_resource_get_client(surface) != c ||
                  (v->role && v->role != ROLE_CURSOR))) {
    wl_resource_post_error(r, WL_POINTER_ERROR_ROLE,
                           "cursor surface already has another role");
    return;
  }
  if (v) {
    if (v->width > MAX_CURSOR_SIZE || v->height > MAX_CURSOR_SIZE ||
        tree_gpu(v)) {
      wl_resource_post_error(surface, WL_SURFACE_ERROR_INVALID_SIZE,
                             "cursor buffer exceeds 512 pixels");
      return;
    }
    v->role = ROLE_CURSOR;
  }
  s->cursor_surface = v;
  s->cursor_set = 1;
  s->cursor_x = x;
  s->cursor_y = y;
  s->cursor_revision++;
}
static const struct wl_pointer_interface pointer_impl = {
    .set_cursor = set_cursor, .release = destroy_request};
static const struct wl_keyboard_interface keyboard_impl = {.release =
                                                               destroy_request};
static void get_pointer(struct wl_client *c, struct wl_resource *r,
                        uint32_t id) {
  struct worldr_apps *s = wl_resource_get_user_data(r);
  if (wl_list_length(&s->pointers) >= 64) {
    post_oom(r);
    return;
  }
  struct input_resource *v = calloc(1, sizeof(*v));
  if (!v) {
    post_oom(r);
    return;
  }
  v->server = s;
  v->resource = wl_resource_create(c, &wl_pointer_interface,
                                   wl_resource_get_version(r), id);
  if (!v->resource) {
    free(v);
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(v->resource, &pointer_impl, v, input_destroy);
  wl_list_insert(s->pointers.prev, &v->link);
  if (belongs(v->resource, s->pointer_focus)) {
    s->pointer_enter_serial = wl_display_next_serial(s->display);
    wl_pointer_send_enter(
        v->resource, s->pointer_enter_serial, s->pointer_focus->resource,
        wl_fixed_from_double(s->pointer_x), wl_fixed_from_double(s->pointer_y));
    pointer_frame(v->resource);
  }
}
static void get_keyboard(struct wl_client *c, struct wl_resource *r,
                         uint32_t id) {
  struct worldr_apps *s = wl_resource_get_user_data(r);
  if (wl_list_length(&s->keyboards) >= 64) {
    post_oom(r);
    return;
  }
  struct input_resource *v = calloc(1, sizeof(*v));
  if (!v) {
    post_oom(r);
    return;
  }
  v->server = s;
  v->resource = wl_resource_create(c, &wl_keyboard_interface,
                                   wl_resource_get_version(r), id);
  if (!v->resource) {
    free(v);
    post_oom(r);
    return;
  }
  wl_resource_set_implementation(v->resource, &keyboard_impl, v, input_destroy);
  wl_list_insert(s->keyboards.prev, &v->link);
  wl_keyboard_send_keymap(v->resource, WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1,
                          s->keymap_fd, s->keymap_size);
  if (wl_resource_get_version(v->resource) >= 4)
    wl_keyboard_send_repeat_info(v->resource, s->repeat_rate, s->repeat_delay);
  if (belongs(v->resource, s->keyboard_focus))
    keyboard_enter(s, v->resource);
}
static void get_touch(struct wl_client *c, struct wl_resource *r, uint32_t id) {
  (void)c;
  (void)id;
  wl_resource_post_error(r, WL_SEAT_ERROR_MISSING_CAPABILITY,
                         "touch is not supported");
}
static const struct wl_seat_interface seat_impl = {.get_pointer = get_pointer,
                                                   .get_keyboard = get_keyboard,
                                                   .get_touch = get_touch,
                                                   .release = destroy_request};
static void bind_seat(struct wl_client *c, void *data, uint32_t ver,
                      uint32_t id) {
  struct wl_resource *r =
      wl_resource_create(c, &wl_seat_interface, ver < 5 ? ver : 5, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &seat_impl, data, NULL);
  wl_seat_send_capabilities(r, WL_SEAT_CAPABILITY_KEYBOARD |
                                   WL_SEAT_CAPABILITY_POINTER);
  if (wl_resource_get_version(r) >= 2)
    wl_seat_send_name(r, "worldr-apps");
}

int worldr_apps_keymap(worldr_apps *s, const char *text) {
  struct xkb_context *ctx = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
  if (!ctx)
    return -1;
  struct xkb_keymap *map = xkb_keymap_new_from_string(
      ctx, text, XKB_KEYMAP_FORMAT_TEXT_V1, XKB_KEYMAP_COMPILE_NO_FLAGS);
  xkb_context_unref(ctx);
  if (!map)
    return -1;
  xkb_keymap_unref(map);
  size_t n = strlen(text) + 1;
  int fd = memfd_create("worldr-app-keymap", MFD_CLOEXEC | MFD_ALLOW_SEALING);
  if (fd < 0)
    return -1;
  if (ftruncate(fd, n) < 0) {
    close(fd);
    return -1;
  }
  size_t offset = 0;
  while (offset < n) {
    ssize_t k = pwrite(fd, text + offset, n - offset, offset);
    if (k < 0 && errno == EINTR)
      continue;
    if (k <= 0) {
      close(fd);
      return -1;
    }
    offset += k;
  }
  fcntl(fd, F_ADD_SEALS,
        F_SEAL_SHRINK | F_SEAL_GROW | F_SEAL_WRITE | F_SEAL_SEAL);
  int old = s->keymap_fd;
  s->keymap_fd = fd;
  s->keymap_size = n;
  struct input_resource *v;
  wl_list_for_each(v, &s->keyboards, link) wl_keyboard_send_keymap(
      v->resource, WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1, fd, n);
  if (old >= 0)
    close(old);
  return 0;
}
static void focus_surface(struct worldr_apps *s, struct app_surface *v) {
  if (v == s->keyboard_focus)
    return;
  struct app_surface *old = s->keyboard_focus;
  struct input_resource *q;
  if (old) {
    wl_list_for_each(q, &s->keyboards, link) if (belongs(q->resource, old)) {
      for (size_t i = 0; i < s->key_count; i++)
        wl_keyboard_send_key(q->resource, wl_display_next_serial(s->display),
                             now_ms(), s->keys[i],
                             WL_KEYBOARD_KEY_STATE_RELEASED);
      wl_keyboard_send_leave(q->resource, wl_display_next_serial(s->display),
                             old->resource);
    }
  }
  s->key_count = 0;
  s->keyboard_focus = v;
  if (old && root_surface(old)->toplevel)
    configure(root_surface(old));
  if (v) {
    if (root_surface(v)->toplevel)
      configure(root_surface(v));
    wl_list_for_each(q, &s->keyboards, link) if (belongs(q->resource, v))
        keyboard_enter(s, q->resource);
    selection_changed(s);
  }
  return;
}
int worldr_apps_focus(worldr_apps *s, uint64_t id) {
  struct app_surface *v = id ? find_surface(s, id) : NULL;
  if (id && !v)
    return -1;
  if (s->popup_grab) {
    if (root_surface(s->popup_grab) == v) {
      if (has_buffer(s->popup_grab))
        v = s->popup_grab;
    } else
      dismiss_popups(s);
  }
  focus_surface(s, v);
  return 0;
}
static int input_contains(struct app_surface *v, float x, float y) {
  if (v->input.infinite)
    return 1;
  int inside = 0;
  for (int i = 0; i < v->input.count; i++)
    if (x >= v->input.operations[i].x && y >= v->input.operations[i].y &&
        (double)x <
            (int64_t)v->input.operations[i].x + v->input.operations[i].width &&
        (double)y <
            (int64_t)v->input.operations[i].y + v->input.operations[i].height)
      inside = v->input.operations[i].add;
  return inside;
}
static struct app_surface *pointer_target(struct app_surface *root, float *x,
                                          float *y) {
  struct app_surface *children[MAX_SURFACES];
  int count = ordered_children(root, children);
  for (int i = count - 1; i >= 0; i--) {
    struct app_surface *v = children[i];
    if (v->below_parent)
      continue;
    float px = *x - v->x, py = *y - v->y;
    struct app_surface *target = pointer_target(v, &px, &py);
    if (target) {
      *x = px;
      *y = py;
      return target;
    }
  }
  int gx = root->popup ? root->geometry_x : 0,
      gy = root->popup ? root->geometry_y : 0;
  int w = root->popup && root->geometry_width ? root->geometry_width
                                              : scale_logical_width(root);
  int h = root->popup && root->geometry_height ? root->geometry_height
                                               : scale_logical_height(root);
  if (*x >= gx && *y >= gy && (double)*x < (int64_t)gx + w &&
      (double)*y < (int64_t)gy + h && input_contains(root, *x, *y))
    return root;
  for (int i = count - 1; i >= 0; i--) {
    struct app_surface *v = children[i];
    if (!v->below_parent)
      continue;
    float px = *x - v->x, py = *y - v->y;
    struct app_surface *target = pointer_target(v, &px, &py);
    if (target) {
      *x = px;
      *y = py;
      return target;
    }
  }
  return NULL;
}
int worldr_apps_pointer(worldr_apps *s, uint64_t id, float x, float y) {
  struct app_surface *v = id ? find_surface(s, id) : NULL;
  if (id && !v)
    return -1;
  if (s->drag_active) {
    if (v)
      v = pointer_target(v, &x, &y);
    drag_motion(s, v, x, y);
    return 0;
  }
  if (v && s->button_count && s->pointer_focus) {
    // Preserve the implicit grab until every button is released, including
    // when the pointer leaves a subsurface's bounds.
    if (root_surface(s->pointer_focus) != v)
      return 0;
    v = s->pointer_focus;
    for (struct app_surface *p = v; p->parent; p = p->parent) {
      x -= p->x;
      y -= p->y;
    }
  } else if (v) {
    v = pointer_target(v, &x, &y);
  }
  if (!v) {
    s->pointer_cancelling++;
    while (s->button_count)
      worldr_apps_button(s, s->buttons[s->button_count - 1], 0, now_ms());
    s->pointer_cancelling--;
  }
  struct input_resource *q;
  if (v != s->pointer_focus) {
    clear_cursor(s);
    s->pointer_enter_serial = 0;
    if (s->pointer_focus)
      wl_list_for_each(q, &s->pointers,
                       link) if (belongs(q->resource, s->pointer_focus)) {
        wl_pointer_send_leave(q->resource, wl_display_next_serial(s->display),
                              s->pointer_focus->resource);
        pointer_frame(q->resource);
      }
    s->pointer_focus = v;
    if (v)
      s->pointer_enter_serial = wl_display_next_serial(s->display);
    if (v)
      wl_list_for_each(q, &s->pointers, link) if (belongs(q->resource, v)) {
        wl_pointer_send_enter(q->resource, s->pointer_enter_serial, v->resource,
                              wl_fixed_from_double(x), wl_fixed_from_double(y));
        pointer_frame(q->resource);
      }
  }
  s->pointer_x = x;
  s->pointer_y = y;
  if (v)
    wl_list_for_each(q, &s->pointers, link) if (belongs(q->resource, v)) {
      wl_pointer_send_motion(q->resource, now_ms(), wl_fixed_from_double(x),
                             wl_fixed_from_double(y));
      pointer_frame(q->resource);
    }
  return 0;
}
void worldr_apps_cursor(worldr_apps *s, worldr_app_cursor *out) {
  memset(out, 0, sizeof(*out));
  out->revision = s->cursor_revision;
  if (!s->pointer_focus)
    return;
  int icon = s->drag_active && s->drag_icon && s->drag_icon->pixels;
  out->drag_icon = icon;
  out->surface_id = root_surface(icon ? s->drag_origin : s->pointer_focus)->id;
  out->set = icon ? 1 : s->cursor_set;
  if (!out->set)
    return;
  struct app_surface *v = icon ? s->drag_icon : s->cursor_surface;
  out->hidden = !v || !v->pixels;
  out->hotspot_x =
      icon ? (s->drag_icon_x == INT32_MIN ? INT32_MAX : -s->drag_icon_x)
           : s->cursor_x;
  out->hotspot_y =
      icon ? (s->drag_icon_y == INT32_MIN ? INT32_MAX : -s->drag_icon_y)
           : s->cursor_y;
  if (out->hidden)
    return;
  out->scale = v->scale;
  out->logical_width = scale_logical_width(v);
  out->logical_height = scale_logical_height(v);
  if (flatten_surface(v, 0) < 0) {
    post_oom(v->resource);
    out->set = 0;
    return;
  }
  out->width = v->snapshot_width;
  out->height = v->snapshot_height;
  out->pixels = v->snapshot;
  out->image_id = v->id;
  out->image_revision = v->revision;
}
void worldr_apps_button(worldr_apps *s, uint32_t code, int pressed,
                        uint32_t time) {
  if (pressed && s->popup_grab) {
    struct app_surface *target = s->pointer_focus;
    while (target && !target->popup)
      target = target->parent;
    if (!target || target->popup_dismissed ||
        root_surface(target) != root_surface(s->popup_grab)) {
      dismiss_popups(s);
      return;
    }
  }

  if (!s->pointer_focus)
    return;
  size_t i = 0;
  while (i < s->button_count && s->buttons[i] != code)
    i++;
  if (pressed) {
    if (i < s->button_count || s->button_count == 32)
      return;
    s->buttons[s->button_count++] = code;
  } else {
    if (i == s->button_count)
      return;
    s->buttons[i] = s->buttons[--s->button_count];
    s->button_serials[i] = s->button_serials[s->button_count];
  }
  uint32_t serial = input_serial(s, s->pointer_focus, pressed);
  if (pressed)
    s->button_serials[i] = serial;
  if (s->drag_active) {
    if (!pressed && code == s->drag_button)
      drag_drop(s);
    return;
  }
  struct input_resource *q;
  wl_list_for_each(q, &s->pointers,
                   link) if (belongs(q->resource, s->pointer_focus)) {
    wl_pointer_send_button(q->resource, serial, time, code,
                           pressed ? WL_POINTER_BUTTON_STATE_PRESSED
                                   : WL_POINTER_BUTTON_STATE_RELEASED);
    pointer_frame(q->resource);
  }
  if (!pressed && !s->button_count && !s->pointer_cancelling) {
    // The implicit grab belonged to the actual child surface. Resolve the
    // current root-local position again after its last release, even when the
    // workspace still routes to the same toplevel and no new motion follows.
    struct app_surface *target = s->pointer_focus;
    float x = s->pointer_x, y = s->pointer_y;
    for (struct app_surface *p = target; p && p->parent; p = p->parent) {
      x += p->x;
      y += p->y;
    }
    struct app_surface *root = root_surface(target);
    if (root)
      worldr_apps_pointer(s, root->id, x, y);
  }
}
int worldr_apps_drag_active(worldr_apps *s) { return s->drag_active; }
void worldr_apps_cancel_drag(worldr_apps *s) {
  if (!s->drag_active && !s->drag_source)
    return;
  drag_cancel(s, 1);
  if (!s->pointer_focus)
    s->button_count = 0;
  s->pointer_cancelling++;
  while (s->button_count)
    worldr_apps_button(s, s->buttons[s->button_count - 1], 0, now_ms());
  s->pointer_cancelling--;
}
void worldr_apps_axis(worldr_apps *s, float h, float v, uint32_t time) {
  struct input_resource *q;
  wl_list_for_each(q, &s->pointers,
                   link) if (belongs(q->resource, s->pointer_focus)) {
    if (wl_resource_get_version(q->resource) >= 5)
      wl_pointer_send_axis_source(q->resource, WL_POINTER_AXIS_SOURCE_WHEEL);
    if (h)
      wl_pointer_send_axis(q->resource, time, WL_POINTER_AXIS_HORIZONTAL_SCROLL,
                           wl_fixed_from_double(h));
    if (v)
      wl_pointer_send_axis(q->resource, time, WL_POINTER_AXIS_VERTICAL_SCROLL,
                           wl_fixed_from_double(v));
    pointer_frame(q->resource);
  }
}
void worldr_apps_key(worldr_apps *s, uint32_t code, int pressed, uint32_t time,
                     uint32_t depressed, uint32_t latched, uint32_t locked,
                     uint32_t group) {
  s->depressed = depressed;
  s->latched = latched;
  s->locked = locked;
  s->group = group;
  if (!s->keyboard_focus)
    return;
  size_t i = 0;
  while (i < s->key_count && s->keys[i] != code)
    i++;
  if (pressed) {
    if (i < s->key_count)
      return;
    if (s->key_count < 256)
      s->keys[s->key_count++] = code;
  } else {
    if (i == s->key_count)
      return;
    s->keys[i] = s->keys[--s->key_count];
  }
  struct input_resource *q;
  wl_list_for_each(q, &s->keyboards,
                   link) if (belongs(q->resource, s->keyboard_focus)) {
    send_modifiers(s, q->resource);
    wl_keyboard_send_key(
        q->resource, input_serial(s, s->keyboard_focus, pressed), time, code,
        pressed ? WL_KEYBOARD_KEY_STATE_PRESSED
                : WL_KEYBOARD_KEY_STATE_RELEASED);
  }
}
int worldr_apps_resize(worldr_apps *s, uint64_t id, int w, int h) {
  struct app_surface *v = find_surface(s, id);
  if (!v)
    return -1;
  v->requested_width = w;
  v->requested_height = h;
  configure(v);
  return 0;
}
void worldr_apps_modifiers(worldr_apps *s, uint32_t depressed, uint32_t latched,
                           uint32_t locked, uint32_t group) {
  s->depressed = depressed;
  s->latched = latched;
  s->locked = locked;
  s->group = group;
  struct input_resource *q;
  wl_list_for_each(q, &s->keyboards,
                   link) if (belongs(q->resource, s->keyboard_focus))
      send_modifiers(s, q->resource);
}
void worldr_apps_repeat(worldr_apps *s, int32_t rate, int32_t delay) {
  s->repeat_rate = rate;
  s->repeat_delay = delay;
  struct input_resource *q;
  wl_list_for_each(q, &s->keyboards,
                   link) if (wl_resource_get_version(q->resource) >= 4)
      wl_keyboard_send_repeat_info(q->resource, rate, delay);
}

static int tree_gpu(struct app_surface *root) {
  struct app_surface *v;
  wl_list_for_each(v, &root->server->surfaces, link) {
    if (!v->gpu || root_surface(v) != root)
      continue;
    int visible = 1;
    for (struct app_surface *p = v; p; p = p->parent)
      if (!has_buffer(p) || p->popup_dismissed ||
          (p->subsurface && !p->placed)) {
        visible = 0;
        break;
      }
    if (visible)
      return 1;
  }
  return 0;
}
static int collect_layers(struct app_surface *root, struct app_surface *v,
                          float x, float y, worldr_app_layer *out, int cap,
                          int *count) {
  struct app_surface *children[MAX_SURFACES];
  int n = ordered_children(v, children);
  // Subsurface stacking has two lists separated by the parent image.
  for (int above = 0; above < 2; above++) {
    if (above) {
      float w = scale_logical_width(v), h = scale_logical_height(v);
      float rw = scale_logical_width(root), rh = scale_logical_height(root);
      float left = fmaxf(0, x), top = fmaxf(0, y), right = fminf(rw, x + w),
            bottom = fminf(rh, y + h);
      if (right > left && bottom > top) {
        if (*count >= cap)
          return -1;
        float u0, v0, u1, v1;
        scale_source_crop(v, &u0, &v0, &u1, &v1);
        out[(*count)++] =
            (worldr_app_layer){.token = v->image_token,
                               .width = v->width,
                               .height = v->height,
                               .gpu = v->gpu,
                               .root = v == root,
                               .opaque = v->opaque,
                               .x = left,
                               .y = top,
                               .logical_width = right - left,
                               .logical_height = bottom - top,
                               .u0 = u0 + (left - x) / w * (u1 - u0),
                               .v0 = v0 + (top - y) / h * (v1 - v0),
                               .u1 = u0 + (right - x) / w * (u1 - u0),
                               .v1 = v0 + (bottom - y) / h * (v1 - v0),
                               .pixels = v->pixels};
      }
    }
    for (int i = 0; i < n; i++) {
      struct app_surface *child = children[i];
      if (child->below_parent == above)
        continue;
      if (collect_layers(root, child, x + child->x, y + child->y, out, cap,
                         count) < 0)
        return -1;
    }
  }
  return 0;
}
int worldr_apps_layers(worldr_apps *s, uint64_t id, worldr_app_layer *out,
                       int cap) {
  struct app_surface *v = find_surface(s, id);
  int count = 0;
  if (v && collect_layers(v, v, 0, 0, out, cap, &count) < 0)
    return -1;
  return count;
}
int worldr_apps_image_alive(worldr_apps *s, uint64_t token) {
  struct app_image *image;
  wl_list_for_each(image, &s->images, link) if (image->token == token) return 1;
  return 0;
}
static int flatten_surface(struct app_surface *root, int opaque) {
  float u0, v0, u1, v1;
  scale_source_crop(root, &u0, &v0, &u1, &v1);
  int width = root->viewport_source
                  ? (int)ceil((double)root->viewport_src[2] * root->scale / 256)
                  : root->width;
  int height =
      root->viewport_source
          ? (int)ceil((double)root->viewport_src[3] * root->scale / 256)
          : root->height;
  if (width < 1 || height < 1 || width > 4096 || height > 4096)
    return -1;
  size_t size = (size_t)width * height * 4;
  if (!root->snapshot_dirty && root->snapshot_width == width &&
      root->snapshot_height == height)
    return 0;
  struct worldr_apps *s = root->server;
  if (s->pixels_bytes - root->snapshot_size + size > MAX_PIXELS_BYTES)
    return -1;
  worldr_app_layer layers[32];
  int count = 0;
  if (collect_layers(root, root, 0, 0, layers, 32, &count) < 0)
    return -1;
  unsigned char *pixels = realloc(root->snapshot, size);
  if (!pixels)
    return -1;
  s->pixels_bytes = s->pixels_bytes - root->snapshot_size + size;
  root->snapshot = pixels;
  root->snapshot_size = size;
  root->snapshot_width = width;
  root->snapshot_height = height;
  memset(pixels, 0, size);
  double dx = (double)width / scale_logical_width(root),
         dy = (double)height / scale_logical_height(root);
  for (int i = 0; i < count; i++) {
    worldr_app_layer *layer = &layers[i];
    if (!layer->pixels)
      return -1;
    int left = (int)floor(layer->x * dx), top = (int)floor(layer->y * dy);
    int right = (int)ceil((layer->x + layer->logical_width) * dx),
        bottom = (int)ceil((layer->y + layer->logical_height) * dy);
    if (left < 0)
      left = 0;
    if (top < 0)
      top = 0;
    if (right > width)
      right = width;
    if (bottom > height)
      bottom = height;
    for (int y = top; y < bottom; y++) {
      double fy = ((y + .5) / dy - layer->y) / layer->logical_height;
      int sy = (int)floor((layer->v0 + fy * (layer->v1 - layer->v0)) *
                          layer->height);
      if (sy < 0)
        sy = 0;
      if (sy >= layer->height)
        sy = layer->height - 1;
      for (int x = left; x < right; x++) {
        double fx = ((x + .5) / dx - layer->x) / layer->logical_width;
        int sx = (int)floor((layer->u0 + fx * (layer->u1 - layer->u0)) *
                            layer->width);
        if (sx < 0)
          sx = 0;
        if (sx >= layer->width)
          sx = layer->width - 1;
        const unsigned char *src =
            layer->pixels + ((size_t)sy * layer->width + sx) * 4;
        unsigned char *dst = pixels + ((size_t)y * width + x) * 4;
        for (int ch = 0; ch < 4; ch++) {
          unsigned value =
              src[ch] + ((unsigned)dst[ch] * (255 - src[3]) + 127) / 255;
          dst[ch] = value > 255 ? 255 : value;
        }
      }
    }
  }
  if (opaque)
    for (size_t i = 3; i < size; i += 4)
      pixels[i] = 255;
  root->snapshot_dirty = 0;
  return 0;
}
int worldr_apps_poll(worldr_apps *s, worldr_app_surface *out, int cap,
                     char *err, size_t errcap) {
  if (wl_event_loop_dispatch(s->loop, 0) < 0) {
    snprintf(err, errcap, "Wayland dispatch: %s", strerror(errno));
    return -1;
  }
  wl_display_flush_clients(s->display);
  int n = 0;
  struct app_surface *v;
  wl_list_for_each(v, &s->surfaces, link) {
    if ((!v->toplevel && !v->x11_mapped) || !has_buffer(v))
      continue;
    if (n == cap) {
      snprintf(err, errcap, "application surface capacity exceeded");
      return -1;
    }
    int layered = tree_gpu(v);
    if (layered) {
      worldr_app_layer layers[32];
      int count = 0;
      if (collect_layers(v, v, 0, 0, layers, 32, &count) < 0) {
        post_oom(v->resource);
        continue;
      }
    }
    if (!layered && flatten_surface(v, 1) < 0) {
      post_oom(v->resource);
      continue;
    }
    out[n++] =
        (worldr_app_surface){.id = v->id,
                             .revision = v->revision,
                             .width = layered ? v->width : v->snapshot_width,
                             .height = layered ? v->height : v->snapshot_height,
                             .logical_width = scale_logical_width(v),
                             .logical_height = scale_logical_height(v),
                             .title = v->title ? v->title : "",
                             .app_id = v->app_id ? v->app_id : "",
                             .pid = v->pid,
                             .pixels = layered ? NULL : v->snapshot,
                             .layered = layered};
  }
  return n;
}
int worldr_apps_open(const char *path, int width, int height, worldr_apps **out,
                     char *err, size_t cap) {
  *out = NULL;
  struct worldr_apps *s = calloc(1, sizeof(*s));
  if (!s) {
    snprintf(err, cap, "allocation failed");
    return -1;
  }
  s->width = width;
  s->height = height;
  s->keymap_fd = -1;
  s->repeat_rate = 25;
  s->repeat_delay = 600;
  wl_list_init(&s->surfaces);
  wl_list_init(&s->keyboards);
  wl_list_init(&s->pointers);
  wl_list_init(&s->data_devices);
  wl_list_init(&s->offers);
  wl_list_init(&s->clipboard_requests);
  wl_list_init(&s->images);
  s->display = wl_display_create();
  if (!s->display) {
    snprintf(err, cap, "cannot create Wayland display");
    goto fail;
  }
  s->loop = wl_display_get_event_loop(s->display);
  struct xkb_context *ctx = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
  struct xkb_rule_names names = {.layout = "us"};
  struct xkb_keymap *keymap =
      ctx ? xkb_keymap_new_from_names(ctx, &names, XKB_KEYMAP_COMPILE_NO_FLAGS)
          : NULL;
  char *text = keymap
                   ? xkb_keymap_get_as_string(keymap, XKB_KEYMAP_FORMAT_TEXT_V1)
                   : NULL;
  if (!text || worldr_apps_keymap(s, text) < 0) {
    free(text);
    if (keymap)
      xkb_keymap_unref(keymap);
    if (ctx)
      xkb_context_unref(ctx);
    snprintf(err, cap, "cannot create XKB keymap");
    goto fail;
  }
  free(text);
  xkb_keymap_unref(keymap);
  xkb_context_unref(ctx);
  if (wl_display_init_shm(s->display) < 0 || scale_globals(s) < 0 ||
      !wl_global_create(s->display, &wl_compositor_interface, 4, s,
                        bind_compositor) ||
      !wl_global_create(s->display, &wl_subcompositor_interface, 1, s,
                        bind_subcompositor) ||
      !wl_global_create(s->display, &xdg_wm_base_interface, 3, s, bind_wm) ||
      !wl_global_create(s->display, &wl_output_interface, 2, s, bind_output) ||
      !wl_global_create(s->display, &wl_seat_interface, 5, s, bind_seat) ||
      !wl_global_create(s->display, &wl_data_device_manager_interface, 3, s,
                        bind_data_manager) ||
      !wl_global_create(s->display, &zxdg_decoration_manager_v1_interface, 1, s,
                        bind_decorations)) {
    snprintf(err, cap, "cannot register Wayland globals");
    goto fail;
  }
  int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC | SOCK_NONBLOCK, 0);
  if (fd < 0) {
    snprintf(err, cap, "socket: %s", strerror(errno));
    goto fail;
  }
  struct sockaddr_un addr = {.sun_family = AF_UNIX};
  if (strlen(path) >= sizeof(addr.sun_path)) {
    close(fd);
    snprintf(err, cap, "socket path too long");
    goto fail;
  }
  strcpy(addr.sun_path, path);
  if (bind(fd, (struct sockaddr *)&addr, sizeof(addr)) < 0 ||
      listen(fd, 16) < 0 || wl_display_add_socket_fd(s->display, fd) < 0) {
    snprintf(err, cap, "private socket: %s", strerror(errno));
    close(fd);
    goto fail;
  }
  *out = s;
  return 0;
fail:
  worldr_apps_close(s);
  return -1;
}
void worldr_apps_close(worldr_apps *s) {
  if (!s)
    return;
  if (s->display) {
    wl_display_destroy_clients(s->display);
    if (s->selection && !s->selection->resource)
      source_release(s->selection);
    wl_display_destroy(s->display);
  }
  struct clipboard_request *q, *tmp;
  wl_list_for_each_safe(q, tmp, &s->clipboard_requests, link) {
    close(q->request.fd);
    free(q->request.mime);
    wl_list_remove(&q->link);
    free(q);
  }
  if (s->keymap_fd >= 0)
    close(s->keymap_fd);
  free(s);
}
void worldr_apps_request_close(worldr_apps *s) {
  struct app_surface *v;
  wl_list_for_each(v, &s->surfaces, link) {
    if (v->toplevel)
      xdg_toplevel_send_close(v->toplevel);
  }
  wl_display_flush_clients(s->display);
}
void worldr_apps_close_surface(worldr_apps *s, uint64_t id) {
  struct app_surface *v = find_surface(s, id);
  if (v) {
    xdg_toplevel_send_close(v->toplevel);
    wl_display_flush_clients(s->display);
  }
}
void worldr_apps_clipboard_offer(worldr_apps *s,
                                 worldr_app_clipboard_offer *out) {
  *out = (worldr_app_clipboard_offer){.revision = s->clipboard_revision};
  if (s->selection) {
    out->id = s->selection->id;
    out->external_id = s->selection->external_id;
    out->mime_count = s->selection->mime_count;
  }
}
const char *worldr_apps_clipboard_mime(worldr_apps *s, int index) {
  if (!s->selection || index < 0 || index >= s->selection->mime_count)
    return NULL;
  return s->selection->mimes[index];
}
int worldr_apps_offer_clipboard(worldr_apps *s, uint64_t external_id,
                                const char **mimes, int count) {
  if (!external_id) {
    replace_selection(s, NULL);
    return 0;
  }
  if (s->source_count >= 128 || count < 0 || count > 64)
    return -1;
  struct app_source *v = calloc(1, sizeof(*v));
  if (!v)
    return -1;
  v->server = s;
  v->external_id = external_id;
  v->id = ++s->next_clipboard_id;
  s->source_count++;
  for (int i = 0; i < count; i++) {
    char *m = strdup(mimes[i]);
    if (!m) {
      source_release(v);
      return -1;
    }
    v->mimes[v->mime_count++] = m;
  }
  replace_selection(s, v);
  return 0;
}
int worldr_apps_receive_clipboard(worldr_apps *s, uint64_t offer_id,
                                  const char *mime, int fd) {
  struct app_source *v = s->selection;
  if (!v || v->id != offer_id || !v->resource || !source_has_mime(v, mime) ||
      fd < 0 || fcntl(fd, F_GETFD) < 0)
    return -1;
  wl_data_source_send_send(v->resource, mime, fd);
  wl_display_flush_clients(s->display);
  return 0;
}
int worldr_apps_clipboard_requests(worldr_apps *s,
                                   worldr_app_clipboard_request *out, int cap) {
  int count = 0;
  while (count < cap && !wl_list_empty(&s->clipboard_requests)) {
    struct clipboard_request *q =
        wl_container_of(s->clipboard_requests.next, q, link);
    out[count++] = q->request;
    wl_list_remove(&q->link);
    free(q);
  }
  return count;
}

// Association is an explicit XWM decision. A raw resource ID is meaningful
// only within the authenticated Xwayland peer process; dimensions never match
// unrelated surfaces heuristically.
int worldr_apps_associate_x11(worldr_apps *s, uint32_t pid, uint32_t object,
                              uint32_t window, const char *title,
                              const char *app_id, uint64_t *id) {
  struct app_surface *v, *found = NULL;
  int roots = 0;
  wl_list_for_each(v, &s->surfaces, link) {
    if (v->toplevel || v->x11_mapped)
      roots++;
    if (v->pid == pid && wl_resource_get_id(v->resource) == object)
      found = v;
    if (v->x11_mapped && v->x11_window == window &&
        !(v->pid == pid && wl_resource_get_id(v->resource) == object))
      return -2;
  }
  if (!found)
    return -1;
  v = found;
  if (v->role != ROLE_NONE &&
      (v->role != ROLE_X11 || (v->x11_window && v->x11_window != window)))
    return -2;
  if (!v->x11_mapped && roots >= 32)
    return -3;
  char *name = strdup(title), *app = strdup(app_id);
  if (!name || !app) {
    free(name);
    free(app);
    return -4;
  }
  free(v->title);
  free(v->app_id);
  v->title = name;
  v->app_id = app;
  v->role = ROLE_X11;
  v->x11_window = window;
  v->x11_mapped = 1;
  *id = v->id;
  changed(v);
  return 0;
}
int worldr_apps_update_x11(worldr_apps *s, uint32_t window, const char *title,
                           const char *app_id) {
  struct app_surface *v;
  wl_list_for_each(v, &s->surfaces, link) {
    if (!v->x11_mapped || v->x11_window != window)
      continue;
    char *name = strdup(title), *app = strdup(app_id);
    if (!name || !app) {
      free(name);
      free(app);
      return -1;
    }
    free(v->title);
    free(v->app_id);
    v->title = name;
    v->app_id = app;
    return 0;
  }
  return 0;
}
void worldr_apps_withdraw_x11(worldr_apps *s, uint32_t window) {
  struct app_surface *v;
  wl_list_for_each(v, &s->surfaces, link) {
    if (!v->x11_mapped || v->x11_window != window)
      continue;
    if (s->drag_origin && descendant(s->drag_origin, v))
      worldr_apps_cancel_drag(s);
    else if (s->drag_target && descendant(s->drag_target, v)) {
      if (s->drag_active)
        drag_motion(s, NULL, 0, 0);
      else
        drag_cancel(s, 1);
    }
    if (s->keyboard_focus && descendant(s->keyboard_focus, v))
      worldr_apps_focus(s, 0);
    if (s->pointer_focus && descendant(s->pointer_focus, v))
      worldr_apps_pointer(s, 0, 0, 0);
    v->x11_mapped = 0;
    v->x11_window = 0;
  }
}
uint32_t worldr_apps_x11_window(worldr_apps *s, uint64_t id) {
  struct app_surface *v = find_surface(s, id);
  return v && v->x11_mapped ? v->x11_window : 0;
}
