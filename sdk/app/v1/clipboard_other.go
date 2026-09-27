//go:build !linux

package app

func (*Host) serveClipboard()                         {}
func (*Host) readClipboard(func(string, error)) error { return ErrClipboardUnavailable }
