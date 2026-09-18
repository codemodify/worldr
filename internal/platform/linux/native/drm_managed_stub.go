//go:build !linux || !cgo

package native

func OpenDRMFromFD(int, ...uint32) (*DRM, error)             { return nil, ErrUnavailable }
func OpenDRMFromFDConnector(int, uint32) (*DRM, error)       { return nil, ErrUnavailable }
func OpenDRMPlanesFromFD(int, ...uint32) (*DRM, error)       { return nil, ErrUnavailable }
func OpenDRMPlanesFromFDConnector(int, uint32) (*DRM, error) { return nil, ErrUnavailable }
func (*DRM) ConnectorID() uint32                             { return 0 }
func DRMOutputsFromFD(int) ([]DRMOutput, error)              { return nil, ErrUnavailable }
func ListDRMOutputs(string) ([]DRMOutput, error)             { return nil, ErrUnavailable }
