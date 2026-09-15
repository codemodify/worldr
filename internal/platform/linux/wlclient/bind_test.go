package wlclient

import (
	"testing"

	"github.com/codemodify/worldr/internal/wayland"
)

func TestClampBindVersion(t *testing.T) {
	tests := []struct {
		iface      string
		advertised uint32
		want       uint32
		ok         bool
	}{
		{ifaceCompositor, 6, 6, true},
		{ifaceCompositor, 10, 6, true},
		{ifaceCompositor, 1, 1, true},
		{ifaceCompositor, 4, 4, true},
		{ifaceCompositor, 0, 0, false},
		{ifaceShm, 2, 1, true}, // our_max is 1 — never bind shm v2
		{ifaceShm, 1, 1, true},
		{ifaceShm, 0, 0, false},
		{ifaceXdg, 6, 6, true},
		{ifaceXdg, 5, 5, true},
		{ifaceXdg, 2, 2, true},
		{ifaceXdg, 0, 0, false},
		{ifaceSeat, 8, 5, true},
		{ifaceSeat, 4, 4, true},
		{ifaceSeat, 0, 0, false},
		{ifaceDataDev, 3, 3, true},
		{ifaceDataDev, 1, 1, true},
		{ifacePrimary, 1, 1, true},
		{ifacePrimary, 0, 0, false},
		{ifaceOutput, 4, 2, true}, // never bind v3+ name/description
		{ifaceOutput, 2, 2, true},
		{ifaceOutput, 1, 1, true},
		{ifaceFracScale, 1, 1, true},
		// common KWin traps — must never bind (v0 or too-high)
		{"wp_viewporter", 1, 0, false},
		{"zwp_linux_dmabuf_v1", 5, 0, false},
		{"wp_cursor_shape_manager_v1", 2, 0, false},
		{"xdg_activation_v1", 1, 0, false},
		{"zxdg_decoration_manager_v1", 1, 0, false},
		{"", 1, 0, false},
	}
	for _, tc := range tests {
		got, ok := clampBindVersion(tc.iface, tc.advertised)
		if ok != tc.ok || got != tc.want {
			t.Errorf("clamp(%q, %d) = %d,%v; want %d,%v",
				tc.iface, tc.advertised, got, ok, tc.want, tc.ok)
		}
		if ok && (got == 0 || got > tc.advertised) {
			t.Errorf("clamp(%q, %d) returned illegal version %d", tc.iface, tc.advertised, got)
		}
	}
}

func TestEncodeRegistryBindRoundtrip(t *testing.T) {
	p, err := encodeRegistryBind(1, "wl_compositor", 6, 4)
	if err != nil {
		t.Fatal(err)
	}
	name, iface, ver, id, err := decodeRegistryBind(p)
	if err != nil {
		t.Fatal(err)
	}
	if name != 1 || iface != "wl_compositor" || ver != 6 || id != 4 {
		t.Fatalf("got name=%d iface=%s ver=%d id=%d", name, iface, ver, id)
	}
	// libwayland size: header not included; payload must be 4-byte aligned
	if len(p)%4 != 0 {
		t.Fatalf("payload not aligned (%d)", len(p))
	}
	// name + "wl_compositor\0" padded + version + id
	// 4 + 4+16 + 4 + 4 = 32
	if len(p) != 32 {
		t.Fatalf("payload size %d want 32", len(p))
	}
}

func TestEncodeRegistryBindRefusesZero(t *testing.T) {
	if _, err := encodeRegistryBind(0, "wl_compositor", 1, 2); err == nil {
		t.Fatal("name 0")
	}
	if _, err := encodeRegistryBind(1, "wl_compositor", 0, 2); err == nil {
		t.Fatal("version 0")
	}
	if _, err := encodeRegistryBind(1, "wl_compositor", 1, 0); err == nil {
		t.Fatal("id 0")
	}
	if _, err := encodeRegistryBind(1, "", 1, 2); err == nil {
		t.Fatal("empty iface")
	}
}

func TestParseRegistryGlobal(t *testing.T) {
	p := wayland.PutU32(nil, 7)
	p = wayland.PutString(p, "xdg_wm_base")
	p = wayland.PutU32(p, 5)
	g, err := parseRegistryGlobal(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.name != 7 || g.iface != "xdg_wm_base" || g.advertised != 5 {
		t.Fatalf("%+v", g)
	}
}

func TestParseRegistryGlobalRejectsNameZero(t *testing.T) {
	p := wayland.PutU32(nil, 0)
	p = wayland.PutString(p, "wl_compositor")
	p = wayland.PutU32(p, 6)
	if _, err := parseRegistryGlobal(p); err == nil {
		t.Fatal("expected error")
	}
}
