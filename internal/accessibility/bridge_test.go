//go:build linux

package accessibility

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture() Snapshot {
	return Snapshot{Applications: []Application{{ID: 7, Key: "native:terminal:1", Title: "Terminal", CoordinateSpace: "application-pixels", Focused: true, NativeSemantics: true, FocusedNode: "find", Nodes: []Node{{ID: "find", Role: "textbox", Label: "Search", X: 10, Y: 12, Width: 200, Height: 30}}}}}
}

func TestSnapshotValidationRejectsAmbiguousTrees(t *testing.T) {
	valid := fixture()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := fixture()
	invalid.Applications[0].Nodes = append(invalid.Applications[0].Nodes, invalid.Applications[0].Nodes[0])
	if err := invalid.Validate(); err == nil {
		t.Fatal("duplicate node accepted")
	}
	invalid = fixture()
	invalid.Applications[0].FocusedNode = "missing"
	if err := invalid.Validate(); err == nil {
		t.Fatal("missing focused node accepted")
	}
	invalid = fixture()
	invalid.Applications[0].CoordinateSpace = "screen"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unimplemented coordinate space accepted")
	}
}

func TestSnapshotValidationAcceptsEveryPublishedNativeRole(t *testing.T) {
	roles := []string{"button", "textbox", "menuitem", "label", "slider", "image", "document", "status"}
	nodes := make([]Node, 0, len(roles))
	for index, role := range roles {
		nodes = append(nodes, Node{ID: role, Role: role, Width: index + 1, Height: 1})
	}
	snapshot := Snapshot{Applications: []Application{{
		ID: 1, Title: "Native semantics", CoordinateSpace: "application-pixels",
		NativeSemantics: true, Nodes: nodes,
	}}}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.Applications[0].Nodes[0].Role = "unknown"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("unknown semantic role accepted")
	}
}

func TestServerStreamsLatestBoundedSnapshotsOnPrivateSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accessibility.sock")
	server, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket permissions: %v %v", info, err)
	}
	if err := server.Publish(fixture()); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	decoder := json.NewDecoder(connection)
	var first Snapshot
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if first.Version != ProtocolVersion || first.Serial != 1 || first.Applications[0].FocusedNode != "find" {
		t.Fatalf("first snapshot: %+v", first)
	}
	// An identical publication does not produce traffic or advance the serial.
	if err := server.Publish(fixture()); err != nil {
		t.Fatal(err)
	}
	updated := fixture()
	updated.Applications[0].Nodes[0].Value = "compiler"
	if err := server.Publish(updated); err != nil {
		t.Fatal(err)
	}
	var second Snapshot
	if err := decoder.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if second.Serial != 2 || second.Applications[0].Nodes[0].Value != "compiler" {
		t.Fatalf("second snapshot: %+v", second)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("socket survived clean close", err)
	}
}
