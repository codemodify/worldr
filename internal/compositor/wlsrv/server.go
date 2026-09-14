// Package wlsrv is a minimal pure-Go Wayland compositor (core + xdg_shell).
package wlsrv

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/codemodify/worldr/internal/engine"
)

// Server accepts Wayland clients and maps surfaces onto a scene.
type Server struct {
	ln          *net.UnixListener
	DisplayName string
	SocketPath  string
	Scene       *engine.Scene
	ScreenW     int
	ScreenH     int
	Import      DMABufImport
	mu          sync.Mutex
	clients     []*Client
	log         *log.Logger
}

// Listen opens $XDG_RUNTIME_DIR/<name>. Empty name → first free wayland-N (from 1).
func Listen(name string, scene *engine.Scene, screenW, screenH int, imp DMABufImport) (*Server, error) {
	dir, err := runtimeDir()
	if err != nil {
		return nil, err
	}
	if name == "" {
		for i := 1; i < 32; i++ {
			cand := fmt.Sprintf("wayland-%d", i)
			p := filepath.Join(dir, cand)
			if _, err := os.Stat(p); os.IsNotExist(err) {
				name = cand
				break
			}
		}
		if name == "" {
			return nil, fmt.Errorf("no free WAYLAND_DISPLAY slot")
		}
	}
	path := filepath.Join(dir, name)
	_ = os.Remove(path)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0700); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, err
	}
	s := &Server{
		ln:          ln,
		DisplayName: name,
		SocketPath:  path,
		Scene:       scene,
		ScreenW:     screenW,
		ScreenH:     screenH,
		Import:      imp,
		log:         log.New(os.Stderr, "wlsrv: ", 0),
	}
	go s.acceptLoop()
	return s, nil
}

func runtimeDir() (string, error) {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		if err := os.MkdirAll(d, 0700); err != nil {
			return "", err
		}
		return d, nil
	}
	d := filepath.Join(os.TempDir(), fmt.Sprintf("worldr-%d", os.Getuid()))
	if err := os.MkdirAll(d, 0700); err != nil {
		return "", err
	}
	_ = os.Setenv("XDG_RUNTIME_DIR", d)
	return d, nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.AcceptUnix()
		if err != nil {
			return
		}
		c := newClient(s, conn)
		s.mu.Lock()
		s.clients = append(s.clients, c)
		s.mu.Unlock()
		s.log.Printf("client connected")
	}
}

// Close stops listening and disconnects clients.
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
	_ = os.Remove(s.SocketPath)
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.close()
	}
}

// Dispatch reads pending requests from all clients.
func (s *Server) Dispatch() {
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.dispatchAvailable()
	}
}

func (s *Server) dropClient(c *Client) {
	s.mu.Lock()
	dst := s.clients[:0]
	for _, x := range s.clients {
		if x != c {
			dst = append(dst, x)
		}
	}
	s.clients = dst
	s.mu.Unlock()
	s.log.Printf("client disconnected")
}

// PointerButton delivers a pointer click in screen space (Phase 3).
func (s *Server) PointerButton(sx, sy int, pressed bool) {
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.pointerButton(sx, sy, pressed)
	}
}

// PointerMotion delivers pointer motion in screen space.
func (s *Server) PointerMotion(sx, sy int) {
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.pointerMotion(sx, sy)
	}
}

// KeyboardKey delivers an evdev key to every client (focused surface filters inside).
func (s *Server) KeyboardKey(code uint32, pressed bool) {
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.KeyboardKey(code, pressed)
	}
}
