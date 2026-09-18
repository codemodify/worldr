package app

import "github.com/codemodify/worldr/internal/experience"

func (a *applicationController) ApplicationDragActive(source uint64) bool {
	if a.closed || a.images[source] == nil {
		return false
	}
	if a.x11 != nil && a.x11.DragActive(source) {
		return true
	}
	server, ok := a.server.(interface{ DragActive() bool })
	return ok && server.DragActive()
}
func (a *applicationController) ApplicationDrag(source, target uint64, event experience.Event) bool {
	if !a.ApplicationDragActive(source) {
		return false
	}
	x11Drag := a.x11 != nil && a.x11.DragActive(source)
	if event.Kind == experience.PointerCancel || event.Kind == experience.KeyboardCancel {
		if x11Drag {
			a.remember(a.x11.CancelXDND())
		}
		if server, ok := a.server.(interface{ CancelDrag() error }); ok {
			a.remember(server.CancelDrag())
		}
		return false
	}
	entry := a.images[target]
	x11Target := false
	if entry == nil {
		a.remember(a.server.Pointer(0, 0, 0))
		if x11Drag {
			_ = a.x11.DragMotion(source, 0, 0, 0, event.Time)
		}
	} else {
		width, height := entry.surface.Texture.Size()
		x := event.X * float32(entry.logicalW) / float32(width)
		y := event.Y * float32(entry.logicalH) / float32(height)
		a.remember(a.server.Pointer(target, x, y))
		if x11Drag {
			if _, ok := a.x11.Window(target); ok {
				x11Target = a.x11.DragMotion(source, target, x, y, event.Time) == nil
			} else {
				_ = a.x11.DragMotion(source, 0, 0, 0, event.Time)
			}
		}
	}
	if event.Kind == experience.PointerUp {
		if x11Drag {
			if !x11Target || a.x11.DropXDND(source, target, event.Time) != nil {
				_ = a.x11.CancelXDND()
			}
		}
		code := event.ButtonCode
		if code == 0 && event.Button == experience.ButtonPrimary {
			code = 272
		}
		if code != 0 {
			a.remember(a.server.Button(code, false, event.Time))
		}
	}
	if x11Drag {
		return x11Target
	}
	return entry != nil
}
