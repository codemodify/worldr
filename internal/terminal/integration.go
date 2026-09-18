package terminal

import _ "embed"

// BashIntegration is opt-in shell setup shown by the command-history UI. It is
// copied to the clipboard on request, never automatically sent to the shell.
//
//go:embed shell_integration.bash
var BashIntegration string
