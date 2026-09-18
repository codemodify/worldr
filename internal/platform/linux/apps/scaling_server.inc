/* Included after app structs and basic resource helpers. Viewporter state is
 * double-buffered with wl_surface.commit; preferences never resize content. */
#include "viewporter-server-protocol.h"
#include "fractional-scale-server-protocol.h"

static int scale_logical_width(struct app_surface *v) {
  if (!has_buffer(v)) return 0;
  if (v->viewport_destination) return v->viewport_dst[0];
  if (v->viewport_source) return v->viewport_src[2] / 256;
  return v->width / v->scale;
}
static int scale_logical_height(struct app_surface *v) {
  if (!has_buffer(v)) return 0;
  if (v->viewport_destination) return v->viewport_dst[1];
  if (v->viewport_source) return v->viewport_src[3] / 256;
  return v->height / v->scale;
}
static void scale_source_crop(struct app_surface *v, float *u0, float *v0,
                              float *u1, float *v1) {
  *u0 = *v0 = 0; *u1 = *v1 = 1;
  if (!v->viewport_source || v->width <= 0 || v->height <= 0) return;
  *u0 = (float)((double)v->viewport_src[0] * v->scale / (256.0 * v->width));
  *v0 = (float)((double)v->viewport_src[1] * v->scale / (256.0 * v->height));
  *u1 = (float)(((double)v->viewport_src[0] + v->viewport_src[2]) * v->scale / (256.0 * v->width));
  *v1 = (float)(((double)v->viewport_src[1] + v->viewport_src[3]) * v->scale / (256.0 * v->height));
}
static int scale_commit(struct app_surface *v) {
  if (v->pending_viewport_source && !v->pending_viewport_destination &&
      (v->pending_viewport_src[2] % 256 || v->pending_viewport_src[3] % 256)) {
    wl_resource_post_error(v->viewport, WP_VIEWPORT_ERROR_BAD_SIZE,
                           "fractional source dimensions need a destination");
    return -1;
  }
  if (has_buffer(v)) {
    if (v->width % v->pending_scale || v->height % v->pending_scale) {
      wl_resource_post_error(v->resource, WL_SURFACE_ERROR_INVALID_SIZE,
                             "buffer dimensions must be a multiple of scale");
      return -1;
    }
    if (v->pending_viewport_source &&
        (((int64_t)v->pending_viewport_src[0] + v->pending_viewport_src[2]) * v->pending_scale > (int64_t)v->width * 256 ||
         ((int64_t)v->pending_viewport_src[1] + v->pending_viewport_src[3]) * v->pending_scale > (int64_t)v->height * 256)) {
      wl_resource_post_error(v->viewport, WP_VIEWPORT_ERROR_OUT_OF_BUFFER,
                             "viewport source extends outside buffer");
      return -1;
    }
    /* Limit logical composition extent independently of source pixel storage. */
    if (v->pending_viewport_destination &&
        (v->pending_viewport_dst[0] > 4096 || v->pending_viewport_dst[1] > 4096)) {
      post_oom(v->resource);
      return -1;
    }
  }
  v->viewport_source = v->pending_viewport_source;
  v->viewport_destination = v->pending_viewport_destination;
  memcpy(v->viewport_src, v->pending_viewport_src, sizeof(v->viewport_src));
  memcpy(v->viewport_dst, v->pending_viewport_dst, sizeof(v->viewport_dst));
  return 0;
}
static void scale_destroy_surface(struct app_surface *v) {
  if (v->viewport) wl_resource_set_user_data(v->viewport, NULL);
  if (v->fractional) wl_resource_set_user_data(v->fractional, NULL);
  v->viewport = v->fractional = NULL;
}
static void viewport_destroyed(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (!v) return;
  v->viewport = NULL;
  v->pending_viewport_source = v->pending_viewport_destination = 0;
  memset(v->pending_viewport_src, 0, sizeof(v->pending_viewport_src));
  memset(v->pending_viewport_dst, 0, sizeof(v->pending_viewport_dst));
}
static struct app_surface *viewport_surface(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (!v) wl_resource_post_error(r, WP_VIEWPORT_ERROR_NO_SURFACE,
                                  "viewport surface has been destroyed");
  return v;
}
static void viewport_source(struct wl_client *client, struct wl_resource *r,
                            wl_fixed_t x, wl_fixed_t y, wl_fixed_t width,
                            wl_fixed_t height) {
  (void)client;
  struct app_surface *v = viewport_surface(r);
  if (!v) return;
  if (x == -256 && y == -256 && width == -256 && height == -256) {
    v->pending_viewport_source = 0;
    return;
  }
  if (x < 0 || y < 0 || width <= 0 || height <= 0) {
    wl_resource_post_error(r, WP_VIEWPORT_ERROR_BAD_VALUE,
                           "viewport source needs nonnegative origin and positive size");
    return;
  }
  v->pending_viewport_source = 1;
  v->pending_viewport_src[0] = x; v->pending_viewport_src[1] = y;
  v->pending_viewport_src[2] = width; v->pending_viewport_src[3] = height;
}
static void viewport_destination(struct wl_client *client,
                                 struct wl_resource *r, int32_t width,
                                 int32_t height) {
  (void)client;
  struct app_surface *v = viewport_surface(r);
  if (!v) return;
  if (width == -1 && height == -1) {
    v->pending_viewport_destination = 0;
    return;
  }
  if (width <= 0 || height <= 0) {
    wl_resource_post_error(r, WP_VIEWPORT_ERROR_BAD_VALUE,
                           "viewport destination needs positive dimensions");
    return;
  }
  v->pending_viewport_destination = 1;
  v->pending_viewport_dst[0] = width; v->pending_viewport_dst[1] = height;
}
static const struct wp_viewport_interface viewport_impl = {
    .destroy = destroy_request,
    .set_source = viewport_source,
    .set_destination = viewport_destination};
static void viewporter_get(struct wl_client *client, struct wl_resource *r,
                           uint32_t id, struct wl_resource *surface) {
  struct app_surface *v = wl_resource_get_user_data(surface);
  if (v->viewport) {
    wl_resource_post_error(r, WP_VIEWPORTER_ERROR_VIEWPORT_EXISTS,
                           "surface already has a viewport");
    return;
  }
  struct wl_resource *resource = wl_resource_create(client, &wp_viewport_interface, 1, id);
  if (!resource) { post_oom(r); return; }
  v->viewport = resource;
  wl_resource_set_implementation(resource, &viewport_impl, v, viewport_destroyed);
}
static const struct wp_viewporter_interface viewporter_impl = {
    .destroy = destroy_request, .get_viewport = viewporter_get};
static void bind_viewporter(struct wl_client *client, void *data,
                            uint32_t version, uint32_t id) {
  (void)version;
  struct wl_resource *r = wl_resource_create(client, &wp_viewporter_interface, 1, id);
  if (!r) { wl_client_post_no_memory(client); return; }
  wl_resource_set_implementation(r, &viewporter_impl, data, NULL);
}
static void fractional_destroyed(struct wl_resource *r) {
  struct app_surface *v = wl_resource_get_user_data(r);
  if (v) v->fractional = NULL;
}
static const struct wp_fractional_scale_v1_interface fractional_impl = {
    .destroy = destroy_request};
static void fractional_get(struct wl_client *client, struct wl_resource *r,
                            uint32_t id, struct wl_resource *surface) {
  struct app_surface *v = wl_resource_get_user_data(surface);
  if (v->fractional) {
    wl_resource_post_error(r, WP_FRACTIONAL_SCALE_MANAGER_V1_ERROR_FRACTIONAL_SCALE_EXISTS,
                           "surface already has fractional scale information");
    return;
  }
  struct wl_resource *resource = wl_resource_create(client, &wp_fractional_scale_v1_interface, 1, id);
  if (!resource) { post_oom(r); return; }
  v->fractional = resource;
  wl_resource_set_implementation(resource, &fractional_impl, v, fractional_destroyed);
  wp_fractional_scale_v1_send_preferred_scale(resource, v->server->preferred_scale120 ? v->server->preferred_scale120 : 120);
}
static const struct wp_fractional_scale_manager_v1_interface fractional_manager_impl = {
    .destroy = destroy_request, .get_fractional_scale = fractional_get};
static void bind_fractional_manager(struct wl_client *client, void *data,
                                    uint32_t version, uint32_t id) {
  (void)version;
  struct wl_resource *r = wl_resource_create(client, &wp_fractional_scale_manager_v1_interface, 1, id);
  if (!r) { wl_client_post_no_memory(client); return; }
  wl_resource_set_implementation(r, &fractional_manager_impl, data, NULL);
}
static int scale_globals(struct worldr_apps *s) {
  if (!s->preferred_scale120) s->preferred_scale120 = 120;
  return wl_global_create(s->display, &wp_viewporter_interface, 1, s, bind_viewporter) &&
         wl_global_create(s->display, &wp_fractional_scale_manager_v1_interface, 1, s, bind_fractional_manager) ? 0 : -1;
}
int worldr_apps_scale120(struct worldr_apps *s, uint32_t scale) {
  if (!s || scale < 1 || scale > 960) return -1;
  if (s->preferred_scale120 == scale) return 0;
  s->preferred_scale120 = scale;
  struct app_surface *v;
  wl_list_for_each(v, &s->surfaces, link)
    if (v->fractional) wp_fractional_scale_v1_send_preferred_scale(v->fractional, scale);
  wl_display_flush_clients(s->display);
  return 0;
}
