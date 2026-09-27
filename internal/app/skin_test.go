package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestSkinOptionAcceptsPresetAndValidatedCustomPackage(t *testing.T) {
	o, err := Parse([]string{"--skin=hologram"}, &bytes.Buffer{})
	if err != nil || o.Skin != "hologram" {
		t.Fatalf("skin option: %+v %v", o, err)
	}
	s, err := loadSkin(o.Skin)
	if err != nil {
		t.Fatal(err)
	}
	s.ID = "custom.test"
	s.Palette["accent"] = "#ABCDEF"
	var data bytes.Buffer
	if err = skin.Encode(&data, s); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "skin.json")
	if err = os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSkin(path)
	if err != nil || loaded.ID != s.ID || loaded.Palette["accent"] != "#ABCDEF" {
		t.Fatalf("load custom skin: %+v %v", loaded, err)
	}
	if err = os.WriteFile(path, []byte(`{"version":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadSkin(path); err == nil {
		t.Fatal("invalid package accepted")
	}
}
