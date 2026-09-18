static void image_retain(struct app_image *image) {
  if (image)
    image->refs++;
}
static void image_release(struct app_image *image) {
  if (!image || --image->refs)
    return;
  image->server->pixels_bytes -= image->size;
  wl_list_remove(&image->link);
  free(image->pixels);
  free(image);
}
static void image_assign(struct app_surface *v, struct app_image *image) {
  image_release(v->image);
  v->image = image;
  v->pixels = image ? image->pixels : NULL;
  v->pixels_size = image ? image->size : 0;
  v->width = image ? image->width : 0;
  v->height = image ? image->height : 0;
  v->gpu = image ? image->gpu : 0;
  v->opaque = image ? image->opaque : 0;
  v->image_token = image ? image->token : 0;
}
static struct app_image *image_create(struct worldr_apps *s,
                                      unsigned char *pixels, size_t size,
                                      int width, int height, int gpu,
                                      int opaque, uint64_t token) {
  struct app_image *image = calloc(1, sizeof(*image));
  if (!image)
    return NULL;
  image->server = s;
  image->refs = 1;
  image->pixels = pixels;
  image->size = size;
  image->width = width;
  image->height = height;
  image->gpu = gpu;
  image->opaque = opaque;
  image->token = token;
  wl_list_insert(s->images.prev, &image->link);
  s->pixels_bytes += size;
  return image;
}
static void update_release(struct app_update *update) {
  if (!update || --update->refs)
    return;
  for (int i = 0; i < update->child_count; i++)
    update_release(update->children[i].update);
  image_release(update->content.image);
  update->server->update_count--;
  free(update);
}
