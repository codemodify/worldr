//go:build !linux || !cgo

package apps

func (s *Server) SetScale120(uint32) error { return errUnavailable }
