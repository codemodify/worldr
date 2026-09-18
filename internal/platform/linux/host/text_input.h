#pragma once
#include "host.h"
#include <wayland-client.h>
worldr_host_text_input *
worldr_host_text_input_new(void (*emit)(void *, worldr_host_event), void *data);
void worldr_host_text_input_close(worldr_host_text_input *t);
void worldr_host_text_input_global(worldr_host_text_input *t,
                                   struct wl_registry *registry, uint32_t name,
                                   uint32_t version);
void worldr_host_text_input_removed(worldr_host_text_input *t, uint32_t name);
void worldr_host_text_input_seat(worldr_host_text_input *t,
                                 struct wl_seat *seat);
void worldr_host_text_input_surface(worldr_host_text_input *t,
                                    struct wl_surface *surface);
void worldr_host_text_input_cancel(worldr_host_text_input *t);
int worldr_host_text_input_available(worldr_host_text_input *t);
void worldr_host_text_input_set(worldr_host_text_input *t, int enabled,
                                const char *context, const char *text,
                                int cursor, int anchor, int x, int y, int width,
                                int height, int scale, int input_method_cause);
