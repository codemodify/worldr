//go:build linux && cgo

package native

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedDRMDescriptorsNeverAcquireOrReleaseMaster(t *testing.T) {
	compiler, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip(err)
	}
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "libdrm").Output()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "drm-fixture")
	args := []string{"-Wall", "-Wextra", "-Werror", "-o", path, "testdata/managed_drm_fixture.c"}
	args = append(args, strings.Fields(string(flags))...)
	if out, err := exec.Command(compiler, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile managed DRM boundary: %v\n%s", err, out)
	}
	if out, err := exec.Command(path).CombinedOutput(); err != nil {
		t.Fatalf("managed DRM ownership fixture: %v\n%s", err, out)
	}
}
func TestManagedDRMRejectsInvalidFDWithoutClosingCaller(t *testing.T) {
	if _, err := OpenDRMFromFD(-1); err == nil {
		t.Fatal("negative DRM FD accepted")
	}
	if _, err := OpenDRMPlanesFromFD(-1); err == nil {
		t.Fatal("negative DRM FD accepted")
	}
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, open := range []func(int) (*DRM, error){func(fd int) (*DRM, error) { return OpenDRMFromFD(fd) }, func(fd int) (*DRM, error) { return OpenDRMPlanesFromFD(fd) }} {
		if d, err := open(int(file.Fd())); err == nil {
			d.Close()
			t.Fatal("non-DRM descriptor accepted")
		}
		if _, err := file.Stat(); err != nil {
			t.Fatal("failed constructor closed caller's descriptor")
		}
	}
	if _, err := DRMOutputsFromFD(int(file.Fd())); err == nil {
		t.Fatal("non-DRM inventory accepted")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("inventory closed borrowed descriptor")
	}
}
