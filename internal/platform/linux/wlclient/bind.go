package wlclient

import (
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/wayland"
)

const (
	ifaceCompositor = "wl_compositor"
	ifaceShm        = "wl_shm"
	ifaceXdg        = "xdg_wm_base"
	ifaceSeat       = "wl_seat"
	ifaceDataDev    = "wl_data_device_manager"
	ifacePrimary    = "zwp_primary_selection_device_manager_v1"
)

// Max version we implement for each interface we actually bind.
// Trap interfaces (output, viewporter, dmabuf, cursor-shape, …)
// have max 0 so they are never bound — a too-high or v0 bind on those
// is a common KWin "invalid arguments for wl_registry.bind" cause.
var bindMaxVersion = map[string]uint32{
	ifaceCompositor: 6, // damage_buffer is v4; we do not use v5 offset
	ifaceShm:        1, // create_pool only (v2 is wl_shm.release)
	ifaceXdg:        6, // client requests are v1; extra events are ignored
	ifaceSeat:       5, // get_pointer + get_keyboard + pointer.frame
	ifaceDataDev:    3, // create_data_source + get_data_device + selection
	ifacePrimary:    1, // zwp_primary_selection if the host advertises it
}

type registryGlobal struct {
	name       uint32
	iface      string
	advertised uint32
}

// maxBindVersion is 0 if we must not bind this interface.
func maxBindVersion(iface string) uint32 {
	return bindMaxVersion[iface]
}

// clampBindVersion returns min(our_max, advertised).
// ok is false for unknown interfaces, advertised 0, or our_max 0.
// Never returns a version above advertised or 0 when ok is true.
func clampBindVersion(iface string, advertised uint32) (uint32, bool) {
	max := maxBindVersion(iface)
	if max == 0 || advertised == 0 {
		return 0, false
	}
	if advertised < max {
		return advertised, true
	}
	return max, true
}

func parseRegistryGlobal(payload []byte) (registryGlobal, error) {
	cur := wayland.NewCursor(payload, nil)
	name, err := cur.U32()
	if err != nil {
		return registryGlobal{}, fmt.Errorf("registry.global name: %w", err)
	}
	iface, err := cur.String()
	if err != nil {
		return registryGlobal{}, fmt.Errorf("registry.global interface: %w", err)
	}
	ver, err := cur.U32()
	if err != nil {
		return registryGlobal{}, fmt.Errorf("registry.global version: %w", err)
	}
	if cur.Remaining() != 0 {
		return registryGlobal{}, fmt.Errorf("registry.global leftover %d bytes name=%d iface=%s", cur.Remaining(), name, iface)
	}
	if name == 0 {
		return registryGlobal{}, fmt.Errorf("registry.global name=0 iface=%s", iface)
	}
	if iface == "" {
		return registryGlobal{}, fmt.Errorf("registry.global empty interface name=%d", name)
	}
	return registryGlobal{name: name, iface: iface, advertised: ver}, nil
}

// encodeRegistryBind is wl_registry.bind: name, interface, version, new_id.
// Refuses version 0, name 0, id 0, or an empty interface (KWin posts
// "invalid arguments for wl_registry#2.bind" for those).
func encodeRegistryBind(name uint32, iface string, version, id uint32) ([]byte, error) {
	if name == 0 || version == 0 || id == 0 || iface == "" {
		return nil, fmt.Errorf("refusing wl_registry.bind name=%d iface=%q advertised_or_req=%d id=%d", name, iface, version, id)
	}
	p := wayland.PutU32(nil, name)
	p = wayland.PutString(p, iface)
	p = wayland.PutU32(p, version)
	p = wayland.PutU32(p, id)
	return p, nil
}

func decodeRegistryBind(p []byte) (name uint32, iface string, version, id uint32, err error) {
	cur := wayland.NewCursor(p, nil)
	if name, err = cur.U32(); err != nil {
		return 0, "", 0, 0, err
	}
	if iface, err = cur.String(); err != nil {
		return 0, "", 0, 0, err
	}
	if version, err = cur.U32(); err != nil {
		return 0, "", 0, 0, err
	}
	if id, err = cur.U32(); err != nil {
		return 0, "", 0, 0, err
	}
	if cur.Remaining() != 0 {
		return 0, "", 0, 0, fmt.Errorf("bind leftover %d bytes", cur.Remaining())
	}
	return name, iface, version, id, nil
}

func logClient(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "wayland-client: "+format+"\n", args...)
}

func logGlobal(g registryGlobal) {
	req, ok := clampBindVersion(g.iface, g.advertised)
	if ok {
		logClient("global name=%d iface=%s advertised=%d bind_as=%d", g.name, g.iface, g.advertised, req)
		return
	}
	logClient("global name=%d iface=%s advertised=%d (skip bind)", g.name, g.iface, g.advertised)
}

func logBind(g registryGlobal, requested, id uint32) {
	logClient("bind name=%d iface=%s advertised=%d requested=%d id=%d",
		g.name, g.iface, g.advertised, requested, id)
}
