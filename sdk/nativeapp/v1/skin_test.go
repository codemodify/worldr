package nativeapp

import (
	"bytes"
	"context"
	"testing"

	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

type skinProtocolApp struct {
	protocolApp
	enabled bool
	skins   []skin.Skin
}

func (a *skinProtocolApp) Manifest() Manifest {
	m := a.protocolApp.Manifest()
	m.Skins = a.enabled
	return m
}
func (a *skinProtocolApp) SetSkin(selected skin.Skin) error {
	a.skins = append(a.skins, selected)
	return nil
}

type missingSkinHandler struct{ protocolApp }

func (a *missingSkinHandler) Manifest() Manifest {
	m := a.protocolApp.Manifest()
	m.Skins = true
	return m
}

func TestServeSkinCapabilityValidationAndLegacyTheme(t *testing.T) {
	valid, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	invalid := valid.Clone()
	invalid.Version = 999
	for _, name := range []string{"valid", "unsupported", "missing", "invalid", "before-hello", "missing-handler"} {
		t.Run(name, func(t *testing.T) {
			app := &skinProtocolApp{enabled: name != "unsupported"}
			var application Application = app
			if name == "missing-handler" {
				application = &missingSkinHandler{}
			}
			selected := &valid
			if name == "missing" {
				selected = nil
			}
			if name == "invalid" {
				selected = &invalid
			}
			hello := Request{Version: Version, Sequence: 1, Kind: RequestHello, Host: Host{Version: Version, MaxSurfaceWidth: 10, MaxSurfaceHeight: 10, MaxSurfaces: 1}}
			update := Request{Version: Version, Sequence: 2, Kind: RequestSkin, Skin: selected}
			requests := []Request{hello, update}
			skinResponse := 1
			if name == "before-hello" {
				requests = []Request{update, hello}
				skinResponse = 0
			}
			legacy := ControlTheme{Family: "glass", Shape: "slab"}
			requests = append(requests, Request{Version: Version, Sequence: 3, Kind: RequestTheme, Theme: &legacy}, Request{Version: Version, Sequence: 4, Kind: RequestShutdown})
			var input, output bytes.Buffer
			codec := NewCodec(nil, &input)
			for _, request := range requests {
				if err := codec.Write(request); err != nil {
					t.Fatal(err)
				}
			}
			if err := Serve(context.Background(), application, &input, &output); err != nil {
				t.Fatal(err)
			}
			reader := NewCodec(&output, nil)
			for index := range requests {
				var response Response
				if err := reader.Read(&response); err != nil {
					t.Fatal(err)
				}
				if index == skinResponse && (response.Error == "") != (name == "valid") {
					t.Fatalf("skin response: %+v", response)
				}
				if index == 2 && response.Error != "" {
					t.Fatal("legacy theme request failed", response.Error)
				}
			}
			if name == "valid" && (len(app.skins) != 1 || app.skins[0].ID != valid.ID || app.themes != 1) {
				t.Fatal("skin or legacy callback missing")
			}
			if name != "valid" && len(app.skins) != 0 {
				t.Fatal("invalid or unnegotiated skin reached callback")
			}
		})
	}
}
