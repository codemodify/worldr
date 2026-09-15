#include "drm_session.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>
#include <xf86drm.h>
#include <xf86drmMode.h>
#include <drm.h>
#include <drm_fourcc.h>
#include <drm_mode.h>

struct worldr_drm {
	int fd;
	char card[64];
	uint32_t connector_id;
	uint32_t crtc_id;
	drmModeModeInfo mode;
	uint32_t fb_id;
	uint32_t handle;
	uint32_t pitch;
	uint32_t size;
	void *map;
	drmModeCrtc *saved;
	uint32_t scan_fb_id;
	uint32_t scan_handle;
	int scan_active;
};

static void seterr(char *err, int errlen, const char *fmt, int e)
{
	if (!err || errlen <= 0) {
		return;
	}
	if (e) {
		snprintf(err, (size_t)errlen, "%s: %s", fmt, strerror(e));
	} else {
		snprintf(err, (size_t)errlen, "%s", fmt);
	}
}

static int open_card(const char *card, char *picked, size_t picked_len, char *err, int errlen)
{
	if (card && card[0]) {
		int fd = open(card, O_RDWR | O_CLOEXEC);
		if (fd < 0) {
			seterr(err, errlen, "open DRM card", errno);
			return -1;
		}
		snprintf(picked, picked_len, "%s", card);
		return fd;
	}
	for (int i = 0; i < 8; i++) {
		char path[64];
		snprintf(path, sizeof(path), "/dev/dri/card%d", i);
		int fd = open(path, O_RDWR | O_CLOEXEC);
		if (fd >= 0) {
			snprintf(picked, picked_len, "%s", path);
			return fd;
		}
	}
	seterr(err, errlen, "no /dev/dri/cardN (no GPU, or missing video/render group)", ENOENT);
	return -1;
}

int worldr_drm_create(const char *card, worldr_drm **out, char *err, int errlen)
{
	worldr_drm *d = (worldr_drm *)calloc(1, sizeof(*d));
	if (!d) {
		seterr(err, errlen, "oom", 0);
		return -1;
	}
	d->fd = -1; /* calloc leaves 0 (stdin) — destroy must not close that */
	if (card && card[0]) {
		d->fd = open_card(card, d->card, sizeof(d->card), err, errlen);
		if (d->fd < 0) {
			free(d);
			return -1;
		}
		if (drmSetMaster(d->fd) != 0) {
			seterr(err, errlen, "drmSetMaster failed — another compositor owns this card. Spare VT: Ctrl+Alt+F3 + scripts/try-tty.sh", errno);
			close(d->fd);
			free(d);
			return -1;
		}
	} else {
		int got = -1;
		for (int i = 0; i < 8; i++) {
			char path[64];
			snprintf(path, sizeof(path), "/dev/dri/card%d", i);
			int fd = open(path, O_RDWR | O_CLOEXEC);
			if (fd < 0) {
				continue;
			}
			if (drmSetMaster(fd) == 0) {
				snprintf(d->card, sizeof(d->card), "%s", path);
				d->fd = fd;
				got = fd;
				break;
			}
			close(fd);
		}
		if (got < 0) {
			seterr(err, errlen, "no DRM card we could drmSetMaster — another compositor owns the GPU, or no /dev/dri/cardN. Spare VT: Ctrl+Alt+F3 + scripts/try-tty.sh", EBUSY);
			free(d);
			return -1;
		}
	}

	drmModeRes *res = drmModeGetResources(d->fd);
	if (!res) {
		seterr(err, errlen, "drmModeGetResources failed", errno);
		drmDropMaster(d->fd);
		close(d->fd);
		free(d);
		return -1;
	}

	drmModeConnector *conn = NULL;
	for (int i = 0; i < res->count_connectors; i++) {
		drmModeConnector *c = drmModeGetConnector(d->fd, res->connectors[i]);
		if (!c) {
			continue;
		}
		if (c->connection == DRM_MODE_CONNECTED && c->count_modes > 0) {
			conn = c;
			break;
		}
		drmModeFreeConnector(c);
	}
	if (!conn) {
		drmModeFreeResources(res);
		seterr(err, errlen, "no connected DRM connector", 0);
		drmDropMaster(d->fd);
		close(d->fd);
		free(d);
		return -1;
	}

	d->connector_id = conn->connector_id;
	d->mode = conn->modes[0];
	for (int i = 0; i < conn->count_modes; i++) {
		if (conn->modes[i].type & DRM_MODE_TYPE_PREFERRED) {
			d->mode = conn->modes[i];
			break;
		}
	}

	if (conn->encoder_id) {
		drmModeEncoder *enc = drmModeGetEncoder(d->fd, conn->encoder_id);
		if (enc) {
			d->crtc_id = enc->crtc_id;
			drmModeFreeEncoder(enc);
		}
	}
	if (!d->crtc_id && conn->count_encoders) {
		drmModeEncoder *enc = drmModeGetEncoder(d->fd, conn->encoders[0]);
		if (enc) {
			d->crtc_id = enc->crtc_id;
			if (!d->crtc_id && res->count_crtcs) {
				d->crtc_id = res->crtcs[0];
			}
			drmModeFreeEncoder(enc);
		}
	}
	if (!d->crtc_id && res->count_crtcs) {
		d->crtc_id = res->crtcs[0];
	}

	uint32_t w = d->mode.hdisplay;
	uint32_t h = d->mode.vdisplay;
	struct drm_mode_create_dumb create;
	memset(&create, 0, sizeof(create));
	create.width = w;
	create.height = h;
	create.bpp = 32;
	if (drmIoctl(d->fd, DRM_IOCTL_MODE_CREATE_DUMB, &create) != 0) {
		seterr(err, errlen, "DRM_IOCTL_MODE_CREATE_DUMB", errno);
		drmModeFreeConnector(conn);
		drmModeFreeResources(res);
		drmDropMaster(d->fd);
		close(d->fd);
		free(d);
		return -1;
	}
	d->handle = create.handle;
	d->pitch = create.pitch;
	d->size = create.size;

	if (drmModeAddFB(d->fd, w, h, 24, 32, d->pitch, d->handle, &d->fb_id) != 0) {
		seterr(err, errlen, "drmModeAddFB", errno);
		struct drm_mode_destroy_dumb destroy = {.handle = d->handle};
		drmIoctl(d->fd, DRM_IOCTL_MODE_DESTROY_DUMB, &destroy);
		drmModeFreeConnector(conn);
		drmModeFreeResources(res);
		drmDropMaster(d->fd);
		close(d->fd);
		free(d);
		return -1;
	}

	struct drm_mode_map_dumb map;
	memset(&map, 0, sizeof(map));
	map.handle = d->handle;
	if (drmIoctl(d->fd, DRM_IOCTL_MODE_MAP_DUMB, &map) != 0) {
		seterr(err, errlen, "DRM_IOCTL_MODE_MAP_DUMB", errno);
		drmModeRmFB(d->fd, d->fb_id);
		struct drm_mode_destroy_dumb destroy = {.handle = d->handle};
		drmIoctl(d->fd, DRM_IOCTL_MODE_DESTROY_DUMB, &destroy);
		drmModeFreeConnector(conn);
		drmModeFreeResources(res);
		drmDropMaster(d->fd);
		close(d->fd);
		free(d);
		return -1;
	}
	d->map = mmap(NULL, d->size, PROT_READ | PROT_WRITE, MAP_SHARED, d->fd, (off_t)map.offset);
	if (d->map == MAP_FAILED) {
		d->map = NULL;
		seterr(err, errlen, "mmap dumb buffer", errno);
		drmModeRmFB(d->fd, d->fb_id);
		struct drm_mode_destroy_dumb destroy = {.handle = d->handle};
		drmIoctl(d->fd, DRM_IOCTL_MODE_DESTROY_DUMB, &destroy);
		drmModeFreeConnector(conn);
		drmModeFreeResources(res);
		drmDropMaster(d->fd);
		close(d->fd);
		free(d);
		return -1;
	}

	d->saved = drmModeGetCrtc(d->fd, d->crtc_id);
	if (drmModeSetCrtc(d->fd, d->crtc_id, d->fb_id, 0, 0, &d->connector_id, 1, &d->mode) != 0) {
		seterr(err, errlen, "drmModeSetCrtc", errno);
		if (d->saved) {
			drmModeFreeCrtc(d->saved);
		}
		munmap(d->map, d->size);
		drmModeRmFB(d->fd, d->fb_id);
		struct drm_mode_destroy_dumb destroy = {.handle = d->handle};
		drmIoctl(d->fd, DRM_IOCTL_MODE_DESTROY_DUMB, &destroy);
		drmModeFreeConnector(conn);
		drmModeFreeResources(res);
		drmDropMaster(d->fd);
		close(d->fd);
		free(d);
		return -1;
	}

	drmModeFreeConnector(conn);
	drmModeFreeResources(res);
	*out = d;
	return 0;
}

void worldr_drm_destroy(worldr_drm *d)
{
	if (!d) {
		return;
	}
	if (d->saved && d->fd >= 0) {
		drmModeSetCrtc(d->fd, d->saved->crtc_id, d->saved->buffer_id,
			       d->saved->x, d->saved->y, &d->connector_id, 1, &d->saved->mode);
		drmModeFreeCrtc(d->saved);
	}
	if (d->map && d->size) {
		munmap(d->map, d->size);
	}
	if (d->scan_fb_id && d->fd >= 0) {
		drmModeRmFB(d->fd, d->scan_fb_id);
		d->scan_fb_id = 0;
	}
	if (d->scan_handle && d->fd >= 0) {
		struct drm_gem_close cl = {.handle = d->scan_handle};
		drmIoctl(d->fd, DRM_IOCTL_GEM_CLOSE, &cl);
		d->scan_handle = 0;
	}
	if (d->fb_id) {
		drmModeRmFB(d->fd, d->fb_id);
	}
	if (d->handle) {
		struct drm_mode_destroy_dumb destroy = {.handle = d->handle};
		drmIoctl(d->fd, DRM_IOCTL_MODE_DESTROY_DUMB, &destroy);
	}
	if (d->fd >= 0) {
		drmDropMaster(d->fd);
		close(d->fd);
		d->fd = -1;
	}
	free(d);
}

const char *worldr_drm_card(const worldr_drm *d)
{
	return d ? d->card : "";
}

uint32_t worldr_drm_width(const worldr_drm *d)
{
	return d ? d->mode.hdisplay : 0;
}

uint32_t worldr_drm_height(const worldr_drm *d)
{
	return d ? d->mode.vdisplay : 0;
}

uint32_t worldr_drm_stride(const worldr_drm *d)
{
	return d ? d->pitch : 0;
}

int worldr_drm_present_bgra(worldr_drm *d, const uint8_t *bgra, uint32_t stride, char *err, int errlen)
{
	if (!d || !d->map || !bgra) {
		seterr(err, errlen, "drm present: missing buffer", 0);
		return -1;
	}
	if (d->scan_active) {
		if (worldr_drm_scanout_restore(d, err, errlen) != 0) {
			return -1;
		}
	}
	uint32_t h = d->mode.vdisplay;
	uint32_t w = d->mode.hdisplay;
	uint8_t *dst = (uint8_t *)d->map;
	for (uint32_t y = 0; y < h; y++) {
		uint32_t src_off = y * stride;
		uint32_t dst_off = y * d->pitch;
		uint32_t n = w * 4;
		if (n > stride) {
			n = stride;
		}
		if (n > d->pitch) {
			n = d->pitch;
		}
		memcpy(dst + dst_off, bgra + src_off, n);
	}
	return 0;
}

static uint32_t prop_id(int fd, uint32_t obj, uint32_t type, const char *name)
{
	drmModeObjectProperties *props = drmModeObjectGetProperties(fd, obj, type);
	if (!props) {
		return 0;
	}
	uint32_t found = 0;
	for (uint32_t i = 0; i < props->count_props; i++) {
		drmModePropertyRes *p = drmModeGetProperty(fd, props->props[i]);
		if (!p) {
			continue;
		}
		if (strcmp(p->name, name) == 0) {
			found = p->prop_id;
		}
		drmModeFreeProperty(p);
		if (found) {
			break;
		}
	}
	drmModeFreeObjectProperties(props);
	return found;
}

static int atomic_set_primary(worldr_drm *d, uint32_t fb_id)
{
	if (drmSetClientCap(d->fd, DRM_CLIENT_CAP_ATOMIC, 1) != 0) {
		return -1;
	}
	if (drmSetClientCap(d->fd, DRM_CLIENT_CAP_UNIVERSAL_PLANES, 1) != 0) {
		return -1;
	}
	drmModePlaneRes *pres = drmModeGetPlaneResources(d->fd);
	if (!pres) {
		return -1;
	}
	uint32_t plane_id = 0;
	for (uint32_t i = 0; i < pres->count_planes; i++) {
		drmModePlane *pl = drmModeGetPlane(d->fd, pres->planes[i]);
		if (!pl) {
			continue;
		}
		int on_crtc = (int)(pl->possible_crtcs & (1u << 0));
		/* Match the CRTC index in resources when we can; otherwise first possible. */
		if (pl->crtc_id == d->crtc_id || (on_crtc && !plane_id)) {
			uint32_t type = prop_id(d->fd, pl->plane_id, DRM_MODE_OBJECT_PLANE, "type");
			drmModeObjectProperties *op = drmModeObjectGetProperties(d->fd, pl->plane_id, DRM_MODE_OBJECT_PLANE);
			int is_primary = 0;
			if (op && type) {
				for (uint32_t k = 0; k < op->count_props; k++) {
					if (op->props[k] == type && op->prop_values[k] == DRM_PLANE_TYPE_PRIMARY) {
						is_primary = 1;
					}
				}
			}
			if (op) {
				drmModeFreeObjectProperties(op);
			}
			if (is_primary || pl->crtc_id == d->crtc_id) {
				plane_id = pl->plane_id;
				drmModeFreePlane(pl);
				if (is_primary) {
					break;
				}
			} else {
				drmModeFreePlane(pl);
			}
		} else {
			drmModeFreePlane(pl);
		}
	}
	drmModeFreePlaneResources(pres);
	if (!plane_id) {
		return -1;
	}

	uint32_t p_fb = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "FB_ID");
	uint32_t p_crtc = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "CRTC_ID");
	uint32_t p_sx = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "SRC_X");
	uint32_t p_sy = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "SRC_Y");
	uint32_t p_sw = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "SRC_W");
	uint32_t p_sh = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "SRC_H");
	uint32_t p_cx = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "CRTC_X");
	uint32_t p_cy = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "CRTC_Y");
	uint32_t p_cw = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "CRTC_W");
	uint32_t p_ch = prop_id(d->fd, plane_id, DRM_MODE_OBJECT_PLANE, "CRTC_H");
	if (!p_fb || !p_crtc || !p_sw || !p_sh || !p_cw || !p_ch) {
		return -1;
	}

	uint32_t w = d->mode.hdisplay;
	uint32_t h = d->mode.vdisplay;
	drmModeAtomicReq *req = drmModeAtomicAlloc();
	if (!req) {
		return -1;
	}
	drmModeAtomicAddProperty(req, plane_id, p_fb, fb_id);
	drmModeAtomicAddProperty(req, plane_id, p_crtc, d->crtc_id);
	if (p_sx) {
		drmModeAtomicAddProperty(req, plane_id, p_sx, 0);
	}
	if (p_sy) {
		drmModeAtomicAddProperty(req, plane_id, p_sy, 0);
	}
	drmModeAtomicAddProperty(req, plane_id, p_sw, (uint64_t)w << 16);
	drmModeAtomicAddProperty(req, plane_id, p_sh, (uint64_t)h << 16);
	if (p_cx) {
		drmModeAtomicAddProperty(req, plane_id, p_cx, 0);
	}
	if (p_cy) {
		drmModeAtomicAddProperty(req, plane_id, p_cy, 0);
	}
	drmModeAtomicAddProperty(req, plane_id, p_cw, w);
	drmModeAtomicAddProperty(req, plane_id, p_ch, h);
	int r = drmModeAtomicCommit(d->fd, req, DRM_MODE_ATOMIC_ALLOW_MODESET, NULL);
	drmModeAtomicFree(req);
	return r;
}

static void drop_scan_fb(worldr_drm *d)
{
	if (!d || d->fd < 0) {
		return;
	}
	if (d->scan_fb_id) {
		drmModeRmFB(d->fd, d->scan_fb_id);
		d->scan_fb_id = 0;
	}
	if (d->scan_handle) {
		struct drm_gem_close cl = {.handle = d->scan_handle};
		drmIoctl(d->fd, DRM_IOCTL_GEM_CLOSE, &cl);
		d->scan_handle = 0;
	}
}

int worldr_drm_scanout_dmabuf(worldr_drm *d, int dmabuf_fd, uint32_t width, uint32_t height,
			      uint32_t fourcc, uint64_t modifier, uint32_t offset, uint32_t pitch,
			      char *err, int errlen)
{
	if (!d || d->fd < 0 || dmabuf_fd < 0) {
		seterr(err, errlen, "drm scanout: missing session or dmabuf fd", 0);
		return -1;
	}
	if (width != d->mode.hdisplay || height != d->mode.vdisplay) {
		seterr(err, errlen, "drm scanout: buffer size != CRTC mode", 0);
		return -1;
	}
	if (pitch == 0) {
		pitch = width * 4;
	}
	uint32_t handle = 0;
	if (drmPrimeFDToHandle(d->fd, dmabuf_fd, &handle) != 0) {
		seterr(err, errlen, "drmPrimeFDToHandle", errno);
		return -1;
	}
	uint32_t handles[4] = {handle, 0, 0, 0};
	uint32_t pitches[4] = {pitch, 0, 0, 0};
	uint32_t offsets[4] = {offset, 0, 0, 0};
	uint64_t mods[4] = {modifier, 0, 0, 0};
	uint32_t fb_id = 0;
	int added = -1;
	int use_mod = modifier != 0 && modifier != DRM_FORMAT_MOD_LINEAR && modifier != DRM_FORMAT_MOD_INVALID;
	if (use_mod) {
		added = drmModeAddFB2WithModifiers(d->fd, width, height, fourcc, handles, pitches, offsets, mods, &fb_id,
						   DRM_MODE_FB_MODIFIERS);
	}
	if (added != 0) {
		added = drmModeAddFB2(d->fd, width, height, fourcc, handles, pitches, offsets, &fb_id, 0);
	}
	if (added != 0) {
		struct drm_gem_close cl = {.handle = handle};
		drmIoctl(d->fd, DRM_IOCTL_GEM_CLOSE, &cl);
		seterr(err, errlen, "drmModeAddFB2", errno);
		return -1;
	}
	int committed = atomic_set_primary(d, fb_id);
	if (committed != 0) {
		committed = drmModeSetCrtc(d->fd, d->crtc_id, fb_id, 0, 0, &d->connector_id, 1, &d->mode);
	}
	if (committed != 0) {
		drmModeRmFB(d->fd, fb_id);
		struct drm_gem_close cl = {.handle = handle};
		drmIoctl(d->fd, DRM_IOCTL_GEM_CLOSE, &cl);
		seterr(err, errlen, "KMS primary commit (atomic/SetCrtc)", errno);
		return -1;
	}
	drop_scan_fb(d);
	d->scan_fb_id = fb_id;
	d->scan_handle = handle;
	d->scan_active = 1;
	return 0;
}

int worldr_drm_scanout_restore(worldr_drm *d, char *err, int errlen)
{
	if (!d || d->fd < 0) {
		seterr(err, errlen, "drm scanout restore: missing session", 0);
		return -1;
	}
	if (!d->scan_active) {
		return 0;
	}
	int r = atomic_set_primary(d, d->fb_id);
	if (r != 0) {
		r = drmModeSetCrtc(d->fd, d->crtc_id, d->fb_id, 0, 0, &d->connector_id, 1, &d->mode);
	}
	drop_scan_fb(d);
	d->scan_active = 0;
	if (r != 0) {
		seterr(err, errlen, "restore dumb FB", errno);
		return -1;
	}
	return 0;
}

int worldr_drm_scanout_active(const worldr_drm *d)
{
	return d && d->scan_active ? 1 : 0;
}
