package nativeapp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// MaxPacketBytes includes JSON/base64 overhead around the 64 MiB retained
// texture budget. Framing uses a four-byte big-endian payload length followed
// by exactly one UTF-8 JSON value.
const MaxPacketBytes = 96 << 20

type RequestKind string

const (
	RequestHello        RequestKind = "hello"
	RequestUpdate       RequestKind = "update"
	RequestInput        RequestKind = "input"
	RequestFocus        RequestKind = "focus"
	RequestResize       RequestKind = "resize"
	RequestCloseSurface RequestKind = "close_surface"
	RequestShutdown     RequestKind = "shutdown"
)

// Request and Response are the v1 wire envelope. Sequence is chosen by the
// host and echoed by the application.
type Request struct {
	Version    uint32      `json:"version"`
	Sequence   uint64      `json:"sequence"`
	Kind       RequestKind `json:"kind"`
	Host       Host        `json:"host,omitempty"`
	DeltaNanos int64       `json:"delta_nanos,omitempty"`
	Surface    SurfaceID   `json:"surface,omitempty"`
	Width      int         `json:"width,omitempty"`
	Height     int         `json:"height,omitempty"`
	Event      *Event      `json:"event,omitempty"`
}

type Response struct {
	Version  uint32    `json:"version"`
	Sequence uint64    `json:"sequence"`
	Manifest *Manifest `json:"manifest,omitempty"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Codec reads and writes the language-neutral v1 framing. One goroutine may
// read while another writes; concurrent writes are serialized.
type Codec struct {
	reader io.Reader
	writer io.Writer
	write  sync.Mutex
}

func NewCodec(reader io.Reader, writer io.Writer) *Codec {
	return &Codec{reader: reader, writer: writer}
}

func (c *Codec) Read(value any) error {
	if c == nil || c.reader == nil {
		return fmt.Errorf("native app protocol has no reader")
	}
	var header [4]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxPacketBytes {
		return fmt.Errorf("native app packet size %d is outside 1..%d", size, MaxPacketBytes)
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return err
	}
	if err := json.Unmarshal(payload, value); err != nil {
		return fmt.Errorf("decode native app packet: %w", err)
	}
	return nil
}

func (c *Codec) Write(value any) error {
	if c == nil || c.writer == nil {
		return fmt.Errorf("native app protocol has no writer")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode native app packet: %w", err)
	}
	if len(payload) == 0 || len(payload) > MaxPacketBytes {
		return fmt.Errorf("native app packet size %d is outside 1..%d", len(payload), MaxPacketBytes)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	c.write.Lock()
	defer c.write.Unlock()
	if err := writeAll(c.writer, header[:]); err != nil {
		return err
	}
	return writeAll(c.writer, payload)
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
