//go:build linux && cgo

package apps

/*
#include "apps.h"
int worldr_apps_scale120(worldr_apps *s, uint32_t scale);
*/
import "C"

import "fmt"

// SetScale120 broadcasts the output's preferred fractional pixel density. A
// numerator of 120 is 1x, 150 is 1.25x, and 240 is 2x. Clients still choose their
// buffers and commit viewport destinations; preferences do not mutate surfaces.
func (s *Server) SetScale120(scale uint32) error {
	if err := s.valid(); err != nil {
		return err
	}
	if C.worldr_apps_scale120(s.ptr, C.uint32_t(scale)) != 0 {
		return fmt.Errorf("preferred scale numerator must be in [1,960]")
	}
	return nil
}
