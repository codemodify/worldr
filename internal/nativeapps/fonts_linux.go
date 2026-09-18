//go:build linux && cgo

package nativeapps

/*
#cgo pkg-config: fontconfig
#include <fontconfig/fontconfig.h>
#include <stdlib.h>
#include <string.h>

// All patterns/charsets stay C-owned, and the copied path outlives the match.
static char *worldr_match_font(FcConfig *config, unsigned int character, int style, int *index) {
	FcPattern *pattern = FcPatternCreate();
	FcCharSet *charset = FcCharSetCreate();
	FcPattern *matched = NULL;
	char *path = NULL;
	if (!pattern || !charset) goto done;
	if (!FcCharSetAddChar(charset, character) ||
	    !FcPatternAddCharSet(pattern, FC_CHARSET, charset) ||
	    !FcPatternAddString(pattern, FC_FAMILY, (const FcChar8 *)"monospace") ||
	    !FcPatternAddInteger(pattern, FC_SPACING, FC_MONO) ||
	    !FcPatternAddInteger(pattern, FC_WEIGHT, (style & 1) ? FC_WEIGHT_BOLD : FC_WEIGHT_REGULAR) ||
	    !FcPatternAddInteger(pattern, FC_SLANT, (style & 2) ? FC_SLANT_ITALIC : FC_SLANT_ROMAN) ||
	    !FcPatternAddBool(pattern, FC_SCALABLE, FcTrue) ||
	    !FcConfigSubstitute(config, pattern, FcMatchPattern)) goto done;
	FcDefaultSubstitute(pattern);
	FcResult result;
	matched = FcFontMatch(config, pattern, &result);
	if (!matched) goto done;
	FcChar8 *file = NULL;
	FcCharSet *coverage = NULL;
	if (FcPatternGetString(matched, FC_FILE, 0, &file) != FcResultMatch ||
	    FcPatternGetCharSet(matched, FC_CHARSET, 0, &coverage) != FcResultMatch ||
	    !FcCharSetHasChar(coverage, character)) goto done;
	*index = 0;
	FcPatternGetInteger(matched, FC_INDEX, 0, index);
	// SFNT supports collection face indices; named variable-font instances
	// encoded in the upper 16 bits require variation support we do not have.
	if (*index < 0 || *index > 0xffff || strlen((const char *)file) > 4096) goto done;
	path = strdup((const char *)file);
done:
	if (matched) FcPatternDestroy(matched);
	if (charset) FcCharSetDestroy(charset);
	if (pattern) FcPatternDestroy(pattern);
	return path;
}
*/
import "C"

import (
	"sync"
	"unsafe"
)

// Share one private configuration among live terminal renderers. No process
// global Fontconfig shutdown, no rescan during painting, and no permanent
// configuration allocation once the last renderer releases its resolver.
var terminalFontconfig struct {
	sync.Mutex
	config *C.FcConfig
	refs   int
}

type systemFontMatcher struct{ closed bool }

func newSystemFontMatcher() terminalFontMatcher {
	terminalFontconfig.Lock()
	defer terminalFontconfig.Unlock()
	if terminalFontconfig.config == nil {
		terminalFontconfig.config = C.FcInitLoadConfigAndFonts()
		if terminalFontconfig.config == nil {
			return nil
		}
		C.FcConfigSetRescanInterval(terminalFontconfig.config, 0)
	}
	terminalFontconfig.refs++
	return &systemFontMatcher{}
}

func (m *systemFontMatcher) Match(character rune, style int) (fallbackFontLocation, bool) {
	terminalFontconfig.Lock()
	defer terminalFontconfig.Unlock()
	if m.closed {
		return fallbackFontLocation{}, false
	}
	var index C.int
	path := C.worldr_match_font(terminalFontconfig.config, C.uint(character), C.int(style), &index)
	if path == nil {
		return fallbackFontLocation{}, false
	}
	defer C.free(unsafe.Pointer(path))
	return fallbackFontLocation{Path: C.GoString(path), Index: int(index)}, true
}

func (m *systemFontMatcher) Close() {
	terminalFontconfig.Lock()
	defer terminalFontconfig.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	terminalFontconfig.refs--
	if terminalFontconfig.refs == 0 {
		C.FcConfigDestroy(terminalFontconfig.config)
		terminalFontconfig.config = nil
	}
}
