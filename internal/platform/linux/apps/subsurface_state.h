// Immutable committed content, shared by queued child dependencies and the
// currently presented surface. The server caps both image bytes and updates.
struct app_image {
  struct wl_list link;
  struct worldr_apps *server;
  unsigned refs;
  uint64_t token;
  unsigned char *pixels;
  size_t size;
  int width, height, gpu, opaque;
};
struct app_input_region {
  int infinite, count;
  struct {
    int32_t x, y, width, height;
    int add;
  } operations[64];
};
struct app_region {
  struct worldr_apps *server;
  struct app_input_region shape;
};
struct app_content {
  struct app_image *image;
  int scale, viewport_source, viewport_destination;
  int32_t viewport_src[4], viewport_dst[2];
  int geometry_x, geometry_y, geometry_width, geometry_height;
  struct app_input_region input;
};
struct app_update {
  struct worldr_apps *server;
  unsigned refs;
  uint64_t generation;
  struct app_content content;
  int child_count;
  struct {
    uint64_t id, association;
    int x, y, below;
    struct app_update *update;
  } children[MAX_SURFACES];
};
