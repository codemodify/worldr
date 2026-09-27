package glass

import _ "embed"

// Hack is distributed unmodified with its MIT/Bitstream license in assets.
// Embedding the face keeps the main GPU text path independent of system fonts.
//
//go:embed assets/Hack-Regular.ttf
var terminalFont []byte
