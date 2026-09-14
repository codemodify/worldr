#pragma once

#include <stdint.h>

typedef struct worldr_vk worldr_vk;

enum {
	WORLDR_VK_HEADLESS = 0,
	WORLDR_VK_DISPLAY = 1
};

int worldr_vk_create(int mode, uint32_t prefer_w, uint32_t prefer_h, worldr_vk **out, char *err, int errlen);
void worldr_vk_destroy(worldr_vk *vk);
const char *worldr_vk_device_name(const worldr_vk *vk);
uint32_t worldr_vk_width(const worldr_vk *vk);
uint32_t worldr_vk_height(const worldr_vk *vk);
uint32_t worldr_vk_vendor_id(const worldr_vk *vk);
int worldr_vk_list_devices(char *out, int outlen, char *err, int errlen);
int worldr_vk_clear_present(worldr_vk *vk, float r, float g, float b, float a, char *err, int errlen);
int worldr_vk_upload_present(worldr_vk *vk, const uint8_t *bgra, uint32_t stride, char *err, int errlen);
int worldr_vk_headless_clear(worldr_vk *vk, float r, float g, float b, float a, uint32_t *out_pixel, char *err, int errlen);
int worldr_vk_has_dmabuf(const worldr_vk *vk);
int worldr_vk_dmabuf_import(worldr_vk *vk, uint32_t width, uint32_t height, uint32_t fourcc, uint64_t modifier,
			    int nplanes, const int *fds, const uint32_t *offsets, const uint32_t *pitches,
			    uint8_t *out_bgra, uint32_t out_stride, char *err, int errlen);
