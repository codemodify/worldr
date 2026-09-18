package app

import (
	"encoding/json"
	"fmt"
	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Fixed 0.1 ms bins cover the complete run without retaining per-frame samples.
// Durations above one second share an overflow bin reported as the actual max.
type durationMetric struct {
	bins       [10001]uint64
	count      uint64
	total, max time.Duration
}

func (m *durationMetric) add(d time.Duration) {
	if d < 0 {
		return
	}
	i := min(len(m.bins)-1, int(d/(100*time.Microsecond)))
	m.bins[i]++
	m.count++
	m.total += d
	if d > m.max {
		m.max = d
	}
}

type durationReport struct {
	Samples uint64  `json:"samples"`
	MeanMS  float64 `json:"mean_ms"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MaxMS   float64 `json:"max_ms"`
}

func (m *durationMetric) percentile(fraction float64) float64 {
	if m.count == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(m.count) * fraction))
	var count uint64
	for i, n := range m.bins {
		count += n
		if count >= target {
			if i == len(m.bins)-1 {
				return float64(m.max) / 1e6
			}
			return min(float64(m.max)/1e6, float64(i+1)/10)
		}
	}
	return 0
}
func (m *durationMetric) report() durationReport {
	r := durationReport{Samples: m.count}
	if m.count > 0 {
		r.MeanMS = float64(m.total) / float64(m.count) / 1e6
		r.P50MS = m.percentile(.5)
		r.P95MS = m.percentile(.95)
		r.P99MS = m.percentile(.99)
		r.MaxMS = float64(m.max) / 1e6
	}
	return r
}

type memorySource interface{ MemoryStats() native.MemoryStats }

type runMetrics struct {
	start, end, lastFrame, lastMemory, pendingInput time.Time
	inputEvents, recoveries, resizes                uint64
	work, render, interval, input                   durationMetric
	heapPeak, rssPeak                               uint64
	sharedPeak                                      uint64
	memory                                          native.MemoryStats
}

func (m *runMetrics) dispatch(now time.Time) {
	m.inputEvents++
	if m.pendingInput.IsZero() {
		m.pendingInput = now
	}
}
func (m *runMetrics) frame(begin, beforeRender, end time.Time, vk memorySource) {
	m.work.add(end.Sub(begin))
	m.render.add(end.Sub(beforeRender))
	if !m.lastFrame.IsZero() {
		m.interval.add(end.Sub(m.lastFrame))
	}
	m.lastFrame = end
	if !m.pendingInput.IsZero() {
		m.input.add(end.Sub(m.pendingInput))
		m.pendingInput = time.Time{}
	}
	if m.lastMemory.IsZero() || end.Sub(m.lastMemory) >= time.Second {
		m.sampleMemory(vk)
		m.lastMemory = end
	}
}
func (m *runMetrics) sampleMemory(vk memorySource) {
	m.sharedPeak = max(m.sharedPeak, dmabuf.RetainedBytes())
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	m.heapPeak = max(m.heapPeak, mem.HeapAlloc)
	if status, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "VmHWM:") || strings.HasPrefix(line, "VmRSS:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					kb, _ := strconv.ParseUint(f[1], 10, 64)
					m.rssPeak = max(m.rssPeak, kb*1024)
				}
			}
		}
	}
	if vk != nil {
		previousPeak := m.memory.PeakBytes
		m.memory = vk.MemoryStats()
		m.memory.PeakBytes = max(m.memory.PeakBytes, previousPeak)
	}
}

type performanceReport struct {
	GPUSharedSnapshotPeakBytes                          uint64    `json:"gpu_shared_snapshot_peak_bytes"`
	Version                                             int       `json:"version"`
	Backend                                             string    `json:"backend"`
	Started                                             time.Time `json:"started"`
	Seconds                                             float64   `json:"seconds"`
	Width, Height                                       int
	Frames                                              uint64 `json:"frames"`
	InputEvents                                         uint64 `json:"input_events"`
	GraphicsRecoveries                                  uint64 `json:"graphics_recoveries"`
	Resizes                                             uint64 `json:"resizes"`
	Work, Render, FrameInterval, DispatchToRenderReturn durationReport
	SampledGoHeapPeakBytes                              uint64             `json:"sampled_go_heap_peak_bytes"`
	ProcessRSSPeakBytes                                 uint64             `json:"process_rss_peak_bytes"`
	GPU                                                 native.MemoryStats `json:"gpu"`
	Error                                               string             `json:"error,omitempty"`
	Measurement                                         string             `json:"measurement"`
}

func (m *runMetrics) write(path, backend string, w, h int, runErr error) error {
	end := m.end
	if end.IsZero() {
		end = time.Now()
	}
	report := performanceReport{GPUSharedSnapshotPeakBytes: m.sharedPeak, Version: 1, Backend: backend, Started: m.start.UTC(), Seconds: end.Sub(m.start).Seconds(), Width: w, Height: h, Frames: m.render.count, InputEvents: m.inputEvents, GraphicsRecoveries: m.recoveries, Resizes: m.resizes, Work: m.work.report(), Render: m.render.report(), FrameInterval: m.interval.report(), DispatchToRenderReturn: m.input.report(), SampledGoHeapPeakBytes: m.heapPeak, ProcessRSSPeakBytes: m.rssPeak, GPU: m.memory, Measurement: "CPU wall-clock durations; percentile upper bounds use 0.1 ms bins. Work excludes frame pacing. DispatchToRenderReturn measures oldest handled input per submitted frame, not input-to-photon latency. RSS covers worldr, not child application processes. GPU counts owned Vulkan allocations, excluding driver/swapchain storage. Exported shared-image snapshot handles have a separate peak counter. Snapshot export excluded."}
	if runErr != nil {
		report.Error = runErr.Error()
	}
	bytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = writeState(path, append(bytes, '\n')); err != nil {
		return fmt.Errorf("write performance report: %w", err)
	}
	return nil
}
