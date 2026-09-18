package native

// DRMOutput is a read-only connector inventory entry. Dimensions and refresh
// describe the preferred mode; disconnected connectors may have no mode.
type DRMOutput struct {
	ID                            uint32
	Card, Name                    string
	Connected                     bool
	Width, Height, RefreshMilliHz uint32
}
