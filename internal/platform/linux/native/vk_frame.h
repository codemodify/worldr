#pragma once
#include "vk_session.h"

typedef struct {
	float x,y,z,nx,ny,nz,r,g,b,a,bx,by,bz;
} worldr_mesh_vertex;

/* One ordered draw. kind0 = overlay triangle range, kind1 = start camera view
 * and clear its depth, kind2 = retained mesh instance, kind3 = opaque texture
 * on a centered unit XY quad, kind4 = depth-read-only mesh, kind5 = premultiplied
 * RGBA screen overlay with no depth reads/writes; kind6 = premultiplied translucent
 * spatial content, depth tested with no depth writes. geometry holds the
 * resource ID for every mesh or surface kind. */
typedef struct {
	uint32_t kind, first, count, flags;
	uint64_t geometry;
	float viewport[4];
	float projection[16], model[16], eye[4], light[4], color[4], wire[4], params[4];
	float material[4], rim[4];
	float glow[4];
	float optical[4];
	float point_position[4][4], point_color[4][4];
	float shadow_projection[16], shadow_params[4];
} worldr_frame_draw;

int worldr_vk_has_geometry(worldr_vk *vk, uint64_t id);
uint32_t worldr_vk_scene_samples(worldr_vk *vk);
int worldr_vk_upload_geometry(worldr_vk *vk, uint64_t id, const worldr_mesh_vertex *vertices, uint32_t vertex_count,
	const uint32_t *indices, uint32_t index_count, char *err, int errlen);
int worldr_vk_release_geometry(worldr_vk *vk, uint64_t id, char *err, int errlen);
int worldr_vk_upload_texture(worldr_vk *vk, uint64_t id, uint32_t width, uint32_t height,
	uint32_t x, uint32_t y, uint32_t damage_width, uint32_t damage_height,
	const uint8_t *rgba, char *err, int errlen);
int worldr_vk_release_texture(worldr_vk *vk, uint64_t id, char *err, int errlen);
int worldr_vk_render_frame(worldr_vk *vk, int linear_color, const float *output_transform,
	const worldr_scene_vertex *vertices, uint32_t vertex_count, const worldr_frame_draw *draws,
	uint32_t draw_count, const float *clear, uint8_t *out_bgra, char *err, int errlen);
/* Call prepare before destroying swapchain images and finish after replacing
 * width/height/format/images. Static mesh allocations and atlas are preserved. */
int worldr_vk_scene_resize_prepare(worldr_vk *vk, char *err, int errlen);
int worldr_vk_scene_resize_finish(worldr_vk *vk, char *err, int errlen);
