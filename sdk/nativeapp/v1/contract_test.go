package nativeapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"
)

func solidTexture(id ResourceID, revision uint64, width, height int, value byte) TextureUpdate {
	pixels := bytes.Repeat([]byte{value, value, value, 255}, width*height)
	return TextureUpdate{ID: id, Revision: revision, Width: width, Height: height, Rect: Rect{Width: width, Height: height}, Pixels: pixels}
}

func TestValidatorEnforcesRetainedResourceLifetime(t *testing.T) {
	var validator Validator
	initial := Snapshot{
		Textures: []TextureUpdate{solidTexture(1, 1, 4, 3, 20)},
		Meshes:   []MeshResource{{ID: 2, Vertices: []Vec3{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, Indices: []uint32{0, 1, 2}}},
		Surfaces: []Surface{{
			ID: 4, Key: "main", Title: "SDK test", Texture: 1,
			Spatial:   &SpatialContent{Objects: []SpatialObject{{ID: 1, Mesh: 2}}},
			Semantics: SemanticTree{Nodes: []SemanticNode{{ID: "plot", Role: RoleImage, Label: "Plot", Bounds: Rect{Width: 4, Height: 3}}, {ID: "gain", Role: RoleSlider, Label: "Gain", Bounds: Rect{Y: 2, Width: 4, Height: 1}}}},
		}},
	}
	if err := validator.Validate(initial); err != nil {
		t.Fatal(err)
	}
	partial := Snapshot{
		Textures: []TextureUpdate{{ID: 1, Revision: 2, Width: 4, Height: 3, Rect: Rect{X: 1, Y: 1, Width: 2, Height: 1}, Pixels: bytes.Repeat([]byte{1}, 8)}},
		Surfaces: initial.Surfaces,
	}
	if err := validator.Validate(partial); err != nil {
		t.Fatal(err)
	}
	bad := Snapshot{Surfaces: initial.Surfaces, RetireTextures: []ResourceID{1}}
	if err := validator.Validate(bad); err == nil {
		t.Fatal("retired a referenced texture")
	}
	// The failed retirement must not mutate validator state.
	if err := validator.Validate(Snapshot{RetireTextures: []ResourceID{1}, RetireMeshes: []ResourceID{2}}); err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(Snapshot{RetireTextures: []ResourceID{1}}); err == nil {
		t.Fatal("accepted duplicate retirement")
	}
}

func TestValidatorRejectsBrokenSurfaceSemanticsAndTextureRevision(t *testing.T) {
	var validator Validator
	if err := validator.Validate(Snapshot{Textures: []TextureUpdate{solidTexture(1, 2, 2, 2, 0)}}); err == nil {
		t.Fatal("accepted a resource beginning after revision 1")
	}
	bad := Snapshot{
		Textures: []TextureUpdate{solidTexture(1, 1, 2, 2, 0)},
		Surfaces: []Surface{{ID: 1, Key: "bad key", Title: "Bad", Texture: 1}},
	}
	if err := validator.Validate(bad); err == nil {
		t.Fatal("accepted an unstable key")
	}
	bad.Surfaces[0].Key = "main"
	bad.Surfaces[0].Semantics = SemanticTree{Nodes: []SemanticNode{{ID: "x", Role: RoleButton, Bounds: Rect{X: 2, Width: 1, Height: 1}}}}
	if err := validator.Validate(bad); err == nil {
		t.Fatal("accepted semantic bounds outside content")
	}
}

func TestValidatorLimitsGlassOpticsToLitTranslucentMeshes(t *testing.T) {
	base := Snapshot{
		Textures: []TextureUpdate{solidTexture(1, 1, 2, 2, 0)},
		Meshes:   []MeshResource{{ID: 2, Vertices: []Vec3{{}, {X: 1}, {Y: 1}}, Indices: []uint32{0, 1, 2}}},
		Surfaces: []Surface{{ID: 1, Key: "glass", Title: "Glass", Texture: 1, Spatial: &SpatialContent{Objects: []SpatialObject{{ID: 1, Mesh: 2, Translucent: true, Material: Material{Transmission: .8, Refraction: .7, RefractionBlur: .2}}}}}},
	}
	var validator Validator
	if err := validator.Validate(base); err != nil {
		t.Fatal(err)
	}
	for _, object := range []SpatialObject{
		{ID: 1, Mesh: 2, Material: Material{Transmission: .8}},
		{ID: 1, Texture: 1, Translucent: true, Material: Material{Transmission: .8}},
		{ID: 1, Mesh: 2, Translucent: true, Material: Material{Transmission: 1.1}},
		{ID: 1, Mesh: 2, Translucent: true, Material: Material{Refraction: .5}},
		{ID: 1, Mesh: 2, Translucent: true, Unlit: true, Material: Material{Transmission: .8, Refraction: .5}},
		{ID: 1, Mesh: 2, Translucent: true, Material: Material{Transmission: .8, RefractionBlur: .2}},
		{ID: 1, Mesh: 2, Translucent: true, Material: Material{Transmission: .8, Refraction: 1.1}},
	} {
		bad := base
		bad.Textures, bad.Meshes = nil, nil
		bad.Surfaces = []Surface{{ID: 1, Key: "glass", Title: "Glass", Texture: 1, Spatial: &SpatialContent{Objects: []SpatialObject{object}}}}
		if err := validator.Validate(bad); err == nil {
			t.Fatal("accepted invalid glass material", object)
		}
	}
}

type protocolApp struct {
	started, updates, inputs, closed int
	pending                          []TextureUpdate
}

func (a *protocolApp) Manifest() Manifest {
	return Manifest{ID: "dev.worldr.test", Name: "Protocol test"}
}
func (a *protocolApp) Start(host Host) error {
	if host.Version != Version {
		return errors.New("wrong host version")
	}
	a.started++
	a.pending = []TextureUpdate{solidTexture(1, 1, 2, 2, 40)}
	return nil
}
func (a *protocolApp) Update(delta time.Duration) error {
	if delta != 16*time.Millisecond {
		return errors.New("wrong delta")
	}
	a.updates++
	return nil
}
func (a *protocolApp) Handle(id SurfaceID, event Event) error {
	if id != 7 || event.Kind != PointerDown {
		return errors.New("wrong input")
	}
	a.inputs++
	return nil
}
func (a *protocolApp) Snapshot() Snapshot {
	updates := a.pending
	a.pending = nil
	return Snapshot{Textures: updates, Surfaces: []Surface{{ID: 7, Key: "main", Title: "Live", Texture: 1}}}
}
func (a *protocolApp) Close() error { a.closed++; return nil }

func TestServeNegotiatesAndSerializesLifecycle(t *testing.T) {
	host, application := net.Pipe()
	defer host.Close()
	app := &protocolApp{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), app, application, application); application.Close() }()
	codec := NewCodec(host, host)
	request := func(value Request) Response {
		t.Helper()
		if err := codec.Write(value); err != nil {
			t.Fatal(err)
		}
		var response Response
		if err := codec.Read(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error != "" {
			t.Fatal(response.Error)
		}
		if response.Sequence != value.Sequence || response.Version != Version {
			t.Fatalf("bad response envelope: %+v", response)
		}
		return response
	}
	hello := request(Request{Version: Version, Sequence: 1, Kind: RequestHello, Host: Host{Version: Version, MaxSurfaceWidth: 4096, MaxSurfaceHeight: 4096, MaxSurfaces: MaxSurfaces}})
	if hello.Manifest == nil || hello.Manifest.ID != "dev.worldr.test" || hello.Snapshot == nil || len(hello.Snapshot.Textures) != 1 {
		t.Fatalf("incomplete hello: %+v", hello)
	}
	request(Request{Version: Version, Sequence: 2, Kind: RequestUpdate, DeltaNanos: int64(16 * time.Millisecond)})
	event := Event{Kind: PointerDown, Button: ButtonPrimary, X: 1, Y: 1}
	request(Request{Version: Version, Sequence: 3, Kind: RequestInput, Surface: 7, Event: &event})
	request(Request{Version: Version, Sequence: 4, Kind: RequestShutdown})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
	if app.started != 1 || app.updates != 1 || app.inputs != 1 || app.closed != 1 {
		t.Fatalf("callbacks: %+v", app)
	}
}

func TestCodecRejectsOversizedPacketBeforeAllocation(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], MaxPacketBytes+1)
	var value Response
	if err := NewCodec(bytes.NewReader(header[:]), nil).Read(&value); err == nil {
		t.Fatal("accepted oversized packet")
	}
}
