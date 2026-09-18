//go:build linux && cgo

package host

/*
#include "text_input.h"
#include <stdlib.h>
*/
import "C"

import "unsafe"

func (w *Window) TextInputAvailable() bool {
	return w != nil && w.ptr != nil && C.worldr_host_text_input_available(C.worldr_host_text_input_state(w.ptr)) != 0
}

// SetTextInput borrows no Go storage. An absent optional compositor protocol is
// a supported fallback; ordinary XKB/compose input continues to work.
func (w *Window) SetTextInput(state TextInputState) error {
	if w == nil || w.ptr == nil {
		return ErrClosed
	}
	if err := state.validate(); err != nil {
		return err
	}
	if !state.Enabled {
		state = TextInputState{}
	}
	context, text := C.CString(state.ContextID), C.CString(state.Surrounding)
	defer C.free(unsafe.Pointer(context))
	defer C.free(unsafe.Pointer(text))
	enabled := 0
	if state.Enabled {
		enabled = 1
	}
	cause := 0
	if w.textInputCause {
		cause = 1
	}
	x, y, width, height := C.int(state.CursorRect[0]), C.int(state.CursorRect[1]), C.int(state.CursorRect[2]), C.int(state.CursorRect[3])
	C.worldr_host_rect_to_surface(w.ptr, &x, &y, &width, &height)
	C.worldr_host_text_input_set(C.worldr_host_text_input_state(w.ptr), C.int(enabled), context, text, C.int(state.Cursor), C.int(state.Anchor), x, y, width, height, 1, C.int(cause))
	w.textInputCause = false
	return nil
}
