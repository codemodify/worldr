//go:build linux && cgo

package textinput

/*
#cgo pkg-config: xkbcommon
#include <stdlib.h>
#include <stdint.h>
#include <xkbcommon/xkbcommon.h>
#include <xkbcommon/xkbcommon-compose.h>

typedef struct {
    struct xkb_context *context;
    struct xkb_keymap *keymap;
    struct xkb_state *state;
    struct xkb_compose_table *table;
    struct xkb_compose_state *compose;
} worldr_textinput;

static void textinput_free(worldr_textinput *t) {
    if (!t) return;
    xkb_compose_state_unref(t->compose);
    xkb_compose_table_unref(t->table);
    xkb_state_unref(t->state);
    xkb_keymap_unref(t->keymap);
    xkb_context_unref(t->context);
    free(t);
}
static worldr_textinput *textinput_new(const char *locale) {
    worldr_textinput *t=calloc(1,sizeof(*t));
    if (!t) return NULL;
    t->context=xkb_context_new(XKB_CONTEXT_NO_FLAGS);
    if (!t->context) goto fail;
    struct xkb_rule_names names={.layout="us"};
    t->keymap=xkb_keymap_new_from_names(t->context,&names,XKB_KEYMAP_COMPILE_NO_FLAGS);
    if (!t->keymap) goto fail;
    t->state=xkb_state_new(t->keymap);
    if (!t->state) goto fail;
    t->table=xkb_compose_table_new_from_locale(t->context,locale,XKB_COMPOSE_COMPILE_NO_FLAGS);
    if (t->table) t->compose=xkb_compose_state_new(t->table,XKB_COMPOSE_STATE_NO_FLAGS);
    return t;
fail:
    textinput_free(t);
    return NULL;
}
static void textinput_reset(worldr_textinput *t) {
    if (t->compose) xkb_compose_state_reset(t->compose);
}
static int textinput_keymap(worldr_textinput *t,const char *source) {
    struct xkb_keymap *map=xkb_keymap_new_from_string(t->context,source,XKB_KEYMAP_FORMAT_TEXT_V1,XKB_KEYMAP_COMPILE_NO_FLAGS);
    if (!map) return -1;
    struct xkb_state *state=xkb_state_new(map);
    if (!state) { xkb_keymap_unref(map); return -1; }
    xkb_state_unref(t->state); xkb_keymap_unref(t->keymap);
    t->state=state; t->keymap=map; textinput_reset(t);
    return 0;
}
static void textinput_modifiers(worldr_textinput *t,uint32_t depressed,uint32_t latched,uint32_t locked,uint32_t group,int shift) {
    if (shift) {
        xkb_mod_index_t index=xkb_keymap_mod_get_index(t->keymap,XKB_MOD_NAME_SHIFT);
        if (index<XKB_MOD_INVALID && index<32) depressed|=(1u<<index);
    }
    xkb_state_update_mask(t->state,depressed,latched,locked,0,0,group);
}
static int textinput_key(worldr_textinput *t,uint32_t key,char *output,size_t size) {
    if (xkb_state_mod_name_is_active(t->state,XKB_MOD_NAME_CTRL,XKB_STATE_MODS_EFFECTIVE)>0 ||
        xkb_state_mod_name_is_active(t->state,XKB_MOD_NAME_ALT,XKB_STATE_MODS_EFFECTIVE)>0 ||
        xkb_state_mod_name_is_active(t->state,XKB_MOD_NAME_LOGO,XKB_STATE_MODS_EFFECTIVE)>0) {
        textinput_reset(t); return 0;
    }
    xkb_keysym_t sym=xkb_state_key_get_one_sym(t->state,key+8);
    if (t->compose) {
        xkb_compose_state_feed(t->compose,sym);
        switch (xkb_compose_state_get_status(t->compose)) {
        case XKB_COMPOSE_COMPOSING: return 0;
        case XKB_COMPOSE_COMPOSED: {
            int n=xkb_compose_state_get_utf8(t->compose,output,size);
            textinput_reset(t); return n;
        }
        case XKB_COMPOSE_CANCELLED: textinput_reset(t); return 0;
        default: break;
        }
    }
    return xkb_state_key_get_utf8(t->state,key+8,output,size);
}
*/
import "C"

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/codemodify/worldr/internal/experience"
)

// Translator belongs to one host goroutine. Forward seat metadata regardless of
// text-field focus; forward physical keys only to the currently focused field.
type Translator struct{ ptr *C.worldr_textinput }

func New() (*Translator, error) {
	locale := "C.UTF-8"
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := os.Getenv(key); value != "" {
			locale = value
			break
		}
	}
	name := C.CString(locale)
	defer C.free(unsafe.Pointer(name))
	ptr := C.textinput_new(name)
	if ptr == nil {
		return nil, fmt.Errorf("cannot initialize native text input")
	}
	return &Translator{ptr: ptr}, nil
}

func (t *Translator) Handle(event experience.Event) string {
	if t == nil || t.ptr == nil {
		return ""
	}
	switch event.Kind {
	case experience.TextCommit, experience.TextPreedit:
		C.textinput_reset(t.ptr)
	case experience.KeymapChanged:
		if len(event.Keymap) == 0 || len(event.Keymap) > 1<<20 || strings.ContainsRune(event.Keymap, 0) {
			return ""
		}
		source := C.CString(event.Keymap)
		C.textinput_keymap(t.ptr, source)
		C.free(unsafe.Pointer(source))
	case experience.KeyboardCancel:
		C.textinput_reset(t.ptr)
		C.textinput_modifiers(t.ptr, 0, 0, 0, 0, 0)
	case experience.KeyboardModifiers:
		C.textinput_modifiers(t.ptr, C.uint32_t(event.Depressed), C.uint32_t(event.Latched), C.uint32_t(event.Locked), C.uint32_t(event.Group), 0)
	case experience.KeyInput:
		if !event.Pressed || event.Keycode == 0 || event.Keycode > 767 {
			return ""
		}
		if event.Modifiers&(experience.ModControl|experience.ModAlt|experience.ModSuper) != 0 || event.Keycode == 1 || event.Keycode == 14 || event.Keycode == 15 || event.Keycode == 28 || event.Keycode == 96 {
			C.textinput_reset(t.ptr)
			return ""
		}
		shift := C.int(0)
		if event.Modifiers.Has(experience.ModShift) {
			shift = 1
		}
		C.textinput_modifiers(t.ptr, C.uint32_t(event.Depressed), C.uint32_t(event.Latched), C.uint32_t(event.Locked), C.uint32_t(event.Group), shift)
		var output [513]byte
		n := int(C.textinput_key(t.ptr, C.uint32_t(event.Keycode), (*C.char)(unsafe.Pointer(&output[0])), C.size_t(len(output))))
		if n > 0 && n < len(output) {
			return printable(string(output[:n]))
		}
	}
	return ""
}

func (t *Translator) Close() {
	if t != nil && t.ptr != nil {
		C.textinput_free(t.ptr)
		t.ptr = nil
	}
}
