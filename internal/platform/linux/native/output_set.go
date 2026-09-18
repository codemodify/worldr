package native

import (
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/codemodify/worldr/internal/render"
)

const MaxOutputs = 8

// Output transfers a rendering session and its desktop rectangle to an
// OutputSet. Each live session must have a distinct identity and matching pixel
// extent. Rectangles share one top-left desktop coordinate space.
type Output struct {
	ID     uint32
	Bounds image.Rectangle
	VK     *VK
}

type OutputInfo struct {
	ID     uint32
	Bounds image.Rectangle
}

// OutputError identifies the connector whose independent rendering session
// failed. It preserves the native cause for errors.Is and lets the session host
// rebuild only that output instead of discarding healthy device residency on
// every monitor.
type OutputError struct {
	OutputID uint32
	Err      error
}

func (e *OutputError) Error() string {
	if e == nil {
		return "output failed"
	}
	return fmt.Sprintf("output %d: %v", e.OutputID, e.Err)
}

func (e *OutputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func outputError(id uint32, err error) error {
	if err == nil {
		return nil
	}
	return &OutputError{OutputID: id, Err: err}
}

type outputTarget interface {
	Size() (uint32, uint32)
	RenderFrame(render.Frame, [4]float32, []byte) error
	SetSceneAtlas(render.Atlas) error
	ReleaseTexture(uint64) error
	ReleaseGeometry(uint64) error
	SetMemoryBudget(uint64) error
	MemoryStats() MemoryStats
	Recover() error
	Close()
}

type outputState struct {
	OutputInfo
	target outputTarget
	vk     *VK
	frame  render.Frame
}

// OutputSet presents one extended desktop through up to eight independent
// Vulkan sessions. Retained CPU/external image and geometry sources are shared;
// device residency is independent. Calls belong to one presentation goroutine.
// It does not own the source textures or the DRM/libseat device supplying them.
// Presentation is sequential and is not synchronized across monitor refreshes.
type OutputSet struct {
	outputs       []outputState
	width, height int
	budget, peak  uint64
	closed        bool
}

// NewOutputSet takes ownership of every VK only on success. Output IDs and
// sessions must be unique; rectangles cannot overlap and must fit the existing
// 7680x4320 desktop bound. Gaps are allowed. Zero budget selects 1 GiB total.
func NewOutputSet(outputs []Output, budget uint64) (*OutputSet, error) {
	states := make([]outputState, len(outputs))
	seen := make(map[*VK]bool, len(outputs))
	for i, out := range outputs {
		if out.VK == nil || seen[out.VK] {
			return nil, fmt.Errorf("output %d has an absent or duplicated Vulkan session", out.ID)
		}
		seen[out.VK] = true
		states[i] = outputState{OutputInfo: OutputInfo{ID: out.ID, Bounds: out.Bounds}, target: out.VK, vk: out.VK}
	}
	return newOutputSet(states, budget)
}

func newOutputSet(states []outputState, budget uint64) (*OutputSet, error) {
	if len(states) == 0 || len(states) > MaxOutputs {
		return nil, fmt.Errorf("output count must be between 1 and %d", MaxOutputs)
	}
	seen := make(map[uint32]bool, len(states))
	var width, height int
	for i, out := range states {
		if out.ID == 0 || seen[out.ID] || out.target == nil {
			return nil, fmt.Errorf("output IDs must be nonzero and unique")
		}
		seen[out.ID] = true
		r := out.Bounds
		if r.Empty() || r.Min.X < 0 || r.Min.Y < 0 || r.Max.X > 7680 || r.Max.Y > 4320 {
			return nil, fmt.Errorf("output %d is outside the 7680x4320 desktop", out.ID)
		}
		w, h := out.target.Size()
		if int64(w) != int64(r.Dx()) || int64(h) != int64(r.Dy()) {
			return nil, fmt.Errorf("output %d rectangle does not match its Vulkan extent", out.ID)
		}
		for _, earlier := range states[:i] {
			if r.Overlaps(earlier.Bounds) {
				return nil, fmt.Errorf("outputs %d and %d overlap", earlier.ID, out.ID)
			}
		}
		width, height = max(width, r.Max.X), max(height, r.Max.Y)
	}
	s := &OutputSet{outputs: states, width: width, height: height}
	if err := s.SetMemoryBudget(budget); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *OutputSet) valid() error {
	if s == nil || s.closed {
		return fmt.Errorf("output set is closed")
	}
	return nil
}
func (s *OutputSet) Primary() *VK {
	if s == nil || s.closed || len(s.outputs) == 0 {
		return nil
	}
	return s.outputs[0].vk
}
func (s *OutputSet) Size() (int, int) {
	if s == nil || s.closed {
		return 0, 0
	}
	return s.width, s.height
}
func (s *OutputSet) Outputs() []OutputInfo {
	if s == nil || s.closed {
		return nil
	}
	result := make([]OutputInfo, len(s.outputs))
	for i, out := range s.outputs {
		result[i] = out.OutputInfo
	}
	return result
}
func outputCoordinate(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

// LocalToGlobal maps framebuffer input from one output to the shared desktop.
// Coordinates outside the output are rejected; captured relative pointer
// movement should be accumulated globally by the seat owner instead.
func (s *OutputSet) LocalToGlobal(id uint32, x, y float32) (float32, float32, bool) {
	if s == nil || s.closed || !outputCoordinate(x) || !outputCoordinate(y) {
		return 0, 0, false
	}
	for _, out := range s.outputs {
		if out.ID == id && x >= 0 && y >= 0 && x < float32(out.Bounds.Dx()) && y < float32(out.Bounds.Dy()) {
			return x + float32(out.Bounds.Min.X), y + float32(out.Bounds.Min.Y), true
		}
	}
	return 0, 0, false
}

// OutputAt returns the output and local coordinates containing a global
// desktop point. Gaps and the far/right edges are not assigned to an output.
func (s *OutputSet) OutputAt(x, y float32) (uint32, float32, float32, bool) {
	if s == nil || s.closed || !outputCoordinate(x) || !outputCoordinate(y) {
		return 0, 0, 0, false
	}
	for _, out := range s.outputs {
		r := out.Bounds
		if x >= float32(r.Min.X) && y >= float32(r.Min.Y) && x < float32(r.Max.X) && y < float32(r.Max.Y) {
			return out.ID, x - float32(r.Min.X), y - float32(r.Min.Y), true
		}
	}
	return 0, 0, 0, false
}

func (s *OutputSet) current() MemoryStats {
	stats := MemoryStats{BudgetBytes: s.budget}
	var peakBound uint64
	for _, out := range s.outputs {
		v := out.target.MemoryStats()
		stats.AllocatedBytes += v.AllocatedBytes
		peakBound += v.PeakBytes
		stats.Images += v.Images
		stats.Buffers += v.Buffers
	}
	// Per-device counters include transient staging allocations. Their sum is
	// a conservative aggregate peak bound, capped by the enforced total budget.
	if s.budget != 0 {
		peakBound = min(peakBound, s.budget)
	}
	s.peak = max(s.peak, stats.AllocatedBytes, peakBound)
	stats.PeakBytes = s.peak
	return stats
}
func (s *OutputSet) MemoryStats() MemoryStats {
	if s == nil || s.closed {
		return MemoryStats{}
	}
	return s.current()
}

// SetMemoryBudget caps the sum of actual owned Vulkan allocations. Each device
// may use the space remaining after the other devices' current allocations.
// Before each operation these reservations are refreshed; caller-owned direct
// uploads to Primary must also be serialized with this set's operations.
func (s *OutputSet) SetMemoryBudget(bytes uint64) error {
	if err := s.valid(); err != nil {
		return err
	}
	if bytes == 0 {
		bytes = DefaultMemoryBudget
	}
	before := s.current()
	if before.AllocatedBytes > bytes {
		return fmt.Errorf("aggregate output budget is below current use: %w", ErrOutOfMemory)
	}
	previous := make([]uint64, len(s.outputs))
	limits := make([]uint64, len(s.outputs))
	for i, out := range s.outputs {
		stats := out.target.MemoryStats()
		previous[i] = stats.BudgetBytes
		limit := bytes - (before.AllocatedBytes - stats.AllocatedBytes)
		if limit == 0 {
			return fmt.Errorf("output %d has no allocation budget: %w", out.ID, ErrOutOfMemory)
		}
		limits[i] = limit
	}
	for i, out := range s.outputs {
		if err := out.target.SetMemoryBudget(limits[i]); err != nil {
			var undo []error
			for j := 0; j < i; j++ {
				if e := s.outputs[j].target.SetMemoryBudget(previous[j]); e != nil {
					undo = append(undo, e)
				}
			}
			return errors.Join(fmt.Errorf("output %d budget: %w", out.ID, err), errors.Join(undo...))
		}
	}
	s.budget = bytes
	return nil
}

func (s *OutputSet) reserve(index int) error {
	stats := s.current()
	own := s.outputs[index].target.MemoryStats().AllocatedBytes
	if stats.AllocatedBytes > s.budget || s.budget-(stats.AllocatedBytes-own) == 0 {
		return fmt.Errorf("aggregate output allocation budget: %w", ErrOutOfMemory)
	}
	return s.outputs[index].target.SetMemoryBudget(s.budget - (stats.AllocatedBytes - own))
}
func (s *OutputSet) rebalance() error {
	var failures []error
	for i, out := range s.outputs {
		if err := s.reserve(i); err != nil {
			failures = append(failures, outputError(out.ID, fmt.Errorf("budget: %w", err)))
		}
	}
	return errors.Join(failures...)
}

// cropOutputFrame preserves every scene projection and model. Only the camera
// viewport and 2D pixel coordinates move into output-local space. Vulkan's
// scissor clips each unchanged global projection to its physical framebuffer.
// Borrowed retained draws/textures are never copied or modified.
func cropOutputFrame(dst *render.Frame, src render.Frame, origin image.Point) {
	dst.LinearColor = src.LinearColor
	dst.Output = src.Output
	dst.Vertices = append(dst.Vertices[:0], src.Vertices...)
	for i := range dst.Vertices {
		dst.Vertices[i].X -= float32(origin.X)
		dst.Vertices[i].Y -= float32(origin.Y)
	}
	clear(dst.Commands)
	dst.Commands = append(dst.Commands[:0], src.Commands...)
	for i := range dst.Commands {
		command := &dst.Commands[i]
		switch command.Kind {
		case render.SceneCommand:
			command.View.Viewport[0] -= float32(origin.X)
			command.View.Viewport[1] -= float32(origin.Y)
		case render.ImageCommand:
			command.Image.Bounds[0] -= float32(origin.X)
			command.Image.Bounds[1] -= float32(origin.Y)
		}
	}
}

// RenderFrame crops one shared frame to every output. Render errors retain the
// connector ID and unwrap the native cause. A frame can already be presented
// on earlier outputs when a later output fails; the host decides recovery.
func (s *OutputSet) RenderFrame(frame render.Frame, clearColor [4]float32) (result error) {
	if err := s.valid(); err != nil {
		return err
	}
	defer func() { result = errors.Join(result, s.rebalance()) }()
	for i := range s.outputs {
		out := &s.outputs[i]
		if err := s.reserve(i); err != nil {
			return outputError(out.ID, err)
		}
		cropOutputFrame(&out.frame, frame, out.Bounds.Min)
		if err := out.target.RenderFrame(out.frame, clearColor, nil); err != nil {
			return outputError(out.ID, err)
		}
		s.current()
	}
	return nil
}

func (s *OutputSet) all(action func(outputTarget) error, allocate bool) (result error) {
	if err := s.valid(); err != nil {
		return err
	}
	defer func() { result = errors.Join(result, s.rebalance()) }()
	var failures []error
	for i, out := range s.outputs {
		if allocate {
			if err := s.reserve(i); err != nil {
				failures = append(failures, outputError(out.ID, err))
				continue
			}
		}
		if err := action(out.target); err != nil {
			failures = append(failures, outputError(out.ID, err))
		}
		s.current()
	}
	return errors.Join(failures...)
}

// SetSceneAtlas uploads identical immutable coverage on every output. A failed
// device may retain its old atlas; report the error and recover/retry before
// presenting a new frame. Recovery uses each VK's retained successful atlas.
func (s *OutputSet) SetSceneAtlas(atlas render.Atlas) error {
	return s.all(func(v outputTarget) error { return v.SetSceneAtlas(atlas) }, true)
}
func (s *OutputSet) ReleaseTexture(id uint64) error {
	return s.all(func(v outputTarget) error { return v.ReleaseTexture(id) }, false)
}
func (s *OutputSet) ReleaseGeometry(id uint64) error {
	return s.all(func(v outputTarget) error { return v.ReleaseGeometry(id) }, false)
}

// Recover recreates every logical device while providers keep their retained
// CPU/external resources. The host bounds retries and coordinates seat state.
func (s *OutputSet) Recover() error {
	return s.all(func(v outputTarget) error { return v.Recover() }, true)
}

// RecoverAffected recreates only the output rendering sessions identified by
// recoverable branches of cause. Errors without OutputError context predate or
// bypass OutputSet, so they conservatively rebuild every output. The host
// remains responsible for bounding attempts and preserving document/input
// state around this call.
func (s *OutputSet) RecoverAffected(cause error) (result error) {
	if err := s.valid(); err != nil {
		return err
	}
	ids := make(map[uint32]struct{})
	if collectAffectedOutputs(cause, ids) {
		return s.Recover()
	}
	if len(ids) == 0 {
		return fmt.Errorf("output recovery requires a device failure: %w", cause)
	}
	known := make(map[uint32]bool, len(s.outputs))
	for _, out := range s.outputs {
		known[out.ID] = true
	}
	for id := range ids {
		if !known[id] {
			return fmt.Errorf("failed output %d is not in the active topology", id)
		}
	}
	defer func() { result = errors.Join(result, s.rebalance()) }()
	var failures []error
	for index := range s.outputs {
		out := &s.outputs[index]
		if _, selected := ids[out.ID]; !selected {
			continue
		}
		if err := s.reserve(index); err != nil {
			failures = append(failures, outputError(out.ID, err))
			continue
		}
		if err := out.target.Recover(); err != nil {
			failures = append(failures, outputError(out.ID, err))
		}
		s.current()
	}
	return errors.Join(failures...)
}

func collectAffectedOutputs(err error, ids map[uint32]struct{}) (unscoped bool) {
	if err == nil {
		return false
	}
	if output, ok := err.(*OutputError); ok {
		if recoverableDeviceError(output.Err) {
			ids[output.OutputID] = struct{}{}
		}
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			unscoped = collectAffectedOutputs(child, ids) || unscoped
		}
		return unscoped
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return collectAffectedOutputs(wrapped.Unwrap(), ids)
	}
	return recoverableDeviceError(err)
}

func recoverableDeviceError(err error) bool {
	return errors.Is(err, ErrDeviceLost) || errors.Is(err, ErrNeedsRecovery) || errors.Is(err, ErrGPUTimeout)
}
func (s *OutputSet) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	for i := len(s.outputs) - 1; i >= 0; i-- {
		s.outputs[i].target.Close()
	}
	s.outputs = nil
}
