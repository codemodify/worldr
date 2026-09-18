#pragma once

#include <stdint.h>

typedef struct worldr_vk worldr_vk;

enum {
	WORLDR_VK_HEADLESS = 0,
	WORLDR_VK_DISPLAY = 1,
	WORLDR_VK_WAYLAND = 2
};

int worldr_vk_create(int mode, uint32_t prefer_w, uint32_t prefer_h, worldr_vk **out, char *err, int errlen);
/* VK_KHR_display on a DRM fd we already drmSetMaster (VK_EXT_acquire_drm_display). */
int worldr_vk_create_on_drm(int drm_fd, uint32_t connector_id, uint32_t prefer_w, uint32_t prefer_h,
			    worldr_vk **out, char *err, int errlen);
int worldr_vk_create_wayland(void *display, void *surface, uint32_t w, uint32_t h,
 worldr_vk **out, char *err, int errlen);
int worldr_vk_resize(worldr_vk *vk, uint32_t w, uint32_t h, char *err, int errlen);
typedef struct { uint64_t allocated,peak,budget; uint32_t images,buffers; } worldr_vk_memory_stats;
int worldr_vk_memory_budget(worldr_vk *vk,uint64_t bytes,char *err,int errlen);
void worldr_vk_memory_usage(worldr_vk *vk,worldr_vk_memory_stats *stats);
int worldr_vk_recover(worldr_vk *vk,char *err,int errlen);
typedef struct { uint32_t width,height,format,offset,stride; uint64_t modifier,allocation; int fd; } worldr_dmabuf;
int worldr_vk_dmabuf_supported(worldr_vk *vk);
int worldr_vk_dmabuf_format_supported(worldr_vk *vk, uint32_t format);
int worldr_vk_dmabuf_modifier_supported(worldr_vk *vk, uint32_t format,
                                        uint64_t modifier);
int worldr_vk_dmabuf_modifiers(worldr_vk *vk, uint32_t format, uint64_t *out,
                               int capacity);
int worldr_vk_copy_dmabuf(worldr_vk *vk,const worldr_dmabuf *source,worldr_dmabuf *snapshot,char *err,int errlen);
int worldr_vk_upload_dmabuf(worldr_vk *vk,uint64_t id,const worldr_dmabuf *source,char *err,int errlen);
void worldr_vk_destroy(worldr_vk *vk);
const char *worldr_vk_device_name(const worldr_vk *vk);
int worldr_vk_render_node(const worldr_vk *vk, uint32_t *major,
                          uint32_t *minor);
uint32_t worldr_vk_width(const worldr_vk *vk);
uint32_t worldr_vk_height(const worldr_vk *vk);
int worldr_vk_list_devices(char *out, int outlen, char *err, int errlen);
int worldr_vk_is_display(const worldr_vk *vk);

/* Programmable scene renderer. Vertex coordinates are top-left pixels, depth
 * is 0 (near) to 1 (far), and RGBA is straight alpha. C copies all input data. */
typedef struct {
	float x, y, z, u, v, r, g, b, a;
} worldr_scene_vertex;
int worldr_vk_scene_atlas(worldr_vk *vk, uint32_t width, uint32_t height,
	const uint8_t *coverage, char *err, int errlen);
