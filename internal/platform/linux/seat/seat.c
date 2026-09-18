//go:build linux && cgo

#include "seat.h"
#include <errno.h>
#include <fcntl.h>
#include <libinput.h>
#include <libseat.h>
#include <libudev.h>
#include <stdlib.h>
#include <unistd.h>

struct worldr_seat {
  struct libseat *handle;
  int events[16], count, overflow;
};
struct input_device {
  int fd, id;
};
struct worldr_input {
  worldr_seat *seat;
  struct libinput *handle;
  struct udev *udev;
  struct input_device devices[64];
  int close_error;
};
static void edge(worldr_seat *s, int event) {
  if (s->count < 16)
    s->events[s->count++] = event;
  else
    s->overflow = 1;
}
static void enabled(struct libseat *seat, void *data) {
  (void)seat;
  edge(data, 1);
}
static void disabled(struct libseat *seat, void *data) {
  (void)seat;
  edge(data, 2);
}
static const struct libseat_seat_listener listener = {enabled, disabled};
worldr_seat *worldr_seat_open(int *error) {
  worldr_seat *s = calloc(1, sizeof(*s));
  if (!s) {
    *error = ENOMEM;
    return NULL;
  }
  s->handle = libseat_open_seat(&listener, s);
  if (!s->handle) {
    *error = errno;
    free(s);
    return NULL;
  }
  return s;
}
const char *worldr_seat_name(worldr_seat *s) {
  return libseat_seat_name(s->handle);
}
int worldr_seat_dispatch(worldr_seat *s) {
  if (!s->count && libseat_dispatch(s->handle, 0) < 0)
    return -errno;
  if (s->overflow)
    return -EOVERFLOW;
  if (!s->count)
    return 0;
  int event = s->events[0];
  for (int i = 1; i < s->count; i++)
    s->events[i - 1] = s->events[i];
  s->count--;
  return event;
}
int worldr_seat_device_open(worldr_seat *s, const char *path, int *fd) {
  int id = libseat_open_device(s->handle, path, fd);
  return id < 0 ? -errno : id;
}
int worldr_seat_device_close(worldr_seat *s, int id) {
  return libseat_close_device(s->handle, id) < 0 ? -errno : 0;
}
int worldr_seat_disable(worldr_seat *s) {
  return libseat_disable_seat(s->handle) < 0 ? -errno : 0;
}
int worldr_seat_switch(worldr_seat *s, int vt) {
  return libseat_switch_session(s->handle, vt) < 0 ? -errno : 0;
}
int worldr_seat_close(worldr_seat *s) {
  int result = libseat_close_seat(s->handle) < 0 ? -errno : 0;
  free(s);
  return result;
}
static int input_open(const char *path, int flags, void *data) {
  worldr_input *i = data;
  int slot = -1;
  for (int n = 0; n < 64; n++)
    if (i->devices[n].fd < 0) {
      slot = n;
      break;
    }
  if (slot < 0)
    return -EMFILE;
  int fd = -1, id = libseat_open_device(i->seat->handle, path, &fd);
  if (id < 0)
    return -errno;
  if (fcntl(fd, F_SETFL, fcntl(fd, F_GETFL) | (flags & O_NONBLOCK)) < 0 ||
      fcntl(fd, F_SETFD, FD_CLOEXEC) < 0) {
    int error = errno;
    libseat_close_device(i->seat->handle, id);
    close(fd);
    return -error;
  }
  i->devices[slot] = (struct input_device){fd, id};
  return fd;
}
static void input_close(int fd, void *data) {
  worldr_input *i = data;
  for (int n = 0; n < 64; n++)
    if (i->devices[n].fd == fd) {
      if (libseat_close_device(i->seat->handle, i->devices[n].id) < 0)
        i->close_error = errno;
      close(fd);
      i->devices[n].fd = -1;
      return;
    }
}
static const struct libinput_interface input_interface = {input_open,
                                                          input_close};
worldr_input *worldr_input_open(worldr_seat *s, int *error) {
  worldr_input *i = calloc(1, sizeof(*i));
  if (!i) {
    *error = ENOMEM;
    return NULL;
  }
  i->seat = s;
  for (int n = 0; n < 64; n++)
    i->devices[n].fd = -1;
  i->udev = udev_new();
  if (!i->udev) {
    *error = ENOMEM;
    free(i);
    return NULL;
  }
  i->handle = libinput_udev_create_context(&input_interface, i, i->udev);
  if (!i->handle) {
    *error = errno ? errno : ENOMEM;
    worldr_input_close(i);
    return NULL;
  }
  if (libinput_udev_assign_seat(i->handle, libseat_seat_name(s->handle)) < 0) {
    *error = ENODEV;
    worldr_input_close(i);
    return NULL;
  }
  return i;
}
int worldr_input_poll(worldr_input *i, worldr_input_event *out, int capacity) {
  if (libinput_dispatch(i->handle) < 0)
    return -EIO;
  int count = 0;
  for (int n = 0; n < 256 && count < capacity; n++) {
    struct libinput_event *event = libinput_get_event(i->handle);
    if (!event)
      break;
    enum libinput_event_type type = libinput_event_get_type(event);
    worldr_input_event value = {0};
    if (type == LIBINPUT_EVENT_DEVICE_REMOVED)
      value.kind = 6;
    else if (type == LIBINPUT_EVENT_KEYBOARD_KEY) {
      struct libinput_event_keyboard *k =
          libinput_event_get_keyboard_event(event);
      value.pressed = libinput_event_keyboard_get_key_state(k) ==
                      LIBINPUT_KEY_STATE_PRESSED;
      uint32_t held = libinput_event_keyboard_get_seat_key_count(k);
      if ((value.pressed && held == 1) || (!value.pressed && !held)) {
        value.kind = 4;
        value.code = libinput_event_keyboard_get_key(k);
        value.time = libinput_event_keyboard_get_time(k);
      }
    } else if (type >= LIBINPUT_EVENT_POINTER_MOTION &&
               type <= LIBINPUT_EVENT_POINTER_SCROLL_CONTINUOUS) {
      struct libinput_event_pointer *p =
          libinput_event_get_pointer_event(event);
      value.time = libinput_event_pointer_get_time(p);
      if (type == LIBINPUT_EVENT_POINTER_MOTION) {
        value.kind = 1;
        value.x = libinput_event_pointer_get_dx(p);
        value.y = libinput_event_pointer_get_dy(p);
      } else if (type == LIBINPUT_EVENT_POINTER_MOTION_ABSOLUTE) {
        value.kind = 2;
        value.x = libinput_event_pointer_get_absolute_x_transformed(p, 1);
        value.y = libinput_event_pointer_get_absolute_y_transformed(p, 1);
      } else if (type == LIBINPUT_EVENT_POINTER_BUTTON) {
        value.pressed = libinput_event_pointer_get_button_state(p) ==
                        LIBINPUT_BUTTON_STATE_PRESSED;
        uint32_t held = libinput_event_pointer_get_seat_button_count(p);
        if ((value.pressed && held == 1) || (!value.pressed && !held)) {
          value.kind = 3;
          value.code = libinput_event_pointer_get_button(p);
        }
      } else if (type == LIBINPUT_EVENT_POINTER_SCROLL_WHEEL ||
                 type == LIBINPUT_EVENT_POINTER_SCROLL_FINGER ||
                 type == LIBINPUT_EVENT_POINTER_SCROLL_CONTINUOUS) {
        value.kind = 5;
        int wheel = type == LIBINPUT_EVENT_POINTER_SCROLL_WHEEL;
        if (libinput_event_pointer_has_axis(
                p, LIBINPUT_POINTER_AXIS_SCROLL_HORIZONTAL))
          value.x = wheel ? libinput_event_pointer_get_scroll_value_v120(
                                p, LIBINPUT_POINTER_AXIS_SCROLL_HORIZONTAL) /
                                12.0
                          : libinput_event_pointer_get_scroll_value(
                                p, LIBINPUT_POINTER_AXIS_SCROLL_HORIZONTAL);
        if (libinput_event_pointer_has_axis(
                p, LIBINPUT_POINTER_AXIS_SCROLL_VERTICAL))
          value.y = wheel ? libinput_event_pointer_get_scroll_value_v120(
                                p, LIBINPUT_POINTER_AXIS_SCROLL_VERTICAL) /
                                12.0
                          : libinput_event_pointer_get_scroll_value(
                                p, LIBINPUT_POINTER_AXIS_SCROLL_VERTICAL);
      }
    }
    if (value.kind)
      out[count++] = value;
    libinput_event_destroy(event);
  }
  return count;
}
int worldr_input_close(worldr_input *i) {
  if (i->handle)
    libinput_unref(i->handle);
  for (int n = 0; n < 64; n++)
    if (i->devices[n].fd >= 0)
      input_close(i->devices[n].fd, i);
  if (i->udev)
    udev_unref(i->udev);
  int result = -i->close_error;
  free(i);
  return result;
}
