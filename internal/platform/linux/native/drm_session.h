#pragma once

#include <stdint.h>

typedef struct worldr_drm worldr_drm;

int worldr_drm_create(const char *card, worldr_drm **out, char *err, int errlen);
void worldr_drm_destroy(worldr_drm *d);
const char *worldr_drm_card(const worldr_drm *d);
uint32_t worldr_drm_width(const worldr_drm *d);
uint32_t worldr_drm_height(const worldr_drm *d);
uint32_t worldr_drm_stride(const worldr_drm *d);
int worldr_drm_present_bgra(worldr_drm *d, const uint8_t *bgra, uint32_t stride, char *err, int errlen);
/* Primary-plane scanout of a client dmabuf. Falls back to SetCrtc when atomic
 * is unavailable. Caller restores the dumb FB with worldr_drm_scanout_restore. */
int worldr_drm_scanout_dmabuf(worldr_drm *d, int dmabuf_fd, uint32_t width, uint32_t height,
			      uint32_t fourcc, uint64_t modifier, uint32_t offset, uint32_t pitch,
			      char *err, int errlen);
int worldr_drm_scanout_restore(worldr_drm *d, char *err, int errlen);
int worldr_drm_scanout_active(const worldr_drm *d);
