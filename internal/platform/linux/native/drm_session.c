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
