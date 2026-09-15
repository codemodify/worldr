package shell

import (
	"fmt"
	"io"

	"github.com/codemodify/worldr/internal/compositor/wlsrv"
	"github.com/codemodify/worldr/internal/platform/linux/wlclient"
)

func wireHostClipboard(win *wlclient.Window, srv *wlsrv.Server, stdout io.Writer) {
	if win == nil || srv == nil {
		return
	}
	win.SetClipImport(func(primary bool, text []byte) {
		srv.ImportHostText(primary, text)
	})
	win.SetClipFulfill(func(primary bool, mime string, fd int) {
		srv.SendSelectionTo(primary, mime, fd)
	})
	srv.SetClipExport(func(primary bool, mimes []string) {
		win.OfferHostText(primary, mimes)
	})
	if win.HostClipBound() {
		msg := "clipboard: nest host bridge on (Plasma ↔ worldr text/plain)"
		if win.HostPrimaryBound() {
			msg += "; primary too"
		}
		fmt.Fprintln(stdout, msg)
		return
	}
	fmt.Fprintln(stdout, "clipboard: host has no wl_data_device_manager — in-compositor only")
}
