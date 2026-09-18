#pragma once

#include <stdint.h>

typedef struct worldr_drm worldr_drm;

int worldr_drm_create(const char *card, worldr_drm **out, char *err,
                      int errlen);
/* Planes-only sidecar: drmSetMaster + connector/CRTC, no primary SetCrtc.
 * Used by vk-display so overlay/cursor can share the master fd. */
int worldr_drm_create_planes(const char *card, worldr_drm **out, char *err,
                             int errlen);
/* Duplicates a libseat-owned descriptor; never changes DRM master ownership. */
int worldr_drm_create_fd(int fd, uint32_t connector_id, int planes_only,
                         worldr_drm **out, char *err, int errlen);
typedef struct {
  uint32_t id, width, height, refresh_millihz;
  int connected;
  char name[64];
} worldr_drm_output;
/* Read-only inventory; neither opens nor acquires the supplied card. */
int worldr_drm_outputs(int fd, worldr_drm_output *out, int capacity, char *err,
                       int errlen);
void worldr_drm_destroy(worldr_drm *d);
const char *worldr_drm_card(const worldr_drm *d);
int worldr_drm_fd(const worldr_drm *d);
uint32_t worldr_drm_connector_id(const worldr_drm *d);
int worldr_drm_planes_only(const worldr_drm *d);
uint32_t worldr_drm_width(const worldr_drm *d);
uint32_t worldr_drm_height(const worldr_drm *d);
uint32_t worldr_drm_stride(const worldr_drm *d);
int worldr_drm_present_bgra(worldr_drm *d, const uint8_t *bgra, uint32_t stride,
                            char *err, int errlen);
/* Primary-plane scanout of a client dmabuf. Falls back to SetCrtc when atomic
 * is unavailable. Caller restores the dumb FB with worldr_drm_scanout_restore.
 */
int worldr_drm_scanout_dmabuf(worldr_drm *d, int dmabuf_fd, uint32_t width,
                              uint32_t height, uint32_t fourcc,
                              uint64_t modifier, uint32_t offset,
                              uint32_t pitch, char *err, int errlen);
int worldr_drm_scanout_restore(worldr_drm *d, char *err, int errlen);
int worldr_drm_scanout_active(const worldr_drm *d);
/* Overlay / cursor plane caps (Intel first). 0/0 when the card has none. */
int worldr_drm_plane_caps(worldr_drm *d, int *overlay, int *cursor,
                          uint32_t *cursor_w, uint32_t *cursor_h);
int worldr_drm_overlay_dmabuf(worldr_drm *d, int dmabuf_fd, uint32_t width,
                              uint32_t height, uint32_t fourcc,
                              uint64_t modifier, uint32_t offset,
                              uint32_t pitch, int32_t x, int32_t y, char *err,
                              int errlen);
int worldr_drm_overlay_disable(worldr_drm *d, char *err, int errlen);
int worldr_drm_cursor_argb(worldr_drm *d, int32_t x, int32_t y, uint32_t width,
                           uint32_t height, const uint8_t *bgra,
                           uint32_t stride, char *err, int errlen);
int worldr_drm_cursor_disable(worldr_drm *d, char *err, int errlen);
