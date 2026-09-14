package shell

import (
	"fmt"
	"os"
	"strings"
)

func hintVKDisplay(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	var b strings.Builder
	b.WriteString(msg)
	b.WriteString("\n\nIntel Mesa (abox) hints for --backend=vk-display:")
	b.WriteString("\n  • Run on a spare VT as DRM master: Ctrl+Alt+F2, login, unset WAYLAND_DISPLAY/DISPLAY.")
	b.WriteString("\n  • Packages: vulkan-intel vulkan-icd-loader mesa libdrm (vulkaninfo --summary should show Intel + 1.4).")
	b.WriteString("\n  • ICD: /usr/share/vulkan/icd.d/intel_icd.x86_64.json  (VK_ICD_FILENAMES if needed).")
	b.WriteString("\n  • Nodes: /dev/dri/card0 and renderD128; groups video,render; logind uaccess on the active VT.")
	b.WriteString("\n  • If another compositor owns the GPU: do not pass --take-over-display from the desktop; use tty2.")
	b.WriteString("\n  • Fallback: --backend=drm (KMS dumb buffer) then --backend=wayland-client (nested, safe).")
	if _, e := os.Stat("/dev/dri"); e != nil {
		b.WriteString("\n  • This process sees no /dev/dri — vk-display cannot work here.")
	}
	return fmt.Errorf("%s", b.String())
}

func hintWaylandClient(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	var b strings.Builder
	b.WriteString(msg)
	b.WriteString("\n\nIntel Mesa (abox) hints for --backend=wayland-client:")
	b.WriteString("\n  • This is a nested debug window, not the compositor. You need an existing Wayland session.")
	b.WriteString("\n  • WAYLAND_DISPLAY and XDG_RUNTIME_DIR must be set (Hyprland/Sway/GNOME export them).")
	b.WriteString("\n  • Socket is $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY — connection refused means the compositor is gone.")
	b.WriteString("\n  • Host must advertise xdg_wm_base + wl_shm + wl_compositor.")
	b.WriteString("\n  • For a real display path use a spare TTY: --backend=vk-display --duration=15s")
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		b.WriteString("\n  • WAYLAND_DISPLAY is empty in this environment.")
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		b.WriteString("\n  • XDG_RUNTIME_DIR is empty — set it to /run/user/$(id -u).")
	}
	return fmt.Errorf("%s", b.String())
}

func hintDRM(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w\n\nDRM/KMS hint: need /dev/dri/cardN, drmSetMaster (spare VT), connected connector. Groups: video,render. Fallback: --backend=wayland-client", err)
}
