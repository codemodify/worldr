// Package sdkhost adapts the public native-app v1 process contract to the
// internal application-surface contract. Protocol I/O is isolated on one
// worker so a slow application cannot block display presentation.
package sdkhost

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

const (
	startupTimeout  = 3 * time.Second
	shutdownTimeout = 500 * time.Millisecond
	requestCapacity = 256
)

type textureResource struct {
	texture       *render.Texture
	revision      uint64
	width, height int
}

type queuedRequest struct{ request nativeapp.Request }
type requestResult struct {
	request  nativeapp.Request
	response nativeapp.Response
	err      error
}

// Provider owns one explicitly launched SDK process. Methods belong to the
// Worldr host goroutine; only framed pipe I/O and process wait run off-thread.
type Provider struct {
	manifest nativeapp.Manifest
	// keyNamespace is manifest.ID for the first process and gains a stable
	// per-manifest instance component for repeated launches.
	keyNamespace string
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	codec        *nativeapp.Codec
	requests     chan queuedRequest
	results      chan requestResult
	wait         chan error
	done         chan struct{}
	log          io.Writer

	validator   nativeapp.Validator
	textures    map[nativeapp.ResourceID]textureResource
	meshes      map[nativeapp.ResourceID]*scene.Mesh
	surfaces    []experience.ApplicationSurface
	semantics   map[uint64]nativeui.SemanticTree
	textInput   map[uint64]experience.TextInputState
	surfaceKeys map[uint64]string
	retired     []uint64
	geometry    []uint64

	sequence   uint64
	tickQueued bool
	lastTick   time.Time
	focused    uint64
	closed     bool
	failed     bool
	err        error
	closeErr   error
	closeOnce  sync.Once
}

var _ experience.Applications = (*Provider)(nil)
var _ experience.ApplicationPoller = (*Provider)(nil)
var _ experience.ApplicationProviderCloser = (*Provider)(nil)
var _ experience.ApplicationTextureRetirer = (*Provider)(nil)
var _ experience.ApplicationGeometryRetirer = (*Provider)(nil)
var _ experience.ApplicationTextInput = (*Provider)(nil)
var _ experience.ApplicationCloser = (*Provider)(nil)

// Open starts command directly, without a shell, and negotiates protocol v1.
// args are copied. stdout is reserved for protocol traffic; stderr goes to log.
func Open(parent context.Context, command string, args []string, log io.Writer) (*Provider, error) {
	path, err := exec.LookPath(command)
	if err != nil {
		return nil, fmt.Errorf("find native app %q: %w", command, err)
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, path, append([]string(nil), args...)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		return nil, err
	}
	if log == nil {
		log = io.Discard
	}
	cmd.Stderr = log
	cmd.Env = nativeEnvironment(os.Environ())
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		cancel()
		return nil, fmt.Errorf("start native app %q: %w", command, err)
	}
	p := &Provider{
		cmd: cmd, cancel: cancel, stdin: stdin, stdout: stdout,
		codec: nativeapp.NewCodec(stdout, stdin), requests: make(chan queuedRequest, requestCapacity),
		results: make(chan requestResult, requestCapacity), wait: make(chan error, 1), done: make(chan struct{}),
		log:      log,
		textures: make(map[nativeapp.ResourceID]textureResource), meshes: make(map[nativeapp.ResourceID]*scene.Mesh),
		semantics: make(map[uint64]nativeui.SemanticTree), textInput: make(map[uint64]experience.TextInputState), surfaceKeys: make(map[uint64]string),
		lastTick: time.Now(),
	}
	go func() { p.wait <- cmd.Wait() }()
	hello := nativeapp.Request{
		Version: nativeapp.Version, Sequence: p.nextSequence(), Kind: nativeapp.RequestHello,
		Host: nativeapp.Host{Version: nativeapp.Version, MaxSurfaceWidth: nativeapp.MaxTextureWidth, MaxSurfaceHeight: nativeapp.MaxTextureHeight, MaxSurfaces: nativeapp.MaxSurfaces},
	}
	type handshake struct {
		response nativeapp.Response
		err      error
	}
	handshakeDone := make(chan handshake, 1)
	go func() {
		if err := p.codec.Write(hello); err != nil {
			handshakeDone <- handshake{err: err}
			return
		}
		var response nativeapp.Response
		err := p.codec.Read(&response)
		handshakeDone <- handshake{response, err}
	}()
	select {
	case result := <-handshakeDone:
		if result.err != nil {
			p.abort()
			return nil, fmt.Errorf("native app handshake: %w", result.err)
		}
		if err := p.acceptEnvelope(hello, result.response); err != nil {
			p.abort()
			return nil, err
		}
		if result.response.Manifest == nil {
			p.abort()
			return nil, fmt.Errorf("native app handshake omitted its manifest")
		}
		if err := result.response.Manifest.Validate(); err != nil {
			p.abort()
			return nil, err
		}
		p.manifest = *result.response.Manifest
		p.keyNamespace = p.manifest.ID
		if result.response.Snapshot == nil {
			p.abort()
			return nil, fmt.Errorf("native app handshake omitted its initial snapshot")
		}
		if err := p.apply(*result.response.Snapshot); err != nil {
			p.abort()
			return nil, fmt.Errorf("native app initial snapshot: %w", err)
		}
	case <-time.After(startupTimeout):
		p.abort()
		return nil, fmt.Errorf("native app %q did not negotiate within %s", command, startupTimeout)
	case <-parent.Done():
		p.abort()
		return nil, parent.Err()
	}
	go p.serveTransport(ctx)
	return p, nil
}

// ApplicationID is the stable manifest identity advertised by the child.
func (p *Provider) ApplicationID() string { return p.manifest.ID }

// SetInstanceOrdinal gives repeated processes with one manifest identity
// distinct workspace keys. It is called by the host before registration; the
// first instance keeps the original v1 key shape for saved-layout compatibility.
func (p *Provider) SetInstanceOrdinal(ordinal int) error {
	if ordinal < 1 || ordinal > 4 {
		return fmt.Errorf("native app instance ordinal must be 1..4")
	}
	old := p.keyNamespace
	if old == "" {
		old = p.manifest.ID
	}
	next := p.manifest.ID
	if ordinal > 1 {
		next += "/instance-" + strconv.Itoa(ordinal)
	}
	if old == next {
		return nil
	}
	oldPrefix := "sdk:" + old + "/"
	for index := range p.surfaces {
		local := p.surfaceKeys[p.surfaces[index].ID]
		if local == "" {
			local = strings.TrimPrefix(p.surfaces[index].Key, oldPrefix)
		}
		if local == "" || local == p.surfaces[index].Key {
			return fmt.Errorf("native app surface key is outside its manifest namespace")
		}
		p.surfaces[index].Key = sdkSurfaceKey(next, local)
	}
	p.keyNamespace = next
	return nil
}

func nativeEnvironment(parent []string) []string {
	result := make([]string, 0, len(parent)+2)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if name != "WORLDR_NATIVE_APP" && name != "WORLDR_NATIVE_APP_VERSION" {
			result = append(result, entry)
		}
	}
	return append(result, "WORLDR_NATIVE_APP=stdio", fmt.Sprintf("WORLDR_NATIVE_APP_VERSION=%d", nativeapp.Version))
}

func (p *Provider) nextSequence() uint64 {
	p.sequence++
	return p.sequence
}

func (p *Provider) serveTransport(ctx context.Context) {
	defer close(p.done)
	for {
		select {
		case <-ctx.Done():
			return
		case queued := <-p.requests:
			result := requestResult{request: queued.request}
			if result.err = p.codec.Write(queued.request); result.err == nil {
				result.err = p.codec.Read(&result.response)
			}
			select {
			case p.results <- result:
			case <-ctx.Done():
				return
			}
			if queued.request.Kind == nativeapp.RequestShutdown || result.err != nil {
				return
			}
		}
	}
}

func (p *Provider) acceptEnvelope(request nativeapp.Request, response nativeapp.Response) error {
	if response.Version != nativeapp.Version || response.Sequence != request.Sequence {
		return fmt.Errorf("native app returned mismatched version or sequence")
	}
	if response.Error != "" {
		return fmt.Errorf("native app: %s", response.Error)
	}
	return nil
}

func (p *Provider) enqueue(kind nativeapp.RequestKind, surface uint64, event *nativeapp.Event, width, height int) {
	if p.closed || p.err != nil {
		return
	}
	request := nativeapp.Request{Version: nativeapp.Version, Sequence: p.nextSequence(), Kind: kind, Surface: nativeapp.SurfaceID(surface), Event: event, Width: width, Height: height}
	select {
	case p.requests <- queuedRequest{request}:
	default:
		if event != nil && event.Kind == nativeapp.PointerMove {
			return
		}
		p.err = fmt.Errorf("native app %q input queue exceeded %d requests", p.manifest.ID, requestCapacity)
	}
}

func (p *Provider) Poll() error {
	if p.closed {
		return p.closeErr
	}
	for {
		select {
		case result := <-p.results:
			if result.request.Kind == nativeapp.RequestUpdate {
				p.tickQueued = false
			}
			if result.err != nil {
				p.err = fmt.Errorf("native app %q transport: %w", p.manifest.ID, result.err)
				continue
			}
			if err := p.acceptEnvelope(result.request, result.response); err != nil {
				p.err = err
				continue
			}
			if result.response.Snapshot == nil && result.request.Kind != nativeapp.RequestShutdown {
				p.err = fmt.Errorf("native app %q omitted a snapshot", p.manifest.ID)
				continue
			}
			if result.response.Snapshot != nil {
				if err := p.apply(*result.response.Snapshot); err != nil {
					p.err = fmt.Errorf("native app %q snapshot: %w", p.manifest.ID, err)
				}
			}
		default:
			goto drained
		}
	}
drained:
	select {
	case waitErr := <-p.wait:
		p.wait = nil
		if p.err == nil {
			if waitErr == nil {
				p.err = fmt.Errorf("native app %q exited", p.manifest.ID)
			} else {
				p.err = fmt.Errorf("native app %q exited: %w", p.manifest.ID, waitErr)
			}
		}
	default:
	}
	if p.err != nil {
		p.fail(p.err)
		return nil
	}
	if !p.tickQueued {
		now := time.Now()
		delta := now.Sub(p.lastTick)
		if delta < 0 {
			delta = 0
		}
		if delta > 10*time.Second {
			delta = 10 * time.Second
		}
		p.lastTick = now
		request := nativeapp.Request{Version: nativeapp.Version, Sequence: p.nextSequence(), Kind: nativeapp.RequestUpdate, DeltaNanos: int64(delta)}
		select {
		case p.requests <- queuedRequest{request}:
			p.tickQueued = true
		default:
			p.err = fmt.Errorf("native app %q update queue exceeded %d requests", p.manifest.ID, requestCapacity)
		}
	}
	if p.err != nil {
		p.fail(p.err)
	}
	return nil
}

func (p *Provider) fail(err error) {
	if p.failed || err == nil {
		return
	}
	p.failed = true
	p.focused = 0
	p.cancel()
	_ = p.stdin.Close()
	_ = p.stdout.Close()
	p.retireAll()
	fmt.Fprintf(p.log, "native app %s stopped: %v; workspace remains open\n", p.manifest.ID, err)
}

func (p *Provider) Surfaces() []experience.ApplicationSurface { return p.surfaces }

func (p *Provider) Focus(id uint64) {
	p.focused = id
	p.enqueue(nativeapp.RequestFocus, id, nil, 0, 0)
}

func (p *Provider) Send(id uint64, event experience.Event) {
	converted := eventToSDK(event)
	p.enqueue(nativeapp.RequestInput, id, &converted, 0, 0)
}

// Seat forwards only connection-wide keyboard state without granting focus.
// Pointer, key and text events need a live surface and arrive through Send.
func (p *Provider) Seat(event experience.Event) {
	switch event.Kind {
	case experience.KeymapChanged, experience.KeyboardModifiers, experience.KeyboardRepeatInfo, experience.KeyboardCancel:
	default:
		return
	}
	converted := eventToSDK(event)
	p.enqueue(nativeapp.RequestInput, 0, &converted, 0, 0)
}

func (p *Provider) Resize(id uint64, width, height int) {
	width = max(1, min(nativeapp.MaxTextureWidth, width))
	height = max(1, min(nativeapp.MaxTextureHeight, height))
	p.enqueue(nativeapp.RequestResize, id, nil, width, height)
}

func (p *Provider) CloseApplication(id uint64) {
	p.enqueue(nativeapp.RequestCloseSurface, id, nil, 0, 0)
}

func (p *Provider) TextInput(id uint64) experience.TextInputState {
	if p.closed || p.focused != id {
		return experience.TextInputState{}
	}
	return p.textInput[id]
}

// Semantics returns an owned copy for a future OS accessibility adapter.
func (p *Provider) Semantics(id uint64) nativeui.SemanticTree {
	tree := p.semantics[id]
	tree.Nodes = append([]nativeui.Node(nil), tree.Nodes...)
	return tree
}

func (p *Provider) RetiredTextures() []uint64 {
	result := p.retired
	p.retired = nil
	return result
}

func (p *Provider) RetiredGeometryIDs() []uint64 {
	result := p.geometry
	p.geometry = nil
	return result
}

func (p *Provider) Close() error {
	p.closeOnce.Do(func() {
		p.closed = true
		shutdown := nativeapp.Request{Version: nativeapp.Version, Sequence: p.nextSequence(), Kind: nativeapp.RequestShutdown}
		sent := false
		if p.err == nil {
			select {
			case p.requests <- queuedRequest{shutdown}:
				sent = true
			default:
			}
		}
		if sent {
			timer := time.NewTimer(shutdownTimeout)
			waiting := true
			for waiting {
				select {
				case result := <-p.results:
					if result.request.Sequence == shutdown.Sequence {
						if result.err != nil {
							p.closeErr = result.err
						} else if err := p.acceptEnvelope(result.request, result.response); err != nil {
							p.closeErr = err
						}
						waiting = false
					}
				case <-timer.C:
					waiting = false
				}
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		p.cancel()
		_ = p.stdin.Close()
		_ = p.stdout.Close()
		select {
		case <-p.done:
		case <-time.After(shutdownTimeout):
		}
		if p.wait != nil {
			select {
			case <-p.wait:
				p.wait = nil
			case <-time.After(shutdownTimeout):
				p.closeErr = errors.Join(p.closeErr, fmt.Errorf("native app %q did not exit", p.manifest.ID))
			}
		}
		p.retireAll()
	})
	return p.closeErr
}

func (p *Provider) abort() {
	p.cancel()
	_ = p.stdin.Close()
	_ = p.stdout.Close()
	select {
	case <-p.wait:
	case <-time.After(shutdownTimeout):
	}
}

func (p *Provider) retireAll() {
	p.surfaces = nil
	clear(p.semantics)
	clear(p.textInput)
	clear(p.surfaceKeys)
	for id, resource := range p.textures {
		p.retired = append(p.retired, resource.texture.ID())
		delete(p.textures, id)
	}
	for id, mesh := range p.meshes {
		p.geometry = append(p.geometry, mesh.Geometry().ID())
		delete(p.meshes, id)
	}
}

func (p *Provider) apply(snapshot nativeapp.Snapshot) error {
	if err := p.validator.Validate(snapshot); err != nil {
		return err
	}
	for _, update := range snapshot.Textures {
		resource, exists := p.textures[update.ID]
		if !exists {
			texture, err := render.NewTexture(update.Width, update.Height, update.Pixels)
			if err != nil {
				return err
			}
			p.textures[update.ID] = textureResource{texture, update.Revision, update.Width, update.Height}
			continue
		}
		var err error
		if resource.width != update.Width || resource.height != update.Height {
			err = resource.texture.Replace(update.Width, update.Height, update.Pixels)
		} else {
			rect := image.Rect(update.Rect.X, update.Rect.Y, update.Rect.X+update.Rect.Width, update.Rect.Y+update.Rect.Height)
			err = resource.texture.Update(rect, update.Pixels)
		}
		if err != nil {
			return err
		}
		resource.revision, resource.width, resource.height = update.Revision, update.Width, update.Height
		p.textures[update.ID] = resource
	}
	for _, resource := range snapshot.Meshes {
		mesh, err := meshFromSDK(resource)
		if err != nil {
			return fmt.Errorf("mesh %d: %w", resource.ID, err)
		}
		p.meshes[resource.ID] = mesh
	}
	surfaces := make([]experience.ApplicationSurface, 0, len(snapshot.Surfaces))
	semantics := make(map[uint64]nativeui.SemanticTree, len(snapshot.Surfaces))
	textInput := make(map[uint64]experience.TextInputState, len(snapshot.Surfaces))
	surfaceKeys := make(map[uint64]string, len(snapshot.Surfaces))
	for _, surface := range snapshot.Surfaces {
		converted, tree, input, err := p.surfaceFromSDK(surface)
		if err != nil {
			return err
		}
		surfaces = append(surfaces, converted)
		semantics[converted.ID], textInput[converted.ID] = tree, input
		surfaceKeys[converted.ID] = surface.Key
	}
	p.surfaces, p.semantics, p.textInput, p.surfaceKeys = surfaces, semantics, textInput, surfaceKeys
	for _, id := range snapshot.RetireTextures {
		resource := p.textures[id]
		p.retired = append(p.retired, resource.texture.ID())
		delete(p.textures, id)
	}
	for _, id := range snapshot.RetireMeshes {
		mesh := p.meshes[id]
		p.geometry = append(p.geometry, mesh.Geometry().ID())
		delete(p.meshes, id)
	}
	return nil
}
