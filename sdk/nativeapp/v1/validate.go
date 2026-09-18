package nativeapp

import (
	"fmt"
	"unicode/utf8"
)

type textureState struct {
	revision      uint64
	width, height int
}

type meshState struct{ vertices, indices int }

// Validator checks the stateful retained-resource rules across snapshots. A
// failed validation leaves its previously accepted state unchanged.
type Validator struct {
	textures map[ResourceID]textureState
	meshes   map[ResourceID]meshState
}

func (v *Validator) Validate(snapshot Snapshot) error {
	textures := make(map[ResourceID]textureState, len(v.textures)+len(snapshot.Textures))
	for id, state := range v.textures {
		textures[id] = state
	}
	meshes := make(map[ResourceID]meshState, len(v.meshes)+len(snapshot.Meshes))
	for id, state := range v.meshes {
		meshes[id] = state
	}
	if err := validateTextureUpdates(textures, snapshot.Textures); err != nil {
		return err
	}
	if err := validateMeshUpdates(meshes, snapshot.Meshes); err != nil {
		return err
	}
	textureRefs, meshRefs, err := validateSurfaces(snapshot.Surfaces, textures, meshes)
	if err != nil {
		return err
	}
	seen := make(map[ResourceID]bool, len(snapshot.RetireTextures))
	for _, id := range snapshot.RetireTextures {
		if id == 0 || seen[id] {
			return fmt.Errorf("texture retirement contains an invalid or duplicate ID")
		}
		seen[id] = true
		if _, ok := textures[id]; !ok {
			return fmt.Errorf("texture %d is not live", id)
		}
		if textureRefs[id] {
			return fmt.Errorf("texture %d is still referenced", id)
		}
		delete(textures, id)
	}
	clear(seen)
	for _, id := range snapshot.RetireMeshes {
		if id == 0 || seen[id] {
			return fmt.Errorf("mesh retirement contains an invalid or duplicate ID")
		}
		seen[id] = true
		if _, ok := meshes[id]; !ok {
			return fmt.Errorf("mesh %d is not live", id)
		}
		if meshRefs[id] {
			return fmt.Errorf("mesh %d is still referenced", id)
		}
		delete(meshes, id)
	}
	if len(textures) > MaxTextures || len(meshes) > MaxMeshes {
		return fmt.Errorf("native app exceeds retained resource count")
	}
	var textureBytes uint64
	for _, state := range textures {
		textureBytes += uint64(state.width) * uint64(state.height) * 4
	}
	if textureBytes > MaxTextureBytes {
		return fmt.Errorf("native app textures exceed %d bytes", MaxTextureBytes)
	}
	var vertices, indices int
	for _, state := range meshes {
		vertices += state.vertices
		indices += state.indices
	}
	if vertices > MaxMeshVertices || indices > MaxMeshIndices {
		return fmt.Errorf("native app meshes exceed %d vertices or %d indices", MaxMeshVertices, MaxMeshIndices)
	}
	v.textures, v.meshes = textures, meshes
	return nil
}

func validateTextureUpdates(textures map[ResourceID]textureState, updates []TextureUpdate) error {
	if len(updates) > MaxTextures {
		return fmt.Errorf("snapshot has too many texture updates")
	}
	seen := make(map[ResourceID]bool, len(updates))
	for _, update := range updates {
		if update.ID == 0 || update.Revision == 0 || seen[update.ID] {
			return fmt.Errorf("texture update contains an invalid ID, revision, or duplicate")
		}
		seen[update.ID] = true
		if update.Width <= 0 || update.Height <= 0 || update.Width > MaxTextureWidth || update.Height > MaxTextureHeight {
			return fmt.Errorf("texture %d dimensions exceed the v1 limit", update.ID)
		}
		if !update.Rect.validWithin(update.Width, update.Height) {
			return fmt.Errorf("texture %d damage is empty or outside its image", update.ID)
		}
		need := uint64(update.Rect.Width) * uint64(update.Rect.Height) * 4
		if need != uint64(len(update.Pixels)) {
			return fmt.Errorf("texture %d damage needs %d RGBA bytes", update.ID, need)
		}
		full := update.Rect == (Rect{Width: update.Width, Height: update.Height})
		old, exists := textures[update.ID]
		if !exists {
			if update.Revision != 1 || !full {
				return fmt.Errorf("texture %d must begin at revision 1 with a full image", update.ID)
			}
		} else {
			if update.Revision != old.revision+1 {
				return fmt.Errorf("texture %d revision must advance from %d to %d", update.ID, old.revision, old.revision+1)
			}
			if (update.Width != old.width || update.Height != old.height) && !full {
				return fmt.Errorf("resized texture %d needs a full image", update.ID)
			}
		}
		textures[update.ID] = textureState{update.Revision, update.Width, update.Height}
	}
	return nil
}

func validateMeshUpdates(meshes map[ResourceID]meshState, updates []MeshResource) error {
	if len(updates) > MaxMeshes {
		return fmt.Errorf("snapshot has too many mesh updates")
	}
	seen := make(map[ResourceID]bool, len(updates))
	for _, mesh := range updates {
		if mesh.ID == 0 || seen[mesh.ID] {
			return fmt.Errorf("mesh update contains an invalid or duplicate ID")
		}
		seen[mesh.ID] = true
		if _, exists := meshes[mesh.ID]; exists {
			return fmt.Errorf("mesh %d is immutable; publish a new ID", mesh.ID)
		}
		if len(mesh.Vertices) == 0 || len(mesh.Vertices) > MaxMeshVertices || len(mesh.Indices) == 0 || len(mesh.Indices) > MaxMeshIndices || len(mesh.Indices)%3 != 0 {
			return fmt.Errorf("mesh %d exceeds v1 geometry limits or has incomplete triangles", mesh.ID)
		}
		if len(mesh.Normals) != 0 && len(mesh.Normals) != len(mesh.Vertices) {
			return fmt.Errorf("mesh %d normals must be empty or match its vertices", mesh.ID)
		}
		if len(mesh.FaceColors) != 0 && len(mesh.FaceColors) != len(mesh.Indices)/3 {
			return fmt.Errorf("mesh %d face colors must match its triangles", mesh.ID)
		}
		for i, vertex := range mesh.Vertices {
			if !finite(vertex.X, vertex.Y, vertex.Z) {
				return fmt.Errorf("mesh %d vertex %d is not finite", mesh.ID, i)
			}
		}
		for i, normal := range mesh.Normals {
			if !finite(normal.X, normal.Y, normal.Z) || normal == (Vec3{}) {
				return fmt.Errorf("mesh %d normal %d is invalid", mesh.ID, i)
			}
		}
		for _, index := range mesh.Indices {
			if uint64(index) >= uint64(len(mesh.Vertices)) {
				return fmt.Errorf("mesh %d index exceeds its vertices", mesh.ID)
			}
		}
		for _, color := range mesh.FaceColors {
			if !finite(color.R, color.G, color.B, color.A) {
				return fmt.Errorf("mesh %d color is not finite", mesh.ID)
			}
		}
		meshes[mesh.ID] = meshState{len(mesh.Vertices), len(mesh.Indices)}
	}
	return nil
}

func validateSurfaces(surfaces []Surface, textures map[ResourceID]textureState, meshes map[ResourceID]meshState) (map[ResourceID]bool, map[ResourceID]bool, error) {
	textureRefs := make(map[ResourceID]bool)
	meshRefs := make(map[ResourceID]bool)
	if len(surfaces) > MaxSurfaces {
		return nil, nil, fmt.Errorf("snapshot exceeds %d surfaces", MaxSurfaces)
	}
	ids, keys := make(map[SurfaceID]bool), make(map[string]bool)
	semanticCount := 0
	for _, surface := range surfaces {
		if surface.ID == 0 || ids[surface.ID] {
			return nil, nil, fmt.Errorf("surface has an invalid or duplicate runtime ID")
		}
		ids[surface.ID] = true
		if !validKey(surface.Key) || keys[surface.Key] {
			return nil, nil, fmt.Errorf("surface %d has an invalid or duplicate stable key", surface.ID)
		}
		keys[surface.Key] = true
		if len(surface.Title) == 0 || len(surface.Title) > 256 || !utf8.ValidString(surface.Title) {
			return nil, nil, fmt.Errorf("surface %d title must be valid UTF-8 and contain 1..256 bytes", surface.ID)
		}
		texture, ok := textures[surface.Texture]
		if surface.Texture == 0 || !ok {
			return nil, nil, fmt.Errorf("surface %d references unknown texture %d", surface.ID, surface.Texture)
		}
		textureRefs[surface.Texture] = true
		if surface.FrameStyle != "" && surface.FrameStyle != FrameDefault && surface.FrameStyle != FrameCinematic && surface.FrameStyle != FramePhotoBracket {
			return nil, nil, fmt.Errorf("surface %d has an unknown frame style", surface.ID)
		}
		if !finite(surface.ContentAspect) || surface.ContentAspect < 0 || !finite(surface.UV[:]...) {
			return nil, nil, fmt.Errorf("surface %d has an invalid aspect or UV", surface.ID)
		}
		if surface.UV != ([4]float32{}) && (surface.UV[2] <= 0 || surface.UV[3] <= 0 || surface.UV[0] < 0 || surface.UV[1] < 0 || surface.UV[0]+surface.UV[2] > 1 || surface.UV[1]+surface.UV[3] > 1) {
			return nil, nil, fmt.Errorf("surface %d UV is outside the texture", surface.ID)
		}
		if err := validateSpatial(surface.Spatial, textures, meshes, textureRefs, meshRefs); err != nil {
			return nil, nil, fmt.Errorf("surface %d: %w", surface.ID, err)
		}
		semanticCount += len(surface.Semantics.Nodes)
		if semanticCount > MaxSemanticNodes {
			return nil, nil, fmt.Errorf("snapshot exceeds %d semantic nodes", MaxSemanticNodes)
		}
		if err := validateSemantics(surface.Semantics, texture.width, texture.height); err != nil {
			return nil, nil, fmt.Errorf("surface %d: %w", surface.ID, err)
		}
		if err := validateTextInput(surface.TextInput, texture.width, texture.height); err != nil {
			return nil, nil, fmt.Errorf("surface %d: %w", surface.ID, err)
		}
	}
	return textureRefs, meshRefs, nil
}

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 128 {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' || r == '/') {
			return false
		}
	}
	return true
}

func validateSpatial(content *SpatialContent, textures map[ResourceID]textureState, meshes map[ResourceID]meshState, textureRefs, meshRefs map[ResourceID]bool) error {
	if content == nil {
		return nil
	}
	if len(content.Objects) > MaxSpatialObjects || len(content.Labels) > MaxSpatialLabels {
		return fmt.Errorf("spatial content exceeds object or label limit")
	}
	seen := make(map[uint64]bool, len(content.Objects))
	for _, object := range content.Objects {
		if object.ID == 0 || seen[object.ID] || object.Parent != 0 && !seen[object.Parent] || object.Mesh != 0 && object.Texture != 0 {
			return fmt.Errorf("spatial object has an invalid identity, parent, or resource pair")
		}
		seen[object.ID] = true
		if object.Mesh != 0 {
			if _, ok := meshes[object.Mesh]; !ok {
				return fmt.Errorf("spatial object references unknown mesh %d", object.Mesh)
			}
			meshRefs[object.Mesh] = true
		}
		if object.Texture != 0 {
			if _, ok := textures[object.Texture]; !ok {
				return fmt.Errorf("spatial object references unknown texture %d", object.Texture)
			}
			textureRefs[object.Texture] = true
		}
		values := append([]float32(nil), object.Transform[:]...)
		values = append(values, object.UV[:]...)
		values = append(values, object.Color.R, object.Color.G, object.Color.B, object.Color.A, object.WireColor.R, object.WireColor.G, object.WireColor.B, object.WireColor.A, object.WireWidth, object.Material.Specular, object.Material.Roughness, object.Material.Metallic, object.Material.RimStrength, object.Material.Transmission, object.Material.Refraction, object.Material.RefractionBlur)
		values = append(values, object.Material.RimColor[:]...)
		values = append(values, object.Glow[:]...)
		if !finite(values...) || object.WireWidth < 0 || !unit(object.Material.Specular, object.Material.Roughness, object.Material.Metallic, object.Material.RimStrength, object.Material.Transmission, object.Material.Refraction, object.Material.RefractionBlur) || !unit(object.Material.RimColor[:]...) || !unit(object.Glow[:]...) || object.Material.Transmission != 0 && (!object.Translucent || object.Mesh == 0) || (object.Material.Refraction != 0 || object.Material.RefractionBlur != 0) && (!object.Translucent || object.Mesh == 0 || object.Unlit || object.Material.Transmission == 0) || object.Material.RefractionBlur != 0 && object.Material.Refraction == 0 {
			return fmt.Errorf("spatial object appearance is invalid")
		}
	}
	for _, label := range content.Labels {
		if len(label.Text) > 512 || !utf8.ValidString(label.Text) || !finite(label.Position.X, label.Position.Y, label.Position.Z, label.Color.R, label.Color.G, label.Color.B, label.Color.A) {
			return fmt.Errorf("spatial label is invalid")
		}
	}
	return nil
}

func unit(values ...float32) bool {
	for _, value := range values {
		if value < 0 || value > 1 {
			return false
		}
	}
	return true
}

func validateSemantics(tree SemanticTree, width, height int) error {
	seen := make(map[string]bool, len(tree.Nodes))
	roles := map[Role]bool{RoleButton: true, RoleTextField: true, RoleMenuItem: true, RoleLabel: true, RoleSlider: true, RoleImage: true, RoleDocument: true, RoleStatus: true}
	for _, node := range tree.Nodes {
		if len(node.ID) == 0 || len(node.ID) > 128 || !utf8.ValidString(node.ID) || seen[node.ID] || !roles[node.Role] {
			return fmt.Errorf("semantic tree has an invalid identity or role")
		}
		seen[node.ID] = true
		if len(node.Label) > 512 || len(node.Value) > 4096 || len(node.Description) > 1024 || !utf8.ValidString(node.Label) || !utf8.ValidString(node.Value) || !utf8.ValidString(node.Description) {
			return fmt.Errorf("semantic node text exceeds its limit")
		}
		if !node.Bounds.validWithin(width, height) {
			return fmt.Errorf("semantic node bounds are outside the surface")
		}
	}
	if tree.FocusedID != "" && !seen[tree.FocusedID] {
		return fmt.Errorf("semantic focus references an unknown node")
	}
	return nil
}

func validateTextInput(state TextInputState, width, height int) error {
	if !state.Enabled {
		return nil
	}
	if len(state.ContextID) == 0 || len(state.ContextID) > 128 || !utf8.ValidString(state.ContextID) || len(state.Surrounding) > 64<<10 || !utf8.ValidString(state.Surrounding) {
		return fmt.Errorf("text input context is invalid")
	}
	boundary := func(offset int) bool {
		return offset == len(state.Surrounding) || utf8.RuneStart(state.Surrounding[offset])
	}
	if state.Cursor < 0 || state.Anchor < 0 || state.Cursor > len(state.Surrounding) || state.Anchor > len(state.Surrounding) || !boundary(state.Cursor) || !boundary(state.Anchor) {
		return fmt.Errorf("text input offsets are not UTF-8 boundaries")
	}
	if !state.CursorRect.validWithin(width, height) {
		return fmt.Errorf("text input cursor rectangle is outside the surface")
	}
	return nil
}

func validateEvent(event Event) error {
	kinds := map[EventKind]bool{
		PointerMove: true, PointerDown: true, PointerUp: true, PointerCancel: true, PointerScroll: true,
		KeyInput: true, KeyboardCancel: true, KeymapChanged: true, KeyboardModifiers: true, KeyboardRepeat: true,
		TextCommit: true, TextPreedit: true,
	}
	if !kinds[event.Kind] {
		return fmt.Errorf("unknown input event %q", event.Kind)
	}
	if !finite(event.X, event.Y, event.ScrollX, event.ScrollY, event.SpatialPoint[0], event.SpatialPoint[1], event.SpatialPoint[2]) {
		return fmt.Errorf("input event contains non-finite coordinates")
	}
	if len(event.Key) > 64 || !utf8.ValidString(event.Key) || len(event.Keymap) > MaxTextBytes || len(event.Text) > MaxTextBytes || len(event.TextContext) > 128 || !utf8.ValidString(event.Text) || !utf8.ValidString(event.TextContext) {
		return fmt.Errorf("input event text exceeds v1 limits")
	}
	return nil
}
