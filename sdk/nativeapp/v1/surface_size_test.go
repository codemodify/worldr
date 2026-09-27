package nativeapp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSurfaceResizeMinimaAreOptionalValidatedAndSerialized(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {96, 64}, {1, 1}, {MaxTextureWidth, MaxTextureHeight}} {
		surface := Surface{ID: 1, Key: "main", Title: "Small tool", Texture: 1, MinWidth: size[0], MinHeight: size[1]}
		snapshot := Snapshot{Textures: []TextureUpdate{solidTexture(1, 1, 4, 4, 0)}, Surfaces: []Surface{surface}}
		var validator Validator
		if err := validator.Validate(snapshot); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(surface)
		if err != nil {
			t.Fatal(err)
		}
		if size == [2]int{} && (bytes.Contains(data, []byte("min_width")) || bytes.Contains(data, []byte("min_height"))) {
			t.Fatal("legacy surface started emitting optional size fields")
		}
		var restored Surface
		if err := json.Unmarshal(data, &restored); err != nil || restored.MinWidth != size[0] || restored.MinHeight != size[1] {
			t.Fatal("surface size metadata did not survive serialization")
		}
	}
	for _, size := range [][2]int{{0, 64}, {96, 0}, {-1, 64}, {96, -1}, {MaxTextureWidth + 1, 64}, {96, MaxTextureHeight + 1}} {
		var validator Validator
		snapshot := Snapshot{Textures: []TextureUpdate{solidTexture(1, 1, 4, 4, 0)}, Surfaces: []Surface{{ID: 1, Key: "main", Title: "Small tool", Texture: 1, MinWidth: size[0], MinHeight: size[1]}}}
		if err := validator.Validate(snapshot); err == nil {
			t.Fatalf("invalid minimum dimensions accepted: %v", size)
		}
		snapshot.Surfaces[0].MinWidth, snapshot.Surfaces[0].MinHeight = 96, 64
		if err := validator.Validate(snapshot); err != nil {
			t.Fatal("rejected minima mutated retained validator state", err)
		}
	}
}
