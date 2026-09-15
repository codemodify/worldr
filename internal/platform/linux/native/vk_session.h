#pragma once

#include <stdint.h>

typedef struct worldr_vk worldr_vk;

enum {
	WORLDR_VK_HEADLESS = 0,
	WORLDR_VK_DISPLAY = 1
};

int worldr_vk_create(int mode, uint32_t prefer_w, uint32_t prefer_h, worldr_vk **out, char *err, int errlen);
/* VK_KHR_display on a DRM fd we already drmSetMaster (VK_EXT_acquire_drm_display). */
int worldr_vk_create_on_drm(int drm_fd, uint32_t connector_id, uint32_t prefer_w, uint32_t prefer_h,
			    worldr_vk **out, char *err, int errlen);
void worldr_vk_destroy(worldr_vk *vk);
const char *worldr_vk_device_name(const worldr_vk *vk);
uint32_t worldr_vk_width(const worldr_vk *vk);
uint32_t worldr_vk_height(const worldr_vk *vk);
uint32_t worldr_vk_vendor_id(const worldr_vk *vk);
int worldr_vk_list_devices(char *out, int outlen, char *err, int errlen);
int worldr_vk_clear_present(worldr_vk *vk, float r, float g, float b, float a, char *err, int errlen);
int worldr_vk_upload_present(worldr_vk *vk, const uint8_t *bgra, uint32_t stride, char *err, int errlen);
int worldr_vk_headless_clear(worldr_vk *vk, float r, float g, float b, float a, uint32_t *out_pixel, char *err, int errlen);
typedef struct {
	int slot;
	int32_t x, y, w, h;
} worldr_vk_layer;

int worldr_vk_has_dmabuf(const worldr_vk *vk);
int worldr_vk_is_display(const worldr_vk *vk);
int worldr_vk_has_timeline(const worldr_vk *vk);
uint32_t worldr_vk_display_planes(const worldr_vk *vk);
/* Import a DRM syncobj timeline fd and vkWaitSemaphores before blit/sample. */
int worldr_vk_wait_timeline_fd(worldr_vk *vk, int fd, uint64_t point, uint64_t timeout_ns, char *err, int errlen);
int worldr_vk_dmabuf_import(worldr_vk *vk, uint32_t width, uint32_t height, uint32_t fourcc, uint64_t modifier,
			    int nplanes, const int *fds, const uint32_t *offsets, const uint32_t *pitches,
			    uint8_t *out_bgra, uint32_t out_stride, char *err, int errlen);
/* Retain an imported VkImage for GPU composite. Slot is 1-based; 0 is invalid. */
int worldr_vk_dmabuf_retain(worldr_vk *vk, uint32_t width, uint32_t height, uint32_t fourcc, uint64_t modifier,
			    int nplanes, const int *fds, const uint32_t *offsets, const uint32_t *pitches,
			    int *out_slot, char *err, int errlen);
void worldr_vk_dmabuf_release(worldr_vk *vk, int slot);
/* Upload CPU desktop, then blit retained dmabuf layers (implicit sync via layout). */
int worldr_vk_upload_present_layers(worldr_vk *vk, const uint8_t *bgra, uint32_t stride,
				    const worldr_vk_layer *layers, int nlayers, char *err, int errlen);
