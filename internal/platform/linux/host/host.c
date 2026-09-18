#include "host.h"
#include "fractional-scale-v1-client-protocol.h"
#include "text_input.h"
#include "viewporter-client-protocol.h"
#include "xdg-shell-client-protocol.h"
#include <errno.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <time.h>
#include <unistd.h>
#include <wayland-client.h>
#include <xkbcommon/xkbcommon.h>

#define EVENT_CAPACITY 512
#define OUTPUT_CAPACITY 16
struct output {
  struct wl_output *object;
  uint32_t name;
  int scale, entered;
  struct worldr_host *host;
};
struct worldr_host {
  struct wl_display *display;
  struct wl_registry *registry;
  struct wl_compositor *compositor;
  struct wl_surface *surface;
  struct xdg_wm_base *wm;
  struct xdg_surface *xdg_surface;
  struct xdg_toplevel *toplevel;
  struct wl_seat *seat;
  struct wl_pointer *pointer;
  struct wl_keyboard *keyboard;
  struct wp_fractional_scale_manager_v1 *fractional_manager;
  struct wp_fractional_scale_v1 *fractional;
  struct wp_viewporter *viewporter;
  struct wp_viewport *viewport;
  struct wl_callback *discovery;
  struct xkb_context *xkb;
  struct xkb_keymap *keymap;
  struct xkb_state *state;
  worldr_host_clipboard *clipboard;
  worldr_host_text_input *text_input;
  char *keymap_text;
  struct output outputs[OUTPUT_CAPACITY];
  int width, height, scale, scale120, configured, closed, overflow, discovered,
      pointer_inside;
  uint32_t preferred_scale120, fractional_name, viewporter_name;
  uint32_t seat_name, compositor_name, wm_name;
  float x, y;
  uint32_t mods;
  uint32_t depressed, latched, locked, group;
  int32_t repeat_rate, repeat_delay;
  unsigned char buttons[0x300];
  worldr_host_event events[EVENT_CAPACITY];
  int count;
};

static void event_free(worldr_host_event e) {
  free(e.keymap);
  free(e.text);
  free(e.text_context);
}
static void push(worldr_host *h, worldr_host_event e) {
  if (h->count == EVENT_CAPACITY) {
    h->overflow = 1;
    event_free(e);
    return;
  }
  h->events[h->count++] = e;
}
static void emit_text(void *data, worldr_host_event e) {
  push((worldr_host *)data, e);
}
static void cancel_pointer(worldr_host *h) {
  memset(h->buttons, 0, sizeof(h->buttons));
  push(h, (worldr_host_event){.kind = HOST_CANCEL});
}
static void push_keymap(worldr_host *h) {
  if (h->keymap_text) {
    char *text = strdup(h->keymap_text);
    if (text)
      push(h, (worldr_host_event){.kind = HOST_KEYMAP, .keymap = text});
  }
}
static void push_modifiers(worldr_host *h) {
  push(h, (worldr_host_event){.kind = HOST_MODIFIERS,
                              .mods = h->mods,
                              .depressed = h->depressed,
                              .latched = h->latched,
                              .locked = h->locked,
                              .group = h->group});
}
static int pixel_size(int logical, int scale120) {
  int size = (logical * scale120 + 60) / 120;
  return size > 0 ? size : 1;
}
static float pointer_x(worldr_host *h) {
  return h->x * (float)pixel_size(h->width, h->scale120) / (float)h->width;
}
static float pointer_y(worldr_host *h) {
  return h->y * (float)pixel_size(h->height, h->scale120) / (float)h->height;
}
static void update_scale(worldr_host *h) {
  int scale = 1;
  for (int i = 0; i < OUTPUT_CAPACITY; i++)
    if (h->outputs[i].entered && h->outputs[i].scale > scale)
      scale = h->outputs[i].scale;
  int effective = h->fractional && h->viewport && h->preferred_scale120
                      ? (int)h->preferred_scale120
                      : scale * 120;
  h->scale = scale;
  if (h->surface)
    wl_surface_set_buffer_scale(h->surface, h->viewport ? 1 : scale);
  if (h->viewport)
    wp_viewport_set_destination(h->viewport, h->width, h->height);
  if (effective != h->scale120) {
    h->scale120 = effective;
    cancel_pointer(h);
    if (h->pointer_inside)
      push(h, (worldr_host_event){.kind = HOST_MOVE,
                                  .x = pointer_x(h),
                                  .y = pointer_y(h),
                                  .mods = h->mods});
  }
}
static void fractional_preferred(void *data,
                                 struct wp_fractional_scale_v1 *object,
                                 uint32_t scale) {
  (void)object;
  worldr_host *h = data;
  if (scale < 30 || scale > 960)
    return;
  h->preferred_scale120 = scale;
  update_scale(h);
}
static const struct wp_fractional_scale_v1_listener fractional_listener = {
    .preferred_scale = fractional_preferred};
static void attach_scaling(worldr_host *h) {
  if (h->surface && h->fractional_manager && h->viewporter && !h->fractional) {
    h->viewport = wp_viewporter_get_viewport(h->viewporter, h->surface);
    h->fractional = wp_fractional_scale_manager_v1_get_fractional_scale(
        h->fractional_manager, h->surface);
    wp_fractional_scale_v1_add_listener(h->fractional, &fractional_listener, h);
    update_scale(h);
  }
}
static void detach_scaling(worldr_host *h) {
  if (h->fractional)
    wp_fractional_scale_v1_destroy(h->fractional);
  h->fractional = NULL;
  if (h->viewport)
    wp_viewport_destroy(h->viewport);
  h->viewport = NULL;
  h->preferred_scale120 = 0;
}
static void surface_enter(void *data, struct wl_surface *s,
                          struct wl_output *o) {
  (void)s;
  worldr_host *h = data;
  for (int i = 0; i < OUTPUT_CAPACITY; i++)
    if (h->outputs[i].object == o)
      h->outputs[i].entered = 1;
  update_scale(h);
}
static void surface_leave(void *data, struct wl_surface *s,
                          struct wl_output *o) {
  (void)s;
  worldr_host *h = data;
  for (int i = 0; i < OUTPUT_CAPACITY; i++)
    if (h->outputs[i].object == o)
      h->outputs[i].entered = 0;
  update_scale(h);
}
static const struct wl_surface_listener surface_listener = {
    .enter = surface_enter, .leave = surface_leave};
static void output_geometry(void *d, struct wl_output *o, int32_t x, int32_t y,
                            int32_t pw, int32_t ph, int32_t sub,
                            const char *make, const char *model,
                            int32_t transform) {
  (void)d;
  (void)o;
  (void)x;
  (void)y;
  (void)pw;
  (void)ph;
  (void)sub;
  (void)make;
  (void)model;
  (void)transform;
}
static void output_mode(void *d, struct wl_output *o, uint32_t flags, int32_t w,
                        int32_t h, int32_t refresh) {
  (void)d;
  (void)o;
  (void)flags;
  (void)w;
  (void)h;
  (void)refresh;
}
static void output_done(void *d, struct wl_output *o) {
  (void)o;
  update_scale(((struct output *)d)->host);
}
static void output_scale(void *d, struct wl_output *o, int32_t scale) {
  (void)o;
  if (scale > 0 && scale <= 8)
    ((struct output *)d)->scale = scale;
}
static const struct wl_output_listener output_listener = {
    .geometry = output_geometry,
    .mode = output_mode,
    .done = output_done,
    .scale = output_scale};
static void ping(void *d, struct xdg_wm_base *wm, uint32_t serial) {
  (void)d;
  xdg_wm_base_pong(wm, serial);
}
static const struct xdg_wm_base_listener wm_listener = {.ping = ping};
static void configure(void *d, struct xdg_surface *s, uint32_t serial) {
  worldr_host *h = d;
  xdg_surface_ack_configure(s, serial);
  h->configured = 1;
}
static const struct xdg_surface_listener xdg_listener = {.configure =
                                                             configure};
static void top_configure(void *d, struct xdg_toplevel *t, int32_t w, int32_t h,
                          struct wl_array *states) {
  (void)t;
  (void)states;
  worldr_host *host = d;
  if (w > 0 && w <= 32768)
    host->width = w;
  if (h > 0 && h <= 32768)
    host->height = h;
  update_scale(host);
}
static void top_close(void *d, struct xdg_toplevel *t) {
  (void)t;
  worldr_host *h = d;
  h->closed = 1;
  push(h, (worldr_host_event){.kind = HOST_CLOSE});
}
static const struct xdg_toplevel_listener top_listener = {
    .configure = top_configure, .close = top_close};
static void ptr_enter(void *d, struct wl_pointer *p, uint32_t serial,
                      struct wl_surface *s, wl_fixed_t x, wl_fixed_t y) {
  (void)s;
  worldr_host *h = d;
  h->pointer_inside = 1;
  h->x = wl_fixed_to_double(x);
  h->y = wl_fixed_to_double(y);
  push(h, (worldr_host_event){.kind = HOST_MOVE,
                              .x = pointer_x(h),
                              .y = pointer_y(h),
                              .mods = h->mods});
  // A null host cursor is intentional: the native renderer draws the pointer.
  wl_pointer_set_cursor(p, serial, NULL, 0, 0);
}
static void ptr_leave(void *d, struct wl_pointer *p, uint32_t serial,
                      struct wl_surface *s) {
  (void)p;
  (void)serial;
  (void)s;
  worldr_host *h = d;
  h->pointer_inside = 0;
  cancel_pointer(h);
}
static void ptr_motion(void *d, struct wl_pointer *p, uint32_t time,
                       wl_fixed_t x, wl_fixed_t y) {
  (void)p;
  worldr_host *h = d;
  h->x = wl_fixed_to_double(x);
  h->y = wl_fixed_to_double(y);
  push(h, (worldr_host_event){.kind = HOST_MOVE,
                              .x = pointer_x(h),
                              .y = pointer_y(h),
                              .mods = h->mods,
                              .time = time});
}
static void ptr_button(void *d, struct wl_pointer *p, uint32_t serial,
                       uint32_t time, uint32_t button, uint32_t state) {
  (void)p;
  (void)serial;
  worldr_host *h = d;
  if (button >= sizeof(h->buttons))
    return;
  int pressed = state == WL_POINTER_BUTTON_STATE_PRESSED;
  if (pressed)
    worldr_host_clipboard_serial(h->clipboard, serial);
  // A canceled capture must not receive an orphan release after focus loss.
  if (!pressed && !h->buttons[button])
    return;
  h->buttons[button] = pressed;
  push(h, (worldr_host_event){.kind = pressed ? HOST_DOWN : HOST_UP,
                              .button = button,
                              .x = pointer_x(h),
                              .y = pointer_y(h),
                              .mods = h->mods,
                              .time = time,
                              .pressed = pressed});
}
static void ptr_axis(void *d, struct wl_pointer *p, uint32_t time,
                     uint32_t axis, wl_fixed_t value) {
  (void)p;
  worldr_host *h = d;
  worldr_host_event e = {.kind = HOST_SCROLL,
                         .x = pointer_x(h),
                         .y = pointer_y(h),
                         .mods = h->mods,
                         .time = time};
  if (axis == WL_POINTER_AXIS_VERTICAL_SCROLL)
    e.scroll_y = wl_fixed_to_double(value);
  else if (axis == WL_POINTER_AXIS_HORIZONTAL_SCROLL)
    e.scroll_x = wl_fixed_to_double(value);
  else
    return;
  push(h, e);
}
static void ptr_frame(void *d, struct wl_pointer *p) {
  (void)d;
  (void)p;
}
static void ptr_axis_source(void *d, struct wl_pointer *p, uint32_t source) {
  (void)d;
  (void)p;
  (void)source;
}
static void ptr_axis_stop(void *d, struct wl_pointer *p, uint32_t time,
                          uint32_t axis) {
  (void)d;
  (void)p;
  (void)time;
  (void)axis;
}
static void ptr_axis_discrete(void *d, struct wl_pointer *p, uint32_t axis,
                              int32_t steps) {
  (void)d;
  (void)p;
  (void)axis;
  (void)steps;
}
static const struct wl_pointer_listener ptr_listener = {
    .enter = ptr_enter,
    .leave = ptr_leave,
    .motion = ptr_motion,
    .button = ptr_button,
    .axis = ptr_axis,
    .frame = ptr_frame,
    .axis_source = ptr_axis_source,
    .axis_stop = ptr_axis_stop,
    .axis_discrete = ptr_axis_discrete};
static void reset_keyboard(worldr_host *h) {
  h->mods = 0;
  h->depressed = 0;
  h->latched = 0;
  h->locked = 0;
  h->group = 0;
  if (h->state)
    xkb_state_update_mask(h->state, 0, 0, 0, 0, 0, 0);
  push(h, (worldr_host_event){.kind = HOST_KEYBOARD_CANCEL});
}
static void keymap(void *d, struct wl_keyboard *k, uint32_t format, int32_t fd,
                   uint32_t size) {
  (void)k;
  worldr_host *h = d;
  reset_keyboard(h);
  free(h->keymap_text);
  h->keymap_text = NULL;
  xkb_state_unref(h->state);
  xkb_keymap_unref(h->keymap);
  h->state = NULL;
  h->keymap = NULL;
  if (format != WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1 || size == 0 ||
      size > 16 * 1024 * 1024) {
    close(fd);
    return;
  }
  char *text = mmap(NULL, size, PROT_READ, MAP_PRIVATE, fd, 0);
  close(fd);
  if (text == MAP_FAILED)
    return;
  struct xkb_keymap *map = NULL;
  if (text[size - 1] == '\0')
    map = xkb_keymap_new_from_string(h->xkb, text, XKB_KEYMAP_FORMAT_TEXT_V1,
                                     XKB_KEYMAP_COMPILE_NO_FLAGS);
  munmap(text, size);
  if (!map)
    return;
  struct xkb_state *state = xkb_state_new(map);
  if (!state) {
    xkb_keymap_unref(map);
    return;
  }
  h->keymap = map;
  h->state = state;
  h->keymap_text = xkb_keymap_get_as_string(map, XKB_KEYMAP_FORMAT_TEXT_V1);
  push_keymap(h);
}
static void key_enter(void *d, struct wl_keyboard *k, uint32_t serial,
                      struct wl_surface *s, struct wl_array *keys) {
  (void)k;
  (void)s;
  (void)keys;
  worldr_host *h = d;
  reset_keyboard(h);
  worldr_host_clipboard_focus(h->clipboard, 1, serial);
}
static void key_leave(void *d, struct wl_keyboard *k, uint32_t serial,
                      struct wl_surface *s) {
  (void)k;
  (void)serial;
  (void)s;
  worldr_host *h = d;
  reset_keyboard(h);
  worldr_host_clipboard_focus(h->clipboard, 0, 0);
}
static void key_event(void *d, struct wl_keyboard *k, uint32_t serial,
                      uint32_t time, uint32_t key, uint32_t state) {
  (void)k;
  (void)serial;
  worldr_host *h = d;
  if (state == WL_KEYBOARD_KEY_STATE_PRESSED)
    worldr_host_clipboard_serial(h->clipboard, serial);
  xkb_keysym_t sym = h->state ? xkb_state_key_get_one_sym(h->state, key + 8)
                              : XKB_KEY_NoSymbol;
  push(h, (worldr_host_event){.kind = HOST_KEY,
                              .code = sym,
                              .keycode = key,
                              .time = time,
                              .mods = h->mods,
                              .pressed = state == WL_KEYBOARD_KEY_STATE_PRESSED,
                              .depressed = h->depressed,
                              .latched = h->latched,
                              .locked = h->locked,
                              .group = h->group});
}
static void modifiers(void *d, struct wl_keyboard *k, uint32_t serial,
                      uint32_t depressed, uint32_t latched, uint32_t locked,
                      uint32_t group) {
  (void)k;
  (void)serial;
  worldr_host *h = d;
  h->depressed = depressed;
  h->latched = latched;
  h->locked = locked;
  h->group = group;
  if (!h->state) {
    push_modifiers(h);
    return;
  }
  xkb_state_update_mask(h->state, depressed, latched, locked, 0, 0, group);
  h->mods = 0;
  if (xkb_state_mod_name_is_active(h->state, XKB_MOD_NAME_CTRL,
                                   XKB_STATE_MODS_EFFECTIVE) > 0)
    h->mods |= 1;
  if (xkb_state_mod_name_is_active(h->state, XKB_MOD_NAME_SHIFT,
                                   XKB_STATE_MODS_EFFECTIVE) > 0)
    h->mods |= 2;
  if (xkb_state_mod_name_is_active(h->state, XKB_MOD_NAME_ALT,
                                   XKB_STATE_MODS_EFFECTIVE) > 0)
    h->mods |= 4;
  if (xkb_state_mod_name_is_active(h->state, XKB_MOD_NAME_LOGO,
                                   XKB_STATE_MODS_EFFECTIVE) > 0)
    h->mods |= 8;
  push_modifiers(h);
}
static void repeat_info(void *d, struct wl_keyboard *k, int32_t rate,
                        int32_t delay) {
  (void)k;
  worldr_host *h = d;
  h->repeat_rate = rate < 0 ? 0 : rate;
  h->repeat_delay = delay < 0 ? 0 : delay;
  push(h, (worldr_host_event){.kind = HOST_REPEAT_INFO,
                              .repeat_rate = h->repeat_rate,
                              .repeat_delay = h->repeat_delay});
}
static const struct wl_keyboard_listener key_listener = {.keymap = keymap,
                                                         .enter = key_enter,
                                                         .leave = key_leave,
                                                         .key = key_event,
                                                         .modifiers = modifiers,
                                                         .repeat_info =
                                                             repeat_info};
static void capabilities(void *d, struct wl_seat *s, uint32_t caps) {
  worldr_host *h = d;
  if ((caps & WL_SEAT_CAPABILITY_POINTER) && !h->pointer) {
    h->pointer = wl_seat_get_pointer(s);
    wl_pointer_add_listener(h->pointer, &ptr_listener, h);
  }
  if (!(caps & WL_SEAT_CAPABILITY_POINTER) && h->pointer) {
    wl_pointer_destroy(h->pointer);
    h->pointer = NULL;
    h->pointer_inside = 0;
    cancel_pointer(h);
  }
  if ((caps & WL_SEAT_CAPABILITY_KEYBOARD) && !h->keyboard) {
    h->keyboard = wl_seat_get_keyboard(s);
    wl_keyboard_add_listener(h->keyboard, &key_listener, h);
  }
  if (!(caps & WL_SEAT_CAPABILITY_KEYBOARD) && h->keyboard) {
    wl_keyboard_destroy(h->keyboard);
    h->keyboard = NULL;
    reset_keyboard(h);
    worldr_host_clipboard_focus(h->clipboard, 0, 0);
  }
}
static void seat_name(void *d, struct wl_seat *s, const char *name) {
  (void)d;
  (void)s;
  (void)name;
}
static const struct wl_seat_listener seat_listener = {
    .capabilities = capabilities, .name = seat_name};
static void global(void *d, struct wl_registry *r, uint32_t name,
                   const char *iface, uint32_t version) {
  worldr_host *h = d;
  if (!strcmp(iface, "wl_compositor") && !h->compositor && version >= 3) {
    h->compositor = wl_registry_bind(r, name, &wl_compositor_interface,
                                     version < 4 ? version : 4);
    h->compositor_name = name;
  } else if (!strcmp(iface, "xdg_wm_base") && !h->wm) {
    h->wm = wl_registry_bind(r, name, &xdg_wm_base_interface, 1);
    h->wm_name = name;
    xdg_wm_base_add_listener(h->wm, &wm_listener, h);
  } else if (!strcmp(iface, "wl_seat") && !h->seat) {
    h->seat = wl_registry_bind(r, name, &wl_seat_interface,
                               version < 5 ? version : 5);
    h->seat_name = name;
    wl_seat_add_listener(h->seat, &seat_listener, h);
    worldr_host_clipboard_seat(h->clipboard, h->seat);
    worldr_host_text_input_seat(h->text_input, h->seat);
  } else if (!strcmp(iface, "zwp_text_input_manager_v3"))
    worldr_host_text_input_global(h->text_input, r, name, version);
  else if (!strcmp(iface, "wp_fractional_scale_manager_v1") &&
           !h->fractional_manager) {
    h->fractional_manager =
        wl_registry_bind(r, name, &wp_fractional_scale_manager_v1_interface, 1);
    h->fractional_name = name;
    attach_scaling(h);
  } else if (!strcmp(iface, "wp_viewporter") && !h->viewporter) {
    h->viewporter = wl_registry_bind(r, name, &wp_viewporter_interface, 1);
    h->viewporter_name = name;
    attach_scaling(h);
  } else if (!strcmp(iface, "wl_data_device_manager"))
    worldr_host_clipboard_global(h->clipboard, r, name, version);
  else if (!strcmp(iface, "wl_output") && version >= 2) {
    for (int i = 0; i < OUTPUT_CAPACITY; i++)
      if (!h->outputs[i].object) {
        struct output *o = &h->outputs[i];
        o->host = h;
        o->name = name;
        o->scale = 1;
        o->object = wl_registry_bind(r, name, &wl_output_interface, 2);
        wl_output_add_listener(o->object, &output_listener, o);
        break;
      }
  }
}
static void removed(void *d, struct wl_registry *r, uint32_t name) {
  (void)r;
  worldr_host *h = d;
  worldr_host_clipboard_removed(h->clipboard, name);
  worldr_host_text_input_removed(h->text_input, name);
  if (name == h->fractional_name && h->fractional_manager) {
    detach_scaling(h);
    wp_fractional_scale_manager_v1_destroy(h->fractional_manager);
    h->fractional_manager = NULL;
    h->fractional_name = 0;
  }
  if (name == h->viewporter_name && h->viewporter) {
    detach_scaling(h);
    wp_viewporter_destroy(h->viewporter);
    h->viewporter = NULL;
    h->viewporter_name = 0;
  }
  if (name == h->seat_name && h->seat) {
    if (h->pointer)
      wl_pointer_destroy(h->pointer);
    if (h->keyboard)
      wl_keyboard_destroy(h->keyboard);
    h->pointer = NULL;
    h->keyboard = NULL;
    h->pointer_inside = 0;
    worldr_host_clipboard_seat(h->clipboard, NULL);
    worldr_host_text_input_seat(h->text_input, NULL);
    wl_seat_destroy(h->seat);
    h->seat = NULL;
    h->seat_name = 0;
    cancel_pointer(h);
    reset_keyboard(h);
  }
  if (name == h->compositor_name || name == h->wm_name) {
    h->closed = 1;
    push(h, (worldr_host_event){.kind = HOST_CLOSE});
  }
  for (int i = 0; i < OUTPUT_CAPACITY; i++)
    if (h->outputs[i].object && h->outputs[i].name == name) {
      wl_output_destroy(h->outputs[i].object);
      memset(&h->outputs[i], 0, sizeof(h->outputs[i]));
    }
  update_scale(h);
}
static const struct wl_registry_listener registry_listener = {
    .global = global, .global_remove = removed};

static int dispatch(worldr_host *h, int timeout) {
  while (wl_display_prepare_read(h->display) != 0)
    if (wl_display_dispatch_pending(h->display) < 0)
      return -1;
  int flushed = wl_display_flush(h->display);
  if (flushed < 0 && errno != EAGAIN) {
    wl_display_cancel_read(h->display);
    return -1;
  }
  struct pollfd fd = {.fd = wl_display_get_fd(h->display), .events = POLLIN};
  if (flushed < 0)
    fd.events |= POLLOUT;
  int n = poll(&fd, 1, timeout);
  if (n < 0 && errno == EINTR) {
    wl_display_cancel_read(h->display);
    return 0;
  }
  if (n < 0 || (fd.revents & (POLLERR | POLLHUP | POLLNVAL))) {
    wl_display_cancel_read(h->display);
    return -1;
  }
  if (n > 0 && fd.revents & POLLIN) {
    if (wl_display_read_events(h->display) < 0)
      return -1;
  } else
    wl_display_cancel_read(h->display);
  if (n > 0 && fd.revents & POLLOUT && wl_display_flush(h->display) < 0 &&
      errno != EAGAIN)
    return -1;
  return wl_display_dispatch_pending(h->display);
}
static void discovered(void *d, struct wl_callback *callback, uint32_t serial) {
  (void)serial;
  worldr_host *h = d;
  wl_callback_destroy(callback);
  h->discovery = NULL;
  h->discovered = 1;
}
static const struct wl_callback_listener discovery_listener = {.done =
                                                                   discovered};
static int64_t now_ms(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return (int64_t)t.tv_sec * 1000 + t.tv_nsec / 1000000;
}
static int await_flag(worldr_host *h, const int *flag, int64_t deadline) {
  while (!*flag && !h->closed) {
    int64_t remaining = deadline - now_ms();
    if (remaining <= 0) {
      errno = ETIMEDOUT;
      return -1;
    }
    if (dispatch(h, (int)remaining) < 0)
      return -1;
  }
  if (h->closed) {
    errno = ECANCELED;
    return -1;
  }
  return 0;
}
int worldr_host_open(const char *title, int w, int height, int fullscreen,
                     int timeout_ms, worldr_host **out, char *err, int errlen) {
  const char *stage = "connection";
  int64_t deadline = now_ms() + timeout_ms;
  *out = NULL;
  worldr_host *h = calloc(1, sizeof(*h));
  if (!h)
    goto fail;
  if (w <= 0 || height <= 0 || w > 32768 || height > 32768 || timeout_ms <= 0) {
    errno = EINVAL;
    goto fail;
  }
  h->width = w;
  h->height = height;
  h->scale = 1;
  h->scale120 = 120;
  h->display = wl_display_connect(NULL);
  if (!h->display)
    goto fail;
  h->clipboard = worldr_host_clipboard_new();
  if (!h->clipboard)
    goto fail;
  h->text_input = worldr_host_text_input_new(emit_text, h);
  if (!h->text_input)
    goto fail;
  h->xkb = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
  if (!h->xkb)
    goto fail;
  h->registry = wl_display_get_registry(h->display);
  if (!h->registry)
    goto fail;
  wl_registry_add_listener(h->registry, &registry_listener, h);
  stage = "registry discovery";
  h->discovery = wl_display_sync(h->display);
  if (!h->discovery)
    goto fail;
  wl_callback_add_listener(h->discovery, &discovery_listener, h);
  if (await_flag(h, &h->discovered, deadline) < 0)
    goto fail;
  if (!h->compositor || !h->wm) {
    stage = "required wl_compositor v3+ and xdg-shell globals";
    errno = EPROTONOSUPPORT;
    goto fail;
  }
  h->surface = wl_compositor_create_surface(h->compositor);
  if (!h->surface)
    goto fail;
  worldr_host_text_input_surface(h->text_input, h->surface);
  wl_surface_add_listener(h->surface, &surface_listener, h);
  attach_scaling(h);
  h->xdg_surface = xdg_wm_base_get_xdg_surface(h->wm, h->surface);
  if (!h->xdg_surface)
    goto fail;
  xdg_surface_add_listener(h->xdg_surface, &xdg_listener, h);
  h->toplevel = xdg_surface_get_toplevel(h->xdg_surface);
  if (!h->toplevel)
    goto fail;
  xdg_toplevel_add_listener(h->toplevel, &top_listener, h);
  xdg_toplevel_set_title(h->toplevel, title);
  xdg_toplevel_set_app_id(h->toplevel, "worldr");
  xdg_toplevel_set_min_size(h->toplevel, 640, 480);
  if (fullscreen)
    xdg_toplevel_set_fullscreen(h->toplevel, NULL);
  wl_surface_commit(h->surface);
  stage = "initial surface configure";
  if (await_flag(h, &h->configured, deadline) < 0)
    goto fail;
  *out = h;
  return 0;
fail:
  snprintf(err, (size_t)errlen, "Wayland host %s failed: %s", stage,
           strerror(errno));
  worldr_host_close(h);
  return -1;
}
void worldr_host_close(worldr_host *h) {
  if (!h)
    return;
  worldr_host_clipboard_close(h->clipboard);
  worldr_host_text_input_close(h->text_input);
  if (h->discovery)
    wl_callback_destroy(h->discovery);
  if (h->pointer)
    wl_pointer_destroy(h->pointer);
  if (h->keyboard)
    wl_keyboard_destroy(h->keyboard);
  if (h->seat)
    wl_seat_destroy(h->seat);
  if (h->toplevel)
    xdg_toplevel_destroy(h->toplevel);
  if (h->xdg_surface)
    xdg_surface_destroy(h->xdg_surface);
  detach_scaling(h);
  if (h->fractional_manager)
    wp_fractional_scale_manager_v1_destroy(h->fractional_manager);
  if (h->viewporter)
    wp_viewporter_destroy(h->viewporter);
  if (h->surface)
    wl_surface_destroy(h->surface);
  for (int i = 0; i < OUTPUT_CAPACITY; i++)
    if (h->outputs[i].object)
      wl_output_destroy(h->outputs[i].object);
  if (h->wm)
    xdg_wm_base_destroy(h->wm);
  if (h->compositor)
    wl_compositor_destroy(h->compositor);
  if (h->registry)
    wl_registry_destroy(h->registry);
  xkb_state_unref(h->state);
  xkb_keymap_unref(h->keymap);
  xkb_context_unref(h->xkb);
  free(h->keymap_text);
  for (int i = 0; i < h->count; i++)
    event_free(h->events[i]);
  if (h->display)
    wl_display_disconnect(h->display);
  free(h);
}
void *worldr_host_display(worldr_host *h) { return h ? h->display : NULL; }
void *worldr_host_surface(worldr_host *h) { return h ? h->surface : NULL; }
worldr_host_clipboard *worldr_host_clipboard_state(worldr_host *h) {
  return h ? h->clipboard : NULL;
}
worldr_host_text_input *worldr_host_text_input_state(worldr_host *h) {
  return h ? h->text_input : NULL;
}
int worldr_host_scale120(worldr_host *h) { return h ? h->scale120 : 120; }
void worldr_host_logical_size(worldr_host *h, int *w, int *height) {
  *w = h ? h->width : 0;
  *height = h ? h->height : 0;
}
void worldr_host_size(worldr_host *h, int *w, int *height) {
  *w = h ? pixel_size(h->width, h->scale120) : 0;
  *height = h ? pixel_size(h->height, h->scale120) : 0;
}
static int64_t floor_ratio(int64_t value, int logical, int pixels) {
  int64_t n = value * logical;
  return n >= 0 ? n / pixels : -((-n + pixels - 1) / pixels);
}
static int bounded_coordinate(int64_t value) {
  if (value > INT32_MAX)
    return INT32_MAX;
  if (value < INT32_MIN)
    return INT32_MIN;
  return (int)value;
}
void worldr_host_rect_to_surface(worldr_host *h, int *x, int *y, int *w,
                                 int *height) {
  int pw = pixel_size(h->width, h->scale120),
      ph = pixel_size(h->height, h->scale120);
  int64_t right = -floor_ratio(-(int64_t)*x - *w, h->width, pw),
          bottom = -floor_ratio(-(int64_t)*y - *height, h->height, ph);
  int64_t left = floor_ratio(*x, h->width, pw),
          top = floor_ratio(*y, h->height, ph);
  *x = bounded_coordinate(left);
  *y = bounded_coordinate(top);
  *w = bounded_coordinate(right - left);
  *height = bounded_coordinate(bottom - top);
}
int worldr_host_poll(worldr_host *h, worldr_host_event *out, int cap, char *err,
                     int errlen) {
  if (!h) {
    snprintf(err, (size_t)errlen, "Wayland host is closed");
    return -1;
  }
  if (dispatch(h, 0) < 0 && !h->closed) {
    snprintf(err, (size_t)errlen, "Wayland connection lost");
    return -1;
  }
  if (h->overflow) {
    for (int i = 0; i < h->count; i++)
      event_free(h->events[i]);
    h->count = 0;
    h->overflow = 0;
    cancel_pointer(h);
    push(h, (worldr_host_event){.kind = HOST_KEYBOARD_CANCEL});
    worldr_host_text_input_cancel(h->text_input);
    push_keymap(h);
    push_modifiers(h);
    push(h, (worldr_host_event){.kind = HOST_REPEAT_INFO,
                                .repeat_rate = h->repeat_rate,
                                .repeat_delay = h->repeat_delay});
    if (h->closed)
      push(h, (worldr_host_event){.kind = HOST_CLOSE});
  }
  if (h->closed && h->count == 0)
    push(h, (worldr_host_event){.kind = HOST_CLOSE});
  int n = h->count < cap ? h->count : cap;
  memcpy(out, h->events, (size_t)n * sizeof(*out));
  memmove(h->events, h->events + n, (size_t)(h->count - n) * sizeof(*out));
  h->count -= n;
  return n;
}
