// Command worldr-shell is the future shell compositor.
//
// Phase 0: placeholder only. Later this process owns the GPU device,
// DRM/KMS present, the Wayland server, and the scene/effect loop.
package main

import (
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/version"
)

func main() {
	fmt.Fprintf(os.Stdout, "worldr-shell %s\n", version.String())
	fmt.Fprintln(os.Stdout, "Phase 0 scaffold: compositor not implemented.")
}
