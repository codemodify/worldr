//go:build linux && cgo

#include "xwm.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <xcb/composite.h>
#include <xcb/res.h>
#include <xcb/xcb.h>
#include <xcb/xfixes.h>

#define WORLDR_XWM_CLIPBOARD_BYTES (1U << 20)
#define WORLDR_XWM_CLIPBOARD_REQUESTS 64
#define WORLDR_XWM_CLIPBOARD_TARGETS (WORLDR_XWM_CLIPBOARD_MIMES + 3)

enum {
  SURFACE,
  ALLOW,
  STATE,
  PROTOCOLS,
  DELETE_WINDOW,
  TAKE_FOCUS,
  TRANSIENT,
  UTF8,
  NET_NAME,
  NET_SUPPORTED,
  NET_CHECK,
  NET_ACTIVE,
  NET_CLIENTS,
  NET_CLOSE,
  WM_SELECTION,
  CM_SELECTION,
  NET_PID,
  CLIPBOARD,
  TARGETS,
  INCR,
  TEXT,
  TEXT_PLAIN,
  TEXT_PLAIN_UTF8,
  CLIP_PROPERTY,
  XDND_AWARE,
  XDND_SELECTION,
  XDND_ENTER,
  XDND_POSITION,
  XDND_STATUS,
  XDND_LEAVE,
  XDND_DROP,
  XDND_FINISHED,
  XDND_TYPE_LIST,
  XDND_ACTION_COPY,
  XDND_PROPERTY,
  ATOM_COUNT
};
typedef struct {
  uint64_t id, external_id;
  xcb_selection_request_event_t event;
  char mime[WORLDR_XWM_CLIPBOARD_MIME_SIZE];
  int active, delivered;
} clipboard_request;
struct worldr_xwm {
  xcb_connection_t *c;
  xcb_window_t root, support;
  int has_res, xfixes_event;
  xcb_atom_t atom[ATOM_COUNT];
  worldr_xwm_window windows[WORLDR_XWM_WINDOWS];
  uint64_t clipboard_revision, clipboard_id, clipboard_serial;
  uint64_t clipboard_external_id, clipboard_receive_id;
  xcb_window_t clipboard_owner;
  xcb_atom_t clipboard_targets[WORLDR_XWM_CLIPBOARD_TARGETS];
  char clipboard_mimes[WORLDR_XWM_CLIPBOARD_TARGETS]
                       [WORLDR_XWM_CLIPBOARD_MIME_SIZE];
  int clipboard_mime_count, clipboard_querying;
  uint8_t *clipboard_result;
  uint32_t clipboard_result_length;
  int clipboard_result_ready, clipboard_result_ok;
  clipboard_request clipboard_requests[WORLDR_XWM_CLIPBOARD_REQUESTS];
  uint64_t dnd_revision, dnd_id, dnd_serial;
  xcb_window_t dnd_owner, dnd_target;
  uint32_t dnd_pid;
  xcb_atom_t dnd_targets[WORLDR_XWM_CLIPBOARD_MIMES];
  char dnd_mimes[WORLDR_XWM_CLIPBOARD_MIMES]
                [WORLDR_XWM_CLIPBOARD_MIME_SIZE];
  int dnd_mime_count, dnd_querying, dnd_active, dnd_accepted, dnd_dropped,
      dnd_target_version;
};
static const char *atom_names[ATOM_COUNT] = {"WL_SURFACE_ID",
                                             "_XWAYLAND_ALLOW_COMMITS",
                                             "WM_STATE",
                                             "WM_PROTOCOLS",
                                             "WM_DELETE_WINDOW",
                                             "WM_TAKE_FOCUS",
                                             "WM_TRANSIENT_FOR",
                                             "UTF8_STRING",
                                             "_NET_WM_NAME",
                                             "_NET_SUPPORTED",
                                             "_NET_SUPPORTING_WM_CHECK",
                                             "_NET_ACTIVE_WINDOW",
                                             "_NET_CLIENT_LIST",
                                             "_NET_CLOSE_WINDOW",
                                             "WM_S0",
                                             "_NET_WM_CM_S0",
                                             "_NET_WM_PID",
                                             "CLIPBOARD",
                                             "TARGETS",
                                             "INCR",
                                             "TEXT",
                                             "text/plain",
                                             "text/plain;charset=utf-8",
                                             "_WORLDR_CLIPBOARD",
                                             "XdndAware",
                                             "XdndSelection",
                                             "XdndEnter",
                                             "XdndPosition",
                                             "XdndStatus",
                                             "XdndLeave",
                                             "XdndDrop",
                                             "XdndFinished",
                                             "XdndTypeList",
                                             "XdndActionCopy",
                                             "_WORLDR_XDND"};
static uint32_t client_pid(worldr_xwm *x, uint32_t id);
static worldr_xwm_window *window(worldr_xwm *x, uint32_t id, int create) {
  if (!id || id == x->root || id == x->support)
    return NULL;
  worldr_xwm_window *empty = NULL;
  for (int i = 0; i < WORLDR_XWM_WINDOWS; i++) {
    if (x->windows[i].id == id)
      return &x->windows[i];
    if (!x->windows[i].id && !empty)
      empty = &x->windows[i];
  }
  if (create && empty) {
    memset(empty, 0, sizeof(*empty));
    empty->id = id;
    return empty;
  }
  return NULL;
}
static void property(worldr_xwm *x, uint32_t win, uint32_t name, uint32_t type,
                     uint8_t format, uint32_t count, const void *data) {
  xcb_change_property(x->c, XCB_PROP_MODE_REPLACE, win, name, type, format,
                      count, data);
}
static xcb_get_property_reply_t *get(worldr_xwm *x, uint32_t id, uint32_t atom,
                                     uint32_t type, uint32_t count) {
  return xcb_get_property_reply(
      x->c, xcb_get_property(x->c, 0, id, atom, type, 0, count), NULL);
}
static void selection_notify(worldr_xwm *x,
                             const xcb_selection_request_event_t *request,
                             xcb_atom_t property_atom) {
  xcb_selection_notify_event_t event = {0};
  event.response_type = XCB_SELECTION_NOTIFY;
  event.time = request->time;
  event.requestor = request->requestor;
  event.selection = request->selection;
  event.target = request->target;
  event.property = property_atom;
  xcb_send_event(x->c, 0, request->requestor, XCB_EVENT_MASK_NO_EVENT,
                 (const char *)&event);
}
static void clipboard_fail_receive(worldr_xwm *x) {
  if (x->clipboard_receive_id && !x->clipboard_result_ready) {
    x->clipboard_result_ready = 1;
    x->clipboard_result_ok = 0;
    x->clipboard_result_length = 0;
  }
}
static void clipboard_reject_requests(worldr_xwm *x) {
  for (int i = 0; i < WORLDR_XWM_CLIPBOARD_REQUESTS; i++) {
    clipboard_request *request = &x->clipboard_requests[i];
    if (request->active)
      selection_notify(x, &request->event, XCB_ATOM_NONE);
    memset(request, 0, sizeof(*request));
  }
}
static void clipboard_reset_offer(worldr_xwm *x) {
  x->clipboard_mime_count = 0;
  memset(x->clipboard_targets, 0, sizeof(x->clipboard_targets));
  memset(x->clipboard_mimes, 0, sizeof(x->clipboard_mimes));
}
static void clipboard_changed(worldr_xwm *x, uint64_t external_id) {
  x->clipboard_revision++;
  x->clipboard_id = ++x->clipboard_serial;
  x->clipboard_external_id = external_id;
}
static int clipboard_add(worldr_xwm *x, xcb_atom_t target, const char *mime) {
  if (!target || !mime || !mime[0])
    return 0;
  for (int i = 0; i < x->clipboard_mime_count; i++)
    if (x->clipboard_targets[i] == target)
      return 1;
  if (x->clipboard_mime_count >= WORLDR_XWM_CLIPBOARD_TARGETS)
    return 0;
  size_t length = strnlen(mime, WORLDR_XWM_CLIPBOARD_MIME_SIZE);
  if (!length || length >= WORLDR_XWM_CLIPBOARD_MIME_SIZE)
    return 0;
  int at = x->clipboard_mime_count++;
  x->clipboard_targets[at] = target;
  memcpy(x->clipboard_mimes[at], mime, length + 1);
  return 1;
}
static int clipboard_mime_name(const char *name, int length) {
  if (length < 3 || length >= WORLDR_XWM_CLIPBOARD_MIME_SIZE ||
      !memchr(name, '/', (size_t)length))
    return 0;
  for (int i = 0; i < length; i++)
    if ((unsigned char)name[i] < 0x21 || (unsigned char)name[i] > 0x7e)
      return 0;
  return 1;
}
static void clipboard_finish_targets(worldr_xwm *x,
                                     xcb_selection_notify_event_t *event) {
  x->clipboard_querying = 0;
  clipboard_reset_offer(x);
  if (event->property != XCB_ATOM_NONE) {
    xcb_get_property_reply_t *reply = get(
        x, x->support, event->property, XCB_ATOM_ATOM,
        WORLDR_XWM_CLIPBOARD_MIMES * 2);
    if (reply && reply->type == XCB_ATOM_ATOM && reply->format == 32) {
      xcb_atom_t *targets = xcb_get_property_value(reply);
      int count = xcb_get_property_value_length(reply) / 4;
      for (int i = 0; i < count &&
                      x->clipboard_mime_count < WORLDR_XWM_CLIPBOARD_TARGETS;
           i++) {
        xcb_atom_t target = targets[i];
        if (target == x->atom[UTF8]) {
          clipboard_add(x, target, "text/plain;charset=utf-8");
        } else if (target == XCB_ATOM_STRING || target == x->atom[TEXT]) {
          clipboard_add(x, target, "text/plain");
        } else if (target == x->atom[TEXT_PLAIN]) {
          clipboard_add(x, target, "text/plain");
        } else if (target == x->atom[TEXT_PLAIN_UTF8]) {
          clipboard_add(x, target, "text/plain;charset=utf-8");
        } else if (target != x->atom[TARGETS] && target != x->atom[INCR]) {
          xcb_get_atom_name_reply_t *atom = xcb_get_atom_name_reply(
              x->c, xcb_get_atom_name(x->c, target), NULL);
          if (atom && clipboard_mime_name(xcb_get_atom_name_name(atom),
                                          xcb_get_atom_name_name_length(atom))) {
            char name[WORLDR_XWM_CLIPBOARD_MIME_SIZE] = {0};
            int length = xcb_get_atom_name_name_length(atom);
            memcpy(name, xcb_get_atom_name_name(atom), (size_t)length);
            clipboard_add(x, target, name);
          }
          free(atom);
        }
      }
    }
    free(reply);
    xcb_delete_property(x->c, x->support, event->property);
  }
  x->clipboard_revision++;
  x->clipboard_external_id = 0;
  x->clipboard_id = x->clipboard_mime_count ? ++x->clipboard_serial : 0;
}
static void clipboard_external_owner(worldr_xwm *x, xcb_window_t owner,
                                     xcb_timestamp_t timestamp) {
  if (owner == x->support)
    return;
  clipboard_fail_receive(x);
  clipboard_reject_requests(x);
  x->clipboard_owner = owner;
  x->clipboard_external_id = 0;
  x->clipboard_querying = 0;
  if (!owner) {
    if (!x->clipboard_owner && !x->clipboard_id &&
        !x->clipboard_external_id && !x->clipboard_mime_count)
      return;
    clipboard_reset_offer(x);
    x->clipboard_revision++;
    x->clipboard_id = 0;
    return;
  }
  x->clipboard_querying = 1;
  xcb_delete_property(x->c, x->support, x->atom[CLIP_PROPERTY]);
  xcb_convert_selection(x->c, x->support, x->atom[CLIPBOARD],
                        x->atom[TARGETS], x->atom[CLIP_PROPERTY], timestamp);
}

static void dnd_message(worldr_xwm *x, xcb_window_t destination,
                        xcb_atom_t type, uint32_t d0, uint32_t d1,
                        uint32_t d2, uint32_t d3, uint32_t d4) {
  xcb_client_message_event_t event = {0};
  event.response_type = XCB_CLIENT_MESSAGE;
  event.format = 32;
  event.window = destination;
  event.type = type;
  event.data.data32[0] = d0;
  event.data.data32[1] = d1;
  event.data.data32[2] = d2;
  event.data.data32[3] = d3;
  event.data.data32[4] = d4;
  xcb_send_event(x->c, 0, destination, XCB_EVENT_MASK_NO_EVENT,
                 (const char *)&event);
}
static void dnd_reset_types(worldr_xwm *x) {
  x->dnd_mime_count = 0;
  memset(x->dnd_targets, 0, sizeof(x->dnd_targets));
  memset(x->dnd_mimes, 0, sizeof(x->dnd_mimes));
}
static int dnd_add(worldr_xwm *x, xcb_atom_t target, const char *mime) {
  if (!target || !mime || !mime[0])
    return 0;
  for (int i = 0; i < x->dnd_mime_count; i++)
    if (x->dnd_targets[i] == target)
      return 1;
  if (x->dnd_mime_count >= WORLDR_XWM_CLIPBOARD_MIMES)
    return 0;
  size_t length = strnlen(mime, WORLDR_XWM_CLIPBOARD_MIME_SIZE);
  if (!length || length >= WORLDR_XWM_CLIPBOARD_MIME_SIZE)
    return 0;
  int at = x->dnd_mime_count++;
  x->dnd_targets[at] = target;
  memcpy(x->dnd_mimes[at], mime, length + 1);
  return 1;
}
static void dnd_add_atom(worldr_xwm *x, xcb_atom_t target) {
  if (!target || target == x->atom[TARGETS] || target == x->atom[INCR])
    return;
  if (target == x->atom[UTF8] || target == x->atom[TEXT_PLAIN_UTF8]) {
    dnd_add(x, target, "text/plain;charset=utf-8");
    return;
  }
  if (target == XCB_ATOM_STRING || target == x->atom[TEXT] ||
      target == x->atom[TEXT_PLAIN]) {
    dnd_add(x, target, "text/plain");
    return;
  }
  xcb_get_atom_name_reply_t *atom = xcb_get_atom_name_reply(
      x->c, xcb_get_atom_name(x->c, target), NULL);
  if (atom && clipboard_mime_name(xcb_get_atom_name_name(atom),
                                  xcb_get_atom_name_name_length(atom))) {
    char name[WORLDR_XWM_CLIPBOARD_MIME_SIZE] = {0};
    int length = xcb_get_atom_name_name_length(atom);
    memcpy(name, xcb_get_atom_name_name(atom), (size_t)length);
    dnd_add(x, target, name);
  }
  free(atom);
}
static void dnd_finish_targets(worldr_xwm *x,
                               xcb_selection_notify_event_t *event) {
  x->dnd_querying = 0;
  dnd_reset_types(x);
  if (event->property != XCB_ATOM_NONE) {
    xcb_get_property_reply_t *reply =
        get(x, x->support, event->property, XCB_ATOM_ATOM,
            WORLDR_XWM_CLIPBOARD_MIMES + 1);
    if (reply && reply->type == XCB_ATOM_ATOM && reply->format == 32 &&
        reply->bytes_after == 0) {
      xcb_atom_t *targets = xcb_get_property_value(reply);
      int count = xcb_get_property_value_length(reply) / 4;
      for (int i = 0; i < count &&
                      x->dnd_mime_count < WORLDR_XWM_CLIPBOARD_MIMES;
           i++)
        dnd_add_atom(x, targets[i]);
    }
    free(reply);
    xcb_delete_property(x->c, x->support, event->property);
  }
  x->dnd_active = x->dnd_mime_count > 0;
  x->dnd_id = x->dnd_active ? ++x->dnd_serial : 0;
  x->dnd_revision++;
}
static void dnd_external_owner(worldr_xwm *x, xcb_window_t owner,
                               xcb_timestamp_t timestamp) {
  if (owner == x->support)
    return;
  worldr_xwm_dnd_cancel(x);
  dnd_reset_types(x);
  x->dnd_owner = owner;
  x->dnd_pid = owner ? client_pid(x, owner) : 0;
  x->dnd_id = 0;
  x->dnd_querying = 0;
  if (!owner) {
    x->dnd_revision++;
    return;
  }
  xcb_get_property_reply_t *types = get(
      x, owner, x->atom[XDND_TYPE_LIST], XCB_ATOM_ATOM,
      WORLDR_XWM_CLIPBOARD_MIMES + 1);
  if (types && types->type == XCB_ATOM_ATOM && types->format == 32 &&
      types->bytes_after == 0) {
    xcb_atom_t *targets = xcb_get_property_value(types);
    int count = xcb_get_property_value_length(types) / 4;
    for (int i = 0; i < count &&
                    x->dnd_mime_count < WORLDR_XWM_CLIPBOARD_MIMES;
         i++)
      dnd_add_atom(x, targets[i]);
  }
  free(types);
  if (x->dnd_mime_count) {
    x->dnd_active = 1;
    x->dnd_id = ++x->dnd_serial;
    x->dnd_revision++;
    return;
  }
  x->dnd_querying = 1;
  xcb_delete_property(x->c, x->support, x->atom[XDND_PROPERTY]);
  xcb_convert_selection(x->c, x->support, x->atom[XDND_SELECTION],
                        x->atom[TARGETS], x->atom[XDND_PROPERTY], timestamp);
}
static void title(worldr_xwm *x, worldr_xwm_window *w) {
  xcb_get_property_reply_t *r =
      get(x, w->id, x->atom[NET_NAME], x->atom[UTF8], 128);
  if (!r || r->format != 8 || !xcb_get_property_value_length(r)) {
    free(r);
    r = get(x, w->id, XCB_ATOM_WM_NAME, XCB_GET_PROPERTY_TYPE_ANY, 128);
  }
  w->title[0] = 0;
  if (r && r->format == 8) {
    int n = xcb_get_property_value_length(r);
    if (n > 511)
      n = 511;
    memcpy(w->title, xcb_get_property_value(r), (size_t)n);
    w->title[n] = 0;
  }
  free(r);
  r = get(x, w->id, XCB_ATOM_WM_CLASS, XCB_ATOM_STRING, 128);
  w->app_id[0] = 0;
  if (r && r->format == 8) {
    const char *p = xcb_get_property_value(r);
    int n = xcb_get_property_value_length(r);
    const char *end = memchr(p, 0, (size_t)n);
    if (end && end + 1 < p + n && end[1]) {
      n -= (int)(end + 1 - p);
      p = end + 1;
    }
    const char *zero = memchr(p, 0, (size_t)n);
    if (zero)
      n = (int)(zero - p);
    if (n > 255)
      n = 255;
    memcpy(w->app_id, p, (size_t)n);
    w->app_id[n] = 0;
  }
  free(r);
  r = get(x, w->id, x->atom[TRANSIENT], XCB_ATOM_WINDOW, 1);
  w->transient_for = 0;
  if (r && r->format == 32 && xcb_get_property_value_length(r) == 4)
    memcpy(&w->transient_for, xcb_get_property_value(r), 4);
  free(r);
}
static uint32_t client_pid(worldr_xwm *x, uint32_t id) {
  if (!x->has_res)
    return 0;
  xcb_res_client_id_spec_t spec = {id, XCB_RES_CLIENT_ID_MASK_LOCAL_CLIENT_PID};
  xcb_res_query_client_ids_reply_t *r = xcb_res_query_client_ids_reply(
      x->c, xcb_res_query_client_ids(x->c, 1, &spec), NULL);
  if (!r)
    return 0;
  uint32_t pid = 0;
  xcb_res_client_id_value_iterator_t it =
      xcb_res_query_client_ids_ids_iterator(r);
  for (; it.rem; xcb_res_client_id_value_next(&it))
    if ((it.data->spec.mask & XCB_RES_CLIENT_ID_MASK_LOCAL_CLIENT_PID) &&
        xcb_res_client_id_value_value_length(it.data) == 1)
      pid = *xcb_res_client_id_value_value(it.data);
  free(r);
  return pid;
}
static void geometry(worldr_xwm *x, worldr_xwm_window *w) {
  xcb_get_geometry_reply_t *r =
      xcb_get_geometry_reply(x->c, xcb_get_geometry(x->c, w->id), NULL);
  if (r) {
    w->x = r->x;
    w->y = r->y;
    w->width = r->width;
    w->height = r->height;
    free(r);
  }
  xcb_get_window_attributes_reply_t *a = xcb_get_window_attributes_reply(
      x->c, xcb_get_window_attributes(x->c, w->id), NULL);
  if (a) {
    w->override_redirect = a->override_redirect;
    free(a);
  }
}
static void clients(worldr_xwm *x) {
  uint32_t list[WORLDR_XWM_WINDOWS];
  int n = 0;
  for (int i = 0; i < WORLDR_XWM_WINDOWS; i++)
    if (x->windows[i].mapped && !x->windows[i].override_redirect)
      list[n++] = x->windows[i].id;
  property(x, x->root, x->atom[NET_CLIENTS], XCB_ATOM_WINDOW, 32, (uint32_t)n,
           list);
}
static int supports(worldr_xwm *x, uint32_t id, uint32_t atom) {
  xcb_get_property_reply_t *r =
      get(x, id, x->atom[PROTOCOLS], XCB_ATOM_ATOM, 64);
  int found = 0;
  if (r && r->format == 32) {
    uint32_t *p = xcb_get_property_value(r);
    int n = xcb_get_property_value_length(r) / 4;
    for (int i = 0; i < n; i++)
      if (p[i] == atom)
        found = 1;
  }
  free(r);
  return found;
}
static void protocol(worldr_xwm *x, uint32_t id, uint32_t atom) {
  xcb_client_message_event_t event = {0};
  event.response_type = XCB_CLIENT_MESSAGE;
  event.format = 32;
  event.window = id;
  event.type = x->atom[PROTOCOLS];
  event.data.data32[0] = atom;
  event.data.data32[1] = XCB_CURRENT_TIME;
  xcb_send_event(x->c, 0, id, XCB_EVENT_MASK_NO_EVENT, (const char *)&event);
}
worldr_xwm *worldr_xwm_open(int fd, char *error, int capacity) {
  worldr_xwm *x = calloc(1, sizeof(*x));
  if (!x) {
    close(fd);
    snprintf(error, (size_t)capacity, "out of memory");
    return NULL;
  }
  x->c = xcb_connect_to_fd(fd, NULL);
  if (!x->c || xcb_connection_has_error(x->c))
    goto failed;
  const xcb_setup_t *setup = xcb_get_setup(x->c);
  xcb_screen_iterator_t screen = xcb_setup_roots_iterator(setup);
  if (!screen.rem)
    goto failed;
  x->root = screen.data->root;
  const xcb_query_extension_reply_t *res =
      xcb_get_extension_data(x->c, &xcb_res_id);
  if (res && res->present) {
    xcb_res_query_version_reply_t *v = xcb_res_query_version_reply(
        x->c, xcb_res_query_version(x->c, 1, 2), NULL);
    x->has_res = v && (v->server_major > 1 ||
                       (v->server_major == 1 && v->server_minor >= 2));
    free(v);
  }
  const xcb_query_extension_reply_t *xfixes =
      xcb_get_extension_data(x->c, &xcb_xfixes_id);
  if (!xfixes || !xfixes->present)
    goto failed;
  xcb_xfixes_query_version_reply_t *xfixes_version =
      xcb_xfixes_query_version_reply(
          x->c, xcb_xfixes_query_version(x->c, 1, 0), NULL);
  if (!xfixes_version)
    goto failed;
  free(xfixes_version);
  x->xfixes_event = xfixes->first_event;
  for (int i = 0; i < ATOM_COUNT; i++) {
    xcb_intern_atom_reply_t *r = xcb_intern_atom_reply(
        x->c,
        xcb_intern_atom(x->c, 0, (uint16_t)strlen(atom_names[i]),
                        atom_names[i]),
        NULL);
    if (!r)
      goto failed;
    x->atom[i] = r->atom;
    free(r);
  }
  uint32_t mask = XCB_EVENT_MASK_SUBSTRUCTURE_REDIRECT |
                  XCB_EVENT_MASK_SUBSTRUCTURE_NOTIFY |
                  XCB_EVENT_MASK_PROPERTY_CHANGE;
  xcb_generic_error_t *err =
      xcb_request_check(x->c, xcb_change_window_attributes_checked(
                                  x->c, x->root, XCB_CW_EVENT_MASK, &mask));
  if (err) {
    free(err);
    goto failed;
  }
  xcb_composite_query_version_reply_t *version =
      xcb_composite_query_version_reply(
          x->c, xcb_composite_query_version(x->c, 0, 4), NULL);
  if (!version)
    goto failed;
  free(version);
  err = xcb_request_check(x->c,
                          xcb_composite_redirect_subwindows_checked(
                              x->c, x->root, XCB_COMPOSITE_REDIRECT_MANUAL));
  if (err) {
    free(err);
    goto failed;
  }
  x->support = xcb_generate_id(x->c);
  xcb_create_window(x->c, XCB_COPY_FROM_PARENT, x->support, x->root, 0, 0, 1, 1,
                    0, XCB_WINDOW_CLASS_INPUT_OUTPUT, screen.data->root_visual,
                    0, NULL);
  property(x, x->root, x->atom[NET_CHECK], XCB_ATOM_WINDOW, 32, 1, &x->support);
  property(x, x->support, x->atom[NET_CHECK], XCB_ATOM_WINDOW, 32, 1,
           &x->support);
  property(x, x->support, x->atom[NET_NAME], x->atom[UTF8], 8, 6, "worldr");
  uint32_t supported[] = {x->atom[NET_CHECK], x->atom[NET_NAME],
                          x->atom[NET_ACTIVE], x->atom[NET_CLIENTS],
                          x->atom[NET_CLOSE]};
  property(x, x->root, x->atom[NET_SUPPORTED], XCB_ATOM_ATOM, 32, 5, supported);
  xcb_set_selection_owner(x->c, x->support, x->atom[WM_SELECTION],
                          XCB_CURRENT_TIME);
  xcb_set_selection_owner(x->c, x->support, x->atom[CM_SELECTION],
                          XCB_CURRENT_TIME);
  xcb_xfixes_select_selection_input(
      x->c, x->support, x->atom[CLIPBOARD],
      XCB_XFIXES_SELECTION_EVENT_MASK_SET_SELECTION_OWNER |
          XCB_XFIXES_SELECTION_EVENT_MASK_SELECTION_WINDOW_DESTROY |
          XCB_XFIXES_SELECTION_EVENT_MASK_SELECTION_CLIENT_CLOSE);
  xcb_xfixes_select_selection_input(
      x->c, x->support, x->atom[XDND_SELECTION],
      XCB_XFIXES_SELECTION_EVENT_MASK_SET_SELECTION_OWNER |
          XCB_XFIXES_SELECTION_EVENT_MASK_SELECTION_WINDOW_DESTROY |
          XCB_XFIXES_SELECTION_EVENT_MASK_SELECTION_CLIENT_CLOSE);
  xcb_get_selection_owner_reply_t *clipboard_owner =
      xcb_get_selection_owner_reply(
          x->c, xcb_get_selection_owner(x->c, x->atom[CLIPBOARD]), NULL);
  if (clipboard_owner) {
    clipboard_external_owner(x, clipboard_owner->owner, XCB_CURRENT_TIME);
    free(clipboard_owner);
  }
  xcb_get_selection_owner_reply_t *dnd_owner = xcb_get_selection_owner_reply(
      x->c, xcb_get_selection_owner(x->c, x->atom[XDND_SELECTION]), NULL);
  if (dnd_owner) {
    dnd_external_owner(x, dnd_owner->owner, XCB_CURRENT_TIME);
    free(dnd_owner);
  }
  xcb_flush(x->c);
  return x;
failed:
  snprintf(error, (size_t)capacity,
           "could not initialize private X11 window manager");
  worldr_xwm_close(x);
  return NULL;
}
void worldr_xwm_close(worldr_xwm *x) {
  if (x) {
    free(x->clipboard_result);
    if (x->c)
      xcb_disconnect(x->c);
    free(x);
  }
}
static void map(worldr_xwm *x, uint32_t id, int request) {
  worldr_xwm_window *w = window(x, id, 1);
  if (!w)
    return;
  geometry(x, w);
  title(x, w);
  w->pid = client_pid(x, id);
  w->mapped = 1;
  uint32_t mask =
      XCB_EVENT_MASK_STRUCTURE_NOTIFY | XCB_EVENT_MASK_PROPERTY_CHANGE;
  xcb_change_window_attributes(x->c, id, XCB_CW_EVENT_MASK, &mask);
  uint32_t border = 0;
  if (!w->override_redirect)
    xcb_configure_window(x->c, id, XCB_CONFIG_WINDOW_BORDER_WIDTH, &border);
  uint32_t allow = 1, state[] = {1, 0};
  property(x, id, x->atom[ALLOW], XCB_ATOM_CARDINAL, 32, 1, &allow);
  if (!w->override_redirect)
    property(x, id, x->atom[STATE], x->atom[STATE], 32, 2, state);
  if (request)
    xcb_map_window(x->c, id);
  clients(x);
}
int worldr_xwm_poll(worldr_xwm *x, worldr_xwm_window *out, int capacity) {
  for (int n = 0; n < 128; n++) {
    xcb_generic_event_t *event = xcb_poll_for_event(x->c);
    if (!event)
      break;
    uint8_t response_type = event->response_type & 0x7f;
    if (response_type ==
        x->xfixes_event + XCB_XFIXES_SELECTION_NOTIFY) {
      xcb_xfixes_selection_notify_event_t *selection = (void *)event;
      if (selection->selection == x->atom[CLIPBOARD])
        clipboard_external_owner(x, selection->owner,
                                 selection->selection_timestamp);
      else if (selection->selection == x->atom[XDND_SELECTION])
        dnd_external_owner(x, selection->owner,
                           selection->selection_timestamp);
      free(event);
      continue;
    }
    switch (response_type) {
    case XCB_MAP_REQUEST:
      map(x, ((xcb_map_request_event_t *)event)->window, 1);
      break;
    case XCB_MAP_NOTIFY: {
      xcb_map_notify_event_t *e = (void *)event;
      if (e->event == x->root)
        map(x, e->window, 0);
      break;
    }
    case XCB_UNMAP_NOTIFY: {
      xcb_unmap_notify_event_t *e = (void *)event;
      if (e->window == x->dnd_owner || e->window == x->dnd_target)
        worldr_xwm_dnd_cancel(x);
      worldr_xwm_window *w = window(x, e->window, 0);
      if (w) {
        w->mapped = 0;
        w->object_id = 0;
        uint32_t state[] = {0, 0};
        property(x, w->id, x->atom[STATE], x->atom[STATE], 32, 2, state);
        clients(x);
      }
      break;
    }
    case XCB_DESTROY_NOTIFY: {
      uint32_t destroyed = ((xcb_destroy_notify_event_t *)event)->window;
      if (destroyed == x->dnd_owner || destroyed == x->dnd_target)
        worldr_xwm_dnd_cancel(x);
      worldr_xwm_window *w =
          window(x, destroyed, 0);
      if (w) {
        memset(w, 0, sizeof(*w));
        clients(x);
      }
      break;
    }
    case XCB_PROPERTY_NOTIFY: {
      xcb_property_notify_event_t *e = (void *)event;
      worldr_xwm_window *w = window(x, e->window, 0);
      if (w && (e->atom == XCB_ATOM_WM_NAME || e->atom == XCB_ATOM_WM_CLASS ||
                e->atom == x->atom[NET_NAME] || e->atom == x->atom[TRANSIENT] ||
                e->atom == x->atom[NET_PID]))
        title(x, w);
      break;
    }
    case XCB_CONFIGURE_REQUEST: {
      xcb_configure_request_event_t *e = (void *)event;
      uint32_t values[7];
      int i = 0;
      if (e->value_mask & XCB_CONFIG_WINDOW_X)
        values[i++] = (uint32_t)(int32_t)e->x;
      if (e->value_mask & XCB_CONFIG_WINDOW_Y)
        values[i++] = (uint32_t)(int32_t)e->y;
      if (e->value_mask & XCB_CONFIG_WINDOW_WIDTH)
        values[i++] = e->width > 4096 ? 4096 : e->width;
      if (e->value_mask & XCB_CONFIG_WINDOW_HEIGHT)
        values[i++] = e->height > 4096 ? 4096 : e->height;
      if (e->value_mask & XCB_CONFIG_WINDOW_BORDER_WIDTH)
        values[i++] = 0;
      if (e->value_mask & XCB_CONFIG_WINDOW_SIBLING)
        values[i++] = e->sibling;
      if (e->value_mask & XCB_CONFIG_WINDOW_STACK_MODE)
        values[i++] = e->stack_mode;
      xcb_configure_window(x->c, e->window, e->value_mask, values);
      break;
    }
    case XCB_CONFIGURE_NOTIFY: {
      xcb_configure_notify_event_t *e = (void *)event;
      worldr_xwm_window *w = window(x, e->window, 0);
      if (w) {
        w->x = e->x;
        w->y = e->y;
        w->width = e->width;
        w->height = e->height;
      }
      break;
    }
    case XCB_CLIENT_MESSAGE: {
      xcb_client_message_event_t *e = (void *)event;
      if (e->format != 32)
        break;
      if (e->type == x->atom[SURFACE]) {
        worldr_xwm_window *w = window(x, e->window, 1);
        if (w)
          w->object_id = e->data.data32[0];
      } else if (e->type == x->atom[NET_CLOSE])
        worldr_xwm_delete(x, e->window);
      else if (e->window == x->support &&
               e->type == x->atom[XDND_STATUS] && x->dnd_active &&
               e->data.data32[0] == x->dnd_target) {
        int accepted = (e->data.data32[1] & 1) &&
                       e->data.data32[4] == x->atom[XDND_ACTION_COPY];
        x->dnd_accepted = accepted;
        dnd_message(x, x->dnd_owner, x->atom[XDND_STATUS], x->dnd_target,
                    accepted ? e->data.data32[1] : 0, e->data.data32[2],
                    e->data.data32[3],
                    accepted ? x->atom[XDND_ACTION_COPY] : XCB_ATOM_NONE);
      } else if (e->window == x->support &&
                 e->type == x->atom[XDND_FINISHED] && x->dnd_active &&
                 e->data.data32[0] == x->dnd_target) {
        int succeeded =
            x->dnd_dropped &&
            (x->dnd_target_version < 5 ||
             ((e->data.data32[1] & 1) &&
              e->data.data32[2] == x->atom[XDND_ACTION_COPY]));
        dnd_message(x, x->dnd_owner, x->atom[XDND_FINISHED], x->dnd_target,
                    succeeded ? 1 : 0,
                    succeeded ? x->atom[XDND_ACTION_COPY] : XCB_ATOM_NONE, 0,
                    0);
        x->dnd_target = XCB_WINDOW_NONE;
        x->dnd_active = x->dnd_accepted = x->dnd_dropped = 0;
        x->dnd_target_version = 0;
        x->dnd_revision++;
      }
      // _NET_ACTIVE_WINDOW is intentionally not a grant of workspace focus.
      break;
    }
    case XCB_SELECTION_REQUEST: {
      xcb_selection_request_event_t *request = (void *)event;
      if (request->selection != x->atom[CLIPBOARD] ||
          x->clipboard_owner != x->support ||
          !x->clipboard_external_id) {
        selection_notify(x, request, XCB_ATOM_NONE);
        break;
      }
      xcb_atom_t destination = request->property;
      if (destination == XCB_ATOM_NONE)
        destination = request->target;
      if (request->target == x->atom[TARGETS]) {
        xcb_atom_t targets[WORLDR_XWM_CLIPBOARD_TARGETS + 1];
        int count = 0;
        targets[count++] = x->atom[TARGETS];
        for (int i = 0; i < x->clipboard_mime_count; i++) {
          int duplicate = 0;
          for (int j = 0; j < count; j++)
            duplicate |= targets[j] == x->clipboard_targets[i];
          if (!duplicate)
            targets[count++] = x->clipboard_targets[i];
        }
        property(x, request->requestor, destination, XCB_ATOM_ATOM, 32,
                 (uint32_t)count, targets);
        selection_notify(x, request, destination);
        break;
      }
      const char *mime = NULL;
      for (int i = 0; i < x->clipboard_mime_count; i++)
        if (request->target == x->clipboard_targets[i]) {
          mime = x->clipboard_mimes[i];
          break;
        }
      clipboard_request *pending = NULL;
      for (int i = 0; i < WORLDR_XWM_CLIPBOARD_REQUESTS; i++)
        if (!x->clipboard_requests[i].active) {
          pending = &x->clipboard_requests[i];
          break;
        }
      if (!mime || !pending) {
        selection_notify(x, request, XCB_ATOM_NONE);
        break;
      }
      pending->active = 1;
      pending->id = ++x->clipboard_serial;
      pending->external_id = x->clipboard_external_id;
      pending->event = *request;
      pending->event.property = destination;
      memcpy(pending->mime, mime, strlen(mime) + 1);
      break;
    }
    case XCB_SELECTION_NOTIFY: {
      xcb_selection_notify_event_t *selection = (void *)event;
      if (selection->selection == x->atom[XDND_SELECTION] &&
          selection->requestor == x->support) {
        if (x->dnd_querying && selection->target == x->atom[TARGETS])
          dnd_finish_targets(x, selection);
        break;
      }
      if (selection->selection != x->atom[CLIPBOARD] ||
          selection->requestor != x->support)
        break;
      if (x->clipboard_querying && selection->target == x->atom[TARGETS]) {
        clipboard_finish_targets(x, selection);
        break;
      }
      if (!x->clipboard_receive_id)
        break;
      x->clipboard_result_ready = 1;
      x->clipboard_result_ok = 0;
      x->clipboard_result_length = 0;
      if (selection->property != XCB_ATOM_NONE) {
        xcb_get_property_reply_t *reply = get(
            x, x->support, selection->property, XCB_GET_PROPERTY_TYPE_ANY,
            WORLDR_XWM_CLIPBOARD_BYTES / 4 + 1);
        if (reply && reply->type != x->atom[INCR] && reply->format == 8 &&
            reply->bytes_after == 0 &&
            xcb_get_property_value_length(reply) <=
                (int)WORLDR_XWM_CLIPBOARD_BYTES) {
          int length = xcb_get_property_value_length(reply);
          uint8_t *data = NULL;
          if (length)
            data = malloc((size_t)length);
          if (!length || data) {
            if (length)
              memcpy(data, xcb_get_property_value(reply), (size_t)length);
            free(x->clipboard_result);
            x->clipboard_result = data;
            x->clipboard_result_length = (uint32_t)length;
            x->clipboard_result_ok = 1;
          }
        }
        free(reply);
        xcb_delete_property(x->c, x->support, selection->property);
      }
      break;
    }
    }
    free(event);
  }
  if (xcb_connection_has_error(x->c))
    return -1;
  xcb_flush(x->c);
  int n = 0;
  for (int i = 0; i < WORLDR_XWM_WINDOWS && n < capacity; i++)
    if (x->windows[i].mapped && x->windows[i].object_id)
      out[n++] = x->windows[i];
  return n;
}
void worldr_xwm_focus(worldr_xwm *x, uint32_t id) {
  worldr_xwm_window *w = window(x, id, 0);
  if (id && (!w || !w->mapped || w->override_redirect))
    return;
  xcb_set_input_focus(x->c, XCB_INPUT_FOCUS_POINTER_ROOT, id ? id : x->root,
                      XCB_CURRENT_TIME);
  if (id && supports(x, id, x->atom[TAKE_FOCUS]))
    protocol(x, id, x->atom[TAKE_FOCUS]);
  property(x, x->root, x->atom[NET_ACTIVE], XCB_ATOM_WINDOW, 32, 1, &id);
  xcb_flush(x->c);
}
void worldr_xwm_resize(worldr_xwm *x, uint32_t id, int width, int height) {
  if (!window(x, id, 0))
    return;
  uint32_t values[] = {(uint32_t)width, (uint32_t)height, 0};
  xcb_configure_window(x->c, id,
                       XCB_CONFIG_WINDOW_WIDTH | XCB_CONFIG_WINDOW_HEIGHT |
                           XCB_CONFIG_WINDOW_BORDER_WIDTH,
                       values);
  xcb_flush(x->c);
}
void worldr_xwm_delete(worldr_xwm *x, uint32_t id) {
  if (!window(x, id, 0))
    return;
  if (supports(x, id, x->atom[DELETE_WINDOW]))
    protocol(x, id, x->atom[DELETE_WINDOW]);
  else
    xcb_kill_client(x->c, id);
  xcb_flush(x->c);
}
static int dnd_source_matches(worldr_xwm *x, uint32_t source) {
  if (!x->dnd_active || !source)
    return 0;
  if (source == x->dnd_owner)
    return 1;
  worldr_xwm_window *w = window(x, source, 0);
  return w && w->pid && x->dnd_pid && w->pid == x->dnd_pid;
}
static uint32_t dnd_version(worldr_xwm *x, uint32_t target) {
  xcb_get_property_reply_t *reply =
      get(x, target, x->atom[XDND_AWARE], XCB_ATOM_ATOM, 1);
  uint32_t version = 0;
  if (reply && reply->type == XCB_ATOM_ATOM && reply->format == 32 &&
      xcb_get_property_value_length(reply) == 4)
    memcpy(&version, xcb_get_property_value(reply), 4);
  free(reply);
  if (version < 3)
    return 0;
  return version > 5 ? 5 : version;
}
static void dnd_leave(worldr_xwm *x) {
  if (x->dnd_target && !x->dnd_dropped)
    dnd_message(x, x->dnd_target, x->atom[XDND_LEAVE], x->support, 0, 0, 0,
                0);
  x->dnd_target = XCB_WINDOW_NONE;
  x->dnd_accepted = 0;
  x->dnd_target_version = 0;
}
void worldr_xwm_get_dnd_offer(worldr_xwm *x, worldr_xwm_dnd_offer *out) {
  memset(out, 0, sizeof(*out));
  out->revision = x->dnd_revision;
  out->id = x->dnd_id;
  out->owner = x->dnd_owner;
  out->pid = x->dnd_pid;
  out->target = x->dnd_target;
  out->active = x->dnd_active;
  out->accepted = x->dnd_accepted;
  out->dropped = x->dnd_dropped;
  out->mime_count = x->dnd_mime_count;
  for (int i = 0; i < x->dnd_mime_count; i++)
    memcpy(out->mimes[i], x->dnd_mimes[i],
           strlen(x->dnd_mimes[i]) + 1);
}
int worldr_xwm_dnd_motion(worldr_xwm *x, uint32_t source, uint32_t target,
                          int px, int py, uint32_t time) {
  if (!dnd_source_matches(x, source) || x->dnd_dropped)
    return -1;
  if (!target) {
    dnd_leave(x);
    xcb_flush(x->c);
    return 0;
  }
  worldr_xwm_window *destination = window(x, target, 0);
  uint32_t version = destination ? dnd_version(x, target) : 0;
  if (!destination || !destination->mapped || !version) {
    dnd_leave(x);
    xcb_flush(x->c);
    return -1;
  }
  if (x->dnd_target != target) {
    dnd_leave(x);
    x->dnd_target = target;
    x->dnd_target_version = (int)version;
    property(x, x->support, x->atom[XDND_TYPE_LIST], XCB_ATOM_ATOM, 32,
             (uint32_t)x->dnd_mime_count, x->dnd_targets);
    uint32_t first = x->dnd_mime_count > 0 ? x->dnd_targets[0] : 0;
    uint32_t second = x->dnd_mime_count > 1 ? x->dnd_targets[1] : 0;
    uint32_t third = x->dnd_mime_count > 2 ? x->dnd_targets[2] : 0;
    dnd_message(x, target, x->atom[XDND_ENTER], x->support,
                (version << 24) | (x->dnd_mime_count > 3), first, second,
                third);
  }
  int64_t root_x = (int64_t)destination->x + px;
  int64_t root_y = (int64_t)destination->y + py;
  if (root_x < INT16_MIN)
    root_x = INT16_MIN;
  if (root_x > INT16_MAX)
    root_x = INT16_MAX;
  if (root_y < INT16_MIN)
    root_y = INT16_MIN;
  if (root_y > INT16_MAX)
    root_y = INT16_MAX;
  uint32_t packed = ((uint32_t)(uint16_t)root_x << 16) |
                    (uint32_t)(uint16_t)root_y;
  dnd_message(x, target, x->atom[XDND_POSITION], x->support, 0, packed, time,
              x->atom[XDND_ACTION_COPY]);
  xcb_flush(x->c);
  return 0;
}
int worldr_xwm_dnd_drop(worldr_xwm *x, uint32_t source, uint32_t target,
                        uint32_t time) {
  if (!dnd_source_matches(x, source) || !target || target != x->dnd_target ||
      !x->dnd_accepted || x->dnd_dropped)
    return -1;
  x->dnd_dropped = 1;
  x->dnd_revision++;
  dnd_message(x, target, x->atom[XDND_DROP], x->support, 0, time, 0, 0);
  xcb_flush(x->c);
  return 0;
}
void worldr_xwm_dnd_cancel(worldr_xwm *x) {
  if (!x)
    return;
  int changed = x->dnd_active || x->dnd_target || x->dnd_dropped;
  if (x->dnd_dropped && x->dnd_owner)
    dnd_message(x, x->dnd_owner, x->atom[XDND_FINISHED], x->dnd_target, 0,
                XCB_ATOM_NONE, 0, 0);
  else
    dnd_leave(x);
  x->dnd_target = XCB_WINDOW_NONE;
  x->dnd_active = x->dnd_accepted = x->dnd_dropped = 0;
  x->dnd_target_version = 0;
  if (changed)
    x->dnd_revision++;
  if (x->c)
    xcb_flush(x->c);
}
void worldr_xwm_get_clipboard_offer(worldr_xwm *x,
                                    worldr_xwm_clipboard_offer *out) {
  memset(out, 0, sizeof(*out));
  out->revision = x->clipboard_revision;
  out->id = x->clipboard_id;
  out->external_id = x->clipboard_external_id;
  for (int i = 0; i < x->clipboard_mime_count; i++) {
    int duplicate = 0;
    for (int j = 0; j < out->mime_count; j++)
      duplicate |= strcmp(out->mimes[j], x->clipboard_mimes[i]) == 0;
    if (!duplicate && out->mime_count < WORLDR_XWM_CLIPBOARD_MIMES) {
      memcpy(out->mimes[out->mime_count], x->clipboard_mimes[i],
             strlen(x->clipboard_mimes[i]) + 1);
      out->mime_count++;
    }
  }
}
static xcb_atom_t clipboard_atom(worldr_xwm *x, const char *name) {
  xcb_intern_atom_reply_t *reply = xcb_intern_atom_reply(
      x->c, xcb_intern_atom(x->c, 0, (uint16_t)strlen(name), name), NULL);
  if (!reply)
    return XCB_ATOM_NONE;
  xcb_atom_t atom = reply->atom;
  free(reply);
  return atom;
}
static int text_mime(const char *mime) {
  return strcmp(mime, "text/plain") == 0 ||
         strcmp(mime, "text/plain;charset=utf-8") == 0 ||
         strcmp(mime, "text/plain;charset=UTF-8") == 0;
}
int worldr_xwm_clipboard_publish(worldr_xwm *x, uint64_t external_id,
                                 const char **mimes, int count) {
  if (count < 0 || count > WORLDR_XWM_CLIPBOARD_MIMES ||
      (!external_id && count))
    return -1;
  clipboard_fail_receive(x);
  clipboard_reject_requests(x);
  clipboard_reset_offer(x);
  x->clipboard_querying = 0;
  if (!external_id) {
    x->clipboard_owner = XCB_WINDOW_NONE;
    x->clipboard_external_id = 0;
    x->clipboard_revision++;
    x->clipboard_id = 0;
    xcb_set_selection_owner(x->c, XCB_WINDOW_NONE, x->atom[CLIPBOARD],
                            XCB_CURRENT_TIME);
    xcb_flush(x->c);
    return 0;
  }
  const char *utf8_mime = NULL, *plain_mime = NULL;
  for (int i = 0; i < count; i++) {
    size_t length = mimes[i]
                        ? strnlen(mimes[i], WORLDR_XWM_CLIPBOARD_MIME_SIZE)
                        : 0;
    if (!length || length >= WORLDR_XWM_CLIPBOARD_MIME_SIZE)
      return -1;
    xcb_atom_t atom = clipboard_atom(x, mimes[i]);
    if (!atom || !clipboard_add(x, atom, mimes[i]))
      return -1;
    if (text_mime(mimes[i])) {
      if (!plain_mime || strcmp(mimes[i], "text/plain") == 0)
        plain_mime = mimes[i];
      if (!utf8_mime || strcmp(mimes[i], "text/plain;charset=utf-8") == 0 ||
          strcmp(mimes[i], "text/plain;charset=UTF-8") == 0)
        utf8_mime = mimes[i];
    }
  }
  if (utf8_mime)
    clipboard_add(x, x->atom[UTF8], utf8_mime);
  if (plain_mime) {
    clipboard_add(x, XCB_ATOM_STRING, plain_mime);
    clipboard_add(x, x->atom[TEXT], plain_mime);
  }
  if (!x->clipboard_mime_count)
    return -1;
  x->clipboard_owner = x->support;
  clipboard_changed(x, external_id);
  xcb_set_selection_owner(x->c, x->support, x->atom[CLIPBOARD],
                          XCB_CURRENT_TIME);
  xcb_flush(x->c);
  return 0;
}
int worldr_xwm_clipboard_receive(worldr_xwm *x, uint64_t id,
                                 uint64_t offer_id, const char *mime) {
  if (!id || x->clipboard_receive_id || x->clipboard_result_ready ||
      !x->clipboard_owner || x->clipboard_owner == x->support ||
      x->clipboard_external_id || offer_id != x->clipboard_id)
    return -1;
  xcb_atom_t target = XCB_ATOM_NONE;
  for (int i = 0; i < x->clipboard_mime_count; i++)
    if (strcmp(mime, x->clipboard_mimes[i]) == 0) {
      target = x->clipboard_targets[i];
      break;
    }
  if (!target)
    return -1;
  x->clipboard_receive_id = id;
  xcb_delete_property(x->c, x->support, x->atom[CLIP_PROPERTY]);
  xcb_convert_selection(x->c, x->support, x->atom[CLIPBOARD], target,
                        x->atom[CLIP_PROPERTY], XCB_CURRENT_TIME);
  xcb_flush(x->c);
  return 0;
}
void worldr_xwm_clipboard_cancel(worldr_xwm *x, uint64_t id) {
  if (id == x->clipboard_receive_id)
    clipboard_fail_receive(x);
}
int worldr_xwm_clipboard_result(worldr_xwm *x, uint64_t *id, int *ok,
                                uint8_t **data, uint32_t *length) {
  if (!x->clipboard_result_ready)
    return 0;
  *id = x->clipboard_receive_id;
  *ok = x->clipboard_result_ok;
  *data = x->clipboard_result;
  *length = x->clipboard_result_length;
  return 1;
}
void worldr_xwm_clipboard_consume_result(worldr_xwm *x) {
  free(x->clipboard_result);
  x->clipboard_result = NULL;
  x->clipboard_result_length = 0;
  x->clipboard_result_ready = 0;
  x->clipboard_result_ok = 0;
  x->clipboard_receive_id = 0;
}
int worldr_xwm_clipboard_requests(worldr_xwm *x,
                                  worldr_xwm_clipboard_request *out,
                                  int capacity) {
  int count = 0;
  for (int i = 0; i < WORLDR_XWM_CLIPBOARD_REQUESTS && count < capacity; i++) {
    clipboard_request *request = &x->clipboard_requests[i];
    if (!request->active || request->delivered)
      continue;
    out[count].id = request->id;
    out[count].external_id = request->external_id;
    memcpy(out[count].mime, request->mime, strlen(request->mime) + 1);
    request->delivered = 1;
    count++;
  }
  return count;
}
void worldr_xwm_clipboard_reply(worldr_xwm *x, uint64_t id,
                                const uint8_t *data, uint32_t length, int ok) {
  clipboard_request *request = NULL;
  for (int i = 0; i < WORLDR_XWM_CLIPBOARD_REQUESTS; i++)
    if (x->clipboard_requests[i].active &&
        x->clipboard_requests[i].id == id) {
      request = &x->clipboard_requests[i];
      break;
    }
  if (!request)
    return;
  uint32_t maximum = xcb_get_maximum_request_length(x->c);
  uint64_t safe = maximum > 16 ? ((uint64_t)maximum - 16) * 4 : 0;
  if (length > WORLDR_XWM_CLIPBOARD_BYTES || length > safe)
    ok = 0;
  if (ok && x->clipboard_owner == x->support &&
      request->external_id == x->clipboard_external_id) {
    property(x, request->event.requestor, request->event.property,
             request->event.target, 8, length, data);
    selection_notify(x, &request->event, request->event.property);
  } else {
    selection_notify(x, &request->event, XCB_ATOM_NONE);
  }
  memset(request, 0, sizeof(*request));
  xcb_flush(x->c);
}
