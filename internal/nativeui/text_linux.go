//go:build linux && cgo

package nativeui

/*
#cgo pkg-config: pangocairo
#include <pango/pangocairo.h>
#include <stdlib.h>
#include <string.h>
static PangoLayout *ui_layout(const char*text,const char*family,double size,int width) {
    PangoFontMap*map=pango_cairo_font_map_get_default();if(!map)return NULL;
    PangoContext*context=pango_font_map_create_context(map);if(!context)return NULL;
    PangoLayout*layout=pango_layout_new(context);g_object_unref(context);if(!layout)return NULL;
    PangoFontDescription*font=pango_font_description_new();pango_font_description_set_family(font,family);pango_font_description_set_absolute_size(font,size*PANGO_SCALE);
    pango_layout_set_font_description(layout,font);pango_font_description_free(font);
    pango_layout_set_text(layout,text,-1);pango_layout_set_single_paragraph_mode(layout,TRUE);pango_layout_set_auto_dir(layout,TRUE);
    if(width>=0){pango_layout_set_width(layout,width*PANGO_SCALE);pango_layout_set_ellipsize(layout,PANGO_ELLIPSIZE_END);}
    return layout;
}
static void ui_metrics(PangoLayout*l,int*width,int*height,int*baseline,int*glyphs,int*runs) {
    pango_layout_get_pixel_size(l,width,height);*baseline=PANGO_PIXELS(pango_layout_get_baseline(l));*glyphs=0;*runs=0;
    PangoLayoutIter*iter=pango_layout_get_iter(l);
    do{PangoLayoutRun*run=pango_layout_iter_get_run_readonly(iter);if(run){*runs+=1;*glyphs+=run->glyphs->num_glyphs;}}while(pango_layout_iter_next_run(iter));pango_layout_iter_free(iter);
}
static int ui_draw(PangoLayout*l,unsigned char*pixels,int width,int height,int stride,int x,int y) {
    cairo_surface_t*surface=cairo_image_surface_create_for_data(pixels,CAIRO_FORMAT_A8,width,height,stride);
    if(cairo_surface_status(surface)!=CAIRO_STATUS_SUCCESS){cairo_surface_destroy(surface);return -1;}
    cairo_t*cr=cairo_create(surface);cairo_set_source_rgba(cr,1,1,1,1);cairo_move_to(cr,x,y);pango_cairo_show_layout(cr,l);cairo_surface_flush(surface);
    int ok=cairo_status(cr)==CAIRO_STATUS_SUCCESS;cairo_destroy(cr);cairo_surface_destroy(surface);return ok?0:-1;
}
static void ui_caret(PangoLayout*l,int index,int*x,int*y){PangoRectangle pos;pango_layout_get_cursor_pos(l,index,&pos,NULL);*x=PANGO_PIXELS(pos.x);*y=PANGO_PIXELS(pos.y);}
static int ui_hit(PangoLayout*l,int x,int y){int index=0,trailing=0;pango_layout_xy_to_index(l,x*PANGO_SCALE,y*PANGO_SCALE,&index,&trailing);const char*text=pango_layout_get_text(l);const char*p=text+index;while(trailing--&&*p)p=g_utf8_next_char(p);return (int)(p-text);}
static int ui_move(PangoLayout*l,int old,int dir){int index=0,trailing=0;pango_layout_move_cursor_visually(l,TRUE,old,0,dir,&index,&trailing);const char*text=pango_layout_get_text(l);if(index<0)return 0;if(index==G_MAXINT)return strlen(text);const char*p=text+index;while(trailing--&&*p)p=g_utf8_next_char(p);return (int)(p-text);}
static int ui_stops(const char*text,int*stops,int capacity){int chars=g_utf8_strlen(text,-1);PangoLogAttr*attrs=g_new0(PangoLogAttr,chars+1);pango_get_log_attrs(text,-1,-1,pango_language_get_default(),attrs,chars+1);int n=0;const char*p=text;for(int i=0;i<=chars;i++){if(attrs[i].is_cursor_position&&n<capacity)stops[n++]=(int)(p-text);if(*p)p=g_utf8_next_char(p);}g_free(attrs);return n;}
static int ui_ranges(PangoLayout*l,int first,int last,int*output,int capacity){PangoLayoutLine*line=pango_layout_get_line_readonly(l,0);if(!line)return 0;int*ranges=NULL,count=0;pango_layout_line_get_x_ranges(line,first,last,&ranges,&count);if(count>capacity)count=capacity;for(int i=0;i<count*2;i++)output[i]=PANGO_PIXELS(ranges[i]);g_free(ranges);return count;}
*/
import "C"

import (
	"fmt"
	"image"
	"unicode/utf8"
	"unsafe"
)

type pangoLayout struct {
	ptr  *C.PangoLayout
	size Metrics
}

func newTextLayout(text, font string, size float64, width int) (textLayout, error) {
	value, family := C.CString(text), C.CString(font)
	defer C.free(unsafe.Pointer(value))
	defer C.free(unsafe.Pointer(family))
	ptr := C.ui_layout(value, family, C.double(size), C.int(width))
	if ptr == nil {
		return nil, fmt.Errorf("create native UI Pango layout")
	}
	var w, h, b, g, r C.int
	C.ui_metrics(ptr, &w, &h, &b, &g, &r)
	return &pangoLayout{ptr: ptr, size: Metrics{int(w), int(h), int(b), int(g), int(r), true}}, nil
}
func (p *pangoLayout) metrics() Metrics { return p.size }
func (p *pangoLayout) close() {
	if p.ptr != nil {
		C.g_object_unref(C.gpointer(p.ptr))
		p.ptr = nil
	}
}
func (p *pangoLayout) draw(dst *image.Alpha, origin image.Point) error {
	// Cairo requires a four-byte-aligned A8 stride. The temporary Go buffer is
	// borrowed only for this synchronous call and is never retained by C.
	width, height := dst.Rect.Dx(), dst.Rect.Dy()
	stride := (width + 3) &^ 3
	pixels := make([]byte, stride*height)
	if C.ui_draw(p.ptr, (*C.uchar)(unsafe.Pointer(&pixels[0])), C.int(width), C.int(height), C.int(stride), C.int(origin.X), C.int(origin.Y)) != 0 {
		return fmt.Errorf("rasterize native UI label")
	}
	for y := 0; y < height; y++ {
		copy(dst.Pix[y*dst.Stride:][:width], pixels[y*stride:][:width])
	}
	return nil
}
func (p *pangoLayout) caret(index int) image.Point {
	var x, y C.int
	C.ui_caret(p.ptr, C.int(index), &x, &y)
	return image.Pt(int(x), int(y))
}
func (p *pangoLayout) hit(point image.Point) int {
	return int(C.ui_hit(p.ptr, C.int(point.X), C.int(point.Y)))
}
func (p *pangoLayout) ranges(first, last int) []image.Rectangle {
	var positions [256]C.int
	n := int(C.ui_ranges(p.ptr, C.int(first), C.int(last), &positions[0], 128))
	result := make([]image.Rectangle, n)
	for i := range result {
		result[i] = image.Rect(int(positions[i*2]), 0, int(positions[i*2+1]), p.size.Height)
	}
	return result
}
func graphemeStops(text string) []int {
	value := C.CString(text)
	defer C.free(unsafe.Pointer(value))
	positions := make([]C.int, utf8.RuneCountInString(text)+1)
	n := int(C.ui_stops(value, &positions[0], C.int(len(positions))))
	result := make([]int, n)
	for i := range result {
		result[i] = int(positions[i])
	}
	return result
}
func visualMove(text string, index, direction int) int {
	layout, err := newTextLayout(text, "Sans", 14, -1)
	if err != nil {
		return index
	}
	defer layout.close()
	return int(C.ui_move(layout.(*pangoLayout).ptr, C.int(index), C.int(direction)))
}
