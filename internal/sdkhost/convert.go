package sdkhost

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

const maxWorkspaceKeyBytes = 256

func sdkSurfaceKey(namespace, local string) string {
	key := "sdk:" + namespace + "/" + local
	if len(key) <= maxWorkspaceKeyBytes {
		return key
	}
	// Manifest IDs and SDK surface keys are ASCII by contract, so byte slicing
	// cannot split UTF-8. Retain a readable prefix and bind the omitted suffix.
	digest := sha256.Sum256([]byte(key))
	suffix := "~" + hex.EncodeToString(digest[:])
	return key[:maxWorkspaceKeyBytes-len(suffix)] + suffix
}

func meshFromSDK(resource nativeapp.MeshResource) (*scene.Mesh, error) {
	vertices := make([]scene.Vec3, len(resource.Vertices))
	for i, value := range resource.Vertices {
		vertices[i] = scene.Vec3{X: value.X, Y: value.Y, Z: value.Z}
	}
	colors := make([]scene.Color, len(resource.FaceColors))
	for i, value := range resource.FaceColors {
		colors[i] = scene.Color{R: value.R, G: value.G, B: value.B, A: value.A}
	}
	if len(resource.Normals) == 0 {
		return scene.NewMesh(vertices, resource.Indices, colors)
	}
	normals := make([]scene.Vec3, len(resource.Normals))
	for i, value := range resource.Normals {
		normals[i] = scene.Vec3{X: value.X, Y: value.Y, Z: value.Z}
	}
	return scene.NewMeshWithNormals(vertices, normals, resource.Indices, colors)
}

func (p *Provider) surfaceFromSDK(surface nativeapp.Surface) (experience.ApplicationSurface, nativeui.SemanticTree, experience.TextInputState, error) {
	resource, ok := p.textures[surface.Texture]
	if !ok {
		return experience.ApplicationSurface{}, nativeui.SemanticTree{}, experience.TextInputState{}, fmt.Errorf("surface %d texture disappeared", surface.ID)
	}
	namespace := p.keyNamespace
	if namespace == "" {
		namespace = p.manifest.ID
	}
	result := experience.ApplicationSurface{
		ID: uint64(surface.ID), Key: sdkSurfaceKey(namespace, surface.Key), AppID: p.manifest.ID,
		Title: surface.Title, Texture: resource.texture, ContentAspect: surface.ContentAspect, SurfaceUV: surface.UV,
		Translucent: surface.Translucent, Frameless: surface.Frameless, DragContent: surface.DragContent,
	}
	switch surface.FrameStyle {
	case nativeapp.FrameCinematic:
		result.FrameStyle = experience.FrameCinematic
	case nativeapp.FramePhotoBracket:
		result.FrameStyle = experience.FramePhotoBracket
	default:
		result.FrameStyle = experience.FrameDefault
	}
	if surface.Spatial != nil {
		content := &experience.SpatialContent{Objects: make([]experience.SpatialObject, 0, len(surface.Spatial.Objects)), Labels: make([]experience.SpatialLabel, 0, len(surface.Spatial.Labels))}
		for _, object := range surface.Spatial.Objects {
			node := scene.Node{
				Transform: scene.Mat4(object.Transform), SurfaceUV: object.UV,
				Color:     scene.Color{R: object.Color.R, G: object.Color.G, B: object.Color.B, A: object.Color.A},
				WireColor: scene.Color{R: object.WireColor.R, G: object.WireColor.G, B: object.WireColor.B, A: object.WireColor.A},
				WireWidth: object.WireWidth,
				Material:  render.Material{Specular: object.Material.Specular, Roughness: object.Material.Roughness, Metallic: object.Material.Metallic, RimStrength: object.Material.RimStrength, RimColor: object.Material.RimColor, Transmission: object.Material.Transmission, Refraction: object.Material.Refraction, RefractionBlur: object.Material.RefractionBlur},
				Glow:      object.Glow, Hidden: object.Hidden, Unlit: object.Unlit, Unpickable: object.Unpickable,
				DepthReadOnly: object.DepthReadOnly, Translucent: object.Translucent, CastShadow: object.CastShadow, ReceiveShadow: object.ReceiveShadow,
			}
			if object.Mesh != 0 {
				node.Mesh = p.meshes[object.Mesh]
			}
			if object.Texture != 0 {
				node.Surface = p.textures[object.Texture].texture
			}
			content.Objects = append(content.Objects, experience.SpatialObject{ID: object.ID, Parent: object.Parent, Node: node})
		}
		for _, label := range surface.Spatial.Labels {
			content.Labels = append(content.Labels, experience.SpatialLabel{Text: label.Text, Position: scene.Vec3{X: label.Position.X, Y: label.Position.Y, Z: label.Position.Z}, Color: scene.Color{R: label.Color.R, G: label.Color.G, B: label.Color.B, A: label.Color.A}})
		}
		if err := content.Validate(); err != nil {
			return experience.ApplicationSurface{}, nativeui.SemanticTree{}, experience.TextInputState{}, err
		}
		result.Spatial = content
	}
	tree := nativeui.SemanticTree{FocusedID: surface.Semantics.FocusedID, Nodes: make([]nativeui.Node, 0, len(surface.Semantics.Nodes))}
	for _, node := range surface.Semantics.Nodes {
		tree.Nodes = append(tree.Nodes, nativeui.Node{
			ID: node.ID, Role: roleFromSDK(node.Role), Label: node.Label, Value: node.Value, Description: node.Description,
			Bounds:   image.Rect(node.Bounds.X, node.Bounds.Y, node.Bounds.X+node.Bounds.Width, node.Bounds.Y+node.Bounds.Height),
			Disabled: node.Disabled, Selected: node.Selected,
		})
	}
	input := experience.TextInputState{
		Enabled: surface.TextInput.Enabled, ContextID: surface.TextInput.ContextID, Surrounding: surface.TextInput.Surrounding,
		Cursor: surface.TextInput.Cursor, Anchor: surface.TextInput.Anchor,
		CursorRect: [4]int{surface.TextInput.CursorRect.X, surface.TextInput.CursorRect.Y, surface.TextInput.CursorRect.Width, surface.TextInput.CursorRect.Height},
	}
	return result, tree, input, nil
}

func roleFromSDK(role nativeapp.Role) nativeui.Role {
	switch role {
	case nativeapp.RoleButton:
		return nativeui.RoleButton
	case nativeapp.RoleTextField:
		return nativeui.RoleTextField
	case nativeapp.RoleMenuItem:
		return nativeui.RoleMenuItem
	case nativeapp.RoleSlider:
		return nativeui.RoleSlider
	case nativeapp.RoleImage:
		return nativeui.RoleImage
	case nativeapp.RoleDocument:
		return nativeui.RoleDocument
	case nativeapp.RoleStatus:
		return nativeui.RoleStatus
	case nativeapp.RoleLabel:
		return nativeui.RoleLabel
	default:
		// The public validator rejects unknown roles before conversion. Keep a
		// harmless fallback for defensive use by isolated conversion tests.
		return nativeui.RoleLabel
	}
}

func eventToSDK(event experience.Event) nativeapp.Event {
	return nativeapp.Event{
		Kind: eventKindToSDK(event.Kind), X: event.X, Y: event.Y, Button: nativeapp.Button(event.Button), ButtonCode: event.ButtonCode,
		ScrollX: event.ScrollX, ScrollY: event.ScrollY, Key: string(event.Key), Keycode: event.Keycode, Modifiers: nativeapp.Modifiers(event.Modifiers),
		Pressed: event.Pressed, Repeat: event.Repeat, Time: event.Time, Keymap: event.Keymap,
		Depressed: event.Depressed, Latched: event.Latched, Locked: event.Locked, Group: event.Group,
		RepeatRate: event.RepeatRate, RepeatDelay: event.RepeatDelay,
		Text: event.Text, TextContext: event.TextContext, PreeditBegin: event.PreeditBegin, PreeditEnd: event.PreeditEnd,
		DeleteBefore: event.DeleteBefore, DeleteAfter: event.DeleteAfter,
		SpatialObject: event.SpatialObject, SpatialTriangle: event.SpatialTriangle, SpatialPoint: event.SpatialPoint,
	}
}

func eventKindToSDK(kind experience.EventKind) nativeapp.EventKind {
	switch kind {
	case experience.PointerMove:
		return nativeapp.PointerMove
	case experience.PointerDown:
		return nativeapp.PointerDown
	case experience.PointerUp:
		return nativeapp.PointerUp
	case experience.PointerCancel:
		return nativeapp.PointerCancel
	case experience.PointerScroll:
		return nativeapp.PointerScroll
	case experience.KeyInput:
		return nativeapp.KeyInput
	case experience.KeyboardCancel:
		return nativeapp.KeyboardCancel
	case experience.KeymapChanged:
		return nativeapp.KeymapChanged
	case experience.KeyboardModifiers:
		return nativeapp.KeyboardModifiers
	case experience.KeyboardRepeatInfo:
		return nativeapp.KeyboardRepeat
	case experience.TextCommit:
		return nativeapp.TextCommit
	case experience.TextPreedit:
		return nativeapp.TextPreedit
	default:
		return ""
	}
}
