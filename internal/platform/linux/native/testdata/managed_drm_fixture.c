// A fake libdrm boundary exercises real descriptor duplication/cleanup without
// opening hardware, acquiring DRM master, or modesetting any display.
#define drmSetMaster fixture_set_master
#define drmDropMaster fixture_drop_master
#define drmModeGetResources fixture_resources
#define drmModeGetConnector fixture_connector
#include "../drm_session.c"

static int master_calls;
static int connected = 1;
int fixture_set_master(int fd) {
  (void)fd;
  master_calls++;
  return 0;
}
int fixture_drop_master(int fd) {
  (void)fd;
  master_calls++;
  return 0;
}
drmModeRes *fixture_resources(int fd) {
  (void)fd;
  drmModeRes *r = calloc(1, sizeof(*r));
  r->count_connectors = 2;
  r->connectors = calloc(2, sizeof(*r->connectors));
  r->connectors[0] = 7;
  r->connectors[1] = 9;
  r->count_crtcs = 1;
  r->crtcs = calloc(1, sizeof(*r->crtcs));
  r->crtcs[0] = 20;
  return r;
}
drmModeConnector *fixture_connector(int fd, uint32_t id) {
  (void)fd;
  drmModeConnector *c = calloc(1, sizeof(*c));
  c->connector_id = id;
  c->connector_type = DRM_MODE_CONNECTOR_DisplayPort;
  c->connector_type_id = id == 7 ? 1 : 2;
  c->connection = connected ? DRM_MODE_CONNECTED : DRM_MODE_DISCONNECTED;
  c->count_modes = 1;
  c->modes = calloc(1, sizeof(*c->modes));
  c->modes[0] = (drmModeModeInfo){.clock = 148500,
                                  .hdisplay = 1920,
                                  .htotal = 2200,
                                  .vdisplay = 1080,
                                  .vtotal = 1125,
                                  .type = DRM_MODE_TYPE_PREFERRED};
  return c;
}
int main(void) {
  int fd = open("/dev/null", O_RDONLY | O_CLOEXEC);
  if (fd < 0)
    return 1;
  char error[512];
  worldr_drm *first = NULL, *second = NULL;
  if (worldr_drm_create_fd(fd, 7, 1, &first, error, 512) != 0)
    return 2;
  if (worldr_drm_create_fd(fd, 9, 1, &second, error, 512) != 0)
    return 3;
  if (first->fd == fd || second->fd == fd || first->fd == second->fd)
    return 4;
  if (first->connector_id != 7 || second->connector_id != 9)
    return 5;
  int duplicate = first->fd;
  worldr_drm_destroy(first);
  if (fcntl(duplicate, F_GETFD) != -1 || fcntl(fd, F_GETFD) < 0 ||
      fcntl(second->fd, F_GETFD) < 0)
    return 6;
  worldr_drm_destroy(second);
  if (fcntl(fd, F_GETFD) < 0 || master_calls)
    return 7;
  connected = 0;
  if (worldr_drm_create_fd(fd, 7, 1, &first, error, 512) == 0 || master_calls ||
      fcntl(fd, F_GETFD) < 0)
    return 8;
  if (worldr_drm_create_fd(fd, 7, 0, &first, error, 512) == 0 || master_calls ||
      fcntl(fd, F_GETFD) < 0)
    return 9;
  connected = 1;
  if (worldr_drm_create_fd(fd, 999, 1, &first, error, 512) == 0 || master_calls)
    return 10;
  worldr_drm_output outputs[2];
  int n = worldr_drm_outputs(fd, outputs, 2, error, 512);
  if (n != 2 || outputs[0].id != 7 || outputs[1].id != 9 ||
      outputs[0].width != 1920 || outputs[0].height != 1080 ||
      outputs[0].refresh_millihz != 60000 || strcmp(outputs[1].name, "DP-2"))
    return 11;
  if (master_calls || fcntl(fd, F_GETFD) < 0)
    return 12;
  close(fd);
  return 0;
}
