// Command worldr-session is the future session manager.
//
// Phase 0: placeholder only. Process isolation (experiences/apps vs the
// shell compositor) is a later-phase concern.
package main

import (
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/version"
)

func main() {
	fmt.Fprintf(os.Stdout, "worldr-session %s\n", version.String())
	fmt.Fprintln(os.Stdout, "Phase 0 scaffold: session manager not implemented.")
}
