//go:build linux && cgo && librsvg

package icontheme

/*
#cgo pkg-config: librsvg-2.0 cairo
#include <cairo.h>
#include <librsvg/rsvg.h>
#include <stdlib.h>
#include <string.h>

static void worldr_rsvg_seterr(char *err, int errlen, const char *msg)
{
	if (err == NULL || errlen <= 0) {
		return;
	}
	if (msg == NULL) {
		msg = "rsvg failed";
	}
	strncpy(err, msg, (size_t)errlen - 1);
	err[errlen - 1] = '\0';
}

int worldr_rsvg_raster(const char *path, int want,
		       unsigned char *out, int out_len,
		       int *w, int *h, int *stride,
		       char *err, int errlen)
{
	GError *gerr = NULL;
	RsvgHandle *handle = NULL;
	cairo_surface_t *surf = NULL;
	cairo_t *cr = NULL;
	unsigned char *data;
	int cr_stride, y;
	RsvgRectangle vp;

	if (path == NULL || out == NULL || want < 1) {
		worldr_rsvg_seterr(err, errlen, "rsvg: bad args");
		return -1;
	}

	handle = rsvg_handle_new_from_file(path, &gerr);
	if (handle == NULL) {
		worldr_rsvg_seterr(err, errlen, gerr ? gerr->message : "rsvg open");
		if (gerr) {
			g_error_free(gerr);
		}
		return -1;
	}

	surf = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, want, want);
	if (cairo_surface_status(surf) != CAIRO_STATUS_SUCCESS) {
		worldr_rsvg_seterr(err, errlen, "cairo surface");
		g_object_unref(handle);
		if (surf) {
			cairo_surface_destroy(surf);
		}
		return -1;
	}
	cr = cairo_create(surf);
	cairo_set_source_rgba(cr, 0, 0, 0, 0);
	cairo_set_operator(cr, CAIRO_OPERATOR_SOURCE);
	cairo_paint(cr);
	cairo_set_operator(cr, CAIRO_OPERATOR_OVER);

	vp.x = 0;
	vp.y = 0;
	vp.width = (double)want;
	vp.height = (double)want;
	if (!rsvg_handle_render_document(handle, cr, &vp, &gerr)) {
		worldr_rsvg_seterr(err, errlen, gerr ? gerr->message : "rsvg render");
		if (gerr) {
			g_error_free(gerr);
		}
		cairo_destroy(cr);
		cairo_surface_destroy(surf);
		g_object_unref(handle);
		return -1;
	}

	cairo_surface_flush(surf);
	data = cairo_image_surface_get_data(surf);
	cr_stride = cairo_image_surface_get_stride(surf);
	if (data == NULL || cr_stride * want > out_len) {
		worldr_rsvg_seterr(err, errlen, "rsvg buffer");
		cairo_destroy(cr);
		cairo_surface_destroy(surf);
		g_object_unref(handle);
		return -1;
	}
	for (y = 0; y < want; y++) {
		memcpy(out + y * (want * 4), data + y * cr_stride, (size_t)want * 4);
	}
	if (w) {
		*w = want;
	}
	if (h) {
		*h = want;
	}
	if (stride) {
		*stride = want * 4;
	}
	cairo_destroy(cr);
	cairo_surface_destroy(surf);
	g_object_unref(handle);
	return 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// RsvgAvailable is true when this binary was built with -tags=librsvg
// and pkg-config found librsvg-2.0 (see Makefile / RUN-ABOX).
func RsvgAvailable() bool { return true }

// RasterRSVG rasters path with librsvg into BGRA at want×want.
func RasterRSVG(path string, want int) (pix []byte, w, h, stride int, err error) {
	if want < 1 {
		want = DefaultWant
	}
	if want > maxSVGEdge {
		want = maxSVGEdge
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	out := make([]byte, want*want*4)
	var cw, ch, cst C.int
	var ebuf [256]C.char
	rc := C.worldr_rsvg_raster(cpath, C.int(want),
		(*C.uchar)(unsafe.Pointer(&out[0])), C.int(len(out)),
		&cw, &ch, &cst, &ebuf[0], C.int(len(ebuf)))
	if rc != 0 {
		return nil, 0, 0, 0, fmt.Errorf("librsvg: %s", C.GoString(&ebuf[0]))
	}
	return out, int(cw), int(ch), int(cst), nil
}
