// Package accessibility exports bounded native control semantics to adapters.
// The transport is deliberately desktop-bus neutral: an AT-SPI adapter can
// consume the same versioned snapshots as test and remote-assistance tools.
package accessibility

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"
)

const (
	ProtocolVersion = 1
	maxApplications = 32
	maxNodes        = 1024
	maxClients      = 8
)

type Node struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Label       string `json:"label,omitempty"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
	X           int    `json:"x,omitempty"`
	Y           int    `json:"y,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Selected    bool   `json:"selected,omitempty"`
}

type Application struct {
	ID              uint64 `json:"id"`
	Key             string `json:"key,omitempty"`
	AppID           string `json:"app_id,omitempty"`
	Title           string `json:"title"`
	CoordinateSpace string `json:"coordinate_space"`
	Focused         bool   `json:"focused,omitempty"`
	NativeSemantics bool   `json:"native_semantics,omitempty"`
	FocusedNode     string `json:"focused_node,omitempty"`
	Nodes           []Node `json:"nodes,omitempty"`
}

type Snapshot struct {
	Version      int           `json:"version"`
	Serial       uint64        `json:"serial"`
	Applications []Application `json:"applications"`
}

func validateText(name, value string) error {
	if !utf8.ValidString(value) || len(value) > 16<<10 {
		return fmt.Errorf("%s is invalid UTF-8 or exceeds 16 KiB", name)
	}
	return nil
}

func (snapshot Snapshot) Validate() error {
	if snapshot.Version != 0 && snapshot.Version != ProtocolVersion {
		return fmt.Errorf("unsupported accessibility protocol version %d", snapshot.Version)
	}
	if len(snapshot.Applications) > maxApplications {
		return fmt.Errorf("accessibility snapshot exceeds %d applications", maxApplications)
	}
	applicationIDs := make(map[uint64]bool, len(snapshot.Applications))
	for _, application := range snapshot.Applications {
		if application.ID == 0 || applicationIDs[application.ID] {
			return fmt.Errorf("accessibility application ID is zero or duplicated")
		}
		applicationIDs[application.ID] = true
		for name, value := range map[string]string{"application key": application.Key, "application ID": application.AppID, "application title": application.Title} {
			if err := validateText(name, value); err != nil {
				return err
			}
		}
		if application.CoordinateSpace != "application-pixels" {
			return fmt.Errorf("unsupported accessibility coordinate space %q", application.CoordinateSpace)
		}
		if len(application.Nodes) > maxNodes {
			return fmt.Errorf("accessibility application exceeds %d nodes", maxNodes)
		}
		nodes := make(map[string]bool, len(application.Nodes))
		for _, node := range application.Nodes {
			if node.ID == "" || nodes[node.ID] {
				return fmt.Errorf("accessibility node ID is empty or duplicated")
			}
			nodes[node.ID] = true
			if !ValidRole(node.Role) {
				return fmt.Errorf("accessibility node %q has unsupported role %q", node.ID, node.Role)
			}
			if node.Width < 0 || node.Height < 0 {
				return fmt.Errorf("accessibility node %q has negative bounds", node.ID)
			}
			for name, value := range map[string]string{"node ID": node.ID, "node role": node.Role, "node label": node.Label, "node value": node.Value, "node description": node.Description} {
				if err := validateText(name, value); err != nil {
					return err
				}
			}
		}
		if application.FocusedNode != "" && !nodes[application.FocusedNode] {
			return fmt.Errorf("focused accessibility node %q is absent", application.FocusedNode)
		}
	}
	return nil
}

type subscriber struct {
	updates    chan Snapshot
	connection *net.UnixConn
}

// Server writes one JSON snapshot per line. Each slow client retains only the
// latest update, so assistive adapters cannot stall rendering or grow memory.
type Server struct {
	mu          sync.Mutex
	listener    *net.UnixListener
	path        string
	done        chan struct{}
	subscribers map[*subscriber]bool
	latest      Snapshot
	closed      bool
	wait        sync.WaitGroup
}

func Listen(path string) (*Server, error) {
	if path == "" || !filepath.IsAbs(path) || len(path) > 96 {
		return nil, fmt.Errorf("accessibility socket needs an absolute path of at most 96 bytes")
	}
	directory := filepath.Dir(path)
	info, err := os.Stat(directory)
	if err != nil {
		return nil, fmt.Errorf("accessibility socket directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("accessibility socket directory must not be writable by group or others")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return nil, fmt.Errorf("accessibility socket directory is owned by uid %d", stat.Uid)
	}
	address := &net.UnixAddr{Name: path, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, fmt.Errorf("accessibility socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		os.Remove(path)
		return nil, err
	}
	server := &Server{listener: listener, path: path, done: make(chan struct{}), subscribers: make(map[*subscriber]bool)}
	server.wait.Add(1)
	go server.accept()
	runtime.SetFinalizer(server, func(server *Server) { _ = server.Close() })
	return server, nil
}

func (server *Server) accept() {
	defer server.wait.Done()
	for {
		connection, err := server.listener.AcceptUnix()
		if err != nil {
			select {
			case <-server.done:
				return
			default:
				continue
			}
		}
		server.mu.Lock()
		if server.closed || len(server.subscribers) >= maxClients {
			server.mu.Unlock()
			connection.Close()
			continue
		}
		subscription := &subscriber{updates: make(chan Snapshot, 1), connection: connection}
		server.subscribers[subscription] = true
		if server.latest.Serial != 0 {
			subscription.updates <- server.latest
		}
		server.wait.Add(1)
		server.mu.Unlock()
		go server.write(connection, subscription)
	}
}

func (server *Server) write(connection *net.UnixConn, subscription *subscriber) {
	defer server.wait.Done()
	defer connection.Close()
	defer func() {
		server.mu.Lock()
		delete(server.subscribers, subscription)
		server.mu.Unlock()
	}()
	encoder := json.NewEncoder(connection)
	for {
		select {
		case snapshot := <-subscription.updates:
			if err := encoder.Encode(snapshot); err != nil {
				return
			}
		case <-server.done:
			return
		}
	}
}

func (server *Server) Publish(snapshot Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	snapshot.Version = ProtocolVersion
	snapshot = clone(snapshot)
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closed {
		return net.ErrClosed
	}
	comparison := snapshot
	comparison.Serial = server.latest.Serial
	if reflect.DeepEqual(comparison, server.latest) {
		return nil
	}
	snapshot.Serial = server.latest.Serial + 1
	if snapshot.Serial == 0 {
		return errors.New("accessibility serial exhausted")
	}
	server.latest = snapshot
	for subscription := range server.subscribers {
		select {
		case subscription.updates <- snapshot:
		default:
			select {
			case <-subscription.updates:
			default:
			}
			subscription.updates <- snapshot
		}
	}
	return nil
}

func (server *Server) Close() error {
	if server == nil {
		return nil
	}
	server.mu.Lock()
	if server.closed {
		server.mu.Unlock()
		return nil
	}
	server.closed = true
	close(server.done)
	err := server.listener.Close()
	for subscription := range server.subscribers {
		_ = subscription.connection.Close()
	}
	server.mu.Unlock()
	server.wait.Wait()
	runtime.SetFinalizer(server, nil)
	if removeErr := os.Remove(server.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		err = errors.Join(err, removeErr)
	}
	return err
}

func clone(snapshot Snapshot) Snapshot {
	result := snapshot
	result.Applications = append([]Application(nil), snapshot.Applications...)
	for index := range result.Applications {
		result.Applications[index].Nodes = append([]Node(nil), snapshot.Applications[index].Nodes...)
	}
	return result
}

// Role is intentionally a small string vocabulary shared with native UI.
func ValidRole(role string) bool {
	switch strings.ToLower(role) {
	case "button", "textbox", "menuitem", "label", "slider", "image", "document", "status":
		return true
	default:
		return false
	}
}
