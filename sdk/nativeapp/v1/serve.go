package nativeapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Serve runs one application connection. It returns after RequestShutdown,
// input closure, context cancellation observed between requests, or a protocol
// failure. The host serializes requests, so application callbacks never overlap.
func Serve(ctx context.Context, app Application, reader io.Reader, writer io.Writer) (serveErr error) {
	if app == nil {
		return fmt.Errorf("native app is nil")
	}
	manifest := app.Manifest()
	if err := manifest.Validate(); err != nil {
		return err
	}
	codec := NewCodec(reader, writer)
	var validator Validator
	var host Host
	started, closed := false, false
	defer func() {
		if started && !closed {
			if closer, ok := app.(Closer); ok {
				serveErr = errors.Join(serveErr, closer.Close())
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var request Request
		if err := codec.Read(&request); err != nil {
			if errors.Is(err, io.EOF) && ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		response := Response{Version: Version, Sequence: request.Sequence}
		if request.Version != Version || request.Sequence == 0 {
			response.Error = fmt.Sprintf("unsupported native app request version %d or zero sequence", request.Version)
			if err := codec.Write(response); err != nil {
				return err
			}
			continue
		}
		var callbackErr error
		switch request.Kind {
		case RequestHello:
			if started {
				callbackErr = fmt.Errorf("hello may only be sent once")
				break
			}
			if request.Host.Version != Version || request.Host.MaxSurfaceWidth <= 0 || request.Host.MaxSurfaceHeight <= 0 || request.Host.MaxSurfaces <= 0 || request.Host.MaxSurfaces > MaxSurfaces {
				callbackErr = fmt.Errorf("host supplied invalid v1 limits")
				break
			}
			if starter, ok := app.(Starter); ok {
				callbackErr = starter.Start(request.Host)
			}
			if callbackErr == nil {
				host = request.Host
				started = true
				response.Manifest = &manifest
			}
		case RequestUpdate:
			if !started {
				callbackErr = fmt.Errorf("hello is required before update")
			} else if request.DeltaNanos < 0 || request.DeltaNanos > int64(10*time.Second) {
				callbackErr = fmt.Errorf("update delta is outside 0..10s")
			} else if updater, ok := app.(Updater); ok {
				callbackErr = updater.Update(time.Duration(request.DeltaNanos))
			}
		case RequestInput:
			if !started || request.Surface == 0 && request.Event != nil && request.Event.Kind != KeymapChanged && request.Event.Kind != KeyboardModifiers && request.Event.Kind != KeyboardRepeat && request.Event.Kind != KeyboardCancel {
				callbackErr = fmt.Errorf("input references an invalid application or surface")
			} else if request.Event == nil {
				callbackErr = fmt.Errorf("input request has no event")
			} else if callbackErr = validateEvent(*request.Event); callbackErr == nil {
				if handler, ok := app.(InputHandler); ok {
					callbackErr = handler.Handle(request.Surface, *request.Event)
				}
			}
		case RequestFocus:
			if !started {
				callbackErr = fmt.Errorf("hello is required before focus")
			} else if handler, ok := app.(FocusHandler); ok {
				callbackErr = handler.Focus(request.Surface)
			}
		case RequestResize:
			if !started || request.Surface == 0 || request.Width <= 0 || request.Height <= 0 || request.Width > host.MaxSurfaceWidth || request.Height > host.MaxSurfaceHeight {
				callbackErr = fmt.Errorf("resize request is outside negotiated limits")
			}
			if callbackErr == nil {
				if handler, ok := app.(ResizeHandler); ok {
					callbackErr = handler.Resize(request.Surface, request.Width, request.Height)
				}
			}
		case RequestCloseSurface:
			if !started || request.Surface == 0 {
				callbackErr = fmt.Errorf("close request has no live surface")
			} else if handler, ok := app.(SurfaceCloser); ok {
				callbackErr = handler.CloseSurface(request.Surface)
			}
		case RequestShutdown:
			if started {
				if closer, ok := app.(Closer); ok {
					callbackErr = closer.Close()
				}
				closed = true
			}
		default:
			callbackErr = fmt.Errorf("unknown native app request %q", request.Kind)
		}
		if callbackErr == nil && started && request.Kind != RequestShutdown {
			snapshot := app.Snapshot()
			if callbackErr = validator.Validate(snapshot); callbackErr == nil {
				response.Snapshot = &snapshot
			}
		}
		if callbackErr != nil {
			response.Error = callbackErr.Error()
		}
		if err := codec.Write(response); err != nil {
			return err
		}
		if request.Kind == RequestShutdown {
			return callbackErr
		}
	}
}
