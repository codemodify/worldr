package xwayland

import (
	"strings"
	"testing"
)

func TestPrivateEnvironment(t *testing.T) {
	env := privateEnvironment([]string{"DISPLAY=:99", "DISPLAY=:100", "XAUTHORITY=/host", "WAYLAND_DISPLAY=host", "WAYLAND_SOCKET=8", "XDG_SESSION_TYPE=wayland", "PATH=/bin", "KEEP=x"}, "/private/wayland", ":17", "/private/auth")
	got := strings.Join(env, "\n")
	for _, value := range []string{"PATH=/bin", "KEEP=x", "WAYLAND_DISPLAY=/private/wayland", "DISPLAY=:17", "XAUTHORITY=/private/auth", "XDG_SESSION_TYPE=x11"} {
		if !strings.Contains(got, value) {
			t.Errorf("missing %s", value)
		}
	}
	for _, forbidden := range []string{"DISPLAY=:99", "DISPLAY=:100", "/host", "WAYLAND_SOCKET=", "WAYLAND_DISPLAY=host"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("inherited %s", forbidden)
		}
	}
}
