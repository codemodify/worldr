package glass

import "github.com/codemodify/worldr/internal/terminal"

// Keep ANSI hue meanings while lifting dark blue/red for the translucent navy
// reading plane. Only indexed ANSI colors follow the theme; applications retain
// control over explicit truecolor and the extended 256-color cube.
func ansiPalette(theme int) [16]terminal.Color {
	values := [...][16]uint32{
		{0x172832, 0xed8f98, 0x92d4ad, 0xe8cb8b, 0x8ebaff, 0xc2a3eb, 0x76deec, 0xd3e5e8,
			0x8298a2, 0xffa7af, 0xb1ecc5, 0xfbe0a4, 0xb3d2ff, 0xddc1ff, 0xa8eff6, 0xf0f9f9},
		{0x292a2a, 0xf29a86, 0xb4ce99, 0xf3c77b, 0xa5c3e8, 0xd4aad0, 0x9bcec7, 0xe9e0d0,
			0xa19b90, 0xffb39d, 0xcde5b2, 0xffdfa4, 0xc3dbf8, 0xecc5e6, 0xbfe7df, 0xfff6e6},
		{0x252638, 0xed98b8, 0x9cd3c4, 0xe4c993, 0x9ebaff, 0xc0a3f5, 0x91d7ef, 0xdcdff0,
			0x949bb8, 0xffb3cd, 0xb9e8d8, 0xf5deac, 0xc2d3ff, 0xddc6ff, 0xb8eafa, 0xf5f2ff},
	}
	var colors [16]terminal.Color
	for i, v := range values[min(2, max(0, theme))] {
		colors[i] = terminal.Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
	}
	return colors
}
