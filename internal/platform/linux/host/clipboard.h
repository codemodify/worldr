#pragma once
#include <stdint.h>
#include <wayland-client.h>

typedef struct worldr_host_clipboard worldr_host_clipboard;
typedef struct { uint64_t revision, id, external_id; int mime_count, available; } worldr_clipboard_offer;
typedef struct { uint64_t external_id; char *mime; int fd; } worldr_clipboard_request;
worldr_host_clipboard *worldr_host_clipboard_new(void);
void worldr_host_clipboard_close(worldr_host_clipboard *c);
void worldr_host_clipboard_global(worldr_host_clipboard *c, struct wl_registry *registry, uint32_t name, uint32_t version);
void worldr_host_clipboard_removed(worldr_host_clipboard *c, uint32_t name);
void worldr_host_clipboard_seat(worldr_host_clipboard *c, struct wl_seat *seat);
void worldr_host_clipboard_focus(worldr_host_clipboard *c, int focused, uint32_t serial);
void worldr_host_clipboard_serial(worldr_host_clipboard *c, uint32_t serial);
worldr_clipboard_offer worldr_host_clipboard_offer(worldr_host_clipboard *c);
const char *worldr_host_clipboard_mime(worldr_host_clipboard *c, int index);
int worldr_host_clipboard_receive(worldr_host_clipboard *c, uint64_t id, const char *mime, int fd);
int worldr_host_clipboard_publish(worldr_host_clipboard *c, uint64_t external_id, const char *const *mimes, int count);
int worldr_host_clipboard_requests(worldr_host_clipboard *c, worldr_clipboard_request *out, int capacity);
