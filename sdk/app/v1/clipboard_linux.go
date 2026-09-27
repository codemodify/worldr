//go:build linux

package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func (h *Host) serveClipboard() {
	if h.window == nil {
		return
	}
	for _, request := range h.window.PollClipboardRequests() {
		text, exists := h.clipboardText[request.ExternalID]
		if !exists {
			_ = unix.Close(request.FD)
			continue
		}
		if err := unix.SetNonblock(request.FD, true); err != nil {
			_ = unix.Close(request.FD)
			continue
		}
		file := os.NewFile(uintptr(request.FD), "worldr-kit-clipboard-send")
		if file == nil {
			_ = unix.Close(request.FD)
			continue
		}
		go func() {
			defer file.Close()
			if err := file.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return
			}
			_, _ = io.Copy(file, strings.NewReader(text))
		}()
	}
}

func (h *Host) readClipboard(callback func(string, error)) error {
	offer := h.window.ClipboardOffer()
	if !offer.Available || offer.ID == 0 {
		return ErrClipboardUnavailable
	}
	mime := ""
	for _, preferred := range []string{"text/plain;charset=utf-8", "UTF8_STRING", "text/plain"} {
		for _, candidate := range offer.MIMEs {
			if candidate == preferred {
				mime = candidate
				break
			}
		}
		if mime != "" {
			break
		}
	}
	if mime == "" {
		return errors.New("clipboard does not contain text")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	if err = h.window.ReceiveClipboard(offer.ID, mime, int(writer.Fd())); err != nil {
		reader.Close()
		writer.Close()
		return err
	}
	writer.Close()
	// Calling Fd on writer only makes that end blocking; the reader stays
	// registered with Go's poller, so a missing or stalled owner cannot hang us.
	if err = reader.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		reader.Close()
		return err
	}
	h.clipboardPending = true
	go func() {
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, maxClipboardBytes+1))
		if len(data) > maxClipboardBytes {
			data = nil
			err = fmt.Errorf("clipboard text exceeds 4 MiB")
		}
		if err != nil {
			data = nil
		}
		result := clipboardResult{callback: callback, text: string(data), err: err}
		select {
		case h.clipboardResults <- result:
		case <-h.done:
		}
	}()
	return nil
}
