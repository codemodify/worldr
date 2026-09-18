// Included by apps.c: protocol objects own client FDs; surface images are
// immutable GPU snapshots owned by the Go importer until host retirement.
#include <linux/dma-buf.h>
#include <sys/ioctl.h>

extern int goWorldrAppsImport(uintptr_t handle, uint64_t token, int fd,
                              int width, int height, uint32_t format,
                              uint64_t modifier, uint32_t offset,
                              uint32_t stride);
struct app_dmabuf {
  struct worldr_apps *server;
  int fd, used, width, height;
  uint32_t offset, stride, format;
  uint64_t modifier;
};
static void dmabuf_buffer_destroy(struct wl_resource *r) {
  struct app_dmabuf *b = wl_resource_get_user_data(r);
  b->server->dmabuf_buffer_count--;
  if (b->fd >= 0)
    close(b->fd);
  free(b);
}
static const struct wl_buffer_interface dmabuf_buffer_impl = {
    .destroy = destroy_request};
static void dmabuf_params_destroy(struct wl_resource *r) {
  struct app_dmabuf *b = wl_resource_get_user_data(r);
  b->server->dmabuf_params_count--;
  if (b->fd >= 0)
    close(b->fd);
  free(b);
}
static void dmabuf_add(struct wl_client *c, struct wl_resource *r, int fd,
                       uint32_t plane, uint32_t offset, uint32_t stride,
                       uint32_t high, uint32_t low) {
  (void)c;
  struct app_dmabuf *b = wl_resource_get_user_data(r);
  uint32_t error = 0;
  if (b->used)
    error = ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_ALREADY_USED;
  else if (plane != 0)
    error = ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_PLANE_IDX;
  else if (b->fd >= 0)
    error = ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_PLANE_SET;
  if (b->used || plane != 0 || b->fd >= 0) {
    close(fd);
    wl_resource_post_error(r, error,
                           "only one unused DMA-BUF plane is supported");
    return;
  }
  fcntl(fd, F_SETFD, FD_CLOEXEC);
  b->fd = fd;
  b->offset = offset;
  b->stride = stride;
  b->modifier = ((uint64_t)high << 32) | low;
}
static void dmabuf_create(struct wl_client *c, struct wl_resource *r,
                          uint32_t id, int32_t width, int32_t height,
                          uint32_t format, uint32_t flags) {
  struct app_dmabuf *b = wl_resource_get_user_data(r);
  if (b->used) {
    wl_resource_post_error(r, ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_ALREADY_USED,
                           "params already used");
    return;
  }
  b->used = 1;
  if (b->fd < 0) {
    wl_resource_post_error(r, ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_INCOMPLETE,
                           "plane zero missing");
    return;
  }
  int supported = 0;
  for (int i = 0; i < b->server->dmabuf_format_count; i++)
    if (format == b->server->dmabuf_formats[i].fourcc &&
        b->modifier == b->server->dmabuf_formats[i].modifier)
      supported = 1;
  if (width < 1 || height < 1 || width > 4096 || height > 4096) {
    wl_resource_post_error(r,
                           ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_INVALID_DIMENSIONS,
                           "DMA-BUF extent outside [1,4096]");
    return;
  }
  if (b->stride < (uint32_t)width * 4 || b->stride > (1u << 20) ||
      b->stride % 4 || b->offset % 4 ||
      (uint64_t)b->offset + (uint64_t)b->stride * (height - 1) +
              (uint64_t)width * 4 >
          (256u << 20)) {
    wl_resource_post_error(r, ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_OUT_OF_BOUNDS,
                           "DMA-BUF plane bounds invalid");
    return;
  }
  // Exporting the implicit fence checks the descriptor without waiting on a
  // producer. The importer performs the bounded fence wait before its copy.
  struct dma_buf_export_sync_file sync = {.flags = DMA_BUF_SYNC_READ, .fd = -1};
  int usable = supported && !flags &&
               ioctl(b->fd, DMA_BUF_IOCTL_EXPORT_SYNC_FILE, &sync) == 0;
  if (sync.fd >= 0)
    close(sync.fd);
  if (!usable || b->server->dmabuf_buffer_count >= 256) {
    close(b->fd);
    b->fd = -1;
    if (id)
      wl_resource_post_error(r,
                             ZWP_LINUX_BUFFER_PARAMS_V1_ERROR_INVALID_WL_BUFFER,
                             "unsupported DMA-BUF storage, flags or capacity");
    else
      zwp_linux_buffer_params_v1_send_failed(r);
    return;
  }
  struct app_dmabuf *image = malloc(sizeof(*image));
  struct wl_resource *buffer =
      image ? wl_resource_create(c, &wl_buffer_interface, 1, id) : NULL;
  if (!image || !buffer) {
    free(image);
    post_oom(r);
    return;
  }
  *image = *b;
  image->width = width;
  image->height = height;
  image->format = format;
  b->fd = -1;
  b->server->dmabuf_buffer_count++;
  wl_resource_set_implementation(buffer, &dmabuf_buffer_impl, image,
                                 dmabuf_buffer_destroy);
  if (!id)
    zwp_linux_buffer_params_v1_send_created(r, buffer);
}
static void dmabuf_create_async(struct wl_client *c, struct wl_resource *r,
                                int32_t w, int32_t h, uint32_t f,
                                uint32_t flags) {
  dmabuf_create(c, r, 0, w, h, f, flags);
}
static const struct zwp_linux_buffer_params_v1_interface dmabuf_params_impl = {
    .destroy = destroy_request,
    .add = dmabuf_add,
    .create = dmabuf_create_async,
    .create_immed = dmabuf_create};
static void dmabuf_params(struct wl_client *c, struct wl_resource *r,
                          uint32_t id) {
  struct worldr_apps *s = wl_resource_get_user_data(r);
  if (s->dmabuf_params_count >= 64) {
    post_oom(r);
    return;
  }
  struct app_dmabuf *b = calloc(1, sizeof(*b));
  struct wl_resource *q =
      b ? wl_resource_create(c, &zwp_linux_buffer_params_v1_interface,
                             wl_resource_get_version(r), id)
        : NULL;
  if (!b || !q) {
    free(b);
    post_oom(r);
    return;
  }
  b->server = s;
  b->fd = -1;
  s->dmabuf_params_count++;
  wl_resource_set_implementation(q, &dmabuf_params_impl, b,
                                 dmabuf_params_destroy);
}

struct dmabuf_table_entry {
  uint32_t format;
  uint32_t padding;
  uint64_t modifier;
};
_Static_assert(sizeof(struct dmabuf_table_entry) == 16,
               "linux-dmabuf format table entry size");

static const struct zwp_linux_dmabuf_feedback_v1_interface
    dmabuf_feedback_impl = {.destroy = destroy_request};

static int dmabuf_write_all(int fd, const void *data, size_t length) {
  const unsigned char *at = data;
  while (length) {
    ssize_t n = write(fd, at, length);
    if (n < 0 && errno == EINTR)
      continue;
    if (n <= 0)
      return -1;
    at += (size_t)n;
    length -= (size_t)n;
  }
  return 0;
}

static int dmabuf_send_feedback(struct wl_resource *feedback,
                                struct worldr_apps *s) {
  struct dmabuf_table_entry table[MAX_DMABUF_FORMATS];
  uint16_t index[MAX_DMABUF_FORMATS];
  for (int i = 0; i < s->dmabuf_format_count; i++) {
    table[i] = (struct dmabuf_table_entry){
        .format = s->dmabuf_formats[i].fourcc,
        .modifier = s->dmabuf_formats[i].modifier};
    index[i] = (uint16_t)i;
  }
  size_t size = (size_t)s->dmabuf_format_count * sizeof(table[0]);
  int fd = memfd_create("worldr-dmabuf-formats",
                        MFD_CLOEXEC | MFD_ALLOW_SEALING);
  if (fd < 0 || dmabuf_write_all(fd, table, size) != 0 ||
      fcntl(fd, F_ADD_SEALS,
            F_SEAL_SHRINK | F_SEAL_GROW | F_SEAL_WRITE | F_SEAL_SEAL) < 0) {
    if (fd >= 0)
      close(fd);
    return -1;
  }
  struct wl_array device;
  struct wl_array indices;
  wl_array_init(&device);
  wl_array_init(&indices);
  dev_t *raw_device = wl_array_add(&device, sizeof(s->dmabuf_device));
  uint16_t *raw_indices =
      wl_array_add(&indices, (size_t)s->dmabuf_format_count * sizeof(uint16_t));
  if (!raw_device || !raw_indices) {
    close(fd);
    wl_array_release(&device);
    wl_array_release(&indices);
    return -1;
  }
  *raw_device = s->dmabuf_device;
  memcpy(raw_indices, index,
         (size_t)s->dmabuf_format_count * sizeof(uint16_t));
  zwp_linux_dmabuf_feedback_v1_send_format_table(feedback, fd,
                                                  (uint32_t)size);
  close(fd);
  zwp_linux_dmabuf_feedback_v1_send_main_device(feedback, &device);
  zwp_linux_dmabuf_feedback_v1_send_tranche_target_device(feedback, &device);
  zwp_linux_dmabuf_feedback_v1_send_tranche_flags(feedback, 0);
  zwp_linux_dmabuf_feedback_v1_send_tranche_formats(feedback, &indices);
  zwp_linux_dmabuf_feedback_v1_send_tranche_done(feedback);
  zwp_linux_dmabuf_feedback_v1_send_done(feedback);
  wl_array_release(&device);
  wl_array_release(&indices);
  return 0;
}

static void dmabuf_feedback(struct wl_client *c, struct wl_resource *r,
                            uint32_t id) {
  struct worldr_apps *s = wl_resource_get_user_data(r);
  struct wl_resource *feedback = wl_resource_create(
      c, &zwp_linux_dmabuf_feedback_v1_interface, 1, id);
  if (!feedback) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(feedback, &dmabuf_feedback_impl, NULL, NULL);
  if (dmabuf_send_feedback(feedback, s) != 0)
    wl_resource_post_no_memory(feedback);
}

static void dmabuf_surface_feedback(struct wl_client *c,
                                    struct wl_resource *r, uint32_t id,
                                    struct wl_resource *surface) {
  (void)surface;
  dmabuf_feedback(c, r, id);
}
static const struct zwp_linux_dmabuf_v1_interface dmabuf_impl = {
    .destroy = destroy_request,
    .create_params = dmabuf_params,
    .get_default_feedback = dmabuf_feedback,
    .get_surface_feedback = dmabuf_surface_feedback};
static void bind_dmabuf(struct wl_client *c, void *data, uint32_t version,
                        uint32_t id) {
  struct worldr_apps *s = data;
  uint32_t supported = s->dmabuf_has_device ? 4 : 3;
  uint32_t bound = version > supported ? supported : version;
  struct wl_resource *r = wl_resource_create(c, &zwp_linux_dmabuf_v1_interface,
                                             bound, id);
  if (!r) {
    wl_client_post_no_memory(c);
    return;
  }
  wl_resource_set_implementation(r, &dmabuf_impl, s, NULL);
  for (int i = 0; bound < 4 && i < s->dmabuf_format_count; i++) {
    int first = 1;
    for (int j = 0; j < i; j++)
      if (s->dmabuf_formats[j].fourcc == s->dmabuf_formats[i].fourcc)
        first = 0;
    if (first)
      zwp_linux_dmabuf_v1_send_format(r, s->dmabuf_formats[i].fourcc);
    if (version >= 3)
      zwp_linux_dmabuf_v1_send_modifier(
          r, s->dmabuf_formats[i].fourcc,
          (uint32_t)(s->dmabuf_formats[i].modifier >> 32),
          (uint32_t)s->dmabuf_formats[i].modifier);
  }
}
int worldr_apps_dmabuf(worldr_apps *s, uintptr_t handle,
                       const worldr_app_dmabuf_format *formats, int count,
                       uint32_t render_major, uint32_t render_minor,
                       int has_render_node) {
  if (s->dmabuf_global || !handle || count < 1 ||
      count > MAX_DMABUF_FORMATS)
    return -1;
  s->importer_handle = handle;
  s->dmabuf_format_count = count;
  memcpy(s->dmabuf_formats, formats, (size_t)count * sizeof(*formats));
  s->dmabuf_has_device = has_render_node;
  if (has_render_node)
    s->dmabuf_device = makedev(render_major, render_minor);
  s->dmabuf_global = wl_global_create(
      s->display, &zwp_linux_dmabuf_v1_interface,
      has_render_node ? 4 : 3, s, bind_dmabuf);
  return s->dmabuf_global ? 0 : -1;
}
static int copy_dmabuf(struct app_surface *v, struct wl_resource *resource) {
  if (!wl_resource_instance_of(resource, &wl_buffer_interface,
                               &dmabuf_buffer_impl)) {
    wl_resource_post_error(v->resource, WL_SURFACE_ERROR_INVALID_SIZE,
                           "unrecognized buffer storage");
    return -1;
  }
  struct app_dmabuf *b = wl_resource_get_user_data(resource);
  struct app_surface *root = v;
  while (root->parent)
    root = root->parent;
  if (root->role == ROLE_CURSOR || root->role == ROLE_DRAG_ICON ||
      b->width % v->pending_scale || b->height % v->pending_scale) {
    wl_resource_post_error(v->resource, WL_SURFACE_ERROR_INVALID_SIZE,
                           "GPU cursor or invalid buffer scale unsupported");
    return -1;
  }
  uint64_t token = ++v->server->next_image;
  if (!goWorldrAppsImport(v->server->importer_handle, token, b->fd, b->width,
                          b->height, b->format, b->modifier, b->offset,
                          b->stride)) {
    wl_resource_post_error(v->resource, WL_SURFACE_ERROR_INVALID_SIZE,
                           "DMA-BUF could not be imported on this GPU");
    return -1;
  }
  int opaque = b->format == 0x34325258u || b->format == 0x34324258u ||
               b->format == 0x30335258u || b->format == 0x30334258u;
  struct app_image *image =
      image_create(v->server, NULL, 0, b->width, b->height, 1,
                   opaque, token);
  if (!image) {
    post_oom(v->resource);
    return -1;
  }
  image_assign(v, image);
  return 0;
}
